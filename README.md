# feeder

A local Go CLI for collecting RSS/Atom feeds and producing personal source-linked digests. Feed excerpts are the default, with opt-in summaries through Ollama or an OpenAI-compatible endpoint.

**Status:** M0–M5 merged through main 6637a69. Parent acceptance, Linux/macOS CI, four-platform packaging and native package smokes passed. Real-model evaluation and manual summary review remain pending before release. No v1 release yet. MIT licensed; Linux and macOS, pure-Go SQLite.

```sh
go build -o feeder ./cmd/feeder
./feeder --home ./demo-state init --timezone America/New_York
./feeder --home ./demo-state config set limit 30
./feeder --home ./demo-state config show --json
./feeder completion bash
bash scripts/accept.sh
```

Implemented commands: `init`, `config show/set`, `feeds add/list/tag/remove/purge`, `fetch`, `digest`, `import`, `export`, `version`, `help`, and `completion bash|zsh|fish`. Each command supports `--help` with examples.

Collect feeds without downloading linked articles:

```sh
./feeder --home ./demo-state feeds add https://example.org/feed.xml --tag tech --title Example
./feeder --home ./demo-state fetch --json
./feeder --home ./demo-state feeds list --json
./feeder --home ./demo-state feeds tag 1 --tag tech --tag go
./feeder --home ./demo-state export --output feeds.opml
./feeder --home ./demo-copy import feeds.opml
./feeder --home ./demo-state feeds remove 1
./feeder --home ./demo-state feeds purge 1 --dry-run
# Actual purge requires --force and a removed subscription.
```

Add/import register without fetching. The first successfully parsed fetch stores all entries as baseline, including after failed attempts; a successful empty feed also initializes baseline. Later arrivals are non-baseline. Edits preserve first-seen time and baseline; repeats do not duplicate items. Identity is per feed: GUID/Atom ID, otherwise resolved article URL, otherwise a hash of title and publication text. Removal retains history; re-add preserves the ID, baseline state, validators and items. Repeated add/import merges tags, and a supplied nonempty title updates the title. `feeds tag` replaces tags; omitting tags clears them.

Every digest refreshes active feeds before selecting by immutable first-seen time. Initial fetches establish baseline and are excluded by default. After the first fetch on 2026-11-02 in the saved timezone, preview baseline with:

```sh
feeder digest --from 2026-11-02 --to 2026-11-03 --include-baseline
```

Use today's date and tomorrow's exclusive end date. Including baseline still respects that calendar window.

```sh
feeder digest                         # previous local calendar day
feeder digest --period weekly --tag tech --limit 30 --output weekly.md
feeder digest --timezone UTC --feed 1 --json --output -
```

Weekly covers the previous Monday–Sunday, using calendar arithmetic through DST/year changes. Explicit dates are inclusive from/exclusive to and conflict with explicit --period. Feed IDs and tags are repeatable (OR within a type, AND between types); removed history remains eligible. Exact article URLs deduplicate after filtering and before the newest-first limit (default 20, max 200). All eligible contributors remain attached; missing links stay separate. New arrivals discovered today cannot enter yesterday, regardless of publisher dates.

Markdown is the default. Entries are labeled excerpts, with absent/short feed content called out; linked full articles are never fetched or verified. Short means fewer than 200 Unicode characters; excerpts cap at 600 plus an ellipsis. Models are called only when summaries are enabled (override saved true with `--summarize=false`). Any failed refresh produces visible incomplete coverage and exit 3 while retaining stored output. Empty/no subscriptions succeeds with a hint. --output FILE writes atomically, refuses overwrite without --force, and protects state files/aliases; --output - writes stdout. See [the exact JSON schema](docs/json.md).

Fetch allows explicitly supplied HTTP(S) URLs, including private/loopback hosts, rejects credentials and other schemes, and normalizes subscription scheme/host, default port, empty path and fragment. Article links resolve against the final feed URL and retain exact query parameters. No article is fetched. Feed HTML becomes plain Unicode text; scripts, styles, ANSI escapes and controls are removed. The default timeout is 30s (`feed_timeout`), with a 5 MiB decompressed body cap and at most five redirects. No cookies or login support. Conditional validators are sent only after a stored success and withheld across origins. A 304 preserves items and initialization. Invalid, oversized, truncated or failed responses preserve items, validators and baseline; attempt/error metadata is updated. Successful feeds commit independently; local storage/output failures remain fatal.

`import FILE_OR_DASH` reads files or stdin, accepts nested outlines, validates the entire document (maximum 5 MiB, 100 outline levels), then imports transactionally without network access. The OPML `category` attribute carries comma-separated tags; nested children inherit category attributes from parents. Folder titles are organizational text, not tags. Tags cannot be empty, contain commas or control characters. Export writes active subscriptions only, as flat outlines with categories, preserving URLs, titles and tags. `export --output -` (default) always emits OPML, including when `--json` is set. Import JSON data is `{"feed_ids":[1,2]}`; add returns `{"id":1}`. List data is an array with ID, URL, title, tags, active/initialized flags, validators and last attempt/success/error. Fetch data is an array of `{"feed_id":1,"status":"ok|not_modified|error","items":0,"error":"optional message"}`; items counts entries parsed in this response, not new arrivals. Partial failures include structured `feed_fetch` errors with feed IDs. Purge preview reports retained item count. Export to a file uses an atomic write, refuses overwrite without `--force`, and rejects state paths, aliases, symlinks and special files. File-export JSON reports `{"output":"path"}`.

