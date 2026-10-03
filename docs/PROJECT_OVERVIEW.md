# Project Creation and Overview

Projects have explicit manager relationships and independent model, policy and
Key scopes. Creating a Project does not inherit Team authority. Omitted initial resources
grant no models, limits, Keys or pending resource applications. Explicit initial
resources and simultaneous requests use the separately reviewed
[creation contract](PROJECT_CREATION.md).

## Initial managers

`POST /api/v1/projects` accepts `name`, `description` and optional `manager_ids`.
Omitting the manager field preserves the creator-only contract. An explicit set
must be nonempty, bounded and contain unique exact enabled User IDs. Ordinary
creators are retained in the manager set. A current direct `projects.write`
permission permits a complete explicit selection that can omit the creator.
`creator_id` remains an immutable creation fact and never supplies later access.

Project, all initial manager relationships and the creation audit commit together
under the governance transaction. Invalid aliases, disabled/offboarded Users,
invalid selections and persistence failures leave no partial resource. Changing
manager relationships later remains a separately authorized complete replacement.

`GET /api/v1/projects/creation-manager-candidates` is a creation-specific bounded
picker for current enabled actors. It returns minimal identity fields and never
grants global member-directory access. Searches remain literal. Existing selected
identities remain selected when they are outside the latest bounded response.
The create form retains the established layout and local Base UI controls.
Unknown creation outcomes do not permit inferred success or automatic replay.

## Authoritative overview

`GET /api/v1/projects/:project_id/overview` rechecks the exact current enabled
actor and canonical Project access inside a read-only repeatable-read database
snapshot. Independent sections retain their existing domain authority:

| Section | Boundary |
| --- | --- |
| Assigned managers and model grants | Exact Project access; counts represent recorded canonical relationships |
| Active Keys | Current manager or `projects.write`; recorded active status and server-owned expiry, not a claim of Project callability |
| Last recorded call | Current manager or `calls.read_all`; immutable Project call fact, without Provider or administrator diagnostics |
| Monthly policy and quota use | Current manager or existing Project policy read authority; exact decimal money, authoritative journal snapshot and installation calendar |
| Pending requests | Complete applicable Project request read authority; never infer a complete count from a partial page or subset of request kinds |
| Latest metadata activity | At most five known committed Project metadata/policy facts; no arbitrary audit JSON or current directory substitution for unrecorded historical names |

The response includes `project_id`, `observed_at`, nullable independent counts,
`calls_available`, `last_call_at`, `monthly_quota` and bounded typed `activities`.
Unavailable sections use null. A known complete query with no rows can return
zero. Unknown accounting, holds and known subtotals remain separate. Historical
money stays grouped by currency with decimal strings. Server-owned calendar
windows and journal `as_of` are distinct from the database observation timestamp.

The existing Overview tab contains pending-request guidance, authorized common
actions, monthly resource and operating cards, and a latest-activity table.
Independent actor/Project query keys and fresh authorization reads suppress old
private cards, manager details and actions during refresh, denial or account
changes. English/Chinese controls preserve the same interaction hierarchy.

## Acceptance and remaining scope

The integrated main package passed mandatory check, Go race/development lifecycle
and production embedding tests, with 1057 Vitest cases in 73 files. Complete
PostgreSQL/MySQL regression passed (Handler 815.082 seconds; Service 6.616
seconds), including exact creation, permission, alias, atomicity and native
quota/Overview fixtures. Both real-process authentication/native lifecycles
passed restart persistence and revocation.

Controlled production/browser acceptance created a Project with two canonical
managers and no implicit grants, policies, Keys or applications. One actual
native Project call settled 5 Tokens against a limit of 10; Overview displayed
the exact persisted last call, one active Key/model and one independent pending
request without applying its grant. Removing the creator's current manager
relationship returned 404 and hid all private cards and actions while retaining
creator attribution; the peer retained access. Explicit restoration and process
restart preserved Sessions, policy, usage and Overview facts. English/Chinese
rendering passed and English was restored.

The disposable QA wait distinguishes synchronous journal settlement from
asynchronous SQL call delivery and compares UTC instants instead of serialized
offset spellings. It never resends native inference or fabricates a last call.

Initial resource configuration and simultaneous creation requests extend the same
form through [reviewed atomic creation](PROJECT_CREATION.md); their acceptance
evidence is recorded separately from this earlier manager/Overview delivery. Recorded active Key counts are not an assertion that a disabled or
archived Project is callable. Full F07 and full product acceptance remain open.
