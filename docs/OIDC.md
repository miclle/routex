# Existing-member OpenID Connect

RouteX supports one administrator-configured Authorization Code identity provider
for explicitly linked existing members. Local password sign-in and RouteX MFA
remain available. There is no automatic account creation, email-based linking,
claim-derived role assignment, LDAP or enforced SSO.

The containing commit delivers the existing-member application implementation
with controlled automated acceptance. Bilingual browser interactions and external
provider deployment remain open gates; F03 and A15 are not complete. Protocol-only
commit `c86eede` retains its independently recorded acceptance.

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
alone is never authority to create or select a local account. The application adapter implements these requirements as described below.
The protocol-only phase remains independently recorded in the verification history.

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


## Configuration and account linking

The Authentication workspace retains the existing method cards and configuration
drawer. Configuration requires an active intrinsic administrator and independent
`registration.write` permission. Save a provider name, HTTPS issuer, client ID,
HTTPS callback ending exactly in `/api/v1/auth/oidc/callback`, and client secret.
A saved secret is encrypted and never returned. Keeping it and replacing it are
explicit separate actions. Every write requires a reviewed strong ETag and reason.

Verification requires fresh local password and current MFA proof, followed by
sign-in to the identity provider. It links that exact administrator identity and
records verification for the captured security configuration. Verification never
enables login automatically. Enabling is a separate reviewed operation that
rechecks the exact verifier, member birth, binding birth and configuration.

Existing admitted members link or unlink their own identity from Account Security
using a fresh Session, CSRF, reviewed ETag, password, current MFA and reason.
Email addresses and provider claims never select a member. Subject identity is
byte-sensitive through a digest and exact checks on both supported databases.
A remote identity cannot be shared between two local members.

A name-only edit preserves verification, bindings and Sessions. Changing issuer,
client ID, callback or secret clears verification and bindings and revokes OIDC
Sessions. Disabling login invalidates its policy generation and revokes OIDC
Sessions while retaining bindings. Unlinking invalidates that exact binding.
Local Sessions and API Keys are unaffected. The browser settles the real Session
after configuration/status/unlink writes before reporting success; stale responses
cannot clear another actor's private state.

## Browser ceremony and sign-in

Start is a same-origin JSON POST. The server creates independent random state and
browser-cookie values, persisting only their digests. The `routex_oidc` cookie is
HttpOnly, Secure, SameSite=Lax, scoped to `/api/v1/auth/oidc` and expires after five
minutes. Deploy the application behind HTTPS. PKCE and nonce are derived with
separate domains from that cookie and the immutable ceremony ID.

The callback captures and redacts query/cookie material before request logging or
panic recovery. It atomically claims the pending ceremony before remote exchange,
then stores only the verified subject and bounded provenance. It never creates a
Session, links an account or mutates configuration. It redirects to the fixed clean
`/auth/oidc/complete` page; authorization codes never enter the SPA.

Explicit Continue reads the current Session and sends a same-origin JSON POST.
An authenticated completion requires current CSRF and the exact original Session
and member births. Login completion rejects an already authenticated browser.
Each ceremony is consumed once; uncertain remote exchange is terminal and cannot
be replayed. A RouteX MFA HTTP 202 remains a transient challenge, not a Session.
Only successful proof creates the final Session with exact OIDC provenance.

Every control-plane and runtime Session authorization checks the current exact
binding, member birth, configuration and policy. Revocation fences invalidate old
runtime captures without a finite Session enumeration limit. Missing provenance
never downgrades to local-password authentication.

Live ceremonies are bounded at 1,024, including terminal records until expiry.
Indexed cleanup removes at most 128 expired records per start. Discovery and token
exchange share a ten-second budget; no database lock is held across remote HTTP.
Protected audit events expose only a validated versioned reason, never configuration,
claims, subjects, cookies, codes or tokens.

## Persistence and secret lifecycle

Frozen GORM V94 adds provider, binding and ceremony tables and additive Session/MFA
primary provenance. Existing local rows gain blank method/revisions and null births;
no historical identity is invented. The migration validates column types, widths,
nullability, microsecond precision and exact ordered indexes before recording success.
Released migration definitions remain unchanged.

Root inventory V3 appends `oidc_providers` as its eighth domain. Every retained
nonempty client-secret envelope, including disabled configuration, uses the exact
`oidc:<provider-id>:<secret-generation>` reference. V1 five-domain and V2 seven-domain
history retain their original meaning. Incomplete older jobs require explicit Resume
and a new complete inventory proof; old completed history cannot prove V3 coverage.
See [internal secrets](SECRETS.md) and [database policy](DATABASE.md).

## Application acceptance

Mandatory project checking and complete Task pass, including Go race/coverage,
6,032 frontend cases in 225 files, development lifecycle and production asset tests.
The official PostgreSQL/MySQL matrix passes 185 ordered business scenarios plus
four constraint checks per driver (447 named results on each, no failures or skips).
Coverage includes empty creation, upgrade, repeat/concurrent migration, partial DDL,
controlled TLS login, native MFA, replay, revocation and same-source process restart.
The ordinary matrix pass records 6,200 named results and one intentional child-helper
skip. A separate real-process authentication lifecycle passes both supported drivers.

The official integration command owns its same-source executable build and bounded
SHA256 binding before either driver runs production restart checks. Each launch
records its exact process/group in a private pre-launch ownership guard. Uncertain
closure blocks table reset, Compose teardown and executable removal; confirmed
closure precedes the next launch. Independent readback confirms unchanged source,
joined owned processes, removed fixtures/Compose resources and preservation of the
original development service. Earlier failed fixture attempts remain failed history.

English/Chinese browser interaction and real-provider deployment acceptance remain
pending. Controlled TLS and process fixtures do not prove external deployment or
complete enterprise authentication. LDAP, additional OAuth providers, enforced SSO
and enterprise emergency recovery remain open F03/A15 scope.
