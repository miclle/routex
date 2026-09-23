# Personal API Keys

Personal Keys authenticate native gateway requests independently of browser sessions. A Key's model scope is an immutable ceiling over stable model IDs. Effective access is its intersection with the owner's current model grants and active models. Administrators receive no implicit calling access.

## Delivery and lifecycle

Creation returns an `rx_` bearer value once. The database stores only its SHA-256 digest and a short display prefix. List, edit, audit, and error responses never return the bearer or digest. The UI keeps delivery values outside React Query caches and persistent browser storage.

A new Key is `pending` and cannot authenticate. The owner must confirm delivery within ten minutes. Closing or canceling the delivery dialog revokes the pending Key. An expired pending delivery remains unusable and cannot be confirmed. Active Keys can be renamed, disabled, re-enabled, or permanently revoked. An expired Key cannot be re-enabled. Revocation is terminal.

Rotation creates a pending replacement with the original model scope, expiration, and activation state. Confirmation locks both records, rechecks scope and source state, and activates the replacement without revoking the original. Multiple confirmed replacements can coexist during rollout; none silently retires the source. Canceling a replacement does not revoke the original. Rotating a disabled Key preserves its disabled state.

Planned retirement is a separate operation. The replacement must be active and unexpired, retain the same scope and expiry, and have a persisted successful gateway call made after its creation by the exact personal owner and replacement Key. Project-attributed calls, another user's calls, the original Key's calls, failed calls, and delivery confirmation do not qualify. Once the application has switched and that evidence is available, the owner completes rotation explicitly to revoke the original. Durable call recording can delay eligibility until the successful fact reaches the database.

Completion records the exact replacement ID in its transactional audit event; the replacement's immutable `replaces_key_id` identifies the source. Concurrent completion requests produce one audit fact, and subsequent retries remain successful even if the replacement is later disabled, expires, or is revoked. Ownership is checked on every retry. No completion event is fabricated for an unrelated replacement.

Emergency revocation through `DELETE` remains immediate and independent of replacement generation, delivery, or verification. A source revoked before its pending replacement is confirmed cannot authorize that confirmation. A separate new Key can be created for recovery; a linked emergency replacement workflow and rotation with a different expiry remain later work.

## API

All routes use the `/api/v1` prefix and require a browser session. Writes require same-origin requests and `X-CSRF-Token`. JSON payloads are limited to 64 KiB. Ownership checks return an indistinguishable not-found result for another user's Key.

| Method | Path | Behavior |
|---|---|---|
| GET | `/keys` | Return `{items: Key[]}` including historical revoked records |
| POST | `/keys` | Accept `{name, model_ids, expires_at?}`; return 201 `{key, secret}` |
| POST | `/keys/:key_id/confirm` | Confirm pending delivery; return the resulting Key |
| PATCH | `/keys/:key_id` | Accept `{name?, enabled?}`; update an active or disabled Key |
| DELETE | `/keys/:key_id` | Permanently revoke; return 204; repeated revocation is idempotent |
| POST | `/keys/:key_id/rotate` | Return 201 `{key, secret}` for a pending replacement |
| POST | `/keys/:key_id/complete-rotation` | Accept `{replacement_key_id}`; return 204 after verified retirement, or 409 until eligible |

Key fields are `id`, `name`, `prefix`, `status`, `model_ids`, `expires_at`, `created_at`, `replaces_key_id`, and `delivery_expires_at`. Expiration timestamps may be null. Mutations and their secret-free audit records commit in the same database transaction.

## Verification and remaining scope

`handler/apikey_integration_test.go` and `handler/apikey_rotation_integration_test.go` run in the shared disposable-database harness on PostgreSQL and MySQL. They cover pending rejection, digest-only storage, disclosure boundaries, CSRF, explicit model grants, immediate grant revocation, cancellation, concurrent confirmation and retirement, disabled rotation, expired delivery, ownership, terminal revocation, restart behavior, and audit persistence. Rotation acceptance uses a real HTTP upstream call; negative fixtures prove that wrong-user, wrong-Key, Project-attributed, and failed calls cannot qualify. Frontend tests cover confirmation, cancel/revoke failures, and secret cache isolation.

[Project Keys](PROJECT_KEYS.md) have independent ownership and authorization. Mutable narrowing policies, usage limits, IP rules, external delivery, and delivery Profiles remain later work packages. Production gateway authorization uses the immutable runtime snapshot and its bounded lease, with synchronous refresh and denial markers after reductions. Database-backed authentication remains available for service use when runtime snapshots are not started. Key lists retain revoked history; pagination, richer history presentation, and delivery policy configuration remain separate follow-up slices.
