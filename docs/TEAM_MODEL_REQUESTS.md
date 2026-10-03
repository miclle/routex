# Team Model requests

Team Model requests add one Model to a shared Team grant through a separately
reviewed operation. Model visibility, a pending request, historical approval and
current runtime application are distinct facts. Existing Personal grants, Key
ceilings, Project grants, routing and resource limits do not change.

## Scope and authority

An enabled applicant selects an active Team from a minimal membership-scoped
picker, including Teams with no Model grants. Creation captures the exact active
membership identity. Candidates expose public Model metadata and whether this
Team has a grant or pending request. Another applicant's pending reason or record
is never returned by discovery. One pending slot is shared per Team/Model.

Review requires current `teams.models.write`, either direct platform authority or
an applicable assigned Team role. Team ownership alone grants no review authority;
self-approval and self-rejection are forbidden. The dedicated resource workspace
returns only Team identity/status and its granted Model metadata, without a global
Team directory, member profiles, unrelated Models, Keys or limits.

Own immutable history remains readable after membership loss. Current Team,
Model, grant, membership and runtime fields become unavailable when current read
authority is absent. A historical receipt never grants current resource access.

## API boundary

| Endpoint | Contract |
| --- | --- |
| `GET /team-model-request-teams` | Minimal active membership-scoped Team picker. |
| `GET /teams/:team_id/model-request-candidates` | Literal search and bounded cursor paging over public active Models, with Team grant/pending facts. |
| `GET /teams/:team_id/model-request-candidates/:model_id` | Exact candidate and reviewed ETag. |
| `GET /team-model-requests` | Own history, including inaccessible former Teams, with server-side filters and cursor paging. |
| `POST /team-model-requests` | Exact Team/Model, UUIDv4 creation intent, required reason and reviewed If-Match. |
| `GET /team-model-requests/:request_id` | Own immutable history and independently authorized current facts. |
| `POST /team-model-requests/:request_id/decision` | Own reviewed withdrawal. |
| `GET /teams/:team_id/model-request-workspace` | Independently authorized minimal review workspace. |
| `GET /teams/:team_id/model-requests` | Scoped reviewer history and filters. |
| `GET /teams/:team_id/model-requests/:request_id` | Exact scoped review and allowed actions. |
| `POST /teams/:team_id/model-requests/:request_id/decision` | Reviewed approve/reject, UUIDv4 decision intent and optional approval or required rejection reason. |

Mutations use the existing HttpOnly Session, same-origin and CSRF boundary.
Bodies reject duplicate/unknown JSON fields and invalid text. Identifiers are
checked exactly independent of database collation; persisted legacy resource IDs
remain compatible. Request record IDs use the `tmr_` prefix. Filters and limits
are validated before database reads.

## Receipts, grants and lifecycle

The first terminal decision is immutable. Approval adds only a missing shared
Team/Model grant and records the original request as its source; it never replaces
an independently existing grant. History, pending slot release, grant and typed
audit event commit atomically. Reauthorized exact retries reconcile the original
receipt before obsolete grant or membership conditions, without restoring grants.

The current local runtime lease must contain the exact original grant source,
active Team and active Model before application is confirmed. Saved history alone
is not publication proof. Missing current authority produces unavailable facts;
revocation or ordinary removal/re-addition supersedes original application proof.
An unchanged canonical grant retains its source during full Model-list updates.

User disablement/offboarding, Team disablement/archive and membership removal or
disablement cancel pending requests in the same governing transaction. Rejoining
does not revive a cancelled request. Approved shared grants survive applicant
departure and may be invoked by other currently eligible Team members. Grant
application does not prove any applicant's current membership or native call.

## Interface

The existing Model drawer offers explicit Personal/Team request scope and a
minimal Team selector. It retains the existing public metadata and integration
layout. Own request history has separate Personal and Team tabs. The existing
Team Models tab adds scoped request history below its authorized tables; reviewers
without the global directory may use `/teams/:resourceId/model-requests`.

Local Base UI dialogs require explicit confirmation. Drafts survive stale reviews
and uncertain results; retries retain exact IDs, ETags and submitted payloads.
Fresh actor/resource authority gates private content and actions. Historical
commit notices never repeat captured current application claims. Paired
`teamModelRequests` English/Chinese copy follows the English-default contract.

## Database and acceptance

Frozen GORM V44 creates request history, a Team/Model pending slot and nullable
`TeamModelGrant.SourceRequestID`. Historical records deliberately have no live
membership/model foreign keys. Unique creation and decision intents, status and
receipt constraints, immutable migration structs and retryable partial-DDL repair
apply to PostgreSQL and MySQL.

Final mandatory check and full test passed 1121 frontend cases in 77 files, Go
race, development lifecycle and production embedding. The complete real
PostgreSQL/MySQL matrix passed (Handler 951.498 seconds; Service 5.827 seconds),
and both authentication/native process lifecycles passed. Controlled production
browser/native/restart proof covered pending denial, scoped approval, shared
completion, unchanged Personal Key ceilings, membership loss, retained historical
receipts, grant revocation and replay without restoration. English/Chinese passed
with English restored, no console errors and owned resource cleanup. No external-provider,
distributed runtime or whole-F19 acceptance is claimed.
