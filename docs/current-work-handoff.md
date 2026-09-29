# Current Work Handoff

- **Status:** active; roadmap resumed and phased implementation in progress
- **Updated:** 2026-09-29T17:00:00+08:00
- **Repository:** `/Users/miclle/github/miclle/routex`
- **Branch:** `main`
- **Base:** `main`
- **Implementation HEAD:** `0c445e11ddd5805bf99422e11d89206e9a1e45e3`
- **Upstream:** `origin/main`, zero commits ahead and zero behind at capture
- **Last pushed implementation state:** `0c445e11ddd5805bf99422e11d89206e9a1e45e3`
- **Current owner:** Codex phased implementation
- **Next owner:** current task until the full objective closes or a later handoff supersedes this file
- **Transfer state:** active; transferable at a clean verified phase boundary
- **Transport:** `origin/main`; resolve the exact handoff commit with `git log -1 -- docs/current-work-handoff.md`
- **Receiver access:** RouteX repository, this document, `docs/IMPLEMENTATION.md`, and the cross-repository roadmap described below

## Objective

Implement every valid RouteX capability represented by F01–F30 and close every A01–A20 acceptance case, while preserving the existing Go/React architecture, PostgreSQL/MySQL portability, GORM-first migrations, the approved product layout, shadcn/ui and Base UI primitives, English project documentation, bilingual English/Chinese UI copy, phased verification, and incremental main-branch delivery. The user resumed this objective on 2026-09-29; bounded packages continue until the full objective closes or an external dependency blocks a specific acceptance gate.

## Current State

### Completed

- The Compose development and isolated PostgreSQL/MySQL test foundation is delivered.
- F01 local initialization and identity, F02 account security, F08 Personal/Project Key lifecycle, F09 current single-node admission controls, F16 currency and immutable price snapshots, and F25 site presentation/announcements meet their current capability definitions.
- Four native inference protocols, durable call facts, usage interfaces, Playground conversation/comparison, executable examples, managed egress, price maintenance, quotas, SMTP administration/test delivery, and the storage/owned-attachment backend have substantial delivered foundations.
- Storage administration is delivered by `26763e8` (`feat(storage): add administration interface`) with the approved overview-card and large-drawer composition, independent read/write/test permissions, transient credentials, exact ETag review, diagnostics, and verified rollback.
- Provider-model input capability metadata is delivered by `3b5e642` (`feat(models): declare input capabilities`). Image and PDF support default to false, update atomically with availability, participate in runtime publication, and are exposed as a conservative intersection across ready, enabled, positive-weight routes for each native protocol.
- F20-B personal-Key attachment resolution is delivered by `652cb44` (`feat(gateway): resolve owned attachments`). That initial slice recognized bounded RouteX attachment references only in protocol-owned image/PDF positions, rejected unsupported selected routes before storage access, verified owner-bound ready objects, deduplicated reads, rewrote native inline data, and performed final admission and upstream dispatch once. F20-F extends the same guarantees to Project ownership and Project Keys.
- F20-C single-model Playground attachment input is delivered by `7fa25d7` (`feat(playground): add attachment input`). The existing Mockup composition now supports bounded PNG, JPEG, and PDF selection, session/CSRF upload and cleanup, effective per-protocol capability gates, transient personal-Key inference, and exact native payload construction for Chat Completions, Responses, Messages, and Gemini. Ready attachment objects expire after one hour through the existing durable cleanup lifecycle, and read access enforces the same deadline even while the cleanup worker is unavailable.
- F20-D comparison attachment input is delivered by `ccb6255` (`feat(playground): add comparison attachments`). The comparison workbench preserves the Mockup's single shared bottom composer, intersects every selected lane's personal-attachment and per-protocol media capabilities, sends the shared owner-bound references through each lane's exact native current turn, keeps requests and cancellation independent, and starts cleanup only after every lane settles without blocking later submissions.
- F20-E conservative multimodal quota admission is delivered by `aa33b5a` (`feat(quotas): bound attachment token admission`). Structurally validated Personal-Key image/PDF requests can use finite token and TPM policies through the selected provider model's full attested input capacity plus the native output cap. A side-effect-free durable preflight rejects exhausted token, RPM, concurrency, unavailable journal, and unsupported money cases before storage reads; final admission repeats every check atomically after owner-bound resolution. Native terminal usage settles exact tokens, incomplete usage retains the hold, and overruns invalidate the bound revision. F20-G supersedes the earlier monetary limitation.
- F20-F Project-owned attachment identity and lifecycle is delivered by `1e4af76` (`feat(storage): add project-owned attachments`). Frozen migration V23 introduces explicit user/Project ownership with portable GORM migration paths and dual-database recovery coverage. Current enabled Project managers govern session upload, metadata, content, deletion, and cleanup; ownership survives manager replacement, disabled or archived Projects remain recoverable without becoming inference-eligible, and Project Keys resolve only their Project objects across all four native protocols. The existing Project Overview links into the unchanged Playground composition, which verifies the Key scope before uploading and never sends the Key to control-plane attachment APIs.
- F20-G exact attachment input pricing is delivered by `0c445e1` (`feat(pricing): add attachment input charges`). Base-only `IMAGE_INPUT / 1_IMAGE` and `PDF_INPUT / 1_PDF` rates apply to strictly validated forwarded occurrences while storage reads remain deduplicated. The gateway reserves token maxima plus exact media components before storage reads, settles authoritative native token/cache usage with immutable occurrence counts, and durably classifies unexpected provider media without retaining quota leases. Frozen GORM migration V24 adds nullable historical media counts; new calls persist explicit zero or positive counts. The existing provider-model price and import interfaces now expose bilingual media rows and preserve decimal/ETag/currency behavior.
- `docs/IMPLEMENTATION.md` contains the authoritative per-capability and per-acceptance status snapshot. The cross-task roadmap is `/Users/miclle/dotfiles/projects/routex/implementation-plan.md`.

