# Provider and Model Catalog

RouteX implements persistent provider configuration, encrypted credential storage, native connection protocols, controlled model discovery, stable model identities, explicit user grants, and validated routing weights. Catalog verification does not prove that inference requests have succeeded.

## Domain Boundaries

| Entity | Responsibility |
|---|---|
| `Provider` (`prv_`) | Stable supplier identity and display name |
| `ProviderConnection` (`con_`) | Supplier-specific Base URL, protocol, and connection name |
| `ProviderCredential` (`crd_`) | Encrypted secret, priority, verification state, and explicit enabled state for one connection |
| `ProviderModel` (`pmd_`) | Exact upstream model identifier and declared input capabilities accepted by one connection |
| `CredentialModelAccess` | Models actually returned by discovery using one credential |
| `Model` (`mdl_`) | Stable authorized identity and lifecycle status |
| `ModelName` | Globally reserved current, compatibility, and historical public names |
| `ModelProviderBinding` (`bnd_`) | A model's explicit supplier model relationship and weight |
| `UserModelGrant` | Explicit user-to-model permission by stable IDs |

Providers do not own transport details or secrets directly. Prices belong to provider models through the current price catalogue. Credential priority does not change supplier binding weights.

Schema version 3 adds these tables without changing the published authentication migrations. Foreign keys protect their relationships. Public names and upstream names use exact, case-sensitive comparison on both databases; MySQL explicitly uses `utf8mb4_bin`. Model names accept 1–128 ASCII letters, digits, `.`, `_`, `:`, `/`, and `-`, starting with a letter or digit. Upstream identifiers retain their original Unicode spelling and case.

## Credential Lifecycle and Verification

Credential creation encrypts the supplied secret with the configured root key and authenticates its immutable credential ID as the encryption reference. The database stores ciphertext only. Listing DTOs omit both plaintext and ciphertext; authentication and catalog SQL use the non-interpolating, silent service database session.

New credentials have `verification_status: "pending"` and `enabled: false`. Verification performs a real `GET <base_url>/models` with the credential's bearer token. The response must be HTTP 200 and contain a `data` array of valid model IDs. Discovery is limited to 2 MiB, 2,000 returned entries, and a 10-second timeout. Duplicate IDs are deduplicated. Neither upstream error bodies nor transport error details are returned to users.

A successful verification records `verified`, the verification time, and the exact discovered model coverage. It does not enable a new credential. Administrators must explicitly enable it. Verification failure records `failed`, clears its discovered coverage, and disables it. Reverification replaces coverage atomically; if previously active models are no longer covered, that credential is disabled until a valid explicit enable operation.

Enabling a credential requires successful verification and coverage of every model currently assigned positive routing weight on that connection. A binding is ready only when the connection has at least one enabled, verified credential and every such credential has discovered that provider model. Manually entered provider models receive no invented verification result; they must appear in real discovery before activation. This phase verifies discovery and authorization, not per-model inference capability or complete credential-pool failover. Those remain separate acceptance requirements.

Base URLs and outbound connections follow the upstream client's SSRF policy. Public HTTPS is the default. Private or local test endpoints require explicit development configuration. Redirects and DNS resolution follow the same policy; credentials must not be forwarded to an unvalidated destination. Missing root-key configuration returns `503` when credential storage is required, without preventing local identity operations.

## Credential workspace

The Provider Credentials tab preserves the existing table, add dialogs, verification, and enabled-state actions. Its compact toolbar filters the complete authorized Provider response by literal trimmed case-insensitive name, Connection, verification state, and enabled state; conditions intersect. Connection selection appears only for multiple Connections. Filtered counts and empty results never imply that credentials were deleted. Changing Provider clears the previous filter state.

The table displays the actual nullable `verified_at` value using the selected language. Missing timestamps remain explicitly not recorded; no secret, masked secret, inferred last-use time, or demonstration failure statistic is exposed. Read access requires `providers.read`; add, verification, and enabled-state actions continue to require `providers.write`. A filter never changes a mutation's exact credential ID. English/Chinese copy and behavior tests cover filtering, state reset, language changes, and permission isolation.

