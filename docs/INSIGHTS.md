# Operations analysis

Operations analysis at `/admin/insights` provides deterministic, read-only
Tokens-by-Model, selected-range Provider quality and scoped Team/Key quota reports.
The page retains the question/result composition, report actions and saved-report
area. It requires current `calls.read_all`
authority; administrator status and unrelated catalogue permissions do not replace
that permission or grant inference access.

Select **Tokens by model**, enter absolute start and end timestamps with explicit
UTC offsets, review the time zone, and run the report. The range includes the start
and excludes the end. UTC is the initial grouping time zone; no calendar week is
silently selected. The separate **Provider quality** shortcut and scoped quota
reports are described below. Only the quota reports provide covered settled-rate
estimates. Quota saved definitions are implemented in V111. The question extension
accepts explicitly confirmed caller text for Tokens-by-Model or Provider-quality AI
advice; it never plans a query from prose.
Deterministic reports make no inference call. Optional AI advice requires a
separate explicit confirmation and stores no report or conversation.

The existing `GET /api/v1/admin/usage` endpoint supplies persisted call facts.
Results expose the captured source, filters, range, grouping, query time, latest
completed record and possible lag. Recorded input/output counts retain their
integer strings and historical model labels. Known subtotals exclude unknown
coverage; unknown coverage is never zero. Model shares use the known subtotal,
not routing weights, effective quotas or a forecast. HTTP success and recorded
usage do not establish native completion or retirement eligibility. No live
catalogue lookup is needed to label historical models.

The existing complete-query bounds remain in force: at most 366 elapsed days,
10,000 selected facts including comparison data, 500 groups per dimension and
1,000 buckets, with a five-second database capture deadline. An overflow requires
a narrower reviewed query; the page does not silently truncate a complete report.
Session or permission renewal/error, actor or parameter changes, and stale replies
hide prior private results and invalidate obsolete work. Report and export dispatch
each require fresh authority.

**Export CSV** uses `GET /api/v1/admin/usage/export.csv` for a separate fresh
authorized capture with the existing safe CSV encoding and 8 MiB response bound.
Its metadata may differ from the displayed report. Preparing a download does not
prove the browser saved a file. Exported data and report inputs are not stored in
browser storage. The creator-private saved-definition workflow is specified below; its current
implementation and verification status are tracked in the handoff.

## Selected-range Provider quality

**Explain Provider quality** uses the same reviewed dates and result area. It requires
both current `calls.read_all` and `providers.read`; neither permission implies the other.
`GET /api/v1/admin/insights/provider-quality` accepts only `from`, `to` and `timezone`.
The server selects immutable upstream attempts whose recorded end is in the exact
inclusive-start/exclusive-end range. Returned bounds represent the actual query
instants in UTC, including nanoseconds, with separate IANA time-zone metadata.
Stored attempt timestamps use the historical microsecond grid. Fractional review
bounds use portable strict/inclusive comparisons at that grid so driver truncation
cannot incorrectly include or exclude a boundary record. At most 366 elapsed
days, 100,000 attempts and 500 exact Provider groups are accepted within a five-second
capture/aggregation deadline. Overflow fails the complete query rather than truncating.

Counts are attempts, not logical calls: failover may contribute to multiple Providers.
Canceled and not-sent attempts are excluded; Credential rejection is separate. Eligible
success rates use attributed attempts only and do not prove native completion.
Duration coverage is explicit. The delivered mean-duration projection provides
`average_duration_ms` from eligible recorded nonnegative full-attempt samples,
weighted by known sample counts across Providers. Unknown samples are excluded;
recorded zero remains zero and no known sample returns null. Finite float64 means
may exceed JavaScript safe integers because duration is a measurement, not an
exact counter. P95 remains a separate nearest-rank full-attempt statistic. Neither
measurement represents first-token latency. The projection is delivered in
`3fa148c` with focused, ordinary and dual-driver qualification.
A Provider name is the latest selected immutable recorded label (attempt ID
breaks an equal-time tie); it does not come from the current catalog. Attempts with
no Provider ID remain a separate global unknown count, outside every group and rate.
The final logical Call's Provider never fills missing attempt attribution. Persisted
records may lag; a successful response does not prove the live recorder is drained.

