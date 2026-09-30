# Operational alerts and notifications

RouteX turns a bounded set of durable operational failures into grouped alerts and recipient-isolated notifications. Delivered sources are failed system jobs, failed Provider credential verification, Provider quality transitions, and terminal route-unavailable calls. RouteX does not create demonstration alerts or infer incidents from browser state.

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

Only enabled users with the current `system.read` permission receive or read notifications. Inbox operations derive the recipient from the authenticated session; clients cannot select another recipient. The worker rechecks `system.read` immediately before SMTP network work and terminates the intent when the recipient is no longer eligible.

`system.read` permits the operations overview, alerts, inbox, and personal settings read. `system.write` is additionally required to change alert state or save personal external-email settings. Notification payloads contain only allowlisted kinds and detail codes. SMTP credentials, raw upstream errors, arbitrary audit JSON, and another recipient's email are never exposed through notification APIs.

## API

Paths are relative to `/api/v1`. Writes require session authentication, CSRF, same-origin checks, strict JSON fields, and current server-side permissions.

| Method and path | Permission | Purpose |
| --- | --- | --- |
| `GET /notifications?status=unread\|all` | `system.read` | List the authenticated user's notifications |
| `POST /notifications/{notification_id}/read` | `system.read` | Mark one owned notification read |
| `POST /notifications/read-all` | `system.read` | Mark all owned notifications read |
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

## Delivery lifecycle

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
