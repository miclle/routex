# Authentication policy and emergency sign-in

The implementation adds an optional OIDC/LDAP-only sign-in policy and a separate
administrator emergency entry. Controlled PostgreSQL/MySQL and process acceptance
is recorded by `54b4b3fb5c51725c4387837d1f24f111beeabc763dab1294584b9da16db4ab4f` and
`ae8c8b73d503494fdf1c4a07ebb31d098e4242d8881063d5a96df2497ebaf193`. Browser acceptance remains deferred because the user
cannot unlock the screen; external identity-provider recovery is unproven.
See [authentication](AUTH.md), [MFA](MFA.md) and [governance](GOVERNANCE.md) for the
existing contracts that this policy extends.

## Enforcement and retained authority

When enforcement is off, the existing local and independent identity methods
remain available under their own configuration, explicit member bindings and
native MFA rules. When enforcement is on, ordinary sign-in accepts only the
verified, enabled enterprise OIDC and LDAP integrations. Custom OAuth, SAML,
GitHub, Google and Discord do not satisfy this allowlist. No provider claims,
remote email or external role automatically provision a member or grant local
permissions.

Enabling requires an active intrinsic administrator with enrolled native MFA as
the designated emergency administrator, and at least one currently enabled,
verified OIDC or LDAP configuration. Configuration availability is a stored
security fact, not an external health check. Enabling closes local registration
in the same transaction and advances its revision. Disabling enforcement leaves
registration closed; an independently authorized registration change is needed
to reopen it later. Administrator-created members retain their normal governance
contract.

A changed policy immediately invalidates excluded local/nonallowlisted Sessions
and pending factors. Active excluded login ceremonies are terminalized so a later
disable cannot revive an old response. Valid OIDC/LDAP proof remains subject to
its existing member, binding, configuration, policy and native-MFA checks.
Authenticated linking and verification retain their own exact Session authority.
Personal/Project API Key inference, quotas and recorded native-call attribution
are independent of this browser sign-in policy.

Provider edits, disablement or outages and member suspension/offboarding keep
their existing lifecycle rules; this policy does not add a last-provider or
broad offboarding prohibition. The active designated emergency administrator
cannot disable MFA until the designation is replaced or enforcement is disabled.
Existing last-administrator and resource-continuity checks still apply.

## Reviewed administration

All paths below include `/api/v1`.

| Method and path | Contract |
| --- | --- |
| GET `/auth/policy` | Anonymous `{enforced,password_login_enabled,emergency_entry_enabled}`. Password is enabled exactly when enforcement is off; the generic emergency entry is enabled exactly when enforcement is on. No designation or factor details are public. |
| GET `/admin/auth/policy` | Current exact Session, intrinsic administrator and independent `registration.write`. Optional single `emergency_admin_id` selects one canonical existing administrator for review. |
| PUT `/admin/auth/policy` | The same authority plus same-origin JSON, CSRF, strong quoted If-Match and `{enforced,emergency_admin_id,reason}`. ID is exactly empty when disabling. |
| POST `/auth/emergency/login` | Anonymous same-origin `{email,password}`; success is native MFA202 with no Session. An already valid Session is rejected. |
| POST `/auth/emergency/mfa/verify` | Anonymous same-origin `{challenge_token,code}` or `{challenge_token,recovery_code}`; Session200 only after actual one-time factor completion. |

The management view contains enforcement, stored designation ID/birth and its
recorded MFA availability, current OIDC/LDAP availability, nullable
`review_emergency_admin:{id,created_at,mfa_enabled}` and `review_etag`. The matching
ETag header is quoted. No-query GET always has a null review candidate, even when
enforcement is on; stored designation facts remain separate. Enable/replacement
requires a fresh exact-ID candidate review. Disable requires a fresh no-query
review. The client cannot convert either ETag into the other scope.

The review binds the current policy and registration state, selected and stored
administrator births/MFA generations and relevant provider verification/security
revisions. PUT rechecks the same facts under governance before accepting even a
no-op. Candidate selection alone changes nothing. The existing authentication
card/configuration drawer preserves explicit review, reason and confirmation;
an ungranted global member directory is not fetched.

