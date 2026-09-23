# Project API Keys

A Project Key belongs to one Project for its entire lifetime. Its creator is an audit fact, never a personal owner or permanent manager. All current enabled Project managers can manage its Keys, as can a platform role with `projects.write`. Team membership, Team ownership, `projects.read_all`, and a Key's creator identity do not grant Key management authority.

## Delivery and Lifecycle

Creation explicitly selects `delivery_mode: "manual"`. This phase supports browser one-time delivery only. It rejects unsupported modes instead of silently falling back to plaintext. Enterprise Vault delivery, application identities, managed connection descriptors, and delivery coordination remain separate future capabilities.

Key IDs use `pky_` prefixed ULIDs. Bearer values use `rxp_` plus 32 cryptographically random bytes encoded as URL-safe base64. The database stores only the SHA-256 verification digest, a masked prefix, immutable ownership/scope/expiry/delivery metadata, state, and audit facts. Complete bearer values appear only in the successful creation or rotation response and cannot be recovered by subsequent reads.

A new credential is `pending`, with a ten-minute delivery confirmation deadline, and cannot authenticate. Confirmation activates it, or preserves a disabled source Key's disabled state during rotation. Closing an unconfirmed delivery can revoke the pending Key immediately through the normal revoke endpoint. An unconfirmed Key remains unusable after its deadline even if no cleanup job has removed its row. Confirmation after the deadline or credential expiry returns `409`.

Confirmed Keys are `active` or `disabled`; revocation is terminal. Name and enabled state can be edited. Project ownership, creator, model scope, expiry, delivery mode, and replacement relation are immutable. Revocation is idempotent and removes runtime authority before a successful management response. No mutation returns the old bearer value.

A disabled Project rejects all Key authentication and prohibits issuance, rotation, confirmation, or enablement. Existing per-Key states remain stored; managers may still inspect, rename, disable, or revoke those Keys. Reenabling the Project restores only Keys already active and unexpired. An archived Project is read-only and cannot be reactivated; its Key and call history remains available under existing authorization.

## Effective Authorization and Manager Continuity

The gateway authorizes a Project Key only when all of these conditions hold:

- The Project is active and retains at least one enabled current manager.
- The Key is active and unexpired.
- The requested stable Model ID belongs to the Key's immutable scope and the Project's current model grants.
- The logical model is currently active, with a usable route for dispatch.

Direct grants to any individual, including the creator or current managers, do not enlarge Project Key scope. Additional Project model grants do not enlarge an existing Key's fixed scope. Grant removal immediately narrows its effective access. Team grants are unrelated.

Project lifecycle and model-grant reductions install a Project runtime denial marker before synchronous authorization refresh. Key disable/revoke similarly install Key denial markers. Snapshot loading reads Project state, managers, accounts, Key metadata, fixed scopes, and current grants within the same repeatable-read transaction as other authorization data. Gateway requests use the immutable snapshot and existing bounded authorization lease; they do not query the Control Plane or a secret store per request.

Resource and Key mutations use the same governance lock before Project and Key locks. Manager removal, Project suspension, account suspension, issuance, confirmation, rotation, and revocation therefore cannot pass contradictory lifecycle checks concurrently. Account suspension still preserves the last active Project manager. Removing or disabling one manager while another remains does not revoke Project Keys or change their ownership. An enabled remaining manager continues administration, including for Keys created by the departing user.

## Rotation and Verified Retirement

Rotation generates a new Key ID and bearer, retaining the same Project, model scope, name, expiry, and manual delivery mode. It never reads or discloses the old credential. The replacement records `replaces_key_id` and starts pending.

Confirming delivery of the replacement does **not** retire the old Key. A user copying a secret does not prove that the application has deployed it or completed a real call. The old Key remains available until explicit retirement or emergency revocation.

Planned completion requires the replacement to be active and unexpired, to retain the same immutable scope and expiry, and to have a persisted successful gateway call attributed to that exact Project and replacement Key after its creation. Failed calls, calls by another Key, another Project's calls, or delivery confirmation alone do not satisfy this requirement. Successful completion revokes the old Key transactionally. The durable recorder may need to finish delivering a call fact before completion becomes eligible; the endpoint returns `409` until that evidence exists.

