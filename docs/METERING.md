# Gateway text assessment

RouteX records an exact, nullable monetary assessment for supported text calls.
It is a call fact, not an invoice, quota deduction, reservation, or financial
ledger entry. Prices are operator-configured and do not establish what a provider
will ultimately invoice. This bounded implementation does not complete all P3
metrics, protocol adapters, finite pricing conditions, or reconciliation.

## One immutable basis per request

The gateway runtime loads current prices and exchange rates in the same consistent
read transaction as routing data. Both participate in the routing digest and are
published together. Before upstream dispatch, each request takes a detached copy
of the selected provider model's schedule, all its configured rates, platform
currency, FX map, adapter ID, and catalogue ETag. No request performs a price lookup
on the runtime hot path. The test-only service path without a started runtime
captures its basis in a repeatable-read transaction before dispatch.

A catalogue change publishes a new runtime generation. Existing requests retain
the old rates, currency, and FX values. Invalid route preparation retains the last
valid route and price generation together; current authorization and its bounded
lease remain independent. After a price write commits, a runtime-refresh failure
returns 503 without rolling back the catalogue. The original ETag is already
consumed, so retrying it cannot duplicate the mutation or audit.

Before dispatch, the durable journal reserves an interruption fact containing the
captured basis and a null amount. On completion, assessment occurs once before the
final fact is fsynced to the journal. The accepted receipt includes the normalized
basis and, when priced, selected tier, quantities, used rate identities, exact
exchange rates, component amounts, and total. Database delivery and process-restart
replay persist that receipt verbatim, without consulting a newer catalogue or
re-running pricing. The canonical request ID makes duplicate delivery idempotent.

## Native usage and completeness

The supported protocol is native OpenAI Chat Completions (`openai_chat`) with the
`routex_text_v1` adapter. The mapping follows the official
[Chat Completions API](https://developers.openai.com/api/reference/resources/chat/subresources/completions/methods/create)
and [typed usage schema](https://github.com/openai/openai-python/blob/main/src/openai/types/completion_usage.py):

| Native field | Stored normalized quantity |
| --- | --- |
| `usage.prompt_tokens` | Total input, including cache categories |
| `usage.completion_tokens` | Total output |
| `usage.prompt_tokens_details.cached_tokens` | Cache-read input |
| `usage.prompt_tokens_details.cache_write_tokens` | Cache-write input |

Reasoning and rejected-prediction detail counts are already part of native output;
they are not added again. Ordinary input is total input minus both cache categories.
Their sum must not exceed total input. Negative, fractional, overflowing, omitted,
and null counters are not treated as zero. In particular, compatible providers
that omit cache-write counts remain unpriced even if they report total tokens.
No tokenizer estimate or assumption fills a missing counter.

For a non-streaming response, an authoritative final envelope must identify
`chat.completion` and contain nonempty choices with nonempty finish reasons. For
streaming, it must identify `chat.completion.chunk`, include usage, and contain the
native final empty choices array. A chunk with only one finished choice cannot
establish completion of a multi-choice request. The gateway requests
`stream_options.include_usage=true`; an interrupted stream may still never supply
that event. Usage from separate frames is never merged into fabricated totals.

Assessment depends on complete authoritative usage, independently of the final
HTTP/call outcome. If final usage arrives before a client disconnect, write error,
or malformed terminal stream event, the call can still have an assessed amount.
A provider may have consumed resources despite delivery failure. Cancellation
before final usage remains unpriced. `[DONE]` alone does not establish usage, and
an HTTP 200 without complete usage does not imply a known charge. Sanitized
non-success upstream error bodies are not parsed as completion envelopes.

## Supported dimensions and null amounts

Text messages, function tools, standard/default service tier, and the finite
configured context tiers are supported. Explicit image/audio/video content,
nonzero native modality counters, non-default service tiers, cache-retention/TTL
options, and external-tool pricing are outside this adapter. Requests may still
be forwarded, but their amount is null. Bounded classifications such as
`request_non_text`, `response_non_text`, `request_service_tier`,
`response_service_tier`, `cache_retention`, and `external_tool` explain unsupported
dimensions without retaining request content or arbitrary provider strings.
An unsupported tier observed in an earlier stream chunk remains unsupported even
if the final usage chunk omits that field.

Each call exposes `pricing_status`:

| Status | Meaning |
| --- | --- |
| `priced` | Complete supported usage, sufficient enabled rates and FX; exact amount may be `"0"` |
| `not_captured` | No price basis, such as an early rejection or a legacy journal fact |
| `unsupported` | A dimension or adapter lies outside supported text pricing |
| `not_final` | No authoritative final usage envelope was observed |
| `unknown_usage` | Final envelope exists but at least one required count is unknown |
| `invalid_usage` | Cache categories exceed total input |
| `missing_price` | Required rate, enabled catalogue, or currency conversion is absent |
| `invalid_configuration` | The captured schedule cannot be calculated safely |

Every status except `priced` has null `charge_amount` and `charge_currency`.
A known zero amount is distinct from unknown. Calculation uses the exact decimal,
rounding, context-tier, and zero-quantity rules in [PRICING](PRICING.md), with no
silent rate or currency fallback.

## Storage, access, and verification

Additive migration 13 adds nullable cache counts, pricing status, catalogue ETag,
nullable exact decimal amount/currency, and a bounded JSON receipt to call records.
Historical rows default to `not_captured`. Amounts are decimal strings, not binary
floating point. Receipt JSON is bounded to 16 KiB within the existing 64 KiB journal
entry limit and contains no prompts, completions, credentials, or raw diagnostics.

Personal and Project call DTOs expose safe counts, pricing status, amount, and
currency. Only administrative call detail exposes the provider-model rate and FX
snapshot plus ETag. Existing ownership and permission checks remain authoritative.

Focused tests cover native cache mapping, finality, unsupported dimensions,
whole-frame parsing, final-usage client disconnects, unknown versus zero,
immutable in-flight price generations, exact receipts, and journal reopen.
`testCallPricingLifecycle` is registered in the shared PostgreSQL/MySQL harness;
it verifies actual HTTP dispatch, price mutation publication, delayed delivery
and restart, old/new exact amounts, unsupported-tier null amounts, concurrent
idempotent replay, and member/admin disclosure boundaries. Controlled fixtures
provide this evidence; live-provider acceptance remains separate.
