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

Resource details reauthorize on mount with actor- and target-scoped queries. Hide cached private details, tabs and actions during renewed reads or errors; obsolete actor/resource responses must not restore them. Project actor, target, manager and model identities require exact server-side checks regardless of database collation. Retain only canonical manager relationship IDs; creator attribution never grants permanent management authority.

Resource lists retain compact filters, tables, and action menus. Details use addressable tabs; Team members use identity/status rows and action menus, model access uses allowed/available tables, and Project settings include the manager table. Candidate pickers call only the authorized resource-specific endpoint. Existing selections outside a bounded search response remain selected. Unknown model names render stable IDs without querying an unauthorized catalog.

Project lists show total retained Project Key counts, including pending, revoked
and expired records, rather than Model counts or active-only Key counts. Read
counts only from server `key_count`; current exact managers or `projects.write`
may receive them, while `projects.read_all` alone receives null/Unknown.
Administrative lists require `projects.read_all` and show only server-projected
stored monthly Tokens, exact decimal money/currency, RPM and TPM. Personal lists
return no policy summary. Keep explicit zero, null/Not set, no stored policy,
unavailable and unknown distinct; never infer effective defaults, unlimited
capacity or remaining allowance, round money through JavaScript numbers, or add
per-row Overview/limit/directory reads. Administrative Project search accepts
literal names and case-sensitive canonical ID fragments without changing scope.
Hide list facts, actions and pending lifecycle dialogs during renewed Session
or list reads, errors and actor changes; administrative lists also wait for fresh
read permissions. Stale replies cannot restore private rows, and lifecycle
dispatch requires fresh independent write authority. After fresh authorized Project detail and Session reads, replace
legacy managers/models/limits tab URLs with settings/resources/resources while
preserving other query parameters. Denied reads never redirect; Team counts and
tabs stay unchanged.

Project call tables reuse the call-record component with Project-specific endpoints and cache keys; they do not render administrator diagnostics or personal history. Metadata, continuity conflicts, terminal archival, and complete model/relationship replacements remain real API operations. New resources receive no implicit model grants. UI copy is paired in the `resources` namespace and follows the same English-default localization contract.

Provider model price editing belongs in the provider-model detail view. Reuse the
local price table and Base UI dialog wrapper, preserve decimal strings through
API submission, and require explicit conflict review before replacing an ETag.

Project model requests belong inside Resource configuration. Keep scoped history, additions-only submission, current-manager checks, and independent reviewer permissions in `views/project-requests`; register paired `projectRequests` translations. Pending requests never change effective grants.

Platform currency settings use the dedicated complete-catalogue currency metadata endpoint. Keep a coherent reviewed ETag, currency, and required-currency snapshot; preserve drafts but block submission when a newer generation appears until explicit review. Preserve decimal strings and fixed self-conversion, and confirm configuration changes before dispatch.

Price file maintenance belongs in `views/price-imports` at `/admin/prices`, with download/upload steps and a separate server-derived difference preview. Apply only the captured UTF-8 CSV or original XLSX/XLS filename and base64 bytes, returned ETag, and preview digest after explicit confirmation. Keep CSV at 32 KiB and workbooks at 512 KiB; the server owns workbook parsing, text-only amount validation, and sheet/cell error locations. Never convert workbook amounts in JavaScript. Never synthesize a preview from edited data or treat an uncertain publication result as success. Keep file limits, read/write permission differences, all located errors, and paired `priceImports` translations covered by tests.

Admission controls use `views/resource-limits` within the addressable Member Limits tab, Project Resource configuration, and existing Key detail dialogs. Supported controls are rolling five-hour/seven-day tokens, monthly tokens and money, TPM, RPM, concurrency, and IP. Preserve stored/effective/inherited values, zero versus null, conjunctive parent IP restrictions, scoped query keys, reason/If-Match writes, explicit stale-policy review, and identical-intent publication retries. Runtime application must be confirmed before reporting enforcement. Read the platform denomination only from the resource-authorized `platform_currency` field. Preserve exact money strings and require explicit review after currency changes. Display authoritative quota windows, coverage, holds, and unknown values separately; never invent remaining allowance.

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

