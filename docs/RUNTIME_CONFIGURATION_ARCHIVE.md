# Gateway configuration archive foundation

The private archive stores complete canonical codec-v1 source facts in encrypted,
ordered chunks. It is a durable storage foundation for reconstructable Gateway
versions. It creates no automatic durable capture, serving authority, source-content API,
activation command, rollback operation, fleet acknowledgement or automatic
retention policy. F30 remains partial.

## Storage contract

Frozen GORM migration V112 adds an immutable version header and chunk table.
Released migrations remain unchanged. Each stable `rcv` identity has an exact
microsecond archive birth, codec version, full content digest, source and
independent authorization/network/pricing observation digests, original byte
length and exact chunk count. An archive birth describes storage identity; it
is not a historical actor, command or installed-process receipt.

The canonical document retains its 16 MiB bound and splits into at most 512
32 KiB chunks, including byte boundaries inside UTF-8 sequences. Chunk IDs
encode their exact version and ordinal. A domain-separated authenticated tuple
binds every immutable header field, chunk identity, order, original length and
plaintext digest. The current credential keyring encrypts each chunk under that
exact reference. No plaintext, verifier hash, source document or envelope is
exposed through entity JSON serialization or sanitized errors.

One GORM transaction retains the complete header and all chunks. Preparation
captures the current root epoch; every envelope uses the existing singleton
commit fence. A root cutover rejects stale preparation atomically. Repeating the
same identity, birth and content verifies the complete retained archive without
rewriting its immutable fields. A different intent at the same identity fails.
A failed or uncertain write is not a positive storage or application receipt;
an explicit identical retry may reconcile the retained complete source.

## Read-only header metadata

`GET /api/v1/admin/runtime/archives` exposes a bounded header-only directory under
current `system.read` authority. The service rechecks the exact enabled, admitted
actor and effective permission in a read-only repeatable-read transaction.
Canonical actor-bound cursors page by descending archive ID, with a default of
20 and a maximum of 100 rows. This order is not archive creation-time order.

The closed `archive_metadata_only` response contains each exact archive ID,
archive creation time, codec version, full content digest and original document
byte count. The latter is neither persisted encrypted bytes nor a retention
budget. No chunks, ciphertext, canonical source, secrets or current catalog
resources are read or returned. Source/authorization/network/pricing digests are
used only to validate retained headers and are not returned.
Header validation does not authenticate encrypted content, validate chunk
completeness or establish coherent capture, publication, installation, fleet
application or rollback readiness. Archive birth is storage identity, not an
original actor or application receipt. Reads perform no write or activation.

The System Status jobs card opens the existing-style local dialog and metadata
table. Fresh actor, Session, permission and jobs generations gate dispatch,
visibility, pagination, refresh and late completion. Exact read generations reject
retained callbacks after an identical same-page refresh. Failed or renewed reads
hide prior rows; closing destroys the query lifetime. English/Chinese labels and
date/number formatting switch without refetching metadata. There is no automatic
read until the action is explicitly opened.

Normal runtime capture is still private memory only and no production caller
writes archives automatically. An empty directory is therefore expected; it does
not mean that no configuration was ever published or installed. Routing and
installation observation histories remain separate. This endpoint deliberately
works without a configured envelope opener and never claims source readability.

## Cold reconstruction and rotation

Cold reads use one repeatable-read transaction, validate the bounded header and
exact ordered chunk set, authenticate each complete tuple, check chunk and whole
content digests and invoke the strict codec decoder. Missing, extra, duplicate,
reordered, substituted, oversized, unknown-codec or cancelled data returns one
generic failure with no partial facts. There is no network access, historical
secret recovery or missing-field lookup from current mutable source rows.

The returned facts remain non-authorizing. Historical revoked, stale, dangling
and unavailable references stay historical; no Key, actor, Session, Credential,
grant or route becomes eligible through reconstruction. Pricing remains an
independent observation and does not become a rollback target.

Root inventory V11 appends `runtime_config_chunks`; V1–V10 keep their exact
historical domain prefixes. Bounded inventory reads derive references from the
retained immutable header/chunk tuple. Envelope rewrap updates ciphertext only,
with exact subject/generation/original-envelope compare-and-swap. Header content
and codec identity stay unchanged. Current rotation and retirement proof require
the new inventory; old terminal histories do not gain archive coverage. Rewrap
must preserve cold readability after the old root is removed.

