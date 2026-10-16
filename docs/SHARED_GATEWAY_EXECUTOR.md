# Dormant shared Gateway executor integration

This source adds the actual Gateway lifecycle connection to the existing shared
SQL authority, coordinator and exclusive durable spool. It is deliberately not
selected by startup or configuration. The executable still starts its local
recorder. Economic/origin migrations remain unregistered; no new deployment,
local-history conversion, shared activation or multi-process acceptance is
claimed. This document describes source preparation, not passing execution.

## Construction and exclusion boundary

`newSharedGatewayRuntime` is private. It requires an already open shared spool,
the original owner registration receipt and the exact current local system
process identity. A bounded READ COMMITTED transaction verifies the current SQL
binding, shared mode/revision, owner fence, registration ID/time, process birth,
and the recorded economic origin/calendar. The spool's installation, mode
revision and slot must match. Local recorder/admission dependencies exclude this
construction. No exported setter, test mode, environment switch or HTTP route
installs the result.

Construction does not establish deployment exclusion or authenticate previously
unproved historical provenance. Fresh initialization still needs actual control
of previous executors/startups and database credentials. An old executable that
ignores the mode barrier must be externally stopped. Original-local transfer,
registered frozen economic/origin schemas, a reviewed startup integration and
real two-process qualification remain prerequisites for activating this branch.

The eventual startup owner must retain the exclusive journal, stop admission,
drain HTTP and flush delivery before closing that file. `StopCallRecorder`
closes shared admission when the private dependency is present; settlement and
explicit fact delivery remain possible after that barrier. No shared background
worker is installed here.

## One transient handle per logical request

After final route preparation and before attachment rewriting, the Gateway
captures its complete original reservation from the parsed native input and
prepared plan. It binds the selected Model ID to the authenticated request-local
Key or Session identity, captures current SQL authorization, and checks the full
current policy/calendar/route set. The original payload, credential plaintext
and canonical client IP stay transient; only existing sanitized manifests,
authorization coordinates and call facts enter durable storage.

Each `GatewayResult` owns one private execution handle. A copied result or a
request ID alone cannot borrow that executor. The handle retains the original
coordinator and live SQL authority. Every account in the existing principal
shape remains conjunctive: User/root Key, Project/root Key, Team/member, or the
single Personal Session User. There is no per-account local fallback.

Attachment preflight uses the same complete SQL evaluator in a bounded
transaction that is always rolled back before a positive return. It may perform
speculative account creation and lazy expiry inside that transaction, but
commits no account, receipt, RPM, executor, quota hold, expiry or clock change.
It grants no permit. The final coordinator admission repeats current checks and
performs the one atomic SQL reservation before dispatch.

The actual HTTP request runs inside the coordinator's once-only dispatch
callback after exact spool and SQL acknowledgements. The callback reacquires
existing immediate connection, source-holder and request/route guards. A failed
or uncertain dispatch acknowledgement cannot replay transport. Failed-attempt
checkpointing uses the same request-owned coordinator before another native
attempt is allowed. Changed bodies never replace original uncertain intent.

## Body lifetime and terminal facts

Chat Completions, Responses, Messages and GenerateContent keep their common
completion path. Their LIFO defers close the response before calling
`FinalizeGatewayCall` with the exact result and sanitized final fact. Shared
bodies use an idempotent close wrapper; all attempted bodies are retained and
must close successfully before logical executor closure can be asserted.
Response headers, HTTP success, native completion, deadline expiry and owner
fencing do not substitute for that closure. Direct service callers also use the
same finalizer; returning an HTTP response does not settle its economics.

Finalization preserves independent nullable Token/money settlement and existing
native completion evidence. Unknown economics keep conservative holds even
when the exact logical executor closes. Historical settlement remains available
after admission stops or current authorization changes. Its detached operation
is finite, retains the original terminal body and never repeats inference.
An uncertain admission cannot be converted into an unadmitted success or local
ledger. Requests rejected before any shared admission attempt, or rejected by a
typed quota/rate/capacity decision before a shared receipt exists, may retain
their ordinary Call fact without inventing an economic receipt. Storage,
transaction, and acknowledgement uncertainty remains fail-closed and is not
recorded through the local ledger. A definitive pre-receipt rejection first
conditionally releases its untouched spool reservation; a failed release is
uncertain and remains pending for recovery.

Only an exact acknowledged SQL terminal makes the spool fact ready. Explicit
`FlushCallRecorder` delivers those facts using idempotent `RecordCall` and
acknowledges the matching spool receipt afterward. Delivery failure leaves the
original fact durable. Request-ID-only `PersistGatewayCall`, generic admission,
local recorder startup, and runtime-less Gateway fallback refuse an installed
shared dependency instead of silently selecting local enforcement.

## Authored checks and remaining qualification

Focused tests cover close-before-settle ordering, canceled callers, unknown
holds, close failures, copied results, changed identities, lost terminal replies,
identical retries, shutdown settlement and foreign process/spool rejection.
The existing opt-in Accept SQL worker adds complete-account preflight rollback
checks for all four principal shapes and a controlled actual Gateway dispatch,
body closure, shared settlement and Call/spool delivery case. Its original
isolated database ownership gate is unchanged. Its manually seeded origin is a
test fixture, never proof of deployment exclusion or Fresh initialization.

Compilation and execution are intentionally pending while another qualification
owns the exclusive heavy lane. Required follow-up: focused race tests, both
existing dedicated real-driver Accept workers, `go tool task check`, complete
ordinary tests, full PostgreSQL/MySQL integration and applicable restart tests.
These focused cases do not prove external provider, native parser, four-protocol
end-to-end, process crash/fleet cutover or whole F17 acceptance.
