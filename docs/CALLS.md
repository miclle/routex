# Call Facts and Query API

Call facts record completed gateway requests without storing prompts, responses, credentials, or raw upstream diagnostics. Facts support personal or Project attribution, exactly one per request. Team attribution, monetary calculation, CSV export, and aggregate analytics are separate work packages. Durable event ingestion is implemented for the single-process deployment.

## Recording Contract

The gateway assigns a canonical server-generated `RequestID` and records one `service.CallFact` after an authenticated request succeeds, fails, or is canceled. Each upstream attempt has a separate ID. A rejection before dialing an upstream has no attempt. Unauthenticated traffic has no trusted user attribution and is not stored as a personal call fact.

A fact contains stable user, Key, model, provider-model, and connection IDs; the public model name used for attribution; protocol; outcome; streaming flag; start and completion timestamps; duration; optional input/output token counts; a safe error classification; and attempts. It contains no credential ID, Authorization value, plaintext secret, request body, response content, or upstream error text. The current protocol is `openai_chat`; outcomes are `success`, `error`, and `canceled`.

Unknown token usage remains `null`. It is not converted to zero or inferred from text length. Monetary values are not calculated in this phase. An error code outside the predefined internal classifications is replaced with `upstream_error`, so an accidental provider message cannot become a stored diagnostic.

`RecordCall` uses a transaction and a unique request ID. Replaying that ID leaves the first accepted fact and its attempts unchanged. Concurrent duplicate delivery therefore cannot double usage. Attempt ID conflicts roll back the new fact instead of producing an incomplete record. Plain unique inserts establish deduplication independently of MySQL's affected-row behavior.

Storage uses microsecond timestamp precision on both databases. Schema version 5 introduces `call_records` and `call_attempts`. Attempts reference their canonical fact. IDs referencing users, Keys, and catalog resources are historical snapshots without cascading foreign keys, so later lifecycle changes cannot erase attribution. Queries never require the original resource to remain active or visible.

## Persistence Failure Boundary

The process opens a private bbolt journal before listening. It synchronously reserves a slot with a safe interruption fact before dispatching an upstream request, then replaces that reservation with the final fact after completion. Default synchronous bbolt commits remain enabled. The journal stores only the same allowlisted attribution and usage fields as the relational fact; it never stores request/response content or provider credentials.

The journal admits at most 4,096 entries, each at most 64 KiB. This bounds logical payload to 256 MiB, not physical file size: page metadata and the file high-water mark require additional disk space. A full or unwritable journal rejects new upstream dispatches with HTTP 503 and `event_buffer_unavailable`. A final journal-write failure cannot change an already sent response; the reserved fallback survives and is recovered as `process_interrupted` with unknown usage after restart. Filesystem or device loss beyond successful durable commits is outside this guarantee.

A background worker delivers up to 64 facts per cycle, with a three-second timeout per fact and a one-second interval. It acknowledges a journal entry only after an idempotent relational transaction commits. A crash between commit and acknowledgment replays the entry without replacing accepted usage or duplicating attempts. Database outages retain ready facts for retry. Pending admissions left by a stopped process become explicit interruption facts during journal reopening.

The HTTP shutdown drains requests before closing the journal. Ready facts do not need to finish delivery before shutdown because they remain on disk. Configure `event_queue_path` on persistent local writable storage; one process exclusively owns each journal file. Do not share a journal between instances or delete it during an outage. Journal files are created with mode 0600 and new directories with mode 0700. Relational schemas and business data continue to use GORM with PostgreSQL/MySQL; bbolt is only the bounded local transport journal.

Authorization during a primary-database outage is separately bounded by the five-second lease in [RUNTIME](RUNTIME.md). Buffering does not extend authorization indefinitely.

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

`testRecorderLifecycle` verifies database-outage buffering, process reopen, pending interruption recovery, commit-before-acknowledgment replay, private file permissions, secret exclusion, and exact-once accepted facts on both databases. Journal unit tests cover capacity, concurrent admission, process locking, and atomic completion. These correctness tests do not establish the production event-latency target.

## Project history

`GET /projects/:project_id/calls` and `GET /projects/:project_id/calls/:request_id` expose sanitized facts to current Project managers or holders of `calls.read_all`. Lists use the existing filters and pagination, with the Project forced by the path. A former creator or unrelated user receives the same 404 as a missing resource. Disabled and archived Projects retain readable history for authorized readers. Project facts contain a Project ID and an empty user ID; they never appear in the creator's personal history. Administrative fact DTOs include `project_id` when applicable. Migration 9 adds this historical attribution column without altering accepted personal facts.
