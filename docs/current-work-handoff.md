# Current Work Handoff

- **Status:** F23 Provider quality and notification-history package delivered; the full RouteX objective is paused at the user's request
- **Updated:** 2026-09-30T13:42:00+08:00
- **Repository:** `/Users/miclle/github/miclle/routex`
- **Branch:** `main`
- **Base:** `main`
- **Verified implementation HEAD:** `b201b0823f8cd8f33495c849b86a7b2589962561`
- **Implementation commits:** `9ef78f6` (`feat(quality): add provider attempt monitoring`) and `b201b08` (`feat(providers): add quality workspace`)
- **Upstream:** `origin/main`, zero commits ahead and zero behind at capture
- **Last pushed implementation state:** `b201b0823f8cd8f33495c849b86a7b2589962561`
- **Current owner:** none while the full objective is paused
- **Next owner:** the next local task after the user resumes and RouteX and dotfiles `main` are synchronized
- **Transfer state:** transferable after this documentation checkpoint; implementation, validation, browser evidence, and remote source commits are published
- **Transport:** `origin/main`; resolve the exact handoff commit with `git log -1 -- docs/current-work-handoff.md`
- **Receiver access:** RouteX `origin/main`, this document, `docs/IMPLEMENTATION.md`, and dotfiles `origin/main`; resolve the latest roadmap checkpoint with `git log -1 -- projects/routex/implementation-plan.md`

## Objective

Implement every valid RouteX capability represented by F01–F30 and close every A01–A20 acceptance case, while preserving the existing Go/React architecture, PostgreSQL/MySQL portability, GORM-first migrations, the approved product layout, shadcn/ui and Base UI primitives, English project documentation, bilingual English/Chinese UI copy, phased verification, and incremental main-branch delivery. The Provider-quality package is complete, and the user requested a pause before another bounded package begins.

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
- F13 active native failover is delivered by `17358de` (`feat(gateway): add bounded route failover`). Published-runtime Chat Completions, Responses, Messages, and Gemini requests now execute at most four replay-safe same-protocol attempts with credential priority, process-local Connection and credential health, current authorization/egress/policy/price rechecks, one durable admission and quota settlement, and no retry after a usable response. Frozen GORM migration V25 stores ordered administrator-only attempt diagnostics. Atomic pre-dispatch recovery and the `no_work` pricing state preserve exact zero economics only when durable evidence proves no provider work.
- F21 safe call-record CSV export is delivered by `dc99f35` (`feat(calls): add safe CSV exports`). Personal, Project, and platform workspaces export the complete applied filter through dedicated server-authorized endpoints. Generation is bounded to five seconds, 10,000 rows, and 8 MiB; authorization and selection share one repeatable-read transaction; member files retain safe columns while platform files add only user/Project attribution. UTF-8 files protect formula-capable cells, preserve exact decimals and null-versus-zero values, and are downloaded through bilingual transient Blob actions in the approved filter layout.
- F22-A immutable Provider-attributed usage is delivered by `d24997a` (`feat(usage): add provider attribution`). Frozen GORM migration V26 snapshots Provider IDs/names, Connection names, and upstream model names only after an attempt enters execution; failover keeps the final attempted route while legacy and pre-attempt calls remain explicit unknowns. Platform reports add Provider filtering and Provider/provider-model/Connection distributions with historical labels; Personal and Project reports retain topology isolation.
- F26 authoritative System Status is delivered by `6213cc7` (`feat(system): track instances and operational jobs`) and `fa71c2e` (`feat(system): add status administration workspace`). Frozen GORM migrations V27 and V28 persist process generations and bounded actual-job history on PostgreSQL and MySQL. Server-owned leases, nullable resource facts, executor-loss reconciliation, revision-checked transactional cleanup, one target-addressable audit event per retired instance, and the bilingual Mockup-faithful administration workspace are implemented.
- The bounded F23 operational-notification package is delivered by `d5ae09f` (`feat(notifications): add durable operational alerts`) and `e5f189c` (`feat(overview): add operations workspace`). Frozen GORM migration V29, durable grouped occurrences, recipient-isolated inbox projections, atomic personal settings, bounded SMTP intents, failed-job and credential-verification sources, real persisted overview aggregates, the Mockup-aligned operations workspace, and paired English/Chinese copy are implemented.
- The F23 Provider-quality package is delivered by `9ef78f6` (`feat(quality): add provider attempt monitoring`) and `b201b08` (`feat(providers): add quality workspace`). Frozen GORM migration V30 records immutable attempt attribution and duration, revisioned Provider policies evaluate closed windows, degraded/recovered transitions and route-unavailable calls publish typed alerts, the operations overview exposes nullable bounded quality facts, and the Provider detail, notification history, and independent severity settings follow the approved bilingual interface.
- `docs/IMPLEMENTATION.md` contains the authoritative per-capability and per-acceptance status snapshot. The cross-task roadmap is `/Users/miclle/dotfiles/projects/routex/implementation-plan.md`.

