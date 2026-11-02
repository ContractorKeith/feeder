package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ContractorKeith/feeder/internal/summarizer"
)

// Run real writers, synchronizing on HTTP arrival after lock acquisition.
func TestCompiledFetchDigestOverlapAndInterrupt(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "feeder")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "go", "build", "-o", bin, "../../cmd/feeder").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	for _, holder := range []string{"fetch", "digest"} {
		t.Run(holder, func(t *testing.T) {
			home := t.TempDir()
			entered, release := make(chan struct{}, 4), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				select {
				case entered <- struct{}{}:
				default:
				}
				select {
				case <-release:
					fmt.Fprint(w, rss(""))
				case <-r.Context().Done():
				}
			}))
			defer server.Close()
			defer unblock()
			// Each child has a deadline, bounded pipe cleanup, and no ambient settings.
			start := func(args ...string) (*exec.Cmd, *bytes.Buffer, *bytes.Buffer, <-chan struct{}, *error) {
				t.Helper()
				childCtx, stop := context.WithTimeout(context.Background(), 15*time.Second)
				cmd := exec.CommandContext(childCtx, bin, append([]string{"--home", home}, args...)...)
				cmd.Env = []string{}
				cmd.WaitDelay = time.Second
				out, diag := new(bytes.Buffer), new(bytes.Buffer)
				cmd.Stdout, cmd.Stderr = out, diag
				if err := cmd.Start(); err != nil {
					stop()
					t.Fatal(err)
				}
				done := make(chan struct{})
				var waitErr error
				go func() { waitErr = cmd.Wait(); close(done) }()
				t.Cleanup(func() { stop(); <-done })
				return cmd, out, diag, done, &waitErr
			}
			wait := func(done <-chan struct{}, err *error, out, diag *bytes.Buffer, want int) {
				t.Helper()
				select {
				case <-done:
				case <-time.After(10 * time.Second):
					t.Fatal("child exceeded finite wait")
				}
				code := 0
				if *err != nil {
					var exit *exec.ExitError
					if !errors.As(*err, &exit) {
						t.Fatal(*err)
					}
					code = exit.ExitCode()
				}
				if code != want {
					t.Fatalf("exit=%d want=%d stdout=%s stderr=%s", code, want, out, diag)
				}
				t.Logf("%s: exit=%d stdout=%s stderr=%s", holder, code, out, diag)
			}
			_, out, diag, done, err := start("feeds", "add", server.URL)
			wait(done, err, out, diag, 0)
			cmd, out, diag, done, err := start(holder, "--json")
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("writer did not reach fixture HTTP phase")
			}
			contender := "digest"
			if holder == "digest" {
				contender = "fetch"
			}
			_, busyOut, busyDiag, busyDone, busyErr := start(contender, "--json", "--lock-timeout", "80ms")
			wait(busyDone, busyErr, busyOut, busyDiag, 4)
			assertProblemJSON(t, busyOut.String(), busyDiag.String(), "busy")
			select {
			case <-entered:
				t.Fatal("busy contender fetched before obtaining lock")
			default:
			}
			if e := cmd.Process.Signal(syscall.SIGINT); e != nil {
				t.Fatal(e)
			}
			wait(done, err, out, diag, 130)
			assertProblemJSON(t, out.String(), diag.String(), "interrupted")
			unblock()
			_, out, diag, done, err = start(contender, "--json", "--lock-timeout", "200ms")
			wait(done, err, out, diag, 0)
			if diag.Len() != 0 || !strings.Contains(out.String(), `"status":"ok"`) {
				t.Fatal(out, diag)
			}
		})
	}
	t.Run("schedule examples", func(t *testing.T) { testScheduleExamples(t, bin) })
}

