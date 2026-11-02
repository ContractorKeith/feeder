---
name: skeptic-env
description: Run feeder review against disposable state and fixture servers.
---
# feeder review environment
Read AGENTS.md and docs/features/digest/SPEC.md. Run bash scripts/accept.sh.
All tests use --home in a directory created for this review; never use the default user data directory.
Use fixture HTTP/model servers for reproducible failures. Cross-process lock/signal tests need compiled subprocesses. No schedules are installed.
Before a tag, use a disposable collection for a real public feed and a configured real Ollama/API model; report unavailable model access rather than inventing evidence. Check docs/evaluation instructions once M5 supplies them.
Inspect first-seen windows (DST), failed initial baseline, remove/re-add, duplicate URLs, partial feed failure, stale summary cache, malformed/oversized model output, output file aliases, locks and cancellation.
No telemetry or external publishing. Do not read credential files or print environment secrets.
