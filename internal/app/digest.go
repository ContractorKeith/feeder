package app

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/ContractorKeith/feeder/internal/storage"
	"github.com/ContractorKeith/feeder/internal/summarizer"
)

// Window is a half-open interval of instants derived from local calendar dates.
type Window struct {
	From     time.Time `json:"from"`
	To       time.Time `json:"to"`
	Timezone string    `json:"timezone"`
}

func CalendarWindow(now time.Time, zone, period, from, to string) (Window, error) {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return Window{}, Invalid(errors.New("invalid timezone"))
	}
	if period != "daily" && period != "weekly" {
		return Window{}, Invalid(errors.New("period must be daily or weekly"))
	}
	w := Window{Timezone: zone}
	if from != "" || to != "" {
		w.From, err = time.ParseInLocation("2006-01-02", from, loc)
		if err != nil {
			return w, Invalid(errors.New("from must be a YYYY-MM-DD date"))
		}
		w.To, err = time.ParseInLocation("2006-01-02", to, loc)
		if err != nil || !w.From.Before(w.To) {
			return w, Invalid(errors.New("to must be a YYYY-MM-DD date after from"))
		}
	} else {
		local := now.In(loc)
		w.To = time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
		days := 1
		if period == "weekly" {
			w.To = w.To.AddDate(0, 0, -(int(local.Weekday())+6)%7)
			days = 7
		}
		w.From = w.To.AddDate(0, 0, -days)
	}
	w.From, w.To = w.From.UTC(), w.To.UTC()
	return w, nil
}

type DigestOptions struct {
	Window          Window
	FeedIDs         []int64
	Tags            []string
	IncludeBaseline bool
	Limit           int
}
type Source struct {
	FeedID     int64  `json:"feed_id"`
	ItemID     int64  `json:"item_id"`
	Title      string `json:"title"`
	FeedURL    string `json:"feed_url"`
	ArticleURL string `json:"article_url"`
}
type DigestEntry struct {
	ItemID         int64     `json:"item_id"`
	Title          string    `json:"title"`
	URL            string    `json:"url"`
	FirstSeen      time.Time `json:"first_seen"`
	Text           string    `json:"text"`
	TextKind       string    `json:"text_kind"`
	LimitedContent string    `json:"limited_content"`
	Sources        []Source  `json:"sources"`
}
type Coverage struct {
	Attempted int           `json:"attempted"`
	Succeeded int           `json:"succeeded"`
	Failed    int           `json:"failed"`
	Feeds     []FetchResult `json:"feeds"`
}
type Digest struct {
	Window       Window        `json:"window"`
	Entries      []DigestEntry `json:"entries"`
	Coverage     Coverage      `json:"coverage"`
	GeneratedAt  time.Time     `json:"generated_at"`
	ContentScope string        `json:"content_scope"`
	Hint         string        `json:"hint,omitempty"`
}

func validateFilters(feeds []storage.Feed, o DigestOptions) error {
	for _, id := range o.FeedIDs {
		found := false
		for _, f := range feeds {
			if f.ID == id {
				found = true
				break
			}
		}
		if !found {
			return Invalid(fmt.Errorf("unknown feed ID %d", id))
		}
	}
	return nil
}

