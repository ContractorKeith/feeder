package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ContractorKeith/feeder/internal/app"
	"github.com/ContractorKeith/feeder/internal/cli"
)

type digestEnvelope struct {
	SchemaVersion int `json:"schema_version"`
	Status        string
	Data          app.Digest
	Warnings      []string
	Errors        []struct {
		Code   string
		FeedID int64 `json:"feed_id"`
	}
}

func decodeDigest(t *testing.T, r result) digestEnvelope {
	t.Helper()
	var e digestEnvelope
	if err := json.Unmarshal([]byte(r.out), &e); err != nil {
		t.Fatal(r, err)
	}
	if e.SchemaVersion != 1 || e.Warnings == nil || e.Errors == nil || e.Data.Entries == nil || e.Data.Coverage.Feeds == nil {
		t.Fatal(r)
	}
	return e
}
func TestDigestDailyWeeklyFiltersAndOutput(t *testing.T) {
	c := newCollection(t)
	body := rss("")
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++; fmt.Fprint(w, body) }))
	defer server.Close()
	success(t, c.call("init", "--timezone", "America/New_York"))
	success(t, c.call("feeds", "add", server.URL+"/one", "--tag", "tech"))
	success(t, c.call("feeds", "add", server.URL+"/two", "--tag", "go"))
	c.now = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	success(t, c.call("fetch")) // Successful empty fetch establishes baseline.
	c.now = time.Date(2026, 11, 1, 5, 0, 0, 0, time.UTC)
	body = rss(entry("one", "Older", "old content"))
	success(t, c.call("fetch"))
	c.now = c.now.Add(time.Nanosecond)
	body = rss(entry("one", "Edited", "edited content") + `<item><guid>two</guid><title>Newest [title]</title><link>https://example.org/shared?a=1&amp;b=2</link><description><![CDATA[**untrusted** [click](https://bad.invalid)]]></description></item>` + `<item><guid>missing</guid><title>No link</title></item>`)
	success(t, c.call("fetch"))
	c.now = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	before := requests
	r := c.call("digest", "--json")
	success(t, r)
	e := decodeDigest(t, r)
	if requests != before+2 || len(e.Data.Entries) != 4 || e.Data.Window.Timezone != "America/New_York" || e.Data.Window.To.Sub(e.Data.Window.From) != 25*time.Hour {
		t.Fatal(requests, before, r)
	}
	if len(e.Data.Entries[1].Sources) != 2 || e.Data.Entries[1].Title != "Newest [title]" || e.Data.Entries[1].URL != "https://example.org/shared?a=1&b=2" {
		t.Fatal(e.Data.Entries)
	}
	r = c.call("digest", "--period", "weekly", "--limit", "3", "--json")
	success(t, r)
	e = decodeDigest(t, r)
	if len(e.Data.Entries) != 3 || e.Data.Window.From.Format(time.RFC3339) != "2026-10-26T04:00:00Z" || len(e.Data.Entries[1].Sources) != 2 {
		t.Fatal(r)
	}
	r = c.call("digest", "--feed", "1", "--feed", "2", "--tag", "tech", "--tag", "other", "--json")
	success(t, r)
	e = decodeDigest(t, r)
	if len(e.Data.Entries) != 3 {
		t.Fatal(r)
	}
	for _, entry := range e.Data.Entries {
		for _, s := range entry.Sources {
			if s.FeedID != 1 {
				t.Fatal(r)
			}
		}
	}
	r = c.call("digest", "--feed", "2", "--tag", "tech", "--json")
	success(t, r)
	if len(decodeDigest(t, r).Data.Entries) != 0 {
		t.Fatal(r)
	}
	r = c.call("digest", "--output", "-")
	success(t, r)
	if !strings.Contains(r.out, `\*\*untrusted\*\* \[click\]`) || !strings.Contains(r.out, "https://example.org/shared?a=1&amp;b=2") || !strings.Contains(r.out, "link unavailable") || !strings.Contains(r.out, "Excerpt — absent") || !strings.Contains(r.out, "Excerpt — short") || !strings.Contains(r.out, "not fetched or verified") {
		t.Fatal(r)
	}
	path := filepath.Join(t.TempDir(), "digest.json")
	r = c.call("digest", "--json", "--output", path)
	success(t, r)
	if r.out != "" || r.err != "" {
		t.Fatal(r)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	decodeDigest(t, result{out: string(data)})
	before = requests
	if r = c.call("digest", "--output", path); r.code != 2 || requests != before {
		t.Fatal(r)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(data, after) {
		t.Fatal("clobbered output")
	}
	success(t, c.call("digest", "--output", path, "--force"))
	success(t, c.call("feeds", "remove", "1"))
	success(t, c.call("feeds", "remove", "2"))
	before = requests
	r = c.call("digest", "--feed", "1", "--json")
	success(t, r)
	if requests != before || len(decodeDigest(t, r).Data.Entries) != 3 {
		t.Fatal(r)
	}
}

func TestDigestBaselineRefreshAndDegraded(t *testing.T) {
	c := newCollection(t)
	body := rss(entry("base", "Baseline", "teaser"))
	fail := false
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if fail {
			http.Error(w, "failure", 500)
			return
		}
		fmt.Fprint(w, body)
	}))
	defer server.Close()
	success(t, c.call("feeds", "add", server.URL))
	r := c.call("digest", "--from", "2026-11-02", "--to", "2026-11-03", "--json")
	success(t, r)
	e := decodeDigest(t, r)
	if len(e.Data.Entries) != 0 || !strings.Contains(e.Data.Hint, "--from 2026-11-02 --to 2026-11-03 --include-baseline") || calls != 1 {
		t.Fatal(r)
	}
	r = c.call("digest", "--from", "2026-11-02", "--to", "2026-11-03", "--include-baseline", "--json")
	success(t, r)
	if len(decodeDigest(t, r).Data.Entries) != 1 {
		t.Fatal(r)
	}
	r = c.call("digest", "--include-baseline", "--json")
	success(t, r)
	if len(decodeDigest(t, r).Data.Entries) != 0 {
		t.Fatal("baseline bypassed window", r)
	}
	c.now = c.now.AddDate(0, 0, 1)
	// First-seen today cannot enter yesterday even when publisher says otherwise.
	body = rss(entry("base", "Baseline", "teaser") + `<item><guid>late</guid><title>Late</title><pubDate>Mon, 02 Nov 2026 10:00:00 GMT</pubDate></item>`)
	r = c.call("digest", "--include-baseline", "--json")
	success(t, r)
	if len(decodeDigest(t, r).Data.Entries) != 1 || len(c.items()) != 2 {
		t.Fatal(r)
	}
	fail = true
	r = c.call("digest", "--include-baseline", "--json")
	e = decodeDigest(t, r)
	if r.code != 3 || e.Status != "degraded" || e.Data.Coverage.Failed != 1 || len(e.Data.Entries) != 1 || len(e.Errors) != 1 || len(e.Warnings) != 1 {
		t.Fatal(r)
	}
	r = c.call("digest", "--json")
	e = decodeDigest(t, r)
	if r.code != 3 || len(e.Data.Entries) != 0 || e.Data.Coverage.Failed != 1 {
		t.Fatal(r)
	}
	// Another active feed commits independently while HTTP failures persist.
	success(t, c.call("feeds", "add", server.URL+"/ok"))
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, rss(entry("good", "Successful baseline", "provided text")))
	}))
	defer good.Close()
	success(t, c.call("feeds", "add", good.URL))
	r = c.call("digest", "--include-baseline", "--json")
	e = decodeDigest(t, r)
	if r.code != 3 || e.Data.Coverage.Succeeded != 1 || e.Data.Coverage.Failed != 2 || !c.feeds()[2].Initialized || len(c.items()) != 3 {
		t.Fatal(r)
	}
	var diag bytes.Buffer
	code := cli.Execute(context.Background(), []string{"--home", c.home, "digest", "--include-baseline"}, brokenWriter{}, &diag, app.Dependencies{Now: func() time.Time { return c.now }, Getenv: func(string) string { return "" }}, "test")
	if code != 1 {
		t.Fatal("output failure did not outrank partial", code, diag.String())
	}
}

