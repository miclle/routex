# Resource limits and admission policy

Status: Personal/Project aggregate and Key policies support RPM, concurrency, IP restrictions, rolling five-hour/seven-day tokens, monthly tokens/money, and TPM. Version 19 source integrates conservative native text reservations and independent settlement; its precise API and remaining limitations are in [QUOTAS.md](QUOTAS.md). Delivered Team Session policies, User/Team defaults and scoped requests are documented separately below. Personal Key and Project aggregate monthly behavior are delivered in the final sections; broader templates and configurable notification thresholds remain separate scope. Database and process acceptance evidence is recorded separately from source implementation.

Bounded same-protocol failover uses one logical admission. Before attachment reads or the first dispatch, RouteX retains only candidates with current capacity and price evidence and reserves the maximum token and exact decimal money bound across them. Retries reuse that reservation and revalidate caller authority, limits, capacity, price, route, and egress evidence before dispatch. They never create another RPM/concurrency debit. If every executed attempt proves that no upstream work occurred, final or crash recovery settlement releases token and money holds as exact zero; any active, completed, or ambiguous work retains normal authoritative/unknown settlement rules.

## Implemented slice

Migration 14 adds `resource_limits` and the singleton `limit_installation`. Additive migration 19 extends the same policy rows with `tokens_5h`, `tokens_7d`, `tokens_month`, `tpm`, `money_month`, and `currency`, and creates `quota_settings` and `reservation_bounds`. Released migrations are unchanged. Existing accounts remain unrestricted until configured; applying a finite quota also requires complete historical coverage and supported capacity/price evidence. Defaults, Team limits, approvals, and alert controls are not accepted by these APIs.

The following routes use session authorization. PUT additionally requires CSRF, a same-origin request, a strong `If-Match` value from the latest read, and a nonempty reason. A first read returns ETag `"0"`. Successful writes publish before acknowledgment; retries of the same actor, prior ETag, normalized body and reason replay publication without an additional audit event.

| GET / PUT path | Authority |
| --- | --- |
| `/api/v1/admin/members/:user_id/limits` | Self or `members.read`/`limits.users.write` for reads; `limits.users.write` for writes |
| `/api/v1/projects/:project_id/limits` | Current manager or `projects.read_all`/`projects.limits.write` for reads; `projects.limits.write` for writes |
| `/api/v1/keys/:key_id/limits` | Current personal owner |
| `/api/v1/projects/:project_id/keys/:key_id/limits` | Current Project manager or existing platform Project management authority |

Example PUT body:

```json
{"rpm":60,"concurrency":4,"ip_mode":"allowlist","ip_ranges":["192.0.2.0/24"],"reason":"Application admission policy"}
```

GET returns `platform_currency`, `stored`, numeric/decimal `effective`, the conjunction in `ip_policies`, `account_id`, `etag`, optional parent ETag, live `rpm_used`/`active`, `quota_usage`, and `enforced`. Quota windows include a coverage flag, settled values, conservative holds, and unknown counts; counters before coverage must not be presented as a complete balance. Null numeric fields mean unrestricted aggregate or inherited child. Limits are checked on inference; IP restrictions also protect `/v1/models`, whose metadata reads do not consume RPM or inference concurrency. HTTP 429 distinguishes `rate_limit_exceeded` and `concurrency_limit_exceeded`; IP rejection is 403 `ip_not_allowed`. Rejected calls reach no upstream and consume no limiter capacity.

The stable Key account is derived from its oldest retained immutable rotation ancestor. Both overlapping credentials share policy and counters. Cycles, missing ancestors or cross-owner ancestry fail closed. No new Key ownership column or backfill is needed in this slice. PUT on either active credential edits that shared account. Parent reductions apply immediately to every descendant; setting a child field to null does not erase its counters.

RPM means successful durable gateway admission, including a later dial failure, timeout, cancellation or upstream rejection. The journal atomically stores admission, aggregate/Key RPM entries, concurrency leases and pending call fallback. Final fact completion releases concurrency exactly once. SQL event acknowledgment preserves RPM history; restart restores the rolling minute and releases orphan local leases. IP rejection, exhausted limits and journal write/capacity failure happen before admission. The journal retains at most 200,000 minute/account entries independently of its 4,096 pending event slots; reaching either capacity returns 503. Each ordinary Key call uses two account entries. Capacity and latency are bounded implementation choices, not measured production SLOs.

An established database is bound to one journal identity. Missing/foreign files refuse startup rather than reset quota. A deliberate restore must restore the matching SQL database and journal together; no automatic rebinding/reset endpoint exists. Back up both and operate one gateway process per installation. Filesystem locking prevents concurrent processes opening the same file, but separate copied files are not a distributed lock service.

Pure tests cover rolling boundaries, atomic aggregate/child contention, rotation continuity, policy publication ordering, IP/proxy trust, idempotent completion and journal recovery. `testResourceLimitLifecycle` extends the isolated PostgreSQL/MySQL harness with actual policy HTTP calls and controlled gateway execution; its final execution result belongs in stage acceptance evidence.

## Administrative Member Limits tab

The member composition moves the existing policy editor from Settings into
addressable `?tab=limits`, preserving its local budget/quota, request-rate/IP and
default-restore composition. The existing GET/PUT
`/api/v1/admin/members/:user_id/limits` and GET/POST
`/api/v1/admin/members/:user_id/limits/default-reset` remain authoritative. No new
API, policy, migration, permission, accounting or native admission behavior is
introduced. Member detail requires `members.read`; writes/reset independently
require `limits.users.write` and a current enabled target. Existing scoped backend
read authority is not broadened by this UI composition.

Only the member panel supplies managed freshness/dispatch guards. It checks exact
current Session/actor/target, permissions and member/policy cache generations
before dispatch and acceptance, aborting obsolete work. A same-actor/target
renewal or read error hides private policy facts/actions/dialogs while keeping
component-local draft and already-dispatched intent. Actor/target changes, logout
and tab exit destroy it. Renewed authority enables only explicit retry with fresh
CSRF; it never automatically writes or declares a late response applied.

Policy intent captures exact reviewed ETag, complete submitted body and reason.
An uncertain intent survives subsequent rejected retries; reviewing or canceling
the policy editor cannot silently replace it. New writes after changed policy or
currency need explicit review. Default reset uses the existing hook-free controls
with member-managed authority/generation and response guards. Its original review
and reason survive renewal/rejected retry; reset review cannot overwrite an
uncertain request. Explicit reset dismissal reports unresolved state rather than
success. Reset never clears journal usage. See [Defaults](DEFAULT_LIMITS.md).

