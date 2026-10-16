# Historical shared admission recovery

`recoverSharedAdmission` reconciles one original retained spool key with SQL
history. It is a source candidate; compilation, focused tests, real-driver
acceptance and runtime startup integration remain pending.

The recovery interfaces deliberately omit admission, dispatch, attempt creation
and terminal preparation. Recovery cannot construct a live coordinator or call
an inference transport. A new process incarnation, owner fence, expired heartbeat
or missing SQL receipt never proves executor closure or known usage.

## Reconciliation

1. Read and validate the original local admission, complete ordered attempt and
   checkpoint prefix, and any terminal. Preserve the original owner, execution
   ID, reservation, maximum hold and digests.
2. Read the exact historical SQL receipt. Validate both complete views before
   acknowledging any transition. A missing or conflicting receipt returns an
   explicit error and leaves the original local record intact.
3. Acknowledge the matching historical acceptance. Before a terminal exists,
   acknowledge matching historical dispatch receipts. Once an original terminal
   exists, preserve its local history: an earlier uncertain dispatch confirmation
   must not require a forbidden new post-terminal journal transition. Exact SQL
   dispatch proof and original attempt attribution remain required. Replay only
   already persisted local checkpoints absent from the SQL prefix. These
   operations confer no execution permission.
4. Without an original durable local terminal, report pending. Do not create a
   terminal, release an executor, invent zero usage, discard the spool record or
   reduce an unknown economic hold.
5. With the original durable terminal, replay its exact historical settlement.
   Only a matching complete SQL terminal receipt allows local delivery readiness.
   The final receipt must retain the original receipt ID and complete prefixes.

Every operation uses the caller's cancellation and a finite timeout of at most
30 seconds. Errors after potentially committed writes remain uncertain and
explicit retries retain the same immutable bodies. Recovery does not detach
background work, renew authority, prune history or acknowledge fact delivery.

## Authored controls

The focused test source covers durable close/reopen with a discarded live
coordinator, terminal recovery after a missing local dispatch acknowledgement,
ordered multi-attempt checkpoint preservation, once-only settlement and preserved
unknown holds, unresolved acceptance/dispatch/checkpoint stages, missing SQL
receipts, before/after commit faults, conflicting histories rejected before
writes, cancellation and timeout bounds. Fault stores model the two durable
sides; they do not establish SQL
locking or PostgreSQL/MySQL behavior.

Bounded pending enumeration supplies exact keys separately. Opening a journal,
choosing a persisted admission mode, owner registration, operator cutover and
two-process acceptance remain separate startup/runtime requirements.
