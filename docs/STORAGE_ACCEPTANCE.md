# Local Native Attachment Storage Acceptance

Status: bounded local native storage drill accepted. The initial composed source
passed mandatory checks, complete ordinary tests and an ordinary production
build. Its first actual local run confirmed authenticated bucket creation,
enabled versioning and anonymous/wrong-secret denial, then failed before native
inference because the Project Key request omitted explicit manual delivery.
That run remains failed; independent process, resource and port cleanup passed.
The successor supplied explicit Project managers and manual Key delivery and
passed fresh mandatory checks, complete ordinary tests and production builds.
Its actual drill progressed through four real PNG/PDF uploads, native rewriting
and owner isolation, then failed the native completion assertion. Both retained
attempts correctly recorded unknown evidence because the controlled peer
omitted the required `object: "chat.completion"` discriminator. The corrected
peer includes that field and retains each exact native response in a private
receipt; the strict completed-evidence assertion remains unchanged. That
successor passed fresh mandatory checks and advanced through six native calls,
application restart and exact version retention, then failed a deletion receipt
comparison. A private diagnostic preserving that comparison proved identical
creation instants and fractions with UTC versus local-offset representations;
the expected pending state and all other target fields matched. The corrected
fixture compares the exact RFC3339Nano creation instant using full fractional
precision while retaining exact target and state assertions. Negative controls
reject nanosecond and microsecond differences. Original failed receipts remain
failed, with independent cleanup proof. Fresh script qualification and a complete
actual drill subsequently passed on the corrected source: fresh mandatory
checking, 23 composed script controls and the actual six-call storage workflow.
Three positively identified application generations cover initial use, retained
Session restart and outage deletion recovery. Independent readback confirms
all 25 workers and four ports closed, exact Compose resources absent and the
original development service preserved. Original recorded versions were
removed, later versions retained and delete markers absent. An initial private
readback used a stale two-generation expectation; its false receipt is retained
separately from the corrected three-generation evidence. This does not
supersede the accepted historical
storage-administration evidence in [Storage](STORAGE.md#controlled-local-versioned-storage-acceptance),
or mark F20/F27 or any external-service scenario complete.

## Fixture boundary

`scripts/storage-acceptance.compose.yaml` defines an isolated PostgreSQL database
and an actual S3-compatible RustFS server. The RustFS alpha release is pinned to
an inspected multi-platform manifest, including Linux arm64 and amd64. It is a
disposable test dependency, not a production storage recommendation. Both ports
bind only to loopback; fresh credentials and a unique Compose project are
mandatory. Storage uses fresh project-owned volumes so real versions survive the deliberate
stop/start outage; the database uses a temporary filesystem. Neither reuses the
development Compose project or any existing database. Container health and filesystem
permissions are validated during the actual isolated launch.

`scripts/storage-acceptance-fixture/main.go` uses the already pinned S3 SDK to
prepare a fresh random bucket, enable versioning and read back that setting. It
requires a literal HTTP loopback endpoint, one bounded 60-second operation
budget, five-second requests, no SDK retries or proxies, and a new receipt in a
physical private directory. It checks anonymous and incorrect-secret access
against the real server. Its receipt remains pending after an uncertain result;
never infer successful setup from an existing bucket or a container health check.
It does not start Compose, launch RouteX, create an attachment or make inference
requests. The test-only SDK permits bucket administration; RouteX's production
object-store client continues to expose only bounded object operations.

## Next controlled run

The controller source is implemented in `scripts/storage-native-acceptance.mjs`,
with the `scripts/test-storage-native.sh` entry point. Its review and execution
must establish
all of the following before extending the acceptance claim:

1. Pin the exact Git base, complete tracked/untracked source and modes, frontend
   build inputs/output, Go toolchain, controller/helper binaries and production
   executable SHA-256. Build the embedded frontend and ordinary production binary
   from that exact source after the mandatory checks and relevant tests pass.
   Neither a stale binary nor development reverse-proxy assets qualify.
2. Create a fresh private artifact directory, unique project, random bucket,
   disposable credentials and loopback ports. Capture intent before Compose
   dispatch, container IDs/image digests, owned process PID/group births and every
   acquired resource. Preserve secrets and response bodies only in private
   artifacts, and report sanitized status/assertion summaries.
3. Build and invoke the fixture helper against that owned endpoint. Independently
   confirm enabled versioning, anonymous and wrong-secret denial, and private
   bucket ownership. Start RouteX against the fresh database with explicitly
   enabled private storage/upstream policy and a private controlled native peer.
4. Through normal authenticated APIs, initialize accounts, configure and verify
   storage, create the controlled Provider/Credential/Model, explicitly declare
   image/PDF capabilities with the current review token, enable the Credential,
   configure complete weights and grants, and create/confirm scoped Keys. Never
   substitute database seeding for those product operations.
5. Upload real valid PNG/PDF bytes through Personal and Project Session endpoints.
   Verify captured hashes and recorded S3 versions. Invoke supported native
   image/PDF scalar references and assert the peer receives the exact resolved
   content. Preserve opaque text/tool data, Project versus personal ownership,
   current-manager authorization, cross-scope denial and no upstream dispatch on
   denied resolution. Keep controlled peer evidence separate from real external
   Provider acceptance and browser upload proof.
6. Independently write a later S3 version for selected exact object keys, retain
   its version ID and digest, and show RouteX reads the originally recorded
   version rather than the mutable latest object. Reconcile call/attempt facts,
   original scope and Session persistence across a new production process.
7. Stop and positively join RouteX before any test-only GORM intervention. In a
   separately compiled, exact-source helper, constrain the dedicated database,
   exact object IDs, immutable scope and prior timestamps. Age only those test
   rows' `created_at` and `next_cleanup_at` consistently; do not change product
   TTL, clocks, state, version, ownership or production code. Re-read the changed
   rows, retain before/after receipts, then restart the ordinary production binary.
   Verify expired metadata/content/native access is denied and cleanup removes
   only the original recorded versions, while independent later versions remain
   readable with matching digests and no delete markers. This is a constructed
   expired-state regression. Natural one-hour elapsed-time acceptance is a
   separate optional run and must retain its actual wait/deadline.
8. Close and join each exact owned application/peer/helper/Compose operation.
   Establish PID and process-group absence, closed and freshly bindable ports,
   and absence of the exact project's containers, networks and volumes. A
   cleanup timeout or unproved ownership leaves artifacts and fails the run;
   never scan/kill unrelated processes or delete another project's resources.
   Preserve the accepted source hashes and successful original assertions. Failed
   runs remain failed and any changed successor receives new source binding.

Use separate finite work, HTTP, startup, expiry and cleanup budgets. Cleanup must
remain possible after a work deadline or cancellation and must not turn a failed
assertion into success. The current full PostgreSQL/MySQL matrix and other heavy
checks must finish before launching this fixture. This controller is not connected to Task or CI until its source and owned cleanup
are reviewed and qualified. Constructed expiry and natural-hour elapsed proof
are separate pending increments; the present controller uses normal deletion
during an actual local storage outage, then real restart recovery.

## Controller inputs and receipts

The controller requires absolute regular executable paths in
`ROUTEX_ACCEPTANCE_BINARY` and `ROUTEX_ACCEPTANCE_HELPER`, plus the private
`ROUTEX_ACCEPTANCE_QUALIFICATION` JSON receipt. The helper is compiled from
`./scripts/storage-acceptance-fixture`; the application must use the ordinary
production build with the exact embedded frontend. The receipt must provide
`source_digest`, `binary_sha256`, `helper_sha256`, `assets_digest`, and
`mandatory_check`, `ordinary_tests`, `production_build` values of `passed`.
The final three values require independent original command evidence; the
controller compares identities but does not recreate that prior qualification.

After separately building and qualifying those executables, the entry point's
`--capture-inputs` mode writes source/asset censuses and executable hashes into a
new private directory. It neither starts services nor claims qualification; use
its identities alongside the original successful command receipts. Source
includes tracked/untracked nonignored files, permission modes and exact symlink
target identity without dereferencing outside content. Embedded
assets receive their own complete recursive census. The build root must be a
physical directory; every nested file must be regular, symlinks are rejected,
and the exact `website/build/index.html` must be present. The actual run repeats
the source/assets captures and re-admits both executables after cleanup. Any
source, asset, application or helper byte change, or executable admission
failure, rejects the final qualification rather than retaining the old hashes.

Images must already exist locally: Compose runs with `--pull never`, and there
is no automatic image download or dependency installation. `owned-intent.json`
precedes Compose dispatch. Sanitized result receipts list the exact project,
ports, native census, independently observed process birth/PGID, direct child
joins and process-group absence. Detailed
container inspection, helper inputs/output and bounded child logs stay in the
0700 artifact directory. The controller never prints secrets or raw SDK/API
errors. The ten-minute work deadline and separate 90-second cleanup allowance
cover finite startup, HTTP, helper, native persistence and recovery waits; no
mutation is automatically replayed.

The implemented minimum scenario creates and verifies the actual storage
revision, uploads four Personal/Project PNG/PDF objects via normal APIs, and
performs six controlled native calls with exact rewriting. Cross-Key scope and a
separate member's Session reads must fail before native dispatch. GORM reads
capture immutable exact Credential/snapshot/Provider/Connection attribution,
retain the original Session and storage versions across restart, and compare
call/attempt facts without historical substitution. The S3 helper writes distinct
later bytes for each exact key, making accidental latest-version reads observable.
During a genuine server outage, each normal DELETE must return the exact
uploaded target and `delete_pending` state. After positively joining the
application, while storage is still stopped, the read-only GORM helper captures
all four exact persisted `delete_pending` rows with unchanged owner scope,
revision, recorded version, digest, content metadata and creation time. The
private `delete-pending-after-join.json` ties that snapshot to the joined
application receipt before storage recovery or the next application launch.
Restart recovery must delete only the original recorded versions, preserve all
later versions and produce no delete markers. The corrected actual production
run passed these assertions, with separate independent cleanup readback.

## Preparation checks

Thirteen lightweight Node controls pass: seven shared supervisor controls and
six S3-specific contract controls. The shared controls cover executable byte admission, forbidden symlink and
execute-bit cases, exact symlink source inventory, real observed child
birth/PGID and join, output overflow with closure, deadline failure with closure,
rejection of an unproved process identity, and repeated real short-lived commands
whose direct joins remain valid when birth cannot be observed. Those commands
explicitly record unobserved birth only after kernel PID and expected group
absence; long-lived application acceptance still requires observed birth/PGID. Go helper source includes
exact-version receipt round-trip, bounded/ambiguous input and foreign
database/fixture/private-receipt controls. Both composed Go helpers passed fresh
race checks and compilation. The complete ordinary run passed all 44 script
controls, including the initial 21 composed acceptance controls. The final
script successor passed all 23 composed controls. These checks alone
do not prove the actual outage/restart lifecycle.

The S3-specific controls in `scripts/storage-native-contracts.test.mjs` cover
real asset byte/mode/addition/deletion drift, root/nested symlinks and missing
index rejection; real executable replacement, execute-bit removal and symlinks;
exact normal DELETE target/pending-state admission; exact full-fraction creation
instants across timezone offsets, including nanosecond/microsecond negatives;
and rejection of ready,
deleted, missing, duplicated or changed-scope/version persisted intent receipts.
These controls do not exercise GORM or the actual outage/restart lifecycle.

The present controller covers Personal/Project attachments with native Chat
Completions only. Team inference, other protocols, constructed expiry, natural
one-hour expiry, real external Providers and browser interactions remain
separate pending acceptance. Neither this source nor the Node controls complete
A17 or production readiness.
