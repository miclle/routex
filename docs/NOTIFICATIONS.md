# Operational alerts and notifications

RouteX turns a bounded set of durable operational failures into grouped alerts and recipient-isolated notifications. Operational sources are failed system jobs, failed Provider credential verification, Provider quality transitions, and terminal route-unavailable calls. Separate Personal/Project inbox records observe current-policy monthly settled-use exhaustion. RouteX does not create demonstration alerts or infer incidents from browser state.

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
