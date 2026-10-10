# Google login

RouteX supports a separate, initially disabled Google profile for explicitly linked existing members. It does not create members, match email addresses, import profile information or grant claim-derived roles. Administrator configuration contains a name, client ID, transient client secret and exact HTTPS callback URL.

## Fixed protocol and identity

The fixed `google.oidc.v1` profile uses Google's authorization, token and signing-key endpoints. Its scopes are exactly `openid profile`; RouteX does not request email or offline access, read UserInfo, persist remote tokens or implement refresh tokens. Authorization requires state, a derived independent nonce and S256 PKCE. Token exchange uses client authentication and validates the signed RS256 ID token, audience, expiry and nonce before staging a verified ceremony.

Identity is the exact case-sensitive printable ASCII `sub` string, one to 255 characters. It is never trimmed, folded, converted to a number or replaced by a name or email. Signed ID tokens may use either documented Google issuer spelling; verified identity is stored with canonical issuer `https://accounts.google.com`. The browser callback independently requires that exact canonical response `iss` value. This conservative callback rule is separate from JWT issuer compatibility.

Google's [OpenID Connect documentation](https://developers.google.com/identity/openid-connect/openid-connect) and [discovery metadata](https://accounts.google.com/.well-known/openid-configuration) define the provider profile. Enterprise OIDC discovery and its verified issuer/sub contract remain unchanged.

## Configuration and linking

Every management read or write requires both intrinsic administrator authority and current `registration.write`. Configuration and status changes require a reviewed strong If-Match and a reason. Saving never verifies or enables login. Explicit verification authenticates and links the administrator's Google identity; enablement remains a separate confirmation. Member linking and unlinking require the exact current Session, local password, any required native MFA, a reviewed ETag and a reason.

Security configuration changes disable and reset only Google verification, remove its bindings and revoke its Sessions, challenges and pending ceremonies. Name-only changes preserve authority. Disabling retains bindings while revoking Google authority; unlinking affects the exact binding. GitHub and enterprise authentication remain independent. Google does not satisfy the approved OIDC/LDAP-only enforced-SSO policy.

## Browser and runtime flow

The dedicated callback URL is `/api/v1/auth/google/callback`. The authentication settings drawer displays the actual origin and saved callback for copying, alongside the separate Google configuration and enablement controls. It uses the existing sign-in, administration and Account Security composition.

The host-only `__Host-routex_google` cookie is Secure, HttpOnly, SameSite Lax, Path=/, with no Domain. Duplicate exact cookies fail closed. Query input is bounded before parsing; duplicate authority fields are rejected while bounded non-authority provider extensions are ignored. Cookie and callback query secrets are removed before downstream observation. No provider decoration grants authority.

Callback exchange and signed identity validation happen once and stage a durable verified ceremony. The callback never creates a Session and redirects to the clean `/auth/google/complete` page. Explicit Continue reads a fresh Session and uses current authority and CSRF when required. Login never silently replaces an existing Session. Native MFA remains HTTP 202 until proof succeeds. Return clears browser correlation only and does not assert durable cancellation.

Ceremonies expire within five minutes, with shared bounded admission and pruning. One ten-second operation budget and one shared five-second local admission budget cover the flow. No database lock spans remote HTTP or password work. Exact provider/profile, binding birth, member birth and configuration/policy revisions fence Sessions and MFA challenges. Proofs, credentials, tokens and enrollment material remain transient and outside browser storage and query/mutation caches.

## Persistence and qualification

Frozen GORM migration V99 admits the correlated Google profile tuple and seeds an empty disabled, unverified configuration. Released V1–V97 migration definitions remain unchanged. V98 and V99 also enforce exact ceremony purposes (`login`, `bind`, `verify`) and states (`pending`, `exchanging`, `verified`, `consumed`, `failed`) through portable GORM constraints. The existing three named-identity tables and seven Session/MFA provenance fields are reused. Root inventory V7 retains eleven domains and covers both admitted named profiles; historical V6 remains its original eleven-domain GitHub inventory. Older jobs or verification receipts never acquire Google coverage.

Controlled application qualification passes. The confirmed mandatory, complete Task, original dual-database integration and separate authentication-lifecycle results are recorded in [Implementation](IMPLEMENTATION.md). Earlier failed runs remain failed history. Controlled SAN-verified TLS, signed ID-token, native MFA, offboarding and process-restart fixtures do not prove interoperability with a registered external Google OAuth client. Bilingual browser and external-provider acceptance remain open. F03/A15 remain Partial; WeChat remains deferred without a service or wire contract, and enforced SSO remains limited to verified OIDC/LDAP.
