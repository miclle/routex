# RouteX Architecture

This document records the initial architecture direction for RouteX. It is a
starting point rather than a frozen implementation specification.

## Current implementation

The repository contains the Go + React application, Docker Compose development
databases, versioned PostgreSQL/MySQL migrations, first-administrator setup,
local password authentication, persistent revocable sessions, server-side
administrator checks, and a protected SPA workspace. The identity API contract
is documented in [AUTH.md](AUTH.md); phased capability and acceptance tracking
is in [IMPLEMENTATION.md](IMPLEMENTATION.md).

Encrypted provider credential storage, explicit verification/enablement, stable
model names/bindings/grants, personal and Project API Keys, four native inference
protocols, immutable request facts, current token/media pricing, and single-node
quota admission are implemented. Distributed enforcement, provider-specific
pricing, and remaining enterprise integrations follow in subsequent work packages.
The initial single-binary packaging does not require future Gateway,
Control Plane, and Data Platform components to share a deployment or availability
boundary. Identity operations currently require the primary database; this does
not establish Gateway independence or production HA.

## Product boundary

RouteX is an AI gateway and control plane for organizations that need to manage
access to multiple AI providers and models. The system is intentionally broader
than a protocol proxy: routing, access control, quotas, credentials, usage,
operations, and governance are first-class concerns.

A deployment is expected to serve one organization while supporting multiple
users, teams, and projects within that organization.

## Product domains

### Gateway

The Gateway is the real-time data plane. Its availability must not depend on
the analytics stack.

Responsibilities include:

- Authenticate RouteX API keys and requests.
- Resolve public model identifiers to internal model identities.
- Select eligible provider/model routes.
- Enforce routing, quota, rate-limit, and concurrency rules.
- Execute upstream requests using provider adapters.
- Handle retry/failover according to explicit policy.
- Emit immutable request and usage facts for asynchronous processing.

### Control Plane

The Control Plane owns configuration and administrative workflows.

Responsibilities include:

- Users, teams, projects, roles, and permissions.
- API keys and their policy scopes.
- Providers, connections, credentials, and provider models.
- Public model catalog and provider routing relationships.
- Quotas, budgets, limits, and routing policies.
- Configuration validation and publication.
- Audit and administrative workflows.

The Control Plane publishes runtime configuration to the Gateway. A temporary
Control Plane outage should not prevent the Gateway from serving traffic using
the last valid configuration.

### Data Platform

The Data Platform processes asynchronous operational data.

Responsibilities include:

- Usage aggregation and reporting.
- Cost calculation and analytics.
- Operational dashboards and insights.
- Historical request analysis.
- Alerting and derived metrics.

A Data Platform failure must not block Gateway request processing.

## Core domain concepts

The initial model should distinguish at least:

- **Provider** — an upstream AI vendor or service identity.
- **Connection** — a technical integration endpoint/protocol for a provider.
- **Credential** — authentication material belonging to a connection.
- **Provider Model** — a model exposed by an upstream connection.
- **Model** — a stable RouteX model identity exposed to users.
- **Route** — the relationship between a RouteX model and an eligible provider model.
- **Project** — a long-lived application/service boundary.
- **API Key** — a credential owned by a user or project and constrained by policy.
- **Usage Event** — an immutable fact emitted by the Gateway for accounting and analytics.

Internal identifiers should remain stable even when public model names change.

## Extension model

RouteX should make optional capabilities extensible without requiring them to be
compiled into or licensed as part of the core project.

Potential extension points include:

- Authentication and identity providers.
- Secret stores and credential backends.
- Policy and authorization engines.
- Audit and event sinks.
- Routing strategies.
- Usage, billing, and analytics exporters.
- Provider adapters and protocol integrations.

The preferred model is a stable API or RPC boundary. Out-of-process extensions
should be favored when they improve isolation, independent deployment, version
compatibility, or security. Language-specific in-process plugin mechanisms
should not become a prerequisite for extending RouteX.

This extension model is intentionally neutral: extensions may be open source,
internal to an organization, or distributed separately under other terms.

## Design principles

### Keep the hot path small

Authentication, route selection, policy enforcement, and request forwarding
belong on the hot path. Reporting, analytics, notifications, and expensive
aggregation do not.

