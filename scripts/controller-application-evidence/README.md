# Controller application evidence

This maintained acceptance command belongs to the root Go module and is not
part of the production RouteX or Controller executable. It uses the existing
GORM/PostgreSQL/MySQL dependencies and performs no schema or business mutation.

It accepts no arguments and reads only private
`/fixture-evidence-input/config.json`. The coordinator supplies the controlled
database password, exact owner, replacement/predecessor Key, Model, Credential,
Connection and ProviderModel IDs, replacement birth, and exactly two distinct
request IDs ordered as native incomplete then completed. No network endpoint,
DSN, table, SQL or output path is configurable. Protected no-follow regular 0600
single-link input reads fail closed outside Linux.

An eight-second context and server-enforced read-only repeatable-read transaction
project those two Calls, each last Attempt and the replacement Key. Limit three
makes duplicate/ambiguous results fail closed. Exact comparisons check the explicitly selected
Personal or Project owner/Key/linkage, recorded birth and call timing, successful statuses, native
markers, Credential/Model/Connection/ProviderModel, canonical snapshot and expected
sole attempt ordinal. The query selects no Key hash, bearer, payload or secret.
Only projected immutable IDs, statuses and timestamps are emitted. Errors and
SQL diagnostics remain suppressed behind a fixed rejection result.

An absent `database_driver` preserves the fixed disposable Compose `postgres:5432`
endpoint; explicit `"postgres"` is equivalent. Explicit `"mysql"` selects only
`mysql:3306`. Both use fixed `routex` database/user. Explicit null, empty strings,
case aliases, mixed canonical/alias fields, duplicate keys and other values are
rejected. No host, DSN, user, database, port or connection options are accepted.
The controlled password must match the 38-character `token_urlsafe(28)` fixture
grammar and is consumed before opening the database. PostgreSQL retains its
two-second connect timeout. MySQL uses UTC timestamp decoding, utf8mb4 and
three-second connect/read/write timeouts; read-only automatic retry,
multistatements and parameter interpolation are disabled. GORM error translation
is enabled with silent logging and no automatic ping/version-discovery I/O.
One connection is allowed. Read-only transaction commit closes its snapshot;
it does not publish configuration or invoke a business write.

Qualification commands after the shared validation lane is available:

```sh
go test -race -count=1 ./scripts/controller-application-evidence
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o bin/controller-application-evidence ./scripts/controller-application-evidence
```

Use the pinned image's actual architecture. In a separately owned Linux/PostgreSQL
test namespace, supply the exact controlled request-pair fixture at private
`/fixture-evidence-test/config.json` and run:

```sh
go test -race -tags integration -count=1 ./scripts/controller-application-evidence -run '^TestPostgresEnforcesReadOnlyAndExactProjection$'
```

That test validates the exact projection, then requires PostgreSQL SQLSTATE
25006 for a deliberate zero-row write inside its read-only transaction. An
unrelated database failure does not satisfy write refusal. It must not run
against the developer's database. The runtime command itself contains no
migration, AutoMigrate, raw Exec, UPDATE, INSERT or DELETE.

A separately reviewed coordinator must pin the command source/binary, private
config, image and exact mounts/network, join its bounded process group and remove
its exact owned container. Passing unit tests or preparing this command does not
prove an actual Agent-installed application call or completed Key retirement.

## Personal and Project owner input

An absent `owner_kind` preserves the original Personal recipe. Explicit
`owner_kind: "personal"` is equivalent: `user_id` and both `key_` identities
are required, and Project-specific fields must be empty/zero.

Project evidence requires `owner_kind: "project"`, an empty `user_id`, canonical
`project_id` (`prj_`), `key_id` and `predecessor_id` (`pky_`), exact
`project_created_at`, `predecessor_created_at`, and `key_created_at` instants,
and the recorded replacement `key_creator_id` (`usr_`). Project birth must not
follow predecessor birth, and predecessor birth must not follow replacement birth.
The existing Model, Credential, Connection, ProviderModel and ordered exact request
pair remain required. No endpoint, DSN, table, SQL, mutable catalogue lookup or
manager Personal identity can select or replace Project ownership.

