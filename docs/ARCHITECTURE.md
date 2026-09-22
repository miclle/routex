# RouteX Architecture

This document records the initial architecture direction for RouteX. It is a
starting point rather than a frozen implementation specification.

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

## Design principles

### Keep the hot path small

Authentication, route selection, policy enforcement, and request forwarding
belong on the hot path. Reporting, analytics, notifications, and expensive
aggregation do not.

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
specific workflow requires it. Secret storage must be abstractable so future
enterprise integrations can use external vault systems.

## Initial implementation sequence

1. Define core domain models and configuration contracts.
2. Implement a minimal Gateway with one provider adapter.
3. Add API key authentication and model resolution.
4. Add provider/model administration APIs.
5. Add routing and failover.
6. Add usage events and asynchronous metering.
7. Add quotas and rate limits.
8. Build the web control plane on stable APIs.
9. Expand provider coverage and operational tooling.

The sequence favors a working data plane and stable contracts before recreating
the complete management UI.