### In Progress

- F04–F07, F10–F15, F17–F23, F27, F28, and F30 are partially completed. Their exact delivered boundaries and remaining conditions are listed in `docs/IMPLEMENTATION.md`.
- A02–A12, A14–A15, and A17–A20 have partial controlled evidence but are not fully accepted.
- F14 protects saved credentials from endpoint changes and now retries the complete TLS/CONNECT or SOCKS5 negotiation across every validated proxy endpoint address. External proxy and production performance acceptance remain open.
- F13 has a replay-safe route-attempt foundation, but active retry, health routing, and failover are not wired into gateway execution.
- F27 now includes storage administration, explicit user/Project ownership, the gateway resolver, both Playground upload controls, and durable abandoned-object expiry. Durable notifications and external-service acceptance remain open.
- F20 now includes explicit provider-model image/PDF declarations, effective per-protocol discovery, user/Project server-side byte resolution, both approved Playground attachment compositions, conservative token/TPM/money admission, and exact per-occurrence media settlement. Provider-specific billing dimensions and external-provider acceptance remain open.

### Not Started or Out of Scope

- F03 enterprise identity, F24 AI operations analysis, F26 instance/system-job management, and F29 API Key Vault delivery are not started.
- A13 complete approval contention and A16 Vault compensation are not started.
- Real external-provider, IdP/LDAP/OAuth, Vault, S3, SMTP, price-source, production deployment, backup/restore, multi-node, and measured-capacity acceptance remain open because the required environments or decisions have not been supplied.
- The roadmap is active. The next bounded package is F13 active route execution: connect the existing replay-safe route-attempt foundation to bounded health-aware retry and failover without retrying after streamed output or duplicating call facts, quota settlement, or metering. Real-provider acceptance remains a separate gate because no dedicated external environment or spending limit has been supplied.

## Working Tree

- **Staged:** none
- **Modified:** only this handoff refresh before its documentation checkpoint
- **Untracked:** none
- **Unpushed commits:** `0c445e1` is pushed; commit and push this handoff refresh separately
- **Do not overwrite:** preserve any new user or concurrent-task changes discovered by the receiver; re-run the state checks before editing

## Decisions and Rationale

| Decision | Rationale | Consequence |
|---|---|---|
| Use `Completed`, `Partially completed`, and `Not started` as progress states | A binary incomplete label hid substantial delivered work | Partial work must list both the delivered boundary and the remaining acceptance gate |
| Preserve Handler → Service → Entity layering | It is the established RouteX architecture | Register routes centrally and keep database setup/adapters in `internal/routex/database/` |
| Use GORM-first frozen numbered migrations | PostgreSQL/MySQL behavior must remain portable and released migrations immutable | Handwritten SQL requires a documented GORM limitation and dual-database coverage |
| Follow the approved product layout with local shadcn/ui and Base UI wrappers | The product interaction specification is already established | Do not redesign pages or introduce Ant Design |
| Keep English as the default UI language and project-document language | This is an explicit project requirement | Pair all visible copy in `en` and `zh`; keep documentation and commit text in English |
| Keep competitor names and comparisons outside RouteX | RouteX must be described independently | Do not introduce reference-project names into code, UI, docs, commits, or PRs |
| Resume in bounded verified packages | The user explicitly resumed the full objective | Complete, test, document, commit, push, and verify each package before advancing |

