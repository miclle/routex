# AGENTS.md

Technical specification for AI coding assistants working on this project.

## Project Overview

RouteX is an AI gateway and control plane, built as a Go + React single-page application that compiles into a single binary. The backend embeds frontend build output via `//go:embed`, so production deployment requires only one executable plus a database.

The current implementation includes persistent administrator setup, local authentication, revocable sessions, a protected workspace, personal and Project Keys, four native inference protocols, immutable call metering, single-node quota enforcement, and user/Project-owned attachments. Product domains and extension boundaries are defined in `docs/ARCHITECTURE.md`; immutable runtime publication, bounded durable call buffering, and governance backends are available. Encrypted provider credentials and stable model catalogs/grants are available. Phase scope and acceptance evidence are tracked in `docs/IMPLEMENTATION.md`. Preserve the Gateway, Control Plane, and Data Platform boundaries as features are added.

## Tech Stack

- Backend: Go 1.27.1 + `fox-gonic/fox` + GORM + PostgreSQL (default) / MySQL
- Frontend: React 19 + TypeScript 6 + Vite 8 + Tailwind CSS 4 + shadcn/ui + Base UI
  - React Router v8 (routing)
  - React Query v5 (server state)
  - Axios (HTTP client)
  - Lucide React (icons)

## Development Commands

```bash
go tool task install        # Install backend and frontend dependencies
go tool task db:up          # Start Compose PostgreSQL (db:mysql for MySQL)
go tool task dev            # Start development environment (hot reload)
go tool task build          # Build production binary (with embedded frontend)
go tool task build-all      # Cross-compile for multiple platforms
go tool task run            # Run in production mode
go tool task lint           # Auto-fix code style and run checks
go tool task check          # Full checks (backend + frontend types + mod tidy)
go tool task test           # Go, frontend, dev lifecycle, and production asset tests
go tool task test-integration # Isolated PostgreSQL/MySQL migration and auth tests
go tool task test-auth-lifecycle # Real-process restart and session persistence
go tool task test-production # Build assets and test production asset serving
go tool task clean          # Remove build artifacts
go tool task update-tools   # Install GolangCI-Lint if missing
go tool actionlint          # Validate GitHub Actions workflows
```

Task, reflex, staticcheck, and actionlint are versioned as Go tool dependencies in `go.mod`. Invoke them with `go tool task`, `go tool reflex`, `go tool staticcheck`, and `go tool actionlint`; do not rely on globally installed versions. GolangCI-Lint is installed in the ignored checkout-local `bin/tools/` directory at the version specified in `scripts/install-tools.sh` and `.github/workflows/golangci-lint.yml`; checks never depend on a mutable global linter installation.

## Directory Overview

```text
cmd/routex/                          # Application entry point and local config
internal/routex/config/              # YAML config loading (PostgreSQL / MySQL)
internal/routex/database/            # GORM database connection and schema migration
internal/routex/entity/              # Data models and domain types
internal/routex/handler/             # HTTP handlers, route registration, middleware
internal/routex/service/             # Business logic, database operations
internal/routex/errors/              # Centralized error types
pkg/httperr/                         # Generic HTTP-status-aware errors
pkg/id/                              # Prefixed ULID helpers
pkg/secret/                          # Random secret and digest helpers
pkg/strutil/                         # Pure string helpers
pkg/gormlog/                         # GORM logger adapter
website/                             # Embedded SPA (frontend + go:embed glue)
  ├── assets_development.go   #   Dev mode: reverse-proxy to Vite dev server
  ├── assets_production.go    #   Prod mode: go:embed static assets
  ├── package.json
  ├── vite.config.ts
  ├── tsconfig*.json
  ├── eslint.config.js
  ├── vitest.config.ts
  ├── components.json         #   shadcn configuration
  ├── index.html
  ├── public/
  ├── build/                  #   Vite build output (embedded)
  └── src/
      ├── main.tsx
      ├── App.tsx
      ├── router.tsx
      ├── globals.css
      ├── api/
      ├── types/
      ├── views/
      ├── components/
      │   ├── app/             #   App shell and product-level composition
      │   └── ui/              #   shadcn-style primitives and Base UI wrappers
      ├── hooks/
      ├── i18n/                 #   English/Chinese catalogs, initialization, and locale tests
      ├── context/
      └── lib/
scripts/                      # Shell helpers invoked by Taskfile (build, check, tooling)
```

