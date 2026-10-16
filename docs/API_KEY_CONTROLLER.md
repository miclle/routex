# Explicit API-Key File Delivery Controller

The optional `routex-controller` executable performs one administrator-assigned,
explicitly pinned Vault Agent delivery. It is independent of the RouteX server,
database, credential root key, administrator Session and inference API. The
server remains a single-binary application; this executable is installed only in
an application environment that opts into managed file delivery.

Commit `14a75fe` delivers the standalone controller and enrolled controller routes.
The focused fake-process, HTTP and CLI boundary passes ordinary and race tests;
the containing phase also passes mandatory checks, frontend tests, authentication
restart and the complete PostgreSQL/MySQL regression. Controlled Personal
Vault/JWT/Agent initial delivery, identical report retry, explicit same-slot
replacement and prelaunch revoked-grant output preservation now pass real-process
acceptance with complete owned cleanup. Browser interaction, application use,
Project/fleet behavior and external workload interoperability remain unverified.
See [delivery contracts](API_KEY_DELIVERY.md) and the exact receipt in
[implementation evidence](IMPLEMENTATION.md#execution-continuation-2026-10-16).
A generated configuration, joined Agent or advisory report does not establish
application use, native completion or predecessor retirement.

The explicit replacement extension passed standalone race tests on its original
isolated source. This composition retains its nine code/test leaves with two
equivalent style corrections and
the delivered Money advice and Gateway dispatch-authority checks. Fresh
`go tool task test-controller` and mandatory project checking passed on this
composition after two equivalent style corrections. The original failed
Staticcheck diagnostics remain retained. Controlled Personal replacement-Agent
qualification now passes; native application use remains a separate acceptance
gate.

## Supported operations

`deliver` fetches one assigned configuration, validates its exact slot, descriptor
revision and opaque server configuration digest, launches the locally pinned
Vault 1.21.4 binary once, and submits one advisory report. There is no polling,
automatic reassignment, scheduled rotation, application reload, inference or
fallback. `replace` is a separate explicit operation for an administrator-assigned
successor revision; it does not automate the rotation lifecycle.
After local preparation, a second bounded configuration read reauthorizes the
grant and must match the entire original configuration, including exact HCL.
Denied or changed reads refuse launch instead of substituting an assignment.

The Agent receives these fixed arguments:

```text
vault agent -config=<private-state-directory>/agent.hcl -exit-after-auth=true
```

The finite-mode contract is grounded in the pinned Vault v1.21.4 source:

- `command/agent.go` registers `exit-after-auth` and waits for template completion
  before finishing the sink worker when templates exist.
- `command/agent/template/template.go` stops the template runner and completes
  its worker after every template has rendered in exit-after-auth mode.

These source facts establish the intended lifecycle. The controlled Personal
real-process acceptance records actual completion separately; parser acceptance
and fake-Agent tests cannot establish other workload compatibility.

The controller accepts only the exact generated HCL grammar: one Vault address,
an optional namespace, one JWT method and one guarded template. It rejects
additional blocks, executable hooks, token sinks, altered paths or permissions,
backups, missing-field guard changes and unversioned reads. The template reads
the exact KV-v2 version `1`. Canonical quoted values and literal structure prevent
server-provided HCL from becoming arbitrary executable configuration.

The configuration digest is a reviewed opaque pin. It binds server-private
identity births and security facts as well as rendered configuration. It must not
be recomputed from public HCL or represented as a locally verified content hash.

## Bootstrap

An administrator first enrolls one exact-slot controller and explicitly assigns
its reviewed current configuration. Store its one-time machine bearer in a
regular `0600` file owned by the controller UID. Never put the bearer in command
arguments, environment variables or the bootstrap JSON.

Provision a private `0700` state directory and the private `0700` output directory
`/var/run/secrets/routex/api-keys`, owned by the controller UID. Supply an
independent regular `0600` workload JWT at
`/var/run/secrets/routex/workload.jwt`. Only Agent reads its contents; the
controller inspects file protection and leaves the JWT unchanged.

The state directory is for exactly one explicit operation. Existing
`operation.json` or `agent.hcl` refuses another launch. Initial `deliver` also
refuses any existing exact-slot output. Each explicit `replace` requires a fresh
private state directory and a reviewed assigned revision greater than one. Keep
earlier state directories unchanged so their original reports remain retryable.
A file's presence never identifies its previous Key, descriptor revision or
application use.

Use an absolute-path regular `0600` bootstrap JSON, owned by the same UID:

```json
{
  "routex_url": "https://routex.example.test",
  "slot_id": "kds_01aaaaaaaaaaaaaaaaaaaaaaaa",
  "descriptor_revision": "1",
  "configuration_digest": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
  "bearer_file": "/run/routex-controller/controller.bearer",
  "state_directory": "/run/routex-controller/operation",
  "agent_executable": "/opt/vault/bin/vault",
  "agent_sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
  "timeout_seconds": 60,
  "allow_loopback_http": false
}
```

Replace the example identities, digest and executable checksum with the reviewed
values. `agent_sha256` pins the operator-approved Vault 1.21.4 binary; the
controller also checks its version using a bounded owned `vault version`
process. The binary must be regular, executable, root/current-UID owned and not
writable by other users; setuid/setgid binaries are rejected. No installation or
binary download occurs automatically.
The executable is bounded at 512 MiB before content verification and during the
streamed checksum read. This accommodates the measured official Linux arm64
Vault 1.21.4 executable (490,358,435 bytes); size alone never proves identity or
authorizes execution. The exact version, approved checksum and all protected-file
checks remain required.
The opened binary's inode and checksum are revalidated after version detection
and immediately before Agent launch. The operator must not mutate the approved
pathname between the final check and execution or rewrite the private HCL file;
this slice does not implement executable-by-file-descriptor platform machinery.

`routex_url` is an origin with no user information, path, query or fragment.
Verified HTTPS is required by default. Environment proxies and redirects are
disabled. `allow_loopback_http` is an explicit local-fixture exception that
permits only numeric loopback addresses, for both RouteX and the Agent's Vault
address; a DNS name resolving to loopback is insufficient. It disables no TLS
verification.

The operation budget is 10–300 seconds. Configuration has no environment
interpolation. Invalid UTF-8, unpaired Unicode surrogates, duplicate members,
unknown fields and over-bound files or HTTP bodies are rejected. Paths are
operator supplied and absolute. Credential/output symlinks and hardlinks are
rejected; ancestor aliases are limited to trusted root-owned system links, and
ancestors must be root/current-UID controlled. Projected symlink-based secret
volumes need an independently maintained protected regular JWT file.

Supported controller process platforms are Linux and macOS. Other platforms
fail closed. The filesystem must support protected regular files, file and directory fsync,
and atomic rename within the output directory; unsupported durability operations
fail closed. The output directory must have only cooperating controller operations
as writers. A shared exact-slot exclusive lock fences `deliver` and `replace`
even across different state directories. It cannot fence an external same-UID
writer; metadata revalidation and rename are not compare-and-swap against one.

## Run and report recovery

Build the optional executable without server/database/frontend dependencies:

```bash
go tool task build-controller
bin/routex-controller deliver -config /run/routex-controller/controller.json
```

For an explicitly assigned successor, use its reviewed revision and opaque digest
in a new protected bootstrap file pointing to a fresh state directory:

```bash
bin/routex-controller replace -config /run/routex-controller/successor.json
```

Replacement opens and retains the current exact-slot protected regular `0600`
inode without reading it. It validates the entire original server HCL before
changing only its one canonical destination token to a private hidden staging
name in the same output directory, derived from the original operation UUID.
Authentication, exact version-one template, permissions and backup prohibition
remain unchanged. No plaintext backup or token sink is created.

After Agent directly joins and its owned group is absent, a third bounded machine
configuration read must equal the entire original assignment. Denial, changed
assignment or cancellation preserves the old output instead of promoting the
stage. This current read is not a distributed lock against a later revocation.
The stage must be a new same-filesystem, owned, single-link regular `0600` file
of 1–4 KiB. The controller fsyncs it and its directory, rechecks its metadata,
and durably saves `promote_pending` with old/stage metadata and unknown output.
It rechecks both held inodes and cancellation immediately before atomic rename
over the exact slot, then fsyncs the directory and checks the installed inode.
Only the subsequently durable report intent may say `written`.

A pre-promotion failure leaves the reviewed old inode untouched by the controller.
An external writer's changed inode is refused and never overwritten intentionally.
Cancellation, unconfirmed Agent closure, uncertain rename/directory sync or a
missing final journal leaves the operation and slot locked for operator review;
there is no automatic restore, stage promotion, Agent relaunch or recovery based
on file presence. A crash after rename but before the final journal remains
unknown, even if the new file is visible. Atomic visibility and fsync depend on
the local filesystem and do not establish application adoption.

The dedicated `go tool task test-controller` command runs the standalone race
boundary suite. These optional Tasks do not change the default server build or
production deployment.

Every launched process receives a private process group and a minimal
environment. stdout/stderr are discarded instead of exposing raw Agent errors.
Cancellation sends TERM, then bounded KILL only to the owned group. A successful
parent exit is insufficient: its direct `Wait` result must be collected and its
group absent. The two teardown grace periods are two seconds each, in addition
to the operation budget. An unjoined or remaining group is an unknown/failed
outcome, never successful delivery. Configuration is removed only after owned
process closure is confirmed.
Unconfirmed closure preserves the exclusive state lock and skips report dispatch
until an operator establishes physical process closure. The lock and operation
records are fsynced; lock acquisition and state replacement use exclusive-create
and atomic-rename operations inside the private state root.

After a successful joined Agent exit, the controller observes only that a fresh
exact-slot regular `0600` file exists, is nonempty and has at most 4 KiB. It never
reads, hashes, logs or transmits the plaintext API Key. This metadata observation
is advisory; it does not validate Key contents or application adoption.

Because the Agent has exited and the machine protocol has no completed state,
the report uses `agent_state: unknown`, not `running`. A protected fresh file may
produce `output_state: written`. A failed/unconfirmed Agent never produces a
written report. Output remains available even when reporting fails.

The controller persists a non-secret operation UUID, exact target pins and
report intent before dispatch. It sends a report at most once per explicit
command. A timeout, lost reply or rejected reply retains the original UUID and
body. Retry only that report:

```bash
bin/routex-controller retry-report -config /run/routex-controller/controller.json
```

`retry-report` does not fetch configuration, inspect the Key file, launch Agent
or claim historical process closure. It preserves the saved revision, digest
and states, even if a newer assignment exists. The server may reject a stale or
revoked grant; rejection cannot justify substituting a current configuration.
An identical confirmed retry remains possible if local receipt persistence
failed. The server's exact report response must match every submitted field and
identify `evidence_kind: controller_report` before local success is reported.
Successful responses also require the server's private/no-store and nosniff
headers, JSON MIME and bounded body. Report timestamps must preserve the server's
UTC microsecond precision. The acknowledgement endpoint returns HTTP 200 with
no configuration ETag; only the configuration GET carries the quoted digest ETag.

The `prepared` and `promote_pending` phases after interruption retain
`unknown/unknown` only. A stranded `controller.lock` or exact-slot output-root
lock refuses automatic recovery or relaunch. Before
manually clearing these locks, an operator must establish that the original owned
processes are absent; this executable does not kill stale stored PIDs or infer
closure after a crash. Replacement also requires explicit inspection of the
stranded slot/stage/journal before any manual slot-lock removal. Report-only retry
never removes a stranded slot lock or infers whether promotion happened. It
preserves delivered output, staging files and operation state.

## Verification boundary

The retained standalone race tests cover the closed configuration/HTTP boundary,
protected-file rejection, binary pins, fixed arguments, minimal environment,
parent/descendant cancellation, pre-existing output, failure states, secret
canaries and exact report-only recovery. Fresh standalone Controller race tests
passed on this composition, including the replacement cases. Fake-Agent file
writes establish these Controller behaviors only.

Controlled Personal real acceptance uses the pinned official Vault 1.21.4
executable and an independent workload JWT. It privately verifies exact version-one
initial and replacement output against distinct version-two sentinels, preserves
the JWT and required file protections, recovers a physically lost accepted ACK
with an identical report-only retry, and rejects a revoked grant before Agent
preparation while preserving installed output. Both operation journals and all
four command traces are retained. Direct joins and independent checks confirm
owned process, container, network, volume and listener closure.

Project slots, additional wrong-subject/audience, missing-field, expiry and
assignment-change cases, native inference/application completion and real external
workload compatibility retain their own acceptance gates. The controlled result
does not claim those cases passed.

The retained replacement cases additionally cover atomic same-slot replacement,
pre-promotion failure and authority/cancellation loss, old-inode and staging-file
changes, shared slot locks across state directories, exact lost-response report
retry without output inspection, and unknown crash windows on both sides of
rename. Mandatory project checking also passed on this composition; neither
check establishes actual Vault/Agent replacement acceptance. Replacement changes no
Key retirement gate: a qualifying successful final native completed call by the
exact replacement Key remains separate and required.

## Reproducible controlled application fixtures

The maintained root-module commands in `scripts/controller-application-fixture`
and `scripts/controller-application-evidence` support a separate finite
application-use acceptance phase. The first serves an authenticated controlled
native peer, invokes the Gateway using the actual Agent-installed slot file,
and separately probes a fixed private predecessor file. The second uses a
server-enforced read-only GORM transaction to verify the exact captured Calls,
last Attempts, replacement owner/birth, native markers and Credential/snapshot
attribution. Neither command adds a production API or bypasses retirement gates.

Their ordinary unit tests participate in the root Go package checks. Linux
no-follow, mode 0600, single-link and ownership tests must additionally run on
Linux; non-Linux private input reads fail closed. See the two command READMEs
for focused tests, cross-build commands and the isolated PostgreSQL read-only
integration control. A coordinator must provide pinned binaries/images, private
inputs, owned namespace/processes, finite traces and independently verified
cleanup. Native `length` remains incomplete; only the durable exact replacement
`completed` evidence can satisfy planned retirement. Controlled fixture tests,
actual Agent application acceptance and real external workload acceptance remain
separate evidence scopes.