## Verification Evidence

| Command or check | Result | Notes |
|---|---|---|
| `go tool task check` on `d6863aa` | pass | Backend lint, TypeScript, formatting, and module checks passed before the implementation commit |
| `go tool task test` on `d6863aa` | pass | Go race tests, 378 Vitest cases, development lifecycle, and production assets passed |
| `go tool task test-integration` on `d6863aa` | pass | PostgreSQL and MySQL lifecycle suite passed in 275.269 seconds |
| `go tool task test-auth-lifecycle` on `d6863aa` | pass | Both supported databases passed real-process lifecycle coverage |
| Remote CI run `35863119409` | pass | Includes database integration and artifact build for `d6863aa` |
| Remote Actionlint run `35863119428` | pass | Workflow syntax passed for `d6863aa` |
| Remote GolangCI-Lint run `35863119455` | pass | Go lint passed for `d6863aa` |
| Documentation `git diff --check` | pass | RouteX and dotfiles documentation diffs contain no whitespace errors |
| Full code/test suite for this documentation-only refresh | not run | No implementation code changed; use document checks only |
| `go tool task check` on `f230d6d` source | pass | Backend lint, formatting, TypeScript, ESLint, and module checks passed; ESLint retained two existing Fast Refresh warnings |
| `go tool task test` on `f230d6d` source | pass | Go race/unit, 378 Vitest cases, four Node checks, development lifecycle, and production assets passed |
| `go tool task test-integration` on `f230d6d` source | pass | PostgreSQL/MySQL handler matrix passed in 397.124 seconds and disposable Compose resources were removed |
| Remote CI run `36516007979` | pass | Backend, frontend, PostgreSQL/MySQL, process restart, embedded assets, development lifecycle, and artifact build passed for `f230d6d` |
| Remote Actionlint run `36516007944` | pass | Workflow syntax passed for `f230d6d` |
| Remote GolangCI-Lint run `36516007929` | pass | Go lint passed for `f230d6d` |
| `go tool task check` on `3b5e642` source | pass | Backend lint, formatting, TypeScript, ESLint, and module checks passed; ESLint retained two existing Fast Refresh warnings |
| `go tool task test` on `3b5e642` source | pass | Go race/unit, 396 Vitest cases, four Node checks, development lifecycle, and production assets passed |
| `go tool task test-integration` on `3b5e642` source | pass | PostgreSQL/MySQL handler matrix passed in 264.100 seconds; service integration passed in 3.227 seconds; disposable Compose resources were removed |
| Focused PostgreSQL/MySQL model capability matrix | pass | Migration, service, gateway, and handler paths passed on both real databases in 71.835 seconds |
| Remote Actionlint run `36522586518` | pass | Workflow syntax passed for `3b5e642` |
| Remote GolangCI-Lint run `36522586538` | pass | Go lint passed for `3b5e642` |
| Remote CI run `36522815734` | pass | The latest pushed checkpoint `067ea41` includes the provider-model implementation and passed backend, frontend, PostgreSQL/MySQL, lifecycle, and build checks |
| Remote Actionlint run `36522815766` | pass | Workflow syntax passed for `067ea41` |
| Remote GolangCI-Lint run `36522815852` | pass | Go lint passed for `067ea41` |
| Focused F20-B attachment race tests | pass | Native scanning, opaque-field isolation, route capability, quota ordering, object integrity, deduplication, rewrite limits, and cancellation coverage passed after final Responses position coverage |
| `go tool task check` and `go tool task test` on F20-B source | pass | Backend lint, formatting, TypeScript, ESLint, Go race/unit, 396 Vitest cases, four Node checks, development lifecycle, and production assets passed; ESLint retained two existing Fast Refresh warnings |
| `go tool task test-integration` on F20-B source | pass | PostgreSQL/MySQL handler matrix passed in 260.257 seconds, service integration passed in 4.245 seconds, and disposable Compose resources were removed |
| `go tool task check` on F20-C source | pass | Backend lint, formatting, TypeScript, ESLint, and module checks passed; ESLint retained two existing Fast Refresh warnings |
| `go tool task test` on F20-C source | pass | Go race/unit, 415 Vitest cases, four Node checks, development lifecycle, frontend production build, and embedded production assets passed |
| `go tool task test-integration` on F20-C source | pass | The complete PostgreSQL/MySQL matrix passed with the Handler package at 325.635 seconds and Service package at 4.236 seconds; disposable Compose resources were removed |
| Focused PostgreSQL/MySQL attachment expiry matrix | pass | Both databases denied expired reads before cleanup, preserved durable retry state, and isolated storage probe state; PostgreSQL passed in 2.05 seconds and MySQL passed in 1.94 seconds |
| Independent F20-C implementation review | pass | Final review reported zero critical findings, warnings, or suggestions after stale-batch cleanup and expiry-read fixes |
| `go tool task check` on F20-D source | pass | Backend lint, formatting, TypeScript, ESLint, and module checks passed; ESLint retained two existing Fast Refresh warnings |
| `go tool task test` on F20-D source | pass | Go race/unit, 425 Vitest cases, four Node checks, development lifecycle, frontend production build, and embedded production assets passed |
| Focused F20-D Playground tests | pass | 51 comparison and conversation cases covered capability intersection, four native payloads, all-lane settlement, non-blocking deletion, stale multi-file upload cleanup, strict returned MIME validation, and storage absence |
| Independent F20-D implementation review | pass | Final review reported zero critical findings, warnings, or suggestions after cleanup-lock and stale-batch regression fixes |
| Remote CI run `36529760039` | pass | The F20-C documentation checkpoint passed backend, frontend, PostgreSQL/MySQL, process lifecycle, production asset, and artifact checks |
| Remote Actionlint run `36529759978` and GolangCI-Lint run `36529759995` | pass | Workflow syntax and Go lint passed for the F20-C documentation checkpoint |
| `go tool task check` on F20-E source | pass | Backend lint, formatting, TypeScript, ESLint, and module checks passed; ESLint retained two existing Fast Refresh warnings |
| `go tool task test` on F20-E source | pass | Go race/unit, 425 Vitest cases, four Node checks, development lifecycle, frontend production build, and embedded production assets passed |
| `go tool task test-integration` on F20-E source | pass | The complete PostgreSQL/MySQL matrix passed with the Handler package at 299.679 seconds and Service package at 6.534 seconds; disposable Compose resources were removed |
| Focused PostgreSQL/MySQL attachment-quota settlement | pass | Both databases proved authoritative token settlement, retained incomplete holds, overrun invalidation, pre-dispatch rejection, and unsupported pricing in 50.821 seconds |
| Independent F20-E implementation review | pass | Final review reported zero critical findings, warnings, or actionable suggestions after the durable preflight fix |
| `go tool actionlint` on F20-E source | pass | Workflow syntax remained valid |
| `go tool task check` on F20-F source | pass | Backend lint, Prettier, TypeScript, ESLint, and module checks passed; ESLint retained two existing Fast Refresh warnings |
| `go tool task test` on F20-F source | pass | Go race/unit coverage, 441 Vitest cases in 41 files, four Node checks, development lifecycle, production build, and embedded production assets passed |
| `go tool task test-integration` on F20-F source | pass | The complete PostgreSQL/MySQL matrix passed with the Handler package at 348.771 seconds and Service package at 5.259 seconds; disposable Compose resources were removed |
| Focused PostgreSQL/MySQL Project attachment lifecycle | pass | Both databases covered empty/concurrent V23 migration, V22 upgrade, interrupted DDL recovery, exact scope indexes and constraints, manager replacement races, disabled/archived recovery, quota ordering, and four native protocols |
| Independent F20-F implementation review | pass | Final review found no code-level correctness, authorization, migration, Key-secrecy, Mockup-conformance, i18n, or test defects; the only warning was this now-resolved handoff refresh |
| `go tool actionlint` on F20-F source | pass | Workflow syntax remained valid |
| `go tool task check` on F20-G source | pass | Backend lint, Prettier, TypeScript, ESLint, and module checks passed; ESLint retained two existing Fast Refresh warnings |
| `go tool task test` on F20-G source | pass | Go race/unit coverage, 445 Vitest cases in 41 files, four Node checks, two development lifecycle checks, production build, and embedded production assets passed |
| `go tool task test-integration` on F20-G source | pass | The complete PostgreSQL/MySQL matrix passed with the Handler package at 357.967 seconds and Service package at 5.158 seconds; disposable Compose resources were removed |
| Independent F20-G implementation review | pass | Final review found no remaining correctness, migration, quota-settlement, protocol, UI, i18n, documentation, or test findings after durable unexpected-media and crash-recovery fixes |
| `go tool actionlint` on F20-G source | pass | Workflow syntax remained valid |
| Remote CI run `36545093687` | pass | Backend, frontend, PostgreSQL/MySQL, real-process restart, production asset, development lifecycle, and artifact jobs passed for `0c445e1` |
| Remote Actionlint run `36545093588` | pass | Workflow syntax passed for `0c445e1` |
| Remote GolangCI-Lint run `36545093642` | pass | Go lint passed for `0c445e1` |

