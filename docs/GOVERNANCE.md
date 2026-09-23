# Member Governance and Platform Permissions

This phase adds controlled public registration, administrator member management, protected built-in roles, additive custom roles, and database-backed platform authorization. Model call grants remain separate from platform permissions. Team and Project responsibilities are documented separately in [resource governance](RESOURCES.md). Complete employee offboarding and enterprise identity synchronization remain subsequent work packages.

## Registration and Member Lifecycle

Registration is disabled by default and persisted in the singleton `governance_settings` row. It cannot create users before installation initialization. Only an active platform administrator can change the switch. The public registration endpoint always creates a `member`, ignores any attempted role injection, grants no models, and atomically creates its initial browser session. It uses the existing normalized email, unique email constraint, 12–72 UTF-8 byte password policy, bcrypt hashing, and safe session response.

Administrators can list, search, inspect, and create members with an initial password. The create response never contains that password or its hash. Initial passwords are supplied explicitly by the administrator; there is no invitation or password-delivery workflow in this phase. Creating an administrator requires the protected platform administrator identity.

Member status and base-role changes are transactional. Before disabling or demoting an active administrator, the service verifies that another active administrator remains. A singleton policy lock serializes concurrent checks so two administrators cannot both remove the last remaining administrator.

Disabling an account deletes its browser sessions and revokes all of its personal API Keys in the same transaction. Subsequent login and gateway authentication reject the account. After commit, reductions install a runtime user tombstone before refreshing the immutable gateway state; a failed refresh cannot restore suspended access. User creation and registration also refresh runtime state before returning success. Reenabling it does not restore revoked Keys or old sessions. Existing direct model grants and custom-role assignments remain stored and apply only while the account is active. Before suspension, the same policy lock also verifies that every nonarchived Team owned by this user and every nonarchived Project managed by this user retains another active responsible user. A continuity conflict aborts the entire suspension, preserving sessions and Keys. Archived resources retain historical relationships and do not prevent suspension. This operation remains account suspension, not the complete future employee offboarding workflow.

## Roles and Authorization

`users.role` remains the persisted built-in identity: `admin` or `member`. Schema version 6 adds protected role metadata (`rol_admin`, `rol_member`), role-to-permission rows, and explicit user-to-custom-role bindings. A user's effective platform permission set is the union of their built-in role and all assigned custom roles. Built-in role definitions cannot be edited or deleted, and built-in IDs cannot be inserted through custom-role assignment APIs.

The implemented resource/action vocabulary is:

| Permission | Scope |
|---|---|
| `members.read` | Search and inspect platform members |
| `members.write` | Create and suspend ordinary members, subject to the restrictions below |
| `roles.read` | Read role definitions and permission metadata |
| `roles.write` | Protected platform administrator role management |
| `registration.write` | Protected platform administrator registration policy |
| `providers.read` / `providers.write` | Read or change provider configuration |
| `models.read_all` / `models.write` | Read or change the administrative model catalog |
| `calls.read_all` | Read calls across users through administrator DTOs |
| `audit.read` | Platform audit access boundary |
| `system.read` | Platform operational state access boundary |
| `teams.read_all` / `teams.write` / `teams.models.write` | Global Team reading, metadata/membership/lifecycle changes, and explicit model grants |
| `projects.read_all` / `projects.write` / `projects.models.write` | Global Project reading, metadata/manager/lifecycle changes, and explicit model grants |

`roles.write` and `registration.write` are reserved for the protected administrator identity. They cannot be assigned through custom roles. The API's `available_permissions` list contains only permissions that may be placed in a custom role; built-in administrator metadata additionally includes the reserved powers. Defining a permission boundary does not claim that every corresponding product page or operational capability has been implemented.

Custom roles are additive and confer only implemented platform actions. They do not grant model invocation, ownership of another user's Keys, Team ownership, Project management, or unrestricted access to personal endpoints. Unknown or duplicate permission values are rejected. A role still assigned to users cannot be deleted. Role replacement and assignment validate the whole request before committing, preventing partial changes.

