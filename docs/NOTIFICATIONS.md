# Operational alerts and notifications

RouteX turns a bounded set of durable operational failures into grouped alerts and recipient-isolated notifications. Operational sources are failed system jobs, failed Provider credential verification, Provider quality transitions, and terminal route-unavailable calls. Separate quota inbox records observe current-policy monthly settled-use exhaustion; Personal monthly warnings record the configured 80% reminder or 90% critical level from complete known settled usage. RouteX does not create demonstration alerts or infer incidents from browser state.

## Persistence and event model

Frozen migration 29 adds five GORM-managed tables:

- `operational_alerts` groups repeated failures by a server-owned key and records `open`, `handling`, or `resolved` state.
- `operational_alert_occurrences` gives each source fact one immutable occurrence identity, so reconciliation and retries cannot inflate counts.
- `notifications` projects each alert into the inbox of every currently eligible recipient.
- `notification_settings` stores a user's external email and explicit severity choices. In-app delivery is always enabled.
- `notification_delivery_intents` records one immutable SMTP intent per occurrence and recipient.

The migration uses frozen schema structs and GORM migrator APIs on PostgreSQL and MySQL. Restrictive foreign keys preserve historical delivery and alert evidence. No service or handler branches on database dialect.

Frozen migration 30 adds bounded Provider/Model subject snapshots to alerts,
occurrences, inbox projections, and SMTP intents. It also adds the Provider
quality source tables described in [Provider quality](PROVIDER_QUALITY.md).

Source persistence remains authoritative. A failed job, credential verification,
quality window, or terminal route decision remains durable even if notification
publication is temporarily unavailable. The background worker reconciles
missing occurrences from durable source rows and audit facts on later passes.

## Authorization and privacy

Every enabled, non-offboarded authenticated user can open their own inbox; an
ordinary user with no visible records receives HTTP 200 with an empty list.
Inbox operations derive the recipient from the session, and clients cannot
select another recipient. Operational records additionally require current
`system.read`. Personal quota records require the exact personal owner; Project
quota records require an exact current enabled manager of an active Project and
a recorded recipient projection. Platform permissions and Project creation do
not substitute for those quota scopes. Manager removal or Project inactivity
suppresses historical rows from lists, unread counts and read actions. A new
manager receives no historical projection when an observation is replayed.

The same source predicates apply before pagination, counts and updates. Exact
identity/association checks prevent case-folded database aliases from granting
access. Read mutations serialize with governance and reauthorize inside the
transaction; inaccessible notification IDs return HTTP 404. Each source reads
at most `limit + 1` rows, then merges descending timestamp/ID order into one page
of at most 100 records and one shared cursor. Current-manager scope discovery
fails closed beyond 1,000 eligible rows rather than returning a partial inbox.

Only eligible `system.read` users receive operational fanout. The SMTP worker
rechecks that permission before network work and terminates intents for an
ineligible recipient. `system.read` permits the operations overview, alerts,
operational inbox records, and personal delivery-settings read. `system.write` is additionally required to change alert state or save personal external-email settings. Notification payloads contain only allowlisted kinds and detail codes. SMTP credentials, raw upstream errors, arbitrary audit JSON, and another recipient's email are never exposed through notification APIs.

## API

Paths are relative to `/api/v1`. Writes require session authentication, CSRF, same-origin checks and current server-side authority. Settings and alert-state JSON writes reject unknown fields; inbox read actions need no JSON payload.

| Method and path | Permission | Purpose |
| --- | --- | --- |
| `GET /notifications?status=unread\|all` | Enabled current recipient; source-specific authority | List visible owned quota and operational notifications |
| `POST /notifications/{notification_id}/read` | Enabled current recipient; source-specific authority | Mark one currently accessible owned notification read |
| `POST /notifications/read-all` | Enabled current recipient; source-specific authority | Mark only currently accessible owned notifications read |
| `GET /notification-settings` | `system.read` | Read personal delivery settings and revision |
| `PUT /notification-settings` | `system.write` | Save the external address and severity choices with an exact ETag |
| `GET /admin/overview` | `system.read` | Read current-day metrics, the 14-day token trend, Provider readiness and quality, top models, and alerts |
| `GET /admin/alerts` | `system.read` | List grouped operational alerts |
| `PATCH /admin/alerts/{alert_id}` | `system.write` | Move an alert to `open`, `handling`, or `resolved` with an exact ETag |

The overview uses real call, attempt, policy, and catalog facts. Token values stay
decimal strings, unknown token and Provider attribution coverage remain explicit,
and UTC defines the current day. Provider quality reports full-attempt P95 and
threshold state rather than a synthetic score. Provider quality is nullable when
the bounded overview query budget or complete-range limit is exceeded, or when a
persisted policy is invalid; the API returns one fixed reason and the UI renders
paired localized copy. The interface does not invent resource percentages,
thresholds, or example incidents.

## Operational email delivery lifecycle

Email is opt-in. A default settings read may suggest the account email as an editable value, but both severity flags remain disabled until an authorized user explicitly saves settings. An enabled and complete SMTP configuration must also exist when the occurrence is published.

The worker claims due intents with a database lease, performs at most three submissions, and records one of these states:

- `pending`: durable and not yet claimed.
- `retry`: a known pre-acceptance failure can be tried again after bounded backoff.
- `sending`: currently leased to a worker.
- `accepted`: the configured relay accepted DATA. This does not prove inbox delivery.
- `failed`: a known terminal failure, exhausted retry budget, changed configuration, or ineligible recipient.
- `unknown`: the process cannot prove whether the relay accepted the message, including an expired `sending` lease. Unknown intents are never retried automatically.

Each intent is bound to the exact saved SMTP ETag and external recipient address captured at publication time. A later transport change terminates the old intent instead of silently sending through a different configuration. Message subject and body are server-owned and bounded. They contain only the alert kind, severity, allowlisted detail code, and an optional validated Provider or Model subject snapshot.

Bounce processing and recipient-inbox tracking are outside the current boundary. External SMTP acceptance also remains a separate environment-backed acceptance step.

## Interface and localization

