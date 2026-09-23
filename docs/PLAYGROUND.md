# Playground

Playground is a native text conversation and model comparison client for native OpenAI Chat Completions, OpenAI Responses, Anthropic Messages, and Gemini Generate Content. It makes real requests; it does not synthesize responses or usage statistics. Other protocols, session-based inference, attachments, and tools remain separate work packages.

## Workflow

1. Create a personal or Project API Key, save its one-time secret, and confirm delivery to enable it.
2. Open Playground and enter the Key. Select **Verify and load models** to request `GET /v1/models` using that Key.
3. Select a model and one of its currently eligible native protocols. The gateway list reflects effective Key/owner/Project grants. Missing legacy protocol metadata implies Chat support; an explicit empty protocol list does not. Switching protocol clears conversation history.
4. Optionally adjust Temperature, Top P, maximum output Tokens, and the system prompt. Choose streaming or ordinary JSON output.
5. Send a message. The page displays incremental text when streaming, the gateway Request ID, and upstream usage when supplied.
6. Stop an active request to abort the browser fetch and close the response stream. Leaving the page also aborts active requests.

Only successfully completed exchanges are included in later conversation context. Failed or canceled partial answers remain visible but are excluded from future requests. Changing the protocol, model, or Key clears the conversation. Clearing the Key also clears the model selection and conversation. Copy output copies only the selected answer; clipboard failures provide a manual-copy fallback message.

## Transport and Secret Handling

- Model discovery, Chat, and Responses use `Authorization: Bearer <key>` against same-origin endpoints. Messages uses only `x-api-key: <key>` with `anthropic-version: 2023-06-01` against `/v1/messages`; Gemini uses only `x-goog-api-key` against the native `/v1beta/models/{name}` action. Authentication forms are never combined, and the client never places credentials in query parameters.
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

Comparison uses a shared credential toolbar, two initial model columns, and one shared message composer. Add comparison creates up to four columns; remove controls appear only above the two-column minimum. Columns scroll horizontally with a 300-pixel minimum width. Each column selects its own currently eligible Chat, Responses, Messages, or Gemini protocol and displays the actual endpoint, partial output, terminal status, Request ID, observed elapsed time, and authoritative usage. Verification uses one transient personal or Project Key, and models with explicit empty protocol capabilities are unavailable.

Send captures one message and concurrently dispatches an independent native request to each selected column. The comparison defaults are streaming, Temperature 0.7, Top P 1, and 2,048 maximum output Tokens. Per-column Stop only aborts that column; failure or cancellation does not stop siblings. Removing a column or changing its model/protocol cancels that column's request and clears only its history. Global sending waits until all current requests settle, with a synchronous guard against duplicate dispatch.

Subsequent requests include only the successful text history of their own column. Failed, incomplete, accepted, and canceled turns remain visible but are excluded from later context. Clear all resets all histories; clearing/changing the Key also resets models and drafts. There are no simulated responses, session-inference controls, or nonfunctional attachment actions.

`website/src/views/playground/compare.test.tsx` covers the two-to-four column bounds, eligible protocols, concurrent native bodies, independent histories/errors/cancellation, duplicate sends, per-column resets, credential clearing, tab teardown, and bilingual accessibility/draft preservation. All client tests use controlled mocked transports, separate from paid-provider or real gateway acceptance.

## Native Messages

Both workbenches select `anthropic_messages` only when the model advertises that eligible protocol. Requests preserve the native `messages`, top-level `system`, `max_tokens`, and stream fields; there is no Chat/Responses fallback or adapter. The single-model maximum-output field permits native zero-token warm-up and limits Temperature to the native 0–1 range. Version selection is fixed at the gateway-supported discovery version.

Ordinary responses require a native Message with a stop reason. Streaming tracks message start, indexed content-block lifecycles, text deltas, cumulative usage, final stop reason, and `message_stop`. Neither bare EOF nor Chat `[DONE]` is completion. Errors retain already received text and the RouteX request identity. Stop reasons distinguish successful completion, incomplete output, refusal, and tool/continuation handoff. The text interface never executes a tool or silently replays incomplete native state; only completed text turns enter subsequent history.

Displayed input Tokens normalize the native disjoint uncached/cache-read/cache-creation categories. All categories must be present and safe nonnegative integers; omitted/invalid categories remain unknown. Stream output uses the latest cumulative value, never a sum of deltas. An omitted or invalid final output count cannot inherit a previous count. Usage becomes final only after `message_stop`, and valid terminal usage remains visible for refusal, handoff, or truncation outcomes.

