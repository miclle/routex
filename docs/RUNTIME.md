# Gateway Runtime Publication

## Single-Process Runtime

The application loads a runtime before it starts serving requests. Routing and authorization use immutable in-memory snapshots while call facts remain a separate persistence concern. `StartRuntime(ctx)` performs an initial publication and starts a one-second background refresh. `StopRuntime()` cancels and joins that publisher during shutdown.

This is a single-process design. It does not provide distributed publication, node acknowledgments, multi-node revocation guarantees, or a highly available control plane. Each process requires its own initial database load after restart; runtime secrets are not persisted to an on-disk snapshot.

## Independent Authorization and Routing

Each refresh reads the required database rows inside a repeatable-read transaction. The authorization snapshot contains active key verification hashes, enabled users, the intersection of key model scopes and current user grants, active models, names and alias deadlines, and current credential eligibility and model coverage. Password hashes are not loaded into the runtime. Key expiry and alias deadlines are checked against the current clock, including during outages.

Authorization has a five-second lease measured from refresh start. Database failure preserves the existing snapshot only for its remaining lease; after that, new requests receive a service-unavailable response. Expired authorization never becomes an unlimited fallback. A successful refresh updates authorization even when the routing configuration is invalid, so a broken route publication cannot restore a revoked key, grant, user, or credential.

Routing preparation validates connection URLs, model relationships, credential coverage, weight totals, and encrypted credentials. Every enabled, verified credential is decrypted during preparation. A successful publication atomically replaces the routing snapshot; an invalid publication retains the last valid one. An unchanged configuration digest avoids repeated decryption. A model with only zero-weight candidates has no published route and does not prevent publishing other models. Nonzero totals must equal 100 per supported protocol.

Gateway authentication, name resolution, route selection, and credential selection do not read the database or secret store when the runtime is active. Existing routes continue to enforce the latest authorization and credential eligibility snapshot. In-memory credentials remain sensitive process memory; the runtime does not claim protection against arbitrary code execution or memory disclosure.

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

Administrators can read `GET /api/v1/admin/runtime` and request publication with `POST /api/v1/admin/runtime/publish`. The publication action uses the existing session, administrator, same-origin, and CSRF controls. Status exposes only readiness, snapshot ID, publication time, authorization lease deadline, refresh time, and generic error classifications.

Migration 7 introduces `runtime_publications` through a frozen GORM schema. Publication metadata is written when the routing snapshot or failure state changes, rather than on every successful authorization lease renewal. Metadata writes are best-effort and cannot replace a valid snapshot with an invalid one. These operational records do not replace transactional audit events or constitute the complete system-audit feature.

Call-fact persistence is separate from runtime authorization and routing. The process starts the bounded durable journal before listening; see [Call Facts](CALLS.md#persistence-failure-boundary) for its capacity, replay, disk-failure, and shutdown boundaries. The authorization lease still bounds which new requests may run during a primary-database outage.

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
