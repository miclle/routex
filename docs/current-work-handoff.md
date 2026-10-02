# Current Work Handoff

- **Status:** implementation active; F08 Key proof hardening pushed; F11 readiness pushed; receipt-backed retirement pushed; member model availability correction pushed; source directory pushed; monthly quota exhaustion inbox checked for phased delivery; Project quota requests assessed next
- **Updated:** 2026-10-02T19:15:23+08:00
- **Repository / branch:** RouteX / `main`
- **Previous checked source baseline:** `13c1b0880a7e0f5fef2558016561015cf683ce31`, pushed and read back from `origin/main`
- **Current owner:** coordinating task; three implementation owners have frozen quota observation/migration/worker, scoped inbox and UI; coordinator completes full database acceptance and delivery; next partial-capability assessment covers Project quota requests
- **Next owner:** current task continues the resumed goal
- **Transport:** identify this checked delivery with `git log -1 -- docs/current-work-handoff.md` and independently verify its upstream
- **Coordination plan:** `~/dotfiles/projects/routex/implementation-plan.md`; synchronize separately and preserve unrelated changes

## Objective and authorization

Implement every valid F01–F30 capability and close every A01–A20 acceptance case.
Preserve the Go/React architecture, Gateway/Control Plane/Data Platform boundaries,
PostgreSQL/MySQL portability, immutable GORM-first migrations, approved layout,
shadcn/ui and Base UI, English-default bilingual interfaces, English documentation
and commits, parallel ownership, tests, and phased main-branch commits and pushes.

The 2026-09-30 two-hour implementation window and its pause are historical. On
2026-10-02 the user resumed the full objective and prioritized partially
implemented capabilities. No new two-hour limit or pause was requested. Completing
one package does not complete the objective or authorize marking it paused.

## Previous checked delivery: F11 Credential metadata and deletion

| Area | Implementation and boundary |
| --- | --- |
| Metadata | Independently authorized GET/PUT metadata endpoints, reviewed strong representation ETags, strict name/priority/reason validation, Connection-scoped portable name comparison, transactional typed audit, current-target publication reconciliation. Ciphertext, Connection identity, enablement, verification, and discovery coverage are unchanged. |
| Deletion | Independently authorized DELETE with a reviewed metadata ETag and reason. One transaction removes only the exact Credential and its discovery-access rows and records a typed audit event. A post-commit tombstone excludes the removed ID from new dispatch before publication. Success confirms current absence and runtime application; a repeat does not assert a historical operation receipt. |
| Interface | Existing compact Credentials table and row action menu, independent read/write authority, local Base UI dialogs, resource-bound drafts, explicit conflict review, immutable uncertain retries, paired English/Chinese labels, and no optimistic deletion. |
| Database | GORM queries and transactions, governance/Connection/Credential locks, portable duplicate comparison, no new migration or dependency. Provider/Connection/models/weights/grants and immutable call/attempt history remain intact. |
| Remaining F11 | Staged rotation, complete pool operations, and real-provider acceptance remain open. New rotation must create a separate pending disabled Credential, verify and explicitly activate it before separately disabling its predecessor; do not overwrite the predecessor's encrypted secret in place. |

The next package assessment covers staged rotation: predecessor lineage,
concurrent/uncertain creation, explicit activation/publication, migration and
negative acceptance. Verification of a new Credential never implicitly enables it.
No paid upstream calls or external credentials have been used.

## Previous checked package: staged replacement preparation

Backend and frontend own separate files. New pending disabled Credentials receive
immutable historical predecessor IDs; frozen GORM V31 adds that nullable column
and durable non-FK creation receipts. The strict replacement endpoint uses reviewed
source If-Match, required name/secret/reason and a stable UUIDv4 request ID. Its
three-field 201/200 response acknowledges saved preparation only. Pending additions
do not refresh runtime or change active routes; actual verification and explicit
enablement remain separate. Old Credential state and discovered coverage stay
intact. Multiple distinct named successors are allowed; an identical request ID
still identifies only one creation.

An authorized retry checks the durable actor/source/non-secret intent receipt and
compares the submitted secret to the exact result's immutable ciphertext within
the service. No unkeyed secret digest is stored. Deleted result, changed actor/body/
secret, or unavailable storage cannot create another result. Source GET cannot
resolve creation uncertainty. The UI freezes original UUID/body/ETag through every
rejected uncertain retry, keeps secrets out of caches/storage, shows lineage in
the existing name cell, and clears sensitive state on completion/dismissal/unmount.

