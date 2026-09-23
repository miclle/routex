# Provider and Model Catalog

This phase implements persistent provider configuration, encrypted credential storage, real OpenAI model discovery, stable model identities, explicit user grants, and validated routing weights. It is the foundation for the first gateway protocol; catalog verification does not prove that inference requests have succeeded.

## Domain Boundaries

| Entity | Responsibility |
|---|---|
| `Provider` (`prv_`) | Stable supplier identity and display name |
| `ProviderConnection` (`con_`) | Supplier-specific Base URL, protocol, and connection name |
| `ProviderCredential` (`crd_`) | Encrypted secret, priority, verification state, and explicit enabled state for one connection |
| `ProviderModel` (`pmd_`) | Exact upstream model identifier accepted by one connection |
| `CredentialModelAccess` | Models actually returned by discovery using one credential |
| `Model` (`mdl_`) | Stable authorized identity and lifecycle status |
| `ModelName` | Globally reserved current, compatibility, and historical public names |
| `ModelProviderBinding` (`bnd_`) | A model's explicit supplier model relationship and weight |
| `UserModelGrant` | Explicit user-to-model permission by stable IDs |

Providers do not own transport details or secrets directly. Prices will belong to provider models in a later phase. Credential priority does not change supplier binding weights.

Schema version 3 adds these tables without changing the published authentication migrations. Foreign keys protect their relationships. Public names and upstream names use exact, case-sensitive comparison on both databases; MySQL explicitly uses `utf8mb4_bin`. Model names accept 1–128 ASCII letters, digits, `.`, `_`, `:`, `/`, and `-`, starting with a letter or digit. Upstream identifiers retain their original Unicode spelling and case.

## Credential Lifecycle and Verification

Credential creation encrypts the supplied secret with the configured root key and authenticates its immutable credential ID as the encryption reference. The database stores ciphertext only. Listing DTOs omit both plaintext and ciphertext; authentication and catalog SQL use the non-interpolating, silent service database session.

New credentials have `verification_status: "pending"` and `enabled: false`. Verification performs a real `GET <base_url>/models` with the credential's bearer token. The response must be HTTP 200 and contain a `data` array of valid model IDs. Discovery is limited to 2 MiB, 2,000 returned entries, and a 10-second timeout. Duplicate IDs are deduplicated. Neither upstream error bodies nor transport error details are returned to users.

A successful verification records `verified`, the verification time, and the exact discovered model coverage. It does not enable a new credential. Administrators must explicitly enable it. Verification failure records `failed`, clears its discovered coverage, and disables it. Reverification replaces coverage atomically; if previously active models are no longer covered, that credential is disabled until a valid explicit enable operation.

Enabling a credential requires successful verification and coverage of every model currently assigned positive routing weight on that connection. A binding is ready only when the connection has at least one enabled, verified credential and every such credential has discovered that provider model. Manually entered provider models receive no invented verification result; they must appear in real discovery before activation. This phase verifies discovery and authorization, not per-model inference capability or complete credential-pool failover. Those remain separate acceptance requirements.

Base URLs and outbound connections follow the upstream client's SSRF policy. Public HTTPS is the default. Private or local test endpoints require explicit development configuration. Redirects and DNS resolution follow the same policy; credentials must not be forwarded to an unvalidated destination. Missing root-key configuration returns `503` when credential storage is required, without preventing local identity operations.

## Models, Names, Grants, and Weights

Creating a model requires a provider model and creates an initial binding with weight `0`. The creating administrator receives an explicit persisted grant in the same transaction. This is a convenience for the first configured route, not a role-based bypass: removing that grant removes the administrator's member-facing model visibility and eligibility for model-scoped access.

Each model has exactly one current name through its transactional creation/rename flow. A nullable unique `current_model_id` constraint prevents multiple current names. Renaming preserves the Model ID, bindings, and grants. The old name becomes a compatibility name until the optional `alias_expires_at` deadline; omitting the deadline expires it immediately. Expired and historical names remain globally reserved and cannot be reused, even by their original model. `ResolveModelName` resolves only active models through a current or unexpired compatibility name; callers must still enforce their own grant and Key checks.

Adding a binding always assigns weight `0`. Weight replacement must include every existing binding exactly once, use integer values from 0 to 100, and total 100 for each protocol. Positive weights require ready bindings. Invalid updates change nothing. The transaction locks the model and its connections in a stable order, so concurrent updates cannot publish mixed weights or bypass concurrent credential changes. Disabling or invalidating credentials can make a previously weighted binding unavailable; a stored positive weight never overrides current readiness.

