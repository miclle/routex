# Native Gemini generation

RouteX implements the `gemini_generate_content` protocol through native `POST /v1beta/models/{public-name}:generateContent` and `POST /v1beta/models/{public-name}:streamGenerateContent` endpoints. Streaming uses SSE; `alt=sse` is optional on the RouteX streaming endpoint and always added upstream. The configured connection base URL includes the API prefix, for example `https://generativelanguage.googleapis.com/v1beta`. No protocol translation, cross-protocol fallback, or automatic retry occurs.

## Identity, transport, and requests

A current public name or unexpired alias resolves through the same Key scope, model grants, Project state, runtime authorization lease, provider-model availability, weighted binding, credential coverage, admission limits, and durable call journal as the other native protocols. Gemini path names use a single segment matching `[A-Za-z0-9][A-Za-z0-9._-]{0,127}`. Models whose other public names contain a slash or colon need a compatible alias. The selected upstream model replaces only the path identity; no synthetic `model` or `stream` fields are inserted into native request JSON. Returned `modelVersion`, when present, is rewritten to the requested public name. Native `responseId` remains opaque and separate from the RouteX `X-Request-ID`.

Clients may provide exactly one `x-goog-api-key`, one `key` query parameter, or one Bearer authorization header. Ambiguous and duplicate credentials are rejected even if their values match. The upstream receives only the selected provider credential in `x-goog-api-key`. Caller authentication and arbitrary query parameters are never forwarded. The ingress middleware removes the native query and authentication headers before downstream dispatch, including unknown paths, default request logging, and panic recovery. Native query parsing is bounded to 8 KiB. Automatic router path and trailing-slash redirects are disabled because they execute before middleware and could reflect query credentials; nonexact API paths return 404. External reverse proxies must independently avoid recording credential-bearing request URLs.

Native JSON parameters and numeric precision are preserved, including contents, system instructions, generation settings, safety controls, function declarations/results, thought signatures, and inline media. RouteX does not execute client tools. Candidate bookkeeping is bounded to 1–8 requested candidates; upstream model-specific restrictions still apply. The request limit is 4 MiB, ordinary response limit 16 MiB, SSE event limit 1 MiB, and streaming deadline five minutes.

Provider-owned resources require an ownership and routing-affinity map that this slice does not implement. Requests therefore reject `cachedContent`, `fileData`/`fileUri` references (including external file URLs), tuned model paths, retrieval/file-search resources, and provider sessions. Known native fields are checked in both camelCase and protobuf snake_case spellings; conflicting aliases are rejected. Function argument/result JSON and function schemas remain opaque user data, so a harmless property named `fileUri` is allowed there. Inline multimedia within native function-result parts is checked as native content. Files, explicit caches, tuned models, batch operations, embeddings, count-tokens, Live, and Interactions endpoints are not implemented and do not fall back to the SPA.

## Native replies, SSE, and errors

Ordinary replies retain native candidates, finish reasons, safety feedback, content parts, function JSON, and thought signatures. SSE preserves native JSON frames and framing metadata rather than fabricating Chat events or a `[DONE]` sentinel. Duplicate terminal candidates, invalid candidate indices, malformed frames, truncated transport, and missing candidate completion are rejected. Prompt-block responses retain native feedback without inventing zero usage.

The native protocol has no documented global terminal event. RouteX conservatively requires **clean EOF plus every requested candidate's finish reason**, or an explicit prompt-block result, before treating streaming usage as final. Usage must appear in a complete snapshot at or after candidate completion. Finish reasons or EOF alone are insufficient. Later usage-only frames replace the entire earlier snapshot; missing or malformed counters cannot inherit preliminary values. Cancellation or a transport error before clean EOF remains `not_final`, even if a candidate finish and usage frame already arrived. This boundary deliberately avoids assuming that no later aggregate usage could follow. Outcome and pricing remain independent once final usage has been established.

HTTP and SSE errors retain the native `error` envelope, bounded HTTP status, and allowlisted status codes. Messages are generic; upstream free-form diagnostics and `details` are removed. No request content, response content, thoughts, provider credentials, or native resource identifiers are persisted in call facts.

## Exact pricing and unknown usage

The immutable rate/FX basis is selected before dispatch and travels through the existing call journal. Replay persists the accepted receipt without consulting current prices. For supported standard text generation:

- Total input is `promptTokenCount`, which already includes cached input.
- Cache reads are `cachedContentTokenCount`.
- Billable output is `candidatesTokenCount + thoughtsTokenCount`, with overflow checks.
- Cache writes are zero because this stateless endpoint does not create explicit TTL cache resources. This is a nonapplicable metric, not a guess about an omitted usage counter.

Missing thought or cache-read counts remain unknown. RouteX does not infer zero from omitted counters, a requested thinking budget, or model names. Reported totals are checked when available. Unknown or incomplete usage produces a null amount with its existing explicit pricing status.

Hosted tools/grounding, tool-use prompt accounting, multimodal inputs/outputs, nonstandard service tiers, and unknown pricing dimensions remain unpriced. Their token counts do not establish a complete charge for search queries, image/audio/video generation, or cache storage. Supported stateless native features can still be forwarded; the response and recorded pricing status make no claim that the four-metric text engine covers those charges.

## Credential discovery and verification

Verification uses native `GET {base}/models?pageSize=1000` with `x-goog-api-key`; it does not make a paid generation call. Pagination is bounded to ten seconds, 2 MiB total response bytes, twenty pages, and 2,000 unique models. Names must be safe `models/{segment}` resources; only models advertising `generateContent` are added as usable generation supply. Duplicate names are deduplicated, cursor cycles are rejected, and no partial pages are committed.

Failed re-verification marks the credential failed and disabled, synchronously revokes runtime authorization, and retains last-success discovery evidence. Retained rows are historical evidence, not proof of current authorization. Discovery capability metadata does not guarantee that every request shape or commercial tier is available.

## Validation boundary

Focused tests exercise native dispatch, protocol isolation, exact nullable usage, discovery pagination/capabilities, actual default logger and recovery redaction, nonexact paths, malformed/late SSE frames, multiple candidates, prompt blocking, transport failure, and cancellation. `testGeminiLifecycle` is registered by the shared PostgreSQL/MySQL harness and covers discovery, generation, SSE, errors, ownership rejection, runtime disable, failed verification, journal restart/idempotency, immutable exact charges, and protocol-filtered usage. Fixtures use local HTTP servers; no external paid calls are needed. Database acceptance remains a separate integration gate.

## Primary protocol references

The adapter is independently implemented from the official [generation API](https://ai.google.dev/api/generate-content), [model discovery API](https://ai.google.dev/api/models), [API key guide](https://ai.google.dev/gemini-api/docs/api-key), [thinking guide](https://ai.google.dev/gemini-api/docs/generate-content/thinking), [context caching guide](https://ai.google.dev/gemini-api/docs/generate-content/caching), and [grounding guide](https://ai.google.dev/gemini-api/docs/generate-content/google-search). The [official Go SDK stream reader](https://github.com/googleapis/go-genai/blob/main/api_client.go) provides an additional reference for SSE framing. No upstream implementation code was copied.
