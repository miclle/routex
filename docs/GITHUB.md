# GitHub login

RouteX supports a separate, initially disabled GitHub.com OAuth App profile for
explicitly linked existing members. It does not create members, match email
addresses, import organizations or grant roles. The administrator configures a
name, client ID, transient client secret and exact HTTPS callback URL. The fixed
profile uses GitHub.com authorization and token endpoints and the authenticated
GitHub REST user endpoint; it offers no endpoint, scope or claim-mapping editor.

## Configuration and identity

Management requires a current intrinsic administrator with `registration.write`.
Configuration and status changes require a reviewed strong If-Match and a reason.
Saving does not verify or enable the profile. Explicit verification authenticates
and links the administrator's GitHub identity; enabling is a separate action.
Member linking and unlinking require the exact current Session, local password,
any required native MFA proof, a reviewed ETag and a reason. Identity is the exact
GitHub numeric user ID, not a mutable login name or email address.

Security configuration changes disable and reset verification, remove GitHub
bindings and revoke its Sessions, challenges and pending ceremonies. A name-only
change preserves them. Disabling retains bindings but revokes GitHub authority;
unlinking revokes only the exact binding. Other authentication methods remain
independent. GitHub is outside the approved OIDC/LDAP enforced-SSO allowlist.

## Browser and runtime flow

Authorization uses state and S256 PKCE, with one host-only
`__Host-routex_github` correlation cookie: Secure, HttpOnly, SameSite Lax, Path=/,
without Domain. Exact duplicate cookie names fail closed. Callback exchange and
profile reading occur once and stage a durable verified ceremony. They never
issue a Session. The callback redirects to the fixed clean
`/auth/github/complete` page; explicit Continue reads the current Session and
completes with current authority and CSRF where required. Login cannot silently
replace a current Session. Native MFA returns HTTP 202 until its proof succeeds.
Return clears only the browser cookie and makes no durable cancellation claim.

Sessions and MFA challenges retain exact provider/profile, binding birth, member
birth and configuration/policy revisions. Every subsequent authority check uses
current facts. Pending ceremonies expire within five minutes, with bounded row
admission and cleanup. One original ten-second operation budget and a shared
five-second local admission budget bound the flow; no database lock spans remote
HTTP or password work. Remote tokens, correlation proofs and client secrets never
enter browser storage or query/mutation caches. Audit changes contain typed
reason-only metadata.

Frozen GORM migration V98 adds the named identity domain without rewriting older
migrations or enterprise provider identities. Root inventory V6 adds encrypted
named-provider credentials with profile/provider/generation-bound AAD while
preserving legacy inventory prefixes. No remote logout, automatic provisioning,
GitHub Enterprise, GitHub Apps, device flow, refresh-token workflow or automatic
callback retry is included.

## Verification status

Source implementation is composed; controlled application, database and restart
qualification is `passed`. The root-confirmed combined GitHub
and Google results are recorded in [Implementation](IMPLEMENTATION.md). Earlier
failed runs remain failed history. Controlled SAN-verified TLS fixtures exercise
the fixed endpoints without real GitHub credentials; they do not establish
interoperability with a registered GitHub OAuth App. Bilingual browser and
external-provider acceptance remain open.
