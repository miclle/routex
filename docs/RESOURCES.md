# Team and Project Governance

Teams and Projects are independent persisted resources. A Team represents an organizational membership boundary. A Project represents an independent shared workload boundary with explicit managers. Neither resource contains the other's identifier, and Team ownership or membership grants no Project authority.

## Identities and Authority

Team IDs use the `tea_` prefix, Project IDs use `prj_`, established Team membership IDs use `tmm_`, and Project manager relationship IDs use `pmg_`. Relationship IDs remain stable when a replacement request retains the same user. Resource names are display labels, not unique identities. Names contain 1–100 Unicode characters after trimming; descriptions contain up to 2,000 characters.

Teams have established memberships with an `owner` or `member` role and `active` or `disabled` status. Pending invitations and membership requests are separate future workflows, so `pending` is not an accepted membership state. A Team must retain at least one active owner whose platform account is enabled. Multiple owners are supported.

An active Team membership grants scoped read access. Ownership is a responsibility marker and does not implicitly grant Team mutation authority, platform administration, Project management, or Personal Key invocation. Explicit Team Session inference separately requires current active membership and the exact Team model grant. Team metadata, lifecycle, and membership changes require `teams.write`. Model assignments require the separate `teams.models.write` permission. `teams.read_all` permits global listing and inspection.

Every enabled platform member can create a Project with an explicit initial manager set. Ordinary creators remain managers; a direct `projects.write` administrator may select a complete set that excludes the creator. Omitting `manager_ids` preserves creator-only creation. Without explicitly reviewed initial resources or requests, new Projects have no model grants, policies, Keys or resource applications. The creation-specific picker does not grant global directory authority. See [Project creation and Overview](PROJECT_OVERVIEW.md) for the atomic creation and authoritative scoped cards. `creator_id` is an audit identity; authority comes from current manager relationships. Managers may change Project metadata and replace the manager set. They cannot change lifecycle state or expand model grants merely because they manage the Project. Those operations require `projects.write` or `projects.models.write`, respectively. `projects.write` also permits manager and metadata administration, while `projects.read_all` permits global listing and inspection. Projects currently have explicit managers, without an additional ordinary Project membership role.

The six platform permissions are available for custom roles. Built-in administrators receive them through schema version 8. Service transactions recheck current permission and account state, so route access or a stale browser session cannot substitute for authorization.

## Lifecycle and Continuity

Both resources start `active`. Platform-authorized lifecycle changes permit `active` to `disabled`, `disabled` to `active`, and `disabled` to `archived`. Direct archival of an active resource is rejected. Archive is terminal: metadata, relationship, model-grant, and lifecycle mutations then return `409`. There is no destructive delete endpoint; archived resources retain their audit identities and historical relationships.

Nonarchived resources retain at least one effective active responsible user, including while disabled. Relationship replacement validates the entire request before making any change. New Project manager assignments require enabled accounts. Active Team memberships require enabled accounts; a disabled Team membership may retain an existing disabled account. A suspended account's stored relationships remain available for governance history but confer no access while that account is disabled.

Resource creation, relationship changes, and account suspension all acquire the singleton governance policy lock before their resource or user locks. When an account is suspended, the transaction checks all nonarchived Teams that it actively owns and Projects that it manages. If suspension would leave any resource without another active owner or manager, it returns `409` before changing the account, sessions, or personal Keys. Concurrent suspensions cannot both pass this check. Archived resources do not block account suspension because their management lifecycle has ended.

All successful mutations append an audit event in the same transaction. Audit records identify actor, action, resource type, and resource ID without copying descriptions, credentials, or private request content.

## Model Scope

Team and Project model assignments use independent grant tables. Assignment validates the complete list against active logical models; duplicate or unknown IDs abort the whole replacement. An empty list means no assigned models.

These grants do not create `user_model_grants`, expand personal API Key scope, or grant platform permissions. Personal Key requests retain their existing direct-user grant rules. Explicit [Team Sessions](TEAM_INFERENCE.md) separately authorize native Chat Completions, Responses, Messages and Gemini text calls from current Team grants and active membership. [Project Keys](PROJECT_KEYS.md) use their own fixed scopes intersected with current Project model grants. [Project resource requests](PROJECT_REQUESTS.md) provide explicit model additions, finite monthly quota applications and finite RPM/TPM/concurrency applications with independent reviewer permissions. [Resource limits](QUOTAS.md) enforce Personal, Project and Key policies. Team Session call history is restricted to the current member's own actor, including owners and administrators. [Team resource limits](TEAM_LIMITS.md) add independently authorized aggregate/member policies and conjunctive native enforcement. Member caps do not reserve aggregate allocations. [Monthly Team member requests](TEAM_REQUESTS.md) add owner-first review, platform escalation and read-only global records without granting owners direct policy writes. Target-scoped [Team roles](TEAM_ROLES.md) provide reviewed administrator assignment and exact Team-only action unions. [User and Team defaults](DEFAULT_LIMITS.md) provide creation snapshots and explicit reviewed resets. Named policy templates, creator-private Team attachments and Team request-code export remain separate unfinished packages.

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

