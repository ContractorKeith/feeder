# M4 reliability coverage

M4 extends the M0–M3 suite only where observable reliability coverage was missing.
All command tests use disposable homes, injected clocks/transports or local HTTP
fixtures. No installed schedules, real subscriptions, credentials or live models.
The canonical gate remains `bash scripts/accept.sh` on Linux/macOS CI.

| M4 criterion | Reused evidence | M4 addition |
|---|---|---|
| Writer busy/cancel and cleanup | `TestWriterBusyAndCancellation` | `TestCompiledFetchDigestOverlapAndInterrupt`: both fetch/digest holder directions; fixture HTTP arrival proves lock ownership; opposite writer exits 4 without fetching, holder SIGINT exits 130, next invocation exits 0 |
| Help output failure | `TestNoStateForInformationalAndInvalidCommands` proves normal help has no state | `TestHelpWriteFailure`: root/subcommand flags, help subcommand and implicit root help return 1 with portable failing stdout; Cobra error retention fix |
| JSON stream hygiene / failure precedence | `TestJSONParseErrorsIgnoreFlagPosition`, `TestFetchOutputFailureOutranksPartial`, `TestDigestLateOutputFailureOutranksCoverage`, `TestSummaryFailuresRetainEntriesAndNeverCache` | Subprocess busy/interrupted results require exactly one schema-1 error envelope and empty stderr |
| Malformed/truncated/large feeds | `TestFetchBoundsAndUnexpected304`, `TestMixedFailuresEmptyBaselineAndAtom`, `TestTrailingFeedDataDoesNotCommit`, `TestMismatchedFeedNestingPreservesCollection` | `TestFailedRefreshPreservesSourceAndSummary`: successful source items/validators/first-seen/baseline/last-success and cached summary survive; attempt/error metadata changes, digest visibly degrades |
| Malformed/truncated/large models | `TestSummaryFailuresRetainEntriesAndNeverCache` covers fallback text, exit 3, response bound, no retries/cached errors | `TestFailedModelPreservesPriorCache`: failed request under another model setting leaves prior successful cache/source usable, and failed setting is retried on the next invocation |
| Database corruption/schema integrity | `TestCorruptDatabaseIsPreserved`, storage newer/inconsistent schema and migration-ledger tests | Corrupt bytes remain unchanged; error now names database/sidecars and verified backup recovery in a separate home |
| Atomic files, aliases, overwrite refusal/force | `TestDigestDailyWeeklyFiltersAndOutput`, `TestDigestInvalidBeforeNetworkOrMutation`, `TestOPMLTransactionalRoundtripAndOutputProtection`, `TestExportRejectsParentAliasesAndLeavesExistingOutput`, output/state tests using deterministic selection seams | Reuse existing cases; no live path-race probes |
| Baseline retry, first-seen, timezone boundaries | `TestMixedFailuresEmptyBaselineAndAtom`, `TestCollectionBaselineEditsValidatorsAndHistory`, `TestDigestBaselineRefreshAndDegraded`, `TestCalendarWindows`, `TestSelectionParsedInstantsAndBoundaries` | Reuse failed-then-successful/empty baseline, immutable first-seen and half-open DST/year boundary coverage |
| External schedules / settings / exits | README setting table, `config --help`, precedence/validation tests | Runnable cron and three launchd examples plus wrapper; compiled test parses actual schedules, checks arguments/intervals/log paths, runs cron commands and plist argument arrays against temporary state/local feed, and verifies daily/weekly files |

The old compiled init/lock test used a startup sleep before SIGINT. It is replaced
by bounded real fetch/digest subprocess tests that signal only after the server
receives the request. Child processes have deadlines, pipe wait bounds and cleanup;
HTTP handlers release on a channel or request cancellation. The gate runs the race
detector once; no probabilistic repetition or open-ended probing is needed.

Before/after evidence: help's five failed-writer cases returned 0 before the fix
and 1 after it; ordinary help still exits 0 without creating state. The strengthened
corruption assertion initially failed with only `file is not a database (26)`;
it now passes with actionable guidance and unchanged bytes. Other new reliability
cases passed against existing M3 behavior and required no production changes.

Linux validates shell syntax, plist XML/arguments/calendar fields and actual wrapper
commands. Native launchd/cron service installation is deliberately untested; macOS
acceptance is CI evidence. Live feed/model smoke, manual summary evaluation,
packaging and release evidence remain M5. The approved product spec is unchanged.