### In Progress

- F04–F07, F10–F15, F17–F23, F27, F28, and F30 are partially completed. Their exact delivered boundaries and remaining conditions are listed in `docs/IMPLEMENTATION.md`.
- A02–A12, A14–A15, and A17–A20 have partial controlled evidence but are not fully accepted.
- F14 protects saved credentials from endpoint changes and now retries the complete TLS/CONNECT or SOCKS5 negotiation across every validated proxy endpoint address. External proxy and production performance acceptance remain open.
- F13 now has active bounded failover and controlled A07/A08 evidence. Real-provider behavior, measured capacity, and multi-node health coordination remain open acceptance gates.
- F23 now includes the real-data operations overview, exact Provider-attempt success/P95 quality, revisioned policies, grouped Provider-quality and route-unavailable alerts, typed affected-resource snapshots, recipient-isolated unread and complete history, independent severity settings, and bounded durable SMTP intents. External SMTP acceptance, bounce/inbox tracking, real-Provider acceptance, and broader quota/enterprise event sources remain open.
- F27 now includes storage administration, explicit user/Project ownership, the gateway resolver, both Playground upload controls, durable abandoned-object expiry, SMTP administration/test delivery, and durable operational email intents. External-service acceptance remains open.
- F20 now includes explicit provider-model image/PDF declarations, effective per-protocol discovery, user/Project server-side byte resolution, both approved Playground attachment compositions, conservative token/TPM/money admission, and exact per-occurrence media settlement. Provider-specific billing dimensions and external-provider acceptance remain open.
- F22 has Personal, Project, and platform usage views plus durable Provider attribution. Team attribution and complete freshness/capacity acceptance remain open.

### Not Started or Out of Scope

- F03 enterprise identity, F24 AI operations analysis, and F29 API Key Vault delivery are not started.
- A13 complete approval contention and A16 Vault compensation are not started.
- Real external-provider, IdP/LDAP/OAuth, Vault, S3, SMTP, price-source, production deployment, backup/restore, multi-node, and measured-capacity acceptance remain open because the required environments or decisions have not been supplied.
- F23 remains partially completed at its documented external and broader-source boundary. Do not start another package until the user resumes the paused objective.

## Working Tree

