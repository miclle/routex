# Current SQL Authorization for Shared Admission

## Status and boundary

This source slice adds a private, unwired current-authorization validator in
`internal/routex/service/admission_shared_authorization_sql.go`. It is not a
shared economic authority or an activated Gateway backend. It creates no schema,
receipt, admission, counter, hold, Session, or transport operation.

`validateSharedAuthorizationSQL` consumes an existing transaction, the original
typed non-secret authorization snapshot, and an explicit current time. It
rejects malformed or mixed snapshots, future capture times, and nontransaction
database handles. It neither authenticates a bearer nor consults a process-local
runtime snapshot. Every SQL helper receives the supplied transaction.

Success means that the current rows still satisfy the original captured
authorization while this transaction retains its locks. It is not a reusable
dispatch permit. A future authority must perform this check at acceptance and
each new dispatch boundary, in the same transaction as its other authoritative
checks and conditional write. Receipt recovery alone cannot grant new transport
execution.

## Caller contract and lock order

The caller must use a bounded `READ COMMITTED` transaction. The validator verifies
that a transaction handle was supplied; it does not inspect database-specific
isolation settings or open a nested transaction. Repeatable-read snapshots must
not substitute for current authority.

The global order starts with the governance singleton, followed by the existing
installation/mode/owner-fence rows. If the caller has already acquired owner
fencing locks, it must already hold governance first. Reacquiring that same
governance row in the validator is intentional. Calling it after taking owner
locks without governance would violate the caller contract.

The authorization slice then locks its subject Users and linked approval rows,
Project or Team, Key or Session, exact selected relationships and grants, primary
identity or managed delivery dependencies, and finally the selected Model.
Project manager candidates are read under the governance barrier, bounded to
1,000 rows, and their Users are locked in deterministic ID order before the
Project. Each candidate relationship is re-read with its exact row lock. The
complete Personal grant set is also bounded to 1,000 rows; overflow fails closed.

Governance serializes resource lifecycle, manager replacement, approval decisions,
identity configuration/unlinking, MFA changes, and Personal grant writers. It is
insufficient on its own for Personal Key and Session revocation: those existing
writers can operate without governance. The retained exact User/Key/Session locks
intersect those writers. Personal Key writers take User before Key; the validator
preserves that order. Session-only deletion has no subsequent authority lock.

Managed delivery locks its command before its binding, after the existing owner
and Key locks. Acknowledgement writers also hold governance. Key revocation holds
the owner/Key locks before updating linked commands. Primary provider and binding
reads are locked and validated within the governance barrier; no remote login or
provider call occurs.

This is the ordering of this bounded slice and its current intersecting writers,
not a claim that every future shared-authority operation has been integrated.

## Current principal checks

| Principal | Required current facts |
| --- | --- |
| Personal Key | Exact admitted, enabled User and birth; linked approval decision; exact active, unexpired Key and lifecycle revision; selected Key model relation; complete Personal grant set, hash and revision; managed delivery acknowledgement when required; exact active Model and birth |
| Personal Session | Exact admitted, enabled User and birth; exact unexpired Session and birth; current authentication policy; the Session's exact current primary provider/binding or emergency proof; complete Personal grant set, hash and revision; exact active Model and birth |
| Project Key | Exact active Project and birth; at least one current admitted manager with locked User/approval/relationship rows; exact active, unexpired Project Key and birth; selected Key model and Project grant; managed delivery acknowledgement when required; exact active Model and birth |
| Team Session | Exact admitted, enabled User and birth; exact unexpired Session with current primary proof and policy; exact active Team and birth; exact active membership, User/Team IDs, role and recorded join instant; selected Team grant and recorded provenance; exact active Model and birth |

The existing pure constructor projects the current SQL facts. The validator
compares every projected original identity, birth, expiry, policy revision,
primary proof, admission proof, grant hash and provenance. Only the evaluation
time advances. Changed or missing current facts fail closed. SQL outages and
cancellation remain errors; they are not converted into authoritative absence.

Project Key creator attribution never grants authority. Current manager
continuity is a current eligibility check; the original snapshot has no captured
manager-generation field. Team Sessions consult no Personal grants. Project and
Team grant entities have no grant revision or birth, so none is invented.

Emergency authentication uses the existing `emergencyCurrent` predicate with the
explicit supplied time, rather than the SQL wrapper's wall clock. Current local
password/MFA and provider subject validators can read their private existing rows
inside the transaction. Those rows, token hashes, password digests, subject
digests and secret material are never returned or newly serialized by this slice.

## Verification sources and remaining work

Dedicated unit sources exercise all four principals, changed current identities,
revocation, expiry, grants, mixed primary proof, linked approval changes,
deterministic manager locking, managed acknowledgement corruption, error
propagation, and explicit emergency evaluation time.

The opt-in real-driver worker is
`TestSharedAuthorizationSQLIntegration` in
`internal/routex/service/admission_shared_authorization_sql_integration_test.go`.
It uses an exclusively allocated disposable `routex_test` database and the
composed migration chain. It checks four current principals, including a Project
Key with a disabled audit creator and a Team Session without Personal grants.
Eleven competing revocation statements must remain blocked by a held successful
authorization transaction. After release, each statement commits and a renewed
check rejects the original snapshot. Four inverse-order cases hold a revocation
transaction first; the validator cannot bypass it and rejects after commit.
Deadlines and worker joins are bounded. These cases use actual PostgreSQL/MySQL
statements, not a process-local mutex.

The worker is deliberately not registered in the existing full matrix. After the
current exclusive diagnostic lane is closed, run it separately for each driver
with `ROUTEX_TEST_SHARED_AUTH_SQL=1`,
`ROUTEX_TEST_SHARED_AUTH_SQL_DRIVER=postgres` or `mysql`, and the corresponding
`ROUTEX_TEST_POSTGRES_DSN` or `ROUTEX_TEST_MYSQL_DSN` pointing to an exclusively
owned disposable database:

```sh
go test -race ./internal/routex/service -run '^TestSharedAuthorizationSQLIntegration$' -count=1 -timeout=90s
```

At source preparation, compilation and execution have not run. Authored tests
are not acceptance evidence. Mandatory checking, both real-driver runs, focused
race tests and coherent composition qualification remain pending.

Current client IP, canonical quota-policy comparison, route/capacity/price
evidence, economic admission and settlement, owner fencing integration, and
runtime wiring are separate remaining boundaries. This validator claims none
of those capabilities.