Team monthly exhaustion notifications freeze current-policy aggregate settled
Tokens/money and the then-current enabled owner/member recipients. Current exact
membership controls inbox/read access; rejoin retains original read state, while
new members receive no historical fanout. Session network generations hide stale
rows/count/read actions; preserve manual same-actor CSRF cache replacement.

Private Team member notices use the same menu and existing Session inbox routes.
Keep team_member distinct from Team aggregate exhaustion: validate the stable
pair digest, frozen Team/User proof fields and exact current recipient before
rendering a recorded Team name or team_id fallback. Only the exhausted member
receives their own stored child-policy notice; owners/admins gain no peer access.
Current enabled active membership governs list/count/read/mark-all. Removal hides
original rows; same-user rejoin may restore their original read state without new
recipients or native replay. Derive exhaustion only from that child's covered,
known settled monthly journal and exact applied policy/calendar/currency. Never
sum parent/child quotas or treat live holds as settlement. Preserve exact decimal
strings, paired notifications copy and current Session-generation guards. Invalid
or foreign member proofs fail closed; no member directory or new endpoint is used.

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
labels, and the existing card/table/520px drawer. List, discovery and detail cache
keys include the current actor and successful Session network generation; detail
also includes the exact Model. Cancel obsolete reads and hide private facts/actions
during renewal, errors or revocation. Key navigation requires confirmed current
personal availability. Render missing price/member/request facts as unknown.

Member Model examples select an exact Personal or named Team source separately
from list filters. A sole source may initialize the selector; multiple sources
require an explicit choice. Use the selected source's ready native protocols,
including non-Chat Team sources. Removed sources or unavailable protocols require
explicit reselection without fallback. Team cURL examples use the existing
standalone builder with nonsecret request data only, sign in separately through
execution-time ROUTEX_EMAIL/ROUTEX_PASSWORD, require Python 3 standard-library
bootstrap, and stop on login challenges. Never export a browser Session/CSRF or
accept a live Key. Generation/copying performs no login, inference or grant write.
Guard copy/navigation synchronously against current authority; late clipboard
completion cannot restore stale notices. Manual same-actor CSRF cache replacement
is not a successful network renewal. Preserve actor/Model-scoped request captures
through incidental renewal without replay; actor/Model changes destroy them.
Invalidate generation-suffixed candidate drawers by their complete actor/Model
prefix. Keep all visible example guidance in the paired catalog translations.


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

Team Session invocation uses explicit Team-scoped native endpoints and a
short-leased runtime Session/member/grant projection. Keep Session CSRF/cookie
transport separate from API Key transport, with exact Team/member checks on every
attempt and immediate local revocation after committed authority changes. Do not
synthesize Team Keys, borrow Personal attachments or debit Personal/Key ledgers.
Native text conversations and comparison support four protocols; code exports
use independent transient authentication. Team attachments preserve exact creator
and active membership scope. Team call tables show only the current
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

User/Team default settings use `views/default-limits` at `/admin/limits` with paired
`defaultLimits` translations. Read access is `system.read` or
`limits.settings.write`; only the latter writes future creation templates. New
User/Team accounts copy current templates atomically; existing accounts change
only through the dedicated reviewed restore flow. Restore user caps only with
`limits.users.write`, and Team aggregates only with all three Team cap permissions.
Preserve zero/null, exact money/currency, local IP, usage/holds/calendar, resource
birth and Key/relationship identity. Freeze unknown reset intent through retries;
report current runtime application separately from saved provenance. Never treat
rule-save persistence as retroactive enforcement.

Project creation uses the purpose-specific manager picker and explicit initial
manager set. Preserve ordinary creators, permit direct `projects.write` selection
and treat creator identity solely as an immutable fact. The existing Overview
uses the scoped server snapshot, independent nullable sections, exact quota
strings, at most five typed activities and real immutable last-call facts. Journal
settlement and SQL call delivery are independent; never invent a last call from
quota use. Suppress private cards and actions while refreshing actor/Project
authority, and never replay uncertain creation automatically.

