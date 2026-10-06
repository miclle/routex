# Member Governance and Platform Permissions

This phase adds controlled public registration, administrator member management, protected built-in roles, additive custom roles, and database-backed platform authorization. Model call grants remain separate from platform permissions. Team and Project responsibilities are documented separately in [resource governance](RESOURCES.md). Complete employee offboarding and enterprise identity synchronization remain subsequent work packages.

## Registration and Member Lifecycle

Registration is disabled by default and persisted in the singleton `governance_settings` row. It cannot create users before installation initialization. An admitted platform administrator with `registration.write` reviews and saves the complete registration policy (enablement, approval requirement and allowed email domains) with a reason and strong If-Match. The approval requirement defaults to false and applies only to new local self-registration; setup, administrator-created accounts and historical accounts remain unmanaged by this requirement.

Public registration always creates a `member`, ignores attempted role injection and grants no models. Without approval it atomically creates its initial browser Session. With approval it creates a linked pending application and returns HTTP202 `{kind:"approval_pending"}` without a Session, cookie, CSRF token or application identifier. Pending, rejected or invalid application identities cannot log in, use Sessions or Keys, or contribute active Team/Project authority. Approval changes only the application decision and its typed audit; it does not enable a disabled account, undo offboarding, grant models or roles, create a Key, or restore revoked credentials. A newly approved active applicant must log in normally. Registration retains normalized email, exact unique email constraints, the12–72 UTF-8 byte password policy and bcrypt hashing.

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
| `members.keys.disable` | Disable another eligible member's Personal Key under independent subject/lifecycle guards |
| `roles.read` | Read role definitions and permission metadata |
| `roles.write` | Protected platform administrator role management |
| `registration.write` | Protected platform administrator registration policy |
| `members.approvals.write` | Protected platform administrator account approval decisions; independent `members.read` is also required |
| `providers.read` / `providers.write` | Read or change provider configuration |
| `models.read_all` / `models.write` | Read or change the administrative model catalog |
| `calls.read_all` | Read calls across users through administrator DTOs |
| `audit.read` | Platform audit access boundary |
| `system.read` | Platform operational state access boundary |
| `teams.read_all` / `teams.write` / `teams.models.write` | Global Team reading, metadata/membership/lifecycle changes, and explicit model grants |
| `projects.read_all` / `projects.write` / `projects.models.write` | Global Project reading, metadata/manager/lifecycle changes, and explicit model grants |

`roles.write`, `registration.write` and `members.approvals.write` are reserved for the protected administrator identity. They cannot be assigned through custom roles. The API's `available_permissions` list contains only permissions that may be placed in a custom role; built-in administrator metadata additionally includes the reserved powers. Defining a permission boundary does not claim that every corresponding product page or operational capability has been implemented.

Custom roles are additive and confer only implemented platform actions. They do not grant model invocation, ownership of another user's Keys, Team ownership, Project management, or unrestricted access to personal endpoints. Unknown or duplicate permission values are rejected. A role still assigned to users or Teams cannot be deleted. Role replacement and assignment validate the whole request before committing, preventing partial changes.

A delegated member manager may create ordinary members and change other ordinary members' enabled state. They cannot create administrators, change any base role, modify an administrator, suspend themselves, create/edit/delete roles, assign roles, or change registration policy. These restrictions are enforced in the service transaction as well as at the HTTP boundary, so direct method use cannot turn a delegated permission into a privilege escalation.

`Service.Permissions` reads current persisted membership and role definitions. `Service.Authorize` rejects unknown permissions and disabled accounts. HTTP routes use `Ctrl.RequirePermission` after session authentication and before the handler. Each request reads current permissions; role revocation does not depend on a browser refresh or a stale session snapshot. Sensitive mutations recheck the actor under the policy lock before applying changes.

## HTTP Contract

Paths are relative to `/api/v1`. Mutations use the established same-origin, JSON, and CSRF policy, except public registration, which uses the same pre-session Origin protection as login and initialization.

| Method and path | Input | Response / access |
|---|---|---|
| `GET /auth/registration` | None | `{enabled,approval_required,allowed_email_domains}`; public, no private validator |
| `POST /auth/register` | `{email,password,name}` | `201` Session and cookie without approval, or `202` `{kind:"approval_pending"}` without authentication; registration must be open |
| `GET /auth/permissions` | Session | `{permissions:[]}`; authenticated |
| `GET /admin/registration` | Session | `{enabled,approval_required,allowed_email_domains,review_etag}` and strong ETag; admitted platform administrator with `registration.write` |
| `PATCH /admin/registration` | `{enabled,approval_required,allowed_email_domains,reason}`, strong If-Match | Current policy confirmation; same authority as GET |
| `GET /admin/members/:user_id/approval` | Session | Reviewed application, independent eligibility/editability/runtime facts and strong ETag; `members.read` |
| `PATCH /admin/members/:user_id/approval` | `{decision:"approve"\|"reject",reason}`, strong If-Match | Exact current decision, eligibility and runtime application; admitted administrator with `members.read` and `members.approvals.write` |
| `GET /admin/members` | Optional `q`, `status`, `role`, `limit`, `cursor` | `{items:Member[],next_cursor:string|null}`; `members.read` |
| `GET /admin/members/:user_id` | None | Member; `members.read` |
| `GET /admin/members/:user_id/keys` | Optional exact `cursor`, `limit` 1–100 (default 40) | Retained Personal Key page; `members.read` |
| `GET /admin/members/:user_id/keys/:key_id` | None | Safe reviewed Key/ETag; `members.read` |
| `POST /admin/members/:user_id/keys/:key_id/disable` | `{reason}`, strong reviewed If-Match | Exact current disabled/runtime confirmation; `members.keys.disable` |
| `POST /admin/members` | `{email,name,password,role?}` | `201`, Member; `members.write`, with administrator creation restricted |
| `PATCH /admin/members/:user_id` | `{disabled?,role?}` | Member; `members.write`, subject to target and continuity restrictions |
| `GET /admin/roles` | None | `{items:Role[],available_permissions:[]}`; authoritative retained `member_count` on list items; `roles.read` |
| `POST /admin/roles` | `{name,permissions:[]}` | `201`, Role; platform administrator |
| `GET /admin/roles/:role_id` | None | Complete reviewed definition and strong ETag; current `roles.read` |
| `PUT /admin/roles/:role_id` | `{name,permissions:[],identity_etag,reason}`, strong reviewed If-Match | Current definition confirmation; admitted intrinsic platform administrator, independently of `roles.read` |
| `DELETE /admin/roles/:role_id` | None | `204`; platform administrator, custom unassigned roles only |
| `PUT /admin/members/:user_id/roles` | `{role_ids:[]}` | Member; platform administrator, custom-role IDs only |

A Member contains `{id,email,name,role,disabled,created_at,role_ids}`. `role_ids` lists custom assignments; `role` identifies the built-in membership. A Role contains `{id,name,builtin,permissions}`.

Member searches treat `%` and `_` as literal input rather than SQL wildcards. Status is `active` or `disabled`; base role is `admin` or `member`. Pagination orders by stable user ID, defaults to 40 results, and limits pages to 100. Invalid input returns `400`, insufficient privileges return `403`, missing resources return `404`, and uniqueness, assigned-role deletion, or last-administrator conflicts return `409` using sanitized errors.

## Persistence, Audit, and Verification

Schema version 6 uses frozen private GORM schema types and the shared migration helpers; it preserves existing users and their base roles. Role names use a SHA-256 name key for exact uniqueness independent of database collation, while the original spelling remains the display value. It initializes registration to disabled only when the settings row does not exist, so migration retries do not reset a configured policy. Foreign keys protect custom-role assignments and permissions. Policy changes and member/role mutations append audit events in their transactions without passwords, session material, or Key secrets.

`testGovernanceLifecycle` runs through the real router against fresh PostgreSQL and MySQL databases. It covers disabled-by-default registration, member-only creation without model grants, CSRF checks, protected built-ins, permission allowlists, additive roles, delegated access, self-grant and administrator escalation failures, atomic session/Key revocation, irreversible Key revocation across reenabling, failed assignment rollback, immediate permission removal, filtered lists, persistent registration settings, and concurrent protection of the last active administrator.

Use `go tool task test-integration` for the database lifecycle. Broader phase evidence and unimplemented governance capabilities are tracked in [the implementation record](IMPLEMENTATION.md).

## Team assignment boundary

[Team roles](TEAM_ROLES.md) are target-scoped relationships, separate from platform
`user_roles`. An enabled current member of an active Team inherits only
`teams.write` and `teams.models.write` from its assigned roles. A role may contain
other platform permissions, but those permissions do not enter Team authority.
Ownership alone contributes no management permission. Session permissions and
application navigation remain based on direct platform roles. Team lifecycle,
quota approvals, Project/Key scope and native model access remain independent.

Only an actual enabled platform administrator may replace Team assignments. The
review binds current assignment and safe role definitions, including selectable
roles; definition changes invalidate a previous review. Assignment writes and role
edits/deletion share the governance lock. Assigned roles cannot be deleted even
when their Team is archived. Typed `team.roles.replace` audit records expose only
bounded before/after role IDs, Team actions and required reason.