- **Staged:** none after the phased commits
- **Modified/untracked:** none expected after this documentation checkpoint; verify with `git status --short`
- **Unpushed commits:** none expected; verify RouteX `HEAD` equals `origin/main`
- **Do not overwrite:** dotfiles has an unrelated local modification at `zsh/.zshrc`; preserve it. Also preserve any new user or concurrent-task changes discovered after synchronization.

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
| Pause after the Provider-quality package | The user explicitly requested a pause after the in-progress task completed | Preserve a clean pushed checkpoint and start no later package until the user resumes |
| Deliver F23 as grouped operational facts plus per-recipient projections | Repeated source failures must not create duplicate alerts or cross recipient boundaries | Persist immutable source occurrences, current grouped state, and exact delivery intents |
| Treat uncertain SMTP completion as terminal unknown | Replaying after DATA may send duplicate email | Expired sending leases and ambiguous acceptance are never retried automatically |

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
| Focused F13 race suites | pass | Route planning, strict native classification, pre-request transport proof, health cooldowns, durable checkpoints, atomic zero-work recovery, cancellation after admission, diagnostics, and partial-write output evidence passed across service, handler, eventqueue, routeattempt, and upstream packages |
| `go tool task check` on F13 source | pass | Backend lint, Prettier, TypeScript, ESLint, and module checks passed; ESLint retained two existing Fast Refresh warnings |
| `go tool task test` on F13 source | pass | Go race/unit coverage, 445 Vitest cases in 41 files, four Node checks, two development lifecycle checks, production build, and embedded production assets passed |
| `go tool task test-integration` on F13 source | pass | The complete PostgreSQL/MySQL matrix passed with the Handler package at 493.026 seconds and Service package at 6.018 seconds; disposable Compose resources were removed |
| Independent F13 reviews | pass | Backend correctness, diagnostic safety, and test-gap reviews reported no remaining actionable findings after zero-work recovery, partial-write evidence, and integration-race fixes |
| `go tool actionlint` and `git diff --check` on F13 source | pass | Workflow syntax and staged whitespace checks passed |
| `NODE_OPTIONS=--no-experimental-webstorage go tool task check` on F21 source | pass | Backend formatting/vet/lint, Prettier, ESLint, TypeScript, and module checks passed; ESLint retained two existing Fast Refresh warnings |
| `NODE_OPTIONS=--no-experimental-webstorage go tool task test` on F21 source | pass | Go race/unit coverage, 449 Vitest cases in 41 files, four Node checks, two development lifecycle checks, production build, and embedded assets passed |
| `go tool task test-integration` on F21 source | pass | PostgreSQL/MySQL Handler passed in 287.938 seconds and Service in 5.502 seconds; disposable Compose resources were removed |
| Focused F21 PostgreSQL/MySQL export lifecycle | pass | Personal, Project, and platform authorization, filter parity, formula protection, redaction, bounds, filenames, and valid/invalid IDs passed in 73.853 seconds |
| Independent F21 review | pass | Final review reported zero critical findings, warnings, or suggestions after filter and Project query-binding fixes |
| Browser verification on F21 source | partial | Personal and platform pages, final right-aligned export actions, and live English/Chinese switching were visible; the in-app browser blocked direct download URLs, so download execution is evidenced by frontend behavior and authenticated HTTP lifecycle tests |
| Remote CI `36557525798` | pass | Frontend, backend, PostgreSQL/MySQL Integration, and artifact-build jobs passed for `dc99f35` |
| Remote Actionlint `36557525834` | pass | Workflow syntax passed for `dc99f35` |
| Remote GolangCI-Lint `36557525816` | pass | Go lint passed for `dc99f35` |
| `NODE_OPTIONS=--no-experimental-webstorage go tool task check` on F22-A source | pass | Backend formatting/vet/lint, Prettier, ESLint, TypeScript, and module checks passed; ESLint retained two existing Fast Refresh warnings |
| `NODE_OPTIONS=--no-experimental-webstorage go tool task test` on F22-A source | pass | Go race/unit coverage, 450 Vitest cases in 41 files, four Node checks, two development lifecycle checks, production build, and embedded assets passed |
| `go tool task test-integration` on F22-A source | pass | PostgreSQL/MySQL Handler passed in 361.029 seconds and Service in 6.971 seconds; disposable Compose resources were removed |
| Independent F22-A review | pass | Final review reported zero open critical findings, warnings, or suggestions after pre-attempt topology stripping and final-failover persistence fixes |
| Browser verification on F22-A source | pass | The platform Usage workspace exposed Provider distribution, retained the approved composition, and switched live between paired English/Chinese copy |
| Remote Actionlint `36561248224` | pass | Workflow syntax passed for `d24997a` |
| Remote GolangCI-Lint `36561248208` | pass | Go lint passed for `d24997a` |
| Remote CI `36561248202` | pass | Backend, frontend, PostgreSQL/MySQL integration, process restart, production assets, development lifecycle, and artifact build passed for `d24997a` |
| `NODE_OPTIONS=--no-experimental-webstorage go tool task check` on F26 source | pass | Backend formatting/vet/lint, Prettier, ESLint, TypeScript, and module checks passed; ESLint retained two existing Fast Refresh warnings |
| `NODE_OPTIONS=--no-experimental-webstorage go tool task test` on F26 source | pass | Go race/unit coverage, 458 Vitest cases in 42 files, four Node checks, two development lifecycle checks, production build, and embedded assets passed |
| `go tool task test-integration` on F26 source | pass | The complete race-enabled PostgreSQL/MySQL migration and handler matrix passed; the final Handler run completed in 335.306 seconds and disposable Compose resources were removed |
| `go tool task test-auth-lifecycle` on F26 source | pass | Both databases passed empty installation, process restart, persisted session, revocation, login, provider/model/Key, ordinary/streaming gateway, and call-fact coverage |
| Independent F26 reviews | pass | Final backend and frontend reviews reported zero critical findings, warnings, or suggestions after pruning, reconciliation, cleanup-audit, bounded-review, and uncertain-result fixes |
| Browser verification on F26 source | pass | The System Status workspace displayed authoritative online/offline registrations and actual jobs in the approved two-card composition and switched live between paired English/Chinese copy before restoring English |
| `go tool actionlint` and `git diff --check` on F26 source | pass | Workflow syntax and whitespace checks passed |
| Remote CI `36567593919` on `e6ca370` | pass | Frontend, backend, PostgreSQL/MySQL integration, real-process restart, production/development assets, and artifact builds passed |
| Remote Actionlint `36567593880` on `e6ca370` | pass | Workflow syntax passed |
| Remote GolangCI-Lint `36567593927` on `e6ca370` | pass | Go lint passed |
| Handoff receiver probe | pass | Referenced files and commits exist, RouteX and dotfiles remote `main` refs match the recorded checkpoints, the first resume action has an explicit command and completion condition, and no placeholder or secret is present |
| Full code/test suite for this handoff-only refresh | not run | No implementation code changed; final `e6ca370` remote CI and the recorded F26 local suites cover the transferred source |
| Focused F23 Go and frontend suites | pass | Notification migration, service, handler, SMTP, and overview tests passed; the overview suite contains 11 focused Vitest cases |
| Focused MySQL notification lifecycle | pass | The real MySQL migration and notification lifecycle suite passed in 78.877 seconds |
| `go tool task test-integration` on final F23 source | pass | The complete race-enabled PostgreSQL/MySQL matrix passed; the Handler package completed in 400.732 seconds and disposable Compose resources were removed |
| `go tool task test-auth-lifecycle` on final F23 source | pass | PostgreSQL and MySQL passed empty initialization, restart, session persistence, logout/login, encrypted catalog and Key confirmation, ordinary/streaming inference, call facts, and revocation |
| `go tool task test` on final F23 source | pass | Go race/unit coverage, 469 Vitest cases in 43 files, four Node checks, two development lifecycle checks, the production build, and embedded assets passed |
| `go tool task check` on final F23 source | pass | GolangCI-Lint reported zero issues; Prettier, TypeScript, module tidiness, and ESLint passed with only the two existing Fast Refresh warnings |
| `go tool actionlint` and `git diff --check` on final F23 source | pass | Workflow syntax and whitespace checks passed |
| Independent F23 reviews | pass | Final review found no production blocker after authoritative-read isolation, pruning recovery, canonical occurrence ordering, MySQL reserved-column portability, and SMTP ambiguity fixes |
| Built-in browser verification on final F23 source | pass | `/admin/overview`, the recipient-isolated notification menu, notification settings dialog, and live English/Chinese switching passed; English was restored and the verified page remained open |
| Focused final Provider-quality suites | pass | Database, entity, service, handler, command, Provider-quality UI, operations overview, and localization coverage passed after the final fixes |
| `go tool task test-integration` on Provider-quality source | pass | The complete race-enabled PostgreSQL/MySQL matrix passed; Handler completed in 387.019 seconds and Service in 5.492 seconds, and disposable Compose resources were removed |
| `go tool task test-auth-lifecycle` on Provider-quality source | pass | PostgreSQL and MySQL passed empty initialization, restart, session persistence, logout/login, encrypted catalog and Key confirmation, ordinary/streaming inference, call facts, and revocation |
| `go tool task test` on Provider-quality source | pass | Go race/unit coverage, 478 Vitest cases in 44 files, four Node checks, two development lifecycle checks, production build, and embedded assets passed |
| `go tool task check` on Provider-quality source | pass | Backend lint reported zero issues; Prettier, TypeScript, module tidiness, and ESLint passed with only the two existing Fast Refresh warnings |
| `go tool actionlint` and `git diff --check` on Provider-quality source | pass | Workflow syntax and whitespace checks passed |
| Independent Provider-quality reviews | pass | Final review reported zero critical findings, warnings, or suggestions after bounded overview, policy-generation, evaluator-isolation, and subject-snapshot fixes |
| Built-in browser verification on `b201b08` | pass with documented data boundary | A freshly restarted development process loaded the Provider list and operations overview, switched live between English and Chinese, exposed unread and complete notification history, and showed independent high/medium email controls before English was restored. The local database contained no Provider, so Provider-detail policy interaction remains covered by focused UI and authenticated API tests rather than this browser run. |

