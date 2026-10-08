# Current implementation handoff

Updated: 2026-10-09. Status: resumed by explicit user instruction; prioritize partially completed capabilities. [Implementation](IMPLEMENTATION.md) records engineering contracts and historical acceptance evidence. Earlier checkpoints below remain historical.

## Current combined phase gate (2026-10-08)

The current main phase passes mandatory checking, complete Task testing and
production build. Its source-bound full PostgreSQL/MySQL regression also passes:
324 direct scenarios, eight constraints, all 4,813 ordinary named tests and all
412 named tests per driver, with complete Go JSON event provenance and no skips,
failures or races. The run completed in 2,055.583 seconds. Independent verification
confirms all captured processes, owned Compose resources and database ports absent.
Acceptance SHA-256:
`58729eb521921a8a50fe3b09321d614300e3e29fde2567cd191f3009fd6777c1`.
These results cover the 1,928-path V84/162-case feature source. The later CI
diagnostic plumbing has separate source and test gates; it does not relabel that
whole-source acceptance. The feature commit delivers Personal
root-Key, Project and Team rolling Token warnings, maintained public-name
reservation checks and the supervised parallel database runner. Controlled
browser, local SMTP/S3 and external-provider acceptance remain separate.
Future V85–V88 features are isolated and excluded from this delivery. Formal
status remains 13 Completed / 14 Partial / 3 Not started; the goal stays active.

Feature delivery `7aad9d7682abf421a6bc7452c92676693544e613` and the subsequent
documentation checkpoint `25de684db0daa787d6275619ba9d47a757fb8559` are pushed.
The documentation checkpoint passes CI37798423333, including both databases,
authentication restart checks and the dependent build, plus GolangCI-Lint37798423366
and Actionlint37798423259. Earlier feature CI37792504167 remains failed:
PostgreSQL exits 1 while MySQL and nonmatrix exit 0. Its terminal prefixes are
truncated and no complete private artifact survives; the exact PostgreSQL cause
remains unknown. The accepted local Full162 is separate evidence. A narrow CI
plumbing change preserves complete original logs, status and ownership ledger
from an exclusively new declared directory in a one-day artifact; default local
allocation and supervisor commands, assertions and cleanup remain unchanged.
The diagnostic change is committed and pushed as
`3720d75a5f5098f465c169026be8edb63d1931df`. Its final mandatory check, complete
Task (5,074 frontend tests), eight controlled entry-point tests and source review
pass. Exact-head Actionlint37806616211 and GolangCI-Lint37806615994 pass;
CI37806616103 now passes all four jobs, including both databases, original
Session restart checks and the dependent build. Root retrieves the original
one-day artifact and verifies its SHA-256, exact committed Go/dependency/Compose
source, complete JSON, status and immediate ownership ledger: all 4,813 ordinary
named tests, 412 named tests per driver, 324 direct scenarios and eight constraints
pass. Complete original-JSON review SHA-256 is
`eea330d8a097d979705c351da135b5a0991aecf6725d01090d800551329ef3bd`.
Remote process/resource absence is supervisor-reported rather than independently
queried; no future-feature or runtime-application acceptance is inferred. The
original older PostgreSQL failure cause remains unknown.
Controlled local SMTP also passes on the retained current executable in 45.579
seconds: one DATA250 acceptance, one lost final reply recorded Unknown, distinct
messages with one attempt per occurrence, and two original-Session restarts
without replay. Root independently verifies all three app process groups, four
listeners and project-labelled Compose resources absent. Acceptance SHA-256:
`bf3bd7e7ffa90c34191a5849e7c82cb691810a653a69aadf87c1e72751cab00d`.
This is local SMTP process evidence only; external TLS/authentication, mailbox
delivery and browser acceptance remain unknown. The prior failed runs stay failed.

The candidate fixture repair passes renewed mandatory checking and the complete
Project monthly lifecycle on real PostgreSQL and MySQL: two direct scenarios,
five named tests in 331.082 seconds, with original privacy assertions intact.
Independent focused review SHA-256:
`97ea8c95be7da22e4b472033778fd3305ed4ef27223deecefdaad8f4a80dd8ff`.
The renewed ordinary Task Full162 did not complete: its aggregate 85-minute
Go alarm interrupted MySQL Connection status after that scenario had run for
12 seconds. The stack shows ordinary runtime loading, and no assertion failure
or named skip precedes the alarm. All 162 PostgreSQL registry cases and 146
MySQL registry cases started, alongside four constraints per driver; starts
are not completed acceptance. The immutable failed log is retained at SHA-256
`c07b9d45e1921721b2b593d9bc524004ebb22f862f6e575863bd7b07026d567a`.
Its captured 1,923-path source floor is
`69823af401a80fb9c4b5833fe9871c8d012f5b47e260b96548c862623a55fc01`.
The owned resources and ports are absent. No acceptance reader ran.

A new candidate changes only the aggregate integration budget and status
records: 120 minutes for Go and 7,500 seconds for supervision. Assertions,
race detection, per-operation bounds, migrations and registry remain unchanged.
Renewed mandatory checking passed, but the subsequent ordinary Task Full162
failed in 4,265.04 seconds on the separate 1,923-path source floor
`0ff35a0627841de4b0303a0c416b2efd43dcf7a154f3491c0b00f5749fd70b73`.
PostgreSQL passed all 162 direct scenarios; MySQL passed 161 and failed the
Team monthly quota warning critical native call with HTTP 503. All eight
constraint checks passed and all 5,636 named starts have matching terminals.
The raw log is retained at SHA-256
`dacedba95a8c386abd0da4955ff76aa062b7c09a5bd69a22cdd36a0da7893c81`.
Independent failure review is
`188200edc3bb4838b51f12a134e5a37527e02573c1f98a77ca9a242222d7da9e`.
Owned processes, Compose resources and captured ports are independently absent.
No acceptance reader ran. Source review confirms a fixture lease-boundary hazard:
periodic runtime publication is stopped, and inbox/fanout checks consume the
five-second manually published authorization before an independent native call.
The original generic error does not prove its exact internal branch. The candidate
adds one explicit refresh immediately before that call, without retrying inference,
changing production leases or weakening assertions. The complete original Team
monthly lifecycle now passes on real PostgreSQL and MySQL: two direct scenarios,
five named tests in 180.404 seconds on captured source floor
`b26dfbde950e825aec09416c53e9a4db61f6eb8d3e6eb115f223025d6909e6f9`.
The retained raw log is
`d1781a56fac133d9a0827e68b70e09667c1bc050fab88e03b3dca95be2cbc802`;
independent focused review is
`84cd5ecaaf23cdfcec9587df9a939d7087953711897ae26903d5fc38005f5115`.
Owned processes, Compose resources and ports are independently absent. This is
focused evidence only; renewed full acceptance is still required before commit.

The negative observer helper now requires a live unchanged authorization lease
and captured snapshot both before and after each cycle, excluding expired silent
no-ops as an alternative explanation. Five independent negatives refresh only
the restored baseline before raw policy, calendar, Team birth, recipient birth
or disabled-actor mutations; no mismatching target is published. The guards-only
diagnostic passed both drivers in 200.573 seconds, so expiration was not
reproduced there. The complete test-only guard/baseline candidate also passes
the original two-driver lifecycle: two direct scenarios and five named tests in
200.812 seconds on source floor
`df4a171e6bda4c73470109eb2321e3bf80d21904bf4e3e07fca763cfb8349d9c`.
Raw log SHA-256:
`52c5721363ab74f5af581191a7440cd999f4d3c48472c18955d1df7738d0978f`.
All original assertions remain intact, production is unchanged, and owned
processes/resources/ports are independently absent. This focused evidence does
not turn the failed Full162 into an accepted run.
Independent timeout review is
`12f9f6ff59ebd9a3b1cf325db73ef7a148c792a2a784fb82477f64245efc522f`.
The source gate records actual checked main content and the three nonexecutable
permission differences normalized only in the private copy. Earlier failed
runs remain failed; final full acceptance and this phase's commit/push are
pending. Later status-document updates are documentation only.

Real PostgreSQL/MySQL Focus14 passed on the captured 1,923-path source at
`185b3cd4b6d1f3d4d0fd3a981dedb579b42f6014a9d54e03e28a289e721c0631`:
14 direct scenarios and 85 named tests in 220.346 seconds. Its acceptance remains
historical evidence for that exact source, not a full-matrix pass.

Ordinary Task Full162 subsequently failed in 4,807.108 seconds: PostgreSQL
passed 161 of 162 direct scenarios, MySQL passed all 162, and all eight constraint
checks passed. The only failed scenario was PostgreSQL Project monthly quota
warnings, reporting temporary runtime unavailability at an unpublished recipient
birth check. No acceptance reader ran. The original log is retained at SHA-256
`2569a3452d048291140bf0a2b853b6d98add9c6dde85a175d8ebd32721952aae`;
independent failure review is
`6c7548f49336da7bc28541d0f4e94ff83ac5623d4ee1d5a5e585c64e45bda5c2`.
Owned processes, Compose containers/networks/volumes and captured ports are absent.

Source review identifies a lease-boundary hazard: the fixture stops periodic
runtime refresh, then spends one five-second authorization lease across two
independent raw birth-mismatch scenarios. The generic original error does not
prove its exact service branch. The candidate fixture repair explicitly refreshes
before each scenario, while leaving both raw mutations unpublished and retaining
all private-history assertions. Production authorization leases, migrations,
registry order and observer behavior remain unchanged. The repaired scenario passes the renewed checks above; full acceptance remains
required before commit/push. Full162 must pass all
324 direct driver scenarios, eight constraint cases, every named test and all
six test-bearing packages; the maintained-name reference package is included.
The superseded 85-minute/5,400-second run remains failed. The new 120-minute /
7,500-second limits are finite budgets, not an ETA.

The earlier complete Task (5,074 frontend tests in 197 files, Go race/coverage,
Node/development lifecycle and production assets) and build are retained from
their checked parent with explicit fixture/document-only derivation. No new
Task/build execution is claimed. The unchanged executable artifact is
`a2449af139f32377219db8492fd0e1088654eb5a1125bb1774b1d817e8eb654d`.

Isolated guided-model defaults, Project root-Key rolling warnings V85, rename
compatibility, protocol-scoped routing and Model recorded metadata V86 have
completed source implementation and focused checks. Rename retains its legacy
POST contract and passes 48 final focused cases. Protocol routing passes its
mandatory check and focused Go/frontend checks. Metadata passes 57 focused and
253 related frontend cases and independent backend source review. These source
results overlap and are not added into a delivery total. A separate combined
worktree now integrates the slices, including configuration timestamp writes
for reviewed route insertion. Actual combined database/runtime/browser acceptance
remains pending; these slices are excluded from the current main phase.
Generic capability classification awaits a product decision and is displayed
as Unknown rather than inferred from names or protocols.

The isolated manual Model draft source passes 102 focused frontend/API cases
and independent review, including a reproduced and repaired one-to-one receipt
correlation defect. Its existing atomic preview/apply boundary is preserved.
Inline Connection creation's successor passes 136 focused API/UI/i18n cases
and independent source review. It closes both earlier findings with synchronous
live write/options checks before dispatch and string-only status decoding.
Selected Provider/egress reads use bounded exact-ID projections; empty UI search
omits the HTTP query parameter. An uncertain retry preserves its original IDs,
body and source even after a freshly authorized option list omits the target.
Its immutable first candidate and reproduced failures remain historical.
The separate combination also reproduces and repairs same-turn mode switching
that could abandon a dispatched manual batch or inline credential intent.
The first frozen combination passes 199 focused frontend cases, scoped Go
race/type/static checks and independent composition review. Its 1,990-path
source preserves all 1,937 foreign parent paths. The successor adds inert
code-dialog highlighting and the finite integration budget. A complete frontend
run found one obsolete guided-batch fixture: it did not explicitly select an
existing Connection and omitted the required nullable initial target. A five-line
fixture-only correction retains every original assertion. Fresh mandatory
checking and all 5,408 frontend tests in 208 files pass on the 1,992-path source:
`edbfbda72871b99d11e2ef5f230b3ca9b8e01b20a7b441e43ab7872d7c945cd2`.
The earlier failed run remains retained; focused and complete test counts overlap
and are not added. The renewed check retains two existing Fast Refresh warnings
and no errors.

Complete backend source units then reported 26 failures: historical migration
and registry guards omit the newly appended V87/168-case suffix, and one bounded
Model DTO guard excludes the approved nullable creation/configuration timestamps.
The 26 fixture leaves now preserve exact historical prefixes and reviewed
new suffixes; the bounded DTO accepts only the two approved nullable timestamps.
Independent source review found no remaining defects. Renewed complete Go
race/coverage source units and mandatory checking passed on the 1,992-path
candidate at source floor
`b70a566c33c9450d01202083e0678ec7df3a8d25b69306e84806eb3891282717`.
All four external database DSNs were unset for source units; this is not real
database acceptance. The unchanged frontend retains the earlier exact-tree
5,408-test evidence; no fresh frontend execution is claimed. No production,
migration or scenario registry was changed to mask a failure. Retained backend
failure log SHA-256:
`3dfd8ab3a6a98082f20dd875c120700286f52347939518e3bc15ce3130ea0c9a`.

A separate managed worktree prepares a process-isolated parallel integration
supervisor: all ordinary non-matrix tests plus complete PostgreSQL and MySQL
matrices, with owned process-group cancellation before Compose cleanup. Fake
process tests initially passed, but independent review found blocking diagnostic
and cancellation defects. The repaired successor passes genuine blocked-pipe and
private identity-ledger failure tests and independent source review. Only seven
owned infrastructure leaves are adopted into the current 162-case/V84 candidate;
all 1,921 foreign source paths and their modes are preserved. Each test worker
retains uncached race checks and gains complete Go JSON event logs; exact ordinary
named inventory and both ordered driver registries must be checked separately.
The supervisor records child PID/PGID before further work and joins every owned
group before Compose cleanup. It retains the finite 7,300-second lifecycle bound,
120-second cleanup reserve and a cancel-aware ten-second terminal allowance.
New mandatory check, complete Task and build have passed on the exact 1,928-path
source at floor
`7a2e59c1052215f7c761ccf5ce15999916ddd5d819144f6b957bc21fafc0ceab`.
The fresh Task passes 5,074 frontend tests in 197 files, Go race/coverage including
the supervisor in 11.308 seconds, five Node tests, two development lifecycle
tests and production assets. Unaffected cached Go results are explicit. A fresh
resource-free ordinary JSON run also passes all 4,813 named tests, preserves the
original ordinary named multiset and pins each package/test identity for later
full readback; all four database DSNs were unset. These overlapping results are
not added. The fresh retained executable is
`33d4655c0da446d6b28095d8f0f8dbedee6ed7ca0d623042f4c6684c24315820`.
The first actual parallel database run did not complete: the outer collector
failed after 1,951.572 seconds with two class-only PermissionError records.
All 4,813 ordinary named tests and PostgreSQL's complete 162 scenarios, four
constraints and 412 named tests pass strict JSON readback. MySQL started 148
registry scenarios and was interrupted before complete terminal records; no
full-matrix acceptance is claimed. The original exception site and errno were
not captured and remain unknown. The supervisor completed cancellation with
exit 143, joined every owned child group, and completed Compose cleanup.
Root independently confirms all ten captured PIDs, nine process groups, both
ports, and project-labelled containers/networks/volumes absent, with no further
cleanup mutation. The captured 1,928-path source and private modes remain exact.
Independent failed-run diagnosis SHA-256:
`1966ffc08cc741aeaf8b8bafc429804ec566ecf3f235db3e07a2e536d7710ff1`;
root absence/source receipt SHA-256:
`c473d8c269a9f76baf37554dcd7beacea7c85b9a2cc0721c3cff5d3ae69fe7e0`.
The failed run remains immutable. The independently reviewed collector successor
passes 20 pure checks and completes a fresh ordinary Task on the same captured
source. Both drivers pass all 162 scenarios, four constraints and 412 named tests;
all 4,813 ordinary named tests also pass. Strict JSON readback confirms complete
balanced events without skips, failures or races. Independent absence receipt:
`3853076811d30dca735cb343ca6af107e393303eb1e2eb08c62480020a938e2e`.
Diagnostic records exclude exception text, local variables and credentials.
Stable discovery runs every 30 seconds only after exact supervisor, endpoints
and all worker identities are captured; polling, cancellation, final/recovery
discovery, test assertions, coverage and deadlines remain unchanged. This fresh
accepted run does not relabel any preceding failure. The original PermissionError
cause remains unknown.
No measured performance improvement is claimed, and future 168-case/V87 features
or fixtures were not copied into this phase. Later status-document edits preserve
production and tests.
The separate future R5 composition now has fresh mandatory check, complete
Task and build passes on its exact 1,997-path source at floor
`8a0b32e45183a7e942ee3fc2005605112dfa5fd7c13faefc9a1a054a13b86ae5`.
The Task passes 5,408 frontend tests in 208 files (198.35 seconds), Go
race/coverage, five Node tests, two development lifecycle tests and production
assets. Unaffected cached Go results and the two existing Fast Refresh warnings
remain explicit. The retained future executable is
`22b7a8f188840cd1c6137c8f3e3b2a733094be14542f5e585ce93d9b9370ab38`;
these overlapping results are not added to earlier focused totals. All 26
fixture corrections, V87 and the ordered 168-case registry remain intact.
Real future database/runtime/browser acceptance and delivery remain pending;
this composition is excluded from main, whose current phase still covers 162
cases and V84. No future gate is substituted for current-phase acceptance.

Process-generation-bound routing application evidence reserves unpublished V87
after queued V85/V86. Its backend passes 29 top-level / 70 named race-test events
and independent source review; the corresponding read-only System jobs dialog
passes 48 related frontend cases and independent review. This routing-only slice
does not claim complete configuration versions, fleet convergence or rollback.
A separate worktree is integrating these candidates while retaining guided
creation, recorded metadata, rename compatibility and protocol routing. None of
these future source candidates is included in current main or its Full162 run;
combined database/runtime/browser and delivery acceptance remain pending.

Local versioned-S3 process qualification remains prepared only. The earlier
SMTP preparation checkpoint below is now superseded by the accepted controlled
run recorded above. SMTP
R4 preserves the prior operational/cleanup logic and qualifies the exact
aggregate-budget script change, observed source check and private-copy
permission normalization. Twenty-eight pure source checks and independent
source review pass. The prior Task/build remain inherited evidence. The subsequent genuine local SMTP process run passes as recorded above. Actual
S3 acceptance remains pending and separate from external service acceptance.
The bounded F20 assessment confirms existing Key/Team Session and
three-language code-copy functionality. Its code-dialog presentation candidate
implements inert lossless highlighting within the existing tabs, without
changing native payloads, authentication or clipboard bytes. It passes 119
focused tests across eight files and independent source review. It is now
included in the separately checked Model/runtime successor, while remaining
excluded from current main and its actual Full162 run. Real browser focus,
clipboard and renewed-authority acceptance remain pending.
A new isolated F28 successor now implements manual cleanup preparation for
previously published Provider credential objects. Two workers own backend and
frontend separately. The backend reserves unpublished V88 after queued V87 for
durable physical-object denial, exact process-generation exposure and joined
native/finite operations. Historical or unproven generations remain blocked;
heartbeat expiry is not drain proof. The frontend reuses the existing review
and confirmation drawer, preserving original uncertain command receipts and
paired English/Chinese guidance. Preview eligibility authorizes a bounded drain
attempt only; acknowledged cleanup means the owned version-1 destroy response,
not erasure of the path, metadata or later versions. Automatic deletion is
excluded. This source work is based on the frozen future R5 composition and
changes neither current Full162 nor that earlier future Full168 candidate.
The seven-file frontend successor passes 81 focused mocked tests, TypeScript,
owned ESLint and formatting, plus independent source review at SHA-256
`e80a90b99863608dcd515407933578e9006b48d2cbbf71b35116da5100231a3b`.
Its backend remains in progress, including durable known-no-effect receipts and
explicit fresh-intent retries only after proven drain. Database, Vault, browser
and delivery acceptance remain pending. The current 105-path main phase passes its complete PostgreSQL/MySQL gate and
is delivered separately by `7aad9d7`.
The prior PostgreSQL Team foreign-feed failure remains unproven and is retained
below. F12/F17/F23/F28/F30 remain Partial, formal totals stay 13 Completed / 14 Partial
/ 3 Not started, and the full objective continues.

Controlled local versioned S3 now passes on the retained V84 executable in
85.064 seconds. Independent review confirms separate storage read/write/test
permissions, bad credentials preserving the active revision, exact attachment
hash round-trip, five original Sessions across an identical executable restart,
and pending deletion surviving an outage before recovery deletes only the
recorded version. An independent second version remains intact with no delete
marker. Final remote versions are empty; one unconfirmed bad-credential probe
remains durably pending rather than falsely reporting deletion. No native call,
attempt or Key is created. Both original app process groups, three captured ports
and exact project-labelled containers/networks/volumes are independently absent.
Acceptance SHA-256:
`45509490de0b22bd789004cf63ea2ec31a2d61526e09fc98599bd3304418a594`.
This is controlled local process/API evidence, not AWS, browser or external
Provider acceptance. The original `P_LOOPBACK_PORT` run failed in 11.638 seconds; the subsequent
`P_RESTORE_EXACT_ENDPOINT` run failed in 36.398 seconds. Both remain failed. The
reviewed helper uses fixed selected loopback ports without retries or endpoint
substitution. Its disposable ordinary bridge permits egress, and the S3 fixture
has an explicit bounded DAC_OVERRIDE exception; no egress isolation is claimed.

Future R5 has not run Full168. Source inspection identifies ten real-driver
fixtures whose complete current-ledger checks still require V85 despite the
V87 registry. The isolated R6 fixture successor preserves all 1,987 foreign
paths and every mode, corrects exactly ten leaves, and keeps historical
reconstruction/data/timestamp assertions. Seventeen extracted predicate cases
and eleven existing checks with twelve nested cases pass under resource-free
race checks; the original R5 predicates reject V87 in all seventeen controls.
R6 source floor is
`393eabbc358e5df7736de8049999787c1f82376bf54adec7d1cf2491d3b5e5e8`.
Fresh root R6 checking, complete Task and production build pass: 5,408 frontend
tests in 208 files, Go race/coverage, Node and lifecycle checks, and production
assets. The fresh resource-free ordinary JSON run independently verifies all
5,005 exact named tests, retaining all 4,813 parent entries plus 192 additions.
The retained R6 executable is
`c4447c241f02a0337ab2004df806366b38e28dbc6ae8e7c22d18d1455dec6ad3`;
the genuine root-owned Full168 run finishes failed in 2,301.284 seconds.
Both complete driver JSON logs retain all 430 expected names with no named skips:
PostgreSQL passes 162 direct scenarios and fails six; MySQL passes 163 and fails
five. All 5,005 ordinary names pass. Root independently verifies all ten captured
PIDs, nine process groups, both ports and the exact project-labelled resources
absent. Complete failed-run review SHA-256 is
`84cb96f53e0a6282aad7d477bbca37fb789268a253e9b1704bee00ba6f99dc21`.
The shared failures are catalog, alias retirement, supply status, recorded-metadata
migration and recorded metadata; PostgreSQL additionally fails runtime-application
migration. Source diagnosis identifies five obsolete fixture boundaries and a
PostgreSQL GORM index-drop limitation. Separate minimal repairs are being prepared,
retaining current historical/timestamp/partial-index assertions. The original
failed run remains failed. Fresh successor gates, focused/full database testing,
browser and main feature delivery remain pending; the frozen R6 adoption patch
is not applied. R5 receipts retain their original source identity.

The isolated F28 backend R2 source remains 34 owned leaves and passes nine
top-level/thirty-eight nested mocked race tests. Its final irreversible remote
claim rechecks the exact current process registration, generation and live lease
after holders join. Independent source review passes with no actionable finding:
`06a07bd5552d77e354e1d0ade10b1ef2cb45e2e16a5526890084f5dbd187531c`.
The original R1 final-caller gap and failed review remain historical. The frontend
R2 passes 95 focused mocked tests, types, lint and formatting, and its independent
review passes at
`c5fb72a842df0cc560ab3737833313d0193c0ebedb41dfc3a8aeda7190a28231`.
It retains the original failed command receipt and requires a separate fresh
server-eligible review, reason and confirmation before a new UUID. Public failure
or timeout alone never proves no remote effect.

The composed V88/170 fixture successor changes only 15 handler test files and
preserves all 1,993 foreign paths, canonical modes, historical reconstruction
versions, retained suffix timestamps and exact registry prefixes. Three extracted
predicate race tests with 29 subtests pass, and independent source review reports
no finding at
`5f0ef8deee9ee39bb66aae00bf32989e23e00a9cdb322963354d71a2682446ff`.
The combined 2,008-path floor is
`bb33c5344eacd09f3aa16b8446dbb49e902d6387f54f4c03b9dfc842322cf36f`.
Fresh root mandatory checking, complete Task and production build pass on this
exact 2,008-path composition: 5,440 frontend tests in 208 files, Go race/coverage,
Node and development lifecycle checks, and production assets. The retained
executable is
`8fa3f71b23c8e2b9d5ce52a674f2b12e4de62dd36cf11a59f94b5a4bdd515380`;
fresh gate receipt SHA-256 is
`fd86bcab120d733ca36e35f0f91a7b7347a4ed7db39c11a3b0e6f8115d33b125`.
A fresh resource-free ordinary JSON run passes all 5,053 exact named tests,
retaining all 5,005 parent entries plus 48 additions with no removed entries.
Independent root ordinary-review SHA-256 is
`798d7351f3ddb410992664fa0fa1936bcdcfe2767e78aade91e0419f7d31101e`.
The root-owned Full170 run is now in progress with isolated concurrent
PostgreSQL/MySQL workers, complete 170-case registries and the unchanged finite
supervision/cleanup budgets. Launch binding SHA-256 is
`836496ea83b12ec3e3082c7469fbf94f88f278d153813f3d5e289198b467e1b0`.
The independently reviewed reader extends only its finite supported registry
cardinality list to 170; all original JSON and failure/skip assertions remain.
Completion, strict complete-log review, independent owned-resource absence,
Vault/native/process restart and feature delivery remain pending.
Browser acceptance is deferred after the user reports that the locked computer
will remain unavailable for about ten hours. No browser fixture is started;
independent source, database and controlled API work continues. Offline
generations without positive joined acknowledgment stay blocked;
process-wide graceful shutdown proof remains a separate gap. No acknowledgment
is synthesized. Overlapping source-test counts are not added into acceptance totals.
No isolated V85–V88 feature is delivered by the current main feature commit.