## Independent Personal Model review

`members.models.write` authorizes only target-scoped direct Model access and
request review. It can be delegated independently of `members.read`,
`members.write` and `models.write`; it reveals no Member email, role, Key or limit
fields. Reviewers cannot approve themselves. See
[Personal Model requests](PERSONAL_MODEL_REQUESTS.md) for exact reviewed decisions,
current authority, additions-only grants and immutable receipt semantics.

## Independent Team Model review

`teams.models.write` authorizes minimal target-scoped Model access and request
review independently of Team directory permissions. A scoped assigned role may
provide it; ownership alone does not. Reviewers cannot approve themselves.
[Team Model requests](TEAM_MODEL_REQUESTS.md) preserve immutable decisions,
same-transaction pending cancellation and shared grants after applicant departure.

## Administrative Personal Key boundary

Member Keys metadata reads retain all Personal lifecycle states, including for
inactive subjects, but expose no Key secret, digest, prefix or Project Key. They
resolve exact owner ancestry and recorded Personal last-use facts without calling
an owner endpoint as the subject. The member page remains `members.read` gated;
Disable authority is checked independently, including in the service transaction.
Neither `members.write` nor another user's owner authority implies this action.
Delegated members cannot target themselves or administrator subjects; inactive or
offboarded subjects cannot be mutated. Administrators need the explicit permission.

A reviewed active Key ETag includes its persistent lifecycle revision. Disable
advances that revision with a real-actor typed audit in one governance transaction.
Every product Personal Key state writer uses the publication gate, preserving
owner operations while fencing re-enable/status ABA. A reduction tombstone precedes
refresh; success requires exact current retained owner/revision/disabled proof and
an unexpired runtime lease. Already-dispatched calls and immutable history remain.