## Blockers, Risks, and Unknowns

- **Blockers:** no blocker prevents the next local package; external provider, enterprise identity, Vault, object-storage, mail, production, and capacity environments are not supplied for their acceptance gates.
- **Risks:** single-process quota evidence does not prove multi-node correctness; provider-specific media billing remains open; real-provider retry/error contracts and production capacity remain unverified; partial milestones must not be presented as full release acceptance.
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
| `internal/routex/database/call_provider_attribution_migration.go` | Frozen V26 Provider attribution migration and portability boundary |
| `docs/ROUTE_ATTEMPTS.md` | Active failover, evidence, health, quota, and diagnostics contract |
| `docs/CALLS.md` | Completed scoped call-query and safe CSV contract that supplies immutable facts to the next usage slice |
| `docs/USAGE.md` | Current usage scopes, filters, aggregation, decimal, and unknown-coverage behavior |
| `docs/SYSTEM_STATUS.md` | Delivered instance, lease, resource, job, cleanup, audit, and UI contract |
| `docs/NOTIFICATIONS.md` | Delivered alert grouping, recipient isolation, settings, SMTP delivery, retention, and failure-state contract |
| `docs/PROVIDER_QUALITY.md` | Immutable attempt attribution, bounded metrics, policy evaluation, alert transitions, and API contract |
| `internal/routex/service/notification*.go` | Durable occurrence projection, inbox/settings, delivery leasing, and source reconciliation |
| `internal/routex/service/system_instance.go` | Server-owned process-generation lifecycle and cleanup boundary |
| `internal/routex/service/system_job.go` | Allowlisted actual-job lifecycle, reconciliation, and retention boundary |
| `internal/routex/service/attachments.go` | Current-manager Project lifecycle and owner-scoped validated byte reads |
| `internal/routex/service/gateway*.go` | Native parsing, admission ordering, route selection, and dispatch boundaries |
| `/Users/miclle/dotfiles/projects/routex/implementation-plan.md` | Cross-task schedule, dependency, acceptance, and pause coordination entry |

