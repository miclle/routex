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

## Target-scoped Team roles (version 41)

Frozen additive GORM V41 creates `team_roles` with a composite Team/Role primary
key, a Role lookup index and restrictive live Team and Role foreign keys. It uses
private frozen ID-only relation structs and the existing bounded migration helper.
Released V1–V40, direct user role assignments and global permission records remain
unchanged; no handwritten production SQL is required.

A partially created table, missing index or either missing foreign key must be
repaired without replacing existing assignments or relationship metadata. Archived
Teams retain assignments, and assigned Roles cannot be deleted or have their
identity rewritten. The real PostgreSQL/MySQL fixture checks table/guard prefixes,
repeat execution, concurrent startup, historical row preservation, orphan and
duplicate rejection, and restrictive delete/update behavior. Current final
acceptance is recorded in [Team roles](TEAM_ROLES.md) for the frozen implementation.

## Version 42: creation defaults

Frozen GORM V42 adds two independently persisted User/Team default limit rules and
nullable `applied_default_etag`/`default_reset_etag` columns on resource limits.
Seeds are unlimited; existing resource rows receive no caps or inferred provenance.
Explicit column tags align acronym/digit fields with portable check expressions.
Checks enforce supported targets, nullable safe integers, currency/money coherence
and bounded revision lengths. AddColumn and constraint reconciliation tolerate
partially committed MySQL DDL; no handwritten SQL is introduced. Final actual
PostgreSQL/MySQL migration and feature focus passed in 243.032 seconds; the final
complete regression matrix passed (Handler 808.330 seconds; Service 6.035 seconds). See
[default limit contracts](DEFAULT_LIMITS.md) for atomic creation, explicit restore
and immutable uncertain-intent boundaries.

## Version 43: Personal Model request history

Frozen additive GORM V43 creates `personal_model_requests` and
`personal_model_request_pending_slots`, adds nullable
`user_model_grants.source_request_id`, and seeds `members.models.write` only on
`rol_admin`. The separate composite pending slot permits one current request per
applicant/Model without preventing later requests after a terminal decision.
Nullable globally unique decision UUIDs and status/receipt checks retain original
terminal evidence. Historical names and identities have no live-resource foreign
keys. Legacy grants keep null provenance and unchanged timestamps. Column, guard,
index and permission repair tolerate partial DDL without altering released
V1–V42. Real dual-database migration and complete race acceptance passed; see
[the request contract](PERSONAL_MODEL_REQUESTS.md).

## Version 44: shared Team Model requests

Frozen GORM V44 adds Team request history and Team/Model pending uniqueness,
plus nullable shared-grant provenance. Its terminal receipts survive membership
loss; no evolving entities define the historical migration. Unique intents,
receipt/status constraints, concurrent/repeat startup and partial-DDL recovery
are covered by real-driver fixtures under acceptance. See
[Team Model requests](TEAM_MODEL_REQUESTS.md).

Frozen V45 adds `project_creation_receipts` using a private bounded GORM schema.
Global creation UUID and Project uniqueness, actor indexing and intent/hash/review
length guards retain immutable historical creation independently of current
Project/manager/grant/policy rows. No live foreign keys or handwritten SQL are added.
See [Project creation and initial resources](PROJECT_CREATION.md).


## Creator-private Team attachments (version 46)

Frozen GORM V46 adds nullable creator User/membership IDs and an immutable expiry
column to storage objects, then reconciles owner-kind and Team creator constraints.
Historical User/Project rows retain null creator/expiry fields. These historical
identities have no new foreign keys: removal/rejoin cannot reassign their ownership.
The deadline is creation plus one hour, independent of cleanup retry scheduling.
Migrator operations are independently retryable after partially applied MySQL DDL;
released V1–V45 steps remain immutable. Historical V22/V23 reconstruction fixtures
also rewind the dependent V46 guard/ledger and restore versions in release order.
Focused upgrade, repeat, partial-DDL, concurrent-startup and constraint proof passed
on PostgreSQL/MySQL. Complete rebuilt-main acceptance remains pending. See
[Object storage](STORAGE.md) and [Team inference](TEAM_INFERENCE.md).


## Internal recoverable-secret root rotation (version 48)

Frozen GORM V48 introduces an inert singleton write policy, root-key metadata,
durable rotation jobs/items/action receipts and private process proofs, then
extends the existing System Job guard. Root material is never persisted.
Historical ciphertext, identity and receipt data remain unchanged. Bounded
Migrator operations reconcile partially applied MySQL DDL independently;
released V1–V47 steps remain immutable. Historical reconstruction fixtures
rewind this dependent guard/ledger before replaying migrations in release order.

The bounded worker uses exact byte keysets and ciphertext compare-and-swap.
GORM has no portable byte-collation syntax, so database-layer `ByteOrder` and
`ByteAfter` quote fixed server-owned columns and parameterize cursor values.
PostgreSQL uses the C collation; MySQL uses binary casts. Dialect handling stays
out of services, handlers and entities, and dry-run tests cover both adapters.
This exception preserves deterministic pagination across database collations.

Initial actual PostgreSQL/MySQL migration tests passed empty/upgrade/repeat,
concurrent startup and partially applied prefixes. A lifecycle rollback failure
exposed stale runtime publication after a committed Egress rewrap; its source
correction leaves migrations and rollback proof unchanged. Repaired migration
and lifecycle focus passed under race detection on both drivers in 124.896s, preserving rollback guards; owned resources were removed
and verified absent. The complete final PostgreSQL/MySQL race matrix passed
(Handler 1341.797s/Service 8.060s), with frozen source and checked cleanup. See
[internal secrets](SECRETS.md) for the historical V48 five-domain contract, versioned inventory and recovery limits.


## Repository price provenance and receipts (version 49)

Frozen GORM V49 adds per-rate repository Model/rate keys, independently owned
context-threshold provenance, an inert singleton configuration, explicit existing
Provider-model/source mappings and durable operation receipts. It follows V48.
No source rate, guessed price or implicit local Model mapping is inserted.
Existing prices start with their actual custom ownership; the compatibility
follow flag is derived only from recorded per-rate source identities.

Use private frozen schema structs and Migrator APIs for columns, constraints,
tables and partially applied DDL repair. Receipts retain exact actor, UUID intent,
review/source/configuration/catalogue identities and microsecond timestamps.
Immutable history and deliberately orphaned mappings/receipts have no lifecycle
foreign key that could remove them. Current price foreign keys remain enforced.
Service writes use portable GORM transactions and governance/pricing locks;
database collation must not authorize a different identity.

The actual PostgreSQL/MySQL migration focus passed empty/current creation,
independent partial upgrades, repeat execution, concurrent repair and constraint
checks in 62.508s with the lifecycle suite. Legitimate price prerequisites are
created before testing historical preservation; the fixture does not weaken
production constraints. Complete final-main PostgreSQL/MySQL race regression subsequently passed
Handler 1314.101s/Service 8.002s with all accepted source hashes unchanged and
verified owned cleanup.


## Guided Model creation receipts (version 50)

Private frozen GORM V50 follows V49 and adds one bounded immutable receipt table.
Migrator APIs handle creation, columns, checks and indexes, including partial DDL
repair. UUID intent uniqueness, exact actor/Connection/review identities and
microsecond timestamps are retained. The maximum 50-item snapshot is bounded to
60 KiB for portable MySQL TEXT storage. No live foreign key can remove historical
receipts when catalogue resources change.

Business writes use portable GORM transactions and governance/subject locks.
The receipt and typed audit are atomic with selected Model/name/binding creation.
No existing grant or Key scope changes implicitly. Current configuration and
runtime publication proof remain separate from historical operation commit.
Actual empty/current upgrade, repeat/concurrent startup, partial repair and
constraint acceptance on PostgreSQL/MySQL passed in the delivered V50 phase;
its complete acceptance evidence is recorded in [IMPLEMENTATION](IMPLEMENTATION.md).

V50 migration fault injection follows the existing test-only PostgreSQL index
removal exception: pinned GORM DropIndex renders an invalid CURRENT_SCHEMA()
qualifier. Only a fixed index name is removed, with presence/absence assertions;
production creation and interrupted-DDL repair use GORM Migrator APIs on both
supported databases. No business-layer dialect handling is introduced.


## Private Team member quota observations (version 51)

Private frozen GORM V51 follows delivered V48/V49/V50. It widens only
`quota_notification_observations.scope_id` to 64 characters and adds nullable
`team_id` and `member_user_id`, each bounded to 30 characters. No new table,
index or live foreign key is introduced. Historical User/Project/Team
observations retain their original scope IDs and NULL member-proof fields;
observations, uniqueness, recipients and read timestamps are not rewritten.

`ck_quota_notification_scope_v51` validates exact ASCII user/project/team/
team_member kinds across database collations. The paired
`ck_quota_notification_member_v51` requires a 52-character member scope digest
and both nonempty historical proof IDs; aggregate kinds retain 1–30-character
IDs and NULL proofs. The migration adds referenced columns before their guard,
measures scope-column width before AlterColumn, and installs both new guards
before removing old scope checks. Bounded Migrator operations can reconcile
partially applied MySQL DDL; released V35/V47 definitions remain immutable.

The integrated harness restores V47/V48/V51 after historical reconstruction
before modern readers run, preserving the released fixtures' checks. Modern
aggregate lifecycle assertions validate complete raw responses, unread/read
state and canonical private child identities before selecting aggregate rows;
they still reject Personal/Project pollution and preserve all-scope alias/cursor/
pagination behavior. No obsolete schema is substituted to suppress member notices.

