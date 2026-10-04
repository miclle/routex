# Native attempt planning

`pkg/routeattempt` is the tested planning and execution boundary for bounded native gateway failover. The published runtime now uses it for Chat Completions, Responses, Messages, and Gemini. Direct database routing remains a single-attempt compatibility path for tests and bootstrap conditions where no runtime is active.

## Immutable request plan

`New(protocol, targets, Options{MaxAttempts, Draw})` copies target and credential data. A plan may be reused concurrently without sharing request progress. A target contains an opaque stable target ID, Connection ID, exact native protocol, positive routing weight (or zero for an inactive candidate), and credential IDs with priorities. Secrets, provider URLs, request bodies, and native response content never enter this package.

Each plan represents exactly one native protocol. Mixed protocols, duplicate target IDs, duplicate credential IDs within one target, and reuse of a credential ID under a different Connection are rejected. The same credential may appear under multiple targets of the same Connection. A credential rejection excludes that credential across those targets for the remaining request. IDs cannot contain NUL or line breaks and are limited to 128 bytes.

Limits are 128 targets, 32 credentials per target, and one to four executed attempts per request. Zero-weight targets never receive requests. Credential count does not influence target weight. Eligible targets retain their relative positive weights; each selected target uses its lowest-priority-number eligible credential. Ties use credential ID order. The default weighted draw uses cryptographic randomness; tests inject a deterministic bounded draw. Draw calls on a shared plan are serialized.

## Execution hooks

`Plan.Run(ctx, Hooks)` returns detached ordered attempt results, whether durable admission succeeded, and an explicit stop reason. It never retains provider error text. Hook errors become fixed package errors; context cancellation retains the context error. Returned records contain opaque internal credential IDs for execution correlation only: public call DTOs and logs omit secrets and private credential IDs. Internal immutable attempt facts preserve the exact Credential and published snapshot IDs separately from the logical call's final route.

Hooks run in this order:

1. `Eligible` reads current candidate health and authorization. False excludes that credential from this selection; errors stop the request.
2. `Prepare` authoritatively rechecks current caller/model/credential authorization, route and egress revisions, and any changed price/reservation bound. It runs before every attempt. It must issue dispatch permission at the caller's atomic policy boundary; the planner itself owns no authorization lock or database transaction.
3. `Admit` runs once after the first successful preparation and reserves the single durable request record. Retries must extend or amend this same reservation through the preparation boundary, rather than admitting another request.
4. `Execute` invokes at most one native upstream request, then returns explicit safety evidence. Entering this callback consumes one attempt slot. Preparation or admission success alone does not create an attempt or increment attempt metering. A connection attempt that never sends the request is still an executed attempt.

Cancellation is checked between hooks and before every execution. An admitted request can therefore return zero executed attempts; the caller must still finalize its durable reservation. Revocation during selection must be caught by the authoritative preparation boundary. A revoked authorization or failed budget amendment stops execution instead of selecting another route to bypass it. Caller-owned per-attempt dispatch tickets or lock boundaries must make this guarantee concrete when integrating the module.

The caller owns native payload construction, guarded transport selection, attempt IDs/timestamps, response cleanup, price snapshots, and final settlement. The planner never parses HTTP status or retries inside an individual execution callback. It performs no backoff sleeps and never switches protocols or public models.

## Active gateway policy

One logical request may execute at most four attempts. The first candidate is selected from currently eligible positive-weight routes; credential priority is evaluated inside the selected route. RouteX retries only when it has positive replay-safety evidence:

- a guarded transport failure proves that application request bytes were not sent;
- a protocol-native authentication envelope proves credential rejection without upstream work; or
- a protocol-native rate-limit envelope proves rejection without upstream work.

Bare or malformed `401`/`429` responses, `403`, `408`, `5xx`, target TLS failures, response resets, timeouts after a possible write, invalid successful responses, stream/parser/finality failures, cancellation, emitted output, final usage, and every unknown-work outcome stop replay. A valid `2xx` response is returned to the protocol handler exactly once. Any later body or stream failure is final for routing purposes.

Connection failures with proven `not_sent` evidence place that Connection in a bounded process-local cooldown. Explicit native authentication rejection places only that credential in a separate cooldown. A native rate rejection affects the current request's route selection without creating a persistent health penalty. Successful response admission clears prior cooldown evidence for that Connection and credential. These cooldowns are routing hints within one process, not durable provider-health history.

