# Resource limits and admission policy

Status: Personal/Project aggregate and Key policies support RPM, concurrency, IP restrictions, rolling five-hour/seven-day tokens, monthly tokens/money, and TPM. Version 19 source integrates conservative native text reservations and independent settlement; its precise API and remaining limitations are in [QUOTAS.md](QUOTAS.md). Team/session contexts, defaults, approvals, alerts, and reset-to-template remain planned extensions for F09/F17/F18 and A04/A11/A12. Database and process acceptance evidence is recorded separately from source implementation.

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

GET returns `stored`, numeric/decimal `effective`, the conjunction in `ip_policies`, `account_id`, `etag`, optional parent ETag, live `rpm_used`/`active`, `quota_usage`, and `enforced`. Quota windows include a coverage flag, settled values, conservative holds, and unknown counts; counters before coverage must not be presented as a complete balance. Null numeric fields mean unrestricted aggregate or inherited child. Limits are checked on inference; IP restrictions also protect `/v1/models`, whose metadata reads do not consume RPM or inference concurrency. HTTP 429 distinguishes `rate_limit_exceeded` and `concurrency_limit_exceeded`; IP rejection is 403 `ip_not_allowed`. Rejected calls reach no upstream and consume no limiter capacity.

The stable Key account is derived from its oldest retained immutable rotation ancestor. Both overlapping credentials share policy and counters. Cycles, missing ancestors or cross-owner ancestry fail closed. No new Key ownership column or backfill is needed in this slice. PUT on either active credential edits that shared account. Parent reductions apply immediately to every descendant; setting a child field to null does not erase its counters.

RPM means successful durable gateway admission, including a later dial failure, timeout, cancellation or upstream rejection. The journal atomically stores admission, aggregate/Key RPM entries, concurrency leases and pending call fallback. Final fact completion releases concurrency exactly once. SQL event acknowledgment preserves RPM history; restart restores the rolling minute and releases orphan local leases. IP rejection, exhausted limits and journal write/capacity failure happen before admission. The journal retains at most 200,000 minute/account entries independently of its 4,096 pending event slots; reaching either capacity returns 503. Each ordinary Key call uses two account entries. Capacity and latency are bounded implementation choices, not measured production SLOs.

An established database is bound to one journal identity. Missing/foreign files refuse startup rather than reset quota. A deliberate restore must restore the matching SQL database and journal together; no automatic rebinding/reset endpoint exists. Back up both and operate one gateway process per installation. Filesystem locking prevents concurrent processes opening the same file, but separate copied files are not a distributed lock service.

Pure tests cover rolling boundaries, atomic aggregate/child contention, rotation continuity, policy publication ordering, IP/proxy trust, idempotent completion and journal recovery. `testResourceLimitLifecycle` extends the isolated PostgreSQL/MySQL harness with actual policy HTTP calls and controlled gateway execution; its final execution result belongs in stage acceptance evidence.

## Decisions

- Personal and Project Keys retain their existing owners. Teams govern explicitly selected session contexts and never own Keys.
- Policy, runtime counters, and asynchronous usage reports are separate. Reports cannot authorize spending or reset counters.
- Every admission resolves one resource context, freezes its policy and price basis, and atomically checks all applicable aggregate and child constraints before dispatch.
- Zero is a closed allowance. Null is not zero: it means no local constraint, or inherited parent constraint where a parent exists.
- Numeric child overrides only narrow a parent. IP restrictions are an intersection of predicates, not a replacement allowlist.
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

Token/money behavior is `stop` for all Project, Team and Key hard limits. Personal quota policies may later support `alert_only`; that mode must be visibly nonblocking and still meter actual usage. RPM, TPM, concurrency and IP are always hard admission rules. A child cannot soften a hard parent. Until durable threshold notifications exist, APIs must reject `alert_only` and alert-setting writes rather than expose inactive controls. Threshold delivery is a separate feature slice with deduplication by account, dimension, period and threshold crossing.

## Windows, money and reset boundaries

- Five-hour and seven-day Token limits use exact rolling intervals `(now-window, now]`, not epoch-aligned buckets. All comparisons use UTC instants. One minute RPM and TPM use the same rolling definition.
- Monthly Token/money limits use `[first day 00:00, next first day 00:00)` in the organization time zone. Calendar arithmetic must handle DST and different month lengths; never assume 30 days or 720 hours.
- A request is attributed to its admitted timestamp and monthly period. Late settlement updates that original period. Active reservations remain admission guards until settled or recovered, including across a boundary; they cannot disappear merely because a stream outlived a rolling window.
- A durable accepted admission counts one RPM unit even when a subsequent dial fails, the upstream rejects it or the stream fails. Validation/IP/quota rejection before durable admission does not consume a unit. No upstream retry exists today; future attempts retain one logical admission and cannot silently reserve twice.
- Token totals are normalized input plus output, with cached tokens already included in input. Cache counts must not be added again. TPM first reserves the upper bound and later uses the actual total. Concurrency counts active logical upstream operations through response/stream close and releases exactly once.
- Money uses the existing pricing adapter's exact decimal semantics: at most 18 integer/fractional input digits, half-even rounding to 18 fractional digits per converted component, then exact sum. The quota compares canonical amounts without converting to floating point. Reservation uses conservative rounding upward, never downward.
- Snapshot actual provider-model prices, price ETag, currency and exchange rates before reserving. An in-flight price edit does not change a reservation or its final basis. Missing required rates, unsupported pricing dimensions, or absent exchange rates reject a money-constrained admission before dispatch; explicit zero prices remain free.
- Changing platform currency while any finite money policy or live monetary reservation exists is rejected until an explicit migration workflow is implemented. Values in different currencies are never compared or added. Existing historical call currency remains immutable.
- Time zone changes after accounting starts require an explicit maintenance migration. The initial implementation rejects them while retained quota periods or reservations exist, avoiding a reset bypass or overlapping calendar definitions. Monotonic elapsed time is used within a process; persisted logical time must not move backwards after restart.

Example: a Project budget of `"0.003"` with `"0.001"` settled and `"0.0015"` reserved has only `"0.0005"` available. Two concurrent requests that each require `"0.0004"` cannot both enter. If the accepted request settles at `"0.00025"`, only that actual amount remains charged and `"0.00015"` is released. An unknown result keeps `"0.0004"` as a visibly unresolved hold, not as an invented actual charge.

## Admission, settlement and durability

The existing call buffer persists before dispatch and removes delivered events after primary-database acknowledgment. It cannot itself be the quota ledger: an acknowledged event may still consume seven-day or monthly quota. Extend the single local journal with separate retained account/reservation indexes; do not derive admission counters from report tables or create an uncoordinated second transaction file.

The runtime store must commit the applicable aggregate and child checks, RPM unit, token/money reservations, concurrency lease and pending call fallback in one synchronous local transaction before upstream dispatch. A rejected transaction changes none of them. Only after this fsync does dispatch become possible. Completion atomically replaces pending state, updates quota amounts, releases concurrency, and makes the final call fact deliverable. Primary SQL delivery remains asynchronous and deduplicated; acknowledging the fact does not remove live quota usage.

If a crash occurs after admission commit but before dispatch, recovery cannot prove that no work ran. It conservatively retains the reservation and records `process_interrupted`; it does not refund merely because SQL has no call row. A caught local failure before dispatch may release future economic reservations through an idempotent cancellation operation, but an already committed RPM admission remains counted.

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
