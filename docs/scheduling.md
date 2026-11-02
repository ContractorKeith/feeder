# External scheduling

feeder has no daemon or schedule installer. These examples run hourly collection,
a daily digest, and a weekly digest against one explicit home. Copy and customize
them, then explicitly install with your scheduler. They use feed excerpts unless
you have saved `summarize=true`; configure a model separately if wanted.

Use an absolute binary path; no shell startup files or interactive PATH are
assumed. Create state with that binary and `--home /absolute/state init --timezone
UTC`, add subscriptions, and fetch once before relying on scheduled digests.
Every first successful fetch establishes excluded baseline, including after earlier
failed attempts. A successful empty feed also establishes baseline. To preview
initial items, use today's local `--from` and tomorrow's exclusive `--to` with
`--include-baseline`. This still respects the window.

Hourly collection matters: first-seen time is when feeder stores an item, not its
publisher date. An item first discovered by this morning's digest cannot enter
yesterday's window. Daily covers the previous calendar day; weekly covers the
previous Monday–Sunday, and may repeat daily entries. Both always refresh first.
Explicit timezone selection handles DST windows with calendar arithmetic.

The examples use UTC for feeder windows. Cron and launchd fire in the host's local
timezone; either use a UTC host or choose host firing times that occur after the
intended UTC window has closed. Changing feeder's timezone does not change the
scheduler's clock. Sleeping/offline machines miss collection opportunities;
launchd coalesces missed calendar firings on wake rather than replaying every hour.
Use explicit date ranges to regenerate missed digests; missing hourly collection
cannot reconstruct original arrival times. Cron implementations differ in DST
firing and missed-run behavior.

## Linux cron

The files are [run-feeder.sh](examples/run-feeder.sh) and
[linux.crontab](examples/linux.crontab). Replace `/home/reader` with your absolute
home path in every crontab command. Create `~/feeder/digests` and `~/feeder/logs`
and copy the wrapper to `~/feeder/run-feeder.sh`. Initialize `~/feeder/state` using
your absolute binary at `~/bin/feeder`. Here `~` describes setup only; the actual
crontab uses absolute paths throughout. Do not include spaces in cron example
paths unless you add shell quoting. Run `crontab -e` yourself to paste the three
customized entries. Nothing in the acceptance gate installs them.

The fetch entry appends one JSON envelope per invocation to `fetch.jsonl`;
JSON diagnostics stay in that envelope. Human digest diagnostics append to
`daily.err` / `weekly.err`. Digest stdout normally stays empty because primary
output goes to the chosen file; `.out` logs capture any future stdout. Shell
redirection errors are also failures and should be monitored. Precreate log
parents and keep state/log/output directories private (for example mode 0700).

## macOS launchd

Copy and customize these three property lists and the same wrapper:

- [org.feeder.fetch.plist](examples/org.feeder.fetch.plist)
- [org.feeder.daily.plist](examples/org.feeder.daily.plist)
- [org.feeder.weekly.plist](examples/org.feeder.weekly.plist)

Replace `/Users/reader` everywhere, including the absolute binary path. Create
`~/feeder/digests` and `~/feeder/logs` before running anything. Put the wrapper at
`~/feeder/run-feeder.sh` and initialize `~/feeder/state` with `~/bin/feeder`.
ProgramArguments is an argument array, so paths containing spaces need no shell
quoting inside the plist. `Weekday=1` is Monday. There is no `RunAtLoad`, daemon,
or KeepAlive retry loop. Each label runs at most one instance at a time, but
fetch/daily/weekly labels can overlap with each other or manual commands.

Validate customized files with `plutil -lint /absolute/path/org.feeder.fetch.plist`
(and daily/weekly). Only when you choose to install, copy them to your
`~/Library/LaunchAgents/` and run, for each file:

```sh
launchctl bootstrap "gui/$(id -u)" "$HOME/Library/LaunchAgents/org.feeder.fetch.plist"
```

To uninstall a job yourself, use `launchctl bootout` with the same domain/file.
launchd's `StandardOutPath` and `StandardErrorPath` keep streams separate and
append logs; they do not invoke shell redirections. Manage log retention yourself.
Inspect `launchctl print "gui/$(id -u)/org.feeder.fetch"` for the last exit status.
These commands are instructions for the user, never run by feeder or its tests.
Plist parsing and wrapper commands are validated on Linux with disposable fixtures;
native macOS scheduler execution is not claimed. macOS acceptance runs in CI.

## Outputs, overlap and recovery

The wrapper uses `--force` to atomically replace `daily.md` and `weekly.md` each
run. These are latest results, not immutable archives. A degraded result also
replaces the file and visibly labels incomplete coverage/fallbacks. Without
`--force`, an existing target is refused before fetching. For snapshots, change
the wrapper to pass a unique absolute filename (for example a UTC timestamp plus
a unique suffix) and omit `--force`; never reuse the same name. A fatal failure
before output commit leaves an existing digest intact. A later diagnostic-stream
failure can return 1 after a digest has already been committed.

Every writer shares a bounded advisory lock per home. A different `--home` is a
separate collection. Default wait is 10s; examples explicitly pass `10s`. Exit 4
means another writer is still busy; leave the lock file in place and retry later
or increase the wait. The OS releases the lock on process exit; deleting the lock
file is not recovery. Read-only commands see committed data. No automatic retry
or hidden schedule is installed.

| Exit | Meaning for automation |
|---|---|
| 0 | Success, including an empty digest |
| 1 | Fatal local/runtime/output failure; inspect stderr or JSON error |
| 2 | Invalid invocation/configuration; fix it before retrying |
| 3 | Produced partial/degraded results, including all feed refreshes failing or model fallbacks; inspect coverage |
| 4 | Writer busy after bounded wait; retry later |
| 130 | Interrupted; a subsequent command can reacquire the lock |

An output failure outranks degraded status. JSON primary data/errors go only to
stdout (or the selected digest file); human diagnostics go to stderr. Help and
completion remain text even with `--json`. The wrapper preserves feeder's exact
exit via `exec`; cron itself does not provide a persistent exit-status ledger, so
inspect the JSON/logs and use your own monitoring if needed. A killed process may
not produce output. An unreadable/corrupt database is preserved: retain
`feeder.db` and its sidecars for recovery and restore a verified backup to a
separate home. Never delete source state to make a scheduled invocation pass.
