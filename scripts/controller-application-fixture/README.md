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

## Real installed Claude compatibility (additive)

Two additional closed operations preserve all existing Chat arguments and input
files:

- `serve-messages` reads `/fixture-native-input/messages-serve.json` and the
  existing private `upstream.secret`. It serves authenticated Anthropic model
  discovery (`GET /v1/models?limit=1000`) and one streaming
  `POST /v1/messages` (optional exact `?beta=true`). Discovery returns the
  `controller-messages-upstream` model with explicit `has_more:false`.
- `client-messages` reads `/fixture-native-input/client-messages.json` and only
  the exact installed slot selected there. It launches the separately verified
  official Linux Claude binary at the fixed `/fixture-client/claude` path,
  against `http://127.0.0.1:19001`, with public model
  `controller-native-messages` and the fixed existing prompt. It does not use
  an issued-Key fallback or export an installed Linux Key to another host.

Both inputs require exact lowercase `source_commit`, `run_id`, and `phase`
fields matching the fresh compiled binding. Peer input additionally requires
`listen`, `lifetime_seconds`, and `max_requests`, with the existing loopback,
900-second and 64-request ceilings. Client input additionally requires only
`slot_id`. Unknown, missing, duplicate or differently cased fields are rejected.
These are separate files; adding these fields to the old Chat inputs remains an
error.

Every new-mode build defaults to refusal. A reviewed fresh recipe must set these
package-main string variables with Go linker `-X` values:

| Variable | Required binding |
| --- | --- |
| `claudeSourceCommit` | Exact nonzero lowercase 40-character product commit |
| `claudeRunID` | Fresh nonzero lowercase 32-character run identifier |
| `claudePhase` | Exactly `f29-claude-messages-v1` |
| `claudeUID` | Canonical positive decimal effective receiver UID |
| `claudeVersion` | Reviewed official numeric release version |
| `claudeSHA256` | Official verified artifact's complete lowercase SHA-256 |
| `claudeSize` | Exact official artifact size, bounded at 512 MiB |
| `claudePlatform` | Exactly `linux-arm64` or `linux-arm64-musl` |

The coordinator must verify the official signed release manifest, its signer,
artifact platform/base compatibility and actual downloaded bytes before adopting
these values. No artifact digest or version is supplied by a runtime config, and
no placeholder is treated as qualified. The version is provenance bound to the
reviewed digest, rather than an unverified `--version` execution. Only Linux
arm64 is supported by this new launcher. The coordinator must mount the artifact
read-only in the owned receiver; directory ancestors must be root- or
receiver-owned and not writable by group/others. The held executable must be
single-linked, regular, mode 0555 or 0755, with exactly the bound size/hash and
stable ownership, timestamps and identity while it is streamed through the
hash. Execution uses that same inherited descriptor, avoiding a second pathname
lookup. The installed Key is read only after artifact verification.

The fixed argument list is `--bare --print --tools "" --no-session-persistence
--model controller-native-messages "Controlled acceptance"`. The launcher creates
fresh mode-0700 HOME, temporary and working directories. Its environment is an
allowlist: HOME, TMPDIR, XDG_CONFIG_HOME, XDG_CACHE_HOME, PATH, LANG,
ANTHROPIC_API_KEY, ANTHROPIC_BASE_URL and
CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC. It never appends the caller's
environment. No user configuration, credential store, proxy, arbitrary command,
extra prompt or executable argument is accepted. The Key occurs only in the
child's environment, not argv or output. Isolation from external traffic still
requires the coordinator's owned network topology; flags alone are not egress
proof.

The peer allows bounded text-only SDK request envelopes (256 KiB, one user
message, no tools, one occurrence of the fixed final prompt, streaming, the
exact upstream model, positive `max_tokens` no greater than 65,536). Opaque
system/cache/SDK metadata is not executed, persisted or printed. Responses emit
ordered `message_start`, text block start/delta/stop, final `message_delta` with
`end_turn` and usage, then `message_stop`, flushing every frame. An unsuccessful
write/flush never prints completed. This is controlled protocol behavior, not a
new RouteX API. No `count_tokens` endpoint is added; it is optional for the real
client. A second inference is rejected; extra or retried Calls cannot be hidden
by discovery traffic.

The launcher retains at most 64 KiB each of stdout/stderr in memory, clears both,
and emits only fixed status and public original-process identity/provenance.
Success requires the exact fixed answer (allowing terminal CR/LF), empty stderr,
original exit zero, original join and observed dedicated group absence. The
active client bound is 45 seconds, followed by at most two seconds for TERM,
two seconds for KILL, two seconds for exec pipe closure and two seconds for group
absence. Linux `waitid(WNOWAIT)` keeps the original child unreaped until signals
finish, so group signalling cannot authorize a recycled leader. Unknown
ownership or join preserves failed/unknown state and the private workspace;
receiver resources require independent physical readback. No complete child
census is claimed. A containing coordinator must own cancellation, finite outer
bounds, live identity and final resources/ports readback.

Client output always leaves `native:"unknown"` and
`client_request_id_displayed:false`. CLI text and peer output do not prove
persisted native completion. The separate maintained evidence operation
`controller-application-evidence messages-once` must independently establish one
exact fresh installed-Key Call/Attempt, Anthropic protocol, actual native
completion, immutable owner/birth and published snapshot. Its SQL-derived
Request ID must not be relabelled as displayed by Claude. Actual client
compatibility, controlled native use, real external upstream use and sustained
application adoption remain separate claims.

Current-host qualification on 2026-10-18 passes 55 fixture and 177 evidence
named host race nodes, mandatory checking, and 90 fixture / 185 evidence Linux
arm64 nodes without race instrumentation or skips. The existing `x/sys` version
is promoted to a direct module requirement for the Linux waitid adapter.

Fresh R4 then uses verified official Claude 2.1.169 Linux arm64 bytes, freshly
rebuilt RouteX/Controller/helpers/JWKS and a no-cache image. Actual Controller and
Vault Agent installation, the original Claude invocation, exactly one durable
successful native-completed Anthropic Call/sole Attempt, independent expected
publication and maintained `messages-once` output pass on PostgreSQL. The
original client joins exit zero; recorded Compose resources and both published
ports close. Independent readback SHA-256:
`55339956a5a9bdf0ffbd0bf7cca78fc439fce7fa97190c7bb8a96e03f23e1e3b`.

The minimal Controller environment also requires a real receiver OS account and
resolvable private home for Vault Agent. R2 omitted that fixture prerequisite;
R3 completed the client but rejected an incorrectly sized coordinator database
password. R1 failed Compose preparation. All three remain failed full gates.
No production fallback or file-safety rule was weakened. This accepted first
Personal Key slice does not qualify Project use, replacement/retirement, the new
mode on MySQL, external upstream adoption or whole F29 completion.