Stored/effective/inherited values, zero/null, exact money and currency maps,
coverage, known subtotals, unknowns and retained/live holds keep their existing
meaning. No remaining allowance or currency conversion is inferred. Other
Personal/Project/Key/Team policy and reset callers retain their existing behavior.
Current-main source checks/build and controlled process/native/browser/restart
acceptance passed. Actual reviewed zero blocked inference without dispatch;
explicit default restoration retained usage and confirmed current runtime policy.
An observer HTTP503 masking a committed response exercised original-intent retry
without another write; it does not prove raw network-loss behavior. Final mandatory
check passed. The reviewed Limits composition is delivered as 2d5cafc with
exact push/read-back; all three exact remote workflows passed. Detailed checkpoints remain in
[Implementation](IMPLEMENTATION.md).

## Administrative Member Team policy display

The Member Teams table uses the resource-authorized member Teams endpoint and
requires independently current `members.read` AND `teams.read_all`. It is
read-only: Team-scoped ownership and quota-write permissions substitute for
neither read grant. There is no relationship or quota editor in this table.

Each row shows the inspected target's **stored** monthly Tokens/money and
RPM/TPM/concurrency. Policy record absence, null/Not set, explicit zero and
unavailable usage remain distinct. `parent_stored` is separate Team restriction
context; it is not an aggregate usage balance, allocated member pool, inherited
remaining allowance or a value to add to the member's cap. Null child limits do
not mean globally unlimited. Current resource-authorized platform currency is
context only and never converts historical charges.

The member's monthly known subtotal, coverage, unknown counts and retained holds
come from its stable Team/User journal account. Live reservations are a separate
nullable projection from the same bounded coherent account batch. Exact integer
and money strings are preserved; no percentages, progress or inferred remainder
is supplied. Each page and row retains its own observation/AsOf/window context,
so loaded pages are not presented as one complete atomic report.

