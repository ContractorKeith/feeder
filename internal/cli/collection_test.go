package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ContractorKeith/feeder/internal/app"
	"github.com/ContractorKeith/feeder/internal/cli"
	"github.com/ContractorKeith/feeder/internal/storage"
)

type collection struct {
	t     *testing.T
	home  string
	now   time.Time
	stdin string
}

func newCollection(t *testing.T) *collection {
	return &collection{t: t, home: t.TempDir(), now: time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)}
}
func (c *collection) call(args ...string) result {
	c.t.Helper()
	var out, diag bytes.Buffer
	code := cli.Execute(context.Background(), append([]string{"--home", c.home}, args...), &out, &diag, app.Dependencies{Now: func() time.Time { return c.now }, Getenv: func(string) string { return "" }, Stdin: strings.NewReader(c.stdin)}, "test")
	return result{code, out.String(), diag.String()}
}
func (c *collection) items() []storage.Item {
	c.t.Helper()
	db, err := storage.OpenRead(context.Background(), c.home)
	if err != nil {
		c.t.Fatal(err)
	}
	defer db.Close()
	items, err := storage.Items(context.Background(), db)
	if err != nil {
		c.t.Fatal(err)
	}
	return items
}
func (c *collection) feeds() []storage.Feed {
	c.t.Helper()
	r := c.call("feeds", "list", "--all", "--json")
	success(c.t, r)
	var e struct{ Data []storage.Feed }
	if err := json.Unmarshal([]byte(r.out), &e); err != nil {
		c.t.Fatal(err)
	}
	return e.Data
}
func rss(items string) string {
	return `<?xml version="1.0"?><rss version="2.0"><channel><title>Fixture</title><link>https://example.org</link><description>fixture</description>` + items + `</channel></rss>`
}
func entry(id, title, content string) string {
	return fmt.Sprintf(`<item><guid>%s</guid><title>%s</title><link>/story?track=1</link><description><![CDATA[%s]]></description></item>`, id, title, content)
}

func TestCollectionBaselineEditsValidatorsAndHistory(t *testing.T) {
	c := newCollection(t)
	body := rss(entry("one", "Original", "<p>Hello 世界</p><script>bad()</script><style>bad</style>"))
	conditional := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rss" {
			t.Error("downloaded article", r.URL)
			http.Error(w, "article", 500)
			return
		}
		if conditional {
			if r.Header.Get("If-None-Match") != "v2" || r.Header.Get("If-Modified-Since") != "Mon, 02 Nov 2026 12:00:00 GMT" {
				t.Error("missing validators", r.Header)
			}
			w.WriteHeader(304)
			return
		}
		w.Header().Set("ETag", "v2")
		w.Header().Set("Last-Modified", "Mon, 02 Nov 2026 12:00:00 GMT")
		fmt.Fprint(w, body)
	}))
	defer server.Close()
	r := c.call("feeds", "add", server.URL+"/rss", "--tag", "tech", "--json")
	success(t, r)
	t.Log("add:", r.out)
	if c.feeds()[0].Initialized {
		t.Fatal("add initialized baseline")
	}
	r = c.call("fetch", "--json")
	success(t, r)
	t.Log("add -> baseline fetch:", r.out)
	initial := c.items()
	if len(initial) != 1 || !initial[0].Baseline || initial[0].Content != "Hello 世界" || initial[0].URL != server.URL+"/story?track=1" {
		t.Fatal(initial)
	}
	c.now = c.now.Add(time.Hour)
	body = rss(entry("one", "Edited", "changed") + entry("two", "New", "new content"))
	r = c.call("fetch", "--json")
	success(t, r)
	t.Log("new-item fetch:", r.out)
	items := c.items()
	if len(items) != 2 || items[0].FirstSeen != initial[0].FirstSeen || !items[0].Baseline || items[0].Content != "changed" || items[0].Hash == initial[0].Hash || items[1].Baseline || items[1].FirstSeen != "2026-11-02T13:00:00Z" {
		t.Fatal(items)
	}
	conditional = true
	success(t, c.call("fetch"))
	success(t, c.call("fetch"))
	if len(c.items()) != 2 {
		t.Fatal("duplicated")
	}
	success(t, c.call("feeds", "remove", "1"))
	success(t, c.call("fetch"))
	if c.feeds()[0].Active {
		t.Fatal("still active")
	}
	r = c.call("export")
	success(t, r)
	if strings.Contains(r.out, server.URL) {
		t.Fatal("exported removed")
	}
	success(t, c.call("feeds", "add", server.URL+"/rss"))
	if !c.feeds()[0].Initialized || len(c.items()) != 2 {
		t.Fatal("lost baseline/history")
	}
	success(t, c.call("fetch"))
	r = c.call("feeds", "list", "--json")
	success(t, r)
	t.Log("list:", r.out)
	r = c.call("export")
	success(t, r)
	t.Log("export:", r.out)
	success(t, c.call("feeds", "remove", "1"))
	r = c.call("feeds", "purge", "1", "--dry-run", "--json")
	success(t, r)
	if !strings.Contains(r.out, `"items":2`) || len(c.items()) != 2 {
		t.Fatal(r)
	}
	success(t, c.call("feeds", "purge", "1", "--force"))
	if len(c.items()) != 0 {
		t.Fatal("retained purged items")
	}
}

