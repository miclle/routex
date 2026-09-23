# Account and Session Security

Authenticated users can update their display name, change their local password, and inspect or revoke their own active sessions. Email and platform-role assignment are outside the profile update contract.

## API

All routes use `/api/v1`, an authenticated browser session, and no-store response headers. Writes require same-origin requests and the current `X-CSRF-Token`.

| Method | Route | Input / response |
|---|---|---|
| PATCH | `/account` | `{name}` → safe user DTO |
| POST | `/account/password` | `{current_password,new_password}` → fresh session cookie and session DTO |
| GET | `/account/sessions` | `{items:[{id,created_at,expires_at,current}]}` |
| DELETE | `/account/sessions/:session_id` | Revoke owned session; 204; unknown or another user's session returns 404 |

Names must contain 1–100 Unicode characters after trimming. Passwords contain 12–72 UTF-8 bytes. Password changes require the current password and a different new password. An incorrect current password returns 400 without expiring the current session.

A password change locks the user, rechecks the previously verified password hash, replaces the hash, deletes every prior session, creates a fresh session, and writes a secret-free audit event in one transaction. Login uses the same user lock and rechecks the hash before creating a session, preventing a stale password verification from recreating a session after the change. API Keys have an independent lifecycle and are not silently transferred or changed.

Session lists exclude bearer digests and expired records. Revoking the current session also clears its cookie; the UI clears private cached data and returns to login. Profile and session mutations write audit events in their transaction.

## Verification

`testAccountLifecycle` in the shared PostgreSQL/MySQL integration harness covers CSRF, name validation/persistence, safe session listing, cross-user revocation denial, individual revocation, incorrect password, cookie/CSRF rotation, invalidation of old sessions, and old/new password login behavior. Frontend tests cover password confirmation, transient form cleanup, session query refresh, and current-session logout.

[Two-step verification and recovery codes](MFA.md) extend this account flow with encrypted TOTP enrollment and session rotation. Password changes invalidate pending MFA challenges while retaining the enrolled factor. [Administrative offboarding](OFFBOARDING.md) has a separate transactional lifecycle. External identity bindings remain separate work.