Personal model access requests use the existing catalog drawer footer and Member
Models tab. A separate minimal requestable directory does not grant invocation
or expose administrative catalog facts. Reviewer access requires independent
`members.models.write` and never borrows `members.read` or `members.write`.
Approvals add one Personal grant only, retain immutable UUID/review receipts and
never expand existing Key scopes. Original retries cannot restore later revoked
grants; exact private source markers distinguish current application from saved
history. Unknown creation/decision intent survives every rejected retry. Keep
actor/target privacy, fresh authority, paired `personalModelRequests` translations
and native authorization-negative coverage.

Personal request history remains accessible through the existing catalogue action
area regardless of current grants. Retain mounted history through renewed actor
reads while hiding private rows, and refresh exact candidate detail independently
of discovery. A captured committed response is historical; current application
copy comes only from fresh authorized request detail.

Team Model requests extend the existing Model drawer with explicit Personal/Team
scope, minimal membership-scoped Team selection and shared Team/Model pending
slots. Own history remains available after membership loss, while current facts
require fresh independent resource authority. Use `teams.models.write` scoped
review without owner-only authority or self-review. Approval adds only a missing
shared Team grant; unchanged canonical grants preserve request provenance and
ordinary removal/re-addition never revives historical proof. Pending eligibility
loss cancels requests atomically; approved shared grants survive applicant
departure. Keep immutable uncertain intents, current/historical status separation,
local Base UI confirmations and paired `teamModelRequests` translations. The
minimal review route is `/teams/:resourceId/model-requests`; never fetch a global
Team/member directory for request workflows.

Team Session native inference uses Cookie/current CSRF through exact Team resource
endpoints for Chat, Responses, Messages and Gemini. Native Key headers and extra
query/workspace selectors are rejected and scrubbed before request logging. Team
model discovery exposes actual ready protocols and per-protocol media capabilities;
member catalogue Team links use source-specific protocols without Personal Key
authority. Team comparison uses two to four independent native text lanes and a
shared composer. Team creator-private attachments use exact Session scope. Team code export captures
only non-secret native request data and generates independent environment-driven
login programs. Renewed
Session/Team authority clears stale selection and transcript while retaining an
unsent prompt; model loading and dispatch remain explicit. A native 401 triggers
a bounded no-store Session probe, and only its active authoritative 401 expires
the Session. Other native failures never establish logout. Cancellation, late
callbacks and noncompleted turns never enter completed text history.

Project creation uses the existing name/description/manager form with optional
initial models and monthly Tokens/money, RPM, TPM and concurrency controls. Keep
direct model/limit permissions independent. The optional initial-request mode
requires the applicant in the explicit initial manager selection and records
independent MODEL_ACCESS, QUOTA and RATE_LIMIT requests atomically at creation.
Use only minimal creation context/model candidates and exact decimal money with
a reviewed currency. Enhanced creation retains one UUIDv4/body/If-Match intent
through uncertainty; explicit retry reconciles the historical receipt without
restoring managers, grants, policies or pending children. Current Project facts
and runtime application require renewed resource authority, and receipt navigation
is explicit. Keep legacy creation compatible and preserve Team creation.

Administrative Model detail uses an exact actor/Model-scoped read, independently
of the global Model directory. Existing routing-table input/output prices are
read-only base INPUT_TOKEN/OUTPUT_TOKEN rates for each exact Provider-model. Keep
models.read_all and prices.read independent, preserve decimal strings, denomination,
1M_TOKEN, known zero, disabled and absent rates, and never synthesize a logical
Model price or infer media/long-context rates. Renew Model authority before price
reads/retries; hide old detail, prices and actions during Session/permission refresh,
errors, target changes and late replies. Price editing remains in Provider-model
details. Use paired catalog/pricing copy and existing Table components.

