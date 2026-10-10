# Gateway installation observations

The single-process observer is implemented and passes controlled dual-database
and restart qualification. Browser and fleet acceptance remain separate.

An installation observation binds routing and Gateway admission actually installed
in one registered serving process. It is separate from the existing routing-only
application records. It is not a historical command receipt, current database
truth, route health, native completion, management Session revocation or an
acknowledgement from every node.

## Installed source

The private projection is built from the actual authorization object before that
object is published. It covers installed Key identities and lookup mappings,
effective model grants and source relationships, resource and quota authority,
Provider/Connection/model/Credential eligibility, aliases, and native Team Session
admission proofs. Canonicalization retains exact identities, resource births,
expiry, optional values and effective policy distinctions. Map order, genuine set
order and equivalent time locations do not change the source.

Closed-field coverage classifies the 32 original authorization fields plus the
private cache: 30 fields participate in source identity. Publication epoch and
authorization expiry remain separate freshness guards; the private cached
projection digest is metadata and is never hashed recursively.
The ten Team-native primary-authentication proof fields are the policy and binding
pairs for OIDC, custom OAuth, LDAP, SAML and fixed named identities. They describe
installed native Team Session admission, not control-plane authentication or MFA
challenge consumption.

`KeysByID` must agree with the verifier-indexed Key map. Project limit owners must
be validated roots with a nil `ReplacesKeyID`; a nonnil empty value is not nil.
Unsupported or inconsistent projection input produces no complete observation.
It never repairs the source or changes admission decisions.

Management Session authentication, governance permissions, MFA enrollment and
challenge/recovery consumption still use their separate database authority. They
are not installed domains certified by this record. Active primary-login proofs
are covered only to the extent that they determine the installed native Team
Session set.

Bearer values, verification hashes, passwords, credential payloads and serialized
configuration are not stored in observation history or returned by the read API.
Private, purpose-separated transient verifier commitments bind the actual lookup
mapping; only the aggregate source digest is retained. The digest also binds the
exact routing snapshot and its existing private source digest.

## First observation and current matching

Additive frozen GORM migration V100 introduces
`runtime_installation_observations`. Each row retains its canonical record ID,
exact instance ID and birth, routing snapshot ID, projection version, private source
digest, original route publication time and first observation time. Existing
migrations and routing-only rows are unchanged and never backfilled as installation
proof.

Repeated observation of the same source in the same exact process preserves the
original ID and first-observed time. A changed installed authorization source can
create a new observation even when the routing snapshot is unchanged. Returning
to a genuinely identical source may reuse its first observation. A process restart
must register a new birth; it cannot reuse an earlier process's proof.

Recording requires the exact live registration and private lease token, current
authorization and routing pointers, publication epoch, fresh authorization lease,
successful routing status and complete projection. Registration checks occur under
the existing bounded row lock, with fresh time and guarded checks after waits and
before acceptance. An uncertain database commit never proves command completion.

Historical observations remain immutable. Current matching is a separate nullable
observation:

| Value | Meaning |
| --- | --- |
| `true` | The fresh installed source in this exact serving generation matches. |
| `false` | This exact fresh serving generation has a different successfully installed source. |
| `null` | Current matching cannot be established, including another process, expiry, invalid routing, unknown projection, stop, or unstable authority. |

These values do not authorize a request. Timed expiry and revocation overlays
remain independently enforced at the native admission checkpoints. Missing history
does not prove that no configuration was installed.

## Bounded best-effort evidence

Projection and both legacy/new recorders share one deadline bounded by the original
caller, original authorization lease and a 250 ms evidence budget. There is no new
budget after projection, a database wait or the legacy recorder. Canonical work is
bounded at 16 MiB and 262,144 visited entries. Exhaustion or late completion makes
evidence unavailable; these are observation bounds, not resource capacity limits.

Synchronous sorting is bounded and its late result is rejected; context checks do
not promise hard preemption. No maximum-size latency result is claimed without
measurement. Evidence adds no remote calls, request replay, detached writer, lease
extension or second desired-state scan.

Current authorization still publishes independently of route validation. When
new reductions are installed but routes are invalid, last valid routes may remain
while revocation continues to deny new calls. Such a refresh creates no combined
success observation and cannot report a positive current match.

## Read API and interface

`GET /api/v1/admin/runtime/installations` requires a fresh normal Session and
independent `system.read`; `system.write` alone does not grant reads. It accepts
an optional exact `instance_id`, actor/filter-bound cursor and limit (20 default,
100 maximum). Unknown, duplicate or empty query parameters are rejected. Reads
have a three-second budget, a 128 KiB response bound, private/no-store/nosniff
headers and no CSRF or mutation requirement.

The fixed page scope is `single_process_gateway_admission`. Public rows expose
only record/process/snapshot identities, projection version, dates, process status
and nullable current matching. Private hashes, tokens, subjects, payloads and raw
errors are excluded. Unsupported or corrupt proof remains unavailable; the server
does not synthesize it from the current catalogue.

The existing System task Details column offers separate Routing application
records and Gateway installation records actions. Both use the existing local
Base UI dialog and table composition, paired English/Chinese copy and localized
dates. Installation queries have independent actor/authority/instance/mode/cursor
keys. Renewal, errors, revocation, actor or target changes and unmount hide private
facts, abort reads and prevent late replies from restoring them. No interface
action accepts a node acknowledgement or publishes configuration.

## Qualification boundary

The V100 phase passes mandatory checking, complete Task (6,439 frontend tests),
focused and unchanged original full PostgreSQL/MySQL qualification, and separate
authentication lifecycle. The complete matrix accepts 203 business scenarios plus
four constraints / 494 named results per driver and 28 genuine restart applications.
Full readback: `2d87e43001af467e23aef7b1319783672935688a0044c72ab91d6cae54b4d1e0`.
Authentication lifecycle independently accepts eight starts; readback:
`d58c52b756e8d01ac02d08388b3c56a226f89f1801a61e0921931ce5fa96e2a5`.
Source modes and owned resource/process/port cleanup are verified.
Real-window browser acceptance remains pending while the screen is unavailable.
These controlled results establish no external provider or fleet guarantee.
F30 remains Partial. Fleet acknowledgement, configuration commands and rollback
are separate requirements; this observer adds no such operation or guarantee.

See [Runtime](RUNTIME.md) for publication and admission boundaries and
[Database](DATABASE.md) for migration portability requirements.

## Accounting activation and route identity

The first admitted metered call freezes the quota calendar by setting the accounting activation marker. That marker and its update timestamp do not change routing or quota policy and are excluded from the route fingerprint. The activation marker remains part of the authorization projection, so installations still observe the changed authorization facts. Calendar revisions and all other policy inputs remain fingerprinted and invalidate an obsolete admission before upstream dispatch.

A deterministic admission/refresh interleaving test covers accounting activation, exactly one upstream dispatch, and continued invalidation after a calendar revision. This controlled reproduction establishes the mechanism; it does not establish the cause of an earlier full integration failure.
