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
is introduced. New internal attempt facts retain exact Credential and publication
IDs without live catalog foreign keys; deletion preserves that history. Older
missing IDs remain unknown and are never inferred from the remaining catalog.
See [internal attempt attribution](CALLS.md#internal-attempt-attribution).

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

### Staged replacement preparation

The existing row menu opens the approved replacement-dialog composition with
source/Connection context, the inherited priority, a new name and secret, and a
required reason. This step creates a separate pending disabled Credential and
retains the predecessor unchanged. The interface describes the saved preparation
step accurately; it cannot simulate verification or claim completed rotation.
Actual Verify and explicit Enable remain separate existing operations.

`POST /admin/credentials/:credential_id/replacements` requires `providers.write`,
same-origin validation, CSRF, a reviewed source metadata `If-Match`, and strict
JSON `{request_id,name,secret,reason}`. Request IDs are lowercase canonical UUIDv4
values. Name and reason follow metadata bounds; the new secret follows the
existing 1–2,048-byte/no-CRLF contract. Connection identity and priority derive from
the reviewed source, without caller-controlled routing or lifecycle fields.

Frozen GORM schema version 31 adds nullable immutable `replaces_credential_id`
lineage and a durable creation-receipt table. Historical IDs have no live foreign
keys to deletable Credential records. A governance-authorized transaction locks
Connection then source, checks the reviewed representation and portable name
uniqueness, encrypts the new secret under its own new ID, and creates the new
Credential, receipt and typed `credential.replacement.create` audit together.
No discovery coverage is copied. Preparation does not refresh runtime: a pending
disabled record with no coverage cannot expand active routes or authorization.

A new preparation returns `201`; an exact repeat returns `200`. Both return only
`{id,connection_id,replaces_credential_id}`, acknowledging saved preparation,
without claiming routing application. The receipt binds actor/source/Connection/
result identities and a canonical hash of non-secret intent. It never stores a
secret, an unkeyed secret hash, or ciphertext copies. Before retry reconciliation,
the server reauthorizes the actor and checks the receipt, then compares the
submitted secret to the result's immutable encrypted secret inside the authorized
service. Comparison uses fixed-length in-memory digests only. The same request ID
cannot identify another actor, source, reviewed ETag, name, reason or secret.

Receipts survive source and result deletion. An exact retry may return an existing
result after source deletion; a deleted result or unavailable Secret Store cannot
create another Credential. Distinct request IDs with distinct Connection-local
names are independent preparations; there is no single-successor restriction.
Catalogue rows expose historical predecessor IDs even when the source no longer
exists, without revealing secret fragments or fetching another resource.

The browser retains the new secret and captured request only in component state.
Transport errors, unavailable storage, and malformed success receipts remain
uncertain; retry the original UUID/body/ETag. Every rejected uncertain retry keeps
that original uncertainty. Reading or reviewing the source cannot prove whether a
replacement was created and cannot unlock another intent. Normal source conflicts
require explicit review before a fresh request ID. Dismissal, success, navigation
and unmount clear sensitive state; no mutation cache or browser storage is used.

Planned retirement requires exact
new-Credential configuration application and an authoritative successful inference
using that new Credential. Exact internal attempt Credential/publication IDs are
available, and parser-owned native terminal evidence now distinguishes completed,
handoff, blocked, incomplete and unknown outcomes. Scoped current runtime
readback and the receipt-backed predecessor retirement gate are available. HTTP success, global runtime readiness, discovery or
a model's success cannot establish the complete retirement gate. Equal priority does not prove the new Credential receives traffic.
The existing emergency disable action remains independently available; supplier-
side revocation is outside this preparation endpoint.

## Models, Names, Grants, and Weights

Creating a model requires a provider model and creates an initial binding with weight `0`. The creating administrator receives an explicit persisted grant in the same transaction. This is a convenience for the first configured route, not a role-based bypass: removing that grant removes the administrator's member-facing model visibility and eligibility for model-scoped access.

Each model has exactly one current name through its transactional creation/rename flow. A nullable unique `current_model_id` constraint prevents multiple current names. Renaming preserves the Model ID, bindings, and grants. The old name becomes a compatibility name until the optional `alias_expires_at` deadline; omitting the deadline expires it immediately. Expired and historical names remain globally reserved and cannot be reused, even by their original model. `ResolveModelName` resolves only active models through a current or unexpired compatibility name; callers must still enforce their own grant and Key checks.

Adding a binding always assigns weight `0`. Weight replacement must include every existing binding exactly once, use integer values from 0 to 100, and total 100 for each protocol. Positive weights require ready bindings. Invalid updates change nothing. The transaction locks the model and its connections in a stable order, so concurrent updates cannot publish mixed weights or bypass concurrent credential changes. Disabling or invalidating credentials can make a previously weighted binding unavailable; a stored positive weight never overrides current readiness.

Grant replacement validates all supplied users before deleting old grants, then commits the new set and its audit event atomically. Disabled or unknown users and duplicate IDs are rejected. An empty array revokes every direct user grant. Member-facing model lists expose only explicitly granted active models, without upstream connection or credential metadata. Model visibility alone does not claim that a route is currently callable.

## HTTP API

All paths below are relative to `/api/v1`. Management endpoints require a session and their independent permissions: `providers.read` for the Provider catalogue, `providers.write` for Provider/Connection/Credential changes and verification, `models.read_all` for the administrative Model catalogue, and `models.write` for Model changes and grantee candidates. Mutations also require same-origin validation, `X-CSRF-Token`, and JSON input. Errors use the shared sanitized `{code,message}` contract. New create operations return `201`; an exact persisted replacement-creation retry returns `200`. Other successful operations return `200`.

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
| `POST /admin/credentials/:credential_id/replacements` | `{request_id,name,secret,reason}` and reviewed source `If-Match` | Saved preparation `{id,connection_id,replaces_credential_id}` |
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
| `GET /model-catalog` | None | Actor-scoped `{items: MemberModelCatalogRecord[]}`; personal and active Team visibility |
| `GET /model-catalog/:model_id` | None | Freshly authorized `MemberModelCatalogRecord`; unavailable visibility returns `404` |

Connections support the native `openai_chat`, `openai_responses`,
`anthropic_messages`, and `gemini_generate_content` protocols. Credential secrets
contain 1–2,048 bytes and cannot contain CR/LF. Priority is an integer from 0 to
10,000. Labels contain 1–100 Unicode characters after trimming.

Provider responses contain `{id,name,connections}`. Connections contain `{id,name,base_url,protocol,credentials,provider_models}`. Provider-model entries contain `{id,upstream_name,enabled,supports_image_input,supports_pdf_input,etag}`. Input capabilities default to false and are changed atomically with availability through the exact-ETag provider-model update endpoint. Catalogue Credential metadata contains `{id,name,priority,enabled,verification_status,verified_at,replaces_credential_id}`; the verification timestamp and historical predecessor ID are nullable. The dedicated reviewed metadata response retains its eight-field representation and does not include lineage.

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


## Credential retirement readiness

An advisory read-only review is available through
`GET /api/v1/admin/credentials/:credential_id/retirement-readiness` with exactly
one `replacement_credential_id` query and independent `providers.read` authority.
It does not disable either Credential, save an operation intent or receipt, or
perform supplier-side revocation. Invalid identities return 400, missing records
404, and existing mismatched lineage or Connection 409.

The explicit representation includes safe `source`/`replacement` metadata,
nullable `snapshot_id`, nullable `evidence {attempt_id,completed_at}`, bounded
`eligible_route_count`, `eligible`, canonical `blockers` and aggregate `etag` with
a matching strong ETag header. Eligibility requires current replacement
verification/enablement, all required positive-route coverage, actual locally
published candidate/transport/authorization eligibility and exact durable native
completed terminal evidence from the replacement under the current configuration.
Generic HTTP success, discovery, usage or global runtime readiness is insufficient.

The source-digest comparison is specific to readiness; ordinary gateway
authorization publication and last-valid routing behavior remain independent.
Bounded runtime capture, consistent current database projection and recapture
avoid waiting for the runtime mutex while borrowing a database connection.
Concurrent changes, unavailable publication, stale scope, cooldown/tombstones,
missing proof and overflow conservatively block readiness. Historical blank
Credential/configuration/native fields are never backfilled.

The existing replacement row menu opens a local Base UI review dialog with
English/Chinese guidance, Refresh and Close. Server facts stay resource-scoped;
previous eligibility is hidden during refresh/error and arbitrary blocker codes
are never rendered as labels. The review concerns this processing instance,
not a fleet acknowledgment or completed retirement. API/UI and controlled
acceptance passed: full check/test (639 Vitest cases in 52 files, Go race/unit,
Node/development lifecycle/production assets), independent review and focused
PostgreSQL/MySQL catalog/replacement/readiness acceptance (160.016 seconds). The
final complete dual-database race matrix passed (Handler 429.433 seconds, Service
5.336 seconds); owned Compose resources were removed. These controlled fixtures
do not replace supplier or fleet acceptance. Receipt-backed planned retirement
follows as a separately checked write package.


## Planned Credential retirement

The existing replacement readiness dialog adds a required reason and an explicit
confirmation for `POST /api/v1/admin/credentials/:credential_id/retire`.
Read and write permissions remain independent. The write requires
`providers.write`, same-origin validation, CSRF and the reviewed strong aggregate
`If-Match`. Its strict JSON contains `request_id` (canonical UUIDv4),
`replacement_credential_id`, `evidence_attempt_id`, `snapshot_id` and `reason`.
The server revalidates the exact reviewed successful terminal native completion;
a later successful call cannot silently replace that proof or invalidate an
otherwise unchanged reviewed intent. The non-empty trimmed reason has the same
1,024-byte UTF-8/control-character limits as metadata edits.

For a fresh intent, publication pinning precedes database connection borrowing.
Governance, sorted Model, Connection and sorted Credential locks protect the
current authorization, lineage, metadata, complete bounded route scope and proof.
A changed scope or configuration requires a new explicit review. The predecessor
must still be enabled; the successor must be verified, explicitly enabled and
present in every required current positive native route. Discovery, HTTP 200,
usage completeness and global readiness do not substitute for native completion.
The transaction disables only the predecessor and atomically creates a frozen
V34 retirement receipt and a typed `credential.retire` audit event. The receipt
has no live foreign keys. Fresh commit installs the predecessor tombstone before
releasing the publication pin, then refreshes runtime. Existing dispatched calls
are not cancelled.

Fresh authorization precedes receipt lookup. An exact historical retry uses the
same actor, IDs, original validator and body; mismatched UUID reuse returns 409.
It never disables a re-enabled predecessor, installs a new tombstone or duplicates
an audit. Historical receipt lookup precedes current record/proof existence
checks. Deleting call evidence or changing configuration after the known disable
does not erase the saved commit or require a fresh inference merely to reconcile
it. A later intentionally re-enabled predecessor needs a fresh intent and current
review to retire again.

HTTP 200 acknowledges known `committed: true` independently of
`runtime_applied`. Its explicit response includes the retirement request UUID,
source/replacement IDs, `committed_at`, nullable `current_snapshot_id` and bounded
safe application blockers. Current application requires the predecessor to exist
and remain disabled, its successor to remain valid with complete current route
coverage, locally coherent publication and predecessor exclusion from new dispatch.
Missing/re-enabled predecessors, missing successors, stale publication or unavailable
routes preserve the historical receipt but cannot report current application.
Supplier revocation and other instances remain separate acceptance boundaries.

The dialog preserves immutable intent through network/uncertain publication
results, saved-but-unapplied receipts, refresh failures and rejected retries.
It does not optimistically change rows or treat refreshing readiness as proof of
an uncertain write. Conflict review retains the reason but requires explicit
acceptance of the new ETag before a fresh request. English and Chinese notices
update without losing the draft; retirement requests never enter mutation caches.

Full format/check/test passed, including 650 Vitest cases in 53 files, Go
race/unit checks, development lifecycle and embedded production assets. Focused
frontend acceptance passed 39 cases. Focused real PostgreSQL/MySQL acceptance
passed in 159.203 seconds; the complete race matrix passed (Handler 449.057
seconds, Service 5.332 seconds). Independent frozen review found no remaining
actionable defect. A disposable production PostgreSQL process and controlled
native upstream passed the built-in browser reason/confirmation/receipt workflow:
the saved receipt confirmed local application, the predecessor became disabled
and the successor remained enabled. Chinese switching passed with no console
errors; owned resources were removed. First durable admission activates quota
accounting and can publish a new configuration, so the controlled fixture obtained
its actual successor completion after that publication. No supplier/fleet
acceptance or whole-F11 completion is claimed.


## Member model availability and examples

The member catalogue separates visibility from current API availability. Its
available statistic counts active models with at least one eligible supported
native protocol. Explicit empty protocol lists remain empty; unknown protocols and
disabled/archived models never select an implicit Chat fallback. The API drawer
retains the existing native examples for eligible routes and the Gemini public
name guard. Without a usable protocol it shows English/Chinese unavailable
guidance, with no fabricated endpoint/example, enabled copy action or suggestion
that creating a Key repairs routing. This frontend correction does not expand
`GET /api/v1/models`, direct grants, Personal Keys or Team invocation.

The focused correction passed 15 dedicated UI tests and complete format/check/test
(665 Vitest cases in 54 files, Go race/unit, Node checks, development lifecycle
and embedded production assets). Open drawers follow refreshed catalogue
eligibility instead of retaining a copied model object.


## Actor-scoped member model directory

`GET /api/v1/model-catalog` combines explicit direct personal grants with explicit
model grants from active Teams and the actor's active memberships. It returns
each active logical model once, preserving each actual source. Empty Team grants
confer no access; platform administration does not imply a personal grant or an
unscoped catalogue. The existing `/api/v1/models`, Personal Key creation ceiling
and inference authorization remain unchanged.

Each record contains only `id`, current `name`, `status`, authoritative UTC
`created_at`, eligible native `protocols`, per-protocol `input_capabilities`,
`personal_available` and `sources`. Personal sources have null Team fields and
`invocation_supported: true` for the implemented personal authentication path.
Team sources contain their authorized Team ID/name and
`invocation_supported: false`; Team visibility does not implement native Team
execution. Personal availability requires a direct grant and an eligible route,
and does not promise that a particular Key, quota or request will be accepted.
No Provider topology, credentials, global member counts, price assumption or
other users' grants appear.

List queries have complete-result bounds of 1,000 distinct models, 100 distinct
grant-bearing Team sources and 5,000 source rows. Overflow returns sanitized
HTTP `422`; no partial list or fabricated count is returned. Detail applies
bounds only to the requested model's sources and reauthorizes current visibility
on each GET, returning `404` after the last grant/membership is revoked. Unsupported
query parameters return `400`. Source reads use one read-only repeatable-read
transaction and exact identity checks, including Model name ownership, before
borrowing a connection for route metadata; a single-connection pool must remain
usable. Native capability intersection follows the existing discovery contract.

The existing member page keeps its card/table/520px drawer composition. Four
statistics describe actual models, personally available models, native protocols
and grant sources. Name, source, protocol and explicitly declared image/PDF
capability filters are conjunctive; name search is literal. Two source labels and
an all-source overflow preserve the exact deduplicated source set. Team sources
never imply a Team Key. Drawer data comes from an independently authorized
actor/model query, never from the list as a permission fallback; cached detail
is hidden during refresh, error or revoked access. Working native examples and
Key navigation require confirmed current personal availability. Historical
creation time is known; route-dependent prices and global member/request facts
remain unknown where the layout displays them.

Parallel backend/UI ownership delivered this scope. Dedicated frontend coverage
passed 27 cases plus eight i18n cases. Complete format/check/test passed (677
Vitest cases in 54 files, Go race/unit, Node checks, development lifecycle and
embedded production assets). Focused real PostgreSQL/MySQL acceptance passed
(Handler 160.920 seconds), covering source deduplication, a ready Team-only model,
Personal Key denial, revocation, exact identity/case-folding defenses, all three
overflow bounds, detail isolation and single-connection metadata reads.

A disposable production process with PostgreSQL and a controlled native upstream
passed browser source overflow, ready Team-only denial, Personal native examples,
authorization revocation with fresh inaccessible details, image filtering, table
composition and English/Chinese switching. No console errors were recorded.
Owned test processes, tab and Compose resources were removed. Personal/Team model
permission requests, explicit Team invocation and broader price/usage contracts
remain unfinished; full F19 and external-provider acceptance are not established.

The complete PostgreSQL/MySQL race integration matrix also passed (Handler
488.717 seconds, Service 5.727 seconds). This does not establish external-provider
or full-platform acceptance.
