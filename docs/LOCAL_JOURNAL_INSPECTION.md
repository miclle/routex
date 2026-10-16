# Read-only local journal inspection

## Delivery boundary

This source package introduces bounded read-only inspection of an existing local
admission journal before ordinary startup recovery. It does not implement an
importer, shared-mode startup, a cutover operation, process takeover, or activation.
The tests described here are source-only and have not run in this delivery packet.

Ordinary `eventqueue.Open` changes local recovery state: it rebuilds indexes,
releases concurrency leases, interrupts active quota receipts, and promotes
pending fallback facts. It must never be used to inspect original cutover evidence.
`OpenLocalInspection` does not call that API or any initialization, recovery,
pruning, write transaction, bucket creation, or filesystem creation operation.

## API and lifetime

```go
inspection, err := eventqueue.OpenLocalInspection(ctx, existingPath)
// Handle err before using inspection.
defer inspection.Close()

header, err := inspection.Header(ctx)
page, err := inspection.ReadPage(ctx, bucketName, exclusiveAfterKey, 128, 256<<10)
```

The handle owns a bbolt `ReadOnly` database and one read transaction until
`Close`. bbolt opens the file with `O_RDONLY`, without `O_CREATE`. Its shared file
lock excludes ordinary bbolt read/write handles on the same inode throughout
inspection. Multiple read-only inspections may coexist. Methods serialize access
to the retained transaction; close is idempotent and releases the transaction
before closing the database.

Opening requires an existing nonempty regular file, rejects a final path symlink,
and verifies that the opened descriptor still identifies the inspected file.
A missing file returns an error matching `os.ErrNotExist`; its parent directory
is not created. File locks are advisory and bind the opened inode. External file
replacement, unlinking, non-bbolt writers and filesystem/mount changes require
separate operational control. Possession of this handle does not prove that a
former executor stopped or that a fleet is quiescent.

Lock attempts use a positive, effectively nonblocking bbolt timeout and a
context-aware 10 ms retry delay. The total lock-acquisition ceiling is one second,
or the caller's earlier cancellation/deadline. An expired caller context is
returned directly; the fixed ceiling returns `bbolt/errors.ErrTimeout`. No background
open goroutine remains after cancellation. Filesystem calls, bbolt opening and
cursor operations are synchronous; this is not a hard real-time bound on a
stalled filesystem or scheduler.

## Recognized original formats

The namespace is frozen to the following exact top-level shapes:

| Version | Required buckets |
| --- | --- |
| `1` | `pending-v1`, `ready-v1`, `rpm-v1`, `leases-v1`, `rpm-counts-v1`, `active-counts-v1`, `limit-meta-v1` |
| `2` | All version 1 buckets plus `quota-entries-v2`, `quota-counters-v2`, `quota-expiry-v2`, `quota-accounts-v2`, `quota-invalid-bounds-v2` |

Missing buckets, additional buckets, shared-spool or mixed namespaces, and an
unknown version are rejected. The metadata bucket permits only `version`,
`installation`, `clock`, and, for version 2 only, `quota`. Nested metadata,
malformed or empty bindings, invalid clocks, unknown quota metadata fields,
invalid calendars, missing version 2 quota/clock metadata and a clock preceding
recorded coverage are rejected. None is repaired.

The header reports the actual format, optional recorded installation binding,
optional clock high-water value, optional recorded calendar/coverage start, exact
raw metadata entries, and each bucket's original sequence. Missing version 1
binding/clock and inactive quota metadata remain absent. A recorded coverage start
does not prove historical completeness. The interface does not manufacture a
coverage start, account birth, settlement, economic receipt, or process closure.

## Detached raw pages and bounds

`ReadPage` selects one recognized bucket and starts strictly after a raw key;
an empty key starts at its first entry. An absent cursor key uses the next greater
key. Returned bucket names, keys, values and continuation keys are detached copies;
mutating a header or page cannot change the transaction or later reads.

| Bound | Maximum |
| --- | --- |
| Raw header evidence | 64 KiB |
| Records per page | 512 |
| Raw bytes per page | 1 MiB |
| Individual key | 32 KiB |
| Individual value | 1 MiB |

Header raw bytes include metadata key/value bytes and each bucket name plus its
eight-byte sequence. Page raw bytes include the selected bucket name, its
eight-byte sequence and all returned key/value bytes. These are raw evidence
bounds, not JSON/base64 transport bounds, journal size limits, or a retained-memory
budget for a caller accumulating many pages.

A page stops before the next entry would exceed the caller's byte budget or after
its entry limit. Its `NextAfter` is the last returned raw key; `Exhausted` says only
that the selected bucket cursor reached its end in this retained transaction.
The caller must inspect every header-listed bucket separately and retain the
original sequence facts. Exhaustion is not proof of valid accounting or a complete
transfer.

An individual entry that exceeds a hard limit, or cannot fit into an empty page,
returns `ErrLocalInspectionBound`. Nested buckets encountered during pagination
return `ErrInvalid`. Cancellation and every error return no partial header/page;
values are never truncated. An error must be resolved before a caller can claim
that it collected even the complete raw bucket contents.

## Validation and corruption boundary

bbolt validates its database metadata during opening and its open errors are
preserved. Inspection validates the recognized namespace and bounded header shape.
It does not run an unbounded whole-file bbolt consistency scan or certify arbitrary
hostile/corrupt internal page structures. Recognized application-shape corruption
returns `ErrInvalid`; out-of-bound raw evidence returns `ErrLocalInspectionBound`.

Non-header values remain opaque. Even a structurally readable value that is not a
valid quota receipt is returned exactly for later semantic validation. Inspection
does not rederive counters or prove consistency between receipts, accounts,
leases, RPM events, indexes and pending/ready facts. Known and unknown holds,
invalid bound markers and active receipt states survive as their original bytes.
No shared receipt, replay permission, upstream-dispatch authorization, safe hold
release or transferable economic completeness follows from a successful read.

## Test source

The focused test source covers:

- Whole-file SHA-256 preservation across accepted and rejected inspection.
- Active original receipts remaining active, known/unknown token and money holds,
  unchanged pending facts, raw values and original bucket sequences.
- Detached metadata fields, bucket names, entries and continuation keys.
- Version 1 missing facts without initialization and a real shared-spool rejection.
- Missing files/parents, directories, symlinks, empty/non-bbolt files, mixed and
  unknown namespaces, missing buckets, and invalid header/calendar/clock shapes.
- Context cancellation while waiting for a writer, fixed lock timeout, exclusion
  of a writer for the handle's lifetime, and lock release after close/errors.
- Exclusive pagination, byte/record limits, oversized header/value rejection,
  nested bucket rejection without partial pages, and opaque invalid receipt bytes.

No compile, build, runtime drill or test execution is claimed here. A later
coordinated qualification must run the focused package tests and applicable
project gates against the final integrated source.
