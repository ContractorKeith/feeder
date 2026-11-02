// Package cli parses commands and owns stdout, stderr, JSON and exit status.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/ContractorKeith/feeder/internal/app"
	"github.com/ContractorKeith/feeder/internal/config"
	"github.com/ContractorKeith/feeder/internal/storage"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type problem struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	FeedID  int64  `json:"feed_id,omitempty"`
	ItemID  int64  `json:"item_id,omitempty"`
}
type envelope struct {
	SchemaVersion int       `json:"schema_version"`
	Status        string    `json:"status"`
	Data          any       `json:"data"`
	Warnings      []string  `json:"warnings"`
	Errors        []problem `json:"errors"`
}

// Execute constructs a fresh command for each invocation, including tests.
func Execute(ctx context.Context, args []string, out, stderr io.Writer, deps app.Dependencies, version string) int {
	d := deps.WithDefaults()
	var home, lockTimeout, timezone string
	var asJSON bool
	root := &cobra.Command{Use: "feeder", Short: "Collect feeds into local, source-linked personal digests", Example: "  feeder init --timezone America/New_York\n  feeder config show --json", Version: version, SilenceErrors: true, SilenceUsage: true}
	root.Long = `Collect feeds into local, source-linked personal digests.

Settings and environment: feeder help config. External scheduling examples:
docs/scheduling.md (cron and launchd; no internal daemon).

Exit codes: 0 success (including empty), 1 fatal local/runtime/output failure,
2 usage/config validation, 3 degraded/partial results, 4 writer busy,
130 interrupted. Output failure outranks degraded status.
Primary data goes to stdout or --output; human diagnostics go to stderr.`
	root.SetOut(out)
	// Cobra's help callback cannot return errors. Retain write failures so
	// both --help and the help command report a fatal output failure.
	var helpErr error
	defaultHelp := root.HelpFunc()
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		writer := &helpWriter{Writer: cmd.OutOrStdout()}
		cmd.SetOut(writer)
		defer cmd.SetOut(writer.Writer)
		defaultHelp(cmd, args)
		helpErr = writer.err
	})
	root.SetErr(stderr)
	root.SetArgs(args)
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetVersionTemplate("{{.Version}}\n")
	root.PersistentFlags().StringVar(&home, "home", "", "state directory (FEEDER_HOME, otherwise platform data directory)")
	root.PersistentFlags().BoolVar(&asJSON, "json", false, "emit schema_version 1 JSON")
	root.PersistentFlags().StringVar(&lockTimeout, "lock-timeout", "10s", "writer lock wait (FEEDER_LOCK_TIMEOUT or saved setting takes precedence over default)")
	overrides := func(cmd *cobra.Command) map[string]string {
		m := map[string]string{}
		if cmd.Flags().Changed("lock-timeout") {
			m["lock_timeout"] = lockTimeout
		}
		if cmd.Flags().Changed("timezone") {
			m["timezone"] = timezone
		}
		return m
	}
	emit := func(data any, warnings []string) error {
		if warnings == nil {
			warnings = []string{}
		}
		if asJSON {
			return json.NewEncoder(out).Encode(envelope{1, "ok", data, warnings, []problem{}})
		}
		for _, w := range warnings {
			if _, err := fmt.Fprintln(stderr, w); err != nil {
				return err
			}
		}
		switch v := data.(type) {
		case string:
			_, err := fmt.Fprintln(out, v)
			return err
		default:
			b, err := json.MarshalIndent(v, "", "  ")
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(out, string(b))
			return err
		}
	}
	resolve := func() (string, error) {
		h, err := config.Home(home, d.Getenv)
		if err != nil {
			return "", err
		}
		return h, nil
	}
	initCmd := &cobra.Command{Use: "init", Short: "Initialize isolated local state and save timezone", Example: "  feeder init --timezone UTC", Args: cobra.NoArgs}
	initCmd.Flags().StringVar(&timezone, "timezone", "", "IANA timezone (environment, machine timezone, then UTC)")
	initCmd.RunE = func(cmd *cobra.Command, _ []string) error {
		h, err := resolve()
		if err != nil {
			return err
		}
		c, w, err := app.Write(ctx, h, d, overrides(cmd), "", "", true)
		if err != nil {
			return err
		}
		return emit(map[string]any{"home": h, "config": c}, w)
	}
	configCmd := &cobra.Command{Use: "config", Short: "Inspect and save non-secret settings", Example: "  feeder config set timezone America/New_York", Long: `Inspect and save non-secret settings.

Settings (environment equivalent is FEEDER_ + uppercase setting):
  timezone: IANA zone (UTC default before initialization)
  summarize: boolean (false); provider: ollama|openai (ollama)
  model: string (empty); endpoint: HTTP(S) URL (http://localhost:11434)
  api_key_file: path (empty; never read by config commands)
  limit: integer 1..200 (20); max_input_chars: positive integer (12000)
  max_output_tokens: positive integer (256)
  feed_timeout: positive duration (30s); model_timeout: positive duration (60s)
  lock_timeout: positive duration (10s)

Precedence: explicit flags > supported FEEDER_* > saved config > defaults.
FEEDER_HOME selects state; --home overrides it. FEEDER_API_KEY supplies
OpenAI summary credentials and is never read or printed by config commands.
OpenAI requires an explicit endpoint; summarize=true requires a model.
Set endpoint/model before enabling those settings. Overrides are never saved.`}
	show := &cobra.Command{Use: "show", Short: "Show effective non-secret settings", Example: "  feeder config show --json", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		h, err := resolve()
		if err != nil {
			return err
		}
		c, err := app.Read(h, d, overrides(cmd))
		if err != nil {
			return err
		}
		return emit(c, nil)
	}}
	set := &cobra.Command{Use: "set KEY VALUE", Short: "Save a supported setting after validation", Example: "  feeder config set limit 30", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		h, err := resolve()
		if err != nil {
			return err
		}
		c, w, err := app.Write(ctx, h, d, overrides(cmd), args[0], args[1], false)
		if err != nil {
			return err
		}
		return emit(c, w)
	}}
	configCmd.AddCommand(show, set)
	versionCmd := &cobra.Command{Use: "version", Short: "Print build version", Example: "  feeder version", Args: cobra.NoArgs, RunE: func(_ *cobra.Command, _ []string) error { return emit(version, nil) }}
	completion := &cobra.Command{Use: "completion bash|zsh|fish", Short: "Emit a shell completion script", Example: "  feeder completion bash > feeder.bash", ValidArgs: []string{"bash", "zsh", "fish"}, Args: cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs), RunE: func(_ *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return root.GenBashCompletionV2(out, true)
		case "zsh":
			return root.GenZshCompletion(out)
		default:
			return root.GenFishCompletion(out, true)
		}
	}}
	root.AddCommand(initCmd, configCmd, versionCmd, completion)
	cio := commandIO{ctx, d, resolve, overrides, emit}
	root.AddCommand(feedCommands(cio), fetchCommand(cio, &asJSON), digestCommand(cio, &asJSON))
	root.AddCommand(opmlCommands(cio)...)
	// Mark parse/argument errors as usage errors; command bodies retain runtime errors.
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return app.Invalid(errors.New("invalid flags; see --help")) })
	for _, c := range []*cobra.Command{initCmd, show, set, versionCmd, completion} {
		check := c.Args
		c.Args = func(cmd *cobra.Command, args []string) error {
			if err := check(cmd, args); err != nil {
				return app.Invalid(errors.New("invalid arguments; see command --help"))
			}
			return nil
		}
	}
	root.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return app.Invalid(errors.New("unknown command; see --help"))
		}
		return cmd.Help()
	}
	configCmd.RunE = func(_ *cobra.Command, _ []string) error {
		return app.Invalid(errors.New("choose config show or config set"))
	}
	err := root.ExecuteContext(ctx)
	if helpErr != nil {
		err = helpErr
	}
	if err == nil {
		return 0
	}
	if _, ok := err.(degraded); ok {
		return 3
	}
	code, kind, message := 1, "runtime", "local operation failed: "+err.Error()
	var invalid *app.ValidationError
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		code, kind, message = 130, "interrupted", "interrupted"
	case errors.Is(err, storage.ErrBusy):
		code, kind, message = 4, "busy", err.Error()
	case errors.As(err, &invalid):
		code, kind, message = 2, "usage", err.Error()
	case strings.HasPrefix(err.Error(), "unknown command "):
		code, kind, message = 2, "usage", "unknown command; see --help"
	}
	if jsonIntent(root, args) {
		if e := json.NewEncoder(out).Encode(envelope{1, "error", nil, []string{}, []problem{{Code: kind, Message: message}}}); e != nil {
			fmt.Fprintln(stderr, "failed to write JSON output")
			return 1
		}
	} else {
		fmt.Fprintln(stderr, message)
	}
	return code
}

