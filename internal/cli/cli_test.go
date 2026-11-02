package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ContractorKeith/feeder/internal/app"
	"github.com/ContractorKeith/feeder/internal/cli"
	"github.com/ContractorKeith/feeder/internal/storage"
)

type result struct {
	code     int
	out, err string
}

func run(ctx context.Context, env map[string]string, args ...string) result {
	var out, stderr bytes.Buffer
	code := cli.Execute(ctx, args, &out, &stderr, app.Dependencies{Now: func() time.Time { return time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC) }, Getenv: func(k string) string { return env[k] }}, "test-version")
	return result{code, out.String(), stderr.String()}
}
func success(t *testing.T, r result) {
	t.Helper()
	if r.code != 0 {
		t.Fatalf("code=%d stdout=%s stderr=%s", r.code, r.out, r.err)
	}
}
func data(t *testing.T, r result) map[string]any {
	t.Helper()
	var e struct {
		Schema   int            `json:"schema_version"`
		Status   string         `json:"status"`
		Data     map[string]any `json:"data"`
		Warnings []any          `json:"warnings"`
		Errors   []any          `json:"errors"`
	}
	if err := json.Unmarshal([]byte(r.out), &e); err != nil {
		t.Fatal(r, err)
	}
	if e.Schema != 1 || e.Status != "ok" || e.Warnings == nil || e.Errors == nil || len(e.Errors) != 0 || r.err != "" {
		t.Fatalf("invalid envelope: %+v / %+v", e, r)
	}
	return e.Data
}

func TestNoStateForInformationalAndInvalidCommands(t *testing.T) {
	cases := []struct {
		args []string
		code int
	}{
		{[]string{"--help"}, 0}, {[]string{"help", "init"}, 0}, {[]string{"config", "--help"}, 0}, {[]string{"version"}, 0}, {[]string{"--version"}, 0},
		{[]string{"completion", "bash"}, 0}, {[]string{"completion", "zsh"}, 0}, {[]string{"completion", "fish"}, 0},
		{[]string{"unknown"}, 2}, {[]string{"init", "extra"}, 2}, {[]string{"init", "--bad"}, 2}, {[]string{"completion", "powershell"}, 2},
		{[]string{"config", "set", "provider", "openai"}, 2}, {[]string{"config", "set", "summarize", "true"}, 2}, {[]string{"config", "set", "limit", "0"}, 2}, {[]string{"config", "set", "timezone", "+03:00"}, 2}, {[]string{"config", "set", "api_key", "secret-marker"}, 2},
		{[]string{"init", "--timezone", "Local"}, 2}, {[]string{"init", "--lock-timeout", "0s"}, 2}, {[]string{"config", "show", "extra"}, 2},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, "_"), func(t *testing.T) {
			h := filepath.Join(t.TempDir(), "absent")
			r := run(context.Background(), nil, append([]string{"--home", h}, tc.args...)...)
			if r.code != tc.code {
				t.Fatalf("%+v", r)
			}
			if _, err := os.Stat(h); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("created state: %v", err)
			}
			if strings.Contains(r.out+r.err, "secret-marker") {
				t.Fatal("echoed secret-shaped input")
			}
		})
	}
}

func TestInitReopenPermissionsAndMigration(t *testing.T) {
	h := filepath.Join(t.TempDir(), "state")
	for i := 0; i < 2; i++ {
		success(t, run(context.Background(), nil, "--home", h, "init", "--timezone", "America/New_York", "--json"))
	}
	r := run(context.Background(), nil, "--home", h, "config", "show", "--json")
	success(t, r)
	if data(t, r)["timezone"] != "America/New_York" {
		t.Fatal(r)
	}
	// Reinitializing with an override leaves the saved timezone intact.
	success(t, run(context.Background(), nil, "--home", h, "init", "--timezone", "UTC"))
	r = run(context.Background(), nil, "--home", h, "config", "show", "--json")
	if data(t, r)["timezone"] != "America/New_York" {
		t.Fatal(r)
	}
	for name, mode := range map[string]os.FileMode{"": 0700, "config.json": 0600, "feeder.db": 0600, "feeder.lock": 0600} {
		info, err := os.Stat(filepath.Join(h, name))
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("%s mode: %v %v", name, info, err)
		}
	}
	db, err := storage.Open(context.Background(), h, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count, version int
	var applied string
	if err := db.QueryRow("SELECT count(*), applied_at FROM schema_migrations").Scan(&count, &applied); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if count != 2 || version != 2 || applied != "2026-11-02T12:00:00Z" {
		t.Fatalf("migration changed: %d %d %s", count, version, applied)
	}
}

