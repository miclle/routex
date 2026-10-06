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

Enabling a credential requires successful verification and coverage of every model currently assigned positive routing weight on that connection. A binding is displayed as ready only when its Provider Model is enabled, the connection has at least one enabled, verified credential, and every such credential has discovered that Provider Model. Weight configuration independently retains the credential-coverage rule, so temporarily disabling supply does not erase weights or prevent an otherwise valid weight update. Manually entered provider models receive no invented verification result; they must appear in real discovery before activation. This phase verifies discovery and authorization, not per-model inference capability or complete credential-pool failover. Those remain separate acceptance requirements.

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

A retained compatibility name can be stopped early from its Model information
card. Review reads one exact name under `models.read_all`; confirmation requires
independent `models.write`, a non-empty reason and the reviewed strong `If-Match`.
The transaction only shortens the recorded deadline and commits one typed audit.
It preserves the Model, current name, bindings, grants and permanent name
reservation. Current names cannot be stopped through this action. Conflicts
retain the draft for explicit review; uncertain results retain the original
request. A matching retired target retry confirms current state/publication,
including natural expiry, without proving the original historical operation.
Saved retirement and runtime application remain separate; failed publication
keeps lookup fail-closed until an authorized exact retry republishes.

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
| `GET /admin/models/:model_id/alias-retirement` | Single exact `name` query | Private review/state/editability/runtime application and strong ETag |
| `POST /admin/models/:model_id/alias-retirement` | `{name,reason}` and reviewed quoted `If-Match` | `{alias,retired,changed,runtime_applied}`; current-target confirmation, no historical receipt |
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
`input_price`, `output_price`, `personal_available` and `sources`. Personal sources have null Team fields and
`invocation_supported: true` for the implemented personal authentication path.
Team sources contain their authorized Team ID/name and independently ready
`invocation_protocols`; input capabilities remain on the Model's per-protocol map.
`invocation_supported: false` preserves the Personal Key separation; it does not
mean the Team's native Session paths are unavailable. Current Team execution still
requires exact enabled Session/User/membership authority and native admission.
Personal availability requires a direct grant and an eligible route,
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
is hidden during refresh, error or revoked access. Native examples require the
explicit currently available Personal or Team source and its ready protocol;
Key navigation requires confirmed current personal availability. Historical
creation time is known. Input/output base-price cells use the current server-owned
projection described below; global member/request facts remain unknown where the
layout displays them. Requestable candidate discovery has no price projection.

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
Owned test processes, tab and Compose resources were removed. At that directory
checkpoint, Personal/Team requests and explicit Team invocation were unfinished;
later sections record those implementations. Broader price/usage, full F19 and
external-provider acceptance remain independent.

The complete PostgreSQL/MySQL race integration matrix also passed (Handler
488.717 seconds, Service 5.727 seconds). This does not establish external-provider
or full-platform acceptance.

## Personal access requests

[Personal Model requests](PERSONAL_MODEL_REQUESTS.md) expose a separate minimal
candidate catalogue and scoped history through the existing catalogue composition.
A pending request creates no grant; approval adds only direct Personal access and
never expands an existing Key. Granted discovery and Team sources retain their
existing authorization contracts.

## Shared Team access requests

[Team Model requests](TEAM_MODEL_REQUESTS.md) use explicit active Team selection
and shared pending uniqueness in the existing catalogue drawer. Approval changes
only the selected Team grant. Own history remains available after membership loss;
current facts become unavailable without renewed resource authority. Team Models
includes independently scoped review without requiring a global directory.

Team source invocation protocols now reflect each current ready native Chat,
Responses, Messages and Gemini route. Source-specific Playground links work for
models without a Chat route. Team visibility and Session invocation do not provide
Personal Key availability; native Team discovery advertises no media capability.

## Scoped administrative detail and route prices

