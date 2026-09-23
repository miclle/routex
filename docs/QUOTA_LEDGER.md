# Durable quota ledger foundation

This module is an internal foundation. The gateway continues to use its existing RPM, concurrency, and IP policies until quota-aware admission and policy management are wired and accepted. Adding these files does not activate token or monetary limits, establish historical coverage, or complete the resource-limit roadmap.

## Authority and activation

The existing exclusively locked local bbolt journal is the enforcement authority. It keeps quota receipts separately from pending and ready call facts. SQL call reports remain asynchronous projections; their delivery and acknowledgment cannot clear enforcement history. The journal must remain on durable local storage with synchronous commits enabled. This design supports one gateway process per journal, not distributed admission across independent processes.

`EnableQuota(timeZone, now)` explicitly upgrades journal format 1 to format 2, records its coverage start and IANA time zone, and is idempotent only for that same zone. Merely opening a legacy journal does not claim that old requests were accounted for. Downgrading to a binary that only understands format 1 fails closed. Missing authoritative buckets, unknown versions, invalid receipts, and inconsistent retained facts prevent startup.

After activation, callers must dispatch through `ReserveWithQuota`; `ReserveWithLimits` rejects legacy dispatch admission. Generic `Reserve` remains available for rejected, non-dispatched request facts and cannot reuse a retained quota request ID. It must never dispatch traffic. The service integration must establish this distinction before calling `EnableQuota`.

A finite window that starts before coverage is unavailable for an older account. A trusted account creation timestamp at or after coverage proves that this account has no earlier usage. Unknown creation time does not. Once a window is entirely covered, an older account can use it. Rotation supplies the immutable original Key account identity and creation time. Nothing here infers an empty history from absent SQL rows, imports incomplete analytics, forgives usage, or exposes a reset operation.

## Atomic admission and settlement

`ReserveWithQuota` checks all supplied aggregate and child accounts, RPM, concurrency, token windows, and monthly money in one synchronous transaction. A successful transaction commits the fallback call fact, leases, quota receipt, immutable policy/bound/price references, and all economic holds together. A rejected child check leaves no aggregate debit or RPM consumption. The caller obtains the effective policies and request-specific proven bound before invoking this transaction; the ledger does not accept policy choices from public request fields.

`CompleteQuota` atomically replaces the fallback with the final call fact, releases concurrency, and settles each independently proven dimension. Its receipt digest binds the exact final payload and settlement. Identical retries remain idempotent after SQL acknowledgment while the receipt is retained; conflicting retries fail. A settled fact is never replaced. Request IDs must remain globally unique beyond receipt retention.

| Evidence | Token accounting | Money accounting |
| --- | --- | --- |
| Complete normalized input and output | Settle actual input plus output | Depends separately on immutable pricing evidence |
| Complete supported usage and effective rate/FX snapshot | Settle known tokens | Settle exact actual charge |
| Known tokens, unknown price | Settle actual tokens | Retain monetary bound, or mark unbounded unknown |
| Unknown tokens, known charge | Retain token bound, or mark unbounded unknown | Settle actual charge |
| Neither dimension proven | Retain both bounds or explicit unknowns | No invented zero charge |

HTTP success is not a completeness signal. Authoritative final usage can settle canceled or failed requests. Cancellation before final usage, interrupted streams, and missing counters retain the affected dimension's hold. An unbounded unknown blocks a later finite policy for that dimension while it remains in the relevant window. An explicitly configured free price is known zero; missing pricing is not.

Actual usage above its reservation is recorded in full, including debt beyond a limit. The ledger marks the bound revision invalid, so further constrained admissions cannot reuse it. It does not clamp the bill or discard an overrun. An independently corrected bound requires a new revision.

## Windows, clocks, and currency

TPM, five-hour tokens, and seven-day tokens use exact rolling intervals `(now - window, now]`. Monthly tokens and money use `[first day at 00:00, next first day at 00:00)` in the installation time zone, including daylight-saving transitions. Receipts retain the admission month; late settlement never moves a charge into its completion month.

Active reservations guard new admissions even when they cross a rolling or monthly boundary. Once terminal, settled usage and remaining conservative holds expire according to their original admission windows. A persisted monotonic wall-clock high-water mark prevents clock rollback from reopening an earlier window. It does not correct an administrator's large forward clock jump; deployments must maintain a trustworthy clock.

Null limits impose no cap at that account; callers supply every applicable aggregate and child scope. Explicit zero closes that dimension, including zero-bound requests. Money uses decimal strings with at most 18 fractional places. Currency identities are explicit; retained nonzero values in incompatible currencies cause rejection rather than conversion using a newer FX rate.

The installation time zone is immutable after activation in this foundation. Currency changes and policy-reset/default-template behavior require the later service transaction rules. No wallet, payment, balance adjustment, or operator release API is provided.

## Recovery and bounded retention

An admission left active after process loss becomes an interrupted receipt. Recovery preserves its conservative economic hold and replays its stored fallback fact; it cannot prove the upstream was never contacted. Concurrency leases are released independently. Receipt-derived counters and expiry indexes are rebuilt and checked at startup.

The ready queue retains its existing configured capacity. Quota receipts have a separate one-million-entry cap and live through the later of the admission month end and seven days after admission. Active reservations and undelivered facts are never removed merely to meet that cap. A full history rejects new admission. Acknowledgment releases only the delivery slot. Once all economic windows expire and the final fact is acknowledged, the receipt can be collected. Bound invalidations and trusted account identities remain durable metadata.

## Exact reservation arithmetic

`pricing.ReserveBound` uses the same validated fixed-metric schedule and FX data as settlement. The caller supplies proven maximum billable input and output tokens and whether cache read/write classifications are possible. Input includes cached tokens; output must include every billable output token, including reasoning where the native protocol defines it that way.

The calculation considers every reachable context tier and applicable input/cache rate, takes a conservative converted rate maximum, and adds an upper rounding allowance for separately rounded settlement components. It then rounds upward to 18 decimal places. Actual settlement continues to use the existing half-even rounding policy. A wholly configured zero-price schedule reserves zero. Missing potentially applicable rates or FX fails closed.

This arithmetic is not proof of a provider's token capacity or a native request's output cap. The protocol adapter must establish those bounds. It must reject unsupported constrained requests rather than substitute bytes, an approximate tokenizer, a guessed cache fraction, or an unrelated protocol's limits. No gateway adapters or capacity claims are enabled by this foundation alone.

## Verification

Focused package tests cover concurrent aggregate/child admission, transaction rollback, settlement idempotency after delivery, independent unknown dimensions, explicit zero, incompatible currencies, incomplete historical coverage, rolling boundaries, clock rollback, DST month boundaries, late settlement, active holds crossing periods, overrun debt, separate delivery/history capacity, corrupt-history rejection, and real process kills after activation, reservation, settlement, and acknowledgment.

Seeded exact-rational property tests compare 7,500 supported actual-usage combinations against their reservation bounds across context tiers, cache classifications, currencies, and 18-place prices. These tests establish the arithmetic property for the sampled cases; they do not certify upstream capacity metadata or a future gateway integration.
