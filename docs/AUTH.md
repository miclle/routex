# Local Identity, Sessions, and Initialization

This phase implements first administrator creation, local login, persistent sessions, logout, and the minimum authorization boundary between `admin` and `member`. [Registration and role administration](GOVERNANCE.md) and [two-step verification](MFA.md) extend this foundation. Password recovery and enterprise identity providers remain separate work.

## Data and Initialization

- `users` uses ULIDs prefixed with `usr_`. Email addresses are trimmed and converted to lowercase, then protected by a database uniqueness constraint. The MySQL email column explicitly uses `utf8mb4_bin` to distinguish different normalized Unicode addresses, matching PostgreSQL behavior. This prevents the default accent-insensitive collation from confusing separate identities. Names must contain 1–100 Unicode characters after trimming.
- Passwords must be valid UTF-8 and contain 12–72 bytes. Only bcrypt hashes at the default cost are stored. Multibyte characters count toward the limit by their UTF-8 byte length.
- Initialization locks the singleton `installations` row with `id=1` inside a transaction. Creating the first administrator, marking initialization complete, and creating the session commit together. Only one concurrent initialization request succeeds; the others return `409`. Failed login attempts uniformly return `401`, without distinguishing an unknown email, an incorrect password, or a disabled account.
- `sessions` uses ULIDs prefixed with `ses_` and stores the SHA-256 digest of a bearer token generated from 32 cryptographically random bytes. Sessions expire after a fixed 7 days; reading a session does not renew it. Each authentication check reads the current user role and disabled status. Logout deletes the session.
- Initialization is currently the only user creation endpoint. Tests against real databases verify that members cannot access administrator APIs. There is no public registration endpoint that bypasses initialization.

## HTTP Contract

Management APIs use `snake_case` JSON. Authentication responses include `Cache-Control: no-store`.

| Method and path | Input | Successful response | Access |
|---|---|---|---|
| `GET /api/v1/setup` | None | `200 {"initialized": false}` before initialization; `true` afterward | Public |
| `POST /api/v1/setup` | `{email,password,name}` | `201`, session response and a session cookie | Before initialization only |
| `POST /api/v1/auth/login` | `{email,password}` | `200` session and cookie, or `202` MFA challenge without a new session | Public |
| `GET /api/v1/auth/session` | Session cookie | `200`, session response | Authenticated |
| `POST /api/v1/auth/logout` | Session cookie and `X-CSRF-Token` | `204`, session revoked and cookie cleared | Authenticated |
| `GET /api/v1/admin/status` | Session cookie | `200 {"initialized": true}` | `admin` |

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