GET /api/v1/admin/models/:model_id reauthorizes the exact current enabled actor
and Model with models.read_all in a coherent read snapshot. It returns the existing
Model DTO without borrowing a target from the global list or selecting credential
secrets. Exact relationship and readiness comparisons preserve supported driver
collations. GET /api/v1/admin/provider-models/:provider_model_id/price retains its
separate prices.read gate and exact live subject; pricing generation, rates and
exchange data are coherent. Neither read grants write or Provider-directory access.

The existing Model routing table shows each route's base input/output amounts,
currency and per-million unit. Known zero, disabled and absent rates stay distinct.
Media and long-context rates are not substitutes. Model/actor/session-specific
queries renew authority and suppress old details during errors or refresh. No
consolidated logical Model price, new navigation or price editing is introduced.
Focused PostgreSQL/MySQL catalogue, scoped detail, price and call-assessment
regression passed under race detection (129.542 seconds). Full local check/test
passed 1207 Vitest cases in 79 files, Go race/unit, development lifecycle and
production embedded assets. Controlled bilingual production browser proof passed
exact decimal/zero/disabled prices, absent schedules, separate Model/price reads,
disabled writes, Session/price persistence after restart and hidden old details
following permission revocation. English was restored, browser errors were empty
and owned QA resources were removed. The complete main-branch PostgreSQL/MySQL matrix passed under race detection
(Handler 971.759 seconds, Service 7.422 seconds); owned Compose resources were
removed. All required local gates passed before this phase delivery.


Disabled supply readiness passed focused real PostgreSQL/MySQL race proof in
101.402 seconds and the complete matrix (Handler 1012.087 seconds, Service
7.001 seconds). Full check/test passed 1207 frontend cases, Go race/unit,
development lifecycle and embedded production assets. Enabling/disabling supply
retains exact prices, grants, weights and names; disabled native routes made zero
upstream dispatches. Owned test resources were removed.


## Guided Model creation

The existing `/admin/models/new` page selects one authorized Provider Connection
and a bounded set of discovered Provider-model items. Connection/protocol identity
is explicit. Pickers require models.read_all and providers.read; committing is an
independent models.write operation. Literal search/cursors remain server-owned;
selected items outside a bounded response stay selected. A batch contains 1–50
unique Provider-model selections, with a new logical Model name or an exact
existing Model target for each item.

Server preview derives valid per-protocol topology: new Models and the first
route for a previously absent protocol receive weight 100; an added backup for
an existing valid protocol receives 0. Existing routing weights are preserved.
Invalid all-zero supply is rejected rather than silently repaired. Ordinary
single-binding APIs retain their existing initial-zero contract. No User, Team
or Project grants are created, and no existing Key scope expands.

Explicit Base UI confirmation submits the reviewed strong If-Match, reason and
one UUIDv4 intent. One transaction writes the bounded Model/name/binding changes,
durable historical receipt and typed audit. Exact same-intent retry cannot replay
creation or restore later topology. Permission, actor, Connection, Provider-model,
Model, name reservation and reviewed revisions are checked independently of
database collation. Private picker/result data is hidden during renewed authority
reads; original uncertain intent survives until explicitly reconciled.

A receipt confirms the saved operation. Current items and pending/applied,
superseded or unavailable application status are separately authorized and proven.
Unavailable subjects never revive a receipt's old names or routes. Configured
backup0 publication does not establish traffic eligibility. Publication proof
includes current encrypted-credential identity, without exposing secrets.

| Method | Endpoint | Purpose |
| --- | --- | --- |
| GET | `/api/v1/admin/model-creation/connections` | Bounded authorized Connections |
| GET | `/api/v1/admin/connections/:connection_id/model-creation` | Exact reviewed Connection context |
| GET | `/api/v1/admin/connections/:connection_id/model-creation/provider-models` | Bounded Provider-model choices |
| GET | `/api/v1/admin/connections/:connection_id/model-creation/models` | Bounded existing targets |
| POST | `/api/v1/admin/connections/:connection_id/model-creation/preview` | Server-derived difference and ETag |
| POST | `/api/v1/admin/connections/:connection_id/model-creation` | Confirmed atomic creation |
| GET | `/api/v1/admin/model-creation/receipts/:request_id` | Authorized historical receipt/current proof |

