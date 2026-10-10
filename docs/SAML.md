# SAML protocol and application boundaries

The independent `pkg/saml` component creates an unsigned SP-initiated Redirect
AuthnRequest and verifies a bounded signed HTTP-POST authentication response.
Application configuration, explicit member linking, durable replay consumption,
Session issuance and browser completion are a separate implementation phase.
The component alone enables no login method or account provisioning.

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

## Application integration in progress

The application phase must persist one-use request, browser and assertion proof
before it can issue Sessions or bind an existing member. The reviewed design uses
separate host-only `__Host-routex_saml_start` and
`__Host-routex_saml_delivery` cookies: Secure, HttpOnly, SameSite Strict, Path=/,
without Domain. A successful cross-site ACS POST stages verified identity and
sets only the independent delivery proof. It redirects to a clean, unauthenticated
completion page. Only explicit same-origin completion with both exact proofs,
current authority and native MFA can finish the ceremony. Duplicate cookie names
and fallback aliases cannot authenticate. These are integration requirements,
not delivered application or real-browser evidence.

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
Controlled component tests do not establish external IdP interoperability,
application authentication, persistent replay protection or browser cookie behavior.