func TestDigestInvalidBeforeNetworkOrMutation(t *testing.T) {
	c := newCollection(t)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; fmt.Fprint(w, rss("")) }))
	defer server.Close()
	success(t, c.call("feeds", "add", server.URL))
	for _, args := range [][]string{
		{"--from", "2026-11-02"}, {"--from", "2026-02-30", "--to", "2026-03-01"}, {"--from", "", "--to", ""},
		{"--period", "weekly", "--from", "2026-11-02", "--to", "2026-11-03"}, {"--period", "monthly"}, {"--timezone", "bad/zone"},
		{"--limit", "0"}, {"--limit", "201"}, {"--feed", "0"}, {"--feed", "1.5"}, {"--feed", "999"}, {"--tag", ""},
		{"--summarize"}, {"--summarize", "--model", "test"}, {"--output", filepath.Join(c.home, "feeder.db"), "--force"},
	} {
		before, _ := json.Marshal(c.feeds()[0])
		r := c.call(append([]string{"digest", "--json"}, args...)...)
		after, _ := json.Marshal(c.feeds()[0])
		if r.code != 2 || calls != 0 || !bytes.Equal(before, after) {
			t.Fatal(args, r, calls)
		}
	}
	c.home = filepath.Join(t.TempDir(), "absent")
	for _, args := range [][]string{{"--feed", "1"}, {"--from", "bad", "--to", "bad"}} {
		r := c.call(append([]string{"digest"}, args...)...)
		if r.code != 2 {
			t.Fatal(r)
		}
		if _, err := os.Stat(c.home); !os.IsNotExist(err) {
			t.Fatal("invalid created state", err)
		}
	}
	r := c.call("digest", "--json")
	success(t, r)
	e := decodeDigest(t, r)
	if len(e.Data.Entries) != 0 || !strings.Contains(e.Data.Hint, "No subscriptions") || e.Data.Coverage.Attempted != 0 {
		t.Fatal(r)
	}
	r = c.call("digest")
	success(t, r)
	if !strings.Contains(r.out, "No eligible items") || !strings.Contains(r.err, "No subscriptions") {
		t.Fatal(r)
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("fixture output failure") }

