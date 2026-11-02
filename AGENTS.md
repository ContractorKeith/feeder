# feeder

A local Go CLI that collects RSS/Atom feeds into SQLite and generates personal source-linked digests. V1 is complete when every v1 row in docs/features/digest/SPEC.md passes on Linux/macOS and release evidence is recorded.

## Status
M0 merged in PR #7 (2026-11-02), with parent acceptance, independent attack fixes and Linux/macOS CI green. M1 merged in PR #11 (main 3587e9f): subscriptions/tags, bounded RSS/Atom fetch and OPML import/export. M2 calendar digests merged in PR #13 (main 8f12aad). M3 model summaries are implemented awaiting parent review and Linux/macOS CI.

## Commands
- `bash scripts/accept.sh` — canonical worker/reviewer/CI gate.
- `go build ./cmd/feeder`, `go test -race ./...`, `go vet ./...` — local development checks.

## Pointers
- docs/PLAN.md — milestone dependency ledger.
- docs/features/digest/SPEC.md — authoritative product and CLI contract.
- CONTEXT.md — glossary; docs/features/digest/grill.md — approved interview decisions.
- Central worklog: ../worklog/projects/feeder.md.

## Conventions & Gotchas
- Pure Go, no CGO. Only storage owns SQL; app owns orchestration; CLI owns argument parsing and output.
- Tests use disposable state through --home or t.TempDir, injected clocks and fixture HTTP servers. Never touch real subscriptions or install schedules.
- Database schema is version 3; JSON envelope remains schema_version 1. Validate schema/ledger integrity; never reset inconsistent data.
- First-seen time is immutable; baseline starts at the first successful fetch, even after failed subscription fetches.
- Always fetch before digest. Validate invocation before network or state changes.
- Secrets never appear in flags, logs, or JSON. Model output and feed HTML are untrusted text.
- Commit as ContractorKeith only, conventional type prefix, no AI attribution.

## Tracking
One milestone = one issue = one branch = one draft PR = one T3 worktree thread. M0 lands alone. Workers are gpt-6.1-sol, medium; no nested delegation. Astra orchestrates and reviews full branch diffs, verifies bounded deterministic reliability regressions in disposable state/local fixture servers, and reruns the shared acceptance gate with Linux/macOS CI. Open-ended attack/exploit exploration and new attack threads are not part of this process. Workers do not merge, tag, publish, or write the central worklog. Report PR URL, head SHA, checks/review state.

## Out of Scope
TUI, full-article scraping, semantic story merging, authenticated feeds, cloud sync, publishing, scheduler daemon, Windows, native Anthropic adapter.