### Credential metadata edits

The existing Credentials table opens a local dialog to edit a credential's name
and priority. Connection context is immutable, and the dialog explains that the
secret remains unchanged. Metadata persistence never decrypts credentials,
calls upstream verification, replaces secrets, changes enablement or verification,
or rewrites discovered model coverage. Priority affects only that Connection's
credential order; supplier binding weights remain unchanged.

Read `GET /admin/credentials/:credential_id/metadata` with `providers.read`.
The response includes the non-secret Credential fields, `connection_id`, and
`etag`; the response header is the quoted ETag. Submit a strict JSON
`{name,priority,reason}` to `PUT` on the same path with `providers.write`,
CSRF, and the reviewed quoted `If-Match`. Names are trimmed valid Unicode labels
of 1–100 characters without control characters. Priority is an explicitly
supplied integer from 0 to 10,000; zero is valid. The trimmed non-empty reason
allows at most 1,024 UTF-8 bytes and no control characters. Additional fields,
including `secret` or `enabled`, are rejected.

New or renamed labels are unique under trimmed case-insensitive comparison within
a Connection. Go case folding under the Connection lock
provides the same behavior on PostgreSQL and MySQL. Existing historical
duplicates are retained, may change priority without renaming, and remain
eligible for unchanged-target publication reconciliation. The write rechecks
authority and locks Connection then Credential;
only name and priority change. Changed metadata and the typed
`credential.metadata.update` audit event commit together. Audit details expose
only bounded before/after name and priority plus the reason.

The strong ETag represents the current non-secret resource, including recorded
verification and enabled state. It is not a monotonic revision or historical
operation receipt. A stale ETag with a different target returns `409` and requires
explicit review of the current record while retaining the draft. If the requested
name and priority already match, the server performs no mutation or duplicate
audit and refreshes runtime before returning the authoritative current record.
This confirms the current target, not the identity or outcome of an original
historical write. `503` or a transport failure can leave a persisted change whose
publication is uncertain; retain the exact request and ETag for retry, or fetch
and explicitly review current state before preparing another intent. A failed
retry does not resolve the original uncertainty. No new schema migration is
required.

### Credential deletion

The same row action menu offers a danger confirmation using the reviewed
metadata record and a required reason. `DELETE /admin/credentials/:credential_id`
requires `providers.write`, same-origin validation, CSRF, a quoted metadata
`If-Match`, and strict JSON `{reason}` with the same 1,024-byte reason bounds.
The target must be a canonical `crd_` identifier and its validator a 64-hex
metadata ETag. A changed existing record returns `409` and requires explicit
review before another deletion intent.

Under governance, Connection, and Credential locks, deletion removes only the
Credential's discovery-access rows and then its configuration row in one GORM
transaction, together with the typed `credential.delete` audit event. Provider,
Connection, provider models, weights, grants, and immutable call/attempt history
remain intact. Deleting the last ready credential is allowed and can make its
routes unavailable; no hidden replacement, fallback credential, or weight change
is introduced. Existing calls do not persist Credential IDs, so deletion does not
invent per-credential historical attribution.

After commit, the server installs a credential tombstone before refreshing the
runtime. New dispatch cannot use that removed credential even if publication
fails. Already dispatched requests are not actively cancelled. Only a `200`
response `{id,absent:true,runtime_applied:true}` confirms current absence and
runtime application. If runtime is not initialized or publication fails, the
response remains uncertain rather than claiming enforcement. Repeating the exact
authorized DELETE can establish that the ID is absent and complete publication
without another audit event. This does not prove who performed an earlier
deletion. An ordinary metadata GET `404`, a permission failure, or a failed retry
cannot establish completion. The client preserves the original intent and never
removes a row optimistically. No migration or secret decryption is needed by the
deletion transaction; remaining runtime configuration still follows its normal
credential-storage requirements.

