# Team and Project Governance

Teams and Projects are independent persisted resources. A Team represents an organizational membership boundary. A Project represents an independent shared workload boundary with explicit managers. Neither resource contains the other's identifier, and Team ownership or membership grants no Project authority.

## Identities and Authority

Team IDs use the `tea_` prefix, Project IDs use `prj_`, established Team membership IDs use `tmm_`, and Project manager relationship IDs use `pmg_`. Relationship IDs remain stable when a replacement request retains the same user. Resource names are display labels, not unique identities. Names contain 1–100 Unicode characters after trimming; descriptions contain up to 2,000 characters.

Teams have established memberships with an `owner` or `member` role and `active` or `disabled` status. Pending invitations and membership requests are separate future workflows, so `pending` is not an accepted membership state. A Team must retain at least one active owner whose platform account is enabled. Multiple owners are supported.

An active Team membership grants scoped read access. Ownership is a responsibility marker and does not implicitly grant Team mutation authority, platform administration, Project management, or model invocation. Team metadata, lifecycle, and membership changes require `teams.write`. Model assignments require the separate `teams.models.write` permission. `teams.read_all` permits global listing and inspection.

Every enabled platform member can create a Project and becomes its initial manager. New Projects have no model grants. `creator_id` is an audit identity; authority comes from current manager relationships. Managers may change Project metadata and replace the manager set. They cannot change lifecycle state or expand model grants merely because they manage the Project. Those operations require `projects.write` or `projects.models.write`, respectively. `projects.write` also permits manager and metadata administration, while `projects.read_all` permits global listing and inspection. Projects currently have explicit managers, without an additional ordinary Project membership role.

The six platform permissions are available for custom roles. Built-in administrators receive them through schema version 8. Service transactions recheck current permission and account state, so route access or a stale browser session cannot substitute for authorization.

## Lifecycle and Continuity

Both resources start `active`. Platform-authorized lifecycle changes permit `active` to `disabled`, `disabled` to `active`, and `disabled` to `archived`. Direct archival of an active resource is rejected. Archive is terminal: metadata, relationship, model-grant, and lifecycle mutations then return `409`. There is no destructive delete endpoint; archived resources retain their audit identities and historical relationships.

Nonarchived resources retain at least one effective active responsible user, including while disabled. Relationship replacement validates the entire request before making any change. New Project manager assignments require enabled accounts. Active Team memberships require enabled accounts; a disabled Team membership may retain an existing disabled account. A suspended account's stored relationships remain available for governance history but confer no access while that account is disabled.

Resource creation, relationship changes, and account suspension all acquire the singleton governance policy lock before their resource or user locks. When an account is suspended, the transaction checks all nonarchived Teams that it actively owns and Projects that it manages. If suspension would leave any resource without another active owner or manager, it returns `409` before changing the account, sessions, or personal Keys. Concurrent suspensions cannot both pass this check. Archived resources do not block account suspension because their management lifecycle has ended.

All successful mutations append an audit event in the same transaction. Audit records identify actor, action, resource type, and resource ID without copying descriptions, credentials, or private request content.

## Model Scope

Team and Project model assignments use independent grant tables. Assignment validates the complete list against active logical models; duplicate or unknown IDs abort the whole replacement. An empty list means no assigned models.

These grants do not create `user_model_grants`, expand personal API Key scope, or grant platform permissions. The current gateway continues to use its existing direct-user and personal-Key authorization rules. [Project Keys](PROJECT_KEYS.md) use their own fixed scopes intersected with current Project model grants. Interactive Team/Project session contexts, resource requests, budgets, quotas, and their enforcement remain later work; this phase exposes no placeholder quota fields and does not claim those runtime capabilities.

## HTTP Contract

All paths below are relative to `/api/v1`. Every route requires an authenticated session. Mutations also require same-origin protection, JSON, and the current CSRF token. Service authorization applies independently of the URL namespace.

