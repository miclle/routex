# OpenID Connect protocol component

The `pkg/oidc` package supplies a bounded authorization-code protocol engine.
It is not yet connected to RouteX HTTP routes or authentication. Enterprise
configuration, account binding, Session creation, MFA handoff and enforced SSO
are not delivered by this component. Existing local authentication is unchanged.

## Protocol contract

The caller supplies immutable issuer/client/callback settings, an explicit
context-aware transport and an endpoint policy. There is no default transport.
All endpoints require HTTPS and reject query strings, fragments and userinfo.
The caller must enforce destination policy and disable transport-level replay;
callback and authorization destinations are checked by that policy as well.

`Discover` captures one metadata response. `AuthorizationURL` generates a code
request with OpenID scope, nonce, state and S256 PKCE. `Exchange` sends one token
POST using explicit `client_secret_basic`, then reads one JWKS response. It
returns only the exact verified issuer and subject. It does not use email or
claims for account linking, return tokens, refresh keys, retry, call UserInfo or
create background workers. The caller owns transport cleanup.

Each operation caps its context at ten seconds. Supply one shorter outer context
when discovery and exchange must share a single budget. Limits are 64 KiB for
metadata and token JSON, 256 KiB/64 keys for JWKS, and 32 KiB for the ID token.
Responses must be JSON HTTP 200; redirects fail. Bodies are bounded and closed
before library parsing. Exported errors contain no upstream response or token.

Only advertised RS256/384/512 and ES256/384/512 are accepted. RSA keys require
2,048–8,192 bits; EC keys require P-256/384/521. Protected JWT `kid` must match
JWK `kid` exactly, including absence, and a declared JWK algorithm must match.
Coreos verifies signatures, issuer, audience and baseline claims. Additional
checks require exact issuer/nonce, appropriate `azp`, integer `iat`/`exp`, and
`nbf` without extra clock skew. A present `at_hash` must validate against the
transient access token. Deployment clocks must be synchronized.

## Integration requirements

An application adapter must persist and atomically claim a bounded, expiring,
single-use ceremony before any exchange. It must independently enforce browser
correlation, current actor and binding identity, CSRF for protected completion,
MFA, Session revocation and encrypted client-secret lifecycle. A verified subject
alone is never authority to create or select a local account. These application
requirements remain in progress outside the delivered package.

## Verification

Controlled package tests cover endpoint policy, cancellation and response bounds,
state/nonce/audience/time validation, signature/key restrictions and access-token
hashes. The reviewed successor passes 119 named race-test results. Transplanting
the new regressions into its preceding implementation reproduces six failures:
unknown/missing key ID, mismatched declared algorithm and wrong/empty/null
access-token hashes. These are controlled protocol checks, not evidence of an
external identity provider or a complete enterprise sign-in workflow.

The adopted component also passes mandatory project checking and complete Task:
Go race/coverage (90.8% statement coverage for this package), all 5,972 frontend
cases, development lifecycle and production asset-serving checks. Application
schema, transactions and authentication behavior are unchanged by this phase;
no new dual-database or external-provider acceptance is claimed.