The earlier checkpoints below retain their original source and evidence scope.

## Current combined validation (2026-10-08)

The corrected 1,923-path source is frozen at
`15daf479c6648d6e913fa5c320968ea586ab6cb40b47019beda35d35c15792df`.
Renewed mandatory checking, complete Task (5,074 frontend tests in 197 files,
Go race/coverage, five Node checks, two development lifecycle tests and
production asset serving) and production build pass. The new artifact is
`a2449af139f32377219db8492fd0e1088654eb5a1125bb1774b1d817e8eb654d`.
Four reviewed documentation files changed during Task; executable and test
sources remained unchanged. The initial whole-document floor was not captured.

Fresh Focus14 fails in 260.778 seconds: 11 direct cases pass, including both
strengthened Key concurrency regressions, all six migrations and both Model
creation cases. PostgreSQL Team foreign-actor history and MySQL Project/Team
recreated-identity history assertions fail. Their cause is under investigation;
privacy assertions and authorization are not weakened. Root independently
confirms source, raw log, owned resources, process/group and captured port
absence. Failure review SHA-256:
`3af16aa8a237552653dd6d474145e03f2cb370b511d84236f74e6fdc6037f1bc`.
No acceptance reader ran. Full162 and phase commit/push remain pending.

Private Project Key V85 source gates and independent review pass; real-driver
acceptance remains pending. A separate worktree implements bounded initial
guided-model name/target assistance while preserving custom drafts and preview
confirmation. Local versioned-S3 R2 preparation passes independent source
review; no actual S3 service qualification has run. F12/F17/F23 and the full
objective remain unfinished. Formal totals remain 13/14/3.

The subsequent isolated Project/Team diagnostic passes all four driver
lifecycles in 266.617 seconds, with seven balanced named tests. Both changed
births and their restoration are checked against persisted database values;
the privacy assertions remain intact. Root independently verifies source/raw
log and owned cleanup. Diagnostic review SHA-256:
`7f87ee32f67e3b389505ef92a73aabb0ff2ec6bd9ad341ebcdadae6882a1f30a`.
The earlier PostgreSQL Team foreign-feed failure is not reproduced and its
cause remains unproven. This diagnostic does not substitute for new Focus14 or
Full162. Only two lifecycle fixtures change; production and schema are unchanged.

The checkpoints below retain their original source and historical scope.

## Combined phase failure and repair (2026-10-08)

The 1,923-path composition passes mandatory main checking, complete Task
(5,074 frontend tests in 197 files, Go race/coverage, five Node checks, two
lifecycle checks and production assets) and production build. Its checked
artifact SHA-256 is
`8b3bc4e13d0b2116c8d36edb6284aa178c7751a244a319906580c9550eaa89e3`.
Two later catalogue/notification documentation clarifications do not change
executable or test sources; the original Task is not relabeled as a new run.

Actual Focus14 on floor
`42c3167e73f0a69e9afbd24c9fa39e1b2bf20557f2eec9af9d615ad128d429dc`
fails in 205.398 seconds: eight direct cases pass, including all six migration
cases; six lifecycle cases fail. Project fixtures use a manager without quota
write authority; Team fixtures omit the public Team target field. Those fixture
contracts are being corrected. The PostgreSQL Key concurrent episode test exposes
a production repeatable-read snapshot/locking defect. Five related observers now
explicitly request portable Read Committed while retaining existing locks and
final application/identity/accounting fences. A synchronized two-transaction
regression verifies the real lock wait and exact transaction identity. Focused
Service race tests, Handler compilation and renewed mandatory checking pass;
renewed complete Task and real-driver acceptance remain pending.
The separate two-driver Model diagnostic passes both cases in 209.758 seconds,
so the earlier timeout's HTTP cause remains unproven. Its fixture now explicitly
publishes restored raw catalogue rows before the sole original native call and
captures any early response; no inference is replayed or production behavior
changed to mask the original failure.

Root independently verifies the failed source/raw log and owned resource,
process/group and port absence. Failure review SHA-256:
`f8b27cfc727ce166d6fa85738f1085bd1674d15542c30fce6f561cabcdbd1388`.
No acceptance reader ran and Full162 has not started. Repairs require renewed
checking and same-source Focus14 before complete regression and commit/push.
Private Project Key V85 work proceeds separately without changing this phase.

## Resume boundary

The user explicitly resumed implementation on 2026-10-08 and authorized
parallel subagents and workspaces. At resume, main was `352a6f7`; its exact-head
CI37675821518, Actionlint37675821707 and GolangCI37675821500 all pass.
The checkout was clean at resume. Preserve existing services and unrelated work.

Member initial-password lifetime is delivered and pushed as `222ea08`.
Its exact-head CI37717567220, Actionlint37717567223 and GolangCI37717567140
all pass. The documentation checkpoint `e9a7b01` also has passing exact-head
CI37722479510, Actionlint37722479550 and GolangCI37722479416. Personal Key
fixture repairs pass Focus4; its subsequent Full158 timed out and remains failed.
The combined Key/Project/Team/model candidate awaits fresh composed-source gates.
The controlled current-artifact browser acceptance below closes genuine price
file delivery and original Reader browser Session restart gaps. Broader Role,
Member and settings workflows retain their own outstanding browser boundaries.
Durable published-source cleanup, external SMTP and other rolling scopes remain
separate unfinished work. The full objective continues without a pause.

## Parallel unfinished capability work

The independently reviewed source candidates are composed in main without staging:
Key V82 (41 original paths plus two test-only repairs), Project V83 (41 paths),
Team V84 (42 paths) and bounded maintained-name reservation filtering (24 paths).
The sequential beforeimages and afterimages are checked; overlapping edits retain
each predecessor's changes. Composition receipt SHA-256:
`e161cd2437fd7f9d161a668c33f825b7c75593b0f3e3fe8d7ca7b98f6cb64b38`.

Project source checking, 52 focused race cases, 28 predecessor guards and 181
frontend cases pass. Team checking, 114 focused race cases, 196 predecessor/Team
guards and 182 frontend cases pass. Model checking, focused Go races, 74 frontend
cases, 39 final lifetime cases and four development checks pass. These are
source-bound worker gates, not composed-main or real-driver acceptance.
Independent reviews identify no actionable source defects. Team warnings use
only own aggregate stored caps; Team-member rolling controls remain unsupported.
The existing layout, admission and monthly warning rules are preserved.

A separate resource-free local versioned-S3 preparation has 15 pure checks.
No S3 image layers, resources, API operations or real-service acceptance have
run. It does not establish AWS/IAM/TLS compatibility or external SMTP evidence.
The full objective remains active; formal totals remain 13/14/3.

## Controlled browser acceptance (2026-10-08)

The newly built production artifact for delivered Member checkpoint `222ea08`
passes a finite controlled browser run. Four genuine download events produce
Excel and CSV files before and after an identical-artifact service restart.
Exact two-rate text, zero, long decimal, disabled state and file hashes are
verified against the API catalogue. The original Reader browser Session remains
usable after restart without a new login; the fresh baseline contains nine
Sessions and preserves all eight original API Sessions and recorded rows.
Read revocation removes private download controls; restoration renews access.
English/Chinese copy, four recipient notices, real mark-all-read and an unrelated
recipient's empty menu are observed. Seven Calls, six Attempts, eight notices
and six nondecreasing episode sample times remain authoritative API/DB facts.
Root independently confirms owned resources, processes and ports are absent.

Artifact SHA-256:
`440c8ba49b7b57f65d5a92469c78553da13962b545c07776a0a754336578e4c9`;
root browser review SHA-256:
`00c637e362ec81c337426896c8ae2e7cb480941ec7051ab18fca219d9e4e2fce`.
The first idle-expired and second database-connection-loss browser attempts
remain failed and retained; the latter's cause is unproven. This third run does
not accept Role CRUD, Member password creation, Notification settings Save,
Personal Key V82 browser behavior or external mail/provider functionality.
F15 is now Completed under the approved current-repository source contract.
A09 remains partial for network adapters, scheduling and wider release evidence.
Current totals are 13 complete, 14 partial and three unstarted.

## Personal Key rolling warning candidate (2026-10-08)

The frozen 41-path worktree candidate is composed on Member delivery `222ea08`.
It adds sampled own-root-Key five-hour/seven-day warnings at fixed 80%/90%, using
current owner/root birth, retained rotation graph and applied policy/calendar proof.
Live enabled nonexpired descendants permit new observations; revoked-root history
remains readable only by its exact original owner. Finite holds stay separate from
settled usage; unknown coverage never rearms or estimates percentages. Cap changes
start a new episode; Keys have no invented default/reset endpoint.

Private frozen GORM V82 adds three tables with portable constraints and indexes.
The exact 156-case prefix is preserved and two lifecycle/migration cases append
for 158. Predecessor fixture assertions retain the exact V82 ledger suffix and
original timestamps. Existing menu composition gains paired translations and
strict snapshot validation, including Unicode code-point name boundaries.

Independent source review found the repaired Unicode length mismatch; no further
production/proof/privacy defect remains identified. Worktree checking, Service
race, Database/Handler units, 223 focused frontend cases and 41 final API boundary
cases pass. Candidate manifest SHA-256:
`c1d48741ca336b20ca98928e7f4cad092c0368869b3fbfc218fce50dd2d2c86f`.
Earlier malformed-tag, disk-space and missing-cache check failures remain retained.
Composed-main formatting, mandatory checking, complete Task and production
build pass. Task records 4,971 frontend cases in 193 suites, Go race/coverage,
four Node checks, two development lifecycle checks and production assets.
The first real Focus34 fails only the new lifecycle on both databases: the
fixture used internal `key` as the public target and a foreign administrator
for owner-only operations. The other 32 direct children pass. One test-only
repair uses `personal_key`, the exact owner and a live rotation descendant,
with explicit administrator/internal-kind/revoked-predecessor denials.
Fresh main checking and focused Handler race/vet pass after that repair.

The first 1,891-file fixture derivative has floor SHA-256
`14e61561a69c1e05c197b083f1d0bc482f8723f7097fb8d787ebb3f966b375d5`.
Its real Focus4 also fails both lifecycle children: the fixture immediately
stops the runtime, so the existing applied-publication fence correctly prevents
inbox persistence. Production remains unchanged. A second repair keeps the real
publisher live, uses the existing bounded fixture publication barrier and joins
the old publisher before reconstruction. Exact injected error/hit, root usage,
transaction rollback, recipient privacy and restart assertions are retained.
Focused Handler/Service race and Handler vet pass. Fixture SHA-256:
`6370afbb99fe2a35cac3c710591038458713ca8fbccb6c39eb991174b08f53ae`.
The prior complete Task/build retain their earlier source boundary. Renewed
main checking passes. The latest same-floor Focus4 now passes all four direct
PostgreSQL/MySQL cases and 31 balanced named tests in 151.369 seconds. Root
independently checks raw events, source bytes/modes, and owned resource/process/
port absence. Focus acceptance SHA-256:
`0c426435b340d273e170fef52a423913ac721b2b54fc28149628901a0f71e827`;
root review SHA-256:
`bb1d81888b5203cb4352100a3ca350e9dec56fbe1089cfb84a669bdcdd1c8fea`.
Complete Full158 failed at the 55-minute Go deadline after 3,320.365 seconds
(Task exit 201), during MySQL `registration_email_domains`, its 105th registered
case. PostgreSQL traversal and started children are not complete PASS evidence.
The source remained unchanged at floor
`df93df7a0343a67885c1d4ad860eeb3db940d7c6a4e063cc205d3d78b4259e71`.
Root independently verified source/modes and absence of owned containers,
networks, volumes, captured process/group and both ports. Failure review SHA-256:
`d6be028f07612f224dc8cce6db214a177855b6992d6cdd87dfb30dae491be749`.
All failed attempts remain retained; no acceptance reader accepted this run.

Key, Project V83, Team V84 and maintained-name filtering are now composed into
one uncommitted phase. The exact original 158-case prefix remains, followed by
two Project and two Team cases: Full162 requires 324 direct driver cases, eight
constraints and balanced named results. Same-source Focus14 first selects model
creation plus the six new migration/lifecycle cases on both drivers. The new
85-minute Go / 5,400-second supervisor ceiling is a bounded execution budget,
not a duration or correctness claim. Fresh composed-main checks, complete Task,
production build and real database acceptance remain pending. No standalone
Full158/Full160 acceptance, Key delivery or whole F12/F17/F23 completion is claimed.

## Checked Member creation sensitive-input repair (2026-10-08)

The existing Members creation dialog now dispatches directly, clears the initial
password on submission, and retains only token-free lifetime/uncertainty facts.
It never stores password-bearing mutation variables or raw transport errors.
Actor/opening/Session/permission generations reject obsolete callbacks; uncertain
outcomes require local dismissal and independent list review before a new intent.
The parent receives only a validated Member ID and renews authoritative reads.
This is a UI repair without backend, schema or layout changes.

The original implementation fails the same cache-retention regression before the
repair. The candidate passes 131 focused tests in six suites, including 22 dialog
cases, scoped formatting, ESLint and TypeScript. Focused log SHA-256:
`6b48cea8f7cce81e42146348858acbeed40bed9f412fc924f50523b75759c637`.
Mandatory main checking and complete Task pass on the unchanged six-file UI
floor. Complete Task records 4,929 frontend cases in 192 suites, Go race/coverage,
four Node checks, two development lifecycle checks and production assets/build.
Main check log SHA-256:
`ba9e0417aee74858eeeb09acc414030835ae28ea3b9fb0465f6be5a8f4169b49`;
complete Task log SHA-256:
`1c6bcc12fe9eae2cfecfe4fc48a9fad1af6e736cf98cf9f419e414ea941bbbd7`.
The containing commit delivers this bounded repair. No schema or backend change
requires renewed dual-driver migration acceptance.
Browser acceptance is separate; F04 and formal capability totals stay unchanged.

## Current delivery and resumed work (2026-10-08)

Commit `5fda07372f85fa17a013f15ecd165f91fdabbb14` delivers explicit Azure Chat deployment
declarations, reviewed cleanup of never-committed Vault credential objects, and
recorded member handover status. Its exact 112-path phase passes formatting,
mandatory main checking and complete Full154 acceptance, with original source
gates and controlled Azure/Vault API/native/restart receipts retained at their
original source. Frozen GORM V79/V80 preserve both supported databases. Previously
published-object cleanup, real Azure and browser acceptance remain open. The
subsequent Personal rolling warning, Close and Notification settings phase is
delivered below; formal capability totals remain unchanged. Exact-head CI37641339082 now passes
Backend Checks, Frontend Checks, PostgreSQL/MySQL Integration and Build Artifacts;
Actionlint37641339288 and GolangCI37641339075 also pass. These remote results
belong to checkpoint `ae8d3e7e62228371d36008bf70b91b5e3287769a`.

The Role create/delete dialog lifetime repair is committed and pushed as
`00700348a440f46039653f87d515d7d45bfe2416`, with exact remote main read-back.
The four UI files retain captured actor/opening authority, reject obsolete
callbacks and preserve dispatched uncertainty across same-owner Session renewal.
Local abandonment never claims cancellation or rollback. Its 198 focused cases,
mandatory main checking and all exact-head CI37648959571 jobs pass;
Actionlint37648959836 and GolangCI37648959562 also pass. Browser acceptance
remains separate, and F04/F05 are not promoted to complete.

The containing commit delivers Personal rolling warnings, local response-close
ownership and the Notification settings repair while preserving those Role
outputs. Its original production source manifest is
`f3889bdec8017c2249d71a0e8fd654389652d47eaecd47012b0c64f34734f459`;
original source gates pass formatting, checking, complete Task (4,895 frontend
cases/191 suites, Go race, four Node checks, two development lifecycle checks and
production assets) and build. The inherited binary is
`f14b06568d4d84f1f62a601487bf08a74575e41f752dd87603339f5c54931d69`.
A four-test-file derivative corrects migration instant comparison, V81 fixture
reset, finite-hold episode assertions and the original-Session response parser.
Production bytes, schema and the binary remain unchanged. Fresh checking passes;
test derivative manifest SHA-256 is
`85dc2c545205d0eed28d9e2fa8bfa74faa33077fb4dcdf38c18ecec4e543f9f3`,
and root source review is
`5aceed78b827e2c9df34881ed12f5d3cc3e82cc0b27fedafa27e18d392cbdfdd`.

Fresh same-floor Focus4 and Focus24 now pass: four direct cases/23 named tests,
then 24 direct cases/43 named tests on PostgreSQL/MySQL. Both readers run once;
root verifies source bytes/modes and owned resource, process-group and port
absence. Focus24 acceptance SHA-256 is
`7fcf4f4608103de410385f26f64681c35ef71dab9e7ab47731ea05351eb7b925`.
Controlled native/API/restart acceptance independently passes seven Calls,
six native completed Attempts (five known usage, one missing usage), eight
notices and six original Sessions. Finite unknown usage retains settled90,
held2 and unknown0 before and after restart; it never rearms an episode or
adds a notice. Native independent review SHA-256 is
`7aac43d8b1254bd773d3e12ce5aa6230ddb5cecf077b89ae49b74aa8cc441813`.
Real Vault API/restart independently passes five creations, two commands,
19 KV audit pairs and two original Sessions with unchanged restart facts.
Independent acceptance SHA-256 is
`9b2a769d944bf96a5ce447588aabd24770f32f1153202c574c7230677c444f70`.
Fresh composed-main format/check/complete Task/build gates pass without
code/configuration drift. Task records 4,907 frontend cases in 191 files,
Go race, four Node checks, two development lifecycle checks and production
assets. Complete Full156 fails after 3,158.203 seconds: six historical
migration fixtures fail on each database because their predecessor ledger
checks omit the correctly retained V81 suffix. The other direct cases pass.
The failed raw run and cleanup remain retained. Three test-only predecessor
fixture repairs preserve the exact V81 suffix, original timestamps, rollback
and constraint assertions. Their independent source review passes; no production,
schema, dependency or frontend code changes. Main formatting, mandatory checking
and all Handler race unit tests pass after these repairs. A fresh composed-main
snapshot (1,878 files) passes Focus16 on PostgreSQL/MySQL: 16 direct cases and
35 balanced named tests in 145.243 seconds, with unchanged source/modes and fresh
owned resource/process/port absence. Focus acceptance SHA-256 is
`86404795e5b4cca585b8aaf4974ac7d706354045686bfc9b31c57df7f4d8a969`;
root review is `0c5646b6c201ab4e50461c0b15555c6b8b4d705c78d0add7fe7094659a494302`.
Final Full156 now passes on that same composed-main snapshot: 312 direct
PostgreSQL/MySQL lifecycle cases, eight constraint checks, 5,404 balanced named
tests and five completed packages in 3,153.195 seconds. Original source bytes
and modes are unchanged; the strict reader runs once after the original exit 0
and independently verifies owned containers, networks, volumes, process group
and ports are absent. Acceptance SHA-256 is
`f447ab8a47f4f0bb22fc9c2295da76bbc048ba1d7660a418ad6cc97b7ec1da50`.
Independent final raw/source/cleanup review passes with SHA-256
`912b771c43578b29ac5cfce40510e389b328aacd2feac80648c6dc9003c94ecd`.
The containing commit delivers this phase after the local gates above. Complete
Task/build remain the earlier composed-main executions; renewed checking,
Handler units, Focus16 and Full156 cover the subsequent three-test-only repair.
Original production native and Vault restart receipts retain their original
artifact and fixture floor; they are not relabeled as new main binary runs.
Exact-head CI37675821518, Actionlint37675821707 and GolangCI37675821500
now all pass for delivered commit `352a6f7f4055fe87f9e5c84df40b6717b38f9cc5`.
Genuine browser and external-provider/mail acceptance remain open.

Earlier failed runs remain failed and retained: original Focus24 (21 direct
passes, three failures); first renewed Focus4 (two migration passes, two episode
assertion failures); unchecked test Close in mandatory checking; and the next
Focus4's login-parser failure at the original-Session restart read. The final
fixture repairs preserve finite holds separately from settled/unknown use,
allow only monotonic sample-order advancement, and require unchanged User/CSRF
with no replacement Cookie. The native helper's stale post-restart hold oracle
was also corrected before actual execution; it was a review finding rather than
an accepted runtime run.

The previous pause is revoked by the user's 2026-10-08 instruction.
Current bounded work addresses Member initial-password lifetime, Personal Key
rolling-warning implementation and existing browser acceptance gaps in parallel.
These candidates remain unaccepted until their own checks and delivery finish.
Durable published Vault cleanup, external SMTP and broader quota sources remain
open; local response Close evidence does not establish durable fleet drain.
Current formal totals are 13 complete, 14 partial and three unstarted.

Saved AppRole is committed and pushed as
`77ece1770394e5217ff72094083e51b01d9eb143`, with exact remote main read-back.
Mandatory main checking, complete Task/build, Focus16, the real Vault
API/native/root-key/original-Session restart workflow and Full150 pass. Browser
acceptance remains pending. CI37610482389 passes all four jobs, including
PostgreSQL/MySQL integration, authentication restart and Build Artifacts;
Actionlint37610482397 and GolangCI37610482371 also pass. These remote results
belong to the delivered AppRole commit. F28 remains partial, and the full
objective continues.
Desktop inventory at 13:15 UTC still reports the Mac locked, with no native
apps available. Genuine browser downloads, focus and browser-Session restart
remain unaccepted; independent database/source work continues.

Commit `e1e78fe75224c2682c2cd9f99d54e7213617e86a` delivers the bounded
Provider storage phase below. Exact remote main is verified, and mandatory main
checking passes. CI37596642907 now passes all four jobs, including
PostgreSQL/MySQL, authentication restart and Build Artifacts;
Actionlint37596642705 and GolangCI37596642831 also pass.

Commit `68dd68a182ac40f1ac9497b02ce2b46bf360d562` delivers the bounded Excel
price-export slice. Exact remote main is verified. CI37578592197 passes all four
jobs, including PostgreSQL/MySQL integration and Build Artifacts;
Actionlint37578592217 and GolangCI37578592206 also pass. The preceding fixture
CI37576843294 was superseded and cancelled. Genuine browser-saved Excel/CSV
files and browser Session restart remain pending. F15 stays partial.

Before the root-proof correction, the private Vault Provider storage candidate
passes its source checks, complete Task tests and build. Its earlier exact-source
Focus12 passes both databases;
those receipts remain tied to their original source. Real Vault validation
confirms saved-policy publication separately from configuration persistence and
observes the resulting Key-scoped unavailable route before the native denial.
A subsequent root-key retirement returns HTTP503 after the real 300-second
observation. The final durable retirement state was not captured and remains
unknown; that run remains failed, with owned resources independently absent.

The diagnosed defect is the ordinary reader gate being reused inside the already
drained root-retirement transaction. A narrow private correction validates the
exact current drained policy before each retained envelope read and immediately
before returning proof. Ordinary readers remain closed; source generations,
cache material, publication digest and authorization checks remain enforced.
Twenty-eight related uncached race checks pass, including meaningful original
failure and late-policy-replacement regressions. After a test-only GORM selector
correction, mandatory checking, complete Task (4,621 frontend tests in 186 files,
Node/development/Go/production assets) and production build pass. Matching
PostgreSQL/MySQL Focus12 passes all 12 direct lifecycles in 235.434 seconds.

The corrected first real Vault run completes Retire with HTTP 200, committed
and publication-applied results, and seven observed domains with no blocked or
changed records. Its final inventory assertion fails because verification
replaces per-subject outcomes with `already_target`, while cumulative counters
retain the initial rewraps. All nine subjects and generations match. That
failed run remains historical and does not establish inference or restart.

A helper-only correction now checks every exact journal subject, generation and
final verification outcome, plus cumulative scanned/rewrapped/already-target
counts of 2N/N/N. Thirty-five resource-free helper checks pass. The subsequent
real Vault workflow exits successfully on the same checked artifact: five Calls,
four native completed Attempts and upstream requests, four CAS-zero creates,
thirteen owned reads and four ACL denials. The seven-domain root retirement uses
a genuine 300-second observation, and post-retirement inference, retained orphan
recovery and inference after application/Vault restart succeed. All five original
API Sessions survive. Root independently confirms owned processes, five ports and
Compose resources are absent. The independent bounded receipt review passes,
and Root rehashes all 119 safe evidence files. API/native acceptance is limited
to that exact source and artifact. The complete 148-case-per-driver regression
passes under its original 55-minute Go and 3,600-second supervisor bounds:
296 direct PostgreSQL/MySQL lifecycles, eight generic constraints and 5,151
balanced named results in 3,073.145 seconds. Root independently rehashes all
1,824 source paths/modes, 132 composition artifacts and raw output, and confirms
owned Compose resources, captured PID/PGID and both database ports are absent.
Acceptance SHA-256:
`6e203f01148a6f2573f8e259813b6d9c5382d5e2c738526d1137a2ad43894808`;
root review SHA-256:
`84f013744efe490f739cc84a11e423e388d9f7ce9a88746d442359f5f25c24b0`.
This phase delivers bounded Vault-backed Provider Credential storage, exact
retained references, durable write compensation, explicit storage policy and
matching root-retirement proof. Browser download, saved AppRole activation and
Provider automatic orphan cleanup remain separate pending boundaries. F28 stays
partial; no complete feature or fleet acceptance is inferred.