The application header contains the Mockup-aligned notification menu with unread
and complete history views plus cursor-based loading. `/admin/overview` preserves
the approved four summary cards, trend/readiness row, top-models/alerts row,
alert details, and settings dialog. Alert details show category and affected
scope. High- and medium-severity email choices are independent and both can be
disabled with an empty external address. The implementation uses local
shadcn-style primitives and Base UI wrappers.

All visible and accessible copy uses the `notifications` i18next namespace with paired English and Chinese catalogs. English remains the default. Operators with only `system.read` can inspect data but cannot see settings or alert-state actions.

## Verification boundary

Focused service and handler tests cover source deduplication, quality transitions,
route-unavailable grouping, subject snapshots, recipient isolation, permission
changes, independent severity choices, settings conflicts, safe payloads,
bounded retries, uncertain acceptance, expired leases, and source
reconciliation. Migration tests cover empty creation, existing-data upgrade,
repeat execution, concurrent startup, restrictive foreign keys, partial
recovery, and no seeded data on PostgreSQL and MySQL. Frontend tests cover the
approved hierarchy, history pagination, read/write gates, scoped query keys,
read actions, conflict review, quality coverage, and live English/Chinese
switching.

Controlled SMTP fixtures prove application delivery behavior without contacting an external mail service. Production relay acceptance, bounce handling, and inbox delivery remain unverified.

## Monthly aggregate exhaustion inbox

The observer reads current applied-policy/current-month exhaustion for Personal
and Project aggregate accounts. Journal-settled tokens or exact monetary amounts
must have complete coverage since the later of the resource creation time and
the current month start. The journal supplies observation time, time zone and
calendar boundaries; unknown usage in the observed dimension, incomplete
coverage and mismatched currencies cannot establish exhaustion. Monetary evidence requires the policy,
platform denomination and all settled currency groups to match. A fresh runtime
authority must attest the exact saved policy revision before publication.

Holds remain reservations rather than settled usage. Null means unlimited and
produces no notice; a finite zero limit can establish a covered limit-reached
observation without fabricating spend. No threshold crossing, historical
backfill, 80%/90% warning, Team/Key aggregate notice, SMTP delivery or additional
admission/stop-calling policy is introduced. The background observer scans finite
monthly policies in bounded keyset batches of 32, advancing past ordinary skips
and wrapping after the complete scope range so later accounts are not starved.

Frozen GORM migration 35 adds `quota_notification_observations` and
`quota_notification_inboxes`. It preserves the existing operational tables and
restrictive `notifications.alert_id` foreign key. An observation freezes scope,
policy revision, month boundaries, time zone, observation time, limit, settled
amount, currency and coverage basis; a Project name is a validated immutable
snapshot. Observation and current eligible recipient projections commit
atomically. Identity is deduplicated by scope, dimension, month, policy revision
and currency. Replays never change a snapshot, reset read state or add recipients;
a new current policy revision or month has a separate identity. No current
usage or current policy claim is inferred from historical inbox records.

Quota records use `kind: monthly_quota_exhausted`, `severity: high`, and
`detail_code: tokens_month_exhausted` or `money_month_exhausted`. They omit
`alert_id` and operational delivery fields, and add `quota_observation_id` plus a
`quota` object containing `scope_kind`, `scope_id`, `dimension`,
`policy_revision`, `month_start`, `month_end`, `time_zone`, `as_of`, `limit`,
`settled` and `currency`. Amounts and revisions are strings; token currency is
null, while money retains its exact recorded currency. Scope is `user` or
`project`, with optional frozen Project subject metadata. Quota facts never fan
out to platform operators or create operational alerts or SMTP intents.

The existing bell menu retains its layout, English-default bilingual copy and
recipient/status cache keys for all signed-in users. Recorded values remain
exact strings, and month/currency come from the server. Cached notices are hidden
during refresh or errors; removed authority cannot be repaired by stale data.
Operational settings retain their independent permission gates.

Focused Go race tests and staticcheck passed. The isolated PostgreSQL/MySQL race
suite passed in 173.349 seconds, covering all migration prefixes through V35,
empty/upgrade/repeat/concurrent/partial schema paths, native settled evidence,
held and unknown usage exclusions, zero versus unlimited, new revisions, exact
money, deduplication, current/removed/new managers, frozen scope snapshots,
merged pagination, case-aliased authority, read isolation, restart and a
single-connection pool. Final format/check/test passed (724 Vitest cases in 56 files plus Go/Node,
development and production checks). The disposable production browser passed
Personal/Project settlement snapshots, English/Chinese switching and revoked
Project authority refresh. Its owned resources were removed. The complete PostgreSQL/MySQL race matrix passed (Handler 499.925 seconds,
Service 5.851 seconds), and its owned containers/network were removed; external mail and broader quota/enterprise alert
acceptance remain open.


## Team monthly settled exhaustion

Team aggregate monthly Tokens and money use the same conservative settled-fact
observer. It freezes a current-policy observation and the then-current enabled
owner/member recipients, bounded to 1000 identities; an overflow cannot become
a partial recipient list. This aggregate notice does not fan out another member's
stable Team/User quota facts. Private child notices are defined separately below;
no Team Key or Personal quota is introduced.

Inbox/count/read actions require current exact enabled membership and an active
Team; a platform administrator has no implicit member access. Removal hides
recorded private notices without rewriting read state. Rejoin restores that
original state; a later member receives no old observation during worker replay.
Recorded subject names, policy revision, month/timezone, settled/limit strings and
currency remain immutable even after live Team changes. Unknown coverage, holds,
partial calendar coverage and unconfirmed application never prove exhaustion.

The existing notification menu renders paired English/Chinese Team snapshot copy.
Every successful Session network generation reauthorizes rows, unread count and
read intents, including structurally equal same-millisecond responses. Renewing
or failed reads hide old private facts; late prior-generation results cannot
restore them. Manual same-actor CSRF cache replacement preserves valid state.

Frozen GORM V47 expands only the observation scope check after installing the
new guard; released V35 remains immutable. Existing observations, deduplication
and historical recipients survive repeat/partial-DDL repair. Current-main source
checks and controlled bilingual production acceptance passed, including exact
membership removal/rejoin and persisted read state after real restart. The complete
current-main PostgreSQL/MySQL regression passed under race detection
(Handler1181.133s, Service7.983s), with owned matrix resources removed. Final
mandatory `go tool task check` passed. Source, fixtures and documentation are
delivered as one scoped main package; inspect its commit and remote checks.


