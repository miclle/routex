# Team Roles

Team-assigned roles contribute explicit management actions within one Team. They
do not change a member's direct platform roles, model invocation grants, quotas,
Key ownership or Project management. Controlled acceptance covers the frozen implementation; full platform release
acceptance remains separate.

## Current authority

A current enabled, non-offboarded member of an active Team inherits the union of
`teams.write` and `teams.models.write` from roles assigned to that Team. These
actions authorize its metadata/membership and model relationships respectively.
Direct platform permissions remain an independent source. Team roles never grant
a global directory, creation of another Team, quota-administrator dimensions,
role assignment, Provider, Project, Key or unrelated actions. Owner responsibility
alone contributes no management permission. Team lifecycle changes require direct
platform `teams.write`; delegated metadata access cannot disable or archive a Team.

Role definitions are read from current authority, so edits take effect on the next
management operation. Removed/disabled membership, disabled/offboarded User and
inactive Team cannot retain inherited actions. Global Session permission queries,
application navigation and native model grants are unchanged. Team member/model
mutations retain their existing authorization, continuity, tombstone and runtime
publication behavior.

## API and saved state

All endpoints are under `/api/v1` with a current Session; writes require CSRF,
same-origin, strict JSON, a reviewed strong If-Match and required reason.

| Endpoint | Contract |
| --- | --- |
| `GET /teams/:team_id/roles` | Assigned role summaries, assignment action union and separate current actor actions |
| `PUT /teams/:team_id/roles` | Protected platform administrator's complete distinct role selection |
| `GET /teams/:team_id/role-candidates` | Target-authorized administrator-only candidate summaries |
| `GET /teams/:team_id/member-candidates` | Target-specific current member-management authority |
| `GET /teams/:team_id/model-candidates` | Target-specific current model-management authority |

The roles representation contains `team_id`, `role_ids`, safe `roles` summaries,
`effective_team_actions`, `actor_team_actions`, `etag` and `can_assign_roles`. Role
summaries expose only ID, name, built-in status and the explicit Team actions.
Global role permissions and unrelated candidate details are not copied into this
projection. Minimal quota-only resource views cannot query role/candidate data.

Only an actual enabled, non-offboarded platform administrator may replace up to
100 distinct role IDs. Governance, Team and sorted Role locks protect assignment,
role edits and deletion. A reviewed hash binds Team lifecycle, assignments and safe
current role definitions; administrator reviews also bind candidate definitions.
Team update time advances monotonically at portable millisecond precision for
assignment and Team metadata generations, preventing role-set ABA. Changed writes
hash the canonical persisted Team inside the same locked transaction; returned
validators must match subsequent roles and candidate representations.
A current exact no-op may acknowledge saved state without another audit; stale
reviews must not be silently rebased or acknowledged from matching IDs alone.

Assignment writes report saved state, not a gateway publication receipt. Unknown
outcomes preserve their exact selection/reason/If-Match; a later GET reports the
current representation. Changed definitions or policy generations require explicit
review before another intent. A separate explicit confirmation may discard the
unresolved local draft after a new same-actor, same-Team read completes and the current assignment
and safe definitions are explicitly reviewed. It performs no server write and makes no claim about the original
operation's outcome. Successful saves and manual refresh use the current
definition generation for candidate queries.

## Persistence and audit

Frozen GORM V41 adds `team_roles` with a composite Team/Role primary key, role
lookup index and restrictive live Team/Role foreign keys. Archived Teams retain
assignments; assigned Roles cannot be deleted. Released V1–V40 and direct user
role relations are unchanged. The migration uses bounded frozen structs and GORM
Migrator APIs without handwritten production SQL.

`team.roles.replace` records the exact Team and before/after role IDs and Team
action unions with a required reason in the same transaction. The identity audit
projection validates only these bounded fields and strips arbitrary JSON.

## Interface and verification boundary

The existing Team detail gains an addressable Roles tab between Members and
Models. Preserve the compact role/type table, permissions dialog, remove/add
picker, explicit Save and effective action table. Local Team/actor queries gate
metadata/member/model controls without merging actions into the global Session
permission cache. Candidate selections remain selected outside a bounded result.
All visible copy is paired in `resources` with English as the default.

Final check and test passed, including 957 Vitest cases in 67 files. Real
PostgreSQL/MySQL migration focus passed (202.479 seconds), corrected role workflow
focus passed (238.34 seconds), and the complete matrix passed (Handler 723.801
seconds; Service 6.167 seconds). Both actual database process auth/native lifecycles
passed. Controlled browser/API proof covers scoped management, negative authority,
revocation, restart, English/Chinese views and a committed response-loss scenario
without a false historical success claim. Native Team protocol expansion,
templates and distributed publication remain separate scope.