func TestConfigPrecedenceAndNoSecretReads(t *testing.T) {
	h := filepath.Join(t.TempDir(), "state")
	other := filepath.Join(t.TempDir(), "unused")
	env := map[string]string{"FEEDER_HOME": other, "FEEDER_TIMEZONE": "America/Chicago", "FEEDER_LIMIT": "40", "FEEDER_API_KEY": "never-print-key", "FEEDER_API_KEY_FILE": filepath.Join(t.TempDir(), "does-not-exist")}
	success(t, run(context.Background(), env, "--home", h, "init", "--timezone", "America/New_York", "--lock-timeout", "200ms"))
	success(t, run(context.Background(), env, "--home", h, "config", "set", "limit", "25"))
	r := run(context.Background(), env, "--home", h, "config", "show", "--lock-timeout", "300ms", "--json")
	success(t, r)
	c := data(t, r)
	if c["limit"] != float64(40) || c["timezone"] != "America/Chicago" || c["lock_timeout"] != "300ms" {
		t.Fatal(c)
	}
	r = run(context.Background(), nil, "--home", h, "config", "show", "--json")
	c = data(t, r)
	if c["limit"] != float64(25) || c["timezone"] != "America/New_York" || c["lock_timeout"] != "10s" || c["api_key_file"] != "" {
		t.Fatal(c)
	}
	if strings.Contains(r.out, "never-print-key") {
		t.Fatal("secret leak")
	}
	if _, err := os.Stat(other); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("--home did not isolate state")
	}
	env = map[string]string{"FEEDER_HOME": filepath.Join(t.TempDir(), "env-state"), "TZ": "Etc/GMT+5"}
	success(t, run(context.Background(), env, "init"))
	r = run(context.Background(), env, "config", "show", "--json")
	if data(t, r)["timezone"] != "Etc/GMT+5" {
		t.Fatal(r)
	}
}

func TestValidationAndJSONErrors(t *testing.T) {
	for k, v := range map[string]string{"FEEDER_TIMEZONE": "not-a-zone", "FEEDER_LIMIT": "201", "FEEDER_MODEL_TIMEOUT": "-1s", "FEEDER_SUMMARIZE": "true", "FEEDER_PROVIDER": "openai", "FEEDER_ENDPOINT": "https://user:secret@example.org", "FEEDER_MAX_INPUT_CHARS": "0"} {
		t.Run(k, func(t *testing.T) {
			h := filepath.Join(t.TempDir(), "absent")
			r := run(context.Background(), map[string]string{k: v}, "--json", "--home", h, "init")
			if r.code != 2 || r.err != "" || strings.Contains(r.out, "secret") {
				t.Fatal(r)
			}
			var e map[string]any
			if err := json.Unmarshal([]byte(r.out), &e); err != nil || e["status"] != "error" || e["schema_version"] != float64(1) {
				t.Fatal(r, err)
			}
			if _, err := os.Stat(h); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("invalid invocation created state")
			}
		})
	}
	h := t.TempDir()
	success(t, run(context.Background(), nil, "--home", h, "init", "--timezone", "UTC"))
	before, err := os.ReadFile(filepath.Join(h, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"provider", "openai"}, {"summarize", "true"}, {"limit", "201"}, {"endpoint", "https://user:secret@example.org"}} {
		r := run(context.Background(), nil, append([]string{"--home", h, "config", "set"}, args...)...)
		if r.code != 2 {
			t.Fatal(r)
		}
	}
	after, _ := os.ReadFile(filepath.Join(h, "config.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("invalid config changed saved state")
	}
	success(t, run(context.Background(), nil, "--home", h, "config", "set", "endpoint", "https://example.org/v1"))
	success(t, run(context.Background(), nil, "--home", h, "config", "set", "provider", "openai"))
	success(t, run(context.Background(), nil, "--home", h, "config", "set", "model", "test-model"))
	success(t, run(context.Background(), nil, "--home", h, "config", "set", "summarize", "true"))
}

