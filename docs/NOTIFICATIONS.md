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
