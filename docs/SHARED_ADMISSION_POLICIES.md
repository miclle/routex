# Current SQL admission policy checks

This document describes the private, unwired
`validateSharedAdmissionPoliciesSQL` source slice. It checks current stored
policies, account births, the original calendar, and a transient live client IP
inside a caller-owned SQL transaction. It does not reserve quota, count requests,
grant dispatch authority, settle calls, or activate a shared backend.

## Transaction contract

Call this checker only after a successful `validateSharedAuthorizationSQL` in
the **same bounded READ COMMITTED transaction**. Acquire governance before
installation/mode/owner fences; retain the preceding authorization loader's
owner, resource, Key, Session and grant locks through the policy check and the
future atomic admission operation. The private function rejects a base GORM DB
and accepts no `Service`, `GatewayResult`, bearer, or runtime snapshot.

The policy checker does not produce an authorization token or a reusable proof
object. Its call-order precondition is explicit; a transaction pointer alone
does not establish that the preceding authorization check ran. No production
call site has been wired by this slice.

All SQL reads use fresh GORM query state backed by the supplied transaction.
There is no nested transaction, base-DB fallback, external authentication, or
network operation. Retained Key graphs are read with exact owner predicates,
deterministic ordering, a 10,000-record bound plus overflow sentinel, and row
locks. Existing normalized typed policy readers and root predicates are reused.

## Exact independent accounts

| Principal | Accounts | Account births |
| --- | --- | --- |
| Personal Session | Exact User account | Original User creation instant |
| Personal Key | Exact User account and oldest retained Key-root account | User creation and oldest root creation, respectively |
| Project Key | Exact Project account and oldest retained Project Key-root account | Project creation and oldest root creation, respectively |
| Team Session | Exact Team account and stable Team/User pair account | Original Team creation for both accounts |

Accounts are derived from the captured principal after its current SQL
authorization check. An arbitrary policy account, Key prefix, audit creator,
Project manager, or final logical-call identity cannot establish a root.

Key rotation shares the oldest retained root account. Disabled, revoked and
expired predecessors do not reset that account. The selected current Key must
retain its exact owner and captured birth, active status and expiry; a Personal
Key must also retain its captured lifecycle revision. Retained graphs must have
complete same-owner ancestry and valid births, and must not collide with the
other Key table. The root's **ResourceLimit ETag** and birth are compared against
the original policy capture. They are distinct from the current Key's lifecycle
revision and creation instant.

The Team/User account is the existing complete SHA-256 pair digest encoded in
unpadded base32 after the `team_member_` prefix. Membership replacement does not
reset its Team birth. Current membership authority belongs to the preceding
authorization check; Team admission never borrows User policy or Personal grants.

## Stored policies and original capture

The checker compares the complete original `eventqueue.QuotaLimit` projection:
account, policy revision, birth, RPM, concurrency, rolling five-hour/seven-day
tokens, monthly tokens and money, TPM, exact decimal amount and currency,
canonical per-dimension monthly behavior, and exact Personal-Key/Project-Key/
Team-member flags. Account order is irrelevant; duplicates, missing accounts,
extra accounts, substituted roots, and changed facts fail closed. Births compare
instants rather than Go location or monotonic metadata.

Missing ResourceLimit rows mean the existing synthetic revision `0` with a
normalized empty policy. Explicit zero caps remain distinct from absent caps.
Known principal/root births are required; unknown birth is never replaced by
request time. Default creation templates are not consulted during admission.
Creation and explicit reset copy them into stored ResourceLimit rows, and an
unrelated template revision does not change those existing policies.

Team policies preserve the existing supported contract: IP mode is `none` with
no ranges. Team-member policies additionally have no rolling five-hour/seven-day
cap. This slice does not expand those controls. Parent and root policies remain
independent and conjunctive rather than being collapsed into merged minima.

The singleton quota-setting row is locked and must match the original calendar
ETag and exact time-zone spelling. Empty, invalid and host-dependent `Local`
zones are rejected. `AccountingStarted` is an existing activation flag, not an
invented calendar revision. The pricing-setting singleton is locked; finite
money policies must still use its current platform denomination. Amounts remain
exact decimal strings. This check does not prove route prices or reservation
capacity; those are separate pending boundaries.

## Live client IP and lock intersections

The caller supplies the canonical address already derived by the existing
trusted-proxy request boundary. It must be valid, unmapped and zone-free. The
checker never reparses headers. Personal Session checks its User IP policy;
Personal/Project Keys check both parent and root with `limits.Allows`. Team
policies retain their supported no-IP contract. IP rejection does not imply any
economic debit or dispatch.

The address is a separate transient function argument. It is not added to the
captured authorization, admission request, digest, spool, receipt, or SQL schema.
A stored intent cannot recreate live request identity or authorize recovery
dispatch. Future authority integration must preserve this boundary explicitly.

Supported policy writers, Project request application, default reset, creation
defaults, and pricing/calendar configuration writers intersect the held
governance barrier. Personal Key writers lock User before Keys; Project Key
writers take governance and Project before Keys. The preceding authorization
loader already owns these owner locks before the retained graph reads. Policy
rows, calendar and denomination have their own retained row locks. In particular,
quota activation can update `AccountingStarted` without governance and must
still intersect the quota-setting row lock. Missing policy rows are protected
against supported insertion by governance; an absent row alone is not a portable
SQL locking guarantee. No inverse calendar-to-governance acquisition is added.

## Authored verification and pending acceptance

Dedicated unit sources cover the four principals, inactive continuity roots,
complete capture changes, missing/duplicate/extra accounts, root substitution,
bounded exact graphs, alias/collision rejection, calendar/policy/denomination
changes, independent parent/root IP checks, supported Team restrictions,
membership replacement, stored-only defaults, unknown/zero distinctions,
transaction/IP input validation, and SQL error propagation.

`TestSharedAdmissionPoliciesSQLIntegration` is an opt-in real-driver worker,
deliberately outside the existing full matrix. It requires an exclusively owned
disposable `routex_test` database. Four principal cases use real rows. Five
reader-first cases hold successful authorization/policy transactions while direct
competing policy, IP, calendar and denomination writes must hit their bounded
deadline. Five writer-first cases hold changed rows before a renewed check; the
reader must block, then reject the original capture after commit. Competing
workers and cancellation joins are bounded. Direct test writes omit governance
to exercise the actual policy/calendar/denomination row locks independently.

After the exclusive diagnostic lane is released, run the worker separately on
both drivers with `ROUTEX_TEST_SHARED_POLICY_SQL=1`,
`ROUTEX_TEST_SHARED_POLICY_SQL_DRIVER=postgres` or `mysql`, and the corresponding
`ROUTEX_TEST_POSTGRES_DSN` or `ROUTEX_TEST_MYSQL_DSN` for that dedicated database:

```sh
go test -race ./internal/routex/service -run '^TestSharedAdmissionPoliciesSQLIntegration$' -count=1 -timeout=90s
```

At source preparation, no compiler, tests, race checks, mandatory check, build,
or runtime acceptance has run. Authored tests are not passed evidence. Coherent
source qualification, both real-driver runs, route/capacity/price checks, shared
economic authority, and runtime wiring remain pending.
