# API Contract Rules

These rules apply to HTTP APIs, frontend API clients, tests, examples, and future SDK or CLI contracts.

## Routes

- Main application APIs should live under `/api/v1/...`.
- Register all API routes in `internal/routex/handler/handler.go`.
- Health checks, embedded SPA assets, and clearly documented integration endpoints may live outside `/api/v1`.
- Keep docs, tests, and frontend API clients synchronized with the actual route paths.

## JSON Contracts

- Use explicit `json` tags on Go request and response DTOs.
- Prefer `snake_case` wire fields for API JSON, such as `created_at` and `user_id`.
- Do not support duplicate field spellings for the same meaning unless there is a documented versioning plan.
- Frontend types should reflect the backend wire contract. If UI code needs camelCase, convert at the API boundary.

## DTO Boundaries

- Do not expose GORM entities as HTTP response bodies.
- Keep HTTP DTOs near the handlers that use them.
- Keep database entities, provider SDK structs, and frontend UI state as separate types.
- When changing an API, check handler DTOs, service inputs/results, frontend `website/src/api/`, frontend `website/src/types/`, tests, and docs.

## Status And Errors

- Creating a resource should usually return `201 Created`.
- Successful deletes or commands with no response body should usually return `204 No Content`.
- Map internal, database, and provider errors to stable HTTP status responses before they reach clients.
- Do not expose DSNs, credentials, SQL details, or provider internals in error responses.
