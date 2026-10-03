# Current Work Handoff

- **Status:** implementation active; prioritize partially completed capabilities
- **Updated:** 2026-10-04
- **Repository / branch:** RouteX / `main`
- **Previous checked baseline:** `17c984715009dde7241cfc278cf094fcfb405e98`, pushed and read back from `origin/main`
- **Current owner:** coordinator owns integration, actual runtime acceptance and delivery; parallel owners prepare isolated source and fixtures
- **Transport:** identify the current checked delivery with `git log -1 -- docs/current-work-handoff.md` and independently verify its upstream
- **Roadmap:** [Implementation and acceptance index](IMPLEMENTATION.md); cross-task coordination is maintained separately in `~/dotfiles/projects/routex/implementation-plan.md`

## Objective and authorization

Implement every valid F01–F30 capability and close every A01–A20 acceptance case.
The user resumed the full objective and prioritized partially completed work. No
new time limit or pause was requested. Completing one package does not complete
or pause the objective. Formal totals remain 10 complete, 17 partial and three
unstarted until the relevant capability acceptance is closed.

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
| Current documentation commit | Team native comparison and Key/Team native finality | Full local checks, 1345 cases, complete PostgreSQL/MySQL matrix and controlled bilingual native/cancellation/restart/revocation proof passed |

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
| `project-list-navigation` | Bounded exact Project list projection and literal ID search adapter; six UI/type/catalog files, authorized legacy redirects and fixtures | Carried comparison source check/test/build passed 1365 frontend cases in 82 files; actual driver/browser proof remains |
| `team-session-code` | Frozen independent Team-native code builders, conversation/comparison dialogs, parameter Reset and finality tests | Full carried-source check/test/build passed 1495 cases; actual generated-program/browser acceptance remains |
| `team-native-attachments` | Creator-private Team media source frozen; V46/runtime source passed race/lint, interface passed 497 focused cases; acceptance fixtures, English docs and controlled browser helper are frozen | Full carried-source check/test/build passed 1551 frontend cases in 88 files; real V46/storage/native/browser acceptance remains |
| `model-supply-status` | Four frozen files listed above | Checked local delivery; inspect exact new-main remote checks |
| `team-quota-notifications` | F17 Team aggregate monthly settled-exhaustion observer/inbox extension; source and fixture owners are active, frozen V47 follows V46 | Exact current recipient authority, immutable historical snapshots and driver/browser acceptance remain |

Project list completion is the bounded remaining F07 package: total retained
Project Key records in both lists; configured monthly Tokens/money/currency/RPM/TPM
in the administrator list; literal name-or-ID search; managers → settings and
models/limits → resources redirects after fresh exact authorization. Unknown,
unauthorized, absent local policy, null controls and known zero remain distinct.
Do not substitute Overview active-Key counts or inferred effective defaults.

Team comparison preserves two to four independent native text lanes, a shared
composer, transient Session authority, individual cancellation and completed text
history. Team media and code export remain separately unfinished. Code export and
parameter Reset passed full carried-source check/test/build with 1495 frontend
cases in 86 files. Creator-private Team media passed its combined full gates with
1551 cases in 88 files; source, fixtures, English documents and owned browser
helper are frozen. Actual acceptance remains separate. A native 401
requires an active authoritative no-store Session probe before logout; upstream
rejection does not establish Session expiry. The carried conversation Key path now applies the same native finality/history/
export criteria, covered by 53 focused cases.

## First valid action

1. Run the frozen Project list/navigation focus at `/tmp/routex-project-list-focus.sh`
   serially, then controlled bilingual production browser/restart/revocation proof
   with `/tmp/routex-project-list-browser-qa.py`. Carry only its owned source and
   narrow documentation/rule changes into main, run required complete regression
   and final check, and close F07 only after acceptance. Team comparison is checked
   and delivered with this document; verify its exact pushed SHA and remote checks.
   The earlier matrix failure remains recorded separately in the implementation
   index. Repaired focus passed 66.650 seconds; final matrix passed Handler
   1032.267 seconds and Service 8.273 seconds with owned cleanup.
2. Complete Project list actual-driver and bilingual production browser/restart/
   revocation proof; its carried-source local check/test passed 1227 frontend
   cases in 80 files; the combined comparison candidate subsequently passed 1365 cases in 82 files. Close F07 only after its concrete remaining package passes.
3. Finish the separate Team code-export and parameter-reset package, preserving
   fresh authority, completed-text history and transient authentication.
4. Update the external coordinator separately, preserving unrelated dotfiles
   changes. Continue remaining partial capabilities; do not mark the full goal
   complete or paused after these packages.

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

The F19 read-only assessment identifies the next bounded member Overview account
summary: Personal plus current Team accounts, separate aggregate/stable-member
quota facts, scoped usage navigation, exact amounts, unknown coverage and fresh
authority. Existing Model source filters/detail/native examples need no redesign.
No Overview implementation or acceptance is claimed.