Shared transport logic owns same-origin authentication, redirect/cookie exclusion, sanitized native errors, and bounded output helpers. Dedicated protocol parsers retain separate finality and accounting rules. `playground-messages.test.ts` covers native headers and parameters, ordinary/streaming response shapes, lifecycle errors, cumulative and unknown counters, native 529/errors, cancellation, and text-only output handling. Single and comparison workbench tests cover Messages-only discovery, system/history shape, native endpoint labeling, and independent handoff/refusal/incomplete states.

## Native Gemini

Both workbenches select `gemini_generate_content` only when advertised for the selected model. The single-model workbench supports ordinary `generateContent` and SSE `streamGenerateContent`; comparison uses the streaming action. Path identity must match `[A-Za-z0-9][A-Za-z0-9._-]{0,127}` and is encoded as one segment. Discovery currently exposes current public names rather than alias rows. An incompatible selected name receives a localized explanation and is rejected before dispatch; the interface never invents an alias or switches protocol.

Requests contain native `contents` with user/model roles and text parts, optional `systemInstruction`, and `generationConfig` with Temperature, Top P, output limit, and exactly one candidate. The model name and streaming flag determine the URL and are excluded from JSON. Completed text history remains inline and transient. Thoughts, thought signatures, function calls, and other non-text parts are neither rendered nor replayed; a tool handoff is explicit and excluded from future history. Unknown finish reasons remain incomplete, while native safety blocks are shown as refusals.

Gemini has no global terminal SSE event. The client therefore requires clean EOF plus a candidate finish reason or explicit prompt block. It keeps usage unavailable until that boundary and never treats Chat `[DONE]`, a finish frame alone, cancellation, or interrupted transport as completion. Each later usage snapshot replaces the previous snapshot; a malformed or incomplete final snapshot cannot inherit preliminary values. Input is the reported prompt count, which already includes cached input. Output is candidates plus thoughts; missing thought counts remain unknown. Counters must be exact safe nonnegative integers, and a reported total must agree. The RouteX `X-Request-ID` remains separate from native `responseId`.

`playground-gemini.test.ts` covers exclusive authentication, exact native bodies, unsafe paths, split UTF-8, ordinary and SSE finality, final usage replacement and unknown counters, native usage aliases, refusal/truncation/tool outcomes, cancellation before EOF, duplicate candidates, malformed frames, and sanitized errors. Workbench tests cover Gemini-only discovery, system/history mappings, ordinary versus streaming paths, comparison cancellation isolation, successful-only context, and bilingual alias guidance. These are controlled client tests; database and provider acceptance are separate gates.

## Request code

Get code sits beside the single conversation's Clear action. Each comparison lane has a compact code action in its header. The shared dialog provides functional cURL, Python, and JavaScript tabs, a Copy code action, and a manual-copy fallback. Opening it captures the selected model/protocol, current parameters, system instruction, completed history, and current draft. An empty draft uses an explicit message placeholder for the caller to replace. Incomplete and failed turns are excluded, and a comparison example contains only that lane's successful history.

The request builder has no credential input. Every generated language reads `ROUTEX_API_KEY` from the caller's local environment; the transient Key entered in Playground is never substituted. Examples preserve native authentication, routes, parameters, streaming selection, and history roles. Gemini's model identity remains in its validated single-segment path, with no synthetic model/stream body fields. Unsupported paths do not produce copyable examples.

cURL examples target POSIX shells and quote URLs/JSON as literal shell arguments while allowing expansion only in the fixed authentication header. Python examples use Python 3 standard-library `urllib`, reject redirects, and decode their JSON payload rather than inserting JavaScript boolean/null syntax into Python. JavaScript examples run as ES modules in Node.js with native `fetch`, omit cookies, and reject redirects. Both print native response bytes; streaming SSE is not translated into another protocol. No generated example performs an automatic retry or a paid call during preview/copy.

Focused tests execute generated commands against a shell function, a stub Python opener, and a stub JavaScript fetch. They verify native bodies and headers, Unicode/newlines/apostrophes, literal command-substitution text, environment-key expansion, redirects, unsafe Gemini names, and absence of transient credentials. Dialog and workbench tests cover three language tabs, clipboard errors, captured settings/history/drafts, localized notices, and lane isolation. These local execution checks make no live gateway or provider request.

Python 3 is optional for local frontend development: only the Python execution cases are explicitly skipped when `python3` is unavailable. Structural native-payload assertions and all other client checks still run. CI must provide Python 3 and verify it before invoking this test so that interpreter execution is never silently omitted from release evidence.
