# feeder — personal feed digests

Status: build-ready, 2026-11-02. Keith confirmed the eight-round grill and authorized autonomous specification, tickets and implementation. This is the single authoritative product specification.

## Problem statement
People following many feeds need a brief they can read or hand to an agent without running a hosted reader or manually collecting links. Feed failures, late arrivals and model failures must be visible rather than silently dropping coverage.

## Solution
A single MIT Go binary stores subscriptions and feed items in local SQLite. Hourly external collection establishes first-seen timestamps. Every digest refreshes active subscriptions before selecting the requested calendar window. Output is a source-linked personal brief, using feed excerpts by default and opt-in model summaries.

## User stories
1. Add, tag, list and remove subscriptions without losing historical entries; import/export OPML.
2. Fetch feeds repeatedly without duplicating existing entries; edits update content but never reset first-seen time.
3. Receive daily/weekly briefs in a saved timezone with stable filters and limits, and explicit date ranges for missed periods.
4. Understand initial baseline exclusion, empty windows, late arrivals and incomplete coverage.
5. Request summaries locally or by API with caching and bounded requests; retain labeled excerpts on failure.
6. Pipe JSON to an agent or write Markdown to a chosen file without mixed diagnostics.
7. Schedule collection externally, safely overlap runs, and isolate collections with --home.
8. Install one binary or Homebrew formula on Linux/macOS and verify behavior without real network/model dependencies in CI.

## Feature matrix
| Feature | Decision | Milestone |
|---|---|---|
| Config/timezone, version/help, SQLite schema/migrations, bounded lock | v1 | M0 |
| RSS/Atom, subscriptions/tags, baseline and update identity, HTTP validators | v1 | M1 |
| Remove retaining history, explicit purge, OPML import/export | v1 | M1 |
| Daily/weekly/range windows, fetch-before-digest, filters and newest-first limit | v1 | M2 |
| Exact article-URL digest dedupe, all contributing sources | v1 | M2 |
| Markdown/JSON/stdout/file output, partial and empty results | v1 | M2 |
| Ollama/OpenAI-compatible summaries, caching and limits | v1 | M3 |
| Robust failure/cancellation/concurrency and scheduling documentation | v1 | M4 |
| Four binaries, checksum artifacts, Homebrew and evaluation kit | v1 | M5 |
| TUI, full-article fetch, authenticated feeds, Windows, other package managers | Later | — |
| Native additional model APIs, immutable run archives, item revision history | Later | — |
| Multi-user shared DB, sync, newsletter sending/publishing, internal scheduler | Later | — |
| Semantic story merging, automatic model ranking, monetary budgets | Later | — |
| Telemetry, hidden paid fallback or silent article scraping | Drop | — |

## Implementation decisions
### Boundaries and dependencies
- CLI parses arguments and renders results; application service owns fetch/select/summarize workflows. Storage owns SQL and migrations. Feed transport/parser and summarizer are narrow outbound seams.
- Tests enter through command execution with injected clock, transports and disposable state; use compiled subprocesses for locks, signals and output contracts.
- SQLite is durable source data, not a disposable cache. Never repair corruption by silently deleting it. Summary records alone are rebuildable.
- Use modernc.org/sqlite for CGO-free cross-builds, gofeed for real-world RSS/Atom parsing, Cobra for consistent flags/help/completions. Each dependency replaces substantial parser/platform work. Use standard-library JSON config and HTTP instead of Viper/provider SDKs. Pin resolved versions in go.mod/go.sum. An advisory-lock dependency is acceptable if it reduces cross-platform error-prone code.
- Store config.json and feeder.db beneath one home directory. Default Linux: XDG_DATA_HOME/feeder or ~/.local/share/feeder; macOS: ~/Library/Application Support/feeder. Override with --home > FEEDER_HOME. Directory mode 0700, config/database 0600.
- Settings precedence: command flags > supported FEEDER_* env > saved config > defaults. Never mutate saved config as a side effect of an override.
- Save an IANA timezone on initialization (derive from machine if possible; UTC fallback with explicit notice). Do not save an ephemeral fixed-offset zone in place of an IANA zone. Embed tzdata for portability.
- Acquire a cross-process advisory writer lock before initialization, migrations, config changes, subscription changes, fetch and digest work. Default wait 10s with context cancellation; busy exit on expiry. One coarse command lock is acceptable in v1; never hold a SQL transaction during network calls. Read-only commands must see committed state.
- Validate flags, dates, filters, provider configuration and output paths before fetching or invoking models. Help/version/completion never create state.

