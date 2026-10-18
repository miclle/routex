# Controlled PostgreSQL cleanup evidence command

This is a private PostgreSQL capability around the portable parent library.
It is not a production RouteX command, cleanup authorizer, schema installer,
remote client or completed F28 acceptance. It takes no arguments and reads one
bounded JSON object from native Linux stdin. There is no input file path,
endpoint, DSN, driver, database, user, SQL, historical-state or output-path option.

All four linker bindings are required and invalid by default:

- `main.BuildSource`: canonical full application-source SHA;
- `main.BuildRunID`: fresh32-hex run ID;
- `main.BuildPhase`: exactly `normal` or `f02`;
- `main.BuildTrustedHostUID`: canonical positive non-root decimal UID.

Actual real/effective UID must both equal that compiled UID. No UID fallback,
CHOWN, SETUID, privileged staging or unsupported-platform reader is supplied.
Compiled `normal` derives library phase `acknowledged`; compiled `f02` derives
`pending`. The caller cannot select a different durable state in JSON. Source
and run in the input must match the compiled bindings exactly.

BuildSource/input source_commit are the caller's declared application-source
binding; neither SQL nor a string linker value authenticates the actual binary
source. Root must independently record the application checkout/qualification,
actual helper commit and full helper/product manifests, fresh binary/build,
database image/fullID/network and native original process/UID receipts. A new
F28 helper build must not be described as built from untouched0c. A future
reviewed mixed-source wrapper may bind the qualified application and new helper
commit separately. This command adds no fields/schema for that future wrapper
and no current-source qualification flags.

## Canonical secret input

Exactly these23 flat JSON keys are required; duplicates, case aliases, unknown
keys, nulls, malformed UTF-8, trailing objects and input beyond16KiB refuse:

```
source_commit, run_id, database_password,
creation_request_id, request_id, actor_id, actor_birth,
integration_id, integration_birth, revision_id,
credential_id, credential_birth, creation_birth,
creator_process_id, creator_generation, creator_birth,
physical_object, operation_proof, reviewed_etag, reason,
run_started, as_of, root_epoch
```

The independently captured identity, birth, audit/source chronology, observation
interval and exact intent requirements remain those of the parent library.
Phase, connection options and secret-bearing product rows are never accepted.
The controlled password is32–128 URL-safe ASCII characters; do not put it in
argv, environment, a test fixture in Git, logs or notes. The source-owned
coordinator must independently validate its protected input bytes before
streaming them once into the exact original process. This command duplicates
no protected file reader and does not authenticate the upstream stdin sender.

Linux accepts only a pipe/socket stdin descriptor, uses nonblocking polling for
at most 5 seconds and requires EOF. Empty input, open partial input, errors,
overflow and unknown flag restoration refuse. Read buffers are cleared on
failure; the original stdin flags are restored. The raw input and explicit
decoded password field are cleared before database opening. Go JSON/driver
string allocations are not a secure heap-erasure guarantee.

## Fixed database and safe output

Connection is only `postgres:5432`, with user `routex_f28_normal` and database
`routex_f28_normal_<compiled run>` or user `routex_f28_f02` and database
`routex_f28_f02_<compiled run>`. The application_name is fixed by phase/run.
SSL is disabled only for this disposable private Compose namespace; connect
timeout is2seconds. Inherited `PG*` variables are rejected, including service,
pass-file and options defaults. No environment connection override is admitted.

GORM logging is discarded, error translation enabled and automatic ping disabled.
One connection with zero idle connections is allowed. Read work has a shared
three-second context, including parent `Project`, its checked rollback,
a separate fixed read-only RepeatableRead namespace-identity transaction with
checked rollback, followed by explicit pool closure. `database/sql.Close` has
no caller-context argument; pinned pgx stdlib `Conn.Close` has its own 5-second
context. This is not an overall three-second wall-time guarantee. Context is
checked after pool closure and output; the original process supervisor must
independently bound total execution/closure. Namespace identity uses the sole
fixed PostgreSQL `current_database()`/`current_user` query: GORM has no equivalent
portable expression API. No migration, AutoMigrate, write probe, schema mutation,
service operation or Vault/HTTP request runs here.

Errors emit only `{"result":"cleanup_evidence_rejected","runtime_acceptance":false}`
and exit1. Successful output occurs only after valid library projection, both
rollbacks, context and actual pool close, and has exactly result,
database_capability=`postgres_private_controlled`, runtime_acceptance=false and
the parent's safe `evidence`. The nested exact eleven-field `evidence.claim` can
feed the existing R3 `freeze_pending_claim` after independent original503/join
and closure proof. Expectation, private rows, reason, operation proof, review
token, password, DSN, SQL and driver error payloads are never serialized.
Short/failed stdout or context expiry during output cannot produce exit0;
retain original exit and full output even when some safe bytes were written.

## Qualification scope and remaining recipe

Root host race qualification passes the portable command controls and Darwin
unsupported-platform refusal, together with the parent library: 76 named nodes
pass, without fail, skip or race reports. A fresh Linux arm64 test binary also
passes all three native stdin controls in an owned offline nonroot container;
the original exits zero and the container is removed. That native test binary
has no race instrumentation. Parent-library seeded PostgreSQL and MySQL tests
separately pass nine named race-test nodes per driver, including actual read-only
write refusal and rollback. Those library tests do not invoke this command.
No actual PostgreSQL CLI/native producer is qualified yet.

After root source review and release of the exclusive validation lane:

```sh
go test -race -count=1 ./scripts/provider-credential-cleanup-evidence/...
go tool task check
```

Root must build a fresh native Linux binary with all four reviewed `-X` bindings,
then qualify real stdin/UID/fixed namespace, actual pending/acknowledged SQL,
query/rollback/pool-close failures and original process/resource closure in
owned normal/f02 namespaces. A future coordinator must separately prove safe
server logging, actual API503, fault23514, Vault204/audit/metrics, original
Session restart/identical replay and no additional remote destroy. This command
does not mark normal_receipt, stage_dependencies_ready or run_f02 ready and
does not infer native completion, global acceptance or fleet adoption. MySQL
support is qualified for the parent library's seeded read-only projection;
this first command intentionally has no MySQL adapter.