func TestMixedFailuresEmptyBaselineAndAtom(t *testing.T) {
	c := newCollection(t)
	broken := true
	empty := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if broken {
			w.Header().Set("ETag", "bad")
		} else {
			w.Header().Set("ETag", "stored")
		}
		switch r.URL.Path {
		case "/rss":
			if broken {
				fmt.Fprint(w, `<rss><channel><item><guid>partial</guid></item>`)
			} else {
				fmt.Fprint(w, rss(entry("one", "First", "after failure")))
			}
		case "/atom":
			fmt.Fprint(w, `<feed xmlns="http://www.w3.org/2005/Atom"><title>Atom</title><id>urn:feed</id><updated>2026-11-02T10:00:00Z</updated>`)
			if !empty {
				fmt.Fprint(w, `<entry><id>urn:item</id><title>Atom item</title><link href="/atom-story"/><updated>2026-11-02T11:00:00Z</updated><content type="html">&lt;p&gt;Atom text&lt;/p&gt;</content></entry>`)
			}
			fmt.Fprint(w, "</feed>")
		}
	}))
	defer server.Close()
	success(t, c.call("feeds", "add", server.URL+"/rss"))
	success(t, c.call("feeds", "add", server.URL+"/atom"))
	r := c.call("fetch", "--json")
	if r.code != 3 || r.err != "" || !strings.Contains(r.out, `"status":"degraded"`) || !strings.Contains(r.out, `"feed_id":1`) {
		t.Fatal(r)
	}
	feeds := c.feeds()
	if feeds[0].Initialized || feeds[0].ETag != "" || feeds[0].LastSuccess != "" || !feeds[1].Initialized || len(c.items()) != 0 {
		t.Fatal(feeds, c.items())
	}
	broken = false
	empty = false
	c.now = c.now.Add(time.Hour)
	success(t, c.call("fetch"))
	items := c.items()
	if len(items) != 2 || !items[0].Baseline || items[1].Baseline || items[1].Content != "Atom text" {
		t.Fatal(items)
	}
	before := items[0]
	beforeFeed := c.feeds()[0]
	broken = true
	r = c.call("fetch")
	if r.code != 3 {
		t.Fatal(r)
	}
	if f := c.feeds()[0]; f.ETag != "stored" || f.LastSuccess != beforeFeed.LastSuccess || !f.Initialized {
		t.Fatal("failed parse changed validators/baseline", f)
	}
	if after := c.items()[0]; after != before {
		t.Fatal("failed parse changed item", after, before)
	}
}

func TestFetchBoundsAndUnexpected304(t *testing.T) {
	for _, mode := range []string{"large", "redirect", "credentials", "304", "status", "timeout", "truncated"} {
		t.Run(mode, func(t *testing.T) {
			c := newCollection(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "large":
					fmt.Fprint(w, rss(strings.Repeat("x", app.MaxFeedBytes)))
				case "redirect":
					http.Redirect(w, r, "/again", 302)
				case "credentials":
					http.Redirect(w, r, "http://user:password@example.org/rss", 302)
				case "304":
					w.WriteHeader(304)
				case "status":
					http.Error(w, "secret server detail", 503)
				case "timeout":
					select {
					case <-r.Context().Done():
					case <-time.After(time.Second):
					}
				case "truncated":
					w.Header().Set("Content-Length", "100000")
					fmt.Fprint(w, rss(""))
				}
			}))
			defer server.Close()
			success(t, c.call("feeds", "add", server.URL))
			if mode == "timeout" {
				success(t, c.call("config", "set", "feed_timeout", "10ms"))
			}
			r := c.call("fetch", "--json")
			if r.code != 3 || strings.Contains(r.out, "password") || strings.Contains(r.out, "secret server detail") {
				t.Fatal(r)
			}
			f := c.feeds()[0]
			if f.Initialized || f.ETag != "" || len(c.items()) != 0 {
				t.Fatal(f)
			}
		})
	}
}