## Models, Names, Grants, and Weights

Creating a model requires a provider model and creates an initial binding with weight `0`. The creating administrator receives an explicit persisted grant in the same transaction. This is a convenience for the first configured route, not a role-based bypass: removing that grant removes the administrator's member-facing model visibility and eligibility for model-scoped access.

Each model has exactly one current name through its transactional creation/rename flow. A nullable unique `current_model_id` constraint prevents multiple current names. Renaming preserves the Model ID, bindings, and grants. The old name becomes a compatibility name until the optional `alias_expires_at` deadline; omitting the deadline expires it immediately. Expired and historical names remain globally reserved and cannot be reused, even by their original model. `ResolveModelName` resolves only active models through a current or unexpired compatibility name; callers must still enforce their own grant and Key checks.

Adding a binding always assigns weight `0`. Weight replacement must include every existing binding exactly once, use integer values from 0 to 100, and total 100 for each protocol. Positive weights require ready bindings. Invalid updates change nothing. The transaction locks the model and its connections in a stable order, so concurrent updates cannot publish mixed weights or bypass concurrent credential changes. Disabling or invalidating credentials can make a previously weighted binding unavailable; a stored positive weight never overrides current readiness.

Grant replacement validates all supplied users before deleting old grants, then commits the new set and its audit event atomically. Disabled or unknown users and duplicate IDs are rejected. An empty array revokes every direct user grant. Member-facing model lists expose only explicitly granted active models, without upstream connection or credential metadata. Model visibility alone does not claim that a route is currently callable.

## HTTP API

All paths below are relative to `/api/v1`. Management endpoints require a session and their independent permissions: `providers.read` for the Provider catalogue, `providers.write` for Provider/Connection/Credential changes and verification, `models.read_all` for the administrative Model catalogue, and `models.write` for Model changes and grantee candidates. Mutations also require same-origin validation, `X-CSRF-Token`, and JSON input. Errors use the shared sanitized `{code,message}` contract. All create operations return `201`; other successful operations return `200`.

| Method and path | Request | Response |
|---|---|---|
| `GET /admin/providers` | None | `{items: Provider[]}` |
| `POST /admin/providers` | `{name,connection_name,base_url,protocol,credential_name,secret}` | Provider with its initial connection and credential |
| `POST /admin/providers/:provider_id/connections` | `{name,base_url,protocol,credential_name,secret}` | Connection |
| `POST /admin/connections/:connection_id/credentials` | `{name,secret,priority}` | Credential metadata |
| `POST /admin/credentials/:credential_id/verify` | `{}` | `{verified,discovered_models,message}` |
| `PATCH /admin/credentials/:credential_id` | `{enabled}` | Credential metadata |
| `GET /admin/credentials/:credential_id/metadata` | None | Non-secret Credential metadata, `connection_id`, `etag`; quoted ETag header |
| `PUT /admin/credentials/:credential_id/metadata` | `{name,priority,reason}` and quoted `If-Match` | Current authoritative metadata after runtime publication |
| `DELETE /admin/credentials/:credential_id` | `{reason}` and quoted metadata `If-Match` | `{id,absent:true,runtime_applied:true}` after current absence and publication |
| `POST /admin/connections/:connection_id/models` | `{upstream_name}` | `{id,upstream_name}` |
| `PATCH /admin/provider-models/:provider_model_id` | `{etag,enabled?,supports_image_input?,supports_pdf_input?}` | Provider-model configuration |
| `GET /admin/models` | None | `{items: Model[]}` |
| `POST /admin/models` | `{name,provider_model_id}` | Model |
| `POST /admin/models/:model_id/bindings` | `{provider_model_id}` | Model with the new zero-weight binding |
| `PUT /admin/models/:model_id/weights` | `{weights:[{binding_id,weight}]}` | Model |
| `POST /admin/models/:model_id/rename` | `{name,alias_expires_at?}` | Model |
| `PUT /admin/models/:model_id/grants` | `{user_ids:[]}` | Model |
| `GET /admin/model-grantees` | None | `{items:[{id,email,name}]}` for active users |
| `GET /models` | None | `{items:[{id,name,status,protocol,protocols,input_capabilities}]}` for the current user's grants; any authenticated role |

