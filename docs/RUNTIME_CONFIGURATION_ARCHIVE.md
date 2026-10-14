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