## Core Architecture Constraints

### Backend

- Follow the `Handler -> Service -> Entity` layering
- Preserve exact Credential and published snapshot IDs on each internal call attempt, including failed and interrupted attempts. Legacy missing attribution stays unknown; never substitute the logical call's final route or current catalog. Keep public call DTOs separate, and do not treat HTTP success or usage completeness as native completion proof. Parser-owned `NativeCompletionEvidence` uses exact unknown/completed/handoff/blocked/incomplete values; legacy blanks remain unknown, and terminal evidence never overrides final attempt status.
- Register all routes in `internal/routex/handler/handler.go`
- Keep database connection and versioned migration setup in `internal/routex/database/`; services receive a ready `*gorm.DB`. Add new immutable migration versions instead of editing released steps or using current entities for startup AutoMigrate.
- PostgreSQL (default) and MySQL are supported; switch via `driver` in YAML config
- YAML config contains only bootstrap settings (address, database driver, connection string, credential root key, and upstream network policy)
- Configuration files may reference environment variables with `${NAME}` or `${NAME:-fallback}`
- Expand environment variables after parsing YAML so values cannot alter configuration syntax

### Database portability and migrations (mandatory)

- Use GORM model tags and `Migrator` APIs first for schema changes, including tables, columns, indexes, and foreign keys. Use GORM query builders and transactions for ordinary persistence.
- Keep numbered migrations immutable after release. Each new version uses private, frozen schema structs; do not use evolving business entities as historical migration definitions. `AutoMigrate` is allowed only against an explicitly bounded frozen schema when its behavior is appropriate; never run it unconditionally against every current entity at startup.
- Services, handlers, and domain entities must not branch on database dialects or import database drivers. Keep connection setup, error translation, collation compatibility, locking, and unavoidable dialect adapters inside `internal/routex/database/`.
- Enable GORM error translation and handle portable errors such as `gorm.ErrDuplicatedKey` in business code. Do not inspect PostgreSQL SQLSTATE or MySQL numeric error codes in services.
- Handwritten SQL is an exception for capabilities GORM cannot correctly express. Document the limitation and rationale beside the database-layer implementation. Parameterize values and cover every supported database; performance-driven exceptions require measured evidence.
- Every migration must pass empty-database creation, existing-data upgrade, repeat execution, concurrent startup, and relevant constraint/index tests on real PostgreSQL and MySQL. Account for partially applied MySQL DDL; never rely on transactional DDL rollback across drivers.
- Released SQL migrations remain unchanged. They are historical compatibility records, not a template for new migrations. See `docs/DATABASE.md` for the policy and current justified exceptions.

### Frontend

- Treat the approved product Mockup as the layout and interaction specification. Reproduce its navigation, page composition, tables, drawers, forms, spacing, and interaction hierarchy; do not invent an alternative layout during implementation.
- Implement that design using local shadcn/ui components and Base UI wrappers. Do not add Ant Design/antd as a dependency or copy its runtime components.
- Preserve the specified UI while binding real RouteX APIs and permissions. Any necessary divergence must be explained by a concrete domain/security contract and documented, rather than treated as permission to redesign the page.

