# Discord login

The Discord profile is an independent existing-member login method. Controlled
application qualification passes; browser and registered external-client
acceptance remain pending. It does not create members, match email
addresses, import roles or guild membership, use Bot tokens, or enable forced SSO.
Local passwords, native RouteX MFA and the other identity methods remain separate.

## Fixed protocol and exact identity

The fixed `discord.oauth2.v1` profile uses authorization-code OAuth with S256 PKCE
and exactly the `identify` scope. Its endpoints are fixed:

| Operation | Endpoint |
| --- | --- |
| Browser authorization | `https://discord.com/oauth2/authorize` |
| Token exchange | `https://discord.com/api/v10/oauth2/token` |
| Current authenticated user | `https://discord.com/api/v10/users/@me` |

Token exchange uses HTTP Basic only, with each client credential form-encoded
before Basic encoding. The code, exact callback and PKCE verifier are sent in the
form body; client credentials are not repeated there. One successful token
exchange permits one Bearer-authenticated current-user request. There is no
redirect, retry, authentication fallback, refresh workflow, discovery or ID-token
validation. Failed token exchange performs no current-user request.

Both the application client ID and returned top-level user `id` must be canonical
nonzero uint64 decimal strings: one to twenty ASCII digits, no leading zero and
at most `18446744073709551615`. A JSON number is rejected. The exact admitted
string remains the subject; no floating-point conversion, trimming, case folding,
username or email substitution is permitted. `https://discord.com` is the fixed
identity namespace, not an asserted OIDC issuer. Other provider namespaces remain
independent even when subject text happens to match.

