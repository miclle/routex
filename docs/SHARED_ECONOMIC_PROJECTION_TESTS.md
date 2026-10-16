# Shared economic projection SQL tests

`TestSharedEconomicProjectionSQLIntegration` is a dedicated, opt-in, single-driver
worker for dormant accounting projection operations. Run PostgreSQL and MySQL
separately against fresh, exclusively owned `routex_test` databases. It refuses
an existing migration ledger or economic tables, applies the current predecessor
schema, then creates only the seven economic entities with test-only GORM DDL.
The frozen migration has separate qualification; this worker does not qualify a
migration, activate shared mode, admit inference or launch an application.

The nine direct children exercise:

- Case-distinct account identities, exact retained births, sorted acquisition,
  duplicate refusal and retained-only lookup without missing-account synthesis.
- Independent active token/money holds, executor occupancy and RPM counts.
- Known-zero addition and subtraction over a nonempty counter, preserving its
  exact bytes even when MySQL reports no changed row for an identical update.
- Independent known/unknown terminal dimensions in all original admission
  windows, including the admission month; unknown active holds never time out.
- Exact-once expiry that retains immutable receipt bytes and invalid-bound
  records, while active quota and executor occupancy remain present.
- Corrupt subtraction data and counter underflow, each rolling back an earlier
  RPM subtraction and deletion in the same transaction.
- A 4,097-row expiry overflow probe refusing the whole batch without changing
  contributions, counts, counters or retained receipts.
- Exact account and counter row locks held past helper return, excluding a
  second SQL connection until the first transaction ends.

Fixtures explicitly seed historical canonical admission data and exact account
births before creating contributions. They do not establish current SQL
authorization, route eligibility, economic cutover or executor closure. A timer
only expires RPM and terminal quota projections. Terminal economic data is not
native completion or process closure evidence. Current authorization and owner
fencing remain separate prerequisites for any future live admission adapter.

The worker has a 120-second parent context. Concurrent SQL operations use a
five-second context and a further bounded five-second join after cancellation.
The lock tests intentionally bypass the domain lock in their two isolated
read-only transactions so that a domain lock cannot substitute for the exact
account/counter lock being tested. No live GORM callbacks or production hooks
are installed.

After the execution owner grants the real-driver lane, use the repository's
owned Compose/supervisor lifecycle with a new empty database for each command:

```sh
ROUTEX_TEST_SHARED_ECONOMIC_PROJECTION_SQL=1 \
ROUTEX_TEST_SHARED_ECONOMIC_PROJECTION_SQL_DRIVER=postgres \
GOMAXPROCS=1 GOFLAGS=-p=1 GOMEMLIMIT=512MiB \
go test -json -tags development -race -count=1 -timeout=3m \
  ./internal/routex/service -run '^TestSharedEconomicProjectionSQLIntegration$'
```

Supply the owned disposable PostgreSQL DSN through `ROUTEX_TEST_POSTGRES_DSN`.
For MySQL, select `mysql` and supply `ROUTEX_TEST_MYSQL_DSN`. Keep DSNs out of
reports and command logs. The outer owner must bound the process lifetime,
directly join the exact test process and remove only its Compose resources.
The expected named inventory is ten nodes per selected driver: one parent and
nine direct children, with zero skips when explicitly enabled.

These tests are authored source only. Compilation, race execution and real
PostgreSQL/MySQL results remain pending. No acceptance transfers from earlier
history, schema or policy workers.
