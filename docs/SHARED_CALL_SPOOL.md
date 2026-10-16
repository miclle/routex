# Shared call spool source contract

**Source candidate — execution and qualification pending.** A concrete
synchronous bbolt spool and private service adapter now implement the local
storage side of the [shared admission coordinator](SHARED_ADMISSION_COORDINATOR.md).
They are not wired to Gateway startup, enable no deployment mode, and implement
no shared GORM economic authority, migration, owner-registration protocol or
legacy-ledger transfer. Supported enforcement remains the existing local mode.

## Explicit mode and file ownership

`OpenSharedSpool` acquires its own exclusive bbolt file lock and persists an
immutable versioned shared-mode binding: installation ID, spool ID, mode revision
and capacity/data/history limits. A local or foreign journal, mismatched binding,
unexpected bucket, missing initialization component or invalid persisted record
is rejected. There is no automatic migration, repair or mode downgrade.

Local `Queue.Open` rejects the shared spool's reserved buckets before any local
initialization or economic recovery. This also rejects a partial shared journal
whose metadata marker is missing. Ordinary local-journal behavior is unchanged.
Tests capture complete file bytes before rejected opens in both directions;
their execution remains pending.

Reopening a shared spool validates its bounded records and leaves pending
admissions, attempt checkpoints and terminal preparations pending. It never
settles accounting, releases gateway concurrency, promotes interrupted facts,
registers a new owner or reconstructs an inference permit. A new incarnation
cannot adopt a previous request merely by opening the file; separate shared
authority reconciliation and exact closure proof are still required.

## Ordered durable records

The package stores opaque serialized bodies with independent byte checksums,
canonical consumer operation digests and exact installation/spool/incarnation/
fence/execution/request keys. It imports no service or domain entity. The private
service adapter validates coordinator digests, bounded sanitized call facts,
original resource scope, exact ordered attempt attribution, independent economic
settlement and closure evidence before requesting package transitions.

| Stage | Local durable contract |
| --- | --- |
| Reserve | Capture the original admission body in one bounded slot. An existing different identity/body conflicts; a delivered request cannot be recreated. |
| Accept | Bind the exact confirmed shared admission receipt. Attempt preparation requires that binding. |
| Attempt | Append an immutable ordered attempt. A subsequent attempt requires the prior local checkpoint and confirmed dispatch; the coordinator separately requires shared acknowledgement and safe native retry evidence. |
| Dispatch | Capture the exact shared dispatch receipt. This stored receipt never authorizes transport replay. |
| Checkpoint | Preserve the original ordered evidence and safe crash-recovery settlement. A different retry body cannot replace it. |
| Terminal | Preserve the original final fact and independently known/unknown economic dimensions after explicit logical executor closure. Preparation alone is not delivery readiness. |
| Ready | Bind the exact original terminal digest, request/owner identity and confirmed admission receipt ID. The coordinator supplies this only after confirming the matching authoritative SQL terminal receipt. |

The adapter records the trusted coordinator's confirmed receipt assertion; it
does not independently query SQL or manufacture SQL proof from a receipt string.
Each transition is one synchronous bbolt transaction with conditional state and
identity checks. Failed comparisons and cancellation before commit roll back
the whole transaction. Inputs, reads and delivery bytes are independent copies.

Admission stores the coordinator's versioned, secret-free captured authorization
and checks its exact principal/model scope against the initial call fact. The
same validation applies on read and reopen; a self-consistent digest alone cannot
admit malformed or cross-owner proof. Actual policy/account revisions remain in
the original policy projection and admission digest, not placeholder numeric
policy/auth generations. Captured validity is not current SQL authority: fresh
Key/Session/owner/grant/primary checks remain required at admission and dispatch.
Process-local runtime/egress generations are not shared fencing proof.

