# Playground

Playground is a native conversation and model comparison client for native OpenAI Chat Completions, OpenAI Responses, Anthropic Messages, and Gemini Generate Content. Both workbenches support user-owned and Project-owned PNG, JPEG, and PDF attachments. It makes real requests; it does not synthesize responses or usage statistics. An explicit Team Session source supports the four native protocols in the existing conversation and comparison workbenches, with creator-private managed images/PDFs when discovery declares support; Team code examples use independent Session authentication; interactive tool execution is outside this text interface.

## Workflow

1. Create a personal or Project API Key, save its one-time secret, and confirm delivery to enable it.
2. Open Playground and enter the Key. Select **Verify and load models** to request `GET /v1/models` using that Key.
3. Select a model and one of its currently eligible native protocols. The gateway list reflects effective Key/owner/Project grants. Missing legacy protocol metadata implies Chat support; an explicit empty protocol list does not. Switching protocol clears conversation history.
4. When the model, protocol, and verified Key scope allow attachments, use the lower-left paperclip to upload up to four PNG, JPEG, or PDF files. Each file is limited to 2 MiB. Selected files appear as removable chips immediately above the message field.
5. Optionally adjust Temperature, Top P, maximum output Tokens, and the system prompt. Choose streaming or ordinary JSON output.
6. Send a message. The page displays the submitted filenames under the user message, incremental text when streaming, the gateway Request ID, and upstream usage when supplied.
7. Stop an active request to abort the browser fetch and close the response stream. Leaving the page also aborts active requests.

Only native completed, non-empty text exchanges without refusal or non-text output are included in later conversation context. Failed or canceled partial answers remain visible but are excluded from future requests. Changing the protocol, model, or Key clears the conversation. Clearing the Key also clears the model selection and conversation. Copy output copies only the selected answer; clipboard failures provide a manual-copy fallback message.

Model discovery returns effective protocol-specific image and PDF input
capabilities plus one non-secret attachment scope for the entered credential.
Personal Keys return the user scope. Project Keys return the Project scope and
Project ID; every listed model for one verified Key must agree. Missing metadata
remains text-only. The composer follows the approved chips-above-textarea layout,
with the paperclip at lower left and send/stop actions at lower right. A Project
Key can use only objects owned by that exact Project and never a manager's
personal object.

Uploads use the authenticated session and CSRF token. Personal uploads call
`/api/v1/attachments`; Project uploads call
`/api/v1/projects/:project_id/attachments` and require a current enabled manager.
API Key inference continues to use only the transient Key and never sends session
cookies. Explicit Team Session native inference uses its separate same-origin cookie/CSRF
transport and cannot borrow Personal or Project attachment authority. The optional `?project=` value from Project Overview contains only an
expected Project ID; a mismatched verified Key clears models and drafts. Direct
Playground use derives the target from verified model metadata. File objects,
returned object IDs, and native references stay in component memory. Removing a
draft, changing its scope, Key, model, protocol, or lane set, leaving the
workbench, or settling the inference request triggers deletion of known
one-invocation objects. An upload already accepted by the server is allowed to
return after navigation so the client can delete its object. Ready attachments
also carry a durable one-hour expiry consumed by the storage cleanup worker,
which covers a terminated browser that can no longer receive the object ID.
Submitted filenames remain as non-sensitive transcript labels, while later
context contains only completed text turns.

Direct API callers upload with the session and CSRF-protected attachment API, then
place the returned object URI only in a native image/PDF position described in
[Object storage and owned attachments](STORAGE.md). References in text or tool
arguments are ordinary user content. Requests are limited to four occurrences,
four unique objects, 8 MiB of raw bytes and 12 MiB after inline expansion. A
finite token or TPM policy admits only validated owner-scoped attachment references
with the route's administrator-attested input capacity and the protocol's explicit
output cap. The reservation uses that full input capacity and never estimates
media tokens from file properties. A finite monetary policy also requires an
enabled base image/PDF rate for every validated occurrence kind and adds the exact
occurrence components before object storage access. Missing prices or FX fail
without reading bytes. Requests that also contain an unsupported native tool,
tier, cache, or media shape fail the stricter request-shape check first.

## Transport and Secret Handling

