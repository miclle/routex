# Shared admission economic schema

## Current boundary

The V119 source candidate defines seven frozen, dormant economic tables. It is
not registered in the migration sequence, initialized, selected as a runtime
backend, or connected to Gateway execution. Migration creates no domain,
capacity, accounting coverage, receipt, projection, or mode row. A future
explicit coverage and cutover operation must establish the domain; an empty
schema is not complete historical coverage.

The economic authority and its `Accept`, `Read`, `MarkDispatch`, `Checkpoint`,
and `Settle` implementation remain separate work. Schema checks do not establish
current authorization, route readiness, reservation capacity, native completion,
executor closure, durable delivery, or two-process quota enforcement.

## Owned tables

| Table | Identity and retained data |
| --- | --- |
| `admission_economic_domains` | Exact installation; format version, explicit initialization, coverage start, monotonic clock, explicitly supplied receipt capacity and receipt count. |
| `admission_economic_receipts` | Installation and exact RequestID hash; original RequestID, ExecutionID and ReceiptID plus their exact hashes; original owner identity/birth; immutable original intent/digest, admission instant and original month bounds; optional terminal body/digest pair. |
| `admission_economic_attempts` | Installation, request hash and attempt number 1–4; exact original RequestID and AttemptID, attempt identity hash, immutable dispatch body/digest, and a separate optional checkpoint body/digest pair. |
| `admission_economic_accounts` | Installation and exact account hash; original account bytes and authoritative birth; independent nonnegative logical RPM and executor counts. |
| `admission_economic_counters` | Installation, account hash and window; bounded canonical quota-counter bytes. Monetary accumulators remain arbitrary-size integer-unit strings inside validated JSON. |
| `admission_economic_contributions` | Installation, request hash, account hash, kind and window; original RequestID, exact subtraction payload and optional projection expiration. |
| `admission_economic_invalid_bounds` | Installation and exact original bound-revision hash; original revision, causing request identity/hash and positive recorded instant. |

Hash columns are lowercase SHA-256 keys derived from the exact original ASCII
identity bytes. They prevent case-insensitive database collation from merging
case-distinct operations. They are lookup/index keys, not authority tokens. The
storage adapter must recompute the hashes and compare the original identities,
births and canonical operation bodies after every lookup; matching a database
key or a 64-byte string alone proves none of those facts. Hash columns do not
replace exact original identities.

Every relationship belongs to the economic domain, receipt or account and uses
`RESTRICT` for update and deletion. No retained receipt references mutable
users, Sessions, Keys, Projects, Teams, owner registrations, processes,
Credentials or catalog records through a foreign key. Historical checkpoint and
settlement recovery must remain possible after those records change or disappear.

The Go `Window` field maps explicitly to `window_name`. Windows are `active`,
`minute`, `five_hours`, `seven_days`, or `month_<original-start>`; validation of
those exact values belongs to the projection adapter.

## Bounds and representation

GORM owns schema creation, columns, indexes and restrictive foreign keys.
Historical V119 models are private frozen types; migration imports no evolving
business entities and does not run unbounded startup `AutoMigrate`.

Operation, contribution and counter bodies use native GORM `[]byte` columns:
PostgreSQL BYTEA and MySQL LONGBLOB. Each nonempty body is capped at 128 KiB.
Checkpoint and terminal tails are SQL NULL until recorded; body and digest must
appear together, with a 64-byte digest and a nonempty bounded body. Their
once-only behavior and immutable original fields require conditional service
transitions and exact readback; SQL shape alone cannot enforce immutability.

Account and original operation identities retain the existing 1–64-byte bounds.
Composite index widths stay below MySQL's 3,072-byte budget even at four bytes
per character. Primary and unique identity indexes use full columns, never
prefix or expression indexes. Persisted births use the existing authoritative
microsecond timestamp representation without automatic creation timestamps.
No absent birth is synthesized into the current time.

Coverage and clock coordinates are Unix nanoseconds. An uninitialized explicit
domain requires zero coverage, clock and receipt count. An initialized domain
requires positive coverage and a clock at least that coverage. Capacity has no
default and must be supplied explicitly; the receipt count is nonnegative and
cannot exceed it. Admission timestamps retain room for the seven-day expiration
addition, and the original month end must follow admission.

Counter JSON is opaque to portable SQL checks. Before arithmetic, the adapter
must decode and validate it with `eventqueue.ValidateQuotaCounter`, then use
`eventqueue.QuotaCounterDelta`. Negative, corrupt, noncanonical or oversized
projections fail closed. Never narrow accumulated money to a SQL decimal,
floating-point value or single-amount precision.