Focused UI tests passed **128/128**, including 35 new replacement cases and existing
metadata/deletion/filter/catalog/i18n flows; TypeScript and scoped ESLint passed.
Full check and test passed (619 Vitest cases in 51 files, Go race/unit, four Node
checks, development lifecycle and embedded production assets). Independent
contract review reported no actionable finding. The first real database matrix
failed in the new PostgreSQL upgrade fixture because the pinned GORM DropIndex
emitted invalid `CURRENT_SCHEMA()` syntax. Two fixed allowlisted test-only index
statements correct this fixture while MySQL retains Migrator.DropIndex. Focused
real PostgreSQL/MySQL migration plus replacement lifecycle tests passed under
the race detector (127.168 seconds), and mandatory check passed after correction.
The final complete PostgreSQL/MySQL matrix passed (Handler 404.327 seconds,
Service 5.012 seconds). Owned Compose containers/network were removed. Production V31 uses GORM AddColumn/CreateIndex/table
operations and is unchanged by this test-only correction.

An independent production binary restart with an isolated PostgreSQL database and
unchanged root key reconciled the same request (201 before restart, 200 after,
same result ID). Both rows remained unchanged/pending/disabled; changed-secret
reuse returned 409. The embedded browser confirmed lineage, English/Chinese
preparation fields/state and cancellation without a write. English was restored,
and the owned temporary tab/process/database/network/config/journal were removed.
Independent-process and browser evidence remain separate from the complete
dual-database matrix; all required local checks passed before this delivery.

## Replacement preparation verification

| Check | Result |
| --- | --- |
| Focused UI | 128/128 cases, including 35 replacement cases, plus TypeScript/scoped ESLint |
| Mandatory full check | Passed after final fixture correction; zero errors, two existing Fast Refresh warnings |
| Full test | 619 Vitest cases in 51 files, Go race/unit, four Node checks, development lifecycle, production build/embedded assets |
| Focused dual databases | V31 prefix migration and replacement lifecycle under race detection, 127.168 seconds |
| Full dual databases | PostgreSQL/MySQL passed: Handler 404.327 seconds, Service 5.012 seconds; owned resources removed |
| Process restart | New preparation 201, exact retry from independent restarted binary 200/same ID, changed-secret reuse 409 |
| Embedded browser | English/Chinese original table/menu, historical lineage, preparation fields/state and cancellation; no browser write/upstream call; English restored and fixture removed |
| Review and docs | Independent focused review: no actionable finding; whitespace and 68 local Markdown references passed |

## Previous checked package: immutable per-attempt attribution

The persistence owner adds bounded optional CredentialID/SnapshotID to internal
attempt facts and exact RecordCall copies, with frozen GORM V32 and a historical
lookup index. The runtime owner copies actual dispatch IDs into failed attempts,
final native results and fsynced active interruption checkpoints. Existing journal
JSON carries the new fields without changing quota admission or the queue format.
Old database/journal fields remain empty/unknown; the parent logical call snapshot
and current catalog are never attribution fallbacks. Public call DTOs and CSV
remain unchanged. No native completion or retirement behavior is added yet.

Focused service/handler race tests passed, and full format/check/test passed
with 619 Vitest cases in 51 files, Go race/unit, four Node checks, development
lifecycle and production embedded assets. Focused real PostgreSQL/MySQL V32
migration and attribution lifecycle passed in 121.681 seconds. Mandatory check
was rerun after initial fixture corrections. The first complete matrix failed
in the older recorder fixture: its deliberately long placeholder Credential ID
now violates the stored ID bound. The fixture now uses a bounded historical ID
and verifies exact attribution through outage, interruption and immutable replay.
Independent review also found the direct-database compatibility dispatch missing
a pre-request durable checkpoint. It now checkpoints immediately before dispatch,
returns the existing 503 without any upstream request on checkpoint failure, and
clears the unstarted attempt. Focused race tests cover both that failure and
held-request journal recovery; the final full format/check/test and PostgreSQL/MySQL matrix passed (Handler
410.182 seconds, Service 5.252 seconds). Owned Compose resources were removed.
Independent review found no remaining actionable defect. The real-database
compatibility fixture also confirmed interrupted recovery and SQL replay.
Native completion proof remains separate: HTTP/call success may
include empty Chat payloads or native blocks; only parser-owned terminal evidence
can support a later planned retirement gate.

