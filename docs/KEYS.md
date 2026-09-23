# Personal API Keys

Personal Keys authenticate native gateway requests independently of browser sessions. A Key's model scope is an immutable ceiling over stable model IDs. Effective access is its intersection with the owner's current model grants and active models. Administrators receive no implicit calling access.

## Delivery and lifecycle

Creation returns an `rx_` bearer value once. The database stores only its SHA-256 digest and a short display prefix. List, edit, audit, and error responses never return the bearer or digest. The UI keeps delivery values outside React Query caches and persistent browser storage.

A new Key is `pending` and cannot authenticate. The owner must confirm delivery within ten minutes. Closing or canceling the delivery dialog revokes the pending Key. An expired pending delivery remains unusable and cannot be confirmed. Active Keys can be renamed, disabled, re-enabled, or permanently revoked. An expired Key cannot be re-enabled. Revocation is terminal.

Rotation creates a pending replacement with the original model scope, expiration, and activation state. Until confirmation, the original retains its state. Confirmation locks both records and atomically revokes the original. Concurrent replacements cannot both activate. Canceling a replacement does not revoke the original. Rotating a disabled Key preserves its disabled state.

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

Key fields are `id`, `name`, `prefix`, `status`, `model_ids`, `expires_at`, `created_at`, `replaces_key_id`, and `delivery_expires_at`. Expiration timestamps may be null. Mutations and their secret-free audit records commit in the same database transaction.

## Verification and remaining scope

`handler/apikey_integration_test.go` runs in the shared disposable-database harness on PostgreSQL and MySQL. It covers pending rejection, digest-only storage, disclosure boundaries, CSRF, explicit model grants, immediate grant revocation, cancellation, concurrent rotation, disabled rotation, expired delivery, ownership, terminal revocation, restart behavior, and audit persistence. Frontend tests cover confirmation, cancel/revoke failures, and secret cache isolation.

Project ownership, mutable narrowing policies, usage limits, IP rules, external delivery, and delivery Profiles belong to later work packages. The current bearer verifier reads authoritative database state on each request; this does not establish offline authorization or snapshot-based gateway availability.
