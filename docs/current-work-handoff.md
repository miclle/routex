# Current Work Handoff

- **Status:** implementation active; F06 completed and pushed; F22 scoped Team reports pushed; F07 canonical authority accepted for phased main delivery; F17 defaults active in an isolated worktree
- **Updated:** 2026-10-03
- **Repository / branch:** RouteX / `main`
- **Previous checked source baseline:** `598ffd17e00f8ef651b8a8a4f7bbfbae864f8c48`, pushed and read back from `origin/main`
- **Current owner:** coordinator owns phased F07 delivery and F17 integration; three owners implement F17 backend/API, frozen migration/acceptance and frontend
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

`docs/IMPLEMENTATION.md` remains the authoritative inventory: **10 completed, 17
partially completed, 3 not started**. This counts completed capabilities, not effort.
A01 and A13 have complete controlled acceptance. A02–A12, A14–A15 and A17–A20
have partial evidence; A16 is not started.

Delivered foundations include local identity/MFA, durable sessions and Keys,
governance and Team/Project management, four native protocols and bounded
same-protocol failover, immutable call/price/Provider-attempt facts, safe CSV and
price workbook workflows, currency administration, scoped usage, managed egress,
durable quotas, owned image/PDF attachments and Playground compositions, SMTP,
storage, site/announcements, authoritative System Status, operational/Provider
quality notifications, resource quota interfaces, capacity attestation, installation
calendar controls, and Credential filtering. Complete contracts and external
boundaries remain in the domain documents and implementation index.

Personal/Project settled monthly-exhaustion inboxes are delivered with scoped
recipients and exact policy/window facts. Team reminders, broader thresholds,
notification delivery and complete quota stop-policy scope remain separate. A
reservation rejection does not prove settled exhaustion; unknown usage and holds
must not fabricate exhaustion.

Team defaults, templates, further Team protocols/attachments, configurable quota
stop policy, enterprise identity, Vault, distributed enforcement, external price
sync, saved reports, AI analysis, backup/restore and final production acceptance
remain open. Team roles are completed; current Project authority is in acceptance. Never infer Team
attribution or debit from membership alone.

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
Local whitespace and all 67 checked Markdown references passed. This package was committed and pushed at exact
`ce568febf9d85170f60f6e9ad4e2a608d565b872`; upstream read-back matched and
the source tree was clean before the next package began. Actionlint
37000084170 and GolangCI-Lint 37000084191 succeeded. Full CI 37000084055 succeeded, including frontend/backend checks, complete
database/process-restart acceptance and Build Artifacts. All three remote
workflows are green for the exact notification commit; remote results remain
separate from local acceptance.

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


## Latest checked package: F18 Project monthly quota requests

The existing Project model-request lifecycle and quota enforcement are the
prerequisites. Three owners completed a bounded QUOTA request kind, frozen
additive V36 history fields, independent reviewer authority and the existing
Resource configuration/request history composition. V36 schema and final lifecycle
acceptance pass on both databases. This package is ready for phased commit and push.

Monthly targets are finite: omitted fields remain unchanged, zero is a real cap,
explicit null is rejected, and money stays exact decimal text. A manager-only
submission context supplies a composite validator for the policy and platform
currency generation. Request details supply another fresh validator bound to
immutable request intent and current policy/currency. Approvals use independent
projects.limits.write authority and disallow self-approval. Each reviewer sees
only its request kind unless independently authorized as manager or global reader.

Pending requests leave enforcement unchanged. Approval must patch only requested
fields, retain immutable normalized policy/decision evidence, serialize with
admission and governance, and reconcile actual publication. A replay after a
newer policy edit must preserve that newer policy; historical approval and current
application/supersession are separate facts. Team and rate-limit approvals remain
open. V36 and the quota lifecycle are registered in the shared harness. The final
focused PostgreSQL/MySQL race run passed in 184.141 seconds, covering all V36
migration prefixes, old model requests, new quota requests and monthly inbox
permissions. The owned resources were removed. Final check and test passed:
759 frontend cases in 57 files, Go race/unit, Node checks, development lifecycle
and embedded production assets; Actionlint also passed. The complete isolated
PostgreSQL/MySQL race matrix passed (Handler 504.548 seconds, Service 5.267
seconds), and all owned resources were removed. The 71-case focused UI suite
includes malformed HTTP 200 quota receipts and valid terminal creation replays.

