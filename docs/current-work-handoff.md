# Current Work Handoff

- **Status:** active; implementation paused by user instruction
- **Updated:** 2026-09-24T14:46:19+08:00
- **Repository:** `/Users/miclle/github/miclle/routex`
- **Branch:** `main`
- **Base:** `main`
- **Implementation HEAD:** `d6863aaf86076474e6272b82e8a25106b12c41a1`
- **Upstream:** `origin/main`, zero commits ahead and zero behind at capture
- **Last pushed implementation state:** `d6863aaf86076474e6272b82e8a25106b12c41a1`
- **Current owner:** Codex documentation checkpoint
- **Next owner:** unassigned
- **Transfer state:** transferable after the documentation commit is present on `origin/main`
- **Transport:** `origin/main`; resolve the exact handoff commit with `git log -1 -- docs/current-work-handoff.md`
- **Receiver access:** RouteX repository, this document, `docs/IMPLEMENTATION.md`, and the cross-repository roadmap described below

## Objective

Implement every valid RouteX capability represented by F01–F30 and close every A01–A20 acceptance case, while preserving the existing Go/React architecture, PostgreSQL/MySQL portability, GORM-first migrations, the approved product layout, shadcn/ui and Base UI primitives, English project documentation, bilingual English/Chinese UI copy, phased verification, and incremental main-branch delivery. This handoff records the deliberate pause after the storage backend delivery; it does not authorize another implementation package.

## Current State

### Completed

- The Compose development and isolated PostgreSQL/MySQL test foundation is delivered.
- F01 local initialization and identity, F02 account security, F08 Personal/Project Key lifecycle, F09 current single-node admission controls, F16 currency and immutable price snapshots, and F25 site presentation/announcements meet their current capability definitions.
- Four native inference protocols, durable call facts, usage interfaces, Playground conversation/comparison, executable examples, managed egress, price maintenance, quotas, SMTP administration/test delivery, and the storage/owned-attachment backend have substantial delivered foundations.
- The latest implementation commit is `d6863aa` (`feat(storage): add owned attachment backend`). Its CI, PostgreSQL/MySQL integration, artifact build, Actionlint, and GolangCI-Lint runs passed.
- `docs/IMPLEMENTATION.md` contains the authoritative per-capability and per-acceptance status snapshot. The cross-task roadmap is `/Users/miclle/dotfiles/projects/routex/implementation-plan.md`.

### In Progress

- F04–F07, F10–F15, F17–F23, F27, F28, and F30 are partially completed. Their exact delivered boundaries and remaining conditions are listed in `docs/IMPLEMENTATION.md`.
- A02–A12, A14–A15, and A17–A20 have partial controlled evidence but are not fully accepted.
- F14 has two unresolved production-safety findings: changing a proxy endpoint must not allow saved credentials to be retained and sent to the new endpoint, and proxy tunneling must try later validated DNS addresses when the first address cannot connect.
- F13 has a replay-safe route-attempt foundation, but active retry, health routing, and failover are not wired into gateway execution.
- F27 delivers the storage backend and owned attachment APIs only. Storage administration and attachment web interfaces were not started.

### Not Started or Out of Scope

- F03 enterprise identity, F24 AI operations analysis, F26 instance/system-job management, and F29 API Key Vault delivery are not started.
- A13 complete approval contention and A16 Vault compensation are not started.
- Real external-provider, IdP/LDAP/OAuth, Vault, S3, SMTP, price-source, production deployment, backup/restore, multi-node, and measured-capacity acceptance remain open because the required environments or decisions have not been supplied.
- Work that had not started is paused. No implementation task is active and no new package may begin until the user resumes the roadmap.

## Working Tree

- **Staged:** none after the documentation delivery commit
- **Modified:** none after the documentation delivery commit
- **Untracked:** none after the documentation delivery commit
- **Unpushed commits:** none after the documentation delivery commit is pushed
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
| Keep the roadmap paused | The user explicitly stopped unstarted work | Documentation and read-only inspection are allowed; implementation requires an explicit resume instruction |

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

## Blockers, Risks, and Unknowns

- **Blockers:** implementation is paused by user instruction; external provider, enterprise identity, Vault, object-storage, mail, production, and capacity environments are not supplied.
- **Risks:** the two F14 egress findings must be fixed before production use; single-process quota evidence does not prove multi-node correctness; storage has no administration or attachment UI; partial milestones must not be presented as full release acceptance.
- **Unknowns:** production topology, multi-node requirement, initial provider, spending limit, capacity targets, backup/restore procedure, IdP choices, Vault layout, price-source contract, and AI-analysis scope remain undecided or unverified.

## Files to Read First

| Path | Why it matters |
|---|---|
| `docs/IMPLEMENTATION.md` | Authoritative F01–F30 and A01–A20 status, evidence, and remaining boundaries |
| `docs/ARCHITECTURE.md` | Gateway, Control Plane, and Data Platform ownership boundaries |
| `AGENTS.md` | Mandatory architecture, migration, UI, localization, formatting, and verification rules |
| `docs/EGRESS.md` | Current managed-egress contract and safety boundary |
| `internal/routex/service/egress.go` | Saved-auth endpoint binding and management behavior implicated by the open finding |
| `pkg/upstream/egress.go` | Proxy dialing logic implicated by the multi-address finding |
| `docs/STORAGE.md` | Delivered backend boundary and explicitly missing web interfaces |
| `/Users/miclle/dotfiles/projects/routex/implementation-plan.md` | Cross-task schedule, dependency, acceptance, and pause coordination entry |

## Next Actions

1. After the user explicitly resumes implementation, run `git status --short --branch`, `git rev-parse HEAD`, `git rev-list --left-right --count HEAD...@{upstream}`, and read `docs/IMPLEMENTATION.md`; continue only when the repository and pause inventory are reconciled.
2. Use `internal/routex/service/egress.go` and `pkg/upstream/egress.go` as the first bounded safety package: reject saved-auth retention across endpoint changes and try every validated target address before reporting connection failure. Completion requires focused race tests, current egress HTTP/service tests, `go tool task check`, and relevant PostgreSQL/MySQL integration coverage.
3. Update `docs/EGRESS.md`, `docs/IMPLEMENTATION.md`, and this handoff with the exact delivered boundary and verification. Commit and push only under the session's active authorization, then verify remote CI before selecting another package.

## Environment and Access

- **Required tools/services:** Go 1.27.1; module-managed Task, reflex, staticcheck, and actionlint; Node/npm from the project environment; Docker Compose; PostgreSQL 18 and MySQL 8.4 for integration.
- **Local-only configuration:** `cmd/routex/config.local.yaml`, optional `ROUTEX_HTTP_PORT`, `ROUTEX_VITE_PORT`, `ROUTEX_API_BASE_URL`, and `ROUTEX_VITE_DEV_SERVER_URL`; do not record secret values.
- **Current local development process:** RouteX was started on backend port `19000`, Vite port `15173`, and Compose PostgreSQL port `15433`. Verify health rather than assuming the process survived the handoff.
- **Access needed:** dedicated provider credentials and spending authorization, IdP/LDAP/OAuth resources, Vault, external S3/SMTP test environments, price-source contract, and production deployment access for their respective acceptance gates.
- **Setup caveats:** default ports `9000` and `5173` were occupied by unrelated local services; do not terminate unrelated processes. Use explicit alternative ports when necessary.

## Handoff History

- **Continues from:** no earlier repository-local handoff
- **Supersedes:** none
- **Closeout condition:** all F01–F30 capabilities and A01–A20 acceptance cases are completed with current evidence, or a later handoff replaces this document with an equally verifiable resume point
