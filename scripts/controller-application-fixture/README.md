# Controller application fixture

This maintained acceptance helper belongs to the root Go module. It is not
included in the production RouteX or Controller executable. Its unit tests are
included by the normal package checks and tests; Linux file-safety cases have a
Linux build constraint. No external upstream or fleet guarantee follows from a
controlled fixture result.

The command accepts one mode, with no flags, secrets, URLs or paths in arguments:

- `serve`: read private `/fixture-native-input/serve.json` and `upstream.secret`;
  serve authenticated native discovery and fixed Chat responses.
- `invoke`: read private `invoke.json`, then the exact actual Agent-installed
  slot file under `/var/run/secrets/routex/api-keys`; make one Gateway request.
- `probe-predecessor`: read only private `predecessor.key`; make one request and
  succeed only for HTTP 401. This never overrides the installed slot reader.

Example peer config, supplied by a separate owned coordinator:

```json
{"listen":"127.0.0.1:19003","lifetime_seconds":900,"max_requests":32,"finish_reasons":["length","stop","stop"]}
```

Example invocation config:

```json
{"slot_id":"kds_01arz3ndektsv4rrffq69g5fav"}
```

The secret input is a private newline-free alphanumeric/base64url value. Key
inputs require either the 46-byte `rx_` Personal grammar or the 47-byte
`rxp_` Project grammar, each with exactly 43 base64url suffix characters. Every private file must be regular,
mode 0600, single-linked and bounded; directory ancestors must not be writable
by group or others. Installed Keys must belong to the capsule effective UID.
Private bind inputs may retain the coordinator UID. Linux `openat` traversal
rejects symlinks beneath held directory descriptors. The known `/var/run ->
/run` and `/var/run -> ../run` spellings must resolve to the same canonical
output directory; other aliases fail closed. The pinned image records `../run`.
All private reads fail closed on non-Linux systems.

The peer admits only the expected authenticated discovery and nonstreaming Chat
paths, fixed upstream Model, one fixed user message and `max_tokens:16`.
Discovery does not consume a native-call slot. Native output supplies explicit
3/2/5 usage and a `length` or `stop` finish reason. A finite request/call budget,
16 KiB payload limit, header/read/write timeouts and a maximum 900-second lifetime
bound the peer. Owned SIGTERM/SIGINT uses a two-second graceful shutdown.

Invocation targets only the owned Gateway at loopback port 19001. It has an
8-second deadline, bounded headers/body, no proxy, redirects, retries or body
replay. Results contain fixed status codes, sanitized Request IDs and native
outcomes; no raw payload, Key, hash, authentication header, argv or environment
is printed. Predecessor output omits the native field. Exit zero captures an HTTP
response in `invoke`; the coordinator must still assert HTTP 200 and the expected
native outcome. The separate probe requires HTTP 401.

Qualification commands, run only after the shared validation lane is available:

```sh
go test -race -count=1 ./scripts/controller-application-fixture
go test -race -count=1 ./scripts/controller-application-evidence
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o bin/controller-application-fixture ./scripts/controller-application-fixture
```

Run the fixture tests again inside a separately owned Linux root test container
so file protection, wrong-owner and no-follow cases execute. A host run that
excludes those tests does not establish Linux file safety. Use the pinned image's
actual architecture when building. A separate coordinator must own namespace,
ports, inputs, process groups, executable/image pins, finite traces and cleanup.
It must separately prove persisted calls/receipts, planned retirement, old-Key
401 with no new upstream event, and a fresh installed-replacement call.

Project support changes only bearer grammar in the maintained fixture. Installed
Project Keys use the same exact slot reader and effective-UID protection;
preserved predecessor inputs retain the existing private bind-owner contract.
Neither a `pky_` record identity nor a Project URL argument is a bearer. The
same native parsing, one-request limit, no-retry/no-redirect transport and
status-only predecessor output apply to both owner kinds. The coordinator must
use Project-scoped persistence APIs and the separate exact Project projection.