## Private Team member monthly exhaustion

A member's stored child monthly cap is observed independently from Team aggregate
limits. Only the exact current enabled, nonoffboarded active member/owner receives
their own child event. An owner is eligible for their own child account only;
other members, administrators and operators gain no implicit private recipient
access. Parent and child balances are never summed or flattened into an allowance.
An exhausted child can have a notice while its Team aggregate remains below cap.

The existing Session endpoints GET `/api/v1/notifications`, POST
`/api/v1/notifications/:id/read` and POST `/api/v1/notifications/read-all` are
unchanged, including CSRF and same-origin mutation guards. New snapshots retain
kind `monthly_quota_exhausted`, severity `high` and the existing token/money detail
codes. `subject_type` and `quota.scope_kind` are `team_member`; `subject_id` and
`quota.scope_id` contain the same stable 52-character pair digest. Frozen
`quota.team_id` and `quota.member_user_id` identify the exact Team and recipient.
The recorded subject name is the validated Team name; aggregate snapshots omit
these new proof fields. Policy revision, limit, settled amount, currency, month
boundaries, timezone and observation time remain immutable exact-string facts.

List, unread count, read-one and mark-all require original recipient ownership
plus current exact active Team/User membership and matching frozen pair proofs.
Removal, disable, archive or offboarding hides the original notice. Same-user
rejoin may restore its original read state; replay never adds recipients, changes
the snapshot or resets read state. That current inbox entitlement does not revive
an old captured native membership or change the stable child accounting identity.

Qualification requires known settled usage in the observed dimension, complete
monthly coverage since the later of Team creation/month start, and exact current
applied child and parent policies, calendar revision/timezone, currency and
published membership/lease/tombstone proof. Unknown or uncovered usage, holds,
stale/unpublished policy and expired authority do not prove exhaustion. Null is
unlimited; covered known zero with a finite zero cap remains valid evidence.
Native finality does not introduce a new settlement gate or alter accounting.
Observation and the sole self-recipient inbox insertion commit atomically.

The worker has an independent 32-membership keyset cursor and batched child-policy
lookup; aggregate scanning is unchanged. Skips and failures advance rather than
starving later rows. Explicit reconciliation completes both full cursor cycles;
background ticks advance one page of each. Fresh SQL subject checks and private
publication proof are repeated before commit, with no network under locks.

The existing bilingual menu labels this as your member quota in the recorded
Team, distinct from aggregate exhaustion. It validates the digest and exact
recipient/Team proofs before using the frozen name or Team ID fallback. Successful
Session network generations reauthorize rows/count/read intents; manual same-actor
CSRF replacement retains its existing exception, and late/foreign responses fail
closed. No member directory, new permission or new endpoint is added.

V51 preserves released V35/V47 migrations and historical aggregate notices.
This 24-path integrated candidate passed source format/check/test/build with
2094 Vitest cases in 110 files, Go race/unit, Node lifecycle and embedded
production asset checks. The production binary SHA256 is
`dbc326ef0554c1a41498e0465987c7a950abff50b0e69f8997de89262c1360e7`.

The corrected revision-5 PostgreSQL/MySQL race focus passed (Handler 98.815s).
Both V51 migrations and lifecycle cases passed, including exact 30-byte overflow
canonicalization and fixed five-native assertions on each driver. All 81 source
protections stayed exact; owned focus resources were removed. Earlier fixture
failures and their isolated repairs remain in [Implementation](IMPLEMENTATION.md).

The separate controlled production/browser scenario passed using the unchanged
binary and synthetic USD rates restricted to test setup. Three actual native
requests covered warmup, caller and peer. Settled Team usage was 10 against
aggregate caps of 100; caller usage/caps were 5/5 and peer usage/caps 5/20 for
both Tokens and money. Exactly two child notices belonged to the caller; owner,
peer, administrator and new member received none. Finite denial added no dispatch.

English and reopened Chinese menus passed. A genuine token-notice click produced
one read and one unread record, with two all-history rows. After removal, real
Session/list reads showed empty history; rejoin created a new membership while
restoring original notice IDs/read state, without historical fanout. Owner/peer
browser histories stayed empty. Same-binary restart preserved Sessions, notices,
read state and immutable facts without inference replay; post-restart browser
Session/list reads passed. The observer recorded 10 browser Session HTTP 200s
and 13 list HTTP 200s, not proof of a minute automatic-renewal interval. Console
errors/warnings were zero. All scenario processes, proxy, Compose resources,
ports and temporary tab were removed. Helper SHA256:
`6506641b175a1527407e43ae6f590f5891dc60165678e5a40c4e6dc2185e1103`.

The complete unchanged-source PostgreSQL/MySQL race matrix passed Handler
1456.730s and Service 8.504s. All 81 protected source hashes and package/lock
bytes stayed exact; runner and coordinator independently verified that every
owned matrix container, network and volume was removed. Local controlled
acceptance and final mandatory `go tool task check` passed. The checked 30-path
phase is committed/pushed as `5363d3ce53ca4d1248227b2f941aae642c433499`, with
matching remote main read-back and clean main. Its distinct CI, Actionlint and
GolangCI-Lint are still in progress; remote success is not inferred. Source,
corrected focus, production/browser and full matrix remain distinct evidence.
SMTP, warning thresholds, configurable stop policy and broader F17/F23 stay open.

## Personal monthly warning integration V59

The existing Session-scoped inbox now has a separate Personal-only warning
observer. Positive finite stored monthly Tokens or money limits may produce
`near` at80% or `critical` at90%, using arbitrary-precision integer/rational
comparison. The highest eligible current level is recorded; this is neither a
historical crossing claim nor a forecast. At or above100%, only the existing
exhaustion workflow applies. Null/zero limits do not generate these warnings.
Unknown dimension usage, uncovered history, mismatched monetary denomination or
unpublished policy/calendar/recipient authority suppress observations. Holds
remain reservations and are never counted as settled usage. Gateway admission
and its existing hard-stop behavior are unchanged.

