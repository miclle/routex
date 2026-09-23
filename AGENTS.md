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
- Provider and model detail pages use resource URLs. Profile and security settings use separate routes; inference configuration and conversation remain inside one split workbench.

### Single Binary Embedding

- `website/assets_development.go` (`//go:build development`) reverse-proxies to Vite dev server
- `website/assets_production.go` (`//go:build !development`) serves assets via `//go:embed build/*`
- NotFound handler: `/api` prefix returns JSON 404; all other routes fall back to SPA index
- `go tool task test` covers development and production asset serving; CI runs production tests after building frontend assets
- Development services run under Task with fail-fast cancellation; never scan and kill unrelated processes to free ports
- Keep Vite Host validation enabled; permit custom development hostnames explicitly

## Mandatory Rules

- Write documentation, commit titles/bodies, and PR descriptions in English.

- Respect the existing layering and directory structure; do not reshape architecture for local changes
- Run `go tool task check` before committing
- When changing frontend structure or UI primitives, update this file and `.agents/rules/frontend.md` together

## Pre-commit Checklist

- Run `go tool task check`; do not commit if it fails
- Verify whether frontend API calls or types need to be updated accordingly
- For UI behavior or primitive changes, run the focused Vitest tests or `cd website && npm test`
- For schema, transaction, or authentication changes, run `go tool task test-integration`; identity persistence changes also run `go tool task test-auth-lifecycle`.
