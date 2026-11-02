# feeder — grill decisions

Status: shared understanding confirmed; specification, tickets and implementation authorized on 2026-11-02.

## Starting direction

- Name: feeder, keeping the name despite existing GitHub/npm uses.
- Go + SQLite, MIT license, Linux/macOS single binary.
- CLI first; TUI later. JSON output for agents.
- RSS/Atom subscriptions, fetching, deduplication, OPML import/export, daily/weekly digests.
- Optional Ollama/API summaries; external schedulers invoke commands.

## Round 1 — purpose and content (confirmed)

- Personal reading brief, not a newsletter publishing workflow.
- Short per-item summaries with source links, not a combined narrative.
- Explicit feed/tag filters and a configurable newest-first limit select items.
- Use feed-provided content only in v1; clearly label limited coverage. No linked-article fetching.

## Round 2 — time and repeat runs (confirmed)

- Daily means the previous calendar day; weekly means the previous Monday–Sunday calendar week. Explicit date ranges cover missed days.
- Eligibility uses first-seen time, not the publisher's publication date. Initial backlog handling remains unresolved.
- Digest windows are independent: daily/weekly overlap is allowed, and prior inclusion does not suppress an item.
- Save a timezone, initially taken from the machine, with a per-command override.

## Round 3 — backlog and item lifecycle (confirmed)

- Store entries from the initial fetch as baseline items, excluded from normal digests unless explicitly included.
- Update changed items and invalidate their cached summaries, preserving original first-seen time.
- Within a digest, merge entries with exactly matching article URLs and retain all contributing sources. Different URLs remain separate in v1.
- Removing a subscription stops fetches but retains historical items; destructive deletion is a separate explicit purge command.

## Round 4 — models and usage controls (confirmed)

- V1 supports Ollama and one configurable OpenAI-compatible API connection; additional native API adapters are later scope.
- Digests use feed excerpts by default. Summaries require an explicit flag or saved setting.
- Summary failures retain the item with a labeled feed excerpt, mark output as degraded, and return a distinct exit status.
- Cache summaries by content, model, and prompt settings. Select items before invoking models; enforce configurable item, input-size, and output-token caps.
- No currency-denominated budget or per-run paid API confirmation in v1.

## Round 5 — fetching and output (confirmed)

- Always fetch before generating a digest; the proposed explicit-refresh-only approach was rejected.
- Keep successful feed updates when others fail, report failures per feed, return a distinct partial-success exit status, and produce the digest with an incomplete-coverage warning.
- Markdown goes to stdout by default; --json provides structured output and --output selects a file. Diagnostics go to stderr. No email or publishing in v1.
- Saved digest files are snapshots. Regenerating an old range uses current stored content; immutable run archives and item revision history are out of scope for v1.

## Round 6 — collection timing and onboarding (confirmed)

- Keep first-seen eligibility and use a separately scheduled hourly fetch; digest generation still always refreshes. A late discovery enters its discovery-day window, normally appearing in the following day's brief.
- Produce an explicit “No eligible items” digest for empty windows. Exit successfully if fetching succeeded; fetch failures still yield degraded status.
- Explain initial baseline exclusion in command output and show the exact opt-in preview command. Never silently include baseline items.

## Round 7 — operating boundaries (confirmed)

- One local database/config per user, with --home to isolate separate collections or tests. No sync or shared multi-user database in v1.
- Overlapping writers wait for a bounded period, then return a clear busy exit status instead of waiting indefinitely.
- Public HTTP(S) feeds, including explicitly subscribed local-network URLs. Authenticated feeds, login flows, and browser cookies are deferred.
- Document cron schedules for Linux and launchd schedules for macOS. Users install schedules explicitly; feeder has no service installer or scheduler daemon.

## Round 8 — release and build workflow (confirmed)

- Deterministic fixture-driven CI on Linux/macOS, without API keys or a running model. Before release, run real-feed and real-model smoke checks in a disposable collection.
- Maintain a fixed summary evaluation set covering full feed content, teasers, empty content, and misleading instructions embedded in feed text. Manually review source-linked summaries for unsupported claims and limited-content labels before release; this is evidence for the evaluated model/settings, not a universal accuracy guarantee.
- Distribute Linux/macOS binaries for amd64/arm64 and a Homebrew formula. Windows and other package managers are later scope.
- Request Codex gpt-6.1-sol at medium reasoning for milestone workers, subject to availability. One isolated worker per milestone; independent review and attack gates follow the linked build process. Do not substitute an unavailable model without Keith's decision.

## Confirmation

Keith confirmed the shared understanding and authorized the orchestrator to run spec and tickets autonomously, then build using new T3 threads with Sol 6.1 Medium workers. Astra owns orchestration and review.

## Next workflow

Run /spec, then /tickets, then the authorized build. Follow the Basecamp build process at https://app.basecamp.com/6307587/buckets/49448999/documents/10464058936 for scaffolding, milestone issues, the single repository board card, logging, and implementation gates. M0 lands alone before dependent milestones.
