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
makes duplicate/ambiguous results fail closed. Exact comparisons check Personal
owner/Key/linkage, recorded birth and call timing, successful statuses, native
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
