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

Current exceptions are the database-level migration locks (`pg_advisory_lock` and `GET_LOCK`) and their release operations. GORM does not expose a portable connection-scoped advisory lock. These operations are contained in the database package, run on the same dedicated connection, and have concurrent-startup tests on both databases. Failed unlocks discard the physical connection to prevent a locked session from returning to the pool. Historical released DDL retains its existing dialect-specific collation and schema behavior for upgrade compatibility.