Export uses `GET /api/v1/admin/insights/provider-quality/export.csv` for a separate
fresh server capture under both permissions, with the existing formula-safe CSV
encoder, private/no-store headers and 8 MiB transport limit. It does not serialize
cached browser facts. Summary and Provider rows identify their scope; unknown
attribution remains explicit in capture metadata. Report, export and late replies
share the existing actor/Session/permission and parameter lifetime fences. Provider
identities and raw attempts are never sent to the upstream model. Explicitly
confirmed Provider-quality advice uses a separate fixed anonymous aggregate capture.
This report does not add a forecast, alert threshold or routing policy.
Operational observations reuse the existing result composition to explain
recorded rate-limited and server-error eligible attempts, unknown attribution
and unknown duration coverage. They derive only from the captured report,
remain separate from AI advice and create no alert or inferred anomaly.
Complete/empty captures remain distinct from unavailable evidence. Focused
frontend qualification passed 78 tests in five files; composed primary
formatting and mandatory checking also passed. Browser acceptance remains open.

Focused source tests cover attribution, exclusions, range precision, complete bounds,
CSV safety, strict wire parsing, authority loss and bilingual rendering. The
composed implementation passed complete PostgreSQL/MySQL and authentication
qualification before main delivery; see [Implementation](IMPLEMENTATION.md) for
source and receipt provenance. Browser and real-provider acceptance remain open.

## Creator-private saved definitions

The user-confirmed retention policy is creator-private visibility until explicit
owner deletion. Historical `tokens_by_model` and `provider_quality` definitions
retain exact absolute `from`/`to` instants and an IANA `timezone`. V111 adds `team_quota` with a Tokens/Money dimension and `key_quota`
with that dimension plus an exact authorized member/Project scope. Quota
definitions contain no historical date range, pagination cursor, resource name,
currency snapshot or result rows. Titles are localized from the kind. No statistics, historical model labels, AI answers, inference model
selection, free-text question or result snapshot is persisted.

The delivered implementation uses Session-authenticated
`/api/v1/insights/reports` endpoints for bounded owner listing and explicit creation
and deletion. The server derives the owner from the current enabled Session;
administrator privileges never confer another creator's definitions. Saving also
requires the existing current report-kind permissions: `calls.read_all`, plus
`providers.read` for Provider quality. Quota creation requires the matching
current resource authority without enumerating Keys or reading accounting. The
Team definition represents the actor's dynamic readable-Team query, not a global
Team grant. Loading never executes the query or restores former authority; an
explicit Run freshly authorizes the saved scope. Owner listing and deletion read no aggregate
statistics. Writes use CSRF and strict typed input. Stable creation intent supports
explicit identical retries without accepting a changed body or resurrecting a
deleted definition. Deletion removes the saved parameter payload; a minimal intent
receipt may remain solely to reject obsolete creation replay.

The existing Save action and saved-report table retain their composition. Loading
a definition only fills parameters for review, clears prior results/advice and
requires a separate explicit run through the existing currently authorized report
endpoint. It does not restore old access or make a paid inference call. Delete
requires local confirmation. Private lists and asynchronous write completions
remain fenced by the current actor, Session and authority. Definitions and write
intentions are never placed in browser storage.

Cancel/Close and local save-intent discard synchronously revoke queued callbacks.
Each save/delete dispatch generation is consumed once: an obsolete callback
cannot delete a canceled target, turn a discarded retry into a fresh save, or
repeat a request after its first outcome. A current explicit retry retains the
original request and current authority.

The 2026-10-23 controlled PostgreSQL/MySQL in-app-browser qualification records
native201/200/201 saves with first/retry browser503 replies. Real list refresh
never resolves the historical unknown intent. Identical explicit retry keeps the
UUID and definition ID; discard then Save creates a new definition. Cancel/Close
produce no DELETE. SQL, exact HTTP bodies and complete owned-resource closure
are independently checked. This covers Tokens-by-Model saved receipts; queued
callback races remain maintained-test evidence, and wider report/viewport and
external-model acceptance remain separate. See the current implementation index
for exact run IDs and retained evidence.

