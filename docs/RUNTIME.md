# Gateway Runtime Publication

## Single-Process Runtime

The application loads a runtime before it starts serving requests. Routing and authorization use immutable in-memory snapshots while call facts remain a separate persistence concern. `StartRuntime(ctx)` performs an initial publication and starts a one-second background refresh. `StopRuntime()` cancels and joins that publisher during shutdown.

This is a single-process design. It does not provide distributed publication, node acknowledgments, multi-node revocation guarantees, or a highly available control plane. Each process requires its own initial database load after restart; runtime secrets are not persisted to an on-disk snapshot.

## Independent Authorization and Routing

Each refresh reads the required database rows inside a repeatable-read transaction. The authorization snapshot contains active key verification hashes, enabled users, the intersection of key model scopes and current user grants, active models, names and alias deadlines, and current credential eligibility and model coverage. Password hashes are not loaded into the runtime. Key expiry and alias deadlines are checked against the current clock, including during outages.

Authorization has a five-second lease measured from refresh start. Database failure preserves the existing snapshot only for its remaining lease; after that, new requests receive a service-unavailable response. Expired authorization never becomes an unlimited fallback. A successful refresh updates authorization even when the routing configuration is invalid, so a broken route publication cannot restore a revoked key, grant, user, or credential.

Routing preparation validates connection URLs, model relationships, credential coverage, weight totals, and encrypted credentials. Every enabled, verified credential is decrypted during preparation. A successful publication atomically replaces the routing snapshot; an invalid publication retains the last valid one. An unchanged configuration digest avoids repeated decryption. A model with only zero-weight candidates has no published route and does not prevent publishing other models. Nonzero totals must equal 100 per supported protocol.

Gateway authentication, name resolution, route selection, and credential selection do not read the database or secret store when the runtime is active. Existing routes continue to enforce the latest authorization and credential eligibility snapshot. In-memory credentials remain sensitive process memory; the runtime does not claim protection against arbitrary code execution or memory disclosure.

## Bounded active routing

Every native request captures a detached route plan from one immutable routing snapshot. Before each dispatch, RouteX rechecks the current Key, personal or Project owner, model grant, provider-model and credential authorization, route snapshot, egress generation/revision, limit policy, quota capacity, price evidence, and process-local health. A revocation or changed admission boundary stops the run; it never selects another route to bypass current policy.

Quota admission alone does not authorize dispatch. The original caller and selected route are checked again inside the local publication guard, before creating attempt evidence and after the durable checkpoint, immediately before releasing that guard for upstream HTTP. A reduction present at either check prevents upstream work without repeating quota admission. These checks use the current process-local authorization snapshot and tombstones; they do not provide a cross-process revocation guarantee. Not every revocation writer acquires the publication guard, so this recheck does not establish atomic commit-to-dispatch ordering for every mutation: a reduction racing after the final check can still follow a permitted dispatch. Already dispatched attempts retain their captured identity and settlement obligations.

The dispatch-revocation regression covers all four native protocols, Personal and
Project callers, reductions before and after the durable attempt checkpoint,
original finite Token reservations, zero-work restart recovery and preservation
of already received upstream attempts. The focused Gateway race run passes 519
named nodes, including 144 new nodes; the containing Service/Handler race run
passes 7,534 nodes with nine explicit dedicated-database skips. These controlled
checks do not establish cross-process mutation-to-dispatch ordering.

Positive-weight eligible routes retain their configured proportions. Credential priority applies inside a selected route. Proven pre-request Connection failures and explicit native credential rejection create bounded process-local cooldowns; a successful response admission clears the relevant health evidence. Native rate rejection excludes that Connection only within the current request. Health is intentionally not persisted or coordinated between nodes.

A valid `2xx` response transfers ownership to the protocol handler and ends route selection. Ordinary parsing, streaming finality, client writes, usage parsing, and cancellation after that boundary cannot trigger another attempt. See [Native attempt planning](ROUTE_ATTEMPTS.md) for the exact evidence matrix, attempt limit, and durable checkpoint contract.

