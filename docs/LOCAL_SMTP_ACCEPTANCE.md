# Local SMTP capture acceptance

This maintained drill runs the exact already-qualified default production
executable against disposable PostgreSQL, MySQL and a digest-pinned Mailpit
capture relay. It creates no inference Keys or Calls. It is a local acceptance
controller, not a deployment recommendation or a substitute for production
TLS/authentication, recipient mailbox delivery, bounce handling or browser
acceptance. The initial composed implementation passed mandatory checks,
complete ordinary tests and production builds. Its first actual local run
captured one real test email, then failed the strict identical-intent receipt
assertion. A private diagnostic preserving that assertion confirmed identical
recorded instants but different timestamp zone representations between the
first response and replay. Both failed runs retain independently confirmed
cleanup. The successor reads the recorded GORM receipt before returning first
success, keeps the strict assertion, and requires fresh qualification and an
actual complete run. That successor passed mandatory checks, complete ordinary
tests, production builds and the focused real PostgreSQL/MySQL SMTP lifecycle.
The next actual drill passed strict test-receipt replay but failed after a
successful notification-read mutation: the mutation DTO intentionally omits
the delivery facts attached by the recipient list endpoint. The corrected
controller checks mutation identity/read state, reads the exact private inbox
again and preserves the complete delivery assertions on that list projection.
It retains private mutation/list receipts for both drivers. This corrected
source passed fresh mandatory checks and all 22 composed script controls.
The complete actual drill passed for PostgreSQL and MySQL: four application
generations, four captured messages, unchanged original Sessions, strict test
replay and twelve-second retained-delivery observations. Independent readback
confirmed all twenty workers and nine ports closed, exact resources absent and
the original development service preserved. Earlier failed runs remain failed;
this acceptance covers the disposable local relay rather than recipient inbox
delivery or production SMTP authentication/TLS. The final formatted successor
also passed fresh mandatory checks, all 23 composed controls and the complete
actual drill. Its initial independent readback found closed ports not yet
freshly bindable; that receipt remains false. A separately retained delayed
readback confirmed all nine ports bindable and complete cleanup, without
rerunning or relabeling the original process result.

The SMTP endpoint and capture API bind only to random loopback ports. RouteX
uses its normal configuration APIs with `allow_private_smtp: true`, explicit
`NONE` security and no authentication solely for this disposable fixture.
Sender and recipient addresses use `example.invalid`. No user credentials are
needed. Mailpit forwarding is not configured, and its duplicate Message-ID
suppression is explicitly disabled so replay cannot be hidden by the relay.

The drill configures SMTP through the ordinary administrator transport/sender
APIs, sends a real test email, and explicitly opts one recipient into high
severity while leaving medium severity disabled. A single real failed
Credential manual verification creates a durable operational source and an
email intent. This manual check remains separate from inference statistics.
The relay API proves the recorded MAIL FROM and first RCPT TO recipient,
single-recipient message header, Message-ID, server-owned subject/body and
raw message. Mailpit does not expose the complete RCPT TO envelope through
these receipts; the drill does not prove that no additional envelope recipient
was submitted. A read-only GORM helper verifies
the exact audit/occurrence/recipient/transport-revision binding and retained
single-attempt accepted delivery. It never migrates or writes database rows.

Each database uses two application process generations, the same executable,
root key, database, journal and original Session. An explicit identical test
intent retry must return the original receipt. A twelve-second read-only
observation after restart must preserve the same private read
receipt, immutable facts and exact capture census, without new upstream
checks, SMTP submissions or relay receipts. That bounded observation proves
this workflow; elapsed time does not prove an actual worker-poll count, indefinite
availability or fleet behavior. Both long-lived application generations require
observed process birth and exact owned process-group identity.

Prepare and qualify the executables before running the drill:

```bash
go build -o /absolute/private/path/smtp-fixture ./scripts/smtp-acceptance-fixture
export ROUTEX_SMTP_BINARY=/absolute/path/to/qualified/routex
export ROUTEX_SMTP_HELPER=/absolute/private/path/smtp-fixture
bash scripts/test-smtp-acceptance.sh --capture-inputs
```

Input capture starts no services and is explicitly unqualified. Its private
`inputs.json` binds `source_digest`, `binary_sha256`, `helper_sha256` and
`assets_digest`. The complete `website/build` inventory includes every regular
file's relative path, mode and SHA-256; symlinks and missing index assets fail.
After formatting, mandatory checking, ordinary tests and default production
build pass on those exact bytes, prepare a private qualification JSON retaining
those four inputs and setting `mandatory_check`, `ordinary_tests` and
`production_build` to `passed`. Build the helper and production executable
before capturing; ignored binary outputs do not alter the source inventory.
The source, binary, helper and asset census must still match at completion;
late drift fails cleanup qualification even after functional observations pass.
The fixture root key uses canonical Base64 for exactly 32 decoded bytes, matching
the application's configuration contract. Lightweight controls cover invalid
encoding, actual asset byte/mode/addition/deletion changes and qualification
binding. Do not fabricate passing qualification fields from the input capture.

```bash
export ROUTEX_SMTP_QUALIFICATION=/absolute/private/path/qualification.json
bash scripts/test-smtp-acceptance.sh
```

The three pinned images in `scripts/smtp-acceptance.compose.yaml` must already
be available locally: the drill uses `--pull never`. Arrange image acquisition
and execution separately from concurrent full-database qualification. A fresh
private result directory stores receipts, message captures and bounded child
logs; stdout contains only a fixed outcome and that directory. Root keys,
passwords, Session/CSRF values and raw message content must never be copied to
shared documentation or console output.

Success requires both functional acceptance and independent cleanup of exact
owned process groups, Compose containers/networks/volumes and captured ports.
Failure retains its original stage and separate cleanup outcome. Closing the
controlled HTTP peer has its own finite deadline inside the cleanup budget. Unrelated
development services are neither discovered nor stopped. Review `result.json`
and its input/source/ownership receipts; a successful test email alone is not
the complete drill, and SMTP acceptance never proves recipient inbox delivery.
