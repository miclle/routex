# Gateway monetary assessment

RouteX records an exact, nullable monetary assessment for supported native calls.
It is a call fact, not an invoice, quota deduction, reservation, or financial
ledger entry. Prices are operator-configured and do not establish what a provider
will ultimately invoice. This bounded implementation does not complete all P3
provider-specific dimensions, finite pricing conditions, or reconciliation.

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

Dedicated adapters normalize complete native usage for Chat Completions,
Responses, Messages, and Gemini. The token contract is inclusive input, output,
cache-read input, and cache-write input:

| Native field | Stored normalized quantity |
| --- | --- |
| Chat `prompt_tokens` / `completion_tokens` | Inclusive input / output |
| Responses `input_tokens` / `output_tokens` | Inclusive input / output |
| Messages ordinary input plus cache-read/cache-creation | Inclusive input; native output remains output |
| Gemini prompt / cached input / candidates plus thoughts | Inclusive input, cache-read input, and output |

Reasoning and rejected-prediction detail counts are already part of native output;
they are not added again. Ordinary input is total input minus both cache categories.
Their sum must not exceed total input. Negative, fractional, overflowing, omitted,
and null counters are not treated as zero. In particular, compatible providers
that omit cache-write counts remain unpriced even if they report total tokens.
No tokenizer estimate or assumption fills a missing counter.

Each protocol requires its own native terminal evidence. Chat requires completed
choices or the final usage chunk; Responses requires a terminal response status;
Messages requires the final message/delta lifecycle; Gemini requires clean EOF
with a native finish or prompt-block outcome. Usage from unrelated or preliminary
frames is never merged into fabricated totals. See the protocol-specific documents
for the exact ordinary and SSE contracts.

Assessment depends on complete authoritative usage, independently of the final
HTTP/call outcome. If final usage arrives before a client disconnect, write error,
or malformed terminal stream event, the call can still have an assessed amount.
A provider may have consumed resources despite delivery failure. Cancellation
before final usage remains unpriced. `[DONE]` alone does not establish usage, and
an HTTP 200 without complete usage does not imply a known charge. Sanitized
non-success upstream error bodies are not parsed as completion envelopes.

## Supported dimensions and null amounts

Text messages, supported function tools, standard/default service tiers, and the
finite configured context tiers are supported. Strictly validated owner-authorized
RouteX image/PDF attachment inputs are also supported when the captured schedule
contains an enabled base `IMAGE_INPUT / 1_IMAGE` or `PDF_INPUT / 1_PDF` rate for
every media kind present. The immutable occurrence counts are combined with the
authoritative aggregate token usage; repeated forwarded references count
repeatedly even when storage reads are deduplicated.

Remote or caller-inline media, output image/audio/video, non-default service tiers,
unsupported cache-retention/TTL options, hosted tools, page/pixel/byte rates, and
unknown provider conditions remain outside the adapters. Requests may still be
forwarded without a finite money policy, but their amount is null. Bounded classifications such as
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
| `unsupported` | A dimension or adapter lies outside supported native pricing |
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
Migration 24 adds nullable image/PDF occurrence counts. Historical rows retain
null counts as unknown; every new parsed request records an explicit zero or
positive count. Amounts are decimal strings, not binary floating point. Receipt
JSON is bounded to 16 KiB within the existing 64 KiB journal entry limit and
contains no prompts, completions, attachment identifiers, credentials, or raw
diagnostics.

Personal and Project call DTOs expose safe token/media counts, pricing status,
amount, and currency. Only administrative call detail exposes the provider-model
rate and FX snapshot plus ETag. Existing ownership and permission checks remain
authoritative.

Focused tests cover all four native cache/finality mappings, unsupported
dimensions, media occurrence counts, repeated references, whole-frame parsing,
final-usage client disconnects, unknown versus zero, immutable in-flight price
generations, exact receipts, and journal reopen.
`testCallPricingLifecycle` is registered in the shared PostgreSQL/MySQL harness;
it verifies actual HTTP dispatch, price mutation publication, delayed delivery
and restart, old/new exact amounts, unsupported-tier null amounts, concurrent
idempotent replay, and member/admin disclosure boundaries. Controlled fixtures
provide this evidence; live-provider acceptance remains separate.
