# Personal Model Requests

An enabled member can request one active Model for direct Personal access. The
model catalogue retains its existing cards, tables, filters and detail drawer.
The Available to request filter exposes a minimal candidate catalogue, and the
existing drawer footer collects a required reason. The existing toolbar also opens own request history so completed requests remain
reachable after effective grants change. This small action preserves historical
review reachability independently of current catalogue membership. Request history
remains scoped to the signed-in member. Team visibility alone does not grant Personal access.

A reviewer needs the independent `members.models.write` permission. Identity
editing with `members.write`, catalogue administration with `models.write`, Team
ownership and Project management confer no request-review authority. Reviewers
cannot approve their own requests. The existing Member Models tab and the scoped
`/admin/members/:memberId/models` workspace expose direct Model grants and request
history without requiring the global member directory. The minimum workspace
contains no email, roles, Keys, limits or other private Member fields.

## HTTP contract

All paths below are relative to `/api/v1`. Authentication, same-origin JSON and
CSRF rules apply. Creation and decisions require a strong quoted `If-Match` from
the exact reviewed resource. Unknown fields, duplicate JSON fields, malformed
UUIDs, invalid UTF-8, control characters and unsupported queries are rejected.

| Method and path | Contract |
| --- | --- |
| `GET /model-access-candidates` | Minimal active Models, literal `q`, cursor and `limit` of 1–50; current grant and pending-request facts only. |
| `GET /model-access-candidates/:model_id` | Independently authorized candidate and reviewed ETag. |
| `GET /personal-model-requests` | Own history; optional status, opaque scoped cursor and limit of 1–50. |
| `POST /personal-model-requests` | UUIDv4 `request_id`, exact `model_id`, non-empty reason of at most 1,024 UTF-8 bytes; `201` for new, `200` for an exact known intent. |
| `GET /personal-model-requests/:request_id` | Own historical record, current facts, allowed actions and review ETag. |
| `POST /personal-model-requests/:request_id/decision` | UUIDv4 `decision_id`, action `withdraw`, empty reason; exact reviewed ETag. |
| `GET /admin/members/:user_id/model-access-workspace` | Independently authorized minimum direct-grant workspace. |
| `GET /admin/members/:user_id/model-requests` | Target-scoped reviewer history with the same bounded filters. |
| `GET /admin/members/:user_id/model-requests/:request_id` | Exact target-scoped review and current facts. |
| `POST /admin/members/:user_id/model-requests/:request_id/decision` | UUIDv4 decision, `approve` or `reject`; rejection requires a reason; first terminal decision wins. |

Candidates reveal no Provider, Connection, Credential, pricing, global grant or
member directory. The granted discovery endpoints retain their existing meaning.
Actor, target, request, Model and Model-name ownership are checked exactly under
both supported database collations. Every receipt retry rechecks current authority.

## Approval, receipts and runtime application

Approval atomically inserts only the requested direct User Model grant, records
its request provenance, releases the pending slot and appends a typed audit event.
It preserves other grants and every existing Key's immutable Model ceiling.
A newly created and confirmed Key may select the approved Model; an old Key
cannot gain that Model through approval. Native dispatch still requires eligible
routing, current authentication and quota admission.

A decision response separates `committed` and immutable `saved_request` from
`current_granted`, `runtime_applied` and `application_status`. Current application
requires the exact original request provenance in a fresh published snapshot.
An approval receipt may be historically committed while current application is
pending or superseded. Revocation or an ordinary remove/re-add does not recreate
that original provenance. Retrying a historical decision never restores access.

Client creation and decision intents retain the exact actor, target, UUID, body
and reviewed ETag through uncertain outcomes. Conflict review preserves drafts;
refreshing current facts does not resolve an uncertain original operation.
Private data and actions hide during renewed session, permission or target reads.
Sensitive request intent remains in component state, never browser storage or a
React Query mutation cache. All visible controls and accessible names use paired
English/Chinese `personalModelRequests` translations; English remains the default.

User disablement and offboarding atomically cancel pending requests and release
their slots with audit evidence. Inactive Models can be rejected or withdrawn,
but cannot be approved. A database-layer lifecycle helper supports explicit
cancellation by a future Model lifecycle operation; no new Model-status endpoint
is claimed. Historical submitted names and decisions never borrow a later identity.

## Persistence and verification

Frozen GORM V43 creates immutable request history and a separate unique pending
applicant/Model slot, adds nullable direct-grant provenance, and seeds only the
built-in administrator's review permission. Historical rows deliberately have
no live applicant/Model foreign keys. Checks and unique indexes enforce bounded
identities, supported status and complete terminal receipts. Partially applied
MySQL DDL is repaired independently. Released V1–V42 stay unchanged.

Final mandatory check and full tests passed, including 1,089 frontend cases in
75 files, Go race tests, development lifecycle and production asset serving. Both
real-process database authentication/native lifecycles passed. Final production
browser/native/restart acceptance passed pending no-access, independent review,
unchanged old-Key scope, new-Key completion, revocation, original receipt retry
without restoration, stable pending history and fresh superseded application
copy. English/Chinese rendering passed, English was restored, no browser console
errors were recorded and owned processes/tab/Compose resources were removed.
Complete real PostgreSQL/MySQL race regression passed (Handler 834.672 seconds;
Service 6.110 seconds), including migration and lifecycle acceptance. Full F19 remains partial: Team Model requests,
further Team protocols and broader member overview/price/usage facts remain open.