The corrected revision-5 PostgreSQL/MySQL focus passed under race detection
(Handler 98.815s), including both V51 migration and private lifecycle cases.
Both drivers observed exact 30-byte overflow canonicalization; the lifecycle
fixtures retained their fixed five-native assertions, immutable prior token
history and exact fresh money policy revision. All 81 protected source hashes
stayed unchanged and owned focus resources were verified absent. Independent
production/browser/restart proof also passed. Earlier fixture failures remain
in [Implementation evidence](IMPLEMENTATION.md). The complete unchanged-source
PostgreSQL/MySQL race matrix passed Handler 1456.730s and Service 8.504s. The
runner and coordinator independently verified all 81 protected source hashes,
byte-identical package/lock and absence of every owned matrix container, network
and volume. Local acceptance and final mandatory `go tool task check` passed.
The 30-path source/documentation phase was committed and pushed as
`5363d3ce53ca4d1248227b2f941aae642c433499`; exact remote main read-back matched
and main was clean. Its distinct CI, Actionlint and GolangCI-Lint are in progress,
not yet accepted green. See [Notifications](NOTIFICATIONS.md).

## Personal Key lifecycle revisions (V52)

Private frozen GORM V52 follows V51 without modifying released migrations. It
adds `api_keys.lifecycle_revision` as a non-null, bounded 30-character string with
an empty bootstrap default. GORM `Migrator.AddColumn` is used only if the column
is absent. Existing blank revisions are read in ordered batches of 200; each gets
a fresh `kvr_` identity through a conditional update that still requires blank
state. Assigned values survive repeated or concurrent startup. Partial MySQL
column/backfill commits are repaired before acknowledging V52. The same migration
idempotently seeds `members.keys.disable` only for `rol_admin`; no new table, live
foreign key, Key grant or Project ownership change is introduced.

The runtime business entity and every product Personal Key state writer maintain
the revision. Reviewed ETags bind this persistent identity rather than UpdatedAt
precision, preventing status ABA from restoring an older active review. A private
published retained-state map carries only owner, revision and status; it does not
expose credential material or turn a disabled record into an active authenticator.
Disable/audit persistence and current published runtime proof remain separate;
there is no new operation receipt table.

The migration and Member Keys routes are delivered in checked 818500a. Focused real
PostgreSQL/MySQL migration replay passed with historical preservation assertions
retained, including a full-row check before reconstructing partial backfill. The
fixture uses the frozen column-only type so reconstruction does not update
historical timestamps; production migration behavior is unchanged. R2 source
checks/build passed. The complete dual-driver regression and final mandatory check also passed.
Authentication lifecycle passed on both drivers. A frontend-only focus repair passed rebuilt source checks without changing
backend behavior; rebuilt-artifact native/browser/restart acceptance passed.
Exact push/read-back and all three remote checks passed. Detailed
checkpoints belong in [Implementation](IMPLEMENTATION.md).


## Nullable Team membership joined time (V53)

The integrated candidate private frozen GORM V53 follows V52 and adds only nullable
`team_memberships.joined_at` through `Migrator.HasColumn`/`AddColumn`. It creates
no table, timestamp default, history backfill, permission or live foreign key.
Existing rows remain SQL NULL through upgrade/repeat/concurrent/partial-DDL
reconciliation. No Team creation, identifier, audit, policy or startup timestamp
substitutes for unknown historical join time. Released migrations stay unchanged.
The entity field is excluded from existing JSON serialization; only the new scoped
Member Teams DTO projects its recorded UTC value, leaving runtime digests and
existing embedded policy ETags unaffected by descriptive metadata.

All three production membership creation boundaries preserve generation identity:
resource creation timestamps only new owner relationships; complete member
replacement preserves the retained exact relationship ID and nullable joined
time through role/status changes; offboarding continuity does the same for
existing replacement-owner relationships and timestamps only newly created ones.
Removal then re-add creates a new membership ID/time while stable Team/User
policy and journal history survive. Lifecycle reactivation and promotion do not
invent a historical join or reset quota coverage. Mutation inputs never supply
joined time.

