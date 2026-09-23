# Contributing to RouteX

Thanks for your interest in RouteX.

## Local verification

Follow the [development setup](README.md#development) to install dependencies and tools. Before submitting changes, run `go tool task check`, `go tool task test`, `npm --prefix website run lint`, and `go tool task build`. The automated unit tests do not require a running database; starting the application does.

See [AGENTS.md](AGENTS.md) and `.agents/rules/` for code organization and API, frontend, security, and testing conventions.

## Development principles

- Keep the gateway data plane independent from asynchronous analytics.
- Prefer explicit domain boundaries over shared global state.
- Preserve provider-native behavior where RouteX does not intentionally define an abstraction.
- Treat authentication, credentials, quotas, routing, and audit data as security-sensitive.
- Add tests for behavior changes and bug fixes.
- Prefer stable extension interfaces over hard-coding optional integrations into the core.

## Licensing

Unless explicitly stated otherwise, contributions to RouteX are accepted under the Apache License 2.0.

By submitting a contribution, you represent that you have the right to submit it under the project license. Contributions intentionally submitted for inclusion in RouteX are governed by the contribution terms in Section 5 of the Apache License 2.0 unless a separate written agreement applies.

## Pull requests

Keep pull requests focused. Explain the problem being solved, the behavior change, and any compatibility or migration impact. Larger architectural changes should be documented before substantial implementation begins.
