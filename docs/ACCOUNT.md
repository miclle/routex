# Account and Session Security

Authenticated users can update their display name, change their local password, and inspect or revoke their own active sessions. Email and platform-role assignment are outside the profile update contract.

## API

All routes use `/api/v1`, an authenticated browser session, and no-store response headers. Writes require same-origin requests and the current `X-CSRF-Token`.

| Method | Route | Input / response |
|---|---|---|
| PATCH | `/account` | `{name}` → safe user DTO |
| POST | `/account/password` | `{current_password,new_password}` → replacement Session/cookie, or committed `200 {reauthentication_required:true}` with cookie expiry for an emergency caller |
| GET | `/account/sessions` | `{items:[{id,created_at,expires_at,current}]}` |
| DELETE | `/account/sessions/:session_id` | Revoke owned session; 204; unknown or another user's session returns 404 |

Names must contain 1–100 Unicode characters after trimming. Passwords contain 12–72 UTF-8 bytes. Password changes require the current password and a different new password. An incorrect current password returns 400 without expiring the current session.

A password change acquires governance, locks the user and rechecks the exact authenticated Session and previously verified password hash before replacing the hash, deleting prior authority and writing its secret-free audit in one transaction. Local replacement remains supported while enforcement is off. A valid nonlocal replacement keeps every original primary field and the original absolute expiry. An emergency caller instead receives a committed reauthentication receipt with no replacement Session; the successful security change must not be reported as a rejected login. Clients immediately clear private Session/query state and offer explicit sign-in. Login rechecks current policy, user birth and password after bcrypt, preventing stale verification from recreating authority. These source-implemented [policy/security transitions](AUTH_POLICY.md) await controlled qualification and browser acceptance. API Keys have an independent lifecycle and are not silently transferred or changed.

Session lists exclude bearer digests and expired records. Revoking the current session also clears its cookie; the UI clears private cached data and returns to login. Profile and session mutations write audit events in their transaction.

## Verification

`testAccountLifecycle` in the shared PostgreSQL/MySQL integration harness covers CSRF, name validation/persistence, safe session listing, cross-user revocation denial, individual revocation, incorrect password, cookie/CSRF rotation, invalidation of old sessions, and old/new password login behavior. Frontend tests cover password confirmation, transient form cleanup, session query refresh, and current-session logout.

[Two-step verification and recovery codes](MFA.md) extend this account flow with encrypted TOTP enrollment and session rotation. Password changes invalidate pending MFA challenges while retaining the enrolled factor. [Administrative offboarding](OFFBOARDING.md) has a separate transactional lifecycle. Explicit external identity bindings retain their independent provider lifecycles; [authentication policy](AUTH_POLICY.md) separately restricts which browser primary methods may remain authorized.