- Routing: React Router v8
- Server state management: React Query (`@tanstack/react-query`)
- API calls go in `website/src/api/`
- Type definitions go in `website/src/types/`
- Pages go in `website/src/views/`
- App-wide layout/composition belongs in `website/src/components/app/`
- Reusable UI primitives belong in `website/src/components/ui/`
- Prefer local shadcn-style primitives, Tailwind tokens, Lucide icons, and Base UI wrappers over one-off markup
- Authentication pages use `/setup` and `/login`; `AuthGate` protects the application shell. Session and setup state are React Query resources; cookies remain HttpOnly and CSRF tokens stay in memory.
- `/account` updates the profile; `/account/security` rotates passwords and sessions and revokes individual sessions. Password changes replace cached session/CSRF data; revoking the current session clears private caches and returns to login.
- `/playground` invokes native inference with a transient personal or Project Key, without browser storage or mutation-cache persistence. `/calls` and `/admin/calls` use separate list/detail query keys and permission boundaries.
- Catalog routes include `/admin/providers`, `/admin/models`, `/models`, and `/keys`. Administrative views use `AdminOnly` in addition to server authorization; the app navigation follows the session role.
- Planned Personal/Project Key retirement requires a persisted exact-owner replacement call whose successful last attempt carries native `completed` evidence after Key creation. HTTP/call success, known usage, delivery, handoff, blocking, truncation and unknown history are insufficient. Preserve historical completed-retirement retries and current authorization; UI guidance reflects the server gate without inferring eligibility.
- One-time Key secrets stay only in component state until confirmed or revoked; never return secrets from a React Query mutation into its cache. Local `Dialog` wraps Base UI for modal focus and keyboard behavior.
- Use the local Base UI `Input` wrapper for form controls, with labels, autocomplete, validation, and pending states.
- Wrap Base UI headless components in local `components/ui/*` modules before using them from pages
- `AppShell` owns the 248px desktop sidebar, 80px collapsed state, mobile navigation drawer, 64px page bar, workspace/management navigation, account menu, and recipient-scoped notification menu. Local `Table`, Base UI `Drawer`, and Base UI `Menu` primitives support list/detail workflows; `Menu` exposes side/alignment options for both sidebar and header placement.
- Governance uses effective permission queries for page gates and navigation, member detail tabs, role permission dialogs, and a registration configuration drawer. `Switch` wraps Base UI.
- Provider credential tables retain the existing add/verify/toggle workflow and use compact conjunctive metadata filters. Search names literally, reset filters on Provider change, and render only recorded verification timestamps with the selected language. Keep read and write permissions independent and every mutation bound to the exact credential ID; never expose secret fragments or invent pool statistics.
- Credential metadata editing uses the existing Credentials table and a local Base UI dialog for name, priority, and reason. Read its resource-scoped metadata endpoint and submit the reviewed strong If-Match. Preserve conflict drafts and the exact request through uncertain publication retries. A matching current target reconciles saved name/priority and runtime publication only; it does not prove the original historical operation. Never edit or display secret material, enablement, verification, or discovery coverage through this dialog.
- Credential deletion uses the same row action menu and a Base UI danger confirmation with reviewed metadata and a required reason. Never remove a row optimistically or treat metadata GET 404 as success. Only the exact authorized DELETE response confirming current absence and runtime application completes the workflow. Preserve uncertain intent on every failed retry; retain already dispatched calls and immutable history. Deleting the last ready credential may leave its routes unavailable without changing weights or introducing fallback credentials.
- Credential retirement readiness is a read-only advisory review in the existing replacement row menu and Base UI dialog. Show only server-confirmed current configuration, bounded eligible route count, native completion and localized blockers. It changes no credential and claims no completed retirement or fleet application. Clear prior eligibility during refresh/error and keep read/write permissions independent; unknown blocker codes use localized generic guidance.
- Credential replacement preparation uses the existing row menu and local Base UI dialog. Create a new linked pending disabled record with reviewed source If-Match, a stable UUIDv4 intent, new transient secret and required reason; inherit Connection/priority and preserve the predecessor. Retain exact uncertain creation intent and never clear it by reviewing the source. A saved receipt is not routing application or completed rotation. Reuse real Verify and explicit Enable separately; never auto-disable the predecessor or infer new-Credential inference evidence.
- Provider and model detail pages use resource URLs. Profile and security settings use separate routes; inference configuration and conversation remain inside one split workbench.
- Provider detail defaults to the addressable Overview and preserves the Overview, Connections, Credentials, Models, and Settings hierarchy. Quality cards use immutable attempt facts and full-attempt duration; never infer Provider attribution from a final logical call or mutable catalog record. Policy controls require independent `system.read`/`system.write` gates, an exact reviewed ETag, and a non-empty reason. Preserve drafts through explicit conflict review.

