# Team Resource Limits

This bounded package adds finite Team policies to the existing
[Team Session foundation](TEAM_INFERENCE.md). Source implementation has controlled local acceptance on both real databases,
production assets and the bilingual browser workflow.
Team approval workflows and complete product acceptance remain separate.

## API and authority

All paths below are under `/api/v1`.

| Endpoint | Read boundary | Write boundary |
| --- | --- | --- |
| `GET/PUT /teams/:team_id/limits` | Current active member, global Team reader or dimension writer | Independent dimension permissions |
| `GET/PUT /teams/:team_id/members/:user_id/limits` | Current member's own policy, current owner, global Team reader or dimension writer | Independent dimension permissions |

Every actor and target is validated with exact current identity and enabled,
active lifecycle. Team owners cannot inspect other actors' call history through
these policy APIs. A dimension-only platform operator receives a minimal Team
workspace with `resource_limit_workspace_only:true`, without member/model
directories or global-list access.

## Policy boundary

Aggregate policies cover rolling five-hour/seven-day Tokens, monthly Tokens,
monthly money in the platform denomination, RPM, TPM and concurrency. Member
policies cover monthly Tokens/money and RPM/TPM/concurrency. Null inherits the
parent; zero is a hard cap. Member overrides may only narrow the reviewed parent.
A later aggregate reduction takes effect conjunctively without rewriting saved
member policy or usage. Member allocations do not reserve or expand the shared
Team aggregate.

Direct platform writes require independently granted `teams.tokens.write`,
`teams.money.write` or `teams.rates.write` for the changed dimensions. Current Team
owners receive no implicit direct mutation permission. Current-member/owner scoped
reads and limits-only administrative projection must not expose the global member
or model directory. Separate future owner approvals, platform escalation, default
templates and notification workflows remain unfinished.

## Accounting and review constraints

Persist full stable Team/User policy identity through a new frozen GORM migration;
never use replaceable membership IDs or truncate account hashes. Both accounts
retain the existing Team creation coverage bound through removal, rejoin and
restart. Preserve immutable settlement receipts and require complete windows,
capacity and price evidence before finite admission. Missing historical coverage
is unknown, not a fresh quota reset.

Bind stored/full policy, parent policy, platform currency and current lifecycle
in the reviewed validator. Preserve inaccessible fields, exact decimal values,
explicit conflict review and identical-intent uncertain retries. Only confirmed
runtime application can be reported as enforced.

Team PUT is a sparse policy update, unlike the existing complete-replacement
Personal/Project/Key policy API. Each present field requires its dimension's
current permission, even if its value is unchanged. Omitted fields are preserved;
explicit null clears the local override, and zero closes it. Finite money requires
the current platform currency. Unsupported Team IP controls and member rolling
windows are rejected.

GET returns stored/effective policy, exact decimal amounts, authoritative journal
windows, `platform_currency`, `editable_fields`, and a strong composite ETag.
Member records include `team_id`, the target User ID and `parent_etag`. PUT requires
one quoted strong `If-Match`, current Session/CSRF, same-origin validation and a
reason. Normal conflicts require explicit review while retaining the draft.
Publication uncertainty retains the original complete intent; an identical retry
must recheck current authority and lifecycle, never restore a superseded policy.
Successful application proof checks published revision, full policy, current
member identity, lease, revocation tombstones and monetary denomination.

## Interface and persistence

The addressable Team Limits tab retains Budget and Token quotas plus Request
limits cards. The existing member action menu opens Adjust member resources in a
local Base UI dialog. Independent dimension writers see only supported editable
fields. Read-only owners/members retain stored/effective values, authoritative use,
holds and unknown coverage. English/Chinese changes preserve drafts. Incidental
focus/reconnect events cannot discard original uncertain submissions; fresh
mounts, explicit refresh and dispatch still enforce current authority.

Frozen GORM V39 widens only `resource_limits.scope_id` to 64 characters and seeds
the three independent administrator permissions. The full 52-character stable
Team/User digest and existing journal account bytes are preserved. Released
V1–V38 remain immutable. Repeated or interrupted migration repairs missing seeds
without changing historical policies or resetting use. Member audit facts retain
the stable User ID with separately recorded Team ID.

## Verification and remaining scope

Final `go tool task check` and `go tool task test` passed: pinned backend lint
reported zero issues; 876 Vitest cases in 63 files, Go race/unit, four Node checks,
development lifecycle and production embedding passed. `go tool actionlint` and
real-process `go tool task test-auth-lifecycle` passed on PostgreSQL and MySQL.
The complete actual database race matrix passed (Handler 623.428 seconds, Service
6.559 seconds), including V39 preservation/concurrent/repeat/interrupted recovery,
independent dimension permissions, strict HTTP review, lifecycle/rejoin conflicts,
exact money, single-winner writes, publication outage/retry and native finite
aggregate/member admission. Pure runtime tests cover incomplete coverage, capacity,
unknown use, atomic no-partial-debit admission and revocation/lease boundaries.

Owned embedded-production/PostgreSQL browser proof covered native SSE input 4 /
output 1 with unknown omitted total; child cap 5 and aggregate cap 10 rejected
before dispatch; Personal use stayed independent. English/Chinese limit controls
saved aggregate 25 and member 10 after an independent revision caused 409; explicit
review retained the draft. Membership removal/rejoin preserved the stable policy
and prior use. Restart retained aggregate use 10/member use 5, then one admitted
native call advanced them to 15/10. All owned processes, browser tabs and Compose
resources were removed; the developer service was untouched.

Acceptance corrected one GORM field-name update (`PreviousETag` maps to
`previous_e_tag`), primary-key nullability during width alteration, independent
money patch allocation and denomination-sensitive current-application proof. A
frontend late-response test now explicitly clears the old query before resolving
its held response rather than racing React Query's zero-delay GC timer.

Team-assigned roles remain a separate F06 gap. Team requests/escalation, templates,
notifications, additional Session protocols and distributed enforcement remain
unfinished. No paid upstream, external-provider, production-load or complete
release acceptance is claimed by these controlled fixtures.
