# Shared Economic History SQL Tests

The dormant SQL history worker exercises `sharedEconomicSQLStore.Read` and
`Checkpoint` against an exclusively owned disposable PostgreSQL or MySQL
`routex_test` database. The tests are authored and have not been executed.
They are not registered in the official matrix and change no production route,
quota authority, registry, runtime wiring or migration.

The worker first runs the current predecessor migrations, requires the three
economic tables to be absent, and creates only `AdmissionEconomicDomain`,
`AdmissionEconomicReceipt` and `AdmissionEconomicAttempt` through test-only
GORM `Migrator.CreateTable`. Their draft entity definitions are copied exactly
from the schema candidate. Frozen migration creation, upgrade, repetition,
concurrent startup and constraint qualification belong to the separate schema
worker. After that migration is composed, the worker's setup must be adapted
explicitly; it must not silently reuse an existing economic database.

Accepted receipts and dispatches are seeded directly as canonical, bounded
history. The initialized coverage domain and shared mode are test fixtures,
not cutover or accounting-coverage evidence. The worker does not implement or
qualify `Accept`, `MarkDispatch`, `Settle`, quota debits, unknown-hold release,
executor closure, live routing, inference or crash/uncertain-commit recovery.

## Coverage

There are 15 direct children, or 16 named nodes including the worker root:

- Identical competing checkpoints return successfully and retain one exact body;
  conflicting valid bodies produce one winner and one explicit conflict. Winner
  retries cannot replace the stored digest or canonical bytes.
- Exact receipt and checkpoint history survives actual current Personal Key,
  Project Key, Personal Session and Team membership revocation. A positive
  current authorization check precedes each revocation and a negative check
  follows it. History operations restore no dispatch permission.
- Actual owner fencing and a test-only mode revision change invalidate current
  owner authority without removing historical checkpoint/retry/read access.
- Missing first attempts, missing predecessor checkpoints, noncanonical dispatch
  bytes, changed checkpoint digests and case-changed original request IDs fail
  closed. Each control begins with valid ordered history and rolls back its
  corruption before checking retained history again.
- A terminal receipt rejects a new checkpoint while retaining exact prior
  checkpoint retries. Canonical but inconsistent terminal history is rejected.
- An enclosing transaction rollback, a pre-cancelled context and a deadline
  waiting on the domain lock leave no checkpoint. Fresh explicit retry works
  after the failed transaction; errors never stand in for absent history.
- Missing or changed original keys cannot create or rewrite retained history.
  Returned dispatches, checkpoint bytes, pointer-valued recovery amounts and
  terminal bytes are detached from persisted state.

Checkpoint controls also compare the complete original receipt and immutable
original dispatch fields, excluding only the independently mutable checkpoint
pair. Driver timestamp locations are normalized to UTC; instants, precision,
digests and stored bytes remain exact. Goroutines never call the parent test's
`Fatal`; competing operations use bounded contexts and direct result joins.
There is no live GORM callback registration, schema teardown, automatic retry
or external provider request.

## Deferred execution

Run each driver separately under the existing owned disposable database
supervisor after the compiler/database lane is released. Supply the selected
`ROUTEX_TEST_POSTGRES_DSN` or `ROUTEX_TEST_MYSQL_DSN` through the existing transient
environment mechanism; do not put credentials in command arguments or logs.
The worker has a 90-second parent deadline and five-second operation/join bounds.

```bash
ROUTEX_TEST_SHARED_ECONOMIC_HISTORY_SQL=1 \
ROUTEX_TEST_SHARED_ECONOMIC_HISTORY_SQL_DRIVER=postgres \
GOMAXPROCS=1 GOFLAGS=-p=1 GOMEMLIMIT=512MiB \
go test -tags development -race -json -count=1 -timeout=2m \
  ./internal/routex/service -run '^TestSharedEconomicHistorySQLIntegration$'
```

Use `mysql` for the second fresh, isolated driver. Neither a gated skip nor a
single-driver pass is both-driver acceptance. Preserve original failures,
source hashes, balanced run/pass/fail/skip counts and owned physical cleanup.
No workload may share the worker's database with an application or another
migration/archive worker.