- Model discovery, Chat, and Responses use `Authorization: Bearer <key>` against same-origin endpoints. Messages uses only `x-api-key: <key>` with `anthropic-version: 2023-06-01` against `/v1/messages`; Gemini uses only `x-goog-api-key` against the native `/v1beta/models/{name}` action. Authentication forms are never combined, and the client never places credentials in query parameters.
- API Key requests omit browser cookies and reject redirects. Team Session model discovery and native inference use separate current-Session cookie/CSRF authentication against the exact Team path. Both sources reject redirects; changing sources destroys transient state.
- Attachment upload/deletion and explicit Team Session discovery/native inference use separate Session transports. Team uploads require exact creator/current membership and canonical managed references. Project routes receive only the non-secret Project path ID after server authorization; they never receive the entered inference Key. Returned object identifiers and selected `File` objects are not stored in React Query or browser storage.
- The Key remains only in component memory and the password input while the page is mounted. The client never writes it to localStorage, sessionStorage, React Query caches, logs, or generated request examples.
- The native client uses `fetch` and an `AbortController`, not React Query mutations, so request arguments and secrets are not retained in a mutation cache.
- Native error messages retain useful gateway context, with the supplied Key redacted if it appears in a message. Output is rendered as plain text, not executable HTML.
- Responses are not persisted. Reloading or leaving the page discards the conversation and entered Key.

## Response Handling

Chat Completions ordinary responses consume `choices[0].message.content` or a textual refusal. Streaming responses consume SSE `data:` events, decode UTF-8 across transport chunks, support LF and CRLF frame boundaries, and append `choices[0].delta.content` or textual refusal deltas. Empty usage-only chunks are supported. Recorded input/output counters remain visible when Chat omits total usage; the total is explicitly unknown and is never synthesized. `[DONE]` completes a stream.

A stream that closes without `[DONE]`, a malformed event, an unexpected content type, or a native error event is reported as a failure while preserving already received text. The client never silently retries a request after output has started. The page displays `X-Request-ID` when available. Missing usage remains explicitly unavailable; it is not estimated from text.

Responses requests use native `input`, `instructions`, and `max_output_tokens`, with full inline text history and no `previous_response_id`, stored-state references, or Chat stream options. Protocol selection never falls back to another endpoint. The page renders output text and refusal content; other native output items are not rendered, executed, or replayed. Tool configuration, structured-output editors, and reasoning-history replay remain outside this text interface.

Responses SSE consumes typed text/refusal deltas and completes only on a coherent `response.completed`, `response.failed`, or `response.incomplete` envelope. Final text and complete usage replace incremental display state. Bare EOF, Chat `[DONE]`, malformed events, and mismatched terminal status remain failures. HTTP 202 or a queued/in-progress ordinary response is labeled accepted but not completed; the page does not poll or retrieve stored response IDs. Failed, incomplete, accepted, and canceled turns are excluded from subsequent history. Token counts come only from a complete authoritative terminal usage object, including for failed/incomplete outcomes; missing counters are not estimated.

To bound browser memory, an individual buffered SSE event is limited to 1,048,576 JavaScript string code units, and accumulated answer text to 2,097,152 code units. Exceeding either limit cancels stream reading and reports an error. These are frontend display limits, not model context-window or gateway capacity claims.

## Verification

`website/src/api/playground.test.ts` covers native authorization, request parameters, ordinary output, split UTF-8/CRLF SSE, usage chunks, partial stream failures, truncation, malformed events, unexpected content types, HTTP errors, secret redaction, and cancellation.

`website/src/api/playground-responses.test.ts` additionally covers native Responses bodies, accepted HTTP 202, typed terminal states, authoritative usage, refusals, malformed/truncated streams, non-text output, secret redaction, and cancellation.

`website/src/api/attachments.test.ts`, `website/src/lib/playground-attachments.test.ts`, `website/src/views/playground/playground.test.tsx`, and `website/src/views/playground/compare.test.tsx` cover the isolated session/CSRF upload boundary, user/Project scope validation, Project-context mismatch handling, all four native reference shapes, capability gating, exact returned MIME validation, type/size/count validation, deduplication, chip removal, filename transcript rendering, settlement cleanup, bilingual pending state, late multi-file upload cleanup after navigation, Key isolation, cancellation, successful-only conversation history, and active-inference unmount abort.