func TestDigestMalformedCoverageAndSettingOverrides(t *testing.T) {
	c := newCollection(t)
	body := rss("")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
	defer server.Close()
	success(t, c.call("feeds", "add", server.URL))
	success(t, c.call("fetch"))
	c.now = c.now.Add(-time.Hour)
	body = rss(entry("one", "Stored", "provided content"))
	success(t, c.call("fetch"))
	success(t, c.call("config", "set", "timezone", "America/New_York"))
	c.now = c.now.Add(time.Hour)
	body = rss("") + "<broken>"
	r := c.call("digest", "--timezone", "UTC", "--from", "2026-11-02", "--to", "2026-11-03", "--json")
	e := decodeDigest(t, r)
	if r.code != 3 || e.Data.Coverage.Failed != 1 || len(e.Data.Entries) != 1 || e.Data.Window.Timezone != "UTC" || len(c.items()) != 1 {
		t.Fatal(r)
	}
	if got := data(t, c.call("config", "show", "--json"))["timezone"]; got != "America/New_York" {
		t.Fatal("override persisted", got)
	}
	body = rss("")
	var out, diag bytes.Buffer
	code := cli.Execute(context.Background(), []string{"--home", c.home, "digest", "--timezone", "UTC", "--from", "2026-11-02", "--to", "2026-11-03", "--json"}, &out, &diag, app.Dependencies{Now: func() time.Time { return c.now }, Getenv: func(k string) string {
		if k == "FEEDER_LIMIT" {
			return "1"
		}
		return ""
	}}, "test")
	if code != 0 || len(decodeDigest(t, result{out: out.String()}).Data.Entries) != 1 {
		t.Fatal(code, out.String(), diag.String())
	}
}