The fixed Project projection joins `project_api_keys`, its exact predecessor and
the recorded Project birth. Both Calls must have empty `user_id`, exact
`project_id`, and exact Project-owned replacement/predecessor rows. Creator
metadata is checked separately and grants no authority. Original immutable last
Attempt attribution and native/status/birth checks remain mandatory. A canonical
snapshot is checked per Attempt; the two Attempts need not share a snapshot.
The query uses portable GORM joins and predicates, without a dialect branch;
the existing PostgreSQL SQLSTATE test remains bound to its controlled PostgreSQL
namespace. Driver support alone does not establish MySQL runtime application
acceptance.

The same isolated read-only integration test selects the fixed owner-kind table
for its deliberate zero-row write and still requires exact SQLSTATE 25006. Supply
a fresh Project exact pair; do not relabel or reuse a Personal runtime receipt.
The original Personal pair and final post-retirement pair remain independent
immutable config/proof files. This extension requires its own source review,
unit/race and real isolated PostgreSQL qualification before any Project claim.

Input decoding rejects case aliases of `owner_kind` before struct decoding,
including null aliases and mixed canonical/alias fields. Connection validation
requires the bounded password only at input preparation. The actual run path
consumes that field before opening the database; fixed query and projection
validation retain every non-secret owner, birth, native and attribution check.
The SQLSTATE integration control uses the same secret-consumption transition.

## Explicit isolated MySQL refusal and unchanged-pair probe

The MySQL probe requires **both** `integration` and `routex_mysql_readonly` build
tags. It is not selected by ordinary tests or the maintained full integration
entry point. The existing PostgreSQL test name, tag and private input path remain
unchanged. Compile and run the MySQL test-side pure refusal/rollback controls
without reading any database input:

```sh
go test -race -tags 'integration routex_mysql_readonly' -count=1 ./scripts/controller-application-evidence -run '^TestMySQLReadOnlyPair(ErrorRequiresTyped1792AndExactState|RollbackAlwaysRunsAndUnknownNeverPasses)$'
```

In a separately owned fresh Linux/MySQL namespace, run as its trusted non-root
input owner. Stage the exact reviewed config at
`/fixture-evidence-mysql-test/config.json` with explicit `database_driver: "mysql"`.
The existing protected descriptor-relative reader requires regular mode 0600,
single link, no symlink, stable bytes/metadata and current effective UID ownership;
no alternate path or DSN is accepted. Supply the actual installed replacement
Key's two immutable native request IDs, exact Personal or Project ownership and
births. No fixture rows are seeded by this test.

```sh
go test -race -json -tags 'integration routex_mysql_readonly' -count=1 ./scripts/controller-application-evidence -run '^TestMySQLExactNativePairReadOnlyRefusalIntegration$'
```

Within a twelve-second context, the test begins a read-only repeatable-read
transaction, validates the exact two-row native projection, then attempts a
zero-match Key status update using the conjunction of the two distinct validated
Key IDs. It must receive an actual `*mysql.MySQLError` numbered 1792 with exact
SQLSTATE `25006`; string matches, unrelated database failures, unexpected write
success, cancellation and timeout all fail. Every post-BEGIN path attempts
rollback and an unknown rollback fails. A distinct fresh read-only transaction
must reproduce the same complete pair after confirmed rollback, and the pool
must close successfully before a bounded `mysql_pair_receipt` is emitted. The
receipt contains immutable IDs/native/status/birth facts, not secrets or error
payloads. GORM translation must preserve this otherwise unsupported typed error.

Run fresh original and post-retirement pairs separately for Personal and Project
acceptance. Retain original Go JSON event order and complete named/package
terminals: a long receipt may span multiple Output events and is not evidence
until safely reconstructed and checked against the exact trusted owner. Missing,
failed, cancelled, skipped or truncated readback never passes. This isolated
probe neither performs retirement nor proves delivery, browser behavior,
external workload adoption or full F29 completion. The coordinator still owns
fresh artifacts, source/image/input pins, exact process/resource closure and
actual installed-Key native application use.

## Separate one-call Messages evidence

`controller-application-evidence messages-once` is a separate source-authored
operation for one controlled real application's Anthropic Messages call. It does
not replace or weaken the no-argument Chat/two-row grammar or its existing
PostgreSQL/MySQL read-only-pair probes. No new default Task selection, production
route, service, database schema or external client artifact is supplied here.
Source authoring is not runtime qualification.

This operation reads only the protected native Linux file
`/fixture-evidence-input/messages-once.json` through the existing no-follow,
single-link, mode0600, stable descriptor reader, additionally requiring current
EUID ownership. It accepts no other arguments or path/network/DSN override.
The exact required common keys are:

