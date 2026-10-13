# Gateway configuration source codec

Codec v1 is a private, bounded representation of complete Gateway configuration
source facts. It is groundwork for reconstructable versions; it is not a version
archive, publication command, rollback API or runtime activation path. F30 remains
partial. No database schema, encrypted archive, root inventory, capture pipeline,
retention policy or serving behavior changes in this slice.

## Source boundary

The frozen structs in `internal/routex/service/runtime_configuration_source_v1.go`
are independent of evolving business entities. Explicit adapters retain recorded
actor admission and approval provenance, Personal and Project Keys, scopes and
rotation roots, grants, Projects and managers, Teams and memberships, authentication
policy, identity provider and binding proofs, Session birth and expiry, catalog
and routing bindings, private transport generations, credential coverage and Azure
attestations, network selection, stored limits, effective policies, quota settings,
reservation bounds and account birth/revision facts.

Every optional field and collection retains its explicit null/empty distinction.
Zero remains different from null. UTC timestamps retain nanosecond precision and
birth identity. Amounts and exchange rates remain exact decimal strings, including
recorded trailing zeros; binary floating point and exponent notation are rejected.
The adapters detach pointers, maps and slices, and canonical ordering never sorts
or mutates the live source.

Credential and egress material is excluded. Inline credentials retain the existing
ciphertext commitment. Vault credentials retain the existing source proof plus
immutable reference, descriptor and marker digests, integration/reference births,
revision, reader generation and nonsecret source context. Reader ciphertext and
plaintext credentials are not retained. Emergency policy retains a password proof
digest and MFA generation, without password hashes or MFA material. Bearer secrets,
CSRF values, request content, live HTTP clients, source holders, process-local
epochs, leases and egress generations are excluded. This private document still
contains Key/Session verifier hashes and identity proof facts: it must never be
returned by a public API or written to ordinary logs.

Pricing is a separately labelled observation with its own domain-separated digest.
It retains exact currency/FX/catalog/rate input so captured price facts can be
examined, without making historical pricing a rollback target.

## Codec and reconstruction contract

The complete canonical JSON document is capped at 16 MiB and 262,144 JSON token
entries, with nesting bounded at 32. Sorted row collections reject duplicates;
map keys use canonical JSON order. The decoder rejects unknown codecs, unknown,
extra, missing and duplicate fields, noncanonical bytes/order, trailing documents,
malformed references, births, generations, decimals and content/pricing digests.
Errors return one sanitized message without echoing private source or parser input.

The content digest is domain-separated and covers every frozen field. Existing
routing source and authorization projection digests retain their existing meanings
as independent observations; this codec neither changes nor reinterprets them.
They are not recomputed from missing secret material or used as historical command
receipts. The caller of the explicit source adapter must establish coherent source
capture and the bootstrap network-policy fingerprint in a future capture phase.
This slice provides no such capture claim.

Cold reconstruction accepts only bytes, expected digest and context. It returns
fully detached historical facts plus unresolved credential/egress dependencies.
It has no database, keyring, network, Service or publication input and cannot
backfill a missing field from current mutable rows. Canonical stale births and dangling historical references remain recorded facts;
the codec does not require current target presence or birth equality and never
backfills them. Nonempty malformed source identities and proof/generation values
still fail closed. Historical verification, enablement, Keys, Sessions, grants and
identity policy do not confer current authority. The result cannot be assigned to runtime
authorization/routes. Future archive and rollback work must independently enforce
current revocations, exact births, source availability and publication fences.

## Validation

The focused test suite covers a new child process decoding the complete source,
all source-domain content changes, all supported identity source families,
opaque Vault dependency fidelity, real denied/stale Session proof cold roundtrips,
recorded unavailable/dangling facts, forbidden secret material, exact null/zero and
decimal/birth/generation fidelity, detached source adapters, canonical tampering,
unknown codecs, invalid references, digest mismatch, bounds and cancellation.
Corrected focused Go race qualification passes all 79 named tests across 12
top-level tests, with zero failures or skips. Repository mandatory checking passed
for the original source; the nil-context regression assertion was subsequently
restored with narrow documented staticcheck exemptions; renewed mandatory checking
also passed for that correction. This slice introduces no schema or database access.
No runtime activation, database archive, browser, fleet acknowledgement or rollback
acceptance is claimed here.
