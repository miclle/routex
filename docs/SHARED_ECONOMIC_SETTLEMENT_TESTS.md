# Shared economic settlement SQL tests

`TestSharedEconomicSettlementSQLIntegration` is a dedicated, opt-in, single-driver
worker for historical settlement. It requires an empty, exclusively owned
`routex_test` database, applies the current predecessor migrations, and creates
only the seven economic entities with bounded test-only GORM DDL. Frozen
migration acceptance is separate. No application, inference transport, shared
cutover or new dispatch authority is created.

Each case seeds its own dormant accounting domain and clock, exact canonical
admission, account birth, original dispatch and active quota/executor/RPM
contributions. Terminal fixtures pass the normal call and original-dispatch
validators. `Actual` derives from `quotaCallSettlement`; favorable usage is never
injected into the positive terminal fixtures. The priced overrun uses the
existing pricing fixture and `finalizeCallPricing`, retaining the entire amount.
Its native evidence is `incomplete`, which does not claim native completion.

The eight children cover:

- Concurrent identical settlement releases logical executor occupancy exactly
  once while original unknown token/money holds remain in their windows.
- Exact replay, conflicting canonical terminal bytes and detached return values.
- Favorable `Actual` without matching facts, missing executor closure and
  unproven native closure all refuse without changing original accounting.
- Full token/money overrun debt and permanent invalidation of the original bound
  revision; expiry and later terminal replay preserve the exact invalidation.
- Missing retained accounts refuse without synthesizing new historical state.
- Pre-cancelled and row-lock-blocked cancellation, plus complete outer rollback
  of a positive nested settlement, preserve the original receipt and projections.
- A real current Personal Key authorization and registered owner first pass
  their respective checks, then revocation/fencing/mode revision remove current
  authority while original historical settlement remains possible.
- Settlement after the admission month retains only unexpired original rolling
  windows, never charges the new month and never restarts an expiry clock.

Historical settlement is not current authorization, process takeover, native
completion, deliverability or a new dispatch permit. Executor closure is supplied
by the exact terminal intent independently of economic completeness. These
storage fixtures validate that distinction; they do not observe a real network
executor closing. Permanent invalidation here means the retained invalid-bound
record, not qualification of the still-unimplemented live admission gate.

The parent has a 120-second context. Concurrent operations and cancellation joins
are bounded at five seconds. The blocked cancellation control holds only the
exact account row and installs no live GORM callbacks or production hooks.

After the execution owner grants a lane, run each selected driver through the
existing owned disposable Compose/supervisor lifecycle, with a fresh database:

```sh
ROUTEX_TEST_SHARED_ECONOMIC_SETTLEMENT_SQL=1 \
ROUTEX_TEST_SHARED_ECONOMIC_SETTLEMENT_SQL_DRIVER=postgres \
GOMAXPROCS=1 GOFLAGS=-p=1 GOMEMLIMIT=512MiB \
go test -json -tags development -race -count=1 -timeout=3m \
  ./internal/routex/service -run '^TestSharedEconomicSettlementSQLIntegration$'
```

Supply the disposable DSN through `ROUTEX_TEST_POSTGRES_DSN`; select `mysql` and
`ROUTEX_TEST_MYSQL_DSN` for the other driver. Do not print DSNs. Bound and directly
join the exact test process, then verify removal of only its owned resources.
Expected inventory: nine named nodes per enabled driver, one parent and eight
children, with zero skips. The source reuses the projection and history fixture
helpers; include those leaves in any selective composition.

Compilation, race execution and real PostgreSQL/MySQL acceptance remain pending.
