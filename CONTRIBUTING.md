# Contributing to RouteX

Thanks for your interest in RouteX.

## Local verification

Follow the [development setup](README.md#development) to install dependencies and tools. Before submitting changes, run `go tool task check`, `go tool task test`, `npm --prefix website run lint`, and `go tool task build`. The automated unit tests do not require a running database; starting the application does.

See [AGENTS.md](AGENTS.md) and `.agents/rules/` for code organization and API, frontend, security, and testing conventions.

## Formatting and linting

Frontend formatting is enforced by the pinned Prettier version and
`website/.prettierrc.json`. Run `npm --prefix website run format` to format source,
tests, styles, and configuration. Run `npm --prefix website run lint:fix` to apply
ESLint fixes and then format. `go tool task lint` includes both frontend fixes.
Generated assets, dependencies, coverage, and the npm lockfile are excluded.

`npm --prefix website run format:check` verifies formatting without changing files.
`go tool task check` and CI require both this check and ESLint. Format before
reviewing and committing each change; do not compress JSX, handlers, or tests into
single lines or bypass the formatter with broad ignore directives. Formatting
changes preserve behavior and still require the normal type, test, and build gates.

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