Creator-private definitions are delivered in `4ee3909` after focused tests and
mandatory checking. The composed implementation also passed complete ordinary,
unfiltered PostgreSQL/MySQL and authentication qualification. Those original controlled
results do not establish browser or real external-service acceptance; the newer
bounded browser qualification above has its own recorded scope.

## Optional personal-model AI advice

The separate model picker reads `GET /api/v1/admin/insights/models`. It lists only
current-user callable personal Chat models; `calls.read_all` does not grant model
access. No Key is entered or borrowed from another user, Team or Project. Review
the selected model, absolute dates/time zone, requested answer language and personal
quota/cost disclosure before confirming one explicit analysis request. The page
never starts or retries paid inference automatically.

For selected-range advice, `POST /api/v1/admin/insights/analyze` accepts only
`report: tokens_by_model | provider_quality`,
`model_id`, `from`, `to`, `timezone` and `language: en | zh`, plus an optional
explicitly reviewed `question`, with the current
Session's CSRF token. The server captures the authorized aggregate report again;
this is not the displayed report or CSV snapshot. Server-derived data sent upstream
contains only recorded aggregate counts, coverage, capture metadata and temporary
labels such as `M1` for Models or `P1` for Providers. Provider advice requires
independent current `calls.read_all` and `providers.read` before capture,
native dispatch and private answer publication. It uses the existing immutable
Provider-quality attempt capture, with unknown attribution outside the attributed
summary, excluded and Credential-rejected attempts outside eligibility, and
known/unknown full-attempt duration coverage. Rate-limited and server-error
counts may overlap. Mean and P95 are not first-token latency; recorded success
does not prove live health, fleet coverage or native completion.
Raw calls/attempts, member identities, historical Provider/Model names/IDs, credentials and history
are excluded from the server-added projection. When selected, the exact reviewed
question is separately sent as untrusted text beside those aggregates. Caller
prose may itself contain private information; it is never claimed to contain only
aggregate data. The UTF-8 question is bounded to 1,024 bytes, is nonblank and has
no control characters except newline/tab. It cannot choose SQL, native roles,
tools, report kinds, extra data sources or broader authorization. The same fixed
report is freshly captured; the selected model uses its ordinary personal
authorization, metering and quota path.

The response contains `request_id`, the exact native `completion`, nullable
`text`, independent `capture` metadata and a temporary historical identity map.
Tokens advice retains its `persisted_call_records` capture and `model_id` labels;
Provider advice uses `persisted_call_attempts` and `provider_id` labels. Cross-kind
sources and label shapes fail closed at the client wire boundary. The identity
map remains private to the authorized caller and is excluded from upstream data.
Only native `completed` may carry answer text. `unknown`, `handoff`, `blocked`
and `incomplete` remain distinct and display no invented answer. The answer is
untrusted plain text, with no HTML/Markdown execution, links, tools, query or
configuration actions. It does not establish forecasts or native completion for
the historical calls being analyzed. A completed analysis does not prove a saved
report, browser download, deployment or policy change.

The strict wire boundary limits models and labels to 500, input JSON to 2 KiB,
aggregate projection to 64 KiB, answer text to 16 KiB UTF-8 and the complete response
to 1 MiB; no truncation is accepted. Overflow requires a narrower reviewed query.
The question, review, answer, capture and temporary labels remain in component
state only, outside query/mutation caches and browser storage. Question/model/range/report-kind
changes, renewed or failed Session/permission/model reads, actor changes and
unmount hide prior answers,
revoke prepared confirmation, abort obsolete work and reject late replies. Each
submit reads current CSRF. Cancellation does not prove dispatched inference or
charges were undone. Requested answer language is captured with the confirmed
request; switching UI language does not replay or translate an existing answer.
Saved-definition loading
only prepares the selected controls and clears previous reviews/results; it
never invokes a model or restores a prior answer.

## Current-page Team quota advice