## Blockers, Risks, and Unknowns

- **Blockers:** no blocker prevents the next local package; external provider, enterprise identity, Vault, object-storage, mail, production, and capacity environments are not supplied for their acceptance gates.
- **Risks:** single-process quota evidence does not prove multi-node correctness; provider-specific media billing remains open; active retries must preserve stream, quota, metering, and attempt idempotency; partial milestones must not be presented as full release acceptance.
- **Unknowns:** production topology, multi-node requirement, initial provider, spending limit, capacity targets, backup/restore procedure, IdP choices, Vault layout, price-source contract, and AI-analysis scope remain undecided or unverified.

## Files to Read First

| Path | Why it matters |
|---|---|
| `docs/IMPLEMENTATION.md` | Authoritative F01–F30 and A01–A20 status, evidence, and remaining boundaries |
| `docs/ARCHITECTURE.md` | Gateway, Control Plane, and Data Platform ownership boundaries |
| `AGENTS.md` | Mandatory architecture, migration, UI, localization, formatting, and verification rules |
| `docs/STORAGE.md` | Delivered object/owner boundary, attachment expiry, and gateway resolver contract |
| `docs/PLAYGROUND.md` | Native-protocol, transient-credential, and single-model attachment behavior |
| `docs/PRICING.md` | Current token/media metrics, exact arithmetic, adapter, quote, and unsupported-dimension contract |
| `docs/PROVIDER_MODELS.md` | Effective image/PDF capability contract used by attachment routing |
| `internal/routex/database/call_media_pricing_migration.go` | Frozen V24 nullable media-count migration and portability boundary |
| `pkg/routeattempt/` | Existing replay-safe attempt-selection foundation for the next F13 package |
| `internal/routex/service/attachments.go` | Current-manager Project lifecycle and owner-scoped validated byte reads |
| `internal/routex/service/gateway*.go` | Native parsing, admission ordering, route selection, and dispatch boundaries |
| `/Users/miclle/dotfiles/projects/routex/implementation-plan.md` | Cross-task schedule, dependency, acceptance, and pause coordination entry |

