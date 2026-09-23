# Current price catalogue and text pricing

RouteX has one current price aggregate per actual provider model. It is identified
by the stable `ProviderModelID`, independently of the upstream name or platform
model names. Provider identity and connection protocol are derived from the
existing catalogue. Price records use `prc_` IDs and rate records use `rat_` IDs.
There are no price books, publication stages, effective-time versions, alternate
supplier/public catalogues, or formula expressions.

The control-plane quote API is a deterministic dry run. The gateway also assesses
supported final text usage against the price and exchange-rate snapshot captured
before dispatch. These immutable call amounts do not deduct quota, create invoices,
or post financial ledger records. See [Gateway text assessment](METERING.md).

## Text pricing adapter

RouteX uses a finite Go text-pricing adapter identified as `routex_text_v1`.
It calculates quotes with exact rational arithmetic and explicit validation.
The current RouteX catalogue and configured exchange rates are the authoritative
inputs. Unsupported pricing dimensions are rejected rather than inferred.

The supported adapter has these explicit boundaries:

- Protocol: `openai_chat`; quotes contain normalized **text token** usage only.
  The connection protocol is checked from the existing provider model. Unsupported
  native protocols are rejected. Multimodal usage is not inferred from model names
  and must not be submitted as text-token pricing. Native protocols with different
  usage definitions need their own normalization adapter before gateway billing.
- Metrics: `INPUT_TOKEN`, `OUTPUT_TOKEN`, `CACHE_READ_TOKEN`, `CACHE_WRITE_TOKEN`.
- Unit: `1M_TOKEN`, meaning the price for one million tokens.
- Tiers: `base` and `long_context`; one optional threshold, either `128000` or
  `200000` tokens. Zero disables the threshold. Enabled long-context rates require
  a threshold. Disabled old long-context rates may remain stored after disabling it.
- Currency: `USD`, `CNY`, `EUR`, `GBP`, `JPY`, `HKD`, or `SGD`. Different rates for
  the same model may use different currencies.

There is no silent rate fallback. Missing or disabled rates are errors when their
quantity is nonzero. Unlike upstream convenience defaults, a cache price does not
fall back to ordinary input, and a missing long-context price does not fall back to
base. Inclusive thresholds, arbitrary/multiple tiers, provider-specific cache TTL,
batch modifiers, image, request, character, audio, video, and other conditions are
not supported in this slice. Unknown request fields are rejected so these meanings
cannot silently be discarded.

## Normalized usage and calculation

Every quote requires all four nonnegative integer usage counts, including explicit
zero cache counts. Missing counts do not mean zero. `input_tokens` is the **total
input including cache-read and cache-write tokens**:

```text
ordinary_input = input_tokens - cache_read_tokens - cache_write_tokens
```

Cache categories must not overlap and their sum must not exceed total input.
Invalid usage is rejected instead of being clamped. The native Chat Completions usage adapter establishes these invariants before
assessing a call; other protocols require separate adapters.

The long-context tier applies only when total input is strictly greater than the
configured threshold. Equality uses base. The selected tier applies to the whole
request, including output and both cache categories, rather than charging only the
portion above the threshold. Output length never selects the tier.

For each nonzero quantity:

```text
component = quantity × rate_amount ÷ 1,000,000 × source_to_platform_exchange_rate
```

Amounts and exchange rates are plain decimal strings with at most 18 integer and
18 fractional digits. Exponents, negative values, `NaN`, and floating-point JSON
numbers are rejected. Calculations use `math/big.Rat`, with no binary floating
point. Each converted component rounds once to 18 fractional digits using
round-half-even. The total is the exact sum of those rounded components. Trailing
fractional zeros are removed in returned amounts. An explicit price `"0"` means
free; an exchange rate must be positive, and the platform currency's own rate is
exactly `"1"`.

Only nonzero quantities require an enabled rate and currency conversion during a
quote. Fully known zero usage returns zero only if the model has at least one
enabled configured rate. An absent price aggregate or a fully disabled aggregate
remains unpriced even for zero usage. An explicitly free nonzero quantity still
requires its currency conversion to be configured.

The quote captures provider-model and price IDs, adapter ID, normalized usage,
selected tier and threshold, each used rate's identity/amount/currency/unit,
exchange rates, per-component charges, platform currency, total, and catalogue
ETag. Later catalogue edits cannot change the returned snapshot. Gateway calls
retain the same basis and finalized result durably; settlement and replay never
read the latest catalogue to recalculate historical amounts.

## Catalogue mutations and exchange rates

All price and currency mutations share one opaque catalogue ETag. The ETag is a
concurrency token, not a business version or a retained price history. Writes lock
the settings row and compare the supplied token in the same transaction. Catalogue
reads use READ COMMITTED and hold that lock across price and FX reads, preventing
a new ETag from being paired with an earlier MySQL snapshot. A stale
token returns `409`; no part of that rejected batch or audit is committed.

