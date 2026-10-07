# Current price catalogue and native pricing

RouteX has one current price aggregate per actual provider model. It is identified
by the stable `ProviderModelID`, independently of the upstream name or platform
model names. Provider identity and connection protocol are derived from the
existing catalogue. Price records use `prc_` IDs and rate records use `rat_` IDs.
There are no price books, publication stages, effective-time versions, alternate
supplier/public catalogues, or formula expressions.

The control-plane quote API is a deterministic dry run. The gateway also assesses
supported final native usage against the price and exchange-rate snapshot captured
before dispatch. These immutable call amounts do not deduct quota, create invoices,
or post financial ledger records. See [Gateway assessment](METERING.md).

## Token and media pricing adapters

RouteX keeps the finite Go text-pricing adapter `routex_text_v1` and adds
`routex_multimodal_v1` when a schedule contains an image or PDF input rate. Both
calculate quotes with exact rational arithmetic and explicit validation. The
current RouteX catalogue and configured exchange rates are the authoritative
inputs. Unsupported pricing dimensions are rejected rather than inferred.

The supported adapter has these explicit boundaries:

- Protocols: `openai_chat`, `openai_responses`, `anthropic_messages`, and
  `gemini_generate_content`. Dedicated native adapters normalize their terminal
  token counters before gateway assessment.
- Token metrics: `INPUT_TOKEN`, `OUTPUT_TOKEN`, `CACHE_READ_TOKEN`, and
  `CACHE_WRITE_TOKEN`, each with unit `1M_TOKEN`.
- Media metrics: `IMAGE_INPUT` with unit `1_IMAGE`, and `PDF_INPUT` with unit
  `1_PDF`. They count strictly validated RouteX attachment-reference occurrences
  that are forwarded upstream. Reusing the same object twice counts twice while
  object storage still reads it once.
- Tiers: `base` and `long_context`; one optional threshold, either `128000` or
  `200000` tokens. Zero disables the threshold. Enabled long-context rates require
  a threshold. Media rates are base-only. Disabled old long-context rates may
  remain stored after disabling it.
- Currency: `USD`, `CNY`, `EUR`, `GBP`, `JPY`, `HKD`, or `SGD`. Different rates for
  the same model may use different currencies.

There is no silent rate fallback. Missing or disabled rates are errors when their
quantity is nonzero. Unlike upstream convenience defaults, a cache price does not
fall back to ordinary input, and a missing long-context price does not fall back to
base. An enabled media rate with amount `0` is the explicit declaration that the
native aggregate input-token price already covers that media kind. RouteX never
derives a price from bytes, image dimensions, pixels, PDF pages, object metadata,
model names, or sample calls. Inclusive thresholds, arbitrary/multiple tiers,
provider-specific cache TTL, batch modifiers, request, character, audio, video,
and other conditions remain unsupported. Unknown request fields are rejected so
these meanings cannot silently be discarded.

## Normalized usage and calculation

Every quote requires all six nonnegative integer usage counts, including explicit
zero cache and media counts. Missing counts do not mean zero. `input_tokens` is the
**total input including cache-read, cache-write, and provider-reported media input
tokens**:

```text
ordinary_input = input_tokens - cache_read_tokens - cache_write_tokens
```

Cache categories must not overlap and their sum must not exceed total input.
Invalid usage is rejected instead of being clamped. Dedicated Chat Completions, [Responses](RESPONSES.md) and [Messages](MESSAGES.md)
adapters establish these invariants before assessing a call. Messages sums native
uncached input, cache creation and cache reads into inclusive input and treats
thinking as part of native output. Unsupported cache lifetimes, hosted tools and
request conditions remain explicitly unpriced; other protocols require separate
adapters.

The long-context tier applies only when total input is strictly greater than the
configured threshold. Equality uses base. The selected tier applies to the whole
request, including output and both cache categories, rather than charging only the
portion above the threshold. Output length never selects the tier.

