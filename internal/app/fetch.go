package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ContractorKeith/feeder/internal/storage"
	"github.com/mmcdole/gofeed"
	"golang.org/x/net/html/charset"
)

const MaxFeedBytes = 5 << 20

type FetchResult struct {
	FeedID int64  `json:"feed_id"`
	Status string `json:"status"`
	Items  int    `json:"items"`
	Error  string `json:"error,omitempty"`
}

func fetch(ctx context.Context, db *sql.DB, d Dependencies, timeout time.Duration) ([]FetchResult, error) {
	feeds, err := storage.List(ctx, db, false)
	if err != nil {
		return nil, err
	}
	results := []FetchResult{}
	for _, f := range feeds {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		items, etag, modified, unchanged, fetchErr := fetchOne(ctx, d.HTTP, timeout, f)
		r := FetchResult{FeedID: f.ID, Status: "ok", Items: len(items)}
		if fetchErr != nil {
			if err = ctx.Err(); err != nil {
				return nil, err
			}
			r.Status = "error"
			r.Error = fetchErr.Error()
			if err = storage.FailedFetch(ctx, db, f.ID, d.Now(), r.Error); err != nil {
				return nil, err
			}
		} else if err = storage.CommitFetch(ctx, db, f, items, d.Now(), etag, modified, unchanged); err != nil {
			return nil, err
		} else if unchanged {
			r.Status = "not_modified"
		}
		results = append(results, r)
	}
	return results, nil
}
func fetchOne(ctx context.Context, client *http.Client, timeout time.Duration, f storage.Feed) ([]storage.Item, string, string, bool, error) {
	fail := func(message string) ([]storage.Item, string, string, bool, error) {
		return nil, "", "", false, errors.New(message)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// Clone injected clients to enforce bounds without mutating shared transports.
	bounded := *client
	bounded.Timeout = timeout
	bounded.Jar = nil
	bounded.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 {
			return errors.New("redirect limit")
		}
		if _, err := FeedURL(req.URL.String()); err != nil {
			return errors.New("invalid redirect")
		}
		// No credentials or cookies, including headers added by an injected client.
		req.Header.Del("Authorization")
		req.Header.Del("Cookie")
		if len(via) > 0 && (req.URL.Scheme != via[0].URL.Scheme || req.URL.Host != via[0].URL.Host) {
			req.Header.Del("If-None-Match")
			req.Header.Del("If-Modified-Since")
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.URL, nil)
	if err != nil {
		return fail("invalid request")
	}
	req.Header.Set("User-Agent", "feeder/1")
	if f.Initialized {
		if f.ETag != "" {
			req.Header.Set("If-None-Match", f.ETag)
		}
		if f.Modified != "" {
			req.Header.Set("If-Modified-Since", f.Modified)
		}
	}
	resp, err := bounded.Do(req)
	if err != nil {
		return fail("feed transport failed (timeout, redirect, or connection)")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		if !f.Initialized {
			return fail("unexpected 304 before initial successful fetch")
		}
		return nil, "", "", true, nil
	}
	if resp.StatusCode != http.StatusOK {
		return fail(fmt.Sprintf("feed HTTP status %d", resp.StatusCode))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxFeedBytes+1))
	if err != nil {
		return fail("feed body read failed")
	}
	if len(body) > MaxFeedBytes {
		return fail("feed body exceeds 5 MiB limit")
	}
	if err := completeFeedDocument(body); err != nil {
		return fail("invalid RSS or Atom feed document")
	}
	parsed, err := gofeed.NewParser().ParseString(string(body))
	if err != nil || (parsed.FeedType != "rss" && parsed.FeedType != "atom") {
		return fail("invalid RSS or Atom feed")
	}
	base := resp.Request.URL
	items := make([]storage.Item, 0, len(parsed.Items))
	for _, entry := range parsed.Items {
		link := ""
		if u, err := url.Parse(entry.Link); err == nil && entry.Link != "" {
			resolved := base.ResolveReference(u)
			if _, err = FeedURL(resolved.String()); err == nil {
				link = resolved.String()
			}
		}
		published := ""
		if entry.PublishedParsed != nil {
			published = entry.PublishedParsed.UTC().Format(time.RFC3339Nano)
		}
		title := PlainText(entry.Title)
		content := entry.Content
		if content == "" {
			content = entry.Description
		}
		content = PlainText(content)
		key := "guid:" + entry.GUID
		if entry.GUID == "" {
			key = "link:" + link
			if link == "" {
				key = fmt.Sprintf("fallback:%x", sha256.Sum256([]byte(title+"\x00"+entry.Published)))
			}
		}
		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(title+"\x00"+link+"\x00"+content)))
		items = append(items, storage.Item{Key: key, Title: title, URL: link, Content: content, Hash: hash, Published: published})
	}
	return items, resp.Header.Get("ETag"), resp.Header.Get("Last-Modified"), false, nil
}

// gofeed stops at the feed's closing element. Check the entire bounded body so
// a valid prefix cannot hide malformed trailing data or another document.
func completeFeedDocument(body []byte) error {
	d := xml.NewDecoder(bytes.NewReader(bytes.TrimPrefix(body, []byte{0xef, 0xbb, 0xbf})))
	d.CharsetReader = charset.NewReaderLabel
	// Strict XML tokenization rejects mismatched nesting instead of inserting
	// synthetic closing tags; charset decoding and UTF-8 BOM support remain.
	d.Strict = true
	depth, roots := 0, 0
	for {
		token, err := d.Token()
		if err == io.EOF && roots == 1 && depth == 0 {
			return nil
		}
		if err != nil {
			return err
		}
		switch v := token.(type) {
		case xml.StartElement:
			if depth == 0 {
				roots++
				if roots > 1 {
					return errors.New("multiple feed roots")
				}
			}
			depth++
		case xml.EndElement:
			depth--
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(v)) != "" {
				return errors.New("text outside feed root")
			}
		}
	}
}