## Independent projection and history lifetimes

Contribution kinds are the exact ASCII values `rpm`, `executor`, `active_quota`,
and `terminal_quota`; SQL guards reject case and padding aliases on both drivers.
RPM and terminal quota contributions require positive expiration and a named
window. Executor and active quota contributions have no timer expiration.
Heartbeat loss, owner fencing, process deletion and restart cannot manufacture
executor closure or known economic usage.

Projection expiry removes only exact contributions and their counter effects.
It must retain original admission, dispatch, checkpoint and terminal bodies,
receipt identities and invalidated bound revisions. Releasing logical executor
concurrency remains independent of unknown token and money dimensions. A
terminal nil dimension retains its original held bound or unknown contribution
until its original window expires. Late settlement does not shift consumption
into the completion month.

## Partial DDL and validation

The candidate repairs missing columns only in empty interrupted tables, adding
unguarded columns before CHECK-bearing columns. Missing columns in populated
history or projections fail closed. Existing representations are never silently
altered. Actual column type, width, precision, primary-key status, nullability,
defaults and named index column order/uniqueness are inspected before success.
Frozen checks are validated against retained rows and reconciled through the
existing GORM migration helpers.

The numbered runner owns migration serialization and registration. The opt-in
candidate test uses the same connection-scoped migration advisory lock but
writes no V119 ledger row; its result is not registered-startup acceptance.

## Authored validation and remaining gates

Focused frozen-schema tests cover current/frozen scalar parity, exact physical
CHECK column names, restrictive economic-only references, absent defaults,
portable binary bodies, nullable-tail pairing, the four-attempt bound, kind and
expiration guards, and index byte budgets.

The dedicated real-driver test requires an explicitly owned, initially empty
`routex_test` database and one driver per worker. It authors empty creation,
repeat execution without seeding, empty interrupted-column repair, real V118
existing-data preservation, populated repeat, two independent connection pools
under the migration lock, constraint violations, case-distinct exact identities,
wrong named-index rejection, projection deletion retaining history and bound
invalidation, and populated missing-body rejection.

These tests are authored and unexecuted. Compilation, focused checks,
PostgreSQL/MySQL execution, registered concurrent startup, full mandatory gates,
commit/push and runtime activation remain pending. Two connection pools in the
schema test do not prove two-process runtime admission enforcement.

When the execution lane is available, run the focused schema tests first. The
opt-in real-driver worker uses these explicit environment inputs:

```sh
ROUTEX_TEST_ECONOMIC_SCHEMA=1
ROUTEX_TEST_ECONOMIC_SCHEMA_OWNED=1
ROUTEX_TEST_ECONOMIC_SCHEMA_DRIVER=postgres  # or mysql
ROUTEX_TEST_ECONOMIC_SCHEMA_DSN=<owned disposable database DSN>
```

Run only `TestAdmissionEconomicV119Integration` in that worker, independently for
PostgreSQL and MySQL. Its last rejection case deliberately leaves a malformed
owned test table for inspection; discard the disposable database afterward.

## Dormant projection and settlement source

A subsequent unactivated adapter adds exact account births, independent RPM and
logical executor contributions, canonical Token/money projections and bounded
expiry. Every contribution retains its own validated subtraction data. Expiry
never locks or deletes old receipts, and active holds/executors have no timer.
A 4,097-row probe refuses a batch above the 4,096-row work bound; the caller
rolls back the complete transaction. There is no unbounded cleanup or scaling
claim. Known-zero changes preserve a locked identical counter without relying
on MySQL reporting a changed row. Historical account lookup cannot create a
missing account.

The dormant `Settle` operation locks the domain, original receipt and sorted
accounts, validates the exact terminal fact and releases the logical executor
once. It moves independent known or unknown Token/money dimensions from active
holds into their original still-live admission windows. Late settlement never
changes the original month. Overrun records complete debt and permanently
invalidates the original nonempty bound revision. It keeps the first cause.
Identical terminal acknowledgement retries return immutable history without
reapplying projections; conflicting bodies refuse. All economic, terminal and
clock writes share one READ COMMITTED transaction and require no current owner,
mode, authentication or route authority. These operations allow no dispatch.

This source remains unregistered and unwired. Focused and dedicated real-driver
projection/settlement tests must pass before qualification. Admission, dispatch,
explicit coverage/cutover and two-process runtime acceptance remain separate
unfinished work.