A full Team response contains `{id,name,description,status,created_at,members,model_ids}`. A dimension-only Team limit writer receives `{id,name,description,status,resource_limit_workspace_only:true}` for the directed Limits workspace; relationships and global directory access remain unavailable. Each member contains `{id,user_id,name,email,role,status}`. A full Project response contains `{id,name,description,status,creator_id,created_at,managers,model_ids,key_count,limits}`. The last two fields are list projections; detail and creation responses return them as `null`. A quota-only reviewer with `projects.limits.write` receives only `{id,name,description,status,request_workspace_only:true}` for the directed request workspace; this does not expose relationships or grant global listing authority. Each manager contains `{id,user_id,name,email}`. Names and emails identify related platform users; password hashes, sessions, and Key material never appear.

Personal lists remain scoped even for administrators. Global lists require their explicit `read_all` permission. An unrelated user receives `404` for a resource detail. List pagination orders by stable resource ID, defaults to 40 items, and caps pages at 100. The next cursor is `null` at the end. Search matches resource names case-insensitively and treats SQL wildcard characters literally. Project search also matches literal, case-sensitive fragments of the canonical Project ID; it does not normalize an ID alias into authority. The administrative Project filter accepts a name or ID fragment, while the personal list keeps its current management scope. State filters accept `active`, `disabled`, or `archived`. Relationship and model replacement requests are bounded to 1,000 items and remain subject to the management request body limit.

Invalid input returns `400`, insufficient mutation authority returns `403`, unavailable detail returns `404`, and continuity or lifecycle conflicts return `409`. Removing oneself from a Project manager set is allowed if another enabled manager remains; the returned mutation result describes the completed operation, while subsequent scoped reads require the new membership state.

## Project List Facts and Navigation

Both Project lists use the server-projected total retained `key_count` in the
existing Key-count column. The count includes pending, active, disabled, revoked
and expired Project Key records; it is not a count of active Keys or granted
Models. A currently enabled exact manager or holder of `projects.write` may read
it. `projects.read_all` alone permits the administrative list but leaves each
Key count `null`, displayed as Unknown. Global list authority never broadens the
personal list beyond current exact manager relationships.

The administrative list also projects `limits` as
`{stored,tokens_month,money_month,currency,rpm,tpm}` from the stored Project policy.
The administrative endpoint requires `projects.read_all`, which independently
permits Project limit reads; existing manager and `projects.limits.write` limit
read authority does not itself grant global listing. Personal lists return
`limits:null`. These summaries contain no effective defaults, inherited values,
usage, holds, remaining allowance or runtime-enforcement proof. They do not make
per-row Overview, limit or directory requests.

| Recorded value                                             | List meaning                                                                     |
| ---------------------------------------------------------- | -------------------------------------------------------------------------------- |
| `key_count:0`                                              | No retained Project Key records                                                  |
| `key_count:null`                                           | Key count is unknown to this reader                                              |
| `limits:null`                                              | Policy summary is unavailable in this response                                   |
| `limits.stored:false`                                      | No stored Project policy; nullable controls are `null` and currency is empty     |
| `limits.stored:true` with a nullable control set to `null` | That local control is Not set; this does not assert unlimited effective capacity |
| An explicit numeric or money zero                          | A recorded zero, preserved independently of unset or absent policy               |
| Missing or invalid projection fields                       | Unknown; never substituted with zero or a default policy                         |

Monthly money remains an exact decimal string alongside its recorded currency.
The browser never rounds it through JavaScript numbers or substitutes the current
platform denomination. Monthly Tokens, RPM and TPM display only their recorded
stored values. Team model-count columns and Team tab navigation are unchanged.

Project lists reauthorize through actor-scoped queries after Session renewal and,
for administrative lists, permission renewal. Rows, policy summaries, action
menus and pending lifecycle dialogs stay hidden during renewed reads, errors or
account changes. Late results for an obsolete actor or generation cannot restore
private rows. Lifecycle actions still require their independent current write
permission.

After a successful fresh actor- and target-authorized Project detail read and a
fresh Session, legacy `?tab=managers` URLs become `?tab=settings`; `?tab=models`
and `?tab=limits` become `?tab=resources`. The router replaces the current history
entry and preserves other query parameters on scoped and administrative Project
URLs. Pending, denied or obsolete reads never rewrite the URL or reveal tabs.
Only these known Project aliases are canonicalized; Team tabs retain their
existing URLs.

