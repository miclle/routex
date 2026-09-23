# AGENTS.md

Technical specification for AI coding assistants working on this project.

## Project Overview

RouteX is an AI gateway and control plane, built as a Go + React single-page application that compiles into a single binary. The backend embeds frontend build output via `//go:embed`, so production deployment requires only one executable plus a database.

The current implementation includes persistent administrator setup, local authentication, revocable sessions, and a protected workspace. Product domains and extension boundaries are defined in `docs/ARCHITECTURE.md`; OpenAI chat routing and call records are available; immutable runtime publication, bounded durable call buffering, and governance backends are available; quotas and monetary metering remain subsequent work packages. Encrypted provider credentials, stable model catalogs/grants, and personal API Keys are available. Phase scope and acceptance evidence are tracked in `docs/IMPLEMENTATION.md`. Preserve the Gateway, Control Plane, and Data Platform boundaries as features are added.

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
- `/playground` invokes native inference with a transient personal Key, without browser storage or mutation-cache persistence. `/calls` and `/admin/calls` use separate list/detail query keys and permission boundaries.
- Catalog routes include `/admin/providers`, `/admin/models`, `/models`, and `/keys`. Administrative views use `AdminOnly` in addition to server authorization; the app navigation follows the session role.
- One-time Key secrets stay only in component state until confirmed or revoked; never return secrets from a React Query mutation into its cache. Local `Dialog` wraps Base UI for modal focus and keyboard behavior.
- Use the local Base UI `Input` wrapper for form controls, with labels, autocomplete, validation, and pending states.
- Wrap Base UI headless components in local `components/ui/*` modules before using them from pages
- `AppShell` owns the 248px desktop sidebar, 80px collapsed state, mobile navigation drawer, 64px page bar, workspace/management navigation, and account menu. Local `Table`, Base UI `Drawer`, and Base UI `Menu` primitives support list/detail workflows.
- Governance uses effective permission queries for page gates and navigation, member detail tabs, role permission dialogs, and a registration configuration drawer. `Switch` wraps Base UI.
- Provider and model detail pages use resource URLs. Profile and security settings use separate routes; inference configuration and conversation remain inside one split workbench.

### Single Binary Embedding

- `website/assets_development.go` (`//go:build development`) reverse-proxies to Vite dev server
- `website/assets_production.go` (`//go:build !development`) serves assets via `//go:embed build/*`
- NotFound handler: `/api` prefix returns JSON 404; all other routes fall back to SPA index
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

Admission controls use `views/resource-limits` within member Settings, Project Resource configuration, and existing Key detail dialogs. Supported controls are RPM, concurrency, and IP only. Preserve stored/effective/inherited values, zero versus null, conjunctive parent IP restrictions, scoped query keys, reason/If-Match writes, explicit stale-policy review, and identical-intent publication retries. Runtime application must be confirmed before reporting enforcement.

Provider-model availability belongs in the existing detail page before prices. Keep stored state separate from routing weights, preserve exact ETags through explicit review, and reconcile uncertain publication before retrying.
