# Transaction-scoped shared account usage

## Status

This package is dormant, source-only preparation for shared admission. It is not
wired to the Gateway, Control Plane, quota reports, a public API, or startup.
Formatting and source checks do not qualify execution. Unit compilation and the
dedicated PostgreSQL/MySQL scenarios remain pending until their exact source can
run in the available validation lane.

## Internal contract

`sampleSharedAccountUsageSQL(tx, target, account, now)` reads one exact account
from the existing SQL economic projections. `target` carries the exact
installation ID and current shared-mode revision. The caller supplies an actual,
bounded GORM transaction and acquires governance **before** calling the helper.
The helper acquires no governance lock, opens no transaction, and does not commit.
The caller must roll back on every error and must not publish a returned proof
until the caller's transaction has committed successfully.

Lock order is governance (caller), installation binding, admission mode, economic
domain, exact account, expired projections and their counters. The helper checks
the initialized singleton binding, exact current lower-case shared mode and
revision, and initialized economic domain. An owner heartbeat, process fence,
dispatch permit, authorization receipt, or resource read permission is not
provided by this operation; the integrating caller must establish its own
required authority.

The detached internal result contains:

- `Account`: the exact requested account, including case.
- `AsOf`: the maximum of valid caller time and the retained domain clock.
- `Recorded`: nullable original account birth, RPM count, and logical executor
  count. A null value means the account is unregistered. It does not mean zero
  usage or establish accounting coverage. A registered zero retains its original
  birth and a non-null result.

No missing account is created. An otherwise successful missing-account read can
advance the existing domain's monotonic clock, under the same transaction. This
clock update is part of the operation even when no projections expire; the
helper is therefore a transaction-scoped sample, not a read-only inspection.
Rejected authority, cancellation, malformed source data, expiry failure and
overflow must roll back all effects.

## Expiry and bounds

The operation delegates expiry to the existing single-account projection
primitive. Original retained projection bodies determine the cutoff and exact
subtraction. At most 4,096 due RPM/terminal-quota contributions may be processed;
a 4,097th row causes an unavailable result before subtraction. No partial proof
is returned. A malformed later expiry record rolls back any earlier subtraction
when the caller follows the transaction contract.

Logical executor and active quota contributions never expire by a timer. Unknown
token or money usage does not turn independent RPM/executor counts into unknown
or zero. Immutable receipts and invalid-bound evidence remain retained. Sampling
does not read historical Call Records or a local journal to manufacture missing
SQL history.

Identity validation preserves the exact installation/account digest and the
microsecond-precision original birth. Zero, future, wrapped/out-of-range dates,
negative counts, an uninitialized domain and a stale mode revision cannot yield
a usage proof. `AsOf` cannot regress with the caller's clock.

The package deliberately provides no token/money totals, monthly or rolling quota
batch, frozen-calendar binding, global money-hold census, pagination, fleet
closure, or mode transition. The 4,096 bound is expiry work for one account;
it is not a guarantee about global economic completeness. Broader quota reads
require separate bounded APIs and calendar/coverage proofs before activation.

## Pending verification

Focused source controls cover exact mode and identity, original birth, monotonic
time, representable bounds, detached results, registered zero, cancellation, and
the actual-transaction requirement before storage access.

`TestSharedAccountUsageSQLIntegration` is a dedicated opt-in worker, outside the
complete driver matrix. Allocate one fresh exclusively owned disposable database
per driver using the existing integration connection contract, then select:

```text
ROUTEX_TEST_SHARED_USAGE_SQL=1
ROUTEX_TEST_SHARED_USAGE_SQL_DRIVER=postgres|mysql
```

The source checks for absent migration history/economic tables, migrates the
current predecessor, then creates seven bounded economic tables and initializes
mode/domain facts solely as test fixtures. This is not production initialization
or qualification of the independent immutable economic migration.

Pending real-driver scenarios cover original expiry just before/at cutoff;
independent executor and active unknown quota retention; immutable receipt and
invalid-bound retention; missing/case-distinct accounts without creation;
registered zero; monotonic sampling; current mode/revision rejection; transaction
requirement; rollback after later expiry corruption; 4,097-row overflow; retained
account locks; cancelled domain-lock waits; and uninitialized-domain rejection.
Both supported drivers, mandatory project checks, and any later wiring-specific
acceptance must pass before claiming delivery or activation.
