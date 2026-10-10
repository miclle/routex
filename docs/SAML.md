# SAML protocol and application boundaries

The independent `pkg/saml` component creates an unsigned SP-initiated Redirect
AuthnRequest and verifies a bounded signed HTTP-POST authentication response.
The application adds reviewed configuration, explicit existing-member linking,
durable request and assertion correlation, native MFA and Session provenance.
Controlled application, PostgreSQL/MySQL and process-restart qualification pass.
Bilingual browser and external IdP acceptance remain open; component results
remain separate.

## Protocol contract

`New` captures explicit IdP issuer, HTTPS SSO endpoint, SP entity ID, HTTPS ACS
endpoint and one pinned public signing certificate in DER form. It makes no
network request and performs no metadata discovery. HTTPS endpoints have no
userinfo, query or fragment. Caller-owned random RequestID and opaque RelayState
are required; RelayState is never a return URL.

Verification accepts canonical base64 of at most 256 KiB of XML. A bounded
namespace-aware admission pass rejects external entities, directives, comments,
duplicate IDs, named-prefix rebinding and ambiguous structure before signature
work. Exactly one plaintext Assertion and one enveloped signature are supported;
the signed parent is either Response or Assertion. Encrypted assertions, dual
signatures, external signature references and IdP-initiated responses are excluded.
Ordinary scoped default namespaces are supported.

Both maintained validators must succeed against the pinned certificate. The
component consumes the canonical referenced node returned by signature validation
and compares its authentication facts with the separately validated assertion.
Strong algorithms, exact request correlation, recipient, destination, issuer,
audience and persistent NameID are required. NameID bytes and case are preserved;
email, display names, groups and arbitrary attributes grant no identity or rights.

Assertion Conditions and bearer confirmation validity are at most five minutes;
IssueInstant is at most 90 seconds old. An optional IdP SessionNotOnOrAfter must
be future and can shorten proof expiry, but a long IdP session does not extend
assertion validity. AuthnContext does not substitute for RouteX native MFA.
Configuration/certificate checks repeat around validation. The ten-second context
bounds operation admission and completion; synchronous bounded crypto is not
advertised as preemptively cancellable.

## Dependency and licensing

The component pins `github.com/crewjam/saml` v0.5.1 and explicitly requires
`github.com/russellhaering/goxmldsig` v1.6.1. The latter includes fixes beyond the
older transitive version; do not remove that pin without reviewing upstream
signature-validation and canonicalization advisories. XML parsing uses the pinned
`etree` dependency. Verbatim dependency licenses are retained under
`third_party/licenses/` and indexed there.

## Application contract

Authentication settings contain one reviewed SAML provider: name, exact IdP issuer,
HTTPS SSO URL, SP entity ID, fixed HTTPS ACS URL and one public signing certificate.
Configuration and status writes require administrator authority, a fresh strong
If-Match review and a reason. Saving does not verify or enable the provider.
Explicit administrator verification proves the external identity for that existing
administrator and links it; enabling remains a separate reviewed action. Account
linking and unlinking require the current exact Session, local password, any
required native MFA proof, a reviewed ETag and a reason. No NameID, email or group
claim creates a member or grants a role.

The application persists one-use request, browser and assertion proof. Separate
host-only `__Host-routex_saml_start` and `__Host-routex_saml_delivery` cookies are
Secure, HttpOnly, SameSite Strict, Path=/, without Domain. Only their exact names
are accepted; duplicate names and fallback aliases cannot authenticate. Each
independent secret is hash-bound to the same durable ceremony. Starting another
ceremony replaces the browser pair; mismatched pairs fail closed.

`POST /api/v1/auth/saml/acs` accepts the bounded cross-site form without relying
on a Session or start cookie. A valid signed response stages proof and sets only
the independent delivery cookie. Success and rejection both redirect to the fixed
clean `/auth/saml/complete` page; ACS never issues a Session or creates a binding.
The page performs no automatic completion. Explicit Continue reads the real
Session and submits same-origin completion with both browser proofs; a current
Session also requires CSRF. Login cannot silently replace an existing Session.
Binding and administrator verification recheck the exact original Session,
member birth, password/MFA generation, binding, configuration and policy.

