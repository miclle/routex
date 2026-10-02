# Current Work Handoff

- **Status:** implementation active; F11 replacement preparation pushed; per-attempt attribution checked; native completion evidence next
- **Updated:** 2026-10-02T14:31:00+08:00
- **Repository / branch:** RouteX / `main`
- **Previous checked source baseline:** `16c0cff6098390fdd6f7df0a9160901859cfad31`, pushed and read back from `origin/main`
- **Current owner:** coordinating task; independent persistence/schema and native-parser owners prepared for native completion evidence
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

## Latest checked package: immutable per-attempt attribution

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

F17 reminders were assessed, but are not delivered by this package. Current
notifications authorize platform operators only. Personal/Project recipients,
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

- Replacement preparation was pushed as `16c0cff`, with its exact remote SHA read back and a clean RouteX tree before per-attempt attribution began. The following checked package delivers V32 attribution, runtime recording, compatibility checkpoint repair, tests and directly associated documents; identify its commit with the transport command above. Metadata/deletion was previously pushed as `1d3e430`.
- Dotfiles baseline was `3615f848b9dca4ef1d18c981c93c3b797146e37e`, pushed to `origin/main`; stage only the RouteX coordination record for later updates.
- Preserve unrelated modified dotfiles `zsh/.zshrc`; never stage, overwrite or discard it.
- Fetch both repositories' `main` branches to transfer checked commits. Ignored configuration, databases, processes and temporary logs do not transfer; recreate them from `docs/DEVELOPMENT.md`.

## Next actions

1. Commit/push the checked V32 package, read back its exact remote SHA and inspect its own CI; earlier replacement CI is separate evidence.
2. Add parser-owned native completion evidence with bounded unknown/completed/handoff/blocked/incomplete values. The persistence owner implements frozen V33 and exact storage/legacy normalization; the runtime owner implements native ordinary/SSE observations without changing forwarding, status or usage contracts. Weak/unsupported shapes and legacy history stay unknown.
3. Validate native terminal requirements, multiple Chat choices, clean Gemini EOF, cancellation/transport failure and journal replay, then run mandatory check/test/matrix before the next commit/push.
4. Follow with scoped current-configuration readback before a server-gated planned retirement operation. A separate durable retirement receipt must reconcile historical commit and current application after source disable changes publication; retries must not re-disable a re-enabled source. HTTP/call success can include empty Chat payloads or Gemini prompt blocks; discovery/global readiness/usage completeness cannot authorize retirement. Emergency disable stays independent.
5. Real-provider, SMTP, IdP/LDAP, Vault, S3, price-source and production acceptance require supplied environments and resources; do not search other accounts for credentials.

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
