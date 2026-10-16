# Admission authority boundary

The follow-on [dormant Gateway executor integration](SHARED_GATEWAY_EXECUTOR.md)
connects the real request lifecycle in source without a startup selector or
shared activation. Its execution qualification remains pending. The original
foundation scope and historical verification limits below remain separate.

**Source candidate — qualification pending.** This foundation separates the
existing local enforcement authority from call-fact delivery. It changes no
schema, deployment mode, quota algorithm or supported node count. Multi-node
admission remains unfinished.

`Service.admission` is explicitly configured during recorder startup, after the
installation binding and calendar have been validated. Its internal
`admissionStore` interface owns synchronous admission, attempt checkpoints,
settlement, original receipts, calendar activation, coverage, account birth
proofs, usage windows and live monetary holds. Gateway preflight and final
admission use this authority, as do resource-limit reads, currency guards,
Overview and quota analysis, and quota notification workers.

The local adapter delegates each operation to the existing bbolt transaction.
It preserves atomic conjunctive account checks, RPM and concurrency, exact
decimal money, independent Token and monetary settlement, nullable unknowns,
retained conservative holds, calendar freezing and original account births.
Monthly alert-only behavior still applies independently to each eligible account
and dimension; it cannot bypass hard parent/child items, rolling limits, missing
coverage, unknown accounting, prices or currency checks.

The interface carries a context for authority operations. Existing local journal
operations remain synchronous and cannot be canceled midway through their
commit. Native dispatch paths pass their request context; historical internal
context-free admission/checkpoint entry points retain their compatibility
behavior. No retry automatically repeats an upstream call.

Those compatibility entry points currently supply background contexts. This
foundation makes no remote timeout or bounded distributed-I/O claim. A shared
implementation must establish caller-derived or finite operation contexts at
every admission, checkpoint, settlement and fencing seam before activation.

The recorder separately owns the local file lifecycle, bounded spool, delivery
reads and acknowledgements. In this local implementation, Reserve, UpdatePending
and Complete payload operations still commit the call fallback, checkpoint or
final fact atomically with economic state in the same journal. A rejected call
with no admission can retain its final fact through the recorder's existing
spool-only path. Acknowledging delivery never clears economic history. The
adapter does not close the recorder's file.

A future shared implementation cannot satisfy this interface merely by saving
counters and omitting those payload writes. It must explicitly coordinate local
spool reservation, checkpoints and terminal evidence with shared economic
transactions, preserving the durable pre-dispatch barrier and every uncertain
crash seam. Local Queue calls cannot be a fallback economic authority.

A missing configured authority is never inferred from an available spool.
Gateway operations fail closed; optional usage projections retain their existing
unknown/unavailable representation. A failed authority operation cannot select
the spool as an economic fallback. Tests must explicitly bind the local adapter
to their journal rather than rely on a production fallback.

## Remaining shared admission work

The next implementation must add a real portable GORM authority with shared
settled and reserved accounting, conjunctive windows/rates/concurrency, exact
account identities and policy generations, idempotent receipts/settlement and
bounded crash reconciliation. Keep per-node call spools, but establish the
durable dispatch barrier between their local commits and the shared transaction;
uncertain commits must never authorize dispatch or redispatch. Lost process
heartbeats alone cannot prove upstream work stopped or release economic holds.

Activation requires an installation-persisted mode and no local fallback when
shared authority is unavailable. Existing local history needs an explicitly
verified quiescent transfer of coverage, calendar, original receipts, account
births, settled values, holds, unknowns, RPM and invalid bounds. Asynchronous Call
reports cannot seed authoritative history. Preserve released migration steps and
allocate a new frozen GORM schema only after its actual dependencies are delivered.

Qualification must include real PostgreSQL/MySQL migrations and two gateway
processes with independent spools. Exercise last-allowance contention, every
account/window and independent alert-only item, exact money, trusted IP and
conjunctive restrictions, streaming/cancellation, missing terminal usage,
duplicate reports, uncertain dispatch/settlement, partitions, lagging policy
publication, restart and transfer recovery. This source foundation does not
establish those results or complete F17, A11 or release acceptance.

See [resource limits](RESOURCE_LIMITS.md), [quota accounting](QUOTAS.md) and the
[existing local ledger](QUOTA_LEDGER.md) for the current enforcement contracts.
