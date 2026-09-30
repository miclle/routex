# Current Work Handoff

- **Status:** selected quota and Provider interface package delivered; implementation paused at a verified local checkpoint, remote checkpoint verification pending
- **Updated:** 2026-09-30T17:16:00+08:00
- **Repository:** RouteX
- **Branch / base:** `main`
- **Verified implementation HEAD:** `ce79699a4ca1a287dd6f07279fbb344bac2817b9`
- **Pushed implementation commits:** `6d41bf6` (`feat(limits): expose scoped budget currency`), `c402ad0` (`feat(quotas): add policy and capacity controls`), `a3e9c2a` (`feat(quotas): add installation calendar`), `ce79699` (`feat(providers): filter credential pools`)
- **Current owner:** no implementation worker is active; coordinating task verifies the remote checkpoint before ending the window
- **Transfer:** `origin/main`; resolve the final checkpoint using `git log -1 -- docs/current-work-handoff.md`
- **Coordination plan:** `~/dotfiles/projects/routex/implementation-plan.md`; synchronize dotfiles separately and preserve unrelated changes

## Objective and authorization

Implement every valid F01–F30 capability and close every A01–A20 acceptance case. Preserve the Go/React architecture, Gateway/Control Plane/Data Platform boundaries, PostgreSQL/MySQL portability, immutable GORM-first migrations, approved layout, shadcn/ui and Base UI, bilingual English-default interfaces, English documentation and commits, parallel ownership, tests, and phased main-branch commits and pushes.

The latest user authorization is a two-hour local window on 2026-09-30, from approximately 15:47 to 17:47 Asia/Shanghai. The selected partial-capability package covers existing Personal/Project/Key quota interfaces (F04/F17), Provider-model capacity attestations (F11), the installation quota calendar (F17), and credential-pool filtering (F11). Partial delivery does not complete the overall objective.

## Current delivery

| Package | Implementation and boundary |
| --- | --- |
| Resource denomination | `6d41bf6` exposes `platform_currency` through already authorized limits reads. Members need no administrator price access. Real PostgreSQL/MySQL tests cover all four resource paths, currency changes, and cross-resource denial. |
| Quota policies and usage | `c402ad0` adds rolling five-hour/seven-day tokens, monthly tokens and exact-decimal budget, TPM, stored/effective/parent values, and server-owned quota windows. Keep zero distinct from null, unknown history distinct from zero usage, historical currency maps intact, and remaining allowance uninferred. Complete-policy writes preserve every control, reason, ETag, and retry intent. |
| Capacity attestation | `c402ad0` adds a Provider-model detail card and Base UI dialog with positive input/output maxima, evidence, reason, protocol, and reviewed revisions. Configured means saved only; the read contract cannot establish overrun validity or gateway eligibility. Conflicts require review; uncertain writes retry the original request. |
| Credential pool inspection | `ce79699` adds compact conjunctive name/Connection/verification/enabled filters to the existing Credentials table, resets filters across Providers, and renders recorded nullable verification timestamps using the selected language. Existing exact-ID actions and permissions remain intact. |
| Installation calendar | `a3e9c2a` adds a Quota calendar card to System information, independent read/write authority, server-owned freeze status, exact revision/reason, explicit confirmation, preserved drafts and identical retries. Calendar-only writers do not mount the site editor or initiate its page query; public site presentation remains available in the shell. The server rejects host-dependent `Local` without changing legacy read/runtime handling. |

The calendar is an installation-scoped contract extension within the existing settings composition. It does not invent a Team/default-policy workspace. No new migration, dependency, or native upstream request is introduced by this package.

## Overall status

`docs/IMPLEMENTATION.md` remains the authoritative inventory: **8 completed, 19 partially completed, 3 not started**. This is a binary completion count, not an effort percentage. A01 is fully accepted; A02–A12, A14–A15, and A17–A20 have partial evidence; A13 and A16 are not started.

Delivered foundations include local identity/MFA, durable sessions and Keys, governance and Team/Project management, four native protocols and bounded same-protocol failover, immutable call/price/Provider-attempt facts, safe CSV and price workbook workflows, currency administration, scoped usage, managed egress, durable quotas, owned image/PDF attachments and Playground compositions, SMTP configuration, storage administration, site/announcements, authoritative System Status, and operational/Provider-quality notifications. Their complete contracts and external boundaries remain in the domain documents and implementation index.