Frozen GORM V59 adds `quota_warning_observations` and `quota_warning_inboxes`,
without changing released exhaustion migrations or operational/SMTP tables.
Observation and exact owner projection commit together under current governance
authority. Identity binds owner creation, dimension, month, policy revision,
currency, level and `personal-monthly-80-90-v1`. Repeated reconciliation preserves
the original snapshot, identity and read state. List/count/mark/all require the
exact currently admitted owner and matching recorded creation identity; a
recreated account cannot inherit historical notices. Creation identity is private.
The worker scans bounded32-account batches with an independent warning cursor.

The wire kind is `monthly_quota_warning`; severity is medium/near or high/critical.
It adds `quota_warning_observation_id` and typed `quota_warning`, with exact user
scope, dimension, saved policy revision, server month/calendar/as-of, decimal
limit/settled strings, nullable token currency, level, threshold and generation.
It omits exhaustion `quota`, operational alert/delivery fields and private birth
metadata. The existing notification menu validates this schema, renders unknown
snapshots explicitly and shows paired English/Chinese recorded facts. It computes
neither remaining allowance nor a current percentage. Recipient/session cache
gates, history and read actions remain unchanged.

### Personal warning acceptance

Focused PostgreSQL/MySQL acceptance passed eight selected scenarios and 128
named events. Complete race-enabled regression passed 107 ordered scenarios per
driver, eight additional constraint cases and 3,361 named PASS events,
with no named failures or skips. PostgreSQL took 904.15s;
MySQL took 1169.90s. The original 105-scenario prefix and
all 1,520 protected source paths remained exact. Owned Compose containers,
networks and volumes are independently absent.

Full log SHA256: `d62c54939fae35c1a9a1bafe1d826f03146d111b7a9e1b8fca7c942e240b3148`.
Focused log SHA256: `604401ae11e39c6e0e40b6732edd3cda1c85c41a9f1067ccec92d0fdee87bf30`.

Controlled PostgreSQL production and bilingual browser acceptance passed against
binary `8af28a7b8ecfd13974b4db15017e92da681308fb3df641445311bc0431e65bc9`.
Six completed native calls and attempts produced four Personal warnings and no
exhaustion or operational/SMTP fanout. Recorded Tokens 8/10 and 9/10 and USD
9/11.25 and 9/10 appeared in default English and live Chinese. Single read
returned HTTP 200; read-all returned 204 and retained all historical rows.
The same binary/configuration/database/journal and original Sessions survived
process restart without additional inference or login. Immutable call/attempt,
warning and read-state facts were unchanged; administrator/other-account reads
remained isolated. Escape restored trigger focus, console errors/warnings were
empty, and owned tabs/listeners/Compose resources are independently absent.
MySQL production browser verification is not claimed; dual-driver native and
migration acceptance is recorded separately.

Formatting, mandatory checks, Go race source tests, pinned backend lint, 3,169
frontend cases in 151 files, four Node checks, production build and embedded
asset tests passed. Final mandatory checking also passed with no errors and two
existing Fast Refresh warnings; delivery is tracked in Git history.
Team aggregate, Project, private Team-member and Key warnings, SMTP and
configurable thresholds remain outside this Personal slice. F17/F23 and formal
11 complete / 16 partial / 3 unstarted totals remain unchanged.

### Historical failed attempts and corrections

Focused R1 failed test oracles for mixed warning/exhaustion shape, literal
`etag` instead of GORM field `ETag`, and timestamp representation equality.
Log SHA256: `3123ca24aa0de78534bf0d5290e7577b144f5644ff70bf88cd3738bc498ffb1b`.
R4 preserves every assertion with exact persisted policy restoration,
`time.Equal` timestamp instants and a genuinely persisted 1ms User birth change.
Focused R2 failed the missing-native-usage money oracle.
Log SHA256: `81a221dec63238df8bf1edc7efb0d81cb2da9046b9c31ba5865e357b517e5165`.
R5 retains TokensHeld=2, no MoneyHeld and MoneyUnknown=1: money was unconstrained
at admission, and later policy changes cannot rewrite the original receipt.
Runtime, UI, V59, eight native calls, seven warning observations and three
exhaustion observations stayed unchanged. Earlier failed runs remain failed;
the successor's actual pass does not reclassify them.

Two failed private production helpers are separate: absent price 404 before
creation, and a binding variable overwritten after near/critical. Successors
review the existing filtered price list and retain a distinct immutable binding;
product source is unchanged. The finite full-suite deadline is 40 minutes,
based on the measured 2,015.362-second prior matrix and added scenarios.
Individual query/request/readiness bounds and race/assertion coverage are unchanged.

## Team aggregate monthly warnings V60

Team warnings use the user-selected fixed 80% reminder and 90% critical levels
for complete known settled monthly Tokens and exact decimal money. At 100%,
the existing exhaustion observer and hard-stop policy remain responsible.
Unknown usage, holds, incomplete coverage or mismatched current policy, calendar,
currency or runtime authority produce no percentage. No SMTP, permission or
admission change is included.

Frozen GORM V60 adds private Team observations and recipient inboxes without
changing released migrations. Creation atomically records exact Team/User births
and the bounded original owner/member recipient set. Replays preserve recorded
names, recipients and read state. Reads and marks require the original admitted
recipient, current active membership and matching Team/User incarnation; leaving
hides history and rejoining restores the original history. Later membership and
administrator privilege never expand historical access.

The existing notification menu and its request/controller boundaries retain their
layout, paired English/Chinese recorded facts and independent read state. Missing
recorded names fall back to exact IDs without fetching an unauthorized directory.

### Acceptance

The phase follows checked/pushed Personal warnings `38de94f`. V60 is registered;
scenarios 108–109 append to the unchanged 107 prefix. The current targeted Team
native lifecycles passed on both databases with 231 named events;
the preceding R3 separately passed the other 14 selected driver cases, including
both V60 migrations and the strict mixed-exhaustion workflow.
Full regression passed 109 ordered scenarios per PostgreSQL/MySQL, eight
constraint cases and 3,526 named PASS events without named failures
or skips. All 1,533 protected paths remained exact; owned Compose resources are
independently absent. PostgreSQL took 967.37s; MySQL took
1290.15s. Full log SHA256:
`648a88a9a5c264080f42179f4a8ed427712b1855c71950a15982a36750696409`.
Focused log SHA256: `ee8c285ce3733a6373b38335d63daa025a54b93d98a44cc88f5f2bf6233e0537`.

