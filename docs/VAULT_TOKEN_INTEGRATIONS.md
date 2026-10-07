# Vault Token integrations

Status: Token integrations and their two encrypted root domains are delivered by
`c2368ddb0251415e72bfeee948ad23ed7a173ee4`, with exact remote main read-back.
The accepted contextual Full141 passes 282 direct PostgreSQL/MySQL lifecycles
and eight constraint checks. Controlled Vault API R7 covers configuration,
retained-auth Cleanup, persistent Vault/application restart and both original
API Sessions. Its artifact remains distinct from the later full-regression
fixture successor; the receipts are not interchangeable.

Root independently verifies six revisions, two probes, four stage commands,
ten paired Vault requests (six successful effects and four ACL denials), seven
observed root domains, the real 300-second observation and completed retirement.
API-only review: `efb14524ce1c06fe6de54be050d03e7d10946a6cd3c93fde25be6886c73cf538`.
Historical failures remain failed. Browser proof and complete F28/A16 acceptance
remain separate and unfinished. The independent credential SDK is delivered by
`78c76d2`; the SDK itself does not activate Provider storage. The current Provider
write-policy implementation below is a separate source phase whose fresh driver,
real-Vault and full-current acceptance remain pending in this candidate. Its
configured mode does not establish Vault availability. See
[Implementation](IMPLEMENTATION.md) for separately recorded evidence.

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

Frozen GORM V72 adds retained integration revisions, separate writer/reader encrypted auth records, configuration receipts and durable probe claims/results. It extends root inventory with `vault_writer_auth` and `vault_reader_auth`, including every retained auth revision. Startup authentication, rewrap, verification, rollback and retirement cover both domains. Root reader draining tracks finite in-flight probes and credential reads. The Provider storage resolver prepares routing snapshots separately; Gateway inference uses those immutable prepared snapshots rather than fetching Vault for each native request.

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

Both Vault R7 and Personal R6 API runs use the original R3 artifact `19320f5e16792e790748feb0a69fd6f70d1c3fc4f571a5e27c6e70d8494b5df4` and source floor `1b78ece3a09c458141ceff256f702822c7e9f0b714766e8458a3b92520276101`. The later R4 parent-callback repair changes a test fixture only; its whole-source/backend-test hashes and gate receipts remain distinct even though production code is identical. API results do not become R4 whole-source acceptance. Expanded Focus18 and the subsequently accepted Full141 belong to that later source; the earlier checkpoint recorded Full141 as running.

The Token integration phase is delivered. Actual browser controls, bilingual workflow and AuthGate recovery remain pending; source UI tests are separate from browser acceptance. Desktop control reports a locked Mac and the in-app browser cannot attach a new webview; those observations do not prove a product cause or UI/download success. A bounded fresh normal-browser gate must verify the existing controls, independent authority and original-Session restart against an exact reviewed artifact. No complete F17/F28 or full-objective acceptance is claimed.

## Explicit AppRole SDK login

The standalone `Client.LoginAppRole(ctx, authMount, roleID, secretID)` makes one
guarded, non-retrying POST to the selected authentication mount, independently
from the KV mount. It inherits the descriptor namespace and existing endpoint,
DNS, network and TLS policy. It sends no Vault Token header, performs no KV
operation, and has a ten-second deadline bounded by the caller context.

The response requires a bounded Token and a positive integer lease that fits
Go `time.Duration`. The local lease conservatively starts before dispatch.
`LoginToken.Token` returns a transient string copy until expiry or `Close`;
`Close` clears owned bytes and invalidates the handle without remote revocation.
Callers must close the handle, avoid copying it, and discard any copied strings.
Redacted formatting and null JSON prevent ordinary serialization from exposing
auth material; this is not physical-erasure evidence.

Existing KV primitives retain their request counts and do not authenticate
automatically. The SDK provides no Token cache, renewal, persistence or
Integration management. Higher layers must own durable claims and total
operation deadlines. A failed login may consume a Secret ID use and is not
retried. This primitive does not enable AppRole in saved integrations, Provider
storage or Gateway requests; those service, schema, UI and runtime contracts
remain separate unfinished work. Package tests do not establish real Vault
ACLs, browser controls or persistent restart acceptance.

## Remaining scope

Saved AppRole integration, TLS/client authentication, Kubernetes authentication, automatic orphan recovery, API Key delivery and fleet coordination remain unfinished. A configured integration or successful controlled probe does not establish any of those capabilities. See [Implementation](IMPLEMENTATION.md), [Root rotation](SECRETS.md) and [Secret storage](SECRET_STORAGE.md) for separately recorded delivery and acceptance evidence.

## Provider credential write policy

The Storage tab contains two stacked configured-source cards for internal encrypted
storage and Vault KV v2, followed by the existing root rotation card. A policy
save changes future Provider credential writes only; it neither moves existing
credentials nor proves remote availability. Vault selection requires the exact
server-issued eligible Integration and saved revision with writer and reader auth
present. There is no Integration enable flag, probe prerequisite or automatic
selection of a newer revision. Policy reads and writes retain the admitted
platform-administrator boundary and independent `secrets.read`/`secrets.write`,
reviewed strong If-Match, required reason and explicit confirmation.

Provider, Connection and Credential creation and replacement read their
`providers.write`-authorized nonsecret source context without Secrets directory
access. New UI requests capture its raw 64-character `storage_policy_etag` and a UUIDv4 with
the exact original public intent and transient secret. Vault requires both fields
before effects; legacy inline requests may omit them. A stale review fails before
planning. Ordinary creation returns 201, including exact UUID reconciliation;
replacement retains 201 for its first saved result and 200 for reconciliation.
Explicit-UUID Provider/Connection responses contain only the exact creation-owned
bootstrap rows and their current nonsecret state. `storage_source` is recorded
inline/Vault metadata, not proof of verification, enabled routes or native use.

The service durably retains the original source, immutable descriptor/auth revision
and owned version-one plan before remote effects. An uncertain UUID retry cannot
rewrite that intent to follow a later policy or replay a CAS write. It may reconcile
only the matching owned value under fresh actor, target, retained-auth and finite
root-reader-lease authority. Current reader removal or Integration deletion/rebirth
revokes availability; nonempty later descriptor/auth changes do not repoint old
references. Verification and Enable remain separate explicit operations. Secret
inputs stay transient, outside browser storage and query/mutation caches.

All seven existing root inventory domains remain in scope, including retained
writer/reader auth revisions. Ciphertext rewrap does not change logical creation
intent or auth revision. No automatic orphan cleanup authority is introduced;
unknown remote effects remain unknown. AppRole login remains a standalone SDK
primitive, with no saved AppRole auth, policy UI or automatic authentication.
These source contracts do not replace fresh real-driver, real-Vault, same-artifact
restart or browser acceptance, and they do not complete F28 or A16.
