# Captured Gateway reservation manifest

The version-one service manifest is a source-only foundation for a future SQL
route and reservation verifier. It is not wired into admission, persisted, or
accepted as current authority. Compilation, focused race tests, mandatory checks
and real PostgreSQL/MySQL qualification remain pending.

`captureGatewayReservationManifest` consumes the final admission-prepared
candidate plan, its original bound/evidence, the corresponding quota snapshot,
and the original parsed native payload before attachment rewriting. Construction
performs no database/runtime read, network request, secret resolution or storage
object read. It does not prove that a local journal reservation committed.

The detached result retains Model, binding, Provider, Connection, Provider-model
and Credential IDs and actual births; original weights/priorities; recorded
revisions, transport and verification generations; explicit input declarations;
and the exact original candidate pairs. Unknown historical births are rejected,
not filled from later catalogue state or the current clock. The new hydration
fields do not change local route selection, admission, retries or settlement.

Capacity includes the exact attested input/output maxima, protocol, transport
generation and revision. Money-constrained captures include the complete detached
price schedule and FX map, including currently unused rates. Existing candidate
bound construction, `pricing.ReserveBound` and canonical evidence digests remain
the arithmetic source. The original logical-request maximum hold must match
exactly; it cannot be reduced for a cheaper fallback or recomputed after capture.
Absent unconstrained capacity/pricing/hold values remain absent, not proven zero.

Native classification is derived again with the existing four protocol parsers.
It records only the input container/shape, finite output capacity, cache
reachability, closed pricing dimensions, supported Messages-header conditions
and ordered image/PDF occurrence kinds/shapes. Duplicate references remain
separate forwarded occurrences. Input reservation uses attested maximum input;
no attachment byte, page, pixel or token estimation is introduced.

No prompt, message, tool schema, arbitrary user field, attachment ID/location/name,
URL, upstream model name, credential value, ciphertext, Vault descriptor or
source-holder proof is retained. A versioned transport fingerprint binds the
original normalized endpoint/protocol/adapter/API-version/upstream-name tuple
without retaining the endpoint/name. The existing egress revision is retained;
its secret-bearing inputs must only be recomputed inside the server. Azure
captures retain the operator-attestation identity and coverage revision, not
reader secrets or Vault descriptors. A future verifier must reconstruct current
coverage through the existing bounded deployment-coverage machinery.

The complete candidate set is bounded to 128 targets and 32 Credentials per
target, with the existing maximum of four attempts. Each captured price basis
uses the existing 16 KiB limit; the complete manifest is limited to 4 MiB.
Oversized or inconsistent captures are rejected as a whole, never truncated.
Captured values are detached copies, including amount/output pointers, API
version pointers, price-rate slices and FX maps. Canonical candidate ordering is
binding ID then Credential ID; it does not change selection priority or weights.

The next verifier must use caller-owned READ COMMITTED transactions, intersect
current writer locks, check exact IDs/births and all captured constituents, and
refuse dispatch after a relevant change. It must not discover new candidates or
reduce/recharge the original hold. Current local snapshot, egress, credential
source-holder, authorization and health checks remain independently required.
Revocation must not prevent settlement of actual already-dispatched work. This
slice adds no SQL verifier, shared activation, migration, public API or fleet
acknowledgement.
