# Shared admission coordinator source contract

The follow-on [dormant Gateway executor integration](SHARED_GATEWAY_EXECUTOR.md)
connects the real request lifecycle in source without a startup selector or
shared activation. Its execution qualification remains pending. The original
foundation scope and historical verification limits below remain separate.

**Source candidate — execution and qualification pending.** The private
service-layer coordinator defines the two-store ordering needed by shared
admission. It is not wired to the Gateway, implements no GORM authority or bbolt
spool adapter, changes no migration or configuration, and enables no shared mode.
The supported enforcement backend remains the existing local authority.

## Explicit ownership and original intent

One coordinator belongs to one transient logical gateway executor. It captures
the installation, persisted mode revision, spool identity, owner incarnation and
fencing generation, request and account policies, a versioned captured
authorization snapshot, calendar, bounds, initial recovery evidence and bounded
sanitized call payload.
The original intent also retains its exact owner process birth and mandatory
detached reservation manifest. The complete capture, bound, model birth and
protocol must agree with the original authorization and fallback. The manifest
retains the exact original plan snapshot; logical call attribution may remain
blank, while a nonblank snapshot must match. Every internal attempt retains the
exact original snapshot. The full canonical intent remains capped at 128 KiB. See
[Original reservation binding](SHARED_ADMISSION_RESERVATION.md).
It creates a fresh transient execution identity before any durable reservation.
An executor reconstructed after interruption cannot adopt another execution's
receipt or use a recorded dispatch as an execution permit.

Local spool and shared economic authority are independent explicit interfaces.
Neither can be selected as the other's fallback. Every retry retains the
original captured body and digest. Attempt identities retain exact Credential
and snapshot attribution; final and checkpoint facts preserve the immutable
resource scope and ordered attempt identities. Read-back requires matching
installation/owner/execution/request/digest evidence. A timeout or incomplete
read is never proof of absence.


## Captured authorization is not current SQL authority

`sharedCapturedAuthorization` version 1 replaces the unwired numeric
`AuthGeneration`. There is no global authentication generation. The redundant
`PolicyGeneration` is removed: the existing complete `Policies` projection and
admission digest retain account births, exact revisions and accounting semantics.
`CalendarRevision` represents the stored quota-setting ETag; this source change
does not alter its encoding or the existing quota-policy digest.

The pure constructor takes an explicit captured evaluation time and supplied
coherent durable facts. It checks exact owner/model relationships, active states,
expiry, registration admission, grants and the closed primary-authentication
shape, then copies only the following nonsecret fields:

| Principal | Captured facts |
| --- | --- |
| Personal Key | Model and User IDs/births, actual admission proof, personal grant revision/hash, Key ID/birth/nullable expiry and its existing lifecycle revision. The selected Model must belong to both the personal grants and Key scope. |
| Project Key | Model, Project and Key IDs/births, nullable expiry and exact Project/Key model relationships. Creator attribution is not a User owner; Project Keys have no lifecycle revision to invent. |
| Personal Session | Model, User and Session IDs/births, Session expiry, admission and personal grant proof, current authentication-policy revision and the exact primary binding. |
| Team Session | Model, User, Session and Team IDs/births, admission and primary proof, exact membership ID/role/nullable joined time, and optional grant provenance. A missing joined time remains unknown. |

Remote primary proofs retain binding ID/birth, actual configuration and policy
revisions, exact User birth, and the named provider/profile namespace when
applicable. Local authentication retains its existing empty method. Emergency
proof retains the current policy revision, User birth, MFA generation and factor
completion time. The constructor checks the existing emergency conditions using
its explicit evaluation time; password verifiers and their derived digests never
enter the snapshot. Mixed or incomplete primary families fail closed. Forced SSO
continues to permit only OIDC, LDAP and the configured emergency path.

No bearer, bearer digest, CSRF token, password verifier, external subject, whole
entity, model name or secret is serialized. The snapshot is bound to the exact
initial call scope and immutable admission digest. Caller mutation cannot replace
its saved original values during retry. Team membership has no creation column;
Team/Project grant rows have no birth or revision. Optional existing provenance
is preserved without claiming a synthetic ABA fence for those rows.

Validity means consistency of supplied captured facts, not current authorization
or a dispatch permit. Existing runtime Session identities collapse binding keys
and do not expose every persisted primary field; they cannot alone manufacture
this complete snapshot. Coherent durable capture and runtime wiring remain
required. A future SQL authority must read current Key/Session existence and
births, enabled/admitted owners, current grants, authentication policy and exact
primary bindings, current managed-delivery acknowledgement where required, and
resource lifecycle. Project authorization also requires current eligible
managers; it must not use the Key creator as authority. Emergency authorization
must independently recheck current MFA and password-bound Session validity
without persisting those verifiers in the spool. Revocation prevents new
acceptance/dispatch; it cannot discard settlement of the original accepted
request and its already observed economic evidence.