type helpWriter struct {
	io.Writer
	err error
}

func (w *helpWriter) Write(b []byte) (int, error) {
	n, err := w.Writer.Write(b)
	if err == nil && n != len(b) {
		err = io.ErrShortWrite
	}
	if w.err == nil {
		w.err = err
	}
	return n, err
}

// jsonIntent recovers output intent when Cobra stops at an earlier bad token.
// Known value-taking flags consume their next token, even when it is --json;
// an option terminator ends scanning. Unknown flags cannot consume known flags.
func jsonIntent(root *cobra.Command, args []string) bool {
	var lookup func(*cobra.Command, string) *pflag.Flag
	lookup = func(cmd *cobra.Command, name string) *pflag.Flag {
		if f := cmd.Flags().Lookup(name); f != nil {
			return f
		}
		if f := cmd.PersistentFlags().Lookup(name); f != nil {
			return f
		}
		for _, child := range cmd.Commands() {
			if f := lookup(child, name); f != nil {
				return f
			}
		}
		return nil
	}
	enabled := false
	for i := 0; i < len(args); i++ {
		token := args[i]
		if token == "--" {
			break
		}
		if !strings.HasPrefix(token, "--") {
			continue
		}
		name, value, attached := strings.Cut(strings.TrimPrefix(token, "--"), "=")
		flag := lookup(root, name)
		if flag == nil {
			continue
		}
		if name == "json" {
			if !attached {
				enabled = true
			} else if v, err := strconv.ParseBool(value); err == nil {
				enabled = v
			}
			continue
		}
		if !attached && flag.NoOptDefVal == "" {
			i++
		}
	}
	return enabled
}