F11 remains partial for real-provider acceptance and complete pool operations. F17 remains partial for Team/default rules, templates, approvals, configurable stop policy, and quota alerts. Explicit Team invocation context is still absent; do not add Team attribution or debit by guessing from membership. Enterprise identity, Vault, distributed enforcement, external price synchronization, saved reports, AI analysis, backup/restore, and final production acceptance remain open.

## Verification

| Check | Result and boundary |
| --- | --- |
| Exact resource-contract snapshot `go tool task check` / `go tool task test` | Passed before `6d41bf6`; 478 Vitest cases plus Go race, Node, development lifecycle, and production assets. |
| Exact quota/capacity snapshot `go tool task check` / `go tool task test` | Passed before `c402ad0`; 500 Vitest cases in 45 files plus Go race, Node, lifecycle, and embedded assets. |
| Resource-contract full database matrix | Passed on PostgreSQL/MySQL; Handler 549.579 seconds, Service 10.187 seconds; disposable containers/network removed. |
| Final combined `go tool task check` | Passed; no lint errors, two existing Fast Refresh warnings. |
| Final combined `go tool task test` | Passed; 531 Vitest cases in 48 files, Go race/unit, four Node checks, development lifecycle, production build/assets. |
| Final combined database matrix | Passed separately on PostgreSQL/MySQL; Handler 459.145 seconds, Service 6.000 seconds, with stable timezone rejection, unchanged accounting state, attachment settlement, and complete migration/lifecycle coverage; disposable containers/network removed. |
| Independent reviews | Quota/capacity: no remaining finding. Calendar: rejected uncertain retry bug fixed; final review has no remaining finding. |
| Browser quota workflow | Member Settings reproduced the inline editor, preserved a live bilingual draft, and saved tokens/TPM plus `12.500000000000000001 USD` without rounding in an isolated database. |
| Browser capacity workflow | An isolated fixture saved explicit Chat capacity 128000/16384 with controlled evidence/reason. No external verification or inference occurred. |
| Browser calendar workflow | An isolated fixture confirmed and saved Asia/Shanghai, displayed runtime-application confirmation, switched live English/Chinese, and restored English. |
| Remote verification for `c402ad0` | CI `36689900695`, Actionlint `36689900904`, and GolangCI-Lint `36689900833` passed; CI includes dual databases, authentication restarts, frontend/backend, production assets, development lifecycle and build artifacts. Superseded `6d41bf6` CI was cancelled by its next push; its completed jobs are not a complete CI pass. |

Verification caveats:

- Earlier Node 26 snapshots used `NODE_OPTIONS=--no-experimental-webstorage` for jsdom compatibility. The shell later resolved Node 24.21.0. Final combined checks passed on installed Node 22.23.2, matching the CI Node 22 major (CI pins 22.22.0).
- The first local database run failed with truncated diagnostics; an unchanged-source rerun passed, but its original cause was not established. An isolated quota/capacity Vitest run also timed out in a price-import test and cascaded; its full rerun passed all 500 cases. Neither earlier failure is claimed diagnosed or fixed.
- The first calendar matrix reached Go's default ten-minute package alarm while a newly started unit test had run for zero seconds. The integration entry now uses a finite 20-minute package bound, preserving all assertions and race checks.
- Simultaneous final Vitest testing timed out in existing audit/governance/egress cases and cascaded. A separate Node 24 run passed 530/531 with one existing System Status five-second timeout. The unchanged implementation passed all 531 on Node 22.23.2; no production timeout or assertion was relaxed.
- The first 20-minute database run failed at MySQL attachment-fixture runtime startup, before recorder or settlement. Its generic error did not distinguish deadline expiry from invalid configuration. A bounded, sanitized test-only diagnostic now records startup elapsed and publication status/error code on recurrence. The complete isolated matrix passed afterward. Contention is plausible, not a proven root cause or repaired production defect.

