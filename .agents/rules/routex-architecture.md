# RouteX Architecture Rules

These rules describe the RouteX bootstrap. Follow `docs/ARCHITECTURE.md` for product domains, data-plane independence, and extension boundaries.

## Principles

- Preserve the existing `Handler -> Service -> Entity` layering.
- Keep route registration centralized in `internal/routex/handler/handler.go`.
- Keep database connection and migration setup in `internal/routex/database/`.
- Put application models in `internal/routex/entity/`; do not expose entities directly as HTTP contracts.
- Put reusable, business-agnostic helpers in `pkg/`, with small APIs and tests.
- Add domain modules and integrations only when a concrete RouteX use case needs them.

## Backend Boundaries

- Handlers own HTTP binding, status codes, response DTOs, and route grouping.
- Services own business logic and database access.
- Entities own GORM models, table names, persistence constants, and narrow model helpers.
- Configuration should stay bootstrap-focused: listen address, database driver, DSN, and similarly necessary startup settings.
- PostgreSQL and MySQL support must stay explicit; if a feature only works with one driver, reject unsupported drivers early.

## Frontend Boundaries

- API calls live in `website/src/api/`.
- Shared HTTP contract types live in `website/src/types/`.
- Page-level routes live in `website/src/views/`.
- App shell and product-level composition live in `website/src/components/app/`.
- Reusable UI primitives live in `website/src/components/ui/`.
- Reuse the existing React Router, React Query, shadcn/ui, Base UI, Tailwind, and Axios patterns.
- Pages should import local UI primitives rather than importing Base UI directly.
- Do not hard-code backend origins in components; use the Vite proxy and shared API client.

## Change Checks

- Start code changes with `git status --short`.
- Keep changes small and scoped to the current RouteX implementation phase.
- Run `go tool task check` before committing.
- Run `go tool task test` when behavior, API contracts, database models, or UI interactions change.
