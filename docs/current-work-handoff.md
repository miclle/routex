# Current Work Handoff

- **Status:** implementation active; prioritize partially completed capabilities
- **Updated:** 2026-10-04
- **Repository / branch:** RouteX / `main`
- **Previous checked baseline:** `15effef1b54b07b8ae4120283e193f291ece9583`, pushed and read back from `origin/main`
- **Current owner:** coordinator owns integration, actual runtime acceptance and delivery; parallel owners prepare isolated source and fixtures
- **Transport:** identify the current checked delivery with `git log -1 -- docs/current-work-handoff.md` and independently verify its upstream
- **Roadmap:** [Implementation and acceptance index](IMPLEMENTATION.md); cross-task coordination is maintained separately in `~/dotfiles/projects/routex/implementation-plan.md`

## Objective and authorization

Implement every valid F01–F30 capability and close every A01–A20 acceptance case.
The user resumed the full objective and prioritized partially completed work. No
new time limit or pause was requested. Completing one package does not complete
or pause the objective. Formal totals are now 11 complete, 16 partial and three unstarted after
checked F07 closure.

Preserve the Go/React architecture and domain boundaries, PostgreSQL/MySQL,
immutable GORM-first migrations, approved layouts, shadcn/ui and Base UI,
English-default bilingual interfaces, English documents and commits, parallel
ownership and phased main-branch commits and pushes after required checks pass.
Controlled upstream evidence remains separate from real-provider acceptance.
Never claim runtime enforcement from persistence alone.

## Recent checked deliveries

| Main source | Package | Acceptance |
| --- | --- | --- |
| `ef821a6` | F06 Team roles | Complete capability; exact remote checks passed |
| `598ffd1` | Team usage reports | Scope-preserving reports; exact remote checks passed |
| `4c669dc` | Canonical Project authority | Exact manager/resource identities; exact remote checks passed |
| `35e279a` | User/Team defaults | Explicit restore and current authority; exact remote checks passed |
| `5006126` | Initial Project managers and Overview | Scoped creation and authoritative summaries; exact remote checks passed |
| `4dffc88` | Personal Model requests | Pending requests never grant access; exact remote checks passed |
| `ea73a05` | Shared Team Model requests | Shared pending uniqueness and independent review; exact remote checks passed |
| `d29ffa8` | Four native Team text protocols | CI 37146984375, Actionlint 37146984325 and GolangCI-Lint 37146984327 passed |
| `4a32986` | Initial Project resources and simultaneous requests | Full local checks, 1181 frontend cases, complete PostgreSQL/MySQL matrix, both real-process authentication/native lifecycles and controlled bilingual browser/two-restart proof passed |
| `01129cf` | Combined-router API 404 repair | Full check and complete handler race tests passed in development and production |
| `3e8a159` | Model route base prices | Full local checks, 1207 frontend cases, complete PostgreSQL/MySQL matrix and controlled bilingual production browser/restart/revocation proof passed |
| `81f4697` | Team native comparison and Key/Team native finality | Full local checks, 1345 cases, complete PostgreSQL/MySQL matrix and controlled bilingual native/cancellation/restart/revocation proof passed |
| `2fc39b1` | Final F07 Project lists/navigation | Full check/test, 1365 frontend cases, complete PostgreSQL/MySQL matrix and controlled bilingual filter/permission/alias/restart proof passed |
| `32643a8` | Team request code and Reset | Final main check/test/build, 1521 frontend cases, independent generated programs and controlled renewal/revocation/restart proof; all exact remote checks passed |

The complete initial-resource matrix passed with Handler 971.977 seconds and
Service 7.062 seconds. Earlier failed fixture runs are recorded in the acceptance
index and do not count as acceptance. Frozen GORM V45 and historical receipts do
not restore later deleted resources, relationships, grants or policy.

The complete Model-price matrix passed with Handler 971.759 seconds and Service
7.422 seconds; focused driver checks passed in 129.542 seconds. Amounts remain
exact decimal strings with currency/unit, known zero, disabled and absent rates
kept distinct. Model detail and price reads authorize independently.

All controlled QA resources were removed and English was restored. No paid
upstream calls or external credentials were used. Relevant contracts live in
[Catalog](CATALOG.md), [Pricing](PRICING.md), [Resources](RESOURCES.md),
[Personal requests](PERSONAL_MODEL_REQUESTS.md) and
[Team requests](TEAM_MODEL_REQUESTS.md).

## Remote checks

Initial-resource Actionlint 37150472987 and GolangCI-Lint 37150472989 passed;
CI 37150472951 was canceled by a later main push under existing branch concurrency.
API-repair Actionlint 37151365367 and GolangCI-Lint 37151365400 passed;
CI 37151365382 was also canceled by the later main push.