func assertProblemJSON(t *testing.T, out, diag, code string) {
	t.Helper()
	var e struct {
		Schema int `json:"schema_version"`
		Status string
		Errors []struct{ Code string }
	}
	dec := json.NewDecoder(strings.NewReader(out))
	if err := dec.Decode(&e); err != nil || e.Schema != 1 || e.Status != "error" || len(e.Errors) != 1 || e.Errors[0].Code != code || diag != "" {
		t.Fatalf("invalid JSON: %s / %s (%v)", out, diag, err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		t.Fatalf("extra stdout: %v", err)
	}
}

// Failed refreshes must retain both source identity and a previously good summary.
func TestFailedRefreshPreservesSourceAndSummary(t *testing.T) {
	for _, mode := range []string{"malformed", "truncated", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			c := newCollection(t)
			failed := false
			feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if failed {
					if r.Header.Get("If-None-Match") != "good" {
						t.Error("lost successful validator")
					}
					w.Header().Set("ETag", "bad")
					switch mode {
					case "malformed":
						fmt.Fprint(w, rss(entry("two", "Partial", "bad"))+"junk")
					case "truncated":
						w.Header().Set("Content-Length", "100000")
						fmt.Fprint(w, rss(entry("two", "Partial", "bad")))
					case "oversized":
						fmt.Fprint(w, rss(entry("two", "Partial", strings.Repeat("x", (5<<20)+1))))
					}
					return
				}
				w.Header().Set("ETag", "good")
				fmt.Fprint(w, rss(entry("one", "Stored", "original source")))
			}))
			defer feed.Close()
			modelCalls := 0
			model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				modelCalls++
				fmt.Fprint(w, `{"message":{"content":"Stored summary"}}`)
			}))
			defer model.Close()
			success(t, c.call("feeds", "add", feed.URL))
			args := []string{"digest", "--from", "2026-11-02", "--to", "2026-11-03", "--include-baseline", "--summarize", "--model", "fixture", "--endpoint", model.URL, "--json"}
			success(t, c.call(args...))
			items, source := c.items(), c.feeds()[0]
			failed = true
			c.now = c.now.Add(time.Hour)
			r := c.call(args...)
			e := decodeDigest(t, r)
			after := c.feeds()[0]
			if r.code != 3 || r.err != "" || e.Status != "degraded" || e.Data.Coverage.Failed != 1 || len(e.Data.Entries) != 1 || e.Data.Entries[0].TextKind != "summary" || e.Data.Entries[0].Text != "Stored summary" || modelCalls != 1 || !reflect.DeepEqual(items, c.items()) || after.ETag != source.ETag || after.LastSuccess != source.LastSuccess || !after.Initialized || after.LastError == "" || after.LastAttempt == source.LastAttempt {
				t.Fatal("failed refresh changed successful state", r, after)
			}
		})
	}
}

func TestFailedModelPreservesPriorCache(t *testing.T) {
	for _, mode := range []string{"malformed", "truncated", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			c := newCollection(t)
			feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, rss(entry("one", "Stored", "original source")))
			}))
			defer feed.Close()
			calls := 0
			model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if calls == 1 {
					fmt.Fprint(w, `{"message":{"content":"Stored summary"}}`)
					return
				}
				switch mode {
				case "malformed":
					fmt.Fprint(w, `{"message":`)
				case "truncated":
					w.Header().Set("Content-Length", "100000")
					fmt.Fprint(w, `{"message":{"content":"partial"}}`)
				case "oversized":
					fmt.Fprint(w, strings.Repeat("x", summarizer.ResponseBytes+1))
				}
			}))
			defer model.Close()
			success(t, c.call("feeds", "add", feed.URL))
			args := []string{"digest", "--from", "2026-11-02", "--to", "2026-11-03", "--include-baseline", "--summarize", "--model", "good", "--endpoint", model.URL, "--json"}
			success(t, c.call(args...))
			items := c.items()
			for i := 0; i < 2; i++ {
				r := c.call(append(args, "--model", "failing")...)
				e := decodeDigest(t, r)
				if r.code != 3 || r.err != "" || e.Status != "degraded" || e.Data.Entries[0].TextKind != "fallback" || e.Data.Entries[0].Text != "original source" {
					t.Fatal(r)
				}
			}
			r := c.call(args...)
			success(t, r)
			if calls != 3 || decodeDigest(t, r).Data.Entries[0].Text != "Stored summary" || !reflect.DeepEqual(items, c.items()) {
				t.Fatal("prior cache/source lost or failure cached", calls, r)
			}
		})
	}
}

