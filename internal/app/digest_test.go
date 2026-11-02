package app

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/ContractorKeith/feeder/internal/storage"
)

func TestCalendarWindows(t *testing.T) {
	for _, tt := range []struct {
		name, now, zone, period, from, to string
		hours                             int
	}{
		{"spring", "2026-03-09T12:00:00Z", "America/New_York", "daily", "2026-03-08T05:00:00Z", "2026-03-09T04:00:00Z", 23},
		{"fall", "2026-11-02T12:00:00Z", "America/New_York", "daily", "2026-11-01T04:00:00Z", "2026-11-02T05:00:00Z", 25},
		{"year", "2027-01-01T12:00:00Z", "UTC", "daily", "2026-12-31T00:00:00Z", "2027-01-01T00:00:00Z", 24},
		{"weekly year", "2027-01-04T12:00:00Z", "UTC", "weekly", "2026-12-28T00:00:00Z", "2027-01-04T00:00:00Z", 168},
		{"weekly Sunday", "2027-01-03T12:00:00Z", "UTC", "weekly", "2026-12-21T00:00:00Z", "2026-12-28T00:00:00Z", 168},
		{"weekly spring", "2026-03-09T12:00:00Z", "America/New_York", "weekly", "2026-03-02T05:00:00Z", "2026-03-09T04:00:00Z", 167},
		{"weekly fall", "2026-11-02T12:00:00Z", "America/New_York", "weekly", "2026-10-26T04:00:00Z", "2026-11-02T05:00:00Z", 169},
	} {
		t.Run(tt.name, func(t *testing.T) {
			now, _ := time.Parse(time.RFC3339, tt.now)
			w, err := CalendarWindow(now, tt.zone, tt.period, "", "")
			if err != nil || w.From.Format(time.RFC3339) != tt.from || w.To.Format(time.RFC3339) != tt.to || w.To.Sub(w.From) != time.Duration(tt.hours)*time.Hour {
				t.Fatal(w, err)
			}
		})
	}
	for _, pair := range [][2]string{{"2026-02-30", "2026-03-01"}, {"2026-01-01", ""}, {"2026-1-01", "2026-01-02"}, {"2026-01-02", "2026-01-02"}, {"2026-01-03", "2026-01-02"}} {
		if _, err := CalendarWindow(time.Now(), "UTC", "daily", pair[0], pair[1]); err == nil {
			t.Fatal(pair)
		}
	}
	w, err := CalendarWindow(time.Now(), "America/New_York", "daily", "2026-11-01", "2026-11-02")
	if err != nil || w.To.Sub(w.From) != 25*time.Hour {
		t.Fatal(w, err)
	}
}

func TestSelectionParsedInstantsAndBoundaries(t *testing.T) {
	start, _ := time.Parse(time.RFC3339, "2026-11-02T00:00:00Z")
	feeds := []storage.Feed{{ID: 1, Tags: []string{"tech"}}, {ID: 2, Tags: []string{"go"}}}
	items := []storage.Item{
		{ID: 1, FeedID: 1, FirstSeen: "2026-11-02T00:00:00Z", Title: "at start", URL: "https://example.org/a"},
		{ID: 2, FeedID: 2, FirstSeen: "2026-11-02T00:00:00.000000001Z", Title: "new representative", URL: "https://example.org/a"},
		{ID: 3, FeedID: 1, FirstSeen: "2026-11-02T01:00:00+01:00", Title: "same instant higher ID"},
		{ID: 4, FeedID: 1, FirstSeen: "2026-11-01T23:59:59.999999999Z"},
		{ID: 5, FeedID: 1, FirstSeen: "2026-11-03T00:00:00Z"},
		{ID: 6, FeedID: 1, FirstSeen: "2026-11-02T23:59:59.999999999Z", Baseline: true},
	}
	o := DigestOptions{Window: Window{From: start, To: start.AddDate(0, 0, 1), Timezone: "UTC"}, Limit: 20}
	got, err := SelectDigest(items, feeds, o)
	if err != nil || len(got) != 2 || got[0].ItemID != 2 || len(got[0].Sources) != 2 || got[1].ItemID != 3 {
		t.Fatal(got, err)
	}
	o.IncludeBaseline = true
	got, err = SelectDigest(items, feeds, o)
	if err != nil || len(got) != 3 || got[0].ItemID != 6 {
		t.Fatal(got, err)
	}
	o.Tags = []string{"tech"}
	o.FeedIDs = []int64{2}
	got, err = SelectDigest(items, feeds, o)
	if err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
}

func TestExcerptUnicodeBounds(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		size   int
		label  string
		length int
	}{{0, "absent", 0}, {199, "short", 199}, {200, "feed_content", 200}, {600, "feed_content", 600}, {601, "feed_content", 601}} {
		content := strings.Repeat("界", tt.size)
		got, err := SelectDigest([]storage.Item{{ID: 1, FeedID: 1, FirstSeen: now.Format(time.RFC3339Nano), Content: content}}, []storage.Feed{{ID: 1}}, DigestOptions{Window: Window{From: now, To: now.Add(time.Hour)}, Limit: 1})
		if err != nil || len(got) != 1 || got[0].LimitedContent != tt.label || len([]rune(got[0].Text)) != tt.length || !utf8.ValidString(got[0].Text) {
			t.Fatal(tt, got, err)
		}
		if tt.size > 600 && !strings.HasSuffix(got[0].Text, "…") {
			t.Fatal(got)
		}
	}
}
