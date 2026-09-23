# Native Responses gateway

This slice implements foreground `POST /v1/responses` using a separate
`openai_responses` connection protocol. Requests use the same RouteX personal or
Project bearer keys, explicit model grants, aliases, runtime snapshots, provider
eligibility, credential verification, admission limits, and durable call journal
as Chat Completions. Routes remain protocol-specific; RouteX never translates a
Responses request into Chat Completions or falls back across protocols. Configured
binding weights total 100 independently for each protocol. Disabled provider
models and revoked credentials remain ineligible without changing saved weights.

Both member `GET /api/v1/models` and bearer `GET /v1/models` expose additive
`protocols` arrays containing the currently eligible native protocols, sorted by
name. An empty array means the model identity is granted but currently has no
usable route. The member endpoint retains its legacy `protocol` field as the
first eligible protocol, or an empty string. Consumers must use `protocols` for
capability selection; neither list exposes provider names or connection details.

The implementation preserves native input, function/custom tools, reasoning,
structured text, and unknown parameter values. It rewrites the public model to
the selected provider model before dispatch, and rewrites only native response
model identity on the way back. It does not inject Chat Completions stream options.
Ordinary output objects and typed SSE output, tool, reasoning, refusal, and
lifecycle events keep their native structure and sequence numbers. The official
[create reference](https://developers.openai.com/api/reference/cli/resources/responses/methods/create)
and [streaming guide](https://developers.openai.com/api/docs/guides/streaming-responses)
define those native structures.

## Supported state boundary

Clients send full inline input or stateless history, including function call
outputs and reasoning items. Existing provider resources are not RouteX-owned
resources: non-null `previous_response_id`, `conversation`, saved `prompt`, item
references, file IDs, vector-store IDs, existing containers, and connected-account
references, including file-based image masks, are rejected. The check follows actual input/tool fields; identically
named properties inside user function schemas or text are not treated as resource
access. Inline image/file data and native remote image/file URLs can be forwarded;
RouteX does not fetch those URLs. Their pricing is explicitly unsupported by the
text adapter. File-search tools and unsupported native input/tool types are rejected.
An automatic new code-interpreter container and caller-supplied MCP server URL do
not grant access to a preexisting provider resource, but incur unsupported tool
pricing and do not imply resource retrieval APIs.

`background:true` is unsupported. Client disconnect or timeout cancels the active
upstream HTTP request. This does not implement background cancellation: the
[official background contract](https://developers.openai.com/api/docs/guides/background)
uses separate stored-response APIs, while synchronous cancellation closes the
connection. `store` is preserved exactly as supplied or omitted; upstream defaults
and retention therefore still apply. RouteX never stores prompts, outputs, or
provider response bodies in its call journal. The official
[conversation-state guide](https://developers.openai.com/api/docs/guides/conversation-state)
describes upstream storage behavior.

Retrieve/delete response, input-items, input-token counting, compact, background
cancel, Conversations, and WebSocket endpoints remain unsupported. No response-ID
ownership/credential-affinity map is claimed. A returned upstream response ID does
not grant access through RouteX to any of these endpoints.

## Errors and finality

Native error envelope shape, HTTP status, and recognized safe error identifiers
are retained. Messages, tool-result error fields, and diagnostic extras are sanitized, and unknown error
identifiers receive a generic code. This is a deliberate security boundary, not
byte-for-byte error-body passthrough. No provider credential or raw error body is
persisted or reflected. Native failed/incomplete response objects and SSE terminal
events remain native responses rather than being converted to chat chunks.

A stream completes only with an authoritative `response.completed`,
`response.failed`, or `response.incomplete` event. Bare EOF or a chat `[DONE]`
marker is insufficient. Error events, malformed envelopes, oversized events,
transport failures, and cancellations produce one request fact and one actual
attempt at most. Limits and journal reservation happen before upstream dispatch.
Existing Chat Completions behavior remains unchanged.

## Metering

The dedicated adapter reads `usage.input_tokens`, `output_tokens`, and
`input_tokens_details.cached_tokens` / `cache_write_tokens`. Input includes both
cache categories; reasoning details are already included in output. Missing or
invalid counters remain unknown. Current official
[prompt caching documentation](https://developers.openai.com/api/docs/guides/prompt-caching)
provides these native categories and their relationship.

Only complete usage from a terminal native response can settle against the
immutable rate/FX basis captured before dispatch. Final usage can be chargeable
even for incomplete/failed outcomes or a client cancellation after the terminal
usage was received. Cancellation before final usage is not priced. No incremental
usage merging, token estimation, latest-catalogue settlement, or duplicate terminal
charging is allowed. Hosted tools, non-text inputs/outputs, unsupported cache
conditions, and non-default service tiers remain explicitly unpriced.

Controlled-upstream tests provide protocol evidence without paid external calls.
Real-provider acceptance and the remaining stateful APIs remain separate gates.

## Verification

`TestResponses*` exercises native parameter preservation, schema-safe ownership
checks, protocol isolation, admission before dispatch, safe native errors, typed
SSE ordering/identity, whole-frame usage replacement, malformed/oversized streams,
and cancellation before and after authoritative usage. `FuzzResponsesNativeEvents`
checks malformed event parsing without contacting an upstream.

`testResponsesLifecycle` runs from the shared PostgreSQL/MySQL integration harness
on an empty migrated database. Its controlled HTTP upstream verifies the native
endpoint and model rewrite. The test covers separate Chat/Responses routing under
one public model, actual protocol listings, provider-model disable, ordinary and
SSE receipts, sanitized 429 errors, rejected cross-resource references, canceled
calls, exact decimal pricing, and journal restart/replay without duplicate facts.
The database suite is a separate validation gate from parser/unit tests.
