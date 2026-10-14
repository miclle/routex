# API-Key delivery

This document describes the API-Key delivery application contract. The exact
phase delivery, source/mode closure and scoped acceptance receipts are recorded
in [Implementation](IMPLEMENTATION.md). Status for this delivery:
F29 Partial; source gates, controlled PostgreSQL/MySQL application coverage, RouteX process restart/authentication and the production build passed on the qualified phase floor; bounded real local Vault acceptance passed, browser interaction was not performed, and external caller interoperability remains unproven.

Controlled transport peers, real PostgreSQL/MySQL application tests, genuine
RouteX process restart/authentication, real local Vault JWT/CAS-zero
behavior, English/Chinese browser interaction and external caller interoperability
are separate acceptance scopes. The separately qualified
[writer SDK](VAULT_API_KEY_WRITER.md) does not establish application guarantees.

## Manual and managed modes

Personal and Project Keys have an immutable stored `delivery_mode`: `manual` or
`managed_vault`. Existing personal Keys backfill to manual; existing Project modes
retain their meaning. Changing a mode creates a replacement Key. The policy's
allowed modes govern new issuance, and its default initializes the form only;
neither setting changes existing Key modes or grants authority.

Manual creation retains the one-time browser secret and explicit confirmation.
Managed creation generates the bearer internally and sends it only to its exact
Vault destination. Managed responses contain safe Key metadata and a delivery
receipt, with no `secret` member, even on uncertainty. No failed managed operation
falls back to plaintext display, manual confirmation or another destination.
The public manual confirmation endpoint rejects managed Keys.

## Operator prerequisites

The existing Secrets workspace contains the delivery policy, application target
review, independent Writer and Provisioner credentials, and the caller JWT template.
Saving an integration or a credential is not verification. Setup is explicit:

1. Select the saved Vault infrastructure and retain its exact immutable revision.
   Configure distinct Writer and Provisioner identities with transient operator
   Tokens. Their encrypted authentication is independent of Provider writer/reader
   authentication; neither purpose borrows a Provider identity.
2. Save the JWT template: a pre-enabled auth mount, exact issuer and JWKS source,
   audience, bounded literal subjects and positive caller-token TTL/max-TTL.
   The initial implementation requires `TTL <= max-TTL <= 3600` seconds; this cap
   concerns caller Vault Tokens, not the lifetime of a RouteX Key. No trust is
   inferred from a Session, email, application name or default template.
3. Link the saved identities and template to the policy. A verified configuration
   can remain manual-only. Explicit dependency verification checks the captured
   infrastructure, machine identities and trust contract. Adding managed mode
   requires current server-confirmed dependencies; a Provider integration probe
   or configured flag does not satisfy this gate.
4. Review each owner-created application target. Principal-control approval and
   scoped isolation attestation are separate actions with separate review tokens
   and required reasons. Reconcile the generated role/policy only after both facts
   and all security revisions are current. There is no manual READY override.

The application obtains its JWT independently and exchanges it at the configured
Vault auth mount for a short-lived caller Token. RouteX neither mints nor receives
that JWT or caller Token. The Provisioner owns only generated policy/role names,
uses an exact JWT subject/audience with no default policy, and reads back those
bounded metadata documents. It cannot enable auth mounts or read/write KV values.
The Writer performs one generated-path CAS-zero write and exposes no read, overwrite,
delete, destroy or compensating cleanup operation.

READY means current dependency validation, exact Principal approval, current
operator isolation attestation and generated-grant reconciliation. It does not
prove caller login/read or complete effective caller privilege. Vault Identity
aliases, entities and groups can add unrelated grants; the operator must separately
check that exact subject's isolation from neighboring targets and Provider paths.
`operator_attested` records this deployment fact, not a measured global guarantee.
Revoked/stale isolation blocks new claims and pending activation. It does not
silently revoke already active Keys or delete their Vault objects.

## Owner workflow

Use the existing Personal or Project Key create/rotation dialog. Choose an allowed
mode. For managed delivery, enter the application, environment, exact JWT subject
and reason, then explicitly resolve the target. An empty Consumer ID asks the
server to allocate a stable application identity; a supplied ID selects an existing
exact-owner Consumer. Resolution creates no Key and grants no approval or isolation.
One approved Principal is supported per application/environment target.

Pending targets remain unavailable for issuance. After the required operator
reviews and reconciliation, explicitly read current facts and confirm the exact
READY target, policy review and Key request. Owners cannot supply Profile IDs,
Vault paths, machine identities or substitute security revisions. Personal ownership
comes from the current actor; Project ownership comes from the authorized path and
current manager/platform authority, never creator attribution.