Model-price CI 37152547296, Actionlint 37152547292 and GolangCI-Lint 37152547317
all passed for exact source `3e8a159`. CI includes both driver integrations and
build artifacts. Disabled-supply source `17c9847` CI 37153960379, Actionlint
37153960330 and GolangCI-Lint 37153960321 also succeeded. Inspect later exact main
checks separately; canceled runs are never green.

Team-comparison source `81f4697` CI 37158084172, Actionlint 37158084229
and GolangCI-Lint 37158084221 all succeeded, including both-driver integrations
and build artifacts.

## Checked package: disabled supply readiness

The coordinator carried four frozen source/test files and the integration entry
from the isolated `model-supply-status` worktree:

- `internal/routex/service/catalog_models.go`
- `internal/routex/service/catalog_model_detail.go`
- `internal/routex/service/catalog_model_readiness_test.go`
- `internal/routex/handler/model_supply_status_integration_test.go`
- `internal/routex/handler/auth_integration_test.go`

The catalogue now displays disabled Provider-model supply as not ready. A private
credential-coverage fact preserves positive-weight activation requirements, so
temporary availability does not erase configuration. No DTO, schema, runtime,
pricing, grant or frontend contract changes.

Focused actual PostgreSQL/MySQL race proof passed in 101.402 seconds, including
list/detail enable/disable/re-enable, unchanged prices/weights/names/grants,
covered weight writes, uncovered rejection, zero native dispatch while disabled
and current permission revocation. Full main check/test passed 1207 frontend cases in 79 files, Go race/unit,
development lifecycle and embedded production assets. The complete
PostgreSQL/MySQL race matrix passed (Handler 1012.087 seconds, Service 7.001
seconds), and final mandatory check passed. Owned Compose resources were removed.
Source and acceptance documents are delivered together; inspect this document's
commit and exact remote checks for the current delivery.

## Isolated packages

Use `git worktree list` to locate these attached worktrees; never copy their entire
diffs because each contains carried baselines and shared harness/rule files.

| Worktree | Owned source and status | Remaining gate |
| --- | --- | --- |
| `team-native-comparison` | Frozen Team comparison and Key/Team native finality source carried into main; local checks and controlled production proof passed | Complete main matrix passed: Handler 1032.267 seconds / Service 8.273 seconds; final formatting/check passed |
| `project-list-navigation` | Bounded exact Project list projection and literal ID search adapter; six UI/type/catalog files, authorized legacy redirects and fixtures | Carried comparison source passed 1365 cases; Complete main regression passed Handler 1041.548 seconds / Service 7.515 seconds; final check and controlled bilingual browser/restart/revocation passed |
| `team-session-code` | Frozen independent Team-native code builders, conversation/comparison dialogs, parameter Reset and finality tests | Final main check/test/build passed 1521 cases/88 files; separate actual24 generated programs and final9 browser calls passed; final Session renewal/revocation/restart accepted |
| `team-native-attachments` | Creator-private Team media source frozen; V46/runtime source passed race/lint, interface passed 497 focused cases; acceptance fixtures, English docs and controlled browser helper are frozen | Renewal candidate passed 1557 cases/89 files; V46 migration passed both drivers, corrected lifecycle focus runs; browser acceptance remains |
| `model-supply-status` | Four frozen files listed above | Checked local delivery; inspect exact new-main remote checks |
| `team-quota-notifications` | Frozen F17 Team aggregate monthly settled-exhaustion observer/inbox source and fixtures; V47 follows V46 | Focused actual driver lifecycle and V47 passed; renewed source check/test/build passed 1237 cases/81 files; controlled browser and main acceptance remain |

Project list completion is the bounded remaining F07 package: total retained
Project Key records in both lists; configured monthly Tokens/money/currency/RPM/TPM
in the administrator list; literal name-or-ID search; managers → settings and
models/limits → resources redirects after fresh exact authorization. Unknown,
unauthorized, absent local policy, null controls and known zero remain distinct.
Do not substitute Overview active-Key counts or inferred effective defaults.

Team comparison preserves two to four independent native text lanes, a shared
composer, transient Session authority, individual cancellation and completed text
history. Team media remains unfinished. Final code export and Reset candidate
passed current-main check/test/build with 1521 cases in 88 files and controlled
production/browser acceptance. The media renewal candidate passed 1557 cases in
89 files; corrected actual lifecycle/storage focus passed in 76.817 seconds; controlled browser and main acceptance remain pending. A native
401 requires an active authoritative no-store Session probe before logout;
upstream rejection does not establish Session expiry. The conversation Key path
uses the same native finality/history/export criteria.

## First valid action