// Parse the actual examples and execute their wrapper commands in disposable state.
// This validates files/arguments, not cron or launchd service installation.
func testScheduleExamples(t *testing.T, bin string) {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"logs", "digests", "bin"} {
		if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(bin, filepath.Join(root, "bin", "feeder")); err != nil {
		t.Fatal(err)
	}
	wrapper, err := os.ReadFile("../../docs/examples/run-feeder.sh")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "run-feeder.sh"), wrapper, 0600); err != nil {
		t.Fatal(err)
	}
	execute := func(args ...string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		cmd.Env = []string{}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("example: %v %s", err, out)
		}
	}
	execute("/bin/sh", "-n", filepath.Join(root, "run-feeder.sh"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, rss("")) }))
	defer server.Close()
	execute(bin, "--home", filepath.Join(root, "state"), "init", "--timezone", "UTC")
	execute(bin, "--home", filepath.Join(root, "state"), "feeds", "add", server.URL)
	cron, err := os.ReadFile("../../docs/examples/linux.crontab")
	if err != nil {
		t.Fatal(err)
	}
	lines := 0
	for _, line := range strings.Split(string(cron), "\n") {
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 6 {
			t.Fatal("invalid crontab entry", line)
		}
		expected := [][]string{{"0", "*", "*", "*", "*"}, {"10", "7", "*", "*", "*"}, {"20", "7", "*", "*", "1"}}
		if lines >= len(expected) || !reflect.DeepEqual(fields[:5], expected[lines]) {
			t.Fatal("unexpected calendar", line)
		}
		command := strings.ReplaceAll(strings.Join(fields[5:], " "), "/home/reader/feeder", root)
		command = strings.ReplaceAll(command, "/home/reader/bin/feeder", filepath.Join(root, "bin", "feeder"))
		execute("/bin/sh", "-c", command)
		lines++
	}
	if lines != 3 {
		t.Fatal("missing cron jobs", lines)
	}
	for i, op := range []string{"fetch", "daily", "weekly"} {
		b, err := os.ReadFile("../../docs/examples/org.feeder." + op + ".plist")
		if err != nil {
			t.Fatal(err)
		}
		var plist struct {
			XMLName xml.Name `xml:"plist"`
			Dict    struct {
				Keys    []string `xml:"key"`
				Strings []string `xml:"string"`
				Array   struct {
					Args []string `xml:"string"`
				} `xml:"array"`
				Calendar struct {
					Keys   []string `xml:"key"`
					Values []int    `xml:"integer"`
				} `xml:"dict"`
			} `xml:"dict"`
		}
		if err := xml.Unmarshal(b, &plist); err != nil {
			t.Fatal(err)
		}
		expectedKeys := []string{"Label", "ProgramArguments", "StartCalendarInterval", "StandardOutPath", "StandardErrorPath"}
		expectedCalendarKeys := [][]string{{"Minute"}, {"Hour", "Minute"}, {"Weekday", "Hour", "Minute"}}
		expectedCalendarValues := [][]int{{0}, {7, 10}, {1, 7, 20}}
		suffix := ".out"
		if op == "fetch" {
			suffix = ".jsonl"
		}
		expectedStrings := []string{"org.feeder." + op, "/Users/reader/feeder/logs/" + op + suffix, "/Users/reader/feeder/logs/" + op + ".err"}
		args := plist.Dict.Array.Args
		expectedArgs := []string{"/bin/sh", "/Users/reader/feeder/run-feeder.sh", "/Users/reader/bin/feeder", "/Users/reader/feeder/state", "UTC", "/Users/reader/feeder/digests", op, "10s"}
		if !reflect.DeepEqual(plist.Dict.Keys, expectedKeys) || !reflect.DeepEqual(plist.Dict.Strings, expectedStrings) || !reflect.DeepEqual(args, expectedArgs) || !reflect.DeepEqual(plist.Dict.Calendar.Keys, expectedCalendarKeys[i]) || !reflect.DeepEqual(plist.Dict.Calendar.Values, expectedCalendarValues[i]) {
			t.Fatalf("invalid plist: %+v", plist)
		}
		for j, arg := range args {
			arg = strings.ReplaceAll(arg, "/Users/reader/feeder", root)
			args[j] = strings.ReplaceAll(arg, "/Users/reader/bin/feeder", filepath.Join(root, "bin", "feeder"))
		}
		execute(args...)
	}
	for _, op := range []string{"daily", "weekly"} {
		b, err := os.ReadFile(filepath.Join(root, "digests", op+".md"))
		if err != nil || !bytes.Contains(b, []byte("No eligible items")) {
			t.Fatal("missing digest", op, err)
		}
	}
}