Controlled PostgreSQL production and bilingual browser acceptance passed against
binary `957ef673e5e71ec068ed7d6c57e7fcbf5466a2d0327037db4ff0a0b41c357cb0`.
Six completed native calls/attempts produced four observations and eight
original-recipient inboxes, with no Personal warning, exhaustion or SMTP fanout.
Tokens 8/10 and 9/10 and USD 9/11.25 and 9/10 appeared in default English and live
Chinese. One recipient's HTTP 200 single-read preserved the other's unread state;
both HTTP 204 read-all operations retained history. Removal hid the member's
history; rejoining restored it and later members received no old notifications.
The same artifact/configuration/database/journal and original Sessions survived
restart without inference or login, preserving calls, attempt attribution,
snapshots and all read state. Escape restored trigger focus, console errors and
warnings were empty, and owned tabs/listeners/Compose resources are independently
absent. MySQL browser acceptance is not claimed; dual-driver migration/native
acceptance is separate.

Go source race tests, 3,225 frontend cases in 151 files, four Node checks, pinned
backend lint, formatting, mandatory checks, production build and embedded asset
tests passed. The mandatory pre-commit gate is `go tool task check`; scoped English main
commits and pushes record delivery. Project, private Team-member/Key warnings, configurable thresholds,
SMTP and Restore recovery remain separate. F17/F23 and formal 11/16/3 remain
unchanged; the full implementation objective continues.

### Retained failed runs

Focused R1 failed the root fixture's old ledger 59 expectation after V60 correctly
created 60; R2 failed test-only mixed-inbox classification, null-money currency
input and timestamp representation; R3 failed a 33-character synthetic ID against
the unchanged 30-character schema. Narrow test corrections preserve all history,
native, permission and negative assertions. Their failed log SHA256 values are
`97005cee07c84a9f29669ecaa29b786847379f9e30c6bbed2b89ad8b8d426df0`,
`3ad6923a5d237833b94cf96abf2d6136f109b8560c155cf94c200b6b45edd70c` and
`32fa3fa535c60abb26c840bf31dd11cf3dc203a6dfb0f59744b1f23b92081e38`.
The first production run stopped before membership/restart because its private
helper required an omitted optional terminal `next_cursor`. A one-line helper
correction retains exact four unique IDs and complete pagination assertions;
fresh R2 production passed. Earlier failures remain failed and their owned
resources are independently absent. The initial check tooling collision was
resolved by running the pinned linter without a concurrent second instance;
subsequent mandatory checking passed without product changes.

## Project monthly warning integration V61

Project warnings record fixed 80% reminders and 90% critical observations for
complete settled monthly Tokens and exact decimal money. Frozen GORM V61 keeps
the original admitted manager recipients and exact Project/User births. Current
management gates reads/marks; leaving hides history, same-incarnation rejoining
restores it, and later managers receive no old rows. Creator or administrator
privilege grants no notification access. Unknown usage, holds, incomplete coverage
and stale policy/calendar/currency/runtime facts produce no percentage; existing
exhaustion, hard stops and SMTP behavior remain unchanged.

The existing notification menu retains paired English/Chinese facts and
independent recipient read state. Recorded names fall back to stable IDs without
a directory query. Public metadata excludes private births and Key secrets.

Focused PostgreSQL/MySQL acceptance passed 20 selected cases/105 events; core
source checks passed 33 cases/349 events. The exact worktree source passed 3,286
frontend cases in 151 files, four Node checks, production build and embedded
assets. Main carries those same runtime/UI/fixture afterimages; its mandatory
check and current production build/assets passed. The complete 111-scenario
matrix passed 111 ordered cases per PostgreSQL/MySQL, eight constraints and
3,697 named events without failures or skips, preserving the original 109 prefix.
All 1,546 protected paths stayed exact. PostgreSQL took 1036.51s and MySQL
1262.62s; owned containers, networks and volumes are independently absent.
Full log SHA256:
`a3713d931969681838c771c4234519fddcf912bb06592495e0e64d53e159c5e6`.

Controlled current-main PostgreSQL production and bilingual browser acceptance
passed against binary `83ec876354abbdadd8b75a8e4fd908de71f1976a441809089cea457b788ef48c`.
Six completed native calls/attempts produced four Project observations and eight
original-manager inboxes. Tokens 8/10 and 9/10 and exact USD 9/11.25 and 9/10
appeared in default English and live Chinese. Single-read HTTP 200 changed only
its recipient; both read-all HTTP 204 operations preserved history. Manager
removal hid history, rejoining restored it, and later managers received no old
notifications. The same artifact/configuration/database/journal and original
API/browser Sessions survived restart without more inference or login, retaining
immutable call attribution and read state. Console warnings/errors were empty,
Escape restored trigger focus, and owned tabs/listeners/Compose resources are
independently absent. This is controlled PostgreSQL proof; MySQL browser and
external Provider/SMTP acceptance are not claimed. Final mandatory checking and complete Task testing passed, including Go race,
3,286 frontend cases, four Node checks, two development lifecycle checks and
production build/embedded assets. The containing commit records this phase. F17/F23 and formal 11/16/3 are
unchanged; the full implementation objective continues.

## Private Team-member monthly warning integration V62

Private member warnings use the approved fixed 80% reminder and 90% critical
thresholds on complete settled monthly Tokens and exact decimal money. Their
scope is the stable Team/User pair, independent of a replaced Membership row.
Frozen GORM V62 creates immutable observations and original-recipient inboxes,
with separate exact Team and User birth proofs. Current published parent and
member policies, membership, currency, calendar and settled journal coverage
must agree. Holds, unknown usage and stale authority never produce percentages.
No owner, administrator or peer receives a member's warning; removal hides
history and same-identity rejoining restores the original read state. The existing
hard stop, exhaustion notification and SMTP contracts remain unchanged.

