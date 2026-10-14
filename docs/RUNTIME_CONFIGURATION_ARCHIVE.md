# Gateway configuration archive foundation

The private archive stores complete canonical codec-v1 source facts in encrypted,
ordered chunks. It is a durable storage foundation for reconstructable Gateway
versions. It creates no capture pipeline, serving authority, public archive API,
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

Later phases must establish coherent original-source capture, bounded retained
bytes/counts, deduplication and backpressure without delaying fresh denial
publication. Current runtime can install fresh authorization while retaining old
routes after route validation failure; that mixed state is not a complete
captured version. Reviewed current-authority rollback and fleet acknowledgement
remain separate work. This foundation stores nothing automatically.
