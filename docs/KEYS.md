# Personal API Keys

Personal Keys authenticate native gateway requests independently of browser sessions. A Key's model scope is an immutable ceiling over stable model IDs. Effective access is its intersection with the owner's current model grants and active models. Administrators receive no implicit calling access.

## Delivery and lifecycle

Creation returns an `rx_` bearer value once. The database stores only its SHA-256 digest and a short display prefix. List, edit, audit, and error responses never return the bearer or digest. The UI keeps delivery values outside React Query caches and persistent browser storage.

A new Key is `pending` and cannot authenticate. The owner must confirm delivery within ten minutes. Closing or canceling the delivery dialog revokes the pending Key. An expired pending delivery remains unusable and cannot be confirmed. Active Keys can be renamed, disabled, re-enabled, or permanently revoked. An expired Key cannot be re-enabled. Revocation is terminal.

Rotation creates a pending replacement with the original model scope, expiration, and activation state. Confirmation locks both records, rechecks scope and source state, and activates the replacement without revoking the original. Multiple confirmed replacements can coexist during rollout; none silently retires the source. Canceling a replacement does not revoke the original. Rotating a disabled Key preserves its disabled state.

Planned retirement is a separate operation. The replacement must be active and unexpired, retain the same scope and expiry, and have a persisted successful gateway call made after its creation by the exact personal owner and replacement Key, whose last attempt is successful and carries exact native `completed` evidence. HTTP acceptance, known usage, empty or unknown native results, tool handoffs, blocked or truncated responses, canceled or failed attempts, absent attempts, and an earlier completion followed by another attempt do not qualify. Neither do Project-attributed calls, another user's calls, the original Key's calls, or delivery confirmation. Once the application has switched and that evidence is available, the owner completes rotation explicitly to revoke the original. Durable call recording can delay eligibility until the completed fact reaches the database. A bounded GORM projection rechecks immutable owner, Key, request, status, native marker, ordinal and creation boundary exactly in Go; database collation cannot promote mismatched evidence. A corrupt latest candidate conservatively requires a fresh qualifying call.

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

`handler/apikey_integration_test.go` and `handler/apikey_rotation_integration_test.go` run in the shared disposable-database harness on PostgreSQL and MySQL. They cover pending rejection, digest-only storage, disclosure boundaries, CSRF, explicit model grants, immediate grant revocation, cancellation, concurrent confirmation and retirement, disabled rotation, expired delivery, ownership, terminal revocation, restart behavior, and audit persistence. Rotation acceptance uses a real native-completed HTTP upstream call. Actual HTTP 200 empty, refused, tool-handoff and output-limit fixtures remain ineligible. Adversarial immutable facts cover wrong-user, wrong-Key, Project-attributed, pre-creation, missing, canceled, failed, nonterminal and raw case-folded evidence. Independent database pools race completion and prove durable, single-audit retirement and historical retry semantics. Frontend tests cover confirmation, cancel/revoke failures, bilingual completion guidance and secret cache isolation.

[Project Keys](PROJECT_KEYS.md) have independent ownership and authorization. Token and money limits, RPM/TPM/concurrency and IP restrictions are implemented through the separate [resource-limits API](RESOURCE_LIMITS.md). External delivery and delivery Profiles remain later work packages. Production gateway authorization uses the immutable runtime snapshot and its bounded lease, with synchronous refresh and denial markers after reductions. Database-backed authentication remains available for service use when runtime snapshots are not started. Key lists retain revoked history; pagination, richer history presentation, and delivery policy configuration remain separate follow-up slices.

## Administrative Member Keys

The addressable Member Keys tab reads retained Personal Key metadata through
`GET /api/v1/admin/members/:user_id/keys` and its `/:key_id` detail route under
independent `members.read`. Pages default to 40 rows, accept limits 1–100 and use
an exact retained subject-owned Key cursor. Complete ancestry resolution is
bounded to 8192 records; overflow fails explicitly. Project Keys never enter this
projection, and the real reader is never substituted with the subject owner.

Rows expose safe identity/name, all retained lifecycle states, recorded model
ceiling IDs, expiration and recorded timestamps, a reviewed ETag, server-owned
Disable eligibility and last-use coverage. Last use comes only from exact Personal
subject/Key call facts; it is not native completion evidence. No bearer, digest,
display prefix, delivery material or raw revision is returned. Model labels use
already-authorized metadata or stable IDs without fetching a directory.