Login resolves the exact issuer and persistent NameID to an existing binding
created no later than ceremony admission, then checks current member admission.
Completion consumes the ceremony atomically. Native MFA still returns a transient
HTTP 202 challenge rather than a Session; a successful native proof preserves the
SAML primary provenance. An enabled, unchanged and currently verified policy,
exact binding identity and member birth remain required for Sessions and challenges.

A name-only save preserves bindings, verification and SAML Sessions. Changing the
issuer, SSO URL, SP entity ID, ACS URL or signing certificate resets verification,
disables admission, removes bindings and revokes SAML Sessions, challenges and
pending ceremonies. Disabling preserves bindings but revokes SAML authority;
explicit unlink revokes only that binding's authority. Local, OIDC, OAuth and LDAP
authentication remain independent. Assertion replay receipts survive these changes
until the signed proof expires. Explicit Return clears the browser pair through
the same-origin abandon endpoint; it does not claim durable ceremony cancellation.

## Bounds and privacy

Ceremonies expire within five minutes. At most 1,024 unexpired ceremonies and
1,024 unexpired assertion receipts are admitted; cleanup removes at most 128
expired rows per admission. Receipt retention rounds signed expiry upward to
microseconds, while completion expiry rounds downward. Start and ACS operations
have a parent-shortened ten-second budget; every local database phase reuses one
original admission deadline capped at five seconds, including failure cleanup.
No database lock spans protocol signing or signature validation. ACS form reads
have a finite five-second transport deadline within the original operation budget.
Unsupported deadline control fails closed. Protocol XML and signature bounds remain
those described above.

ACS accepts only its strict form fields and bounded body. The middleware captures
form material before ordinary logging and removes both correlation cookies on
every path. Proofs, raw assertions, passwords and MFA material are not browser
storage or query/mutation-cache resources. API responses use private/no-store
headers and sanitized errors; protected audit records contain typed reason-only
changes, not assertion or identity material. The public signing certificate adds
no encrypted root-key inventory domain.

SAML is excluded from the approved enforced-SSO allowlist, which accepts verified
OIDC and LDAP only. Forced-SSO and emergency recovery remain separate work.

## Verification

Focused race tests cover genuine Response and Assertion signatures, RSA/ECDSA,
scoped default namespaces, tampering, wrapping, weak algorithms, exact identity,
time/audience/certificate bounds, cancellation and parallel immutable clients.
All 104 named focused race tests pass without skips or failures. Mandatory
checking and complete Task pass, including 6,177 frontend tests and production
assets; component statement coverage is 85.2%. The original fixture failures remain
recorded in the implementation index.
This is historical component qualification. Application qualification separately
passes mandatory check R4 and complete Task R2, including 6,228 frontend tests
in 232 files, Go race, development lifecycle and embedded production assets.
The original complete integration Task passes in 2,949.064 seconds: 194 business
scenarios plus four constraints, 467 balanced passes on each database and 8,095
ordinary passes with three intentional TLS helper skips. The matrix includes
genuine signed SAML, native MFA, held callbacks, offboarding and two-generation
SAML Session restart. Full readback SHA-256:
`2997180c2ed1703b6e58b63aac87b08b10c3d3764aa80dc8f742bf11bf1356cd`.

The separate original authentication lifecycle passes with eight observed process
starts across both databases. Readback SHA-256:
`932056b2fd3b7844e60f9bf7358cad2680e5272177aa91cea23a67fb38ca75e8`.
Both readbacks verify the unchanged 2,286-path source/mode floor, absence of all
owned processes/groups and Compose resources, refused and freshly bindable ports,
removed temporary helpers and preservation of the original development service.
Earlier failed fixture/runtime diagnostics remain recorded in the acceptance
index; they are not passing gates. Bilingual browser and external IdP acceptance
remain open. No metadata discovery, SLO, remote logout, IdP-initiated login or
automatic provisioning is implemented by this profile.