| Method and path | Input | Access and result |
|---|---|---|
| `GET /teams` | `q`, `status`, `limit`, `cursor` | Current active Team memberships; `{items,next_cursor}` |
| `GET /teams/:team_id` | None | Scoped membership or relevant Team platform permission; Team |
| `GET /admin/teams` | Same list filters | `teams.read_all`; global list |
| `POST /admin/teams` | `{name,description?,owner_ids}` | `teams.write`; `201`, Team, at least one enabled owner |
| `GET /admin/teams/:team_id` | None | Same resource read policy; Team |
| `PATCH /admin/teams/:team_id` | `{name?,description?,status?}` | `teams.write`; updated Team |
| `PUT /admin/teams/:team_id/members` | `{members:[{user_id,role,status}]}` | `teams.write`; atomic complete replacement |
| `PUT /admin/teams/:team_id/models` | `{model_ids:[]}` | `teams.models.write`; atomic complete replacement |
| `GET /projects` | `q`, `status`, `limit`, `cursor` | Current explicit Project management scope; `{items,next_cursor}` |
| `POST /projects` | `{name,description?}` | Enabled member; `201`, Project with creator as initial manager |
| `GET /projects/:project_id` | None | Current manager or relevant Project platform permission; Project |
| `PATCH /projects/:project_id` | `{name?,description?,status?}` | Manager for metadata; `projects.write` required for lifecycle |
| `PUT /projects/:project_id/managers` | `{user_ids:[]}` | Current manager or `projects.write`; nonempty complete replacement |
| `PUT /projects/:project_id/models` | `{model_ids:[]}` | `projects.models.write`; complete replacement |
| `GET /admin/projects` | Same list filters | `projects.read_all`; global list |
| `GET/PATCH /admin/projects/:project_id` | As above | Same service authorization as scoped route |
| `PUT /admin/projects/:project_id/managers` | As above | Same manager replacement contract |
| `PUT /admin/projects/:project_id/models` | As above | Same model assignment contract |

A Team response contains `{id,name,description,status,created_at,members,model_ids}`. Each member contains `{id,user_id,name,email,role,status}`. A Project contains `{id,name,description,status,creator_id,created_at,managers,model_ids}`. Each manager contains `{id,user_id,name,email}`. Names and emails identify related platform users; password hashes, sessions, and Key material never appear.

Personal lists remain scoped even for administrators. Global lists require their explicit `read_all` permission. An unrelated user receives `404` for a resource detail. List pagination orders by stable resource ID, defaults to 40 items, and caps pages at 100. The next cursor is `null` at the end. Search matches resource names case-insensitively and treats SQL wildcard characters literally. State filters accept `active`, `disabled`, or `archived`. Relationship and model replacement requests are bounded to 1,000 items and remain subject to the management request body limit.

Invalid input returns `400`, insufficient mutation authority returns `403`, unavailable detail returns `404`, and continuity or lifecycle conflicts return `409`. Removing oneself from a Project manager set is allowed if another enabled manager remains; the returned mutation result describes the completed operation, while subsequent scoped reads require the new membership state.

## Storage and Verification

Schema version 8 uses private frozen GORM definitions, explicit belongs-to relations, foreign keys, unique membership constraints, state checks, and indexes for user/model lookups. It creates no recursive changes to user or model tables. The shared migration helper reconciles constraints and indexes on retry, and permission seeds use conflict-safe inserts.

`testResourceLifecycle` runs in the shared fresh-database integration harness for PostgreSQL and MySQL. It checks explicit identities, scoped reads, owner and manager privilege limits, cross-resource isolation, atomic relationship/model replacement, personal-Key scope separation, pagination/search, terminal archival, foreign keys, transactional audit, and concurrent suspension continuity. Run `go tool task test-integration`; ordinary tests skip the database lifecycle when dedicated test DSNs are absent.

## Scoped selection APIs

`GET /projects/:project_id/manager-candidates?q=...` is limited to current managers and holders of `projects.write`; it returns enabled users for that Project's manager form. `GET /admin/team-member-candidates?q=...` requires `teams.write`. `GET /admin/resource-model-candidates?kind=teams|projects&q=...` requires the corresponding `*.models.write` permission and returns active logical models. Results are bounded to 50 items, sorted by stable ID, with literal case-insensitive name/email search. User items contain only ID, name, and email; model items contain ID and current name. These endpoints do not grant general member-directory or model-administration access. Archived Projects cannot use mutation pickers.
