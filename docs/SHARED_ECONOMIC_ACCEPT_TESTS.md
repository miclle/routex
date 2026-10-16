# Shared Economic Accept and Dispatch Qualification

Status: source-only candidate. The dedicated worker has not been executed on
PostgreSQL or MySQL. It does not enable shared admission, qualify schema migration,
establish historical cutover coverage, dispatch inference, or prove fleet behavior.

## Isolated worker

`TestSharedEconomicAcceptSQLIntegration` requires both
`ROUTEX_TEST_SHARED_ECONOMIC_ACCEPT_SQL=1` and
`ROUTEX_TEST_SHARED_ECONOMIC_ACCEPT_SQL_DRIVER=postgres` or `mysql`. The existing
runtime archive integration database configuration supplies the selected driver's
connection. Allocate a fresh, exclusively owned disposable database for each run;
the worker rejects an existing migration registry. Never point it at a development,
shared, or production database.

The worker runs the current predecessor migrations, then creates exactly the seven
bounded economic entity tables through the GORM Migrator. This test-only DDL does
not replace the separate frozen migration acceptance. Its initialized empty domain
and coverage are explicit fixture facts, rather than inferred production state.
The worker has a 120-second context bound. A supervisor remains responsible for
database/container ownership, process closure, unchanged source verification,
complete test-name census, and retained original failure evidence.

## Coherent live fixture

The fixture combines the existing real SQL owner, authorization, policy, and
two-route catalogue fixtures. It replaces the catalogue fixture's unresolved
ciphertext with actual authenticated inline ciphertext, creates real bindings for
the exact principal's Model and a current public name, and records a test-only
Key or Session verifier. Production repeatable-read runtime loading, authorization
projection, credential preparation, route construction, principal authentication,
quota scope preparation, and reservation capture then build the request.

`newSharedEconomicLiveAuthority` receives that same prepared plan, result, quota
data, and transient parsed payload. No successful authorization callback, synthetic
allow function, or favorable runtime map patch substitutes for those operations.
The runtime authorization lease uses the existing production lease duration.
Inline credentials exercise the genuine inline source path; Vault source-holder
closure and remote Agent behavior require their own qualification.

The suite calls actual `Accept` before eligible `MarkDispatch`. An independently
read historical receipt cannot establish the live object's accepted execution.
No upstream HTTP request or paid inference is performed.

## Required assertions

- Personal Keys, Project Keys, Personal Sessions, and Team Sessions must all
  authenticate and admit through the actual SQL/live boundary.
- Concurrent identical Accept calls retain one original receipt, one domain
  capacity debit, and one RPM, executor, and complete maximum quota contribution
  for every exact account. Exact acknowledgement replay changes no economic row.
- Changed execution, calendar, or candidate input must never rewrite the accepted
  original. Token, money, RPM, and concurrency refusals are independent and leave
  all seven economic tables unchanged.
- Current Key revocation, owner fencing, mode revision, selected transport
  disablement, and denied source IP block admission acknowledgement and new
  dispatch. The IP case preserves the policy revision to exercise the independently
  retained IP condition.
- A second selected original attempt requires the exact retained no-work
  checkpoint prefix. Missing checkpoints and candidates outside the original
  reservation fail without changing economic state. A retry preserves the
  complete original maximum hold.
- A retained invalid-bound cause blocks both a new request and dispatch without
  repairing the invalid revision or partially applying accounting.
- Already cancelled work and cancellation behind a held governance row leave no
  partial receipt, account, counter, contribution, or capacity debit. An outer
  transaction rollback removes a complete nested admission atomically. Dispatch
  independently exercises cancelled work, a held governance row, and a complete
  nested attempt followed by outer rollback; the original dispatch still works
  after rollback.
- Historical settlement remains possible after committed Key revocation, releases
  the original executor exactly once, and preserves original terminal replay.

Refusal/replay snapshots compare every field of all seven entity row sets using
deterministic ordering. Entity JSON suppression must not turn these checks into
empty-object comparisons. Real driver execution, mandatory checks, and integration
into the maintained test runner remain pending.