Grant replacement validates all supplied users before deleting old grants, then commits the new set and its audit event atomically. Disabled or unknown users and duplicate IDs are rejected. An empty array revokes every direct user grant. Member-facing model lists expose only explicitly granted active models, without upstream connection or credential metadata. Model visibility alone does not claim that a route is currently callable.

## HTTP API

All paths below are relative to `/api/v1`. Administrator endpoints require a session and the `admin` role. Mutations also require same-origin validation, `X-CSRF-Token`, and JSON input. Errors use the shared sanitized `{code,message}` contract. All create operations return `201`; other successful operations return `200`.

| Method and path | Request | Response |
|---|---|---|
| `GET /admin/providers` | None | `{items: Provider[]}` |
| `POST /admin/providers` | `{name,connection_name,base_url,protocol,credential_name,secret}` | Provider with its initial connection and credential |
| `POST /admin/providers/:provider_id/connections` | `{name,base_url,protocol,credential_name,secret}` | Connection |
| `POST /admin/connections/:connection_id/credentials` | `{name,secret,priority}` | Credential metadata |
| `POST /admin/credentials/:credential_id/verify` | `{}` | `{verified,discovered_models,message}` |
| `PATCH /admin/credentials/:credential_id` | `{enabled}` | Credential metadata |
| `POST /admin/connections/:connection_id/models` | `{upstream_name}` | `{id,upstream_name}` |
| `GET /admin/models` | None | `{items: Model[]}` |
| `POST /admin/models` | `{name,provider_model_id}` | Model |
| `POST /admin/models/:model_id/bindings` | `{provider_model_id}` | Model with the new zero-weight binding |
| `PUT /admin/models/:model_id/weights` | `{weights:[{binding_id,weight}]}` | Model |
| `POST /admin/models/:model_id/rename` | `{name,alias_expires_at?}` | Model |
| `PUT /admin/models/:model_id/grants` | `{user_ids:[]}` | Model |
| `GET /admin/model-grantees` | None | `{items:[{id,email,name}]}` for active users |
| `GET /models` | None | `{items:[{id,name,status,protocol}]}` for the current user's grants; any authenticated role |

The only connection protocol in this phase is `openai_chat`. Credential secrets contain 1–2,048 bytes and cannot contain CR/LF. Priority is an integer from 0 to 10,000. Labels contain 1–100 Unicode characters after trimming.

Provider responses contain `{id,name,connections}`. Connections contain `{id,name,base_url,protocol,credentials,provider_models}`. Credential metadata contains `{id,name,priority,enabled,verification_status,verified_at}`; the verification timestamp is nullable.

Administrator model responses contain `{id,name,status,names,bindings,granted_user_ids}`. Name entries contain `{name,is_current,expires_at}`. Binding entries contain `{id,provider_model_id,provider_id,connection_id,upstream_name,protocol,weight,ready}`. `ready` is derived from current credential coverage, not a persisted success flag.

## Audit and Verification

Provider, connection, credential, provider-model, binding, name, weight, and grant changes write audit events in their database transactions. Audit records contain actor, action, resource type, and stable resource ID, without secrets or request bodies. Credential verification also writes an audit event.

`testCatalogLifecycle` runs through the real router against each isolated PostgreSQL/MySQL database under `go tool task test-integration`. It uses a controlled HTTP upstream to verify actual bearer authentication and model discovery. Tests cover encryption references, response redaction, verification and enablement gates, limited credential coverage, exact name comparisons, zero-weight candidates, atomic concurrent weight updates, name compatibility and expiry, historical-name reservation, explicit grant visibility, failed grant rollback, member authorization failures, CSRF enforcement, failed reverification, and audit persistence.

Controlled upstream tests do not replace a real supplier smoke test. Provider credentials supplied for production, complete protocol support, per-model inference validation, gateway execution, rotation workflows, and full provider lifecycle management have separate acceptance requirements. See [the implementation record](IMPLEMENTATION.md) for current phase evidence and remaining scope.

Provider model availability is managed independently of credential verification,
routing bindings and prices. See [PROVIDER_MODELS](PROVIDER_MODELS.md) for state
changes, gateway eligibility, ETags and publication recovery.
