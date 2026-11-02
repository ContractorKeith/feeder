package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/ContractorKeith/feeder/internal/app"
	"github.com/ContractorKeith/feeder/internal/output"
	"github.com/spf13/cobra"
)

func digestCommand(c commandIO, asJSON *bool) *cobra.Command {
	var period, from, to, zone, path string
	var ids, tags []string
	var baseline, force, summarize bool
	var limit int
	var provider, model, endpoint string
	var inputCap, outputCap int
	cmd := &cobra.Command{Use: "digest", Short: "Refresh active feeds and generate a calendar digest from feed excerpts", Example: "  feeder digest --period weekly --tag tech --output weekly.md\n  feeder digest --from 2026-11-02 --to 2026-11-03 --include-baseline", Args: usageArgs(cobra.NoArgs)}
	f := cmd.Flags()
	f.StringVar(&period, "period", "daily", "previous calendar daily or Monday-Sunday weekly window")
	f.StringVar(&from, "from", "", "inclusive local date YYYY-MM-DD (requires --to)")
	f.StringVar(&to, "to", "", "exclusive local date YYYY-MM-DD (requires --from)")
	f.StringVar(&zone, "timezone", "", "IANA timezone override (otherwise saved/environment setting)")
	f.StringArrayVar(&ids, "feed", nil, "feed numeric ID, including removed history (repeatable OR)")
	f.StringArrayVar(&tags, "tag", nil, "tag (repeatable OR; AND with feed filters)")
	f.IntVar(&limit, "limit", 20, "maximum deduplicated entries, 1..200 (otherwise saved/environment setting)")
	f.BoolVar(&baseline, "include-baseline", false, "include initial fetch items within the selected window")
	f.StringVar(&path, "output", "-", "output file or - for stdout (atomic, no overwrite)")
	f.BoolVar(&force, "force", false, "overwrite an existing regular output file")
	f.BoolVar(&summarize, "summarize", false, "reserved for M3; only false is supported")
	f.StringVar(&provider, "provider", "", "summary provider (reserved for M3)")
	f.StringVar(&model, "model", "", "summary model (reserved for M3)")
	f.StringVar(&endpoint, "endpoint", "", "summary endpoint (reserved for M3)")
	f.IntVar(&inputCap, "max-input-chars", 12000, "summary input cap (reserved for M3)")
	f.IntVar(&outputCap, "max-output-tokens", 256, "summary output cap (reserved for M3)")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		invalid := func(message string) error { return app.Invalid(errors.New(message)) }
		if f.Changed("from") != f.Changed("to") || ((f.Changed("from") || f.Changed("to")) && f.Changed("period")) {
			return invalid("supply --from and --to together, without --period")
		}
		if f.Changed("from") && (from == "" || to == "") {
			return invalid("date flags must not be empty")
		}
		service, err := c.collection(cmd)
		if err != nil {
			return err
		}
		overrides := service.Overrides
		for name, value := range map[string]string{"timezone": zone, "limit": strconv.Itoa(limit), "summarize": strconv.FormatBool(summarize), "provider": provider, "model": model, "endpoint": endpoint, "max-input-chars": strconv.Itoa(inputCap), "max-output-tokens": strconv.Itoa(outputCap)} {
			if f.Changed(name) {
				overrides[strings.ReplaceAll(name, "-", "_")] = value
			}
		}
		service.Overrides = overrides
		settings, err := app.Read(service.Home, c.d, overrides)
		if err != nil {
			return err
		}
		if settings["summarize"] == true {
			return invalid("summaries are not implemented until M3; use --summarize=false")
		}
		window, err := app.CalendarWindow(c.d.Now(), settings["timezone"].(string), period, from, to)
		if err != nil {
			return err
		}
		feedIDs := []int64{}
		for _, raw := range ids {
			id, e := strconv.ParseInt(raw, 10, 64)
			if e != nil || id < 1 {
				return invalid("--feed requires a positive numeric ID")
			}
			feedIDs = append(feedIDs, id)
		}
		tags, err = app.Tags(tags)
		if err != nil {
			return err
		}
		if err = output.Validate(service.Home, path, force); err != nil {
			return app.Invalid(err)
		}
		digest, err := service.GenerateDigest(app.DigestOptions{Window: window, FeedIDs: feedIDs, Tags: tags, IncludeBaseline: baseline, Limit: settings["limit"].(int)})
		if err != nil {
			return err
		}
		warnings := []string{}
		problems := []problem{}
		status := "ok"
		if digest.Coverage.Failed > 0 {
			status = "degraded"
			warnings = append(warnings, "Incomplete feed coverage; stored entries may omit arrivals during failed collection.")
			for _, r := range digest.Coverage.Feeds {
				if r.Error != "" {
					problems = append(problems, problem{Code: "feed_fetch", Message: r.Error, FeedID: r.FeedID})
				}
			}
		}
		var b bytes.Buffer
		if *asJSON {
			err = json.NewEncoder(&b).Encode(envelope{1, status, digest, warnings, problems})
		} else {
			_, err = b.WriteString(renderDigest(digest))
		}
		if err != nil {
			return err
		}
		if path == "-" {
			_, err = cmd.OutOrStdout().Write(b.Bytes())
		} else {
			err = output.Write(service.Home, path, force, b.Bytes())
		}
		if err != nil {
			return err
		}
		if !*asJSON {
			for _, w := range warnings {
				if _, err = fmt.Fprintln(cmd.ErrOrStderr(), w); err != nil {
					return err
				}
			}
			for _, p := range problems {
				if _, err = fmt.Fprintf(cmd.ErrOrStderr(), "feed %d: %s\n", p.FeedID, p.Message); err != nil {
					return err
				}
			}
			if digest.Hint != "" {
				if _, err = fmt.Fprintln(cmd.ErrOrStderr(), digest.Hint); err != nil {
					return err
				}
			}
		}
		if status == "degraded" {
			return degraded{}
		}
		return nil
	}
	return cmd
}

