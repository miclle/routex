# Vault Token integrations

Status: private implementation candidate. The combined source passes formatting, mandatory check, complete Task/build and 18 focused PostgreSQL/MySQL lifecycle cases. Full141 is running. Controlled Vault API R7 passes configuration, retained-auth Cleanup, persistent Vault/application restart and both original API Sessions. Root independently verifies six revisions, two probes, four stage commands, ten paired Vault requests (six successful effects and four ACL denials), seven observed root domains, the real 300-second observation and completed retirement. Before/after/finish database projections are identical; source, artifact/config and exact owned cleanup remain verified. Root API-only review SHA-256: `efb14524ce1c06fe6de54be050d03e7d10946a6cd3c93fde25be6886c73cf538`. Earlier failed runs, including the restart-wait failure, remain failed; the fixed loopback-port successor does not reinterpret their missing evidence. Browser and delivery remain pending. Active Provider credential storage remains internal.

## Configuration and authority

The existing Secrets workspace exposes an addressable Vault tab at `/admin/secrets?tab=vault`. Its integration table, row menu and 720px configuration drawer use local shadcn-style components and Base UI. English is the default; every visible control and observation supports live Chinese switching.

An enabled, admitted administrator requires independent permissions: `secrets.read` for inspection, `secrets.write` for configuration, `secrets.test` for explicit probes and `secrets.rotate` for internal root rotation. Assigning a permission does not bypass the administrator requirement. Configuration and probe actions repeat current server authorization.

Each saved revision records a name, guarded endpoint, optional namespace, KV-v2 mount, path prefix and data field. Writer and reader support Token authentication only, with explicit keep/replace/remove actions. Their distinct Token strings establish local separation; they do not prove separate remote principals or least privilege. Tokens remain transient in the mounted form and encrypted in separate immutable server records. They are absent from returned DTOs, mutation caches, browser storage and audit metadata. Dismissal, authority loss and unmount clear transient material; secret retry recovery across remount is not supported.

A save requires a current strong If-Match, reason and UUIDv4 intent. The descriptor and both auth references change atomically. A durable receipt proves that exact historical configuration commit, not routing activation or remote verification. Conflicts require explicit current review; uncertain retries retain their exact mounted request. A configuration change makes earlier probe observations historical. Root ciphertext rewrap alone does not change the logical configuration revision.

## Explicit bounded tests

Write test persists a server-owned random plan and an execution claim before a single KV-v2 CAS-zero write. The path, marker and expected version are never caller choices. Read test uses the exact saved plan and current configuration revision, verifies the owned version-one content and live metadata, then performs the explicitly confirmed version-one destruction. Separate cleanup revalidates ownership using the original retained descriptor/auth revision; it cannot perform a fresh Read test of a changed configuration.

Each command is finite: Write makes at most one request; Read or Cleanup makes at most one read and one destroy request. The client suppresses automatic transport replay, follows the upstream DNS/network policy and rejects redirects. The service durably claims the complete command before its first HTTP request. It cannot persist a Read success between the client-owned read and conditional destroy. A crash or failed result persistence leaves independent observations unknown; no database lock spans remote HTTP. An unresolved plan blocks another Write until cleanup disposition is confirmed. Repeating the original UUID returns current recorded observations without dispatching again; those observations do not prove an earlier command succeeded.

Write, Read and Cleanup observations remain separate. A cleanup acknowledgement records the remote exact-version destruction acknowledgement; it does not mean metadata/path deletion, physical erasure or removal of later versions. Lost responses and persistence failures remain uncertain. Exact-version read and destroy are separate remote requests and provide no atomic fence against a concurrently privileged metadata delete/recreate.

## Persistence and root inventory

Frozen GORM V72 adds retained integration revisions, separate writer/reader encrypted auth records, configuration receipts and durable probe claims/results. It extends root inventory with `vault_writer_auth` and `vault_reader_auth`, including every retained auth revision. Startup authentication, rewrap, verification, rollback and retirement cover both domains. Root reader draining tracks finite in-flight probes; Gateway inference continues using its existing published internal credential store.

Inventory version one preserves historical five-domain jobs. Nonterminal old jobs require explicit reviewed Resume to version two and a fresh seven-domain scan. Completed old history stays historical. Missing domain observations stay not scanned, never synthetic verified zeros. Zero and unknown inventory versions fail closed. Root retirement still requires complete current proof and server-owned observation.

## API

All paths below use `/api/v1/admin/secrets`:

| Operation | Endpoint | Permission |
| --- | --- | --- |
| List/create | `GET` / `POST /integrations` | read / write |
| Inspect/update | `GET` / `PUT /integrations/:id` | read / write |
| Write test | `POST /integrations/:id/probes/write` | test |
| Inspect probe | `GET /integrations/:id/probes/:probe_id` | read |
| Read test | `POST /integrations/:id/probes/:probe_id/read` | test |
| Owned cleanup | `POST /integrations/:id/probes/:probe_id/cleanup` | test |

Private reads use no-store; writes and tests require current Session/CSRF and strict bounded JSON. Probe IDs, configuration generations, exact actor/resource births and review validators are server checked. The bounded catalogue uses canonical server cursors rather than fetching credential directories.

## Controlled API evidence and remaining gates

Both Vault R7 and Personal R6 API runs use the original R3 artifact `19320f5e16792e790748feb0a69fd6f70d1c3fc4f571a5e27c6e70d8494b5df4` and source floor `1b78ece3a09c458141ceff256f702822c7e9f0b714766e8458a3b92520276101`. The later R4 parent-callback repair changes a test fixture only; its whole-source/backend-test hashes and gate receipts remain distinct even though production code is identical. API results do not become R4 whole-source acceptance. Expanded Focus18 and current Full141 belong to that later source; Full141 is still running.

Browser, bilingual controls, AuthGate recovery and feature delivery remain pending for this phase. Desktop control reports a locked Mac and the in-app browser cannot attach a new webview; those observations do not prove a product cause or UI/download success. A bounded fresh normal-browser gate must verify the existing controls, independent authority and original-Session restart against an exact reviewed artifact. No complete F17/F28 or full-objective acceptance is claimed.

## Remaining scope

AppRole, TLS/client authentication, Kubernetes authentication, active Provider storage switching, API Key delivery and fleet coordination remain unfinished. A configured integration or successful controlled probe does not establish any of those capabilities. See [Implementation](IMPLEMENTATION.md), [Root rotation](SECRETS.md) and [Secret storage](SECRET_STORAGE.md) for separately recorded delivery and acceptance evidence.