Emergency revocation is independent and available immediately, without waiting for replacement issuance, delivery, or verification. A revoked source Key cannot authorize a still-pending replacement's confirmation. Concurrent revoke/confirm/rotate cannot restore the revoked source.

This verified-retirement contract is the canonical target for planned rotation. The earlier personal-Key implementation currently retires its source during delivery confirmation and still requires alignment in a later personal-Key lifecycle update; Project Keys do not inherit that shortcut. This slice also keeps expiry immutable across rotation; a separately created Key is required for a different lifetime.

## API Contract

Paths are relative to `/api/v1`. All routes require an authenticated session and current Project management authority. Mutations require same-origin protection and a CSRF token; JSON bodies use the management request size limit. Unrelated users receive `404` without access to Project credential metadata.

| Method and path | Input | Result |
|---|---|---|
| `GET /projects/:project_id/keys` | Optional `status`, `limit`, `cursor` | `{items:Key[],next_cursor:string|null}` |
| `POST /projects/:project_id/keys` | `{name,model_ids,expires_at?,delivery_mode:"manual"}` | `201`, `{key,secret}` once |
| `GET /projects/:project_id/keys/:key_id` | None | Safe Key metadata |
| `PATCH /projects/:project_id/keys/:key_id` | `{name?,enabled?}` | Updated safe Key metadata |
| `DELETE /projects/:project_id/keys/:key_id` | None | `204`; immediate terminal revocation |
| `POST /projects/:project_id/keys/:key_id/confirm` | None | Confirmed safe Key metadata |
| `POST /projects/:project_id/keys/:key_id/rotate` | `{delivery_mode:"manual"}` | `201`, `{key,secret}` for the replacement |
| `POST /projects/:project_id/keys/:key_id/complete-rotation` | `{replacement_key_id}` | `204` after verified retirement |

A safe Key contains `{id,project_id,creator_id,name,prefix,status,model_ids,expires_at,created_at,replaces_key_id,delivery_expires_at,delivery_mode}`. Lists retain revoked Keys and replacement links as lifecycle history. Pagination orders by descending stable Key ID, defaults to 40 items, and caps pages at 100. Key scope contains 1–128 distinct, currently authorized active Model IDs at issuance. Name validation uses the standard 1–100-character label policy. Null expiry means no fixed expiration; supplied expiration must be in the future.

No endpoint accepts a new personal owner or Project owner for an existing Key. Migrating from a personal credential requires separately creating a Project Key, changing the caller configuration, and revoking the former personal Key.

## Call Attribution and Storage

Project calls carry `project_id` and an empty `user_id`; personal calls carry `user_id` and an empty `project_id`. A call fact must have exactly one owner. The creator is never copied into personal usage attribution. Project facts are excluded from personal call queries and retained through Project archival or manager changes. Stable Project and Key IDs remain historical facts rather than live cascading ownership relations.

Schema version 9 creates `project_api_keys` and `project_api_key_models` through frozen private GORM definitions, explicit belongs-to foreign keys, state checks, and supporting indexes. It adds the default-empty Project attribution column and composite Project/time/request index to existing call records through GORM migration operations. It does not rewrite released migration versions or rely on service-layer SQL dialect checks.

`testProjectKeyLifecycle` exercises the real router, HTTP upstream fixture, durable call recorder, and runtime snapshot against PostgreSQL and MySQL. Coverage includes one-time secret safety, pending/expired/revoked rejection, authorization intersections, Project disable/reactivation, independent creator departure, accurate call ownership, verified replacement retirement, and manager-removal, Project-disable, and revoke races. The shared isolated harness owns database reset and migration; run `go tool task test-integration` after registering the helper.

Project/Key token budgets, money limits, RPM/TPM/concurrency, IP restrictions, approval workflows, Vault integration, and corresponding UI remain explicitly outside this slice. The implemented model/lifecycle checks do not stand in for those later controls.
