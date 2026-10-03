# Team Monthly Quota Requests

Team requests propose an increase to the applicant's monthly Token or money cap.
They build on [Team resource limits](TEAM_LIMITS.md) and do not create a Key,
reserve an allocation, reset usage, or change policies while pending. Full product
acceptance is tracked separately in [the implementation index](IMPLEMENTATION.md).

## Approval and authority

A current active member submits one dimension and a finite target above the
effective member cap. An unlimited effective cap cannot be increased by a finite
request. Token targets are canonical integer strings within the JavaScript safe
integer bound. Money targets preserve exact decimal strings in the current
platform currency. The reviewed context binds the full aggregate/member policies,
currency, quota calendar and current lifecycle; advisory usage is not a write
validator.

An eligible current owner other than the applicant always reviews first, including
requests above the aggregate cap. The first valid current owner decision wins.
An owner approval within the current aggregate applies only the member cap. An
owner approval above it advances to the platform quota stage without changing
either policy. If no eligible non-applicant owner exists, the request starts at
that platform stage. Losing the last eligible owner cancels the obsolete owner
step and advances once, preserving the history.

The server returns an approval effect preview from the current reviewed stage:
owner escalation leaves both effective caps unchanged, a within-aggregate approval
changes the member cap only, and final overflow raises both to the exact target.
Confirmation renders these before/after values and requires the same reviewed ETag;
clients do not synthesize a policy preview.

Platform approval requires `teams.tokens.write` or `teams.money.write` for the
request's dimension. An applicant cannot approve their own request at either
stage. Final approval raises the aggregate only when necessary and applies the
member target atomically, preserving all other policy fields and usage. The
aggregate is shared capacity rather than the sum of member allocations. A Team
owner receives no implicit direct policy-write permission.

## Endpoints and review

All paths are under `/api/v1`; mutations require the current Session, CSRF and
same-origin checks. The authenticated legacy `/admin/approvals` and
`/admin/quota-approvals` routes redirect to `/admin/quota-requests`, where the
independent destination permission remains required.

| Endpoint | Contract |
| --- | --- |
| `GET /teams/:team_id/quota-request-context` | Current member's monthly Token or money review context |
| `POST /teams/:team_id/quota-requests` | Reviewed `If-Match`, immutable creation UUID, dimension, exact target and required reason |
| `GET /quota-requests` | Server-filtered applicant history or currently assigned approvals, bounded cursor pages |
| `GET /quota-requests/:request_id` | Currently authorized detail, ordered steps and permitted actions |
| `POST /quota-requests/:request_id/decision` | Reviewed `If-Match`, immutable decision UUID, exact step ID and approve/reject/withdraw intent |
| `GET /admin/quota-requests` | Independent `teams.quota_requests.read_all`, read-only global records |
| `GET /admin/quota-requests/:request_id` | Same independent authority; no mutation actions |

Rejection requires a reason; approval comments and withdrawal reasons are optional.
An uncertain decision retains its exact UUID, step, body and validator. A repeated
owner-stage receipt cannot decide a later platform stage. A committed decision is
an immutable historical fact; current runtime application is reported separately
as pending, applied or superseded. A rejected retry does not resolve the original
uncertainty. Changed policy, currency, lifecycle or stage requires explicit review
before a new intent. Fresh decisions, permitted actions, workspace links and effect
previews all require a coherent current request stage and step. History must start
at ordinal one and remain contiguous; a platform second step requires a recorded
approved or system-cancelled owner step. Invalid history cannot bypass owner review.

## Lifecycle and persistence

Frozen GORM V40 adds request records, at most two ordered approval steps and a
pending slot per Team/applicant/dimension. Historical names, submitted context and
final approved policies remain recorded without cascading live-resource foreign
keys. Creation UUIDs, decision UUIDs and ordered steps have portable uniqueness
constraints; pending decisions use nullable UUIDs. The migration seeds only the
independent read-only administrator permission and preserves released V1–V39.

Removing or disabling the applicant membership, disabling the applicant account,
offboarding, or disabling/archiving the Team cancels pending requests in the same
transaction and releases their pending slots. Rejoining or re-enabling cannot
revive them. Owner changes reconcile pending owner stages transactionally without
altering quota policy. Approval rechecks current subject and authority before any
policy change.

## Interface and remaining scope

The existing quota-request workspace uses My requests and Pending approvals tabs,
compact filters and tables, a creation dialog and a detail comparison/timeline
drawer. The platform record workspace is read-only. Its workspace link appears only when
the server confirms a current assigned approve/reject reviewer; terminal records,
applicant withdrawal alone and global read authority do not produce that link.
English and Chinese copy use the `teamRequests` namespace with English as the default. Current-session and
resource-scoped queries must hide stale private rows after authority changes.

This package covers monthly Tokens and money only. Team-assigned roles, default
templates, request-rate/rolling-window applications, notification delivery,
additional Session protocols and distributed enforcement remain separate work.

## Controlled verification

The final source passed `go tool task check` and `go tool task test`: 922 Vitest
cases in 65 files, Go unit/race checks, four Node contract checks, development
process lifecycle and production asset embedding. Independent read-only review
found no remaining concrete defect after the current-stage coherence correction.

A disposable production binary and database exercised the actual English/Chinese
workspace, request submission, owner escalation with unchanged caps (member 5,
Team 10), and the platform confirmation preview and final atomic update to 15/15.
The read-only terminal record had no approval workspace link. Three native calls
used five Tokens each; a fourth received 429 before another upstream dispatch.
An independently restarted binary retained both stage receipts, approved caps and
used 15 Tokens. The original historical owner receipt did not decide the platform
step. Browser console checks were empty, English was restored, and owned processes,
tabs, configuration, journal and Compose resources were removed.

`go tool task test-auth-lifecycle` passed initialization, persisted/revoked Sessions,
controlled ordinary/streaming inference, call facts and independent process restart
on real PostgreSQL and MySQL. The complete real PostgreSQL/MySQL race matrix passed: Handler 704.846 seconds
and Service 6.084 seconds, including V40 repair/history/constraint checks, workflow
authorization, immutable receipts, lifecycle cancellation, malformed-stage denial,
atomic rollback and actual native approved-quota enforcement. These controlled environments do
not establish real-provider, distributed-node, performance or full release acceptance.


The added owner approve/reject race passed on both databases (5.92/5.94 seconds;
focused runner 208.460 seconds). Exactly one decision and audit are retained;
rejection releases the pending slot, while approved overflow retains one platform
stage. Both outcomes preserve aggregate 25/member 20, used 20 and upstream dispatch.
The defined F18 request scope and A13 controlled approval acceptance are complete.