A delegated member manager may create ordinary members and change other ordinary members' enabled state. They cannot create administrators, change any base role, modify an administrator, suspend themselves, create/edit/delete roles, assign roles, or change registration policy. These restrictions are enforced in the service transaction as well as at the HTTP boundary, so direct method use cannot turn a delegated permission into a privilege escalation.

`Service.Permissions` reads current persisted membership and role definitions. `Service.Authorize` rejects unknown permissions and disabled accounts. HTTP routes use `Ctrl.RequirePermission` after session authentication and before the handler. Each request reads current permissions; role revocation does not depend on a browser refresh or a stale session snapshot. Sensitive mutations recheck the actor under the policy lock before applying changes.

## HTTP Contract

Paths are relative to `/api/v1`. Mutations use the established same-origin, JSON, and CSRF policy, except public registration, which uses the same pre-session Origin protection as login and initialization.

| Method and path | Input | Response / access |
|---|---|---|
| `GET /auth/registration` | None | `{enabled}`; public |
| `POST /auth/register` | `{email,password,name}` | `201`, existing session response and cookie; registration must be open |
| `GET /auth/permissions` | Session | `{permissions:[]}`; authenticated |
| `GET /admin/registration` | Session | `{enabled}`; platform administrator |
| `PATCH /admin/registration` | `{enabled}` | `{enabled}`; platform administrator |
| `GET /admin/members` | Optional `q`, `status`, `role`, `limit`, `cursor` | `{items:Member[],next_cursor:string|null}`; `members.read` |
| `GET /admin/members/:user_id` | None | Member; `members.read` |
| `POST /admin/members` | `{email,name,password,role?}` | `201`, Member; `members.write`, with administrator creation restricted |
| `PATCH /admin/members/:user_id` | `{disabled?,role?}` | Member; `members.write`, subject to target and continuity restrictions |
| `GET /admin/roles` | None | `{items:Role[],available_permissions:[]}`; `roles.read` |
| `POST /admin/roles` | `{name,permissions:[]}` | `201`, Role; platform administrator |
| `PUT /admin/roles/:role_id` | `{name,permissions:[]}` | Role; platform administrator |
| `DELETE /admin/roles/:role_id` | None | `204`; platform administrator, custom unassigned roles only |
| `PUT /admin/members/:user_id/roles` | `{role_ids:[]}` | Member; platform administrator, custom-role IDs only |

A Member contains `{id,email,name,role,disabled,created_at,role_ids}`. `role_ids` lists custom assignments; `role` identifies the built-in membership. A Role contains `{id,name,builtin,permissions}`.

Member searches treat `%` and `_` as literal input rather than SQL wildcards. Status is `active` or `disabled`; base role is `admin` or `member`. Pagination orders by stable user ID, defaults to 40 results, and limits pages to 100. Invalid input returns `400`, insufficient privileges return `403`, missing resources return `404`, and uniqueness, assigned-role deletion, or last-administrator conflicts return `409` using sanitized errors.

## Persistence, Audit, and Verification

Schema version 6 uses frozen private GORM schema types and the shared migration helpers; it preserves existing users and their base roles. Role names use a SHA-256 name key for exact uniqueness independent of database collation, while the original spelling remains the display value. It initializes registration to disabled only when the settings row does not exist, so migration retries do not reset a configured policy. Foreign keys protect custom-role assignments and permissions. Policy changes and member/role mutations append audit events in their transactions without passwords, session material, or Key secrets.

`testGovernanceLifecycle` runs through the real router against fresh PostgreSQL and MySQL databases. It covers disabled-by-default registration, member-only creation without model grants, CSRF checks, protected built-ins, permission allowlists, additive roles, delegated access, self-grant and administrator escalation failures, atomic session/Key revocation, irreversible Key revocation across reenabling, failed assignment rollback, immediate permission removal, filtered lists, persistent registration settings, and concurrent protection of the last active administrator.

Use `go tool task test-integration` for the database lifecycle. Broader phase evidence and unimplemented governance capabilities are tracked in [the implementation record](IMPLEMENTATION.md).
