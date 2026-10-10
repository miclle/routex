# Generic OAuth authorization-code component

RouteX's bounded generic OAuth component accepts explicit Authorization, Token
and JSON profile endpoints. It returns only a configured stable subject; it does
not establish a RouteX member, Session or binding. Existing-member application
integration is described separately below. There is no email linking, automatic
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
application callback, browser workflow or complete F03/A15 acceptance is claimed
by that protocol-only qualification; the application has separate evidence below.

Protocol references: [OAuth 2.0](https://www.rfc-editor.org/rfc/rfc6749),
[Bearer token usage](https://www.rfc-editor.org/rfc/rfc6750),
[PKCE](https://www.rfc-editor.org/rfc/rfc7636) and
[current security practice](https://www.rfc-editor.org/rfc/rfc9700).

## Existing-member application integration

The application adds one independent custom provider beside local login and OIDC,
explicit linking for existing admitted members, and separate callbacks,
completion, native MFA and revocable provenance. It creates no account, links no
email, assigns no claim-derived role and introduces no enforced SSO or presets.

### Configuration and linking

Intrinsic administrators with independent `registration.write` review a strong
ETag and reason in the existing authentication drawer. Configure explicit HTTPS
Authorization, Token and JSON profile endpoints, the exact callback path
`/api/v1/auth/oauth/callback`, a client ID, transient replacement secret, explicit
Basic or POST client authentication, ordered scopes and a JSON string array of
profile object keys. Dots and numeric-looking keys remain literal object keys;
there are no array indexes, implicit email selection or floating-point subjects.

Save, Verify and Enable are separate reviewed operations. Verification requires
fresh local password/native MFA proof, then the actual provider callback for the
exact administrator and configuration. Existing members bind or unlink only their
own identity in Account Security with fresh Session/CSRF, local proof and review.
A remote typed identity cannot be shared by two members. Name-only edits preserve
security facts; security-tuple changes invalidate OAuth bindings, ceremonies,
verification and Sessions. OAuth mutations preserve local/OIDC authentication.

### Ceremony and session boundary

Same-origin starts use independent random state and browser correlation. The
separate `routex_oauth` cookie is Secure, HttpOnly, SameSite=Lax, scoped to
`/api/v1/auth/oauth` and expires after five minutes. Pending ceremonies are bounded
at 1,024 retained live records; indexed expired pruning removes at most 128.
Only hashes and domain-separated derived PKCE are used for browser proofs.

The callback redacts proof material before logging, atomically claims before one
bounded token/profile exchange and redirects only to `/auth/oauth/complete`.
It creates no Session or binding. Manual completion rechecks exact actor, Session,
member/binding births and captured policy/configuration. A native MFA HTTP 202
remains a transient challenge. Existing Sessions require CSRF; login refuses an
already authenticated caller. Missing, mixed or obsolete primary provenance never
falls back to local login. Runtime Team Sessions retain independent revocation
fences even when publication refresh fails.

The bilingual UI retains reviewed ordered-array drafts and exact uncertain write
intents. Secrets, passwords, MFA proofs and authorization results stay transient,
outside browser storage and shared query/mutation caches. Completion fences the
fresh transient Session read before POST. Successful writes settle the real Session;
obsolete responses cannot restore private state or settle another actor's intent.

### Migration, inventory and remaining acceptance

Frozen GORM V95 adds three OAuth tables and five independent Session/MFA provenance
columns each. Exact method/blank checks account for both supported database
collations, while released V1–V94 remain immutable. Historical migration tooling
may run a validated prefix through `MigrateThrough`; it rejects a newer ledger
before schema DDL and is never a business-service downgrade path.

Current root inventory V4 appends `oauth_providers` as the ninth domain; V1/five,
V2/seven and V3/eight history retain their exact original scopes. Every nonempty
client-secret envelope, including disabled configuration, uses the independent
`oauth:<provider-id>:<secret-generation>` reference. Older nonterminal jobs require
explicit reviewed Resume and new complete proof. Typed audit readback exposes only
a validated reason, never endpoints, profile, subject, cookie, code or token.

Formatting, corrected mandatory checking, 1,284 focused Go race results and
299 focused frontend cases pass. Two process-helper skips are intentional. Both
drivers pass all five selected migration/login/restart/root-rotation scenarios,
including genuine same-source OAuth restarts and encrypted OAuth secret rotation.
Independent readback confirms unchanged source and closed owned resources. The
frontend repair excludes render bookkeeping without delaying actual Session or
permission revocation. Complete Task passes Go race/coverage, 6,104 frontend cases
in 228 files, development lifecycle and production assets. The official complete
188-scenario matrix passes 450 named results on each database without failures or
skips; ordinary regressions pass 6,895 named results with two intentional TLS
helper skips. Independent readback confirms unchanged source and closed owned
processes/resources. Separate real-process authentication lifecycle also passes
both drivers. Its application sampler has an argv limitation; no individual PID
or port census is claimed, while the joined inherited Task group is confirmed
absent. See [implementation evidence](IMPLEMENTATION.md).

Bilingual browser interactions and real-provider deployment remain open.
Controlled fixtures do not prove external deployment. F03 and A15 remain Partial
throughout this slice.