### Data model and feed lifecycle
Subscriptions: stable numeric ID, unique normalized HTTP(S) URL, display title, tags, active flag, initialized/baseline state, ETag/Last-Modified, last attempt/success/error. Removal deactivates; re-add restores same ID/history and initialized state.
Items: stable per-subscription key, title, exact resolved HTTP(S) article URL (optional), plain feed content, content hash, immutable first-seen UTC instant, optional published time, baseline boolean. Key prefers feed GUID/Atom ID; absent that use article URL, then deterministic title/publication fallback. Avoid merging GUIDs across subscriptions. Content edits update in place and invalidate summaries, but do not reset first-seen/baseline.
Baseline is all items from the first successful parsed fetch, including after prior failures. Successful empty feed initializes baseline. A failed or oversized parse commits no partial items or validators. Subsequent successful fetches add new items as non-baseline and retain items missing from later feed pages.
Summary cache keys include provider type, endpoint, model, prompt version, content hash and effective input/output settings. Never cache fallback/error text as a successful summary.
Purge is explicit and scoped to a removed subscription, deletes its items/cache and subscription record; --dry-run previews counts and --force confirms actual deletion. No implicit purge.
OPML accepts file or stdin, handles nested outlines, validates all URLs before transactional import, imports idempotently without network, preserves tags through documented OPML categories. Export includes active subscriptions only.

### Transport and content
Only explicit public HTTP(S) subscriptions, including explicitly entered loopback/private-network feeds. Reject URL credentials and unsupported schemes. Redirects are bounded; never forward secrets across origins. No cookie/login support.
Fetch uses bounded HTTP timeout (default 30s), body size (default 5 MiB), and redirect count (5). Honor conditional requests; only send validators after a successful stored response. A 304 must not reinitialize or create items. Independent feed failures preserve successful results and expose per-feed status.
Convert HTML feed content to readable text, discard scripts/styles and control sequences, preserve Unicode. Feed content is untrusted data. No execution, no tool access and no linked article downloads. Exact article URL is resolved relative to feed location, without heuristic tracking-param stripping.

### Digest selection
Default previous local calendar day; weekly previous Monday 00:00 through current Monday 00:00. Explicit --from/--to are YYYY-MM-DD local dates with inclusive from/exclusive to. Windows are half-open UTC instants derived using calendar arithmetic, including DST and year changes.
Always attempt refresh of active subscriptions before digest selection, even historical windows. Fetch failure still permits stored data output with coverage warning. Hourly collection is separately scheduled. New entries discovered today do not enter yesterday's window.
Filter eligible items by immutable first-seen time, feed IDs and tags; baseline excluded unless --include-baseline. Within a filter type use OR, between feed/tag types use AND. Removed subscriptions' history remains eligible and filterable, but they are never fetched.
For onboarding, show a concrete command using today's explicit range and --include-baseline after initial fetch. --include-baseline does not bypass the date window.
Dedupe eligible items by exact article URL; missing links stay separate and visibly say link unavailable. Use newest first-seen descending, then stable ID tie break. Deduplicate before limiting (default 20, positive configurable max 200). Sources include all eligible matching contributors. Choose newest representative content deterministically.
Daily/weekly overlap is allowed. No read/unread or delivered state. Regeneration uses latest content, so only saved files preserve snapshots.
Empty windows produce “No eligible items”, status ok if refresh succeeded. No subscriptions is a valid empty result with an onboarding hint.
Limited coverage must always be clear: all content is feed-provided, never verified full article. Flag absent/short content using documented deterministic heuristic; this is not proof that longer content is complete.

### Models
Excerpts default; --summarize or saved summarize=true enables model calls; --summarize=false overrides. Support Ollama /api/chat (non-streaming) and OpenAI-compatible /chat/completions under configured base URL (non-streaming). Explicit model required when summarizing.
API credential is supplied via FEEDER_API_KEY or a configured API key file path; never inline CLI secrets or saved plaintext key. Never print headers or echo keys in failures. Local Ollama needs no key. Do not silently switch provider on failure.
One selected deduped item per request, cache first. Default input cap 12000 Unicode characters, output cap 256 tokens, request timeout 60s, no automatic retries in v1. Maximum model calls equals selected item limit. Bounds must apply to response reads too.
Prompt instructs summarization of supplied data only, ignores instructions within it, requests short factual prose and no invented claims. Program attaches original source links; don't trust model-created citations. Empty content skips model with labeled no-content excerpt.
Errors/timeouts/malformed/empty responses produce labeled excerpt fallback and degraded status. Cancellation stops promptly. Never silently truncate away all content or treat invalid provider/model configuration as a successful fallback.
Keep evaluation fixtures for normal text, teaser, empty and prompt injection. Manual release review records model/settings, input, output, unsupported-claim findings and limited-content behavior.

## CLI contract
All commands are noninteractive. No colors by default. --help/-h, help <command>, --version and version; completion bash|zsh|fish. Help includes purpose, flags/defaults and an example.