## Durable ordering

| Stage | Required durable ordering | Interrupted result |
| --- | --- | --- |
| Admission | Reserve local payload slot; verify its exact intent; atomically accept through the shared authority; confirm the receipt locally. | No dispatch. Explicit retry uses the captured original admission. RPM, holds and concurrency must be idempotent in the future authority transaction. |
| Attempt | Prepare the exact fallback locally; commit the shared dispatch intent; confirm that intent locally; invoke transport once. | Any uncertainty at or after the shared dispatch call permanently forbids execution of that attempt. Read-back cannot restore permission to send it. |
| Checkpoint | Capture and persist the original ordered local evidence; publish its exact digest to the shared authority. | Further native attempts remain blocked until acknowledgement or exact read-back. Changed evidence conflicts. |
| Terminal | Explicitly close the logical executor; capture and persist the final fact locally; settle through the shared authority; make the matching local fact ready. | Exact terminal retries/read-back complete storage work without repeating inference or replacing original evidence. |

The transport callback remains under the caller's original context and deadline;
the short storage deadline is not substituted for the inference deadline. It
must not create detached inference work. Callback completion alone is not a
logical executor closure: the caller must finish response-body and stream
lifetimes before explicitly closing the executor.

Only an actually executed attempt with acknowledged, explicit no-work evidence
can permit the next bounded native attempt. An uncertain dispatch intent, an
unknown attempt or an unacknowledged checkpoint cannot permit fallback. The
existing routing planner still owns retry classification, candidate eligibility
and fresh route authorization. The future shared authority must recheck the
captured identities and actual current policy, grant and primary-authentication
revisions at admission and every new dispatch; this coordinator does not
establish those SQL checks itself.

Checkpoint, reconciliation and settlement use finite detached operation
contexts so client cancellation cannot discard already captured evidence.
Detached operations still expire, and expiry preserves uncertainty. Explicit
retry workers must replay the original storage intent and never execute
inference.

## Closure and independent economic settlement

Gateway concurrency retains its existing meaning: a RouteX logical executor /
transport lifetime. Proven closure of that exact executor permits its shared
gateway concurrency to be released independently of unresolved Tokens or money.
A fenced exclusive-spool reopen can support recovery proof for the former
process; heartbeat or owner lease expiry alone cannot.

Upstream work is a separate fact. Local cancellation, owner fencing, socket
closure, deadline expiry and process closure do not establish known upstream
usage. Terminal settlement keeps nullable Token and monetary dimensions
independent, validates supplied settlement against the actual final call fact,
and preserves exact monetary strings. Missing dimensions retain their original
conservative bounds or explicit unbounded unknowns in the future authority.
Native terminal evidence and explicit no-work proof remain distinct from HTTP
success and usage completeness. The coordinator introduces no remote-upstream
concurrency cap.

## Adapter and activation work still required

The shared authority must implement portable GORM transactions, exact conditional
owner/incarnation fencing, all conjunctive accounts/windows, independent
alert-only behavior, coverage/account birth, exact money, idempotent original
receipts and economic history retention after call delivery. Storage adapters
must return independent records, validate supplied digests and reject conflicting
transitions atomically. The test doubles demonstrate interrupted-commit ordering;
they are not those algorithms or real database concurrency proof.

The local spool requires explicit shared-mode open/recovery semantics. Current
`Queue.Open` automatically recovers local quota state and promotes pending facts;
that behavior cannot be reused before shared reconciliation. No such change is
included here. Missing/corrupt spools, new-incarnation recovery, quiescent legacy
history transfer, persistent deployment-mode selection, real adapter wiring and
two-process PostgreSQL/MySQL qualification remain unfinished.

Authored tests cover original-body retention across interrupted admission,
uncertain dispatch with no execution or replay, local preparation retries,
checkpoint barriers, unknown-attempt rejection, terminal faults with canceled
clients, exact known-Token/unknown-money read-back, owner fencing, tampered
digests and active-executor closure rejection. Captured-authorization tests cover
all four principals, every supported primary family, explicit-time expiry,
owner/model/birth and mixed-proof rejection, detached copies and immutable retry
digests. All test execution and mandatory
project checks remain pending for this source candidate.

See [the authority boundary](ADMISSION_AUTHORITY.md) and the
[existing local ledger contract](QUOTA_LEDGER.md).
