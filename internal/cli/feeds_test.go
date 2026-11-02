package cli_test

import (
	"context"
	"strings"
	"testing"
)

func TestSubscriptionLifecycle(t *testing.T) {
	h := t.TempDir()
	call := func(args ...string) result {
		return run(context.Background(), nil, append([]string{"--home", h, "--json"}, args...)...)
	}
	success(t, call("feeds", "add", "https://example.org/rss", "--tag", "tech"))
	r := call("feeds", "list")
	success(t, r)
	if !strings.Contains(r.out, `"id":1`) {
		t.Fatal(r)
	}
	success(t, call("feeds", "remove", "1"))
	r = call("feeds", "list")
	success(t, r)
	if !strings.Contains(r.out, `"data":[]`) {
		t.Fatal(r)
	}
	success(t, call("feeds", "add", "https://example.org/rss"))
	r = call("feeds", "list")
	if !strings.Contains(r.out, `"id":1`) {
		t.Fatal(r)
	}
	if call("feeds", "purge", "1", "--force").code != 2 {
		t.Fatal("purged active")
	}
	success(t, call("feeds", "tag", "1", "--tag", "new"))
	r = call("feeds", "list")
	if !strings.Contains(r.out, `"tags":["new"]`) {
		t.Fatal(r)
	}
	success(t, call("feeds", "tag", "1"))
	r = call("feeds", "list")
	if !strings.Contains(r.out, `"tags":[]`) {
		t.Fatal(r)
	}
	success(t, call("feeds", "remove", "1"))
	success(t, call("feeds", "purge", "1", "--dry-run"))
	success(t, call("feeds", "purge", "1", "--force"))
	success(t, call("feeds", "add", "https://example.org/rss"))
	r = call("feeds", "list")
	if strings.Contains(r.out, `"id":1`) || !strings.Contains(r.out, `"active":true`) {
		t.Fatal(r)
	}
}

func TestFetchEstablishesBaseline(t *testing.T) {
	h := t.TempDir()
	r := run(context.Background(), nil, "--home", h, "fetch", "--json")
	success(t, r)
	if !strings.Contains(r.out, `"data":[]`) {
		t.Fatal(r)
	}
}

func TestOPMLInvalidImportDoesNotCreateState(t *testing.T) {
	h := t.TempDir()
	r := run(context.Background(), nil, "--home", h, "import", "missing.opml")
	if r.code == 0 {
		t.Fatal(r)
	}
	r = run(context.Background(), nil, "--home", h, "export")
	success(t, r)
	if !strings.Contains(r.out, "<opml") {
		t.Fatal(r)
	}
}
