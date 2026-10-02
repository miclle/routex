# Current Work Handoff

- **Status:** implementation active; F11 Credential metadata/deletion checked; staged replacement preparation is next
- **Updated:** 2026-10-02T13:12:00+08:00
- **Repository / branch:** RouteX / `main`
- **Verified source baseline:** `66874aa2ac444831dfcd74888dde4273c4dc864e`, equal to `origin/main` before this package
- **Current owner:** coordinating task; independent Credential backend and frontend workers have finished their assigned changes
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

## Current package: F11 Credential metadata and deletion

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
| Source baseline remote checks | [CI 36700399323](https://github.com/miclle/routex/actions/runs/36700399323), [Actionlint 36700399389](https://github.com/miclle/routex/actions/runs/36700399389), and [GolangCI-Lint 36700399452](https://github.com/miclle/routex/actions/runs/36700399452) passed for `66874aa`; they do not establish acceptance of this new package. |
| Focused frontend | 68 Credential metadata/deletion/filter/i18n cases passed; TypeScript and scoped ESLint passed. |
| Focused backend | Metadata/deletion validation and typed audit projection tests passed; independent contract review reported no remaining finding. |
| Exact combined `go tool task check` | Passed after formatting, with no lint errors and two existing Fast Refresh warnings. |
| Exact combined `go tool task test` | Passed: 584 Vitest cases, Go race/unit, four Node checks, development lifecycle and production build/embedded assets. |
| Full PostgreSQL/MySQL matrix | Passed after physical-pool isolation: Handler 386.887 seconds, Service 5.144 seconds. Both databases, complete frozen migrations and lifecycle assertions ran; disposable containers/network were removed. |
| Browser | Isolated PostgreSQL fixture confirmed the original Credentials table/action menu, English editing fields and cancelled draft, Chinese deletion preview/reason and cancellation with the row intact. No browser write or real upstream call was submitted. Final embedded assets were rebuilt, reloaded, and observed in English and Chinese; English was restored. The temporary tab, service, Compose database/network and fixture files were removed. |

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

- RouteX baseline was clean and equal to `origin/main`; current changes belong to this F11 package and its tests/docs/rules.
- Dotfiles baseline was `dc9cf2288fee21aa8935aa7e2458bc68183b8568`, equal to `origin/main`; stage only the RouteX coordination record for its separate commit.
- Preserve unrelated modified dotfiles `zsh/.zshrc`; never stage, overwrite or discard it.
- Fetch both repositories' `main` branches to transfer checked commits. Ignored configuration, databases, processes and temporary logs do not transfer; recreate them from `docs/DEVELOPMENT.md`.

## Next actions

1. The complete isolated PostgreSQL/MySQL matrix passed; preserve its exact-source evidence.
2. Finish scoped README, catalogue, implementation-index and handoff/coordination updates; check whitespace and local links, then commit and push the checked package to `main`.
3. Verify remote SHA and exact CI outcomes. Do not attribute baseline CI to a later commit.
4. Continue the next bounded partial-capability package, starting with the staged Credential rotation assessment. Keep separate backend/frontend ownership, reviewed API contracts and meaningful tests.
5. Real-provider, SMTP, IdP/LDAP, Vault, S3, price-source and production acceptance require supplied environments and resources; do not search other accounts for credentials.

## Files to read first

| Path | Purpose |
| --- | --- |
| `AGENTS.md`, `.agents/rules/frontend.md` | Mandatory architecture, UI, localization, formatting and verification |
| `docs/IMPLEMENTATION.md`, `docs/CATALOG.md` | Full F/A inventory and Credential lifecycle contracts |
| `internal/routex/service/credential_metadata.go`, `credential_delete.go` | Persistence, audit, authorization and publication boundaries |
| `website/src/views/providers/credential-metadata.tsx`, `credential-delete.tsx` | Draft, conflict and retry behavior |
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
