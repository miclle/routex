# Usage queries

Usage reports aggregate immutable, deduplicated `call_records`. They provide request counts, reported token coverage, historical charges, time trends, model distributions, and Key rankings for Personal and Project principals. Team reports provide shared model/trend/currency aggregates without Key or contributor identities. They do not read live quota counters or recalculate historical receipts.

## Endpoints and authority

| Endpoint                                 | Access and attribution                                                                                                                                              |
| ---------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET /api/v1/usage`                      | Current active account; only its Personal facts (`user_id` matches exactly and both `project_id` and `team_id` are empty), including revoked or rotated Keys.                                   |
| `GET /api/v1/projects/:project_id/usage` | Current Project manager or `calls.read_all`; only that Project's facts. The creator has no implicit access. Disabled and archived Project history remains readable. |
| `GET /api/v1/teams/:team_id/usage` | Exact current enabled active-Team membership; all canonical facts attributed to that Team, without contributor identities. |
| `GET /api/v1/admin/usage`                | Current `calls.read_all` authority; installation-wide facts, optionally narrowed by principal or route.                                                             |

A repeatable-read transaction checks current account/permissions/manager membership and selects facts from the same database snapshot. A membership change affects subsequent queries. Personal and Project results expose model and Key IDs; [Team results](TEAM_USAGE.md) expose model-only dimensions and an exact `team_id` echo, with empty Key groups; provider, provider-model, and connection dimensions are restricted to the administrative endpoint. Guessed filters never widen the caller's scope. Historical identity selection uses the database-layer exact-text adapter on both supported drivers. Case aliases never select a canonical identity; malformed trailing whitespace returns `400`. Project resource aliases return `404` even for managers or administrators. Archived history still requires the exact enabled actor and current manager or platform authority.


## Recorded Model caller coverage

Personal, Team, Project and platform reports add
`member_count_basis: "distinct_recorded_actors"`. Every Model group in `current`
and, when requested, `previous` contains `members` with exactly `value`, `known`
and `unknown_calls`. `known` counts distinct safe recorded `CallRecord.UserID`
strings in that Model and half-open window. IDs compare exactly, without
trimming, case folding or joining a current directory. A safe historical ID has
an exact `usr_` prefix, a nonempty ASCII letter/digit/underscore/hyphen suffix and
at most 30 bytes. Missing or invalid recorded IDs contribute one unattributed
call per canonical request. Differently cased valid suffixes remain distinct;
an aliased prefix is unproven.

`value` equals `known` only when `unknown_calls` is zero; otherwise it is null.
The nonnegative values satisfy `known + unknown_calls <= stats.requests`.
Success, error and canceled facts participate equally; canonical RequestID
replay never adds another caller. Removed directory entries, membership changes,
rejoins, Key ownership and current manager authority do not rewrite recorded
attribution. Unknown Model groups retain coverage without inventing a Model or
actor identity. This measures recorded callers, not current members, grantees or
successful native users.

The existing authorization and read-only repeatable-read transaction select the
same scoped facts; the one bounded fact SELECT includes only one additional
internal `user_id` field. No additional query, directory join, permission,
endpoint, schema or quota behavior is added. Raw actor IDs and sets remain
private. Only Model groups carry `members`: summaries, trends, Keys, Providers,
Provider Models and Connections do not. Existing amounts, token coverage,
freshness and all scoped CSV columns/bytes remain unchanged.

A complete marked report supports zero for an absent selected Model. Legacy
unmarked, incomplete, rejected or overflow reports do not establish a total.
Catalogue rendering preserves an Unknown primary value when attribution is
incomplete and shows known distinct callers and unattributed calls separately.
The catalogue explicitly queries UTC month-to-query in one selected Personal or
shared Team account; these boundaries do not derive quota-calendar facts.

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
| `protocol`                           | `openai_chat`, `openai_responses`, `anthropic_messages`, `gemini_generate_content`.                                                                                                                        |
| `stream`                             | Exact streaming flag.                                                                                                                                                                                        |
| `user_id`, `project_id`              | Administrative endpoint only; mutually exclusive. `user_id` selects only Personal attribution.                                                                                                               |
| `provider_id`, `provider_model_id`, `connection_id` | Administrative endpoint only; exact historical route IDs captured from an attempted route.                                                                                                      |

Weeks start on Monday. Days and months follow the chosen calendar, including daylight-saving transitions; hourly buckets advance by elapsed hours from the first local hour boundary. Repeated DST hours have distinct offsets. Empty buckets are included. The first and last buckets may extend beyond the requested range, but their facts are always clipped to the exact range. Comparison ranges and buckets are returned explicitly; comparison does not silently substitute a previous calendar month.

## Response and completeness

The response contains `current`, optional `previous`, `timezone`, resolved `granularity`, `available_dimensions`, and freshness metadata. Each period has `from`, `to`, `summary`, `trend`, `models`, and `keys`. Administrative results also have `providers`, `provider_models`, and `connections` when nonempty.

Each summary, bucket, and distribution group reports:

- `requests`, `successes`, `errors`, and `canceled` from canonical request facts. Attempt retries are not additional calls.
- `success_rate` as a fraction from 0 to 1 and `average_duration_ms` across all selected calls; both are `null` when no calls exist. Duration is the gateway's recorded request duration, not provider time-to-first-token.
- `tokens.input`, `tokens.output`, and `tokens.total`, each shaped as `{ "value": "12", "known": "12", "unknown_calls": 0 }`. Decimal integer **strings** preserve values above JavaScript's safe integer range. `value` is `null` if any call lacks a required counter; `known` still sums all present counters. Total coverage requires both input and output per call. Empty sets have known zero. Reported counters are not a guarantee of final provider accounting; interrupted streams may contain partial counters. Cache counters are not added again to input/output totals.
- `amounts`, a sorted array of `{ "currency": "USD", "amount": "0.3", "calls": 2 }`. Each sum uses exact decimal arithmetic over captured `priced` receipts. Zero-priced calls remain known zero. Distinct currencies are never combined or converted using current FX. An empty array means no known amount, not a zero bill.
- `unknown_amount_calls` and `pricing_statuses` describe charge coverage independently of token coverage and HTTP success.

Groups retain historical IDs even when the live resource is renamed or deleted. Model, provider, provider-model, and connection labels use the latest selected fact's immutable name snapshot, breaking equal timestamps by request ID. A stable legacy route ID without a captured label is rendered as its ID. Missing IDs form an explicit `unknown: true` group. Key rows use historical IDs without a mutable name lookup. Rankings order by the known token subtotal, then request count, then ID; unknown usage is not silently estimated. Clients can choose another display metric from each group's complete statistics.

Provider topology is recorded only after an upstream attempt enters execution. Admission failures and cancellations before the first attempt retain empty topology, while retries and failover record the final attempted route. Version 26 adds the immutable provider and route-name snapshots with frozen GORM schema types and a provider/time index. Existing rows remain explicit unknowns; reports never join mutable catalog rows to manufacture historical attribution.

`source` is `persisted_call_records` and `may_lag` is `true`: pending journal entries and in-flight calls are absent until persistence. `queried_at` is the query start timestamp. `latest_completed_at` is the greatest completion timestamp **among the selected facts**, or `null`; it is not a global ingestion watermark and does not expose other principals' activity. These reports are not the authoritative source for admission decisions.

## Durable freshness and historical pricing verification

The genuine-native lifecycle passed on both supported databases under race
detection (Handler65.282s). Five real controlled Chat completions per driver
cover a blocked in-flight old price/FX snapshot, a new generation, explicit zero,
terminal unknown counters and nonzero usage with free rates. Monetary holds
retain the existing denomination-change guard. Queued/in-flight facts remain
absent from JSON/CSV; the empty peer retains null selected completion. Actual
commit-before-ack replay, late arrivals and two Service restarts preserve complete
fixed-range current/previous projections, exact historical currencies, records
and attempts without another upstream dispatch. Source, selected completion and
unknown coverage remain independent of ingestion completeness. This local proof
does not establish throughput, persistence-delay percentiles or fleet recovery.

## Bounded complete reads

Each requested period must be positive and no longer than 366 elapsed days. Each period is limited to 1,000 buckets and each distribution to 500 distinct IDs. The combined current/comparison selection is limited to 10,000 facts. The query reads at most 10,001 rows to detect overflow and excludes receipt JSON and attempt bodies; aggregation is portable Go code over a bounded selection. Database authorization and selection share a five-second timeout. Invalid ranges return `400`; row, bucket, or dimension overflow returns `422` with no partial report. Narrow the range or filters and retry. Query cancellation/deadline failure returns a generic `503`.

The bilingual Personal, Project and platform interfaces provide filters, summary cards, trends, model/Key distributions and rankings using these reports. The platform interface additionally provides a Provider ID filter and provider, provider-model, and connection distributions with immutable historical labels. Personal and Project responses omit those topology dimensions. Exact values and unknown coverage remain visible. Team Session facts retain immutable Team/member attribution and are excluded from Personal reports. Platform reports include their recorded usage and prices without fabricating a Key ranking. The existing usage page defaults to Personal and offers named own active Teams; Team mode suppresses Key controls and stale private results during refresh or authorization errors. Platform reports support an exact historical `team_id` filter under independent `calls.read_all`, omitting Key dimensions for that filter while preserving authorized provider diagnostics. Team filters cannot combine Personal-user, Project or Key attribution. Scoped CSV export is described below; forecasts and background rollups remain open. Large installations will need indexed rollups or another explicitly complete aggregation path instead of increasing these synchronous bounds without evaluation.

## Verification

Pure tests cover exact decimal/token sums, mixed currencies, free and unknown charges, partial token counters, zero-filled buckets, immutable historical labels, scope-safe filters, strict query parsing, DST hour/day boundaries, leap months, equal-duration comparison, and cardinality/range rejection. `testUsageLifecycle` is part of the coordinated PostgreSQL/MySQL harness: it exercises canonical replay, Personal/Project/platform isolation, creator-versus-manager history access, live membership/account revocation, guessed Key/model/provider filters, archived Project history, route redaction, rename-stable provider history, and complete-versus-overflow boundaries including comparison rows. The migration lifecycle covers empty creation, existing-data upgrade, partial-DDL recovery, repeat execution, concurrent startup, field constraints, and provider-index restoration on both supported databases.


## Exact-identity verification checkpoint

The current CSV/identity source passed complete format/check/test/build with
1774 frontend cases in 99 files. The actual PostgreSQL/MySQL Usage/Team/CSV/
identity/Home regression passed under race detection (Handler118.128s). Its
identity fixture covers canonical positives and case/trailing aliases for all
eight historical selectors, exact Personal attribution, foreign history,
manager/administrator Project paths and archived history. Seeded immutable facts
prove query/projection behavior; genuine native pricing/delivery/replay acceptance
is tracked separately.


## Team report verification checkpoint

The Team workflow focus passed on actual PostgreSQL/MySQL (Handler 215.926
seconds), including exact identity/scope, unknown counters, decimal currencies,
canonical replay, independent connection reopen, member revocation, historical
contributors, platform history and bounded complete-read overflow. Final main
check and test passed with 990 Vitest cases in 69 files, Go race/unit, development
lifecycle and embedded production assets. The final complete matrix passed (Handler 745.019 seconds; Service 6.692
seconds), including the final platform Team-dimension correction.

A disposable production binary recorded one native Team call from each of two
actors. Both see two requests and ten known Tokens in the Team report; the member
Personal report remains empty and actor-only Team history contains one call.
Removing membership and refreshing the real English/Chinese interface hides the
old aggregate and denies access. Rejoining restores the same immutable aggregate;
an independent process restart preserves it. The platform Team filter displays
the same totals with authorized provider groups and no fabricated Key controls.
Owned browser/process/config/journal/Compose resources were removed. This evidence
does not establish measured capacity, external providers or complete release
acceptance; F22 remains partial.

## Member monthly account Overview

The self-only [member Overview](MEMBER_OVERVIEW.md) reads current monthly quota
accounts separately from immutable usage-report aggregates. It preserves Personal,
Team aggregate and the current member's stable Team/User account as distinct
scopes. Live reservations and monthly settled/retained amounts are separate,
coherent journal facts. Its links open the existing Personal or exact Team report;
it never turns a report into remaining allowance or runtime enforcement proof.

The server-owned `30d` preset resolves once to the preceding 720 elapsed hours
with a half-open upper bound. Timezone determines calendar buckets, including
partial edges and DST, without changing that elapsed duration. Existing explicit
ranges, comparison and complete-query bounds remain unchanged. The Personal
member Home uses this preset with UTC daily buckets and no comparison; see
[Member Overview](MEMBER_OVERVIEW.md).

## Scoped CSV export

Implementation candidate, 2026-10-04. Frozen source is carried onto checked
Team-notice main. Current-main source/build, actual PostgreSQL/MySQL,
production/browser download and complete regression acceptance remain gates;
source-only checks are not a delivered file workflow.

The existing filter row adds its final Export CSV action. It sends the last
applied filters, independently of unsaved filter drafts, to one fresh
server-owned report. The capture can differ from the screen as durable facts
arrive. Language changes update guidance without replacing filter inputs.

| Scope | Authenticated GET route | Download filename |
| --- | --- | --- |
| Personal | `/usage/export.csv` | `routex-personal-usage.csv` |
| Project | `/projects/:project_id/usage/export.csv` | `routex-project-usage.csv` |
| Team | `/teams/:team_id/usage/export.csv` | `routex-team-usage.csv` |
| Platform | `/admin/usage/export.csv` | `routex-platform-usage.csv` |

All routes are under `/api/v1` and reuse the corresponding JSON report's exact
query parser, current authorization, bounds and historical data. There is one
report capture, no second planner, mutable catalogue join, CSV pagination or
inference operation. Personal and Project export Model/Key groups; Team exports
Model only. Platform exports only its independently authorized dimensions and
omits Key groups when Team attribution is selected. Historical attribution does
not grant current Project/Team access.

UTF-8 schema `routex_usage_v1` is rectangular: one metadata row, current and
optional previous periods, summary/trend/group statistics, and separate amount
and pricing-status children. Metadata records applied selectors, resolved range,
timezone/granularity, own query time, source, lag and available dimensions.
Children copy the parent locator and leave statistical fact cells blank. Do not
sum summary, trend and group projections together. Money retains its recorded
currency without conversion; known Token subtotals and unknown coverage remain
separate. Nulls are blank; exact zero is present.

Text encoding `apostrophe_text_v1` adds exactly one ASCII apostrophe to present
Token/money strings, identifiers, timestamps and dynamic text, including values
already beginning with an apostrophe. Decode by removing exactly one prefix.
CSV quoting preserves original whitespace, commas, quotes and newlines. Plain
CSV consumers see the prefix; import these columns as text for precision.
This is reversible formula protection, not a promise of automatic spreadsheet
typing or numeric precision.

The complete file is encoded before download headers under the report's
five-second total deadline and an 8 MiB byte bound. Success is HTTP200 with
`text/csv; charset=utf-8`, private/no-store, nosniff and the fixed scoped filename.
Overflow returns JSON422 and expired/canceled work JSON503 without a partial
file or download disposition. Existing query/capture errors retain their status.

Export holds a synchronous duplicate lock and transient abort controller.
Actor, resource, source, applied filters, report authority or successful network
Session generation changes invalidate the captured intent; late replies cannot
prepare a file. Manual same-actor CSRF cache replacement does not renew Session
authority. Export401/403/404 hides prior private facts until explicit successful
refresh; other errors do not diagnose revocation. Blob URLs and downloaded bytes
never enter query caches or browser storage and are promptly released. A browser
"download prepared" notice proves initiation only; actual browser-saved bytes
and separate authenticated HTTP captures require independent evidence.

The filter owner retains the applied request and a separate unsaved draft across
same-actor Session and permission reads. This includes raw invalid input so
renewal does not silently replace a draft with the applied request. Reports and
exports remain generation-scoped and disappear during renewed authority reads;
pending downloads abort and late callbacks are discarded. Actor or exact source
changes reset the filter owner. Filter drafts never enter browser storage or
query caches.