The source contracts are covered by `internal/routex/service/project_list.go`,
`internal/routex/handler/project_list_integration_test.go` and
`website/src/views/resources/project-list-navigation.test.tsx`. Controlled
browser, production and dual-database acceptance are recorded separately in the
implementation handoff; this contract does not declare those gates complete.

## Storage and Verification

Schema version 8 uses private frozen GORM definitions, explicit belongs-to relations, foreign keys, unique membership constraints, state checks, and indexes for user/model lookups. It creates no recursive changes to user or model tables. The shared migration helper reconciles constraints and indexes on retry, and permission seeds use conflict-safe inserts.

`testResourceLifecycle` runs in the shared fresh-database integration harness for PostgreSQL and MySQL. It checks explicit identities, scoped reads, owner and manager privilege limits, cross-resource isolation, atomic relationship/model replacement, personal-Key scope separation, pagination/search, terminal archival, foreign keys, transactional audit, and concurrent suspension continuity. Run `go tool task test-integration`; ordinary tests skip the database lifecycle when dedicated test DSNs are absent.

## Scoped selection APIs

`GET /projects/:project_id/manager-candidates?q=...` is limited to current managers and holders of `projects.write`; it returns enabled users for that Project's manager form. `GET /admin/team-member-candidates?q=...` requires `teams.write`. `GET /admin/resource-model-candidates?kind=teams|projects&q=...` requires the corresponding `*.models.write` permission and returns active logical models. Results are bounded to 50 items, sorted by stable ID, with literal case-insensitive name/email search. User items contain only ID, name, and email; model items contain ID and current name. These endpoints do not grant general member-directory or model-administration access. Archived Projects cannot use mutation pickers.

## Target-scoped Team roles

Team role assignments are independent of direct user roles. Current active members
inherit only `teams.write` and `teams.models.write` within the assigned active Team.
Metadata, membership and model controls use the target-specific role projection;
Team lifecycle changes still require direct platform `teams.write`. Global lists,
Team creation, quotas, Project management and native model grants remain separate.
Only the protected platform administrator may replace Team roles with a reviewed
If-Match and required reason. The existing detail gains a Roles tab with the compact
assignment table, permission dialog, bounded picker and explicit Save.

Target-specific member and model candidate endpoints support delegated controls
without fetching the global directory. Quota-only resource representations never
fetch role data or candidates. See [Team roles](TEAM_ROLES.md) for the exact API,
current-definition review, saved-state and acceptance contracts.

## Canonical Project authority

Project actor IDs, resource IDs, selected manager IDs and model IDs are exact
identities. Database collation must not turn case aliases into authority. Service
checks require a currently enabled, non-offboarded exact actor and exact direct
role/permission associations or current exact Project manager membership. The
immutable creator fact gives no permanent management authority. Manager-only
users cannot assign models or change lifecycle state; platform editors need their
independent action permission and do not become managers by editing a resource.

Manager replacement validates every selected enabled user before changing the
complete set. Retained canonical relationships preserve their IDs. A corrupt
association with an aliased Project or user cannot lend its ID to a repaired
canonical replacement. Model replacement similarly validates exact active model
identities. Invalid aliases, unavailable users/models, forbidden actors and
continuity conflicts leave relationships, audit and runtime publication unchanged.
Disabled Projects remain administratively editable; archived Projects remain
terminal.

Resource detail queries are actor- and target-scoped, reauthorize on mount, and
hide cached private details, tabs and actions while the renewed read is pending or
fails. Late responses from another actor or target cannot restore a prior detail.
The existing layout, localized loading/errors and permission-specific controls
are preserved. This protection covers Team details through the same component.

The real PostgreSQL/MySQL `project_authority` lifecycle also exercises Project Key
independence from its creator's later manager removal. Failed publication after a
grant replacement retains the Project tombstone: native authentication returns
401 and dispatches nothing. Fresh successful publication restores the still-granted
model; removed grants remain unavailable and immutable call history remains intact.
The authority package adds no migration, endpoint or dependency. Initial
manager selection and Project overview behavior are documented separately in
[Project creation and Overview](PROJECT_OVERVIEW.md).

## Shared Team Model requests

[Team Model requests](TEAM_MODEL_REQUESTS.md) preserve exact membership-bound
submission, independent scoped review and immutable history. Pending requests
change no access; approval affects only shared Team grants. Full Model-list
updates retain unchanged canonical grant provenance, while removal/re-addition
does not revive the original receipt's application proof.

[Project creation and initial resources](PROJECT_CREATION.md) extend the existing
form with independently authorized initial models and limits or atomic separate
pending requests. Reviewed context/currency and a stable creation receipt separate
historical commit from renewed current authority and runtime application.
