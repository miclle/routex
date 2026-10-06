# Internal secret storage and root rotation

Checked implementation, 2026-10-04. Final main format/check/test/build passed
with 1829 frontend cases in 101 files, Go race/unit, development lifecycle,
embedded assets and unchanged dependency bytes. The repaired PostgreSQL/MySQL
migration and lifecycle focus passed under race detection in 124.896s, including
third-root rollback; owned resources were removed and verified absent.
Controlled production/browser acceptance passed seven real five-domain probes,
both continuous 300-second server observation windows, reverse rotation,
next-root-only restart, bilingual historical views and Session revocation.
The real-process authentication lifecycle passed on both databases and cleaned
its owned resources. The complete final PostgreSQL/MySQL race matrix passed: Handler 1341.797s and
Service 8.060s. Owned resources were removed and verified absent; all 56 frozen
code hashes remained unchanged. The final mandatory check passed before delivery.
External Vault identities and storage switching are separate unfinished work.

## Keyring bootstrap

Root material is loaded from private bootstrap configuration. Each root is a
base64-encoded 32-byte key with an exact ASCII identity. Root bytes are never
saved in the database, displayed in the browser, or returned by management APIs.
Environment expansion happens after YAML parsing.

```yaml
encryption_keyring:
  legacy_key_id: current
  write_key_id: current
  keys:
    - id: current
      key: ${ROUTEX_ROOT_CURRENT}
    - id: replacement
      key: ${ROUTEX_ROOT_REPLACEMENT}
```

`legacy_key_id` identifies the one root authorized to open historical v1
envelopes. Empty legacy identity is allowed for a v2-only keyring. Identities are
exact 1–64 character ASCII strings containing letters, digits, `_` or `-`; case,
whitespace and repeated identities are not normalized. Roots must have distinct
material. A keyring contains at most 64 roots including an explicitly merged
legacy root.

The durable initialized policy owns the active write root and epoch. A restart
cannot silently replace it with the bootstrap write selector. Missing, changed
or unavailable required root material fails closed. Legacy-only configuration
remains compatible with installations that have not initialized a keyring
policy; it cannot bypass an initialized policy.

New keyring writes use v2 envelopes with authenticated exact root identity and
stable secret reference. Rotation authenticates each retained payload and
rewraps its data-encryption key; payload ciphertext and nonce remain unchanged.
Legacy `encryption_key` readers/writers retain their historical v1 contract.

## Management workflow

The existing administration shell exposes `/admin/secrets` and addressable
`/admin/secrets/rotations/:rotation_id`. An enabled current administrator must
independently possess `secrets.read` to inspect state and `secrets.rotate` to
change it. Delegating those permissions to a member does not bypass the
administrator requirement.

The internal status card and Base UI dialog show actual configured key IDs,
policy epoch, local process publication proof and recorded work for each secret
domain. No guessed percentage, complete-work denominator, external store choice
or arbitrary secret row is shown. English is default; labels, states, guidance
and accessible names have paired English/Chinese translations.

1. Preload both current and replacement roots and publish the current combined
   process. Review current configuration and the exact strong ETag.
2. Start with a replacement key ID, one UUIDv4 intent and a required reason. The
   committed write-policy epoch fences every secret writer. A stale prepared
   operation rejects its whole transaction instead of replaying a secret write.
3. The bounded durable worker scans all five retained domains, uses exact
   ciphertext compare-and-swap, and performs a fresh complete verification.
   Every committed page publishes the actual current inventory after releasing
   its transactions and egress locks. A failed publication preserves the durable
   cursor, counts and rewraps while blocking progress.
   Corruption or unavailable proof blocks global progress; it never becomes
   partial retirement. Explicit resume requires a fresh review.
4. After verified zero old-root dependencies and matching actual publication,
   maintain 300 seconds of continuous server-owned observation. Restart,
   interrupted verification or changed publication resets continuity. A browser
   clock or bootstrap root list cannot prove retirement readiness.
5. Retire through the reviewed confirmation. The process drains prior tracked
   decrypt operations before installing the restricted decryption view.

The supported process boundary is one current combined instance. Another live
or unverified process blocks cutover and retirement; this is not fleet
acknowledgement. Existing valid runtime publication remains available during
failed refreshes. A committed receipt and current publication are distinct.

## Retained domains and write safety

| Domain | Included retained encrypted records |
| --- | --- |
| Provider credentials | Every nonempty credential envelope, including pending or disabled credentials |
| Egress authentication | Saved proxy authentication with its exact generation reference |
| SMTP authentication | Saved descriptor authentication with its exact generation reference |
| Storage authentication | Every retained revision, including failed/candidate descriptors and historical revisions beyond the administration page limit |
| Authenticator factors | Pending, enabled, disabled and retained/orphan factor envelopes using their immutable user/generation reference |