For each nonzero token quantity:

```text
component = quantity × rate_amount ÷ 1,000,000 × source_to_platform_exchange_rate
```

For each nonzero media occurrence quantity:

```text
component = quantity × rate_amount × source_to_platform_exchange_rate
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

A price batch submits 1–20 model aggregates, with 1–10 rates per submitted model.
Each `(provider_model_id, metric, tier)` is unique. Submitted rates are upserted;
omitted rates remain unchanged. `enabled` is mandatory and explicitly setting it
to `false` disables that rate. Existing IDs remain stable. An omitted
`context_threshold` preserves the current threshold; a new aggregate defaults to
zero. A threshold change must leave the resulting aggregate valid. For example,
disable retained long-context rates when setting the threshold to zero.

Authenticated detail edits and reviewed CSV/XLS/XLSX imports reuse the same
validated catalogue transaction boundary and set `follow_repository: false`.
Clients cannot claim repository provenance. Import preview is server-derived and
commit replays only the captured file bytes, catalogue ETag, and preview digest.
Repository synchronization uses the explicit mapping, preview and receipt
workflow described below.

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


## Price workbook export

`GET /api/v1/admin/prices/export.xlsx` requires a current authenticated actor
with independent `prices.read`. It needs neither `prices.write` nor a CSRF
header and changes no price, catalogue metadata, grant or audit. The existing
CSV endpoint and its bytes remain unchanged. Both formats use the same complete
authorized read-only repeatable-read price snapshot; no supplier-directory or
per-row frontend query is added.

Success is a genuine ZIP-based XLSX workbook with one visible `Prices` sheet.
Its ordered columns are `provider_model_id`, `metric`, `tier`, `unit`, `currency`,
`amount`, `enabled`, `context_threshold` and `upstream_name`. All cells are literal
inline text, preserving exact decimal strings, explicit zero, disabled rates,
integer thresholds, currencies and recorded names. Formula-like names remain
literal; XLSX does not add CSV's protective apostrophe. No formulas, external
links, macros, hidden sheets or executable cells are emitted. Invalid UTF-8 or
XML-incompatible text is rejected rather than silently changed.

HTTP200 uses
`application/vnd.openxmlformats-officedocument.spreadsheetml.sheet`, fixed
`Content-Disposition: attachment; filename="routex-prices.xlsx"`, the quoted
current catalogue ETag, `Cache-Control: private, no-store` and
`X-Content-Type-Options: nosniff`. Authentication/permission errors remain 401/403.
All XLSX responses are private/no-store; complete-export or text failures return
JSON422 before a workbook body, download disposition or ETag is emitted.
Ordinary database errors retain centralized mapping.

Export permits at most 500 stored price models and
`500 * pricing.MaxRates` rates (currently 5,000). Compressed output is at most
512 KiB, expanded XML at most 4 MiB in total and per entry, at most 128 ZIP
entries, 100,000 XML tokens per entry and depth 64. The serializer emits five
fixed parts. These are independent rejection ceilings, not a promise that every
500-model/5,000-rate catalogue fits. The export is complete or rejected; a
header-only empty catalogue export is valid.

The existing `/admin/prices` download step offers Download Excel followed by
Download CSV. The client checks the Excel MIME, nonempty Blob and 512 KiB byte
bound without parsing cells or converting money, then downloads the original
Blob under `routex-prices.xlsx`. Downloads retain transient actor and successful
Session/permission generations, an abort controller and a duplicate-operation
lock. Renewal, error, read-permission withdrawal, expiry, actor change and
unmount discard obsolete replies before a file or notice is prepared. Recovery
never starts another export. No Blob enters query/mutation caches or browser
storage; object URLs are revoked. A prepared-download notice is not proof that
the browser saved a file.

Download limits do not enlarge upload limits: CSV imports remain 32 KiB,
workbook imports 512 KiB, and both retain 200 rows and 20 models per batch.
Split larger valid exports into valid upload batches. Empty imports remain
invalid, and omitted rows never imply deletion. Server previews, captured bytes,
ETag/digest confirmation and independent write permission remain unchanged.

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
| `POST /admin/prices/quote` | `{provider_model_id,usage:{input_tokens,output_tokens,cache_read_tokens,cache_write_tokens,image_inputs,pdf_inputs}}`; returns `{etag,quote}` |

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
free pricing, missing and disabled token/media rates, repeated media occurrences,
missing overall configuration with known zero usage, strict threshold equality,
cache-inclusive tier selection, output tier selection, invalid cache overlap,
unsupported units/protocols, and immutable quote results. The same fixed semantics
cover both 128k and 200k thresholds.

The shared PostgreSQL/MySQL integration helper `testPricingLifecycle` covers atomic
batch rollback, partial upserts and stable IDs, permission delegation, CSRF,
unknown fields, missing normalized usage, ETag conflict and concurrent writers,
price/FX concurrency, exact quotes, missing FX rollback, cursor pagination, and
normalized audits. Invoke it through `go tool task test-integration` after migration
11 and route registration are wired.

Per-page, per-pixel, per-byte, provider-specific media-token splits, audio/video,
and other native billing dimensions remain unsupported until an authoritative
provider contract and controlled fixture establish their quantities. External
paid-provider equivalence remains a separate acceptance gate. This slice does not
claim full provider pricing compatibility.

The [Gemini adapter](GEMINI.md) normalizes native candidate, thought and cached input counters. Missing counters, hosted-tool conditions and unsupported modalities remain explicitly unpriced.

The administrative Model routing table may read each exact Provider-model's base
input/output schedule under independent prices.read authority. Scoped reads retain
PricePage and a coherent pricing generation/FX snapshot, reject query expansion,
and preserve exact decimal values. Missing, disabled and zero rates stay distinct;
no route-price aggregation, media inference or pricing write occurs in that table.

## Repository source maintenance

The initial trusted source is the versioned `prices/catalog.json` file embedded
in the RouteX binary. Its complete bytes identify a source generation by SHA-256.
The file initially shipped empty. The reviewed three-model/six-base-rate seed
has controlled application acceptance; no invented market rate is added. A
later source adapter can replace this mechanism without granting the browser
arbitrary URL, path, Git or amount authority.

### Initial reviewed source seed

The selected trusted source remains the file inside this RouteX repository. The
reviewed data candidate contains exactly three model entries and six enabled
base `INPUT_TOKEN` / `OUTPUT_TOKEN` rates, observed on 2026-10-05. It uses strict
schema version 1, decimal strings, USD per 1,000,000 tokens and a zero context
threshold for each entry. The embedded source passed current-binary preview,
application, durable receipt, same-artifact restart and no-write replay on
PostgreSQL and MySQL. Its exact native
identifiers and direct-provider standard schedule are:

| Native model | Protocol | Base input (USD/1M tokens) | Base output (USD/1M tokens) |
| --- | --- | --- | --- |
| `gpt-4.1-mini-2025-04-14` | `openai_chat` | `0.40` | `1.60` |
| `claude-haiku-4-5-20251001` | `anthropic_messages` | `1` | `5` |
| `gemini-3.5-flash-lite` | `gemini_generate_content` | `0.30` | `2.50` |

Source keys remain the exact identities in `prices/catalog.json`; they are not
local ProviderModel IDs or model discovery. Primary provider documents and
observed billing conditions are recorded in [prices/README.md](../prices/README.md).
This snapshot supplies no universal pricing coverage or paid-invoice proof.

Use the existing repository maintenance card at `/admin/prices`:

1. Read the embedded source and its complete-byte SHA-256. Maintain the versioned
   file manually; a changed source requires building and deploying its binary.
   There is no arbitrary runtime path, scheduled external synchronization or
   network fetch from this source package.
2. Explicitly map existing exact ProviderModel IDs to source model keys with
   matching native protocols. Saving or enabling the configuration changes no
   prices and does not discover models or verify account/endpoint eligibility.
   Preserve independent `prices.read` and `prices.write` authority.
3. Request the server-derived difference preview for the exact selected mappings
   or rates. Keep its review ETag, source digest, preview digest and selection;
   do not synthesize changes in the browser. Review protected custom zero,
   disabled and same-amount rates separately.
4. Enter a reason and explicitly confirm the captured operation, retaining one
   UUIDv4 intent and the same selection/digests through uncertain retries with
   the reviewed If-Match. Only the authorized result confirms durable commitment
   and separately reports current configuration/runtime application. A matching
   GET never resolves the original uncertain operation.

Cache-read/write rates are absent, not zero or ordinary-input prices. Positive
cache use without an applicable rate remains unpriced; finite money reservation
may reject a schedule missing reachable cache rates, even for an uncached request.
Cache storage, media occurrences, long-context tiers, Batch/Flex/Priority,
regional/reseller/negotiated/discount schedules and other unsupported conditions
are omitted. Source omission neither deletes saved rates nor overrides custom
ownership, and updates never recalculate immutable historical call amounts.

The earlier empty-production-source acceptance below remains historical and
distinct from the current seed acceptance above. F15 remains Partially completed because broader external and release acceptance remains open.

Explicit mappings connect existing Provider-model identities to reviewed source keys.
Configuration changes do not apply prices. A server-derived preview binds the
reviewed source, configuration, catalogue and selected identities; application
requires explicit confirmation, a reason and one retained UUIDv4 intent. Durable
receipts establish historical commitment separately from current configuration
and runtime publication. A matching current read alone cannot resolve an original
uncertain operation. Receipt timestamps are projected in UTC with their exact
persisted instant and microsecond precision. Direct service and HTTP receipts
therefore retain the same immutable identity across server timezones; this
normalization does not change a saved operation or current publication status.

Per-rate provenance preserves manually submitted values, including zero,
disabled and same-amount edits. Unsubmitted rates retain their ownership. Selected
custom rates can be explicitly restored from the reviewed source; missing source
rows never imply deletion. Context threshold ownership is independent, and
changes must not reinterpret protected custom rates. Historical monetary call
bases stay immutable.

The maintenance card belongs above the existing price-file workflow. Selected
rate restoration belongs in the existing Provider-model price table. Read and
write permissions remain independent. Operational timestamps describe the last
fresh committed application, including no-change outcomes; previews, rejected
requests, replays and rolled-back failures do not fabricate success metadata.

The source, backend, interface and frozen V49 integration passed current-main
source gates, actual PostgreSQL/MySQL migration/lifecycle, controlled production,
manual bilingual browser and complete dual-driver regression. Receipt publication
and restart boundaries were verified separately from historical commitment. Controlled synthetic
source rates, if used for acceptance, belong only in a separately labelled test
artifact; the production catalogue remains empty.


Current-main source gates passed with 1908 frontend cases/104 files and full Go
race/unit/dev/asset checks. Actual PostgreSQL/MySQL migration/lifecycle focus
passed in 62.508s after a fixture-only live-price-FK preparation correction, preserving
all orphan receipt/mapping and historical pricing assertions. Owned cleanup and
49 current code hashes were verified. Two final-main isolated production scenarios each passed exactly three completed
native calls, immutable pricing bases and real restart without replay. Manual
English/Chinese browser confirmation verified exact operation receipts, a genuine
60-second Session renewal with retained reason, selected-rate restoration and
the empty production-source boundary. Owned resources were removed and checked.
Complete main race regression passed Handler 1314.101s/Service 8.002s and the
mandatory check passed;
separately labelled synthetic prices are test-only.