Rotation derives name, models, limits, IP, expiry and enabled/disabled state from
the predecessor. Managed rotation sends empty server-owned metadata fields and the
reviewed delivery selection; the browser does not reconstruct inherited metadata.
The predecessor remains intact. A disabled replacement can have an acknowledged
write while local runtime status remains unknown and no descriptor is available;
that is not proof of active admission. Retirement remains a separate reviewed
operation requiring a persisted exact-owner replacement call whose successful
last attempt has native `completed` evidence after replacement creation. Delivery,
HTTP success, copied descriptors and known usage do not satisfy retirement.

## Receipt, uncertainty and recovery

| Fact | Meaning |
| --- | --- |
| `delivery_status=pending` | No acknowledgement recorded yet. |
| `acknowledged` | The exact one-shot write has acceptable version-one metadata, response closure and original-context proof. |
| `unknown` | The outcome is uncertain; it does not prove absence and permits no new write for that claim. |
| `not_attempted` | The recorded operation did not enter the write; it does not prove no earlier process wrote the destination. |
| `runtime_status=confirmed` | Exact local serving-generation Key publication is confirmed. |
| `runtime_status=pending|unknown` | Usability/enforcement is not established. |

A durable dispatch claim commits before remote I/O. Only the original claimed
operation may invoke its prepared Writer once; no database/publication lock is
held across that I/O. Concurrent retries and reconstruction of the Service read
existing claims rather than creating a new write. A claimed operation without a
durable acknowledgement remains unknown. Current actor/Session, owner/manager,
security, target/isolation and pending-Key facts are checked again after I/O;
a late result cannot reactivate a revoked Key or departed issuer's authority.

Keep the original UUIDv4, body and reviewed If-Match after an uncertain result.
An explicit retry uses that exact intent; changed actor/owner/body conflicts.
Reading current facts never proves historical success. A confirmed exact receipt
may be explicitly accepted by the user; a missing receipt or HTTP404 is not proof
that nothing was written. Return 201 only for newly acknowledged and published
completion, 202 for pending/unknown publication, and 200 for same-intent safe replay.
These statuses are not caller-side application proof.

Acknowledged-only publication reconciliation is local. It uses the original UUID
and current receipt review, rechecks eligible current authority and security facts,
and performs no Vault request, bearer recovery or additional write. A terminally
revoked claim cannot be revived. An eligible current Project manager may reconcile
Project-owned acknowledged publication without changing original issuer attribution.

The descriptor is available only after acknowledged delivery and confirmed local
publication. It contains public connection/authentication instructions, exact
version-one destination and a decimal-string descriptor revision, never a bearer.
Its coordinator is `not_configured` and application status is `unknown`. Copying a
descriptor proves neither browser persistence nor application configuration/login,
successful inference, native completion or fleet acknowledgement.

## Application integration configuration

The owner receipt dialog includes an application configuration card for the
supported Vault Agent JWT integration, followed by collapsed delivery details.
It derives the HCL only from the current server descriptor. Exact Key, profile,
Principal and slot identities, role, descriptor revision, JWT method and KV-v2
version-one destination must agree; unsupported or malformed tuples remain
unavailable rather than generating partial instructions. Raw non-secret
descriptor copying remains available separately.

Supply and renew the workload JWT externally at
`/var/run/secrets/routex/workload.jwt`. Agent reads the descriptor's exact JWT
auth mount and role, preserves that JWT file, and renders the API Key to
`/var/run/secrets/routex/api-keys/<slot_id>`. The output filename stays stable for
replacements in the same application slot; a changed target can create another
slot. The KV-v2 template reads the exact recorded path with `?version=1`, using
the descriptor's exact data field. Missing or empty fields fail rendering.
The output has permissions `0600`, no backup, and no Vault Token sink. The
application Key file and workload JWT file are separate.

The caller must explicitly copy, review and apply the configuration, prepare
the JWT source and output directory, run Agent, and reload or restart its
application as appropriate. Configuration rendering and clipboard completion
make no network request or delivery-status change. They establish neither
Agent authentication nor deployment, inference, native completion or retirement.
CSI and External Secrets configuration is unavailable until an independent,
supported authentication and lifecycle contract exists. No coordinator or
automatic rotation is introduced.

Configuration and descriptor copy actions share the receipt's existing
actor/Session/owner/Key/request authority fence and duplicate-operation guard.
Each new read or publication recovery synchronously invalidates the prior
descriptor fact and copy generation before dispatch. Refresh, failed reads and
authority changes hide prior configuration; retained configuration and raw
descriptor callbacks cannot copy obsolete facts even after an identical
same-authority read succeeds, or publish a new completion notice.
Descriptors and generated configuration remain outside query/mutation caches
and browser storage. English and Chinese labels switch without changing the
machine configuration. Focused renderer and receipt tests cover these contracts.
A local Vault Agent 1.21.4 process accepted a generated no-namespace HCL fixture
and initialized its JWT and template handlers without a supplied JWT, then shut
down cleanly. This verifies configuration startup only; it does not establish
real JWT authentication, template output, application use or browser acceptance.

## Replacement-call evidence