Successful PUT returns the safe view with `confirmation:"saved"`,
`runtime_applied` and `reauthentication_required`. Enabling from a local Session
can revoke that same caller. This is a committed200 receipt with an expired
cookie, not an authentication rejection. Consume the receipt, clear private
Session/query state and explicitly sign in again; do not require a now-forbidden
protected reread. Runtime publication is independent of the durable save. Keep
an uncertain original body/ETag for explicit retry and review a conflict before
changing intent; current GET facts do not prove a historical PUT succeeded.

## Actual emergency proof and security changes

Emergency sign-in is a separate local password check followed by the actual
native TOTP or an unused recovery code. Password success always returns202 and
never issues a Session. The five-minute challenge is purpose-separated from
ordinary login; neither factor endpoint consumes the other's challenge. Public
rejections do not reveal which administrator is designated or whether MFA exists.

Completion rechecks the exact current policy revision, administrator ID/birth,
admission and intrinsic role, password commitment, canonical native MFA generation
and unexpired challenge. Factor consumption, challenge deletion and Session
creation commit together. Existing native attempt/lockout and replay protections
remain active. The emergency Session records the real factor completion instant
and has the existing fixed seven-day lifetime; no legacy Session acquires
retroactive factor evidence.

Authenticated security changes recheck the exact original Session before proof
consumption. A valid nonlocal Session replacement preserves all original primary
provenance and its original absolute expiry, including when enforcement is off.
It cannot turn into blank local authority or silently renew its lifetime.
Ordinary local replacement behavior remains available while enforcement is off.

Emergency password changes and recovery-code regeneration commit the requested
change, revoke prior authority and require fresh emergency authentication. The
password response is `{"reauthentication_required":true}` with an expired cookie.
Recovery regeneration returns `{"session":null,"recovery_codes":[...],
"reauthentication_required":true}`. Keep its ten one-time codes transiently for
explicit review before returning to sign-in. No response-loss recovery can return
those plaintext codes again; do not automatically replay a password/factor
operation. The public `/auth/reauthenticate` UI is a transient receipt surface,
not a backend route, navigation-state proof or persistent storage location.

## Runtime and persistence limits

Governance precedes user, exact Session and MFA locking. One parent-shortened
operation admission supplies at most10s overall and an original local5s deadline
shared by every database phase; returning from bcrypt or provider I/O cannot
renew it. Canceled/stale operations cannot issue later authority.

Runtime initialization and refresh read the actual current policy; unavailable
or malformed state has no implicit enforcement-off fallback. A committed local
policy-generation deny fence checks Team Session admission and immediate
pre-dispatch even if runtime publication fails and an older snapshot remains
leased. Refresh cannot overwrite a newer fence. Independently valid OIDC/LDAP
remain governed by their existing proofs and bounded leases. `runtime_applied`
is local publication evidence, never a fleet acknowledgement.

Frozen GORM V103 follows V102 and adds policy/designation state and five private
emergency fields to Sessions and native MFA challenges. Existing rows receive
off-policy/empty-proof defaults; no emergency factor completion is backfilled.
Released V1–V102 definitions remain unchanged. Portable retained-state checks and
partial-DDL recovery must pass on both supported databases before acceptance.
No encrypted field or root-inventory domain is added: the preceding V9 inventory
retains its twelve-domain meaning.

The installed authorization serializer uses projection2 with domain
`routex.gateway-authorization.v2`, including exact policy and Team Session
emergency evidence. Historical projection1 observations remain immutable, and
their current-match value is null/unknown rather than a comparison across digest
domains. The existing single-process scope and shared250ms best-effort observation
budget remain unchanged. Installation observation is separate from authentication
or command completion and cannot weaken admission on evidence failure.

Controlled TLS, real-driver and same-binary process scenarios pass with their
original ownership, deadline and cleanup guards in the acceptance receipts above.
These local results do not establish browser language/lifetime flows or external
recovery drills. F03/A15 remain Partial and capability totals are unchanged.
