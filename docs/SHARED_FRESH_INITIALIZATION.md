# Private fresh shared-domain initialization

This is an unwired source foundation. There is no public API, administrator
action, CLI command, runtime activation, owner registration, or registered
economic/origin migration in this slice. The normal server still calls the local
`StartCallRecorder`; initializing a shared domain does not make that startup path
usable. Existing local installations cannot use this constructor to convert or
discard their history.

## Caller prerequisites

`initializeFreshSharedDomainSQL` requires a genuinely new deployment and schema.
Before capturing the boundary, the operator must exclude every old executor and
startup, control access to deployment/database credentials, and maintain that
exclusion until initialization commits. Every permitted startup version must
respect the persisted admission-mode barrier. An old binary that ignores that
barrier requires actual external shutdown. A reviewed offline integration is
still required to enforce these operational prerequisites.

This is an explicit caller responsibility, not a boolean accepted by the helper.
Empty tables, missing local files, expired process leases, stopped/retired process
metadata, and matching canonical digests do not prove executor exclusion or
absence of erased history. The captured boundary must be the actual beginning
of the controlled fresh interval; arbitrary backdating is not permitted.

The caller must supply a real, bounded READ COMMITTED transaction, obtain governance before
calling, and roll back on any helper error. The helper never starts a nested
transaction or obtains governance after a lower-order lock. Return values become
reviewable SQL facts only after a positively acknowledged commit; they do not
establish routing activation, a dispatch permit, or fleet application.

## Atomic write and retries

Capture installation ID, exact time-zone spelling, reviewed quota-calendar ETag,
boundary nanoseconds, and explicit positive receipt capacity once. Preserve that
exact intent through an uncertain commit; do not regenerate an ID, use a newer
calendar ETag, round a boundary, or choose a new capacity for a retry.

Lock order is governance (already held by the caller), administrative
Installation, LimitInstallation, AdmissionMode, QuotaSetting, economic domain,
then immutable origin. The first write requires the administrative installation
to remain uninitialized, an empty/uninitialized admission binding, no mode, and
the exact unactivated reviewed calendar. Bounded, unscoped existence probes
reject observable resources, sessions, limit policies, call/attempt/economic history,
audit history,
owners, processes, and runtime observations. These are refusal checks, not
positive proof of the deployment prerequisite. Registered migration seeds such
as settings, permissions, and creation-default templates are retained; the
probed runtime/history tables are not seeded by the registered migrations.

One transaction creates the domain and canonical fresh origin, initializes the
exact binding, inserts shared mode revision 1, and freezes calendar accounting.
Receipt capacity is immutable in this slice. The existing origin format retains
its eight calendar/coverage coordinates; capacity remains in the exact domain
row and must match the original intent on retries.

A matching successful retry remains valid after administrator/resource creation,
economic receipts, clock advancement, or a later same-zone calendar revision.
It validates the original canonical origin and capacity plus the current exact
binding, shared mode, frozen zone, and coverage. It never resets the clock,
capacity, or current calendar revision. Missing partial provenance, another
intent, and a changed mode fail. Callers must retain the original intent until
the commit outcome is resolved.

## Calendar reads and real resource births

`readSharedEconomicCalendarSQL` requires caller-held governance and independently
established current resource authority. In the caller's real transaction it
locks binding/mode, current quota setting, domain, then origin. It makes no
writes. Missing legacy provenance, different exact identities, changed zone,
different coverage, or a regressed/invalid clock fail closed. The returned
current calendar ETag and historical freeze ETag are distinct. A same-zone save
does not invalidate the historical origin. This reader does not change the
separate historical settlement loader or authenticate provenance supplied by
another source.

The existing setup transaction locks the administrative Installation before
creating the first administrator and copying its creation-default policy.
`applyCreationDefaultLimit` does not subsequently acquire governance. Taking
that Installation lock after governance prevents a concurrent first setup from
creating a resource inside the initialization transaction's fresh interval
without introducing a reverse governance lock order. The normal server starts
its local journal before serving setup, so a future offline startup integration
must initialize first; the current setup page is not an initialization entry.

Quota enforcement retains actual resource births. An administrator created after
fresh coverage can use a finite first-month policy even when the calendar month
began earlier. An older or unknown parent account still lacks that coverage;
creating a new root Key cannot bypass the independent parent check. No birth is
rewritten to the boundary. Database timestamp precision and the actual ordering
of capture, commit, and setup remain part of real-driver acceptance.

## Verification boundary

Focused unit source checks deterministic coordinates, invalid intents, real
transaction requirements, historical/current calendar separation, corruption,
and first-month coverage with new, older, and unknown account births. The
dedicated SQL source exercises complete registered migrations followed by
bounded test-only candidate tables, initial refusal cases, rollback atomicity,
explicit registered V118 binding/mode/owner and history dependency checks,
concurrent identical initialization, lost-reply replay, real first setup, local
identity-preparation refusal, read-only sampling, progressed clock/revision,
changed-intent conflicts, and missing/corrupt/stale provenance.

The SQL worker is opt-in, runs one driver per exclusively owned initially empty
`routex_test` database, and reuses the existing disposable database identity
check. Set `ROUTEX_TEST_SHARED_FRESH_SQL=1`,
`ROUTEX_TEST_SHARED_FRESH_SQL_DRIVER=postgres` or `mysql`,
`ROUTEX_TEST_SHARED_FRESH_SQL_OWNED=1`, and the corresponding
`ROUTEX_TEST_POSTGRES_DSN` or `ROUTEX_TEST_MYSQL_DSN`. Select only
`TestSharedFreshDomainSQLIntegration`; never share its schema with another
worker or application. Failed workers retain their source/diagnostic identity;
resource cleanup remains owned by the isolated runner.

These tests do not qualify frozen migration DDL, full shared admission,
multi-process startup exclusion, old binary shutdown, an active local-to-shared
transition, or browser behavior. Those remain independent requirements before
any public integration or activation. Compilation, focused tests, both real
drivers, and mandatory project checks remain pending until the execution gate
is released.
