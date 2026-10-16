# Durable shared admission owner foundation

This source foundation adds dormant installation/mode metadata and durable spool
owner receipts. It does not enable shared admission. No runtime startup path,
public API, takeover, mode transition, transfer, pruning or executor-closure API
is included. Existing local quota behavior remains authoritative.

## Installation and process identity

The installation identity is the initialized `limit_installation[1].journal_id`
used by the existing local accounting journal. It is not the setup singleton or
a runtime snapshot ID. V118 creates `admission_modes` and
`admission_spool_owners` using private frozen GORM models. A mode row is seeded
as `local`, revision `1`, only when the exact canonical journal identity is
already initialized. A fresh or uninitialized journal receives no mode row.
Initializing the local journal later does not activate this unwired foundation.
A future reviewed cutover must reconcile that state explicitly.

A real process incarnation is the exact `system_instances.id` and `started_at`
pair. Registration and current-owner checks lock and validate that stored pair.
Heartbeat, lease expiry, stopped and retired metadata are not executor/transport
closure evidence. Owner history does not prevent process metadata retirement or
deletion; a missing or reborn current process fails the current-owner guard.
Historical receipts still retain their original process birth.

The private `currentSystemProcessIdentity` accessor copies the local process ID
and exact microsecond birth together under the lifecycle read lock. Missing or
malformed facts return no identity; UTC normalization preserves the recorded
instant without rounding. The detached pair is only local identity metadata.
SQL owner registration and current-owner checks remain mandatory, and neither
this accessor nor lease metadata proves executor or transport closure. It adds
no startup registration, owner activation or change to existing ID attribution.

## Exact durable operations

`admissionOwnerStore` is private and is not an economic authority. New
registration requires the exact initialized installation, current shared mode
revision and current process birth. Shared mode appears only in controlled test
fixtures; this slice provides no production activation operation.

Each installation/spool slot accepts one lowercase UUIDv4 registration with its
exact installation, positive SQL-representable mode revision, canonical spool
identity, process ID/birth and original fence `1`. A different UUID never takes
over that slot, even after lease expiry or terminal fencing. Alias identifiers,
changed bodies, foreign births and unsigned-to-signed revision overflow fail
closed. Unique constraints retain the registration and optional fencing UUIDs.

One exact terminal fencing UUID changes the current fence from `1` to `2`.
An identical explicit retry returns the original saved fencing receipt. Another
fencing intent cannot advance or reactivate the owner. Fencing neither closes
an executor nor releases any quota, rate, concurrency or unknown economic hold.

Exact registration retries return the original immutable receipt even after
fencing, mode/revision changes or process rebirth/deletion. Exact fencing retries
likewise return their original receipt after current mode/birth changes. They
require the retained installation/mode parent rows, which owner foreign keys
protect from deletion. They do not require those parents to match the original
current mode. A historical receipt is not proof of current owner authority.
`CheckCurrentOwner` separately checks the current initialized installation,
shared revision, active fence and exact existing process birth inside the future
caller's transaction. It does not validate a Key, Session, grant, policy or call.

## Transactions and incomplete outcomes

Registration/fencing use read-committed GORM transactions. The private lock order
is `LimitInstallation -> AdmissionMode -> AdmissionSpoolOwner -> SystemInstance`
where a process lookup is needed. Existing process startup/retirement/closure
writers take governance before process rows, and heartbeat updates process rows
without taking owner locks. Existing journal binding takes the installation row
without taking process locks. A future combined transaction must take governance
before this private order, then independently intersect every current
principal/grant/policy mutation barrier. This foundation does not establish that
combined authorization or economic authority.

Cancellation and storage/commit failures return errors without success receipts.
An uncertain commit may have persisted; only an identical explicit retry can
read its saved original intent. Missing reads do not establish executor closure.
The tests discard an acknowledgement and retry to check durable idempotency;
that control does not manufacture proof of a specific transport/SQL commit fault.

V118 repairs missing columns only in empty partially created tables, refuses
nonempty incomplete tables and validates scalar/index shapes. It reconciles
constraints through the existing database-layer GORM helpers. Released V1–V117
migration bytes remain unchanged.

## Qualification and next boundary

This candidate is source-only until its focused tests and both real databases
run. Authored pure tests cover identity bounds, independent historical receipts,
malformed terminal state and error classification. The separately gated
`TestAdmissionOwnerStoreIntegration` requires fresh exclusive PostgreSQL and
MySQL `routex_test` databases, `ROUTEX_TEST_ADMISSION_OWNERS=1`, and the existing
`ROUTEX_TEST_POSTGRES_DSN` / `ROUTEX_TEST_MYSQL_DSN` inputs. It covers empty and
initialized upgrade, repeat/concurrent migration, bounded partial-DDL repair,
constraints, exact registration/retry, competing registration, terminal fencing,
cancellation and current-versus-historical process/mode facts. It is not yet
registered in the maintained integration supervisor and must not share its
schema with other workers or applications.

Before full V118 qualification, current-head fixture guards and historical
schema-peeling helpers need a separately reviewed additive suffix update.
A future local-to-shared cutover still requires explicit quiescence of every
pending local request, exact account/hold transfer and durable shared request
receipts. Ordinary local journal opening mutates recovery state and cannot serve
as a read-only transfer proof. Until those mechanisms and current four-principal
SQL authorization are implemented and qualified, multi-node economic enforcement
and owner takeover remain unavailable.