Current-main source checks/build and repaired focused PostgreSQL/MySQL migration
and lifecycle tests passed, including empty/upgrade/repeat/concurrent/partial-DDL
preservation and writer continuity. Six-child Overview/Teams regression and
mandatory check passed after correcting value comparisons in an existing fixture.
A fresh complete 89-case-per-driver matrix, authentication lifecycle and controlled
native/browser/restart remain pending; this candidate is not yet a checked delivery.
See [Governance](GOVERNANCE.md#administrative-member-teams-candidate).

### Member Teams process acceptance checkpoint, 2026-10-05

The standard PostgreSQL/MySQL authentication lifecycle passed, including real
process restart, persisted sessions, revocation, encrypted credentials, native
ordinary/streaming calls and immutable call history. Its owned Compose resources
were independently absent afterward. The R1 controlled native Teams run failed
at the live-reservation observation before its browser checkpoint. This is an
unresolved acceptance failure, not a delivered feature or browser pass. The
failed run was cleaned up and its exact owned containers, networks, volumes and
application listener were independently absent. All 164 protected source paths
and the checked production binary remain unchanged. Diagnose the observed
reservation values, rerun native/browser/restart acceptance, then pass a fresh
complete 89-case-per-driver matrix before committing this phase.

### Member Teams native and browser acceptance passed, 2026-10-05

Controlled process R3 passed eight immutable native completions (six known usage,
two missing usage), two membership denials without upstream dispatch, three real
join-date writers, nine typed continuity Team audits and same-artifact restart.
The primary member retained 12 known Tokens and exact 12.000000000000000004 USD;
aggregate 15 included its independent peer. A finite missing-usage call retained
5 Tokens and the conservative 5.000000000000000003 USD bound; the separate
unbounded case retained one unknown record. Original and rejoined membership
attribution, policies, ciphertext and immutable history survived restart.

R1/R2 failures remain above: R2 measured that only the helper's active-money
expectation differed. R3 corrected that single constant to the measured bound
supported by pricing component rounding; product source/binary stayed unchanged.
Actual browser acceptance passed English-default and live Chinese switching,
exact amounts, historical unknown joins, five target-only relationships, disabled
and archived state, keyboard tooltip and Escape, refresh, horizontal table access
and 390px mobile containment. Switching to the member-reader account kept the
Teams deep URL while hiding its private table and showing independent authority
guidance. Browser inspection produced no additional native dispatch or changes
to captured audits, memberships, ciphertext or history. Five rows are not proof
of the default 20-row Load More workflow; that boundary has source/driver tests.
The owned tab closed, viewport reset and Compose resources/listener were
independently absent. All 164 protected paths remained exact. A fresh standard
complete 89-case-per-driver PostgreSQL/MySQL matrix is running; commit/push remain
pending its success.

### Member Teams complete local acceptance passed, 2026-10-05

The fresh standard `go tool task test-integration` passed (exit 0) after the
value-comparison fixture correction: the unchanged ordered 89-case harness ran
against PostgreSQL and MySQL, Handler 1526.419s. Configuration 1.744s, database
1.630s, errors 1.467s and service 8.107s also passed. Its exact owned Compose
containers, networks and volumes were independently absent. All 164 protected
source hashes, V53 and the checked production binary remain exact. Together
with source format/check/test/build (2302 frontend cases/120 files), R6 focused
regression, mandatory check, both-driver auth/process lifecycle and R3
native/browser/restart acceptance, this phase is ready for a scoped main commit
and push. Previous failed fixture/helper runs remain explicit historical
evidence. Remote delivery and CI are not yet claimed.


## Personal Model grant revisions (V54 source preparation)

Private frozen GORM V54 follows V53 and adds only the private
`users.personal_grant_revision` field: non-null 64-character lowercase hexadecimal,
with an all-zero initial migration sentinel and
`ck_users_personal_grant_revision`. The sentinel records a schema baseline,
not a historical grant operation. The migration uses Migrator column and check
APIs, repairs a partially added null/empty baseline and reconciles column shape
before acknowledging the step. It introduces no table, receipt, permission,
foreign key or implicit Model grant; released migrations remain unchanged.
Root registered V54 after the accepted V53 delivery; the harness ledger now
expects 54 versions. Empty/upgrade/repeat/concurrent/partial-DDL and constraint acceptance passed
on real PostgreSQL and MySQL.

Every actual direct-grant/provenance change must advance a cryptographically
random revision in the same transaction. The three existing writers are legacy
Model creation's actor grant, complete Model-to-user replacement and first
Personal-request approval insertion; the new Member complete-set writer shares
that fence. Retained exact grant rows keep their original `CreatedAt` and nullable
`SourceRequestID`. True equal-state reconciliation makes no grant, revision or
audit write. A generation prevents remove/re-add ABA from validating an old review.
The new delta and typed before/after audit commit atomically.

Private authorization publication binds exact User creation/enablement/revision
and the complete grant proof, including empty sets and null provenance. The
reduction tombstone applies only to Personal Key authorization and prepared
attempts; it does not reuse the broader User-denial fence or revoke Team Session
or Project authority. New Personal grants never expand retained Key ceilings.
Saved state and current private publication remain separate; a successful
current-state confirmation is not an immutable operation receipt.

Real-driver migration and historical preservation acceptance passed. Controlled
native reduction, quota/history preservation, publication outage and same-artifact
restart also passed. The accepted Team histories and current V53
checkpoints above are unchanged. See
[Governance](GOVERNANCE.md#administrative-member-models-source-preparation).


Member Models passed focused dual-driver acceptance and the complete unmodified
ordered 91-case-per-driver race matrix, including V54 lifecycle/constraint tests,
plus both-driver authentication/process restart. Source checks, Go race,
development/production assets, production build and fresh 2346 frontend tests in
123 files passed. Controlled process/native/browser/restart acceptance passed ten
native completions and four zero-dispatch denials, genuine publication failure,
Personal-only reduction, unchanged Team/Project authority, old Key ceilings and
zero-write current-state retry. Browser evidence covers bilingual tables, retained
drafts, reason validation, independent permissions and responsive containment;
browser grant writes are not claimed. The 197-path successor differs from the
full-matrix floor only in a tested Actor fixture. Detailed failed and successful
checkpoints remain in [Implementation](IMPLEMENTATION.md#member-models-actual-acceptance-2026-10-05).
Final mandatory check passed; checked main delivery remains pending. F04/F19 and formal
11 complete/16 partial/3 unstarted remain unchanged.


Member Models current confirmation tolerates only a bounded busy snapshot read
after the committed transaction and one successful publication. Every fresh
capture reauthorizes the actor and exact target/set; explicit non-application,
authority changes, cancellation, deadline, stopped or expired runtime fail
conservatively. It does not replay writes or establish a historical receipt.
Focused dual-driver R4, complete integration, authentication and controlled
native/browser acceptance passed; current-state confirmation remains distinct
from historical operation evidence.

## Local registration approval migration V57

The current approval integration appends one frozen GORM migration; released
versions remain unchanged. It adds nullable `users.approval_application_id`,
default-false registration approval policy and a private64-hex policy generation,
plus `registration_approval_applications`. Each application records an exact User
ID and creation identity, canonical application ID, pending/approved/rejected
state, private revision and nullable recorded decision fields. A unique User
index, restricted User foreign key and state/revision/decision checks protect
these records. There is no reverse foreign key or cascading history deletion.

No historical account receives an invented application, approval or timestamp.
The migration seeds only the built-in administrator's reserved approval power
and advances its definition generation when that permission is newly inserted.
GORM Migrator APIs repair supported partial column/table/index/constraint prefixes;
invalid retained rows fail constraint installation rather than being fabricated.
Empty creation, existing-data upgrade, repetition, concurrent startup and partial
MySQL DDL recovery are covered by the new real-driver fixture. Both PostgreSQL and MySQL passed these migration cases in the focused and
complete V57/102 regression. Authentication and production restart retained the
original database and migration ledger.

## Registration email-domain migration V58

The new frozen private GORM schema adds a bounded JSON-text domain array to the
singleton governance settings. Its historical unrestricted representation is
`[]`. The migration uses GORM Migrator AddColumn, ColumnTypes and AlterColumn plus
a query-builder NULL-only backfill; it introduces no handwritten SQL. It first
adds a nullable column, preserves non-NULL retained values, validates the frozen
canonical grammar, then reconciles the 2,048-character non-null column and
default. Invalid retained values or singleton identities stop migration rather
than resetting a configured restriction. Historical migrations remain unchanged.

The new migration fixture covers empty creation, existing-data preservation,
repeat/concurrent startup, nullable/incorrect-shape partial prefixes and invalid
retained values on PostgreSQL and MySQL. Both drivers passed this focused fixture
and the associated registration lifecycle. Authentication/native restart on both
drivers and controlled PostgreSQL same-artifact/Session restart passed. Full105 R1
exposed a test-only V57 fixture assumption that its migration was always the final
ledger row. The fixture must reconstruct its explicit released V57, preserve all
other ledger rows including V58, and retain its original upgrade/partial/repeat/
concurrent/constraint assertions. No released migration changes. Corrected R2
passed105 ordered cases per driver, eight constraints and3,247 named events
without failure or skip. All1,509 protected paths remained exact, and owned
containers/networks/volumes are independently absent. Current-artifact
production and original-Session restart also passed. The containing checked
commit releases V58; final checking and remote CI remain separate gates.
Full log SHA256:
`7dc6857e3ae6df5e2afcd2f76e64afcd4937e0d48dae4fd1f6ae0b06fd9a2ec6`.

## Personal monthly warning migration V59

Frozen private GORM schemas add `quota_warning_observations` and
`quota_warning_inboxes`. Unique identities include owner creation, dimension,
month, policy revision, currency, level and the fixed80/90 threshold generation.
Checks constrain recorded month/as-of/coverage, dimension, currency, level and
threshold pairs; inbox uniqueness preserves one exact owner projection. GORM
Migrator APIs repair missing supported columns and reconcile schema/index/check
prefixes before recording V59. No new handwritten SQL or business-layer dialect
branch is introduced; released migrations and exhaustion history stay unchanged.

The real-driver fixture pins V59 explicitly and preserves all other ledger
generations. Empty creation, existing-data upgrade, repeat/concurrent startup,
partial MySQL DDL, constraints/indexes and retained history passed on PostgreSQL
and MySQL. Historical migrations and existing User timestamp precision are
unchanged. Identity-negative fixtures prove a persisted 1ms birth change and
exact restoration; timestamp comparisons retain every scalar and exact instant.

Focused PostgreSQL/MySQL acceptance passed eight selected scenarios and 128
named events. Complete race-enabled regression passed 107 ordered scenarios per
driver, eight additional constraint cases and 3,361 named PASS events,
with no named failures or skips. PostgreSQL took 904.15s;
MySQL took 1169.90s. The original 105-scenario prefix and
all 1,520 protected source paths remained exact. Owned Compose containers,
networks and volumes are independently absent.

Full log SHA256: `d62c54939fae35c1a9a1bafe1d826f03146d111b7a9e1b8fca7c942e240b3148`.
Focused log SHA256: `604401ae11e39c6e0e40b6732edd3cda1c85c41a9f1067ccec92d0fdee87bf30`.

Controlled PostgreSQL production and bilingual browser acceptance passed against
binary `8af28a7b8ecfd13974b4db15017e92da681308fb3df641445311bc0431e65bc9`.
Six completed native calls and attempts produced four Personal warnings and no
exhaustion or operational/SMTP fanout. Recorded Tokens 8/10 and 9/10 and USD
9/11.25 and 9/10 appeared in default English and live Chinese. Single read
returned HTTP 200; read-all returned 204 and retained all historical rows.
The same binary/configuration/database/journal and original Sessions survived
process restart without additional inference or login. Immutable call/attempt,
warning and read-state facts were unchanged; administrator/other-account reads
remained isolated. Escape restored trigger focus, console errors/warnings were
empty, and owned tabs/listeners/Compose resources are independently absent.
MySQL production browser verification is not claimed; dual-driver native and
migration acceptance is recorded separately.

Formatting, mandatory checks, Go race source tests, pinned backend lint, 3,169
frontend cases in 151 files, four Node checks, production build and embedded
asset tests passed. Final mandatory checking also passed with no errors and two
existing Fast Refresh warnings; delivery is tracked in Git history.
Team aggregate, Project, private Team-member and Key warnings, SMTP and
configurable thresholds remain outside this Personal slice. F17/F23 and formal
11 complete / 16 partial / 3 unstarted totals remain unchanged.

### Historical failed attempts and corrections

Focused R1 failed test oracles for mixed warning/exhaustion shape, literal
`etag` instead of GORM field `ETag`, and timestamp representation equality.
Log SHA256: `3123ca24aa0de78534bf0d5290e7577b144f5644ff70bf88cd3738bc498ffb1b`.
R4 preserves every assertion with exact persisted policy restoration,
`time.Equal` timestamp instants and a genuinely persisted 1ms User birth change.
Focused R2 failed the missing-native-usage money oracle.
Log SHA256: `81a221dec63238df8bf1edc7efb0d81cb2da9046b9c31ba5865e357b517e5165`.
R5 retains TokensHeld=2, no MoneyHeld and MoneyUnknown=1: money was unconstrained
at admission, and later policy changes cannot rewrite the original receipt.
Runtime, UI, V59, eight native calls, seven warning observations and three
exhaustion observations stayed unchanged. Earlier failed runs remain failed;
the successor's actual pass does not reclassify them.

Two failed private production helpers are separate: absent price 404 before
creation, and a binding variable overwritten after near/critical. Successors
review the existing filtered price list and retain a distinct immutable binding;
product source is unchanged. The finite full-suite deadline is 40 minutes,
based on the measured 2,015.362-second prior matrix and added scenarios.
Individual query/request/readiness bounds and race/assertion coverage are unchanged.

## Team aggregate monthly warnings V60

Frozen GORM V60 adds private Team observations and recipient inboxes with immutable
original recipient/Team/User birth facts and read state. Private frozen structs
and GORM Migrator APIs preserve existing V59 and all released steps. Exact
current membership and incarnation gate reads/marks; replay never expands
historical recipients. It does not change admission or existing exhaustion.

Both supported databases passed V60 creation, upgrade, repeat/concurrent startup
and constraints, plus Team native warning lifecycles. Full 109 preserved the
original 107 prefix and passed 3,526 named events with eight constraints.
All 1,533 source paths stayed exact and owned resources are absent.
See [Notifications](NOTIFICATIONS.md#team-aggregate-monthly-warnings-v60) for
threshold, runtime, production/browser/restart and retained failed-run evidence.

## Project monthly warning integration V61

Frozen GORM V61 and the unchanged 109-scenario prefix plus Project cases 110–111
extend monthly 80%/90% notifications to original current managers. Focused real
PostgreSQL/MySQL workflows, frontend/build/assets and current-main PostgreSQL
production/bilingual/privacy/original-Session restart and full 111 passed;
final mandatory checking and complete Task tests passed. The containing commit
records this phase. See [Notifications](NOTIFICATIONS.md#project-monthly-warning-integration-v61) for
scope, counts, artifact, runtime evidence and acceptance limits. F17/F23 remain
partial and the full objective continues.

## Private Team-member monthly warning integration V62

Private member warnings use the approved fixed 80% reminder and 90% critical
thresholds on complete settled monthly Tokens and exact decimal money. Their
scope is the stable Team/User pair, independent of a replaced Membership row.
Frozen GORM V62 creates immutable observations and original-recipient inboxes,
with separate exact Team and User birth proofs. Current published parent and
member policies, membership, currency, calendar and settled journal coverage
must agree. Holds, unknown usage and stale authority never produce percentages.
No owner, administrator or peer receives a member's warning; removal hides
history and same-identity rejoining restores the original read state. The existing
hard stop, exhaustion notification and SMTP contracts remain unchanged.

Core race tests passed eight top-level tests/111 named events. Corrected fixture
source tests passed five top-level tests/15 named events. Actual isolated
PostgreSQL/MySQL focus passed all six selected lifecycles and 47 named events,
without failures, skips or race reports; PostgreSQL took 68.16s and MySQL 91.01s.
All 1,559 protected source paths and semantic staged identities remained exact;
owned containers, networks and volumes are independently absent. Earlier R1/R2
fixture runs remain failed: an unused builder sent unsupported policy fields;
the corrected fixture now uses the real presence-aware decoder. No production
server contract was relaxed.

The identical runtime/UI source passed complete Task testing: Go race/coverage,
3,365 frontend cases in151 files, four Node checks, two development lifecycle
checks and production assets. Main mandatory checking and production build
passed; a test-only scope initializer cleanup preserves the exact identity.
The finite45-minute aggregate integration bound reflects the measured2,299.13s
predecessor; per-query/request/readiness deadlines and assertions are unchanged.

Controlled current-main PostgreSQL production and bilingual browser acceptance
passed: six native completions/attempts, four immutable observations/four
sole-recipient inboxes, recorded Tokens8/10 and9/10 plus USD9/11.25 and9/10,
owner/peer/admin isolation, removal/rejoin history, single-read200/read-all204
and identical artifact/configuration/database/journal/original Sessions after
restart without additional login or inference. Binary SHA256:
`7fbdaa881f1c6957bb0b3e457cf763b07590b2dac0937192c1f8ad9562fb6375`.
All1,559 main source paths stayed exact; owned tabs/listeners/Compose resources
are absent. MySQL browser acceptance is not claimed. Two prior helper failures
remain failed; source-only corrections removed an impossible public field
expectation and used actual fanout table names before fresh complete acceptance.

Full113 R1 failed despite PostgreSQL113 passing: MySQL's existing Personal-warning
unpublished fixture returned runtime-unavailable at line413. All new Member
cases passed;3,858 named PASS events, no skips/race reports and exact source/index
are recorded independently from the failure. Log SHA256:
`e948286d79ee24e2346551314ee1ff5bf5250fd7bef114a757d6fe38e17f7392`.
The stopped-publication fixture has a five-second lease and multiple sequential
negative segments; expiration between observation fences is a source-supported
explanation, not an instrumented historical branch proof. A narrow successor
refreshes valid baselines before deliberate mutations, preserving all assertions
and production deadlines. Fresh focus passed eight selected Personal/Member
migration/lifecycle cases and71 named events on both drivers, with no failures,
skips or race reports (PostgreSQL66.07s, MySQL104.29s). Log SHA256:
`ac9b3537532e43a4d49f01b539988db48ba59712d30641f334e8360dcf53e3e8`.
All1,559 worktree paths/index stayed exact and owned resources are absent. Main
carries the same minimal fixture and passed final mandatory checking. Corrected
full113 R2 passed3,861 named run/pass events and eight constraints with no
failures/skips/race reports: PostgreSQL1,019.91s, MySQL1,443.51s and total
2,494.711s. All1,559 source paths and semantic index identities stayed exact;
owned containers/networks/volumes are independently absent. Log SHA256:
`d56436dcdce3bd334a763c5e62555bf416ddc932894aa29bf2da0c191ed9c92e`.
A narrow text-output reader correction recognizes Go summary ordering; earlier
reader rejections remain recorded and no test/source/resource changed for it.
Main retains an assertion-equivalent Member fixture initializer lint correction
outside the frozen worktree; its focused pure race and final mandatory checks
passed. The containing commit records this phase. F17/F23 and formal11/16/3
remain unchanged.

## Team creation receipts V63

Frozen private GORM V63 adds durable creation receipts with exact actor/Team birth
proofs, a unique actor/creation intent and bounded immutable snapshot. There are
no live foreign keys to deletable resources. MySQL uses MEDIUMTEXT for the
128 KiB snapshot bound; precision3 birth timestamps match the persisted resource
identities. Services use GORM transactions and database-layer exact comparisons;
metadata, owners, copied defaults, authorized sparse overrides, audit and receipt
commit together. Startup retains released V62 unchanged.

Migration fixtures cover empty/repeated creation, existing-data upgrade, partial
DDL and concurrent startup on both databases. The pinned PostgreSQL GORM adapter
cannot express this fixture's fixed unqualified index removal correctly; only
the test fault-injection step uses a documented fixed DROP INDEX, with presence
checks before/after. MySQL keeps GORM DropIndex. The production migration uses
GORM and its schema definition is unchanged. The earlier failed fixture run is
retained. Corrected R9 passed all ten selected PostgreSQL/MySQL cases, preserving
the exact source/index and independently verified cleanup. Unfiltered full115
then passed 115 ordered scenarios per database, eight constraints and 3,963
named RUN/PASS events. All 1,584 source paths and the semantic index stayed
exact, and owned resources are independently absent. Controlled PostgreSQL
production/browser/original-Session restart passed with exactly two Teams, two
receipts and two typed creation audits, without inference dispatch. No MySQL
browser run is claimed; dual-driver migration/lifecycle proof is the full115 gate.

## Key monthly warning observations V64/V65

Frozen private GORM V64 and V65 add separate Personal and Project Key warning
observations and recipient inboxes. Exact original rotation-root/resource/User
births, calendar, policy generation and complete settled journal coverage fence
publication. Unique observation identities and independent inbox read state are
persistent; live foreign keys cannot erase retained historical facts.
Released V63 and earlier steps remain immutable. The new migration/lifecycle
cases append to the unchanged 115-case prefix. Focused dual-driver acceptance
passed; complete119 subsequently passed both drivers. Controlled production
acceptance also passed; the containing commit records this checked phase. The measured predecessor
plus successful focused cases justify the finite 55-minute aggregate Go limit;
query, request, readiness, race and assertion boundaries remain unchanged.


### Complete Key119 acceptance

The isolated V64/V65 candidate passed 119 ordered scenarios per driver, eight
constraints and 4,370 named RUN/PASS events, without failures/skips/race reports.
PostgreSQL took 1,201.57s, MySQL 1,485.34s and the whole command 2,770.780s.
All 1,616 protected source paths and the semantic index stayed exact; owned
containers/networks/volumes are independently absent. Log SHA256:
`01d4ae20460d3e9606e795afb9ab321b2a4162dffffb3804eaeda1896be39dfb`.
This matrix ran on the original isolated candidate; current main inherits its
compiled backend through the separately verified comment-only equivalence.
Main check, complete Task (3,714 frontend cases/157 files), production build and
both real-process authentication/gateway lifecycle gates passed. Controlled Key
production/bilingual/original-Session restart acceptance also passed as recorded below.

### Controlled Key production acceptance

The final production artifact
`ce55e0d3e30e3b3e1f88bfec8e0d1ff375c45477f9aaa19c07a814aba875b8ed`
passed two separate PostgreSQL environments on the reviewed 1,617-path main
source floor. Personal acceptance recorded six native calls, five observations
and five original-owner inbox rows. Project acceptance recorded six native
calls, five observations and ten independently read original-manager inbox rows.
Normal browser sign-in, default English/live Chinese, original rotation-root
labels, exact decimal amounts, single-read200/read-all204, recipient isolation
and Escape/focus passed. All seven original browser Sessions reread after an
identical-artifact/config/database/journal process restart without reloading
those authenticated documents, logging in again or replaying inference.

The Project API acceptance separately verified removal/rejoining and no late
manager backfill. Its ten-second disabled interval confirms no new or changed
observations during that interval; it does not prove worker invocation. Unknown
coverage, holds, currency mismatches and hard stops remain dual-driver matrix
evidence rather than claimed browser scenarios. Owned tabs, listeners and both
Compose projects' containers/networks/volumes are independently absent. The
combined result is 12 native calls/attempts, ten observations and 15 inbox rows.

## Initial Team Model receipt provenance V66

Frozen private GORM V66 adds version/count/digest fields to creation receipts,
`team_creation_receipt_models` keyed by creation/Model identity with precision-six
original Model births, and nullable `source_creation_receipt_id` to Team grants.
Version zero retains count zero, null digest and no children; nonempty version
one binds 1–1,000 complete sorted identities. Readers reject missing, extra or
corrupt children rather than interpreting them as empty. Released V63 snapshot
bounds and normalized empty-request hashes stay unchanged; no historical Model
or source provenance is invented. Receipt children have no new live-resource
foreign keys that could erase retained history.

Creation writes explicit grants, receipt children and typed audits in the same
transaction. Current publication compares complete stored grants and their
original source/birth facts independently of eligible invocation routes. A
saved receipt never proves usable inference and a retry never restores removed
grants. Scenarios 120–121 append to the unchanged 119-case prefix; the ledger is V66. Empty creation, upgrade, repeated/concurrent startup, partial
DDL, constraints and historical preservation passed complete real-driver gates.

Focus R2 recorded 14 direct passes and two migration failures caused by omitted
fixture Team/Model parents, before V66 assertions. Both new lifecycle cases
passed. The corrected fixture supplies valid persisted parents and retains the
released foreign keys. Focus R3 passed sixteen direct cases on the unchanged guarded 1,630-path
code floor; complete121 subsequently passed on both drivers. Main checking,
Task, authentication/gateway lifecycle and build gates also passed. Controlled
bilingual/original-Session recovery and native invocation have separate accepted
evidence. The original final collector remains failed at its denied-usage oracle;
read-only review confirms exact durable grants/receipt births/audits and native
attribution. This main documentation is outside that protected matrix.

## Recorded Role description V67

The frozen private schema adds only roles.description through GORM AddColumn and
creates the named 2,000-byte constraint through Migrator. Its empty default
preserves existing records. HasColumn/HasConstraint checks resume partially
applied MySQL DDL. Released steps are unchanged; no handwritten DDL is added.

Both real databases pass the new upgrade/repeat/concurrent/constraint cases in
the complete124 matrix. Schema-reset fixtures renew physical pools around schema
recreation and migration replay so retained PostgreSQL statement descriptions
are not reused after DDL. This test-only repair leaves production connections and
released migrations unchanged. Earlier full123 failed and remains failed; the
subsequent complete124 is independently accepted. Main checks, complete Task
testing and the production build also pass. Controlled Role description browser
and original-Session restart acceptance pass. Connection conflict, response-loss,
Session-recovery and same-artifact restart acceptance also pass in a fresh
controlled environment; its earlier actual-503 run remains failed. The containing
commit delivers this checked phase.

## Frozen duty template migration V68

V68 is a data-only GORM transaction using private frozen Role and permission
structs. It adds three immutable duty roles and 17 exact grants, with stable
namespaced definition/name keys and preserved birth timestamps for valid partial
seeds. Bounded pre/post validation refuses aliases, incompatible identities and
extra permissions instead of overwriting historical authority. Released schema
steps and custom roles remain unchanged; no handwritten DDL is introduced.

The complete 126-case matrix passes empty creation, V67 upgrade, repeat and
concurrent startup, partial-seed repair and collision rejection on PostgreSQL and
MySQL. Existing global catalogue bounds and query budgets also pass. This is
isolated candidate acceptance. Main complete Task and both authentication/gateway
persistence lifecycles also pass; paired locale-only successor checking/build and
controlled Finance browser assignment/removal pass. No released migration changes
or repeat full-matrix run were needed for the copy correction. Owned browser/app
and Compose resources are independently absent. The containing commit delivers
this slice; unrelated transient read diagnostics are retained separately.

## Provider Model reverse binding index (V69)

Frozen GORM migration V69 adds the nonunique
`idx_bindings_provider_model(provider_model_id,model_id,id)` index with
`HasIndex`/`CreateIndex`. Its schema does not use evolving business entities.
The index supports the complete Provider-scoped stored-binding projection; it
changes neither relationships nor routing weights. Creation, upgrade, repeat, interrupted-ledger and concurrent-startup acceptance
passes on PostgreSQL and MySQL in the complete129 gate.

Index-removal fault simulation has two fixture-only adapters. The pinned
PostgreSQL GORM `DropIndex` generates invalid schema-expression syntax, so the
fixture drops one fixed index with documented SQL. MySQL may use that reverse
index to support an existing foreign key; the fixture creates a frozen GORM
supporting index before removal and removes it only after V69 restores the full
physical key. Foreign keys are never disabled. Production migration stays GORM.
The initial focused run retains both removal failures as failed evidence.

V69 complete dual-driver acceptance passes 129 ordered scenarios per database, including empty/upgrade/repeat/concurrent/index paths. The historical duty-role reconstruction fixture now checks exactly V1–V68 while retaining later ledger entries. This changes test scope only, never a released migration.

## Personal User monthly behavior (V70)

Frozen GORM V70 adds only `resource_limits.tokens_month_behavior` and
`money_month_behavior`: VARCHAR(16), NOT NULL, default stop. Two exact byte/length
value constraints accept only stop/alert_only, and the scope constraint permits
alert-only only for exact User scope. Storage16 preserves invalid trailing-space
suffixes for CHECK rejection instead of MySQL truncating them to valid values.
Partial valid DDL resumes through bounded GORM Migrator checks of width,
nullability/default and missing constraints. Released V1–V69 remain unchanged;
no usage, receipt, identity, relationship or warning history is reset.

The registry appends the two Personal migration/lifecycle cases and Provider
metadata after the unchanged 130-case prefix: 133 scenarios per driver and ledger
version 70. V68/V69 fixtures retain bounded historical prefix/replay checks. The
exact scoped candidate excludes pending Excel changes and passes its own full133:
266 direct PostgreSQL/MySQL lifecycle cases, eight constraints and 4,756 balanced
named results. Its 1,034 backend paths and all 1,718 source paths/modes are verified;
owned resources, process and listeners are absent. Acceptance SHA-256:
`ccb38e2b4b508f6304c2246b0d161e18e66263a0d023fd7ca332970af26b4ac2`.
The older private 1,036-backend R2 is separate historical evidence.
Current delivery is tracked in [Implementation](IMPLEMENTATION.md).

## Team aggregate monthly behavior (V71)

Frozen GORM V71 reuses V70's two width-16 behavior columns and replaces
only the scope constraint to permit exact User and Team aggregate alert-only
policies. Team-member, Project and Key rows remain hard-only. It performs bounded
validation and portable GORM Migrator constraint recovery; no column widening,
ledger rewrite, counter reset, row normalization or historical migration edit is
introduced. Released V1–V70 schema definitions remain unchanged.

The registry appends Team migration/lifecycle after the exact 133-case prefix,
with current ledger 71 and 135 scenarios per database. The V70 fixture continues
to test V70's historical User-only contract and then replays V71 for the current
schema. Portable upgrade/repeat/concurrent/partial-DDL and scope-value assertions
remain strict. Focus R3 accepts the two new cases and seven relevant predecessors
on both databases; complete private135 passes. The exact staged 1,040-backend
candidate passes its own 18-case focused dual-driver run and complete Task/check/build.
These remain separate evidence; earlier failed fixture records are retained.

## Vault Token integration and versioned inventory (V72 candidate)

Frozen GORM V72 adds eight bounded tables for integrations, immutable revisions,
separate writer/reader encrypted auth, configuration receipts, durable probe plans
and command claims. Migrator APIs create constraints, indexes and foreign keys
with bounded partial-MySQL-DDL recovery. Released V1-V71 definitions stay unchanged.
Root jobs gain explicit inventory versions: historical version1 has five domains;
version2 adds every retained Vault writer/reader auth envelope. Missing counts
remain not_scanned/null; zero/unknown versions reject. Nonterminal version1 jobs
require explicit Resume and a fresh seven-domain proof. Completed historical
jobs are not relabeled. See [Vault Token integrations](VAULT_TOKEN_INTEGRATIONS.md).

## Saved Vault AppRole authentication (V78 candidate)

Frozen GORM V78 follows V77 and adds a size 16, non-null, Token-default method column to the existing writer and reader auth tables. Exact byte-length and character constraints admit only token and approle under either database collation. Legacy backfill preserves every ciphertext, auth generation and original root reference; released V1-V77 steps remain unchanged. A partially applied column must have the exact type, width, nullability and default before bounded Migrator constraint repair can proceed.

No new secret table or root domain is introduced: the complete AppRole tuple remains encrypted in the existing auth row. Two proposed real-driver cases append after the exact 148-case prefix, covering empty creation, retained-data upgrade, repeat/concurrent startup, partial DDL, constraints and ordinary configuration/probe lifecycle. Registration and source tests do not establish actual database or Vault acceptance; the candidate requires fresh matching evidence.

## Personal Key monthly behavior (V73)

Frozen GORM V73 reuses V70's width16, non-null, stop-default columns. It installs
an exact user/team/key scope fence before removing V71's fence. Stored key scope
alone does not authorize soft behavior: application proof requires the exact
Personal owner/root identity. Project Keys require their separate Project/root proof. Value/kind aliases
remain invalid on PostgreSQL and MySQL. Partial DDL is repaired with bounded
Migrator checks; released migrations, counters and historical identities remain
unchanged. The repaired Personal migration/lifecycle focus passes both drivers;
The containing 97-path phase is delivered as `c2368dd`; original source receipts remain distinct.

## Project aggregate monthly behavior (V74)

Frozen GORM V74 follows V73 and adds exact project to its permitted scope fence,
installing the new constraint before removing its predecessor. Width16, stop
defaults and exact value checks remain. Team-member stored policies remain hard;
Project Key soft policy requires separate application identity proof. No released migration or retained accounting row is rewritten.

The final registry preserves 139 original names/order and appends the two
Project scenarios for 141. Three bounded historical migration fixture bindings
keep V74 recorded while removing its fence, explicitly reconstruct V73 and run
the original V70/V71/V73 scenarios. Their real startup calls skip recorded V74.
A deferred remove/replay of V74 restores the final fence; only explicitly replayed
ledger timestamps may change. Original scenario assertions remain intact. They are explicit fixture companions, not unchanged
function bindings. Final acceptance requires empty/upgrade/repeat/concurrent,
partial-DDL and constraints on both databases, combined focused 18 cases and the
unfiltered 282-direct-case Full141 plus eight generic constraints. Full141 passes
on both databases; controlled Project runtime/browser acceptance remains pending. See
[Implementation](IMPLEMENTATION.md) for separately recorded evidence.

An earlier combined focused run failed: all Vault and Personal cases passed,
while both Project lifecycles fail a test-only warning expectation. Its recorded
coverage account has known settled150 Tokens, held150, TokensUnknown0 and
MoneyUnknown1. A token cap187 legitimately produces a near warning from known
settled Tokens; unknown money excludes only its own dimension. The fixture-only
successor sets that coverage token cap200 (75% settled), retaining the exact
money cap187 attounits, journal facts, zero-warning assertion, unknown-usage503,
native counts and final four observations/eight manager inboxes. Product warning
and admission semantics are unchanged. The cumulative fixture also retains its
exact manager setup and historical V73 reconstruction. The final source adds
only the existing pinned pgx test dependency's direct declaration, without any
version or go.sum change. Fresh final source gates and expanded Focus18 pass with all 1,769 source bytes and modes unchanged. Full141 passes 282 direct lifecycle cases and eight constraints on both databases. The separately bound Vault R7 and Personal R6 controlled API/restart runs pass; Project runtime and browser remain pending; the containing phase is delivered as `c2368dd`. Earlier failures stay failed. See [Controlled acceptance boundary](RESOURCE_LIMITS.md#candidate-verification-boundary) for the R3 runtime artifact versus R4 test-only source distinction.

## Project Key monthly behavior without schema changes

Existing V73/V74 mode columns and exact shared `key` fence support this package.
No migration, historical step or counter is changed. Business and runtime proof
require exact Project identity/birth, immutable Key root/birth, complete bounded
rotation ancestry and no Personal-Key collision before allowing soft policy.
Generic row decoding rejects unproved soft Key accounts. The journal receives
server-owned Project Key evidence separately from Personal Key authority.

The current registry appends the Project Key lifecycle after the original 141
ordered cases, retaining released V74 and historical constraint assertions.
Three necessary integration fixture updates reflect canonical child stop modes,
invalid-mode rejection and exact invalid root-birth denial. The final test-only
successor uses a whole-second Project birth mutation that both databases can
retain, and expects saved child modes to survive Key rotation. A below-exhaustion
warning fixture uses settled150 against cap166 to exercise critical90 rather
than asserting a percentage notice after exhaustion. Production warning
thresholds and original hard denial/native counts remain unchanged. See
[Implementation](IMPLEMENTATION.md) for actual driver and runtime results.

## Team-member monthly behavior (V75 source candidate)

Frozen GORM V75 extends the exact monthly-mode scope fence to `team_member` using
private schema definitions. It adds no columns and does not alter released V70–V74.
Width16, non-null stop defaults and exact case/length/value checks remain. Install
the successor scope CHECK before removing V74's fence; repeat/partial-DDL startup
must preserve rows and repair only the bounded schema. PostgreSQL/MySQL collation
aliases, unsupported scopes and invalid modes remain rejected. Database scope alone
cannot authorize soft behavior: application proof binds current Team/User/member
identity, original Team birth and the complete retained pair account.

Historical test reconstruction extends the existing bounded helper to retain V75's
ledger while rebuilding the original V70/V71/V73/V74 fences and assertions, then
restores V75. Unrelated ledger timestamps must remain exact. The original V74 body
is retained in a private historical helper; the 142 registry names/function bindings
remain and only two Team-member cases are appended. These are explicit test-only
compatibility deltas, not changes to released migrations or relaxed constraints.

The private Team R3 source candidate passes mandatory checking, complete Task
and build with 4,526 frontend tests; its backend bytes/modes are identical to R2.
Real empty/upgrade/repeat/concurrent, partial-DDL and constraint evidence must keep
its own PostgreSQL/MySQL source binding. The original failed lifecycle restart and
its fixture-only startup repair remain separate history, preserving accounting,
Session, CallRecord/Attempt and no-new-native assertions. See
[Implementation](IMPLEMENTATION.md) for current driver and runtime acceptance.

## Connection enablement (V76)

Frozen GORM V76 adds `provider_connections.enabled` through the Migrator API,
with a non-null true default. Existing and new Connections retain enabled routing
by default. The step checks the actual column type, nullability and default after
creation, including partial-DDL and repeated startup. It neither edits released
migrations nor rewrites child Credentials, models, bindings or history.

Status writes use a GORM transaction and map updates so false is persisted. They
retain the Connection's shared revision and record a typed `connection.status.update`
audit containing only before/after status and required reason. Runtime publication
and exact identity revalidation remain application responsibilities; a stored
boolean alone does not prove routing application. The real-driver migration and
lifecycle evidence remains bound to its exact candidate in
[Implementation](IMPLEMENTATION.md).

## Classic Azure deployment coverage (V79 candidate)

Frozen GORM V79 follows V78. It adds the immutable Connection adapter (native by default) and nullable dated API version. Exact constraints preserve native/null defaults and require Azure classic Chat with an explicit version. Partial MySQL DDL must have the expected column type, width, nullability and default before bounded Migrator repair proceeds; released V1-V78 remain unchanged.

The credential_deployment_attestations table stores the exact Credential/ProviderModel pair and retained identity proof. It introduces no copied discovery access or live foreign keys. Current applicability requires the exact Credential, source, Connection transport and ProviderModel identities. Credential coverage_revision is monotonic and nonnegative; bounded coverage_review_etag and coverage_intent_sha256 retain the immediately preceding reviewed change for exact current reconciliation, preventing old uncertain intent from restoring withdrawn coverage. These fields contain no credential material.

The two Azure migration and coverage lifecycle cases append after the unchanged 150-case registry prefix. Source checks and registration remain separate from real PostgreSQL/MySQL migration, controlled native, browser and final delivery evidence.

## Manual Provider orphan disposition (V80)

Frozen GORM V80 adds `provider_credential_cleanups` and
`provider_credential_creation_uses` after V79 without changing released steps.
Private frozen structs, model tags and the database-layer Migrator create and
repair this bounded schema on PostgreSQL and MySQL. The cleanup table uses the
original creation UUID as its primary key and a unique command UUID, enforcing
one retained disposition per creation. Exact enum checks distinguish `pending`,
`unknown`, `failed` and `acknowledged`. Actor/Integration births, reviewed ETag,
operation proof, root epoch, reason and separate ownership/cleanup observations
remain private retained command facts; no cleanup Token is stored or hashed.

Creation-use rows retain the exact process identity/generation and exposure
fact. Missing or foreign provenance is a blocker. Historical instance rows
remain relevant after lease expiry, stopping or retirement; removing heartbeat
liveness does not prove that source material has drained. Original creation
stages and credential reference history remain unchanged.

Creation admission and final commit use the same governance serialization as
the durable disposition. Remote authentication, ownership reads and destruction
run after database locks are released. Exact token-free retries only read the
recorded receipt; persistence or response loss never authorizes another remote
effect. Both migration/lifecycle cases follow the unchanged 152-scenario prefix.
The PostgreSQL missing-index fixture uses one fixed test-only DROP statement
because the pinned GORM driver generates an invalid CURRENT_SCHEMA qualifier;
production repair still uses the Migrator. See [Implementation](IMPLEMENTATION.md)
for separately scoped source, real-database, real-Vault and delivery evidence.

### V81: Personal rolling warning episodes

Frozen V81 adds `personal_rolling_quota_warning_states`,
`personal_rolling_quota_warning_observations` and
`personal_rolling_quota_warning_inboxes`. State identity is exact User ID, original
birth and rolling window. State retains sampled cap/reset lineage, an episode
identity, emitted-level flags and the last authoritative sample time; sample time
orders updates and is not a notification dedup key. Immutable observations have
an episode/level unique index, exact applied revision, covered window facts and
integer settled/cap counters. Recipient inboxes uniquely bind observation and
recipient, with a read timestamp and paging index. History has no foreign key to
mutable live Users, so deletion never rewrites recorded facts; service reads gate
history against the current exact User birth.

Window, level/threshold and generation checks use portable byte-exact predicates
rather than assuming a case-sensitive database collation. Amount/window checks
reject invalid snapshots. The migration uses bounded private frozen structs and
GORM table/column/index/constraint APIs. Registered dual-driver fixtures specify
partial first DDL and concurrent/repeat startup checks; released versions 1–80
are unchanged. The new lifecycle fixture specifies genuine settled
native use, concurrent producer deduplication, transactional inbox rollback,
recipient isolation and restart preservation. These fixtures require separate
actual PostgreSQL/MySQL acceptance; source compilation is not that acceptance.

Positive caps and settled counters retain explicit signed-int64 bounds; settled
use may equal or exceed the cap, so a first exhausted sample can retain its
90% critical observation. Null and zero policy caps produce no percentage
observation. Unknown or incomplete coverage must leave episode state intact,
and only a fully known sample below 80% rearms the same-cap episode. Sampled
cap/default-reset changes have separate lineage. Monthly tables and admission
semantics are unchanged. The registry preserves the original 154-case prefix
and appends exactly the V81 migration and lifecycle cases. Final composed-main
Full156 passes 312 direct PostgreSQL/MySQL cases, eight constraints and 5,404
balanced named tests; controlled native/API/restart acceptance also passes on
the unchanged production backend. Three historical predecessor test fixtures
now preserve the exact V81 suffix and original timestamps. Earlier failures
remain retained; browser acceptance and other-account rolling warnings remain
open. See [Implementation](IMPLEMENTATION.md) for the source-bound receipts.

The finite Vault response-close prerequisite adds no persisted closure receipt,
source-denial schema or cleanup migration. V82 is separately allocated to Personal
Key rolling warnings below. Its private, non-serialized holder and
SDK observations cannot establish cross-process absence or authorize cleanup
of previously published Provider sources.

### V82: Personal Key rolling warning episodes

Private frozen V82 adds `personal_key_rolling_quota_warning_states`,
`personal_key_rolling_quota_warning_observations` and
`personal_key_rolling_quota_warning_inboxes`. Composite state identity retains the
exact root Key, original owner/root births and window. Observations uniquely bind
episode and level; inboxes uniquely bind observation and recipient and retain a
recipient/time/ID paging index. Service proof owns historical identity checks, so
mutable resource deletion never rewrites immutable recorded facts.

GORM Migrator APIs repair bounded partial tables before migrating the frozen
structs; portable byte-exact window/level/generation constraints avoid collation
assumptions. Signed-int64 counters, positive denominator and coherent timestamps
are constrained. Released steps 1–81 are unchanged. Registered fixtures cover
empty/upgrade/repeat/concurrent/partial MySQL DDL, constraints/indexes, actual
native settled use, retained rotation, recipient privacy, rollback and restart.
The exact original 156-case registry remains in order, with two new cases appended
to 158, retained within the current 162-case registry. The complete current
PostgreSQL/MySQL matrix passes, including migration lifecycle and constraint
checks; receipts are recorded in [Implementation](IMPLEMENTATION.md).

### V83/V84: Project and Team aggregate rolling warning episodes

Private frozen V83 adds Project rolling state, observation and inbox tables;
V84 adds the equivalent distinct Team aggregate tables. Both use frozen schema
structs and GORM Migrator APIs for bounded partial-DDL repair, portable indexes
and constraints. Prior frozen implementations remain unchanged, including the
separately reviewed V82 implementation. The containing verified phase releases
V82–V84; later migrations must retain these frozen steps unchanged.

State retains exact resource birth, window and sampled episode lineage. Unique
observation/level and observation/recipient indexes prevent duplicate fanout;
recipient/time/ID indexes support private paging. Current recipient and runtime
proofs stay in the Service layer, with atomic state/observation/inbox writes.
Historical resource deletion never rewrites recorded identities or samples.

The composed registry preserves all original 158 cases and appends exactly two
Project and two Team scenarios, for 162. Fixtures cover empty creation, upgrades,
repeat/concurrent execution, partially applied MySQL DDL, constraints/indexes,
real journal facts, recipient removal/rejoin/birth, rollback and reconstruction.
The prior Full158 reached its 55-minute deadline and remains failed. Expanded
same-source PostgreSQL/MySQL acceptance now passes all 324 direct scenarios,
eight constraints, 4,813 ordinary named tests and 412 named tests per driver.
The accepted parallel run completed in 2,055.583 seconds; earlier failed runs and
their finite budgets remain historical.


## Locked warning transaction visibility

Personal and Project Key warning observers, plus Personal, Project and Team
rolling observers, explicitly request portable Read Committed transactions.
The governance lock, owner or Project locks, complete retained Key graph and
policy locks remain in place. Calendar/currency publication stays protected by
the existing service lock, and the final runtime pointer, application lease,
identity and accounting proof are checked before commit. A waiter must read the
preceding observer's committed episode state; an earlier Repeatable Read snapshot
can miss that state after waiting on PostgreSQL's governance lock and attempt a
duplicate insert. No duplicate error is ignored and no fixture is serialized to
hide the concurrency. The synchronized regression preserves two real concurrent
observers and exact transaction identity. Source checks and the full real-driver regression pass; source-bound receipts
and separate runtime/browser gates are recorded in [Implementation](IMPLEMENTATION.md).


## Corrected R8 candidate contract additions

These additions describe the isolated candidate; complete dual-driver and
composed-main acceptance remain pending. Earlier acceptance records stay
bound to their original source.


### Candidate V85: Project root-Key rolling warnings

Private frozen GORM V85 adds distinct Project root-Key episode state, immutable
observation and original-recipient inbox tables. It retains the released V1–V84
steps unchanged. Own stored five-hour/seven-day Token caps, complete settled-use
coverage, exact root/Project births and sampled 80%/90% episodes remain separate
from inherited caps and Project aggregate balances. State, observation and inbox
fanout share one transaction. The current migration/lifecycle fixture projection
retains the exact full V87 ledger and all unaffected suffix timestamps; historical
V85 reconstruction stays V85 and preserves V86/V87 suffix rows.

V85–V87 are prepared candidate migrations, not released by this adoption packet.
Actual same-source PostgreSQL/MySQL Full168, current composed main gates and
separate controlled process/browser acceptance remain pending.

### V86 candidate: recorded Model configuration timestamps

A private frozen GORM schema adds only nullable `models.config_updated_at` with
microsecond precision and no default or automatic timestamp behavior. Existing
rows stay null. Prior numbered versions 1–85 remain byte-identical. `HasColumn` and
`AddColumn` allow a partially completed MySQL DDL step to resume without replacing
recorded values; there is no startup migration against the current Model entity.

Configuration writers explicitly stamp within their existing transaction only
when configuration changes. New Model births retain the released millisecond
precision shared by both databases; configuration updates use the new portable
microsecond column. Rollback preserves both the prior configuration and time.
The field is excluded from entity-based historical receipt/proof JSON and is
exposed only through explicit administrative DTOs.

Two candidate scenarios append to the exact original 164-case registry, giving
166. Migration coverage includes legacy nulls and original identities/names,
repeat/concurrent migration, a ledger interruption with the column already
present, nullable writes and exact microsecond SQL readback. Lifecycle coverage
includes six configuration writers, no-op/replay/rollback preservation, independent
catalogue/statistics permissions and UTC persisted logical counts. Complete real-driver matrix acceptance remains pending. The focused R7
PostgreSQL/MySQL receipt is separate; resource-free checks establish no migration
or release acceptance.

### V87 candidate: routing application observations

V87 appends `runtime_routing_applications` using one private frozen GORM schema.
It follows candidate V85/V86 without changing released steps. A unique
`(instance_id, snapshot_id)` index prevents repeated polls from replacing the
first observation. A process/time index supports bounded history reads; recorded
birth, route publication and application observation use microsecond precision.
A digest-length constraint complements the service's exact lower-case digest
and canonical ID validation. No bootstrap material, process token, Key hashes or
runtime secrets are stored in this table.

The table retains historical process observations independently of current
registrations. It intentionally has no cascading relationship to instances;
missing or changed registration births render current liveness unknown and can
never authorize a positive current-state match. Existing V7 publications cannot
supply exact process identity and are not backfilled. Original publication and
instance rows, their timestamps and the complete earlier ledger remain unchanged.

The recorder locks the exact registered process row, validates its private token
and birth, and inserts only under a successful current routing capture. It never
upserts an alias or overwrites a prior row. Persistence errors leave evidence
unknown without changing published runtime or revocation. Partial MySQL DDL is
reentered through the bounded frozen migration helper; no transactional DDL
rollback assumption is made. New migration fixtures cover empty table creation,
legacy preservation/no fabricated history, repeat/concurrent startup, partially
applied indexes, unique/constraint checks and unchanged recorded values.

V87 remains a candidate. Complete Full168, controlled process restart and
composed-main gates are root-owned pending acceptance. The focused real-driver
receipt is separate and does not establish complete integration or release.

### Database-layer exact-table index drop exception

The pinned `gorm.io/driver/postgres` v1.6.2 migrator emits
`DROP INDEX CURRENT_SCHEMA().<quoted index>` for an unqualified table. PostgreSQL
rejects that syntax; the driver's explicit table-schema path instead binds the
schema as a value where an identifier is required. The corrected
`database.DropIndex` adapter is confined to the database layer. Its PostgreSQL
path uses GORM's statement/migrator parser for model names, table overrides and
configured naming strategies, then a value-bound native catalog lookup to resolve
the exact table namespace and index membership through the native search path.
Same-named indexes on another table or in another namespace cannot satisfy that
lookup. Conflicting qualified index schemas, malformed identifiers, missing
membership and lookup failures stop before DDL. Namespace and index identifiers
are quoted as individual GORM clause components. The MySQL path retains GORM's
existing table-scoped migrator.

GORM cannot express the corrected PostgreSQL relation/index resolution and DDL
through its portable index-drop API, so this is a justified database-layer SQL
exception. Catalog values are bound; identifiers are quoted. It introduces no
dialect branching in services or handlers and changes no released migration.
The runtime application migration fixture uses this adapter while preserving its
original partial-index reentry assertions. Pure supported-driver adapter tests
and the separate focused real PostgreSQL/MySQL fixture receipt cover this change;
complete Full168 and final composed-main gates remain pending. The lookup and
DDL are separate statements and do not promise atomicity against concurrent
external schema replacement.

The current frozen R8 source passes check, complete Task testing and build,
5,032 ordinary named Go tests, and the separate two-driver Project warning
lifecycle. Earlier focused R7 receipts remain bound to that source. Complete
R8 Full168, composed-main gates, browser and delivery remain pending. These
source-specific facts do not establish wider runtime or feature acceptance.


### V88/V89: published cleanup commands and exact process closure

Frozen V88 adds distinct published-cleanup command receipts and durable physical
source denial, use and process-generation records. Each request UUID preserves
its immutable original outcome; a permanent physical-object remote claim prevents
a second cleanup effect. Known drain timeouts may permit a newly reviewed UUID
only after complete joins and durable private no-SDK-effect evidence. The private
proof is not a public DTO field, and failed/unknown remote or persistence outcomes
cannot establish it. Released V1–V87 steps are unchanged.

Frozen V89 adds positive closed-process evidence through a private frozen schema
and bounded partial-DDL repair. Admission closes before draining; exact generation,
instance registration/birth/token and all holders must be positively joined. A
new closure rechecks the full fresh UTC clock after governance and instance reads
against lease expiry, then persists one matching UTC microsecond timestamp for
closure and stop facts atomically. Exact durable historical closure reconciliation
retains its original timestamp even after the old lease expires; failed writes
leave admission closed without manufacturing a positive proof. Migration fixture
adapters retain every prior version and unaffected ledger timestamp; historical
reconstruction keeps its original version with an explicit finite suffix.

Fresh main checks, complete Task/build and the source-bound Full172 matrix pass
on PostgreSQL and MySQL, including 344 direct scenarios, eight constraints and
all 434 balanced names per driver. Empty creation, retained-data upgrades,
repeat/concurrent startup and partial-DDL paths remain in the complete registry.
The same executable also passes controlled local shutdown and two real restarts.
See [current acceptance](IMPLEMENTATION.md#current-vault-main-integration-2026-10-09)
for the exact source boundary. Browser, external Vault and fleet acceptance are
separate; older isolated results are not substituted for these main gates.

## Immutable routing-weight history (V90)

Immutable migration V90 defines routing-weight history
for private frozen history/command schema structs. Complete snapshot bytes use
GORM's portable binary mapping so the 512 KiB bound remains valid on PostgreSQL
and MySQL. Business services do not branch on driver dialect. Retained versions
are validated as bounded typed canonical data with exact rows and digest checks;
arbitrary JSON is never exposed as administrative metadata.

Source tests cover empty creation, upgrade with existing catalog data, repeat
execution, concurrent startup and relevant constraints, including binary payloads
beyond 64 KiB and at the 512 KiB boundary. Verification and delivery evidence
remain recorded separately from this schema contract. Released V1–V89 migration
steps remain unchanged.


### V91: Provider enablement and shared review revision

A private frozen GORM schema adds `providers.enabled` (non-null, default true)
and `providers.e_tag` (non-null, 30 characters, default `0`). Existing Providers
remain enabled when the column is added; already present false values and
revisions are preserved. Bounded HasColumn/AddColumn steps validate the resulting
shape and allow reentry after partially applied MySQL DDL. There is no handwritten
production SQL or change to released V1–V90 steps. Name and status writes advance
the same revision. A changed status and its typed before/after/reason audit persist
atomically; no child state, grant, price, weight or routing history is rewritten.


### V92: Connection transport generation and bound evidence

Private frozen GORM schemas add four non-null, 30-character proof columns, each
with the literal default `0`: `provider_connections.transport_generation`,
`provider_credentials.verified_transport_generation`,
`provider_models.capability_transport_generation` and
`reservation_bounds.transport_generation`. Migration V92 uses bounded
HasColumn/AddColumn steps and validates the resulting type, length, nullability
and default. It can reenter partially applied MySQL DDL; it does not assume
transactional DDL rollback or alter released V1–V91 migrations.

Generation `0` preserves compatibility between an unchanged legacy Connection
and its existing evidence. It does not verify a pending Credential, configure an
absent capacity attestation or create discovery/coverage history. Existing status,
declarations, timestamps and relationships remain stored. A changed canonical
Base URL/protocol/adapter/API-version tuple receives a new durable `rev_` generation
and shared metadata revision; returning to an earlier tuple receives another
generation. Name-only changes do not advance the transport generation. The tuple,
generation and typed before/after/reason transport audit commit atomically. Child
states, grants, prices, weights and immutable call/routing history are unchanged.

Verification, capability and capacity writes bind their evidence to the reviewed
current generation. Azure deployment coverage also binds that generation in its
existing identity proof. Old declarations remain historical facts rather than
current evidence after a transport change; status-only writes never reattest them.
The transient Connection diagnostic adds no result/history schema and writes no
catalogue, configuration, verification, discovery, enablement, routing or audit
facts. Required existing Vault source-use/exposure and drain-safety bookkeeping
still applies; this is not a zero-database-write guarantee.


## Existing-member OIDC and root inventory V3 (V94)

Frozen GORM V94 adds `oidc_providers`, `oidc_bindings` and `oidc_ceremonies` and six
additive provenance fields on Sessions/MFA challenges. Their physical `oidc_*`
names are explicit in both frozen migration and business models: GORM's initialism
normalization must not select a different persisted contract. Independent literal
name tests accompany model-to-schema parity checks. It uses private frozen schemas,
GORM table/column/index/constraint operations and the existing database-layer ordered
index metadata adapter. No business layer branches on PostgreSQL/MySQL dialect.
Existing local authentication retains blank primary metadata and null identity births.

Resumable DDL validates required/null columns, exact widths/types, microsecond timestamp
precision, uninvented time defaults, primary keys and ordered complete unique indexes.
The singleton and primary-method checks reject collation aliases. V94 appends root
inventory V3/eight domains while preserving V1/five and V2/seven definitions and rows;
released V48/V72 remain unchanged. A failed step must not acquire a version ledger row.
The official complete PostgreSQL/MySQL matrix passes the V94 creation, upgrade,
repeat, concurrent, partial-DDL and constraint cases, along with authentication
and same-source restart scenarios. Separate process lifecycle acceptance also
passes. Browser and real-provider acceptance remain open; see [OIDC](OIDC.md).

## Existing-member custom OAuth and root inventory V4 (V95)

Frozen GORM V95 adds independent provider, typed-subject binding and hash-only
ceremony tables, plus five explicit OAuth provenance fields on Sessions and MFA
challenges. It replaces primary provenance and inventory checks through GORM,
keeping released V1–V94 immutable. Nullable microsecond births are never backfilled;
local/OIDC data retains blank/null nonmatching OAuth proof.

Exact-empty checks use portable `OCTET_LENGTH(...)=0`; exact primary discriminants
also validate byte lengths and ASCII values. MySQL padding semantics cannot turn
whitespace-only required blanks or padded methods into valid proof. Retained bad
rows fail migration rather than being silently rewritten. Valid rows are reviewed
before CHECK replacement; interrupted MySQL DROP/CREATE can resume without a
ledger row or assumed transactional-DDL rollback. Column/index/type/default/precision
validation remains mandatory.

`MigrateThrough(ctx, db, lastVersion)` shares the normal connection, advisory lock,
timeout, ledger and apply loop. It accepts only a finite supported prefix, rejects
zero/negative/above-current bounds, and rejects an existing newer/invalid ledger
before schema DDL. It cannot downgrade or inject arbitrary steps. Historical V94
tests use independent literal schema projections and this bounded tooling seam,
then restore full-current migration. Business services still receive a ready DB.

Inventory V4 appends OAuth as domain nine, preserving exact V1/V2/V3 history.
The official complete PostgreSQL/MySQL matrix passes all 188 business scenarios
and four constraints per driver (450 named passes each, no failures or skips),
including V95 empty/upgrade/repeat/concurrent/partial-DDL, exact provenance checks,
root-key rotation and same-source OAuth restart. Complete Task and the separate
real-process authentication lifecycle pass. Browser and real-provider acceptance
remain open. See [OAuth](OAUTH.md).

## Existing-member LDAP and root inventory V5 (V96)

Frozen GORM V96 adds an initially unconfigured disabled `ldap_providers`
singleton, `ldap_bindings` with exact retained identity and domain-bound digest,
and five LDAP proof columns each on Sessions and native MFA challenges. It
supersedes primary-method checks with four exact local/OIDC/OAuth/LDAP arms;
nonmatching proof columns must remain byte-empty or null. No historical member
birth or primary proof is guessed or backfilled. The unconfigured singleton has
an empty identity attribute; configured identity must be `entryUUID` or
`objectGUID`.

Private frozen models and GORM Migrator operations own schema changes. Existing
V1–V95 steps remain unchanged. Retained rows and bounded column/index definitions
are validated before constraint replacement; interrupted MySQL DDL can resume
without a version ledger row or assumed DDL rollback. Failed migration never
publishes V96. Current-schema tests retain independent V95 literal projections
and the exact earlier migration prefix assertions.

Root inventory V5 appends LDAP as domain ten. Earlier versions retain their
five/seven/eight/nine-domain meaning; their completed records do not prove V5
retirement. Every retained nonempty LDAP service-password envelope, including
disabled configuration, is included using `ldap:ldap:<generation>`.

The V96 integration registry appended LDAP migration, lifecycle and same-source
process restart scenarios, bringing that historical business inventory to 191.
Controlled PostgreSQL/MySQL and real-process restart qualification pass; see
[LDAP](LDAP.md). The V97 inventory and acceptance are recorded below.

## Existing-member SAML application (V97)

Frozen GORM V97 adds four tables: the initially disabled, unconfigured
`saml_providers` singleton, `saml_bindings`, `saml_ceremonies` and
`saml_assertion_receipts`. Each Session and native MFA challenge gains five SAML
proof columns: binding ID, binding creation time, configuration revision, policy
revision and member creation time. Primary-method constraints admit the exact
SAML arm while requiring all other methods' proof fields to be empty or null;
local, OIDC, OAuth and LDAP provenance remains independently checked. Existing
members and historical Sessions receive no inferred SAML identity or proof.

Private frozen models and GORM Migrator operations define table, column, index
and constraint changes. V1–V96 remain unchanged. Retained data and frozen column
and index definitions are validated before replacing primary-method checks;
interrupted MySQL DDL is replayable without assuming rollback. The V97 ledger
entry is written only after the complete migration succeeds.

Bindings have unique member and issuer/subject-digest identities, with exact
issuer and persistent subject bytes retained privately. Ceremonies uniquely bind
request ID and independent browser proof hashes to provider/configuration/policy
and admitted actor facts. Governance transactions serialize bounded admission and
one-use consumption. Live ceremonies and assertion receipts each have a 1,024-row
admission cap; at most 128 expired rows are pruned per admission. Assertion receipt
expiry covers the full signed proof and survives configuration changes and binding
deletion, so those operations cannot reset replay protection.

Only public IdP certificate material is stored. Root inventory remains V5 with ten
domains; no SAML root-secret domain is introduced. V97 empty/upgrade/repeat,
concurrent startup, interrupted DDL, constraint/index, signed application lifecycle
and restart controls pass the original complete PostgreSQL/MySQL integration
matrix: 194 business scenarios plus four constraints and 467 balanced names per
driver. The separate real-process authentication lifecycle also passes. See
[SAML](SAML.md) for the evidence and open browser/external IdP boundaries.