Team request code reuses the existing conversation/comparison code dialog.
Require fresh actor/Team/Session authority and an exact discovered model; clear
captured code on renewal, conflict, context changes or teardown. Each generated
program independently signs in with ROUTEX_EMAIL/ROUTEX_PASSWORD, verifies an
authenticated current Session and CSRF, then invokes exactly one Team-native
request. MFA HTTP 202 never becomes a Session. Redirects, failed proofs and
malformed authority stop before inference without retries. Keep passwords,
cookies and CSRF out of arguments, files, browser storage, logs and captured
snippet data. Python uses its private in-memory CookieJar; cURL requires Python 3
for private authentication then receives native curl configuration through stdin;
JavaScript uses Node's native fetch. Existing Key examples remain unchanged.

The conversation parameter Reset action restores Temperature 0.7, Top P 1,
maximum output Tokens 2048 and an empty system prompt. Preserve credential source,
Team, model, protocol, streaming, completed history and the unsent prompt; dispatch
no request. Disable parameter Reset while an invocation is active. Keep all
labels/notices bilingual and code previews aligned with the current defaults.

Team authority renewal observes every successful network Session read, including
structurally identical data with an unchanged timestamp. Clear Team models,
completed history and captured code, abort pending inference, preserve unsent
drafts, and require explicit model rediscovery. Observe the existing Session query
without creating an additional network observer. Manual same-actor CSRF cache
replacement is not a Team authority renewal and preserves completed history.

Member Overview keeps the approved identity header and monthly account table.
The self-only /overview/accounts endpoint exposes bounded current Personal and
Team accounts, with separate aggregate and stable Team/User member policies.
Preserve exact decimal strings, null versus zero, recorded currency, runtime
application, coverage and unknown usage. Monthly settled/retained facts and live
active_reservations are separate snapshots from the same coherent batch; never
add them or infer a remaining allowance. Only covered, known settled usage with
a positive finite Token cap may produce a percentage. Reauthorize the Session on
mount and every completed read, hide private rows/actions during refresh/error,
and ignore obsolete actor/generation pages. Keep usage links scoped and paired
overview translations; never fetch resource/model/member directories to fill the
monthly table.

Administrative member detail keeps the existing Overview tab and three cards for
Personal monthly Tokens, Personal monthly money and total retained Personal Keys.
Read only the exact member-scoped /admin/members/:user_id/overview endpoint under
independent members.read authority. The count includes every retained Personal
Key status, excludes Project Keys, and exposes no Key IDs, secrets or actions;
it does not establish active or callable Keys. Preserve count and money strings,
settled usage, unresolved monthly facts, live reservations, unknown coverage and
server-owned runtime application separately. No progress or remaining allowance
is inferred. Scope both parent detail and Overview queries to actor, target and
successful Session network generation; hide old cards during renewed authority,
pending/error reads and target changes. Reject obsolete responses and callbacks,
including structurally identical same-millisecond renewal. Observe the existing
Session query without another network observer; manual CSRF replacement alone
is not a network renewal. Keep paired governance translations and the existing
access-status section; this read-only view grants no member or Key mutation.

Creator-private Team media uses only exact Team Session/CSRF attachment routes and
canonical managed references in the four native image/PDF scalar positions. Bind
objects to exact Team, creator user and active membership, with a fixed deadline
one hour after creation; peers, owners, administrators and rejoined memberships
receive no bypass. A renewed Session may remain server-eligible for the same
creator/membership, while the UI clears unsent media and aborts uploads without
restoration or replay. Keep existing chips/picker/shared composer, intersect every
selected lane's capabilities, reject mismatched response ownership and retain
submitted objects until all lanes settle, including cancellation and unmount.
Cleanup denial defers to durable expiry. Keep files, credentials and references
transient, never borrow Personal/Project uploads, preserve completed plaintext
history only and disable code export while media is selected.