func TestOPMLTransactionalRoundtripAndOutputProtection(t *testing.T) {
	c := newCollection(t)
	c.stdin = `<opml version="2.0"><body><outline text="group" category="tech"><outline text="One" xmlUrl="https://example.org/rss" category="go,news"/><outline text="nested"><outline text="Two" xmlUrl="https://example.org/atom"/></outline></outline></body></opml>`
	success(t, c.call("import", "-"))
	success(t, c.call("import", "-"))
	feeds := c.feeds()
	if len(feeds) != 2 || strings.Join(feeds[0].Tags, ",") != "go,news,tech" || strings.Join(feeds[1].Tags, ",") != "tech" {
		t.Fatal(feeds)
	}
	r := c.call("export")
	success(t, r)
	other := newCollection(t)
	other.stdin = r.out
	success(t, other.call("import", "-"))
	again := other.call("export")
	success(t, again)
	if r.out != again.out {
		t.Fatal(r, again)
	}
	for _, invalid := range []string{
		`<opml><body><outline xmlUrl="https://example.org/new"/><outline xmlUrl="file:///etc/passwd"/></body></opml>`,
		`<opml><body><outline xmlUrl="https://example.org/new"/></body></opml><junk/>`,
		`<opml><body><outline xmlUrl="https://example.org/new"/>`,
		`<opml><body><outline xmlUrl="https://user:password@example.org"/></body></opml>`,
	} {
		c.stdin = invalid
		r = c.call("import", "-")
		if r.code != 2 || strings.Contains(r.err, "password") || len(c.feeds()) != 2 {
			t.Fatal(r, c.feeds())
		}
	}
	path := filepath.Join(t.TempDir(), "feeds.opml")
	success(t, c.call("export", "--output", path))
	if c.call("export", "--output", path).code != 2 {
		t.Fatal("overwrote")
	}
	success(t, c.call("export", "--output", path, "--force"))
	for _, name := range []string{"feeder.db", "config.json", "feeder.lock", "feeder.db-wal"} {
		if c.call("export", "--output", filepath.Join(c.home, name), "--force").code != 2 {
			t.Fatal("clobbered", name)
		}
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Link(filepath.Join(c.home, "feeder.db"), alias); err != nil {
		t.Fatal(err)
	}
	if c.call("export", "--output", alias, "--force").code != 2 {
		t.Fatal("accepted hardlink")
	}
	os.Remove(alias)
	success(t, c.call("feeds", "remove", "1"))
	r = c.call("export")
	if strings.Contains(r.out, "/rss") {
		t.Fatal(r)
	}
}

func TestIdentityFallbacksAndInvalidInvocation(t *testing.T) {
	c := newCollection(t)
	body := rss(`<item><title>Same</title><pubDate>Mon, 02 Nov 2026 10:00:00 GMT</pubDate></item><item><title>Linked</title><link>/linked</link></item>`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
	defer server.Close()
	success(t, c.call("feeds", "add", server.URL+"/a"))
	success(t, c.call("feeds", "add", server.URL+"/b"))
	success(t, c.call("fetch"))
	success(t, c.call("fetch"))
	if len(c.items()) != 4 {
		t.Fatal("identity merged across feeds", c.items())
	}
	for _, args := range [][]string{{"feeds", "add", "file:///tmp/a"}, {"feeds", "add", "http://user:password@localhost"}, {"feeds", "tag", "0"}, {"feeds", "purge", "1"}, {"feeds", "purge", "1", "--dry-run", "--force"}, {"feeds", "list", "extra"}, {"fetch", "extra"}, {"import"}, {"export", "extra"}} {
		h := filepath.Join(t.TempDir(), "absent")
		r := run(context.Background(), nil, append([]string{"--home", h}, args...)...)
		if r.code != 2 {
			t.Fatal(args, r)
		}
		if _, err := os.Stat(h); !os.IsNotExist(err) {
			t.Fatal("created state", args, err)
		}
	}
}

type failingOutput struct{}

func (failingOutput) Write([]byte) (int, error) { return 0, fmt.Errorf("fixture output failure") }
func TestFetchOutputFailureOutranksPartial(t *testing.T) {
	c := newCollection(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer server.Close()
	success(t, c.call("feeds", "add", server.URL))
	for _, jsonFlag := range []string{"--json", "--json=false"} {
		var diag bytes.Buffer
		code := cli.Execute(context.Background(), []string{"--home", c.home, "fetch", jsonFlag}, failingOutput{}, &diag, app.Dependencies{Getenv: func(string) string { return "" }}, "test")
		if code != 1 {
			t.Fatal("output failure did not outrank partial", code, diag.String())
		}
	}
}

func TestRedirectValidatorsDoNotCrossOrigins(t *testing.T) {
	c := newCollection(t)
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") != "" || r.Header.Get("If-Modified-Since") != "" || r.Header.Get("Cookie") != "" {
			t.Error("forwarded headers", r.Header)
		}
		w.Header().Set("ETag", "private-etag")
		fmt.Fprint(w, rss(""))
	}))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 302) }))
	defer source.Close()
	success(t, c.call("feeds", "add", source.URL))
	success(t, c.call("fetch"))
	success(t, c.call("fetch"))
}

