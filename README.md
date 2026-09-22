# RouteX

RouteX is an open-source AI gateway and control plane for connecting applications to multiple AI providers and models through a governed, observable, and extensible platform.

> RouteX is at an early stage. The project is being implemented from the ground up; the existing product mockups and design exploration are maintained separately.

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

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the initial architecture principles.

## Project status

RouteX is currently in the initial implementation phase. APIs, data models, package boundaries, extension interfaces, and deployment topology may change before the first stable release.

## License

RouteX is licensed under the [Apache License 2.0](LICENSE).

See [NOTICE](NOTICE) for attribution information.

## Contributing

Contributions are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) before submitting changes.