The existing assistant can review the selected Team Tokens or Money page after an explicit
report run. Its separate strict request selects `team_quota_tokens` or
`team_quota_money`, canonical
personal `model_id`, `language`, reviewed `cursor` (empty for the first page),
`limit` (1–50), and optional exact `question`. Range, currency, dimension and browser statistics
fields are rejected; the fixed report kind owns the dimension and the server owns the currency. The server consumes the actor-bound cursor and captures only that
page again; it neither automatically pages nor produces global totals.

Only anonymous `T1` labels, stored own caps/behavior, settled amounts, separate
reservations, unknown coverage and already supplied estimates enter the aggregate
projection. Team names, IDs, memberships and cursor remain outside it. Nullable
facts remain unknown. Existing date estimates retain their trailing 168-hour
settled-admission basis and monthly reset boundary; the assistant adds no new
threshold calculation, prediction, parent allowance or enforcement claim.
`quota_capture` replaces range `capture` in this response, with source
`local_quota_journal`, exact cursor/limit, observation/calendar metadata and
`has_more`; a transient private Team label map stays with the authorized caller.

Fresh Session, report read and personal-model authority are checked independently
of each captured Team's current scoped read and exact birth before dispatch and
answer publication. Late revocation discards the private answer while preserving
actual native usage and settlement. Page, dimension, refresh, permission, model
or Session changes revoke confirmation and obsolete answers. No automatic paid
inference, policy write or saved answer is added. This phase's authored tests require separate qualification;
source changes alone establish no browser or real-driver acceptance.

## Earlier isolated source checks (historical)

The earlier deterministic foundation passed 46 focused cases in four files.
The subsequent AI draft now passes formatting, TypeScript, scoped ESLint and
102 focused frontend cases in six files, including strict wire parsing, explicit
confirmation, native states, authority changes, cancellation and stale replies.
Nineteen focused backend tests (51 named nodes) also pass with the development
build tag and race detection. An earlier untagged handler invocation could not
compile because this isolated draft had no generated production assets; it is
retained as a failed invocation, not acceptance. These checks bind the current
isolated draft only; qualification must be renewed on the phase's rebased source.
Full checks, real-database, native model/runtime and browser acceptance remain
pending. No new implementation is delivered by these draft checks. Saved-definition
audience and retention were undecided at that historical checkpoint.

## Earlier isolated qualification checkpoint (historical, 2026-10-12)

The rebased isolated candidate passes mandatory checking and the complete Task,
including Go race/coverage, 6,933 frontend cases in 258 files, Node lifecycle
checks and production assets. Its 2,548-path source floor is
`a8a3d91aa905bcba40004b00844f72c5d6a2ca9ee0eab9abdb02b4599dc86612`;
complete-gate receipt SHA-256 is
`6489aa362c2a8c77f5fb95518eb0c040a1f38aab8773a0388adda45ffe4acdfd`.
The first real-driver focus passes thirteen PostgreSQL names but fails MySQL
migration preparation before the selected leaves; cleanup is independently
verified. This is not dual-driver acceptance. Delivery awaits fresh qualification
of the coherent latest-main authentication/operations candidate. Browser, real
external-model and saved-definition acceptance remain separate.

## Coherent delivery boundary

The containing delivery includes the deterministic authorized Tokens-by-Model
report, a separate fresh CSV capture and explicit optional personal Chat advice.
Actual coherent source, dual-driver and authentication qualification and the
canonical source/receipt provenance are indexed in
[Implementation](IMPLEMENTATION.md). Earlier isolated results above retain their
original source and failures; they are not transferred to the delivered source.

