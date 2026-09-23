# AGENTS.md

Technical specification for AI coding assistants working on this project.

## Project Overview

RouteX is an AI gateway and control plane, built as a Go + React single-page application that compiles into a single binary. The backend embeds frontend build output via `//go:embed`, so production deployment requires only one executable plus a database.

The current implementation is a bootstrap scaffold. Product domains and extension boundaries are defined in `docs/ARCHITECTURE.md`; provider routing, authentication, quotas, and metering are not implemented yet. Preserve the Gateway, Control Plane, and Data Platform boundaries as features are added.

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
go tool task dev            # Start development environment (hot reload)
go tool task build          # Build production binary (with embedded frontend)
go tool task build-all      # Cross-compile for multiple platforms
go tool task run            # Run in production mode
go tool task lint           # Auto-fix code style and run checks
go tool task check          # Full checks (backend + frontend types + mod tidy)
go tool task test           # Go, frontend, dev lifecycle, and production asset tests
go tool task test-production # Build assets and test production asset serving
go tool task clean          # Remove build artifacts
go tool task update-tools   # Install GolangCI-Lint if missing
go tool actionlint          # Validate GitHub Actions workflows
```

Task, reflex, staticcheck, and actionlint are versioned as Go tool dependencies in `go.mod`. Invoke them with `go tool task`, `go tool reflex`, `go tool staticcheck`, and `go tool actionlint`; do not rely on globally installed versions. GolangCI-Lint is installed separately at the version specified in `scripts/install-tools.sh` and `.github/workflows/golangci-lint.yml`.

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
- Keep database connection and migration setup in `internal/routex/database/`; services receive a ready `*gorm.DB`
- PostgreSQL (default) and MySQL are supported; switch via `driver` in YAML config
- YAML config contains only bootstrap settings (address, database driver, connection string)
- Configuration files may reference environment variables with `${NAME}` or `${NAME:-fallback}`
- Expand environment variables after parsing YAML so values cannot alter configuration syntax

### Frontend

- Routing: React Router v8
- Server state management: React Query (`@tanstack/react-query`)
- API calls go in `website/src/api/`
- Type definitions go in `website/src/types/`
- Pages go in `website/src/views/`
- App-wide layout/composition belongs in `website/src/components/app/`
- Reusable UI primitives belong in `website/src/components/ui/`
- Prefer local shadcn-style primitives, Tailwind tokens, Lucide icons, and Base UI wrappers over one-off markup
- Wrap Base UI headless components in local `components/ui/*` modules before using them from pages

### Single Binary Embedding

- `website/assets_development.go` (`//go:build development`) reverse-proxies to Vite dev server
- `website/assets_production.go` (`//go:build !development`) serves assets via `//go:embed build/*`
- NotFound handler: `/api` prefix returns JSON 404; all other routes fall back to SPA index
- `go tool task test` covers development and production asset serving; CI runs production tests after building frontend assets
- Development services run under Task with fail-fast cancellation; never scan and kill unrelated processes to free ports
- Keep Vite Host validation enabled; permit custom development hostnames explicitly

## Mandatory Rules

- Respect the existing layering and directory structure; do not reshape architecture for local changes
- Run `go tool task check` before committing
- When changing frontend structure or UI primitives, update this file and `.agents/rules/frontend.md` together

## Pre-commit Checklist

- Run `go tool task check`; do not commit if it fails
- Verify whether frontend API calls or types need to be updated accordingly
- For UI behavior or primitive changes, run the focused Vitest tests or `cd website && npm test`