## Revocation Before Acknowledgment

Supported control-plane mutation paths publish synchronously after their database transaction and before returning a successful response. Reduction and revocation paths first install an in-memory tombstone for the affected key, user, model, or credential. If refreshing fails, the mutation returns HTTP 503 and the reduction remains enforced by its tombstone or the newly published authorization snapshot. The database transaction may already have committed, so callers should refresh resource state before retrying a create operation.

Tombstones carry generation numbers. A background refresh that started before a newer revocation cannot erase that revocation when it finishes. Only a fresh authorization publication can clear older tombstones, and that publication continues to exclude the revoked resource according to the committed database state. This avoids a polling-only delay after a successful in-process mutation.

Personal key creation, confirmation, updates, revocation, and rotation lock the active owner before any key row. Account disable uses the same owner-first order before revoking keys, so an issuance waiting behind disable must return unauthorized. Grant replacement locks requested owners in stable ID order before the model, preserving that order during foreign-key checks. Creating and confirming a planned replacement leave the original usable. Confirmation publishes the replacement without treating delivery as successful application rollout. Explicit rotation completion requires a successful persisted call by the exact owner and replacement Key, then revokes the original and publishes that reduction before acknowledgment. Emergency revocation remains independent of this verification gate.

The one-second background poll handles changes made outside the process. It is not a substitute for synchronous mutation publication. During a database outage, external changes cannot be observed; the five-second authorization lease is the fail-closed bound. Requests already authorized and in progress are not retroactively replayed or terminated by a later key revocation.

## Project-Owned Keys

The same repeatable-read publication loads project keys, their immutable model scopes, project grants, project lifecycle state, and current managers. A Project Key requires an active project and at least one enabled current manager. Its effective model access is the intersection of the key scope, current project grants, and active models. The creator is audit metadata and does not become the inference owner; replacing a manager does not transfer key ownership or silently broaden its scope.

Project bearers use the `rxp_` prefix and retain their stable project ID in the gateway authorization result. Personal bearers keep the `rx_` prefix and personal owner rules. Runtime lookup checks that the bearer kind matches the cached ownership metadata. Both kinds enforce current expiry, the five-second authorization lease, key revocation, and model reductions without database reads.

Project lifecycle and grant mutations use generation-tagged project tombstones before synchronous publication. A stale refresh cannot clear a newer project invalidation. User tombstones are not treated as project ownership: enabled-manager eligibility comes from the published project authorization state, while direct project tombstones deny affected project keys immediately.

## Administration and Evidence

