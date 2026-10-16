# Bounded pending spool enumeration

`SharedSpool.ReadPending` lists the exact original keys of retained records that
are not ready for call-fact delivery. It is a read-only package operation, not
an admission, recovery, executor-closure or economic operation. Gateway startup
and shared admission activation remain separate work.

```go
ReadPending(ctx context.Context, after *SharedSpoolPendingCursor, limit int) (SharedSpoolPendingPage, error)
```

The page returns `Keys []SharedSpoolKey` and an optional `Next` cursor. Keys retain
the original installation, spool, mode revision, incarnation, fence, execution,
request and admission digest. Use the existing exact-key `ReadRecord` to read
their current retained bodies. Enumeration does not authorize adopting or
replaying an execution and does not prove that the later read is unchanged.

Start with a nil cursor. A continuation contains the exact spool identity and
the last fully examined request ID, ordered by the existing bbolt byte order.
The next page seeks strictly after that ID, even if its record was acknowledged
and removed between reads. Foreign identities, empty or malformed request IDs,
and invalid limits fail without returning partial results.

Each call accepts a limit from 1 through 64, independently of configured spool
capacity. It returns fewer keys when necessary. Every examined record, including
a ready record that is excluded from results, counts toward both fixed bounds:

| Bound | Maximum per page |
| --- | --- |
| Returned pending keys | Requested limit, at most 64 |
| Examined retained records | 64 |
| Validated raw request-key, record and ready-index bytes | 8 MiB |
| One serialized record | Existing 2 MiB cap |
| One ready-index entry | Existing 2 KiB cap |

The byte bound limits serialized content admitted to page validation; it is not
a physical disk-read, bbolt page, allocation or latency guarantee. Mapped value
lengths are checked before decoding. An over-budget next record is deferred,
without advancing the continuation past it. The existing single-record bound
fits the page budget, so a valid remaining record cannot cause a non-progressing
continuation.

An empty `Keys` page with a non-nil `Next` is valid progress through ready
records. Continue until `Next` is nil, which means that read reached the current
end. Each page uses one read transaction; multiple pages are not a durable global
snapshot. Later insertions before a continuation require a new sweep from nil.
Keys that become ready or are delivered after enumeration must be re-read and
handled according to their actual exact-key state.

Every examined record uses the existing structural, body-checksum, attribution
and ordered-stage validation. Its stored request ID must equal its bucket key.
A ready record is excluded only after its ready index exactly matches its
retained receipt. An unexpected ready index on a pending record, missing or
changed ready index, malformed or oversized content, or a same-key delivered
tombstone fails closed. This is page-local validation, not an audit of unseen
records or orphan index entries elsewhere in the journal.

Cancellation is checked before validation, inside the read transaction, between
records and before returning the page. Cancellation or corruption discards the
entire page, including previously examined valid keys and continuation. Reads
never repair, prune, acknowledge, promote, settle or release a retained record.

Focused test sources cover exact original identities across reopen and multiple
pending stages, preserved unknown dispatch evidence, ready/delivered exclusion,
byte-identical read-only files, ready-only scan progress, maximum-record byte
budget deferral, fixed limits on small spools, identity-bound cursor validation,
mid-page cancellation with no partial result, deleted cursor keys, repeat sweeps
for earlier insertions, and corrupt suffixes after a valid first record.

This slice has source formatting and whitespace validation only until its
separate focused/race qualification runs. It supplies no recovery-only
coordinator constructor, shared SQL proof, owner replacement, history transfer
or two-process Gateway acceptance. See [shared call spool](SHARED_CALL_SPOOL.md)
for the existing durable stages and delivery boundary.