func TestExportRejectsParentAliasesAndLeavesExistingOutput(t *testing.T) {
	c := newCollection(t)
	success(t, c.call("init", "--timezone", "UTC"))
	alias := filepath.Join(t.TempDir(), "home-alias")
	if err := os.Symlink(c.home, alias); err != nil {
		t.Fatal(err)
	}
	if r := c.call("export", "--output", filepath.Join(alias, "feeder.db"), "--force"); r.code != 2 {
		t.Fatal(r)
	}
	file := filepath.Join(t.TempDir(), "keep.opml")
	if err := os.WriteFile(file, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if r := c.call("export", "--output", file); r.code != 2 {
		t.Fatal(r)
	}
	b, err := os.ReadFile(file)
	if err != nil || string(b) != "keep" {
		t.Fatal("clobbered output", err)
	}
	symlink := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(file, symlink); err != nil {
		t.Fatal(err)
	}
	if r := c.call("export", "--output", symlink, "--force"); r.code != 2 {
		t.Fatal(r)
	}
}

type fixtureTransport func(*http.Request) (*http.Response, error)

func (f fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestFetchUsesInjectedTransportAndDoesNotMutateClient(t *testing.T) {
	c := newCollection(t)
	success(t, c.call("feeds", "add", "https://fixture.invalid/rss"))
	client := &http.Client{Timeout: time.Minute, Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://fixture.invalid/rss" {
			t.Error("unexpected request", r.URL)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(rss(entry("a", "Fixture", "provided content")))), Request: r}, nil
	})}
	var out, diag bytes.Buffer
	code := cli.Execute(context.Background(), []string{"--home", c.home, "fetch", "--json"}, &out, &diag, app.Dependencies{HTTP: client, Now: func() time.Time { return c.now }, Getenv: func(string) string { return "" }}, "test")
	if code != 0 || client.Timeout != time.Minute || len(c.items()) != 1 {
		t.Fatal(code, out.String(), diag.String())
	}
}
func TestNormalizedSubscriptionsAndInvalidOPMLDoNotMutate(t *testing.T) {
	c := newCollection(t)
	success(t, c.call("feeds", "add", "http://EXAMPLE.org:80#ignored"))
	success(t, c.call("feeds", "add", "http://example.org/"))
	if feeds := c.feeds(); len(feeds) != 1 || feeds[0].ID != 1 || feeds[0].URL != "http://example.org/" {
		t.Fatal(feeds)
	}
	c.home = filepath.Join(t.TempDir(), "absent")
	c.stdin = `<opml><body><outline xmlUrl="https://example.org/valid"/><outline xmlUrl="file:///bad"/></body></opml>`
	if r := c.call("import", "-"); r.code != 2 {
		t.Fatal(r)
	}
	if _, err := os.Stat(c.home); !os.IsNotExist(err) {
		t.Fatal("invalid import created state", err)
	}
}

func TestTrailingFeedDataDoesNotCommit(t *testing.T) {
	for _, suffix := range []string{"<broken>", "garbage", "<rss><channel/></rss>"} {
		t.Run(suffix, func(t *testing.T) {
			c := newCollection(t)
			body := rss(entry("old", "Original", "keep"))
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("ETag", "candidate")
				fmt.Fprint(w, body)
			}))
			defer server.Close()
			success(t, c.call("feeds", "add", server.URL))
			success(t, c.call("fetch"))
			before := c.items()[0]
			body = rss(entry("new", "New", "must not commit")) + suffix
			if r := c.call("fetch", "--json"); r.code != 3 {
				t.Fatal("accepted trailing malformed feed", r)
			}
			if items := c.items(); len(items) != 1 || items[0] != before {
				t.Fatal("malformed feed changed stored items", items)
			}
		})
	}
}