Administrators can read `GET /api/v1/admin/runtime` for current readiness, snapshot ID, publication time, authorization lease deadline, refresh time, and generic error classifications. The [manual publication command](#manual-publication-command) changes the POST wire contract and adds actor-owned receipt lookup. Its bounded acceptance is recorded below; a command receipt remains separate from current runtime readiness.

Migration 7 introduces `runtime_publications` through a frozen GORM schema. Publication metadata is written when the routing snapshot or failure state changes, rather than on every successful authorization lease renewal. Metadata writes are best-effort and cannot replace a valid snapshot with an invalid one. These operational records do not replace transactional audit events or constitute the complete system-audit feature.

Call-fact persistence is separate from runtime authorization and routing. The process starts the bounded durable journal before listening; see [Call Facts](CALLS.md#persistence-failure-boundary) for its capacity, replay, disk-failure, and shutdown boundaries. The authorization lease still bounds which new requests may run during a primary-database outage.

## Manual publication command

The manual publication command replaces the former publication action without a
command body and its general `RuntimeStatus` response. Clients must replace `{}`
or an empty body with the following exact JSON object and consume a command
receipt. The former request is rejected with 400.

```json
{"request_id":"11111111-1111-4111-8111-111111111111","reason":"Publish current authoritative configuration"}
```

| Endpoint | Contract |
| --- | --- |
| `POST /api/v1/admin/runtime/publish` | Submit or read back the identical original command. Requires a current authenticated intrinsic administrator, same-origin checks, CSRF and JSON. |
| `GET /api/v1/admin/runtime/publication-commands/:requestID` | Read only the current intrinsic administrator's own ID-and-birth-bound receipt. Requires a current authenticated Session; it does not dispatch publication. |

The POST body is bounded to 16 KiB. Its only fields are `request_id`, a canonical
lowercase UUIDv4, and `reason`, unchanged trimmed control-free UTF-8 text of 1–1000
runes. Duplicate, case-aliased, unknown, missing, null and malformed fields,
invalid UTF-8 or unpaired Unicode escapes are rejected. Both endpoints reject
query parameters, including a bare `?`. Responses are private, no-store and
nosniff, including denials. There is no review ETag: publication reads current
authoritative state rather than applying a reviewed configuration version.

The receipt has exactly `request_id`, `actor_id`, `reason`,
`claim_instance_id`, `claim_instance_started_at`, `claimed_at`,
`finalized_at`, `status` and `failure_code`. `finalized_at` and
`failure_code` are explicit null when absent.

| Status | HTTP | Meaning |
| --- | --- | --- |
| `pending` | 202 | The original claim has positively owned local in-flight work under its original context. |
| `unknown` | 202 | A claim exists without that local evidence; no result or absence of effects is proved. |
| `refresh_returned_nil` | 200 | The original refresh returned nil and its terminal receipt committed. |
| `refresh_returned_error` | 200 | The original refresh returned an error and its terminal receipt committed; `failure_code` is exactly `runtime_refresh_failed`. |

Only the error status has a non-null failure code. An identical request ID,
exact actor identity and birth, and unchanged reason never dispatch another
refresh. Changed reason or foreign actor/birth returns 409; a missing own receipt
returns 404. Current Session and intrinsic administrator authority are checked
again even for historical receipts. Unreadable or uncertain database results
return 503; a later authorized read may discover an originally committed result.

The claim and admission audit commit before at most one refresh. Claim failure or
uncertainty prevents dispatch. One original three-second context covers the
claim, authority/process recheck, refresh and terminal receipt transaction; no
renewed budget or detached finalizer is added. Terminal receipt and terminal
audit commit together and cannot be rewritten. Restart never resumes claimed
work; without the original positive local ticket and context, it remains unknown.

A receipt records the original function outcome. It attributes no snapshot,
installation observation or fleet acknowledgement and provides no rollback or
no-effects guarantee. A failed refresh may already publish fresh revocation.
Current runtime and application observations remain separate APIs. Existing
runtime qualification below retains its original historical scope. This command's
strict HTTP/body, idempotency, authority/birth, claim/finalization fault and portable
migration checks pass in `54b4b3fb5c51725c4387837d1f24f111beeabc763dab1294584b9da16db4ab4f`. The existing
same-binary two-process HTTP restart witness preserves terminal receipts and
unchanged original command/audit rows; that historical witness does not exercise an interrupted
in-flight OS-process claim. The direct service load counter is separate evidence.
Default production build acceptance is `502293c38322976fca690ca4d885bc1d92f731392e2cec3d8df2bdd0fb91b1ba`;
the integration process witness does not itself prove a default production build.

### Controlled interruption witness

`runtime_publication_interruption_integration_test.go` extends the existing
`runtime_publication_command` real-database scenario without adding a registry
entry or changing production code. It launches two generations of the exact
SHA-256-pinned Go test executable against the dedicated `routex_test` database
and a private loopback listener. The first generation holds one original manual
refresh inside its unchanged three-second context. Independent HTTP and GORM
reads must prove the owned pending receipt, exact durable claim and claim-only
audit before the context expires; the controller then sends SIGKILL to its
positively owned process group and joins that exact child.

The second generation retains the original Session and must return unknown for
both an authorized receipt read and identical POST, without changing the
original actor/birth/reason/claim fields, finalizing an outcome or dispatching a
new refresh. A changed reason remains a conflict and an invalid current Session
remains denied. The original command and claim-only audit must remain field-for-
field equal; the new process's source-load count must remain at its startup value.
Private stage files contain only identity and stage facts. Exclusive guards
precede process dispatch and remain blocking when ownership or cleanup is
uncertain; exact Wait, process-group disappearance and listener reuse are
required before successful cleanup.

The corrected clean candidate passed mandatory checking and a fresh selective
real PostgreSQL/MySQL run: eleven named migration/lifecycle results per driver,
plus 10,227 nonmatrix Go race passes and seven expected environment/child skips.
The selective runner retained the official nonmatrix checks, migration prelude,
constraints, timers and cleanup; only its two driver selectors changed. The
original callback-registration lint failure remains retained, followed by the
narrow returned-error assignment correction.

Independent observation captured all four controlled child births, exact groups,
recorded Instance identities and the same test-executable digest. Final readback
confirmed child/group/fixture and Compose closure, unchanged tested source and
preserved development listeners. All captured ports refused connections and
passed Go-compatible reuse-address bind/listen checks; the preceding raw MySQL
listener bind failures remain retained without a proven cause. Primary composed mandatory checking also passed before commit. This focused
acceptance remains distinct from production-binary and fleet acceptance.

The witness uses the existing POSIX restart guard on Linux/macOS; it does not
prove interrupted production-binary behavior, Windows process behavior, fleet
acknowledgement, rollback or absence of publication effects. Historical
terminal-receipt acceptance remains separate.

## Verification and Remaining Acceptance

Unit tests cover in-memory inference without a database handle or secret store, lease expiry, immediate key/user/model/credential tombstones, stale-publication generations, alias expiry, credential coverage reductions, bad weights, and invalid credential envelopes. The isolated PostgreSQL and MySQL lifecycle tests cover real publication, metadata persistence, invalid-route retention, grant withdrawal despite a bad route, and inference through a previously prepared route after the runtime's database connection is closed. With background polling stopped, they also exercise public key, grant, credential, weight, and rename mutations and assert their effects immediately. An owner-row lock test covers key issuance concurrent with account disable, disabled-owner rejection for every key mutation, and the absence of resurrected keys after re-enabling the owner.

```bash
go test -race -tags development ./internal/routex/service ./internal/routex/handler
go tool task test-integration
```

Correctness tests establish the control-flow and revocation ordering contracts. They do not establish a production p99 latency, capacity target, multi-node guarantee, or real-provider acceptance. Those require measured workloads and the relevant external environment.

Chat Completions, Responses and Messages use independent `(model_id, protocol)` route groups.
The Key-scoped model list exposes currently eligible protocols without provider
identities; native requests never fall back across protocol groups.

Gemini uses the independent `gemini_generate_content` route group and never falls back to another native protocol.


## Corrected R8 candidate contract additions

These additions describe the isolated candidate; complete dual-driver and
composed-main acceptance remain pending. Earlier acceptance records stay
bound to their original source.

## Routing application evidence (candidate V87)

`GET /api/v1/admin/runtime/applications` requires fresh `system.read` and returns
bounded process-scoped **routing-only** application history. It accepts optional
`instance_id`, opaque actor/filter-bound `cursor`, and canonical `limit` (default
20, maximum 100). Unknown or duplicate parameters, explicit empty values and
unsafe IDs are rejected. Responses are private and not cacheable; no route digest,
process lease token, Key material, credentials or serialized configuration is public.

Each append-only record binds the exact registered `ins_` process generation and
persisted birth to a `cfg_` routing snapshot, private source digest, original route
publication time and first recorded application observation. Repeated successful
polls do not rewrite that first observation. Legacy publication rows have no
process identity and are never backfilled as application records. This records
neither a full authorization version nor fleet acknowledgement or a job-specific
operation receipt. There is no public write endpoint accepting node claims.

The recorder runs only after successful route preparation/publication, under the
existing publication and runtime locks. Its database transaction rechecks the
exact process token, birth, active lease and current routing/authorization
pointers and revocation generation. Registration-lock contention skips recording
without waiting. It uses a maximum 250-millisecond budget,
capped by the remaining original authorization lease and parent context. This
may add at most that recorder budget to publication-lock occupancy; it never
renews the lease, changes admission or performs request hot-path I/O. Failed,
expired or obsolete captures cannot commit a new successful observation. Evidence
persistence failure leaves history unavailable and a later successful refresh may
retry; an already applied in-memory route remains governed by existing admission.

History and current observations remain separate. `instance_status` is `online`,
`offline` or `unknown` from the exact retained process birth and server lease.
`current_serving_snapshot_matches` is nullable: only the serving process with a
matching active registration, unexpired authorization, stable pointers/generation
and no current refresh error can return true or false. Other processes, missing
or changed identities, failed/unstable publication and expired authorization
return null. True identifies the serving process's current routing snapshot; it
proves neither current complete authorization nor successful inference. False
means that exact serving process currently has a different routing snapshot.
Restart creates a new process identity and never inherits old current evidence.

V87 and its migration/lifecycle fixtures remain candidates. Separate focused
PostgreSQL/MySQL proof does not establish complete Full168, controlled production
process, UI or composed-main acceptance; these remain pending. Source tests alone
do not complete F30. Complete configuration
versions, general rollback and distributed emergency revocation remain separate.

The current frozen R8 source passes check, complete Task testing and build,
5,032 ordinary named Go tests, and the separate two-driver Project warning
lifecycle. Earlier focused R7 receipts remain bound to that source. Complete
R8 Full168, composed-main gates, browser and delivery remain pending. These
source-specific facts do not establish wider runtime or feature acceptance.


## Credential-source admission closure

Published Vault credential admission records exact physical source and serving
process generation before a holder can use it. Cleanup permanently denies the
physical object before a bounded drain, rechecks dependencies and current
registered cleanup-process authority, and claims the one permitted remote attempt
only after positive joins. Existing immutable attempt attribution is never
rewritten by deletion or cleanup. Restart cannot borrow an old generation's live
capability; absent/expired process observations are not closure proof.

HTTP shutdown closes credential-source admission before draining requests.
Service stop waits for its exact holder census and SDK clients, then records
positive durable generation closure only with fresh instance/lease authority.
Canceled drains, incomplete joins, uncertain persistence and poisoned client Close
outcomes remain failure or unknown and cannot authorize cleanup. Previously
committed exact closure may be reconciled without changing its timestamps or
replaying a remote effect. These mechanisms do not establish complete-fleet
closure, native inference completion or forced-crash/offline recovery.

Fresh main acceptance includes both complete database matrices and a controlled
local run with one native completed call and two same-binary restarts. Retained
call/attempt identities and original Sessions remain exact; historical closure
and a fresh joined generation are independently read back. These observations
do not establish forced-crash recovery or fleet-wide closure. See the
[current acceptance boundary](IMPLEMENTATION.md#current-vault-main-integration-2026-10-09).

## Whole-Provider dispatch gate

Published authorization retains exact Provider identity, birth, enablement and
shared review revision. Route plans and retirement readiness captures retain
that same proof; fresh mutable catalogue facts cannot substitute for a captured
Provider incarnation. New discovery, route candidates, weight rollback eligibility
and final dispatch require the matching enabled Provider, independently of child
Connection, Credential and Provider Model gates. Unknown proof remains denied.

A committed disable publishes a synchronous local denial before refresh; a failed
refresh cannot reopen dispatch. Current application checks bind the exact current
digest, publication epoch, egress generation and Provider proof, including a
Provider with no Connections. Held admitted native calls retain their captured
Credential/snapshot attribution and may finish without changing history. Failed
and interrupted attempts also retain their exact captured identities; a status
write never manufactures native completion evidence. Enabling
preserves every child state and configured weight. Local publication confirmation
is separate from historical operation identity and fleet coverage.

Complete runtime refreshes serialize the read-through-publication pipeline.
Publication admission precedes borrowing a database connection; the effects lock
is acquired after loading, so current proofs remain observable during that read.
Cancellation and stopped-publisher checks after lock acquisition prevent late
publication or revocation-barrier clearing by obsolete loads. These ordering
rules do not change the authorization lease or establish native availability.


## Connection transport generation and diagnostic checks

Transport editing extends the existing Connection metadata operation. A complete
Base URL/protocol/adapter/API-version tuple requires an explicitly disabled
Connection, fresh write authority, the exact strong metadata If-Match and a
required reason. Any retained ProviderModel locks protocol and adapter. Reads
require independent read authority; write permission does not grant a read. Egress
configuration remains a separate operation. URL/adapter validation and network
policy still apply. Saving transport neither invokes Verify nor enables records
or makes an inference request.

A canonical tuple change advances the durable transport generation. Published
proofs and final dispatch bind exact resource identities/births, revisions and
that generation. Old Credential verification/discovery and Azure coverage cannot
authorize current supply; stale ProviderModel capability declarations cannot
admit routes or public discovery. Stale capacity evidence cannot satisfy finite
reservation requirements. Recorded declarations, timestamps, enablement, grants,
prices, weights and immutable history remain unchanged. Explicit capability
review submits both declarations, even when both are false; availability-only
Credential/ProviderModel toggles never renew evidence. Real Verify, any required
Azure coverage/capability/capacity attestation and explicit Enable remain separate
operations. Neither stored positive counts nor a successful diagnostic substitutes
for these proofs.

The writer installs a local Connection denial after its transaction boundary
before runtime refresh. Confirmation reauthorizes the exact committed target and
checks the current local source digest and egress generation. Failed postcommit
refresh or confirmation returns unavailable and retains an uncertain outcome.
External admission requires the exact original review; an obsolete token does
not become valid because current values match. A fresh read is not a historical
receipt. An abandoned local retry requires a separately reviewed and confirmed
new operation. Already dispatched calls retain their captured client, Credential
and snapshot attribution; new admission cannot borrow an old transport proof.
Current local application does not establish native health or fleet application.

`POST /api/v1/admin/connections/:connection_id/test` accepts only an exact
`credential_id` and the reviewed strong metadata If-Match, behind the existing
Session, CSRF and independent Provider read/write checks. Its ten-second parent
budget covers source resolution and bounded native model-list GET parsing.
Explicit pending or disabled records may be tested; no alternate Credential is
borrowed. Azure classic reports authentication-only scope with an unknown model
count. Other successful checks report only the bounded count from that check,
without persisting discovery or verification. Failed and unavailable remain
distinct, and no check establishes future callability.

The diagnostic captures the exact source holder and effective egress client,
including the default/proxy revision, identity and birth. Fresh authorization,
review, source and transport facts are rechecked before dispatch and before
return; changed facts or an expired request context cannot produce a successful
result.
Existing source-use admission and bounded shutdown/drain interlocks remain in
force, including required Vault safety records. The private transient DTO contains
no secret, raw upstream body or serialized configuration. It changes no catalogue,
configuration, verification, discovery, enablement, routing, audit or persisted
diagnostic result.

### Reviewing recorded Gateway installations

The System Status task Details dialog offers Gateway installation records for
its executor. An optional exact canonical `cfg_` snapshot ID narrows those records;
“Review all recorded processes” removes only the executor filter. Review the
changed filters explicitly to load a fresh page. Each row retains its process ID,
process birth and first observation. Filter changes cancel pending reads and hide
obsolete results, alongside the existing Session and independent `system.read`
authority fences.

`GET /api/v1/admin/runtime/installations` accepts optional `instance_id`,
`snapshot_id`, `cursor` and `limit` (default 20, maximum 100). IDs are exact and
case-sensitive on both supported databases. Cursors are bound to the current
actor, instance scope and snapshot filter; changing any of these requires starting
at the newest page. Reads retain the three-second budget and 128 KiB response
bound. No write permission or mutation is introduced.

All-process review is retained observation history, not a complete fleet census.
Equal routing snapshot IDs do not establish equal Gateway admission: different
installed authorization sources can share one routing snapshot. Nullable current
matching remains a separate comparison against this serving process; another
process or unavailable comparison stays Unknown. These records prove neither
current database state, management Session revocation, publication-command
completion nor fleet application, and introduce no rollback operation.
