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
The opt-in native command-to-PostgreSQL gate below separately qualifies the
controlled seeded producer; actual API/Vault cleanup remains unqualified.

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


## Opt-in native command-to-PostgreSQL test

The maintained `TestCleanupEvidenceCLIPostgresIntegration` passes its first
controlled native Linux/PostgreSQL gate on 2026-10-18. It requires all four tags
`integration,routex_cleanup_evidence,routex_cleanup_evidence_cli,linux` and an
actual Linux process. Missing/invalid prerequisites fail; there is no skip or
in-memory database fallback. A host test invocation that excludes the Linux file
is not this gate. Default Task/integration selectors remain unchanged.

Root provisions one fresh Compose project using the existing `compose.test.yaml`
PostgreSQL service and independently reviewed current image. A single owned Linux
runner joins that project's network, where the sole database service resolves as
`postgres:5432`. Before the test, root creates ONLY these empty databases and
owners, using two distinct new private passwords:

- `routex_f28_normal_<run_id>`, owned by `routex_f28_normal`;
- `routex_f28_f02_<run_id>`, owned by `routex_f28_f02`.

The runner's real/effective UID must equal the same current-host positive nonroot
UID bound into both commands. Root separately builds two fresh native Linux
commands from the exact reviewed helper/product source; each uses all four `-X`
bindings above, with the same application-source/run/UID and respectively
`BuildPhase=normal` or `BuildPhase=f02`. The test performs no Go build, role or
database creation, privileged provisioning or remote operation. It verifies the
exact database/principal and an empty schema before applying the existing
released `database.Migrate` and finite SQL seed. Existing `routex_test` library
integration guards and their nine-node scenarios are unchanged.

Provide exactly nine fields in a native Linux protected configuration file:

```json
{
  "source_commit": "ROOT_MUST_PIN_QUALIFIED_APPLICATION_SOURCE",
  "run_id": "ROOT_MUST_GENERATE_FRESH_32_HEX",
  "host_uid": 0,
  "normal_password": "ROOT_MUST_SUPPLY_PRIVATE_NORMAL_PASSWORD",
  "f02_password": "ROOT_MUST_SUPPLY_DIFFERENT_PRIVATE_F02_PASSWORD",
  "normal_binary": "/fixture/normal/cleanup-evidence",
  "normal_sha256": "ROOT_MUST_PIN_FRESH_NORMAL_BINARY",
  "f02_binary": "/fixture/f02/cleanup-evidence",
  "f02_sha256": "ROOT_MUST_PIN_FRESH_F02_BINARY"
}
```

These placeholders deliberately refuse. Source/run are canonical lowercase
40/32-hex; SHA pins are lowercase64-hex; passwords are distinct32–128 URL-safe
ASCII. This is test-only input, never a new production connection option. The
configuration is regular0600/link1, at most16KiB, owned by the actual runner UID;
each binary is regular0500/link1, at most64MiB, owned by that UID. Every path is
absolute/canonical with no symlink ancestors and a final owned0700 parent.
Root must arrange native Linux ownership; a host bind's UID mapping is not a
fallback. Configuration passwords never enter command arguments, environment,
Git fixtures or logs. Only the nonsecret configuration pathname is an environment
input. As with the command, clearing buffers is not secure Go heap erasure.

Root first compiles the test binary from the exact same reviewed source, with
Linux GOOS/the target GOARCH, the three explicit feature tags and `go test -c`
for `./scripts/provider-credential-cleanup-evidence`. Do not execute it on Darwin.
For the current arm64 target, a CGO-disabled native test run has no race
instrumentation; it is distinct from host race evidence. Inside the fresh owned
runner, the exact selector is:

```sh
ROUTEX_TEST_CLEANUP_EVIDENCE_CLI_OWNED=1 \
ROUTEX_TEST_CLEANUP_EVIDENCE_CLI_CONFIG=/fixture/private/cli-test.json \
/fixture/cleanup-cli-sql.test \
  -test.run '^TestCleanupEvidenceCLIPostgresIntegration$' \
  -test.v=test2json -test.count=1 -test.timeout=180s
```

Root captures the ORIGINAL exit and complete GoJSON using the pinned
`go tool test2json -t -p github.com/miclle/routex/scripts/provider-credential-cleanup-evidence`
converter around that actual native test process, together with build/source,
UID/network/full-container IDs, original joins and independent resource closure.
The source declares18 named nodes and15 original command invocations on complete
success; these are expectations, not observed passes. No absence of the named
test, partial output, fail, cancellation, timeout or missing terminal qualifies.

Both phase commands receive exactly the existing23-key input through an actual
stdin pipe followed by EOF. Each original command gets15seconds, then only its
owned original process handle may be killed and joined for at most5seconds;
remaining test pipes have a2-second WaitDelay. Original PID/PGID, exit, interruption
and process-group absence are recorded without claiming a full child census.
Any unknown original join/group closure prevents further command launches and
preserves altered fixtures. Stderr is capped at4KiB and never printed; stdout is
capped at16KiB. Only an exact safe expected output (including its final LF) is
retained as base64 in GoJSON. Unknown output is not logged. Negative original
exit1 remains1; it is not rewritten to successful command execution.

The source checks exact acknowledged/pending eleven-field tuples, expired
pending's unchanged original claim, and unchanged before/after retained SQL for
every command. It refuses foreign source/run, wrong password, microsecond birth,
missing exact claim audit and the opposite phase's durable history. Expected
safe output is constructed directly from the fixed fixture contract, without
calling the projection under test as its oracle. The acknowledged seed adds only
its exact observe audit. SQL fixture changes/restorations belong solely to the
controlled test. Fixed historical fixture timestamps and IDs are synthetic;
replacing their source/run labels does not manufacture fresh runtime histories.

The fresh R2 run passes 18/18 named nodes with 15 original command invocations:
three exact safe outputs succeed and twelve negative commands retain expected
exit 1. Both phase binaries and the test/converter binaries were freshly built
for Linux arm64 from the reviewed helper source plus this test, with application
source binding `679d8ea`. The real runner is nonroot UID 501 with read-only
root/storage; root provisions ownership in a fresh dedicated Linux volume before
launch. All original joins and recorded resources/ports close. The earlier R1
bind-mount ownership refusal remains failed and is not counted as acceptance.
Complete GoJSON SHA-256:
`a7a4cbc0c904ebd4331bf776d7d005aa10ae646ed049a3d7d3e11a54660af910`.

This gate qualifies actual command production of seeded SQL evidence only.
`runtime_acceptance` stays false. It does not qualify original API cleanup,
normal remote acknowledgement/F02, Session/restart/identical replay, real Vault
HTTP204/audit/metrics, source-process fleet closure or global F28 completion.
The actual helper build/manifest and qualified application-source binding must
remain separate; no source label authenticates a binary by itself.