The user approved administrator-provided reusable SecretIDs for saved AppRole
identities. Backend and frontend proceed in separate private workspaces, with
independent writer/reader identities and backward-compatible Token support.
Login Tokens remain transient, and bounded login occurs only for explicit
verification, write/read or restart preparation. There is no automatic SecretID
creation or rotation and no expansion of Cleanup authority. The existing AppRole
SDK is delivered; saved Integration activation is still pending. The separate
backend candidate registers frozen GORM V78 and appends two dedicated cases after
the original 148-scenario prefix. Its frozen implementation and test-only
maximum-body successor pass
independent review and scoped race, vet, staticcheck and formatting checks.
The first composed mandatory check retains a test-only QF1001 failure.
An equivalent conditional successor preserves the original assertion and passes
scoped race and mandatory checking. The final composed mandatory check, complete
Task and production build pass
with 4,659 frontend tests in 186 files, four Node helpers, two development
lifecycle checks, Go race tests and production assets. Root rehashes all 111
artifact leaves and 1,830 source files/modes, including 47 owned outputs and
1,783 exact parent files. Matching Focus16 now passes all sixteen direct
PostgreSQL/MySQL lifecycles and nineteen named JSON events in 255.360 seconds.
Root independently verifies raw events, 1,830 source paths/modes, 111 composition
artifacts, captured processes/ports and owned resource absence. Acceptance:
`fb18ea54d2eecd1b1227e0143c2fa12c738d9a745824cff8842d398f5e9181e2`.
Complete Full150 acceptance now passes on this unchanged candidate; the detailed
receipt is recorded below. The nine-file frontend
proposal passes 185 related tests plus types, lint and formatting and is bound
into that reviewed final composition. Browser checks remain pending.

The first real-AppRole validate-only attempt fails before creating resources:
the helper retains its inherited six-case selector while requiring the new
eight-case/16-direct receipt. Real Focus16 remains accepted. A helper-only
successor must preserve the complete original eight-case order, operation
oracles and budgets; no failed validation is relabeled as runtime success.

The next real-AppRole run failed in the helper's compatibility branch: the
existing integration handler returned HTTP200 while the assertion expected
HTTP201. The preceding native/root/restart workflow observed seventeen actual
successful AppRole logins and seventeen credential KV operations; the final
Token compatibility and two denied-login checks did not complete. All owned
resources, five ports and application processes are independently absent.
The complete run remains failed. A narrow helper-only successor fixes five
completed HTTP200 assertions and passes 46 pure tests. The exact Vault remote
denial contract is being checked before retry. Full150 and delivery stay open.

The next complete real-Vault run also remains failed: its original
API/native/root/restart workflow completed, then Probe Read returned HTTP409
because the helper submitted the Integration ETag rather than the Probe ETag.
Root verifies all five ports, both application process groups and Compose
resources are absent; all 1,830 source paths/modes remain exact. The matching
HTTP400 Login oracle is now source-verified for the pinned Vault version.
A checked helper successor passes 67 pure tests and now uses the exact Probe
review and separate integrations for the two denied Login plans. Its fresh
real workflow passes independent safe-projection and root review: nineteen real
Logins (seventeen successful and two denied), twenty credential KV operations,
four ACL denials and forty-three paired audit records. Five Calls, four native
completed Attempts and five original Sessions are retained through restart.
Root-key rotation observes the full 300-second interval across nine subjects and
seven domains. Historical uncertain writes remain distinct from their recovery.
Root verifies unchanged source/modes and absence of owned Compose resources,
process groups and all five listeners. Root acceptance:
`20477f1be1c168efad4c24f737d5d92df7645e71e87409e1b4022063d775a286`.
Complete Full150 passes under the original 55-minute Go and 3,600-second
supervisor bounds: 300 ordered PostgreSQL/MySQL lifecycles, eight constraints,
5,194 balanced named results and five package completions in 3,098.321 seconds.
Root independently confirms all 1,830 source bytes/modes, 111 composition
artifacts and raw output, with owned Compose resources, captured PID/PGID and
both database ports absent. Acceptance SHA-256:
`7ddd2ec2d07e1aa9a2aecf791837ee2c20721316c548eb2e560bd4bcfd6b7287`;
root review SHA-256:
`2d875bbb55cb72d117d116f00c6c866fd7fa1b8b6936690aa83460b215c41ad7`.
The exact forty-seven-path AppRole proposal is applied in the main working
checkout, with formatting and mandatory main checking passed. Every non-status
source byte and tracked executable mode matches the tested candidate;
pre-existing local permission bits are preserved. Browser acceptance remains
pending. This phase delivers saved independent writer/reader AppRole identities,
bounded transient Login and frozen GORM V78; F28 remains partial.

The user selects administrator preview and explicit confirmation for
cleanup of Provider credential objects with no remaining RouteX reference.
In-flight calls and uncertain writes must retain their objects. Automatic
scheduled deletion is not part of the initial cleanup workflow. The bounded
never-committed creation slice passes independent backend and interface review.
Its frozen GORM V80 retains the original 152-scenario prefix and appends two
cleanup cases. Durable creation dispositions, full-operation local holders and
recorded process-use proof block active, previously exposed and uncertain objects,
including references from retained expired or retired peers. UI confirmation
requires an independent transient Cleanup Token; exact nonsecret intent survives
uncertainty, and reconciliation does not repeat remote destruction. Four localized
confirmation/receipt messages explicitly limit acknowledged cleanup to owned
version 1, retaining later versions and metadata. Backend review SHA-256:
`617ef9223bdb6319ba670ec04a60b71d9ab124a438a5ac1cc51ceaf3564bcba0`.
The first combined mandatory check retains a style-only QF1003 failure. An
independently reviewed equivalent switch correction passes renewed checking.
The next complete Task fails after 216.846 seconds, with 4,822 frontend passes
and one registration-approval fixture failure. Strict detail decoding correctly
rejects the list-only handover flag left in its list-to-detail test projection.
A test-only projection correction passes 160 related API cases.

The coherent R3 candidate passes formatting, mandatory checking, complete Task
(4,823 frontend tests in 190 suites plus Go race, Node/development and production
assets) and build. Root verifies all 1,862 source paths/modes and 125 composition
artifacts. Source acceptance SHA-256:
`ec0c7c06a3c36ada5e7e173aaeb2922949be61d3664dec15f7cc74d90c077729`;
root source review SHA-256:
`5ef5dc81e93ca7dc75577a6b3ce66f9d25f72d71dc690c881c6a89a0ae70fa19`.
The exact 108-file functional carry (32 new and 76 existing files) is applied to
main. Mandatory main checking passes and all 1,860 non-status source files match
the candidate; root progress documents are retained independently.

Fresh Focus28 fails after 325.474 seconds: all 28 selected direct cases start,
with 23 passing and five failing. Root verifies unchanged source/modes and raw
output, with owned Compose resources, captured PID/PGID and both ports absent.
Failure review SHA-256:
`e13f5ba25d5f8312e27cb679aebd44e7638e78a5f6d82ffe87f384ff71b0e51b`.
Both databases expose outdated retained-Team query budgets and a real cleanup
claim audit defect: a 36-character creation UUID exceeds the 30-character audit
resource ID column, returning HTTP503 before remote destruction. PostgreSQL
also exposes the pinned GORM DropIndex CURRENT_SCHEMA syntax defect in the
migration fixture. A four-file successor is being composed: exact Team page-query
budgets and one-query checks, canonical Credential audit identity with safe
creation/command correlation and schema-bound regression, and a narrowly
explained test-only index adapter. Released migrations and policy, permission
and finality assertions stay unchanged. The three cleanup successor leaves pass
26 service and two handler race cases, mandatory checking and a schema-bound
negative control that rejects the original UUID audit before destruction. Root
source review SHA-256:
`bc444bd59759e67761b028fdeb576d85cb12e1c5d816fa84a5e3048017c2936d`.
The combined four-file R4 candidate passes formatting, mandatory checking,
complete Task (4,823 frontend tests in 190 suites plus Go race and asset checks)
and production build. Root verifies all 1,862 source paths/modes and 128 artifacts;
source acceptance SHA-256:
`5313be64688894cd93b520b2f81e5f96f1fabd280b2c6d446c93bf570ad2aa10`;
root source review SHA-256:
`fa0f6dd1db7484f4534122055ab53c80b5d4012de624f41db22ccabb20c9c0cb`.
The four corrections are carried to main with all 1,860 non-status paths matching
R4 and root status documents preserved. Fresh main checking passes. Fresh
Focus28 passes on the exact R4 source: 28 direct lifecycles and 31 balanced
named results in 335.459 seconds. Source/modes, raw JSON events, owned Compose
resources, captured PID/PGID and both ports are independently verified.
Acceptance SHA-256:
`6cd01bf91c2ba2421ffe33e5c1ad54aa0c78b5376d99997084f6f33a4c9eb9fd`;
root review SHA-256:
`70cea15d3a7ef7580a77291ce0c624398285e1e553ba7d84673dd800ba37aeff`.
The first real Vault cleanup workflow fails before preview/destruction after one
real creation Write and original Read. The helper incorrectly requires a
canonical Provider target ID for a new-Provider intent whose target is empty.
Its original operation DTO and precise failure label were not retained; a
narrow helper correction and safe fixed-label diagnostics are being prepared.
Root verifies unchanged source/artifact and complete owned resource/process/port
absence; failure review SHA-256:
`cff8d913fac5cd9c8d83f16a188edf00cd9a8ba031cefa37900183acf9ec7a7e`.
The corrected real Vault workflow passes independent root review: five creation
operations, two explicit cleanup commands, 13 product KV effects, six QA effects,
19 request/response audit pairs and 68 normal API requests. The two original
Sessions and all retained non-instance database projections stay exact through
restart. Dedicated cleanup ACL refusal, unknown original writes, committed
references and changed-process blockers are checked; version two and retained
metadata remain unchanged after acknowledged version-one destruction. Root
verifies 101 safe evidence files, unchanged source/artifact and owned resource,
process and port absence without reading private authentication/configuration,
raw audit or application logs. Review SHA-256:
`bf17f9661d68cbffcedcf9810d78e3ccd177f866cdccb47f19af0d28d17d0ecb`.
Full154 fails after 3,143.003 seconds on the exact R4 source, with the original
55-minute Go and 3,600-second outer bounds. Both databases execute all 154 ordered
scenarios: 304 direct cases and eight constraints pass; four Connection metadata
and status cases fail because old exact DTO assertions omit the additive adapter
and API-version fields. All 5,271 named starts have terminal results (5,264 passes
and seven failures including parent suites). Root independently verifies unchanged
source, raw output and absence of owned resources, PID/PGID and ports. The failure
is retained. The strict two-fixture successor passes mandatory main checking;
complete field sets and native/null transport assertions are retained. Renewed
four-case dual-driver focus initially fails after 120.337 seconds: metadata passes
both drivers, but status reaches one further obsolete disabled-metadata field
count. Root retains its JSON/raw/source and complete owned cleanup evidence. A
strict 11-field/native/null singleton correction passes renewed mandatory main
checking. Fresh Focus4 passes all four direct cases and seven balanced JSON
results in 120.309 seconds. Root independently verifies source, raw results,
owned resources, ports and PID/PGID absence. Review SHA-256:
`4450400dfd4f3a423cda08f645a4bb2c14df2f6e50461cccf003ae1a7ff77f5b`.
Renewed Full154 passes on exact R6 under the original 55-minute Go and
3,600-second supervisor bounds: 308 ordered PostgreSQL/MySQL lifecycles, eight
constraints, 5,271 balanced named results and five package completions in
3,138.407 seconds. Root independently verifies all 1,862 source paths/modes,
raw text-v counts and complete owned process, port and Compose cleanup.
Acceptance SHA-256:
`f4076c07d8b2bd21ef58a47a93f6f4964a03089a92e3366b7903be1100b022f3`;
root review SHA-256:
`dbef9ff9dfca14b1f86c8b62d2031901b3a54ebf683d5ca0dea3aad1695099ca`.
Original failed runs remain historical. The three scoped technical documents
now include the creation-orphan cleanup boundary and frozen GORM V80. Final
mandatory main checking passes. The phase is committed as `5fda07372f85fa17a013f15ecd165f91fdabbb14`;
browser acceptance remains separate.
Browser acceptance remains pending. Previously published objects still
require complete native-call and stream drain before cleanup eligibility.

A separate ten-file local source-holder prerequisite passes 251 service and 64
parser race cases plus deliberate regression controls. Independent review finds
one availability defect: unused open holder states accumulate until a 5,000-state
limit rejects new sources. The two-file correction passes 24 race cases and
independent review; only open zero-holder entries are reclaimed, while closed denial tombstones remain
retained. The clean R2 foundation passes formatting, mandatory checking,
complete Task (4,823 frontend cases/190 suites plus Go race and asset checks) and
production build. Its first mandatory check retains a test-only unchecked Close
failure; the exact checked-discard correction preserves production bytes and
ownership assertions. Root verifies 1,864 source paths/modes, ten outputs and
1,854 unchanged R4 paths. Source review SHA-256:
`f5f9d52eb6f4976cdcd283f071c3a2a3498d9f77d12b9f837de4f72ff4d2cbcb`.
Separate seven-scenario-per-driver native/Vault acceptance is prepared on the
exact 1,864-file holder source. Root verifies all 34 support artifacts, finite
scenario selection, unchanged assertions and original timeout/resource bounds.
It remains unlaunched. The known Connection fixture corrections must be composed
before new driver runs. The failed R4 Full154 cannot substitute for this source. This prerequisite is
not part of R4 and does not activate published-object cleanup or prove fleet drain.

The V82 published/deleted-object proposal uses stable physical object denial,
sticky Close-error uncertainty and required process registration/first-exposure
interlocks. New capable processes may register with durable zero-exposure denial
acknowledgements before Vault preparation; interrupted claims quarantine their
objects without preventing unrelated startup. Every old or unproven generation
continues to block cleanup. Independent review requires one amendment: finite
KV/AppRole response Close errors are currently discarded, so native-only joins
cannot prove complete drain. A private fixed closure-result seam must record
poison before holder release without rewriting original remote observations.
The amended proposal passes independent review. Its four-file private SDK
response-closure prerequisite passes 216 named race checks and independent source
review, preserving explicit recovery and original remote observations. Service
ownership, poison-before-release integration and durable activation are pending. Durable activation still
has no migration/runtime acceptance and follows the separately reserved V81
warning feature.

The next bounded F23 slice is Personal-user own stored five-hour/seven-day
Token warnings in the existing recipient-private inbox. V81 is reserved privately
for sampled durable episodes and immutable observations. The selected 80%/90%
thresholds apply only to fully covered, known settled use under a proven current
cap; holds, unknown use and zero-denominator caps produce no inferred percentage.
Each episode emits at most one reminder and one critical warning, rearming only
below 80% with complete authoritative coverage. This does not change admission,
monthly warnings, email delivery or other-account/inherited warning scope. Source
implementation is in progress: 62 balanced named scoped Go/race results and 513
focused frontend cases pass in the private workspace. A later repetition fails
at the handler linker with host ENOSPC and remains failed; only ignored copied
dependencies are reclaimed. Root then finds first-sample100% suppression; a
narrow rolling-only successor must preserve the90% critical warning at or above
the cap across producer, schema, inbox and decoder without changing monthly
behavior. Its meaningful oldRED/newGREEN source checks, 44 balanced race cases
and 519 frontend cases pass; independent exact-source review closes the issue
without changing monthly/admission behavior. The 1,875-file holder-plus-warning composition is prepared privately, with the
known Connection fixture corrections still to be composed. Its first coherent mandatory check fails two test-only errcheck findings;
complete Task/build never start and all source bytes remain exact. A narrow
explicit-discard/checked-removal fixture successor preserves error injection and
callback timing. Renewed R4 format/check/complete Task/build pass, including
4,859 frontend cases in 191 suites and Go race/production asset checks. Root
verifies 1,875 source paths/modes, 128 artifacts, 53 cumulative outputs and the
unchanged 154-case registry prefix before the two new warning cases. The separate
24-case driver profile is prepared; real-driver, runtime and delivery acceptance
remain pending.

The operational warning runtime helper passes independent source review and
32 resource-free checks. It preserves normal management API provisioning,
seven Calls, six native completed Attempts, eight notices and six original
Sessions through restart. This is a reviewed plan, not executed runtime evidence;
real database and native acceptance remain pending.

A separate F23 follow-up repairs the existing Overview Notification settings
dialog's fresh actor-authorized reads and captured save intent. It preserves
layout, independent read/write permissions, drafts and explicit ETag conflict
review, and prevents obsolete completions from changing a reopened dialog or
another actor's cache. Private implementation and focused source tests pass;
no backend schema or SMTP integration is added. Existing SMTP implementation
and external relay/inbox acceptance remain separate.

The local Close prerequisite now passes independent source review and root
verification: 136 service and 24 SDK named race cases pass, with 1,878 source
paths/modes and 61 packet artifacts verified. Finite operations own fresh clients;
physical aliases retain Close uncertainty before holder release. This changes no
schema or published-object cleanup eligibility, and durable V82 activation remains
excluded. The next coherent candidate combines this prerequisite, V81 warnings
and the separate Notification settings repair. Earlier warning-only Focus24 and
Full156 preparations remain unlaunched; their source gates and helper reviews are
retained at their original source rather than transferred to the successor.

Independent review of the six-file Notification settings candidate finds one
callback ownership defect: an old pending save for actor A can affect a new A
lifetime after A-to-B-to-A. A real component regression reproduces the failure;
the initial 530-case source checks do not close it. The original candidate and
CHANGES_REQUIRED review are retained. The narrow R2 actor-lifetime successor
closes the finding: independent original regression 1/1 and targeted status,
Session, body and queued-authority vectors 8/8 pass. The 535-case producer suite
also passes. Independent closure manifest SHA-256:
`1b03b9fca9ff9d48e7501dae7a986f41feeb848ef40ae8fa537284a7d92c8e70`.
Coherent composition and whole source gates now pass as recorded above; fresh
database and actual runtime acceptance remain pending.

The next already-partial protocol slice is Azure OpenAI upstream adaptation.
The user selected explicit administrator deployment attestation, separate from
Credential authentication and actual model discovery. It will retain exact
Credential/deployment ownership, actor, reason and version, and will not issue
paid verification calls or borrow management-plane authority. The final
coherent source passes renewed checking, complete Task (4,756 frontend tests in 188 files plus
Go races, Node/development helpers and production assets) and build. Its first
real Focus8 fails startup on both PostgreSQL and MySQL at frozen V79: the GORM
column name and explicit coverage-review column validation disagree. No selected
scenario executes; owned resources and unchanged source/modes are independently
verified. A narrow explicit-column mapping correction now passes renewed
coherent formatting, mandatory checking, complete Task and build with unchanged
4,756 frontend tests in 188 files. Fresh Focus8 now passes all eight direct PostgreSQL/MySQL scenarios and eleven
balanced named results in 155.254 seconds. Root verifies the exact 1,847 source
bytes/modes, 161 composition artifacts, raw output and absence of owned processes,
ports and Compose resources. Acceptance SHA-256:
`edc2fd51638dbe225f0be3053634d77ef231afb9fbf6ee142642dc1eb6134824`;
root review SHA-256:
`6acbd0becd3d9806f27069bb33fa0b57bba3bb6d3fdb942a9730efe71c49607e`.
Controlled local API/native/restart acceptance now passes on this corrected
artifact: six Calls, four native completed Attempts, two pre-dispatch denials,
five original API Sessions, one Azure foundation listing with zero discovered
deployments, and one native discovery listing. Four observed upstream requests
match exact adapter authentication, path, body and attribution. Coverage complete
sets, withdrawal, stale/ABA conflicts, explicit current review and replacement
noninheritance pass. Five original Call rows and three Attempt rows survive
restart unchanged, followed by exactly one additional Call/Attempt; all other
fixed projections and original public/session response digests remain equal.
Root rehashes 143 safe evidence files and all 1,847 source bytes/modes and confirms
all owned Compose resources, both app PID/PGID pairs and three listeners absent.
Root acceptance SHA-256:
`ef576b6d83f0b4c9ca89c3353d745d927081b5ccf9a5b1444cbc0f3ccf1d034d`.
This is controlled local upstream proof; real Azure, browser activation and
delivery remain pending. A fresh combined Full154 will cover Azure and cleanup,
rather than a redundant Full152. Project quota
and rate applications are already complete and are not reopened by stale stage summaries.

A bounded Member list parity gap is being implemented privately: expose a
recorded current handover plan and an authorized detail action, preserving the
account's actual enabled/disabled state. No schema, global directory or new
permission is introduced. Frozen source and independent review pass: 117 focused
frontend cases, types/lint/format and 46 named Go race results with vet and
staticcheck. This source is being composed with Azure and cleanup before fresh
combined driver acceptance; browser acceptance and delivery remain pending.

Formal totals remain 12 complete, 15 partial and three unstarted. The full
objective remains active. Older checkpoints retain their original source and
acceptance scope.

## Current monthly modes, Connection and Vault continuation (2026-10-07)

This phase adds bounded Excel price export to the existing price-file workspace,
with the Excel action before CSV and a text-only workbook that preserves exact
decimal values. CSV output and import preview/confirmation remain unchanged;
export ceilings do not enlarge the independent import limits. Downloads use
transient Blobs, current actor/Session/permission checks, cancellation and a
duplicate-operation guard. A prepared notice does not prove a saved file.

The exact `07ce11845002a4d4a3aa9d59abe55db230bbfdb2`-based private candidate
preserves the delivered member-Key fixture repair and changes fifteen price
paths. Formatting, mandatory checking, 58 balanced scoped Go race results,
65 focused frontend tests/four suites, complete Task (4,579 frontend cases in
183 files, Node/development/Go/production assets) and production build pass.
Independent source review is
`225324150e41ac0784b87c32bc54539413f433cbd05d2796ee67d67a84e09bf7`;
all 1,788 unrelated paths and source modes remain exact. The earlier price Focus4
passes its exact prior candidate on PostgreSQL/MySQL; its receipt is not
relabeled as a new complete matrix for this contextual candidate.
Genuine browser-saved Excel/CSV, bilingual download interaction and browser
Session restart remain pending. F15 stays partial, formal totals stay 12 complete,
15 partial and three unstarted, and the full objective remains active.

Commit `07ce11845002a4d4a3aa9d59abe55db230bbfdb2` is pushed and remote main
read-back is exact. Its new Actionlint37576843297 and GolangCI37576843344 pass;
CI37576843294 remains in progress at this checkpoint. The earlier failed
CI37571163309 stays failed. New phase CI must be checked independently.

Preceding delivered feature baseline: `f9e72ca5023fdde0c7495bf0e3a8092f9e5fb0ed`
(Connection availability). The exact 54-path commit/tree and remote main read-back
are verified. Actionlint37571163377 and GolangCI37571163243 pass. CI37571163309
passes Backend/Frontend checks but fails its database job on a MySQL member-Key
fixture data race; build artifacts are skipped. At that historical checkpoint,
the index was empty and fifteen price-export paths were preserved. The current-parent delivery check passes in 65.165 seconds; the
controlled native/restart workflow has independent review. Browser/AuthGate and
full F11 acceptance remain separate.

The preceding `91f347de0593c0166602a80922ba7ef1016d4288`
(explicit AppRole SDK login) follows `de5b6966cc215893b64177b3b8844e82227ed93a`
(Team-member monthly modes) and `a5bb00bc59bb36c8bbe589d8bb7bc5019fed22fa`
(Project Key monthly modes and asynchronous ownership protection).
All four phase commits have exact remote main read-back. Prior SDK-head
Actionlint37567989752 and GolangCI37567989801 pass. CI37567991337 passed
Backend and Frontend checks but was cancelled after the Connection push;
its interrupted database job is not a passed remote regression.
Team-head Actionlint37564319251 and GolangCI37564319317 pass. Its CI37564319287
passed Backend and Frontend checks but was cancelled after the SDK push;
that interrupted integration job is not a passed remote regression.
Earlier deliveries include
`78c76d204e7745eaf11a4df4828ddac551f99623` (credential SDK) and
`c2368ddb0251415e72bfeee948ad23ed7a173ee4` (Vault Token/root inventory and
Personal Key/Project aggregate monthly modes).
The c2368dd full CI run was cancelled after the SDK push; cancellation is not
integration acceptance. The SDK-head Actionlint and GolangCI pass. Full CI37554117091
passes all four jobs: backend, frontend, PostgreSQL/MySQL integration and build. Earlier sections retain their original source and time.

The Project Key delivery adds independent monthly Token and money modes to
its stable rotation account. A child `alert_only` mode never weakens a hard
Project parent. Limits keep null and zero distinct, decimal money exact, and
existing rate, unknown-usage and missing-price rejection rules intact. Current
Project managers receive recipient-scoped settled-usage warnings at 80/90 percent;
platform Key management authority grants no implicit notification membership.
The existing Key limits editor supplies bilingual controls, reviewed revisions,
conflict review and identical-intent publication retries.

The final Key ownership candidate passes formatting, mandatory checking, complete Task
(4,520 frontend tests in 181 files, four Node checks, two development lifecycle
checks, Go race/coverage and production assets) and build. Focus20 passes 20
direct PostgreSQL/MySQL cases and 23 balanced named results in 310.590 seconds;
acceptance `e7c4f233e41a84b4914edac2d323d9d1908cc2de5b3363ccd9d5d0a7f751e97f`.
Controlled API/restart passes 11 Calls, five native completed Attempts, six
pre-admission denials, two 90 percent warning dimensions/four current-manager
inboxes and all five original API Sessions. Nineteen fixed database projections
remain identical across restart. Root independently verifies exact source,
binary and owned resource/process/port cleanup; root review
`b6042ef5d52a380832ab5c949dcd07ed3baa8b4e19c797ed29cfaf92e2068b28`.
The binary is `941297373484eb2a66d5b1c64fea819d665c9fb18f595fff924770c05fcd70a2`.
Full142 on the original R4 backend exits201 in 3,227.939 seconds: 283 of 284
lifecycle cases and all eight constraints pass; only MySQL Credential retirement
fails at the final current-runtime assertion. Its fresh intent is durably committed
but reports `runtime_unavailable`. Source/modes and owned resources, ports and
processes are independently verified. This run remains failed. The fixture leaves
a restarted Service's background publisher running while its readiness path uses
a nonblocking publication lock. The quiescent fixture successor preserves all
requests and the final applied assertion; its targeted PostgreSQL/MySQL run passes
both retirement lifecycles in 95.223 seconds, with independent source and owned
cleanup verification. Acceptance: `d73addd111c42427a3aac3f8b4cea5269e2156127f54872d27d6a10813185fb2`.
The original failing internal branch remains unrecorded. The historical Full142
run remains failed; the passing targeted repair and existing passing source/API
gates support this bounded phase, without claiming a new Full142 run.