State is `config.json`, `feeder.db` and `feeder.lock` under `--home`, then `FEEDER_HOME`, then the platform default: Linux `$XDG_DATA_HOME/feeder` (or `~/.local/share/feeder`), macOS `~/Library/Application Support/feeder`. State directories are mode 0700; config, database and lock files are mode 0600. Help, version and completion never create state. `config show` reads committed config without creating or migrating state, and shows defaults before initialization.

`init` saves an IANA timezone, deriving it from the machine when possible; it warns when falling back to UTC. Repeating init preserves saved settings, including the timezone. Explicit initialization timezone uses `--timezone` over `FEEDER_TIMEZONE`. Embedded timezone data makes IANA zones available independently of system tzdata.

Settings precedence is explicit command flags > supported environment > saved config > defaults. Overrides never change saved settings. `config set` validates both saved and effective settings and atomically replaces the config while holding a bounded cross-process advisory writer lock. The default wait is 10s; `--lock-timeout` overrides it. Ctrl-C cancels waits. SQLite migrations are transactional; corrupt or newer databases are preserved and produce an error. State targets (including SQLite journal/WAL/shared-memory sidecars) must be regular, singly linked files. Symlinks, hardlinks and special files are rejected before mutations; app-owned files use descriptor-relative no-follow opens. The selected home itself must be a directory rather than a symlink. Config files larger than 1 MiB are rejected without replacement. Database schema version 3 adds the summary cache, migrating versions 1/2 transactionally while preserving subscriptions and items. The migration ledger, expected table definitions, SQLite integrity and foreign keys are checked on open; inconsistent schemas are rejected without resetting data. JSON envelope schema_version remains 1.

| Saved setting | Environment | Default / bounds |
|---|---|---|
| timezone | FEEDER_TIMEZONE | UTC before init; IANA zone |
| summarize | FEEDER_SUMMARIZE | false; boolean |
| provider | FEEDER_PROVIDER | ollama; ollama or openai |
| model | FEEDER_MODEL | empty; required when summarize=true |
| endpoint | FEEDER_ENDPOINT | http://localhost:11434; HTTP(S), no credentials/query/fragment |
| api_key_file | FEEDER_API_KEY_FILE | empty; key file path |
| limit | FEEDER_LIMIT | 20; integer 1–200 |
| max_input_chars | FEEDER_MAX_INPUT_CHARS | 12000; positive integer |
| max_output_tokens | FEEDER_MAX_OUTPUT_TOKENS | 256; positive integer |
| feed_timeout | FEEDER_FEED_TIMEOUT | 30s; positive Go duration |
| model_timeout | FEEDER_MODEL_TIMEOUT | 60s; positive Go duration |
| lock_timeout | FEEDER_LOCK_TIMEOUT | 10s; positive Go duration |

Set the endpoint before selecting `openai` and set the model before enabling `summarize`. OpenAI cannot inherit Ollama's default endpoint. Summary settings are validated before digest refresh; feed collection never makes model requests. `FEEDER_API_KEY` and key-file contents are never read or printed by foundation commands. There is no inline API key setting or flag.

Opt-in summaries use standard HTTP without provider SDKs. Configure a model, then enable for one digest:

```sh
feeder config set model llama3.2
feeder digest --summarize --provider ollama
# OpenAI-compatible base URL must be explicit; supply the key via environment
# or config set api_key_file /path/to/private-key-file.
feeder digest --summarize --provider openai --model your-model --endpoint https://api.example.org/v1
```

The supported subset is nonstreaming Ollama `POST /api/chat` with `options.num_predict`, or OpenAI-compatible base URL plus `/chat/completions` with `max_tokens`. Both send system/user messages, `model`, and `stream:false`. This is not a compatibility claim for every vendor. OpenAI-compatible servers may run without authentication (for example MLX). With neither a key nor key file configured, no Authorization header is sent. Authenticated servers use `FEEDER_API_KEY` (takes precedence) or a readable regular key file, trimmed and at most 8192 bytes. Explicit invalid/empty key files still fail validation; server authentication failures produce labeled fallback. Ollama requires no key. There are no key flags or saved plaintext keys. Redirects are rejected for model requests, so credentials cannot cross origins. Raw provider diagnostics are never printed.

Selection, filtering, exact-URL deduplication and limit happen before model calls. Each nonempty selected representative supplies its full plain feed content, capped at `max_input_chars` Unicode code points (default 12000), rather than its 600-character display excerpt. The user message JSON-quotes this untrusted data; the system prompt requests source-only factual prose, ignores embedded instructions, and supplies no tools. The program attaches stored source links. Empty content skips the request, retaining an absent-content excerpt label. Generated text is untrusted and Markdown-escaped.

