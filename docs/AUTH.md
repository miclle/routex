# Local Identity, Sessions, and Initialization

This phase implements first administrator creation, local login, persistent sessions, logout, and the minimum authorization boundary between `admin` and `member`. [Registration and role administration](GOVERNANCE.md) and [two-step verification](MFA.md) extend this foundation. [Existing-member OIDC](OIDC.md) adds explicit external identity linking while preserving local authentication and native MFA. Password recovery and broader enterprise identity remain separate work.

## Data and Initialization

- `users` uses ULIDs prefixed with `usr_`. Email addresses are trimmed and converted to lowercase, then protected by a database uniqueness constraint. The MySQL email column explicitly uses `utf8mb4_bin` to distinguish different normalized Unicode addresses, matching PostgreSQL behavior. This prevents the default accent-insensitive collation from confusing separate identities. Names must contain 1–100 Unicode characters after trimming.
- Passwords must be valid UTF-8 and contain 12–72 bytes. Only bcrypt hashes at the default cost are stored. Multibyte characters count toward the limit by their UTF-8 byte length.
- Initialization locks the singleton `installations` row with `id=1` inside a transaction. Creating the first administrator, marking initialization complete, and creating the session commit together. Only one concurrent initialization request succeeds; the others return `409`. Failed login attempts uniformly return `401`, without distinguishing an unknown email, an incorrect password, or a disabled account.
- `sessions` uses ULIDs prefixed with `ses_` and stores the SHA-256 digest of a bearer token generated from 32 cryptographically random bytes. Sessions expire after a fixed 7 days; reading a session does not renew it. Each authentication check reads the current exact user identity, enabled/offboarding state and registration admission. Logout deletes the session.
- After initialization, administrators may create users and explicitly enable local self-registration. Registration requiring approval returns a pending outcome without authentication. Setup, administrator-created users and historical accounts do not acquire inferred approval applications. See [governance](GOVERNANCE.md) for the reviewed policy and decision contracts.

## HTTP Contract

Management APIs use `snake_case` JSON. Authentication responses include `Cache-Control: no-store`.

| Method and path | Input | Successful response | Access |
|---|---|---|---|
| `GET /api/v1/setup` | None | `200 {"initialized": false}` before initialization; `true` afterward | Public |
| `POST /api/v1/setup` | `{email,password,name}` | `201`, session response and a session cookie | Before initialization only |
| `POST /api/v1/auth/login` | `{email,password}` | `200` session and cookie, or `202` MFA challenge without a new session | Public |
| `GET /api/v1/auth/registration` | None | `200 {enabled,approval_required}` | Public |
| `POST /api/v1/auth/register` | `{email,password,name}` | `201` Session without approval, or `202 {kind:"approval_pending"}` without a Session/cookie | Public, initialized and registration open |
| `GET /api/v1/auth/session` | Session cookie | `200`, session response | Authenticated |
| `POST /api/v1/auth/logout` | Session cookie and `X-CSRF-Token` | `204`, session revoked and cookie cleared | Authenticated |
| `GET /api/v1/admin/status` | Session cookie | `200 {"initialized": true}` | `admin` |

A registration HTTP202 and an MFA login HTTP202 are distinct transient outcomes; neither is a Session or authorizes workspace navigation. Pending, rejected and invalid approval links produce generic authentication denial. After approval, a fresh ordinary login still needs the normal password/MFA flow. Approval does not issue or restore Sessions, Keys or model grants.

Session responses contain only public profile fields and a CSRF token:

```json
{
  "user": {
    "id": "usr_01...",
    "email": "admin@example.com",
    "name": "Administrator",
    "role": "admin"
  },
  "csrf_token": "..."
}
```

Errors use the shape `{ "code": 401, "message": "unauthorized" }`. Binding errors and invalid input return `400`; missing, invalid, expired, or revoked sessions return `401`; insufficient administrator privileges or invalid CSRF/Origin checks return `403`; repeated initialization returns `409`; database failures return a sanitized `500`. Responses never expose underlying SQL, passwords, or detailed binding errors.

Initialization and login accept only `application/json`, with a maximum request body size of 4 KiB. Automatic DTO rendering in fox v0.1.2 always uses `200`, so initialization explicitly renders its typed DTO to preserve `201`. MFA login challenges explicitly render `202`; new MFA proof payloads use strict single-object decoding. Other JSON routes use fox request binding and return-value rendering.

## Cookies, CSRF, and Deployment Boundaries

The `routex_session` cookie uses `HttpOnly`, `SameSite=Strict`, `Path=/`, and a 7-day expiration. Requests received over TLS also set `Secure`. The bearer token is never included in JSON or stored in localStorage by the frontend. The CSRF token is derived from the session bearer using SHA-256 with a separate prefix and can be recovered through the session endpoint. The server compares CSRF tokens in constant time.

Initialization, login, and logout require a browser's `Origin` to match the current request scheme and Host. They also reject `Sec-Fetch-Site: cross-site` and `same-site`. Command-line clients may omit Origin; logout still requires a CSRF token. Future mutation endpoints authenticated by a session must apply the corresponding authorization and CSRF protections.

Local development supports same-origin HTTP and Vite proxying that preserves the original Host. `X-Forwarded-*` headers are not trusted: client-supplied headers cannot enable secure cookies or relax Origin validation. Production deployments that terminate HTTPS at a reverse proxy still require an explicit trusted-proxy and external-origin configuration. Authentication support for that topology is not considered verified until that configuration is implemented. Comprehensive login abuse throttling and session cleanup jobs are also pending requirements for a public-facing release.

Authentication database queries disable interpolated SQL logging to avoid exposing password hashes, session digests, and personal information. Logs must not record request bodies, cookies, or Authorization headers.