Run `npm --prefix website test`, `npm --prefix website run lint`, and `npm --prefix website run build`. These tests validate the client with controlled responses. They do not establish real-provider compatibility or production readiness; gateway integration and real-provider smoke tests remain separate evidence.

## Model comparison

The Model conversation and Model comparison tabs keep separate transient workbenches. Switching tabs destroys the hidden workbench, aborts its active fetches, and clears its entered Key and history. The conversation workbench retains its existing settings/transcript layout.

Comparison uses a shared credential toolbar, two initial model columns, and one shared message composer. Add comparison creates up to four columns; remove controls appear only above the two-column minimum. Columns scroll horizontally with a 300-pixel minimum width. Each column selects its own currently eligible Chat, Responses, Messages, or Gemini protocol and displays the actual endpoint, partial output, terminal status, Request ID, observed elapsed time, and authoritative usage. Verification uses one transient personal or Project Key, or the selected active Team and current Session. Models with explicit empty protocol capabilities are unavailable.

The shared composer preserves the approved chips-above-textarea layout, lower-left paperclip, and lower-right send action. Attachment types are the conservative intersection of every selected column's effective protocol capabilities, and every selected model must share the verified Key scope. One session/CSRF upload creates the shared transient draft; the same owner-bound references are encoded into each column's native current turn while requests and cancellation remain independent. Changing the scope, Key, a model, a protocol, or the column set invalidates and deletes the draft. After submission, object deletion starts only after every column settles and cannot block the next message; the durable one-hour expiry covers failed or interrupted cleanup.

Send captures one message and concurrently dispatches an independent native request to each selected column. The comparison defaults are streaming, Temperature 0.7, Top P 1, and 2,048 maximum output Tokens. Per-column Stop only aborts that column; failure or cancellation does not stop siblings. Removing a column or changing its model/protocol cancels that column's request and clears only its history. Global sending waits until all current requests settle, with a synchronous guard against duplicate dispatch.

Subsequent requests include only the successful text history of their own column. Failed, incomplete, accepted, and canceled turns remain visible but are excluded from later context. Submitted filenames remain visible under each column's user message without replaying object references. Clear all resets all histories; clearing/changing the Key also resets models and drafts. There are no simulated responses. Explicit Team Session comparison uses the same four native protocols through separate cookie/CSRF requests for the exact Team. It shares no Personal or Project Key account or attachment scope.

`website/src/views/playground/compare.test.tsx` covers the two-to-four column bounds, eligible protocols, shared-composer placement, capability intersection, concurrent native attachment bodies, independent histories/errors/cancellation, non-blocking cleanup, duplicate sends, context invalidation, credential clearing, stale multi-file upload termination, tab teardown, and bilingual accessibility/draft preservation. All client tests use controlled mocked transports, separate from paid-provider or real gateway acceptance.

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

Get code is disabled while a transient attachment draft is selected. RouteX does
not export short-lived object identifiers into an example that may outlive their
one-invocation cleanup boundary.

The request builder has no credential input. Every generated language reads `ROUTEX_API_KEY` from the caller's local environment; the transient Key entered in Playground is never substituted. Examples preserve native authentication, routes, parameters, streaming selection, and history roles. Gemini's model identity remains in its validated single-segment path, with no synthetic model/stream body fields. Unsupported paths do not produce copyable examples.

cURL examples target POSIX shells and quote URLs/JSON as literal shell arguments while allowing expansion only in the fixed authentication header. Python examples use Python 3 standard-library `urllib`, reject redirects, and decode their JSON payload rather than inserting JavaScript boolean/null syntax into Python. JavaScript examples run as ES modules in Node.js with native `fetch`, omit cookies, and reject redirects. Both print native response bytes; streaming SSE is not translated into another protocol. No generated example performs an automatic retry or a paid call during preview/copy.

Focused tests execute generated commands against a shell function, a stub Python opener, and a stub JavaScript fetch. They verify native bodies and headers, Unicode/newlines/apostrophes, literal command-substitution text, environment-key expansion, redirects, unsafe Gemini names, and absence of transient credentials. Dialog and workbench tests cover three language tabs, clipboard errors, captured settings/history/drafts, localized notices, and lane isolation. These local execution checks make no live gateway or provider request.

Python 3 is optional for local frontend development: only the Python execution cases are explicitly skipped when `python3` is unavailable. Structural native-payload assertions and all other client checks still run. CI must provide Python 3 and verify it before invoking this test so that interpreter execution is never silently omitted from release evidence.