Passwords, Personal/Project API Key digests, Sessions and recovery-code digests
are irreversible verification values and are outside recoverable-secret
rotation. Rotation changes no model grants, authentication factors, challenge
state or call history.

Writers prepare against one write epoch before external validation and check the
same epoch inside the complete commit transaction. Authentication `keep` writes
copy the locked current ciphertext, preserving any intervening worker rewrap.
Selective updates never restore an older whole-row secret snapshot. Deleted
subjects stay deleted; replacements are retained and rechecked. MFA keeps its
User-before-policy lock order without taking the governance lock.

Frozen GORM V48 adds an inert policy and private key/job/item/receipt/process
records and extends the actual System Job code guard. Released migrations remain
unchanged. Startup repairs partially applied DDL without discarding retained
history. Private proof ciphertext, subject identifiers, leases and ciphertext
digests are not public rotation DTOs.

## Receipts, rollback and recovery

Every mutation keeps one exact reviewed ETag, UUIDv4 and body through uncertain
responses and rejected retries. Refreshing state or finding matching values does
not resolve the original intent. An authorized immutable receipt proves
historical commit; current policy/publication application is separately checked.
Historical retries do not reapply cutover or recreate completed jobs. Current
identity and permission checks remain required before receipt access.

Rollback is a new write epoch and an authenticated reverse five-domain job with
its own verification and observation. It does not restore previous envelopes,
undo native calls or make an old single-key binary compatible with v2 writes.
Keep the modern keyring-capable binary and required roots throughout recovery.
A logically retired root cannot be selected for another cutover or rollback,
even if its bytes remain in bootstrap configuration. Test rollback with a
previously unused third root while its retained predecessor remains active.

Logical retirement restricts live decrypt access; it does not claim memory
erasure or destruction of offline key material. Historical backups may still
require archived roots. Recover those backups through an independently reviewed
offline procedure rather than weakening the active write policy.

## API

| Route | Authority and behavior |
| --- | --- |
| `GET /admin/secrets` | Current administrator plus `secrets.read`; read-only status |
| `GET /admin/secrets/rotations/:rotation_id` | Same independent read authority; exact job status |
| `POST /admin/secrets/rotations` | Current administrator plus `secrets.rotate`, CSRF and reviewed If-Match; start one intent |
| `POST /admin/secrets/rotations/:rotation_id/resume` | Same mutation boundary; explicit resume |
| `POST /admin/secrets/rotations/:rotation_id/retire` | Same mutation boundary plus server-confirmed readiness |
| `POST /admin/secrets/rotations/:rotation_id/rollback` | Same mutation boundary; new reverse job |

Responses are private and not cacheable. Mutation results report historical
commit independently from `write_policy_applied`, `publication_applied` and
`application_status`. Count/epoch values remain decimal strings. Unsupported or
unknown blockers receive generic localized guidance; root material never enters
browser storage or mutation state.

Seeded encrypted-row preservation, source tests and a successful response alone
do not prove Provider inference, proxy execution, SMTP delivery, Storage reads or
MFA sign-in after rotation. Actual controlled product-operation and restart
evidence is required before this candidate is delivered.

## External Token probe client foundation

`pkg/vault` provides bounded KV-v2 probe operations using a dedicated
`upstream.NewNonReplayingClient`. The existing inference client is unchanged.
The probe client retains outbound address validation, verified TLS, no redirects
and no environment proxy; it uses HTTP/1 with dedicated connections and does
not replay an ambiguous request. Tokens and probe markers are transient and
never appear in returned observations. Writer and reader Tokens must differ;
this does not establish distinct remote principals.

`Prepare` makes no HTTP request. It generates an opaque probe identity and
marker, returning a nonsecret plan bound to the descriptor and marker digest.
`Write` consumes the prepared attempt and sends at most one CAS-zero creation.
`ReadAndCleanup` or `CleanupOwned` checks the exact live version-one metadata
and marker digest before one conditional version-one destruction request. A
failed or mismatched read never authorizes cleanup. Later versions and metadata
remain intact; absence or a lost response cannot prove acknowledgement.

The future service must persist the plan and immutable authentication revisions
and durably claim each command before any HTTP. It must not hold a database
transaction across the read/cleanup sequence or replay an uncertain command.
Recovery can establish current ownership without inventing original Write
success. KV-v2 supplies no atomic path-incarnation fence between read and
destroy; the remote probe prefix must exclude concurrent path replacement.
The package does not activate external secret storage or implement switching.

Controlled HTTP/TLS tests cover replay prevention, bounded response parsing,
independent observations, failed-read cleanup rejection and staged operations.
These are source-level tests, not real Vault, identity, migration, management
API, browser or process-restart acceptance. The separate durable configuration
backend and administration workspace remain in progress under F28.

This bounded foundation passes mandatory checking, the complete Task suite and
production build against its exact source composition, including 4,402 frontend
cases and all Go race/coverage tests. The containing commit delivers these package
operations. Real Vault and durable configuration acceptance remain separate.