func TestDigestLimitsAndPrecedence(t *testing.T) {
	c := newCollection(t)
	body := rss("")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
	defer server.Close()
	success(t, c.call("feeds", "add", server.URL))
	success(t, c.call("fetch"))
	var items strings.Builder
	for i := 0; i < 205; i++ {
		fmt.Fprintf(&items, "<item><guid>%d</guid><title>Item %d</title></item>", i, i)
	}
	body = rss(items.String())
	args := []string{"digest", "--from", "2026-11-02", "--to", "2026-11-03", "--json"}
	r := c.call(args...)
	success(t, r)
	e := decodeDigest(t, r)
	if len(e.Data.Entries) != 20 || e.Data.Entries[0].ItemID != 205 {
		t.Fatal(r)
	}
	r = c.call(append(args, "--limit", "200")...)
	success(t, r)
	if len(decodeDigest(t, r).Data.Entries) != 200 {
		t.Fatal(r)
	}
	success(t, c.call("config", "set", "limit", "30"))
	r = c.call(args...)
	success(t, r)
	if len(decodeDigest(t, r).Data.Entries) != 30 {
		t.Fatal(r)
	}
	for _, override := range []bool{false, true} {
		var out, diag bytes.Buffer
		callArgs := append([]string{"--home", c.home}, args...)
		expected := 11
		if override {
			callArgs = append(callArgs, "--limit", "4")
			expected = 4
		}
		code := cli.Execute(context.Background(), callArgs, &out, &diag, app.Dependencies{Now: func() time.Time { return c.now }, Getenv: func(k string) string {
			if k == "FEEDER_LIMIT" {
				return "11"
			}
			return ""
		}}, "test")
		if code != 0 || len(decodeDigest(t, result{out: out.String()}).Data.Entries) != expected {
			t.Fatal(code, out.String(), diag.String())
		}
	}
}

func TestDigestLateOutputFailureOutranksCoverage(t *testing.T) {
	c := newCollection(t)
	path := filepath.Join(t.TempDir(), "result.md")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := os.WriteFile(path, []byte("concurrent result"), 0600); err != nil {
			t.Error(err)
		}
		http.Error(w, "failure", 500)
	}))
	defer server.Close()
	success(t, c.call("feeds", "add", server.URL))
	r := c.call("digest", "--output", path, "--json")
	if r.code != 1 || !strings.Contains(r.out, `"status":"error"`) {
		t.Fatal(r)
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "concurrent result" {
		t.Fatal(string(b), err)
	}
	files, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(files) != 1 {
		t.Fatal(files, err)
	}
}

func TestDigestMarkdownPreservesArticleURL(t *testing.T) {
	c := newCollection(t)
	raw := "https://example.org/story?q=&copy;&literal=<value>&x=(a)#part"
	body := rss(`<item><guid>one</guid><title>Article</title><link>https://example.org/story?q=&amp;copy;&amp;literal=&lt;value&gt;&amp;x=(a)#part</link></item>`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
	defer server.Close()
	success(t, c.call("feeds", "add", server.URL))
	args := []string{"digest", "--from", "2026-11-02", "--to", "2026-11-03", "--include-baseline"}
	r := c.call(append(args, "--json")...)
	success(t, r)
	e := decodeDigest(t, r)
	if len(e.Data.Entries) != 1 || e.Data.Entries[0].URL != raw || e.Data.Entries[0].Sources[0].ArticleURL != raw {
		t.Fatal(r)
	}
	r = c.call(args...)
	success(t, r)
	if !strings.Contains(r.out, "[Article](<https://example.org/story?q=&amp;copy;&amp;literal=&lt;value&gt;&amp;x=(a)#part>)") {
		t.Fatal(r)
	}
}