The original admission also persists a mandatory detached reservation manifest
and explicit microsecond owner process birth. Both are admission-digest inputs.
Structural validation retains every original candidate, capacity and price
constituent and checks exact RequestID, model/birth, protocol, snapshot and bound
consistency. Logical call SnapshotID may remain blank; a nonblank value must
match the manifest. Attempts must use that exact original snapshot and a
retained exact Credential/Connection/provider-model tuple. See
[Original reservation binding](SHARED_ADMISSION_RESERVATION.md).

## Bounds and delivery

Each serialized body is at most 128 KiB, including its embedded fact and
metadata. The service still validates each raw call fact at 64 KiB. Attempt count
uses the existing `routeattempt.MaxAttempts` of four. A complete record therefore
retains at most ten bodies: one admission, four attempts, four checkpoints and
one terminal. The 2 MiB serialized record limit includes all repeated bodies,
base64 expansion, keys, receipts and metadata; it is checked before every write.

The current package envelope leaves room for every individually accepted stage:
ten maximum bodies encode to 1,747,640 base64 bytes. With all identifiers and
digests at their permitted 64-byte maximum, unsigned integers at their maximum,
all four dispatch receipts and readiness metadata, the complete compact record
is bounded at 1,753,968 bytes, including 6,328 bytes of envelope metadata. This
leaves 343,184 bytes below the 2 MiB cap. These figures are source-level envelope
arithmetic, not executed qualification. They do not imply that admission has
reserved a physically allocated bbolt page budget.

This total-record guarantee applies to bodies that satisfy the configured
per-body limit. Admission alone does not prove that every later service DTO fits
a caller-selected smaller `MaxDataBytes`. Before activation, integration must
use and qualify the complete service envelope budget, including base64 encoding
of the bounded fact and every repeated semantic field, and reject configuration
that cannot preserve a valid terminal. No runtime configuration is wired here.

Each ready index entry and delivered tombstone is at most 2 KiB. Live retained
data is bounded by configured capacity × (2 MiB record + 2 KiB ready index), plus
configured delivered-history capacity × 2 KiB, plus small binding metadata and
bbolt page/index overhead. These are retained-data limits, not a promise that the
physical bbolt file shrinks or a substitute for filesystem disk limits.

Delivery reads only ready terminal records. The adapter returns their original
transient final facts for the existing idempotent SQL reporting contract; no
asynchronous delivery result is interpreted as admission accounting. Acknowledgement
requires the exact request/owner/execution key, receipt ID and terminal digest.
It atomically removes the delivered payload and ready index while preserving a
bounded exact receipt tombstone. The same acknowledgement remains idempotent;
changed identities/digests conflict.

Delivered-history exhaustion fails closed and leaves the ready fact intact.
There is no silent eviction or automatic pruning. Shared SQL economic history,
original receipts, account births, coverage, unknowns and conservative holds are
outside this spool and are never erased by local delivery acknowledgement.

All adapter methods check actual cancellation/deadline expiry before validation
or a durable transition. Package mutations check again inside the transaction
and immediately before synchronous commit. An in-progress filesystem commit is
not interruptible; uncertain completion requires exact read-back and identical
storage retry, never inference replay. The coordinator's finite detached
checkpoint/settlement contexts remain responsible for cleanup after client
cancellation.

## Qualification still pending

Authored real-file tests cover bidirectional byte-identical mode rejection,
foreign bindings, exclusive open, canceled creation, pending reopen, exact owner
conflicts, independent bytes, ordered CAS, capacity contention, repeated-fact
bounds, cancellation rollback, corrupt-body rejection, exact acknowledgement
and fail-closed delivered-history exhaustion. Adapter tests exercise the real
spool with the coordinator and interrupted shared-commit test doubles, including
unknown economics, terminal restart/delivery, prohibited dispatch recovery,
altered evidence and canceled/expired operations. They establish no actual SQL
or multi-process economic enforcement proof; all execution is pending.

Mandatory checks, local journal regression, focused spool/coordinator tests and
applicable races must pass before delivery. Actual shared GORM transactions,
durable request-authority correction, owner recovery, verified quiescent history
transfer, runtime integration and real PostgreSQL/MySQL two-process acceptance
remain unfinished. No shared mode may be activated from this source slice alone.