## Resume Actions

1. Wait for the user to resume the objective; no implementation package is active at this checkpoint.
2. After resumption, synchronize RouteX and dotfiles `main`, verify both upstream SHAs, and preserve the unrelated dotfiles `zsh/.zshrc` modification.
3. Read `docs/IMPLEMENTATION.md`, `docs/PROVIDER_QUALITY.md`, `docs/NOTIFICATIONS.md`, this handoff, and the dotfiles roadmap before selecting the next bounded package.
4. Choose the next locally executable partial or unstarted capability from the authoritative roadmap. Keep external Provider and SMTP acceptance open until environments are supplied.
5. Preserve the same GORM-first dual-database, restart, permission, i18n, Mockup, browser, independent-review, phased-commit, push, and remote-CI gates before advancing again.

## Environment and Access

- **Required tools/services:** Go 1.27.1; module-managed Task, reflex, staticcheck, and actionlint; Node/npm from the project environment; Docker Compose; PostgreSQL 18 and MySQL 8.4 for integration.
- **Local-only configuration:** `cmd/routex/config.local.yaml`, optional `ROUTEX_HTTP_PORT`, `ROUTEX_VITE_PORT`, `ROUTEX_API_BASE_URL`, and `ROUTEX_VITE_DEV_SERVER_URL`; do not record secret values.
- **Current local development process:** RouteX was started on backend port `19000`, Vite port `15173`, and Compose PostgreSQL port `15433`. Verify health rather than assuming the process survived the handoff.
- **Access needed:** dedicated provider credentials and spending authorization, IdP/LDAP/OAuth resources, Vault, external S3/SMTP test environments, price-source contract, and production deployment access for their respective acceptance gates.
- **Setup caveats:** default ports `9000` and `5173` were occupied by unrelated local services; do not terminate unrelated processes. Use explicit alternative ports when necessary.

## Handoff History

- **Continues from:** storage administration at `26763e8`, provider-model input capabilities at `3b5e642`, personal-Key attachment resolution at `652cb44`, single-model attachment input at `7fa25d7`, comparison attachment input at `ccb6255`, conservative multimodal quota admission at `aa33b5a`, Project-owned attachment lifecycle at `1e4af76`, attachment input pricing at `0c445e1`, bounded native failover at `17358de`, safe call-record CSV export at `dc99f35`, immutable Provider usage attribution at `d24997a`, authoritative System Status at `6213cc7` plus `fa71c2e`, durable operational notifications at `d5ae09f` plus `e5f189c`, and Provider quality at `9ef78f6` plus `b201b08`
- **Supersedes:** the storage-administration-next checkpoint at `bcc663a`
- **Closeout condition:** all F01–F30 capabilities and A01–A20 acceptance cases are completed with current evidence, or a later handoff replaces this document with an equally verifiable resume point
