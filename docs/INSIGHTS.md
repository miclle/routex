# Operations analysis

Operations analysis at `/admin/insights` provides deterministic, read-only
Tokens-by-Model and selected-range Provider quality reports. The page retains the question/result composition,
report actions and saved-report area. It requires current `calls.read_all`
authority; administrator status and unrelated catalogue permissions do not replace
that permission or grant inference access.

Select **Tokens by model**, enter absolute start and end timestamps with explicit
UTC offsets, review the time zone, and run the report. The range includes the start
and excludes the end. UTC is the initial grouping time zone; no calendar week is
silently selected. Free-text analysis, Team usage and Key forecasts are explicitly unsupported.
The separate **Provider quality** shortcut is described below. Forecasts remain unavailable. The deterministic
report makes no inference call. Optional AI advice requires a separate explicit
confirmation and stores no report or conversation.

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
Duration coverage is explicit; P95 uses known full-attempt durations, not first-token
latency. A Provider name is the latest selected immutable recorded label (attempt ID
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
facts are never sent to the optional AI endpoint, which remains Tokens-by-Model only.
This report does not add a forecast, alert threshold or routing policy.

Focused source tests cover attribution, exclusions, range precision, complete bounds,
CSV safety, strict wire parsing, authority loss and bilingual rendering. Real-driver
qualification of the extended existing Provider-quality scenario and browser acceptance
must be recorded separately; implementation alone does not establish them.

## Creator-private saved definitions

The user-confirmed retention policy is creator-private visibility until explicit
owner deletion. A definition contains only `tokens_by_model` or `provider_quality`,
exact absolute `from`/`to` instants and an IANA `timezone`. Titles are localized
from the kind. No statistics, historical model labels, AI answers, inference model
selection, free-text question or result snapshot is persisted.

The implementation in progress uses Session-authenticated
`/api/v1/insights/reports` endpoints for bounded owner listing and explicit creation
and deletion. The server derives the owner from the current enabled Session;
administrator privileges never confer another creator's definitions. Saving also
requires the existing current report-kind permissions: `calls.read_all`, plus
`providers.read` for Provider quality. Owner listing and deletion read no aggregate
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

This is a specification for the isolated work in progress, not delivered or tested
behavior. Formatting, unit/UI tests, mandatory checks, real PostgreSQL/MySQL
migration and ownership tests, and browser acceptance must be recorded separately.

## Optional personal-model AI advice

The separate model picker reads `GET /api/v1/admin/insights/models`. It lists only
current-user callable personal Chat models; `calls.read_all` does not grant model
access. No Key is entered or borrowed from another user, Team or Project. Review
the selected model, absolute dates/time zone, requested answer language and personal
quota/cost disclosure before confirming one explicit analysis request. The page
never starts or retries paid inference automatically.

`POST /api/v1/admin/insights/analyze` accepts only `report: tokens_by_model`,
`model_id`, `from`, `to`, `timezone` and `language: en | zh`, with the current
Session's CSRF token. The server captures the authorized aggregate report again;
this is not the displayed report or CSV snapshot. Only recorded aggregate counts,
coverage, capture metadata and temporary labels such as `M1` are sent upstream.
Raw calls, member identities, historical model names/IDs, credentials, questions
and history are excluded. The selected inference model still uses its ordinary
personal authorization, metering and quota path.

The response contains `request_id`, the exact native `completion`, nullable
`text`, independent `capture` metadata and a temporary historical Model-label map.
Only native `completed` may carry answer text. `unknown`, `handoff`, `blocked`
and `incomplete` remain distinct and display no invented answer. The answer is
untrusted plain text, with no HTML/Markdown execution, links, tools, query or
configuration actions. It does not establish forecasts or native completion for
the historical calls being analyzed. A completed analysis does not prove a saved
report, browser download, deployment or policy change.

The strict wire boundary limits models and labels to 500, input JSON to 2 KiB,
aggregate projection to 64 KiB, answer text to 16 KiB UTF-8 and the complete response
to 1 MiB; no truncation is accepted. Overflow requires a narrower reviewed query.
The answer, capture and temporary labels remain in component state only, outside
query/mutation caches and browser storage. Model/range changes, renewed or failed
Session/permission/model reads, actor changes and unmount hide prior answers,
revoke prepared confirmation, abort obsolete work and reject late replies. Each
submit reads current CSRF. Cancellation does not prove dispatched inference or
charges were undone. Requested answer language is captured with the confirmed
request; switching UI language does not replay or translate an existing answer.

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
temporary Model labels only. No free-text questions, conversation history, raw
calls, borrowed Key/Team/Project authority or persisted answer is added. Browser
and real external-model acceptance remain deferred. Creator-private saved definitions are now a separately specified implementation
package. Forecasts and broader evaluation remain unimplemented scope.