The limit projection distinguishes stored and effective policy, validated shared
rotation quota roots, authoritative windows, coverage, known subtotals, unknown
counts and holds. Usage counters and currency maps remain exact strings; zero,
null and unavailable data remain distinct. Denomination comes from the scoped
`platform_currency`. No remaining allowance, lifetime sum or currency conversion
is inferred. See [Resource limits](RESOURCE_LIMITS.md).

`POST /api/v1/admin/members/:user_id/keys/:key_id/disable` requires independent
`members.keys.disable`, current Session, same origin, CSRF, JSON `{reason}` and the
reviewed strong quoted ETag. `members.write` and owner permissions confer no such
authority. Delegated actors cannot disable their own Key or an administrator
subject's Key; even administrators require the explicit permission. Only active,
unexpired Keys of current enabled, non-offboarded subjects can begin this action.
This tab adds no create, rotate, revoke, reveal or re-enable operation.

Persistent `LifecycleRevision` participates in the ETag alongside exact owner,
Key, metadata, expiry and ceiling. Product Personal Key state writers advance the
revision, so re-enable followed by another disable cannot reuse an older active
review merely because status or timestamp precision matches. A shared publication
gate serializes these writers through commit and reduction tombstones. Disable
and its secret-free real-actor audit commit atomically; runtime publication then
must prove the exact current owner/revision/disabled status under a valid lease.
The private retained-state proof is separate from the active authentication map.

A confirmed response has exact `user_id`, `id`, `status:"disabled"`, current `etag`,
`runtime_applied:true` and `confirmation:"current_disabled_state"`. It confirms
current state, not the original operation or a historical receipt. An authorized
retry of an already disabled retained target adds no audit; a newer re-enable
makes the old intent conflict. Publication/proof failure may return 503 after
commit. Preserve original reason and If-Match across all uncertain or rejected
retries; a metadata GET, 404 or missing active runtime entry never proves success.
The UI reviews the exact row in a local danger dialog. A stable actor/subject
mount retains only transient drafts and uncertain intent during incidental reads
or errors, while obsolete private rows/actions/dialog remain hidden. Actor/target
changes and logout destroy that state. It uses current authority on dispatch
and binds replies to actor, subject, Key and Session generations.

This bounded package has complete local acceptance and awaits commit/push. R2 source checks/build and focused dual-driver migration/lifecycle checks passed.
The full dual-driver matrix and final mandatory check also passed. Authentication
lifecycle passed on both drivers. The bounded focus repair now passed rebuilt source checks. Escape/Cancel returns
focus only to the connected exact currently reviewed row; authority loss or a
hidden/disconnected trigger skips restoration. Success refresh still invalidates
the list immediately and does not guarantee row focus. Rebuilt-artifact native/browser/restart acceptance also passed with exact current
state confirmation and retained immutable history. New remote CI remains unknown. See
[Implementation evidence](IMPLEMENTATION.md).

## Personal Key monthly threshold behavior

Owner-scoped Personal Key limits expose separate monthly Token and money stop
or alert-only modes in the existing detail editor. Rotation shares the original
quota root, exact owner, usage and policy. User and Key thresholds are evaluated
independently; a soft ancestor removes only its own monthly stopping decision.
Project Keys use their separately proved Project/root identity. All accounting, reservation, currency,
rolling-window and rate boundaries continue to apply. See
[Resource limits](RESOURCE_LIMITS.md#personal-key-monthly-behavior-v73)
for the delivered contract and current acceptance boundary.

Project Key stored modes operate independently of the exact Project parent's
monthly decisions. Its parent summary exposes canonical Project modes separately;
its own controls require immutable Project/root proof and never borrow a
manager's Personal policy. See [Project aggregate behavior](RESOURCE_LIMITS.md#project-aggregate-monthly-behavior-v74).

## Controlled Personal Key API checkpoint

The R6 API/native/restart fixture passes 19 logical calls, seven native attempts (six priced and one unknown), 12 no-attempt denials, four warning inboxes with one read and five original Sessions. Bounded database snapshots and exact owned cleanup pass independently. It binds the old R3 artifact; the later parent-callback test repair does not relabel that receipt. The containing phase is delivered as `c2368dd`; browser and AuthGate remain pending. See [Acceptance boundary](RESOURCE_LIMITS.md#candidate-verification-boundary).

## Project Key monthly threshold behavior

The Project Key limits dialog preserves current Project management authority
and the oldest retained immutable rotation root. Token and money modes operate
independently for that root and its Project parent, with complete-policy
omission-to-stop, explicit conflict review and original uncertain intent.
A soft account never bypasses another hard account or accounting, reservation,
price/currency, rolling/rate/IP guards. See
[Project Key policy](RESOURCE_LIMITS.md#project-key-monthly-behavior).
