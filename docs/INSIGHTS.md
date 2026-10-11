# Operations analysis

Operations analysis at `/admin/insights` provides a deterministic, read-only
recorded-Tokens-by-Model report. The page retains the question/result composition,
report actions and saved-report area. It requires current `calls.read_all`
authority; administrator status and unrelated catalogue permissions do not replace
that permission or grant inference access.

Select **Tokens by model**, enter absolute start and end timestamps with explicit
UTC offsets, review the time zone, and run the report. The range includes the start
and excludes the end. UTC is the initial grouping time zone; no calendar week is
silently selected. Free-text analysis and the other suggested questions are
explicitly unsupported. Forecasts and saved report definitions remain unavailable. The deterministic
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
browser storage. **Save report** and the saved-report table explain that persistence
is unavailable.

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
audience and retention still require a separate product decision.

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
and real external-model acceptance remain deferred. Saved-definition audience and
retention, forecasts and broader evaluation remain separate unimplemented scope.