The member Home Overview preserves its identity header and monthly resource-account table, followed by three Personal thirty-day usage cards, a full-width Token trend, and Model/API Key detail tabs. Request only the existing Personal Usage API with the server-anchored 30d preset, UTC daily buckets and no comparison. Preserve exact returned range, observed query time, recorded completion and durable-delivery lag. Keep the authoritative success-rate denominator, including cancellations and admission failures, explicit. Render exact token strings and unknown coverage separately; leave unknown trend buckets as gaps and derive bounded shares only from known positive totals with BigInt. Historical identities come from the report alone. Scope queries to the current actor and successful Session generation, hide private facts during renewal/errors, abort obsolete reads, and ignore late responses. Keep overflow guidance localized and link to Usage without fabricating narrower results, remaining allowance, combined Team usage, or Key secrets.

Usage CSV export remains the final action in the existing filter row and uses
only the last applied scoped filters. Request one authorized server CSV; never
serialize cached report data or fetch a directory to fill it. Preserve the
reversible `apostrophe_text_v1` exact-text encoding, null versus zero, recorded
currencies and independent Personal/Project/Team/platform dimensions. Scope the
transient download to actor, target, applied filters and successful Session
generation; abort obsolete work and reject late callbacks without replay. Keep
401/403/404 authority loss separate from bounded-complete-export failures. Blob
URLs stay transient and are revoked promptly. A prepared download is not proof
that a file was saved. Keep paired `usage` guidance and independent server gates.
Preserve non-sensitive applied filters and independent raw drafts in an actor-and-exact-source owner through successful
Session or permission renewal. Generation-bound report/export subtrees still
unmount during authority reads; actor or Personal/Team/Project/platform source
changes clear filter intent. Never persist drafts in browser storage or private
query caches.

Internal secret storage uses `views/secrets` at `/admin/secrets` and addressable
rotation URLs. Require a current administrator and independent `secrets.read`
and `secrets.rotate` authority. Scope reads to actor, target and successful
Session generation; hide old private state during renewed reads/errors and
ignore obsolete replies. Keep the approved internal status card and local Base
UI confirmation dialog, exact configured key IDs, string epochs/counts and
server-owned blockers/observation times. Never display root material, private
proofs, secret subjects, invented progress or fleet acknowledgement. Mutations
retain the reviewed strong ETag, UUIDv4, body and reason through uncertain and
rejected retries; refresh never proves the original operation. Historical
receipts and current write-policy/publication application remain separate. Keep
paired `secrets` translations and clear transient action state on teardown.

Addressable historical root-rotation details expose only that recorded job's
server-allowed actions. Global Start belongs to `/admin/secrets` and is never
prepared from a historical task URL; the shell retains the Credential storage
title on addressable rotation routes.

Repository price maintenance uses the existing `views/price-imports` workspace
and a local `repository` composition above the file workflow. Configure explicit
stable-ID mappings before previewing selected models; configuration never applies
prices. Preserve reviewed source/configuration/catalogue generations, independent
read/write permission gates and immutable UUIDv4 intents through uncertain results
and temporary authority reads. Retain private workflow owners while hiding cached
fragments during renewed reads. Selected rate restoration belongs in the existing
Provider-model price table and uses only server-derived differences after explicit
confirmation. Keep exact decimal strings, per-rate custom zero/disabled/same-amount
protection, separate threshold ownership and historical receipts distinct from
current configuration/runtime proof. Register paired `priceImports` and `pricing`
copy; previews and retries never invent last-success metadata.

Model compatibility-name Early stop belongs in the existing Model information
card and local Base UI review/confirmation dialog. Keep models.read_all and
models.write independent; select one exact retained name with a reviewed strong
If-Match and required reason. Retain the exact original body/reason/ETag through
uncertain or rejected retries, hide private facts during renewed authority reads,
and clear ownership on actor/Model changes. Fresh review and matching current
retirement confirm only present non-callability/runtime application; they never
prove the original historical operation. Preserve stable Model/grants/bindings,
permanent historical-name reservation and already-dispatched calls. Register
paired catalog copy, render only recorded UTC deadlines with the selected locale,
and never infer eligibility from the browser clock.