Earlier focused failures are retained. Repairs affect only test fixtures:
valid soft-policy PUTs, rotation mode preservation, representable Project birth,
and warning caps that reach the intended threshold without changing settlement.
The API helper's writer-only parent read was corrected to use an already
privileged current manager; product authorization remains unchanged.

Team-member monthly modes (V75) and Connection enablement (V76) are delivered.
Their separate original candidate evidence follows. Team's restart fixture repair passes all four direct
PostgreSQL/MySQL cases; its Full144 regression passes 288 direct cases and all eight constraints in 3,212.747 seconds; root independently verifies the original R2 source and cleanup. A separate frontend
repair preserves an uncertain save across same-actor Session renewal, releases
busy state and rejects stale results; fresh complete Task passes 4,526 frontend
tests in 181 files with mandatory checking and build. Controlled Team API R7 passes in 23.490 seconds: eight Calls, four native completed
Attempts, four pre-admission denials and five original API Sessions survive a real
process restart. Twenty-one bounded durable projections remain identical; four
scoped 80/90 warnings and one read mark survive removal/rejoin and restart.
Root independently verifies recorded Credential/Connection/Model/snapshot
attribution, source, artifact and owned cleanup. Earlier helper failures remain
historical. R6 exposed a legitimate additional money warning after a policy
revision; R7 configures the existing hard-child money cap as zero, preserving
four-warning assertions, all call budgets and unchanged product behavior.
The contextual Team delivery candidate retains the current Key ownership guard and
quiescent retirement fixture. Its six focused UI suites pass 176 tests; mandatory
checking, complete Task (4,535 frontend tests in 181 files) and production build
pass. The exact production backend remains the accepted Full144 implementation;
the retirement test repair has separate passing two-driver evidence. Original R2
full/API and R3 UI receipts keep their original source identities. The 37-path Team phase is delivered as `de5b6966cc215893b64177b3b8844e82227ed93a`,
with exact remote read-back and final staged checking. Browser acceptance remains separate.
Connection source gates pass 4,545 frontend tests in 182 files, mandatory checking,
Go race tests and build. Its first focused run actually selects 28 direct cases because of substring
matching: 26 pass, while both new status cases fail on a stale role fixture. The
anchored R2 run selects exactly 20 cases: 18 pass and both status cases fail on a
fixture that incorrectly expects authorized logical Models to disappear when
supply is disabled. A fixture-only R3 checks the retained Model ID, empty native
protocols/capabilities while disabled and exact Chat metadata when enabled;
its Focus20 passes all 20 direct scenarios and 87 balanced named results in
235.271 seconds. Root verifies exact source and owned cleanup. The contextual
Full146 regression exits201 in 3,118.295 seconds on its unchanged frozen source.
Both drivers fail the old member-list summary warmup with HTTP503; its synthetic
`ml_connection` ID is rejected by the canonical Connection identity guard. The
new Connection scenarios pass in the raw log. Independent failure inventory verifies
290 direct passes, two failures, eight constraint passes and all five package
terminal events. Exact source and owned resource/process/port cleanup pass. The
four-literal fixture-only correction passes 56 source race results and staticcheck;
its isolated PostgreSQL/MySQL Focus2 passes both direct cases and five paired
named results in 115.234 seconds. Root independently verifies the unchanged
1,798-path repaired source, modes and owned cleanup; review
`77230809513d1cadd6f967003e251b017c58b7c4de00ca076fff0610146b59ce`.
The original Full146 remains failed and
no complete passing Full146 is inferred. The controlled normal HTTP/native/restart
workflow passes all five manual stages: four Calls, three native completed Attempts,
one disabled call without Attempt, four typed status changes, one discovery GET
and four original API Sessions. Owned app PID/groups and all three captured
ports/Compose labels are independently absent. Root independently verifies final source, safe HTTP observations, all selected
restart projections and retained Calls/Attempts/Sessions; native/restart review
`16bd6d9ef46e57bf8a69a02735bb5231616a53c0a1e5b02e1e3932f9198f2a6b`.
The current-head 1,800-path delivery candidate passes mandatory checking with
unchanged code bytes/modes. Browser and AuthGate acceptance remain separate. An earlier root setup
failed before controller construction because its output directory was precreated;
zero API/native requests occurred and exact owned cleanup passed. The fresh
workflow retains the identical helper and source.
The separate final source/build candidate includes the two feature documents
without changing the running matrix source. Its complete Task passes 4,560
frontend tests in 182 files, four Node checks, development lifecycle, Go
race/coverage and production assets; production build passes. Root independently
verifies every source hash/mode, the two-document-only difference and rebuilt
artifact. This source acceptance does not infer Full146 or browser results.
No product discovery behavior or native call budget changes; original failed
evidence remains unchanged.

Source review also found generic Key saves could restore private cache entries
after unmount or Session changes. The delivered actor/query and completion guard passes 103 focused tests across
five suites, types, lint and formatting; complete candidate formatting, mandatory
checking, tests and build pass. These receipts retain their original source
identities; the later Team delivery preserves the guard. Provider storage
switching and durable orphan recovery remain open after the standalone SDK.
Browser/AuthGate and genuine saved CSV/XLSX downloads remain unverified because
desktop control still reported a locked Mac after the latest unlock reply.
Overall totals stay **12 complete, 15 partial, three unstarted**; F17 and F28
remain partial. Continue partial capabilities after every checked phase delivery.

Project Key monthly modes and the Key ownership guard are delivered as
`a5bb00bc59bb36c8bbe589d8bb7bc5019fed22fa`, with exact remote main read-back.
The 35-path stage passes mandatory checking, focused UI/source gates, the original
20-case driver focus and API/restart acceptance, and the two-driver retirement
fixture repair. Historical Full142 remains failed; no new Full142 or browser
receipt is inferred. Team and Connection phases are delivered with their separate source identities.

Vault Provider storage implementation continues in isolated backend and frontend
worktrees. The bounded scope covers a reviewed future-write policy, durable creation
plans and immutable references, source-aware verification/runtime preparation and
matching existing management workflows. Original references pin their descriptor
and reader generation. New Vault writes require a stable UUIDv4 creation intent;
legacy internal writes retain compatibility. Policy review binds the selected
Integration revision rather than silently following later configuration. Automatic
cleanup authority remains an unanswered preference; no cleanup Token, worker or
complete A16 compensation is selected by this independent implementation slice.

The frozen backend proposal passes 259 named related race tests, development
compilation, vet, staticcheck and formatting. Its 24 Go outputs retain their
original source identity; V77 remains unregistered there. The matching 21-leaf
frontend proposal passes 481 related tests, seven final policy tests and mandatory
checking. A private composition now merges both onto the current Connection,
Team and Key source and registers V77 after V75/V76. Its initial compile passes,
but a new recovery regression exposed an old-policy generation fence that blocked
original UUID recovery after future policy changes. The recovery-only correction
passes focused policy and retained-revision cases. A new root-rewrap regression
also failed before a current finite reader-lease epoch fence replaced the old
historical-epoch recovery gate, preserving the immutable original plan. Fresh
composed source race checks pass 484 named results with no failures or skips.
The sole member-list fixture repair is included; formatting and mandatory checking
pass on the guarded 1,820-path source. Complete Task and production build
exit zero with source bytes/modes unchanged. Root independently verifies all
140 packet leaves, 1,820 source hashes/modes, 64 outputs/22 additions and 1,756
preserved parent paths; complete frontend tests pass 4,602 cases in 185 files.
The first six-case PostgreSQL/MySQL run exits1 in 195.343 seconds: six direct
cases pass and six fail. Migration, member-list and Vault Integration cases pass
on both drivers; replacement, root rotation and new storage lifecycle fail on
both. Recorded failures are HTTP404 versus409, a writer preparation/epoch gate,
and HTTP400 versus200 respectively. The original source remains unchanged and
owned source, process groups, label resources and captured ports are independently
verified unchanged/absent. Source diagnosis identifies two product defects: the
policy PUT reused a 129-character Connection ETag validator for its quoted
64-character policy ETag, and an inline replacement replay mapped a deleted
receipt result to404 instead of the existing409 contract. Minimal private fixes
with regression tests pass in frozen R2. Its five-leaf successor preserves 1,815
R1 paths and every source mode. Formatting, mandatory checking (60.026 seconds),
complete Task (4,602 frontend tests across 185 files) and build pass on unchanged
source; 139 uncached related race results pass. Root independently verifies its
42 packet leaves, clean 1,820-path source and artifact; source-only review is
`9ca7b44112f3355fc7131991b95dc3ac5e384d1dbb8223c3711b0c893f145dd6`.
The root-rotation fixture now captures only fixed query-stage/error classes while
preserving its five-second barrier and all epoch/finality assertions. The targeted
two-driver diagnostic exits 1 in 110.252 seconds with five balanced named failures.
Both driver diagnostics record `returned_nil` before an expected pause, with fixed
stage `other_query`; the shared failure marker does not identify which gate failed.
This is no epoch or rotation proof. Root independently verifies unchanged source,
raw pairing and owned resource/process/port cleanup; failed-run review is
`da6479d6258184f84ff9e074927565f9b502e1386711314c7f69931462f2c056`.
Callback/preparation diagnosis continues; its cause remains unproven. The controlled
runtime draft uses normal APIs and read-only projections, retaining the current
intrinsic-administrator boundary. No passing Focus12 or active storage is claimed.
The initial contextual check also caught a
unit-test selector style issue; these failed checks remain failed. Real-driver migration and
creation/recovery acceptance remain pending. No source-only result establishes
active Provider storage, automatic compensation or browser completion.

The separate R2 non-root focused run exits 1 in 165.293 seconds: eight of ten
ordered PostgreSQL/MySQL cases pass, including both corrected policy/replacement
flows. Only the storage lifecycle fails, at its second `StartRuntime` call.
Source diagnosis confirms the fixture incorrectly restarts a stopped Service;
`StartRuntime` is a one-time application lifecycle operation. A private successor
constructs a fresh Service against the same database, key ring and network policy,
preserving all outage, healthy startup and later preparation assertions. R4 passes
formatting, mandatory checking, complete Task (4,602 frontend cases/185 files),
build and one meaningful resource-free lifecycle race test. Independent source
review is `0b720a460ded7ff244237c0319c766dcf2a4e8d7043396c0f88b93c137fe963e`;
fresh real-driver validation remains pending. The failed run and exact
owned cleanup remain retained; review
`4352e8350358177aa32dcb5b29f3b3720ff07d55e08cc75dcbde147b9bad9cb3`.

The R3 root diagnostic adds only five cumulative boolean observations to the
existing fixture. All 1,819 other R2 source paths and every mode remain exact;
formatting, mandatory checking, complete Task (4,602 frontend cases in 185 files)
and build pass. Independent source review is
`bb2a1d7dc137819cd96d56608577f885a4d897fdf5a5277de38ccad501e28bd6`.
This is diagnostic preparation, with no root-cause or rotation repair claim.
Its fresh two-driver diagnostic exits 1 in 110.244 seconds. Both governance table
and schema queries were observed, but the captured context gate did not match,
and the pause was never entered. This narrows diagnosis without proving a cause.
The original deadline and assertions remain intact; unchanged source and owned
cleanup are independently verified, review
`7934fa56c7556bb2b79d2aafbe49068c8461fe6a1b6015b079fd8d0dae54d878`.

The historical Connection-head price-export candidate adopts the fifteen scoped
paths without changing its parent main. Formatting, mandatory checking, 58 named Go race
results, 65 frontend tests in four files and production build pass. The current
Connection implementation and 1,788 unrelated source paths remain exact;
source review `7093eab53ee8772b1b6632f134a2bc9daa675ee5ee7751b9aa92198fd8a1a9f6`.
Four real-driver price scenarios pass in 90.225 seconds, with seven balanced
named results, unchanged source and independently verified owned cleanup;
acceptance `256a82c975932411786099ebc9b51d32796e7686a14dca7e0584d5c967c15c3a`,
root review `da255f5bdfc472e50999ecb716f41c8835b2135d4090504407fbc080018b5227`.
Genuine saved Excel/CSV browser delivery remains pending. No downloaded file,
complete matrix or completed F15 capability is inferred.

The private root-rotation R6 fixture now recognizes the exact bounded typed
Provider inventory projection while excluding ordinary runtime catalogue reads.
The legacy map branch, CAS context/table, five-second barrier and epoch/finality
assertions remain intact. Twelve named race checks, both actual-method probes,
formatting, mandatory checking, complete Task (4,602 frontend cases/185 files)
and build pass. Independent source-only review is
`c33e3b7f7421e6e3ae049206fb4c31de779a4a1d6b7b3d0e0d86ad08a867e3d2`.
Fresh Focus12 exits 1 in 230.363 seconds: ten direct scenarios pass, including
root rotation on both drivers; only the two storage lifecycles fail at their
bounded root-migration wait. Independent source/resource/process/port failure
review is `abae78bcae205b09b260995d8ea974f4d1859d8c9de70591933ca306ac97c91e`.
Source and an actual-worker resource-free proof identify a fixture mismatch:
it checks phase `observing`, while production saves status `observing` and phase
`observation`. The actual final job state was not recorded in the failed run.
A fixture-only successor and fresh validation are pending; controlled real-Vault
GO remains pending. Historical failures remain failed. Vault storage remains
private and F28 remains partial.

The current-head CI race is separate from these private feature checks. At
`member_keys_integration_test.go:227`, the fixture registers a GORM Create callback
while the existing runtime publisher executes that callback processor. The race
trace identifies registration/compilation against `setRuntimeStatus` creation;
the failed 2,949.92-second Identity run remains failed. A scoped fixture correction
registers before workers start, atomically arms the original audit-failure request,
and removes the callback after runtime/recorder shutdown. Production behavior,
rollback, authority and restart assertions remain unchanged. Main mandatory
checking passes. Fresh exact-CI-source PostgreSQL/MySQL validation passes in
125.268 seconds with two direct lifecycles and five balanced named results;
acceptance `55e3c3eda79a76f33ebda50be33f3fef3be9b1aa3edd73701dda2b810fa4f735`,
root review `9a6043b01c125d9fe7fbb4c174556ea1840c706c3152e22455262febca60d1e8`.
All 1,799 unrelated archived-head paths and source modes remain exact. Independent
review confirms worker teardown and unchanged price paths. Commit `07ce118`
delivers only this fixture correction and the two status documents; it does not
deliver the fifteen then-pending price-export paths or private Vault storage. The
original CI remains failed and a new complete remote result remains pending.


A separate two-file SDK proposal adds explicit bounded AppRole login, with
transient closeable lease material and no Token header, cache, renewal, KV request
or service activation. Its 25 new auth cases and existing Vault/guarded transport
race cases pass (293 balanced named results). Contextual mandatory checking
passes, and commit `91f347de0593c0166602a80922ba7ef1016d4288` delivers the exact
three-path SDK/documentation phase with remote main read-back. Existing integration
identities remain Token-only; saved AppRole Integration activation stays pending.
The first controlled real-Vault SDK run fails the expired-SecretID denial: four
login requests produce three successes and one denial, with no KV calls. Pinned
Vault source explains periodic expiry cleanup, so its two-second fixture wait
was insufficient; the precise historical internal path was not observed. The
original run remains failed. A wait-only successor passes four explicit SDK
logins in 73.567 seconds: two valid 60-second leases with local Close, and two
HTTP400 denials. Its returned one-second Secret ID TTL is followed by an observed
70.005-second wait. Four unique audit request/response pairs confirm exactly two
successes, two denials and zero KV requests. Root independently verifies all
frozen source/artifact hashes and modes, exact owned resource/process/port cleanup;
SDK-login-only review SHA-256
`44b20572a2339a156c0303f9305bad14ac2b097900901d67a73c271598d43988`.
This does not establish saved Integration, namespace, ACL, Provider storage,
native inference, restart or browser acceptance.


## Historical checkpoints

The following checkpoints are historical. Their pending/running labels describe
the recorded checkpoint, not current main or the acceptance status above.

Historical pre-parent-repair checkpoint: that fixture-only successor passed mandatory checking, formatting, complete Task (4,506 frontend cases in 181 files, four Node checks, two development lifecycle checks, Go race/coverage and production assets) and production build. All 1,769 source paths and modes remain exact. Source acceptance SHA-256: `f0b5ea37af5061c653142fdcf721883431db7f7f28370f90555c2ac0041a6ec7`. It uses a coverage token cap of 200 while retaining the exact money cap, held/unknown facts, HTTP503 and final warning/Call assertions. Its combined Focus16 passed; the later parent-fixture successor and expanded Focus18 are recorded below. Full141 is active on that successor; controlled API, restart, browser and delivery remain pending. Current main CI37538989100 passes backend/frontend checks but fails database integration because GORM callback removal in the Personal monthly test races runtime reads; build artifacts and authentication restart were skipped. Previous failures remain retained.

## Provider credential KV-v2 SDK boundary (2026-10-07)

