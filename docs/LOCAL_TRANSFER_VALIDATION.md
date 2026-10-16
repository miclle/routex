# Original Local-v2 Transfer Validation

`eventqueue.ValidateLocalTransferSnapshot` is a dormant read-only preparation
API. It accepts one already-open `LocalInspection`, explicit total bounds, and
a cutover instant in Unix nanoseconds. The same retained inspection transaction
supplies the header and every page of all twelve original local-v2 buckets.
The caller owns inspection lifetime and genuine deployment exclusion.

This source is not registered in a migration, command, service or public route.
It does not open a writable queue, recover, prune, import, convert original
receipts into shared receipts, or activate shared admission.

## Bounds and complete evidence

All four bounds are required: cumulative inspection bytes, bucket records,
distinct quota/RPM account identities, and recorded references. Bytes include
the header and repeated page framing. Reference counts include receipt-account
relationships, quota expiry references, RPM events and sticky invalidations.
Existing per-operation inspection bounds still apply. Quota receipts and RPM
events additionally retain their original 1,000,000 and 200,000 capacity limits.
No global capacity is inferred for other buckets.

Every bucket must be exhausted in the retained snapshot. Cancellation, closed
inspection, overflow, invalid data or an undrained journal returns an error and
no partial result. Cutover must be at or after the recorded clock high-water.
The result preserves detached original keys, values and sequences. Its framed
SHA-256 covers each original bucket and entry exactly once, independently of
page boundaries. It is an internal consistency digest, not authenticated
historical provenance or proof that the inspected path cannot be replaced.

## Original semantics and independent projections

The initial supported scope requires empty pending, ready and lease buckets,
no active quota receipts, and no orphan active counts. Validation checks original
metadata, canonical recorded births, receipt states and dimensions, exact original
month coordinates in the recorded time zone, sequences, sticky invalidations,
expiry references and stored projections. Duplicate/case-alias JSON fields,
unknown fields and additional JSON are rejected. Original receipt bytes remain
unchanged; omitted legacy optional fields are not reconstructed as evidence.

Stored quota counters are recomputed from the **retained expiry references**,
including already-expired references. Stored RPM counts are recomputed from
**all retained RPM events**. A terminal write can advance the clock without
pruning those projections, so comparing stored counters directly with a live
projection would incorrectly reject valid original journals. Missing unexpired
quota/RPM references are rejected even if the corresponding counter is removed.
Expired references may already have been pruned; an expired counter without its
retained reference is inconsistent.

Only after stored projections agree does validation derive separate live quota
and RPM projections at the requested cutover instant. It excludes expired
contributions without changing any original data or regrouping calendar months.
Unknown account birth remains recorded zero. An independently unknown terminal
dimension retains its original bound or unbounded unknown hold. Sticky invalid
bound markers survive absence of a pruned receipt and subsequent request ID
reuse. A marker's request ID has no incarnation proof and is never attributed
to a newer retained receipt sharing that ID. Retained pre-v2 RPM events never
manufacture quota receipts or resource births.

Quota activation can retain the existing logical timestamp during equal time
or clock rollback. RPM events exactly at `CoverageStart` therefore remain
historically ambiguous, even if their request ID matches a retained receipt.
Only strictly later events establish the quota-era boundary used for receipt
consistency checks. A strictly later event requires its original quota receipt
until the maximum of its recorded-zone month end and admission plus seven days,
independently of RPM's one-minute expiry. Completely retired history may already
be pruned at that exact economic horizon; neither its absence nor an ambiguous
boundary event establishes historical transfer authority.

Terminal payloads are absent in this drained scope. Their settlement digest
syntax can be checked, but the digest cannot be recomputed from an acknowledged
payload that no longer exists. Absence does not prove acknowledgment or delivery;
`UnverifiedSettlementDigests` reports this limit explicitly.

## Incomplete transfer

A successful result always has typed `TransferState = LocalTransferIncomplete`
and lists unproven historical provenance, executor closure, installation binding
authority, historical SQL calendar, shared receipt coordinates, and ingestion /
cutover. A missing installation binding remains missing. Calendar consistency
uses the recorded zone and available time-zone database; it does not authenticate
historical calendar configuration.

Original local data cannot supply shared owner/fence/execution identities,
historical SQL ETags, reservation manifests or current authorization. Further
work must establish those boundaries, implement a distinct original-history
representation and audited importer, perform an actual excluded cutover, and
qualify real restart and multi-process behavior. No accepted transfer, runtime
activation, native completion or full F17 acceptance is claimed by this API.

## Verification status

The isolated source includes tests for expired-but-retained versus live
projections, already-pruned references, corruption, exact total bounds,
multipage census, cancellation, drainage, unknown births and interrupted holds,
legacy RPM, equality/rollback activation boundaries, sticky invalidations across
request ID reuse, economic retirement horizons and original optional-field
receipt bytes.
Only source formatting and whitespace checking have been performed for this
slice. Compilation, test execution and integration delivery remain pending.
