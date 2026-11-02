// Evaluation drives the real feeder executable; fixture mode proves plumbing only.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/ContractorKeith/feeder/internal/app"
	"github.com/ContractorKeith/feeder/internal/summarizer"
)

var fixtureNames = []string{"normal", "teaser", "empty", "instruction-bearing"}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "evaluate:", err)
		os.Exit(1)
	}
}
func run(argsIn []string) error {
	f := flag.NewFlagSet("evaluate", flag.ContinueOnError)
	binary := f.String("binary", "", "absolute feeder binary path")
	out := f.String("out", "", "new absolute evidence directory")
	provider := f.String("provider", "", "fixture, ollama or openai")
	endpoint := f.String("endpoint", "", "explicit real model base URL")
	model := f.String("model", "", "explicit real model name")
	if err := f.Parse(argsIn); err != nil {
		return err
	}
	if f.NArg() != 0 || !filepath.IsAbs(*binary) || !filepath.IsAbs(*out) {
		return errors.New("supply --binary and --out as absolute paths, no extra arguments")
	}
	if *provider != "fixture" && *provider != "ollama" && *provider != "openai" {
		return errors.New("supply --provider fixture|ollama|openai")
	}
	if *provider != "fixture" && (*endpoint == "" || *model == "") {
		return errors.New("real evaluation requires --endpoint and --model")
	}
	if *provider == "fixture" && (*endpoint != "" || *model != "") {
		return errors.New("fixture mode does not accept endpoint/model")
	}
	if *provider != "fixture" {
		u, err := url.Parse(*endpoint)
		if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
			return errors.New("endpoint must be an HTTP(S) base URL without credentials, query or fragment")
		}
	}
	if err := os.Mkdir(*out, 0700); err != nil {
		return err
	}
	// Capture fixed inputs and exact prompt implementation from this checkout.
	source, err := os.ReadFile("internal/summarizer/summarizer.go")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(*out, "prompt-source.go.txt"), source, 0600); err != nil {
		return err
	}
	expectedInputs := map[string]string{}
	var rss strings.Builder
	rss.WriteString(`<rss version="2.0"><channel><title>Evaluation fixtures</title><link>https://example.org/</link><description>Fixed feed data only</description>`)
	for _, name := range fixtureNames {
		b, e := os.ReadFile("internal/summarizer/testdata/" + name + ".txt")
		if e != nil {
			return e
		}
		if e = os.WriteFile(filepath.Join(*out, name+".txt"), b, 0600); e != nil {
			return e
		}
		plain := app.PlainText(string(b))
		if utf8.RuneCountInString(plain) > 600 {
			return errors.New("fixture exceeds exact-input capture cap")
		}
		expectedInputs[name] = plain
		var escaped bytes.Buffer
		if e = xml.EscapeText(&escaped, b); e != nil {
			return e
		}
		fmt.Fprintf(&rss, "<item><guid>%s</guid><title>%s</title><link>https://example.org/%s</link><description>%s</description></item>", name, name, name, escaped.String())
	}
	rss.WriteString("</channel></rss>")
	if err = os.WriteFile(filepath.Join(*out, "feed.xml"), []byte(rss.String()), 0600); err != nil {
		return err
	}
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, rss.String())
	}))
	defer feed.Close()
	var calls atomic.Int64
	fixture := *provider == "fixture"
	if fixture {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			call := calls.Add(1)
			var request map[string]any
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&request); err != nil {
				http.Error(w, "bad request", 400)
				return
			}
			b, _ := json.MarshalIndent(request, "", "  ")
			if err := os.WriteFile(filepath.Join(*out, fmt.Sprintf("fixture-request-%d.json", call)), b, 0600); err != nil {
				http.Error(w, "capture failed", 500)
				return
			}
			fmt.Fprint(w, `{"message":{"content":"Fixture response: harness proof only."}}`)
		}))
		defer server.Close()
		*provider = "ollama"
		*endpoint = server.URL
		*model = "fixture-harness-only"
	}
	home := filepath.Join(*out, "state")
	// Isolate all ambient FEEDER settings; forward only an explicitly supplied key.
	env := []string{}
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "FEEDER_") {
			env = append(env, entry)
		}
	}
	if !fixture && os.Getenv("FEEDER_API_KEY") != "" {
		env = append(env, "FEEDER_API_KEY="+os.Getenv("FEEDER_API_KEY"))
	}
	invoke := func(name string, args ...string) ([]byte, int, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, *binary, append([]string{"--home", home}, args...)...)
		cmd.Env = env
		var diag bytes.Buffer
		cmd.Stderr = &diag
		b, e := cmd.Output()
		code := 0
		if e != nil {
			var exit *exec.ExitError
			if !errors.As(e, &exit) {
				return nil, 1, e
			}
			code = exit.ExitCode()
		}
		for suffix, data := range map[string][]byte{".json": b, ".stderr": diag.Bytes(), ".exit": []byte(fmt.Sprintln(code))} {
			if err := os.WriteFile(filepath.Join(*out, name+suffix), data, 0600); err != nil {
				return nil, code, err
			}
		}
		return b, code, nil
	}
	for _, step := range []struct {
		name string
		args []string
	}{{"version", []string{"version", "--json"}}, {"init", []string{"init", "--timezone", "UTC", "--json"}}, {"subscribe", []string{"feeds", "add", feed.URL, "--json"}}} {
		_, code, e := invoke(step.name, step.args...)
		if e != nil {
			return e
		}
		if code != 0 {
			return fmt.Errorf("%s exited %d", step.name, code)
		}
	}
	now := time.Now().UTC()
	from := now.AddDate(0, 0, -1).Format("2006-01-02")
	to := now.AddDate(0, 0, 2).Format("2006-01-02")
	args := []string{"digest", "--from", from, "--to", to, "--timezone", "UTC", "--include-baseline", "--json"}
	b, code, err := invoke("excerpts", append(args, "--summarize=false")...)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("excerpt smoke exited %d", code)
	}
	var excerpt struct {
		Data app.Digest `json:"data"`
	}
	if err = json.Unmarshal(b, &excerpt); err != nil {
		return err
	}
	if len(excerpt.Data.Entries) != 4 {
		return errors.New("expected four fixture entries")
	}
	// These short fixtures fit the 600-character excerpt and 12000-character model caps.
	inputs := map[string]string{}
	for _, entry := range excerpt.Data.Entries {
		if expected, ok := expectedInputs[entry.Title]; !ok || expected != entry.Text {
			return errors.New("CLI input differs from fixed fixture")
		}
		inputs[entry.Title] = entry.Text
		if entry.URL != "https://example.org/"+entry.Title || len(entry.Sources) != 1 {
			return errors.New("missing original source link")
		}
	}
	raw, err := json.MarshalIndent(inputs, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(*out, "model-inputs.json"), raw, 0600); err != nil {
		return err
	}
	summaryArgs := append(append([]string{}, args...), "--summarize", "--provider", *provider, "--endpoint", *endpoint, "--model", *model, "--max-input-chars", "12000", "--max-output-tokens", "256")
	b, code, err = invoke("summaries", summaryArgs...)
	if err != nil {
		return err
	}
	metadata := map[string]any{"fixture_model_only": fixture, "provider": *provider, "endpoint": *endpoint, "model": *model, "prompt_version": summarizer.PromptVersion, "input_cap": 12000, "output_cap": 256, "model_timeout": "60s", "temperature": "provider default (not sent)", "seed": "provider default (not sent)", "arguments": summaryArgs, "summary_exit": code, "utc": now.Format(time.RFC3339), "fixture_calls": calls.Load()}
	raw, err = json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(*out, "run.json"), raw, 0600); err != nil {
		return err
	}
	template, err := os.ReadFile("docs/evaluation/result-template.md")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(*out, "REVIEW.md"), template, 0600); err != nil {
		return err
	}
	if fixture {
		var result struct {
			Data app.Digest `json:"data"`
		}
		if err = json.Unmarshal(b, &result); err != nil {
			return err
		}
		if code != 0 || calls.Load() != 3 || len(result.Data.Entries) != 4 {
			return fmt.Errorf("fixture proof failed: exit %d, calls %d", code, calls.Load())
		}
		for _, entry := range result.Data.Entries {
			kind, label := "summary", "short"
			if entry.Title == "empty" {
				kind, label = "excerpt", "absent"
			}
			if entry.TextKind != kind || entry.LimitedContent != label || entry.URL != "https://example.org/"+entry.Title {
				return errors.New("fixture output labels/links differ")
			}
		}
		fmt.Println("fixture harness: ok (four entries, three model calls; no quality evidence)")
	} else {
		fmt.Printf("real model evaluation recorded; feeder exit %d; manual review required\n", code)
	}
	if code != 0 {
		return fmt.Errorf("summary digest exited %d; evidence retained", code)
	}
	return nil
}