func TestMismatchedFeedNestingPreservesCollection(t *testing.T) {
	for _, fixture := range []struct{ name, bad, good string }{
		{"rss", `<rss version="2.0"><channel><title>Fixture</title><item><guid>one</guid><title>Broken</title></channel></rss>`, rss(entry("one", "Valid", "content"))},
		{"atom", `<feed xmlns="http://www.w3.org/2005/Atom"><title>Atom</title><entry><id>one</id><title>Broken</title></feed>`, `<feed xmlns="http://www.w3.org/2005/Atom"><title>Atom</title><entry><id>one</id><title>Valid</title><content>content</content></entry></feed>`},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			c := newCollection(t)
			body := fixture.bad
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("ETag", "candidate")
				w.Header().Set("Last-Modified", "Mon, 02 Nov 2026 12:00:00 GMT")
				fmt.Fprint(w, body)
			}))
			defer server.Close()
			success(t, c.call("feeds", "add", server.URL))
			r := c.call("fetch", "--json")
			if r.code != 3 {
				t.Fatal("accepted mismatched XML nesting", r)
			}
			f := c.feeds()[0]
			if f.Initialized || f.ETag != "" || f.Modified != "" || f.LastSuccess != "" || len(c.items()) != 0 {
				t.Fatal("failed initial document committed state", f, c.items())
			}
			body = fixture.good
			success(t, c.call("fetch"))
			before := c.items()
			f = c.feeds()[0]
			if len(before) != 1 || !before[0].Baseline || !f.Initialized || f.ETag != "candidate" {
				t.Fatal(f, before)
			}
			c.now = c.now.Add(time.Hour)
			body = fixture.bad
			r = c.call("fetch", "--json")
			if r.code != 3 {
				t.Fatal(r)
			}
			items := c.items()
			after := c.feeds()[0]
			if len(items) != 1 || items[0] != before[0] || after.ETag != f.ETag || after.Modified != f.Modified || after.LastSuccess != f.LastSuccess || !after.Initialized {
				t.Fatal("mismatched response changed collection", after, items)
			}
		})
	}
}

func TestValidFeedEncodingsStillCollect(t *testing.T) {
	for _, fixture := range []struct{ name, body, content string }{
		{"rss", rss(entry("one", "Valid", "text")), "text"},
		{"atom", `<feed xmlns="http://www.w3.org/2005/Atom"><title>Atom</title><entry><id>one</id><title>Valid</title><content>text</content></entry></feed>`, "text"},
		{"bom", "\xef\xbb\xbf" + rss(entry("one", "Valid", "text")), "text"},
		{"latin1", "<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><rss version=\"2.0\"><channel><title>Fixture</title><item><guid>one</guid><title>Valid</title><description>caf\xe9</description></item></channel></rss>", "café"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			c := newCollection(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, fixture.body) }))
			defer server.Close()
			success(t, c.call("feeds", "add", server.URL))
			success(t, c.call("fetch"))
			items := c.items()
			if len(items) != 1 || !items[0].Baseline || items[0].Content != fixture.content {
				t.Fatal(items)
			}
		})
	}
}

func TestInvalidURLPortsDoNotMutateSubscriptions(t *testing.T) {
	for _, port := range []string{"0", "65536", "9999999999999999999999", ""} {
		for _, command := range []string{"add", "import"} {
			t.Run(command+"/"+port, func(t *testing.T) {
				c := newCollection(t)
				c.home = filepath.Join(t.TempDir(), "absent")
				raw := "http://example.org:" + port + "/rss"
				var r result
				if command == "add" {
					r = c.call("feeds", "add", raw)
				} else {
					c.stdin = `<opml><body><outline xmlUrl="http://example.org/valid"/><outline xmlUrl="` + raw + `"/></body></opml>`
					r = c.call("import", "-")
				}
				if r.code != 2 {
					t.Fatal("invalid port accepted", r)
				}
				if _, err := os.Stat(c.home); !os.IsNotExist(err) {
					t.Fatal("invalid URL created state", err)
				}
			})
		}
	}
}
func TestURLPortBoundsRegisterWithoutFetching(t *testing.T) {
	c := newCollection(t)
	success(t, c.call("feeds", "add", "http://example.org:1/rss"))
	c.stdin = `<opml><body><outline xmlUrl="http://example.org:65535/rss"/></body></opml>`
	success(t, c.call("import", "-"))
	feeds := c.feeds()
	if len(feeds) != 2 || feeds[0].Initialized || feeds[1].Initialized {
		t.Fatal(feeds)
	}
}
