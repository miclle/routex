# Repository price source

`catalog.json` is the initial versioned RouteX price source. Its complete bytes
are embedded when RouteX is built; changing the file requires building and
deploying the binary containing that source. The source package performs no
network request and does not read an arbitrary runtime path.

The shipped catalogue is initially empty. Do not add guessed prices or local
ProviderModel IDs. Source maintenance requires reviewed prices and billing
semantics. Test prices belong only in test fixtures. Administrative maintenance uses explicit existing Provider-model/source mappings,
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