Core race tests passed eight top-level tests/111 named events. Corrected fixture
source tests passed five top-level tests/15 named events. Actual isolated
PostgreSQL/MySQL focus passed all six selected lifecycles and 47 named events,
without failures, skips or race reports; PostgreSQL took 68.16s and MySQL 91.01s.
All 1,559 protected source paths and semantic staged identities remained exact;
owned containers, networks and volumes are independently absent. Earlier R1/R2
fixture runs remain failed: an unused builder sent unsupported policy fields;
the corrected fixture now uses the real presence-aware decoder. No production
server contract was relaxed.

The identical runtime/UI source passed complete Task testing: Go race/coverage,
3,365 frontend cases in151 files, four Node checks, two development lifecycle
checks and production assets. Main mandatory checking and production build
passed; a test-only scope initializer cleanup preserves the exact identity.
The finite45-minute aggregate integration bound reflects the measured2,299.13s
predecessor; per-query/request/readiness deadlines and assertions are unchanged.

Controlled current-main PostgreSQL production and bilingual browser acceptance
passed: six native completions/attempts, four immutable observations/four
sole-recipient inboxes, recorded Tokens8/10 and9/10 plus USD9/11.25 and9/10,
owner/peer/admin isolation, removal/rejoin history, single-read200/read-all204
and identical artifact/configuration/database/journal/original Sessions after
restart without additional login or inference. Binary SHA256:
`7fbdaa881f1c6957bb0b3e457cf763b07590b2dac0937192c1f8ad9562fb6375`.
All1,559 main source paths stayed exact; owned tabs/listeners/Compose resources
are absent. MySQL browser acceptance is not claimed. Two prior helper failures
remain failed; source-only corrections removed an impossible public field
expectation and used actual fanout table names before fresh complete acceptance.

Full113 R1 failed despite PostgreSQL113 passing: MySQL's existing Personal-warning
unpublished fixture returned runtime-unavailable at line413. All new Member
cases passed;3,858 named PASS events, no skips/race reports and exact source/index
are recorded independently from the failure. Log SHA256:
`e948286d79ee24e2346551314ee1ff5bf5250fd7bef114a757d6fe38e17f7392`.
The stopped-publication fixture has a five-second lease and multiple sequential
negative segments; expiration between observation fences is a source-supported
explanation, not an instrumented historical branch proof. A narrow successor
refreshes valid baselines before deliberate mutations, preserving all assertions
and production deadlines. Fresh focus passed eight selected Personal/Member
migration/lifecycle cases and71 named events on both drivers, with no failures,
skips or race reports (PostgreSQL66.07s, MySQL104.29s). Log SHA256:
`ac9b3537532e43a4d49f01b539988db48ba59712d30641f334e8360dcf53e3e8`.
All1,559 worktree paths/index stayed exact and owned resources are absent. Main
carries the same minimal fixture and passed final mandatory checking. Corrected
full113 R2 passed3,861 named run/pass events and eight constraints with no
failures/skips/race reports: PostgreSQL1,019.91s, MySQL1,443.51s and total
2,494.711s. All1,559 source paths and semantic index identities stayed exact;
owned containers/networks/volumes are independently absent. Log SHA256:
`d56436dcdce3bd334a763c5e62555bf416ddc932894aa29bf2da0c191ed9c92e`.
A narrow text-output reader correction recognizes Go summary ordering; earlier
reader rejections remain recorded and no test/source/resource changed for it.
Main retains an assertion-equivalent Member fixture initializer lint correction
outside the frozen worktree; its focused pure race and final mandatory checks
passed. The containing commit records this phase. F17/F23 and formal11/16/3
remain unchanged.

## Personal and Project Key monthly warnings

Personal and Project Keys share monthly quota accounts across their recorded
rotation lineage. The recipient menu displays the original root Key name or
stable ID, never secret material or current inferred allowance. Complete known
settled Tokens and exact decimal money produce immutable observations at the
approved 80% reminder and 90% critical thresholds. Unknown coverage, holds,
unpublished policy generations and mismatched resource births do not produce
estimated warning percentages. Existing exhaustion and hard-stop behavior stays
separate; these warnings do not add SMTP delivery.

Personal observations use `kwo_` and original-owner inboxes use `kwi_` with
`personal-key-monthly-80-90-v1`. Revoked predecessors retain authorized original
owner history while eligible successors share the same root quota account.
Project observations use `jwo_` and separate original-manager inboxes use `jwi_`
with `project-key-monthly-80-90-v1`. Creator or administrator attribution grants
no recipient access. Current admitted original managers keep independent read
state; removal hides history, same-birth rejoining restores it, and later
managers receive no historical backfill. Public records exclude private birth,
manager, credential and lineage proofs.

Frozen GORM V64/V65 and scenarios 116–119 extend the unchanged 115-case prefix.
Focused PostgreSQL/MySQL R8 passed 16 direct cases and 87 named events without
failures or skips; owned resources are independently absent. The complete119
matrix passed against its protected candidate source: 4,370 named events and
eight constraints, without failures or skips. Current main carries
the exact Key outputs while preserving delivered default-rule drafts, submitted
intent ownership and strict Team timestamps. All 947 Go files match the candidate
except one proven two-line V62 release-status comment; scripts, dependencies and
Compose descriptors are exact. This mapping does not claim that the matrix ran
on the later main source floor. Main mandatory checking, complete Task (3,714 frontend cases/157 files),
production build and both real-process authentication/gateway lifecycle gates
passed. Controlled Key native/bilingual original-Session browser acceptance
also passed as recorded below; the containing commit records this checked phase.


### Complete Key119 acceptance

The isolated V64/V65 candidate passed 119 ordered scenarios per driver, eight
constraints and 4,370 named RUN/PASS events, without failures/skips/race reports.
PostgreSQL took 1,201.57s, MySQL 1,485.34s and the whole command 2,770.780s.
All 1,616 protected source paths and the semantic index stayed exact; owned
containers/networks/volumes are independently absent. Log SHA256:
`01d4ae20460d3e9606e795afb9ab321b2a4162dffffb3804eaeda1896be39dfb`.
This matrix ran on the original isolated candidate; current main inherits its
compiled backend through the separately verified comment-only equivalence.
Main check, complete Task (3,714 frontend cases/157 files), production build and
both real-process authentication/gateway lifecycle gates passed. Controlled Key
production/bilingual/original-Session restart acceptance also passed as recorded below.

