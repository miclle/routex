# Native Messages gateway

RouteX supports foreground `POST /v1/messages` through the `anthropic_messages`
connection protocol. It shares personal/Project Key authorization, explicit model
grants, per-protocol weighted routing, provider-model eligibility, immutable
runtime snapshots, admission limits, and durable request facts. It never converts
a Messages request to Chat Completions or Responses, and never falls back across
protocols. The configured upstream Base URL includes its API prefix, normally
`https://api.anthropic.com/v1`.

## Native contract

Use a RouteX Key in `x-api-key`, or use the existing bearer authorization form.
Supplying both forms is rejected to avoid ambiguous credentials. Exactly one
`anthropic-version` value is required and forwarded; its supported shape is a
10-character ISO date. `anthropic-beta` values are preserved, including repeated
headers, within 4 KiB, 32 names, and 128 ASCII token characters per name. The
selected provider credential replaces client authentication upstream. Only these
native version/beta headers are forwarded. Client workspace/profile selection
headers are rejected because they cannot authorize access to provider-owned
resources. Response `request-id`, `X-Request-ID`, and native error `request_id`
refer to the RouteX request identity.

Native `messages`, `system`, client tools and tool results, stop sequences,
thinking/signatures, structured output, and additional JSON parameters retain
their structure and values. RouteX only rewrites model identity. It allows
`max_tokens: 0` for native cache warm-up. The gateway limits are currently 4 MiB
requests, 16 MiB ordinary responses, 1 MiB SSE events, and five minutes per request;
these are RouteX bounds, not a claim to support every upstream size limit.

The authoritative contracts are the [Messages API](https://platform.claude.com/docs/en/api/http/messages/create),
[API versions](https://platform.claude.com/docs/en/api/versioning), and
[beta headers](https://platform.claude.com/docs/en/api/beta-headers).

## Resource ownership

Inline text, images, documents, client tool history, and signed/redacted thinking
history are supported. Opaque tool inputs and schemas are never recursively
interpreted as resource selectors. Actual file sources, file-based citations,
container uploads/reuse, skills, and provider-owned session/connector references
are rejected until RouteX has an ownership and credential-affinity map. Inline
URL sources are forwarded for the provider to fetch; RouteX does not fetch them.
Server-tool history block types outside the supported inline subset are rejected.
URL-based MCP configuration may be forwarded but remains unpriced.

Files, Skills, Batches, token-counting and managed state endpoints are outside this
slice. The existing public `/v1/models` remains the RouteX model-list contract;
this does not implement the native SDK's Models resource envelope.

## Streaming, errors, and cancellation

Native message, content-block, text/tool-input/thinking/signature delta, ping, and
future event types are forwarded. Known lifecycle events are checked for ordering,
block identity, duplicate starts, and cumulative usage regression. A successful
stream requires a final stop reason followed by `message_stop`; EOF alone is
incomplete. The first terminal event ends the request, preventing duplicate
terminal output and charging. Client disconnect cancels the upstream context.

Native HTTP error statuses, including 529, and recognized error types are retained.
Messages and diagnostic extras are sanitized; error request IDs use RouteX's
identity. Mid-stream errors retain the native error envelope. The gateway does
not retry. See the official [streaming](https://platform.claude.com/docs/en/build-with-claude/streaming)
and [error](https://platform.claude.com/docs/en/api/errors) contracts.

## Discovery and verification

Verification uses the provider's [Models API](https://platform.claude.com/docs/en/api/http/models/list)
with its encrypted credential in `x-api-key` and version `2023-06-01`. Pagination
uses `limit=1000`, `after_id`, `has_more`, and `last_id`. It is bounded to ten
seconds, 2 MiB total response bytes, 20 pages, and 2,000 unique models. Malformed
pages, cursor cycles, exceeded bounds, and unsupported/unauthorized endpoints fail
verification; partial results never become current verified coverage.

Successful discovery replaces coverage atomically. Failed Messages reverification
retains the last successful model/access rows as evidence, but marks the credential
failed and disabled and revokes runtime authorization. Historical coverage is not
proof of current usability. Other protocols retain their existing failure behavior.
No billable inference request is used as a fallback verification probe.

## Exact metering

The native adapter normalizes total input as uncached input plus cache-read and
cache-creation tokens, with overflow checks. It does not infer missing categories
as zero. Output already includes thinking tokens. Streaming uses the initial input
snapshot and replaces cumulative output values from `message_delta`; it never sums
those deltas. Invalid present counters become unknown instead of inheriting an
earlier value. Finality requires `message_stop`.

The captured rate/FX snapshot settles supported text, client tool, and thinking
usage. Five-minute cache writes require explicit, consistent native TTL breakdown;
one-hour, mixed, or unknown write accounting remains unpriced. Hosted tools,
non-text input, nonstandard tier/region/speed, context-management, and multiple
internal iterations remain explicitly unsupported pricing dimensions. Stop reason
`max_tokens`, refusal, or tool handoff does not itself make valid complete usage
unbillable. Cancellation before final usage is unpriced; cancellation after the
terminal usage was received can retain an exact charge. See [cache accounting](https://platform.claude.com/docs/en/build-with-claude/prompt-caching)
and [thinking accounting](https://platform.claude.com/docs/en/build-with-claude/extended-thinking).

`TestMessages*` covers headers, ownership, discovery bounds, protocol isolation,
SSE ordering, cumulative counters, TTL conditions, and cancellation. The shared
PostgreSQL/MySQL `testMessagesLifecycle` verifies native HTTP behavior, failed
reverification, exact prices, usage filtering, journal replay, and one persisted
request fact. Tests use controlled upstream servers; real-provider acceptance is
still a separate gate.
