# Member Offboarding

## Inventory and Permissions

`GET /api/v1/admin/members/:user_id/offboarding` requires `members.read`. It returns masked personal-key metadata, Projects currently managed by the member, Teams where the member is an owner, current responsibility relationships, last-administrator protection, an `inventory_version`, and up to 100 recent case receipts. Completed cases remain persisted even after account reactivation. Key values, token hashes, password hashes, and reauthentication proofs are never returned or stored in cases.

Planned handover requires `members.write`. A custom role cannot offboard a platform administrator, and these administration actions cannot target the acting member. Emergency disable requires an active platform administrator and fresh confirmation of that administrator's current password. The last active platform administrator remains protected.

## Planned Handover

Create a plan with `POST /api/v1/admin/members/:user_id/offboarding/plans`:

```json
{
  "request_id": "offboarding-request-unique-id",
  "inventory_version": "digest-from-the-current-inventory",
  "planned_at": "2026-10-01T09:00:00Z",
  "reason": "Planned departure",
  "project_assignments": [
    { "project_id": "prj_example", "manager_user_ids": ["usr_successor"] }
  ],
  "team_assignments": [
    {
      "team_id": "tem_example",
      "owner_user_ids": ["usr_successor"],
      "add_member_user_ids": ["usr_successor"]
    }
  ]
}
```

A Project that has another enabled manager needs no replacement assignment. A Team that has another enabled, active owner also needs no replacement. Otherwise a valid successor is required before the plan can become `ready_to_complete`. Team successors must already be active members, or the request must explicitly include them in `add_member_user_ids`. Archived resources retain their archived state and do not require a new active manager or owner.

Preparing a plan changes no account, key, membership, management, or inference policy. `planned_at` records the intended date; it does not schedule automatic execution. Complete a reviewed plan explicitly with `POST /api/v1/admin/members/:user_id/offboarding/:case_id/complete`.

Completion reloads and validates current responsibilities under the governance lock. A changed inventory or unavailable successor rejects the operation. The administrator must review a fresh inventory and create a new plan instead of applying a stale replacement list. Session activity does not invalidate a plan; completion revokes all sessions present at execution time.

One transaction adds and validates successors, removes the departing member from Project management and all Team memberships, revokes all personal keys including pending deliveries, removes direct role assignments and resets the base role to `member`, deletes browser sessions, disables the member, sets `offboarded_at`, writes audits, and completes the case. Project ownership, creator metadata, model grants, Project keys, resource state, and historical calls remain unchanged. Rotation of Project keys is a separate deliberate action.

## Emergency Disable

`POST /api/v1/admin/members/:user_id/offboarding/emergency` accepts `request_id`, `current_password`, `reason`, and optional `team_assignments` using the same Team shape as a planned handover. The password proof is checked before the transaction and its hash is checked again under the account lock, so a concurrent password change invalidates the proof.

Projects with another enabled manager retain that manager. A sole-managed non-archived Project receives the acting administrator as interim manager before the departing member is removed. Teams with another enabled owner retain that owner. For a sole-owner Team, the service promotes an enabled active member, selected deterministically by user ID. If no such member exists, the administrator must explicitly select an enabled successor and acknowledge adding that person to the Team. A missing successor rejects the whole transaction; emergency handling cannot silently leave an unmanaged resource.

Emergency completion uses the same atomic revocation, relationship removal, audit, and account-state changes as planned completion. It preserves Project keys and history. Identifying compromised Project credentials and explicitly rotating or revoking those keys remains a separate incident-response action; creator identity alone does not establish compromise or inference ownership.

## Idempotency, Publication, and Reactivation

A creation or emergency `request_id` binds its actor, target member, operation, and normalized payload. Reusing it for a different action returns a conflict. Repeating the same plan request returns the original receipt, and concurrent completion of one case produces one completion audit. The emergency password proof is checked again on retries and is excluded from persisted idempotency data.

Mutations take the governance lock and user locks in stable order. Read-committed transactions ensure completion sees personal keys committed before it acquires the departing account lock. Key issuance takes that account lock too, preventing a key from being issued across account disable. After commit, runtime user and affected-project tombstones are installed and authorization is republished before a successful response. Publication failure returns 503 after the database transaction has committed; retry the same case or request ID. Browser session deletion is already part of the database transaction, and the runtime's background publisher continues retrying publication.

An ordinary suspended member has no `offboarded_at` timestamp. Completed planned or emergency offboarding sets it. Explicit authorized account enable clears it and emits `member.reactivate`. This does not restore deleted Team memberships, Project manager relationships, direct roles, old sessions, or revoked personal keys. A former administrator returns as a member unless a separate authorized role assignment explicitly grants administrator access. Historical completed cases remain unchanged. Existing resource configuration is not rolled back, and any new relationship or grant requires its normal explicit management action.

## Verification and Boundaries

Focused unit tests cover successor validation, deterministic emergency continuity, and exclusion of password proofs from persisted request digests. The isolated database helper covers read-only plans, stale inventory and disabled-successor rejection, concurrent completion, runtime personal-key revocation, preserved Project keys, explicit reactivation, emergency reauthentication, and Team/Project continuity on PostgreSQL and MySQL.

```bash
go test -race -tags development ./internal/routex/service ./internal/routex/handler
go tool task test-integration
```

This implementation revokes local sessions. External identity-provider sessions, external credential access profiles, notification delivery, scheduled execution, and a durable notification outbox are not implemented by this workflow. The control-plane transaction and local runtime publication do not establish multi-node or external-identity revocation guarantees.

## Web workflow

The member offboarding page presents personal access revocation separately from preserved Project assets. It requires `members.read`; actionable controls additionally enforce `members.write`, self-target and last-administrator protection, and the platform-administrator requirement for administrator targets and emergency handling.

The planned flow searches enabled members with pagination, allows multiple successors, and requires an explicit membership acknowledgment for Team successors who are not already active members. Saving a reviewed plan does not execute it. A separate confirmation applies the plan. The intended date is informational and never schedules execution. Stale inventories return a review-and-refresh action that clears old assignments before a new plan is prepared.

Emergency handling explains interim Project management and Team continuity. Its password input is cleared before dispatch and removed on dismissal. Reauthentication runs through a direct API call; neither password-bearing variables nor Axios request/error objects enter the query or mutation cache. A rejected password refreshes console session state without treating the password failure itself as proof that the session expired.

Uncertain transport or publication failures retain the same request identifier and reviewed intent for an explicit retry. The UI never retries mutations automatically. Successful completion refreshes member status and history and reports that personal access was revoked while Project keys and history were preserved. All workflow copy is available in English and Chinese.