Final-source and remote results are separate evidence. Refresh the latest checkpoint's CI state at resumption; a cancelled superseded run is not a complete pass.

## Decisions and constraints

- Keep scoped authority and query keys; no administrator catalogue fetch solely to render a member quota currency.
- Preserve exact decimal strings, safe whole-token values, full-policy replacement, parent narrowing, and native authoritative usage.
- Configured capacity is an attestation, never a proof from discovery or a successful sample call.
- A failed retry cannot resolve an earlier unknown publication outcome. Preserve the captured body/ETag until successful reconciliation or explicit current-state review.
- Calendar boundaries are server-owned and freeze at first accounting intent. Changing a calendar cannot reset usage.
- External paid Provider, SMTP, IdP/LDAP, Vault, S3, price-source and production acceptance require their corresponding supplied environments; no external credentials or spending authorization have been supplied.

## Resume actions

1. The selected package is checkpointed and implementation is paused. Start another package only after the user resumes the objective; completing this window does not complete RouteX.
2. Synchronize RouteX and dotfiles `main`, verify upstream SHAs, and preserve unrelated dotfiles `zsh/.zshrc` changes.
3. Read this handoff, `docs/IMPLEMENTATION.md`, `docs/QUOTAS.md`, and the coordination plan. Historical evidence is not a current runtime guarantee.
4. Reassess the next bounded local package: credential metadata editing (name/priority) is an observed missing Provider action, but needs a real service/API contract, audit, concurrency/publication behavior, dual-database tests, and the existing dialog grammar. Do not fold secret replacement, deletion, or external validation into a metadata-only package. Team quotas/defaults require explicit invocation/debit contracts; real-provider and external-mail acceptance require supplied environments.
5. Continue independent file ownership, GORM-first dual-database validation, scoped permission/i18n/browser evidence, English docs, checked commits, pushes, and remote CI.

## Files to read first

| Path | Purpose |
| --- | --- |
| `AGENTS.md`, `.agents/rules/frontend.md` | Mandatory architecture, UI, localization, formatting and verification rules |
| `docs/IMPLEMENTATION.md` | Complete F/A inventory and unfinished boundaries |
| `docs/QUOTAS.md`, `docs/QUOTA_LEDGER.md` | Policy, calendar, capacity, admission and unknown-history contracts |
| `docs/RESOURCE_LIMITS.md` | Resource ownership and complete-policy replacement |
| `docs/PROVIDER_MODELS.md`, `docs/PROVIDER_QUALITY.md` | Provider-model capabilities and immutable attempt quality |
| `docs/NOTIFICATIONS.md` | Existing operational source and delivery boundaries before adding quota alerts |
| `website/src/views/resource-limits/` | Shared quota editor, usage rendering and exact-decimal narrowing |
| `website/src/views/pricing/provider-model-capacity.tsx` | Capacity read/editor/conflict/retry behavior |
| `website/src/views/site/quota-calendar.tsx` | Calendar read/write, confirmation, freeze and retry behavior |
| `internal/routex/service/quota_settings.go` | Durable calendar validation and publication boundary |
| `~/dotfiles/projects/routex/implementation-plan.md` | Cross-task scope, dependencies, acceptance and evidence |

## Environment and cleanup

Go 1.27.1, module-managed tools, project Node/npm, Docker Compose, PostgreSQL 18 and MySQL 8.4 are required. Secrets remain in ignored local configuration or process environment; never put them in this handoff.

The development service uses backend `19000`, Vite `15173`, and Compose PostgreSQL `15433`. Check `/health` rather than assuming a process survived transfer. Default ports belong to unrelated local services; never kill them to free ports.

Browser write verification uses a separately created disposable PostgreSQL database and localhost backend `19001`; it sends no real upstream requests and leaves the user's development database unchanged. The owned fixture service/database/tab and both validation worktrees have been removed. The development service/database remain available. Older unrelated/prunable worktree registrations are outside this cleanup.

This checkpoint continues from Provider quality at `9ef78f6` / `b201b08` and its documentation checkpoint `d8b4d1c`. Historical detailed verification remains recoverable from that handoff revision. Completion of the selected package does not complete F01–F30 or A01–A20.