Guided Model creation uses the existing creation page and the `modelCreation`
namespace. Select one exact authorized Connection and up to 50 Provider-model
items; retain selections outside bounded picker responses. Keep models.read_all /
providers.read separate from models.write. Preview server-derived new100,
same-protocol backup0 and first-protocol100 weights, then require reason and
explicit Base UI confirmation with a reviewed ETag and UUIDv4 intent. Preserve
that exact intent across uncertainty, renewed authority reads and rejected retries.
Receipts prove historical commit independently of current configuration/runtime
application; pending/superseded/unavailable are not success. New Models receive
no implicit grants and existing Keys never expand. Do not infer traffic readiness
from saved zero-weight configuration or render credential material.

Public Model name assistance stays inside the guided creation name control. The
versioned local reference file contains only reviewed official identities, source
URLs and review dates. The local Base UI Autocomplete accepts custom names and
requires explicit selection; malformed reference metadata disables suggestions
without disabling input. Suggestions never infer Connection/ProviderModel
selection, routing readiness, protocols, capabilities, prices or grants. Preserve
exact actor/Connection/row authority and the existing server preview, reason and
immutable creation intent; close obsolete suggestion interactions on renewed
reads or resource changes. Keep paired modelCreation copy.

Autocomplete dismissal labels use the paired common close translation. Base UI
1.8.0 exposes no public label prop for its two native hidden dismiss controls;
the local wrapper localizes only the exact ref-bound input/popup sibling labels.
Preserve native dismissal handlers, focus guards and custom input. Never use a
global query, observe the document, hide accessibility controls or disable focus
management. Keep open-popup language switching, both native dismiss actions,
Escape, reopening, row isolation and StrictMode covered by tests; a changed
native sibling structure must fail the focused compatibility tests.

Administrative Member Keys remain in the addressable Member detail Keys tab. Use
the exact Personal-only metadata projection with independent members.read and
members.keys.disable gates; never fetch a model directory, expose credential
material or add owner/Project lifecycle controls. Preserve exact-string quota
counters and currency amounts, monthly coverage and live holds, and shared
rotation roots without inventing remaining allowance. Review the exact Key and
strong persistent ETag in the local Base UI danger dialog with a required reason.
Same-actor/target renewed reads hide private rows/actions/dialogs while retaining
only the original transient uncertain reason and revision for explicit retry;
actor/target changes abort obsolete work and destroy that intent. Only the exact
authorized disable response confirms current disabled runtime state, never an
original historical operation. Keep paired governance copy.

Row-menu review dialogs may pass the native Base UI Popup `finalFocus` through the
local Dialog and use the local Menu `triggerRef` for their exact current row.
Member Key Escape and Cancel return focus only to a connected trigger with fresh
Session, permission, target and list authority. Skip disconnected or hidden rows
when authority changes or successful disable refreshes the list; never delay
private-data hiding or force focus through timers, global queries or stale DOM
references. Callers without these optional props retain native default behavior.

Administrative Member budgets, quotas and limits use `?tab=limits`; Settings
retains identity and lifecycle actions. Mount the Member-only limits composition
beside the other detail content with a stable actor/target identity. Hide policy,
edit and reset controls during Session, permission, target or policy refreshes and
errors without destroying the same-target unsent draft or original uncertain
request. Fresh `members.read` governs reads and `limits.users.write` independently
governs writes. Reuse the existing policy editor and default-restoration controls
with parent-managed authority; do not add Session observers. Guard review,
dispatch and response synchronously against current query generations, submit
original uncertain bodies and strong If-Match values only on explicit retry with
current CSRF, and never let a rejected retry clear uncertainty. Actor, target,
logout and tab changes destroy local private intent; explicit reset dismissal
retains its existing uncertainty warning. Preserve legacy Project, Key, Team and
default-limit callers and confirm runtime application before reporting enforcement.
