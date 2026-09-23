# Platform currency and exchange rates

The currency workspace manages one current conversion configuration for the price
catalogue. Source model prices keep their original currencies. A conversion means
`1 unit of source currency = N units of platform currency`; self-conversion is
exactly `1`. Supported codes are USD, CNY, EUR, GBP, JPY, HKD and SGD.

Existing call assessments retain their request-time price and FX basis. A currency
edit does not rewrite past amounts or the prices charged in their source currency.
Reports must retain currency identity rather than add historical amounts from
different platform currencies together.

## Read and write contract

Both endpoints require a console session. Reads require `prices.read`; writes
require `prices.write`, same-origin protection and the session CSRF token.

- `GET /api/v1/admin/prices/currency` returns `{etag,currency,required_currencies}`.
  The currency object contains `platform_currency` and decimal-string `rates`.
  Requirements cover every enabled rate in the complete catalogue, independently
  of catalogue pagination. Disabled rates do not require an exchange rate.
- `PUT /api/v1/admin/prices/currency` accepts `{etag,currency}` and replaces the
  current finite conversion map atomically. Its response is the existing price-page
  envelope with the new ETag and currency configuration.

Reads use the same catalogue generation lock and READ COMMITTED behavior as price
reads, so required currencies, FX values and ETag cannot come from different
catalogue generations. Writes revalidate all enabled currencies inside the pricing
transaction. A stale ETag returns 409 and an incomplete conversion map returns
422; neither partially changes the configuration. A runtime publication failure
can return 503 after commit. Reload and reconcile before retrying a mutation.

Rates must be positive plain decimal strings with at most 18 integer and 18
fractional digits. Zero, negative values, exponents, floating-point JSON numbers
and unsupported currencies are rejected. Omitted unused currencies are allowed.
The server remains authoritative even when a client performs its own validation.

## Interface behavior

The workspace preserves a current-configuration card, exchange-rate table and
explicit confirmation dialog. Changing the platform currency clears the other
draft rates instead of reinterpreting old conversions. The self-conversion input
is fixed, required currencies are marked, and blank optional rows are omitted from
the submitted map. The confirmation shows the old/new platform currency and every
submitted conversion without converting decimal strings to JavaScript numbers.

An editor retains one reviewed ETag, currency configuration and requirements
snapshot. A background update cannot silently replace that basis. A changed ETag
or 409 blocks confirmation until the user explicitly reloads and reviews the
retained draft. Cancel changes copies the current reviewed configuration. Both
pending state and a synchronous submission guard prevent duplicate confirmations.
Read-only users can inspect the same configuration without mutation controls.
English and Chinese copy update without discarding the current draft.

## Verification

HTTP tests cover unauthenticated and delegated permission checks, complete currency
requirements, disabled-rate exclusion, and currency/ETag coherence. The shared
PostgreSQL/MySQL harness validates writes, missing-conversion rollback and catalogue
concurrency. Seven frontend workflow tests cover initial read-only behavior,
required rates, currency switching, exact decimals, confirmation, duplicate
submission, stale/background generation changes and live language switching.

Controlled browser verification exercises the same table and confirmation flow
with synthetic HTTP data. It does not establish external exchange-rate feed or
provider-invoice accuracy. Automatic rate feeds, monetary policy conversion and
mixed-currency analytics are separate features.
