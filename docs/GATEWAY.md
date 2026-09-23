# OpenAI Chat Gateway

## Supported Endpoints

The initial gateway supports `GET /v1/models` and `POST /v1/chat/completions` with `Authorization: Bearer <personal-api-key>`. Browser session cookies do not authorize these endpoints. Keys must be confirmed, active, unexpired, and owned by an enabled user. Effective model access is the intersection of the key's stable model scope and the owner's current grants; an administrator has no implicit inference bypass.

`GET /v1/models` returns an OpenAI-compatible list of current public names visible to that effective scope. Listing a model does not guarantee that it has a currently usable route. Chat requests accept the current name or an unexpired compatibility alias. Expired aliases and inaccessible models receive the same `model_not_found` response. Routing and authorization use the stable internal model ID.

Chat requests preserve native JSON parameters and replace only the outbound `model` with the selected provider model's name. Returned JSON and SSE chunks replace their `model` field with the caller's public name. The configured connection base URL must include the provider API prefix, such as `https://api.example.com/v1`; the gateway appends `/chat/completions`.

## Routing and Credential Selection

The gateway reads all bindings for the requested model and OpenAI Chat protocol. Weights must be nonnegative and total exactly 100. A cryptographically random weighted choice selects one binding; zero-weight candidates cannot receive traffic. Within that connection, only enabled, verified credentials with discovery evidence for the selected provider model are eligible. Lower numeric priority wins, followed by creation time and stable credential ID.

The selected credential is decrypted against its immutable credential reference and sent only in the intended upstream request. The shared outbound client enforces URL, DNS/IP, TLS, proxy, and redirect restrictions described in [Credential Storage and Upstream Network Policy](SECRET_STORAGE.md). Client headers, cookies, and authorization are not forwarded. Each request receives a generated `X-Request-ID`, which is also sent to the upstream.

This initial version makes exactly one attempt. An unavailable selected connection returns an error; it does not silently redistribute configured weights, switch protocols, retry a request, or replay a partial stream. Dynamic health-based failover and credential retry remain separate work.

## Streaming, Cancellation, and Errors

Ordinary requests use the outbound client's 30-second total timeout. Streaming requests keep the guarded transport but remove that total client timeout; a five-minute request context bounds the stream. The transport retains its 30-second response-header timeout. Client cancellation closes the upstream request. Streaming output is read one bounded event at a time and flushed incrementally, providing backpressure without accumulating the complete response. Individual downstream writes have a 30-second deadline.

The request body limit is 4 MiB, ordinary response limit is 16 MiB, and SSE event limit is 1 MiB. OpenAI JSON `data:` events and the terminal `[DONE]` event are supported. SSE comments and non-data fields are ignored. Missing `[DONE]`, malformed JSON, oversized events, and upstream error events terminate the stream with a generic error event when the caller is still connected. No retry occurs before or after output starts.

Errors use an OpenAI-style `error` object containing `message`, `type`, and `code`. Upstream error bodies and headers are not relayed: HTTP 429 becomes a sanitized 429, upstream timeouts become 504, and other upstream failures become 502. Internal database errors and credential details are never returned. An upstream success payload is application content and is not an arbitrary content-redaction service; upstreams must not embed credentials in successful model output.

## Call Facts

Authenticated chat requests record stable request, user, key, model, connection, and provider-model identifiers, request and attempt timing, stream mode, completion status, and generic error codes. Ordinary responses and final SSE usage events contribute prompt and completion token counts when explicitly reported. Missing or invalid usage remains unknown rather than becoming zero. Prompts, completions, credential identifiers, secrets, and raw upstream errors are not stored.

Call recording uses a separate context with a three-second timeout so client cancellation does not cancel persistence. Request ID deduplication prevents repeated accepted facts from doubling usage. Recording failure is logged with the generated request ID; a response already sent cannot be changed retroactively. A durable spool and recovery after database failure are not implemented yet, so this version cannot promise lossless metering during storage outages.

## Current Phase Boundary

This implementation reads current key/grant/routing state from the database and decrypts the selected credential while preparing each request. It does not yet publish or retain a last-valid runtime snapshot. A database outage can therefore block authorization and routing. Snapshot publication, secret preparation outside the hot path, explicit revocation propagation, and outage recovery remain open P1 requirements; the complete P1 stage must not be marked accepted on the basis of these endpoints alone.

Real-provider acceptance also remains separate from controlled-upstream tests. No external provider credentials are required by the local integration suite.

## Verification

```bash
go test -race -tags development ./internal/routex/service ./internal/routex/handler
go tool task test-integration
```

The isolated PostgreSQL/MySQL lifecycle tests cover discovery and credential verification, explicit grants and confirmed keys, real ordinary and streaming HTTP proxying, response sanitization, aliases and expiry, client cancellation, disabled credentials, grant revocation, key revocation, and persisted usage facts. Unit tests cover zero-weight exclusion, valid totals, native request parameters, malformed streams, bounded events, and secret-safe error responses.
