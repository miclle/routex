# Database Portability and Migration Policy

This is a mandatory engineering policy. RouteX supports PostgreSQL and MySQL through GORM. Business services should express domain behavior without knowing which database is active.

## GORM first

Use GORM model tags, query builders, transactions, and Migrator APIs for ordinary persistence and schema changes. GORM provides portable table, column, index, and constraint operations. Its `TranslateError` option maps supported driver failures to errors such as `gorm.ErrDuplicatedKey`. See the official [migration documentation](https://gorm.io/docs/migration.html) and [error handling documentation](https://gorm.io/docs/error_handling.html).

Driver imports, dialect switches, vendor-specific locking, collation compatibility, and any missing error adapters belong in `internal/routex/database/`. Handlers, services, and entities must not inspect SQLSTATE or driver error numbers. Portable, parameterized query expressions are permitted within GORM queries; this policy does not prohibit ordinary joins or predicates.

## Versioned schema changes

A migration version is an immutable function over a ready GORM database handle. Its private schema structs represent that version permanently and declare explicit table names, sizes, precision, indexes, and relationships. Never alias a frozen schema to a current business entity: changing the entity would silently change historical upgrades.

Prefer targeted `Migrator` operations. The `migrateTables` helper creates only the models explicitly owned by a version and reconciles their declared indexes and constraints on retry. It does not recursively evolve relation targets or change existing columns. A relation to an existing table may use a private primary-key reference struct, which must not be passed as a table to create. Later changes to existing columns require explicit new versions using `AddColumn`, `RenameColumn`, or other appropriate Migrator APIs.

`AutoMigrate` can be appropriate for a deliberately bounded, frozen schema; it is not the application startup migration strategy. It neither infers intended renames nor deletes unused columns. Numbered history, data backfills, compatibility transitions, and release decisions remain application responsibilities.

MySQL can commit DDL implicitly. Design retryable steps and advance the migration ledger only after a complete version succeeds. Both supported databases must pass fresh installation, upgrades preserving data, repeated execution, concurrent startup, and constraint/index acceptance. Existing version 1–4 SQL is preserved unchanged because it has shipped; new migrations follow this policy.

## Raw SQL exceptions

An exception requires a GORM capability gap or measured performance need, a local rationale, parameterized inputs, and tests for each supported driver. Do not create parallel PostgreSQL/MySQL implementations of ordinary CRUD or table definitions.

Current exceptions include byte-exact authority comparison through the database-layer GORM expressions described below, and the database-level migration locks (`pg_advisory_lock` and `GET_LOCK`) and their release operations. GORM does not expose a portable connection-scoped advisory lock. These operations are contained in the database package, run on the same dedicated connection, and have concurrent-startup tests on both databases. Failed unlocks discard the physical connection to prevent a locked session from returning to the pool. Historical released DDL retains its existing dialect-specific collation and schema behavior for upgrade compatibility.


## Credential replacement preparation (version 31)

Version 31 adds a nullable historical predecessor ID to `provider_credentials`
and a dedicated `credential_replacement_receipts` table. The frozen production
schema uses GORM column/index/table operations, with no new
handwritten SQL or driver branch. The upgrade fixture uses two fixed allowlisted
PostgreSQL index-removal statements because the pinned GORM PostgreSQL Migrator
generates invalid `DROP INDEX CURRENT_SCHEMA().name` syntax; MySQL retains
`Migrator.DropIndex`. This exception is test-only and verifies index removal before
partial-DDL recovery; production and released steps are unchanged.
Lineage and receipt identities intentionally have no live foreign keys to
Credential rows: source deletion must preserve the replacement's historical
lineage, and result deletion must preserve creation deduplication.

The UUIDv4 request ID is the receipt primary key; result Credential IDs are unique,
while predecessor indexes are nonunique. Different named preparation intents may
share a predecessor. Receipts contain only actor/source/Connection/result IDs and
non-secret intent hashes. Secret comparison uses the original encrypted result
inside the authorized service, never a persisted unkeyed secret digest.

Real PostgreSQL/MySQL acceptance covers version-30 upgrades and old-row
preservation, empty and repeated migration, concurrent startup, interrupted
DDL/index reconciliation, receipt uniqueness and deletion durability. Do not
edit earlier released schema steps or use current entities as frozen definitions.

## Attempt Credential attribution (version 32)

Version 32 adds `call_attempts.credential_id` and `snapshot_id`, each a non-null
`VARCHAR(30)` with an empty default. The frozen additive schema uses targeted
GORM column and index operations; it does not evolve current business entities.
The historical lookup index orders Credential, exact published snapshot,
completion time and attempt ID. No live Credential or publication foreign key is
added, and old rows retain unknown attribution without a backfill.

Acceptance includes V31 data preservation, empty/repeat/concurrent migration,
independently missing columns and index repair, portable schema constraints,
immutable RecordCall delivery/replay and public DTO omission on both databases.
The index-removal fault injection uses the same bounded PostgreSQL test-only
Migrator workaround documented above. A parameterized PostgreSQL catalog query
verifies physical index-key order because the pinned GORM `GetIndexes` query does
not retain ordinal order; MySQL uses GORM introspection. Both exceptions are
test-only; production migration remains GORM-only.
The complete PostgreSQL/MySQL matrix passed under race detection (Handler
410.182 seconds, Service 5.252 seconds), including the new migration and
attribution lifecycle; owned Compose resources were removed.

## Native completion evidence (version 33)

Frozen version 33 adds `call_attempts.native_completion_evidence` as a non-null
`VARCHAR(20)` defaulting to `unknown`. Targeted GORM column/constraint operations
use a standard bounded IN check for unknown/completed/handoff/blocked/incomplete.
Services enforce the exact case-sensitive enum independently of supported database
collations. Existing attempt IDs, Credential/publication attribution and indexes
remain intact; old success rows remain unknown. No live foreign key or new index
is required.

Acceptance covers empty creation, existing-data upgrades, repeat/concurrent
startup, partially applied column/check repair, portable constraints, immutable
RecordCall/replay and DTO omission on both databases. Final-source focused
acceptance passed in 140.280 seconds; the complete matrix passed under race
detection (Handler 415.087 seconds, Service 5.164 seconds). Owned Compose resources
were removed. Earlier V32 evidence remains separate.


## Credential retirement receipts (version 34)

Frozen version 34 creates `credential_retirement_receipts` through GORM's bounded
frozen-schema migration. Its only key is the request UUID primary key; no live
Credential, actor, Connection, snapshot or attempt foreign key is added. Required
bounded historical identity fields, non-secret intent/validator hashes and a UTC
microsecond commit timestamp remain independently readable after live records are
deleted. A portable CHECK requires distinct predecessor and successor IDs. There
is no source uniqueness constraint: a deliberately re-enabled source may later
have a fresh reviewed retirement intent. No production handwritten SQL is needed.

Empty-database creation, V33 upgrades, repeat/concurrent startup, interrupted
constraint/ledger repair, primary-key uniqueness, required columns, distinct IDs
and historical receipt preservation are covered by the new migration fixtures.
Actual PostgreSQL/MySQL acceptance passed, including the complete race matrix
(Handler 449.057 seconds, Service 5.332 seconds). Controlled receipt replay,
concurrent intents, typed-audit rollback, deleted proof preservation, independent
single-connection restart, re-enabled predecessor protection and bounded
publication-pin lock waits passed. Owned Compose resources were removed.


## Exact authority identifiers

GORM equality follows the database column collation. MySQL default collations
can equate distinct case, accents or trailing spaces, which is unsuitable for
recipient and resource authority predicates used before pagination, counts and
updates. The database-layer `ExactText` and `ExactTextColumns` adapters use
parameterized GORM expressions with quoted server-owned columns. PostgreSQL uses
ordinary equality; MySQL uses binary casts for byte-exact comparison. GORM has
no portable collation-independent equality operator, so this bounded expression
is a justified database-layer exception. Services never branch on drivers.
Values remain bound independently; exact Go identity checks remain a second
guard. Dry-run parameterization tests pass. Actual PostgreSQL/MySQL acceptance
covers recipient, scope, observation join, role assignment and permission aliases
before list limits, counts and read updates; the focused race run passed in
173.349 seconds.

## Monthly quota observation and inbox (version 35)

Two separate frozen GORM tables retain immutable current-month observations and
recipient projections. Scope/dimension/month/policy/currency uniqueness prevents
reconciliation from duplicating a notice; observation/recipient uniqueness
preserves read state. Existing operational notification foreign keys and released
migrations remain unchanged. Neither table has a live identity, resource or
observation foreign key: historical observations and recipients survive resource
removal, and authorized reads require an exact observation join.

Actual PostgreSQL/MySQL focused acceptance passed under race detection in
173.349 seconds, including empty creation, V34 upgrade preserving data, repeat
and concurrent startup, interrupted DDL repair, required columns, six-field
uniqueness, recipient uniqueness, indexes and historical preservation. The fixture
uses three fixed allowlisted PostgreSQL index-removal statements because the
pinned GORM Migrator generates invalid CURRENT_SCHEMA syntax; MySQL uses
Migrator.DropIndex. This is test-only; production V35 remains GORM-only.
The complete PostgreSQL/MySQL race matrix also passed (Handler 499.925 seconds,
Service 5.851 seconds); its owned containers/network were removed.


## Project monthly quota request history (version 36)

Frozen additive V36 extends the released Project request table with an explicit
kind, baseline policy revision, original decision review validator, approved
policy revision, decision intent hash and immutable normalized approved policy.
Historical rows default to MODEL_ACCESS and preserve existing model arrays and
terminal decisions. QUOTA approvals must retain their review/revision/hash/policy
evidence. No live foreign key or table rename is introduced.

Targeted GORM column and check operations repair partially applied DDL without
rewriting old versions. The approved policy TEXT column uses an expression empty
default through its GORM tag, which is portable to MySQL and PostgreSQL. Explicit column tags bind the new ETag fields to the exact names used by the
constraints, independently of GORM acronym splitting. Actual PostgreSQL/MySQL
empty creation, V35 upgrades, repeat/concurrent startup, six individual missing
column repairs, constraint-only recovery, indexes, uniqueness and non-null/kind/
approval constraints have passed. Historical model and quota timestamps are
compared to their pre-migration persisted values, preserving the released MySQL
DATETIME precision without inferring a precision upgrade. Combined migration/model/quota/monthly-notification focused acceptance passed in
184.141 seconds. The complete PostgreSQL/MySQL race matrix passed (Handler
504.548 seconds, Service 5.267 seconds), and owned resources were removed. Native
quota enforcement and historical receipt replay are covered separately by the
lifecycle and production browser evidence in PROJECT_REQUESTS.md.


## Project request-rate constraints (version 37)

Frozen GORM V37 adds no columns and preserves released V1–V36. Its private schema
installs the RATE_LIMIT immutable approval evidence check, then a separately named
MODEL_ACCESS/QUOTA/RATE_LIMIT kind check, then removes the old two-kind check.
The existing quota approval check remains. Every partial DDL prefix keeps the old
conservative kind guard until both new checks exist; reruns repair either missing
new check without reinstalling an incompatible historical guard.

Real PostgreSQL/MySQL fixtures reconstruct released V36 checks, retain complete
historical rows and persisted timestamps, compare unchanged columns, replay
concurrent/repeated/interrupted upgrades and reject missing approved-rate evidence.
They preserve pending requests without fabricated approvals and existing intent
uniqueness. Focused actual PostgreSQL/MySQL race acceptance passed in 210.144
seconds; the full matrix passed (Handler 529.867 seconds, Service 5.063 seconds).
Native rate enforcement and receipt/restart acceptance are recorded in
[Project requests](PROJECT_REQUESTS.md).


## Immutable Team call attribution (version 38)

Frozen additive GORM V38 adds empty-default Team and Team membership IDs, a
Team/User/started-at/Request-ID cursor index and a subject guard. A Team fact
requires Team, membership and User IDs with blank Project and Key IDs; legacy
non-Team rows retain empty Team fields. There are no live resource foreign keys.
Released V1–V37 remain unchanged. Both missing-column prefixes and a missing
guard/index are retryable through GORM Migrator APIs.

Real-database fixtures preserve legacy facts and persisted timestamps, exercise
empty/upgraded/repeated/concurrent/partially applied migrations and reject mixed
subjects. The test-only PostgreSQL index-removal helper retains the documented
pinned-GORM exception used by earlier migration fixtures; production V38 is GORM-only.


## Stable Team policy identities (version 39)

Frozen GORM V39 widens only `resource_limits.scope_id` from 30 to 64 characters
and independently seeds `teams.tokens.write`, `teams.money.write`, and
`teams.rates.write` on the administrator role. Its private full policy schema
retains explicit primary-key `NOT NULL` when GORM alters the column. No raw SQL,
new policy table, journal identity migration or historical use reset is required.
Released V1–V38 remain unchanged.

Real PostgreSQL/MySQL fixtures reconstruct the old width using a frozen test
struct, preserve every historical policy value and persisted timestamp, run
concurrent/repeated upgrades, store the full 52-character Team/User digest, reject
oversized identities and repair each interrupted permission-seed prefix. The
Team policy lifecycle and native finite admission are separate actual acceptance
fixtures; see [Team resource limits](TEAM_LIMITS.md).

## Team monthly request history (version 40)

Frozen additive GORM V40 creates `team_quota_requests`,
`team_quota_request_steps` and `team_quota_pending_slots`. Historical request
identity, names, submitted context and approved policy snapshots do not depend on
live-resource foreign keys. Creation intents are globally unique; decision UUIDs
are nullable until a human action and globally unique when present. Each request
has at most two independently reviewed ordered stages. A composite primary key
permits only one pending request per Team/applicant/monthly dimension, with a
separate unique request reference.

GORM model checks constrain dimensions, currencies, statuses, stage ordinals and
complete approval/decision evidence. Interrupted table-prefix and missing guard,
index or permission-seed repair must preserve saved snapshots and timestamps.
V40 seeds `teams.quota_requests.read_all` independently of direct quota writes;
released V1–V39 remain unchanged. The complete real PostgreSQL/MySQL race suite
passed with V40 prefix, constraint, concurrent-startup and historical-data fixtures
(Handler 704.846 seconds; Service 6.084 seconds). Owned Compose resources were
removed. See [Team requests](TEAM_REQUESTS.md) for lifecycle, transaction and
runtime-application contracts.