Default output cap is 256 tokens and per-request timeout is 60s (`model_timeout`); response reads cap at 1 MiB, including decompressed data. No automatic retries or alternate providers. Maximum requests equal the selected entry limit. Successful summaries alone are cached durably by item/content, provider, normalized endpoint, model, prompt version and effective caps (including timeout/response limit). Edits and setting changes invalidate reuse; purge cascades to cached records. Failures, timeouts, empty/malformed/oversized responses retain labeled excerpt fallbacks (`text_kind: fallback`), degraded JSON/Markdown and exit 3. Cancellation exits 130; fatal output failures take precedence over degraded status.

Fixed evaluation inputs are in `internal/summarizer/testdata/`: normal, teaser, empty and instruction-bearing text. These are deterministic protocol/reliability fixtures, not real-model quality evidence. Manual release review must record real model/settings, input/output, unsupported claims and limited-content behavior; no live-model evaluation has been performed for M3.

`--json` on state/config commands and `version` emits one envelope on stdout:

```json
{"schema_version":1,"status":"ok","data":{},"warnings":[],"errors":[]}
```

For `init`, data is `{ "home": "...", "config": { ...effective settings... } }`; for config show/set it is the effective settings object; for `version` it is a string. Settings use JSON strings, booleans and integers; durations are normalized Go duration strings. Errors use status `error`, data `null`, and errors `[{"code":"usage|runtime|busy|interrupted","message":"..."}]`. JSON warnings stay in the envelope; human diagnostics go to stderr. Error output honors `--json` on either side of an invalid command or flag. Tokens consumed as flag values and tokens after `--` do not select JSON mode. Help, completion scripts and `--version` retain their native text formats. Exit codes: 0 success, 1 local/runtime or output failure, 2 usage/config validation, 4 writer busy, 130 interrupted. Code 3 and status `degraded` report partial feed results or summary fallbacks, including all feeds failing.

Tests use temporary state, injected clocks/HTTP clients/environment, fixture HTTP servers, and compiled subprocesses for contention and signals. `scripts/accept.sh` runs formatting, vet, race tests, a CGO-disabled build and isolated CLI smoke checks. CI runs that same gate on Linux and macOS. `golang.org/x/sys/unix` supplies descriptor-based OS advisory locks on Linux/macOS; the same validated descriptor is held through lock wait and release. Cobra owns parsing/help/completion and modernc SQLite avoids a CGO runtime dependency. M1 adds gofeed for real-world RSS/Atom parsing (and its parser dependencies), plus x/net/html for safe HTML-to-text extraction; both preserve the pure-Go build.

- [External cron/launchd scheduling and exit handling](docs/scheduling.md)
- [M4 reliability coverage map](docs/reliability.md)
- [Product specification](docs/features/digest/SPEC.md)
- [Build plan](docs/PLAN.md)
- [Glossary](CONTEXT.md)

Not affiliated with other projects named feeder.

## Install and uninstall

No v1 release or Homebrew tap is published yet. For now, build from source using
the pinned Go version in go.mod:

```sh
go build -trimpath -o feeder ./cmd/feeder
mkdir -p "$HOME/.local/bin"
install -m 0755 feeder "$HOME/.local/bin/feeder"
export PATH="$HOME/.local/bin:$PATH"
feeder --help
```

Persist that PATH in your shell configuration yourself if needed. Linux/macOS
release archives will contain a single executable, MIT license, README and build
metadata. After the parent publishes a release, download the matching OS/CPU
archive and SHA256SUMS from that release, then verify **before** extraction:
`sha256sum --check --ignore-missing SHA256SUMS` on Linux, or
`shasum -a 256 --check SHA256SUMS` on macOS (download all four archives for a
complete check, or check the matching checksum row). Extract into a new temporary
directory and install its feeder as above. See [packaging/release checklist](docs/release.md)
for exact asset names, locally testable formula generation and native smoke.
Generated formulas use real archive hashes; a tap install command will be supplied
only after the parent publishes a tap.

For a disposable first run:

```sh
demo=$(mktemp -d)
feeder --home "$demo/state" init --timezone UTC
feeder --home "$demo/state" feeds add https://go.dev/blog/feed.atom
feeder --home "$demo/state" fetch --json
feeder --home "$demo/state" digest --from 2000-01-01 --to 2100-01-01 --include-baseline --limit 5
```

That wide range is useful only for a fresh demo home. In an ongoing collection,
use actual calendar dates as described above. Feed content is never a verified
full article. Summaries require explicit opt-in and an already configured model;
see the [manual evaluation kit and disposable live smoke](docs/evaluation/README.md).

Uninstall the manually installed executable with `rm "$HOME/.local/bin/feeder"`.
For a future tap installation use `brew uninstall feeder`. Neither removes
subscriptions or history. Stop any schedules you installed yourself using the
[scheduling instructions](docs/scheduling.md). Back up state before intentionally
removing it; removing the binary is enough to stop manual use.
