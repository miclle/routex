# Local Recorder Startup Isolation

Status: source-only candidate. Compilation, full checks, and real PostgreSQL/MySQL
execution remain pending. This boundary does not activate shared admission,
qualify economic schema migration, prove live cutover, or provide ongoing runtime
mode fencing.

## Explicit mode before recovery

`StartCallRecorder` must validate the persisted installation and admission mode
before opening or recovering a local journal. Shared mode, missing mode for an
initialized installation, foreign identities, contradictory initialization,
retained shared owners, and unreadable authority fail without a local fallback.

Startup follows governance, installation, then mode lock ordering in a
READ COMMITTED transaction. A second transaction reacquires these locks and keeps
them across the file validation, recovery, binding, and SQL acknowledgement.
Owner registration uses installation-before-mode ordering. No new process lease,
system-instance registration, or implicit stale-process takeover is required.
One successful startup check cannot secure a later live mode change; coordinated
cutover and subsequent dispatch fencing remain separate work.

## Fresh installation and interrupted binding

The first transaction commits only the immutable journal identity. It never
inserts a mode beside an uninitialized binding, preserving the frozen V118
invariant. Missing mode may reconcile only when the installation is uninitialized,
quota accounting has never started, no call or shared-owner history exists, and
the file is absent or an exact-ID, structurally recognized empty format1 journal.
An installation without an assigned identity additionally requires an absent file.

The second transaction opens and binds that exact file, then atomically records
the local mode and SQL initialization acknowledgement. A crash after file binding
but before acknowledgement retries the precommitted identity; matching identity
alone does not prove an empty history. Unexpected unbound or unrecognized existing
files require explicit investigation and are never silently adopted or erased.

`eventqueue.OpenBoundLocal` validates the file identity and mode before any
recovery mutation on the same exclusive file handle. Fresh binding is part of its
durable transaction. Established missing paths create neither a file nor parent
directories. Its checked `OpenFile` retains the original regular nonempty inode,
removes creation/truncation flags for an existing path, and uses exclusive creation
for a fresh path. This prevents bbolt from initializing a vanished, replaced, or
preexisting empty file before the journal validator runs. Existing paths never
recreate parent directories. Cancellation and failed SQL acknowledgement close
acquired handles. Filesystem writers must respect the exclusive journal lock;
this boundary does not secure the storage directory against arbitrary external
mutation while a process holds its file handle.
The original `eventqueue.Open` API and ordinary local recovery behavior remain
available to existing package callers through the mechanically extracted helper.

## Qualification

Focused package tests cover exact fresh retry, established pending recovery,
foreign files, nonempty or accounting-enabled uninitialized files, removed-history
sequence markers, unknown buckets, shared files, missing established paths, and
cancelled file-lock acquisition and acquired-handle closure. Deterministic private
test seams cover disappearing paths/parents, zero-byte and foreign replacements,
preexisting zero-byte files, and absent-path collisions without invoking hooks in
production. Service tests cover coherent mode predicates and
unreadable startup authority without filesystem creation.

The dedicated `TestLocalRecorderStartupSQLIntegration` worker requires
`ROUTEX_TEST_LOCAL_RECORDER_STARTUP=1` and
`ROUTEX_TEST_LOCAL_RECORDER_STARTUP_DRIVER=postgres` or `mysql`, plus the existing
isolated `routex_test` driver DSN. Allocate a fresh, exclusively owned database for
each run. The worker rejects an existing migration registry, has a 90-second
context bound, and runs only existing migrations. It verifies identity-commit
retry, file-bind-before-SQL-ack recovery, ordinary local restart, byte-identical
mode refusals, same-ID nonempty fresh refusal, absent established paths, cancelled
SQL locking, accounting-history refusal, unreadable SQL authority, and unchanged
frozen migration history. No test-only economic tables
or new migration versions are introduced by this slice.