Replacement exact remote checks all passed for `16c0cff`: CI
[36970977822](https://github.com/miclle/routex/actions/runs/36970977822), Actionlint
[36970977665](https://github.com/miclle/routex/actions/runs/36970977665), and
GolangCI-Lint [36970977651](https://github.com/miclle/routex/actions/runs/36970977651).
The CI includes backend/frontend checks, dual databases, real-process session
lifecycle and build artifacts; it does not cover the subsequent V32 attribution
package. Inspect that package's exact remote checks separately.

## Previous checked package: parser-owned native completion evidence

The persistence owner added an internal `NativeCompletionEvidence` attempt field
with exact values unknown/completed/handoff/blocked/incomplete, frozen GORM V33,
legacy normalization and real-database migration/replay/privacy tests. The native
parser owner added an observation separate from token usage for Chat, Responses,
Messages and Gemini, preserving forwarding, native finality, quota and public DTOs.
Weak or unsupported shapes remain unknown. Completion is recorded only at native
terminal events or clean Gemini EOF; successful HTTP, usage, discovery and health
are never fallback proof. Chat must account for every requested bounded choice.

Initial focused tests passed, then mandatory check exposed two lint findings,
corrected without changing behavior. Native review found Responses status-only
promotion of refusal/tool handoff. Bounded terminal-output observation now keeps
refusal blocked, function/custom-tool handoff distinct, recognized normal text
completed, and weak/future/empty/nonterminal or contradictory output unknown.
Ordinary/SSE regressions and real outcome fixtures preserve forwarding/error/
usage semantics. Focused re-review closed the finding with no remaining issue.

Final focused race tests passed (Service 4.486 seconds, Handler 2.994 seconds),
staticcheck/lint passed, and full format/check/test passed with 619 Vitest cases
in 51 files, Go race/unit, four Node checks, development lifecycle and production
assets. Final-source focused PostgreSQL/MySQL migration/persistence/Responses
acceptance passed in 140.280 seconds; owned resources were removed. The earlier
seven-lifecycle run passed in 146.843 seconds on the prior parser copy and is
separate evidence. The final complete PostgreSQL/MySQL matrix passed (Handler 415.087 seconds,
Service 5.164 seconds), including native fixtures, replay and migration
constraints. Owned Compose resources were removed. Thirty local Markdown
references and whitespace checks passed. No scoped
retirement-readiness endpoint or predecessor retirement was part of that parser
package. Subsequent checked packages below add exact readiness and receipt-backed
retirement, separating historical commitment from current application and
preserving a predecessor that was subsequently enabled.

## Overall status and other partial work

`docs/IMPLEMENTATION.md` remains the authoritative inventory: **8 completed, 19
partially completed, 3 not started**. This counts completed capabilities, not effort.
A01 is fully accepted; A02–A12, A14–A15, and A17–A20 have partial evidence; A13 and
A16 are not started.

Delivered foundations include local identity/MFA, durable sessions and Keys,
governance and Team/Project management, four native protocols and bounded
same-protocol failover, immutable call/price/Provider-attempt facts, safe CSV and
price workbook workflows, currency administration, scoped usage, managed egress,
durable quotas, owned image/PDF attachments and Playground compositions, SMTP,
storage, site/announcements, authoritative System Status, operational/Provider
quality notifications, resource quota interfaces, capacity attestation, installation
calendar controls, and Credential filtering. Complete contracts and external
boundaries remain in the domain documents and implementation index.

F17 reminders were assessed, but are not delivered by this package. The earlier checked
notifications authorized platform operators only; the current quota package adds
separate scoped recipients and remains under validation. Personal/Project recipients,
thresholds, durable resource/dimension/window decisions, dedupe and policy/period
transitions need explicit contracts. A reservation rejection does not prove
settled quota exhaustion; unknown usage and conservative holds must not fabricate
exhaustion. Operator-only observations are not member/Project reminder delivery.

Team invocation context/defaults, templates, approvals, configurable quota stop
policy, enterprise identity, Vault, distributed enforcement, external price sync,
saved reports, AI analysis, backup/restore, and final production acceptance remain
open. Never infer Team attribution or debit from membership alone.

## Verification

| Check | Result and boundary |
| --- | --- |
| Metadata/deletion remote checks | [CI 36967885700](https://github.com/miclle/routex/actions/runs/36967885700), [Actionlint 36967885686](https://github.com/miclle/routex/actions/runs/36967885686), and [GolangCI-Lint 36967885754](https://github.com/miclle/routex/actions/runs/36967885754) passed for exact `1d3e430`, including dual databases, process restarts and embedded artifacts. They do not establish acceptance of uncommitted replacement preparation. |
| Previous focused frontend | 68 Credential metadata/deletion/filter/i18n cases passed; TypeScript and scoped ESLint passed. |
| Previous focused backend | Metadata/deletion validation and typed audit projection tests passed; independent contract review reported no remaining finding. |
| Metadata/deletion `go tool task check` | Passed after formatting, with no lint errors and two existing Fast Refresh warnings. |
| Metadata/deletion `go tool task test` | Passed: 584 Vitest cases, Go race/unit, four Node checks, development lifecycle and production build/embedded assets. |
| Metadata/deletion database matrix | Passed after physical-pool isolation: Handler 386.887 seconds, Service 5.144 seconds. Both databases, complete frozen migrations and lifecycle assertions ran; disposable containers/network were removed. |
| Metadata/deletion browser | Isolated PostgreSQL fixture confirmed the original Credentials table/action menu, English editing fields and cancelled draft, Chinese deletion preview/reason and cancellation with the row intact. No browser write or real upstream call was submitted. Final embedded assets were rebuilt, reloaded, and observed in English and Chinese; English was restored. The temporary tab, service, Compose database/network and fixture files were removed. |

The first full check exposed a staticcheck Boolean simplification in a new test;
the equivalent condition was fixed. The first full frontend run passed 583/584:
an existing Verify/Enable workflow still queried inline buttons after the approved
row menu was restored. It now exercises menu items and preserves verification-before-
enablement assertions. The final full check/test passed without relaxed timeouts or
assertions, using installed Node 22.23.2, matching the CI major (22).

The first database matrix failed with PostgreSQL `cached plan must not change
result type` on an initial `CallAttempt` read, before the first deletion. Earlier
migration fixtures drop/re-add columns and prepare the same `SELECT *`; rebuilding
the schema while retaining the physical pgx pool preserved an incompatible cached
result shape. Each lifecycle fixture now closes/reopens the pool after schema
reset, retaining production connection settings, migrations and all assertions.
An independent read-only diagnosis confirmed this boundary.

Prior exact checkpoints and transient test diagnostics remain recoverable from
`66874aa` and earlier handoff revisions; cancelled runs are not complete passes.
Final-source checks, real database acceptance, browser evidence and remote CI must
be reported separately.

## Working tree and transfer

- Replacement preparation was pushed as `16c0cff`, with its exact remote SHA read back and a clean RouteX tree before per-attempt attribution began. V32 attribution and the compatibility checkpoint were pushed as `c7f80d9` with exact remote main readback. The following checked package delivers V33 native completion observation, migration, tests and documents; identify its commit with the transport command above. Metadata/deletion was previously pushed as `1d3e430`.
- Dotfiles baseline was `0cc0644cf51873f61febd71f7a115c3420310755`, pushed to `origin/main`; stage only the RouteX coordination record for later updates.
- Preserve unrelated modified dotfiles `zsh/.zshrc`; never stage, overwrite or discard it.
- Fetch both repositories' `main` branches to transfer checked commits. Ignored configuration, databases, processes and temporary logs do not transfer; recreate them from `docs/DEVELOPMENT.md`.

## Next actions

1. V32 was pushed as `c7f80d9f30dcd5b7eed66374a6eb59fc3ef5913b`; exact remote main was read back and the tree was clean before V33. Inspect its CI [36973994747](https://github.com/miclle/routex/actions/runs/36973994747), passed with backend/frontend, dual databases, independent-process session lifecycle and build artifacts. Its Actionlint [36973994777](https://github.com/miclle/routex/actions/runs/36973994777) and GolangCI-Lint [36973994746](https://github.com/miclle/routex/actions/runs/36973994746) passed. Earlier replacement CI is separate evidence.
2. Commit/push the checked V33 native completion package and inspect its own exact remote CI; read back the main SHA. Its persistence owner delivered frozen V33 and exact storage/legacy normalization; its runtime owner delivered native ordinary/SSE observations without changing forwarding, status or usage contracts. Weak/unsupported shapes and legacy history stay unknown.
3. Native terminal, multiple Chat choices, clean Gemini EOF, cancellation/transport failure and journal replay are checked for V33. Preserve them in all later changes; mandatory check/test/matrix must pass before each subsequent code commit/push.
4. First tighten existing Personal/Project Key planned-rotation gates in a separate checked fix. They currently count generic successful logical calls, and their old controlled fixtures use empty Chat choices that V33 correctly marks unknown. Require a successful terminal completed attempt for the exact replacement Key/owner and creation boundary; preserve historical idempotent rotations and all existing authorization/scope/expiry checks. Fetch and compare exact marker/status values in Go after portable GORM filtering.
5. Follow with scoped current-configuration readback before a server-gated planned retirement operation. A separate durable retirement receipt must reconcile historical commit and current application after source disable changes publication; retries must not re-disable a re-enabled source. HTTP/call success can include empty Chat payloads or Gemini prompt blocks; discovery/global readiness/usage completeness cannot authorize retirement. Emergency disable stays independent.
6. Real-provider, SMTP, IdP/LDAP, Vault, S3, price-source and production acceptance require supplied environments and resources; do not search other accounts for credentials.

## Files to read first

| Path | Purpose |
| --- | --- |
| `AGENTS.md`, `.agents/rules/frontend.md` | Mandatory architecture, UI, localization, formatting and verification |
| `docs/IMPLEMENTATION.md`, `docs/CATALOG.md` | Full F/A inventory and Credential lifecycle contracts |
| `internal/routex/service/credential_replacements.go`, `credential_metadata.go`, `credential_delete.go` | Durable preparation receipts, persistence, audit, authorization and publication boundaries |
| `website/src/views/providers/credential-replacements.tsx`, `credential-metadata.tsx`, `credential-delete.tsx` | Transient secrets, reviewed drafts, conflict and retry behavior |
| `docs/QUOTAS.md`, `docs/QUOTA_LEDGER.md`, `docs/RESOURCE_LIMITS.md` | Scoped policy, unknown-history and accounting contracts |
| `docs/NOTIFICATIONS.md` | Existing source/recipient/delivery boundaries before quota reminders |
| `~/dotfiles/projects/routex/implementation-plan.md` | Cross-task scope, dependencies, acceptance and evidence |

## Environment

Go 1.27.1, module-managed tools, Node/npm, Docker Compose, PostgreSQL 18 and MySQL
8.4 are required. Secrets stay in ignored configuration or process environment.
The shell resolves Node 24.21.0; checks for this package use installed Node 22.23.2.
Docker was started for isolated validation. The integration script owns and
removes its fresh containers/network without touching the developer database.

Development normally uses backend `19000`, Vite `15173`, and Compose PostgreSQL
`15433`. No development service/database was started or verified in this package;
check `/health` before relying on an older process. Never kill unrelated processes
to free ports. The isolated browser fixture used backend `19001` and its own temporary Compose
PostgreSQL database. Its tab, service, database/network and temporary fixture files
were removed after preview/cancellation checks; no browser write or real upstream
request was submitted.


## Previous checked package: Key retirement proof hardening

Personal and Project gates now require an exact immutable successful replacement
call after Key creation and its successful last attempt with native completed
evidence. A portable bounded GORM projection performs exact Go comparisons of
owner/Key/request/status/marker/timing/ordinal and excludes later attempts. HTTP
200 empty/refused/tool/limited output, unknown/failed/canceled/missing facts,
misattribution and raw case-folded candidates cannot qualify. Historical completed
audit replay, current authority/scope/expiry, emergency revocation and runtime
invalidation remain intact. No migration or public API change.

Service and real-HTTP fixtures are frozen. Independent read-only review found no
actionable issue. English/Chinese existing-dialog guidance and live-switch tests
passed 44/44. Full format/check/test passed (619 Vitest cases in 51 files, Go race/
unit, four Node checks, dev lifecycle and production assets). Focused PostgreSQL/
MySQL lifecycle race acceptance passed in 146.174 seconds and removed all owned
Compose resources. The final complete matrix passed (Handler 417.693 seconds, Service 5.211
seconds); owned Compose resources were removed. Whitespace and local Markdown
references passed. This delivery was pushed as `7af4c49`, with exact remote main readback.
Its exact CI, Actionlint and GolangCI-Lint all succeeded. Synchronize
the coordination plan separately, then continue F11 scoped readiness and durable
retirement receipt.

The preceding native-proof commit's exact remote checks all succeeded:
[CI](https://github.com/miclle/routex/actions/runs/36977642453),
[Actionlint](https://github.com/miclle/routex/actions/runs/36977642519), and
[GolangCI-Lint](https://github.com/miclle/routex/actions/runs/36977642469).
The separately pushed coordination baseline is
`53a5ba9e7c6b1f701f0be61b6cb95afe4ee9a72b`. These earlier remote results do not accept the
current Key package. The inventory remains 8 complete, 19 partial and
3 not started; only A01 is fully accepted. The overall objective remains active.


## Latest checked package: F11 read-only replacement readiness

The prior checked Key fix is pushed at `7af4c49`, with exact remote main readback
and a clean tree before this package. Its exact [CI](https://github.com/miclle/routex/actions/runs/36980216647),
[Actionlint](https://github.com/miclle/routex/actions/runs/36980216548) and
[GolangCI-Lint](https://github.com/miclle/routex/actions/runs/36980216588) all
succeeded, including independent process/session restart and build jobs. The coordination baseline is separately pushed at `72b7b38`.

Implement advisory GET retirement-readiness with exact source/replacement lineage,
current verified/explicitly enabled state, bounded positive native-route coverage,
local source-digest/publication coherence, current database projection matching and
exact successful terminal completed-attempt proof under the current cfg. Capture
and recapture must not wait for runtime.mu with a borrowed DB connection or run
DB work under that mutex. Preserve independent ordinary auth/route publication.
No write endpoint, receipt, migration, implicit disable or supplier revocation
in this phase.

Backend owns service/handler/GET registration and controlled lifecycle fixture;
runtime owns private source-digest and bounded scope/evidence helpers; coordinating
task owns the existing replacement row menu/Base UI review, DTO validation, paired
translations and docs. Independent frozen review found no actionable defect. Final focused UI/i18n
passed 28 cases and full format/check/test passed (639 Vitest/52 files, Go race/
unit, four Node checks, dev lifecycle and production assets). Focused actual
PostgreSQL/MySQL catalog/replacement/readiness race acceptance passed in 160.016
seconds; final complete race matrix passed (Handler 429.433 seconds, Service
5.336 seconds). All owned Compose resources were removed; whitespace and local
Markdown references passed. Commit/push only this checked scope, read back main
and its own CI, then synchronize the coordination plan separately. Receipt-backed
retirement is the next write package.
The overall objective remains active and the capability count stays 8/19/3.


## Latest checked package: receipt-backed planned retirement

POST retire must bind a stable UUIDv4 intent, reviewed aggregate If-Match, exact
replacement/AttemptID/pre-disable cfg and required reason. Revalidate that same
immutable attempt; a newer call must never silently replace reviewed proof.
Fresh source disable, frozen V34 durable non-FK receipt and one safe typed audit
commit atomically. Authorize before exact receipt lookup; replay compares the
original identity/hash and never re-disables a re-enabled/deleted source.

HTTP 200 may acknowledge known `committed:true` independently of current
`runtime_applied`; failed publication, re-enabled/deleted source or invalid
successor must remain incomplete. Preserve original intent through uncertain/
rejected retries. No fresh inference is required merely because source disable
or process restart changes cfg after the saved commit. Current successor scope/
publication and source exclusion still require confirmation. The runtime owner
implements a minimal publication pin acquired before DB borrowing, avoiding
runtime/DB lock inversion and commit races. Phase7 source passed local acceptance:
backend owns the frozen V34 receipt/transaction/POST and controlled dual-database
fixtures; runtime owns publication pinning, exact reviewed proof and current
application reconciliation; root owns the existing dialog controls, safe audit
projection, paired copy and documentation. Source is checked and pushed at `4ee1a0236330de57b8b64c4858ab97e328744be0`; exact remote main SHA was verified. Continue under the resumed full objective.

- Readiness is pushed at `a9d8dc3e89bf15e22a898095bee8e5006ec44d48`, verified
  against remote main. Its Actionlint 36984325353 and GolangCI-Lint 36984325458
  and CI 36984325423 all succeeded, including dual-database/restart and build jobs. The external
  coordination checkpoint is pushed at `5f02c77`.
- Final focused frontend passed 39 cases. Full format/check/test passed with
  650 Vitest cases/53 files, Go race/unit, four Node checks, development lifecycle
  and embedded production assets. Final pre-commit check passed.
- Focused real PostgreSQL/MySQL acceptance passed in 159.203 seconds; complete
  race matrix passed (Handler 449.057 seconds, Service 5.332 seconds). Independent
  frozen review found no remaining actionable defect. An earlier integration
  fixture callback-removal race was fixed by awaiting its concurrent publisher
  before cleanup; the final matrix passed. Owned resources were removed.
- Disposable production PostgreSQL plus a controlled native upstream passed the
  built-in-browser retirement reason, explicit exact proof confirmation, receipt
  and local application flow. Predecessor disabled/successor enabled, bilingual
  switching and console checks passed. First durable admission starts accounting
  and publishes a new configuration; proof was obtained after that publication.
  The owned browser tab, process and Compose resources were removed. No supplier
  or fleet proof is claimed.

## Current partial capability: F19 member model directory

First fix the existing available count and fabricated Chat example for models
without eligible protocols. Preserve the direct personal `/api/v1/models` and
Personal Key ceiling. Then add a separate actor-scoped `/api/v1/model-catalog`
with reauthorized detail, deduplicated personal/active-Team visibility and exact
non-secret sources. Team visibility does not grant Personal Key access or Team
invocation; explicit Team execution and non-Project requests remain separate.
Follow the existing card/table/filter/drawer composition and paired translations.
Cover literal/intersected filters, revocation, scoped sources, unavailable routes,
capability intersections and bounded overflow with UI and real dual-database
acceptance. The inventory stays 8 complete/19 partial/3 not started; only A01 is
fully accepted. Continue the full objective after phased delivery.


### F19 first bounded correction

The existing member catalogue counts an active model as available only when it
has a supported eligible native protocol. An explicit empty protocol list, unknown
protocol, disabled or archived model must not fabricate a Chat endpoint or cURL
example. The existing drawer displays localized unavailable guidance, disables
copy and withholds a misleading Key-management suggestion. Supported native
examples and the Gemini path guard remain. Dedicated frontend tests passed 15
cases; full format/check/test passed (665 Vitest cases in 54 files, Go race/unit,
four Node checks, development lifecycle and embedded production assets). No
expanded catalogue endpoint or Team invocation is delivered by this correction.


Credential retirement `4ee1a02` exact remote CI completed successfully:
[CI](https://github.com/miclle/routex/actions/runs/36990759345),
[Actionlint](https://github.com/miclle/routex/actions/runs/36990759328) and
[GolangCI-Lint](https://github.com/miclle/routex/actions/runs/36990759313), including
dual databases, independent process authentication restart and build artifacts.
Its coordination checkpoint is separately pushed at `87ffd86`. Those remote
checks remain evidence for that package, separate from this UI correction.


### Checked F19 source/detail package

The native-availability correction is pushed at `ccd5a081f34fecea9e911be914d67783fd0399c4`,
with exact remote main readback and a clean tree before this package. Its own
CI 36992315572, Actionlint 36992315568 and GolangCI-Lint 36992315770 all succeeded. The external coordination checkpoint
is pushed at `f44c253`. Two independent owners now implement the actor-scoped
`model-catalog` list/detail, exact personal/active-Team sources, bounded complete
reads and actual native protocol/capability metadata, plus the approved filters,
source overflow and independently reauthorized drawer. The coordinating task
owns the shared identity integration registration and existing catalogue UI
fixture adaptation, documentation and complete acceptance. No new migration or
Personal Key authority is added. The directory source was frozen, checked, committed and pushed. Complete check/test passed
(677 Vitest cases in 54 files, Go race/unit, Node/development/production checks);
focused dual-database source acceptance passed (Handler 160.920 seconds). The
complete PostgreSQL/MySQL race matrix passed (Handler 488.717 seconds, Service
5.727 seconds); its owned containers/network were cleaned. Final check passed.
Independent backend review found no remaining actionable production defect.
All 69 checked local Markdown references and whitespace checks passed. The disposable production browser
passed source overflow, ready Team-only denial, personal native examples, revoked
detail freshness, table/image filtering and bilingual switching without console
errors. Owned browser/production process/Compose resources were cleaned. Full F19
and the overall objective remain active.

The next partial-capability assessment is F23 aggregate quota exhaustion inbox
notifications. Existing operational alerts cannot safely fan Personal/Project
quota facts to all system operators. Scope needs durable observed settled-use
facts, exact currency/coverage, deduplication and current-owner/Project-manager
authorization. Threshold warnings, Team quotas and external SMTP acceptance are
not inferred. F23 implementation is now active under those three owners. Frozen V35 uses
separate quota observation/inbox tables; existing operational history/FKs, alert
fanout, SMTP and settings permissions remain. The existing inbox endpoints merge
currently authorized sources; ordinary enabled members may read their own quota
notices without gaining operational authority. Root owns worker startup/shutdown,
shared migration/lifecycle registrations, documentation, full checks and delivery.
Focused actual PostgreSQL/MySQL acceptance has passed; final local evidence is recorded below.


### F19 source-directory transport and CI

Exact RouteX main `13c1b0880a7e0f5fef2558016561015cf683ce31` was pushed and
read back with a clean source tree. External coordination checkpoint
`ae4d36ac5ec6ecec06018abb3bae543e5242d3c8` was pushed independently, preserving
unrelated dotfiles changes. Own Actionlint 36995869572 and GolangCI-Lint
36995869624 and CI 36995869421 all succeeded, including dual databases, independent-process
authentication restart and artifact builds.
Keep these remote results separate from the next quota-notification source.


## Current package: F23 monthly settled-exhaustion inbox

Frozen GORM V35 adds separate observation and recipient tables, leaving released
steps and operational notification foreign keys unchanged. The bounded worker
requires a fresh exact applied policy and covered current-month settled usage.
It freezes scope, revision, window, denomination and amounts, creates immutable
recipient projections atomically, and preserves read state through replay. Holds
and unknown amounts cannot establish exhaustion. No crossing, warning threshold,
Team/Key quota source, historical backfill, quota email or additional admission
rule is claimed.

Enabled recipients can read their own empty inbox. Exact Personal ownership and
recorded/current active Project management govern quota history; operational rows
still require system.read. Exact byte comparisons before pagination, counts and
read mutations reject MySQL collation aliases. Settings retain independent
permissions. The existing bilingual bell validates response envelopes and hides
stale rows/counts on refresh, actor changes or malformed responses.

Final format and mandatory check passed, with zero errors and two existing Fast
Refresh warnings. Full test passed: 724 Vitest cases in 56 files, Go race/unit,
four Node checks, development lifecycle and production embedded assets. Focused
actual PostgreSQL/MySQL migration and lifecycle race acceptance passed in
173.349 seconds. The complete PostgreSQL/MySQL race matrix passed: Handler 499.925 seconds,
Service 5.851 seconds. Its isolated containers and network were removed.
Local whitespace and all 67 checked Markdown references passed. This package
is ready for its phased main-branch commit; exact transport and remote CI are
verified separately after publication.

The disposable production binary and isolated PostgreSQL browser fixture
published real Personal/Project notices after controlled native Chat settlement
(5 settled tokens, limit 5). English and Chinese rendering preserved the exact
policy/month/time-zone snapshot. Revoking Project management through the real
API caused an old notification read to fail and refreshed the menu to only the
Personal notice, with one unread record. English was restored. The owned tab,
binary process, upstream process, database container and network were removed.
Browser evidence is separate from the complete database matrix and external
provider or email acceptance.

F17/F23 and full RouteX acceptance remain open. The next assessed partial package
is Project monthly quota requests and independent approval, using the existing
resource configuration and request history. Team/request-rate approvals and
broader sources remain separate unfinished scope.