Failures found and fixed before this acceptance include exact GORM ETag column
mapping, comparison against persisted historical MySQL timestamps, uniform legacy
withdrawal denial, inherited query OR grouping, and monetary fixture funds that
include conservative reservation margins. Production price arithmetic stays
unchanged. Independent UI review fixed quota-only workspace unmounts, current
same-actor CSRF retries and terminal stale-review forms.

Owned production/PostgreSQL/browser proof confirms pending Tokens 5 remains 5,
approval to 10, native settlement to 10 with next-call rejection, later direct
policy 15, historical exact approval replay and final-source service restart
preserving 15. English/Chinese detail separates baseline 5, saved approval 10 and
current 15. A quota-only reviewer opened only the authorized Resource
configuration/request workspace. The owned tab, binary, controlled upstream,
database container and network have been removed; no developer service was touched.
A fresh final build repeated the complete browser/native/restart workflow after
receipt validation changed. A13 has partial Project approval evidence; its Team
overflow boundary remains unaccepted. Capability totals remain 8 complete, 19
partial and 3 not started; only A01 is fully accepted.

At this historical monthly checkpoint, RPM/TPM/concurrency requests were assessed
and not yet started. The following checked package records their implementation
and acceptance; this paragraph is not a current next action.

External coordination checkpoint `ff383331a5428cf80713289b58bd7d14aa3205b8`
was pushed and read back independently. Only the RouteX plan was staged; unrelated
zsh configuration changes were preserved.


## Published monthly quota checkpoint and checked rate package

