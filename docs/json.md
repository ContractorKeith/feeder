# JSON output (schema_version 1)

`--json` emits one UTF-8 JSON object followed by a newline. Arrays are always
arrays, including empty results. Human diagnostics do not appear on stdout.
`digest --json --output FILE` writes this same complete envelope atomically to
FILE and leaves stdout empty; `--output -` writes stdout. Existing files require
`--force`. Fatal errors are reported through the normal stdout error envelope
(or stderr without `--json`), and never publish a partial destination file.

The envelope has exactly these fields:

| Field | Type | Meaning |
|---|---|---|
| schema_version | integer | Always 1 |
| status | string | `ok`, `degraded`, or `error` |
| data | command-specific value or null | Digest object below; null on fatal/usage error |
| warnings | string array | Coverage and summary fallback warnings for degraded digests |
| errors | object array | `code`, `message` strings; optional numeric `feed_id` or `item_id` |

Exit codes: 0 successful (including empty), 1 fatal local/output error,
2 usage/config validation, 3 degraded feed coverage or summary fallback (including every active feed
failing), 4 writer busy, 130 interrupted. Fatal output errors outrank exit 3.
Error codes are `usage`, `runtime`, `busy`, `interrupted`, `feed_fetch`, or `summary`. Summary errors carry numeric `item_id`; messages are generic and never contain raw provider diagnostics.

## Digest data

| Field | Type | Meaning |
|---|---|---|
| window | object | `from`, `to` RFC3339 UTC instants; `timezone` effective IANA zone |
| entries | object array | Selected, deduplicated entries in newest first-seen order |
| coverage | object | Integer `attempted`, `succeeded`, `failed`; `feeds` result array |
| generated_at | string | RFC3339Nano UTC generation instant after refresh/selection |
| content_scope | string | Feed-only limitation notice |
| hint | optional string | Empty-result onboarding/baseline command using today's local dates |

Window `from` is inclusive and `to` exclusive. Daily means the previous local
calendar day; weekly means previous Monday 00:00 through current Monday 00:00.
Explicit `--from`/`--to` local dates cannot be combined with explicit `--period`.
UTC window durations therefore vary across DST transitions.

Each entry contains exactly:

| Field | Type | Meaning |
|---|---|---|
| item_id | integer | Representative stored item ID |
| title | string | Representative plain feed title (may be empty) |
| url | string | Exact resolved article URL; empty when unavailable |
| first_seen | string | Representative immutable RFC3339Nano UTC instant |
| text | string | Feed excerpt or model summary, empty for absent content |
| text_kind | string | `excerpt`, `summary`, or `fallback` |
| limited_content | string | `absent`, `short`, or `feed_content` |
| sources | object array | All eligible contributing stored items, before limit |

`limited_content` is `absent` for zero Unicode code points, `short` for 1–199,
and `feed_content` for at least 200, measured on stored plain feed content before
excerpt truncation. Excerpts use at most the first 600 Unicode code points,
with an added ellipsis when truncated. Longer feed content is never proof of a
complete article. No linked article is downloaded. Models are opt-in via effective summarize=true;
`--summarize=false` overrides saved/environment true. A successful summary uses
`text_kind: summary`; a failed request retains the excerpt as `fallback` with
status `degraded`, a `summary` error containing `item_id`, warning and exit 3.
Empty feed content skips the model and remains `excerpt`/`absent`, without
summary degradation. Limited-content labels always describe the original feed
content, even after summarization. Links/sources remain program-attached stored
values. Configuration and key-file validation fail before refresh or state changes.

Each source has integer `feed_id`, integer `item_id`, string `title` (subscription
display title), string `feed_url`, and string `article_url` (empty if absent).
Duplicate eligible items with the exact same nonempty article URL form one
entry. Missing URLs remain separate. The representative is newest first-seen,
then highest stable item ID. Entries and sources use that same deterministic
order. Filters are applied before grouping; all eligible contributors are kept
for selected entries. Limit applies after grouping (default 20, range 1–200).
Feed filters use OR, tag filters use OR, and the two types combine with AND.
Removed feeds retain filterable history but are never refreshed.

Coverage feeds have integer `feed_id`, string `status` (`ok`, `not_modified`,
`error`), integer `items` (entries parsed, not newly inserted), and optional
string `error` on failure. Every active subscription is attempted, including
those outside selection filters and historical windows. Successful feeds commit
independently. Any failed refresh produces degraded output with stored data,
including an empty selection. No active subscriptions means zero attempted and
an empty coverage array; retained history may still produce entries.

Example (one absent-content entry):

```json
{"schema_version":1,"status":"ok","data":{"window":{"from":"2026-11-01T04:00:00Z","to":"2026-11-02T05:00:00Z","timezone":"America/New_York"},"entries":[{"item_id":2,"title":"A source item","url":"https://example.org/story?track=1","first_seen":"2026-11-01T12:00:00.000000001Z","text":"","text_kind":"excerpt","limited_content":"absent","sources":[{"feed_id":1,"item_id":2,"title":"Example","feed_url":"https://example.org/feed.xml","article_url":"https://example.org/story?track=1"}]}],"coverage":{"attempted":1,"succeeded":1,"failed":0,"feeds":[{"feed_id":1,"status":"not_modified","items":0}]},"generated_at":"2026-11-02T12:00:00Z","content_scope":"Feed-provided content only; linked full articles are not fetched or verified."},"warnings":[],"errors":[]}
```

For other command data shapes, see [README](../README.md). State database schema
version 3 is independent of this JSON envelope version.
