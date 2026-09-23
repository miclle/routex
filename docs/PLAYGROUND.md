# Playground

Playground is a single-model text conversation client for native OpenAI Chat Completions and Responses. It makes real requests; it does not synthesize responses or usage statistics. Other protocols, session-based inference, attachments, and tools remain separate work packages.

## Workflow

1. Create a personal or Project API Key, save its one-time secret, and confirm delivery to enable it.
2. Open Playground and enter the Key. Select **Verify and load models** to request `GET /v1/models` using that Key.
3. Select a model and one of its currently eligible native protocols. The gateway list reflects effective Key/owner/Project grants. Missing legacy protocol metadata implies Chat support; an explicit empty protocol list does not. Switching protocol clears conversation history.
4. Optionally adjust Temperature, Top P, maximum output Tokens, and the system prompt. Choose streaming or ordinary JSON output.
5. Send a message. The page displays incremental text when streaming, the gateway Request ID, and upstream usage when supplied.
6. Stop an active request to abort the browser fetch and close the response stream. Leaving the page also aborts active requests.

Only successfully completed exchanges are included in later conversation context. Failed or canceled partial answers remain visible but are excluded from future requests. Changing the protocol, model, or Key clears the conversation. Clearing the Key also clears the model selection and conversation. Copy output copies only the selected answer; clipboard failures provide a manual-copy fallback message.

## Transport and Secret Handling

- Native requests use `Authorization: Bearer <key>` against same-origin `/v1/models` , `/v1/chat/completions`, and `/v1/responses`.
- Requests omit browser cookies and reject redirects. Gateway authentication is independent of the control-plane session that protects access to the page.
- The Key remains only in component memory and the password input while the page is mounted. The client never writes it to localStorage, sessionStorage, React Query caches, logs, or generated request examples.
- The native client uses `fetch` and an `AbortController`, not React Query mutations, so request arguments and secrets are not retained in a mutation cache.
- Native error messages retain useful gateway context, with the supplied Key redacted if it appears in a message. Output is rendered as plain text, not executable HTML.
- Responses are not persisted. Reloading or leaving the page discards the conversation and entered Key.

## Response Handling

Chat Completions ordinary responses consume `choices[0].message.content` or a textual refusal. Streaming responses consume SSE `data:` events, decode UTF-8 across transport chunks, support LF and CRLF frame boundaries, and append `choices[0].delta.content` or textual refusal deltas. Empty usage-only chunks are supported. `[DONE]` completes a stream.

A stream that closes without `[DONE]`, a malformed event, an unexpected content type, or a native error event is reported as a failure while preserving already received text. The client never silently retries a request after output has started. The page displays `X-Request-ID` when available. Missing usage remains explicitly unavailable; it is not estimated from text.

Responses requests use native `input`, `instructions`, and `max_output_tokens`, with full inline text history and no `previous_response_id`, stored-state references, or Chat stream options. Protocol selection never falls back to another endpoint. The page renders output text and refusal content; other native output items are not rendered, executed, or replayed. Tool configuration, structured-output editors, and reasoning-history replay remain outside this text interface.

Responses SSE consumes typed text/refusal deltas and completes only on a coherent `response.completed`, `response.failed`, or `response.incomplete` envelope. Final text and complete usage replace incremental display state. Bare EOF, Chat `[DONE]`, malformed events, and mismatched terminal status remain failures. HTTP 202 or a queued/in-progress ordinary response is labeled accepted but not completed; the page does not poll or retrieve stored response IDs. Failed, incomplete, accepted, and canceled turns are excluded from subsequent history. Token counts come only from a complete authoritative terminal usage object, including for failed/incomplete outcomes; missing counters are not estimated.

To bound browser memory, an individual buffered SSE event is limited to 1,048,576 JavaScript string code units, and accumulated answer text to 2,097,152 code units. Exceeding either limit cancels stream reading and reports an error. These are frontend display limits, not model context-window or gateway capacity claims.

## Verification

`website/src/api/playground.test.ts` covers native authorization, request parameters, ordinary output, split UTF-8/CRLF SSE, usage chunks, partial stream failures, truncation, malformed events, unexpected content types, HTTP errors, secret redaction, and cancellation.

`website/src/api/playground-responses.test.ts` additionally covers native Responses bodies, accepted HTTP 202, typed terminal states, authoritative usage, refusals, malformed/truncated streams, non-text output, secret redaction, and cancellation.

`website/src/views/playground/playground.test.tsx` covers Key verification, model selection, ordinary and streaming invocation, incremental output, request IDs, usage, cancellation, successful-only conversation history, clearing secrets, verification recovery, and unmount abort.

Run `npm --prefix website test`, `npm --prefix website run lint`, and `npm --prefix website run build`. These tests validate the client with controlled responses. They do not establish real-provider compatibility or production readiness; gateway integration and real-provider smoke tests remain separate evidence.

## Model comparison

The Model conversation and Model comparison tabs keep separate transient workbenches. Switching tabs destroys the hidden workbench, aborts its active fetches, and clears its entered Key and history. The conversation workbench retains its existing settings/transcript layout.

Comparison uses a shared credential toolbar, two initial model columns, and one shared message composer. Add comparison creates up to four columns; remove controls appear only above the two-column minimum. Columns scroll horizontally with a 300-pixel minimum width. Each column selects its own currently eligible Chat or Responses protocol and displays the actual endpoint, partial output, terminal status, Request ID, observed elapsed time, and authoritative usage. Verification uses one transient personal or Project Key, and models with explicit empty protocol capabilities are unavailable.

Send captures one message and concurrently dispatches an independent native request to each selected column. The comparison defaults are streaming, Temperature 0.7, Top P 1, and 2,048 maximum output Tokens. Per-column Stop only aborts that column; failure or cancellation does not stop siblings. Removing a column or changing its model/protocol cancels that column's request and clears only its history. Global sending waits until all current requests settle, with a synchronous guard against duplicate dispatch.

Subsequent requests include only the successful text history of their own column. Failed, incomplete, accepted, and canceled turns remain visible but are excluded from later context. Clear all resets all histories; clearing/changing the Key also resets models and drafts. There are no simulated responses, session-inference controls, or nonfunctional attachment actions.

`website/src/views/playground/compare.test.tsx` covers the two-to-four column bounds, eligible protocols, concurrent native bodies, independent histories/errors/cancellation, duplicate sends, per-column resets, credential clearing, tab teardown, and bilingual accessibility/draft preservation. All client tests use controlled mocked transports, separate from paid-provider or real gateway acceptance.