## Explicit Team Session conversation and comparison

API Key is the default credential source. Selecting Team Session loads only the
signed-in actor's own active Teams, followed by independently authorized native
Team model discovery. A `?team=` or `?model=` value is expected navigation context
only; it never grants access. The source selector preserves the existing left
configuration panel. Text-only restrictions and empty guidance are bilingual.

Team discovery and native inference use the current Session and CSRF without an entered Key.
Changing actor, source, Team, model or page aborts requests and clears history,
credentials, attachments and code drafts. Late callbacks cannot restore the old
authority. Own-Team pickers and history are revalidated on remount, and denied
responses hide stale rows and selected details. See [Team Session inference](TEAM_INFERENCE.md)
for endpoint, publication, attribution and unfinished scope.


## Native history and renewed authority

Conversation and comparison apply the same completed-text criterion to both Key
and Team sources. Known usage or a successful HTTP transport never makes a
refusal, handoff, truncation, accepted result or empty output completed text. Such
results remain visible with their native status and usage, but do not enter later
inline history or generated code.

Team comparison dispatches one independent request per lane. A lane cancellation
leaves completed siblings intact. Renewed Session/Team reads abort stale work and
clear models/history while retaining an unsent prompt; loading models and sending
again require explicit actions. Changing actor, source or Team destroys transient
state. A native 401 initiates a bounded five-second, no-store, same-origin Session
probe. Only an authoritative 401 from the active probe expires the Session; an
upstream 401, successful probe, network error, timeout or late cancellation cannot
establish logout. Native permission failures clear stale comparison authority
without inferring which server-side relationship changed.

Creator-private Team media extends the existing F20 workbenches, with its own acceptance gates. Team request code and
parameter Reset are implemented below; their actual generated-program/browser
acceptance and external-provider evidence remain separate in the implementation
index.


## Team request code and parameter Reset

Team conversation and comparison use the existing three-language code dialog.
It captures the current native body, completed text history, parameters and exact
Team path without accepting the live cookie, CSRF or password. Preview and Copy
make no request. Current authority, a confirmed Team and exact discovered model
are required; renewal clears captured code and requires explicit rediscovery.

Generated programs read ROUTEX_EMAIL and ROUTEX_PASSWORD from their environment,
sign in independently, verify the current actor/Session/CSRF and then issue one
exact native request. HTTP 202 requires two-step verification in RouteX and stops
before inference. Redirects, rejected authentication and malformed authority also
stop without replay. Python 3 uses standard-library in-memory cookies. JavaScript
uses Node native fetch and a private in-memory cookie value. The cURL tab requires
Python 3 for private authentication, followed by curl --disable --config - with
configuration supplied on stdin. Authentication is never placed in arguments,
files or printed output. Native response bytes remain protocol-specific; accepted
transport alone never proves native completion. Existing Key snippets still read
ROUTEX_API_KEY and never authenticate a Session.

Reset in the conversation parameter header restores Temperature 0.7, Top P 1,
2048 maximum output Tokens and an empty system prompt. It preserves source,
credential, Team, model, protocol, streaming, history and the unsent prompt and
makes no request. It is disabled during active inference. The localized notice
and subsequent request/code previews use the restored values.

Focused generated-program, dialog, conversation, comparison, parameter and
finality tests cover ordinary/SSE behavior, all four protocols, independent login,
MFA/authentication denial, redirects, private credentials, completed-history
exclusion, explicit renewal, Reset without dispatch and live language switching.
Final main check/test/build passed 1521 frontend cases in 88 files and four Node
checks. Controlled acceptance separately executed 24 generated programs and nine
final rebuilt-browser calls, including automatic Session renewal, bilingual
controls, revocation and restart without replay. External-provider acceptance
remains independent.


Team authority renewal observes every successful network Session read, including
structurally identical data with an unchanged timestamp. Clear Team models,
completed history and captured code, abort pending inference, preserve unsent
drafts, and require explicit model rediscovery. Observe the existing Session query
without creating an additional network observer. Manual same-actor CSRF cache
replacement is not a Team authority renewal and preserves completed history.

### Team attachment authority and lifetime

