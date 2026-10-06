# RouteX

RouteX is an open-source AI gateway and control plane for connecting applications to multiple AI providers and models through a governed, observable, and extensible platform.

> RouteX is at an early stage. Identity, sessions, authenticator/recovery verification, member/role administration, Team/Project management, personal and Project Keys, and offboarding have persistent APIs and interfaces. The member model catalogue preserves explicit Personal/Team grant sources, native capabilities, conjunctive filters and independently authorized details; Team grant visibility remains separate from Personal Key invocation; explicit Team Sessions can invoke native Chat, Responses, Messages and Gemini and retain their own Team call history. Four native inference protocols use prepared runtime snapshots and a bounded durable journal. Current token and per-occurrence image/PDF prices, exact currency conversion, immutable call assessments, atomic CSV/XLS/XLSX imports, CSV exports, and bilingual currency configuration are available. A versioned repository price file supports explicit local mappings, reviewed synchronization and selected-rate restoration, while protecting custom zero, disabled and same-amount prices; the shipped file is empty until reviewed rates are supplied. Personal and Project image/PDF attachments use explicit owner scopes, native protocol mapping, transient Playground lifecycles, and capacity-attested token/TPM/money admission. Current Project managers govern Project uploads while Project Keys resolve only objects owned by their immutable Project. Personal/Project aggregate and Key RPM, concurrency, source-IP, rolling-token, monthly-token, TPM, and monthly-money limits are enforced with durable admission accounting. Canonical Personal, Team, Project and platform usage reports have bilingual interfaces and scoped APIs. Each scope can export the complete authorized report as bounded UTF-8 CSV, preserving exact historical amounts, recorded currencies, unknown coverage and spreadsheet-safe text. Team reports share aggregate model/trend/currency statistics while individual Team call history remains restricted to the current actor. Authoritative process leases, nullable resource samples, real operational jobs, revision-checked offline cleanup, immutable Provider-attempt quality, revisioned quality thresholds, grouped operational alerts, recipient-isolated notification history, current-policy monthly settled-use exhaustion inboxes for Personal/Project and Team aggregate policies, and bounded operational SMTP delivery are available. Finite Team aggregate/member policies add rolling/monthly Tokens, monthly money and request-rate enforcement with independent platform permissions and stable accounting. Monthly Team member requests use owner-first review, explicit platform escalation and atomic quota updates. Team-assigned roles contribute only current Team metadata/member and model-management actions, with administrator-reviewed assignment and independent platform authority. Templates, distributed enforcement, provider-specific media pricing, external service acceptance, broader quota/enterprise alerts, and enterprise integrations remain in progress. See [the implementation and acceptance index](docs/IMPLEMENTATION.md) for scope and evidence.

## Development

The application uses Go 1.27.1, fox-gonic/fox, GORM, and PostgreSQL (or MySQL), with React 19, TypeScript 6, Vite 8, Tailwind CSS 4, React Router 8, and React Query 5. Reusable UI components follow shadcn/ui and Base UI patterns.

Requirements: Go 1.27.1+, Node.js 22.22+, and Docker with Compose (or a dedicated PostgreSQL/MySQL database).

Task, reflex, staticcheck, and actionlint are managed by the `tool` directives in `go.mod` and run through `go tool`; global installations are not required. `go tool task update-tools` installs the pinned GolangCI-Lint in the checkout-local `bin/tools/` directory.

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
go tool task check          # Go checks, frontend formatting/lint/types, module tidiness
go tool task test           # Go, frontend, dev lifecycle, and production asset tests
go tool task test-integration # Isolated PostgreSQL + MySQL domain integration tests
go tool task test-auth-lifecycle # Real-process identity/inference/restart tests on both databases
go tool actionlint          # Validate GitHub Actions workflows
npm --prefix website run lint   # Frontend ESLint
npm --prefix website run format # Format frontend source with Prettier
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
internal/routex/entity/      Identity, catalog, governance, and request-fact models
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

## Product implementation

The active implementation goal covers the complete capability and acceptance inventory in [the implementation index](docs/IMPLEMENTATION.md). Current catalog and personal Key contracts are documented in [CATALOG](docs/CATALOG.md) and [KEYS](docs/KEYS.md). The [price catalogue and exact text quote API](docs/PRICING.md) provide price and currency administration. [Immutable gateway text assessments](docs/METERING.md) preserve each call's price basis; [token and monetary quotas](docs/QUOTAS.md) use the [durable quota ledger](docs/QUOTA_LEDGER.md) for native gateway admission and settlement. [Native attempt planning](docs/ROUTE_ATTEMPTS.md) defines the active bounded same-protocol failover and replay-safety contract. Configure [encrypted credential storage and upstream network policy](docs/SECRET_STORAGE.md) before adding provider credentials.