### Single Binary Embedding

- `website/assets_development.go` (`//go:build development`) reverse-proxies to Vite dev server
- `website/assets_production.go` (`//go:build !development`) serves assets via `//go:embed build/*`
- NotFound handler: `/api`, `/v1`, and `/v1beta` API paths return JSON 404; other GET/HEAD routes fall back to the SPA index
- `go tool task test` covers development and production asset serving; CI runs production tests after building frontend assets
- Development services run under Task with fail-fast cancellation; never scan and kill unrelated processes to free ports
- Keep Vite Host validation enabled; permit custom development hostnames explicitly

## Mandatory Rules

- Describe RouteX independently. Keep competitor comparisons and implementation research outside the project repository; preserve any required third-party licensing notices.

- Write documentation, commit titles/bodies, and PR descriptions in English.

- Respect the existing layering and directory structure; do not reshape architecture for local changes
- Run `go tool task check` before committing
- When changing frontend structure or UI primitives, update this file and `.agents/rules/frontend.md` together

## Pre-commit Checklist

- Run `go tool task check`; do not commit if it fails
- Verify whether frontend API calls or types need to be updated accordingly
- For UI behavior or primitive changes, run the focused Vitest tests or `cd website && npm test`
- For schema, transaction, or authentication changes, run `go tool task test-integration`; identity persistence changes also run `go tool task test-auth-lifecycle`.

## Internationalization and Formatting

- Use `i18next` and `react-i18next` for every visible label, validation message, status, empty state, and accessible name. Supported languages are `en` and `zh`; English is the default regardless of browser locale.
- Keep paired English and Chinese catalogs in `website/src/i18n/locales/`. Register each namespace in `website/src/i18n/index.ts`. Shared/auth/Key/Playground copy uses `common`; catalog, governance, and account/calls use `catalog`, `governance`, and `activity` respectively.
- Use interpolation and plural forms instead of concatenating translated fragments. Render notices from translation keys so switching language updates existing messages without clearing user input. Format dates with the selected language; preserve user content, identifiers, protocol values, and sanitized server messages.
- `LanguageSwitcher` is available on authentication surfaces and in the app header. Persist only the non-sensitive `routex.language` preference. Browser storage failures must not prevent switching. Never persist credentials, session/CSRF tokens, or Playground content.
- Keep default-English workflow tests, live language-switch tests, key/interpolation parity, and missing-translation coverage passing when copy or namespaces change.
- Run `npm --prefix website run format` or `npm --prefix website run lint:fix` before each implementation phase is submitted. Prettier owns layout formatting; do not hand-compress JSX or add broad formatter ignores. `go tool task check` enforces formatting, ESLint, and types before commits.

## Team and Project Interfaces

Team and Project pages use separate scoped and global routes. Personal lists never fetch the global directory. Administrative list navigation requires `teams.read_all` or `projects.read_all`; resource detail authority is enforced by the server. Team owners receive scoped read access only. Current Project managers may edit metadata and manager assignments, while lifecycle and model changes require their explicit platform permissions.

Resource lists retain compact filters, tables, and action menus. Details use addressable tabs; Team members use identity/status rows and action menus, model access uses allowed/available tables, and Project settings include the manager table. Candidate pickers call only the authorized resource-specific endpoint. Existing selections outside a bounded search response remain selected. Unknown model names render stable IDs without querying an unauthorized catalog.

Project call tables reuse the call-record component with Project-specific endpoints and cache keys; they do not render administrator diagnostics or personal history. Metadata, continuity conflicts, terminal archival, and complete model/relationship replacements remain real API operations. New resources receive no implicit model grants. UI copy is paired in the `resources` namespace and follows the same English-default localization contract.