Operational notification publication starts from durable, server-owned facts.
Source writes never depend on notification success. A bounded background worker
reconciles missing occurrences, claims immutable SMTP intents with leases, and
records explicit terminal outcomes outside the Gateway request path. See
[Operational alerts and notifications](NOTIFICATIONS.md).

Provider quality reads immutable upstream-attempt snapshots. Historical rows
without a Provider snapshot remain unattributed and are never joined to the
mutable catalog or final logical-call route. A bounded evaluator persists closed
quality windows and publishes only durable state transitions. See
[Provider quality](PROVIDER_QUALITY.md).

### Fail explicitly

Unsupported provider capabilities or pricing combinations should return clear
errors rather than silently approximating behavior.

### Native protocols first

RouteX may provide compatibility interfaces, but provider adapters should retain
enough provider-native semantics to avoid forcing every upstream into a lowest
common denominator.

### Configuration is versioned runtime state

Administrative edits should become effective only after validation and
publication. Gateways consume validated runtime state rather than reading
mutable administrative tables on every request.

### Credentials are secrets

Plaintext provider credentials should never be exposed after creation unless a
specific workflow requires it. Secret storage must be abstractable so external
vault systems can be integrated without changing core credential semantics.

### Extensions should remain optional

Core RouteX behavior must not depend on proprietary or separately distributed
extensions. Stable contracts should allow optional capabilities to evolve
without forcing unrelated changes into the core.

## Implementation sequence

Identity bootstrap, persistent sessions, the protected web workspace, and the
database development/test lifecycle are implemented. Provider connections,
encrypted credentials, stable model identities/grants, and personal API Keys
are implemented with controlled PostgreSQL/MySQL acceptance. The native gateway
and request records have controlled ordinary/streaming and restart evidence.
Immutable runtime publication, separately leased authorization, and a bounded durable local call journal are implemented. The journal is transport storage; relational domain models remain in GORM-managed PostgreSQL/MySQL.

Each running RouteX process also registers a distinct process generation in the
primary database. Server-owned heartbeats and leases provide the authoritative
liveness boundary; the browser receives the evaluated state and never derives
it from local time. Registration captures bounded build and runtime metadata plus
nullable resource samples. The current single-binary deployment reports the
truthful `combined` role and does not imply leader election or separate worker
roles.

Recent runtime publication, durable call delivery, and storage cleanup runs are
persisted as bounded, allowlisted system jobs. Operational reporting is
best-effort and cannot change the outcome of the work it observes. A read-time
reconciliation converts work whose executor has stopped or lost its lease into a
safe failed state instead of presenting it as active indefinitely. Revision-
checked offline cleanup retires only reviewed stale registrations; current and
live generations are protected, and a surviving process can recover its own
registration on a later heartbeat. See [SYSTEM_STATUS.md](SYSTEM_STATUS.md) for
the API, authority, and failure contracts.

Member/role/registration interfaces and Team/Project governance backends are implemented.
Project Keys retain Project ownership independently of their creators. Personal and
Project Key retirement requires a confirmed replacement and a persisted successful
call. Transactional offboarding preserves resource continuity while removing user
authority and personal credentials. Resource and offboarding interfaces, automatic
planned execution, and external delivery integrations remain in progress. Metering and quotas, additional native protocols, and enterprise
integrations remain later phases. See
[IMPLEMENTATION.md](IMPLEMENTATION.md) for the dependency order, protocol matrix,
acceptance cases, and unverified deployment boundaries. A complete capability requires its
API, UI, persistence, and tests; separate backend and UI commits do not establish
full capability acceptance on their own.


## Explicit Team Session inference boundary

[Team Session inference](TEAM_INFERENCE.md) uses a typed Session identity alongside
the existing Key identity in the native attempt pipeline. Private short-leased
Session hashes, current exact membership and Team model grants are published
without changing Provider configuration digests. Per-attempt authorization and
local revocation tombstones preserve Control Plane failure isolation; absent or
expired publication fails closed.

Text-only Chat retains one immutable Team subject and stable Team/User journal
accounts. Personal/Project Key authority and ledgers remain separate. Current
members can read only their own Team call facts. [Finite Team policies](TEAM_LIMITS.md)
publish independently authorized aggregate/member rules into leased immutable
snapshots and atomically admit both stable accounts without synchronous Control
Plane reads. Approval workflows, additional Session protocols and fleet-wide
immediate revocation remain separate acceptance boundaries.
