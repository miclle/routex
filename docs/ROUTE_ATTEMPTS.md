# Native attempt planning

`pkg/routeattempt` is a tested planning and execution boundary for future bounded gateway failover. It is not yet wired into the gateway. Active protocol handlers continue to make one upstream attempt; the package alone does not enable retries, persistent health tracking, multi-attempt pricing, or additional quota reservations.

## Immutable request plan

`New(protocol, targets, Options{MaxAttempts, Draw})` copies target and credential data. A plan may be reused concurrently without sharing request progress. A target contains an opaque stable target ID, Connection ID, exact native protocol, positive routing weight (or zero for an inactive candidate), and credential IDs with priorities. Secrets, provider URLs, request bodies, and native response content never enter this package.

Each plan represents exactly one native protocol. Mixed protocols, duplicate target IDs, duplicate credential IDs within one target, and reuse of a credential ID under a different Connection are rejected. The same credential may appear under multiple targets of the same Connection. A credential rejection excludes that credential across those targets for the remaining request. IDs cannot contain NUL or line breaks and are limited to 128 bytes.

Limits are 128 targets, 32 credentials per target, and one to four executed attempts per request. Zero-weight targets never receive requests. Credential count does not influence target weight. Eligible targets retain their relative positive weights; each selected target uses its lowest-priority-number eligible credential. Ties use credential ID order. The default weighted draw uses cryptographic randomness; tests inject a deterministic bounded draw. Draw calls on a shared plan are serialized.

## Execution hooks

`Plan.Run(ctx, Hooks)` returns detached ordered attempt results, whether durable admission succeeded, and an explicit stop reason. It never retains provider error text. Hook errors become fixed package errors; context cancellation retains the context error. Returned records contain opaque internal credential IDs for execution correlation only: gateway logs and persisted call facts must continue to omit credential IDs and secrets.

Hooks run in this order:

1. `Eligible` reads current candidate health and authorization. False excludes that credential from this selection; errors stop the request.
2. `Prepare` authoritatively rechecks current caller/model/credential authorization, route and egress revisions, and any changed price/reservation bound. It runs before every attempt. It must issue dispatch permission at the caller's atomic policy boundary; the planner itself owns no authorization lock or database transaction.
3. `Admit` runs once after the first successful preparation and reserves the single durable request record. Retries must extend or amend this same reservation through the preparation boundary, rather than admitting another request.
4. `Execute` invokes at most one native upstream request, then returns explicit safety evidence. Entering this callback consumes one attempt slot. Preparation or admission success alone does not create an attempt or increment attempt metering. A connection attempt that never sends the request is still an executed attempt.

Cancellation is checked between hooks and before every execution. An admitted request can therefore return zero executed attempts; the caller must still finalize its durable reservation. Revocation during selection must be caught by the authoritative preparation boundary. A revoked authorization or failed budget amendment stops execution instead of selecting another route to bypass it. Caller-owned per-attempt dispatch tickets or lock boundaries must make this guarantee concrete when integrating the module.

The caller owns native payload construction, guarded transport selection, attempt IDs/timestamps, response cleanup, price snapshots, and final settlement. The planner never parses HTTP status or retries inside an individual execution callback. It performs no backoff sleeps and never switches protocols or models.

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

Use one canonical request ID and one durable admission for the whole run, plus unique internal IDs for executed attempts. Finalize the request once, including cancellation and hook failures after admission. Every next target requires an appropriate immutable price basis and a reservation bound before execution; a preparation failure cannot release an already consumed or uncertain prior reservation. Preserve prior attempt outcomes without treating them as separate successful calls or aggregating unknown usage as zero.

The package deliberately blocks retry after uncertain execution or final usage. This avoids pretending that a different target or credential makes a possibly charged request safe to replay. A future integration must prove journal restart deduplication, price-bound updates, candidate/current-policy checks, response cleanup, native protocol error classification, and final call/attempt persistence before enabling gateway retries.

## Focused validation

```bash
go test -race -count=1 ./pkg/routeattempt
```

Tests cover exact deterministic weight intervals, credential priority independent of route weights, health-based renormalization, credential versus Connection exclusions, output/finality/unknown-work safety, one durable admission across retries, preparation failures without fabricated attempts, cancellation after admission and execution, current revocation before the next attempt, explicit attempt exhaustion, malformed randomness, immutable snapshots/results, and concurrent independent runs. These pure tests do not replace gateway, quota, or dual-database acceptance.
