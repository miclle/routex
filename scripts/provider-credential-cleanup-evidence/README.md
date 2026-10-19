# Published credential cleanup evidence

This package is a maintained **read-only library**, not a CLI, cleanup
implementation, topology runner or acceptance result. It supplies the durable
claim/source-history adapter missing from the controlled F28 coordinator.
Existing product services remain the sole cleanup authority.

The first scope is one fresh controlled **published** cleanup command. Orphan
creation failures, failed/unknown durable terminal states, multiple historical
commands for one physical object and fleets larger than32 source processes are
not covered. Missing, ambiguous, foreign or overflowing facts refuse.

## Inputs and behavior

`Project(ctx, db, expected)` borrows a caller-owned ready GORM handle. It opens
one read-only RepeatableRead transaction, bounded to at most3seconds and the
original caller deadline, explicitly rolls back on every post-BEGIN path, and
returns no evidence after query, validation, rollback, context or panic failure.
This library replaces logging with GORM's discard logger and returns only fixed
`ErrRejected`; raw input, SQL and driver error messages are never returned.
It creates no database pool and performs no migration or remote cleanup.
Executing the transaction may acquire a connection through the borrowed pool.
The caller owns and must close the pool; this library never closes a borrowed
pool or claims that closure occurred.

The independently captured `Expectation` binds source/run, original creation
and cleanup UUIDs, actor/integration/credential births, retained auth revision,
creator process identity, physical object/operation proof, original reviewed
strong token/reason/root epoch and the bounded observation interval. Source/run
labels are caller bindings, not identities authenticated by SQL. The caller
must verify the fresh owned namespace, source/binary/build and original process
receipts independently. Never derive expected identities from whatever current
row happens to be returned. No DSN, host, SQL or generic table override exists.

Fixed explicit nonsecret columns are read through GORM from the creation
operation, both cleanup namespaces, current exact actor/integration and retained
revision, absence of the exact Credential, permanent source denial, original
creation-use owner, bounded complete source-process/use history and cleanup
audits. All returned IDs/births are compared exactly in Go after database
collation matching. The cleanup namespace must be published only; an orphan row
or another command for the same creation/physical object refuses. Secret/auth
ciphertext, storage intent, marker material and system-instance lease tokens are
never selected. Reason, reviewed token, operation proof and raw observation/audit
JSON remain private and are omitted from serialized evidence.

`Validate(expected, evidence)` checks retained rows and derived fields. Original
Vault write/read observations must both record success. `pending` requires no
FinishedAt, original empty ownership/not_attempted cleanup, known-no-effectfalse,
remote RequestID exactly equal to the original command, deadline=start+30seconds,
exactly one claim audit and no observe audit. `acknowledged` instead requires
recorded succeeded ownership and cleanup, FinishedAt and exactly one matching
observe audit. Both retain exact creation/command UUIDs, Credential audit resource,
actor and time relationships. Duplicate/missing/case-alias/nested JSON fields,
null booleans, unknown statuses and extra cleanup audits refuse. Every projected
source use binds a retained exact process generation/birth; open registered
processes must have a recorded JoinedAt. Creation precedes the original claim,
and the fresh denial retains that exact claim's StartedAt;
an acknowledged command requires its claim audit and each generation's drain
join or shutdown no later than its original FinishedAt. A later shutdown remains
valid when a timely join already exists. These durable facts do **not** replace
current SystemInstance/lease checks, local holder joins, original process closure,
API authority or remote audit/metrics witnesses.

Expiry changes only `Evidence.PresentedState` to unknown. The original durable
`Claim.State` remains pending, and its StartedAt/Deadline/FinishedAt are not
rewritten. Projected timestamp pointers do not alias retained validation rows.
Normal acknowledged SQL evidence does not prove literal remote HTTP204,
idempotent replay, numeric metrics attribution or complete normal-control closure.

## Exact future coordinator interface

A reviewed future caller constructs the expectation from original run-owned
inputs and passes its own ready DB handle:

```go
value, err := cleanupevidence.Project(ctx, ownedDB, expected)
if err != nil {
    // Preserve failed/unknown original outcome; do not retry a cleanup.
    return err
}
claim, err := cleanupevidence.PendingClaim(expected, value)
if err != nil {
    return err
}
// Marshal only claim into the protected coordinator transport.
```

`PendingClaim` emits exactly the eleven keys consumed by the existing
`OriginalCleanup.freeze_pending_claim` contract:

```
request_id, creation_request_id, state, finished_at, started_at, deadline,
observation_state, observe_audit_count, claim_audit_count, remote_request_id,
known_no_effect
```

The nested `cmd/cleanup-evidence` command supplies a compiled, controlled PostgreSQL/MySQL
stdin/connection caller for the fixed normal/f02 namespaces. Its Linux stdin
controls pass; actual command-to-database qualification remains pending. It adds no
protected file reader or coordinator readiness. The private coordinator must
bind the original503 and owner-joined terminal before
feeding this map, then use its original Session, natural30-second deadline,
same-binary restart and identical token-free replay. It must compare the replay's
original claim tuple and independently prove no additional remote destroy.
This library does not enable `normal_receipt`, `stage_dependencies_ready` or
`run_f02`, nor invent a normal acknowledgement or raw Vault metric grammar.

Relevant current product contracts are
`internal/routex/handler/handler.go` provider-orphans routes;
`service/provider_credential_cleanup.go` claim/finalization/audit;
`service/provider_credential_cleanup_views.go` receipt/expiry projection;
`service/credential_published_cleanup_ledger.go` independent published namespace;
and `entity/credential_source_drain.go` permanent denial/process/use facts.
The route takes the **creation UUID**, while audit resource is the canonical
Credential ID. The original cleanup UUID is distinct and lives in the command
body and command-receipt URL. They are never interchangeable.

## Validation scope

Thirteen top-level library tests cover exact mapping/expiry, normal versus pending semantics, identities and
births, ambiguous namespaces, unknown effects/audits/source history, strict JSON,
private output/tampering, actual pinned GORM embedded-column mapping, original
creation/claim/finish ordering without borrowing later drain facts, and real
in-process `database/sql` transaction/GORM scan behavior using a finite fake
driver. The fake driver has no network or real database. It checks ReadOnly,
RepeatableRead, mandatory rollback including rollback refusal, and caller-owned
pool closure; it cannot establish PostgreSQL/MySQL server behavior.

Root host qualification passes all 76 named library/command race-test nodes,
with no fail, skip or race report. A regression first reproduced GORM opening a
transaction despite a pre-existing caller error; the library now rejects that
handle before borrowing a transaction. The failed regression is retained.
Fresh seeded PostgreSQL and MySQL qualification passes nine named nodes per
driver with race detection, including real read-only write refusal and checked
rollback. Both original workers exit zero; their two Compose containers, one
network and two loopback ports are closed. A freshly compiled Linux arm64 test
binary separately passes all three native stdin controls in an owned offline,
nonroot container, which exits zero and is removed. That native run has no race
instrumentation; the host's portable controls retain their separate race proof.
Run relevant gates serially with the project validation lane:

```sh
go test -race -count=1 ./scripts/provider-credential-cleanup-evidence
go tool task check
```

These SQL fixtures exercise released-schema projection, not an actual API
cleanup operation. Actual source-bound command production, normal acknowledgement,
published F02 fault, API/restart/Session replay, safe server logging,
Vault204/audit/metrics correlation and owned topology closure remain separate
acceptance gates. No F28/A16/global completion follows from this library.
