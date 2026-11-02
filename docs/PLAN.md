# feeder — build plan

Status: M0 merged via PR #7 on 2026-11-02; parent acceptance and Linux/macOS CI passed after independent attack findings were fixed. M1 implementation is in its draft PR; parent acceptance and Linux/macOS CI remain required before integration. Execution authorized 2026-11-02.

The authoritative spec is [features/digest/SPEC.md](features/digest/SPEC.md). Interview history stays beside it; root CONTEXT.md contains only language.

## Milestones and integration order

| Milestone | Outcome | Depends on | Demonstrable acceptance |
|---|---|---|---|
| M0 | Harness, schema, config, lock, CLI skeleton, CI | None | Isolated init/config/version, schema reopen, contention, Linux/macOS gate |
| M1 | Subscribe and reliably collect feeds | M0 | RSS/Atom baseline then new/edited items, removal and OPML round trip |
| M2 | Generate calendar digests | M1 | Refresh, timezone windows, first-seen, filters/dedupe, Markdown/JSON/file output |
| M3 | Summarize with bounded model calls | M2 | Fake Ollama/API requests, cache invalidation, caps, labeled degraded fallbacks |
| M4 | Prove operational failures and scheduling | M3 | Process contention/cancel, malformed/large feeds, atomic output, documented schedules |
| M5 | Package and validate release candidate | M4 | Four cross-builds, archive/checksum tests, Homebrew recipe, summary eval kit |

Merge order: M0 → M1 → M2 → M3 → M4 → M5. Shared CLI/storage contracts make sequential integration cheaper than parallel churn. Each milestone is a vertical slice with a command-level demonstration and a draft PR.

## Acceptance
`bash scripts/accept.sh` is the shared gate: format, vet, race tests, build, command smoke checks. It must fail before M0 exists and must never claim success by skipping absent packages. M0 makes it executable end to end; later milestones extend command-level fixtures. The shell stays Bash 3.2 compatible.

Before a release tag: inspect full main diff, run independent adversarial release review, real-feed/model checks in disposable state, manual summary evaluation, cross-platform CI and package verification. Record any unavailable live-model evidence honestly; never substitute fake checks for a real-model release gate.

## Tracking and process
GitHub milestone issues are authoritative. One Basecamp github card has one step per milestone. The orchestrator reads full branch diffs, reruns acceptance, tests adversarial cases, posts review evidence, marks ready and merges with merge commits. User requested Astra as orchestrator/reviewer and new T3 Sol 6.1 Medium implementation threads. Follow worker-brief for all assignments. Use the central worklog after merges. No autonomous paid credentials setup or machine-wide schedule installation.

## References
- [Newsboat](https://github.com/newsboat/newsboat): terminal feed workflow reference; do not copy its full reader/TUI scope.
- [rss-ollama-news-cli](https://github.com/mai-space/rss-ollama-news-cli): local model/feed workflow reference; feeder emphasizes deterministic windows and automation contracts.
- mdkb: process and packaging reference in the sibling workspace.