### Controlled Key production acceptance

The final production artifact
`ce55e0d3e30e3b3e1f88bfec8e0d1ff375c45477f9aaa19c07a814aba875b8ed`
passed two separate PostgreSQL environments on the reviewed 1,617-path main
source floor. Personal acceptance recorded six native calls, five observations
and five original-owner inbox rows. Project acceptance recorded six native
calls, five observations and ten independently read original-manager inbox rows.
Normal browser sign-in, default English/live Chinese, original rotation-root
labels, exact decimal amounts, single-read200/read-all204, recipient isolation
and Escape/focus passed. All seven original browser Sessions reread after an
identical-artifact/config/database/journal process restart without reloading
those authenticated documents, logging in again or replaying inference.

The Project API acceptance separately verified removal/rejoining and no late
manager backfill. Its ten-second disabled interval confirms no new or changed
observations during that interval; it does not prove worker invocation. Unknown
coverage, holds, currency mismatches and hard stops remain dual-driver matrix
evidence rather than claimed browser scenarios. Owned tabs, listeners and both
Compose projects' containers/networks/volumes are independently absent. The
combined result is 12 native calls/attempts, ten observations and 15 inbox rows.

Project Key warning titles separate Chinese prose from the English resource
name consistently. This presentation correction changes no recorded warning
or recipient behavior.

The copy correction passed 145 focused notification/localization cases, the
complete 3,714-case frontend suite across 157 files, formatting and mandatory
checking. The initial five stale title expectations remain a failed checkpoint;
they were updated without changing behavior assertions.

## Personal rolling Token warnings

The initial rolling warning scope is a User's own stored five-hour and seven-day
Token caps. A bounded observer uses the authoritative settled journal counters
and the account's original registered birth. It requires the exact current User
birth and admission state, complete coverage since the later of window start or
User creation, no unknown Token usage in that window, and current runtime
application of the stored cap and calendar. Reservations are not added to
settled usage. Null and zero caps have no percentage denominator; zero remains
an enforced admission limit. This observer changes no admission or quota policy.

Frozen GORM V81 introduces current episode states, immutable rolling observations
and recipient inboxes. Episodes are sampled, not reconstructed crossing history.
Each window emits at most one 80% reminder and one 90% critical observation per
episode. A first sample at or above 90%, including 100% or higher, emits only
critical and suppresses a later lower
reminder in that episode. A fully covered known sample below 80% rearms the
window. Unknown or incomplete coverage preserves the prior state; elapsed time
alone never resets it. At or above 100% no additional exhaustion event is emitted.

A sampled change to the actual monitored cap or a new explicit default-reset
review starts a new episode under fresh runtime application proof. The reset
lineage survives subsequent ordinary full-policy saves that clear default
provenance. Unrelated reason, money, rate or IP edits do not rearm an unchanged
cap. Policy transitions never sampled by the observer are not reconstructed.
The state, observation and recipient row commit in one transaction; a failed
final application recheck rolls back the entire observation.

The wire kind is `personal_rolling_quota_warning`, with
`rolling_quota_warning_observation_id` and `rolling_quota_warning`. Its immutable
snapshot includes exact User scope, episode, applied policy revision, window kind
(`5h` or `7d`), window boundaries, sample and coverage times, resource birth,
time zone, integer Token cap and settled strings, level, threshold and generation.
The window ends at the sample time. The in-app menu renders those recorded facts
in English or Chinese without estimating remaining allowance. Inbox reads and
read mutations reauthorize the current recipient and exact User birth. Email
settings remain for their existing operational sources; this slice adds no mail.

Inherited caps, Team member and Project Key rolling warnings remain outside
this initial User scope. The separate Personal Key, Project aggregate and Team
aggregate candidates are described below. Monthly producers and their immutable
observations remain independent. Final dual-driver/full-matrix and controlled
native/API/restart acceptance are recorded in [Implementation](IMPLEMENTATION.md).
The containing commit delivers this bounded scope; genuine browser acceptance,
external mail and broader rolling-warning sources remain open.

## Personal Key rolling Token warnings

The composed V82 candidate samples only positive stored Personal root-Key
five-hour/seven-day Token caps. Frozen GORM V82 adds independent episode-state,
immutable-observation and recipient-inbox tables; released migrations remain
unchanged.
The monthly Key proof is reused for exact owner/root births, bounded retained
rotation graph, shared root account and currently enabled nonexpired descendant.
An unrelated Project Key with the same ID never qualifies. Reads preserve the
recorded root name and exact birth-scoped history, including a revoked retained
root; new observations still require a live eligible descendant and applied runtime
policy/calendar. Thresholds and sampled episode semantics match Personal rolling
warnings: 80% near, 90% critical, first exhausted sample critical only, fully known
below-80% sample rearms, and changed stored cap starts a new episode. General ETag
or reason edits do not reset an episode. Keys have no creation-default/reset API.

Finite holds are not settled usage and do not suppress a fully covered settled
warning; unknown usage prevents sampling and rearming. Null/zero caps have no
denominator. State, immutable observation and recipient inbox persist atomically
under a final applied-proof check. The wire kind is
`personal_key_rolling_quota_warning`, with its own observation ID and exact original
owner/root birth. The existing English/Chinese menu renders recorded counters and
windows only; names retain up to 100 trimmed Unicode code points. Historical reads,
merged paging and read mutations remain recipient scoped. This candidate changes
no admission, monthly warning, external mail or layout behavior. Current source
and remaining real-database/delivery gates are tracked in
[Implementation](IMPLEMENTATION.md).

## Project aggregate rolling Token warnings

The composed V83 candidate samples an active Project's own stored five-hour and
seven-day Token caps against the exact registered `project:<id>` journal account
and Project birth. It does not sample Project Key or manager Personal accounts.
Complete known coverage begins at the later of the window start or resource
birth; held reservations are separate from settled Tokens. Unknown usage,
incomplete coverage or unavailable application proof cannot emit or rearm a
warning. Null and zero caps have no percentage denominator; zero remains an
admission limit. No inherited default is materialized by observation.