Analysis remains limited to the explicitly selected current user's callable
personal Chat model and personal quota, with bounded authorized aggregates and
temporary Model labels. The question extension adds explicitly reviewed
transient caller text to that unchanged aggregate scope, not query planning or
conversation history. Question typing/submission prepares review only; Confirm
starts one fresh request and cancellation never automatically retries it. No raw
calls, borrowed Key/Team/Project authority or persisted question/answer is added.
The mean duration is delivered in `3fa148c` and captured observations in
`adf3363`. Question qualification passed focused Go race tests, mandatory checking,
and complete ordinary tests including 7,274 frontend cases in 270 files.
PostgreSQL and MySQL each passed all 25 selected analysis/constraint names, with
unchanged source and owned cleanup. Primary composed mandatory checking also passed before commit. Quota definitions are delivered in `d9af644`. Earlier delivered
receipts remain unchanged. Browser
and real external-model acceptance remain deferred. Creator-private definitions
and scoped quota forecasts are delivered in `4ee3909` and `bb542e9`, with coherent
mandatory, ordinary, dual-database and authentication qualification recorded in
the implementation index. Broader evaluation remains unimplemented scope.

## Scoped Team and Key quota analysis

The existing operations-analysis layout includes Team and Key quota shortcuts.
The page and endpoints retain `calls.read_all`; each server
read also checks current Team authority or the explicitly selected member or
Project Key authority. No platform-wide Key directory or new permission is
introduced. Lists are bounded to 20 accounts by default and 50 at most; Key
rotation successors share one deduplicated root account. Cards and exports
describe the selected page rather than implying complete platform totals.

`GET /api/v1/admin/insights/quota/teams` and
`GET /api/v1/admin/insights/quota/keys` use the selected Tokens or Money dimension.
Key reads require an explicit member or Project scope. Their `/export.csv`
counterparts independently reauthorize and produce a fresh coherent server
batch. All result reads preserve actor, Session, permission and target freshness.

Percentages concern an account's own stored monthly cap, separately from parent
constraints and stop/alert-only behavior. Explicit zero, no own cap, unknown
usage, incomplete coverage and unavailable accounting remain distinct. Money
uses exact decimal strings and matching currency; reservations and unknown
amounts are shown separately from settled usage.

Exhaustion estimates use settled usage for requests admitted in the preceding
168 hours at a constant rate. Both the complete journal coverage and the current
account incarnation must cover that window. Unknown usage, incomplete coverage
or a zero rate yields no forecast. Estimates stop at the account's calendar
monthly reset; they do not predict parent constraints, future workload or actual
request blocking. Pending reservations are excluded from the rate. Retained terminal reservations
represent incomplete actual usage even when an unbounded-unknown counter is zero;
they suppress utilization percentages and forecasts while settled subtotals remain
visible. Active pending reservations and retained terminal reservations are
disclosed separately.

The existing reading-guidance card also presents deterministic observations from
the captured account page: near/critical levels, reached caps separated by their
stored Stop or Alert only behavior, server-confirmed seven-day estimates, and
unavailable, incomplete or unknown forecast evidence. An empty capture is stated
explicitly. Observations update when the page changes and never accumulate into
global totals. They add no thresholds, forecast calculations, inference calls or
policy writes, and establish neither actual request blocking nor parent allowance.

Quota identities and uncaptured data never enter optional AI advice. After explicit
review, Team and Key Token or Money advice may use only the captured page's anonymous
quota facts; the report dimension and Money denomination remain explicitly bound.
Quota definitions extend the same owner-private Save, Load and confirmed Delete
workflow in `d9af644`; frozen V111 preserves released V110 receipts and adds
disjoint parameter constraints. The complete unfiltered PostgreSQL and MySQL
matrix each passed all 583 names without failures or skips; nonmatrix Go passed
10,235 names with seven explicit skips. Independent readback accepted unchanged
source and owned cleanup. Earlier failed matrices remain failed. Browser and
external-service acceptance remain separate, and the complete goal stays active.

## Provider-quality AI phase acceptance (2026-10-14)

The composed Provider-quality advice phase passed focused Go race tests and 203
frontend cases, mandatory checking, and complete ordinary qualification with
7,303 frontend cases in 270 files. Selected PostgreSQL and MySQL qualification
each passed 29 names; nonmatrix Go passed 10,319 names with seven explicit skips.
Independent source/cleanup readback accepted unchanged source and owned resource
closure, SHA-256
`a8b17bbd834518ec55131fdec8a80922fc17ecb81281cb163001ec74e407484e`.
This is selected controlled acceptance, not an unfiltered matrix, browser proof
or external-model acceptance. A follow-up held-response exact personal-Model-grant
revocation leaf passed 30 named tests on each PostgreSQL and MySQL driver with no
failures or skips, preserving one dispatch and settlement while denying the revoked
private answer.