Provider model price editing belongs in the provider-model detail view. Reuse the
local price table and Base UI dialog wrapper, preserve decimal strings through
API submission, and require explicit conflict review before replacing an ETag.

Project model requests belong inside Resource configuration. Keep scoped history, additions-only submission, current-manager checks, and independent reviewer permissions in `views/project-requests`; register paired `projectRequests` translations. Pending requests never change effective grants.

Platform currency settings use the dedicated complete-catalogue currency metadata endpoint. Keep a coherent reviewed ETag, currency, and required-currency snapshot; preserve drafts but block submission when a newer generation appears until explicit review. Preserve decimal strings and fixed self-conversion, and confirm configuration changes before dispatch.

Price file maintenance belongs in `views/price-imports` at `/admin/prices`, with download/upload steps and a separate server-derived difference preview. Apply only the captured UTF-8 CSV or original XLSX/XLS filename and base64 bytes, returned ETag, and preview digest after explicit confirmation. Keep CSV at 32 KiB and workbooks at 512 KiB; the server owns workbook parsing, text-only amount validation, and sheet/cell error locations. Never convert workbook amounts in JavaScript. Never synthesize a preview from edited data or treat an uncertain publication result as success. Keep file limits, read/write permission differences, all located errors, and paired `priceImports` translations covered by tests.

Admission controls use `views/resource-limits` within member Settings, Project Resource configuration, and existing Key detail dialogs. Supported controls are rolling five-hour/seven-day tokens, monthly tokens and money, TPM, RPM, concurrency, and IP. Preserve stored/effective/inherited values, zero versus null, conjunctive parent IP restrictions, scoped query keys, reason/If-Match writes, explicit stale-policy review, and identical-intent publication retries. Runtime application must be confirmed before reporting enforcement. Read the platform denomination only from the resource-authorized `platform_currency` field. Preserve exact money strings and require explicit review after currency changes. Display authoritative quota windows, coverage, holds, and unknown values separately; never invent remaining allowance.

Provider-model availability and input capabilities belong in the existing detail page before prices. Treat image and PDF input support as explicit provider-model declarations rather than inferring them from names or protocols. Save availability and capabilities atomically with one reviewed ETag, keep them separate from routing weights, and reconcile uncertain publication before retrying. Public model capability metadata must be the per-protocol intersection across every ready, enabled, positive-weight route.

Two-step verification uses the existing sign-in card and security settings card/dialogs. A login HTTP 202 is a transient challenge, never a Session or authenticated navigation. Keep challenges, proofs, enrollment material, and one-time recovery codes in component state only; sensitive operations must not use mutation caches or browser storage. Render the server-issued authenticator URI locally with the pinned QR library, without external QR services. Clear sensitive state on completion, dismissal, expiry, and unmount. Handle generic proof failures locally, refresh the real session when appropriate, and replace the current Session/CSRF while resetting private queries after successful MFA changes. Keep paired `mfa` translations and license notices.

Native protocol catalog controls display actual connection and model protocols. Create connections with an explicit protocol, preserve per-protocol binding weights, and generate matching member API examples. The current Chat Playground only offers models with eligible Chat routes from the Key-scoped model list. Unsupported `/v1` and `/v1beta` paths return JSON 404 rather than SPA HTML.

Usage reports preserve Personal, Project and platform scope in query keys and authorization. Render unknown token coverage separately from known subtotals, keep historical amounts as decimal strings grouped by currency, and leave unknown trend buckets as gaps. Use the existing filter/card/trend/distribution/ranking layout and Project tab, with bilingual controls and explicit complete-query overflow errors.

The Playground selects native Chat Completions or Responses from eligible model protocols and sends matching inline-history payloads. Keep protocol parsers separate, require their native terminal events, retain authoritative terminal usage and show accepted/incomplete/failed states explicitly. Model/protocol changes clear the conversation; cancellation and duplicate-submit guards must not replay requests. Keys and history remain transient local state.

The Playground uses native conversation and model comparison tabs. `views/playground/chat.tsx` owns single-model interaction; `compare.tsx` owns two to four independent native lanes with shared prompt submission and per-lane cancellation. Leaving a tab destroys its transient credentials and requests.

