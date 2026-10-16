# Shared SQL admission and dispatch

The follow-on [dormant Gateway executor integration](SHARED_GATEWAY_EXECUTOR.md)
connects the real request lifecycle in source without a startup selector or
shared activation. Its execution qualification remains pending. The original
foundation scope and historical verification limits below remain separate.

## Candidate scope

The isolated candidate adds `Accept` and `MarkDispatch` to the dormant SQL
economic store. Historical reads, checkpoints and settlement remain independent
of current authorization. This source is not selected by the Gateway and does
not establish cutover, historical coverage or multi-process runtime acceptance.

A live authority retains the original detached admission request, the current
process's Gateway plan and result, and a separately captured canonical client IP.
The IP, source holders and plaintext credentials are never serialized into the
admission intent. Recovered history cannot construct these process-local inputs.
Construction recaptures the manifest from the actual parsed native payload,
prepared plan/result and pricing data, requiring an exact match. Self-consistent
supplied metadata alone cannot replace that original capture. The transient
authority also binds the exact execution key after a successful `Accept` response;
reading an existing receipt does not establish that local acknowledgement.

The complete original request includes the reservation manifest and process
birth. Its canonical bytes must fit the existing 128 KiB spool limit. The
manifest's independent capture limit does not increase the admission limit.

## Transitions

Both operations hold the existing egress-then-limit process read locks and use
one READ COMMITTED SQL transaction. Current checks take governance, installation,
mode, owner/process, authorization, policies/calendar and route locks before
economic domain, request and sorted account locks. Exact current revocation and
policy checks remain separate from process-local snapshot, source-holder, egress
and route-health checks. Runtime checks run before SQL and again before commit.

Admission expires bounded accounting projections, checks existing rate and quota
rules, and atomically inserts the immutable receipt, RPM/executor/active quota
contributions and domain count/clock. Identical original admissions return their
retained receipt without consuming capacity again. Changed bodies or execution
identities conflict. Full receipt capacity and failed checks roll back the whole
transaction, including any projection expiry or newly created accounts.

Dispatch selects only an exact route from the original reservation, preserves
the logical request's original maximum hold, verifies retained active holds and
executor contributions, and refuses invalidated original bound revisions. It
requires an uninterrupted checkpoint prefix before a new attempt and stores at
most four immutable dispatch intents. Terminal history cannot authorize another
attempt. An identical dispatch acknowledgement is retained evidence, not a
reusable execution permit; the coordinator forbids transport replay after an
uncertain dispatch result.

The actual transport still requires the existing connection/source dispatch
guards immediately before work. A successful SQL operation alone does not prove
upstream execution, native completion, final usage or executor closure.

## Validation boundary

Authored focused tests cover exact original-route selection, immutable maximum
holds, changed snapshots and missing live authority. Compilation, whole-transition
PostgreSQL/MySQL tests, rollback and competing-writer tests remain required.
No runtime activation or completed feature is claimed by source preparation.
