# Usage queries

Usage reports aggregate immutable, deduplicated `call_records`. They provide request counts, reported token coverage, historical charges, time trends, model distributions, and Key rankings for Personal and Project principals. They do not read live quota counters or recalculate historical receipts.

## Endpoints and authority

| Endpoint                                 | Access and attribution                                                                                                                                              |
| ---------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET /api/v1/usage`                      | Current active account; only its Personal facts (`user_id` matches and `project_id` is empty), including revoked or rotated Keys.                                   |
| `GET /api/v1/projects/:project_id/usage` | Current Project manager or `calls.read_all`; only that Project's facts. The creator has no implicit access. Disabled and archived Project history remains readable. |
| `GET /api/v1/admin/usage`                | Current `calls.read_all` authority; installation-wide facts, optionally narrowed by principal or route.                                                             |

A repeatable-read transaction checks current account/permissions/manager membership and selects facts from the same database snapshot. A membership change affects subsequent queries. Personal and Project results expose model and Key IDs; upstream connection and provider-model dimensions are restricted to the administrative endpoint. Guessed filters never widen the caller's scope.

## Query contract

Unknown, repeated, malformed, and explicitly empty query parameters return `400`. Booleans are exactly `true` or `false`.

| Parameter                            | Values                                                                                                                                                                                                       |
| ------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `period`                             | `today`, `24h`, `7d`, `month` (default), `90d`, `year`. `today`, `month`, and `year` start at the corresponding local calendar boundary; the other presets are elapsed durations. Presets end at query time. |
| `from`, `to`                         | An RFC3339 timestamp pair, including offsets and optional fractional seconds. Mutually exclusive with `period`. Selection uses request `started_at` in `[from, to)`.                                         |
| `timezone`                           | IANA location, default `UTC`. Used for calendar boundaries and returned bucket offsets.                                                                                                                      |
| `granularity`                        | `auto` (default), `hour`, `day`, `week`, `month`. Auto chooses hours through 48 hours, days through 90 days, otherwise weeks.                                                                                |
| `compare`                            | Include the immediately preceding period of equal **elapsed duration**, with identical filters. Default `false`.                                                                                             |
| `model_id`, `key_id`                 | Exact historical IDs.                                                                                                                                                                                        |
| `status`                             | `success`, `error`, `canceled`.                                                                                                                                                                              |
| `protocol`                           | `openai_chat`, `openai_responses`.                                                                                                                                                                                     |
| `stream`                             | Exact streaming flag.                                                                                                                                                                                        |
| `user_id`, `project_id`              | Administrative endpoint only; mutually exclusive. `user_id` selects only Personal attribution.                                                                                                               |
| `provider_model_id`, `connection_id` | Administrative endpoint only; exact historical route IDs.                                                                                                                                                    |

Weeks start on Monday. Days and months follow the chosen calendar, including daylight-saving transitions; hourly buckets advance by elapsed hours from the first local hour boundary. Repeated DST hours have distinct offsets. Empty buckets are included. The first and last buckets may extend beyond the requested range, but their facts are always clipped to the exact range. Comparison ranges and buckets are returned explicitly; comparison does not silently substitute a previous calendar month.

## Response and completeness

The response contains `current`, optional `previous`, `timezone`, resolved `granularity`, `available_dimensions`, and freshness metadata. Each period has `from`, `to`, `summary`, `trend`, `models`, and `keys`. Administrative results also have `provider_models` and `connections` when nonempty.

Each summary, bucket, and distribution group reports:

- `requests`, `successes`, `errors`, and `canceled` from canonical request facts. Attempt retries are not additional calls.
- `success_rate` as a fraction from 0 to 1 and `average_duration_ms` across all selected calls; both are `null` when no calls exist. Duration is the gateway's recorded request duration, not provider time-to-first-token.
- `tokens.input`, `tokens.output`, and `tokens.total`, each shaped as `{ "value": "12", "known": "12", "unknown_calls": 0 }`. Decimal integer **strings** preserve values above JavaScript's safe integer range. `value` is `null` if any call lacks a required counter; `known` still sums all present counters. Total coverage requires both input and output per call. Empty sets have known zero. Reported counters are not a guarantee of final provider accounting; interrupted streams may contain partial counters. Cache counters are not added again to input/output totals.
- `amounts`, a sorted array of `{ "currency": "USD", "amount": "0.3", "calls": 2 }`. Each sum uses exact decimal arithmetic over captured `priced` receipts. Zero-priced calls remain known zero. Distinct currencies are never combined or converted using current FX. An empty array means no known amount, not a zero bill.
- `unknown_amount_calls` and `pricing_statuses` describe charge coverage independently of token coverage and HTTP success.

Groups retain historical IDs even when the live resource is renamed or deleted. Model labels use the latest selected fact's model-name snapshot, breaking equal timestamps by request ID. Missing IDs form an explicit `unknown: true` group. Key rows use historical IDs without a mutable name lookup. Rankings order by the known token subtotal, then request count, then ID; unknown usage is not silently estimated. Clients can choose another display metric from each group's complete statistics.

`source` is `persisted_call_records` and `may_lag` is `true`: pending journal entries and in-flight calls are absent until persistence. `queried_at` is the query start timestamp. `latest_completed_at` is the greatest completion timestamp **among the selected facts**, or `null`; it is not a global ingestion watermark and does not expose other principals' activity. These reports are not the authoritative source for admission decisions.

## Bounded complete reads

Each requested period must be positive and no longer than 366 elapsed days. Each period is limited to 1,000 buckets and each distribution to 500 distinct IDs. The combined current/comparison selection is limited to 10,000 facts. The query reads at most 10,001 rows to detect overflow and excludes receipt JSON and attempt bodies; aggregation is portable Go code over a bounded selection. Database authorization and selection share a five-second timeout. Invalid ranges return `400`; row, bucket, or dimension overflow returns `422` with no partial report. Narrow the range or filters and retry. Query cancellation/deadline failure returns a generic `503`.

The bilingual Personal, Project and platform interfaces provide filters, summary cards, trends, model/Key distributions and rankings using these reports. Exact values and unknown coverage remain visible. This slice intentionally has no Team attribution, provider totals, CSV export, forecast or background rollup. Existing facts have connection and provider-model snapshots but no immutable provider ID. Joining the current catalog to manufacture historical provider totals would change old attribution, so that dimension requires a later schema extension. Large installations will need indexed rollups or another explicitly complete aggregation path instead of increasing these synchronous bounds without evaluation.

## Verification

Pure tests cover exact decimal/token sums, mixed currencies, free and unknown charges, partial token counters, zero-filled buckets, historical labels, scope-safe filters, strict query parsing, DST hour/day boundaries, leap months, equal-duration comparison, and cardinality/range rejection. `testUsageLifecycle` is part of the coordinated PostgreSQL/MySQL harness: it exercises canonical replay, Personal/Project/platform isolation, creator-versus-manager history access, live membership/account revocation, guessed Key/model filters, archived Project history, route redaction, and complete-versus-overflow boundaries including comparison rows.

Protocol filtering also accepts `anthropic_messages` for native Messages calls.