## Confirmed retention policy

The initial durable-capture policy is at most 100 retained versions and at most
256 MiB of actual persisted encrypted content. Both limits apply together.
Byte accounting uses the stored encrypted envelopes, including their encoding,
rather than plaintext size or an estimated encryption overhead. Exactly identical
canonical content is deduplicated; source-publication provenance must remain
separate from content identity and current installation status.

Reaching either limit pauses new archive writes. It does not delete history,
block current routing or delay fresh authorization and revocation publication.
Cleanup requires an administrator's explicit preview and confirmation; no
scheduled deletion is enabled. Root-envelope rewrap must preserve correct byte
accounting because envelope sizes can change. The durable writer, capacity
ledger and cleanup workflow are not implemented by this policy decision.

## Validation and remaining work

Source qualification covers frozen schema/FK/index direction and portability,
exact additive inventory guards, chunk boundaries, full tuple substitutions,
bounded cold reconstruction, cancellation, private serialization and old-root
removal after rewrap. Focused archive race tests, paired translations and the
mandatory project checks have passed. The maintained sequential archive worker
has passed real PostgreSQL/MySQL migration, persistence, commit-failure,
concurrency, root-epoch and cold-process qualification. The complete maintained
identity matrix has passed on PostgreSQL and MySQL, and its owned processes,
databases, ports and private helpers have been cleaned up.

Historical integration fixtures peel V112 only after proving exact retained
receipts, empty archive tables and compatibility with literal V111 root guards.
Retained archives or V11 root observations refuse the peel before schema writes;
older report helpers cannot consume the archive receipt independently. This
mechanism exists only in the disposable test harness and is not a product
downgrade operation.

The [private historical capture seam](RUNTIME_CONFIGURATION_CAPTURE.md) now
prepares complete original-source facts in memory after successful publication,
without an archive write or serving authority. Later phases must establish
durable capture provenance, bounded retained
bytes/counts, deduplication and backpressure without delaying fresh denial
publication. Current runtime can install fresh authorization while retaining old
routes after route validation failure; that mixed state is not a complete
captured version. Reviewed current-authority rollback and fleet acknowledgement
remain separate work. This foundation stores nothing automatically.

## Administrator storage cleanup contract

The cleanup slice is under implementation and has not completed qualification.
It belongs in the existing archive-history dialog. An intrinsic administrator
must have current `system.read` and `system.write`, an exact current Session,
a bounded selection of 1–20 archive IDs, a server-issued strong preview ETag,
a reason and a stable UUIDv4 request identity.

Preview reports retained count and actual encrypted-envelope bytes, selected
count and bytes, and both fixed caps. It reads no source or secret material.
The ETag binds the exact actor birth, selected immutable headers, chunk count
and actual bytes, retention ledger, and canonical content targets. Any concurrent
archive admission or byte-accounting change requires a new unsent review.

Cleanup serializes with the archive writer and root-envelope rewrap. Header,
chunks, provenance, actual-byte accounting, immutable original receipt and audit
commit atomically. The audit targets the first canonical selected archive ID;
its details retain the complete selection, UUID request, reason and released
encrypted bytes. The UUID is not stored in the audit's bounded resource-ID field.
If a selected version owns a canonical digest mapping, a
remaining duplicate becomes the target only after authenticated reconstruction
and exact canonical-byte comparison. Legacy provenance is never inferred.
No automatic deletion is introduced. Future activation or rollback references
must establish explicit retention protection before referencing archives.

A failed or unknown acknowledgement is not success. An explicit identical retry
reauthorizes the current administrator and returns only the exact retained receipt
without repeating deletion. The UI retains dispatched intent in mounted local
state, rejects obsolete read/actor responses and never infers deletion from a
missing row. Same-actor Session, permission, jobs or list renewal hides prior
facts and releases obsolete pending requests while retaining the exact dispatched
intent. Explicit replay waits for fresh authority and a successful current list
read, then uses current CSRF with the original UUID, selection, reason and ETag.
Actor change, dismissal or parent unmount clears that local retry state. Reasons
are validated before capture as nonblank, trimmed Unicode text of at most 1,000
characters without control characters. Cleanup releases archive storage only and
changes no current route or runtime installation.

