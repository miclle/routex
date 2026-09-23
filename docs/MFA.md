# Two-step verification

Local accounts can enroll an authenticator app and receive ten single-use recovery codes. MFA uses six-digit HMAC-SHA1 TOTP with a 30-second step, following [RFC 6238](https://www.rfc-editor.org/rfc/rfc6238) and the [RFC 4226](https://www.rfc-editor.org/rfc/rfc4226) truncation algorithm. The verifier accepts the current step and one adjacent step in either direction, then persists the greatest accepted step. A successful code cannot be reused for another login or security mutation, even within its displayed validity period. Accurate server time is required.

MFA-enabled accounts never receive a new session from a password-only login. Password checks produce an expiring challenge; a valid TOTP or unused recovery code must complete that challenge before the usual session cookie is issued. Existing non-MFA login responses remain unchanged.

## HTTP contract

Routes use the `/api/v1` prefix. Every response has `Cache-Control: no-store`. Login and verification require same-origin JSON requests, including the existing Fetch Metadata checks; verification does not require a session or CSRF token. Account routes require a current session, and every account mutation additionally requires the current `X-CSRF-Token` and same-origin checks. New MFA JSON payloads must contain one non-null object with known fields; unknown fields, trailing JSON, malformed values, and bodies over 4 KiB return a sanitized `400` before any proof transition.

| Method and route                   | Request                                                         | Success                                                                                                                       |
| ---------------------------------- | --------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| `POST /auth/login`                 | `{email,password}`                                              | Non-MFA: existing `200` session response and cookie. MFA: `202` challenge response, with no new session cookie or CSRF token. |
| `POST /auth/mfa/verify`            | `{challenge_token,code}` or `{challenge_token,recovery_code}`   | `200` existing session response and cookie.                                                                                   |
| `GET /account/mfa`                 | None                                                            | `{enabled,enrollment_available,enrollment_pending,recovery_codes_remaining}`.                                                 |
| `POST /account/mfa/enrollment`     | `{current_password}`                                            | `{secret,otpauth_uri,enrollment_token,expires_at}`. A new pending enrollment replaces the previous one.                       |
| `DELETE /account/mfa/enrollment`   | None                                                            | `204`; discard pending enrollment. An enabled factor is not canceled by this route.                                           |
| `POST /account/mfa/enable`         | `{current_password,enrollment_token,code}`                      | `{session,recovery_codes}` and a rotated session cookie.                                                                      |
| `POST /account/mfa/recovery-codes` | `{current_password,code}` or `{current_password,recovery_code}` | `{session,recovery_codes}` and a rotated session cookie. The old code set is destroyed.                                       |
| `POST /account/mfa/disable`        | `{current_password,code}` or `{current_password,recovery_code}` | Existing session response and a rotated cookie.                                                                               |

A login challenge has this shape:

```json
{
  "mfa_required": true,
  "challenge_token": "opaque transient bearer",
  "expires_at": "2026-09-23T12:05:00Z",
  "methods": ["totp", "recovery_code"]
}
```

Enrollment and login challenges expire after five minutes. Only the newest challenge of each purpose remains usable for an account. Enrollment confirmation also requires the initiating session and rechecks the current password. Enrollment itself does not enable MFA; confirmation requires a valid code from the new authenticator secret.

`secret` is an unpadded Base32 value. `otpauth_uri` contains issuer `RouteX`, the account email as the label, `algorithm=SHA1`, `digits=6`, and `period=30`; the client can render it locally as a QR code. Provisioning must never call an external QR service. `enrollment_available` indicates whether the server has a configured encryption store, not that an arbitrary restored ciphertext can be decrypted.

Recovery codes use `RXR-XXXXXXXX-XXXXXXXX-XXXXXXXX-XXXXXXXX`, with 128 random bits each. Case is ignored and surrounding whitespace is trimmed. Plaintext is returned only on enablement or regeneration. There is no endpoint to display existing codes again. If the response is lost, authenticate with the enrolled app and regenerate a new set. Using a recovery code to regenerate or disable consumes it in the same transaction as that action.

## Session and lifecycle transitions

Enablement, recovery-code regeneration, and disablement revoke every prior browser session and pending MFA challenge, then issue one fresh session for the successful caller. Clients must replace their current session/CSRF state and clear private cached data accordingly. API Keys have an independent lifecycle and are not revoked or changed by MFA operations.

Password changes retain the enrolled factor but revoke pending challenges and prior sessions. Account disablement and completed offboarding delete challenges inside their existing locked transactions. Explicit reactivation never revives those challenges. A subsequent login needs a fresh password check and the retained factor. No clock timestamp is used as an account security revision.

The internal password-only `Service.Login` method also refuses MFA-enabled accounts. Interactive callers use `BeginLogin` followed by `CompleteMFALogin`; callers cannot silently downgrade to password-only sessions.

## Persistence and abuse limits

Frozen migration 16 adds `user_mfa`, `mfa_challenges`, and `mfa_recovery_codes` through GORM. The authentication query path suppresses interpolated SQL logging. The existing 32-byte root-key store encrypts each random 20-byte TOTP secret, binding the envelope to the account and a random enrollment generation. Missing or mismatched encryption keys make TOTP operations fail closed with a generic `503`. Recovery-code verification uses its independently stored digest and can still prove possession without decrypting the authenticator secret.

Challenge tokens contain 256 random bits and are stored only as SHA-256 digests. They bind the account's current password hash digest and MFA generation. Recovery-code digests are domain-separated by account and generation. Plain passwords, authenticator secrets, challenge bearers, OTP values, and recovery codes must never enter audit payloads, application logs, browser persistent storage, browser navigation URLs, external network requests, analytics, or client mutation/query caches. The provisioning URI is transient enrollment material rendered locally, never fetched as a network URL.

Every transition locks the active User first. It rechecks password state and, for account mutations, locks and checks the initiating session before updating factor/challenge/recovery rows. Successful proof consumption, session creation, and secret-free audit events commit together. Concurrent completion can succeed only once. Failed proofs commit their attempt counters before returning an error, rather than rolling them back with the rejection.

Five consecutive invalid proofs impose a fifteen-minute account-wide MFA lockout. Reissuing a login or enrollment challenge, restarting the process, or canceling an enrollment does not reset the failure budget. Each challenge also allows at most five failures. After the cooldown, a valid proof resets the counters. This is factor-verification throttling; it does not replace the broader password-login abuse controls needed for public-facing deployments.

Invalid passwords, codes, consumed/replaced/expired challenges, replayed TOTP steps, and lockout return generic `401`. Already-enabled enrollment or operations requiring a factor that is disabled return `409`. Encryption availability errors return generic `503`; SQL and cryptographic details are never returned. A failed second-factor proof does not create a new session. Existing authenticated-session lifecycle remains independent until a successful security mutation explicitly rotates it.

## Verification

Pure tests use the published HOTP/TOTP vectors, including times beyond 2038, and cover adjacent-step tolerance, persistent replay cutoffs, input formats, independent random secrets, encrypted account binding, provisioning URIs, and recovery-code digest domains.

The coordinated PostgreSQL/MySQL `testMFALifecycle` helper covers enrollment replacement and expiry, password rechecks, CSRF/Origin, encrypted secret storage, no session at the password-only step, TOTP replay across challenges, durable account lockout across fresh challenges, concurrent recovery consumption, secret-store mismatch, password/disable/reactivation/offboarding invalidation, recovery regeneration, and session rotation. It uses the shared isolated database lifecycle; no production database or external authenticator service is required.

## Web interface

The existing security page provides enrollment, disable, recovery-code regeneration, and pending-enrollment cancellation. Enrollment uses a locally generated scannable SVG QR and a manual secret, both from the authoritative server response. The QR library is pinned and its required notices are embedded with the application. The sign-in page treats HTTP 202 as an unauthenticated, expiring local challenge and supports either authenticator or recovery proof.

Sensitive operations use direct API requests with status-only errors, rather than React Query mutations. Challenge/proof/enrollment/recovery material remains in transient component state and is cleared on dismissal, expiration, completion, or unmount. Proof failures do not automatically expire the session; the client refreshes the actual session to distinguish a bad proof from revoked authentication. Successful factor changes replace Session/CSRF and reset private cached query data. Recovery plaintext is shown once only after a verified successful enable or regeneration.
