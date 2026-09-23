# CSV price import and export

RouteX supports local CSV preview, atomic commit, and current-catalogue export.
These endpoints reuse the current text-price catalogue, exact decimal validation,
ETag concurrency, audit, and runtime-publication boundary. No separate price book,
file database, or migration is introduced. CSV does not modify invocation grants,
model lifecycle, credentials, platform currency, or exchange rates.

This is the CSV portion of price-file management. XLS/XLSX parsing, remote
repository synchronization, restore-to-repository behavior, and additional pricing
metrics or conditions remain separate work. The current source/follow metadata
belongs to a model-price aggregate; per-rate repository ownership is not claimed.

## File contract

Files are UTF-8 CSV, optionally with a UTF-8 BOM, using comma delimiters and standard
quoted fields. LF and CRLF are accepted. Column order is arbitrary. Every required
column must occur exactly once; unknown or duplicate columns are rejected. Empty
lines are ignored. Values are not silently trimmed or case-folded.

| Column | Required header | Value |
| --- | --- | --- |
| `provider_model_id` | Yes | Existing stable provider-model ID; the sole mapping identity |
| `metric` | Yes | `INPUT_TOKEN`, `OUTPUT_TOKEN`, `CACHE_READ_TOKEN`, or `CACHE_WRITE_TOKEN` |
| `tier` | Yes | `base` or `long_context` |
| `unit` | Yes | `1M_TOKEN` |
| `currency` | Yes | `USD`, `CNY`, `EUR`, `GBP`, `JPY`, `HKD`, or `SGD` |
| `amount` | Yes | Nonnegative plain decimal string; up to 18 integer and 18 fractional digits |
| `enabled` | Yes | Explicit lowercase `true` or `false` |
| `context_threshold` | Yes | Blank preserves the current value; `0`, `128000`, or `200000` sets it explicitly |
| `upstream_name` | No | Advisory display label; ignored for mapping, validation of identity, and writes |

A new model-price aggregate with blank threshold starts at zero. Every row for the
same provider model must specify the same threshold, including blank. A duplicate
`(provider_model_id, metric, tier)` rejects the file even when values agree. Models
must already exist on an `openai_chat` connection. Unsupported dimensions such as
batch, cache TTL, image, audio, or arbitrary conditions are rejected as unknown
columns or unsupported values; they are never silently reduced to ordinary text.

```csv
provider_model_id,metric,tier,unit,currency,amount,enabled,context_threshold
pmo_example,INPUT_TOKEN,base,1M_TOKEN,USD,0,true,128000
pmo_example,OUTPUT_TOKEN,base,1M_TOKEN,USD,2.5,true,128000
pmo_example,CACHE_READ_TOKEN,base,1M_TOKEN,USD,0.25,false,128000
```

The example ID must be replaced with an existing provider-model ID. A zero price
is explicitly free; a disabled rate remains explicitly disabled. Omitted rows and
metrics are preserved, never deleted or disabled. Retained rates participate in
validation: setting threshold zero while retaining enabled long-context rates is
invalid. Configure required currency conversions through the currency API before
enabling rates that use them.

Import bounds are 32 KiB of CSV text, 160 data rows, and 20 distinct provider models.
The management request body is also limited to 64 KiB, including JSON escaping.
The normalized before/after audit must fit the existing 60 KiB audit bound; preview
reports a file-level error if the changes require a smaller batch. No files are
written to disk or retained by preview.

## Preview and commit

All paths are under `/api/v1`. Preview and export require `prices.read`; commit
requires `prices.write`. Both POST endpoints require the authenticated session's
`X-CSRF-Token`. Unknown JSON fields and trailing JSON values are rejected, so
clients cannot submit alternate items, source labels, or discarded intent.

| Method and path | Request and response |
| --- | --- |
| `POST /admin/prices/import/preview` | `{csv: string}` → `{etag,preview_digest,valid,items,changes,errors}` |
| `POST /admin/prices/import/commit` | `{csv: string,etag: string,preview_digest: string}` → `{preview,catalogue}` on success |
| `GET /admin/prices/export.csv` | Complete CSV download, with catalogue `ETag` header |