Guided creation was delivered on main `4fed603` after source, dual-driver,
production/browser/native/restart and complete regression acceptance. External
provider and wider Model capability acceptance remain independent.


## Advisory public Model names

The guided creation name control now offers advisory suggestions through the local
Base UI Autocomplete while retaining fully custom input. The versioned local
`website/src/data/public-model-references.v1.json` contains four reviewed exact
identities: `gpt-5.2`, `gpt-5.2-2025-12-11`, `claude-sonnet-4-6` and
`gemini-2.5-flash`. Each entry records its official documentation URL and review
date. These are reviewed names, not claims of current availability or the latest
provider release. No reference API, network catalogue synchronization or server
endpoint is added.

The parser accepts only the version-1 schema, at most 100 unique bounded ASCII
names, canonical credential-free HTTPS URLs on reviewed official hosts and valid
review dates. Malformed metadata disables suggestions without disabling custom
input. Search is literal and case-insensitive, returns original exact spelling,
excludes only exact selected sibling names and displays at most eight results.
The existing server preview remains authoritative for name reservation/collision.

Arrow navigation does not fill the field. Explicit pointer or Enter selection
changes only that row's name; Escape dismisses suggestions while retaining custom
text. Selection changes no Connection, Provider-model, row mode, reason, sibling
row, routing weight, price, grant or credential. It makes no creation request and
claims no protocol, media capability, capacity or supply readiness. The existing
server preview, required reason, explicit confirmation and immutable UUID receipt
workflow are unchanged. Only that independently confirmed operation can create a
Model or binding.

Actor, exact Connection and row identity, current mode/removal, synchronous busy
lock and fresh Session/permission/context/picker generations guard suggestion
updates. Renewed authority hides obsolete interactions; late option events cannot
restore private or cleared rows. Successful authority reads recreate the control.
English/Chinese helper copy changes live while exact custom text is retained.
Both frontend rule files append the same bounded contract, preserving all later
Member Overview and other instructions.

Final R2 acceptance passed 2162 frontend cases in 114 files, including 63 focused
cases in four files, source checks and production build. Controlled bilingual
browser/native/restart proof verified localized native dismissal controls, one
explicit creation receipt, three completed calls/dispatches, old-Key zero-dispatch
denial and separate grant/new-Key operations without implicit access. Restart
retained receipt/runtime/call facts without replay. Browser locale switching was
verified with a closed popup and reopen; open-popup switching has focused source
proof only. This frontend-only slice retains the accepted backend matrix and
changes no route, schema, permission or immutable call basis. Full independent
R1/R2 evidence is recorded in [Implementation](IMPLEMENTATION.md).

The checked implementation is delivered by the commit containing this acceptance
record; consult Git history for its SHA. Remote CI for that new commit remains
pending. F12 and formal 11/16/3 totals are unchanged; advisory names do not establish
official-provider supply or wider catalogue/routing acceptance.

## Explicit-source member examples

The existing card/table, 520px Model drawer, connection settings, inline cURL
example and model-request footer are retained. The example source selector is
independent of the catalogue filter. One source may initialize the selection;
multiple sources require an explicit choice. A removed source stays unavailable
and a protocol that loses readiness requires explicit reselection. No Personal
or Team fallback is selected silently. Responses-only, Messages-only and
Gemini-only Team sources use their own ready protocol subsets. The Gemini public
name guard remains in effect.

