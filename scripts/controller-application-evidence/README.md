# Controller application evidence

This maintained acceptance command belongs to the root Go module and is not
part of the production RouteX or Controller executable. It uses the existing
GORM/PostgreSQL dependencies and performs no schema or business mutation.

It accepts no arguments and reads only private
`/fixture-evidence-input/config.json`. The coordinator supplies the controlled
PostgreSQL password, exact owner, replacement/predecessor Key, Model, Credential,
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

The endpoint is the disposable Compose `postgres` service and fixed `routex`
database/user. The controlled password must match the 38-character
`token_urlsafe(28)` fixture grammar. One connection is allowed with a two-second
connect timeout. Read-only transaction commit closes its snapshot; it does not
publish configuration or invoke a business write.

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
this command and SQLSTATE test still intentionally connect only to the controlled
PostgreSQL namespace. They do not establish MySQL runtime application acceptance.

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