Discord's [OAuth2 documentation](https://docs.discord.com/developers/topics/oauth2)
and [current-user resource](https://docs.discord.com/developers/resources/user)
describe authorization, HTTP Basic token authentication and identity operations.
The [account-linking guide](https://docs.discord.com/developers/discord-social-sdk/development-guides/account-linking-with-discord)
separately documents PKCE verifier exchange with confidential Basic authentication.
Applying that combination to the selected identify-only web flow remains an
interoperability inference. It is not evidence that a registered live
web application has completed this exact flow. Controlled fixtures and external
Discord interoperability are separate acceptance boundaries; no external SDK is
required by RouteX.

## Configuration, verification and linking

The authentication settings use the existing method card and configuration drawer
with name, client ID, transient client secret, HTTPS origin/callback guidance and
separate enablement. The dedicated registered callback path is
`/api/v1/auth/discord/callback`. Endpoints, scopes and identity mapping are not
editable. Saving never verifies or enables login. Explicit administrator
verification authenticates and links that administrator's own identity for the
exact configuration revision; enabling remains a separate reviewed action.

Every management read, save, status change and verification requires a current
intrinsic administrator and independent `registration.write`. Final callback and
completion repeat this combined authority for verification purposes only. Member
linking and unlinking instead require the exact current self Session, local
password, required native MFA, reviewed strong If-Match and a reason. They do not
require management permission. Public login accepts only an explicitly linked,
currently admitted member; unknown identity never provisions an account.

The safe configuration response contains only `name`, `client_id`, `callback_url`,
`secret_configured`, `enabled`, `verified`, `review_etag` and `mfa_required`. The
self response contains `available`, `name`, `bound`, `review_etag` and
`mfa_required`. Neither returns a subject, client secret, access token or stored
authentication envelope. An unconfigured profile remains empty, disabled and
unverified. Keep-secret writes require an empty submitted secret; replacement
accepts one to 4,096 UTF-8 bytes without ASCII controls. Names are trimmed and
bounded to 100 runes; reasons use the existing trimmed 1,024-byte limit. Callback
URLs are HTTPS, at most 2,048 bytes, with the exact callback path and no query,
fragment or user information. Proof inputs retain existing local-password and
native-MFA validation.

Security changes to client ID, callback or secret disable and reset Discord
verification, remove its bindings and revoke its Sessions, native challenges and
pending ceremonies. Name-only changes preserve those security facts while
changing the review revision. Disable retains bindings and revokes Discord
authority; unlink affects the exact self binding. Neither operation revokes
unrelated local or enterprise authentication. Held remote work must pass fresh
member, binding, configuration, policy, Session and proof-generation checks before
it can restore any authority. Offboarding remains authoritative during the flow.

## HTTP and browser flow

All paths below are relative to `/api/v1`. Management and self mutation routes
require same-origin CSRF protection, strict JSON, a reason and reviewed strong
If-Match where indicated. Their reads require current Sessions and resource
scope; management additionally requires the combined authority described above.

| Route | Purpose and authority |
| --- | --- |
| `GET /auth/discord` | Public availability and name only |
| `POST /auth/discord/start` | Empty-object anonymous login start; an existing valid Session is rejected |
| `GET /auth/discord/callback` | One-use exchange and proof staging; never Session creation |
| `POST /auth/discord/complete` | Empty-object manual completion; fresh optional Session and CSRF when valid, exact original Session for linking/verification |
| `POST /auth/discord/abandon` | Empty-object same-origin browser-proof clearing; CSRF when a valid Session exists |
| `GET`, `PUT /admin/auth/discord` | Read or save configuration; PUT requires If-Match |
| `PUT /admin/auth/discord/status` | Explicit enable/disable with If-Match |
| `POST /admin/auth/discord/verify` | Password/MFA-reviewed verification start with If-Match |
| `GET /account/identity/discord` | Current self binding metadata |
| `POST /account/identity/discord/bind`, `/unlink` | Password/MFA-reviewed self linking or unlinking with If-Match |

A start returns only an authorization URL and sets the exact host-only
`__Host-routex_discord` cookie: Secure, HttpOnly, SameSite Lax, Path=/ and no Domain.
Exact duplicate cookie names fail closed. The callback captures and redacts its
cookie and query before downstream observation. Raw query input is bounded to
8,192 bytes; state/code/error authority keys are unique, and bounded single-valued
provider decorations are ignored. There is no invented OIDC `iss` or nonce
requirement for this OAuth profile.

The callback claims the durable ceremony before remote exchange, stages verified
proof once, and redirects only to clean `/auth/discord/complete` outside AuthGate.
Explicit Continue performs a transient fresh Session read and then completes
using current authority and CSRF. The opaque server ceremony owns its purpose;
the clean page cannot infer it. Login never silently replaces an existing Session.
HTTP 202 is a native MFA challenge, not a Session or authenticated navigation.
Successful linking or verification returns only its confirmed kind. Return to
sign-in navigates only after confirmed empty HTTP 204 from abandon; that clears
browser correlation, without claiming durable cancellation or remote logout.
Unresolved ceremonies expire, and a subsequent start replaces browser proof.

There is no automatic callback/completion replay. Secrets, tokens, correlation
proofs and native-MFA material remain transient and outside browser storage and
query/mutation caches. Actor changes, Session renewal, lost authority, dismissal
and unmount fence late responses. Uncertain configuration/status intents retain
the original reviewed body and ETag for explicit identical retry. HTTP responses
are private/no-store/nosniff; callback handling also prevents referrer disclosure.
Public audit changes expose only the typed `discord_identity` kind and validated
reason for admitted actions, never arbitrary private audit JSON.

## Bounds, persistence and acceptance

Remote token/profile work shares one parent-capped ten-second budget. Every local
admission phase reuses the original five-second deadline; no stage renews it.
No database lock spans remote HTTP or password work. The shared named-identity
admission cap is 1,024 live ceremonies, with at most 128 expired rows pruned per
admission and five-minute expiry. Response parsing closes bounded bodies before
use, rejects duplicate JSON keys and malformed Unicode, and limits token and
identity bodies to 64 KiB and 256 KiB respectively. An authorization URL is at
most 8,192 bytes. Missing or uncertain proof fails closed.

Frozen GORM V101 extends exact provider/profile/namespace and primary-proof checks
and inserts an empty disabled Discord configuration. It preserves released
V1–V100, the three named-identity tables and seven Session/MFA provenance fields.
Those fields bind the provider and profile, exact binding/member births and
configuration/policy revisions. Retained rows are validated before CHECK
replacement; partial MySQL DDL retries do not assume transactional rollback.

Root inventory V8 keeps the same eleven domains. Discord client secrets use the
existing `named_identity_providers` domain with profile/provider/generation-bound
AAD. Disabled retained configurations remain covered; remote tokens are not
inventory records. Historical V1–V7 jobs and verified observations keep their
original scopes and never acquire Discord coverage. In particular, V7 does not
prove Discord retirement readiness. No twelfth root-secret domain is added.

Mandatory checking and complete Task pass, including 6,539 frontend cases in
240 files, Go race/coverage, development lifecycle and embedded production assets.
The unchanged original complete PostgreSQL/MySQL matrix passes 206 business
scenarios plus four constraints, 509 balanced names per driver, 9,293 ordinary
passes/three intentional TLS-helper skips and 32 genuine application starts.
Separate authentication lifecycle passes eight genuine starts/four success
summaries. Independent readback verifies owned process/resource closure, refused
and fresh-bindable database ports, unchanged 2,395-path source/modes and preserved
original development. Delivery documentation is then synchronized and mandatory
checking renewed before commit. Bilingual browser and registered external-client
acceptance remain pending. Prior failed runs remain failed.
See [Implementation](IMPLEMENTATION.md) for independently recorded phase evidence.
F03/A15 remain Partial. Discord does not satisfy the verified OIDC/LDAP-only
forced-SSO boundary. The separately scoped enforcement and designated-administrator
emergency recovery implementation remains under qualification; it is not delivered
by this Discord phase. WeChat remains explicitly deferred.
