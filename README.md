# RouteX

RouteX is an open-source AI gateway and control plane for connecting applications to multiple AI providers and models through a governed, observable, and extensible platform.

> RouteX is at an early stage. Persistent administrator setup, local login, sessions, and a protected workspace are implemented. Provider routing, API keys, quotas, and metering are still planned. See [the implementation and acceptance index](docs/IMPLEMENTATION.md) for scope and evidence.

## Development

The application uses Go 1.27.1, fox-gonic/fox, GORM, and PostgreSQL (or MySQL), with React 19, TypeScript 6, Vite 8, Tailwind CSS 4, React Router 8, and React Query 5. Reusable UI components follow shadcn/ui and Base UI patterns.

Requirements: Go 1.27.1+, Node.js 22.22+, and Docker with Compose (or a dedicated PostgreSQL/MySQL database).

Task, reflex, staticcheck, and actionlint are managed by the `tool` directives in `go.mod` and run through `go tool`; global installations are not required. `go tool task update-tools` installs GolangCI-Lint separately if it is missing.

```bash
git clone https://github.com/miclle/routex.git
cd routex
go tool task install
go tool task update-tools
go tool task db:up
cp cmd/routex/config.example.yaml cmd/routex/config.local.yaml
```

Compose creates a local PostgreSQL database on `127.0.0.1:15433`, matching the example configuration. Local configuration is ignored by Git. The server applies versioned migrations on startup; it does not create the database itself. Existing local config files are not overwritten. See [local development](docs/DEVELOPMENT.md) for MySQL, port overrides, persistent volumes, and isolated database tests.

```bash
go tool task dev
```

Open `http://localhost:9000`. Development starts the Go server with hot reload on port 9000 and Vite on port 5173. An empty installation opens `/setup` to create the first administrator; initialized installations require `/login`. Sessions survive application restarts and logout revokes the current session. `GET /health` returns `ok`. See [authentication contracts](docs/AUTH.md) for cookie, CSRF, and deployment boundaries.

Task manages the two development processes together: interrupting the task or a process failure stops its companion. Occupied ports produce an error; startup does not terminate other projects' processes. Vite keeps its default Host validation. Add a specific hostname to `server.allowedHosts` in `website/vite.config.ts` if you use a custom development domain.

To use different ports:

```bash
ROUTEX_HTTP_PORT=9100 ROUTEX_VITE_PORT=3100 go tool task dev
```

`ROUTEX_API_BASE_URL` overrides Vite's API proxy target; `ROUTEX_VITE_DEV_SERVER_URL` overrides the Go development asset proxy target. YAML configuration supports `${NAME}` and `${NAME:-fallback}` environment substitutions in parsed string values, preserving quotes, backslashes, and newlines supplied through environment variables.

### Commands

```bash
go tool task check          # Go formatting/vet/lint, frontend types, module tidiness
go tool task test           # Go, frontend, dev lifecycle, and production asset tests
go tool task test-integration # Isolated PostgreSQL + MySQL migration/auth tests
go tool task test-auth-lifecycle # Real-process auth and restart tests on both databases
go tool actionlint          # Validate GitHub Actions workflows
cd website && npm run lint  # Frontend ESLint
```

From the repository root:

```bash
go tool task build          # Build frontend assets and bin/routex
./bin/routex -c cmd/routex/config.local.yaml
go tool task build-all      # Build linux/darwin/windows binaries for amd64/arm64
```

Production embeds the frontend into the Go binary. Build frontend assets before running Go commands without the `development` build tag. GitHub Actions includes backend checks, frontend lint/types/tests/build, binary builds, GolangCI-Lint, workflow validation, and dependency review.

### Source layout

```text
cmd/routex/                  Application entry point and example configuration
internal/routex/config/      Bootstrap configuration
internal/routex/database/    Database connection and migrations
internal/routex/handler/     HTTP routes and handlers
internal/routex/service/     Business logic
internal/routex/entity/      Persistence models for users, sessions, and installation
internal/routex/errors/      Application errors
pkg/                         Reusable helpers
website/                     React SPA and Go asset embedding/development proxy
scripts/                     Build and verification scripts
```

## Goals

RouteX is intended to provide:

- Multi-provider and multi-model access
- Native provider protocol support where practical
- Model routing, failover, and traffic policies
- API key and project-based access control
- Quotas, rate limits, budgets, and usage metering
- Provider, credential, and model management
- Auditability and operational visibility
- A member workspace and administrative control plane
- Extensible provider adapters and integrations

## Architecture

The target architecture is split into three product domains:

1. **Gateway** — the real-time data plane responsible for authentication, routing, policy enforcement, and upstream requests.
2. **Control Plane** — configuration, administration, projects, teams, providers, models, credentials, quotas, and policy management.
3. **Data Platform** — asynchronous usage processing, reporting, analytics, and operational insights.

RouteX is designed with explicit extension points so optional capabilities can be developed and deployed independently without coupling them to the open-source core.

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the architecture principles and [docs/IMPLEMENTATION.md](docs/IMPLEMENTATION.md) for phased work and acceptance criteria.

## Project status

RouteX is currently in the initial implementation phase. APIs, data models, package boundaries, extension interfaces, and deployment topology may change before the first stable release.

## License

RouteX is licensed under the [Apache License 2.0](LICENSE).

See [NOTICE](NOTICE) for attribution information.

## Contributing

Contributions are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) before submitting changes.