1. Team code/Reset final main acceptance is complete: 1521 frontend cases/88
   files, complete check/test/build, nine new browser native calls, automatic
   Session renewal cleanup, bilingual controls, revocation/restart without replay
   and owned-resource cleanup. The unchanged builder has separate earlier actual
   proof from 24 programs. Delivered and read back as `32643a8`; Actionlint 37161943329 and
   GolangCI-Lint 37161943298 and CI 37161943372 all passed for the exact delivered source.
2. Creator-private Team media source remains frozen; the latest actual focus
   failed at the legitimate creator DELETE. Named diagnostics located an
   initialized GORM locking-query state leak across User/Team/member reads.
   Both V46 migration cases passed. The fresh-Session query clone preserves locks/context and exact guards; its
   real authorizer regression demonstrated red/green on both dialect adapters.
   The rebuilt clone candidate passed complete check/test/build at 1557 cases/89
   files. Root-only lifecycle and V46 focus passed on both drivers in 76.817 seconds; controlled browser proof and main acceptance remain.
3. Team monthly notifications now passed the complete focused PostgreSQL/MySQL
   lifecycle and V47 cases (Handler 109.108 seconds), including both existing
   siblings. The inbox Session-renewal repair passed isolated check/test/build,
   1237 frontend cases/81 files and meaningful same-data/same-millisecond renewal
   regressions. Controlled browser proof and main integration remain pending.
4. F07 remains delivered at `2fc39b1`; all exact remote checks passed. Member
   Overview's updated source passed 1408 cases/83 files and complete isolated
   check/test/build. Its cold calendar read is repaired; actual fixtures then
   exposed an overlong test price ID and an omitted blocked history row. The
   final focused lifecycle passed on both drivers in 82.517 seconds. Integrated
   main check/test/build passed 1584 frontend cases/90 files. Controlled production browser proof now passed, including unknown usage, current membership removal/rejoin and real-process restart with no replay. The complete main matrix passed with Handler 1091.619 seconds and Service 7.953 seconds; final mandatory check passed. This phase is ready for its checked main delivery.
5. Update the external coordinator separately, preserving unrelated dotfiles
   changes. Continue partial capabilities without pausing the full objective.

The coordinator alone runs actual database/app/browser fixtures. Other owners
prepare source and focused unit/race/type/style evidence. Isolated test databases
never reuse development volumes; cleanup targets only the owned project/process.

## Next partial-capability source package

F17 Team aggregate monthly exhaustion notices extend the existing immutable
monthly inbox and bell without changing admission or introducing warning
thresholds. Observe only current applied monthly Tokens/money, known settled
usage, full coverage, exact calendar/currency and an active Team. Freeze bounded
exact enabled owner/member recipients; operators receive no implicit fanout.
Read and mutation authority require current exact active membership. An original
recipient may regain aggregate-history visibility after authorized rejoin; new
members do not receive old notices on replay. V47 is an additive frozen scope
constraint after creator-private attachment V46; historical migrations remain
unchanged. Root restores V47 after the existing frozen V35 reconstruction fixture.
This source package and broader alerts/stop-policy settings remain unfinished.

The accepted F19 assessment is now being implemented in the isolated
`member-overview-accounts` worktree. Backend and acceptance owners prepare
source only; real acceptance remains coordinator-owned. The bounded self-only
member Overview account summary includes Personal plus current Team accounts,
separate aggregate/stable-member quota facts, scoped usage navigation, exact amounts, unknown coverage and fresh
authority. Existing Model source filters/detail/native examples need no redesign.
No accepted Overview delivery is claimed.


## Team code/Reset current main acceptance

The 22 scoped paths in `/tmp/routex-team-code-main-manifest.txt` are carried.
Final current-main check/test/build passed 1521 cases/88 files. The final binary
passed nine independent native browser calls, automatic Session renewal clearing
models/history/code while retaining the draft, bilingual state, revocation and
restart with no replay. The unchanged snippet builder has earlier independent
proof from 24 programs across three languages, four protocols and both stream
modes; they are not counted as the final browser calls. The complete Project
backend matrix remains valid because this phase changes only frontend and docs.
All owned actual resources were removed. Media lifecycle proof follows serially;
member Overview source is frozen with isolated full gates, actual proof pending.

## Parallel next member Overview slice