Authorized disabled-target retry reports only `current_disabled_state`, with no
second audit or claim of original historical commit. A newer re-enable conflicts
with the old review. Failed publication can leave a committed operation uncertain;
the UI keeps the original reason/If-Match and does not reconcile from metadata GET
or 404. [Keys](KEYS.md#administrative-member-keys) documents the row, limits and
transient confirmation contract. V52 seeds only the built-in administrator's new
permission; assignment does not transfer ownership or inference access. Integrated R2 source checks/build and focused dual-driver lifecycle/migration
checks passed. The full dual-driver regression and final mandatory check also passed.
Authentication lifecycle passed on both drivers. The bounded browser focus correction passed rebuilt source checks and skips
restoration on lost authority or hidden/disconnected rows. Immediate success
invalidation remains. Rebuilt-artifact native/browser/restart acceptance passed.
The checked 45-path package was committed/pushed as
818500abea154b23b442e8a1ce5404627a2dc7c1 with exact remote read-back. Actionlint
and GolangCI-Lint passed; CI 37214572870 also completed successfully for that exact commit.

## Addressable Member Limits candidate

The Member detail Limits tab at `?tab=limits` owns budget/quota, request-rate and
IP policy controls plus explicit reset-to-current-default. Settings retains
identity and lifecycle actions. The page remains independently `members.read`
gated; only `limits.users.write` permits policy/reset writes. Neither
`members.write` nor the Limits tab itself adds spending or owner authority.

The member-only panel is a stable sibling for the same actor/target. Incidental
Session, permission, member or policy reads hide private facts/actions/dialogs
while retaining local unsent drafts and exact already-dispatched policy intent.
Actor/target changes, tab exit and logout destroy state and abort obsolete work.
Dispatch and callbacks check current cache status/generation, exact actor/target,
current write permission and enabled target; explicit retry uses current CSRF.
No extra Session observer, browser storage or private draft cache is introduced.

A policy conflict preserves drafts and needs explicit review. Uncertain writes
retain the original reason/body/If-Match through renewal and rejected retries;
review cannot replace that unresolved intent. Default reset reuses the hook-free
controls with member-managed authority and generation guards, retaining original
review/reason through renewal or rejection. Saved policy and runtime enforcement
remain separate. Existing Project/Key/Team callers keep their existing behavior.
Current-main source checks/build and controlled policy/native/browser/restart
acceptance passed. The checked observer masked one committed backend response
with HTTP503; explicit original retry confirmed current enforcement without a
second policy or audit write. This is not raw network-loss proof. Final mandatory
check passed. The 17-path Limits package is delivered as
2d5cafc255561933e478dca637e1df95e0b935a5 with exact push/remote read-back.
Actionlint and GolangCI-Lint passed; its CI remains pending at this checkpoint. Detailed evidence is in
[Implementation](IMPLEMENTATION.md).
See [Resource limits](RESOURCE_LIMITS.md#administrative-member-limits-tab).


## Administrative Member Teams candidate

The addressable `?tab=teams` is a read-only six-column table immediately after
Overview: Team link, Team status, target-member monthly Tokens/quota, budget,
configured rates and recorded joined time. It neither edits relationships nor
opens a Team directory. Retained disabled memberships and disabled/archived Teams
remain inspectable; removed relationships are absent.

| Operation | Required current authority |
| --- | --- |
| Open the tab and read each page | `members.read` AND `teams.read_all` |
| Follow an exact Team resource link | Both enclosing read grants remain fresh; the destination reauthorizes independently |
| Write a Team, membership or policy | No action is supplied by this tab; existing write permissions do not replace either read grant |

The server reads only the exact retained target's relationships. Personal usage,
caller membership, owners, administrative role labels and write authority never
expand this list. The scoped endpoint is
`GET /api/v1/admin/members/:user_id/teams`, with default20/maximum50 and an opaque
actor/target-bound cursor. Load more is explicit; no total or global scan is
invented. Every page is a fresh read and carries its own observation context.

Fresh Session, either permission, exact target and list generations gate rows,
links, tooltips and pagination synchronously. Renewal/error hides private facts;
obsolete responses cannot restore them. Actor/target/tab/logout destroys these
read-only pages. No additional enabled Session observer, browser storage,
mutation, draft or per-row limit query is added. A denied direct Teams URL remains
at that address with a localized denial instead of pretending Overview succeeded.

Exact stored member monthly/rate values and parent context remain distinct.
Historical charges stay in their recorded currencies; unknown coverage, retained
monthly holds and separate live reservations never become zero or remaining quota.
Recorded joined time is descriptive and may be Unknown; it grants no current
membership authority and resets no stable Team/User accounting. A local Base UI
Tooltip wrapper supports accessible scope/context help within the six columns.

The reviewed 17-path backend and 13-path frontend are integrated as one source
candidate. Current-main source checks/build and repaired focused PostgreSQL/MySQL
migration/lifecycle passed, including six-child Overview regression after a
fixture-only full-matrix failure. Mandatory check passed. Authentication lifecycle,
controlled native/browser/restart, a fresh successful full matrix and delivery
remain pending. Contracts:
[Resource limits](RESOURCE_LIMITS.md#administrative-member-team-policy-display)
and [Database](DATABASE.md#nullable-team-membership-joined-time-v53).

### Member Teams process acceptance checkpoint, 2026-10-05

The standard PostgreSQL/MySQL authentication lifecycle passed, including real
process restart, persisted sessions, revocation, encrypted credentials, native
ordinary/streaming calls and immutable call history. Its owned Compose resources
were independently absent afterward. The R1 controlled native Teams run failed
at the live-reservation observation before its browser checkpoint. This is an
unresolved acceptance failure, not a delivered feature or browser pass. The
failed run was cleaned up and its exact owned containers, networks, volumes and
application listener were independently absent. All 164 protected source paths
and the checked production binary remain unchanged. Diagnose the observed
reservation values, rerun native/browser/restart acceptance, then pass a fresh
complete 89-case-per-driver matrix before committing this phase.

### Member Teams native and browser acceptance passed, 2026-10-05

Controlled process R3 passed eight immutable native completions (six known usage,
two missing usage), two membership denials without upstream dispatch, three real
join-date writers, nine typed continuity Team audits and same-artifact restart.
The primary member retained 12 known Tokens and exact 12.000000000000000004 USD;
aggregate 15 included its independent peer. A finite missing-usage call retained
5 Tokens and the conservative 5.000000000000000003 USD bound; the separate
unbounded case retained one unknown record. Original and rejoined membership
attribution, policies, ciphertext and immutable history survived restart.

R1/R2 failures remain above: R2 measured that only the helper's active-money
expectation differed. R3 corrected that single constant to the measured bound
supported by pricing component rounding; product source/binary stayed unchanged.
Actual browser acceptance passed English-default and live Chinese switching,
exact amounts, historical unknown joins, five target-only relationships, disabled
and archived state, keyboard tooltip and Escape, refresh, horizontal table access
and 390px mobile containment. Switching to the member-reader account kept the
Teams deep URL while hiding its private table and showing independent authority
guidance. Browser inspection produced no additional native dispatch or changes
to captured audits, memberships, ciphertext or history. Five rows are not proof
of the default 20-row Load More workflow; that boundary has source/driver tests.
The owned tab closed, viewport reset and Compose resources/listener were
independently absent. All 164 protected paths remained exact. A fresh standard
complete 89-case-per-driver PostgreSQL/MySQL matrix is running; commit/push remain
pending its success.

### Member Teams complete local acceptance passed, 2026-10-05

The fresh standard `go tool task test-integration` passed (exit 0) after the
value-comparison fixture correction: the unchanged ordered 89-case harness ran
against PostgreSQL and MySQL, Handler 1526.419s. Configuration 1.744s, database
1.630s, errors 1.467s and service 8.107s also passed. Its exact owned Compose
containers, networks and volumes were independently absent. All 164 protected
source hashes, V53 and the checked production binary remain exact. Together
with source format/check/test/build (2302 frontend cases/120 files), R6 focused
regression, mandatory check, both-driver auth/process lifecycle and R3
native/browser/restart acceptance, this phase is ready for a scoped main commit
and push. Previous failed fixture/helper runs remain explicit historical
evidence. Remote delivery and CI are not yet claimed.


## Administrative Member Models source preparation

The Models tab gives `?tab=models` two compact Personal-grant tables:
retained authorized Models and other candidates, with local Add/Remove followed
by one reason review and explicit Save. The nine cells show Model name/ID, Type,
Providers, native protocols, availability, input/output base prices, creation time
and semantic update time. Type and update time remain Unknown: neither has a
current declaration. Provider labels require independent `providers.read`; prices
require independent `prices.read`. Exact decimal strings, zero, disabled prices,
missing rates and heterogeneous schedules remain distinct. No directory or
per-row lookup is introduced; the complete union is bounded at 1,000 Models.
Current active Team-only candidates are excluded without exposing Team names.

The `GET /api/v1/admin/members/:user_id/models` accepts current
`members.read` OR `members.models.write` for its minimal scoped metadata. The
Member page still requires fresh `members.read`. Direct replacement through PUT
requires explicit `members.models.write`, an exact enabled/nonoffboarded target,
strong reviewed If-Match and reason. This platform permission may edit self,
administrator or member targets; no role label grants implicit authority. Existing
Personal-request review keeps its separate nonself approval rule.

A successful matching PUT confirms only `current_model_grants` and current
runtime application. It is not a historical operation receipt. Failed publication
can leave saved rows uncertain; metadata GET and later matching configuration do
not prove the original write. The UI retains its original body, reason and
If-Match through same-target renewal/error and rejected retries, and explicitly
retries with current CSRF. Current cache generations gate dispatch and callbacks;
private rows, actions and dialogs hide during renewed reads. Actor, target, logout
and tab changes destroy local intent. Request history reuses parent authority
without another Session observer and retains its independent receipt semantics.

The reviewed backend (24 leaves), frontend (15 leaves) and two real-driver
fixtures are now integrated on the checked Member Teams baseline. Root registered
V54 and both routes, retaining all earlier 89 harness entries before the two new
entries (91 per driver). Isolated source race/static/pinned lint and 182 related
frontend tests passed. Integrated checks/build, real PostgreSQL/MySQL and controlled
native/browser/restart acceptance passed; the checked delivery was committed and
pushed as `569abcd`. The accepted Member
Teams evidence remains unchanged.
F04/F19 remain Partial; formal totals stay 11 completed, 16 partial and three
unstarted. See [Personal requests](PERSONAL_MODEL_REQUESTS.md#direct-personal-grant-editor-source-preparation)
and [Database](DATABASE.md#personal-model-grant-revisions-v54-source-preparation).


Member Models passed focused dual-driver acceptance and the complete unmodified
ordered 91-case-per-driver race matrix, including V54 lifecycle/constraint tests,
plus both-driver authentication/process restart. Source checks, Go race,
development/production assets, production build and fresh 2346 frontend tests in
123 files passed. Controlled process/native/browser/restart acceptance passed ten
native completions and four zero-dispatch denials, genuine publication failure,
Personal-only reduction, unchanged Team/Project authority, old Key ceilings and
zero-write current-state retry. Browser evidence covers bilingual tables, retained
drafts, reason validation, independent permissions and responsive containment;
browser grant writes are not claimed. The 197-path successor differs from the
full-matrix floor only in a tested Actor fixture. Detailed failed and successful
checkpoints remain in [Implementation](IMPLEMENTATION.md#member-models-actual-acceptance-2026-10-05).
Final mandatory check passed, followed by checked main delivery `569abcd`. F04/F19 and formal
11 complete/16 partial/3 unstarted remain unchanged.


Member Models current confirmation tolerates only a bounded busy snapshot read
after the committed transaction and one successful publication. Every fresh
capture reauthorizes the actor and exact target/set; explicit non-application,
authority changes, cancellation, deadline, stopped or expired runtime fail
conservatively. It does not replay writes or establish a historical receipt.
Focused dual-driver R4, complete integration, authentication and controlled
native/browser acceptance passed; current-state confirmation remains distinct
from historical operation evidence.


### Stable Member Model review revisions

The strong review ETag binds the actor, target identity and lifecycle, Personal
grant revision and provenance, Team exclusion basis, and complete persisted
Model, supply, credential coverage and egress configuration. Transient runtime
mutex contention, lease state and the resulting availability/protocol projection
do not change that configuration revision. A brief busy observation therefore
cannot make an otherwise identical reviewed reduction conflict.

Readiness remains independent: every addition is checked against the current
runtime projection at dispatch. Unknown, expired, tombstoned or unpublished
routes remain nonselectable. Real configuration or authorization changes still
invalidate review, and postcommit confirmation still requires current grants and
runtime application. No mutation or publication is repeated by this correction.

The correction passed deterministic contention and fresh-addition tests, source
race tests, and mandatory checks. The fresh complete ordered integration matrix
passed all 91 scenarios per database and eight pre-loop constraints on PostgreSQL
and MySQL. Both-driver authentication, persisted sessions, process restart, and
gateway lifecycle checks also passed. Owned test resources were independently
verified absent. The earlier CI and local contention failures remain recorded in
[Implementation](IMPLEMENTATION.md#own-team-export-regression-and-member-review-correction-2026-10-05).


## Member basic information

The existing Member Settings Basic information card edits the retained name;
email remains read-only. Resource-scoped `GET /admin/members/:user_id/metadata`
returns only user ID, name, lifecycle status, server-owned editability and a
strong ETag. It accepts independent `members.read` or `members.write`; the parent
Member page still requires fresh `members.read`. PUT requires `members.write`,
CSRF, the reviewed If-Match and exactly name/reason. Delegated members cannot edit
self or administrator targets; an explicit administrator may. Disabled targets
remain editable, while offboarded targets conflict. Actor and target identities
are exact regardless of database collation.

A changed name and typed before/after/reason audit commit atomically. Names use
bounded Unicode labels, not a generic profile update: email, roles, enabled state,
Keys, grants, quota and runtime publication are unchanged. An identical current
name is a zero-write confirmation even with an older validator; a stale differing
name conflicts. Fresh postcommit authority and current-name confirmation may fail
without replaying the write. Confirmation means current member name only, never
a historical operation receipt. Metadata GET cannot resolve original uncertainty.

The Base UI confirmation retains reviewed name, reason and If-Match through
incidental renewal, errors and uncertain/rejected retries. Explicit conflict
review requires abandoning the old intent before adopting a newer revision.
Actor, target, logout and tab changes clear the draft. The existing Settings
composition, independent role/lifecycle controls and one Session observer remain.
Implementation is integrated after checked own-Team CSV. Source checks passed.
The first dual-driver focus used an invalid empty-model Personal Key fixture and
is not acceptance; its explicit-model/grant repair passed both drivers while
preserving all Key/catalogue invariance assertions. Metadata also passed both
children in the full matrix, whose existing Member Models price observation failed
and requires a separate test timing repair. Authentication/restart and controlled
production/browser acceptance passed: bilingual drafts, stale-review rejection,
explicit fresh review, one UI save, typed audits and unchanged protected facts.
The controlled Metadata process dispatched no inference; owned resources and
listener were empty. After the separate test-only Member Models observation repair,
the fresh complete matrix passed all92 scenarios per driver and eight constraints,
with five test-bearing packages complete, no skips/failures, unchanged239 protected
paths and independently empty owned resources. Twenty deterministic race repetitions
and three real Member Models repetitions per driver also passed. The checked delivery
is represented by the commit containing this record; remote CI remains independent.
F04 and formal totals remain unchanged.

## Member list summaries

The existing Member table retains its eleven columns: name, email, status, Teams,
monthly Personal Tokens, monthly Personal money, Personal Keys, recent login,
creation time, update time and actions. Existing compact filters and action menus
remain. Resource-scoped listing requires a current exact enabled, nonoffboarded
actor with `members.read`; write actions retain independent permission checks.

`GET /admin/members` supports literal name/email search, active/disabled and base
role filters, stable exact-ID cursor paging, default 40 and maximum 100 rows.
Reject unknown, duplicate or malformed selectors. Return an actor-bound page,
observation timestamp, platform denomination and private no-store response.
Batch retained roles, all-status Personal Key counts and monthly Personal policies/
journal accounts; Project Keys and Team usage cannot become Personal totals.
Stored policy, inherited/default limits, zero, unlimited, known usage, coverage,
unknown retained holds and live reservations remain distinct, with exact decimal
and integer strings. Runtime application requires current server evidence.

Team metadata requires independent `teams.read_all`; a denied section is null
and performs no Team hydration. Complete retained Team memberships are bounded
at 1,000 per page; overflow is explicit and never a truncated complete list.
Direct role relationships are bounded at 10,000. Bound batch queries and complete
identity hydration rather than issuing requests for each row. Historical recent
login remains null/unknown in this phase; never derive it from retained Sessions.
The separately queued recorded-login migration will change that contract only
after its own accepted delivery. Selected-language dates and live translations
preserve search inputs. Fresh actor, Session and filter generations hide obsolete
rows/actions and reject late pages without a global directory fallback.

The checked list phase preserves V54 and all previous 92 scenarios, appending
only `member_list_summary`. Final source tests passed 2,486 frontend cases in 129
files, four Node tests, two development lifecycle tests, Go race and production
assets. Focused dual-driver tests, both-driver authentication and controlled
four-call native/bilingual browser/restart acceptance passed. The complete final
matrix passed all 93 scenarios per database, eight constraints and all five
test-bearing packages (Handler 1616.250s), with unchanged protected source and
independently empty owned inventories. Mandatory checks passed before phase
submission. Earlier failed fixtures and browser findings remain historical in
Implementation; F04 and formal completion totals remain unchanged.

Clients retain the authoritative `user_<id>` Personal account identity and
recorded RFC3339 offsets. Historical names are bounded escaped text rather than
revalidated creation input; empty names use the recorded email as accessible
link/action context. English-default/live-Chinese browser checks confirmed all
columns, literal search, role filtering/clearing and independent Team access.
Actual tooltip activation remains separately unverified; source tooltip tests
retain their scope. No whole User/Session/catalogue preservation is inferred
from the eight protected historical table digests.

## Member Overview effective Models

The existing Overview retains account cards, Access information and the effective
Models table. Read the exact bounded union of direct Personal grants and active
Team grants; the Team portion requires independent `teams.read_all`. A denied
Team section reveals no Team IDs, names, SQL hydration or implied complete union.
Separate `providers.read` and `prices.read` govern metadata and rates. Effective
rows are advisory current configuration, never Key ceiling expansion or native
invocation proof. Current readiness requires retained exact publication, lease,
credential/cipher continuity and route evidence; unknown remains unknown.

Keep distinct source identities, complete bounds (100 Teams, 1,000 Models and
5,000 Team relationships), explicit overflow and exact decimal price strings.
No per-row directory requests or credential decryption occur. A ready route
cannot establish successful inference or admission. The table has no edit actions
and does not mutate grants, Keys, quota or call facts. Fresh parent Session,
permission, target and read generations hide stale rows and portals.

The 26-path source packet is integrated on checked Member list `af9c22e`, with
V54 unchanged and one scenario appended (94 per database). Source and root-owned
real-driver, zero-inference process/bilingual browser/restart and full regression
acceptance passed on the corrected source, as recorded below. Frozen successor source does not change F04 completion.

Effective Models source gates passed on checked List main: mandatory check,
2,546 frontend cases in 131 files, four Node tests, two development lifecycle
tests, Go race and embedded production assets (1.686s). The freshly built artifact
SHA256 is `bf094619eadcf09335246e8083914c1130142c5b0412569a041ee3d5ea4158f0`.
The Task build's npm install removed optional Linux libc lock metadata without
changing dependency versions; root saved the diff and restored only that owned
build-generated edit to the exact checked lock. All 268 protected source paths
were confirmed exact afterward. Actual driver/process/browser/full gates remain
pending. Source acceptance is distinct from actual runtime or phase delivery.

The first actual Effective Models focus passed five lifecycle children and all
eight constraints, but PostgreSQL's alias-membership fixture violated the real
User foreign key. MySQL passed the alias case. Owned resources were independently
absent and all268 protected source paths remained exact. This run is not accepted.
The one-leaf test-only repair creates a disabled case-variant User parent via
GORM where distinct primary keys are supported; portable `gorm.ErrDuplicatedKey`
handles collation folding. Restore the canonical membership before removing only
the exact independently created alias. Foreign keys, production authorization,
negative alias exclusion, query budgets and immutable/no-dispatch checks remain
unchanged. Handler source race passed; corrected actual acceptance is pending.


### Effective Models real focus and browser correction, 2026-10-05

The corrected PostgreSQL/MySQL focus passed all six selected lifecycle children
and eight constraints, with every nested test terminal verified and all 268 source
paths unchanged. The earlier PostgreSQL fixture foreign-key failure remains a
failed run. The first production/browser run timed out awaiting its explicit
300-second browser release during context continuation; owned resources were
independently absent afterward, and that run is not accepted.

The next unchanged-artifact process passed its Personal-only and authorized
three-model union, exact 18-place prices, independent permissions, no inference
and restart checks. All 20 protected table digests stayed exact; no native POST,
CallRecord, CallAttempt or post-setup upstream request occurred. English/Chinese
browser reads confirmed the data but exposed nowrap price spans overlapping
adjacent 180px columns and retained model identifiers overflowing 220px cells.
Browser acceptance failed; restart browser views and denied browser identities
were not completed in that run. Owned tab, services, Compose resources and port
were removed before source repair.

Wrap the exact monetary string and retained model identifier inside the existing
fixed cells without changing columns, layout, amounts or API contracts. Source
verification, a newly built artifact and new bilingual process/browser/restart
acceptance remain required. Earlier 2,546 source tests and artifact bf094619 are
historical evidence for the pre-correction source, not the corrected delivery.


### Checked Member effective Models acceptance, 2026-10-05

The corrected source retains the existing Overview cards and ten-column table,
with exact monetary values and model identifiers wrapped inside their fixed
cells. Format, mandatory checks, all 2,546 frontend cases/131 files, four Node
tests, two development lifecycle tests, Go race and production assets passed.
The corrected production artifact SHA256 is
`ce427279babb61a72a2c451d68e821d4e543d1aa30fc9def1c76cc43efeb88c1`.

Both-driver focused acceptance passed six lifecycle children and eight
constraints. The corrected artifact passed controlled production and browser
acceptance: Personal-only versus complete authorized three-model union, two
distinct shared-model sources, independent metadata/rate permissions, exact
18-place input/output amounts, English/Chinese live switching and cell geometry,
write-only and Team-only read denials, and same-artifact process restart with
bilingual reopened reads. Console warnings/errors were empty. No native POST,
CallRecord, CallAttempt or post-setup upstream request occurred; all 20 protected
table digests stayed exact. Session and whole User/catalogue preservation are
not inferred from those table digests. Availability remains advisory current
configuration, never Key ceiling expansion, admission or native completion.
Actual source-tooltip activation was not separately verified.

The complete unchanged-source matrix passed all 94 ordered scenarios on both
PostgreSQL and MySQL, eight pre-loop constraints and five test-bearing packages,
with no failed or skipped tests. Handler 1644.881s; Service 10.804s. All 268 source
protections and the exact 29-path dirty boundary remained unchanged during
acceptance. Log SHA256:
`fe14a67f171068ed0e497273aafd56b61ee73f682da32f43711336836df00263`.
All owned containers, networks, volumes, browser tabs and application listeners
were independently absent after cleanup. The earlier foreign-key fixture
failure, operator-pause timeout and pre-correction browser overflow remain
historical failed runs above; they are not passed evidence.

Checked List main `af9c22e` now has successful exact CI 37252847114,
Actionlint 37252847110 and GolangCI-Lint 37252847145. Final mandatory checks passed. The
checked Effective Models phase is represented by the commit containing this
record; new remote workflows remain independent. F04 and overall 11/16/3 totals stay
partial/unchanged. Continue recorded recent login, Access, Roles and State;
registration approval, role definitions/templates and Team-role review remain
separate unfinished requirements.


### Recorded recent successful login integration, 2026-10-05

The reviewed 36-path slice is integrated onto checked, pushed effective Models
main `88e8480`. All unowned accepted source remains exact. Additive frozen GORM
V55 adds nullable microsecond recent-login history without synthetic backfill.
Only a completed password or MFA sign-in records the timestamp atomically with
its Session. Setup, registration, administrator creation, challenge issuance,
failed proofs, Session reads and security Session replacements do not record a
sign-in. The existing Member list and Overview display the recorded value or
explicit historical unavailability, with English/Chinese formatting. Session and
creation DTOs retain their existing shape. Metadata and grant revisions remain
independent.

The source protection floor contains 282 paths. The original 94 ordered
integration scenarios remain unchanged; exactly two Recent Login scenarios append
for 96 and V55. Source checks, real PostgreSQL/MySQL migration/authentication/MFA,
controlled zero-inference process/browser/restart and complete integration gates
are pending. This integration is not accepted delivery. F04 remains Partial and
formal totals remain 11 complete, 16 partial and three unstarted.


The first real Recent Login focus is not acceptance: PostgreSQL rejected a
historical fixture's cached SELECT-star result after deliberate column
reconstruction; both drivers rejected three obsolete Session paths. The run
exited one, eight constraint checks passed and only one lifecycle completed.
Owned containers, networks and volumes were independently absent. A two-file
test-only correction explicitly selects the frozen historical GORM fields and
uses the registered `/api/v1/auth/session` path in all three reads. All original
history, rollback, status and revision assertions remain; production source,
V55 and the original ordered 94-case prefix are unchanged. Corrected source and
actual acceptance are pending.


### Recent Login focused and production acceptance, 2026-10-05

Source gates passed: mandatory check, 2,620 frontend cases in 132 files
(103.56s), four Node tests, two development lifecycle tests, Go race and production
assets (1.692s). The two fixture-only repairs subsequently passed fresh focused
Go race source checks. The corrected real PostgreSQL/MySQL focus passed all four
lifecycle children and eight constraints with no failed/skipped test; migration
children took 1.47s/3.29s and login children 13.15s/14.84s. Both-driver real
authentication, persisted Sessions, gateway lifecycle and restart also passed.

The unchanged production artifact SHA256
`a20bc2bd8010c15ad438ec3709a9a96546225fd9ea4c72aecd2aceafd95e1bf6`
passed controlled Recent Login process/browser/restart. The helper performed
exactly five successful password sign-ins and one genuine MFA completion; the
browser's separate reader sign-in is outside that helper count. Failed proofs,
challenge issuance, Session renewal, password/security Session replacements,
setup and registration did not create another login record. Recorded subject
and historical/registered NULL history remained exact through the same-artifact,
config, database and journal restart. Public Session/create/challenge DTO shapes
and private Member detail/list parity passed.

English-default/live-Chinese Member list and Overview showed recorded time and
explicit historical unavailability, with read-only controls. Restart renewed real
Sessions, hid private content during renewal and restored fresh authorized views.
After reader-role revocation, English/Chinese detail and the English directory
denied access without private cards/rows; the server also rejected the existing
Session's detail/list reads. Console warnings/errors were empty. Twelve
screenshots were retained separately. Source tests, rather than browser evidence,
cover same-millisecond/late-response races, equal/backward clock and changed-row
reconciliation.

This Recent Login process dispatched zero inference POSTs and retained zero call
records/attempts; the separate authentication regression includes its own native
gateway tests. All 282 source protections remained exact during the actual runs.
Owned focus/auth/process inventories and the IPv4 application listener were
independently absent after teardown; only the temporary browser tab was closed.
The complete unchanged-source 96-case-per-driver matrix and final mandatory
commit checks remain pending. Earlier failed focus remains historical. F04 and
formal totals are unchanged; continue Access, Roles, State and registration
approval after checked delivery.

### Recent login full-matrix fixture correction, 2026-10-05

The first complete V55 matrix failed only the historical V54 migration fixture
on both drivers: its fixed total of 54 rejected the correct 55-version ledger.
Each driver passed 95 of 96 ordered lifecycle scenarios, including both new
Recent Login scenarios. This failed run is retained as evidence, not acceptance.
The test now compares the exact incoming ordered version list before removal,
after removal, concurrent replay and repeated migration; V54 must remain unique.
All historical data, constraint and partial-DDL assertions remain. The whole
harness still requires exactly 55 migrations. Corrected focus and full rerun are
pending; no product or released migration changed in this correction.

The corrected three-child focus passed on both databases: six lifecycle children
and eight preloop constraints, exact parent/package completion, no failure or
skipped child, and independently confirmed owned resource absence. The complete
96-case rerun and final commit checks remain pending.

### Recorded successful login complete acceptance, 2026-10-05

The corrected unchanged-source V55 matrix exited 0 with all 96 ordered lifecycle
scenarios on each of PostgreSQL and MySQL, eight pre-loop constraints and all
five test-bearing packages complete. No named test failed or skipped (2,662
named tests). PostgreSQL took 714.160s, MySQL 919.720s, Handler 1638.068s and
Service 10.790s. The original 94-case prefix and the two appended login scenarios
remained exact. Full log SHA256:
`7cd48573f2af38c3afd1e553bc59a3e68a0077f69786f3fc77507c162efe154c`.

All 282 protected paths, reviewed 40-path dirty scope, main HEAD, empty index and
production artifact remained exact across the run. Owned containers, volumes
and networks were independently absent after cleanup. The log reviewer was
corrected to require the exact four map-derived constraints once each before
the strictly ordered lifecycle matrix; Go map iteration does not define their
order. Missing-constraint and duplicate-constraint negatives passed. This
reviewer-only correction changed no product or test source. Earlier failed
focus and historical V54-fixture full run remain recorded above, not accepted
evidence. The corrected six-child focus, complete source tests (2,620 frontend
cases in 132 files), both-driver authentication/native persistence, and the
separate zero-inference production/MFA/bilingual browser/restart proof remain
independent passed gates. Final mandatory `go tool task check` passed with no
lint errors and two existing Fast Refresh warnings. The containing scoped main
commit is the checked delivery; push and remote workflows remain independent.

Recorded login means an actual committed password or MFA sign-in. Historical
NULL remains unknown; Session reads, setup, registration and security replacement
do not manufacture login history. F04 remains Partial and formal totals remain
11 complete, 16 partial and three unstarted. Continue Access, Roles, State and
local registration approval; queued source packets are not accepted delivery.

### Member Access summary integration, 2026-10-05

The reviewed 28-path joint R2 is carried contextually onto checked recent-login
main d51a52e. Fourteen new leaves extend the protected floor from 282 to 296;
all unowned predecessor code, the three corrected migration/login fixtures and
paired Recent/Effective wrapping rules remain exact. There is no migration or
writer: V55 and the original 96 order remain unchanged, with one Access scenario
appended for 97. The existing Overview uses its six approved Access cards and
header facts; Role and Team names each require independent read permission.
Denied metadata stays unknown, overflow fails the complete read, and renewed
actor/target reads hide stale private facts. English/Chinese copy is paired.

Source, focused and complete real-driver, production/bilingual browser/restart
and final mandatory gates are pending. The reviewed R3 root helpers require
the accepted 282 predecessor and exact 296 source, preserve every unowned code
hash, and permit only explicitly reviewed three-document updates. The process
is zero inference; its selected older List fixture separately performs four
genuine native calls. Queued Roles/State and isolated local-approval source
work remain unaccepted. F04 and formal 11/16/3 remain unchanged.

### Member Access source and production acceptance, 2026-10-05

Current-main format and mandatory checks passed, followed by complete source
tests: 2,684 frontend cases in 135 files, Go race tests, four Node checks,
two development lifecycle tests and production assets. The actual PostgreSQL
and MySQL focus passed eight lifecycle children and eight pre-loop constraints
with complete parents and no failed or skipped child. The Access slice remains
read-only, at V55 with 97 ordered lifecycle scenarios.

The same production binary (SHA256
`68425d9692fc61d9f0b5299e380c981b4e41efcd5cb63a4b5dcac14ac5201972`)
passed controlled read-only process and English/Chinese browser acceptance for
independent Role/Team metadata permissions, writer-only denial, real summaries,
and hidden private facts during renewed reads. A same-artifact, database,
configuration and credential-root restart retained the validated browser Session
and passed both languages again without replaying login. Eight screenshots were
captured; console warnings and errors were empty. This process dispatched zero
inference requests and created zero calls or attempts. Original Sessions, grants,
roles, Teams, policies, catalogue and history remained unchanged. The older List
fixture selected by the focus separately performs four genuine native calls.

Two initial process attempts failed the helper's restart Session baseline and
remain historical failed evidence. The corrected helper captures the complete
validated pre-restart Session set, retaining original Session equality and
rejecting unexpected post-restart Session creation. A separate helper-only
registry check now scopes its assertion to the current migration registry;
legacy SQL has an independent append expression. Neither correction changed
product source or the production binary. Actual tooltip activation was not
separately verified; historical-null update timestamps and incidental renewal
races have source coverage only. Owned containers, networks, volumes, listeners
and the temporary browser tab were independently confirmed absent.

The complete unchanged-code 97-case-per-driver regression and final pre-commit
check remain pending. F04 stays Partial and formal totals stay 11 complete,
16 partial and three unstarted. Continue reviewed Roles, State and registration
approval after this checked slice; source packets alone are not delivery.

### Member Access complete regression, 2026-10-05

The unchanged-code complete V55 matrix exited 0 with all 97 ordered lifecycle
scenarios on each of PostgreSQL and MySQL, eight pre-loop constraints, all
five test-bearing packages complete and no failed or skipped named test
(2,707 named tests). PostgreSQL took 773.670s, MySQL 973.960s, Handler 1751.999s
and Service 11.221s. The original 96 prefix and appended Access scenario remained
exact. Full log SHA256:
`1aae2c7abb01ea85c585fbe54ab1a3923a5a27cce823f9a0315c0391e3dfd3f5`.

All 296 protected paths, the reviewed 31-path dirty scope, main d51a52e, empty
index and production artifact stayed exact during the actual run. Owned
containers, networks and volumes were independently absent after teardown.
The complete source tests (2,684 cases/135 files), both-driver focus (eight lifecycle children/eight constraints) and controlled zero-inference
production/bilingual browser/same-Session restart gates remain independently
passed. Final mandatory `go tool task check` passed with zero lint errors and two
existing Fast Refresh warnings before the scoped main commit.

Recorded-login predecessor d51a52e now has all three exact remote workflows
successful: CI 37264974282, Actionlint 37264974348 and GolangCI-Lint 37264974195.
These remote results apply to that predecessor, independently of this delivery.
F04 and formal 11/16/3 remain partial/unchanged. Next integrate the reviewed
Roles joint, including its two-leaf failed-request retry fix, then State and
local approval. Their isolated source proofs do not establish actual acceptance.


### Reviewed member Roles integration, 2026-10-05

The 43-path reviewed joint is integrated onto checked Access delivery 9205ca5.
The existing member Roles tab now reads bounded assignments and independently
authorized definitions, retains reviewed additions/removals, and confirms a
complete replacement with a reason, definition proofs and strong If-Match.
Current-state retries retain the original request; they do not prove a historical
operation or runtime enforcement. A failed request releases its submit lock so
an explicit identical retry remains available. English and Chinese are paired.

Frozen GORM migration V56 records private member-assignment and role-definition
revisions. The original 97 lifecycle cases remain in order, followed by the
revision migration and reviewed Roles scenarios (99 per driver). Existing
internal fixture assignment shares the writer engine; the public legacy writer
is replaced by the reviewed route. Measured full-matrix duration of 1751.999s
justifies a finite 35-minute full test deadline with all race and assertion
checks retained.

Current-main source checks, both-driver migration/lifecycle, authentication
persistence, production bilingual browser/restart and the complete regression
are pending. Source-only preparation is not accepted delivery. F04 remains
Partial; formal totals remain 11 complete, 16 partial and three unstarted.
Continue reviewed account State, local registration approval and the repository
price source after this independently checked phase.


### Member Roles source and production proof, 2026-10-05

Mandatory source checks passed with zero lint errors and two existing Fast Refresh
warnings. Complete source tests passed 2,747 frontend cases in 138 files, Go race,
four Node checks, two development lifecycle tests and production assets. The
production binary SHA256 is
`0186b40e5c43b6741f9cfcd7a1a976a8e50e8156f36e793ab4e974e261169be9`.
Real-process authentication, native gateway and restart tests passed on both
PostgreSQL and MySQL with independent owned-resource cleanup.

The separate production Roles process and browser passed English/Chinese
combined-reader controls, scoped permission details, administrator add/remove
drafts, retained language-switch drafts, required reason and one complete
confirmed replacement. The identical original review confirmed current state
with zero additional writes or audits. Same-artifact/database/configuration/root
and surviving-Session restart passed both languages without another login.
Seven screenshots were recorded; console warnings/errors were empty. This
Roles process dispatched zero inference requests, calls or attempts. Original
Sessions, other assignments and protected facts were retained. Single-read and
delegated-writer denials were actual API checks, not separate browser scenarios.
Browser transport loss, ABA and bounds remain source/driver coverage only.

The first real-driver focus failed two fixture assertions on each driver. GORM
map scanning of a declared pointer string left a pointer value unhandled by the
fixture; typed single-column Pluck with exact-one cardinality replaces that
reader. A malformed-ID request accidentally appended whitespace to the literal
route; it now targets the resource ID and separately preserves literal-route
404 coverage. Every migration preservation, partial-DDL, concurrency, constraint,
authority and mutation assertion remains. Product source and the production
binary are unchanged by these two test-only repairs. The failed run remains
failed historical evidence with confirmed cleanup. Corrected both-driver focus,
complete 99-case regression and final mandatory checks remain pending.


### Corrected Roles focus and full-regression failure, 2026-10-05

The corrected PostgreSQL/MySQL focus passed all eight lifecycle children, eight
constraints and complete parents with zero failure or skip. Independent owned
container/network/volume inventories were empty. Report SHA256:
`13f5aa58c6afdb81797b1a2753a314b5acf8c04a1089fa23adf71e92600d9227`.
Access delivery 9205ca5 now has all three exact remote workflows successful:
CI 37269675399, Actionlint 37269675308 and GolangCI-Lint 37269675118.

The first complete Roles regression recorded a genuine existing
`project_initial_resources` failure. Its historical case-aliased permission
records are intentionally unknown and never confer direct Project authority.
The new member-role projection rejected those safe uppercase recorded codes
while hydrating the administrator candidate catalogue, blocking an unrelated
valid role revocation. This is a production compatibility regression, distinct
from the two earlier fixture errors. The failed owned Handler test was terminated
after the failure was recorded; the partial run is not a completed matrix.
Runner exit was 1, all 322 source paths and the artifact stayed exact, and owned
containers/networks/volumes were independently absent. Raw failed log SHA256:
`5f186415178859a0b9ec09021c97fa0064990ef41312c8cf2c171178fc163284`.

A bounded projection/API repair and regression tests are in preparation. They
must retain original permission-code casing and exact implemented permission
matching, with unknown codes excluded from usable authority. The historical
negative Project fixture remains intact. New source/build, relevant driver
focus, production and complete regression gates are required before this phase
can be submitted. F04 and formal totals remain Partial/unchanged.


### Recorded permission compatibility correction, 2026-10-05

The reviewed four-file repair is integrated. Bounded safe recorded permission
codes retain their exact ASCII case in definition reads and scoped API decoding.
Implemented permission unions and new-role writes still require exact catalogue
membership; uppercase aliases grant no authority. The unchanged historical
Project initialization fixture will be replayed on both drivers. Original-code
regressions reproduced three Go failures and four API decoder failures; corrected
isolated source tests passed all 13 selected Go race tests and 45 API cases.
These source results do not accept the actual driver or production repair. New
source/build, expanded driver focus, production/restart and complete 99-case
regression gates remain pending; the previous failed run remains failed evidence.


### Corrected Roles current-main gates, 2026-10-05

The four-file compatibility repair passed mandatory checks and complete source
tests: 2,761 frontend cases in 138 files, Go race, four Node checks, two development
lifecycle checks and production assets. New production artifact SHA256:
`06ca34d828505ac7306f82ecfe5b14d96875efc8ad7360056903c761b1154e50`.
Expanded PostgreSQL/MySQL focus passed all ten lifecycle children, including the
unchanged Project initialization negative-permission fixture, eight constraints
and complete parents/package without failure or skip. Report SHA256:
`4b218e037da0cbeca931d56111857873078b3e6c4fd091e4a202495ed4b80944`.

The repaired production process and bilingual browser passed one reviewed
replacement, required reason, retained language-switch draft and exact original
current-state retry with zero additional writes/audits. Same-artifact/database/
configuration/root and original browser-Session restart passed without another
login. Seven screenshots and empty browser warning/error observations were
recorded; native calls/attempts remained zero. Original Sessions, other
assignments and protected facts were preserved. A preceding browser run exceeded
its finite five-minute checkpoint before submitting; it remains failed evidence
with independent cleanup, and the successful rerun used unchanged code/artifact.

Both-driver authentication lifecycle again passed initialization, persistent
Sessions, logout/revocation, encrypted provider, model grants, Keys, ordinary/
streaming native calls and restart. Every owned resource/listener was independently
absent after teardown. All 322 source protections stayed exact through these
gates. Complete 99-case regression and final mandatory checks remain pending;
no Roles delivery or whole-F04 completion is claimed. Formal totals stay 11/16/3.


### Checked Member Roles complete regression, 2026-10-05

The corrected complete regression passed all 99 ordered lifecycle scenarios on
each real database, eight pre-loop constraints, five test-bearing packages and
2,790 named tests with no named failure or skip. PostgreSQL took 762.050s,
MySQL 984.010s and Handler 1750.486s. Exact log SHA256:
`637ea40b9db486e1dc5b7cb3b4f92fd620ca60b268d4842912c6ef98f5515966`.
The runner exited zero, all 322 protected source paths and the production artifact
remained exact, and independent container/network/volume inventories were empty.
Both drivers retained the historical case-alias authority denials while allowing
the unrelated valid role clear. Prior fixture, incomplete full-run and finite
browser-checkpoint failures remain historical failed attempts, not passed gates.

Together with current source2761/138, expanded focus10+8, both-driver
authentication/native persistence and new-artifact bilingual browser/current
retry/same-Session restart, this accepts the bounded member Roles implementation.
The containing commit is the scoped checked Roles delivery after final mandatory
checks; commit/push/remote read-back remain separate coordinator observations.
Global Role definition editing, reviewed account State and local registration
approval remain independent follow-up work. F04 and F05 remain Partial; formal
11-complete/16-partial/three-unstarted totals are unchanged.

## Reviewed Member State integration — historical preparation checkpoint

The existing Member Settings cards and list status menu now use an exact-target
state review and strict one-field PATCH with a reason, strong If-Match and local
Base UI confirmation. Read and write authority stay independent. A changing
review detects private revision ABA and metadata label changes; identical desired
state can confirm current state with zero writes or audits after fresh authority.
No historical operation receipt is returned.

Base-role success confirms current database identity. Account-access success
requires the server's current bounded runtime lifecycle proof. Disable revokes
Personal Keys, Sessions, MFA challenges and pending member requests atomically
with the typed audit. Ordinary enable and reactivation never revive credentials;
reactivation clears only the current marker and retains offboarding history.
Self-disable/demotion can commit before a fresh response returns 401/403, so the
UI preserves uncertainty according to current authentication and authority.

The 37-path source is carried on checked Roles a44838d, preserves frozen V56
without another migration, appends only the 100th driver scenario, and extends
source protections from 322 to 343. Fifteen legacy HTTP fixtures use explicit
reviewed test adapters; ordinary trusted service callers retain their shared
mutation engine. The English/Chinese UI, retained retry intent, private headers,
continuity and last-admin guards require actual source/dual-driver/authentication/
production browser/restart acceptance before delivery. No pending gate is claimed
as passed; F04/F05 and formal 11/16/3 remain unchanged.

## Member State source, focused database and production acceptance

Current carried State source passed format, mandatory checks, Go race, 2,815
frontend cases in 140 files, development lifecycle, production asset tests and
production build. PostgreSQL and MySQL focused acceptance passed all 16 selected
lifecycle children, eight pre-loop constraints and 40 nested children without
failure or skip. The complete 343 protected hashes stayed exact and owned
Compose containers, networks and volumes were independently absent.

Production SHA256
`b94eef3cdc1db3cbbc6b58749599732d6fd6b7f3203e8b7183292de390da3369`
passed one actual reviewed browser disable, required-reason validation, bilingual
read-only cards and a surviving-browser-Session restart with no new login. Five
explicit typed State audits covered base promotion/restoration, disable, enable
and reactivation. Current-only original retry added no writes/audits; later
differing state rejected that retry. Old Session cookies and the revoked Key
stayed denied, while the completed offboarding case remained unchanged.
Seven screenshots and empty browser warning/error observations were retained;
zero native dispatches, call records or attempts were created. Delegated/outsider
denials were independent API checks; self-commit and publication-fault assertions
remain source/driver evidence. No transport-loss or original-operation receipt is
claimed. Owned ports were reusable, the test tab closed and English restored.

The build's optional Linux libc metadata drift was restored after exact JSON
comparison proved only 18 metadata removals and no package/version change. An
initial blank browser page loaded after deliberate reload before sign-in; no
product correction is claimed. Both-driver independent authentication/native/restart
passed; the owned project inventories were independently empty.

Complete regression then passed 100 ordered scenarios per PostgreSQL/MySQL,
eight constraints, five test-bearing packages and 2,866 named tests with no named
failure or skip. PostgreSQL took 786.760s, MySQL 1015.930s and Handler 1807.116s.
Log SHA256: `6de49d8751725622ef7278c01920be37f49cd8e66625c4b30d684cfe8a7aed0c`.
All 343 protected paths remained exact and the full-run project's containers,
networks and volumes were independently absent.

A four-file frontend follow-up, plus its parent fixture adaptation, retains the exact original request after every
failed dispatch, including a first 409 that may follow a durable commit. Matching
reads, Cancel and Escape never resolve uncertainty. Explicit Abandon discards only
local retries, retains the draft and requires current review/new confirmation;
the original outcome remains unknown. Private real-QueryClient RED/GREEN evidence
passed 21 State tests, including fresh CSRF, failed renewed reads, obsolete actions,
bilingual guidance and pending-request abandonment denial. Backend, schema,
authentication and the complete integration harness remain byte-identical to the
accepted regression source. The earlier browser/restart evidence retains its
original artifact hash; no new real transport-loss or first409-after-commit
experiment is claimed. The additional parent-governance fixture retains its
continuity/end-state assertions and explicitly abandons before a new review.

Corrected complete source passed 2,817 frontend cases in 140 files, Go race,
four Node checks, two development lifecycle checks and production asset serving
(1.669s). Corrected mandatory checks passed with zero lint errors and two existing
Fast Refresh warnings. Formatting, dependency files and all unrelated protections
stayed exact. A fresh embedded binary is built separately; the prior process and
browser proof is not relabeled with its hash. Final mandatory checking remains a
pre-commit requirement; commit/push/read-back and new remote workflows are separate
coordinator observations. The containing commit records the bounded State slice.
Continue actual local registration approval, then the remaining Member workflow
and Role definition work. F04/F05 and formal 11/16/3 remain unchanged.

## Local registration approval integration

Frozen GORM migration V57 adds the private account/application relationship and
policy generation without backfilling historical decisions or changing released
migrations. Member list/detail summaries expose status and current admission
eligibility, while dedicated review exposes only recorded application fields.
The existing registration drawer, Member status/Overview and Base UI decision
dialogs implement the English-default bilingual workflow.

Each dispatched policy or decision request retains its exact reason, body and
If-Match through failed responses, dismissal and same-actor authority renewal.
A conflict after dispatch may follow a committed change. Refreshing metadata or
matching current values does not prove the original operation. Explicit local
abandonment preserves a draft for a new review and leaves the original outcome
unknown. Successful retries confirm current state and runtime application, not a
durable historical decision receipt.

Mandatory checks, complete source tests and fresh uncached Go race passed; the
unchanged frontend suite retains 2,910 cases in 144 files. The corrected focused
real PostgreSQL/MySQL check passed 27 named tests and retained the original
Overview seven-query, list nine/eleven/ten-query and Effective Models twenty-query
budgets, independent permissions and exact admission proofs. Both-driver
independent authentication/native restart passed.

The fresh complete regression passed all 102 ordered scenarios per driver,
preserving the original 100-case prefix, frozen V57, eight pre-loop constraints
and five test-bearing packages. All 204 direct lifecycle cases and 3,049 named
tests passed without named failure or skip. The 395 protected R17 paths stayed
exact; owned containers, networks and volumes were independently absent. The
first failed full run and later query-budget failures remain historical evidence;
the repairs reuse the same transaction's freshly admitted actor and do not relax
independent permissions, identity/application checks or driver assertions.
Current advisory evidence follows the installed Gateway snapshot's exact identity,
lease and denial fences; approval write confirmation additionally requires an
active publisher.

The controlled production process and bilingual browser now passed against the
same R17 source and production artifact. Anonymous registration returned HTTP 202
without a Session cookie; pending login remained denied. Approval changed only the
retained application and current admission, preserving existing Users, Sessions,
Keys, model grants and MFA facts. The intended creation-default policy copy and
its typed audit were verified separately and never admitted the pending account.
After explicit model authorization and a new confirmed Personal Key, exactly one
controlled native Chat completion produced one durable call and attempt with
recorded Credential/snapshot attribution. Restart retained the original binary,
configuration, database, journal and Sessions; read-only English/Chinese browser
checks passed without signing in again or dispatching another inference request.
All owned resources and temporary browser tabs were cleaned. Earlier failed runs,
including the finite browser-checkpoint timeout, remain historical evidence.
This confirms current state and runtime application, not a historical operation
receipt or completion of the entire Member capability. The final mandatory check passed. The containing commit delivers this bounded
approval phase; remote push/read-back and workflow results are recorded separately.

Allowed email domains, offboarding UI/summary and repository price seed data
remain separate queued source packages. Role-definition review is the integrated
candidate described below, not a delivered feature. Invitation delivery is not
included.
F04/F05 remain Partial; formal totals stay 11 complete, 16 partial and three
unstarted.

## Reviewed Role definitions: current candidate

The bounded Role-definition phase follows checked approval
`b10eb6cf900994b8a0e10b7a81e346ed10144cc8`. It retains the existing global Role
table, grouped permission choices and local Edit/View dialogs. Resource GET/PUT
use private, no-store headers before Session validation; GET requires current
`roles.read`, while PUT freshly requires an admitted, enabled, non-offboarded
intrinsic platform administrator. Delegated `roles.write` does not authorize it.
Built-ins remain readable and immutable. Existing creation/deletion and
Member/Team assignments are separate operations.

GET returns exactly `id`, `name`, `description`, `builtin`, complete recorded `permissions`,
current `available_permissions`, `definition_etag`, nullable `identity_etag`,
`review_etag` and `can_edit`. Safe unknown recorded codes remain visible but do
not become assignable; complete definition reads and replacement are bounded to
100 permission codes. Unknown creation provenance yields a read-only review.
PUT replaces the name, description and full sorted unique permission set, including explicit empty
arrays, with the recorded identity proof, a strong quoted review If-Match and a
nonempty reason. Names retain the existing 100-code-point bound; reasons are
trim-exact, control-free UTF-8 at most 1,024 bytes, within a 64-KiB request.

A real change atomically advances the existing durable definition revision,
saves name/permissions and appends typed `role.definition.update` before/after
facts with the reason. No-op changes neither rows nor audits. The identity proof
binds exact Role ID and stored creation identity; supported recreation with a
different creation identity conflicts before equality confirmation. This does
not promise detection of unsupported raw row cloning that preserves both facts.
Fresh independent postcommit authority/current-definition checks precede success.
Only the exact PUT response confirms `current_role_definition` with effect
`current_database`; it proves no historical actor, operation receipt, assignment,
model grant, native admission or fleet publication. Exact-current authorized
retries can confirm with zero writes; stale changing proposals cannot restore an
old generation.

The editor retains every dispatched body, identity proof and If-Match after a
failed response, including an initial 409. Matching GET, refresh, Cancel or
Escape cannot resolve uncertainty. Same-actor/target renewal hides private UI
but preserves the local retry; fresh CSRF is used only with the original request.
Explicit Abandon permits a new reviewed request while leaving the original
outcome unknown. Actor/target/logout/unmount destroys that scope; obsolete reads
or callbacks cannot restore it. English/Chinese drafts and accessible names
remain in the existing layout, with no additional editor Session observer.

No migration is added. Frozen V57 and the 102-scenario prefix remain unchanged;
Role definitions append scenario 103. The complete post-repair race regression
passed 103 ordered lifecycle scenarios per driver, eight constraint cases and
3,173 named pass events, with no named failures or skips (PostgreSQL 837.37s;
MySQL 1090.87s). Log SHA256:
`1fa9500a0ad8f3558f4f1004f98f01c8d8e6e1ca6a8481a5b579b33165745f5b`.
The backend, schema and harness stayed exact after that gate. The later UI-only
compact Base UI Input adjustment passed mandatory checks, 2,993 frontend cases
across 146 files, four Node checks, two development lifecycle checks, production
build and asset tests. Independent authentication/native restart passed on both
databases. Every owned integration and restart resource was independently absent.

Controlled bilingual production acceptance passed before and after the Input
adjustment. Both runs returned reviewed browser PUT statuses 200, 409, 409 and
200; rejected retries retained exact bodies/ETags and explicit Abandon plus a new
review used a new ETag. The earlier run additionally verified three typed
A → B → C → D audits and no audit for matching confirmation. The latest binary
`76d2254276ba7a8f5b4c676a8e80e48a999497428f025b5dd4fbb948ff7d979b`
verified actual Space activation, built-in 403 denial, dismissal/language draft
retention, Escape focus and same-binary/configuration/database/Session restart
against all 412 protected source paths. Zero calls/native POSTs were created.
No console errors were observed; an initial blank load needed one reload, whose
cause is unestablished. The temporary tab, listeners and Compose resources are
absent. Earlier incomplete helper/browser attempts remain historical evidence.

The Role definition phase is checked, committed and pushed as
`ecd130b10734fe3a931170dc10305468c0536e4d`, with exact remote read-back. Its
CI run 37331397426 was cancelled; latest CI37332855651 failed the MySQL bounded Member Roles read.
Historical Approval CI failure remains
separate. F04/F05 remain partial; formal 11/16/3 is unchanged. Approval b10eb6cf remains delivered. Its CI run
37314987013 failed a proven PostgreSQL fixture-registry race and a masked MySQL
error whose historical cause remains unknown. The repaired local focus and full
regression passed without weakening deadlines or query budgets; they do not
establish remote convergence. F04/F05 remain partial; formal 11/16/3 is unchanged.

### Grouped Role permissions

The existing Create/Edit dialogs use compact local Base UI Input checkboxes for
individual current assignable actions and per-resource select-all. Partial groups
retain native indeterminate state; unavailable recorded codes never become
assignable through a group toggle. Existing review, failed-request intent and
independent authority gates still apply. The Role list summarizes distinct
resources and recorded action counts, including explicit zero, in both languages.
Current production keyboard, save/readback and same-Session bilingual restart
acceptance passed with zero inference.

## Member workflow source integration

Allowed email domains apply only to new local self-registration. An empty array
is unrestricted. The complete reviewed policy accepts at most 32 explicit ASCII
DNS names and 2,048 encoded bytes. Boundary ASCII spaces and letter case are
normalized; names are sorted and duplicates rejected. Matching uses the exact
normalized email domain, without suffix expansion, wildcard matching, Unicode
conversion or DNS requests. Existing sign-in, setup, administrator-created members
and recorded approval decisions remain independent. A disallowed registration
creates no User, application, Session or audit. Invalid retained policy fails
closed. The drawer preserves drafts and the original failed dispatched intent.

Role-list counts describe retained identities and global assignments, including
disabled, offboarded, pending and rejected identities where the relationship
remains stored. Built-in counts use the recorded base role; custom counts use
exact existing User/Role assignment identities. Team-scoped relationships never
contribute. Counts are neither effective permission counts nor active-user totals.
A five-second read-only repeatable-read snapshot binds current actor admission,
read authority, complete definitions and batched counts. Catalogue or permission
overflow returns 422 instead of a partial result. Missing counts render unknown.

The existing offboarding page retains the exact failed mutation intent, including
a first conflict, until confirmation or explicit abandonment. Its read boundary
requires fresh actor and exact target authority. Member Settings displays only
validated recorded case facts, with an addressable link to the existing workflow;
it hides cached private facts while authority is being renewed or has failed.
Retained-user lookup rejects collation aliases before returning inventory.

Current source integration includes frozen GORM V58 and preserves the complete
103-scenario prefix, appending the domain migration and lifecycle as cases
104–105. Frontend 3,130 cases across 151 files and four Node checks passed.
Focused domain migration/lifecycle, offboarding and Role-definition scenarios
passed on both real drivers. The first governance focus failed only its new
query-count instrumentation because authentication reads disable SQL logging;
the fixture-only counter correction passed all five focused scenarios per driver,
including exact 6/7/7 statement budgets and explicit 1001-entry overflow.
Controlled current-artifact PostgreSQL production/browser policy, offboarding
conflict/retry/completion, bilingual summaries/counts and same-Session restart
passed on the current indexed-query artifact, with no inference. Full105 R1
exposed the old approval fixture's final-version assumption; its narrow repair
identifies released V57 and preserves every other ledger row, including V58.
Corrected R2 passed105 ordered cases per driver, eight constraints and3,247
named events without failure or skip. All1,509 protected paths remained exact;
owned resources are independently absent. No historical migration is edited.

Member Role batch reads now use a parameterized indexed ID superset conjunctively
with the existing byte-exact comparisons. Collation aliases never gain authority.
Measured MySQL10,000-assignment reads improved from4.820s/full scan to0.905s/
primary-key range scan, preserving complete history,8/11/47 statement budgets for
101/1001/10000 assignments and the five-second deadline. Both real-driver focused
runs passed. Diagnostic timing and EXPLAIN instrumentation is not product code;
complete corrected regression passed. Remote CI convergence remains separate.
Current production/browser artifact SHA256 is
`0e0b04678e95f65205e33317cca9a8c0344833cb40e5e46fba9bd0de72dda7c0`;
full log SHA256 is
`7dc6857e3ae6df5e2afcd2f76e64afcd4937e0d48dae4fd1f6ae0b06fd9a2ec6`.
The containing checked commit delivers this bounded workflow slice; F04/F05 and
formal11/16/3 remain unchanged.

## Recorded Role descriptions V67

The existing table shows each recorded description beneath the Role name; the
Edit/View dialog uses the local Base UI textarea between name and permissions.
New custom Role creation/replacement requires a nonempty description, trimmed
with Go White_Space rules and bounded to 2,000 UTF-8 bytes. Internal LF and U+FEFF
are retained; other controls and malformed Unicode are rejected. Historical empty
values remain empty and display localized Not provided. Builtins remain immutable.

PUT requires the original reviewed incarnation, strong If-Match and reason.
Description-only changes advance the durable definition revision and record typed
version-2 before/after description facts; exact current-target retries add no
audit. Only the authorized PUT confirms current database contents, independently
of historical operation evidence. Whole AuthGate-subtree recovery and lost Role
creation responses are outside this slice.

The isolated complete124 matrix passes both databases and all eight constraints,
with 4,491 matching named RUN/PASS events. All production Go files match that
accepted candidate. One test-only lint correction uses the equivalent promoted
`db.Name()` method; the other 970 Go files remain byte-exact.

The carried source passes mandatory checking, complete Task testing (3,929
frontend cases in 163 files), and the embedded production build. Controlled
PostgreSQL browser acceptance covers English/Chinese descriptions, retained LF
and U+FEFF, read-only access, legacy empty descriptions, conflict review, exact
retries, no duplicate audit, and original Sessions after process restart. Four
browser PUTs returned 200, 409, 409 and 200; three typed description updates were
recorded. Owned resources are absent. This proves the current-target workflow,
not whole-subtree recovery or a historical operation receipt. The containing
commit delivers this slice; F05 remains partial.