The sampled 80%/90% episode rules are shared with Personal rolling warnings. A
first covered sample at or above 90%, including 100% or higher, emits critical
only. Known settled usage below 80%, a sampled monitored-cap change or a new
explicit creation-default reset review may rearm. Ordinary reason, revision,
money, rate or IP edits do not reset an unchanged cap. A reviewed reset is consumed
once, with its lineage retained across later ordinary policy saves. The observer
requires the exact current applied stored policy, calendar, resource birth and
live runtime pointer/lease before sampling and again before commit.

Frozen GORM V83 adds Project rolling states, immutable observations and recipient
inboxes. State, observation and the bounded complete original recipient set
commit atomically; a failed final proof or inbox write rolls back the whole
transition. At observation time, recipients must be exact current enabled managers
with complete admission facts and matching published relationships. List, unread
count and read mutations require that same original recipient birth plus current
exact management of the active Project. Removal or inactivity hides the history;
rejoining may restore that original user's read state, while a new manager gains
no historical projection. Platform permissions alone grant no manager inbox.

The wire kind is `project_rolling_quota_warning`, with
`rolling_quota_warning_observation_id` and `project_rolling_quota_warning`.
Snapshots freeze the exact Project scope/name/birth, window and coverage times,
policy revision, episode, timezone, integer settled/cap strings, threshold and
generation. The existing localized menu renders recorded facts only; it does not
infer current allowance, mail delivery or stop-calling policy.

## Team aggregate rolling Token warnings

The composed V84 candidate samples an active Team's own stored five-hour and
seven-day Token caps against the exact registered `team:<id>` account and Team
birth. Team aggregate balances are separate from stable Team/member child
balances; no child, Personal, Project or Key counter is summed into a Team
percentage. It uses the same fully covered known settled window and sampled
80%/90% episode rules. Finite holds stay reservations; unknown or incomplete
coverage preserves prior episode state. Null/zero caps produce no percentage
warning, and general policy edits do not rearm an unchanged monitored cap. A
sampled cap change or new explicit default-reset review requires fresh applied
runtime proof; the reset lineage is consumed once and survives ordinary saves.

Frozen GORM V84 adds Team rolling states, immutable observations and original
recipient inboxes without editing versions 1–83. Exact current Team/resource
birth, stored policy/calendar, live publisher, current runtime pointer and lease
are checked before observation and again before commit. State, observation and
all inbox rows share one governance-locked transaction, so recipient overflow,
inbox failure or a lost final proof cannot leave a partial episode.

Only then-current enabled admitted owners/members of that active Team with exact
published membership identities receive the observation, bounded to 1,000
recipients. A platform administrator has no implicit membership. Historical
list/count/read access uses the original recipient's recorded User birth and
current exact enabled Team membership plus the original Team birth. Removal,
disable, offboarding or Team inactivity suppresses access without rewriting
read state. Rejoin can restore the same original user's recorded rows; a later
member or recreated User/Team cannot borrow them. A membership generation proves
current invocation authority, rather than creating a new historical recipient.

The wire kind is `team_rolling_quota_warning`, with
`rolling_quota_warning_observation_id` and `team_rolling_quota_warning`.
Snapshots preserve exact Team scope/name/birth, episode, applied policy revision,
window (`5h` or `7d`), sample/coverage times, timezone, settled/cap integer strings,
level, threshold and generation. The strict notification decoder rejects mixed
families or inconsistent scope/threshold/coverage facts. The existing bilingual
menu, recipient-scoped query lifetimes, pagination and read actions are reused;
renewed or failed reads hide old facts and late responses cannot restore them.
No new email source, layout or admission writer is introduced.

## Composed rolling-warning acceptance boundary

The V82 Personal Key, V83 Project aggregate and V84 Team aggregate implementation
passes current source checking, complete Task/build and the full PostgreSQL/MySQL
matrix: 324 direct scenarios, eight constraints, 4,813 ordinary named tests and
412 named tests per driver. The containing phase delivers these bounded scopes.
Earlier monthly/Personal evidence retains its original source identity; controlled
production/API/native/restart and browser acceptance remain separate.

The previous Full158 attempt did not pass: it timed out at the 55-minute bound
while MySQL reached scenario 105, with Task exit 201 after 3320.365 seconds.
That failure remains historical evidence. The adopted parallel supervisor uses
finite work/cleanup budgets and preserves every assertion; its fresh accepted
Full162 completed in 2,055.583 seconds without a performance comparison claim. Controlled
production/API/native/restart and any browser evidence remain separate gates;
no external mail or paid-provider inference is inferred from source tests.


## Locked warning transaction visibility

Personal and Project Key warning observers, plus Personal, Project and Team
rolling observers, explicitly request portable Read Committed transactions.
The governance lock, owner or Project locks, complete retained Key graph and
policy locks remain in place. Calendar/currency publication stays protected by
the existing service lock, and the final runtime pointer, application lease,
identity and accounting proof are checked before commit. A waiter must read the
preceding observer's committed episode state; an earlier Repeatable Read snapshot
can miss that state after waiting on PostgreSQL's governance lock and attempt a
duplicate insert. No duplicate error is ignored and no fixture is serialized to
hide the concurrency. The synchronized regression preserves two real concurrent
observers and exact transaction identity. Source checks and the full real-driver regression pass; source-bound receipts
and separate runtime/browser gates are recorded in [Implementation](IMPLEMENTATION.md).


## Controlled local SMTP process acceptance

The `7aad9d7` production artifact passes a real local SMTP/HTTP/database workflow
in 45.579 seconds. The controlled peer receives one notification with DATA250,
then a distinct occurrence whose lost final reply stays Unknown. Both retain one
delivery attempt; two application restarts preserve the original Session and do
not replay either message. The workflow creates no Keys or native Calls. Root
independently verifies captured app processes, four listeners and owned Compose
resources absent. Acceptance SHA-256:
`bf3bd7e7ffa90c34191a5849e7c82cb691810a653a69aadf87c1e72751cab00d`.

This establishes local process behavior only. External SMTP TLS/authentication,
recipient mailbox delivery and browser acceptance remain separate open gates.