## Cleanup proposal qualification checkpoint

The isolated V115 proposal has authored real-driver migration, partial-DDL,
retained-intent refusal, transactional audit rollback, lost-acknowledgement
replay and authenticated legacy-content rebind controls. These controls are
registered inside the maintained exclusive archive worker. The first two
dual-driver runs failed and remain retained. The first exposed a fixture reset
that left successor migration receipts; the second exposed an oversized audit
resource identity that rolled back successful cleanup on both databases. The
isolated successor corrects both, retains exact audit-intent assertions, and
normalizes PostgreSQL timestamp locations before full-field receipt comparison.
Its real-driver qualification remains pending.
Source review identified a lossy unpaired-Unicode decoder path; it is corrected
in the proposal with paired/unpaired escape regressions. The corrected database,
service and handler race focus passed all 22 named tests. The repaired frontend
and localization focus passed all 63 cases; mandatory and browser acceptance
remain open. These source results are not complete integration acceptance.

The direct three-file frontend run passed all five new cleanup component cases
but failed one API header fixture and twelve existing archive-page cases. Axios
normalizes header whitespace before the decoder receives it; the inappropriate
newline fixture is replaced with a noncanonical uppercase digest case. The
existing page first failed at its unchanged five-second timeout, followed by
overlapping React act calls. Its historical cause remains unproven. The earlier
accidental full frontend launch was terminated and is not acceptance. No V115
mandatory, backend, database, browser or main-composition pass is claimed.

The repaired three-file frontend run now passes all 54 cases with unchanged
five-second test limits, one DOM worker and a 512 MiB Node heap. All twelve
sealed repair paths and modes remained unchanged after the run, and its owned
process group is gone. Same-actor Session, permission, Jobs and list renewal
preserves only the exact dispatched cleanup intent; renewed authority hides
prior facts and permits explicit identical retry with fresh CSRF. Actor change,
dismissal and parent unmount discard it. Reasons validate before capture.
Earlier failed and interrupted frontend runs remain retained separately.
Backend source now rechecks Session expiry before returning a preview and before
committing cleanup or returning a saved receipt. Controlled expiry during the
preview and audit stages must roll back the transaction and return no private
review or receipt. Persisted reason/selection payloads have frozen GORM bounds.
Registered anonymous-route and malformed-wire regression controls are also
authored. These backend changes and their real-driver controls remain unqualified.

## Authenticated integrity review

`GET /api/v1/admin/runtime/archives/:archive_id/validation` requires an exact
current intrinsic administrator Session and independent `system.read`. It is a
read-only operation; `system.write`, CSRF and a reason are not required. Query
parameters are rejected. One three-second repeatable-read transaction rechecks
current admission, actor/Session birth, primary authentication policy and read
permission before decoding and before returning.

The existing bounded cold reader authenticates every retained chunk and the
complete canonical document using current available envelope roots. Missing or
corrupt chunks, unavailable roots and cancellation return sanitized failure with
no partial success. The closed response contains only scope
`archive_integrity_only`, archive identity/birth, codec version, observation time
and `integrity: authenticated`. No source facts, secret-derived digests, ciphertext
or decrypted content leave the server. No network or historical secret recovery
is performed. This result proves one successful cold read, not future readability,
rollback eligibility, current serving configuration, command completion, application
adoption or fleet acknowledgement. It creates no activation reference, audit
mutation, retention reservation or publication. It issues no mutation ETag.

The existing archive-history table offers an explicit Validate integrity action
for current administrators with read authority, independently of cleanup/write
authority. Results and errors remain transient and are cleared by refresh or any
authority/target generation change. Obsolete replies cannot restore them. No
automatic validation or retry runs. English/Chinese copy uses local primitives
and preserves the existing table/dialog layout.

Focused frontend tests and TypeScript checks have passed for this isolated successor.
Backend compiler tests, real PostgreSQL/MySQL, coherent project gates and browser
qualification remain pending. Validation does not implement activation or rollback.