Connections support the native `openai_chat`, `openai_responses`,
`anthropic_messages`, and `gemini_generate_content` protocols. Credential secrets
contain 1–2,048 bytes and cannot contain CR/LF. Priority is an integer from 0 to
10,000. Labels contain 1–100 Unicode characters after trimming.

Provider responses contain `{id,name,connections}`. Connections contain `{id,name,base_url,protocol,credentials,provider_models}`. Provider-model entries contain `{id,upstream_name,enabled,supports_image_input,supports_pdf_input,etag}`. Input capabilities default to false and are changed atomically with availability through the exact-ETag provider-model update endpoint. Credential metadata contains `{id,name,priority,enabled,verification_status,verified_at}`; the verification timestamp is nullable.

Member model responses include the currently eligible native `protocols` and an
`input_capabilities` map for those protocols. RouteX advertises `image` or `pdf`
only when every ready, enabled, positive-weight route for that protocol declares
the capability. Disabled, unready, and zero-weight routes cannot expand the
effective set. The stable value order is `image`, then `pdf`.

Administrator model responses contain `{id,name,status,names,bindings,granted_user_ids}`. Name entries contain `{name,is_current,expires_at}`. Binding entries contain `{id,provider_model_id,provider_id,connection_id,upstream_name,protocol,weight,ready}`. `ready` is derived from current credential coverage, not a persisted success flag.

## Audit and Verification

Provider, connection, credential, provider-model, binding, name, weight, and grant changes write audit events in their database transactions. Audit records contain actor, action, resource type, and stable resource ID, without secrets or request bodies. Credential verification also writes an audit event.

`testCatalogLifecycle` runs through the real router against each isolated PostgreSQL/MySQL database under `go tool task test-integration`. It uses a controlled HTTP upstream to verify actual bearer authentication and model discovery. Tests cover encryption references, response redaction, verification and enablement gates, limited credential coverage, exact name comparisons, zero-weight candidates, atomic concurrent weight updates, name compatibility and expiry, historical-name reservation, explicit grant visibility, failed grant rollback, member authorization failures, CSRF enforcement, failed reverification, and audit persistence.

The Credential metadata and deletion lifecycle cases add reviewed ETag conflicts,
portable connection-local names, strict input and independent permissions,
transactional audit rollback/redaction, unchanged ciphertext and discovery state,
priority-zero updates, publication failure and exact retries, absent-target
reconciliation, removed discovery references, retained immutable call/attempt
history, last-ready-credential unavailability, and already dispatched call
completion. The full integration harness opens a fresh production-configured
connection pool after each schema reset, so driver statement caches cannot retain
result shapes from earlier frozen-migration fixtures.

Frontend tests cover the existing row menu, verification-before-enablement,
metadata and deletion dialogs, draft/conflict/retry boundaries, malformed success
responses, resource changes, independent permissions and paired English/Chinese
copy. Browser preview/cancellation evidence is separate from actual API mutation
and runtime-enforcement acceptance.

Controlled upstream tests do not replace a real supplier smoke test. Provider credentials supplied for production, complete protocol support, per-model inference validation, gateway execution, rotation workflows, and full provider lifecycle management have separate acceptance requirements. See [the implementation record](IMPLEMENTATION.md) for current phase evidence and remaining scope.

Provider model availability is managed independently of credential verification,
routing bindings and prices. See [PROVIDER_MODELS](PROVIDER_MODELS.md) for state
changes, gateway eligibility, ETags and publication recovery.