Gateway attachment references use `routex://attachments/<object-id>` only in native image/PDF scalar positions. Personal Keys resolve ready objects in their immutable user scope; Project Keys resolve ready objects in their immutable Project scope and never borrow a manager's personal objects. Project attachment session routes require a current enabled manager, keep the Key out of control-plane requests, and derive ownership from the Project path only after server authorization. Model discovery exposes the non-secret attachment scope so the Playground can select the correct session endpoint; an optional `?project=` value is only an expected context and never carries a Key. Keep request-local deduplication and byte/expanded-payload bounds, preserve opaque text/tool data, recheck Project lifecycle after remote reads, and perform only one final quota admission and upstream dispatch. A validated attachment plan may use the existing provider-model capacity attestation for finite token/TPM reservation, but no file property may be converted into tokens. Finite money policies require enabled base `IMAGE_INPUT / 1_IMAGE` and `PDF_INPUT / 1_PDF` rates for the exact validated occurrence kinds; missing rates fail before storage reads, and page/pixel/byte estimates remain forbidden. Playground attachment controls remain transient client state and use the session upload API separately from inference authentication. `views/playground/attachments.tsx` owns the Mockup-aligned chips and picker composition; comparison keeps one shared bottom composer and intersects every selected lane's capabilities while native requests and cancellation remain independent. Protocol payload builders remain pure in `lib/playground-attachments.ts`. Keep code export disabled while a transient attachment draft is selected.

Quota calendar configuration stays within System information. Read access requires `system.read` or `limits.settings.write`; only the latter permits calendar writes. Calendar-only access never fetches or renders the site editor. Shared permission requirements may use a non-empty any-of list; an empty list grants nothing. Use server-owned editability, a reviewed ETag and reason, explicit Base UI confirmation, preserved conflict drafts, and immutable uncertain publication retries. A rejected retry cannot resolve the original uncertainty. Never reset usage or derive freeze status from the browser.

Site presentation is read through the public `site` query. `SitePresentation` applies the document title and the server language default without overwriting explicit language preferences; `SiteLogo` and `SiteFooter` compose the existing shell/auth layouts. Auth cleanup preserves public site settings. System information and announcement pages retain separate read/write permissions, and only authenticated shells mount the active announcement feed.

Native Messages is available in conversation and comparison lanes. `api/playground-transport.ts` owns bounded native HTTP/SSE transport; protocol clients retain independent event/finality and usage semantics. Messages histories include only completed text turns, with transient credentials and no cross-protocol fallback.

The audit workspace is a read-only, permission-gated table with filters, cursor pagination and a details drawer. Historical metadata that was not recorded stays unknown; arbitrary audit JSON is never rendered or exposed. Filter changes clear stale rows and selected details before fetching.

Native Gemini is available in conversation and comparison lanes. Requests preserve native contents/systemInstruction/generationConfig and one-candidate output. Histories contain only completed text, and final usage requires clean EOF with a native candidate finish or explicit prompt block. Unsafe model path names are rejected before dispatch with localized guidance.

Managed egress uses the `/admin/egress` workspace and `egress` translation namespace. Provider Connection controls select platform default, direct, or a named proxy with revision checks. Proxy diagnostics display measured stages, and authentication edits use explicit keep/replace/remove semantics with transient secret inputs. Read, write, and diagnostic permissions remain separate.

Storage administration uses `/admin/storage` with the approved overview card and large configuration drawer. Keep `storage.read`, `storage.write`, and `storage.test` independent; keep credentials transient and require explicit keep/replace/remove actions. Preserve exact ETags through conflict review, run probes only against saved descriptors, and report rollback success only after the target revision is verified and published.

Playground code dialogs expose working cURL, Python and JavaScript examples from the current native request in single or comparison mode. Shared snippet builders never accept an API Key; generated programs read an environment variable. Completed text history and current parameters are preserved, and an empty prompt uses an explicit editable placeholder.

