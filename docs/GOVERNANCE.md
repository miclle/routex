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
| `members.keys.disable` | Disable another eligible member's Personal Key under independent subject/lifecycle guards |
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

Custom roles are additive and confer only implemented platform actions. They do not grant model invocation, ownership of another user's Keys, Team ownership, Project management, or unrestricted access to personal endpoints. Unknown or duplicate permission values are rejected. A role still assigned to users or Teams cannot be deleted. Role replacement and assignment validate the whole request before committing, preventing partial changes.

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
| `GET /admin/members/:user_id/keys` | Optional exact `cursor`, `limit` 1–100 (default 40) | Retained Personal Key page; `members.read` |
| `GET /admin/members/:user_id/keys/:key_id` | None | Safe reviewed Key/ETag; `members.read` |
| `POST /admin/members/:user_id/keys/:key_id/disable` | `{reason}`, strong reviewed If-Match | Exact current disabled/runtime confirmation; `members.keys.disable` |
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