Member, Project, and Key restriction interfaces expose token, TPM, and exact-decimal budget policies with authoritative usage snapshots. Provider-model details include explicit capacity attestation controls, and System information includes the installation quota calendar. Provider credential tables support combined name, Connection, verification, and enabled-state filters, recorded verification timestamps, reviewed name/priority editing, and reviewed deletion with confirmed runtime application, and staged replacement preparation with durable creation deduplication. New replacements remain pending and disabled until separately verified and enabled. Their read-only retirement review checks exact native completion under the current local configuration and complete eligible route coverage; receipt-backed retirement atomically disables the reviewed predecessor and confirms current local application separately from its saved historical receipt. Real-provider acceptance remains open. These interfaces preserve independent permissions, reviewed revisions, and English/Chinese copy. Personal/Project monthly exhaustion notices use covered settled usage and retain exact policy/window snapshots in the recipient inbox; they create no quota email or new admission rule. Project monthly quota and finite RPM/TPM/concurrency applications reuse Resource configuration and scoped request history, with independent reviewers, exact money, reviewed policy/currency generations, atomic approval and current runtime application evidence. Combined submissions retain independent records and retry intents. Monthly Team member requests use the own/pending workspace, owner-first approval, dimension-specific platform escalation and independently authorized read-only records. Reviewed effect previews distinguish unchanged escalation from final atomic aggregate/member application. Reviewed [User and Team defaults](docs/DEFAULT_LIMITS.md) copy policies only at creation and support explicit resets without clearing usage or IP restrictions. [Project creation and Overview](docs/PROJECT_OVERVIEW.md) support initial managers and scoped operational facts. [Four native Team Session protocols](docs/TEAM_INFERENCE.md) use current membership and separate Team accounting in the existing Playground. [Personal Model requests](docs/PERSONAL_MODEL_REQUESTS.md) and [shared Team Model requests](docs/TEAM_MODEL_REQUESTS.md) add independently scoped review without expanding existing Key ceilings. Named policy templates and broader alert policies remain unfinished.

[Member Overview](docs/MEMBER_OVERVIEW.md) preserves separate monthly Personal, Team aggregate and stable Team/User accounts. Exact saved limits, current application, known usage, unknown coverage and live reservations remain distinct; Team and member allowances are never added. Current membership gates visibility, and scoped Usage links retain the selected account. Personal thirty-day cards, a full-width Token trend and Model/API Key detail tabs
are a separate candidate package with their own current-main acceptance gates.

Project management validates exact current actors, managers, selected users and
models across both supported databases. Creator attribution never retains
management rights after removal. Resource details reauthorize before displaying
cached private data or actions; renewed reads and failures hide the prior detail.
See [Teams and Projects](docs/RESOURCES.md) for the current authority contract and
remaining creation/overview scope.

[User and Team defaults](docs/DEFAULT_LIMITS.md) provide reviewed seven-field
creation templates and explicit restore controls. Template changes affect future
accounts only; restores preserve recorded usage, holds, IP restrictions and Key
identity, and distinguish saved policy from confirmed runtime application. Team creation
uses a reviewed default generation, explicit owners and sparse initial overrides;
durable receipts reconcile the exact original request after uncertain responses.
Initial Team Model selection remains a separate pending capability.

Gateway and delivery contracts: [Chat inference](docs/GATEWAY.md), [native Responses](docs/RESPONSES.md), [native Messages](docs/MESSAGES.md), [native Gemini](docs/GEMINI.md), [Playground](docs/PLAYGROUND.md), [Team Session inference](docs/TEAM_INFERENCE.md), [Team resource limits](docs/TEAM_LIMITS.md), [Team monthly requests](docs/TEAM_REQUESTS.md), [Team roles](docs/TEAM_ROLES.md), [managed egress](docs/EGRESS.md), [SMTP administration and controlled test delivery](docs/SMTP.md), [operational alerts and notifications](docs/NOTIFICATIONS.md), [Provider quality](docs/PROVIDER_QUALITY.md), [object storage and owned attachments](docs/STORAGE.md), [runtime publication](docs/RUNTIME.md), [durable call records](docs/CALLS.md), [usage queries](docs/USAGE.md), [Team usage](docs/TEAM_USAGE.md), [System Status](docs/SYSTEM_STATUS.md), [audit log](docs/AUDIT.md), [governance](docs/GOVERNANCE.md), [Teams and Projects](docs/RESOURCES.md), [Project Keys](docs/PROJECT_KEYS.md), [Project resource requests](docs/PROJECT_REQUESTS.md), [offboarding](docs/OFFBOARDING.md), and [site settings and announcements](docs/SITE.md), [account security](docs/ACCOUNT.md), and [two-step verification](docs/MFA.md). [Internal secret storage and root rotation](docs/SECRETS.md) documents the keyring and guarded five-domain workflow; source, complete database regression and controlled production/browser/restart acceptance passed. Preserve the encryption root key and the configured local call-journal file across restarts. The journal requires persistent writable storage; production remains one process, with external integration and performance acceptance still open.

Member model visibility and native API availability are displayed separately.
Unavailable protocols have localized guidance without fabricated request examples;
open API drawers reflect refreshed eligibility.

Initial Project resources are configured in the existing creation form, with
independent model/limit permissions and exact decimal budgets. Optional initial
requests create separate pending model, quota and rate-limit records without
applying them. [The creation contract](docs/PROJECT_CREATION.md) documents reviewed
context, durable receipts, current publication proof and legacy compatibility.
Controlled dual-database, browser, native and restart acceptance passed; current
evidence is in [the implementation index](docs/IMPLEMENTATION.md).

Creator-private Team image/PDF attachments extend the existing Playground and
comparison composer. Only the creating enabled member under the exact current
membership can use them; Personal/Project Keys, other members and platform
administrators receive no ownership bypass. Server expiry and durable cleanup
remain independent of browser state. See [Team inference](docs/TEAM_INFERENCE.md)
for the contract and its separate acceptance status.