## Current-page Key quota advice

The fixed `key_quota_tokens` or `key_quota_money` analysis accepts canonical personal `model_id`,
`language`, one explicitly reviewed `scope_kind` (`member` or `project`), canonical
`scope_id`, `cursor`, `limit` (1–50), and optional exact `question`. Range fields,
browser statistics, currency/dimension fields and mixed report grammars are rejected. The server repeats the existing authorized
quota query in the report kind's fixed dimension; it does not accept browser statistics, widen the scope or fetch other
pages. Rotation descendants share their recorded root account. Only anonymous K
labels, existing own-cap/behavior/settled/held/unknown facts and supplied estimates
enter the closed upstream projection. No new forecast or threshold math is added.

Key and owner IDs, names and cursor remain in the transient private capture and
label map in `key_quota_capture` and `key_id` labels. Member roots use `key_`
identities and Project roots use `pky_`; a different scope prefix is rejected.
Exact owner/root birth and current scope read are checked independently
from report read, current Session and personal-model authority before dispatch and
answer publication. Late revocation suppresses the answer while actual native
attempts and personal quota settlement remain recorded. Scope, target, page or
authority changes invalidate the review and answer.

This source slice and its authored tests are not runtime, browser, external-model
or delivery acceptance. It introduces no persistence, schema or policy writes.

## Current-page Money advice

The Money variants reuse the same current-page review and personal-model confirmation,
existing quota-journal query, immutable attempt attribution and personal settlement.
They add no global enumeration, SQL, schema, persistence, policy writes or forecast math.
The server derives Money from `team_quota_money` or `key_quota_money`; the browser cannot
supply statistics or a denomination. Token requests and their closed response capture
retain their existing wire shape.

Every Money amount remains an exact decimal string, including very large caps and
18-digit fractional precision. Unknown request counts remain integer strings. The
closed anonymous upstream projection adds the server's report `currency` and, when an
own cap exists, its separately recorded `local_cap_currency`. The stored-cap currency
is captured privately with the policy rather than inferred from the current report.
A cap whose stored denomination differs from the report remains unavailable with
unknown utilization and no comparable settled amounts, reservations or estimate;
there is no conversion, relabeling or summation across currencies. Zero, no own cap,
unknown usage, coverage gaps and Stop versus Alert only remain distinct.

Money `quota_capture` and `key_quota_capture` add `dimension: money` and `currency`.
The client compares the returned denomination with the reviewed local page before
publishing an answer; its local currency review argument is never sent in the request.
The existing dialog and answer disclose the denomination in English and Chinese.
Page, dimension, currency, scope, target, Session, permission, model and actor generations
continue to revoke confirmation, abort obsolete calls and suppress late answers.
Private identities, cursors and exact owner/root/Team birth proofs remain outside
upstream aggregates; current authority is checked again before dispatch and publication.

Regression tests cover exact anonymous amounts, mismatched stored currencies,
closed request/response grammars, local currency review, explicit confirmation and stale
Money replies. Real-driver scenarios additionally exercise Team/member/Project scopes,
root-account deduplication, personal quota denial, immutable native metering and late
Team/owner/root birth changes. Focused qualification passed 66 service and 11 handler
race tests, 263 frontend tests, and 35 tests on each real PostgreSQL/MySQL database.
The first mandatory check rejected frontend report-union narrowing and malformed
test-input casts. A successor fixes those types without changing the qualified backend;
formatting, `go tool task check` and complete ordinary tests pass on that successor,
including all 7,734 frontend tests and production assets. The first ordinary run
hit two unrelated dialog-workflow test deadlines; unchanged focused reproduction
(43 tests) and complete ordinary retry pass with original timeouts/configuration.
The initial deadline cause remains unproven. Browser and external model acceptance
remain separate from these controlled tests.