RouteX `7c0d41053f14c546eab9106a27beaad8327d41c6` was committed and pushed
to main with exact remote readback and a clean tree before the next package.
[CI](https://github.com/miclle/routex/actions/runs/37007482201),
[Actionlint](https://github.com/miclle/routex/actions/runs/37007482276) and
[GolangCI-Lint](https://github.com/miclle/routex/actions/runs/37007482186) all
succeeded, including database tests, independent-process authentication restart
and build artifacts. Local checks and acceptance above are final for that commit.
External coordination `f74ecf11f8a33d4dc6c8b32f3627be9c31bfea40` records the
delivery and next package; unrelated zsh changes remain preserved.

Three owners completed finite Project RATE_LIMIT requests for RPM, TPM and
concurrency. Schema ownership covered frozen V37 and entity kinds; backend owns
strict submission, bounded shared quota/rate approval and native execution tests;
frontend owns the existing adjustment form/history and bilingual partial results.
The coordinator completed shared integration registration, docs, full checks and
owned production/browser acceptance. The current package is ready for phased
commit/push; identify its eventual SHA through this document's Git history.

Combined monthly/rate submission creates independent records and immutable retry
intents. Blank preserves a field, zero caps it, explicit null is rejected. Current
manager submission and nonself projects.limits.write review remain separate.
Historical MODEL/QUOTA digests stay byte-identical. Full-policy patches preserve
settled usage, monthly/rolling/money/IP restrictions and Key ceilings. Original
approval replay never restores a superseded policy. Runtime application requires
current publication evidence. Independent UI review fixed actor/unmount leakage
and fail-closed handling of contradictory application flags.

Final verification: go tool task check and go tool task test passed, including
800 frontend cases in 58 files, Go race/unit, Node, development lifecycle and
production assets. Focused request UI: 112 cases. Actionlint passed. Focused real
PostgreSQL/MySQL race matrix: 210.144 seconds; full matrix: Handler 529.867 seconds,
Service 5.063 seconds. Frozen V37 preserves released migrations and passes empty,
upgrade, repeat, concurrent and interrupted-prefix constraint tests.

Owned final production/PostgreSQL/browser acceptance proved baseline monthly 100
and RPM/TPM/concurrency 1/5/1, independent pending requests for 200 and 2/10/2,
unchanged rates after monthly-only approval, actual native rate rejection after
separate rate approval, later policy 250 and 3/15/3, and exact historical approval
replay preserving the latter before and after restart. English/Chinese details
show baseline, requested, saved and current values with supersession explicit.
All owned tabs, processes, upstream and Compose resources were removed; the
existing developer service was untouched. Remote CI for the new SHA must be read
back after push; the green runs above refer only to the monthly commit.

## Next partial package: Team session invocation and call attribution

Read-only assessment found Team grants are visible but cannot authorize current
Key-only native invocation. Team policy approval would therefore lack enforcement.
First implement explicit Team-scoped Session text-only native Chat, short-leased
runtime Session/member/grant authorization, immediate local revocation, immutable
Team/actor call attribution and separate Team history. Keep Personal/Project Keys
and their ledgers separate; do not infer authority from a union model catalogue.
No Team implementation or Team quota acceptance is claimed at this checkpoint.

First action after committing/pushing the checked rate package: inspect runtime
identity, session revocation, call journaling and scoped history contracts; assign
three non-overlapping owners before editing. Use a new frozen additive migration
for Team attribution and current exact Team/member authorization for every attempt.
Preserve the approved Playground source-selector position with explicit named Team
Sessions, separate CSRF/cookie transport, and bilingual text-only limitations.
Reject attachments/comparison/code export until supported rather than falling back
to Personal credentials. Validate both databases, revocation/lease/replay boundaries
and owned native/browser workflows before the next main commit and push.

## Published rate checkpoint and active Team foundation

RouteX `85730293c22e50a311c463ab62dbf4d4b6902b3a` is pushed and the exact
origin/main SHA was read back before Team edits. External coordination
`699b68a937fb1bbd344e7767a62f3226d883685d` was independently pushed/read back;
only the plan was staged and unrelated zsh work remains preserved. Actionlint
37011327393 and GolangCI-Lint 37011327464 succeeded. CI 37011327474 also succeeded, including its real database/restart
acceptance and build artifacts. These runs apply to the rate commit only.

Team foundation implementation is now active. Ownership: runtime Session/member
publication and local revocation; typed native invocation and separate journal
accounts; approved bilingual Playground source selection and own-actor Team calls.
The coordinator owns frozen V38 call attribution, scoped history, Personal
history/usage/export exclusions, documentation and final acceptance. Source is
uncommitted until focused/full checks, real dual-database and browser proof pass.
This phase does not implement finite Team policies or approvals. Do not extend
read authority from directory administration or treat Team owners as readers of
other actors' calls. Pair journal identity remains stable through membership
replacement; current authority and historical membership attribution are distinct.


## Checked Team Session foundation and next finite-policy package

Current source implements the contract in TEAM_INFERENCE.md. All implementation
owners are frozen; the coordinator completed V38, history/report isolation,
canonical member-ID validation, shared query helper, final Chat partial-usage
correction and documentation. Final check/test passed with 843 Vitest cases in
61 files. Actual focused dual-database race acceptance passed in 261.152 seconds;
full matrix passed (Handler 610.579 seconds, Service 5.598 seconds). Real-process
Session/Key lifecycle and Actionlint passed. Owned production/browser proof covers
English/Chinese native usage, current-member own history, removal denial, rejoin
and durable restart. Owned runtime/database/browser resources were removed.

Corrections during acceptance: fixed test-only GORM index removal/cached column
projection and valid usage-only SSE fixtures; fixed mutable callback registration
racing a background recorder using fixed registration plus atomic failure switches;
fixed stale frontend caches and canonical actor IDs. Production native finality and
all released migrations remain intact. Missing Chat total usage is unknown while
reported input/output remains visible. None of these results claims finite Team
quota enforcement or real-provider acceptance.

Next action: inspect resource_limit.go, resource_limits.go, resource_limits_runtime.go,
quota_runtime.go and the existing stable Team/User account helper. Add a new frozen
GORM migration for bounded Team policy scope IDs and independent permissions, then
extend aggregate/member policy resolution, authoritative usage and native finite
admission. Reuse Resource limits within the existing Team settings/member workflow.
Do not use membership IDs as durable account identity, reset use on rejoin, grant
owners implicit platform mutation, or build quota approvals before real finite
Team enforcement. Keep Team overflow/assigned/escalated approvals as a subsequent
bounded package. The full objective remains active; capability totals are unchanged.

Delivery transport: this document travels with its checked main commit. Resolve
that delivery using git log -1 -- docs/current-work-handoff.md and verify upstream
before resuming. Remote CI for this new commit must be checked independently;
successful runs listed above apply to the earlier Project rate commit.


## Published Team Session checkpoint and active finite-policy ownership

Team Session source `24fec89ec34adede7d99cb6087af0b748f596d4f` was committed,
pushed and read back from origin/main with a clean tree before finite-policy work.
Coordination plan `e47ea4ecdb027a450fee0c93368d4d706597f9ab` was independently
pushed/read back with only the plan staged; unrelated zsh changes were preserved.
Exact Actionlint 37016949801 and GolangCI-Lint 37016949517 succeeded. Exact CI
37016950128 also succeeded, including dual-database/restart acceptance and built
artifacts. These three results apply to the Team Session commit only.

The new bounded Team policy package is active; see TEAM_LIMITS.md. Parallel owners:
policy API/permissions/composite review/minimal Team projection; native aggregate
and stable-pair finite admission; approved Team Limits/member-adjustment UI. The
coordinator owns frozen V39 scope widening and permission seeds, shared HTTP DTO,
routes and migration harness, docs and final acceptance. Current source remains
uncommitted until all required checks and actual dual-database proof pass. Do not
claim finite enforcement or Team approval from this active checkpoint.


## Checked finite Team policy checkpoint

All implementation owners are frozen. Final check/test passed (876 Vitest cases in
63 files; Go race/unit, Node, development lifecycle and production embedding).
Actionlint and both real-process identity/Key lifecycle drivers passed. The full
actual PostgreSQL/MySQL race matrix passed: Handler 623.428 seconds and Service
6.559 seconds. It includes frozen V39 empty/upgrade/repeat/concurrent/interrupted
migration and real Team policy/native finite fixtures. Owned production/browser
proof confirms cap rejection before dispatch, exact current application, isolated
Personal use, bilingual edits/conflict review and stable policy/use through
membership rejoin and process restart. All owned resources were cleaned up.

Corrections: explicit primary-key NOT NULL during GORM width alteration; mapped
PreviousETag update; fresh exact-money patch allocation; full monetary denomination
application proof; deterministic old-query clearing in the late-response test.
Released migrations and Personal/Project/Key policy contracts are unchanged.

This checked delivery travels with its main commit; resolve the exact SHA with
`git log -1 -- docs/TEAM_LIMITS.md` and compare to origin/main. Remote workflows
for that SHA must be verified after push; earlier green runs apply only to earlier
commits. External coordination must stage only its implementation plan, preserving
unrelated zsh changes.

Next action: implement monthly Team member quota requests after this checkpoint is
committed/pushed. Read the assessed contract and TEAM_LIMITS.md; assign separate
persistence/migration, service/API and UI owners before editing. Support one Team
and monthly Token or money dimension; current non-applicant owners review first,
then overflow escalates to dimension-authorized platform review. Without another
eligible owner, start at platform review. Final overflow approval changes aggregate
and member caps atomically; no self-approval or implicit owner write authority.
Use stable creation/step decision receipts, pending-slot uniqueness, immutable
submission versus fresh current review, exact amounts and current application versus
superseded history. Preserve approved workspace/table/dialog/drawer composition,
read-only global records and English-default localization. Full checks, actual
both-database/native/restart/browser acceptance precede another main commit/push.
F06 also retains Team-role assignment/permission union; all full acceptance remains
open and the user has not requested a pause.


## Published finite Team policy checkpoint and resumed request ownership

RouteX `e25d8a9023c555a15d9842bfee50c2c2ab32076c` was pushed and exact
origin/main was read back with a clean source tree. Exact CI 37022492454,
Actionlint 37022492517 and GolangCI-Lint 37022492550 all succeeded. External
coordination `a5e9c0239583a5fb8c94e136c68e114945e2027b` was separately
pushed/read back, staging only the plan and preserving unrelated zsh work.

The user resumed the full goal on 2026-10-03 and again prioritized partial work.
No Team request implementation landed before the interruption. Three owners have
resumed: dedicated request/step/pending-slot entities and frozen V40 migration;
monthly owner/platform workflow and strict scoped API; approved bilingual workspace,
creation dialog, timeline/detail drawer and independently authorized read-only
platform records. The coordinator owns shared route registration, reset/migration
harness, independent global-read permission, safe audit projection, docs and actual
acceptance. New source remains uncommitted until required checks pass.

Keep creation UUID and per-step decision UUID/review intent immutable. An owner
approval may record an approved step while the request remains pending at the
platform node; it is not final policy application. Membership loss/replacement
cancels pending applications and rejoin cannot revive them. Preserve historical
approved policies while reporting current pending/applied/superseded publication
separately. Full both-database, native, restart and browser proof precede delivery.


## Monthly Team request verification checkpoint, 2026-10-03

The V40 schema, owner-first monthly Token/money workflow, platform escalation,
read-only global records and bilingual interfaces are frozen. The final coherence
review passed after rejecting mismatched status/step and noncontiguous histories.
Immutable existing receipts reconcile before fresh-decision checks. Server-owned
effect previews distinguish unchanged owner escalation from atomic final quota
writes; read-only terminal records never expose an approval workspace shortcut.

Mandatory check and full test passed after compatibility redirects and import
formatting: 922 Vitest cases in 65 files, Go race/unit, four Node checks, developer
process lifecycle and production asset embedding. Full actual PostgreSQL/MySQL
race acceptance passed (Handler 704.846 seconds; Service 6.084 seconds) and both
real-database process lifecycle suites passed. V40 migration prefixes, preserved
history, constraints, scoped authority, exact money, owner transitions, cancellation
through membership/account/Team/offboarding changes, stale review, malformed stage
and immutable receipt replay are covered.

The actual disposable production browser used English-default creation and live
Chinese switching. Member 5/Team 10 target 15 entered the owner first; owner
confirmation retained 5/10 and entered the platform stage. Final confirmation
rendered 5→15 and 10→15, and current application was confirmed. Three controlled
five-Token calls succeeded and the fourth stopped before dispatch; an independent
restart preserved stage receipts, approved caps and used 15. Terminal global records
were read-only without a workspace link. Owned tab, services, configuration, journal
and Compose resources were removed; the developer service was untouched.

The full matrix retains the existing concurrent-owner approve/approve and exact
final-audit rollback cases. A final test-only approve/reject competition appended after native/restart proof
passed on both actual databases (5.92/5.94 seconds; full focused runner 208.460
seconds), with exactly one winning receipt/audit and unchanged policies/use. No
business behavior changed after the full matrix. Final required check/test passed
with 922 Vitest cases. F18/A13 are complete in their defined controlled scope;
capability totals are 9 complete, 18 partial and 3 unstarted. A01/A13 are accepted;
full product/release acceptance remains open.

The next assessed partial package is F06 Team-assigned roles and scoped permission
union: frozen V41 relationship; exact `teams.write`/`teams.models.write` action
allowlist; target-specific member/model/role candidates; protected actual-admin
assignment; reviewed Role definitions; assigned-role deletion guard; existing
detail Roles table/dialog/picker/Save and Team-local gates. Direct/global permissions,
Team quota-administrator dimensions, Project/Key authority and native grants remain
independent. Source implementation starts only after this request phase is committed
and pushed. The overall objective remains active with no new pause.


## Published monthly requests and active F06 Team roles

RouteX `bfad9f42893c3cba7bf509df9a969574c9a76be0` was pushed to `origin/main` and
its exact remote SHA read back. External coordination plan commit `c5483bc` was
pushed with unrelated shell changes preserved. Exact Actionlint 37122415984 and
GolangCI-Lint 37122416013 and CI 37122416012 all succeeded for that exact
commit. F18/A13 scope is accepted locally; full objective remains active.

F06 ownership is now explicit: persistence owns frozen V41/live TeamRole and
migration tests; service/API owns target-scoped role union, candidate/assignment
endpoints, resource hooks and deletion guard; frontend owns the existing Roles
tab/table/dialog/picker and local actor/Team gates. The coordinator owns shared
route registration, reset/ledger helpers, safe audit, rules/docs and actual
acceptance. Current role changes are uncommitted and unaccepted until frozen
source, both real databases, scoped runtime/browser proof and mandatory checks pass.

Role assignment uses strict current If-Match and saved-state reconciliation,
without a new general workflow or receipt schema. Team lifecycle remains direct
platform authority; scoped roles contribute only metadata/membership and model
actions. Preserve current role definitions through explicit conflict review and
keep global Session permissions, quotas, native grants, Projects and Keys independent.

## Active F06 verification checkpoint

F18 exact CI 37122416012, Actionlint 37122415984 and GolangCI-Lint 37122416013
all succeeded. F06 V41 migration acceptance passed on actual PostgreSQL/MySQL
under the race detector (Handler 202.479 seconds), including partial-schema repair
and unchanged direct role grants. The first early run found an owned migration
fixture user leaking into the shared setup baseline; bounded function-return
cleanup repaired test isolation. No production migration behavior was weakened.

Both real-database independent authentication/gateway process lifecycles passed.
The disposable production API proved target-only metadata/model operations,
unchanged global permissions, immediate definition revocation, stale review409,
assignment removal and native grant independence, then independent restart. The
full role workflow matrix and final interface acceptance are still pending.

Actual browser checks found an empty candidate query rejected by strict parsing
and a saved assignment leaving old-generation candidate data. The frontend owner
is repairing transport omission and current-generation refresh, plus an explicit
current-state review/discard path for unresolved local drafts. None of these checks
constitutes a historical operation receipt or runtime publication proof. Final
source/check/test and both-database workflow acceptance precede another main commit.

The final role-save review additionally found a PUT/GET validator mismatch from
transient versus persisted timestamp precision. Changed writes now reload the
persisted Team in the same locked transaction, and Team metadata uses the same
portable monotonic millisecond generation. Real-database assertions bind PUT
body/header to subsequent role/candidate validators and interleaved metadata ABA.
Earlier full runners were explicitly stopped for coherent-source repair and are
not accepted evidence. The complete release matrix is running against frozen
backend source. Unknown-result discard now requires a new same-actor/Team read
before opening its confirmation, preserving original intent and outcome status.


## Final F06 browser and process proof

The final isolated production binary and built interface passed actual scoped
metadata/model/candidate operations, cross-Team and global denials, lifecycle and
assignment denials, immediate Role-definition revocation, stale reviewed writes,
assignment removal, independent native grants and process restart. Global Session
permissions remained unchanged. The member Roles table and permission dialog
showed only the two supported Team actions, without assignment controls; English
and live Chinese views passed and English was restored.

A controlled local response-loss proxy committed one assignment but returned 503.
The exact original retry returned 409 without resolving the unknown outcome. A new
same-actor/Team read preceded the explicit current-state confirmation; discarding
removed only the local draft. Two further consecutive saves without reload proved
current validators and candidate refresh. The owned browser tab, proxy, process,
Compose PostgreSQL/network and configuration/journal were removed.

The first complete workflow matrix failed because a test tried to create ordinary
roles with reserved `roles.write`; the API correctly rejected those inputs. A
subsequent focused run found a test-only singular `kind=team` query instead of the
supported `kind=teams`. Both fixtures are corrected without weakening production
validation. The corrected focused workflow matrix and complete final matrix remain
required before acceptance or commit. F06 remains partial pending these gates.


## Active isolated F22 Team usage package

The corrected F06 workflow focus passed on PostgreSQL and MySQL (238.34 seconds);
its final complete matrix is running against frozen main-checkout source. Final
check and test passed with 957 Vitest cases in 67 files, Go race/unit, development
lifecycle and embedded assets. F06 remains partial until full acceptance and phased
delivery. No further F06 production edits are planned unless acceptance finds a
specific defect.

The managed `team-usage` worktree starts from the checked F18 main baseline. Three
owners implement report/API logic, an independent real-database fixture, and the
existing usage interface respectively. The coordinator owns route/harness
registration, documentation, integration and final acceptance. Main's F06 source
remains isolated. Team reports authorize exact current enabled membership and an
active Team within the facts snapshot, expose only model/trend/currency aggregates,
and preserve actor-only call history. Platform `team_id` filtering uses independent
`calls.read_all` and immutable historical attribution. No new migration is needed.
F22 and the full objective remain active and incomplete.


## Accepted F06 source ready for main delivery

The corrected complete PostgreSQL/MySQL matrix passed (Handler 723.801 seconds;
Service 6.167 seconds) and removed its owned Compose resources. All mandatory
checks are green for the frozen source, including 957 Vitest cases in 67 files and
both database process auth/native lifecycles. F06 is now completed in the formal
capability index; totals are 10 completed, 17 partial and 3 not started. A02 and
full release acceptance remain open. This document's own checked delivery is
identified by `git log -1 -- docs/current-work-handoff.md`; never infer a new
commit SHA from an uncommitted document.

F22 Team usage remains active in the isolated `team-usage` worktree. Backend and
fixture source are frozen and focused compile/race/lint passed; the frontend
continues focused tests. A sole isolated real PostgreSQL/MySQL workflow focus has
started. Do not mix its source into this F06 commit or claim F22 acceptance yet.


## Published F06 and active F22/F07 checkpoint

F06 `ef821a689cfeb31b75bf737e29199094865850e9` was committed, pushed and read
back from `origin/main`; the coordination update `c456432` was also pushed while
preserving unrelated shell edits. Exact Actionlint 37127402424 and GolangCI-Lint
37127402443 succeeded; CI 37127402456 also succeeded for the exact commit.

The isolated F22 source has been integrated into main while preserving V41,
Team-role routes/rules and shared reset helpers. Final local check/test passed
with 990 Vitest cases in 69 files. The actual Team workflow focus passed on both
databases (215.926 seconds); a final correction removes Key availability from the
platform Team filter while preserving its authorized provider dimensions. The
complete final matrix is running against that final source. Actual native calls,
process restart and browser proof establish two actors/two requests/ten known
Tokens, empty member Personal usage, actor-only history, stale-result removal
upon revoked membership, exact rejoin totals, bilingual views and platform
Team/provider filtering without fabricated Keys. Owned acceptance resources were
removed; F22 remains partial for broader complete freshness/capacity acceptance.

F07 production, fixture and frontend owners work only in the separate
`project-authority` worktree. Canonical Project/actor/manager/model checks retain
valid direct-platform/current-manager distinctions and stable manager relation
IDs; alias inputs must leave relationships, audit and publication unchanged.
Private Project detail data and actions are hidden during renewed authorization
or errors and cannot be restored by old actor/resource responses. Source local
verification is progressing; real database and final main acceptance are pending.
No F07 completion or commit is claimed. The overall objective remains active.


## Accepted F22 source ready for main delivery

The final complete PostgreSQL/MySQL matrix passed (Handler 745.019 seconds;
Service 6.692 seconds) and removed its owned resources. All mandatory checks and
actual native/browser/restart proof are green for final F22 source, including
990 Vitest cases in 69 files. F22 remains partial for broader freshness/capacity
acceptance; totals remain 10 completed, 17 partial and 3 not started. Exact F06
CI 37127402456, Actionlint 37127402424 and GolangCI-Lint 37127402443 all succeeded.
The final F22 commit is identified through Git history rather than an invented
self-referencing SHA. Next complete the isolated F07 authority fixture, actual
both-database focus, final main integration/checks and controlled runtime proof.
The full objective remains active; no pause or overall completion is claimed.

## Current F07 acceptance checkpoint

F22 is pushed at `598ffd17e00f8ef651b8a8a4f7bbfbae864f8c48` with final local
check/test, dual-database and production/native/browser/restart evidence. Its exact
Actionlint 37128676137 and GolangCI-Lint 37128676202 succeeded; CI 37128676192 also succeeded for the exact commit. The completed `team-usage` worktree could not be
archived because the app reports it protected by a pinned task/workspace; retain
it and do not manually remove it. The separate `project-authority` worktree remains
needed for current source/fixture ownership.

F07 source is integrated into main, uncommitted, and limited to Project service
checks, one real-database fixture/registration, focused service tests, the shared
resource detail and eight UI privacy tests, plus directly related documents/rules.
Full check/test passed with 998 Vitest cases in 70 files. The initial real database
focus stopped on a test-only 403 expectation where the existing Project tombstone
correctly returned 401. Corrected source additionally proves original Project Key
independence after creator manager removal; the sole PostgreSQL/MySQL focus passed (226.497 seconds). The final full matrix
passed (Handler 760.155 seconds; Service 6.376 seconds); controlled production-browser/restart acceptance passed.

All required F07 local gates have passed. Commit and push this scoped F07 delivery,
verify its remote checks, then integrate only frozen F17 source and run its focused
GORM migration/default-policy acceptance before the final full matrix. F07 remains partial
for initial multi-manager creation and the complete Project overview. F17 User/Team defaults are being implemented in the separate `resource-defaults`
worktree by backend/API, frozen GORM V42/acceptance, and frontend owners. Its
unsubmitted source is separate from this F07 delivery. The objective continues and is not paused.

Controlled F07 production acceptance used an isolated owned PostgreSQL database.
Current-manager settings and the manager table were verified in English/Chinese;
actual manager removal produced detail/candidate 404 and mutation 403, and browser
refresh hid the old private data and actions. Restoring current management and
restarting the independent binary preserved Sessions, immutable creator identity
and exact current authority. Browser Retry restored the authorized detail, English
was restored and final browser error logs were empty. The temporary tab was closed;
owned process/database/network/files were removed by the acceptance script.

## Accepted F07 source ready for phased main delivery

Final PostgreSQL/MySQL full acceptance passed (Handler 760.155 seconds; Service
6.376 seconds) and removed owned Compose resources. Mandatory full check passed
on corrected final source; full test passed with 998 Vitest cases in 70 files,
Go race/unit, development lifecycle and embedded production assets. Controlled
production browser/restart evidence and 82 local Markdown references also passed.
F07 remains partial for creation/overview scope; totals remain 10 completed,
17 partial and 3 not started. The overall objective continues with F17 defaults
in its isolated worktree. Identify this checked delivery using Git history;
do not invent its own commit SHA before commit.