Saved policy and current runtime proof are separate. Application requires exact
active subject/Team/membership, creation identities, full child/parent policy
revisions and normalized values, calendar/currency, current published lease and
no tombstone. Disabled/offboarded targets or inactive relationships/Teams retain
readable history with runtime application false. Missing/inactive/outage journal
facts remain unknown. Joined time cannot reset coverage, usage or accounting.
Current-main source checks/build and repaired focused PostgreSQL/MySQL tests
passed, including the six-child Overview regression; mandatory check passed.
Authentication lifecycle, native/browser/restart, a fresh complete matrix and
checked delivery remain pending. See [Governance](GOVERNANCE.md#administrative-member-teams-candidate).

## Decisions

- Personal and Project Keys retain their existing owners. Teams govern explicitly selected session contexts and never own Keys.
- Policy, runtime counters, and asynchronous usage reports are separate. Reports cannot authorize spending or reset counters.
- Every admission resolves one resource context, freezes its policy and price basis, and atomically checks all applicable aggregate and child constraints before dispatch.
- Zero is a closed allowance. Null is not zero: it means no local constraint, or inherited parent constraint where a parent exists.
- Numeric child overrides only narrow a hard parent cap. A soft Team aggregate monthly threshold may be lower than its hard member cap; other parent dimensions remain hard. IP restrictions are an intersection of predicates, not a replacement allowlist.
- User and Team defaults are templates for new resources. Changing a template does not silently change existing accounts. Reset is an explicit audited copy of the current template and never clears usage.
- Hard Token, TPM, and money limits require a verified finite reservation bound. Character counts, JSON sizes, estimated tokenizers, missing usage, and absent prices cannot be treated as exact limits or free usage.
- The first implementation is one gateway process with a persistent, exclusively locked local journal. It is not a multi-node quota service.

## Context and effective policy

| Request identity | Required context | Authorization | Counters checked together |
| --- | --- | --- | --- |
| Personal Key | Personal owner, derived from the Key | Active owner and Key; current personal grants intersect immutable Key model scope | Personal account and Key limit account |
| Project Key | Project, derived from the Key | Active Project with an enabled manager; active Key; Project grants intersect Key model scope | Project aggregate and Key limit account |
| Personal session | Explicit `{type: "personal", id: user_id}` | Session user is the context user; active account and personal grants | Personal account |
| Team session | Explicit `{type: "team", id: team_id}` | Active session user, Team and membership; Team grants | Team aggregate and this Team/member account |

Session inference is an upcoming endpoint, not an alternative authentication mode for current native Key endpoints. Missing, duplicate, mismatched, or ambiguous session contexts fail before admission. The client must send the user's selected context even when only one context is available. Personal or Project Key callers cannot supply a Team context to change attribution. A Team call checks neither personal budget nor another Team budget. Project calls never charge a manager or creator's personal account.

One call fact carries its actor (when applicable), context type/ID, Key ID, policy revision, runtime snapshot ID and charge basis. Aggregate and child counters are simultaneous enforcement views of that one fact, not two monetary charges. Reports must not add the two views together. Membership removal and rejoining do not erase Team/member usage: the durable account identity is `(team_id, user_id)`, independent of the membership row ID.

Defaults have no aggregate counters. Personal accounts and Teams copy the corresponding defaults at creation; preexisting resources migrate to unrestricted values to preserve current behavior. Projects begin with explicit unrestricted numeric values and no model access; no Team or user template is implicitly applied to a Project. An administrator can set Project policy, while managers can request an increase and narrow individual Keys.

For every numeric field, an inherited child uses the current parent value; an explicit child uses `min(parent, child)` with null treated as infinity. Write validation rejects a newly requested child value above its current finite parent. A later parent reduction clamps existing children at runtime without corrupting or automatically increasing their stored values. Raising the parent cannot exceed an existing explicit child ceiling. The response shows stored values, effective values, and the source of each inherited value.

A parent and each child retain distinct counters. For example, a Project's 100-token remainder and one Key's 30-token remainder admit a 25-token reservation only if both checks succeed; another Key cannot consume that reservation. Team member allowances do not preallocate the Team pool: member limits may sum above the Team total, but actual admissions always share the aggregate ceiling.

Rotation preserves a Key's immutable `limit_account_id`, restrictions, and used/reserved counters across the old/new overlap. A new unrelated Key receives a new limit account but still shares its owner aggregate. Revocation, disabling, edits, model grant changes, and reset-to-default never clear accounting history.

## Delivered schema and planned extensions

Use frozen GORM models and explicit table names in the new migration. Decimal amounts are canonical strings and integer quantities use signed 64-bit fields with checked arithmetic. API Token and rate values are nonnegative safe JSON integers (at most `9007199254740991`); negative, fractional, exponent-encoded money, unknown fields and duplicate scope identities are rejected.

| Table or extension | Fields and invariants |
| --- | --- |
| `quota_settings` singleton (delivered) | Organization IANA `time_zone` (initially `UTC`), revision/ETag and first-activation intent. No counter storage here. |
| `limit_defaults` (planned) | Unique `kind=user|team`; revision; all numeric fields below; currency; quota behavior; alert thresholds. |
| `resource_limits` (delivered) | Unique `(scope_kind, scope_id)` with `user`, `project`, or `key`; ETag, actor, reason, numeric fields, currency, IP policy, timestamps. Team/member associations and copied default revisions remain planned. Scope authorization is checked under the governance transaction lock. |
| `reservation_bounds` (delivered) | One provider-model capacity attestation, derived native protocol, finite input/output maxima, evidence, actor, reason, ETag. No model-name inference. |
| Numeric fields | Nullable `tokens_5h`, `tokens_7d`, `tokens_month`, `money_month`, `rpm`, `tpm`, `concurrency`. `money_month` is a decimal string; configured money has an explicit currency matching the platform currency. |
| IP fields | `ip_mode=none|allowlist|denylist` and normalized CIDR entries in a versioned JSON field. All entries use canonical network addresses. No hostname/DNS policy. |
| Alert fields (planned) | Quota warning percentages, initially 80 and 95, and per-threshold enabled flags. Thresholds do not change allowance. |
| Key account identity | Derive the immutable accounting ID from the oldest retained rotation ancestor; reject missing, cyclic or cross-owner ancestry. Policies refer to this accounting ID without changing Key ownership columns. |
| Call extensions (partly planned; durable quota receipts already retain scope/revision/bounds) | Context kind/ID, Team ID where relevant, limit admission ID, policy revision, reservation/settlement status. Keep exact actual price facts separate from conservative quota holds. |

The policy table is a bounded discriminated resource association, not an arbitrary policy engine. Each kind has explicit service validation and authorization; a Project ID cannot be used with a personal-Key endpoint. Domain resource IDs remain stable historical identifiers. Database dialect differences belong only in migration/database code.

`limit_defaults` snapshots and reset semantics avoid tri-state default ambiguity: null on a resource aggregate means unlimited; null on a Key or Team member means inherit. A per-field override can be cleared with an explicit null to return that child field to inheritance. Reset-to-default replaces all aggregate fields with the current template and records before/after values. No usage-reset API is included. Direct platform edits require a reason and If-Match/ETag; stale edits return 409.

The delivered Personal User and Team aggregate monthly slices below permit independent stop/alert-only for their own dimensions. Released V73/V74 extend this to exact Personal Key roots and Project aggregates. Project Key policies use separate immutable Project/root proof; the V75 source candidate adds independent Team-member modes with separate current Team/User proof. Accounting, currency/price, rolling, rates, concurrency and IP remain independent gates; fixed warning history is unchanged. Configurable thresholds and other scopes are separate phases.

## Windows, money and reset boundaries

- Five-hour and seven-day Token limits use exact rolling intervals `(now-window, now]`, not epoch-aligned buckets. All comparisons use UTC instants. One minute RPM and TPM use the same rolling definition.
- Monthly Token/money limits use `[first day 00:00, next first day 00:00)` in the organization time zone. Calendar arithmetic must handle DST and different month lengths; never assume 30 days or 720 hours.
- A request is attributed to its admitted timestamp and monthly period. Late settlement updates that original period. Active reservations remain admission guards until settled or recovered, including across a boundary; they cannot disappear merely because a stream outlived a rolling window.
- A durable accepted admission counts one RPM unit even when a subsequent dial fails, the upstream rejects it or the stream fails. Validation/IP/quota rejection before durable admission does not consume a unit. Bounded replay-safe attempts retain one logical admission and cannot silently reserve twice.
- Token totals are normalized input plus output, with cached tokens already included in input. Cache counts must not be added again. TPM first reserves the upper bound and later uses the actual total. Concurrency counts active logical upstream operations through response/stream close and releases exactly once.
- Money uses the existing pricing adapter's exact decimal semantics: at most 18 integer/fractional input digits, half-even rounding to 18 fractional digits per converted component, then exact sum. The quota compares canonical amounts without converting to floating point. Reservation uses conservative rounding upward, never downward.
- Snapshot actual provider-model prices, price ETag, currency and exchange rates before reserving. An in-flight price edit does not change a reservation or its final basis. Missing required rates, unsupported pricing dimensions, or absent exchange rates reject a money-constrained admission before dispatch; explicit zero prices remain free.
- Changing platform currency while any finite money policy or live monetary reservation exists is rejected until an explicit migration workflow is implemented. Values in different currencies are never compared or added. Existing historical call currency remains immutable.
- Time zone changes after accounting starts require an explicit maintenance migration. The initial implementation rejects them while retained quota periods or reservations exist, avoiding a reset bypass or overlapping calendar definitions. Monotonic elapsed time is used within a process; persisted logical time must not move backwards after restart.

Example: a Project budget of `"0.003"` with `"0.001"` settled and `"0.0015"` reserved has only `"0.0005"` available. Two concurrent requests that each require `"0.0004"` cannot both enter. If the accepted request settles at `"0.00025"`, only that actual amount remains charged and `"0.00015"` is released. An unknown result keeps `"0.0004"` as a visibly unresolved hold, not as an invented actual charge.

## Admission, settlement and durability

The existing call buffer persists before dispatch and removes delivered events after primary-database acknowledgment. It cannot itself be the quota ledger: an acknowledged event may still consume seven-day or monthly quota. Extend the single local journal with separate retained account/reservation indexes; do not derive admission counters from report tables or create an uncoordinated second transaction file.

The runtime store must commit the applicable aggregate and child checks, RPM unit, token/money reservations, concurrency lease and pending call fallback in one synchronous local transaction before upstream dispatch. A rejected transaction changes none of them. Only after this fsync does dispatch become possible. Completion atomically replaces pending state, updates quota amounts, releases concurrency, and makes the final call fact deliverable. Primary SQL delivery remains asynchronous and deduplicated; acknowledging the fact does not remove live quota usage.

Admission atomically stores a zero-work recovery settlement before dispatch is possible. Entering the first or any later active attempt checkpoints the interruption fact and clears that zero assumption. A crash after admission but before dispatch therefore releases economic holds and records explicit zero usage; a crash during active or ambiguous work retains the conservative hold. A completed sequence of only `not_sent` or `rejected_without_work` attempts also settles at zero. The already committed RPM admission remains counted in every case.

A reservation freezes the request ID, scope account IDs, policy revision, price basis, upper bounds, admitted timestamp, monthly period and terminal state. A duplicate settlement with identical facts is a no-op; a conflicting settlement is rejected without replacing the first accepted result. No prompts, outputs, bearer tokens or provider credentials enter the quota journal.

Confirmed complete usage replaces reserved amounts with actual values independently per dimension, including canceled or failed requests whose authoritative final usage is available. Missing/invalid final usage, broken SSE, and process interruption retain the affected conservative hold until its applicable windows expire. Unknown actual quantities remain null; known tokens can settle while money remains unknown. An initial implementation offers no manual release or refund action; reconciliation requires separate authorized evidence and audit. Concurrent leases can be released on process recovery because no previous process owns local streams, while economic holds remain durable.

If actual usage exceeds the reserved bound, persist the full observed debt and mark the adapter/model bound invalid; deny further constrained admissions through that route. Do not clamp usage to the reservation or claim a zero-overrun guarantee after provider contract violation. Successful bounded-adapter acceptance establishes zero local oversubscription under the declared upstream bound, not a guarantee against arbitrary provider misreporting.

Startup acquires the journal's exclusive lock, validates version and account identity, rebuilds retained counters, resolves previous process leases, and loads a valid runtime policy before listening. Corruption, missing established ledger, unsupported versions, write/fsync failure, or capacity exhaustion fails closed with 503. A database-side installation ledger ID binds the persistent file to this installation; a missing/mismatched existing file cannot silently initialize an empty quota ledger. Backup and restore must include the SQL configuration and matching persistent journal. One live process is supported; two processes sharing SQL but different journals are prohibited operationally until distributed coordination exists.

The store prunes only terminal entries older than every affected retention window and without undelivered facts or unresolved holds. Exact rolling-window indexes need an explicit capacity bound and measurements; reaching capacity rejects new admission rather than evicting accounting state. The current 4096 pending event slots bound delivery backlog, not seven-day usage history, and must not be reused as quota retention capacity.

A SQL outage can defer reporting while existing runtime authorization is still leased; it does not refund or lose quota. The existing five-second authorization lease still expires safely. This implementation does not extend authorization during a database outage or claim HA availability. Runtime policy reductions install a scope deny marker before refresh and complete synchronous publication before a successful mutation acknowledgment. Already admitted bounded work settles against its frozen basis; the new policy applies to every subsequent admission. Failed refresh returns 503 and leaves the affected scope blocked rather than serving an old broader policy.

## Reservation bounds: required implementation gate

Version 19 adds explicit provider-model capacity attestations. Discovery still supplies no proven input/output bound, and unconstrained gateway requests retain native extensions. The post-call pricing adapter alone cannot establish a reservation. Constrained traffic requires both a capacity attestation and a supported native cap; see [QUOTAS.md](QUOTAS.md).

The minimal conservative supported adapter requires validated provider-model capacity metadata and documented request semantics: maximum billable input, maximum billable output, a recognized and enforced native output cap, choice count, cache dimensions and applicable pricing tiers. Restrict the first hard-budget path to supported text requests and one output choice. Reject hosted tools, audio, images, unknown billing dimensions, absent/contradictory output caps, and unknown model capacity before dispatch when a hard quantity constraint applies. Unconstrained existing calls retain their native behavior.

Without a verified tokenizer, reserve the full declared maximum billable input plus the enforced output cap. This is deliberately conservative and can reject a small request when less than the model's full input capacity remains. The UI must explain the reservation requirement rather than silently shrink native parameters. A later verified protocol/model tokenizer may tighten the bound without changing ledger semantics. A raw byte count is not a proven substitute.

For money, maximize the frozen applicable schedule across possible base/long-context tiers and input/cache classifications, then add the output cap cost, applying conservative decimal rounding. Taking only the ordinary input rate is unsafe when cache-write or another applicable tier costs more. Unsupported/missing rate combinations reject rather than assuming zero. The upstream must satisfy the declared bound; controlled contract tests establish deterministic behavior, and real provider acceptance remains a separate requirement.

## Trusted client IP

IP enforcement begins from `Request.RemoteAddr`, parsed as a literal address and normalized with IPv4-mapped IPv6 unmapping. Never use a framework convenience client-IP function with implicit proxy trust. Network entries accept single IPv4/IPv6 addresses or CIDRs; canonicalize host bits, deduplicate, and reject zones, hostnames, ports, invalid prefixes, and empty allow/deny lists. A single address becomes `/32` or `/128`. IPv4-mapped CIDRs must be unambiguously canonicalized or rejected, never interpreted inconsistently.

Default `trusted_proxies` is empty, so all forwarding headers are ignored. An explicit bootstrap CIDR allowlist enables only `X-Forwarded-For` handling when the direct peer is trusted. Require one bounded header value (at most 16 addresses), validate all entries, append the socket peer, and walk from right to left through trusted hops to the nearest untrusted address. A trusted peer without a valid chain is rejected; do not guess a client from a malformed chain. `Forwarded` and `X-Real-IP` are not alternative sources in this first version. Untrusted peers cannot override their socket address by sending any header.

For each applicable policy, `none` contributes true; `allowlist` requires membership in one listed range; `denylist` requires absence from every listed range. The final decision is the conjunction of all parent and child predicates. A Key's `none` never removes its owner's rule, and an apparent broader child allowlist never bypasses a parent denial. Empty intersections are an explicit blocked policy. Request errors do not echo network topology or secret header values.

## Management and approval contracts

The following table describes planned extensions and the intended authority of shared endpoint families. Delivered Personal/Project/Key routes are listed above; separate defaults, Team, approval, and `/limits/effective` endpoints are not implemented.

| Endpoint family | Read authority | Write authority |
| --- | --- | --- |
| `/admin/limits/defaults/:kind` | `limits.read` | `limits.defaults.write` |
| `/admin/members/:id/limits` | `members.read` plus limit read authority | `limits.users.write` |
| `/teams/:id/limits` | Current active member or `teams.read_all` | Team aggregate: `limits.teams.write`; Team/member narrowing: current owner or that permission |
| `/teams/:id/members/:user_id/limits` | Self, current owner, or `limits.teams.write` | Current owner may narrow within Team policy; platform permission may override within current total |
| `/projects/:id/limits` | Current manager or `projects.read_all` | `projects.limits.write`; manager identity alone cannot expand aggregate policy |
| Existing Personal/Project Key mutation endpoints | Existing owner/manager authority | Existing owner/manager may set restrictions only within effective owner policy |
| Corresponding `/limits/effective` read | Same resource read authority | Read only; stored/effective/source/revision and current ledger freshness, never editable counters |

Permissions must be explicitly registered and included in built-in administrator capability. Custom roles gain nothing automatically. All writes use current authority, active-resource checks, optimistic concurrency, audit and synchronous runtime publication. Reset-to-default is a distinct action with reason and ETag. The bootstrap timezone/proxy configuration and live enforcement status must be visible in an authorized read view; UI controls cannot imply that saved but unimplemented limits are active.

F18 follows enforcement, not just policy storage. Team requests initially cover monthly Tokens and money, one pending request per applicant/Team/dimension. Snapshot the submitted member value, member usage, total value, aggregate usage and freshness; requested target must increase the current effective member allowance. A current nonapplicant enabled owner handles the first stage. A request above the current finite Team total escalates without changing any policy; if no eligible owner exists, it starts at a dimension-authorized quota administrator. Unlimited is infinity, not zero. An applicant cannot approve any stage. Final escalation approval changes aggregate and member rules atomically; member limits never reserve aggregate capacity.

Project quota and request-limit requests reuse the established scoped, nonself first-decision workflow but carry explicit requested fields and baseline policy revision. Approval changes only those fields after revalidation and must not restore unrelated stale limits. Define separate `QUOTA` and `REQUEST_LIMIT` payload validators when those kinds are implemented; current `MODEL_ACCESS` remains unchanged. Stale revisions require fresh review rather than unconditional overwrites.

Both request families distinguish persisted approval from runtime application status. Publication failure leaves a recoverable committed decision with `apply_failed`/pending publication metadata; identical action retries attempt publication without charging or applying twice. Rejected, withdrawn and escalated requests have no effective policy changes. Administrative record access alone is read-only and does not grant approval authority.

## Delivery slices and verification

| Slice | Independently reviewable delivery | Required evidence |
| --- | --- | --- |
| 1. Policy contracts | Pure resolver/validators, frozen GORM policy schema, ETags/audit, read/effective APIs and permission matrix. Do not expose successful writes for unsupported dimensions as enforced. | Unit boundary/property tests; both databases including migration upgrade, duplicate identity, stale ETag, authorization and atomic audit. |
| 2. RPM, concurrency and IP | Personal/Project aggregate + Key enforcement, rotation continuity, trusted proxy settings, actual gateway rejects and durable restart behavior; enable only these writable UI controls. | Barrier-driven concurrent admissions; exact rolling boundary tests; ordinary/SSE cancellation; duplicate release; socket/header/CIDR matrix; restart with ongoing and completed calls. |
| 3. Token/money budgets | Verified text bound, atomic retained reservations, settlement/unknown holds, monthly/rolling windows, pricing snapshots, fail-closed journal recovery. Enable Token/TPM/money forms only now. | Fake-clock and controlled upstream tests; both DB management tests; process crash injection at every fsync/dispatch/settle/delivery boundary; price/currency changes; missing/invalid usage and cache/tier scenarios; bounded load evidence. |
| 4. Explicit Team context | Session inference with personal/Team selection, membership/runtime revocation, Team aggregate + member rules and single attribution; existing native Key endpoints unchanged. | A04 personal/two-Team ambiguity and missing context; no cross-pool debit; disabled membership; same user in several Teams; rejoin preserving usage. |
| 5. Requests and alerts | Team owner/escalation and Project limits approval, runtime application status, durable threshold notifications, then explicitly supported alert-only personal budgets. | A13 concurrent/self approvals, aggregate/member atomicity, stale baselines, publication retry; threshold deduplication/retry; read-only admin views. |

Critical deterministic cases include: many requests racing for the last allowance (exactly the permitted number enters), unrelated accounts proceeding independently, parent and child rejection with no partial debit, same Key rotation overlap, quota zero versus null, reductions below current usage, reset without usage loss, DST/calendar and rolling boundary timestamps, preserved price/FX through settlement, duplicate reports, and failure to persist before dispatch producing zero upstream requests. Every fault case verifies both returned status and durable accounting state.

IP tests cover untrusted forged headers, trusted multi-hop chains, missing/duplicate/malformed forwarding headers, IPv4, IPv6, mapped forms, CIDR host-bit normalization, parent allow plus child deny, and an empty effective intersection. No public network or external provider is needed for these deterministic tests.

The UI displays stored and effective rules separately, per-dimension inheritance, scope/currency/timezone, used/reserved/unknown holds, and publication status. It uses the existing resource configuration and Key dialogs rather than adding a fake request type or reporting a hard limit as active before gateway enforcement. English/Chinese behavior tests verify zero/null inputs, decimal strings, permission boundaries, narrowing, reset confirmation, stale edits, retry, and success only after publication acknowledgment.

Full F09/F17/F18 acceptance remains open until the relevant slices, measured single-process capacity/recovery tests, and applicable provider contracts have passed. Multi-node partitions, distributed counters and HA recovery are separate work.

### Member Teams process acceptance checkpoint, 2026-10-05

The standard PostgreSQL/MySQL authentication lifecycle passed, including real
process restart, persisted sessions, revocation, encrypted credentials, native
ordinary/streaming calls and immutable call history. Its owned Compose resources
were independently absent afterward. The R1 controlled native Teams run failed
at the live-reservation observation before its browser checkpoint. This is an
unresolved acceptance failure, not a delivered feature or browser pass. The
failed run was cleaned up and its exact owned containers, networks, volumes and
application listener were independently absent. All 164 protected source paths
and the checked production binary remain unchanged. Diagnose the observed
reservation values, rerun native/browser/restart acceptance, then pass a fresh
complete 89-case-per-driver matrix before committing this phase.

### Member Teams native and browser acceptance passed, 2026-10-05

Controlled process R3 passed eight immutable native completions (six known usage,
two missing usage), two membership denials without upstream dispatch, three real
join-date writers, nine typed continuity Team audits and same-artifact restart.
The primary member retained 12 known Tokens and exact 12.000000000000000004 USD;
aggregate 15 included its independent peer. A finite missing-usage call retained
5 Tokens and the conservative 5.000000000000000003 USD bound; the separate
unbounded case retained one unknown record. Original and rejoined membership
attribution, policies, ciphertext and immutable history survived restart.

R1/R2 failures remain above: R2 measured that only the helper's active-money
expectation differed. R3 corrected that single constant to the measured bound
supported by pricing component rounding; product source/binary stayed unchanged.
Actual browser acceptance passed English-default and live Chinese switching,
exact amounts, historical unknown joins, five target-only relationships, disabled
and archived state, keyboard tooltip and Escape, refresh, horizontal table access
and 390px mobile containment. Switching to the member-reader account kept the
Teams deep URL while hiding its private table and showing independent authority
guidance. Browser inspection produced no additional native dispatch or changes
to captured audits, memberships, ciphertext or history. Five rows are not proof
of the default 20-row Load More workflow; that boundary has source/driver tests.
The owned tab closed, viewport reset and Compose resources/listener were
independently absent. All 164 protected paths remained exact. A fresh standard
complete 89-case-per-driver PostgreSQL/MySQL matrix is running; commit/push remain
pending its success.

### Member Teams complete local acceptance passed, 2026-10-05

The fresh standard `go tool task test-integration` passed (exit 0) after the
value-comparison fixture correction: the unchanged ordered 89-case harness ran
against PostgreSQL and MySQL, Handler 1526.419s. Configuration 1.744s, database
1.630s, errors 1.467s and service 8.107s also passed. Its exact owned Compose
containers, networks and volumes were independently absent. All 164 protected
source hashes, V53 and the checked production binary remain exact. Together
with source format/check/test/build (2302 frontend cases/120 files), R6 focused
regression, mandatory check, both-driver auth/process lifecycle and R3
native/browser/restart acceptance, this phase is ready for a scoped main commit
and push. Previous failed fixture/helper runs remain explicit historical
evidence. Remote delivery and CI are not yet claimed.

## Personal User monthly behavior

Only a User's own monthly Tokens and money caps support independent `stop` or
`alert_only`, through the existing Member Limits API/editor. User stored policy
and its User entry in `ip_policies` expose canonical `tokens_month_behavior` and
`money_month_behavior`. Omission on full replacement resolves to stop; explicit
null/empty/non-string/unknown modes are invalid. Defaults/reset normalize to stop.
Other stored scopes reject behavior fields. A null cap is disabled and retains an
inert saved mode; zero is a real threshold.

Alert-only bypasses only the matching User monthly capacity-exceeded decision.
Accounting availability, known coverage, holds, conservative reservation bounds,
price/currency checks, account birth/current publication, rolling tokens, rates,
concurrency, IP and every other account remain independent gates. Metering and
fixed warning/exhaustion history are unchanged. A Personal Key's hard cap still
applies when its User parent is alert-only. User `[stored]` and Personal Key
`[User parent, Key stored]` chains retain their exact ownership; numeric effective
minima are informational configured-cap projections, not one stopping policy.

The editor confirms complete caps/modes/currency/reason/ETag. Definite first
validation/conflict failures retain an editable draft for explicit review; after
uncertain publication every failed retry retains the original exact intent.
Restore preserves historical non-secret modes but sends its unchanged reset
request and confirms current canonical stop modes. No Key mode editor is added.

The scoped Personal/Provider candidate excludes pending Excel changes and passes
its own exact-source full133 regression: 266 direct lifecycle cases, eight
constraints and 4,756 balanced named results. Acceptance SHA-256:
`ccb38e2b4b508f6304c2246b0d161e18e66263a0d023fd7ca332970af26b4ac2`.
Its 1,034 backend paths differ from the older private 1,036-backend candidate. Controlled Personal browser/native R4 verifies independent
zero stopping, alert-only admission, inactive null caps, English/Chinese drafts,
keyboard confirmation, independent permissions and a hard Key cap under an
alert-only User parent. Seven gateway calls produce three completed native
attempts and four pre-admission rejections with null usage and no attempts.
Same-artifact restart preserves original Sessions and sampled rows; owned cleanup
is verified. Acceptance SHA-256: `75b6b5265014caa9c63b531275c0f89f98ab8fb7512e00c217a446388eccf8cc`.
Current contextual main checks and delivery are tracked in [Implementation](IMPLEMENTATION.md).
F17 remains partial; the following Team monthly behavior is delivered separately.

## Team aggregate monthly stop and alert behavior

The existing Team aggregate GET/PUT `/api/v1/teams/:team_id/limits` exposes
independent `tokens_month_behavior` and `money_month_behavior` values, exactly
`stop` or `alert_only`. Stored aggregate policy and the aggregate parent-chain
entry include canonical modes; absent/legacy hard values read as stop. Numeric
`effective` values do not merge these modes or establish remaining allowance.
The V75 member endpoint exposes independent local modes and the separate Team
parent modes. Project aggregate and proved Personal/Project Key root behavior are
covered by their separate sections below.

Token/money modes require their independent Team dimension write permission;
ownership or read permission is insufficient. A sparse omission preserves the
corresponding saved mode, including ordinary cap/request changes. Mode-only writes
are permitted with fresh authority. Explicit null/invalid modes are rejected; the
V75 member endpoint accepts only its independently authorized monthly modes. Exact
money, denomination, reason and reviewed
composite If-Match remain mandatory. First conflicts require explicit review;
uncertain publication retains the exact original target/body/ETag through rejected
manual retries. A matching current GET cannot resolve the historical uncertainty.

Null caps disable only their own controls; zero is a real threshold. Alert-only
bypasses that exact aggregate monthly capacity comparison, while each independently
hard member dimension, other monthly dimension, rolling Token/rate/IP/concurrency gates, finite bounds,
prices/currency, journal coverage/unknowns, exact births and current lease remain
required. Reservations and settlement continue; missing usage is not zero.
A soft parent numerical ceiling does not prevent a larger hard member cap.
Creation/default restoration copy hard stop modes and never reset counters.
Existing near/critical/exhaustion notifications and recipient history are unchanged.

The Team editor uses the existing layout, local Base UI controls and paired
English/Chinese labels. Normal saves retain local uncertain intent while mounted;
this is not a new AuthGate-remount guarantee. The existing shared Restore boundary
retains the original non-secret Team modes for explicit authorized reset retries.
Private controlled browser/native/restart acceptance passes. Exact staged checking,
complete Task (4,402 frontend cases), build and its own focused dual-driver
regression pass; the containing commit delivers this bounded phase.
Source, staged focused-driver and full-private evidence are tracked separately in
[Implementation](IMPLEMENTATION.md).

## Personal Key monthly behavior (V73)

The existing owner-scoped Personal Key limit API and detail editor support
independent monthly `tokens_month_behavior` and `money_month_behavior`: `stop`
or `alert_only`. This extends the earlier User-only and Team-only phases; their
acceptance records above retain their original scope. Full replacement omission
resolves to stop; explicit null, unknown and non-string modes are rejected. A
null cap disables that dimension while retaining its inert saved mode. Zero is
a real threshold. Stored Personal Key policy and its own parent-chain entry
expose canonical modes; numeric effective minima do not merge account behavior.

User and Personal Key accounts enforce each monthly dimension independently.
An alert-only User threshold of 100 does not stop a Key whose hard threshold is 200;
the Key still stops at 200. A soft Key never bypasses a hard User threshold.
Rotation retains the exact owner and shared quota-root identity, policy and
usage. Project Keys use independent Project/root proof and never borrow Personal Key mode authority.

Only the matching monthly capacity comparison can be bypassed. Accounting
coverage, unknown usage, conservative holds and bounds, price and currency
checks, rolling windows, RPM, TPM, concurrency, IP, current owner and runtime
publication remain required. Warning thresholds remain 80 percent reminder and
90 percent critical, based only on authoritative settled usage. Saving changes
no counters and adds no guessed allowance.

The existing Base UI editor confirms complete policy, exact decimal money,
currency, reason and reviewed If-Match. First definite conflict requires explicit
review. Every uncertain or rejected retry retains the original immutable
request while its owner component remains mounted. Current-content reads do
not prove the historical write. Actor changes, logout and unmount destroy local
state; this phase adds no secret storage or remount recovery.

## Project aggregate monthly behavior (V74)

The existing `GET`/`PUT /api/v1/projects/:project_id/limits` exposes canonical
`tokens_month_behavior` and `money_month_behavior` in stored policy and its sole
IP-policy entry. Each is `stop` or `alert_only`; effective policy stays numeric.
Exact admitted current managers retain scoped reads and numeric requests. Direct
editing requires existing `projects.limits.write`, with no new permission or
creator/admin override. Full PUT omission resets each mode to stop; explicit
null, empty, duplicate, unknown, case/space variants and non-string modes reject.
Null caps retain an inert mode; zero is a real threshold. Money remains an exact
decimal string in the current authorized denomination.

The exact Project Key chain contains Project parent followed by the independent
Key policy. Both expose canonical stored modes; numeric effective minima remain
configured-cap facts rather than a merged stopping decision.
A soft Project threshold of 100 can admit a valid 150 reservation under a hard
Key threshold of 200. A hard Project threshold of 100 still rejects it. These
are independent account/dimension decisions, not a synthetic numeric minimum.
Rolling Tokens, rate/concurrency/IP, finite bounds, pricing/currency, holds,
coverage/unknown usage, lifecycle and current publication remain required.

Numeric QUOTA/RATE_LIMIT request bodies do not accept modes. Approval copies
the current complete Project policy and changes only submitted numeric values,
preserving both current modes. ETags, review tokens, audit and application proof
include modes. Accounting, original Key root identity and fixed settled 80/90
warnings retain their existing current-manager recipient and read-state rules.

The existing Resource configuration uses two adjacent switches and explicit
Base UI confirmation. Fresh exact actor/Project authority gates private fields
and dispatch; every failed uncertain manual retry retains original bytes and
validator. The intent is mounted-only. Project Key summaries show parent behavior
separately from their own controls without inferred remaining capacity.

## Candidate verification boundary

The repaired Personal Key candidate separately passes four selected real
PostgreSQL/MySQL migration/lifecycle cases, unchanged-source/mode checks and
owned cleanup. The original Vault Full137 separately passes 274 direct cases
and eight constraints on its original source. Those receipts do not transfer to
the later historical Cleanup correction or combined Project candidate.

The final 1,769-path combined source passes formatting, mandatory checking,
complete Task (4,506 frontend tests across 181 files), production build and
expanded Focus18 on PostgreSQL/MySQL. Full141 passes 282 direct lifecycle cases, eight constraints and 4,944 balanced named results on both databases in 2,937.954 seconds; exact source and owned cleanup are independently verified. Earlier
obsolete API, historical migration, manager and timestamp/coverage fixture
failures remain retained and are not reinterpreted as successful runs.

Personal Key controlled API/native/restart acceptance passes 19 logical calls,
seven native attempts, 12 pre-admission denials and five original Sessions, with
identical bounded before/after database snapshots and independently verified
owned cleanup. Root accepts this R6 API-only result with SHA-256
`b5de98a326cc009864b555204203e3f60375382afc9ac33d0b4ac55b03a9e5ba`.
TPM is observed as quota_exceeded; RPM remains rate_limit_exceeded. The earlier
failed TPM run lacks its rejected body, so that historical body remains unknown.

Controlled Vault API R7 passes configuration, retained-auth Cleanup, persistent Vault/application restart and both original API Sessions. Root independently verifies six revisions, two probes, four stage commands, ten paired Vault requests (six successful effects and four ACL denials), seven observed root domains, the real 300-second observation and completed retirement. Before/after/finish database projections are identical; source, artifact/config and exact owned cleanup remain verified. Root API-only review SHA-256: `efb14524ce1c06fe6de54be050d03e7d10946a6cd3c93fde25be6886c73cf538`. Earlier failed runs, including the restart-wait failure, remain failed; the fixed loopback-port successor does not reinterpret their missing evidence.

Both Vault R7 and Personal R6 API runs use the original R3 artifact `19320f5e16792e790748feb0a69fd6f70d1c3fc4f571a5e27c6e70d8494b5df4` and source floor `1b78ece3a09c458141ceff256f702822c7e9f0b714766e8458a3b92520276101`. The later R4 parent-callback repair changes a test fixture only; its whole-source/backend-test hashes and gate receipts remain distinct even though production code is identical. API results do not become R4 whole-source acceptance. Expanded Focus18 and accepted Full141 belong to that later source. Full141 acceptance SHA-256 is `03af8b6e72e578f1ccb777c4248ed591cfd4958143b9c4af0134ef92b654cb1e`.

The bounded 97-path phase is delivered on main as `c2368ddb0251415e72bfeee948ad23ed7a173ee4`, with exact remote read-back and final staged mandatory checking. Genuine browser, live bilingual controls and AuthGate recovery remain pending; source UI tests do not replace those checks. Desktop control reports a locked Mac and the in-app browser cannot attach a new webview; those observations do not prove a product cause or UI/download success. A bounded fresh normal-browser gate must verify the existing controls, independent authority and original-Session restart against an exact reviewed artifact. No complete F17/F28 or full-objective acceptance is claimed. See [Implementation](IMPLEMENTATION.md) for current scope and exact evidence.

## Project Key monthly behavior

The existing Project-scoped Key GET/PUT limits route and Key dialog support
independent monthly Token and money `stop`/`alert_only` modes. Reads and writes
retain current Project management authority. Complete PUT omission resets each
mode to stop; explicit null, non-string and noncanonical modes reject. Null caps
retain inert saved modes, while zero is a real threshold. Both stored Key policy
and its own parent-chain entry expose canonical modes. Money stays an exact
decimal string in the authorized platform currency.

The service proves an exact immutable Project, root birth and complete rotation
chain before assigning a Project Key soft policy. Generic shared `key` storage
never supplies that authority. Missing/cross-owner/aliased identities, Personal
collisions and malformed chains fail closed. A confirmed descendant shares the
original root, policy and counters after predecessor revocation. Project facts
retain Project attribution and never debit a creator or manager's Personal pool.

Parent and Key monthly dimensions stop independently: a hard parent still blocks
a soft child, and a hard child still blocks a soft parent. Alert-only bypasses
only that proved account/dimension capacity comparison. Coverage, unknown usage,
holds, native finite bounds, price/currency, rolling Tokens, request rates,
concurrency, IP and current publication remain required. Fixed settled 80/90
notifications retain exact current-manager recipients and independent read state;
exhausted caps are handled by the existing exhaustion notification contract.

The existing dialog keeps explicit confirmation, complete reviewed policy,
If-Match, reason, exact decimals and current actor/Project authority. Failed
uncertain retries retain the original mounted intent; matching current content
never proves the original historical write. No secret or draft enters browser
storage. This package adds no schema version or counter reset. Source, driver,
controlled runtime and browser acceptance are recorded separately in
[Implementation](IMPLEMENTATION.md). Team-member V75 source behavior is described below; actual acceptance remains separate.

## Key editor asynchronous ownership

Personal and Project Key limit reads use actor- and resource-scoped query lifetimes.
An asynchronous save may publish facts only to the same mounted owner, target,
and current Session generation. A same-owner renewal retains the original save
as uncertain and releases the pending state; manual retry uses fresh authority
and CSRF with the original policy and review token. Actor changes, expired or
missing Sessions, target changes and unmount discard stale completion callbacks.
Never recreate private cache entries or report enforcement from an obsolete response.

The narrow frontend correction passes 103 focused tests across five suites, types,
lint and formatting. Complete candidate formatting, mandatory checking, tests (4,520 frontend cases
in 181 files) and build pass. The backend matrix failure and fixture successor
remain separately recorded in Implementation. Accepted database and
API receipts retain their original source and artifact identities; this guard does
not change persistence, quotas, permissions or native dispatch.

## Independent Team-member monthly modes (V75 source candidate)

The existing member GET/PUT adds independently stored `tokens_month_behavior` and
`money_month_behavior`, each exactly `stop` or `alert_only`. Stored member policy
and chain entry 1 emit both canonical modes; entry 0 is the independent Team parent.
Numeric effective limits remain configured-cap facts. Legacy empty modes read as
stop. Sparse omission preserves a mode; explicit null, aliases and unknown values
are rejected. Mode-only Token/money edits require `teams.tokens.write` and
`teams.money.write` respectively; ownership/read access does not grant mutation.
Rates keep their existing separate permission. Numeric quota approvals preserve modes.

Only the proved member account's own monthly capacity comparison can be skipped.
The exact current Team/User/membership and original Team birth establish that proof,
not an account prefix. Stable pair accounting survives removal/rejoin and restart;
removed members cannot continue admission. Hard aggregate caps, unknown usage,
coverage, holds, finite reservation, prices/currency, rolling limits, TPM/RPM,
concurrency, IP and publication fences remain required. Existing private member
80/90 warnings and immutable settlement retain their original semantics.

The existing member dialog exposes two independent switches and an explicit Base UI
confirmation, with paired English/Chinese copy. Reads/rendering/manual dispatch use
fresh Session generation, exact actor/target and published editability. Uncertain
retries keep original bytes/modes/ETag and current CSRF; matching GET is current
configuration, not an original-operation receipt. This intent is mounted-only;
aggregate/default Restore and remount behavior are unchanged.

The private Team R3 source candidate passes formatting, mandatory checking,
complete Task tests and production build, including 4,526 Vitest cases in 181
files, Go race/coverage, Node and asset checks. Its two UI completion-fence leaves
are the only changes from Team R2; all backend bytes and modes remain identical.
Same-owner Session renewal retains an in-flight original submission as uncertain,
releases busy state and requires a fresh authorized manual retry, even after a
late HTTP200. Actor/target changes, logout and unmount discard that ownership.

Original failed Focus4 evidence remains retained. Database, controlled API/restart,
browser and delivery receipts keep their exact source/artifact identities and are
tracked separately in [Implementation](IMPLEMENTATION.md). R3 source gates do not
relabel R2's whole-source database evidence. The registry preserves the 142-case
prefix and appends only the two Team-member scenarios.
