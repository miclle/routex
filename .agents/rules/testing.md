# Testing Rules

These rules apply to Go tests, frontend tests, and verification commands.

## Go Tests

- Prefer table-driven tests with `t.Run()` for multiple cases.
- Use clear human-readable test names.
- Use the standard library or existing project test helpers before adding new test dependencies.
- New `pkg/` packages should include focused unit tests.
- Database behavior should use isolated test databases or be skipped with a clear environment variable gate.

## HTTP Tests

- Prefer handler-level tests through the real router for route binding, status codes, and API shape.
- Test path, query, body, and error cases for non-trivial endpoints.
- Avoid testing service internals through HTTP tests unless the behavior is user-visible.

## Frontend Tests

- Use Vitest and keep tests near the code they cover.
- Test API clients, hooks, route helpers, and state derivation before pure layout details.
- Add focused tests for shared UI primitives that wrap headless behavior such as Base UI tabs, menus, or dialogs.
- Mock network calls through the API boundary rather than hard-coding backend URLs.

## Verification

- Run `go tool task check` before committing.
- Run `go tool task test` for behavior, API, database, or UI interaction changes.
- `go tool task lint` may modify files through `go mod tidy` and `gofmt`; inspect the diff afterward.
- If a command cannot run locally, report the command, the reason, and the remaining risk.