Personal examples retain their native Key environment-variable authentication.
Team examples use the exact selected Team and the existing standalone text
snippet builder. Its descriptor contains only origin, protocol, public Model name,
settings and sample text; no live Key, browser Session/CSRF, account credentials
or attachments. Running the Team program signs in separately using ROUTEX_EMAIL
and ROUTEX_PASSWORD, verifies a current authenticated Session/CSRF, then sends one
Team-native request. cURL requires Python 3 standard-library bootstrap. Login
HTTP 202, redirects or failed/malformed authority stop before inference. Merely
generating or copying an example performs no network or grant operation.

One catalogue Session observer uses successful network generations. List, request
discovery and exact detail queries include actor/generation, with detail also
including the Model ID. Structurally identical same-millisecond Session renewal
still requires renewed resource reads; manual same-actor CSRF cache replacement
does not create a renewal. Obsolete reads are canceled, private facts/actions
are hidden during renewal/errors, and synchronous authority guards protect copy
and navigation. Late clipboard completion cannot restore an obsolete notice;
an already issued clipboard operation cannot be undone. Actor changes clear
selection, history and private filters. Incidental same-actor/Model renewal keeps
mounted request captures without replay. Successful Personal requests invalidate
only the complete actor/Model candidate-drawer prefix, including its generations.

The API client validates complete bounded records, exact requested Model identity,
typed distinct Personal/Team sources, safe persisted IDs, timestamps and native
protocol/capability shapes, and discards unexpected fields before caching. It
accepts safe legacy IDs without assuming modern prefixes. No global Team, Provider,
Key, member or price directory is fetched; unrecorded price/usage/member facts
remain unknown. This candidate changes no backend route, migration, grant or
Key ceiling.

This eleven-path frontend delivery passed format, mandatory check, full tests
(2035 Vitest cases in 109 files, Go race/unit, Node development lifecycle and
production assets) and the embedded production build on baseline `4fed603`.
Four actual browser clipboard programs matched the current builder byte-for-byte
and executed four independent native Team requests. Exactly four immutable
completed calls/attempts retained their original Team/User/membership and exact
Credential/Provider-model/Connection/snapshot through protocol, grant and
membership removal/rejoin and a same-artifact process restart. No Personal call,
browser inference or model-request create occurred; no price was invented.

Default English, reopened Chinese guidance, multiple-source selection, current
minute Session reads, disabled stale examples and second-actor cache isolation
passed the actual browser scenario. Live-language draft retention and
same-millisecond generation/callback races remain focused regression evidence.
All owned services, Compose resources and the browser tab were removed. Backend
code and migrations are unchanged from the separately accepted `4fed603`
PostgreSQL/MySQL matrix; no redundant full database run is claimed. Broader F19
price/usage/overview and external-provider acceptance remain open.

## Reviewed Connection names

The existing Connections tab has six data columns and row actions, literal name and protocol
filters and a row-menu name dialog. Resource-scoped metadata GET requires
providers.read; PUT separately requires providers.write, a strong reviewed
If-Match, normalized name and reason. The token binds the exact Connection and
Provider incarnation and shared revision. Protocol, URL, egress and child
configuration are separate operations.

Only an authorized exact PUT with runtime_applied confirms the current name.
Retain original bytes/token through conflicts, response loss and AuthGate errors;
a matching current GET cannot prove the original historical operation. Explicit
Abandon and fresh review are required to replace an uncertain intent. Original
requests may reconcile current equality without another audit. Names/reasons use
Go White_Space normalization and retain U+FEFF.

The real-driver metadata cases pass in the complete124 matrix; the GORM field
update uses the model field ETag and its existing portable column mapping.
Main checking, complete Task testing and the production build pass. The first
controlled browser run verified read/write separation, filters, English/Chinese
views, two real conflicts and explicit review followed by a successful save.
Its fourth save actually returned 503 before the planned response-loss fault.
That failed run remains retained. A fresh run with unchanged product code passed
five browser PUTs: actual statuses 409, 409, 200, 200 and 200; the fourth successful
response was withheld as 503. Matching reads retained the original request.
An actual Session 200 withheld as 500 hid private content; manual Retry restored
the original actor. The same binary, configuration and database then restarted
without reloading authenticated documents. Fresh reads preceded the exact
original retry, which confirmed current runtime application with no additional
audit. Three typed name updates and independent owned-resource cleanup were
verified. This is controlled local acceptance, not external routing or historical
operation proof. The containing commit delivers this slice; F11 stays partial.