## Next Actions

1. Audit `pkg/routeattempt/`, `internal/routex/service/gateway*.go`, runtime route publication, call attempts, and stream forwarding against F13/A07/A08; complete when retryable phases, health evidence, attempt limits, and no-retry-after-output behavior are explicit.
2. Implement the smallest active health-aware retry/failover slice across the four native protocols while preserving one logical request fact, one quota admission/settlement, independent attempt records, cancellation, and exact native response semantics.
3. Add controlled ordinary/streaming failure matrices, race coverage, and PostgreSQL/MySQL attempt persistence; run the full required checks, update English documentation, then commit and push the verified phase.

## Environment and Access

- **Required tools/services:** Go 1.27.1; module-managed Task, reflex, staticcheck, and actionlint; Node/npm from the project environment; Docker Compose; PostgreSQL 18 and MySQL 8.4 for integration.
- **Local-only configuration:** `cmd/routex/config.local.yaml`, optional `ROUTEX_HTTP_PORT`, `ROUTEX_VITE_PORT`, `ROUTEX_API_BASE_URL`, and `ROUTEX_VITE_DEV_SERVER_URL`; do not record secret values.
- **Current local development process:** RouteX was started on backend port `19000`, Vite port `15173`, and Compose PostgreSQL port `15433`. Verify health rather than assuming the process survived the handoff.
- **Access needed:** dedicated provider credentials and spending authorization, IdP/LDAP/OAuth resources, Vault, external S3/SMTP test environments, price-source contract, and production deployment access for their respective acceptance gates.
- **Setup caveats:** default ports `9000` and `5173` were occupied by unrelated local services; do not terminate unrelated processes. Use explicit alternative ports when necessary.

## Handoff History

- **Continues from:** storage administration at `26763e8`, provider-model input capabilities at `3b5e642`, personal-Key attachment resolution at `652cb44`, single-model attachment input at `7fa25d7`, comparison attachment input at `ccb6255`, conservative multimodal quota admission at `aa33b5a`, Project-owned attachment lifecycle at `1e4af76`, and attachment input pricing at `0c445e1`
- **Supersedes:** the storage-administration-next checkpoint at `bcc663a`
- **Closeout condition:** all F01–F30 capabilities and A01–A20 acceptance cases are completed with current evidence, or a later handoff replaces this document with an equally verifiable resume point