Before reading attachment storage or dispatching an attempt, the gateway filters candidates through current authorization, runtime snapshot, egress revision, capability, health, and quota evidence. Token and monetary policies reserve the maximum supported capacity and exact decimal price across all retained candidates. The request receives one durable admission, one RPM/concurrency debit, one quota hold, and one final settlement regardless of attempt count.

The durable journal stores a zero-work recovery settlement atomically with admission, then checkpoints the ordered evidence before each dispatch. Entering an active attempt clears the pre-dispatch zero assumption. A restart therefore recovers exact zero economics before dispatch or after only proven work-free failures, while active and ambiguous attempts remain unknown. The interruption fact contains completed attempts plus any active attempt, without exposing private credential IDs or arbitrary provider response text in public/member responses. Schema version 25 stores the route stop reason and normalized attempt number, failure class, work evidence, output/finality flags, and allowlisted evidence code. Administrators can inspect this evidence in the existing call drawer; member call APIs remain redacted.

## Strict native rejection evidence

A non-success response authorizes another attempt only when its bounded native
error envelope has one unambiguous interpretation. Root and error positions must
be JSON objects with valid UTF-8, no trailing value and no duplicate decoded
members, including equivalent escaped names. Relevant case aliases and disagreeing
reserved error discriminators keep work unknown. A present Gemini numeric code
must be a canonical integer matching the HTTP 401 or 429 status; an absent code
remains compatible. Benign diagnostic fields remain supported.

Any native success/work marker in a root error envelope blocks replay, even if its
value is null, empty, zero, false or malformed. This does not fabricate known usage,
emitted output, charge, delivery or native completion. The classifier retains
unknown work and the planner stops with unsafe_to_replay.

| Native protocol | Root positions that conflict with error-only rejection |
| --- | --- |
| Chat | choices, usage, id, object, created, model |
| Responses | response, output, output_text, usage, status, incomplete_details, id, object, model, created_at, completed_at; response.* event types block replay |
| Messages | content, usage, stop_reason, stop_sequence, id, model, role; a present type must be error |
| Gemini | candidates, usageMetadata, promptFeedback, modelVersion, responseId |

Inspect only these structural positions and reserved rejection discriminators.
Do not search opaque message, tool or nested diagnostic text for usage/output
words or recursively treat diagnostic member names as work evidence. The existing
1 MiB read bound, native public-error sanitization, one admission/settlement,
current authorization, cancellation and native success parsers remain unchanged.
No new policy, schema, permission or frontend control is introduced.

## Evidence required for replay

An outcome identifies failure scope and one of these work states:

| Work state | Meaning |
| --- | --- |
| `not_sent` | The caller can prove the upstream request was never sent |
| `rejected_without_work` | The native protocol provides an explicit rejection that establishes no work was performed |
| `unknown` | Execution or charging may have occurred |
| `completed` | Upstream work or charge is known to have completed |

A timeout, connection reset after dispatch, absent usage, HTTP 429/5xx, or lack of response output is not proof of no work. Provider-specific classifiers must supply stronger evidence; ambiguity stops replay. A known final usage envelope or any emitted caller output always blocks replay, even if other fields claim a retryable rejection. Do not label a provider response credential-specific without native evidence.

For a proven credential rejection, the next eligible credential on the same target is preferred. When that pool is exhausted, the remaining eligible targets may be selected by weight. For a connection failure or explicit work-free rate rejection, the entire Connection is excluded; walking its other credentials would mask a connection-level fault. Each target/credential pair executes at most once in a run. Exclusions are request-local; persistent health ejection/recovery is a separate integration concern.

The stop reasons distinguish success, unsafe replay, permanent failure, exhausted attempt budget, no candidates, cancellation, and a blocked hook. A safe failure on the last permitted attempt returns `attempt_budget_exhausted` without invoking another preparation/admission callback.

## Metering integration requirements

Use one canonical request ID and one durable admission for the whole run, plus unique internal IDs for executed attempts. Finalize the request once, including cancellation and hook failures after admission. Every next target requires an appropriate immutable price basis and a reservation bound before execution; a preparation failure cannot release an already consumed or uncertain prior reservation. Preserve prior attempt outcomes without treating them as separate successful calls. Only an admitted call with durable proof that every attempt was work-free may record explicit zero tokens and the `no_work` pricing state; unknown usage must remain unknown.