System Status uses `/admin/system-status` with the approved Instances and System
jobs cards. Keep `system.read` and `system.write` independent, derive online and
cleanup eligibility from server-owned leases, and submit the exact reviewed
instance IDs and heartbeat revisions through the Base UI confirmation dialog.
Never infer state from the browser clock, remove rows optimistically, invent
resource percentages, process roles, or demonstration jobs, or report uncertain
cleanup as success. Only actual runtime publication, durable call delivery, and
storage cleanup work may appear as system jobs. Register paired `systemStatus`
translations and preserve independent refresh/error/empty states for both cards.

Provider-model capacity attestations belong in the existing detail view before prices. Read and write permissions remain independent. Use positive safe integer maxima, explicit evidence and reason, reviewed If-Match, retained drafts on conflicts, and immutable intent for uncertain retries. A configured record means a saved attestation, not proof that its revision is currently valid or enforced. Keep paired `pricing` translations.

Team resource limits use the addressable Limits tab and existing member action
menu/dialog. Aggregate policies support rolling/monthly Tokens, monthly money,
RPM, TPM and concurrency; members support monthly Tokens/money and request rates.
Use independent `teams.tokens.write`, `teams.money.write`, and
`teams.rates.write` permissions, sparse presence-aware writes, exact decimal money,
server-owned editable fields and a composite reviewed ETag. Owners receive no
implicit direct write authority. Keep the limits-only workspace free of member/model
directories. Stable Team/User policy and journal identities survive removal/rejoin;
aggregate and member admission is atomic. Preserve drafts and original uncertain
intent through incidental focus/reconnect events, with fresh authority on mount
and dispatch. Runtime confirmation includes the complete policy, revision, current
membership and monetary denomination; persistence alone never proves enforcement.


Credential planned retirement extends the existing replacement readiness dialog.
Keep providers.read and providers.write independent, require a reason and explicit
Base UI confirmation, and dispatch the exact reviewed replacement, native attempt,
configuration and aggregate ETag with one UUIDv4 intent. Retain that immutable
intent through uncertain or rejected retries; refreshing readiness never resolves
uncertainty. A durable receipt confirms historical commit independently of current
runtime application. Never optimistically disable rows, treat a re-enabled or
missing predecessor as currently applied, or claim supplier/fleet revocation.


Member catalogue availability counts only active models with eligible supported
native protocols. Explicit empty protocols and unknown/disabled models must never
fabricate a Chat endpoint or cURL example. Keep localized unavailable guidance,
disabled copy and the native Gemini name guard; Key creation cannot repair route
availability. Team visibility and Personal Key authority remain separate.


Member source visibility uses actor-scoped `model-catalog` queries, separate from
the direct personal `/models` Key selector. Deduplicate models and actual sources;
Team visibility never grants Personal Key or Team invocation authority. Keep
conjunctive literal/source/protocol/explicit image-PDF filters, two-plus-all source
labels, and the existing card/table/520px drawer. Detail cache keys include actor
and Model IDs; hide cached records during refresh, errors or revocation. Enable
examples and Key navigation only after current personal availability is confirmed.
Render missing price/member/request facts as unknown, with paired catalog copy.


Monthly quota exhaustion uses the existing notification menu and recipient-scoped
inbox routes. Enabled members may read their own Personal notices; Project
notices require current enabled manager authority and an active Project at each
read/count/action. Operational visibility still requires `system.read`; delivery
settings and SMTP permissions remain independent. Display only typed server-owned
settled amounts, limits, currency, calendar boundaries and observation times,
with exact strings and paired `notifications` translations. Never infer threshold
crossings, percentages, remaining allowance or exhaustion from holds. Hide stale
rows, unread counts and actions during refresh or authorization failure, and bind
read retries/cache invalidation to the captured actor.


Project monthly quota requests reuse Resource configuration and scoped request
history. QUOTA requests accept finite monthly targets only: blank omits a field,
zero is valid and money remains an exact decimal string. Preserve independent
model/quota reviewer permissions and current-manager authority. Bind submission
and approval to reviewed composite policy/currency validators; retain original
UUID, payload and validator through uncertain retries. Fresh details separate
historical baseline, requested targets and current policy. Saved approval never
proves runtime application, and superseded approval retries never restore an old
policy. Keep paired projectRequests copy and explicit conflict review.