## Versioned Migrations

`database.Migrate` acquires a PostgreSQL advisory lock or MySQL named lock on a dedicated database connection, then applies immutable migration functions in `schema_migrations` order. Versions 1–4 preserve released SQL; new versions use frozen schema definitions and GORM Migrator APIs. Each successful version records its number and UTC timestamp. Startup no longer runs AutoMigrate against the current business entity definitions.

| Version | Purpose |
|---|---|
| 1 | Preserve or create the scaffold's `examples` table without deleting existing data |
| 2 | Create `users`, `sessions`, and `installations`; insert the initialization lock row; add the session-to-user foreign key and indexes on user ID and expiration |

Initial table creation uses repeatable DDL. Because MySQL DDL commits implicitly, an interrupted migration may leave some tables in place; a retry completes the version that has not yet been recorded. Unknown higher versions or invalid version numbers prevent an older binary from starting. Lock release uses a separate timeout. If release fails, migration reports the error and discards the physical connection so that a potentially locked connection cannot return to the pool. Future migrations must add versions instead of modifying steps that have already shipped. Downgrade migrations are not currently available; back up before upgrading, and recover using a matching application version and a complete database backup.

## Verification

```bash
# Unit tests and HTTP security middleware tests.
# Database integration cases explicitly skip when no DSN is provided.
go test -tags development ./internal/routex/...

# Start isolated, disposable PostgreSQL/MySQL databases through Compose.
go tool task test-integration
```

Database tests read `ROUTEX_TEST_POSTGRES_DSN` and `ROUTEX_TEST_MYSQL_DSN` and require the database name `routex_test`. They remove this phase's tables from that database, so these variables must point only to dedicated test databases. A single test package owns the shared database lifecycle to avoid cleanup conflicts between parallel packages.

Coverage includes upgrades preserving existing `Example` data, concurrent and repeated migrations, rejection of future schema versions, foreign keys and indexes, concurrent initialization creating only one administrator, field validation, password hashing, bearer digests, sanitized errors, failed logins, persistent sessions, fixed expiration, logout revocation, member authorization failures, disabled accounts, CSRF, Origin, and TLS cookie behavior. The `test-auth-lifecycle` task separately verifies persistence across application process restarts. See the [implementation record](IMPLEMENTATION.md) for phase-level evidence and remaining scope.


## Existing-member OpenID Connect

The OIDC integration adds explicit member linking and one reviewed provider, while
preserving local passwords, registration admission and native RouteX MFA. Read
[OIDC](OIDC.md) for ceremony, configuration, revocation and verification boundaries.
No remote email/role claim grants local authority. Callback exchange alone never
creates a Session; clean-page completion and any required MFA must succeed first.
The application is delivered in `62c01baf77278b67a774d5777ee218c3a14c5ac6`.
Complete PostgreSQL/MySQL and real-process lifecycle acceptance pass. Bilingual
browser and external-provider acceptance remain pending; F03/A15 remain Partial.

## Existing-member custom OAuth

The separate [custom OAuth application](OAUTH.md) preserves local and OIDC
authentication. It uses explicit endpoints, exact typed profile subjects,
reviewed existing-member binding, a separate correlation cookie and clean manual
completion, native MFA and independently revocable primary provenance. It adds no
provisioning, email linking, provider presets or enforced SSO. Complete Task,
the 188-scenario PostgreSQL/MySQL matrix and real-process authentication lifecycle
pass. Browser and external-provider acceptance remain open; F03/A15 stay Partial.

## Existing-member LDAP

The separate [LDAP integration](LDAP.md) authenticates an explicitly selected
immutable directory identity for an already admitted and explicitly linked member.
A verified service bind/search/user bind/reread does not itself grant a Session.
Local-password and native-MFA proofs protect linking, verification and unlinking;
LDAP login uses the existing native challenge when MFA is enabled. Security tuple
changes revoke only LDAP authority and remove its bindings; disabling retains
bindings but revokes its Sessions/challenges. Local, OIDC and OAuth remain
independent. V96 and root inventory V5 are additive. Complete PostgreSQL/MySQL
and real-process authentication lifecycle qualification pass for the delivered
LDAP application. Real-directory and bilingual browser acceptance remain open.

## Existing-member SAML application

The [SAML application](SAML.md) adds explicit existing-member linking and reviewed
public trust configuration to the independently qualified protocol component.
SP-initiated requests and signed persistent NameIDs are correlated through durable
one-use ceremonies and separate exact host-only start and delivery cookies. A
cross-site ACS POST only stages proof and redirects to a clean page; explicit
same-origin completion requires both proofs and current authority. Native MFA
remains required where enabled, and SAML Sessions/challenges carry exact binding,
member-birth, configuration and policy provenance.

Security configuration changes reset verification, disable admission, remove
bindings and revoke SAML authority; name-only edits preserve it. Disable retains
bindings, and explicit password/MFA-reviewed unlink revokes one binding. Local,
OIDC, OAuth and LDAP remain independent. No provisioning, email/group mapping,
metadata discovery or remote logout is added. SAML remains outside the approved
OIDC/LDAP-only enforced-SSO allowlist. Controlled application, PostgreSQL/MySQL
and real-process restart qualification pass. Bilingual browser and external IdP
acceptance remain open; controlled results do not establish those outcomes.
F03/A15 remain Partial.

## Named login profiles

Independent Google, GitHub, Discord and Telegram settings are separate remaining
work; the single custom OAuth provider does not implement those independent
profiles. WeChat login is deferred by the user because no verification service
or wire contract currently exists. Do not invent a bridge protocol or treat a
static QR image as proof of identity.
