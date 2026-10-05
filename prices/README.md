# Repository price source

`catalog.json` is the initial versioned RouteX price source. Its complete bytes
are embedded when RouteX is built; changing the file requires building and
deploying the binary containing that source. The source package performs no
network request and does not read an arbitrary runtime path.

The catalogue contains three reviewed native-model entries with six base token
rates. Do not add guessed prices or local ProviderModel IDs. Source maintenance
requires reviewed prices and billing semantics; test prices belong only in test
fixtures. Administrative maintenance uses explicit existing Provider-model/source mappings,
a server-derived difference preview, a required reason and explicit confirmation.
Configuration alone does not apply prices. Synchronization protects custom zero,
disabled and same-amount rates; restoration changes only reviewed selected rates.
Durable operation receipts record historical commitment separately from current
runtime application. See [the pricing contract](../docs/PRICING.md).

The source has exactly two top-level fields:

```json
{
  "schema_version": 1,
  "models": []
}
```

Each model requires `key`, `provider_key`, `model`, `protocol`,
`context_threshold`, and a nonempty `rates` array. `key` and `provider_key` are
exact stable source identities; `model` is descriptive native-model metadata.
They do not authorize matching by display name or creating a local resource.
Administrative mappings must point to an existing exact ProviderModel identity.

Each rate requires `key`, `metric`, `tier`, `unit`, `currency`, `amount`, and
`enabled`. Rate source keys are unique across the complete file. Prices use the
existing RouteX pricing rules:

| Metric | Tier | Unit |
| --- | --- | --- |
| `INPUT_TOKEN`, `OUTPUT_TOKEN`, `CACHE_READ_TOKEN`, `CACHE_WRITE_TOKEN` | `base` or `long_context` | `1M_TOKEN` |
| `IMAGE_INPUT` | `base` | `1_IMAGE` |
| `PDF_INPUT` | `base` | `1_PDF` |

Amounts are nonnegative decimal strings with at most 18 integer and 18 fractional
digits. Numeric JSON amounts, exponents and negative amounts are rejected.
`enabled` is an explicit boolean. Supported currencies are `USD`, `CNY`, `EUR`,
`GBP`, `JPY`, `HKD`, and `SGD`; the source never supplies platform exchange rates.
Context thresholds are `0`, `128000`, or `200000`. An enabled long-context token
rate requires a nonzero threshold and uses the existing whole-request tier
selection semantics.

Protocols are `openai_chat`, `openai_responses`, `anthropic_messages`, and
`gemini_generate_content`. Source keys use 1–128 ASCII characters, begin with a
letter or digit, and otherwise permit letters, digits, `_`, `.`, `:`, `/`, and
`-`. Native-model metadata is nonempty UTF-8 text with no control characters or
surrounding whitespace and at most 256 bytes.

The complete UTF-8 file is bounded to 256 KiB and 1000 model entries; each model
has at most ten rates and no repeated metric/tier pair. Unknown fields, omitted
fields, nulls, case aliases, duplicate keys, repeated source identities and
unsupported versions or combinations fail the complete snapshot. Do not insert
capabilities, context-window declarations, URLs, local IDs or executable content.

The complete source SHA-256 identifies the reviewed input. Different source bytes
invalidate that identity even when the normalized prices are equivalent. This
digest is provenance for preview and audit, not a second price catalogue or a
price version users can publish or roll back. Existing historical call amounts
must never be recalculated from a new source.
## Initial reviewed rates

Observed on 2026-10-05. The following amounts are published standard paid
inference prices for the direct provider, in USD per 1,000,000 tokens. Exact
native-model identifiers are source metadata; they do not discover resources,
verify account availability or authorize a mapping to a local ProviderModel.

| Native model | Protocol | Base input | Base output |
| --- | --- | --- | --- |
| `gpt-4.1-mini-2025-04-14` | `openai_chat` | `0.40` | `1.60` |
| `claude-haiku-4-5-20251001` | `anthropic_messages` | `1` | `5` |
| `gemini-3.5-flash-lite` | `gemini_generate_content` | `0.30` | `2.50` |

Primary provider sources:

- OpenAI's [model, pinned snapshot and pricing documentation](https://developers.openai.com/api/docs/models/gpt-4.1-mini)
  lists these token prices and the native `/v1/chat/completions` endpoint. The
  source intentionally contains only the Chat protocol entry.
- Anthropic's [model and pricing documentation](https://platform.claude.com/docs/en/models/haiku-4-5/overview)
  lists the exact snapshot and token rates. Its [Messages API reference](https://platform.claude.com/docs/en/api/messages/create)
  documents the native `/v1/messages` endpoint.
- Google's [model documentation](https://ai.google.dev/gemini-api/docs/models/gemini-3.5-flash-lite)
  records the exact model code; the [Standard paid pricing table](https://ai.google.dev/gemini-api/docs/pricing#gemini-3.5-flash-lite)
  provides these rates. Its [generateContent reference](https://ai.google.dev/api/generate-content)
  documents the native `v1beta/models/{model}:generateContent` endpoint. Published
  output pricing includes thinking tokens; quotes still require the supported
  native adapter's complete counters.

These entries omit cache-read and cache-write rates. Omission never means zero,
free usage or charging cached tokens at the ordinary input price. Published cache
prices have separate conditions: Anthropic distinguishes five-minute and one-hour
write retention, and Google separately bills explicit cache storage in token-hours,
which the v1 source units cannot represent. The existing native adapters retain
all supported cache, tier and request-condition restrictions. Positive cache use
without an applicable rate remains unpriced; finite money reservation may also
reject a schedule missing reachable cache rates, even for an uncached request.
See the [pricing contract](../docs/PRICING.md) for quoting and reservation rules.

No Batch, Flex, Priority, free-tier, cloud-reseller, regional, negotiated,
discounted, hosted-tool, cache-storage or media-occurrence price is supplied. The
base schedule and zero context threshold declare no separate long-context price
rows; they do not establish context capacity or reservation bounds. Do not assume
these direct-provider schedules match customized endpoints or other service tiers.
They are not paid-invoice verification or a claim of universal applicability.

Maintenance is manual. Recheck the primary documents, exact model identifiers,
price conditions and lifecycle before updating these entries. Build and deploy a
binary containing the changed complete source, then review explicit mappings and
the server-derived difference preview before confirming application. Rebuilding
or saving mappings alone never applies a price. Source updates do not recalculate
historical calls, remove omitted rates or override protected custom prices; there
is no network synchronization or scheduled update.