The package deliberately blocks retry after uncertain execution or final usage. This avoids pretending that a different target or credential makes a possibly charged request safe to replay. Controlled tests prove journal restart deduplication, aggregate price/capacity bounds, current candidate checks, failed-response cleanup, native classifiers, and ordered attempt persistence. Real-provider behavior and multi-node health coordination remain separate acceptance gates.

## Focused validation

Internal attempt attribution now copies each actual dispatch's Credential and
published configuration snapshot into failed, final and interrupted facts. A
later attempt never substitutes its IDs into earlier history, and legacy missing
attribution stays unknown rather than borrowing the logical call's final snapshot.
See [Call facts](CALLS.md#internal-attempt-attribution)
for the V32 persistence and acceptance boundary. These identifiers alone do not
prove native completion or authorize planned Credential retirement.

```bash
go test -race -count=1 ./pkg/routeattempt ./pkg/eventqueue ./pkg/upstream ./internal/routex/service
```

Tests cover exact deterministic weight intervals, credential priority independent of route weights, health-based renormalization, credential versus Connection exclusions, output/finality/unknown-work safety, one durable admission across retries, preparation failures without fabricated attempts, cancellation after admission and execution, current revocation before the next attempt, explicit attempt exhaustion, malformed randomness, immutable snapshots/results, and concurrent independent runs. Gateway tests cover strict versus ambiguous native errors, pre-request connection proof, single-admission crash recovery, aggregate quota bounds, and cooldowns. PostgreSQL/MySQL integration covers the additive migration and ordered diagnostic persistence. These tests do not replace real-provider, capacity, or multi-node acceptance.

### Rejection-evidence acceptance checkpoint, 2026-10-04

Source RED reproduced a second execution after contradictory native work or
duplicate rejection fields across four protocols. Revision 2 GREEN passed 12
focused race tests and its dual-driver focus passed 52 POSTs/33 calls per driver.
Independent inspection then exposed a Responses native event/payload gap.
Revision 3 RED reproduced it; GREEN passed 14 focused race tests, source-only
handler compile, full service/handler staticcheck and pinned lint. Only the
Responses contradiction body changed in the genuine fixture; all 52/33
assertions remain intact. The independent helper preserves its 25/17 counts.
At that checkpoint, final-main source/driver/process/browser gates were pending.
The following checkpoint records their results; revision 2 evidence remains
historical and does not establish the additional edge case.
Paid-provider semantics, measured capacity and multi-node health remain open.

### Final revision 3 actual acceptance checkpoint, 2026-10-04

Final main source format/check/test/build passed with 1908 frontend cases/104
files and exact dependency bytes. The final TZ=UTC PostgreSQL/MySQL focus
passed Handler85.316s, including native failover and repository lifecycle/schema
regressions. Each driver verified 52 actual upstream POSTs/33 logical calls: four
native protocols across Personal/Project/Team authentication failover, Team rate
failover, contradictory and duplicated native errors, accepted truncated streams,
three between-attempt revocations and bounded four-attempt exhaustion. One
controlled call-delivery outage recovered genuine journal facts without another
upstream dispatch. Finite holds settled exactly once; internal attempts retained
exact Credential, Connection, Provider-model, publication and scope identities.

An independent final production process retained 25 POSTs/17 logical calls across
four Team authentication failovers, four Project rate failovers, eight Personal
unknown-work failures and accounting warmup. Actual Team removal/rejoin and
same-binary restart preserved immutable calls/ordered attempts without replay.
Manual English/Chinese admin diagnostics showed ordered safe versus one-attempt
unknown evidence. Personal (8), Project (4) and Team (4) member views were scope-separated and
redacted; revoked and unrelated actors had no private resource actions. Existing
browser authentication and fresh four-model Team discovery worked after restart.
No browser inference was dispatched. Owned helper/tab/Compose resources were
closed and checked absent. Complete final-source PostgreSQL/MySQL race regression
passed (Handler 1320.407s, Service 7.798s), with source and dependency bytes
unchanged and exact owned resources absent. These controlled checks do not close
external-provider, capacity or fleet acceptance.
