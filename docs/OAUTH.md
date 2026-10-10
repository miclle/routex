# Generic OAuth authorization-code component

RouteX's bounded generic OAuth component accepts explicit Authorization, Token
and JSON profile endpoints. It returns only a configured stable subject; it does
not establish a RouteX member, Session or binding. Existing-member application
integration is a separate work package. There is no email linking, automatic
provisioning, claim-derived role assignment, discovery or enforced SSO.

## Configuration and transport

Supply HTTPS endpoints and a fixed HTTPS callback, explicit client ID/secret,
`client_secret_basic` or `client_secret_post`, scope tokens and exact object-key
segments for the profile subject. Endpoint URLs reject userinfo, query strings
and fragments. The client copies scope and subject-path slices at construction.

An explicit context-aware endpoint policy and non-replaying RoundTripper are
mandatory. The policy checks every configured endpoint and generated
Authorization destination. The caller owns destination restrictions, transport
cleanup and cancellation of HTTP/body operations. No default transport, redirect,
authentication-method fallback, refresh, cookie jar or background worker exists.

## Exchange and identity

Authorization uses code response type, captured callback, state and S256 PKCE.
The caller must generate independent unpredictable state/verifier material and
atomically consume a trusted single-use ceremony before exchange. The component
checks the exact expected state and a 43–128-character unreserved verifier.

A token POST and one Bearer-authenticated profile GET share a maximum ten-second
context, shortened by the parent's deadline. Basic credentials use form encoding
before Base64. Both responses require JSON HTTP 200; redirects and errors never
trigger another authentication method or profile fallback.

Token JSON is limited to 64 KiB and profile JSON to 256 KiB. Parsing rejects
invalid UTF-8, unpaired surrogate escapes, duplicate decoded keys at every depth,
trailing input and bounded depth/node/member/array/string overflow. Responses are
bounded and closed, including rejected replies. Exported errors carry no response
body, profile, authorization code, secret or access token.

The configured subject is a nonempty bounded string or a canonical nonnegative
integer lexeme. Integers are never converted through floating point. Subject kind
is part of identity: string `"1"` and integer `1` are different. The application
must additionally preserve an exact provider namespace and enforce all local
member, binding, MFA, CSRF, replay, revocation and configuration rules. Tokens and
raw profiles are never returned or retained by the client.

## Verification boundary

The formatted component passes 134 named race-test results, including 31
independent public-API results, with no failures or skips. Mandatory project
checking and complete Task pass: Go race/coverage (90.7% for this package),
6,032 frontend cases in 225 files, development lifecycle and production assets.
The complete 2,171-path source/mode floor remains unchanged through those gates.
The initial project check found two character-predicate lint issues; the corrected
successor preserves the exact allowed characters and passes renewed package and
project gates. That initial failed check remains failed history.

This phase adds no application schema, persistence or authentication behavior.
The preceding OIDC dual-database acceptance remains bound to its delivered source;
it is not generic OAuth application acceptance. No external provider deployment,
application callback, browser workflow or complete F03/A15 acceptance is claimed.

Protocol references: [OAuth 2.0](https://www.rfc-editor.org/rfc/rfc6749),
[Bearer token usage](https://www.rfc-editor.org/rfc/rfc6750),
[PKCE](https://www.rfc-editor.org/rfc/rfc7636) and
[current security practice](https://www.rfc-editor.org/rfc/rfc9700).