| Command synopsis | Help purpose |
|---|---|
| feeder init [--timezone ZONE] | Initialize isolated local state and save timezone |
| feeder config show [--json] | Show effective non-secret settings |
| feeder config set KEY VALUE | Save a supported setting after validation |
| feeder feeds add URL [--tag TAG ...] [--title TITLE] | Register/reactivate a subscription; first fetch establishes baseline |
| feeder feeds list [--all] [--json] | List active subscriptions or retained removed ones |
| feeder feeds tag ID [--tag TAG ...] | Replace tags for a subscription |
| feeder feeds remove ID | Stop fetching while retaining history |
| feeder feeds purge ID --dry-run or --force | Preview/delete a removed subscription and retained items |
| feeder import FILE_OR_DASH | Import OPML transactionally, without fetching |
| feeder export [--output FILE_OR_DASH] | Export active subscriptions as OPML |
| feeder fetch [--json] | Fetch all active subscriptions; report per-feed results |
| feeder digest [--period daily|weekly] [--from DATE --to DATE] | Refresh then generate a calendar digest |
| feeder version | Print build version |
| feeder completion bash|zsh|fish | Emit shell completion script |

Global --home, --json, --lock-timeout; digest flags --timezone, repeatable --feed numeric-ID, --tag, --limit, --include-baseline, --summarize[=false], --provider ollama|openai, --model, --endpoint, --max-input-chars, --max-output-tokens, --output FILE_OR_DASH. Date flags supplied together and conflict with explicit --period. Unknown flags/commands/settings and invalid bounds are usage errors.

Saved settings: timezone, summarize, provider, model, endpoint, api_key_file, limit, max_input_chars, max_output_tokens, feed_timeout, model_timeout, lock_timeout. Environment equivalents prefixed FEEDER_ and uppercase, plus FEEDER_HOME/API_KEY. Exact supported settings/env documented with help/config docs. Defaults Ollama endpoint http://localhost:11434; OpenAI-compatible endpoint must be explicit when selected to avoid accidental transmission.

Output: primary data on stdout, diagnostics stderr. --output file creates atomically and refuses overwrite unless --force; no partial file on fatal errors. Output path must not clobber config/database/lock files (including aliases). --output - means stdout. Writes don't mix JSON with human notices; JSON has schema_version:1, status:ok|degraded|error, data, warnings:[], errors:[] (structured code/message and optional feed/item IDs). Digest data includes window/timezone, selected entries with sources, text kind excerpt|summary|fallback, limited-content label, fetch coverage and generation time. Document exact JSON before M2 ships.
Exit codes: 0 success (including empty); 1 fatal local/runtime failure; 2 usage/config validation; 3 degraded/partial result (including all feed fetches failing with stored output); 4 writer busy; 130 interrupted. Fatal output/write failure outranks degraded status. No successful empty output after an invalid command.

Examples:
```sh
feeder init --timezone America/New_York
feeder feeds add https://example.org/feed.xml --tag tech
feeder fetch --json
feeder digest --period daily
feeder digest --period weekly --tag tech --limit 30 --output weekly.md
feeder digest --from 2026-11-02 --to 2026-11-03 --include-baseline
feeder config set model llama3.2
feeder digest --summarize --provider ollama
feeder export --output - | feeder --home /tmp/feeder-demo import -
feeder feeds remove 1
feeder feeds purge 1 --dry-run
```

## Testing decisions
Canonical gate: bash scripts/accept.sh. Tests use disposable home, injected clock, fixture feed/model servers, no external network/API keys. Cover command output/exit behavior as primary seam; focused tests for DST/identity/cache keys supplement it.
M0: initialization idempotence/migration reopen, config precedence, no side effects on help, lock busy/cancel.
M1: RSS+Atom, empty/failed baseline, repeated/edited entries, 304 validators, OPML nested/idempotent round trip and rejected-input atomicity, remove/re-add/purge.
M2: fixed-clock calendar boundaries (DST/year), baseline/window interaction, filters then dedupe then limit, missing links, removed history, always-refresh, degraded and empty, JSON/stdout/stderr and file overwrite.
M3: fake provider request correctness, zero model calls by default, cached reuse and invalidation (content/model/endpoint/caps), caps before requests, invalid settings before fetch, failed/empty/oversized responses and injection-as-data.
M4: compiled subprocess contention, Ctrl-C, large/truncated/malformed feeds, database corruption preservation, output aliases, failed writes, body/resource limits.
M5: archive layout, checksum validation, four cross-builds, fixture smoke in installed binary. Linux/macOS CI; independent release review and live disposable feed/model smoke and manual summary evaluation before tagging.

## Out of scope
See Later/Drop rows; in particular no full article claims, model ranking, silent fallback to a paid service, internal scheduler or publishing.

## Open questions
None blocking implementation. Exact provider compatibility is the documented Chat Completions/Ollama subset, not a claim of supporting every vendor. Live model availability and cross-platform release evidence must be verified at release time.

## Further notes
One issue/branch/draft PR/new Sol 6.1 Medium T3 worktree thread per milestone. Astra orchestrates/reviews and may fix small findings directly. M0 lands alone. Preserve baseline and first-seen on re-add. Do not tag a release while a required gate is unavailable.
