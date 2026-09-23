# Call Facts and Query API

Call facts record completed gateway requests without storing prompts, responses, credentials, or raw upstream diagnostics. This phase covers individual users and personal API Keys. Team and Project attribution, monetary calculation, CSV export, aggregate analytics, and durable asynchronous event ingestion are separate work packages.

## Recording Contract

The gateway assigns a canonical server-generated `RequestID` and records one `service.CallFact` after an authenticated request succeeds, fails, or is canceled. Each upstream attempt has a separate ID. A rejection before dialing an upstream has no attempt. Unauthenticated traffic has no trusted user attribution and is not stored as a personal call fact.

A fact contains stable user, Key, model, provider-model, and connection IDs; the public model name used for attribution; protocol; outcome; streaming flag; start and completion timestamps; duration; optional input/output token counts; a safe error classification; and attempts. It contains no credential ID, Authorization value, plaintext secret, request body, response content, or upstream error text. The current protocol is `openai_chat`; outcomes are `success`, `error`, and `canceled`.

Unknown token usage remains `null`. It is not converted to zero or inferred from text length. Monetary values are not calculated in this phase. An error code outside the predefined internal classifications is replaced with `upstream_error`, so an accidental provider message cannot become a stored diagnostic.

`RecordCall` uses a transaction and a unique request ID. Replaying that ID leaves the first accepted fact and its attempts unchanged. Concurrent duplicate delivery therefore cannot double usage. Attempt ID conflicts roll back the new fact instead of producing an incomplete record. Plain unique inserts establish deduplication independently of MySQL's affected-row behavior.

Storage uses microsecond timestamp precision on both databases. Schema version 5 introduces `call_records` and `call_attempts`. Attempts reference their canonical fact. IDs referencing users, Keys, and catalog resources are historical snapshots without cascading foreign keys, so later lifecycle changes cannot erase attribution. Queries never require the original resource to remain active or visible.

## Persistence Failure Boundary

Recording must use a bounded context that survives client cancellation. The gateway records after completion using a separate timeout, so a disconnected client does not automatically cancel persistence. A recording failure cannot rewrite an already streamed response. This first synchronous implementation has no durable outbox or replay spool: if the database is unavailable during recording, that fact may be lost and a sanitized operational error must be logged. This is an explicit release limitation, not a claim of lossless ingestion.

## Access and HTTP API

All paths are relative to `/api/v1` and require a valid session. Member endpoints always add the current user ID to the database query. Administrator endpoints additionally require the `admin` role. A request for another user's fact returns the same `404` as a missing fact.

| Endpoint | Result |
|---|---|
| `GET /calls` | Current user's call facts |
| `GET /calls/:request_id` | Current user's safe call detail |
| `GET /admin/calls` | All users' call facts, with actor IDs |
| `GET /admin/calls/:request_id` | Administrator detail with safe routing and attempt metadata |

Lists return `{items: [...], next_cursor: string | null}`. Supported filters are `status`, `model_id`, `key_id`, `from`, and `to`. Timestamps use RFC 3339 and bounds are inclusive. Administrators may also filter by `user_id`. A member cannot use that parameter to select another user.

`limit` defaults to 40 and must be between 1 and 100. `cursor` is opaque to clients. Pagination orders by `started_at DESC, request_id DESC`, so equal timestamps have deterministic order. Filters apply on the server before pagination. Clients must preserve the same filters while following a cursor.

Member items and details contain:

```text
request_id, model_id, model_name, key_id, protocol, status, stream,
started_at, completed_at, duration_ms, input_tokens, output_tokens
```

Administrator list items additionally contain `user_id`. Administrator detail also contains `provider_model_id`, `connection_id`, `error_code`, and `attempts`. Each attempt contains its ID, provider-model and connection IDs, outcome, HTTP status, safe error code, and timestamps. Member DTOs never include upstream route IDs, attempt diagnostics, error codes, or other users' identities.

Authentication responses and these protected API responses use the shared no-store policy. Invalid filters return a sanitized `400`; unauthorized access returns `401` or `403`; scoped misses return `404`.

## Verification

`testCallLifecycle` runs against PostgreSQL and MySQL through the single isolated database integration lifecycle. It covers concurrent duplicate delivery, immutable accepted facts, attempt conflict rollback, fresh-service persistence, unknown usage, ownership filtering, indistinguishable cross-user misses, member/admin DTO boundaries, safe error classification, deterministic cursor pagination, and time/status/model/Key/user filters.

Gateway tests separately establish that real controlled-upstream success, failure, stream completion, and cancellation produce the corresponding facts. Data-store tests alone do not prove gateway recording behavior. Run `go tool task test-integration` for the real database lifecycle and consult [the implementation record](IMPLEMENTATION.md) for current evidence and remaining acceptance work.