## Member catalogue configured base prices

Catalogue list and resource-scoped detail add required `input_price` and
`output_price` cells. Each is `{state, rate}` with `unauthorized`, `unavailable`,
`missing`, `heterogeneous`, `priced`, or `disabled`. Only priced/disabled has
`{amount, unit: "1M_TOKEN", currency}`; all other rates are null. Amounts remain
canonical decimal strings, including zero and 18 fractional places. The existing
cards and table show these facts without changing layout, filters or examples.

Price reading requires independent current `prices.read`, even for an intrinsic
administrator. Catalogue Model visibility and Personal/Team sources remain
separately authorized; price authority grants no Model. A fresh read-only snapshot
rechecks actor birth, exact source identity and permission after the independent
metadata fallback releases its connection. Unauthorized readers hydrate no prices,
Credentials or supplier metadata. Only already-visible exact Model IDs are batched.
This preserves single-connection operation and bounded query counts.

A price is the configured base rate across the complete coherent, ready, enabled,
positive-weight published route set. Equal amount/unit/currency/enablement yields
priced or disabled; entirely absent rates yield missing, and mixed/partly absent
rates yield heterogeneous. Missing/expired/incoherent publication and contention
remain unavailable. No chosen-route rate, quote, currency conversion or invocation
promise is inferred. Requestable candidates remain price-free and render Unknown.

Private source checks pass 34 related Go race tests and 127 scoped frontend cases,
including strict DTO/decimal validation, live language switching and held Session
renewal privacy. The existing catalogue lifecycle passes on real PostgreSQL and
MySQL with zero/18-place disabled rates, immediate permission removal, exact source
isolation and a single-connection pool. Main format/check/full Task and build pass
with 3,973 frontend cases in 163 files. Controlled production/browser acceptance
passes English cards, Chinese table, permission removal/restoration and original
Sessions after same-artifact restart, with zero inference calls. The composed
full126 transaction matrix passes 126 ordered scenarios per database, eight
constraints and 4,570 matched named results. The containing commit delivers this
bounded slice. No schema, endpoint, registry or dependency change is introduced.


## Member catalogue monthly requests

Selecting Personal or one exact named Team source enables one existing scoped
usage report for all visible Models. Personal counts reflect the current actor's
Personal calls; Team counts are shared aggregate calls, including other Team
members. All sources and requestable candidates remain Unknown rather than
summing or guessing. Recorded caller counts use that same selected-account
report; they count distinct recorded actors rather than current Model grantees or
Team members.

Reads use the server's UTC month-to-query period, separate from quota reset
configuration. The existing cards/table show returned from/to/queried-at and a
may-lag qualification. Only complete validated reports can show zero for an absent
current Model; denied, failed, incomplete and renewed reads hide old values.
Actor, source, Session and catalogue generations guard queries and refreshes.
Manual refresh reads the catalogue first; each successful metadata generation
then starts one fresh scoped report, even when its metadata is unchanged.
There are no per-row or administrative usage/directory reads. Prices, filters,
examples and layout remain intact. Counts include retained failed/canceled calls;
they are not remaining quota or a prediction of callability.

The repaired composed source passes complete Task testing (4,005 frontend cases /
164 files), checking and build in an isolated copy. All 1,600 relevant product,
test, dependency and Task paths match current main exactly. Main final checking
and embedded build pass. Controlled three-native EN/ZH browser/restart acceptance
passes Personal1/sharedTeam2, peer scope isolation and one catalogue read followed
by one scoped report per manual refresh. Owned resources are independently absent.
The containing commit delivers this bounded slice. No backend, schema, endpoint,
permission or dependency change is introduced.


