# SQL route and reservation validation

This source-only foundation validates the captured Gateway reservation against
current SQL facts. It is not wired into shared admission or upstream dispatch.
Compilation, focused tests and real PostgreSQL/MySQL qualification are pending.
It adds no migration, receipt, public API or shared-mode activation.

`validateGatewayReservationAcceptSQL` checks every original captured candidate.
`validateGatewayReservationDispatchSQL` checks one exact original candidate and
attempt number while still validating the complete original capture and maximum
hold. Both require a non-null manifest and return only an error. No successful
return is a reusable authority token. The future durable intent/SQL receipt must
retain the original manifest so dispatch cannot substitute a later catalogue.

The caller owns a bounded READ COMMITTED transaction and must already have
successfully checked owner fencing, current principal authorization, exact
policies and calendar revision in that same transaction. Governance must be
locked before owner/authentication rows. The verifier reacquires governance,
then locks the Model, sorted Providers, Connections, Credentials, Provider-models,
bindings, immutable reference/coverage rows, capacity, pricing and egress rows.
Connection locks precede Credential locks, intersecting existing credential
toggles that do not acquire governance. It calls no administrative write/snapshot
helper with a different resource lock order. The verifier performs no writes;
the transaction must nevertheless permit `SELECT FOR UPDATE`, so SQL
`ReadOnly: true` is unsuitable. GORM does not expose a portable transaction
isolation getter; correct isolation and preceding checks remain caller contracts.

Reconstruction checks exact IDs, births, parents, active Model status, positive
weights, current enablement, metadata revisions, transport generations,
verification, declarations and original candidate membership. Exact comparisons
do not depend on database collation. Unknown or absent rows fail closed. SQL
does not reproduce process-local snapshot identity, health, credential
source-holder admission or egress generation: the existing local checks remain
independently mandatory immediately before upstream work.

Capacity and complete price/FX constituents are rebuilt with the existing pure
reservation functions and canonical evidence digests. Unused FX rates remain
part of the comparison. Current price schedules retain the runtime's ID ordering.
The original maximum logical-request hold is never reduced or recharged for a
cheaper selected route. Unconstrained absent capacity/pricing remains absent.
Revocation or validation failure must not block settlement of work dispatched
earlier; economic persistence is outside this slice.

Queries use chunks of at most 256 exact identities/pairs, a cumulative limit of
128 queries and 32,768 returned rows, and explicit overflow probes. Candidate
ceilings remain 128 targets and 32 Credentials per target. Price reads include
only selected Provider-models, at most ten rates per price, and the complete FX
table with an eight-row overflow probe for the seven supported currencies.
Neither the unbounded runtime pricing loader nor catalogue-wide discovery is
used. Coverage reads include only original Credential/Provider-model pairs and
reuse the existing deployment coverage projection. Azure identity is recomputed
from immutable reference metadata without decryption, network access, source
context/source-holder proof or secret resolution. Egress fingerprints are
recomputed internally without decrypting proxy authentication; no loaded private
fields are returned, persisted or logged by this verifier.

The manifest's 4 MiB source ceiling is not the live spool allowance. Eventual
composition must validate the complete captured intent against the existing
`SharedSpoolMaxDataBytes` 128 KiB limit and record envelope. No truncation or
silent limit increase is authorized. Client IP, request text/objects and transient
source-holder proofs remain outside this captured manifest and retain their
separate current checks.

The opt-in `TestGatewayReservationSQLIntegration` worker is deliberately not
registered in the official matrix. It requires an exclusively owned fresh
`routex_test` database and one explicit driver. Authored controls cover unchanged
reconstruction, stored constituent changes, exact identity/birth, selected-only
validation without shrinking the original hold, Azure attestation freshness and
both lock directions for credential toggles. These controls test this route
boundary, not prior owner/auth/policy authorization, native inference or a live
shared authority. Root qualification must execute both supported drivers before
claiming database acceptance.