Preview reads a consistent catalogue/FX snapshot without taking the catalogue
write lock. It performs no business writes, audit insertions, or runtime refresh.
`items` contains canonical model inputs, with decimal amounts normalized and rows
sorted by stable identity. `changes` retains original physical row numbers and
includes actual upstream names, added/updated/unchanged classification,
before/after rates, before/after thresholds, and `stops_following` where applicable.
Changing only a display label never selects a different model.

Each error has `{row,column,code,message}`. Physical row numbers include the header;
row zero identifies a file-level bound. Independent field errors and semantic
errors across parseable rows are collected within the file limits. A malformed CSV
quote makes safe resynchronization impossible, so parsing stops at that syntax
error rather than inventing later row boundaries. Invalid headers prevent mapping
rows to fields. Invalid previews return HTTP 200 with `valid:false` and an empty
digest; they do not imply any applied change.

The preview digest binds the catalogue ETag and canonical effective input. Row and
column ordering, equivalent decimal spellings, line endings, and ignored display
labels do not change it. The digest is a stateless content check, not an
authorization capability or a server-stored approval. Permissions and all content
are checked again during commit.

Commit reparses the submitted CSV, obtains the existing governance and pricing
locks, checks permission and ETag, validates against the current catalogue and FX,
and recomputes the normalized digest. A changed effective input with an old digest
returns 400. Validation errors return 422 with `{preview}` and all located errors.
A stale ETag returns 409 before writes. No validation or transaction failure leaves partial prices, source changes,
audit entries, or a consumed ETag.

A successful commit upserts all submitted rates in one transaction, retains stable
existing IDs, marks affected aggregates `update_source:"csv"` and
`follow_repository:false`, writes one normalized audit with server-assigned source
`csv`, advances the ETag, and synchronously refreshes the runtime. The client cannot
claim an alternate source. Concurrent commits against one ETag have at most one
winner. Replaying the consumed ETag returns 409 and does not duplicate the audit.

As with ordinary price writes, runtime refresh occurs after the transaction
commits. A refresh failure may therefore return 503 after a successful catalogue
change. Reload the catalogue and runtime state before retrying; reusing the old
ETag cannot silently apply the write twice. Existing in-flight gateway calls keep
their previously captured pricing basis.

## Export and spreadsheet safety

Export reads a consistent snapshot and includes every current stored rate,
including explicit zero and disabled rates. It emits the required columns plus
`upstream_name`, exact decimal strings, explicit booleans, and actual thresholds.
It has no hidden first-page behavior: more than 500 model-price aggregates, 4,000
rates, or 2 MiB of generated CSV returns 422 with no partial file. An empty
catalogue returns the header alone; an empty file is not a valid import.

Names are untrusted spreadsheet data. Before CSV quoting, export prefixes a single
apostrophe when a name begins with `=`, `+`, `-`, or `@`, including after leading
Unicode whitespace or control characters. This prevents formula interpretation
when opened in common spreadsheet applications. The display name may therefore
differ from the literal catalogue name; importing it is safe because it never
controls identity or overwrites the model name. CSV syntax quoting separately
preserves commas, quotes, and line breaks.

The download uses a fixed attachment filename, `text/csv; charset=utf-8`, no-store,
and `nosniff` headers. Its quoted HTTP ETag identifies the captured catalogue. An
export larger than the import batch bound must be split into bounded imports,
with a fresh preview for each batch after the preceding commit advances the ETag.

## Verification

Focused parser tests cover exact decimal preservation, explicit zero/disabled
state, stable normalized digests, duplicate rows, conflicting thresholds, unknown
dimensions/source fields, malformed encoding/CSV, file/row/model limits, and
spreadsheet formula protection including leading controls and whitespace.

The shared `testPriceImportLifecycle` helper exercises PostgreSQL and MySQL through
the single database harness: read-only previews, all-row semantic errors, retained
schedule validation, digest mismatch, strict JSON, permissions and CSRF, atomic
commit and rollback, stable IDs, omitted-row preservation, source audits, stale
and concurrent ETags, round-trip export, formula protection, and explicit export
limit failure. Run it through the repository's isolated integration task after
route and harness registration.