Team discovery exposes actual per-protocol `input_capabilities`,
`attachment_scope: "team"`, `attachment_team_id`, `attachment_membership_id`, and
`personal_attachments: false`. The current actor, explicitly selected Team and
exact membership must match before an upload can be selected. Comparison also
intersects the image/PDF capabilities of every selected lane; absent capabilities
or inconsistent targets disable uploads. The existing shared chips, paperclip and
bottom composer serve both sources, without a Team attachment library.

Each Team object belongs to the exact Team, creator user and captured active
membership. The upload response must match that captured identity before the UI
adopts it; unexpected ownership is neither reused nor deleted. Objects expire
exactly one hour after creation. Another member, owner or administrator cannot
borrow them, and leaving/rejoining the Team does not restore the old membership's
objects. Only canonical managed `routex://attachments/obj_<ULID>` references enter
the four protocols' supported image/PDF scalar positions. Remote URLs, raw inline
base64, provider file IDs, audio and video are unavailable for Team media.

Renewed Session reads immediately hide unsent Team media and abort pending
uploads. Reconfirmation clears the draft and model selection while preserving
unsent text; the UI never restores or replays the upload automatically. The server
can still authorize an unexpired object through a renewed Session belonging to
the same enabled creator and exact active membership. These are separate UI and
server contracts. Changing actor, source, Team, model, protocol or lanes clears
unsent media. Submitted references remain retained until all participating
requests settle, including canceled or unmounted lanes. Cleanup uses the captured
Team target; a denied or uncertain deletion defers to durable expiry. Later
history contains completed plaintext and filename labels, never old object
references. Files, identifiers, credentials and CSRF stay transient and out of
browser storage and mutation caches. Code export is disabled while files are
selected or uploading.

Controlled rebuilt-main Team media acceptance passed 16 distinct native calls
across all four protocols, completed-text history without media replay, shared
retention through independent cancellation, exact-version cleanup, current grant
revocation and Session-preserving restart without replay. Bilingual controls and
empty transient state were verified; warnings/errors were absent. Full main
check/test/build and PostgreSQL/MySQL regression passed. This controlled proof
does not establish external-provider compatibility.


## Single-model elapsed observation

Conversation responses display a finalized browser-observed elapsed duration next
to existing request and usage facts. A monotonic clock starts at native dispatch
and ends once on settlement or explicit cancellation, before attachment cleanup.
The value is transient; it does not measure Provider latency, first-token time,
server call duration, quota usage or native completion. Zero is visible, while
non-finite or backwards observations remain absent. Language switching preserves
the value; model/protocol changes, Clear and leaving the conversation discard it.
Late settlement cannot change a canceled observation or restore cleared history.

The 30 focused elapsed lifecycle cases and complete 3,789-case frontend suite
pass, alongside mandatory checks, the embedded build and production asset race
test. Controlled production browser acceptance covers 13 native dispatches across
all four protocols, English/Chinese switching, parameter reset, a real HTTP
failure, cancellation with a real five-second delayed attachment DELETE, Clear,
model change after Stop, and leaving the conversation. The canceled 415 ms value
remained identical after cleanup. Model selection is disabled while running;
direct generation changes, zero and invalid clocks remain unit-test evidence.
The observer buffers SSE, so this run establishes native terminal parsing rather
than progressive browser delivery. Owned processes, Compose resources and five
ports were independently cleared. External-provider compatibility and broader
attachment/F20 acceptance remain separate; detailed evidence is recorded in
`docs/IMPLEMENTATION.md`.


## Corrected R8 candidate contract additions

These additions describe the isolated candidate; complete dual-driver and
composed-main acceptance remain pending. Earlier acceptance records stay
bound to their original source.

The existing code dialog highlights Bash, Python and JavaScript through a pure,
lossless presentation tokenizer. Tokens are inert React text; no HTML parsing or
code execution occurs. Bash here-document bodies stay opaque. Copy always uses
the original generated string, including whitespace and Unicode, rather than
rendered markup. Highlighting is advisory presentation, not syntax validation,
authentication, native completion or provider acceptance.

The current frozen R8 source passes check, complete Task testing and build,
5,032 ordinary named Go tests, and the separate two-driver Project warning
lifecycle. Earlier focused R7 receipts remain bound to that source. Complete
R8 Full168, composed-main gates, browser and delivery remain pending. These
source-specific facts do not establish wider runtime or feature acceptance.