Project rate-limit requests extend the existing Resource adjustment form with
RPM, TPM and concurrency fields. RATE_LIMIT and QUOTA use independent request
records, permissions and immutable mutation intents; a combined form reports each
saved, failed or uncertain result separately. Never resend a saved half. A fresh
explicit policy review may renew only a definitively unsaved intent; a prior
unknown outcome remains unresolved even if a later retry returns a conflict.
Omitted fields preserve the current control, zero is a cap, and explicit null is
unsupported. Use coherent manager-authorized request-limits context, actor-scoped
queries, exact decision receipts and current same-actor CSRF. Preserve model/quota
compatibility and distinguish historical approval from actual current application.

Team Session invocation uses explicit Team-scoped native Chat endpoints and a
short-leased runtime Session/member/grant projection. Keep Session CSRF/cookie
transport separate from API Key transport, with exact Team/member checks on every
attempt and immediate local revocation after committed authority changes. Do not
synthesize Team Keys, borrow Personal attachments or debit Personal/Key ledgers.
The initial source supports text Chat only; unsupported attachment, comparison
and code export controls remain explicit. Team call tables show only the current
active member's own immutable actor facts; directory permissions do not broaden
that route. Preserve actor/Team query keys, fail-closed refreshes and transient
state cleanup. Historical Team membership IDs never define accounting identities.


Team monthly quota requests use `views/team-requests`, `/quota-requests` and the
read-only `/admin/quota-requests` workspace. Preserve owner-first review, independent
Token/money platform stages, no self-approval, exact string targets and server-owned
current actions. Bind immutable creation/decision UUIDs to the reviewed context and
exact step; replaying an owner decision never approves a later platform stage. Keep
uncertain intent through rejected retries, explicitly review conflicts, and display
saved approval separately from current runtime application. Use local Base UI
dialogs/drawers and paired `teamRequests` translations. Approval confirmation must
render the server-issued stage effect preview and require the same reviewed ETag;
owner escalation explicitly changes no quota. Global records expose a workspace
link only when the server confirms a current assigned reviewer. Never fetch a global Team
directory for the personal workspace or enable mutations in global records.


## Team role scope

Team roles use the existing detail Roles tab and compact assignment table, local
permission dialog, candidate picker and explicit Save action. Keep the current
Team/actor query independent of global authenticated permissions and navigation.
Only exact `teams.write` and `teams.models.write` actions may be inherited, for the
assigned active Team and its current enabled members. Owner responsibility alone
grants no management action; global directories, Team quota-administrator dimensions,
Project/Key authority and role-assignment powers stay independent. Team lifecycle
writes retain direct platform permission checks.

Use target-specific member/model/role candidates and server-owned actor actions.
Only protected current platform administrators can assign roles. Preserve the
complete selection, required reason and reviewed If-Match through uncertain retries;
a current GET describes saved state rather than a historical operation receipt.
Changed role definitions require explicit review. Never query role or candidate
data from a minimal quota-only projection. Keep English/Chinese role copy in
`resources`, hide stale authority on refresh/denial, and distinguish saved role
assignment from runtime policy publication.


## Team usage reports

Team aggregate usage belongs in the existing usage workspace with a compact
Personal/own-Team selector. Fetch only the caller's scoped, paginated active Team
list; an expected `?team=` context never authorizes access. Keep actor, exact Team
and filters in query identities, cancel obsolete reads, and suppress stale private
reports during renewed reads or errors. Validate the exact `team_id` echo and
model-only dimensions. Team mode omits Key controls, rankings and provider
breakdowns while preserving existing cards, trends, model groups, historical
currency amounts and unknown coverage. Shared Team reports never expand actor-only
Team call history. Administrative Team filtering requires independent
`calls.read_all` and cannot combine Personal-user or Project subject selectors.