## Member catalogue recorded caller coverage

The existing monthly cards and table reuse the selected Personal or shared Team
report for recorded caller counts. The report marks its provenance with
`member_count_basis: "distinct_recorded_actors"`; each Model group carries
`members: {value, known, unknown_calls}`. A complete marked group without
unattributed calls displays the exact distinct recorded actor total. Otherwise
the primary value stays Unknown, with the recorded known subtotal and number of
unattributed calls shown separately. Legacy unmarked reports remain Unknown.
Only a complete, current marked report supports zero for an absent Model group.

Success, error and canceled requests participate. Counts retain historical exact
actor attribution, including removed/rejoined members; they do not identify
contributors, count current membership/grants, or prove native completion. All
sources and requestable candidates remain Unknown. Current actor, selected
account, Session and catalogue generations hide stale values during renewed
reads/errors. UTC month-to-query boundaries, returned observation time and
may-lag guidance remain explicit. No extra endpoint, per-row query, directory
read, schema, price lookup or source summation is introduced. The same report's
current and optional previous Model groups carry this coverage; the catalogue
uses the existing current-month request with comparison disabled. See
[Usage recorded caller coverage](USAGE.md#recorded-model-caller-coverage).

## Whole-item member catalogue access

The existing catalogue cards and native table rows open the exact Model access
review from passive content or focused Enter/Space. Existing title/API buttons
and source menus remain separately operable. Interactive descendants, portaled
menu events, non-primary clicks and mouse text selection do not activate the
containing item. Visible focus and the local Base UI drawer preserve keyboard
navigation and focus restoration.

Dispatch checks the current actor/Session generation and successful idle catalogue
or candidate query containing the exact target; requestable entries must still
lack a Personal grant. Opening a review adds no authority, directory endpoint,
request mutation or inference. Existing Team/requestable footer authorization
reads remain independent; this interface does not promise one total detail read
for every scope. Twenty-one new regressions retain the previous tests, prices,
filters, source selection and Monthly refresh repair. Actual composed gates and
controlled browser evidence are recorded in Implementation when accepted.

Dismissal resolves a current authorized card, row or native action by exact actor,
Model and representation after Session renewal replaces DOM nodes. The optional
local Drawer focus target retains default behavior for other callers. Removed or
unavailable targets receive no forced focus; unchanged-node and renewed-node
regressions remain covered. The repaired composition passes 4,055 frontend cases/165 files, mandatory
checking/build and controlled bilingual pointer/keyboard, permission, actor and
original-Session restart acceptance. Text-selection and held/error authority races
remain focused source evidence. This bounded slice is delivered by the containing
commit; F19 remains partial.

## Complete Provider Model binding projection

`GET /api/v1/admin/providers/:provider_id/model-bindings` requires both current
`providers.read` and `models.read_all`; Provider read alone cannot reveal logical
Model names or binding counts. The read-only endpoint accepts no query parameters
and returns `Cache-Control: no-store`, including rejected reads. Its complete
envelope is `{provider_id,items}`. Every item contains
`{provider_model_id,connection_id,binding_count,models}`, and every Model contains
only `{id,name}`. A missing historical current name remains null.

The projection covers every current Provider Model, including empty bindings and
stored disabled or zero-weight relationships. It uses exact identities, current
names and one bounded read-only repeatable-read transaction. It exposes no grants,
recipients, credentials, aliases, historical names or readiness claims. More than
10,000 relevant records fails without returning a partial projection.

The existing Provider Models table adds a Models column and conjunctive Bound/
Unbound filter. It accepts the projection only when the complete Provider Model/
Connection set matches the fresh catalogue. Provider-only readers see Unknown
and cannot use binding filters. Renewal, errors and mismatches hide saved facts;
explicit mismatch recovery refreshes the catalogue before requesting a new
projection. Rendering the table never reconstructs bindings from a Model directory.