// GenerateDigest holds one command lock, calling fetch directly rather than
// recursively acquiring the lock through Collection.Fetch.
func (c Collection) GenerateDigest(o DigestOptions) (Digest, error) {
	result := Digest{Window: o.Window, Entries: []DigestEntry{}, ContentScope: "Feed-provided content only; linked full articles are not fetched or verified."}
	settings, err := Read(c.Home, c.Dependencies, c.Overrides)
	if err != nil {
		return result, err
	}
	modelSettings, err := summarizer.Prepare(settings, c.Dependencies.Getenv)
	if err != nil {
		return result, Invalid(err)
	}
	loc, zoneErr := time.LoadLocation(o.Window.Timezone)
	if zoneErr != nil {
		return result, Invalid(errors.New("invalid digest timezone"))
	}
	if o.Limit < 1 || o.Limit > 200 || !o.Window.From.Before(o.Window.To) {
		return result, Invalid(errors.New("invalid digest window or limit"))
	}
	if o.Tags, err = Tags(o.Tags); err != nil {
		return result, err
	}
	// Validate IDs against committed state before any lock/state file is created.
	feeds, err := c.List(true)
	if err != nil {
		return result, err
	}
	if err = validateFilters(feeds, o); err != nil {
		return result, err
	}
	wait, _ := time.ParseDuration(settings["lock_timeout"].(string))
	release, err := storage.Lock(c.Context, c.Home, wait)
	if err != nil {
		return result, err
	}
	defer release()
	// Recheck IDs under the lock before opening a writable store/migrating it.
	readDB, err := storage.OpenRead(c.Context, c.Home)
	if err == nil {
		feeds, err = storage.List(c.Context, readDB, true)
		readDB.Close()
	} else if errors.Is(err, os.ErrNotExist) {
		err = nil
		feeds = nil
	}
	if err != nil {
		return result, err
	}
	if err = validateFilters(feeds, o); err != nil {
		return result, err
	}
	db, err := storage.Open(c.Context, c.Home, c.Dependencies.Now())
	if err != nil {
		return result, err
	}
	defer db.Close()
	timeout, _ := time.ParseDuration(settings["feed_timeout"].(string))
	results, err := fetch(c.Context, db, c.Dependencies, timeout)
	if err != nil {
		return result, err
	}
	result.Coverage = Coverage{Attempted: len(results), Feeds: results}
	for _, r := range results {
		if r.Status == "error" {
			result.Coverage.Failed++
		} else {
			result.Coverage.Succeeded++
		}
	}
	items, err := storage.Items(c.Context, db)
	if err != nil {
		return result, err
	}
	result.Entries, err = SelectDigest(items, feeds, o)
	if err != nil {
		return result, err
	}
	if settings["summarize"] == true {
		byID := map[int64]storage.Item{}
		for _, item := range items {
			byID[item.ID] = item
		}
		for i := range result.Entries {
			if err = c.Context.Err(); err != nil {
				return result, err
			}
			entry := &result.Entries[i]
			content := byID[entry.ItemID].Content
			if content == "" {
				continue
			}
			key := modelSettings.CacheKey(content)
			text, cacheErr := storage.Summary(c.Context, db, entry.ItemID, key)
			if cacheErr != nil {
				return result, cacheErr
			}
			if text == "" {
				text, err = modelSettings.Summarize(c.Context, c.Dependencies.HTTP, content)
				if c.Context.Err() != nil {
					return result, c.Context.Err()
				}
				if err != nil {
					entry.TextKind = "fallback"
					continue
				}
				if err = storage.SaveSummary(c.Context, db, entry.ItemID, key, text); err != nil {
					return result, err
				}
			}
			entry.Text, entry.TextKind = text, "summary"
		}
	}
	result.GeneratedAt = c.Dependencies.Now().UTC()
	if len(result.Entries) == 0 {
		local := result.GeneratedAt.In(loc)
		result.Hint = fmt.Sprintf("Initial fetches establish baseline. Preview today's baseline: feeder digest --timezone %s --from %s --to %s --include-baseline", o.Window.Timezone, local.Format("2006-01-02"), local.AddDate(0, 0, 1).Format("2006-01-02"))
		if len(feeds) == 0 {
			result.Hint = "No subscriptions. Add one with: feeder feeds add https://example.org/feed.xml. " + result.Hint
		}
	}
	return result, nil
}

// SelectDigest filters before grouping and limiting. Timestamps are parsed:
// RFC3339Nano strings do not have chronological lexical ordering.
func SelectDigest(items []storage.Item, feeds []storage.Feed, o DigestOptions) ([]DigestEntry, error) {
	byID := map[int64]storage.Feed{}
	for _, f := range feeds {
		byID[f.ID] = f
	}
	type candidate struct {
		item storage.Item
		seen time.Time
	}
	eligible := []candidate{}
	for _, item := range items {
		if item.Baseline && !o.IncludeBaseline {
			continue
		}
		f := byID[item.FeedID]
		if len(o.FeedIDs) > 0 {
			match := false
			for _, id := range o.FeedIDs {
				if id == f.ID {
					match = true
				}
			}
			if !match {
				continue
			}
		}
		if len(o.Tags) > 0 {
			match := false
			for _, tag := range o.Tags {
				for _, ft := range f.Tags {
					if tag == ft {
						match = true
					}
				}
			}
			if !match {
				continue
			}
		}
		seen, err := time.Parse(time.RFC3339Nano, item.FirstSeen)
		if err != nil {
			return nil, errors.New("invalid stored first-seen timestamp")
		}
		if seen.Before(o.Window.From) || !seen.Before(o.Window.To) {
			continue
		}
		eligible = append(eligible, candidate{item, seen})
	}
	sort.Slice(eligible, func(i, j int) bool {
		if eligible[i].seen.Equal(eligible[j].seen) {
			return eligible[i].item.ID > eligible[j].item.ID
		}
		return eligible[i].seen.After(eligible[j].seen)
	})
	entries := []DigestEntry{}
	groups := map[string]int{}
	for _, candidate := range eligible {
		item := candidate.item
		f := byID[item.FeedID]
		source := Source{FeedID: f.ID, ItemID: item.ID, Title: f.Title, FeedURL: f.URL, ArticleURL: item.URL}
		if index, ok := groups[item.URL]; ok && item.URL != "" {
			entries[index].Sources = append(entries[index].Sources, source)
			continue
		}
		label := "feed_content"
		if utf8.RuneCountInString(item.Content) == 0 {
			label = "absent"
		} else if utf8.RuneCountInString(item.Content) < 200 {
			label = "short"
		}
		text := []rune(item.Content)
		if len(text) > 600 {
			text = append(text[:600], '…')
		}
		entries = append(entries, DigestEntry{ItemID: item.ID, Title: item.Title, URL: item.URL, FirstSeen: candidate.seen.UTC(), Text: string(text), TextKind: "excerpt", LimitedContent: label, Sources: []Source{source}})
		if item.URL != "" {
			groups[item.URL] = len(entries) - 1
		}
	}
	if len(entries) > o.Limit {
		entries = entries[:o.Limit]
	}
	return entries, nil
}