The isolated `member-overview-usage` worktree carries the monthly-account baseline
without delivering it separately. Three source owners prepare server-clock `30d`
report support, the approved three cards/Token trend/Model-Key breakdown and a
fresh database acceptance fixture. The range is exactly the preceding 720 hours;
calendar buckets preserve partial edges. Reads stay Personal-only and reuse the
existing complete-query bounds, exact decimal strings and unknown coverage.
The existing inclusive success-rate denominator remains authoritative and is
explained in both languages. No actual database/app/browser work runs there.
Full isolated check/test/build now passed 1625 frontend cases in 93 files,
Go race, development lifecycle and production assets. Package/lock baselines
are unchanged. This is source evidence; the fresh actual-driver fixture and
production browser acceptance remain pending. The coordinator's current actual
monthly-account complete PostgreSQL/MySQL matrix has passed; its owned resources
were removed. The next actual gate is the already-frozen Team media package,
carried by exact source paths and narrow shared merges before rebuilt-main
controlled browser proof and complete database regression.

## Checked monthly-account delivery

The current bounded phase adds the self-only monthly-account handler/service,
coherent journal batch, Home account table, exact API types/validation, paired
catalogs and source/driver fixtures. Source manifest is those new monthly-account
files plus `handler.go`, `auth_integration_test.go`, `api/overview.ts`,
`types/overview.ts`, `i18n/index.ts` and `views/home/index.tsx`. The paired frontend
rules, README, Usage link, Member Overview contract, acceptance index and handoff
ship together. Team code evidence is refreshed without changing that source.

All required local gates passed: check, test (1584 frontend cases/90 files), build,
complete PostgreSQL/MySQL integration (Handler 1091.619 seconds, Service 7.953
seconds), focused actual lifecycle, controlled production browser and final check.
No schema or identity-persistence change belongs to this phase. Exact remote
checks must be inspected after this document's commit is pushed; preceding source
checks do not prove the next commit. The full objective remains active.

## Active rebuilt-main Team media candidate

The monthly-account phase was delivered and read back as `15effef`. Its Actionlint
37166060859, CI 37166060858 and GolangCI-Lint 37166060843 all passed
for that exact commit.

The coordinator carried 28 frozen Team media source/fixture files and five narrow
shared registrations/historical-test changes into the current main worktree.
F07, code/Reset, Session generation and monthly Overview source remain intact;
no entire older worktree diff or stale rule/document file was copied. V46 follows
V45 with GORM migration and historical guard reconstruction. The paired frontend
rules, database/storage/Team/Playground contracts and README are updated narrowly.
Formatting and complete main check/test/build precede a rebuilt-main production
browser run and mandatory complete PostgreSQL/MySQL matrix. This source is not yet
delivered. Exact creator/current membership, immutable expiry, post-I/O rechecks,
one final admission and independent native lanes remain required. The isolated
repaired lifecycle focus is evidence for those frozen backend paths; earlier
failed runs remain failures.

The initial integrated frontend run passed 1639/1640 cases and failed one old
text-only language assertion. One narrow existing comparison test seam is now
included, making 29 frozen source/fixture paths plus the shared integrations.
Full main check/test/build reruns; do not claim the failed run as acceptance.

Integrated Team media check/test/build now passed 1640 frontend cases/92 files,
Go race, development lifecycle and production assets after the narrow copy seam
repair. Package/lock baselines are unchanged. The rebuilt-main binary is ready
for controlled production browser acceptance; complete main V46 matrix remains
pending. The separate Home thirty-day four-case actual focus passed PostgreSQL
and MySQL in 117.082 seconds, including current usage/Team/account siblings. Its
new report assertions use seeded immutable facts and make no native completion
claim. Final Home integration/browser acceptance remains pending.

The rebuilt-main controlled media browser passed 16 distinct native requests,
four-protocol media/text rounds, creator/exact-membership and one-hour isolation,
shared retention during independent cancellation, exact-version cleanup, four
pre-attempt grant-revocation failures and restart without replay. English was
restored, browser warnings/errors were absent and owned QA resources were removed.
An earlier optional two-lane cancellation scenario failed the four-lane helper
expectation after Session renewal; it is retained as failed evidence. The fresh
complete run confirmed all four selected lanes before dispatch and passed.
A duplicate historical V23 fixture rewind is removed without relaxing constraints.
The full current-main V46 PostgreSQL/MySQL matrix is now running; commit/push
remains gated on it and the final required check. The full objective stays active.

## Checked Team media package

All current-main local gates passed: full check/test/build, 1640 frontend cases
in 92 files, Go race, development lifecycle, embedded production assets and the
complete PostgreSQL/MySQL matrix (Handler 1137.119 seconds; Service 7.537 seconds).
The controlled production proof above uses the same rebuilt binary. Owned
application/storage/native fixtures and all matrix Compose resources were
removed. Final required check precedes scoped main commit/push; read the current
document commit and remote ref for transport, and inspect its exact remote checks.
No external-provider or multi-node acceptance is implied. Next delivery is the
frozen Home thirty-day package, followed by Team notifications; Usage CSV source
owners are preparing its independent complete-report/privacy acceptance. The
full objective remains active.