```
operation, source_commit, run_id, database_password, owner_kind,
key_id, key_created_at, model_id, credential_id, connection_id,
provider_model_id, snapshot_id, run_started, as_of
```

`operation` must be `messages-once`; source/run are canonical lowercase40/32hex.
The explicit owner is `personal` with only `user_id`, or `project` with only
`project_id`, `project_created_at` and `key_creator_id`. Even an empty field from
the other owner grammar refuses. Optional canonical `database_driver` is absent
or `postgres`, or explicitly `mysql`. Explicit empty/null/case aliases, duplicate
or foreign fields refuse. Fixed connection endpoints, user/database, password
shape, driver timeouts/UTC/error translation and secret consumption are the
same maintained connection adapter used by the original pair command.

No `request_id`, `requests`, `protocol` or predecessor field is accepted. Protocol
is fixed to `anthropic_messages`. The caller independently captures actual owner,
fresh installed Key identity/birth and Model/Credential/Connection/ProviderModel
identities. It must supply the exact expected published `cfg_` snapshot from
actual source-bound configuration publication, not discover or substitute it
from the current mutable catalogue or from whichever Attempt happens to return.
Both Call and Attempt snapshots must equal that expected snapshot. Source/run
are caller bindings, not facts SQL can authenticate; the coordinator separately
owns exact current application/helper build/source, image, input and original
process proofs.

`run_started`/`as_of` form a bounded observation interval of at most15minutes.
The Key birth must fall within it, every Call must start after or at that exact
birth, and complete by `as_of`. Project identity/birth and creator metadata are
checked separately; creator attribution grants no authority.

The fixed GORM projection selects ALL retained Calls for this exact fresh Key
and ALL joined Attempts with LEFT joins and limit2. It does not filter success,
protocol, expected RequestID, time or owner before counting, and does not select
only the last Attempt. Exactly one total row is required. A missing Attempt,
second Call, extra/retried Attempt, ordinal other than1, missing/foreign owner,
case-collated ID alias, changed birth, failed status, noncompleted native marker,
other protocol or attribution/snapshot mismatch refuses. With exactly one sole
successful native-completed Attempt, it is also the last Attempt. No Key hashes,
bearers, questions, response bodies or other content columns are selected.

One eight-second read-only RepeatableRead context bounds the work. The snapshot
always rolls back; query, validation, rollback, context, pool-close or output
failure cannot produce a successful command terminal. Pool closure precedes
output. `database/sql.Close` has no context parameter, so the original process
supervisor must still independently bound total wall time and prove closure.
Errors remain only the fixed `evidence_rejected` result with exit1.

Successful output is a separate object with result
`verified_exact_durable_messages_once_evidence`, operation/source/run,
`request_id_source: "exact_bounded_sql_row"`,
`client_request_id_displayed: false`, and one `proof` object. That object contains
the exact SQL-derived RequestID plus immutable Call/sole Attempt/owner/Key/birth/
Credential/snapshot/protocol/status/time facts. The request identity comes ONLY
from that bounded exact row. It does not assert that the real application
printed or displayed that ID. Official client exit/output, actual installed-Key
use, native parser completion, peer call count and owned process/resource closure
remain distinct source-bound witnesses; Agent delivery alone proves none of
application adoption, retirement or external workload completion.

Source tests cover the disjoint canonical grammar, separate Personal and
Project owners, exact native/identity/birth/publication facts, ambiguity and
extra retries, post-consumption fixed connections, query bounds without hidden
filters, and failure before safe output after unknown closure/context.
Current-host race qualification passes 177 evidence and 55 fixture named nodes;
fresh Linux arm64 controls pass 185 evidence and 90 fixture nodes without race
instrumentation or skips. Mandatory checking passes separately.

Fresh R4 real official Claude Personal installed-Key application use passes the
maintained command against PostgreSQL: exactly one Call/sole Attempt, native
completed, immutable attribution and independently captured expected publication.
The client itself still reports native unknown and no displayed Request ID.
Recorded originals, resources and both ports close. Independent readback SHA-256:
`55339956a5a9bdf0ffbd0bf7cca78fc439fce7fa97190c7bb8a96e03f23e1e3b`.
This does not qualify the new mode on MySQL, Project runtime use, replacement or
retirement, read-only refusal pairing, external adoption or global F29 completion.
Earlier failed generations remain failed and are preserved.