// Escape all Markdown syntax in feed text, including raw HTML and autolinks.
func markdownText(s string) string {
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune("\\`*_{}[]<>()#+-.!|~&", r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Entity-escape Markdown destination delimiters and ampersands so rendering
// decodes to the original URL, including query strings containing entities.
func markdownLink(label, raw string) string {
	safe := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\\", "&#92;", " ", "%20", "\"", "&quot;").Replace(raw)
	// Stored links are HTTP(S); fail closed when rendering unexpected data.
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return markdownText(label) + " (link unavailable)"
	}
	return "[" + markdownText(label) + "](<" + safe + ">)"
}
func renderDigest(d app.Digest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Feed digest\n\nWindow: %s to %s (exclusive), timezone %s.\n\n%s\n\nGenerated: %s\n\nCoverage: %d attempted, %d succeeded, %d failed.\n\n", d.Window.From.Format("2006-01-02T15:04:05Z07:00"), d.Window.To.Format("2006-01-02T15:04:05Z07:00"), d.Window.Timezone, d.ContentScope, d.GeneratedAt.Format("2006-01-02T15:04:05Z07:00"), d.Coverage.Attempted, d.Coverage.Succeeded, d.Coverage.Failed)
	if d.Coverage.Failed > 0 {
		b.WriteString("**Incomplete feed coverage. Stored entries may omit arrivals during failed collection.**\n\n")
	}
	if len(d.Entries) == 0 {
		b.WriteString("No eligible items.\n")
	}
	for _, e := range d.Entries {
		title := e.Title
		if title == "" {
			title = "Untitled"
		}
		b.WriteString("## ")
		if e.URL == "" {
			b.WriteString(markdownText(title) + " (link unavailable)")
		} else {
			b.WriteString(markdownLink(title, e.URL))
		}
		fmt.Fprintf(&b, "\n\nExcerpt — %s (feed-provided content).\n\n", e.LimitedContent)
		if e.Text == "" {
			b.WriteString("No feed content available.\n\n")
		} else {
			b.WriteString(markdownText(e.Text) + "\n\n")
		}
		b.WriteString("Sources:\n\n")
		for _, s := range e.Sources {
			label := s.Title
			if label == "" {
				label = fmt.Sprintf("Feed %d", s.FeedID)
			}
			fmt.Fprintf(&b, "- %s (feed %d, item %d)\n", markdownLink(label, s.FeedURL), s.FeedID, s.ItemID)
		}
		b.WriteString("\n")
	}
	return b.String()
}