Successful writes synchronously refresh the gateway runtime after the transaction
commits. A refresh failure returns `503` even though the catalogue change and its
audit have committed. Retrying the same old ETag returns `409` without another
write or audit: reload the catalogue and runtime status before retrying. Existing
in-flight requests retain their original snapshot. A failed route preparation
retains the last valid route and price generation together; authorization still
refreshes independently with its existing bounded lease.

A price batch submits 1–20 model aggregates, with 1–8 rates per submitted model.
Each `(provider_model_id, metric, tier)` is unique. Submitted rates are upserted;
omitted rates remain unchanged. `enabled` is mandatory and explicitly setting it
to `false` disables that rate. Existing IDs remain stable. An omitted
`context_threshold` preserves the current threshold; a new aggregate defaults to
zero. A threshold change must leave the resulting aggregate valid. For example,
disable retained long-context rates when setting the threshold to zero.

All writes currently originate from the authenticated API and set
`update_source: "api"` and `follow_repository: false` on affected aggregates.
Clients cannot claim repository provenance. Manual/import/repository features must
ultimately reuse the same validated catalogue transaction boundary; they are not
implemented here.

A currency write replaces the finite FX map and platform currency atomically.
Rates express source currency to the selected platform currency. Self-conversion
is included as one. Every enabled rate anywhere in the catalogue must have a
conversion under the resulting configuration. Removing a required conversion
fails the whole write with `422`. Configure new conversions before enabling a
price in that currency. Changing platform currency requires explicit replacement
FX values, rather than mechanically reinterpreting the old map.

Committed writes record actor, action, source, before/after normalized values,
and before/after ETags in the generic audit event's nullable `details_json` field.
Price audit details are limited to 60 KiB, and batch sizes are bounded. Unknown
request fields and arbitrary request bodies are never copied to audit details.
Ordinary non-pricing audit events continue to omit this field. Rejected mutations
return an error and do not manufacture a successful business audit.

## HTTP contract

All paths below are under `/api/v1`. Reads and quotes require `prices.read`;
writes require `prices.write`. Both permissions are granted to the built-in
administrator by migration 11 and may be delegated independently. Roles do not
grant model invocation rights. Mutations and quote POSTs require the authenticated
session's `X-CSRF-Token`. Management JSON bodies are limited to 64 KiB.

| Method and path | Contract |
| --- | --- |
| `GET /admin/prices` | `{etag,currency,items,next_cursor}`; filters `provider_model_id`, `cursor`, `limit` (default 50, maximum 100) |
| `GET /admin/provider-models/:provider_model_id/price` | Same envelope with one item; `404` if not configured |
| `PUT /admin/prices` | `{etag,items:[{provider_model_id,context_threshold?,rates:[{metric,tier,unit,currency,amount,enabled}]}]}`; returns the changed items and new ETag |
| `GET /admin/prices/currency` | `{etag,currency,required_currencies}` across all enabled catalogue rates; see [CURRENCY](CURRENCY.md) |
| `PUT /admin/prices/currency` | `{etag,currency:{platform_currency,rates:{USD:"7.1"}}}`; returns new ETag and currency configuration |
| `POST /admin/prices/quote` | `{provider_model_id,usage:{input_tokens,output_tokens,cache_read_tokens,cache_write_tokens}}`; returns `{etag,quote}` |

Each item includes `id`, `provider_id`, `provider_model_id`, `upstream_name`,
`protocol`, `context_threshold`, `update_source`, `follow_repository`, and `rates`.
Rate responses additionally include stable `id`. Rate IDs, provider IDs, source,
and follow flags are not accepted from clients.

Malformed/unsupported input returns `400`, authentication failure `401`, missing
permission or CSRF `403`, a missing catalogue identity `404`, stale ETag `409`, and
unpriced usage or missing required conversion `422`. Responses never expose
upstream credentials or connection secrets.

## Verification and remaining scope

`go test ./pkg/pricing` exercises exact arithmetic, half-even rounding, FX, explicit
free pricing, missing and disabled rates, missing overall configuration with known
zero usage, strict threshold equality, cache-inclusive tier selection, output tier
selection, invalid cache overlap, unsupported units/protocols, and immutable quote
results. The same fixed semantics cover both 128k and 200k thresholds.

The shared PostgreSQL/MySQL integration helper `testPricingLifecycle` covers atomic
batch rollback, partial upserts and stable IDs, permission delegation, CSRF,
unknown fields, missing normalized usage, ETag conflict and concurrent writers,
price/FX concurrency, exact quotes, missing FX rollback, cursor pagination, and
normalized audits. Invoke it through `go tool task test-integration` after migration
11 and route registration are wired.

Remaining P3 work includes required non-token metrics and finite conditions,
CSV/XLS/XLSX import, a documented repository format and sync mechanism, catalogue
UI, additional protocol/usage adapters, quota/reservation integration, and
reconciliation. This slice does not claim completion of P3 or
full provider pricing compatibility.