Delivery receipts include the read-only `replacement_call_status` advisory. The
descriptor is unchanged. Controlled qualification is indexed for the containing
delivery in [Implementation](IMPLEMENTATION.md); it does not establish coordinator
acknowledgement, caller deployment or current retirement eligibility.

| Status | Meaning |
| --- | --- |
| `not_applicable` | This is an initial creation command, rather than a replacement command. |
| `not_recorded` | No qualifying completed replacement call was found in the recorded evidence. This does not mean no call occurred. |
| `completed` | A recorded exact-owner replacement call began after Key creation, succeeded, and its final successful attempt carries native `completed` evidence. |

HTTP success, known usage, handoff, incomplete output, legacy unknown evidence,
and a superseded attempt do not establish completion. The server reuses the
retirement evidence predicate, without exposing call or attempt identifiers.
A database read failure remains an unavailable read; it cannot become
`not_recorded` or a successful verification.

The existing receipt dialog presents this fact separately from delivery, local
runtime publication and descriptor application. A historical completed call does
not establish deployment, coordinator acknowledgement, present callability or
current retirement eligibility. It never automatically retires the predecessor.
Fresh reads retain the existing Personal or Project authority boundary; renewal,
actor or target changes, errors and explicit refresh clear displayed evidence.

## API and authority

All paths start `/api/v1`. Administrative routes use `/admin/api-key-delivery`:
`/policy`, `/identities`, `/identities/:identity_id`, `/caller-template`, `/verify`,
`/targets`, `/targets/:target_id`, and the target's `/approval`, `/isolation` and
`/reconcile` actions. Administrative reads require current intrinsic administrator
plus `secrets.read`. Writes require intrinsic administrator plus independent
`secrets.write`; remote Verify/reconcile also require `secrets.test`. Write authority
is not inferred from read permission, and approval is not external control proof
without the explicit scoped attestation.

Owner routes share `/keys` and `/projects/:project_id/keys`: `/delivery-context`,
`/delivery-targets/resolve`, `/delivery-targets/:target_id`, `/managed`,
`/:key_id/managed-rotation`, `/delivery-receipts/:request_id`,
`/:key_id/delivery-reconcile` and `/:key_id/delivery-descriptor`. Every request
reauthorizes the current admitted Session and resource; Project lifecycle and
manager continuity remain mandatory. Writes require same-origin JSON, current
CSRF, a required reason and the exact reviewed quoted lowercase 64-hex If-Match.
Bodies are bounded and reject unknown/duplicate nested fields and malformed text.
No endpoint accepts arbitrary Vault destinations or returns private entity records.

Service operations use one parent-shortened budget: at most ten seconds for writes
and three seconds for reads, without renewed network-stage time. Caller-token
expiry, machine-credential validation expiry, Key expiry and Session/permission
lifetimes remain independent. Expired or unavailable facts fail closed.

UI private reads hide during renewal/error; actor, Session, permission, target,
resource and option-generation changes synchronously fence stale callbacks.
Same-actor renewal preserves an already dispatched operation as uncertain, without
accepting its late result as success. Actor change, dismissal and unmount discard
transient secret material. Administrative Token review material also expires locally
after five minutes. Secrets/CSRF/Key material stay out of Query/Mutation caches,
browser storage, confirmations and logs. Use paired English/Chinese labels and the
existing local dialog/table/card hierarchy; no standalone setup console or global
owner directory is introduced.

## Persistence and acceptance boundary

Frozen V102 adds ten delivery tables and the Personal Key manual-default mode,
preserves existing Project modes, and extends root inventory to V9/twelve domains.
The manual-only policy singleton is the only initial delivery configuration:
no trusted machine identity, caller template, Profile, approval, attestation,
READY state, dispatch claim or acknowledged binding is seeded. See
[database contracts](DATABASE.md#prepared-api-key-delivery-and-root-inventory-v9-v102).

The `api_key_delivery_auth` domain covers every retained nonempty Writer/Provisioner
envelope, including disabled history. A compact purpose-bound typed-tuple commitment
includes exact auth/integration births, generation, descriptor security revision and
retained VaultRevision ID/birth. Rewrap does not substitute current infrastructure
or change source identity. Historical V1–V8 jobs and receipts retain their original
scope and cannot attest V9 coverage.

Acceptance status and exact receipts are recorded separately in
[Implementation](IMPLEMENTATION.md): Source gates, the original complete PostgreSQL/MySQL matrix, RouteX restart/authentication and production build passed on the qualified phase floor; bounded real local Vault acceptance passed, English/Chinese browser interaction was not performed because the user could not unlock the screen, and external caller interoperability remains unproven.
Controlled peers, real dual-database application qualification and genuine RouteX
process restart do not prove real local Vault JWT/CAS-zero behavior. Real Vault,
bilingual browser and external caller results each need their own recorded scope.
A reconstructed Service is not process restart. No coordinator, automatic rotation,
external cleanup or fleet application is inferred from this bounded contract.