func TestCorruptDatabaseIsPreserved(t *testing.T) {
	h := t.TempDir()
	path := filepath.Join(h, "feeder.db")
	b := []byte("not a sqlite database")
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	r := run(context.Background(), nil, "--home", h, "init", "--timezone", "UTC")
	if r.code != 1 {
		t.Fatal(r)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(b, after) {
		t.Fatal("corrupt DB was modified")
	}
	if _, err := os.Stat(filepath.Join(h, "config.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("config saved after failed migration")
	}
}

func TestHTTPInjectionDoesNotMakeRequests(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer server.Close()
	var out, stderr bytes.Buffer
	code := cli.Execute(context.Background(), []string{"--home", t.TempDir(), "init", "--timezone", "UTC"}, &out, &stderr, app.Dependencies{HTTP: server.Client(), Getenv: func(string) string { return "" }}, "test")
	if code != 0 || calls != 0 {
		t.Fatalf("code=%d requests=%d", code, calls)
	}
}

func TestWriterBusyAndCancellation(t *testing.T) {
	h := t.TempDir()
	release, err := storage.Lock(context.Background(), h, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	started := time.Now()
	r := run(context.Background(), nil, "--home", h, "--lock-timeout", "60ms", "--json", "init", "--timezone", "UTC")
	if r.code != 4 || time.Since(started) > time.Second || !strings.Contains(r.out, `"code":"busy"`) {
		t.Fatal(r)
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(40*time.Millisecond, cancel)
	r = run(ctx, nil, "--home", h, "init", "--timezone", "UTC")
	if r.code != 130 {
		t.Fatal(r)
	}
}

// Build the shipped entry point to verify OS process contention and SIGINT.
func TestCompiledProcessLockAndSignal(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "feeder")
	build := exec.Command("go", "build", "-o", bin, "../../cmd/feeder")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	h := t.TempDir()
	release, err := storage.Lock(context.Background(), h, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	cmd := exec.Command(bin, "--home", h, "--lock-timeout", "80ms", "--json", "init", "--timezone", "UTC")
	// Explicit settings in the child prevent any ambient user configuration.
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 4 || !bytes.Contains(out, []byte(`"code":"busy"`)) {
		t.Fatalf("busy process: %v %s", err, out)
	}
	cmd = exec.Command(bin, "--home", h, "--lock-timeout", "10s", "init", "--timezone", "UTC")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err = <-done:
		if !errors.As(err, &exit) || exit.ExitCode() != 130 {
			t.Fatalf("signal: %v %s", err, stderr.String())
		}
	case <-time.After(3 * time.Second):
		cmd.Process.Kill()
		<-done
		t.Fatal("SIGINT did not stop lock wait")
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command(bin, "--home", h, "init", "--timezone", "UTC")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("lock not reusable: %v %s", err, out)
	}
}

func TestReadOnlyDefaultsAndMalformedConfig(t *testing.T) {
	h := filepath.Join(t.TempDir(), "absent")
	r := run(context.Background(), nil, "--home", h, "config", "show", "--json")
	success(t, r)
	if data(t, r)["limit"] != float64(20) {
		t.Fatal(r)
	}
	if _, err := os.Stat(h); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read created state")
	}
	for _, b := range []string{`{"api_key":"secret-marker"}`, `{"limit":"20"}`, `{"timezone":"invalid-zone"}`, `{} {}`, `not-json`} {
		h := t.TempDir()
		path := filepath.Join(h, "config.json")
		if err := os.WriteFile(path, []byte(b), 0600); err != nil {
			t.Fatal(err)
		}
		r := run(context.Background(), nil, "--home", h, "config", "show", "--json")
		if r.code != 2 || strings.Contains(r.out+r.err, "secret-marker") {
			t.Fatal(r)
		}
		after, _ := os.ReadFile(path)
		if string(after) != b {
			t.Fatal("malformed config changed")
		}
	}
}

func TestHomeWithURICharacters(t *testing.T) {
	h := filepath.Join(t.TempDir(), "state?# odd")
	success(t, run(context.Background(), nil, "--home", h, "init", "--timezone", "UTC"))
	if _, err := os.Stat(filepath.Join(h, "feeder.db")); err != nil {
		t.Fatal(err)
	}
}

func TestOverridesCannotHideInvalidSavedConfig(t *testing.T) {
	h := filepath.Join(t.TempDir(), "absent")
	r := run(context.Background(), map[string]string{"FEEDER_PROVIDER": "ollama"}, "--home", h, "config", "set", "provider", "openai")
	if r.code != 2 {
		t.Fatal(r)
	}
	if _, err := os.Stat(h); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("invalid saved combination created state")
	}
}

func TestExplicitFlagsOverrideInvalidEnvironment(t *testing.T) {
	h := filepath.Join(t.TempDir(), "state")
	env := map[string]string{"FEEDER_TIMEZONE": "invalid", "FEEDER_LOCK_TIMEOUT": "invalid"}
	success(t, run(context.Background(), env, "--home", h, "init", "--timezone", "UTC", "--lock-timeout", "1s"))
}

func TestOpenAIExplicitEndpointMayUseAnyPort(t *testing.T) {
	h := t.TempDir()
	success(t, run(context.Background(), nil, "--home", h, "config", "set", "endpoint", "http://localhost:11434"))
	success(t, run(context.Background(), nil, "--home", h, "config", "set", "provider", "openai"))
}

func TestOversizedConfigPreservesAllState(t *testing.T) {
	for _, suffix := range []string{"", "INVALID"} {
		t.Run("suffix_"+suffix, func(t *testing.T) {
			h := t.TempDir()
			b := append([]byte("{}"), bytes.Repeat([]byte(" "), 1<<20)...)
			b = append(b, []byte(suffix)...)
			path := filepath.Join(h, "config.json")
			if err := os.WriteFile(path, b, 0600); err != nil {
				t.Fatal(err)
			}
			for _, cmd := range [][]string{{"init", "--timezone", "UTC"}, {"config", "set", "limit", "30"}, {"config", "show"}} {
				r := run(context.Background(), nil, append([]string{"--home", h, "--json"}, cmd...)...)
				if r.code != 2 || !strings.Contains(r.out, "1 MiB") {
					t.Fatal(r)
				}
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(b, after) {
					t.Fatal("oversize config modified", err)
				}
				entries, err := os.ReadDir(h)
				if err != nil || len(entries) != 1 {
					t.Fatal("created extra state", entries, err)
				}
			}
		})
	}
}

func TestStateAliasesAndSpecialFilesAreRejected(t *testing.T) {
	names := []string{"config.json", "feeder.db", "feeder.lock", "feeder.db-journal", "feeder.db-wal", "feeder.db-shm"}
	for _, name := range names {
		for _, kind := range []string{"symlink", "hardlink", "directory", "fifo"} {
			t.Run(name+"_"+kind, func(t *testing.T) {
				h := t.TempDir()
				target := filepath.Join(t.TempDir(), "outside")
				// Empty DB is significant: SQLite would initialize the outside file.
				var original []byte
				if name == "config.json" {
					original = []byte("{}")
				}
				if err := os.WriteFile(target, original, 0644); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(h, name)
				var err error
				switch kind {
				case "symlink":
					err = os.Symlink(target, path)
				case "hardlink":
					err = os.Link(target, path)
				case "directory":
					err = os.Mkdir(path, 0755)
				case "fifo":
					err = syscall.Mkfifo(path, 0600)
				}
				if err != nil {
					t.Fatal(err)
				}
				before, err := os.Stat(h)
				if err != nil {
					t.Fatal(err)
				}
				for _, cmd := range [][]string{{"init", "--timezone", "UTC"}, {"config", "set", "limit", "30"}, {"config", "show"}} {
					r := run(context.Background(), nil, append([]string{"--home", h, "--json"}, cmd...)...)
					if r.code == 0 {
						t.Fatal("accepted unsafe state", r)
					}
					b, err := os.ReadFile(target)
					if err != nil || !bytes.Equal(b, original) {
						t.Fatal("wrote outside home", err, len(b))
					}
					info, err := os.Stat(target)
					if err != nil || info.Mode().Perm() != 0644 {
						t.Fatal("chmod outside home", info, err)
					}
					entries, err := os.ReadDir(h)
					if err != nil || len(entries) != 1 {
						t.Fatal("created state before rejecting alias", entries, err)
					}
					after, err := os.Stat(h)
					if err != nil || after.Mode() != before.Mode() {
						t.Fatal("changed home before rejection", err)
					}
				}
			})
		}
	}
}

func TestJSONParseErrorsIgnoreFlagPosition(t *testing.T) {
	for _, bad := range []string{"nonsense", "--bad"} {
		var previous string
		for _, front := range []bool{true, false} {
			h := filepath.Join(t.TempDir(), "absent")
			args := []string{"--home", h}
			if front {
				args = append(args, "--json", bad)
			} else {
				args = append(args, bad, "--json")
			}
			r := run(context.Background(), nil, args...)
			if r.code != 2 || r.err != "" {
				t.Fatal(r)
			}
			dec := json.NewDecoder(strings.NewReader(r.out))
			var e map[string]any
			if err := dec.Decode(&e); err != nil || e["status"] != "error" || e["schema_version"] != float64(1) {
				t.Fatal(r, err)
			}
			if err := dec.Decode(new(any)); err != io.EOF {
				t.Fatal("expected exactly one envelope", r, err)
			}
			if previous != "" && r.out != previous {
				t.Fatal("output depends on flag order", previous, r.out)
			}
			previous = r.out
			if _, err := os.Stat(h); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("created state", err)
			}
		}
	}
	// --json is a value of a known flag, or a positional after --, not intent.
	for _, args := range [][]string{{"--home", "--json", "nonsense"}, {"nonsense", "--", "--json"}, {"config", "set", "model", "--", "--json", "extra"}, {"--bad", "--json=false"}, {"--json", "--bad", "--json=false"}, {"--bad", "--timezone", "--json"}} {
		h := filepath.Join(t.TempDir(), "absent")
		// Insert another home before args so no malformed command can use real state.
		r := run(context.Background(), nil, append([]string{"--home", h}, args...)...)
		if r.code != 2 || r.out != "" || r.err == "" {
			t.Fatal(args, r)
		}
		if _, err := os.Stat(h); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("created state", err)
		}
	}
}

func TestLiteralJSONValueRemainsHumanOutput(t *testing.T) {
	h := t.TempDir()
	r := run(context.Background(), nil, "--home", h, "config", "set", "model", "--", "--json")
	success(t, r)
	var settings map[string]any
	if err := json.Unmarshal([]byte(r.out), &settings); err != nil || settings["model"] != "--json" || settings["schema_version"] != nil {
		t.Fatal("misread model as output intent", r, err)
	}
}

func TestSymlinkHomeIsRejectedWithoutChmod(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.Mkdir(outside, 0755); err != nil {
		t.Fatal(err)
	}
	h := filepath.Join(t.TempDir(), "home")
	if err := os.Symlink(outside, h); err != nil {
		t.Fatal(err)
	}
	r := run(context.Background(), nil, "--home", h, "init", "--timezone", "UTC")
	if r.code == 0 {
		t.Fatal(r)
	}
	info, err := os.Stat(outside)
	if err != nil || info.Mode().Perm() != 0755 {
		t.Fatal("changed outside directory permissions", info, err)
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatal("created outside state", entries, err)
	}
}