The standalone additive `pkg/vault` SDK supplies bounded credential CAS0 Write,
exact-version Read and owned-version Cleanup operations with a separate
high-entropy marker. It adds no persistence, application authorization, root
inventory domain, storage policy or runtime Provider storage switching. Durable
business claims, compensation records and independent cleanup authority remain
future control-plane work. Controlled package tests are separate from actual
Vault, Provider storage, browser and complete F28/A16 acceptance. The accepted
current phase and future Project Key source remain separate from this SDK slice.
See [Credential SDK boundary](SECRET_STORAGE.md#provider-credential-kv-v2-sdk).

The delivered `c2368ddb` baseline has passing Actionlint37553034794 and GolangCI37553034861. CI37553034802 is running and is not yet accepted. These remote checks belong to that preceding phase, independently of this standalone SDK source slice.

## Combined candidate regression checkpoint (2026-10-07)

Current bounded acceptance status: the R4 candidate passes Full141 and independent cleanup review. Vault R7 API/restart and Personal R6 API/native/restart pass on their separately recorded R3 artifact. Commit `c2368ddb0251415e72bfeee948ad23ed7a173ee4` delivers Vault Token integration/root-domain support and Personal Key/Project aggregate monthly modes, with exact remote main read-back; browser/AuthGate and complete F17/F28 acceptance remain pending. Older pending/failed checkpoints below are historical and retain their original source identity.

The exact R4 1,769-path candidate passes the unfiltered Full141 PostgreSQL/MySQL regression: 141 ordered scenarios per driver, 282 direct lifecycles, eight constraints, 4,944 balanced named results and five package completions in 2,937.954 seconds. Acceptance SHA-256: `03af8b6e72e578f1ccb777c4248ed591cfd4958143b9c4af0134ef92b654cb1e`; root independent review SHA-256: `7c770309509b762ec740a8f69ef4b550e25d138bb9b59287d087861b3e7640c1`. Source floor `50f1f7080ce725dd76725327ee35bd771ba4ac58c32305353407032211a9f4aa` and modes `3f1ae9fa6359b595a1da6cef058ab4e5180fcc45270c9acc19379b50ae60207a` remain exact. Owned Compose resources, captured PID/PGID and both database ports are independently absent. Text-output named pairing and package completions are checked; no JSON-event provenance, browser or complete F17/F28 acceptance is inferred. Earlier failed runs remain retained.

The separate Project Key R2 proposal passes private source gates: formatting, mandatory check, complete Task (4,511 frontend cases in 181 files, four Node checks, two development lifecycle checks, Go race/coverage and production assets) and build. Source-gate receipt SHA-256: `70cbd2bb3dc2dd303872d22220da929a65e16889e4fa0ac83e92d44da1670618`; its 1,773-path source floor is `07de5ee680c6db50c1efa6f556e8756c636aa156d4784fbe78dc3081b344cd61`. The reviewed HTTP fixture successor expects the existing DELETE HTTP204 and changes no product behavior. Focus20 R2 failed in 260.432 seconds with exit1 and remains unaccepted. Source remained unchanged and owned cleanup was verified; diagnosis is pending. Full142, controlled runtime, browser and delivery remain pending. Accepted parent Full141 does not accept that later source.

Overall totals remain **12 complete, 15 partial, three unstarted**. F17 and F28 remain partial. Genuine saved XLSX/CSV browser-file delivery remains pending; API workbook inspection or a prepared notice is not download acceptance. No capability or phase completion is claimed by these new receipts.

The exact 1,769-path pre-race-repair candidate now passes its fresh combined Focus16: 16 direct PostgreSQL/MySQL scenarios and 19 balanced named results in 240.332 seconds. Acceptance SHA-256: `b3308194a313d727da9a9182376df88ef8347bcbf33fafa389bb2a9a7fa7dc2a`; independent source, owned Compose resources, PID/PGID and both ports are verified. The unselected Personal monthly lifecycle still has a CI-confirmed test callback race, so this is not full regression acceptance. The reviewed one-leaf repair installs GORM callbacks before workers, uses atomic arming, and removes callbacks after worker shutdown; it preserves all domain/version, policy, native and retry assertions and passes 12 focused race checks. It is carried into the working tree. The new 1,769-path candidate passes formatting, mandatory check, complete Task (4,506 frontend tests across 181 files) and production build; source receipt SHA-256 `270834283fa38d0bb371ab0b70f3bba6b0630da0057219a61b2f020a7ce0c659`. Fresh Focus18 passes all 18 direct PostgreSQL/MySQL lifecycles and 21 balanced named results in 245.378 seconds, including the repaired parent; source/modes and owned resources, PID/PGID and ports are independently verified. Full141 now passes on that exact candidate, as recorded in the R4 receipt above. The isolated CI-head-only two-driver regression also passes: two direct lifecycles and five balanced named results in 100.201 seconds, receipt SHA-256 `ef9825456218524d75da65d5f9c25a49e99f8aea06cb0366a7001b57511475b4`. Mandatory check and 12 focused race results passed for that exact one-leaf source. Commit `4f8c9cfe1afec41b843b9d589df77e031edaf3bf` delivers only the callback fixture repair; remote main is verified at that SHA. Actionlint37547747363 and GolangCI37547747347 pass; CI37547747256 was superseded and cancelled. The documentation checkpoint `618ee0131592adc0342f51cf5dd3d0f1a344d497` is pushed and its Actionlint37547982292 and GolangCI37547982303 pass; the exact `618ee013` baseline CI37547982291 now passes all four jobs, including PostgreSQL/MySQL integration, authentication restart and build artifacts (completed at 00:24:30 UTC). This baseline CI result does not accept the pending feature source. A fresh current-working-tree mandatory check also passes after the repair. Feature code and earlier failed receipts remain separate. Commit `c2368ddb0251415e72bfeee948ad23ed7a173ee4` delivers the bounded code; complete F17/F28 acceptance is not claimed.

Controlled API failures remain separate. Personal setup required the existing `/api/v1/projects` route, and rejected-write equality excludes only the validated per-read `quota_usage.as_of` timestamp. The earlier R5 TPM assertion expected rate_limit_exceeded where source-derived TPM token-window rejection is quota_exceeded; its rejected body was not retained and remains unknown. The minimal reviewed helper R6 preserves all other gates and now passes controlled API/native/restart: 19 logical calls, seven native attempts (six priced and one unknown), 12 pre-admission denials and five original Sessions. Result SHA-256: `b5de98a326cc009864b555204203e3f60375382afc9ac33d0b4ac55b03a9e5ba`. Root independently verifies identical bounded 16-table snapshots, four warnings/one read, exact source/artifact and owned cleanup. TPM is now actually observed as quota_exceeded while RPM remains rate_limit_exceeded.

Controlled Vault API R7 passes configuration, retained-auth Cleanup, persistent Vault/application restart and both original API Sessions. Root independently verifies six revisions, two probes, four stage commands, ten paired Vault requests (six successful effects and four ACL denials), seven observed root domains, the real 300-second observation and completed retirement. Before/after/finish database projections are identical; source, artifact/config and exact owned cleanup remain verified. Root API-only review SHA-256: `efb14524ce1c06fe6de54be050d03e7d10946a6cd3c93fde25be6886c73cf538`. Earlier failed runs, including the restart-wait failure, remain failed; the fixed loopback-port successor does not reinterpret their missing evidence.

Both Vault R7 and Personal R6 API runs use the original R3 artifact `19320f5e16792e790748feb0a69fd6f70d1c3fc4f571a5e27c6e70d8494b5df4` and source floor `1b78ece3a09c458141ceff256f702822c7e9f0b714766e8458a3b92520276101`. The later R4 parent-callback repair changes a test fixture only; its whole-source/backend-test hashes and gate receipts remain distinct even though production code is identical. API results do not become R4 whole-source acceptance. Expanded Focus18 and accepted Full141 belong to that later R4 source. The accepted R4 regression does not relabel the original R3 API/native/restart receipts or establish browser or complete F17/F28 acceptance.

Browser, bilingual controls and AuthGate recovery remain pending for this phase. Desktop control reports a locked Mac and the in-app browser cannot attach a new webview; those observations do not prove a product cause or UI/download success. A bounded fresh normal-browser gate must verify the existing controls, independent authority and original-Session restart against an exact reviewed artifact. No complete F17/F28 or full-objective acceptance is claimed.

The isolated Vault/Personal Key/Project monthly-mode composition passes mandatory checking. Its first complete Task run fails three obsolete Project-rejection frontend assertions; 4,503 other frontend cases pass and Go race/coverage passes. A test-only successor passes all 23 focused API cases, types, lint and formatting while retaining Project Key and invalid-policy rejections. The first 16-case PostgreSQL/MySQL focus fails five scenarios in 225.387 seconds: the Personal migration still requires the replaced V73 scope constraint on both drivers; Project lifecycle creation returns HTTP404 on both drivers; PostgreSQL Vault completed-Cleanup replay differs. Failures remain retained and targeted diagnoses are active. No full141, production artifact, restart, browser or delivery acceptance is inferred. Owned Compose resources and both database listeners are absent.

The reviewed successor changes two Go integration fixtures, one frontend API test and only the direct/indirect classification of already pinned pgx v5.10.0. Frozen product paths remain 1,769 with 1,052 backend paths. Final mandatory checking, complete Task (4,506 frontend cases/181 files, four Node checks, two development lifecycle checks, Go race/coverage and production assets) and build pass with every source byte/mode unchanged. Source acceptance SHA-256: `1612e344df86a39b8d8cddfc8b32d02d3cc1837c3d00dacf2db6b275779240d7`. Initial missing-Git-metadata build failure remains retained; the successor build uses read-only current main metadata without changing product source. Fresh combined Focus16 fails in 235.388 seconds: 14 direct scenarios pass, including the complete historical Vault Cleanup regression and both Personal Key cases on each driver; the two Project scenarios fail the held/unknown percentage assertion. Its 150 known settled Tokens against a cap of 187 correctly produce an 80-percent Tokens warning independently of unknown money. A test-only cap change to 200 is being prepared while preserving unknown money, held accounting, the later HTTP503 and all final warning/Call facts. The whole run remains unaccepted; source and owned resources/processes/ports are independently verified. Full141 and controlled runtime/browser/restart remain pending. A separate Project Key monthly-mode source proposal is being prepared in an isolated copy; no current candidate or main files are changed by that next package.

## Current work and next gates

The latest delivered feature baseline is `b69134da65cadfb26236f9bfb3875457dded4857`: bounded Vault client foundation, with exact remote main read-back. Source checking, complete Task and build pass. Current-head Actionlint37531119952 and GolangCI37531119909 pass; CI37531119990 now passes all frontend, backend, PostgreSQL/MySQL integration and build-artifact jobs.

The durable Vault backend and interface are composed in an isolated candidate: frozen GORM V72, separate encrypted writer/reader Token revisions, durable explicit probe commands, seven-domain root inventory, and the bilingual existing Secrets workspace. The first mandatory check retained a test-only staticcheck switch failure; its exact equivalent successor passes. The final isolated candidate passes mandatory checking, complete Task (4,479 frontend tests/178 files, four Node checks, two development lifecycle checks, Go race/coverage and production assets) and build. All 1,750 product source paths/modes remain exact; source acceptance SHA-256 is `634a7558c06e1e2f2e771a33deaf18d08d44e4607979ffdbd674033dbea61faa`. The same 1,058-backend composition passes its independently bound PostgreSQL/MySQL focus: eight direct lifecycles, 11 balanced named results, 190.298 seconds; acceptance SHA-256 `d9b9be69c4b978b0a3b2852940567bb3ea94314ad0c0fbc0431b9372d285bf66`. All source bytes/modes, owned Compose resources, captured process and both refused ports are independently verified. The original unfiltered full137 regression passes 274 direct PostgreSQL/MySQL lifecycles, eight constraint cases and 4,820 balanced named results in 2,898.253 seconds. Acceptance SHA-256: `60badf2fcab15e024f5115bdb0b4a44e09ec52585f51b89f92ad099939894037`; independent source/resource/process/port review passes. This receipt belongs to the original candidate only. A separate real-Vault API run exposed historical Cleanup returning HTTP404 after auth replacement; the retained current GORM destination primary key conflicts with the historical predicate. A minimal production fix and regression are being prepared. Browser and restart acceptance remain pending.

Personal Key monthly modes now pass renewed mandatory checking, complete Task (4,495 frontend tests/179 files, four Node checks, two development lifecycle checks, Go race/coverage and production assets) and an exact-source production build. The independently verified 1,758-path physical candidate has source acceptance SHA-256 `6205cd50e694ad6e8e3e899274a378ddf75417cab6ddbd0759a696f0de5cb0c1`. The initial two QF1001 failures, exact Boolean-equivalent successors and source-confirmed HTTP503/quota_usage_unknown fixture correction remain traceable. An npm10 build changed only optional Linux libc lock metadata; its drift is retained as unaccepted, the exact lock was restored, and an npm11 rebuild preserved every source byte and mode. The first focused runner rejected a Git-mode string versus filesystem-mode integer mismatch before resources; its exact encoding successor launched successfully. The real PostgreSQL/MySQL focused run then failed race detection in the PostgreSQL lifecycle: live GORM fault-callback registration and shared fixture arming state raced background runtime reads. The failed 125.217-second run is retained; all source bytes/modes remain exact and owned containers, networks, volumes, process group and both ports are independently absent. The fixture-only synchronization repair now passes renewed mandatory checking and six focused race tests (16 named results). The exact repaired 1,758-path candidate passes both migration and lifecycle cases on PostgreSQL and MySQL: four direct cases, seven balanced named results in 100.191 seconds. Acceptance SHA-256: `70a6d88039d4df9c0f294845fe89840b89e3cb64dcc91fb4715453d3fbd37a31`; source/modes and owned cleanup are independently verified. Prior complete Task/build evidence remains attached to the earlier fixture floor; it is not relabeled as a repeated gate.

Project aggregate modes have a frozen V74 source/driver proposal with passing scoped unit/UI, race, staticcheck, vet and formatting checks. Its final composition will retain the Personal callback repair and the pending Vault Cleanup correction. Run the complete 141-case-per-driver regression on that reviewed final candidate; no redundant full139 run is planned. Actual Project driver, runtime and browser acceptance remain pending.

Official Vault setup established four real denied writer/reader ACL controls, but its partial inspection failed because the audit oracle expected update for a denied CAS=0 create. The failed run and owned cleanup are retained; exact operation/path-specific oracle corrections now pass 18 pure tests and remain pending real probe acceptance. Desktop control still reports a locked Mac after the latest unlock reply, so browser acceptance remains pending. None of these candidates is delivered or accepted as complete runtime/browser/restart proof.

Team monthly modes were delivered as `d1fc5926ff71ba2875fc894a6bba4caec29f4b3b`, with exact remote main read-back. The 37-path phase passes exact staged checking, complete Task (4,402 frontend cases/175 files), build and focused PostgreSQL/MySQL regression. Team-head Actionlint37529983988 and GolangCI37529983972 pass; CI37529984012 subsequently ended cancelled and is not recorded as passed. The containing commit delivers the bounded Vault client foundation: carried package races, mandatory main/scoped checking, complete Task (4,402 frontend cases/175 files, four Node checks, two development lifecycle checks, Go race/coverage and production assets) and build pass. All 1,730 product source paths/modes remain exact; generated coverage output is excluded. Acceptance SHA-256: `4d8b7d047bfbae79cd3f357eef1082cdd05a70ed62345bb5fc55e672744643b6`. Initial lint failures are retained; equivalent condition/switch and explicit Close handling repairs pass. This source-only package delivery does not establish real Vault, database migration, management API, browser or restart acceptance. Durable Vault configuration and the existing administration workspace are being implemented separately. Personal Key monthly modes are the next bounded F17 gap; Project and Team-member hard policies remain unchanged. Overall F17/F28 and the full objective remain active.

Previous delivered main is `03c6fe1e53a8c3a7fd31ec9415cc87f9ef8ac02c`: independent Personal monthly threshold behavior and reviewed Provider name editing, following routing draft retention `2723a54`. Exact local staged checking, complete Task (4,379 frontend cases/175 files), production build and full133 PostgreSQL/MySQL regression pass. Remote main is read back at the exact SHA. Current-head GolangCI37527570412 and Actionlint37527570525 pass; CI37527570432 subsequently ended cancelled; the current main CI is tracked separately. Overall totals remain **12 complete, 15 partial, three unstarted**; the full objective is active.

The Personal monthly behavior and Provider name phase excludes pending Excel source. Its exact staged candidate passes mandatory checking, complete Task (4,379 frontend cases in 175 files, four Node checks, two development lifecycle checks, Go race/coverage and production assets), production build and its own original full133 PostgreSQL/MySQL regression: 266 direct lifecycle cases, eight constraints, 4,756 balanced named results and five package completions in 2,903.233 seconds. All 1,718 tested source paths/modes and owned resource/process/port cleanup are independently verified. Acceptance SHA-256: `ccb38e2b4b508f6304c2246b0d161e18e66263a0d023fd7ca332970af26b4ac2`. This candidate has 1,034 backend paths; the older private 1,036-backend full133 R2 and separate Personal/Provider browser receipts remain accurately scoped historical evidence. Commit `03c6fe1e53a8c3a7fd31ec9415cc87f9ef8ac02c` delivers this phase with exact remote main read-back.

Current acceptance work:

- **Routing drafts:** the transient actor/Model draft repair passes mandatory checking, complete Task (4,302 frontend cases in 173 files), formatting and production build. Controlled browser R3 confirms invalid 99/101 totals dispatch no writes, bilingual 60/40 draft retention across real successful Session renewal, one complete eight-binding PUT200, independent read-only controls and no unauthorized price reads. Same-artifact restart preserves three original API Sessions, two browser Sessions and every sampled row; Calls, Attempts and Keys remain zero. Scripts exit zero and owned cleanup is independently verified. Acceptance SHA-256: `3704ef7fe4833485f0088a1adaf8ab31c5b0c552932e8c0e1b92cfde9724b958`. Changed binding identities require explicit review and have source-test coverage. An actual Session-error unmount remains outside the unsent-draft guarantee; earlier failed runs stay retained.
- **Personal monthly behavior:** private R4 passes the complete controlled workflow: independent zero stopping, both alert-only success, blank/null inactive modes, EN/ZH drafts, Base UI Escape/keyboard confirmation, read-only/denied gates and Key parent100 alert versus Key200 hard. Seven gateway calls yield three native completed attempts and four pre-admission rejections with null usage, uncaptured pricing and no attempts. Same binary/config/database/journal restart preserves five original API Sessions, four browser Sessions and all sampled rows. Helper/root exit zero and independent owned cleanup pass. Acceptance SHA-256: `75b6b5265014caa9c63b531275c0f89f98ab8fb7512e00c217a446388eccf8cc`. The earlier null-token oracle and physical-column failures remain retained. Contextual carry is applied and current-main gates pass; the exact isolated staged regression passes and the containing commit delivers this bounded phase.
- **Provider metadata:** controlled R6 passes independent read/write/denied gates, two conflicts, explicit current review, a response-loss publication and immutable retry after genuine Session-error recovery and same-artifact restart. Five editor writes produce three typed name audits; children, original Sessions and zero inference/Keys remain verified. Helper/root exit zero and independent cleanup pass. Acceptance SHA-256: `3688b5cc9932c45f552f80697bec9e8063b45955223521864dab732a8cfcfa97`. Earlier observer/hostname failures remain retained. The contextual carry is applied and current-main gates pass; the exact isolated staged regression passes and the containing commit delivers this bounded phase.
- **Team monthly behavior:** final composition passes 197 focused frontend tests, mandatory checking, complete Task (4,369 frontend cases in 173 files) and build. Focus R3 passes all18 direct cases across PostgreSQL/MySQL,25 balanced named results in400.674 seconds, with unchanged source/modes and independent cleanup. Acceptance SHA-256: `b78ef44670da96ab7f14253b3401b0f22dfaeaadae697a965a1d3c16cd5ee625`. Earlier failures retain the test-only over-width PriceRate ID and insufficient conservative money-reservation allowance; production enforcement and original denial assertions remain unchanged. Full135 R1 now passes 270 direct PostgreSQL/MySQL lifecycle cases, eight constraints, 4,792 balanced named results and five package completions in 2,983.417 seconds. Source1,723/modes and independent cleanup pass; acceptance SHA-256: `658035c5b541e3a25c11084f88a05f22ad35b74ba8bc943288a33d2f2d7b639b`. This is private 1,042-backend evidence. The contextual 31-path code carry and six rebased English documentation/rule paths pass exact staged checking, complete Task (4,402 frontend cases in 175 files, four Node checks, two development lifecycle checks, Go race/coverage and production assets) and build. All 1,724 staged source bytes/modes are independently matched. This 1,040-backend candidate excludes pending Excel changes and passes its own nine-case-per-driver focused regression: 18 direct lifecycles and 25 balanced named results in 465.818 seconds; acceptance SHA-256: `36f2953fd99f1f689656f4698b88da8335ea61b34268ca05accd66c68a30e262`. Exact owned Compose resources, captured process and both ports are independently absent. The accepted predecessor full133 remains distinct from the private full135; no evidence transfer or redundant full135 claim is made. The containing commit delivers this bounded Team phase. Controlled browser R1 ended with an idle timeout and stays failed. Fresh R2 passes EN/ZH, independent token/money permissions, Escape/focus, zero stopping, alert-only admission, inactive null caps and original-document restart. Seven gateway calls produce four completed attempts, including one disclosed Personal warmup; Team settles15Tokens/USD15 while member Personal stays0. All ten original Sessions and sampled rows remain unchanged; source and owned cleanup are independently verified, and temporary tabs are closed. Acceptance SHA-256: `2297d210b6fa437d47a94bacf7d1ea9510db6bd2dcd2dc541678e270327b69ac`. Six incidental GET500 registration/notification responses are retained with unproven causes; no all-request health or ordinary editor-remount uncertainty guarantee is claimed.
- **Excel price export:** source and dual-driver checks pass; genuine saved browser downloads remain unresolved. A fresh current shipping binary, empty disposable catalogue and real focused browser produce prepared notices for one Excel and one original CSV click, but both documented download events time out after ten seconds. All owned resources are cleaned. This does not establish a product cause or blanket browser incompatibility. HTTP200/API workbook inspection do not prove saved delivery; the bounded Excel source phase is checked above and browser acceptance remains open.

Root independently verifies owned resource, process and port cleanup for failed Personal, Provider and routing runs. Failures are retained and are not relabelled as accepted delivery.

SDK guidance `c67f752d20f26ba62f44e0afe4cfd664e8f73b64` completes the finite F19 scope. Its mandatory check, 4,267 frontend tests, build, eleven controlled browser checkpoints, six unchanged original Sessions and independent cleanup pass. Acceptance: `53278838e4c85610dfde1c5dc236cbe589bae3da22eadc039803c769e72667b5`. All three exact-head workflows pass. This does not certify SDK version compatibility or complete broader release cases.

## Earlier delivery checkpoints

Commit `8f17d12` delivers Team creation initial limits V63 after mandatory
checks, complete Task testing, full115 on PostgreSQL/MySQL and controlled
production/bilingual/original-Session restart acceptance. Its delivered
predecessor is Restore recovery `e9003c972bf1d1ee3f2f3495d51cb7cf224091f8`;
Member warnings were delivered as `f7b31b1a111937b5bc2885b942b12f087c27b08b`.
Commit `4adaad2` additionally delivers default-rule SAVE recovery and
ordinary local draft renewal, preserving Member/Restore/Team behavior. Mandatory
checking, complete Task (3,565 frontend cases/157 files), production build and
controlled PostgreSQL bilingual/original-Session restart acceptance pass. Both
ordinary targets also retain exact drafts across genuine periodic Session reads.
Key119 and controlled production acceptance were committed and pushed as
`cf05c57121ea770b1af04163b2c5b7f59c5fab58`, with exact remote main read-back.
Initial Team Model access is delivered as `3e21b8d`; single-model browser elapsed
time is delivered as `d3afeac`, with exact remote main read-back. Role descriptions and Connection metadata passed complete124 on both databases:
124 ordered cases per driver, eight constraints and 4,491 matching named events.
The carried main source passes mandatory checking, complete Task testing
(3,929 frontend cases in 163 files), and the production build. Controlled Role
description browser and original-Session restart acceptance pass. Connection
filters, permission separation, two conflicts and a reviewed save passed, but
its fourth real save returned 503 before the planned response-loss fault. That
run remains failed; its owned resources are absent. A fresh run passed all five
browser PUTs, response-loss and Session-error recovery, and exact original
retry after same-artifact restart without authenticated document reload. The
retry added no audit; all owned resources are absent. The containing commit
delivers this checked Role/Connection phase.

Commit `c800466` delivers immutable, explicitly assignable Procurement,
Finance and Operations duty templates through frozen GORM V68 and the existing
reviewed Member assignment workflow. Complete126 passes 252 ordered PostgreSQL/
MySQL scenarios, eight constraints and 4,540 matching named results. The initial
focus retains its test-only HTTP-status oracle failure; corrected focus passes.
Main format/check/full Task pass (3,948 frontend cases in 163 files), along with
both real authentication/gateway persistence lifecycles. Browser preflight found
stale intrinsic-versus-explicit guidance; the paired locale-only correction passes
106 focused cases, final mandatory checking and production build. Backend source
remains unchanged from the accepted matrix and persistence tests.

Controlled PostgreSQL browser acceptance confirms bilingual immutable duty views,
finite candidate labels, a non-mutating draft, two real reviewed PUT200 saves,
exact Finance grants/counts (0 → 3 → 0 permissions; 0 → 1 → 0 assignments), retained
Member identity, ten builtin PUT/DELETE403 responses and unchanged definitions.
Original API Sessions and the administrator browser remain usable after same-
artifact restart, without authenticated document reload between add/remove.
Recorded scoped access-summary names remain recorded content. The server recorded four GET500 and one GET503 during refreshed reads; the
original cause and browser receipt are unproven. Cancellation is only an inference.
Functional saves and fresh reads pass; no all-request health claim is made. Owned
app, browser tab and labelled Compose resources are independently absent. The
`c800466` commit delivers this bounded functional slice. The full objective and capability totals remain unchanged.

Separate private member-catalogue input/output price source passes focused real
PostgreSQL/MySQL and 127 frontend cases. A further private monthly-request-count
slice passes 155 related frontend cases; it uses one Personal or exact Team report,
with explicit scope and returned UTC/freshness metadata. Price is now carried on
delivered Duty main; monthly counts remain private. Its complete126 transaction
gate is running. Main checking passed; complete frontend testing initially passed
3,972 cases and failed one old reactivation-copy assertion. The narrow test-only
repair expects explicit assignments, retaining all original state and request
assertions and is committed/pushed as `7e55509`, with exact remote read-back.
Focused MemberState/localization passes 29 cases. Renewed format/check/full Task
passes 3,973 frontend cases in 163 files, Go race/coverage, four Node checks,
two development lifecycle checks and production assets; the production build
passes. Price controlled browser/restart acceptance and complete126 transaction
matrix pass. Price is committed/pushed as `20d6049fc44809e9065c63006d5440cdafd58fef`
with exact remote main read-back. Monthly6 is now carried separately on main;
all 1,600 product/test/dependency/Task paths match the repaired complete
regression (4,005 frontend cases / 164 files). Current-main check/build and
controlled three-native bilingual browser/restart pass. The containing commit
delivers the bounded monthly request cells; F19 and formal totals remain open.

Team focused R4–R8 remain failed. R4 exposed a pinned PostgreSQL GORM DropIndex
fixture syntax error and missing private context headers; R5 confirmed the
index fix but found unauthenticated rejection happened before the controller
header. The sole context GET now installs the existing private header middleware
before the original Session gate; service authority and assertions are unchanged.
Source regression/race/check gates passed. R6 passed eight direct cases but
failed both lifecycle cases at missing review: expected428, actual400. A narrow
Team-only required-header successor has passed source checking; malformed400 and legacy
behavior remain unchanged. Source/index and independent cleanup passed. R7/R8 later exposed fixture-only
offboarding column and uppercase approval-ID errors. Offboarding now uses the
actual timestamp with exact persisted readbacks; approval data now uses
the project identifier generator. Both test-only corrections have passed narrow
race tests and mandatory checks on main and the isolated Key candidate. Fresh
R9 passed all ten selected PostgreSQL/MySQL cases, with exact source/index and
independently verified cleanup. Unfiltered full115 then passed 115 ordered cases
per database, eight constraints and 3,963 named RUN/PASS events. PostgreSQL took
1,029.52s, MySQL 1,404.50s and the whole command 2,471.304s. The complete
1,584-path source/index stayed exact; owned containers/networks/volumes are
independently absent. Controlled production acceptance subsequently passed.
A length-only source audit did not establish
canonical identifier validity and is explicitly superseded. Delivered Team main Task passed
3,539 frontend cases/156 files, Go race/coverage, four Node checks, development
lifecycle and production assets. Both databases also passed real-process Session,
gateway and persistent-revocation lifecycle checks on this carried source.

Personal+Project Key V64/V65 warnings now have a reviewed main source carry;
final main and production gates have passed. The isolated candidate passed complete Task on
1,616 exact source paths: 3,661 frontend cases/156 files, four Node checks,
Go race/coverage, development lifecycle and production assets. Both inherited
Team corrections are carried with narrow race/check gates. The 119-case real
matrix and native/bilingual original-Session browser acceptance subsequently passed.
Actual focused R6 passed 12 of 16 direct cases; source-bound Personal operational
diagnostics and a fresh Project receiver then passed their reached assertions
in R7 on both drivers. R7 ended with 14/16 passes: both Project lifecycle cases
exposed a downstream global operational-zero oracle. A test-only Project
diagnostic successor preserves exact captured publication/job/recipient provenance
and SMTP-zero checks. Fresh R8 passed16 direct cases/87 named events with
exact source/index and independently absent owned resources. Full119 passed both drivers: 119 ordered cases per driver, eight constraints,
and 4,370 named RUN/PASS events without failures/skips. Owned resources are
independently absent; prior failed runs retain their failed status.
Warning thresholds are 80% reminder and 90% critical; unknown settled coverage
never becomes an estimated percentage.

Restore remote frontend/backend, Actionlint37384395141 and
GolangCI-Lint37384395016 passed. CI37384395198 is now successful at the exact
delivered Restore head, including both databases, authentication/native process
restart and artifact builds. Member CI37383852751 was cancelled after the later Restore push;
its interrupted checks are not passes. Member independent lint workflows passed.

## Delivered default-rule save recovery

Commit `4adaad2` extends the existing transient submitted-intent owner to
already-dispatched User/Team default saves. Exact policy/reason/If-Match survive
transient AuthGate unmount, with fresh Session/permissions/target reads required
before manual recovery. Matching GET never proves historical save success.
First pre-write409 and rejected uncertain retry remain separate; explicit local
abandonment retains the unknown outcome and requires fresh review.

A successful ordinary Session renewal previously unmounted an undispatched
editor. The narrow local actor/target seed now preserves that mounted draft
while hiding it until renewed authority succeeds. It never authorizes a save or
retains ordinary drafts above AuthGate; actor/target departure still clears it.
Changed policy/currency requires explicit review. The baseline regression failed
before repair;110 related cases and complete main3,565-case frontend tests pass.

Artifact `e3b22ba6d074094db21219ec57d1b9b5f82155dad5dce6203a2d29907479e2e6`
passed both ordinary pre-submit periodic renewals and nine controlled browser
PUTs covering conflicts, committed response loss, manual gate Retry, exact replay,
abandonment/Escape/focus, bilingual reads and original-Session process restart.
Eight action-filtered update audits were counted; metadata decoding is not
claimed. Existing resource policies and zero native resources remained unchanged;
owned listeners, tab and Compose resources are independently absent. Backend,
schema and accepted full115 remain unchanged. Defaults stay future-creation
templates, separate from current account enforcement.

## Accepted initial Team Model access

The checked V66 phase restores Model access between Basic information
and Resource limits without changing V63 limits, SAVE/editor seeds or Key
warnings. Current create/model authority, Provider-label redaction, retained
off-page selections and one aggregate selected-set proof fence atomic explicit
grants. Empty selection grants no Models. Dispatched IDs/token remain with the
original UUID/body/If-Match through same-actor recovery. Complete current grant
publication is separate from route availability and native inference success.

The isolated frontend passed212 related cases and3,706 full cases/157 files,
plus types/lint/format. Focus R2 retains fourteen passes and two migration fixture
failures. The one-file live-parent correction preserves foreign keys/history;
Focus R3 passed sixteen direct cases and nineteen events on both drivers.
Full121 passed on the unchanged isolated 1,630-path floor: 121 ordered scenarios
per driver, eight constraints and 4,397 matching named results. The runner passed
its complete source/Git/index guard before and after execution. Its worktree
subsequently disappeared, so a later strict live review failed and is retained.
Root closed the completed run using the captured final guard, unchanged text
review, exact current Go/dependency equivalence and independent resource absence;
no later live worktree check is claimed. Main has an independent forty-four-file
candidate carry preserving unowned paths/staged identities. Main format/check,
complete Task (3,759 cases/158 frontend files), dual-driver auth/gateway lifecycle
and production build passed. Actual native/browser/restart and separate durable-evidence review subsequently
passed as recorded below; the original failed helper remains failed. The production helper's first source draft had incorrect POST
phase indices; an unexecuted two-line successor passed57 preparation checks.
Those source checks are not browser or runtime acceptance.


The first controlled production run reached normal three-user sign-in, paged
selection, redacted Provider labels, unauthorized selector hiding and an empty
Team 201 with zero assigned Models. Its evidence collector then failed by
selecting an unused `created_at` column absent from `team_model_grants`. This is
a failed helper run, not accepted native/browser/restart delivery. Its exact
application listeners and Compose resources are independently absent. The narrow
collector correction was separately frozen and used by a fresh run.

The second controlled production run passed all ten browser checkpoints through
original-request replay: three normal logins, paged selection and Provider
redaction, unauthorized empty creation, a real selected-review409, committed201
withheld as503, actual AuthGate unmount/manual recovery, EN–ZH–EN retained fields,
same-artifact restart and byte-identical original200. It then failed before any
native request at a helper precondition. The first diagnosis identified an
independent incorrect active-coverage requirement; subsequent source review and
fresh database readback established that the earlier global-zero personal-grant
assertion fails first. Model creation explicitly grants each Model to its creating
controller; these existing grants must be preserved rather than deleted. The
RPM-only calendar must still be checked inactive before admission and active
after successful calls. Independent readback confirmed zero calls and
attempts, inactive UTC accounting, two Teams/receipts/grants/receipt children; all
owned application/database listeners and Compose resources are absent. This run
remains failed, and native acceptance is not claimed. A narrow helper successor
must validate the actual inactive-to-active accounting transition using only the
four disclosed probes, without warmup, journal reset or fabricated coverage.
The preserved root failure receipt SHA256 is
`73e87cb6c201a6763b3da2d6ce139d85ca42bce9741f370212e8a89c37817be7`.


The third production run was rejected before forwarding the original retry:
root clicked before all postrestart creation-authority reads had completed. Its
actual browser checkpoints and cleanup are retained, but it is not accepted.
The fourth run waited for fresh reads and passed all ten browser checkpoints,
including the byte-identical original200 replay without duplicate creation. It
then failed before any native probe because the helper required zero global
personal grants. Readback confirmed exactly51 explicit setup-controller grants,
zero grants for all three browser actors, zero calls/attempts/Keys/Projects and
exactly two Teams/receipts/Team grants/receipt children. The source at
`internal/routex/service/catalog_models.go` intentionally creates those controller
grants. All owned tabs, listeners and Compose resources are independently absent.
A narrow source-only helper successor must retain the exact51 baseline rows,
prove the browser actors remain grant-free and keep the same four native probes
and two upstream requests. This corrects the oracle without changing product
behavior or weakening Team isolation. Fourth-run failure receipt SHA256:
`cdc26cff9e531ee035f21069915349e769540bd860bdadbcb701bcf7e97888a1`.

## Historical Member warnings and Restore recovery


Member warning main and remote are `f7b31b1a111937b5bc2885b942b12f087c27b08b`.
Member V62 is checked/committed/pushed with exact remote read-back. Restore
recovery was subsequently delivered as the separate e9003c9 phase.
Project V61 and Team-member V62 are delivered. The Member phase included
real-driver fixtures, measured integration deadline, paired rules and four
related documents. Corrected PostgreSQL/MySQL focus, mandatory main checking,
complete Task testing (3,365 frontend cases/151 files, Go race/coverage,
four Node checks, two development lifecycle checks and production assets) and
current main production build passed.

Fresh controlled PostgreSQL production/bilingual/private-history/original-Session
restart acceptance passed against binary
`7fbdaa881f1c6957bb0b3e457cf763b07590b2dac0937192c1f8ad9562fb6375`
and all1,559 exact main source paths: six native completions/four observations
and sole-member inboxes, single-read200/read-all204, preserved removal/rejoin
history, empty owner/peer/admin inboxes and no native/login replay on restart.
Owned tabs/listeners/Compose resources are absent. Prior helper runs remain
failed: omitted public recipient field, then nonexistent final fanout tables;
both were corrected solely in test helpers and fresh full workflow rerun.

Full113 R1 failed: PostgreSQL passed all113, MySQL failed only the existing
Personal-warning unpublished reconciliation at fixture line413. New Member
cases passed; source/index and cleanup guards passed. Log SHA256
`e948286d79ee24e2346551314ee1ff5bf5250fd7bef114a757d6fe38e17f7392`.
A narrow fixture successor refreshes a valid baseline before each deliberate
unpublished mutation; production deadlines and all rejection assertions stay
unchanged. Fresh focused dual-driver Personal+Member acceptance passed eight
selected cases and71 named events with no failures/skips/race reports; all1,559
worktree paths/index and independent cleanup passed. PostgreSQL66.07s and
MySQL104.29s; log SHA256
`ac9b3537532e43a4d49f01b539988db48ba59712d30641f334e8360dcf53e3e8`.
The same fixture was carried to main; final mandatory main checking passed.
Corrected full113 R2 passed both databases: 3,861 named run/pass events,
eight constraints, no failures/skips/race reports, PostgreSQL1,019.91s and
MySQL1,443.51s (2,494.711s total). All1,559 frozen source paths and semantic
index identities remained exact; owned containers/networks/volumes are absent.
Log SHA256 `d56436dcdce3bd334a763c5e62555bf416ddc932894aa29bf2da0c191ed9c92e`.
The text-output reader needed a narrow correction for Go summary ordering;
its rejected readings remain recorded and no test/source/resource was rerun
or changed for that correction. Main preserves one assertion-equivalent
Member fixture initializer lint correction outside the historical worktree.
Latest mandatory main checking passed. The containing commit records Member
delivery; no failed run is relabeled as accepted.

Restore recovery passed complete isolated source/Task/build and actual controlled
PostgreSQL browser acceptance: real conflicts, exact original-intent retries,
transient Session-error gate unmount/manual recovery, bilingual draft retention
and same-artifact/original-Session restart. The16 exact UI afterimages are now carried onto checked Member main, paired
rules preserve the Member contract, and Default Limits documents the actual
acceptance. Main composition passed mandatory checking,3,407 frontend cases/153 files,
four Node checks, production asset race tests and embedded build. All16 accepted
UI afterimages are exact. The containing commit records this separate recovery
phase; its earlier browser artifact is distinct from the composed main build.

Team creation V63/115 passed source checks and complete Task testing. First
actual focused R4 failed three cases: PostgreSQL fixture index removal hits the
pinned GORM adapter syntax error; both lifecycle cases require a private,no-store
creation-context response rather than inherited no-store. Seven other direct
cases passed; source/index remained exact and owned resources are absent.
Narrow fixture/header successors are in progress, with no full/browser acceptance. Its source-only focused runner now binds
the accepted inherited Personal-warning fixture correction without changing
case counts, deadlines or acceptance guards.

Personal Key V64 now registers two real-driver cases after the unchanged115-case
prefix, for117 total. The live-runtime fixture keeps the publisher alive and
fences only tagged background reads during deliberately unpublished SQL faults;
manual APIs and the real warning producer retain their normal reads and proof
gates. Actual driver/native/browser acceptance is pending. Accepted Restore
consumers fix the old Restore retry/read-authority mismatch; composed UI passes
3,583 cases/156 files plus four Node checks. Separate mandatory source checking,
development lifecycle and production assets passed; the original failed complete
Task run stays failed. The newly registered fixture's mandatory checking and focused pure race
tests also pass after an assertion-preserving tagged-switch lint correction. Project Key backend and paired notification UI are
being prepared independently: original rotation-root monthly80/90 warnings for
current admitted Project managers, with no late-join replay or administrator
recipient override. Source candidates are not delivery or new formal completions.
The full objective remains active.

Project CI37369875985 is now successful, including actual frontend/backend,
both database and authentication restart checks, build and asset checks.
Actionlint37369875963 and independent GolangCI-Lint37369875938 now succeeded
at exact delivered HEAD9de8b1d. Earlier Runner-allocation cancellations remain
historical failures; their successful retries establish current convergence.

Team creation now composes the accepted Restore consumer with both resource-test
endpoint branches retained. Its combined mandatory checks,3,512 frontend tests/
156 files, four Node checks, production build and embedded assets passed.
Personal and Project Key warning source is composed together with privateV64/V65,
shared worker/inbox wiring and paired rules. Mandatory checking and focused race
checks passed; full UI passed3,661 cases/156 files plus four Node checks. Both new Project Key
fixtures are registered and pure race/check gates passed. Team and Key auth
bootstrap fixtures now assert their actual ledgers63/65, respectively. Team
focused dual-driver execution is authorized on its frozen1,584-path candidate;
its actual result remains pending.
The target complete Key matrix is119 after the unchanged117 prefix. Source-only
preparation never establishes database/native/browser acceptance or delivery.

## Active Project warning integration

Team warnings are checked/pushed as `4504088` with exact remote main read-back;
remote frontend/backend checks and Actionlint passed. The first CI database
and GolangCI-Lint jobs were cancelled before acquiring a hosted Runner; their
annotations report no Runner allocation, not an executed test failure. Both
workflows were retried; remote convergence remains pending. Main carries Project backend/UI/
fixtures and frozen V61, preserving the 109 prefix and adding scenarios 110–111.
Focused real-driver and frontend/build/asset gates passed against the identical
isolated worktree source. Full 111 passed 3,697 named events and eight constraints
without failures/skips; all 1,546 paths remained exact. Actual current-main PostgreSQL production,
bilingual manager history/privacy and original-Session restart passed: six native
completions, four observations/eight original inboxes, single-read HTTP 200 and
read-all HTTP 204, with all original Sessions and no extra inference/login.
Owned resources are absent. See [Notifications](NOTIFICATIONS.md#project-monthly-warning-integration-v61). No new
completion is claimed; the full objective continues.

## Current Team aggregate warning acceptance

Personal monthly warnings are checked/pushed as `38de94f`; CI 37356643442,
GolangCI-Lint 37356643541 and Actionlint 37356643376 all succeeded.
Team V60 and the unchanged 107 prefix plus 108–109 now have full dual-driver,
source and current-production bilingual/original-Session restart acceptance.
The containing commit records this phase. Project warning has focused dual-driver
and frontend/build/full 111 acceptance. Final mandatory checking and complete
Task testing passed; the containing commit records this phase. Team-member
warning and Restore recovery await actual acceptance; source-only checks never
count as delivery. The full objective remains active.

Full 109 passed 3,526 named events and eight constraints on both databases;
all 1,533 source paths stayed exact. Source/frontend/build gates passed, and
controlled PostgreSQL produced six completed calls/attempts, four observations
and eight original-recipient rows. Tokens/money 80%/90%, bilingual history,
membership/rejoin privacy, single-read HTTP 200/read-all HTTP 204 and the same
artifact/configuration/database/journal/original Sessions survived restart.
Owned resources are absent. Full log SHA256:
`648a88a9a5c264080f42179f4a8ed427712b1855c71950a15982a36750696409`.
Browser artifact SHA256:
`957ef673e5e71ec068ed7d6c57e7fcbf5466a2d0327037db4ff0a0b41c357cb0`.
See [Notifications](NOTIFICATIONS.md#team-aggregate-monthly-warnings-v60) for
independent driver and browser evidence, limitations and retained failed runs.

## Personal monthly warning acceptance and delivery

Checked/pushed main predecessor is Member workflow
`cb4bab4416ced1da4489333edaf9d01770ebe930`. CI 37347016641,
GolangCI-Lint 37347016446 and Actionlint 37347016491 all succeeded.
Personal V59 and 107 ordered cases are accepted; final mandatory checking passed.
The phase delivery is tracked in Git history. User-selected thresholds are 80% reminder and
90% critical, using complete known settled monthly usage only.

Focused PostgreSQL/MySQL acceptance passed eight selected scenarios and 128
named events. Complete race-enabled regression passed 107 ordered scenarios per
driver, eight additional constraint cases and 3,361 named PASS events,
with no named failures or skips. PostgreSQL took 904.15s;
MySQL took 1169.90s. The original 105-scenario prefix and
all 1,520 protected source paths remained exact. Owned Compose containers,
networks and volumes are independently absent.

Full log SHA256: `d62c54939fae35c1a9a1bafe1d826f03146d111b7a9e1b8fca7c942e240b3148`.
Focused log SHA256: `604401ae11e39c6e0e40b6732edd3cda1c85c41a9f1067ccec92d0fdee87bf30`.

Controlled PostgreSQL production and bilingual browser acceptance passed against
binary `8af28a7b8ecfd13974b4db15017e92da681308fb3df641445311bc0431e65bc9`.
Six completed native calls and attempts produced four Personal warnings and no
exhaustion or operational/SMTP fanout. Recorded Tokens 8/10 and 9/10 and USD
9/11.25 and 9/10 appeared in default English and live Chinese. Single read
returned HTTP 200; read-all returned 204 and retained all historical rows.
The same binary/configuration/database/journal and original Sessions survived
process restart without additional inference or login. Immutable call/attempt,
warning and read-state facts were unchanged; administrator/other-account reads
remained isolated. Escape restored trigger focus, console errors/warnings were
empty, and owned tabs/listeners/Compose resources are independently absent.
MySQL production browser verification is not claimed; dual-driver native and
migration acceptance is recorded separately.

Formatting, mandatory checks, Go race source tests, pinned backend lint, 3,169
frontend cases in 151 files, four Node checks, production build and embedded
asset tests passed. Final mandatory checking also passed with no errors and two
existing Fast Refresh warnings; delivery is tracked in Git history.
Team aggregate, Project, private Team-member and Key warnings, SMTP and
configurable thresholds remain outside this Personal slice. F17/F23 and formal
11 complete / 16 partial / 3 unstarted totals remain unchanged.

Next: integrate the separately prepared Team aggregate warning slice, then
Project warning and Restore recovery candidates. Their source-only preparation
is not delivered functionality. Keep the full RouteX objective active.

## Checked Member workflow integration

Checked and pushed predecessor is repository price source/grouped Role selection
`3070f191bf24ac2a30406ae22c32bbbcc18129e8`. Its actual price and bilingual
keyboard/restart gates passed. CI 37332855651 subsequently failed the MySQL
10,000-assignment Member Roles read at its five-second deadline; frontend/backend
checks and both standalone lint workflows passed. Role CI 37331397426 was
cancelled. The earlier masked Approval CI error remains historically unexplained.

The uncommitted Member phase integrates existing offboarding recovery and recorded
Settings summaries, exact retained-user scope, complete allowed-email-domain
policy and authoritative retained Role member counts. Frozen GORM V58 is
registered; the original 103-scenario prefix remains and domain migration and
lifecycle append cases104–105. Frontend 3,130 cases/151 files, four Node checks,
mandatory checks, Go race/coverage, two development lifecycle checks, independent
dual-driver authentication/native restart and embedded production assets passed.
Corrected real-driver focus passed seven cases per driver, including V57/V58 and
complete Member Roles history; catalogues500/501/1000
retain6/7/7 statements, with explicit422 at1001. The original logger-counter
failure and isolated fixture-only correction remain recorded separately.

Controlled PostgreSQL production and bilingual browser acceptance passed against
current artifact `0e0b04678e95f65205e33317cca9a8c0344833cb40e5e46fba9bd0de72dda7c0`
and all1,509 protected paths. Actual policy save canonicalized the domain;
disallowed403 created no User, while allowed202 granted no private access.
Existing sign-in remained valid. A real first-plan409 retained the exact request
through dismissal/language/reopen; reviewed restoration allowed the same retry201.
Explicit completion200 recorded audits and revoked the target Session. Saved and
completed Settings facts, canonical policy and retained counts1/3/0 survived the
same binary/configuration/database/journal/administrator Session restart. Zero
native Calls/Attempts/Keys/grants; owned tabs/listeners/Compose resources are absent.
An earlier non-PTY helper stopped on stdin EOF before browser actions and is not
accepted. A later root-generated invalid credential key also failed before browser
actions; the subsequent correctly bound current-artifact run passed. Earlier
browser proof retains its own artifact identity.

Full105 R1 failed only the V57 approval migration fixture on both databases:
it assumed the final ledger row still identified V57 after V58 was registered.
No released migration is changed. The correction must identify V57 explicitly
and preserve every other ledger row, including later generations. The failed log
SHA256 is `0e1e6d74af07f4ab1c3d2c87152e25ece94ea832e660bbc0c431482533a854c4`;
owned full-run resources are independently absent. The corrected full R2 passed
105 ordered lifecycle cases per driver, eight constraint cases and 3,247 named
test events with no failures or skips. PostgreSQL took875.96s, MySQL1135.06s
and Handler2015.362s. Log SHA256:
`7dc6857e3ae6df5e2afcd2f76e64afcd4937e0d48dae4fd1f6ae0b06fd9a2ec6`.
All1,509 protected hashes stayed exact and owned containers/networks/volumes are
independently absent. The original R1 failure remains historical evidence.

The remote timeout repair retains parameterized indexed ID filtering together
with every exact-byte comparison. Actual same-fixture MySQL measurement improved
from4.820s/full scan to0.905s/primary-key range scan of500 candidates; PostgreSQL
passed as well. Both retained complete101/1001/10000 histories,8/11/47 statement
counts, all authorization checks and the unchanged five-second deadline. Focused
candidate acceptance is distinct from complete main regression and remote CI.
The narrow source fix is integrated; diagnostic timing/EXPLAIN instrumentation is
TEMP-only. Current-artifact production, bilingual browser and original-Session
restart passed. The final mandatory check and scoped delivery remain separate.
This bounded phase does not claim complete F04/F05 or remote CI convergence.

Personal monthly80%/90% warning backend/UI and unregistered V59 remain private
preparation awaiting root integration and actual acceptance. Restore recovery is
also separate. No new feature completion is claimed; formal totals stay11/16/3.

## Checked repository price source and grouped Role permissions

The reviewed Role predecessor `ecd130b` is committed/pushed with exact remote
read-back. This bounded phase adds three reviewed native-model entries/six base
USD rates to the existing embedded repository source, and current assignable
permission-group select-all plus localized resource/action summaries to the
existing Role dialogs/table. It adds no schema, API, backend runtime or layout.
Missing cache rates remain absent; source maintenance never automatically applies
rates or rewrites historical calls. See [Pricing](PRICING.md) and
[the versioned source](../prices/README.md) for provenance and billing limits.

Mandatory checks, Go race source, 2,999 frontend cases across 146 files, four Node
checks, production build and asset/source tests passed. Current binary SHA256:
`222713299689f77df5a563167e953c1ba991428ba38c906f69bfbe2875077c42`.
Controlled production acceptance passed on real PostgreSQL and MySQL: exact
three-model source/mappings, configuration with no price writes, side-effect-free
six-rate preview, explicit apply, three price records/six rates/two durable
receipts, current runtime confirmation, same-artifact/Session restart and exact
replay with no extra writes. No Keys, logical routes, calls or attempts were
created. Bilingual browser reads verified real mappings/digest/committed state.
Role browser acceptance verified keyboard all/partial/clear in creation/editing,
correct two-resource/two-action summaries and same-Session bilingual restart.
All 415 protected source paths stayed exact; owned tabs/listeners/Compose
resources are absent. Prior full 103-per-driver backend/schema/harness proof is
retained; this static-data/UI-only phase does not relabel it as a new full run.
The containing 3070f19 commit is checked and pushed with exact remote read-back;
its CI37332855651 failed the MySQL bounded Member Roles read as recorded above. F04/F05/F15 stay
partial; totals remain 11 complete, 16 partial and three unstarted.

## Checked reviewed Role definitions

Checked/pushed clean predecessor is local registration approval
`b10eb6cf900994b8a0e10b7a81e346ed10144cc8`. Its source, complete102-per-driver,
authentication/native restart and controlled bilingual process/browser gates
passed; final mandatory checking and scoped commit/push are complete. Historical
failed helper and query-budget attempts remain separate in the records below.

The bounded Role-only reviewed GET/PUT and existing Edit/View workflow is accepted.
Read authority is current `roles.read`; the independent writer is an admitted
intrinsic platform administrator. Preserve exact review/identity proofs, full
permission replacement, atomic typed reason audit and fresh current-database
confirmation. Preserve every failed dispatched intent with current authority and
fresh CSRF; GET/refresh/dismissal never establishes its historical outcome.
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

Preserve accepted Approval, State/Roles, Member grants and native history while
composing exact root route/harness contexts. Offboarding, allowed-email, Restore
and repository price source remain independently queued. F04/F05 stay Partial;
formal totals remain11 complete,16 partial and three unstarted.

## Checked approval predecessor

Approval's earlier checked predecessor was Member State `74f08d3`, with complete source
checks, full100-per-driver regression, independent authentication restart and
controlled production/browser evidence recorded below. All343 protected paths
matched its accepted source. Its frontend first-conflict recovery correction
retains every failed dispatched intent and requires explicit abandonment before
a new review. Reactivation never restores revoked Sessions, Keys or roles.

This local registration approval phase adds frozen GORM
V57 after the unchanged100-case prefix. Mandatory/source checks and fresh uncached
Go race passed; unchanged frontend source retains 2,910 cases in 144 files.
Corrected focused real-driver acceptance passed 27 named tests with the original
Overview 7, list 9/11/10 and Effective Models 20 budgets. Both-driver independent
authentication/native restart passed. Approval focus preserves five native
completions, thirteen denied native requests, rollback and exact retry checks.

Fresh complete regression passed 102 scenarios on each PostgreSQL/MySQL, all 204
direct lifecycle cases, eight constraints, five test-bearing packages and 3,049
named tests total without named failure or skip. All 395 protected R17 paths stayed
exact; all owned containers, networks and volumes were independently absent.
Earlier failed/stopped full and query-budget runs remain historical, not relabeled
passed. The proof-reuse repair preserves complete same-transaction actor admission
and independent permissions without relaxing budget assertions.

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

At the Approval source checkpoint, the then-queued Role-definition, offboarding
UI/recorded Settings summary and allowed-email combined Go 34 preparation passed
908 top-level tests and 3,160 pass events before its
nonsemantic successors; combined
UI 35 passed 3,102 cases in 150 files, types, lint/format and reversible contexts.
The separate 10-path Restore proposal passed 3,117 private source cases in 150 files only. None is
applied or actually accepted. Repository price seed data remains a separate
queued zero-inference plan. Compose overlapping governance fixture/locale/rule
contexts explicitly; never replace older whole files. Continue these partial
capabilities after checked approval delivery. F04/F05 and formal 11 complete,
16 partial and three unstarted remain unchanged.

## Preceding checked main delivery

Reviewed Roles `a44838d3a8b618d164680cd1ddbd23ae3bdb55c5` is checked,
committed and pushed with exact remote main read-back. Complete source passed
2,761 frontend cases/138 files, Go race, lifecycle/assets and final mandatory
checks. Dual-driver focus passed ten lifecycle children/eight constraints;
complete integration passed 99 scenarios per driver and 2,790 named tests.
Both-driver auth/native restart and controlled bilingual browser/current retry/
same-Session restart passed with seven screenshots and zero native dispatches.
All 322 protected paths remained exact; owned resources were independently absent.
Historical failed attempts remain separate. Exact Roles CI37280249927,
Actionlint37280249997 and GolangCI-Lint37280249786 all succeeded. F04/F05 and formal 11/16/3 remain unchanged.

Access predecessor 9205ca5 is the earlier checked delivery. It preserves the approved
six Access facts and independently authorized Role/Team names, with no migration
or writer. Commit/push/read-back and new remote workflows are separate delivery
observations recorded by the coordinator. F04 and formal 11/16/3 remain unchanged.

Recent-login predecessor d51a52e retains actual committed sign-in timestamps and
historical unknowns. Its complete 96, authentication/native persistence and
controlled production/MFA/bilingual restart gates passed. Commit/push and exact
remote read-back passed; CI 37264974282, Actionlint 37264974348 and
GolangCI-Lint 37264974195 all succeeded. Effective Models 88e8480 and List af9c22e
also retain all three successful exact workflows.

## Previously checked Metadata

Metadata `d261b02` and its separate observation correction `516ee0b` remain checked
with successful exact remote workflows and the following accepted scope.

The existing Basic information card retains its layout and read-only email.
Name updates require independent authority, exact resource identity, reviewed
If-Match, reason and local Base UI confirmation. A changed name and typed audit
commit atomically. Conflict review preserves the draft and requires explicitly
abandoning the old intent. Uncertain retries preserve the original request;
confirmation proves only the current name, never a historical operation receipt.
No role, lifecycle, Key, grant, quota, catalogue or runtime change is included.

Acceptance: format/check and complete source tests passed 2427 frontend cases in
127 files, four Node tests, two development lifecycle tests, Go race and production
assets. Repaired focused PostgreSQL/MySQL metadata/account/governance passed six
lifecycle children plus eight constraints. Both-driver authentication, persisted
Sessions, native gateway lifecycle and process restart passed. The controlled
Metadata process and bilingual browser passed against production SHA256
`1a6e7846fbdb34a75302e5d37599205d4e5be05fb2e22e5441f7727e32991c45`:
stale conflict, retained draft, explicit fresh review, one UI save/typed audit and
read-only controls, zero inference dispatch. Original Sessions, pending Key,
grants, policies, catalogue and call history remained exact. The fresh complete matrix passed all92 scenarios per driver and eight constraints
(Handler1603.903s), with five test-bearing packages complete, no failures/skips and
all239 protected paths unchanged. Final mandatory checks passed before the scoped
main delivery.
All actual owned resources and listeners were independently absent after teardown.

The two-leaf test correction preserves unknown runtime evidence and exact numeric
price assertions. It waits through bounded fresh GET observations when the
publisher is busy, without replaying a mutation or changing production. Fixed
original-provenance and zero/18-place price regressions passed twenty race
repetitions; real Member Models lifecycle passed three repetitions per driver,
with six lifecycle children and 24 constraints. Earlier metadata fixture/full
matrix failures remain historical in Implementation and are not passed evidence.

## Preceding deliveries and remote state

- Own-Team CSV `185611c`: source/full91/authentication/native/browser/restart and
  final mandatory checks passed locally. Four immutable completed native calls,
  exact scope/money and membership denial passed. English/Chinese and mobile
  columns passed. Saved browser-file landing remains unverified; actual HTTP CSV
  bytes and headers passed. Exact Actionlint 37239988907 and GolangCI 37239988911
  passed; CI 37239988896 failed the MySQL immediate restored-proof test assertion.
- Model review correction `3d6118`: deterministic source, full91 and both-driver
  authentication/restart passed. Actionlint/GolangCI passed; CI 37238698287 was
  cancelled by the CSV push after source jobs. Cancellation is not success.
- Member Models `569abcd`: full91, authentication, 2346/123 frontend and
  ten-native/four-denial process/browser/restart passed. Its CI was cancelled by
  the following parity push; local acceptance is distinct from remote CI.
- Member Teams `8074aaa`, Limits `2d5cafc` and Keys `818500a`: checked main
  deliveries with complete local gates and all three exact remote workflows
  successful. Their earlier fixture/helper failures remain in Implementation.

## Checked Member list phase

The containing phase preserves Metadata main and adds the approved eleven-column
Member table, compact filters and action menus. Bounded Personal quota/journal
and Key summaries retain exact strings, zero/null and unknown usage; Team names
require independent read permission. Historical login remains unknown until V55.
The client accepts authoritative `user_<id>` accounts, recorded RFC3339 offsets
and bounded historical names, with an accessible email fallback for empty names.

Final source gates passed 2,486 frontend cases in 129 files, four Node tests,
two development lifecycle tests, Go race and production assets. Mandatory checks
passed with no lint errors and two existing Fast Refresh warnings. The corrected
dual-driver focus passed eight lifecycle children and eight constraints; both-driver
authentication, native gateway and restart passed. The rebuilt production artifact
SHA256 `90f96e8524eec366db9ec2cb2a495ea9f0f39e372d1f05342e5a3034e3db7e50`
passed four controlled native calls/attempts (three known usage, one unknown),
retained finite bounds, immutable attribution and same-artifact restart. Separate
English-default/live-Chinese browser checks passed all columns, literal search,
role filters, clearing, independent Team names and writer-only directory denial;
console warnings/errors were empty. Eight protected historical table digests
remained exact. Actual tooltip activation was not separately verified.

The final unchanged-source matrix passed all 93 ordered scenarios on each of
PostgreSQL and MySQL, eight constraints and all five test-bearing packages
(Handler 1616.250s). No test failed or skipped; all 251 source paths remained exact
and owned containers, networks, volumes and application listeners were absent
after cleanup. Log SHA256:
`3c074531ecc8e225fc93a649cdf6f3bc62e6e48e0fa58e8e5d76fc0bd3943da5`.
Earlier fixture failures, rejected browser observation and interrupted unfiltered
RED command remain historical in Implementation. The log reviewer was corrected
for slash-containing Go subtest labels; six parser checks passed, with exact
integration-parent checks retained. This did not change product or test source.

Final List mandatory check, scoped commit/push and exact main read-back passed
as `af9c22e`, with all three exact remote workflows now successful. Effective
Models then integrated 26 reviewed paths, a one-leaf foreign-key fixture repair
and a three-leaf table/rules wrapping correction. All other source protections
remained exact. The corrected source passed 2,546/131 frontend, Go race,
lifecycle, assets, mandatory checks, both-driver focused and complete 94 matrix,
and controlled zero-inference bilingual process/browser/restart. The complete
acceptance record below gives exact timings, hashes and remaining limits.

## Next checked phases

| Slice | Current state | Next gate |
| --- | --- | --- |
| Member list, effective Models, recorded login and Access | Checked and pushed: af9c22e, 88e8480, d51a52e and 9205ca5 | Preserve accepted behavior |
| Reviewed direct role assignment and Member state | Checked and pushed: a44838d and 74f08d3 | Preserve failed-intent recovery and revocation |
| Local registration approval | Checked/pushed b10eb6cf; local V57/102, authentication/native restart and bilingual process acceptance passed; CI run 37314987013 failed integration | Preserve local evidence; verify a new CI run after the fixture repairs and continue diagnosing the masked MySQL error |
| Reviewed custom-role definitions | Full dual-driver 103-scenario regression, final source checks/build, authentication restart and current bilingual keyboard/retry/restart passed | Verify new remote CI; continue grouped selection and remaining workflow |
| Member offboarding and recorded Settings summary | Source prepared privately | Integrate and run actual acceptance |
| Allowed registration email domains | Frozen V58 proposal and UI prepared privately | Integrate after checked approval and Role phase |
| Repository price seed and Restore intent recovery | Separate private source proposals | Scoped integration and acceptance |

Recheck shared contexts against the checked predecessor and retain earlier ordered
scenarios. Source-only preparations never count as actual runtime acceptance.
Update AGENTS and frontend rules together for UI behavior changes.

## Remaining scope and evidence limits

F04 remains Partial. Reviewed Member state and local registration approval have
bounded acceptance; remaining offboarding UI, invitations and related workflows
still require delivery. Role-definition UX, templates and Team-role review remain
separate F05 work. None of these Member slices completes the full objective.

Preserve earlier catalogue, pricing, attachment, notification and admission
acceptance. Repository prices use the requested versioned local file with no
fabricated values. Real external-provider and SMTP acceptance remain open where
no dedicated environment has been supplied; controlled local evidence does not
substitute for them. Own-Team CSV browser-file landing is a separate limitation.
Consult Implementation and domain documents for exact current remaining items.

## Execution constraints

Root owns actual database/Compose, application, native and browser work serially.
Parallel workers own isolated source/fixtures only and protect other edits. Use
GORM-first frozen additive migrations, portable business persistence, approved
layout, shadcn/Base UI, paired i18next en/zh with English default, and English
documents/commit/PR text. Run relevant tests and mandatory `go tool task check`
before each scoped commit; push and read back main. Keep remote CI independent,
update documents after evidence changes and continue the objective after each
checked phase. Never mark failed, skipped, cancelled or unexecuted gates passed.

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

### Recent Login full-matrix historical failure, 2026-10-05

The first complete 96 run exited 1 (Handler 1641.168s). Each driver passed 95 ordered
lifecycle children; the sole failed child was `member_models_migration`, whose
old fixed total 54 rejected the legitimate V55 ledger. The failed full log remains
immutable; it is not accepted evidence. Root confirmed the 282-leaf source floor
and exact owned container/network/volume absence after completion.

Only the old V54 test changed: exact ordered incoming versions with unique V54
are checked after removal, concurrent replay, repeat execution and final read.
The source patch preserves every historical data and DDL assertion; no product,
released migration, driver branch or original 96 ordering changes. Current whole
harness remains exactly 55. Corrected three-child focus, full 96 rerun and final
commit checks are next. Prior real Recent focus/MFA/browser/restart acceptance
remains separately recorded against its exact unchanged production artifact.

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

## Private Team-member monthly warning integration V62

Private member warnings use the approved fixed 80% reminder and 90% critical
thresholds on complete settled monthly Tokens and exact decimal money. Their
scope is the stable Team/User pair, independent of a replaced Membership row.
Frozen GORM V62 creates immutable observations and original-recipient inboxes,
with separate exact Team and User birth proofs. Current published parent and
member policies, membership, currency, calendar and settled journal coverage
must agree. Holds, unknown usage and stale authority never produce percentages.
No owner, administrator or peer receives a member's warning; removal hides
history and same-identity rejoining restores the original read state. The existing
hard stop, exhaustion notification and SMTP contracts remain unchanged.

Core race tests passed eight top-level tests/111 named events. Corrected fixture
source tests passed five top-level tests/15 named events. Actual isolated
PostgreSQL/MySQL focus passed all six selected lifecycles and 47 named events,
without failures, skips or race reports; PostgreSQL took 68.16s and MySQL 91.01s.
All 1,559 protected source paths and semantic staged identities remained exact;
owned containers, networks and volumes are independently absent. Earlier R1/R2
fixture runs remain failed: an unused builder sent unsupported policy fields;
the corrected fixture now uses the real presence-aware decoder. No production
server contract was relaxed.

The identical runtime/UI source passed complete Task testing: Go race/coverage,
3,365 frontend cases in 151 files, four Node checks, two development lifecycle
checks and production asset building/embedding. The full ordered 113-scenario
matrix is running on both drivers, preserving the original 111 prefix. Its finite
45-minute aggregate deadline reflects the measured 2,299.13-second predecessor;
per-query/request/readiness deadlines and assertions are unchanged. Main now
carries the exact runtime/UI/fixture afterimages. Final mandatory checking,
current production/bilingual/privacy/original-Session restart, full regression
and phased delivery remain pending. F17/F23 and formal 11/16/3 remain unchanged.

## Default-rule save recovery in progress

An independent frontend slice is implementing recovery of dispatched User/Team
default-rule saves after a transient same-actor Session failure. Preserve the
original target, seven-cap policy, reason and reviewed If-Match in the existing
nonpersistent submitted-intent owner; recovery stays idle until an explicit retry.
Current rule reads do not confirm the historical save, and saved defaults apply
to future creation rather than retroactively enforcing existing resources.
No schema or admission change is planned. Source tests, controlled production
and restart acceptance are pending; this work does not complete F17.

### Team creation production response-format blocker

Controlled production QA returned actual browser creation responses 409, 409 and 201. The backend committed one Team and independently confirmed its current runtime policy, but the frontend retained an unknown outcome with a generic failure. A separate, explicitly non-acceptance same-binary replay returned 200 and exposed a valid numeric-offset Team timestamp (`2026-10-06T08:36:07.379764+08:00`) alongside a UTC receipt timestamp. The client currently accepts only UTC `Z` timestamps. This production/browser stage failed; it does not alter the accepted full 115-scenario database result. Both disposable Compose projects, owned listeners and the QA tab were removed. Preserve the first input-channel failure and this response-format failure. Fix and test strict RFC3339 offset handling before rebuilding and repeating actual browser acceptance.

### Team creation offset repair and final production acceptance, 2026-10-06

The preceding response-format failure remains failed evidence. The strict client
validator now accepts RFC3339 UTC or numeric-offset timestamps, retaining
Gregorian validity, finite instant and every original identity/receipt check.
After formatting, mandatory checks, complete Task testing (3,539 frontend cases
in 156 files, four Node checks, two development lifecycle checks, Go race/coverage
and production asset race tests) and embedded build passed. Backend/schema and
the accepted full115 source remain unchanged. Rebuilt binary SHA256:
`38820984a00c1a79a8a23eba769b15065feafab03869a907b1a26ff270de989a`.

Fresh controlled PostgreSQL production/bilingual browser acceptance passed five
real Team creation requests: 409, 409, 201, withheld committed201, exact replay200.
The first conflict and identical retry retained the original reviewed request;
explicit local abandonment and fresh review created the first Team. A second
committed response was withheld as labeled503. Real periodic Session refresh
then returned a labeled500; the observed AuthGate hid the private form, and
fresh same-actor Session, permission, context and owner reads restored the
original dispatched draft without borrowing changed defaults. Original Sessions,
artifact, database and journal survived process restart. Explicit replay used
the original JSON/UUID/If-Match and current CSRF, then navigated to that Team.

English/Chinese views, decimal precision, zero/null distinction, Base UI Escape
and focus return were observed. There were exactly two Teams, receipts and typed
creation audits, zero native calls/attempts/Keys/grants/Providers/Projects, and
no browser console warnings/errors. Owned Compose resources, listeners and QA
tab were independently removed. A blank initial document required one reload
before login; no document reload occurred during submitted intent. Native window
focus was unavailable on the locked Mac; genuine periodic Session refresh
triggered recovery instead. Network observations alone do not prove query-cache
generation handling. Earlier input-channel and timestamp failures remain
separate. F06/F17 and totals of 10 complete/17 partial/three unstarted remain
unchanged; initial Model selection, Key warnings and Default save recovery have
independent pending gates.

## Earlier bounded SAVE acceptance before the draft repair

The controlled PostgreSQL SAVE workflow completed successfully with nine actual
browser PUTs, eight action-filtered default-update audits, bilingual original
intent recovery, manual AuthGate Retry and same-original-Session process restart.
Owned application/observer listeners, browser tab and Compose resources are
independently absent. Audit metadata decoding is not claimed. The production
artifact remains `802eb41426cf139f5141e5ab1da08ea4dfbf78d10419d9df1230bbc9f45e1ab8`.
A separate ordinary undispatched draft was lost during successful periodic
Session renewal; generation-scoped reads unmount its local editor. The narrow
source successor and pre-submit regression are in progress. Do not commit this
phase until that defect is fixed and relevant checks pass. Focus Key R8 is
currently running; full119 and Key production acceptance remain pending.

## Draft-renewal successor source gates

The reviewed two-file successor is carried with paired frontend guidance.
Mandatory checking, complete Task (3,565 frontend cases/157 files), four Node
checks, two development lifecycle checks and production assets pass. New
embedded binary SHA256: `e3b22ba6d074094db21219ec57d1b9b5f82155dad5dce6203a2d29907479e2e6`. Actual pre-submit renewal and
uncertain-intent browser acceptance is pending on this distinct artifact.
Key focus R8 passed all16 direct cases/87 named events and independent cleanup;
the actual full119 run is in progress and its candidate worktree stays protected.

## Repaired SAVE phase ready for delivery

Actual PostgreSQL acceptance passed on the distinct `e3b22ba6…79e2e6` artifact.
Both ordinary User/Team drafts retain exact values and reasons across genuine
periodic Session renewal before save. Nine actual browser PUTs also pass first
conflict, committed response loss, manual AuthGate Retry, identical replay,
uncertain409, explicit abandonment/Escape/focus, fresh review and same-original-
Session process restart. Eight action-filtered audits were counted; typed
metadata decoding is not claimed. Existing policies and zero native resources
stay exact; owned listeners/tab/Compose resources are independently absent.
Mandatory check, complete Task (3,565 frontend cases/157 files) and production
build pass. The containing commit delivers the phase. Key full119 is running
in its protected candidate; Model121 and Role-description preparation are
separate, unaccepted future stages. The all-capability objective remains active.


### Current Key source carry

Reviewed Key code is present in the main working tree, without a commit or
acceptance claim. The 45 exact candidate outputs preserve all delivered
Member/Restore/Team/default-save behavior and add only V64/V65/119 registry
entries. The 1,617-path carried floor differs from the running 1,616-path
candidate through retained documentation, rules and frontend recovery/timestamp
fixes. Of 947 Go files, 946 are hash-exact; the remaining V62 migration differs
only in two release-status comment lines. All scripts, Go dependencies, Compose
files and Taskfile are exact. Complete119 evidence must retain its original
candidate provenance, with this separately reviewed backend equivalence.
Private composed frontend checks passed 650 cases/eight suites, an additional
22 delivered SAVE lifetime cases and both TypeScript configurations. Main checking, complete Task (3,714 frontend cases/157 files), production
build and both real-process authentication/gateway lifecycle gates passed.
Actual Key production/browser/native/restart and delivery remain pending. Independent Role-description source smoke separately passed eight
PostgreSQL/MySQL scenarios and 36 events; it does not accept Model121/whole123
or final Role delivery. The full objective remains active.


### Complete Key119 acceptance

The isolated V64/V65 candidate passed 119 ordered scenarios per driver, eight
constraints and 4,370 named RUN/PASS events, without failures/skips/race reports.
PostgreSQL took 1,201.57s, MySQL 1,485.34s and the whole command 2,770.780s.
All 1,616 protected source paths and the semantic index stayed exact; owned
containers/networks/volumes are independently absent. Log SHA256:
`01d4ae20460d3e9606e795afb9ab321b2a4162dffffb3804eaeda1896be39dfb`.
This matrix ran on the original isolated candidate; current main inherits its
compiled backend through the separately verified comment-only equivalence.
Main check, complete Task (3,714 frontend cases/157 files), production build and
both real-process authentication/gateway lifecycle gates passed. Controlled Key
production/bilingual/original-Session restart acceptance also passed as recorded below.

### Controlled Key production acceptance

The final production artifact
`ce55e0d3e30e3b3e1f88bfec8e0d1ff375c45477f9aaa19c07a814aba875b8ed`
passed two separate PostgreSQL environments on the reviewed 1,617-path main
source floor. Personal acceptance recorded six native calls, five observations
and five original-owner inbox rows. Project acceptance recorded six native
calls, five observations and ten independently read original-manager inbox rows.
Normal browser sign-in, default English/live Chinese, original rotation-root
labels, exact decimal amounts, single-read200/read-all204, recipient isolation
and Escape/focus passed. All seven original browser Sessions reread after an
identical-artifact/config/database/journal process restart without reloading
those authenticated documents, logging in again or replaying inference.

The Project API acceptance separately verified removal/rejoining and no late
manager backfill. Its ten-second disabled interval confirms no new or changed
observations during that interval; it does not prove worker invocation. Unknown
coverage, holds, currency mismatches and hard stops remain dual-driver matrix
evidence rather than claimed browser scenarios. Owned tabs, listeners and both
Compose projects' containers/networks/volumes are independently absent. The
combined result is 12 native calls/attempts, ten observations and 15 inbox rows.

### Project Key Chinese copy spacing

A browser-observed copy correction separates Chinese prose from `Project Key`
in the five warning titles. It changes no notification identity, threshold,
recipient, read-state, schema or runtime behavior.

The copy correction passed 145 focused notification/localization cases, the
complete 3,714-case frontend suite across 157 files, formatting and mandatory
checking. The initial five stale title expectations remain a failed checkpoint;
they were updated without changing behavior assertions.

### Active Model and Role queue

Model Focus R3 passed sixteen direct PostgreSQL/MySQL cases and nineteen
named events on the exact 1,630-path source floor after a fixture-only live-parent
correction. R2 retains fourteen passes and two fixture failures. Full121 is
running against that same code floor, with no documentation overlay or success
claim. The forty-four-file main-context proposal preserves delivered SAVE,
RFC3339 and Key behavior; an independent uncommitted main carry now permits source gates while the complete
database matrix stays protected. No delivery is claimed.

Role description preparation composes twenty-one backend and fourteen UI files
onto this Model floor without conflicts; private V67/two-case registration brings
the proposal to123 cases. Its 250 focused frontend tests/types and earlier eight
real-driver smoke cases remain independent source evidence. Accepted Model121,
actual full123, final production/browser/restart and delivery remain pending.

### Prepared Connection management follow-up

After the Model and Role gates, the next bounded F11 phase adds literal-name
and protocol filters plus reviewed name editing to the existing Connections
table. Backend and frontend source preparation is parallel and independent;
no carry, production or delivery is claimed. Reuse the shared Connection/egress
revision, independent Provider read/write authority, required reason and manual
identical uncertain retries. Protocol, Base URL, egress, Credentials, Models and
weights remain outside this name-edit phase. Real persisted Connection state
and dispatch enforcement precede a later status filter or lifecycle control.

Configurable quota behavior has no approved editable interaction or complete
parent/child safety contract. Existing hard stops remain enforced; the broader
capability stays partial while independent product gaps continue.

## Accepted initial Team Model access, 2026-10-06

The V66 phase closes the remaining F06 creation gap. The existing form keeps
Basic information, Model access and Resource limits, with bounded search,
retained off-page selections, independent Provider-label authority and explicit
empty grants. Atomic creation and original-intent recovery are backed by the
complete121 PostgreSQL/MySQL matrix, main Task testing (3,759 frontend cases),
both authentication/gateway lifecycle drivers and the embedded production build.
The final main mandatory check passed with no errors; two existing Fast Refresh
warnings remain unchanged.

The fifth controlled PostgreSQL run passed the ten browser checkpoints: three
normal sign-ins, pagination and redaction, unauthorized empty creation, explicit
selected-set conflict review, committed-response loss, private-interface unmount
and manual recovery, EN–ZH–EN draft preservation, same-artifact/original-Session
restart and byte-identical creation replay. Four disclosed native probes produced
two Model authorization denials and two completed selected-Model calls, with only
two upstream attempts. Each completed attempt retains its exact Credential,
Connection, provider-model and published snapshot, authoritative1/1 Token usage
and a2 USD charge. Rejected calls retain unknown usage and not-captured pricing;
absence of an attempt never fabricates known-zero usage.

The original runner remains **failed**: its final collector incorrectly required
zero Tokens for pre-admission Model denials. A separately reviewed read-only
post-run check passed against retained observations and exact durable facts:
four Calls, two completed Attempts, two Teams/receipts/owner memberships, two
selected grants/receipt children and two typed creation commits. All51 existing
setup-controller personal grants remain present; browser actors have none.
No Key, Project, extra replay call or duplicate creation was introduced. The
post-run evidence receipt SHA256 is
`6017a1ddf0cede71b1ee646610086f937bc33da0c907c55d732fd03eef364872`.
Visual review passed and the three original tabs had no captured console
warnings/errors. Owned containers, networks, volumes and all four listeners are
independently absent. Post-stop active journal coverage and runtime API
re-enforcement were not observed; the original inactive pre-native/after-denial
checks and inherited quota tests remain separate. This acceptance closes the
stated F06 feature criteria, without relabeling the original helper as passed.

Current capability totals are11 complete,16 partial and3 unstarted. Role
descriptions, Connection metadata and single-model elapsed observation continue
as independent phases; the full product objective remains active.

## Role matrix failure retained, 2026-10-06

The isolated Role description complete123 run ended failed after2,682.073s.
MySQL passed123 ordered scenarios and four constraints. PostgreSQL passed122
scenarios and four constraints but failed the existing
`local_registration_approval_migration` case at V57 with PostgreSQL's cached-plan
result-type error. Both added Role description scenarios passed on both drivers.
No Role delivery or complete123 acceptance is claimed. The captured final
source/index guard passed; diagnosis is confined to the migration/connection
boundary without changing released migration steps. The original failed log
SHA256 is`213b48a8e23b4b14f3add2285ba791e2f2e298410142e2f4c950677c9038408a`.
This separate candidate failure does not relabel the already accepted Model121
run or the scoped Team Model production evidence.

## Accepted browser elapsed phase, 2026-10-06

Initial Team Model access V66 is committed and pushed as
`3e21b8d268e945a694f3bae98369a005943030e0`, with exact remote main read-back.
Single-model browser elapsed now passes mandatory checks, all 3,789 frontend
cases across 159 files, four Node workflow tests, the embedded build, and the
production asset race test. The 30 focused elapsed cases cover every native
protocol, cancellation, delayed cleanup, language changes, zero and invalid
clocks, and discarded generations.

A fresh embedded-binary browser run completed exactly 13 controlled native calls:
six Chat, three Responses, two Messages and two Gemini. Eight ordinary/stream
requests retained their native completion and usage facts alongside elapsed.
English/Chinese switching and parameter reset preserved recorded values and the
unsent draft. A real HTTP 400 retained failed state and finalized elapsed. A real
PNG upload was canceled at 415 ms; its actual DELETE was delayed five seconds,
returned 200, and left that elapsed and stopped state unchanged. Clear removed
history; Stop followed by model change and leaving for comparison prevented late
responses from restoring history or the transient Key. Model selection remains
disabled during active requests, so direct generation changes are unit evidence.
The observer buffers SSE; this proves terminal parsing, not progressive delivery.

Both owned helpers exited zero in order. The root independently confirmed no
owned containers, networks, volumes or five listeners, exact source/index and
artifact/config bytes, and no browser warnings/errors. The scoped root receipt
SHA256 is `d8d9e913b2e388385281e43c311e8bba50dd2ec97c6981da692b3046b7877069`.
An earlier Compose-ID preflight failure and a two-call idle timeout remain failed;
a file-chooser locator timeout is retained as an automation failure. The first
eight terminal article excerpts were preserved from actual tool output after
that timeout reset the browser automation context; bilingual screenshots and
later live evidence remain separate. Build-generated lock metadata was restored
to its reviewed pre-build bytes after teardown; no dependency change is shipped.
No external-provider or complete F20/A17 acceptance is claimed.

Role and Connection now share an isolated complete 124-scenario matrix on
PostgreSQL and MySQL, with original 123 scenarios preserved. It passes124
ordered cases per driver,248 direct cases,eight constraints and4,491 matched
RUN/PASS events in2,763.823s. Root independently verifies all1,654 source paths,
HEAD/empty staging and absent owned containers/networks/volumes. The migration-cache repair passed eight real-driver focused cases and
changes test fixtures only. Connection's GORM revision-field fix passed both
real-driver focused cases; its frontend whitespace fidelity passed 113 cases.
Role/Connection production browser gates and delivery remain pending. Capability
totals remain 11 complete, 16 partial and 3 unstarted; the full objective continues.


## Next member catalogue price slice

Private backend and frontend copies add exact input/output base-price cells to
the existing catalogue cards and table. Missing, heterogeneous, unauthorized and
unavailable facts remain separate; priced/disabled rates preserve decimal strings.
The requestable candidate endpoint remains price-free. Offline source checks
pass 34 related Go race tests and 127 focused frontend tests. The existing real
catalogue lifecycle passes on both PostgreSQL and MySQL, including explicit zero,
18-place disabled prices, immediate permission removal, isolation and single-
connection fallback. Owned resources are absent. The exact Price17 is now carried on main after Duty delivery `c800466`.
The mandatory composed transaction full126 passes in its isolated copy; main
format/check/full Task/build passes after the separate `7e55509` test repair
(3,973 frontend cases / 163 files). Controlled R3 browser acceptance now passes
English cards, Chinese table, reviewed Finance removal/restoration and original
API/browser Sessions after same-artifact restart. The exact production binary is
`afe8befa89cf35f2add4bdbdfc64c68ac109362be860cdb4cc39c5e086a3985a`.
Root acceptance digest is
`88557d59abe4461433a20bfe02c14c0cde6a9805ed77a20ddf380e7bb07e50ce`.
All 20 bounded responses were retained; all four observation rounds passed
without stabilization, and native calls remained zero. Earlier R1 setup and R2
finish assertions remain failed with uncaptured failure bodies, so their causes
remain unknown. Owned tabs, app and Compose resources are independently absent.
The separate monthly interface is still private and excluded from this gate;
its composed non-database regression is now running independently.


Price full126 final acceptance covers 126 ordered scenarios per driver, eight
constraints and 4,570 matched named RUN/PASS results, with no failure, skip or
race. Elapsed time was 2,797.017 seconds (PostgreSQL 1,209.24; MySQL 1,549.25).
Acceptance SHA-256 is
`b7fed0eafa8cce6cf0b05ebdce2c30d50eebb342f9afffd25b80841915e75958`.
Root independently reviewed the complete log, all 519 current production Go
files and absence of the exact owned Compose resources. Main final mandatory
check passes; product sources remain identical to accepted gates. The containing
commit delivers the bounded price projection. Monthly source separately passes
its composed complete Task (4,001 frontend cases / 164 files) and build, but is
not carried or accepted in the browser yet.


## Member catalogue monthly requests carried source

Price delivery is `20d6049fc44809e9065c63006d5440cdafd58fef`, pushed and read back
exactly. Six Monthly outputs are now carried, with all 1,600 product/test/
dependency/Task paths matching the isolated accepted regression. That regression
passes format/check/full Task/build, 4,001 frontend cases in 164 files, Node4,
dev2, Go race/coverage and production assets. Receipt digest is
`bdc4aa3aae7bcad4843659c57e60970b8c4636e75ee24c1e16b497950e27faf4`.
The private binary is a predecessor-stamped source-gate artifact, not current-main
browser evidence. Main final check/build and controlled three-native, bilingual
browser, scope isolation and original-Session restart remain pending. Provider
Models9 is still private and excluded. Capability totals are unchanged.


Monthly main format/check/build pass with binary SHA-256
`83b5c228ecc487c0c8852b77c78ea628a027eccda53b49977f990e36e57590a7`.
The normal build's sole dependency-lock delta removed 18 optional Linux libc
metadata arrays; those generated bytes were retained and the original lock
restored exactly before runtime binding. The first controlled helper failed
before native/browser execution: it requested the unregistered administrative
Team aggregate limits path. Recorded GET404 and the registered Session-scoped
`/api/v1/teams/:team_id/limits` establish a fixture-path error. Its zero-native
result and independently absent owned resources remain separate failed evidence.
A narrow helper successor is being prepared; no product repair is needed.


## Monthly controlled acceptance findings before refresh repair

The corrected Team-limits helper used an interactive root launch after an earlier
plain-pipe invocation reached EOF at setup. Both initial failed environments
made zero native calls, opened no browser tabs and were independently cleaned.
The third controlled environment passed exactly three native calls and immutable
Personal/Team attribution, complete reports Personal1/sharedTeam2/peerPersonal0,
EN/ZH cards/table, exact prices, All-source Unknown, requestable empty state,
filters and original API/browser Sessions after identical-artifact restart.
Owned tabs/listeners/Compose resources are independently absent.

This is not final accepted delivery. Bounded application access logs recorded
an eager selected-scope usage read before the manual catalogue refresh completed,
followed by another read; a later Session generation caused a separate read.
The first server record was HTTP503, but its original error/client receipt was
not captured, so cancellation causality is unproved. Source regression tests
reproduce the redundant eager request. A private repair also covers immediate
identical catalogue responses, which otherwise retain old counts. Final repaired
source checking/full Task/build and fresh native/browser/restart remain pending.
Provider Models' private predecessor gates pass 4,017 frontend cases/165 files,
but those gates cannot be transferred to future repaired Monthly bytes.


Monthly refresh repair is now carried as three exact frontend leaves. It removes
the eager usage refresh and keys the scoped report by each successful catalogue
read generation, including immediate identical responses. Four regressions retain
held Personal/Team reads, exactly one post-read query, error Unknown/no stale
count and failed-catalogue/no usage dispatch; all 175 related cases pass.
Source manifest digest is
`ec7c6cbfdf2fc5f6512310151ab72b4218ffd997e6d5823e170dcd0022bc0788`.
Original premature-request RED and intermediate fast-identical stale-count RED
are retained. New composed complete Task and main final check/build/browser are
still pending; the old 4,001/4,017 results do not prove these new bytes.


## Repaired Monthly request cells: final local acceptance

Complete repaired format/check/Task/build pass: 4,005 frontend cases / 164 files,
Node4, dev2, Go race/coverage and production assets. All 1,600 product/test/
dependency/Task paths match main exactly. The private verified 18-package optional
Linux libc-only lock delta is retained and original bytes restored; final source
agreement digest is
`2a4b547c306f2810c73a801d50437ca6f7f4e24b07416b173a71c88b6c8e25ba`.
Main format/check/build pass. Production binary digest is
`d3e661a0e5bc44fe7c10672faaae5397841412c39b023b39a1daa8fdd671eedd`.

Fresh controlled R4 passes exactly three native calls/attempts with immutable
Personal/Team attribution, complete Personal1/sharedTeam2/peerPersonal0 reports,
EN/ZH card/table, prices, filters, All Unknown and requestable empty state.
Both Personal and Team manual-refresh access-log slices contain exactly one
catalogue200 followed by one scoped report200; these are application logs rather
than browser network traces. Original API/browser Sessions retain counts after
identical artifact/config/database/journal restart. Peer pending reads correctly
show Unknown; initial member price Unknown after restart recovers on one explicit
read, without a cause claim. No all-request health claim is made.
Root acceptance digest is
`7f98318ac7de3fad1ce6868521e2d3fea30b09f37a2631b68da95e1e0ece6da7`.
All owned tabs/listeners/containers/networks/volumes are independently absent.
Earlier wrong-path404, EOF and pre-repair duplicate-read findings remain separate.
The containing commit delivers this bounded slice; Provider Models and whole-item
catalogue activation remain private, with the full objective active.


## Provider Models table carried source

Monthly request cells are committed/pushed as
`daddd4ef0c48101e02ecf74687e0133a044f084b`, with exact remote read-back.
Provider Models9 is carried after that repaired predecessor; all seven existing
beforeimages/two absent new leaves match and locale additions preserve Monthly.
All three Monthly refresh repair leaves remain exact. The existing six-column
Models tab table retains Add and detail links, literal identifier/Connection/
enabled filters, declarations, actor/target resets and fresh read/write authority.
No backend/schema/domain permission changes occur; binding projections remain
unavailable rather than invented. Final complete Task passes 4,021/165 on the
repaired Monthly composition; root verifies all 1,602 product paths, with main
mandatory check/build passing. Current controlled browser/restart acceptance is
recorded in Implementation, root receipt
`c58ce56bbb4626992713c21b6f272012165d9b902b46eacd72daee8c845ccbbb`.
All owned resources are absent. Earlier 4,017 evidence remains historical only.


The focus repair is carried as five exact frontend leaves. The local Drawer
forwards an optional typed finalFocus target; catalogue dismissal resolves the
current authorized actor/Model/representation control after successful Session
renewal replaces its DOM node. Removed, hidden, disconnected, pending, failed or
unauthorized targets receive no forced focus. Thirteen added regressions include
six meaningful failures against the exact predecessor; all 180 related cases in
seven suites pass privately, with types, lint, formatting and exact patch
round-trip. The original unchanged-node focus tests remain intact. Fresh complete
main/private gates and corrected controlled browser acceptance are pending. The
first failed browser run and its separately reached idle deadline remain recorded;
earlier 4,042 results do not accept these repaired bytes.


## Repaired whole-item catalogue access: local acceptance, 2026-10-06

The five-file focus repair passes fresh complete Task testing: 4,055 frontend
cases in 165 files, Node four, development lifecycle two, Go race/coverage and
embedded production assets. Mandatory checking and build pass in the private
composition and main; root independently verifies all 1,602 product paths and
the private gate receipt `4f2e0da9443d145bfc13df8886ff95c8b71e63682ec3d075bd43d404e38844a4`.

Controlled production/browser acceptance passes passive card/cell and Tab,
Enter/Space activation, Close/Escape focus to current article/row/native action,
source-menu isolation, Team-only and blank requestable detail, EN/ZH with retained
source/table filters, real Personal grant removal and normal actor replacement.
Original API and both original browser Sessions remain authorized after identical
artifact/config/database/journal restart without document reload or login.
Recorded Session/catalogue renewals corroborate the workflow; no DOM cause trace
is inferred. Text-selection gestures and delayed/error/obsolete authority races
remain focused source evidence. Recovered selector mistakes and normal
unauthenticated/logout Session401 are retained; no all-request health claim occurs.
The independent database has zero calls/attempts, discovery is one GET with zero
native POSTs, warning/error browser logs are empty, and all owned tabs/processes/
ports/Compose labels are independently absent. Root browser receipt digest is
`585ca88a59fe33e6f1c7cfa6ae44e699630940225fd9d1f4101bfccc8203cf7d`; production artifact is
`2a3229a4d621c3a6ba24746bbbcd4479b5dab30fad3e0de24f76b2fef3433ef9`.
The first focus finding and separate idle failure remain historical. The
containing commit delivers this bounded slice; F19 and 11/16/3 remain unchanged.


## Home identity labels carried candidate, 2026-10-06

Seventeen exact Go/UI outputs add a bounded self-only Role-label read and
bilingual identity Role/current Team labels sharing one monthly account page.
Paired frontend rules and Member Overview contracts are updated. Complete private
format/check/Task/build passes with 4,099 frontend cases/166 files, Node4/dev2,
Go race/coverage and embedded assets; root verifies all 1,609 product paths.
Private gate receipt is
`e060ec9029924ba00dc1cfc686c52644250d40f79245dc9ddfd1b89d73425b81`.
The frozen127 registry retains its entire released126 prefix; five new Go files
bring the production-Go inventory to521. Focused real PostgreSQL/MySQL checks
pass. Full127 R1 failed with proven ENOSPC; the first fixture/diagnostic-loss
failures remain recorded independently. Recovered exact Compose cleanup is
verified and full127 R2 is running with unchanged bounds/source. No complete
database, actual browser or phase-delivery claim is made yet. The full goal
continues, with F19 and 11/16/3 unchanged.

## Home identity actual browser acceptance, 2026-10-06

Final main format, mandatory checking and production build pass. The exact
1,675-path floor and artifact `5773edc91bd763d83aad57b62e8b2938509278949abfd303eedd14e0dd864896`
passed controlled Home browser acceptance: normal member login, Team identity and
monthly table shared paging in both directions, independent Role paging, finite
Finance English/Chinese labels with unchanged user content, explicit identity
refresh, real Role assignment and Team membership removal, and ordinary
member-to-peer logout/login without cached member facts. A separate original
member browser Session and the current peer browser Session survive same-artifact,
config/database/journal restart without reload or login. Three original API
Sessions are unchanged. Independent call, attempt, Personal Key and Project Key
counts are zero; both browser warning/error logs are empty. Both app PIDs, both
owned ports, all Compose resources and both agent tabs are independently absent.
Root browser receipt: `2f34a4faf787230d5d6e17363f293e03caec07a4fdac08d606e1933bf007af96`.

Captured paging windows show one account request for forward pages and one Role
request for Role paging. Normal permission/notification polling and Session
renewal overlap other windows; renewed authorization resets collections and no
all-window single-request claim is made. Language-only switching preserves page
choices with no Role/accounts request in its captured window. Legacy null names
and held/error/obsolete-response races remain source/driver evidence. The actual
full127 R2 is still running, so this is browser acceptance rather than database
or phase delivery. F19 and formal totals remain unchanged.

## Next private preparation, 2026-10-06

Provider Model stored-binding projection is prepared privately with independent
Provider/Model read authorization, a complete bounded current relationship
response, exact ownership checks and a candidate frozen GORM reverse index.
Its existing table adds the approved Model names and conjunctive Bound/Unbound
selector; Provider-only readers retain Unknown without privileged requests.
A meaningful source regression reproduced catalogue/projection mismatch recovery;
the narrow successor refreshes the catalogue before one fresh projection.
The private UI passes 107 related cases/four suites and scoped types/lint/format.
Backend source passes 58 balanced named tests; an evidence-only successor
explicitly binds the accepted final log while preserving an earlier failed log.
V69 registration, focused real-driver/query-plan/upgrade acceptance, the complete
129-scenario matrix, final composition and actual browser remain pending. No
private candidate is delivered on main, and F11 remains partial.

Member catalogue usage-member counts remain Unknown pending the user's choice
between monthly distinct recorded callers and current grant recipients. Existing
source-selected monthly request counts remain delivered and unchanged. No caller
identities, global member directories or guessed historical totals are planned.

## Home identity final database gates and delivery, 2026-10-06

The actual complete PostgreSQL/MySQL full127 R2 passes 254 direct scenarios,
eight constraints and 4,609 balanced named RUN/PASS results in 2,748.615s.
PostgreSQL completes in 1,184.07s and MySQL in 1,515.30s; the new endpoint case
completes in 7.22s and 9.00s. Managed/ordinary/overflow reads retain four/three/two
queries and the five-second budget. The complete 10,000-assignment reads take
approximately 123ms and 118ms; 10,001 overflows fail without a partial result.
Root independently reviews the exact ordered inventory, all 998 Go/Task sources,
original126 prefix, captured bytes and absent owned resources/ports/process.
Accepted full receipt: `ad361bc26afaff114c7d99b9d7b500b196ec239dde3de8ec32e0a5070e31aacc`.
Combined root gates: `ba84d5dfae8639f4cf1f56c9f39958dcbcd168a7aa937fea7120f7edd2ad5105`.
Together with complete Task, mandatory main checking/build and the separate
controlled browser receipt, this completes the bounded Home identity phase in
the containing commit. This does not complete F19 or the full goal. Earlier
failed/unknown attempts and their independently recovered cleanup remain retained.

The user confirmed model usage-member counts as monthly distinct recorded callers
for the selected resource account, without identities. Backend and frontend
preparation proceeds privately; existing monthly requests remain unchanged until
its own gates. Provider Model binding actual focused driver acceptance and final
composition/full129/browser remain separate pending gates.


## Historical ledger correction and Excel source preparation, 2026-10-06

The reviewed historical duty-role fixture now bounds its ledger reads to V1–V68
and checks every ordered version. It retains exact V1–V67 reconstruction and all
seed/grant/assignment/collision checks; the global harness separately verifies
the full current V69 ledger. This test-only correction is carried on main as a
20th source leaf. Corrected complete129 R3 and caller complete130 R2 are prepared
with exact foreign source bytes and executable modes; focused repair passes both databases; binding complete129 R3 continues; caller complete130 R2 was subsequently stopped for the lint successor below. No released migration changed.

The independent Excel download source preserves the existing price page download
step with Excel before CSV. The server generates one visible Prices sheet with
literal text cells, exact decimal strings, bounded complete snapshot and XLSX
limits; CSV and upload batch limits remain unchanged. Download needs independent
current price-read authority. Both formats cancel and discard late files/notices
on authority renewal, error, actor replacement, expiry or unmount. No client
workbook generation, numeric conversion or persisted Blob is introduced.

Backend source has 58 balanced focused results; frontend source has 65 cases in
four suites, types/lint/format and meaningful late-response regressions. Root
independently verifies frozen artifacts and payloads. An independent openpyxl
reader confirms 45 text cells, exact Unicode/formula-looking values and no formulas,
links or macros. These are source/data-only gates; actual database, final composed
Task/build, browser file delivery and phase delivery remain pending. The complete
Home identity CI is confirmed successful at its exact delivered SHA.


## Composed caller lint gate, 2026-10-06

The private final composition preserves the reviewed caller payloads but its
mandatory check reports QF1001 at the historical-actor character classifier.
Root approved one Boolean-equivalent predicate rewrite; exhaustive byte checks
cover all 256 values and all other function bytes remain unchanged. The failed
check remains historical evidence. No main caller source is carried.

Root intentionally stopped caller complete130 R2 and workbook complete130 R1
against the superseded predicate. Neither observed an integration failure before
the stop; both remain unaccepted. Source stays exact and root independently
verifies absent owned resources, listeners and process. Binding complete129 R3
continues unaffected. Revised caller/workbook source gates and fresh complete
validation are required before their separate deliveries.

## Model access copy and highlighting: scoped delivery, 2026-10-06

The existing Model access drawer adds accessible copy controls for its displayed
Base URL, nonsecret Personal authentication header template and exact generated
example. Team examples retain separate Session authentication without a Key
header. Local Bash highlighting preserves original characters as escaped React
text and leaves standalone Team here-document bodies opaque. Copy dispatch uses
current actor, Model, source, protocol and successful idle detail authority;
renewed reads, errors, selection changes and unmount discard late feedback. No
login, inference, grant operation, schema, API or dependency is added.

The clean Caller delivery worktree passes formatting, mandatory checking, complete
Task (4,231 frontend cases/169 files, four Node checks, two development lifecycle
checks, Go race/coverage and embedded assets) and production build. All 1,018
accepted Caller backend/Task paths remain exact. Known optional libc-only build
metadata is preserved and its private source lock restored; dependencies do not
change. Current main mandatory checking passes with all 1,634 combined tested product
paths exact. Check receipt SHA-256: `c1a042acb3fcf2456894b1d22ee6b1c8d0ea0c739acb1eb8fb65a3c9a0cea95b`.

Exact clean-delivery binary controlled EN/ZH card/drawer, real clipboard,
keyboard, grant withdrawal/restoration and ordinary actor replacement pass.
Both original browser Sessions and all original API Sessions survive identical
binary/configuration/database/journal restart without relogin; all six Session
rows stay exact. Root verifies zero Calls/Attempts/Keys/Projects and absent owned
resources/listeners; all temporary tabs are closed. Console warnings/errors are
empty. Withdrawal was observed on renewed closed-drawer lists; late open-drawer
responses remain source tests. A root SQL diagnostic initially used an absent
Session field and was corrected to the actual recorded fields without product
changes. Acceptance SHA-256: `eed88b6d189b45b3c365710cf3c5a68f61d83f87a9973f419320d623f7776ae7`. The historical
private XLSX candidate receipt does not substitute. The containing commit
delivers this bounded slice; F19 remains partial and totals stay 11/16/3.

## Model access SDK guidance and F19 acceptance, 2026-10-07

The containing commit completes the finite F19 capability inventory: own
Overview/identity, authorized catalogue/details, explicit Personal/Team sources,
filters/cards/table, scoped requests, prices/monthly requests/distinct recorded
callers, native examples/exact copying, keyboard/focus and SDK guidance. Team
Session authentication is the documented ownership adaptation. No additional
Overview field is specified. SDK configuration guidance does not certify client
versions or external-provider compatibility; the overall objective remains active.
Current totals are **12 complete, 15 partial and three unstarted**.

The existing drawer adds source/protocol-bound bilingual guidance and official
client links without a new API, dependency, schema or native request. Main
formatting, mandatory checking, complete frontend tests (4,267 cases in 171
files plus four Node checks) and embedded production build pass. Its 1,020
backend paths match the separately accepted XLSX full130 source; this UI phase
adds no persistence change or new database-matrix claim. The first full frontend
run remains failed (4,266 pass, one immediate held-renewal observation failure).
The narrow test successor waits for actual held fetching/loading and the rendered
403 before the unchanged absence assertions, within the original budget. All
145 focused cases, including 17 SDK cases, pass; no product workaround was added.

The final main binary passes eleven real browser checkpoints: English/Chinese
Personal and Team clipboard equality, native Base URL/header placeholders,
Team-only guidance, normal grant withdrawal/restoration, normal logout/login
actor replacement and identical-artifact restart with both original browser
documents. Six original Session rows remain byte-for-byte equal; Calls, Attempts,
Personal/Project Keys and Projects remain zero. One discovery GET and no native
POST occur; browser warning/error logs are empty. Root independently verifies
source/modes/HEAD/index and absent owned processes, containers, networks, volumes
and all three listeners. Root acceptance SHA-256:
`53278838e4c85610dfde1c5dc236cbe589bae3da22eadc039803c769e72667b5`.
Earlier prelaunch, stdin-EOF and idle-timeout attempts remain failed and cleaned.

Only Chat configuration was exercised in this browser fixture. Other native
configuration paths and unsafe Gemini names are covered by source tests. The
header language changes were made after closing the modal; source tests cover
live translation with retained selections. Withdrawal used renewed closed-drawer
lists; held late replies remain source evidence. No downloaded workbook or SDK
execution is inferred from this acceptance.

F19-specific negative evidence is indexed independently of broader release cases:

| Case | Delivered F19 evidence | Remaining broader acceptance |
| --- | --- | --- |
| A02 | Exact actor/Model catalogue and request authorization; unavailable or revoked sources hide details/examples; current actor replacement and server denials. See [catalogue contracts](CATALOG.md) and `model_catalog_integration_test.go`, `personal_model_requests_integration_test.go`, `team_model_requests_integration_test.go`. | Future enterprise/operations boundaries remain open. |
| A04 | Explicit Personal/individual Team source; Team visibility grants no Personal Key access; four native standalone Team programs preserve exact Team/User/membership and one debit through removal/rejoin and restart. See [Team catalogue examples](CATALOG.md#explicit-source-member-examples). | Global end-to-end release acceptance remains partial. |
| A06 | Stable Model ID across rename; expired aliases stop resolving and historical names stay reserved, covered by `catalog_integration_test.go` and `model_alias_retirement_integration_test.go` on both supported databases. | Final platform-wide release acceptance remains partial. |

## Complete133 failure checkpoint, 2026-10-07

The latest exact ProviderR3/F17 complete133 matrix fails after 2,858.254 seconds. Both new monthly-mode migration/lifecycle and Provider metadata scenarios pass on PostgreSQL/MySQL, but the existing member_teams scenario fails on both drivers and monthly_quota_notifications fails on PostgreSQL. Raw log SHA-256 `195087f038eef5727d5a14d00c016adceff222450eafe005ca3172e69e8fa4cb`. Source/modes remain unchanged; root independently verifies exact owned containers/networks/volumes absent, both captured listeners explicitly refuse connections, and the root PID is absent. This is failed evidence; no complete133, separate full132, native-production/browser or main delivery acceptance is transferred. Independent root-cause diagnosis is in progress. SDK c67f752/F19 delivery remains separate and accepted.

## Download delivery diagnostic, 2026-10-07

A disposable private diagnostic build tested retained Blob URLs and one fresh user gesture against the same Excel Blob. Both Excel A/B observations timed out after 10 seconds without a readable saved file, despite genuine activation and HTTP200. CSV A likewise timed out; real Session renewal removed its prepared handle before B could dispatch, so CSV B was not executed and no extra export retry was made. The two browser export GETs are separate from two authorized read-only expected-byte GETs; no native calls, Keys, Models, Providers or prices were created. Console warnings/errors were empty. Neither lifetime nor activation is established as the cause; the exact browser/platform/tool mechanism remains unknown.

The private binary SHA-256 is `8672b62752a03b6e203dd526f4b34865408db5dcaae0b81bfc34cd62cb179d3a`; root diagnostic receipt is `4687cc239a522ecd892f4512388f527cd6a33cfb5188081d51d44ae327fe74e2`. All 1,696 source paths/modes remain exact, main source/index stayed unchanged during the experiment, and root independently verifies owned process, Compose resources and both listeners absent. The initial diagnostic setup assertion incorrectly expected HTTP200 rather than actual201; that orchestration-only failure was cleaned and retained before the corrected run. No diagnostic UI is proposed for shipment. XLSX source and dual-driver evidence remain pending genuine browser file delivery.

The complete133 failure is retained. A private successor now composes only two narrow test-fixture corrections: explicit legacy stop defaults in the member-team whole-row fixture, and the existing fixture-owned publication fence for one-shot positive monthly-notification observation. Assertions, deadlines, native inputs, policies and production guards remain unchanged. The original notification failure branch remains unknown; fresh focused and complete PostgreSQL/MySQL results are required.

## SDK remote checks, 2026-10-07

All current-head checks for SDK commit `c67f752d20f26ba62f44e0afe4cfd664e8f73b64` have completed successfully: CI37495947051 (Backend, Frontend, PostgreSQL/MySQL integration and build artifacts), GolangCI37495947077 and Actionlint37495947120. This remote result is separate from the pending private Provider/F17 successor and unresolved XLSX browser delivery.

## Renewed Provider/F17 focus, 2026-10-07

The exact two-fixture R5 successor passes all ten renewed real database scenarios in 180.338 seconds: monthly notifications, member Teams, monthly behavior migration/lifecycle and Provider metadata on both PostgreSQL and MySQL. Thirteen named RUN/PASS events balance; source1,717/modes stay exact and root independently verifies captured owned containers/networks/volumes, process and both explicitly refused listeners. Focus acceptance SHA-256 `d0720c258d34cae20a36def91f0ab227f820ccd2e58822dcaa1d5cf3af00e38a`; raw log `77d8d3cc815f26c204047e877a4b58b300b38501762edf7a34bd9a0dc4942a43`.

A separately authorized fresh complete133 matrix is now running against this same source under the original bounds. It must pass 266 ordered direct cases, eight constraints and five package terminals before native/browser/restart delivery. Historical failed Full133 R1 remains retained; the original notification failure branch remains unknown. Model protocol search and grouped routing feedback are independent frontend candidates; they do not inherit or modify this database result.

### Model protocol search and DOM Storage test isolation (2026-10-07)

Main `9952edbb32d8e7a829ee4a953f7d9b45355d04e1` is committed and pushed, after
`5e16bc0` test environment isolation. Search uses recorded protocol identifiers
and displayed labels alongside current names, with no additional reads or
authority. Eleven search regressions and two real DOM Storage regressions pass;
mandatory checking, formatting, complete Task testing (4,280 frontend cases in
172 files, four Node checks, Go race/unit, development lifecycle and production
assets) and production build pass. Exact-head remote checks remain pending.
F12 remains partial; source-only routing and Team aggregate behavior work does
not establish actual browser, native, schema or runtime acceptance. The full
objective remains active.

### Model protocol search and grouped routing drafts (2026-10-07)

Configured Model protocol search is delivered by `9952edb`, after `5e16bc0`
DOM Storage test isolation. Search includes raw protocol identifiers and current
displayed labels without additional catalogue reads. Routing now uses the existing
Model detail tables grouped by protocol, with live configured draft totals and one
complete-set save. Only integer 0–100 weights and per-protocol totals of 100 gate
local submission; readiness remains advisory and credential eligibility belongs
to the server. Existing price permissions, current actor/target renewal, generic
Add binding and grants are preserved. English/Chinese switching retains drafts.

Eleven search cases, two Storage cases and eighteen routing regression cases
pass. Mandatory checking, formatting and complete Task testing pass, including
4,298 frontend cases in 173 files, four Node checks, Go race/unit, development
lifecycle and production asset tests. The production build passes. Actual grouped
routing browser acceptance remains pending; these source checks do not prove
external Provider compatibility, inference or fleet application. F12 and the
full objective remain active. Independent price-file browser delivery and Personal
monthly behavior/Provider metadata native/browser/restart gates remain pending.

## Personal monthly behavior contextual delivery proposal, 2026-10-07

The Personal-only candidate adds User monthly Token/money stop/alert-only modes,
canonical omission/default/reset semantics, independent hard Key gates, complete
accounting and current-publication checks, User-only confirmation and exact
uncertain retries/Restore snapshots. Null caps are inactive, zero is real and
numeric effective minima do not imply a combined hard-stop policy.

Proposed scope is23 Go leaves and13 UI leaves: final backend20, the ledger70/132
registration leaf, two accepted test-only compatibility companions, and UI13.
Provider name metadata routes/service/audit/intent additions and Team V71 are
excluded. Current SDK, literal protocol search, routing validation, pending XLSX
sources and all existing foreign documentation remain preserved.

Private full133 R2 is accepted for its exact separate Provider/Personal source.
Root reports the latest Personal controlled seven-call/three-native, null-usage
blocked facts, bilingual/permission/Key-parent controls and original API/browser
Session restart checks passed; final runtime/cleanup receipt is
**[PENDING_ROOT_RUNTIME_RECEIPT]**. This contextual main has not been applied,
checked, built or accepted as complete132. Main source gates:
**[PENDING_MAIN_GATES]**; complete dual-driver132:
**[PENDING_CONTEXTUAL_FULL132]**; delivery: **[PENDING_DELIVERY]**. Keep F17 partial
and capability totals unchanged. Preserve all earlier failed source/fixture/helper
runs as historical evidence. Root owns final status and delivery wording.
