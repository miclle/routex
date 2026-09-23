# fox-gonic Handler Rules

RouteX uses `github.com/fox-gonic/fox`, not raw Gin. The application entrypoint is `cmd/routex`, and route registration is centralized in `internal/routex/handler/handler.go`.

## Core Rules

- Prefer returning values from handlers so fox can render responses consistently.
- For ordinary JSON APIs, use fox request binding and typed response DTOs instead of ad-hoc `any` contracts.
- Bind path parameters with request structs and `uri` tags when an endpoint has a stable contract.
- Keep request and response DTOs as exported named types when they represent public API shape.
- Do not call service or database code from route registration; route registration should wire handler methods only.

## Handler Organization

- Keep the controller type as `Ctrl` unless a larger module boundary is introduced deliberately.
- Give handler methods resource-specific names, such as `CreateUser` or `ListProjects`, rather than generic `Create` or `List`.
- Put routes in the smallest sensible group under `/api/v1`.
- Document exceptions such as health checks, webhooks, streaming endpoints, and reverse proxies near their route registration.

## Status Codes

- Use `c.Status(http.StatusCreated)` for created responses when the response body is still rendered by fox.
- Use `c.Status(http.StatusNoContent)` before returning `nil` for no-body success responses.
- Keep error mapping predictable; do not return raw database or provider errors to clients.
