# Original reservation binding

The shared admission request requires `Reservation *GatewayReservationManifest`
and `OwnerStartedAt time.Time`. The coordinator detaches the complete request
before creating its admission digest. The manifest and original process birth
are immutable digest inputs, retained in the spool and economic receipt rather
than reconstructed from a current runtime snapshot or owner record on retry.
Canonical client IP remains transient; it is not part of the stored request.

Admission validation reuses the existing pure complete-manifest structural
validator. RequestID, exact model identity and birth, native protocol and maximum
reservation must match the captured authorization, initial fallback and original
bound. The manifest retains the exact original plan SnapshotID. Logical call
attribution may remain blank before or after an attempt; any nonblank logical
SnapshotID must match that manifest. A missing manifest, missing or reduced
original target set, corrupt constituent or inconsistent hold fails. Structural
consistency does not establish the trust of caller-supplied data: live authority
must bind the capture to its original trusted plan and current SQL and
process-local execution guards before any new admission or dispatch.

The initial fallback has no inference attempts. Subsequent attempts retain the
same published snapshot and select only an exact Credential/Connection/provider-
model tuple in the original manifest. Checkpoint and terminal facts retain the
original logical scope and exact internal attempt attribution without inventing
a top-level logical SnapshotID when it is unknown. No retry replaces the manifest
with only its selected candidate or recomputes a smaller maximum hold.

Owner process birth is explicit, positive and representable at the persisted
microsecond precision. Historical SQL receipt reads require the retained row
birth to equal the original digest-bound request birth; another valid timestamp
does not substitute for it. Current owner fencing and historical identity remain
separate: later owner changes do not authorize another executor to dispatch or
prevent exact original history reconciliation.

The complete canonical admission intent, including the full manifest, must fit
the existing 128 KiB spool body cap. The standalone manifest's larger capture
limit does not enlarge that cap. Oversized originals fail before reservation;
there is no candidate trimming, pricing truncation or alternate persistence
representation to make them fit. All constituent bounds remain unchanged.

Focused test sources cover missing/reduced captures, model/birth/protocol/
snapshot/bound mismatches, blank logical attribution with exact attempt
attribution, corrupt pricing, missing or imprecise owner birth,
detached nested constituents, immutable digest reuse and a structurally valid
standalone manifest that exceeds the whole-intent spool cap. Historical fixtures
retain exact owner birth and manifest header changes explicitly. Their positive
Chat pricing reservation uses the real conservative evidence/maximum builders,
including the original cache and rounding margin; hold assertions are preserved.

These are source changes only. Compilation, focused/race execution, real-driver
qualification and live Gateway activation remain separate gates. Earlier
projection/history/settlement evidence binds its predecessor source and does not
qualify the changed admission representation.
