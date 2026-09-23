# RouteX Implementation and Acceptance Index

Updated: 2026-09-23. This document records engineering contracts, work packages, and acceptance checks. Interfaces, tables, pages, and metrics marked as planned are not necessarily implemented; delivery evidence appears at the end. The active goal covers all F01–F30 capabilities and A01–A20 acceptance cases; completed stages do not end implementation. The full product is delivered incrementally through P0–P6.

## Scope and Decisions

- Each deployment serves one enterprise. The first iteration runs one RouteX process, with database dependencies managed by Docker Compose and Go/Vite hot reload on the host. Production multi-node HA is outside the first iteration's commitments.
- PostgreSQL is the default development database. MySQL has the same persistence compatibility requirements. Migrations and transactions must be verified against both real databases; SQLite is not a substitute.
- Public and relationship IDs use domain-prefixed ULID strings, such as `usr_…`, `ses_…`, and `mdl_…`. Names can change; IDs remain stable. The singleton installation lock and migration sequence use integer primary keys.
- Initial roles are `admin` and `member`. Management APIs require an administrator; members can manage their own Keys and read their explicitly granted model catalog. Composite roles and object-level authorization arrive in P2. Hiding a button is not access control.
- Pages use React Query and React Router with route-based loading. Forms and layouts use local shadcn/ui primitives; complex interactions use local Base UI wrappers. Failures must offer retry or recovery. Unimplemented features must not display buttons that simulate success.
- Keys belong only to individuals or Projects. A Team is a session interaction context, not a Key resource account. Project management rights come from explicit manager relationships, never from Team membership.
- Self-registration is disabled by default. P2 adds an administrator-controlled setting and registration flow. Creating the first administrator opens the workspace; enterprise authentication configuration arrives in P5, so initialization must not redirect to an unimplemented page.
- Local passwords use bcrypt and contain 12–72 UTF-8 bytes. Passphrases are allowed; character-class combinations are not mandatory. Initial setup collects a name, work email, password, and client-side password confirmation.
- A standalone CLI, response caching, plugin platform, and independently deployed services are outside the current full control-plane scope. Additional requirements need separate work packages.

## Parallel Work and Dependencies

| Work package | Independent ownership | Dependencies / delivery gate |
|---|---|---|
| P0-01 Capability inventory | Capability table and stage-specific use cases in this document | Map F01–F30 to pages, actions, permissions, and A acceptance cases; expand each subflow before implementation |
| P0-02 Identity and persistence contracts | Versioned migrations, identity APIs, entities | Empty databases, repeat execution, and concurrent startup on both databases; establish the P1-01 vertical flow first |
| P0-03 Runtime and test foundation | Compose, test scripts, CI | Dedicated test databases; test cleanup must not affect development volumes; register metrics and external dependencies |
| P1-01a Identity service | Entity / Service / Handler / Go tests | Share the contract below with the UI; concurrent first-administrator creation, sessions, logout, unauthorized access, and CSRF |
| P1-01b Identity UI | API / Types / pages / primitives / Vitest | Can start against the agreed contract; verify jointly with the real backend before merging |
| P1-02 Connections and models | Provider / Connection / Credential / Model | P1-01 authorization; internal encryption, explicit verification and activation, stable model IDs |
| P1-03 Personal Keys | Creation / delivery / authorization / revocation | P1-01 identity and P1-02 model authorization; store only digests |
| P1-04 Native gateway | Snapshots / Chat Completions / streaming | P1-02 and P1-03; controlled-upstream failure matrix; separate acceptance for a real provider |
| P1-05 Request facts | Durable events / queries / end-to-end flow | P1-04; actor and a single attribution context, query authorization and redaction |
| P2–P6 | Continue splitting vertical work packages using the table below | Organization governance → metering and limits → protocols and interactions → enterprise integrations → release acceptance |

Each mergeable work package runs relevant tests, `go tool task check`, and documentation checks before an independent commit to `main`. When tasks share a workspace, assign explicit file ownership; the coordinating task stages and commits the combined result. Run database tests through `go tool task test-integration`. Environment-gated skips during the standard `test` task do not count as database acceptance.

## P1 Data and API Contracts

Preserve the Handler → Service → Entity layering. Management APIs live under `/api/v1`, use snake_case JSON, and return DTOs. Native inference routes live under `/v1` and must not fall through to the SPA.

| Entity | Fields and constraints | Lifecycle |
|---|---|---|
| User | Stable `id`, normalized unique `email`, `name`, password hash, `role`, enabled state, timestamps | Enabling/disabling takes effect immediately for sessions; identity and Keys remain separate |
| Session | Stable `id`, `user_id`, random token digest, expiration time | Fixed seven-day lifetime; logout revokes the session; no readable token persisted in browser storage |
| Installation | Singleton primary key, initialization marker | Created in the same transaction as the first administrator and session; only one concurrent request succeeds |
| Schema migration | Monotonic version, application timestamp | Database lock on a single connection; fixed historical migration steps rather than blindly running AutoMigrate on the latest business entities at every startup |
| Provider / Connection / Credential | Provider identity / protocol, Base URL, network egress / encrypted credentials and storage references | Activation and verification are separate; unknown states never count as verified |
| Model / ModelName / ProviderModel / Binding | Stable model ID / current name and expiring aliases / native upstream name / protocol and weight | Preserve historical names; candidate weight is 0; publication validates a total weight of 100 |
| Personal API Key | Owner, digest, masked value, state, expiration, model scope, replacement relationship | Pending delivery → confirmed activation → revocation; unconfirmed delivery cannot remain valid indefinitely |
| Request event (planned) | Request ID, attempt ID, actor, scope, key/model/provider IDs, protocol, configuration ID, status, Tokens, timestamps | One request fact per request; retries record attempts without duplicate metering; price snapshots are added later |

Implemented fields are defined by the migrations, [authentication API](AUTH.md), [catalog API](CATALOG.md), and [personal Key API](KEYS.md). Add later entities within their work packages instead of creating all future tables upfront.

| API | Authentication / input | Response and errors |
|---|---|---|
| `GET /setup` | Public; no sensitive details | `{initialized:boolean}` |
| `POST /setup` | Same-origin JSON `{email,password,name}` | 201 session response; 409 already initialized; 400 invalid input |
| `POST /auth/login` | Same-origin JSON `{email,password}` | 200 session response; 401 generic authentication failure |
| `GET /auth/session` | HttpOnly session cookie | 200 session response; 401 invalid, disabled, revoked, or expired session |
| `POST /auth/logout` | Cookie + `X-CSRF-Token`, same-origin | 204 revoked; 401 no session; 403 CSRF failure |
| `GET /admin/status` | Authenticated administrator | `{initialized:true}`; 403 for members, 401 when unauthenticated |

The session response is `{user:{id,email,name,role},csrf_token}`. Cookies use HttpOnly, SameSite=Strict, and Path=/, with Secure on HTTPS. Do not infer a secure connection from arbitrary proxy headers. Production TLS termination and trusted-proxy configuration require release-stage verification. Login and initialization check Origin; authenticated write operations also check CSRF. Errors must be stable and must not contain SQL, passwords, request bodies, or provider credentials. Scope data access to the principal; cross-object access errors must not reveal whether an object exists.

## Capability Mapping: Pages, Actions, Permissions, and Tests

This table describes the target scope, not completed implementation. Every data operation needs loading, empty, failure, pending, and success states. Concurrent edits need conflict recovery. A identifiers refer to business acceptance cases; add concrete test files and evidence when each work package lands. Platform administration, self-service, Team management, and Project management are separate authorization contexts.

| Capability / stage | Pages, tabs, and actions | Permissions / key states and acceptance |
|---|---|---|
| F01 · P1/P2 | Initialization, login, registration, registration setting, logout | Public initialization allowed only once; registration requires the setting to be enabled; A01 concurrent first-administrator creation, A02 login, A20 restart |
| F02 · P2/P5 | Profile; security page with password, two-step verification, recovery codes, session revocation | Current user only; reverify identity for sensitive actions; recovery codes are single-use; A02/A15 |
| F03 · P5 | Authentication-provider configuration drawers, callback binding, forced-SSO confirmation, emergency recovery | Administrators configure providers; users bind their own identities; success/rejection/cancellation/expiration/replay for each provider; A15 |
| F04 · P2 | Member filtering, details, activation/deactivation; role, model, quota, and Key tabs | Platform administration; keep pending approval/active/disabled/offboarded states distinct; A02/A05 |
| F05 · P1/P2 | Role list, create/edit, resource-action grants, composite roles | Platform role management; protect built-in roles and combine permissions on the server; A02 |
| F06 · P2/P3 | Team list/create/details; members, owner, models, quotas, member rules | Platform or current-Team management; preserve continuity of the sole owner; A02/A04/A05/A11 |
| F07 · P2/P3 | Administrator and member Project list/create/details; managers, models, Keys, rules, requests | Independent Project management relationships; retain an active manager; A02/A05/A13 |
| F08 · P1/P2 | Personal/Project Key lists, creation form, one-time result, edit, enable/disable, delete, rotate | Ownership limited to individuals/Projects; creator is an audit fact only; pending delivery/active/revoked/expired; A03 |
| F09 · P3 | Key rules: 5-hour/7-day/monthly Tokens, monthly amount, RPM/TPM/concurrency, IP | Can only narrow parent rules; validate IPv4/IPv6/CIDR and trusted source addresses; A03/A11/A12 |
| F10 · P2/P5 | Offboarding asset inventory, successor form, completion confirmation, emergency deactivation | Platform administration; idempotent transaction, personal Key/session revocation, no transfer of personal Keys; A05 |
| F11 · P1/P4 | Provider workspace, connection and credential drawers, discovered/manual models, verification/rotation | Platform administration; successful verification does not implicitly activate a connection; connection timeouts and credential failures are diagnosable; A07/A14 |
| F12 · P1/P4 | Model list/create/details; names, provider bindings, protocol weights, catalog assistance | Platform administration; names cannot be reused, zero-weight candidates receive no traffic; A06/A07 |
| F13 · P1/P4 | Native inference endpoints, runtime state, protocol errors, stream termination | Key or session identity with effective model authorization; preserve each of the four protocols' parameters and errors; A07/A08 |
| F14 · P4 | Network-egress list/edit/default/direct connection, connection diagnostics | Platform administration; real execution of DNS/TCP/TLS/proxy/API diagnostic stages; A07/A14 |
| F15 · P3/P5 | Current price catalog, individual editing, Excel/CSV import preview and errors, export, API, synchronization | Platform price management; shared validation/ETag/atomic commit, protect custom zero prices; A09 |
| F16 · P3 | Currency and exchange-rate page, confirmation dialog, provider-model price details | Platform price management; decimal arithmetic, unit-price snapshots for in-flight requests, reject missing prices; A10 |
| F17 · P3 | User/Team default-rule tabs; individual overrides/restore defaults, budgets, alerts/stop calls | Management of the corresponding resource; intersect inherited rules with Key rules, reservation/cancellation/settlement; A11 |
| F18 · P3 | My requests/pending approvals/escalated approvals/platform records; Project model and quota changes | No self-approval; platform records are read-only; pending → first valid decision, withdrawn requests cannot be approved; A13 |
| F19 · P2/P3/P4 | Member overview, model marketplace, source filters, details, requests, integration examples | Visibility does not imply invocation permission; distinguish personal and individual Team sources; A02/A04/A06 |
| F20 · P4 | Single-model Playground/2–4 model comparison; session/Key, parameters, images/PDF, code, cancellation | One attribution context; each invocation can fail or be canceled independently; authorized attachment reads; A08/A17 |
| F21 · P1/P3/P5 | Personal/platform-wide/Project request records, filters, incremental loading, detail drawer, CSV | Server-side principal isolation; redacted member details; prevent CSV formula injection; A02/A18 |
| F22 · P3/P5 | Usage trends, granularity, Tokens/amounts, filters for principals/Keys/models/providers | Statistics use the same permissions as source facts; late arrivals/deduplication/replay, currencies and data freshness; A10/A14/A18 |
| F23 · P5 | Administration overview, quality/alerts, notification center, notification settings, email delivery status | Real events, recipient permission isolation, retries without duplicate notifications; A18/A19 |
| F24 · P5 | Read-only operational analysis, source explanations, saved reports, parameter/CSV export | Both queries and results enforce authorization; the model has no write permissions; malicious-input negative cases; A18 |
| F25 · P2/P5 | Base layout; site name/URL/Logo/footer/language; announcement creation/editing/closure/history | Platform administration; controlled content, XSS prevention, settings survive restarts; A19 |
| F26 · P5 | Instance list/details, heartbeats/resources, system tasks, offline cleanup confirmation | Platform operations; authoritative offline detection, no accidental deletion of online instances; A19/A20 |
| F27 · P4/P5 | S3 configuration/testing, upload/read/cleanup; SMTP configuration/test delivery | Platform configuration, attachment-object authorization, credential redaction, timeout/retry handling; A17/A19 |
| F28 · P1/P5 | Internal encryption, root-key rotation, Vault integration, provider-storage switching | Separate storage identity, stable references, verification/idempotency/failure compensation; A14/A16 |
| F29 · P5 | Key delivery policies, application identity/Profile, integration descriptors, coordinator/rotation status | Delivery permissions separate from provider Vault identity; no plaintext fallback; configuration applied ≠ invocation verified; A03/A16 |
| F30 · P1–P5 | Configuration validation/publication/acknowledgment/rollback, emergency revocation, audit filters and details | Platform administration; last valid snapshot, replay cannot restore revoked access, no secrets in audit records; A14/A18 |

## Protocol and Model Capability Matrix (Planned)

| Stage | Protocol endpoints | Required coverage | Not yet committed |
|---|---|---|---|
| P1 | `POST /v1/chat/completions` | Native requests/JSON/errors, SSE, cancellation, timeouts, actual model routing | Other protocols and the complete set of vendor APIs |
| P4 | `POST /v1/responses` | Text, supported images/PDF, tool-parameter passthrough, native events | Asynchronous background tasks and server-side conversation continuation need separate decisions |
| P4 | `POST /v1/messages` | Native Anthropic parameters, version headers, errors, stream events, tool parameters | Input types absent from the model's declared capabilities |
| P4 | `POST /v1beta/models/{model}:generateContent`, `:streamGenerateContent` | Native Gemini content/errors, streaming, authorized model-name mapping | File management and other Gemini APIs |
| P1/P4 | `GET /v1/models` | Callable model list, stable name resolution, authorization filtering | Catalog visibility does not automatically grant invocation permission |

Before P4 starts, expand every candidate provider, model, protocol, and image/PDF type into explicit test cases. Reject capabilities unsupported by the upstream rather than silently translating between protocols. Model-generated tool-call parameters may pass through; this does not make RouteX responsible for arbitrary tool execution.

## Snapshot, Revocation, and Event Constraints (Resolve Before P1-04)

After validating the full configuration, the control plane atomically replaces an immutable snapshot. Decrypt provider credentials during preparation; the hot path must not access Vault. Invalid configuration must not replace the previous snapshot. Single-node Key revocation persists the state and updates the runtime deny set. All new requests must be rejected after revocation returns successfully. On startup, load revocation state before accepting traffic. Do not return success when revocation application cannot be confirmed.

Deduplicate events by request ID; attempt IDs distinguish only upstream attempts. Each request has exactly one attribution context: individual, Team, or Project. Replay must not charge usage twice. Asynchronous analytics failures do not block already-authorized traffic. Define separate failure policies for an exhausted durable buffer and an unavailable primary database. Current identity APIs depend on the primary database and must fail safely when it is unavailable; they do not provide offline identity operations.

P1-04 must first select durable event-buffer capacity and behavior when full. P3 adds monetary/quota reservations and atomic settlement; quota enforcement must not rely on querying analytics aggregates. Test old snapshots, revocation sets, and the boundaries around in-flight requests.

## Metrics and External Dependencies

The following are single-node experimental targets, not measured performance or production commitments. Fix the benchmark environment at 4 vCPU, 8 GiB, a colocated database, and a controlled low-latency upstream. Record the actual OS, CPU, database version, and container limits. Results from the current development machine do not directly represent this benchmark.

| Metric | Experimental target | Verification stage |
|---|---|---|
| Additional gateway latency | 1 KiB requests, 2 KiB non-streaming responses, concurrency 50, 100 RPS for 10 minutes; p95 ≤ 20 ms, p99 ≤ 50 ms, proxy-originated error rate < 0.1% | P1 baseline, P6 acceptance |
| SSE resources | 200 concurrent connections, 1 KiB/s per connection for 10 minutes; additional RSS ≤ 256 MiB, no accumulating leftover goroutines | P4/P6 |
| Single-node configuration and revocation | Effective before publication returns successfully; all new requests rejected after revocation returns; operation p99 ≤ 1 second | P1/P3 |
| Events | Normal-state persistence p95 ≤ 5 seconds; duplicate events do not duplicate metering; buffer limit and power-loss boundary decided in P1-04 | P1/P3 |
| Restart/recovery | Ready to serve within 30 seconds with 100,000 user/Key configurations; backup recovery targets of RTO 30 minutes and RPO 24 hours require operational verification | P6 |

| External dependency | Current status / responsibility and deadline |
|---|---|
| First real-provider credentials and spending authorization | Not provided; deployment owner supplies dedicated test resources before P1 real-provider smoke testing; do not search for credentials from other accounts |
| IdP / LDAP / OAuth providers | Not provided; deployment owner supplies resources before P5 integration testing; engineering owns controlled-service contract tests |
| Vault, S3, SMTP | No external accounts provided; test failure paths with locally controlled environments first, then accept real integrations in their respective stages |
| Price-source URL/Schema/maintainer | Undecided; settle the Schema before P3 import work and supply the URL before P5 network synchronization |
| AI analysis model/retention/languages | Decide before P5; analysis cannot launch without an authorized query scope |
| Initial users, production deployment platform, multi-node requirements | Not yet provided; complete single-node development and testing first; do not translate relative weeks into release dates |

## Acceptance and Evidence

2026-09-23, baseline `493cf39`: three parallel subtasks deliver changes, with the coordinating task consolidating acceptance and staged commits.

| Work package | Status | Evidence and limitations |
|---|---|---|
| Compose development and test foundation | Accepted | `6b57c94`; PostgreSQL 18.6 / MySQL 8.4.11; check, test, and actionlint passed in an isolated workspace; development volumes preserved |
| P0-01/02 Identity-first contracts | In progress | F01–F30 page/action/permission index, identity schema/API, and migration design established; later domain schemas and detailed cases will be expanded in their work packages |
| P0-03 Runtime contracts | In progress | Single-node scope, experimental metrics, and external dependencies registered; gateway buffer capacity and full-buffer policy are documented in RUNTIME.md; measured capacity acceptance remains open |
| P1-01 Complete identity flow (F01 local foundation, F05 minimum permissions) | Accepted | Delivered by the identity bootstrap commit: identity backend/frontend, versioned migrations, dual-database and process-restart tests; resolve the exact commit with `git log -- docs/AUTH.md` |
| P1-02 Connections and model grants | Delivered (`3cd5305`); controlled local acceptance passed | Encrypted credential storage, SSRF-resistant upstream client, actual controlled verification, explicit enablement, stable names, atomic weights, and grants; PostgreSQL/MySQL integration passed |
| P1-03 Personal Keys | Delivered (`3cd5305`); controlled local acceptance passed | One-time pending delivery, confirmation, digest storage, concurrent rotation, ownership, revocation and audit; PostgreSQL/MySQL integration passed |
| P1-04/05 | In progress | Native OpenAI chat ordinary/SSE, Key Playground, isolated personal/admin call queries and request facts have controlled dual-database evidence; immutable snapshots and bounded durable events have controlled failure/restart evidence; measured capacity and real-provider acceptance remain open |
| P2 | In progress | Profile/password/session APIs and UI delivered; member/role/registration interfaces and Team/Project backend delivered; Project Keys, offboarding, and resource interfaces advance in separate verified packages |
| P3 | In progress | Current text prices, FX, immutable call assessments, and atomic CSV import/export APIs implemented; quotas, non-token metrics, spreadsheet formats, and synchronization remain open; RPM/concurrency/IP admission implemented |
| P4–P6 | Pending | Full goal remains active; additional protocols, enterprise integrations, and final acceptance follow the preceding dependencies |

Verification in this iteration:

- `go tool task check`: passed; backend lint reported 0 issues, TypeScript and mod tidy passed.
- `go tool task test`: passed; Go race tests, 13 Vitest tests, 3 Vite Host tests, 2 development-process lifecycle tests, production asset build, and Go serving tests.
- `npm --prefix website run lint`: 0 errors; the existing 2 Fast Refresh warnings for badge/button remain.
- `go tool actionlint`: passed. Remote CI, GolangCI-Lint, and Actionlint passed for identity commit `6fd738b` (runs `35825348136`, `35825348215`, and `35825348228`). Later commits require their own verification.
- `go tool task test-integration`: passed on PostgreSQL 18.6 and MySQL 8.4.11 with the final migration and email-identity fixes; the handler integration suite ran uncached with the race detector (19.771 seconds).
- `go tool task test-auth-lifecycle`: passed on both databases with the final patch: empty-database installation, real process stop/restart, original session persistence, logout revocation, and new login. Disposable processes, containers, and networks were cleaned up.
- Browser: embedded production SPA with a dedicated test PostgreSQL database; initialization → home → authenticated after refresh → logout → incorrect password rejected → correct password login passed. The development database was not initialized, and no real enterprise account was used.

A01 is covered on both databases. A02 covers current identity/admin/member boundaries; A20 covers current migration, upgrade, and restart behavior. These partial results do not establish acceptance for every future object's permissions, backup recovery, or the full platform release. Real providers, capacity targets, and external enterprise integrations remain unverified. P1-02/03 now have controlled dual-database evidence. P1-04/05 are in progress; full P1 acceptance still requires ordinary/streaming calls, request facts, and a separately authorized real-provider smoke test.

Required checks: `go tool task check`, `go tool task test`, `go tool task test-integration`, `go tool task test-auth-lifecycle`, and `cd website && npm run lint`; workflow changes additionally require `go tool actionlint`. The complete identity flow also requires browser verification of empty-database initialization → refresh → logout → login, plus persistence across a process restart. Real providers, all four protocols, pricing, enterprise identity, and production deployment have separate later acceptance gates.

### Catalog and personal Key phase verification

The isolated staged source passed `go tool task check`, `go tool task test` (22 Vitest cases, Go race tests, development lifecycle and production asset tests), `go tool task test-integration` (PostgreSQL/MySQL, 34.967 seconds), and `go tool task test-auth-lifecycle` on both databases. This phase adds provider/model/Key UI and API coverage; the combined browser inference flow follows with P1-04/05. No real provider call has been accepted.

### Gateway, call records, and account work

API and runtime boundaries are documented in [GATEWAY](GATEWAY.md), [CALLS](CALLS.md), and [ACCOUNT](ACCOUNT.md). Controlled gateway tests passed on both databases, including SSE and cancellation, priority selection, no implicit retry, alias expiration, Key/grant revocation, and request/usage facts. The staged backend phase retains the 22 existing frontend cases and adds native proxy coverage. The process now uses immutable runtime publication with a five-second authorization lease and a bounded durable local call journal. See RUNTIME.md and CALLS.md for capacity, revocation, replay, and outage boundaries. Measured capacity and external provider evidence still prevent declaring full P1 acceptance. Real-provider acceptance is still pending external test resources and spending authorization.

### Delivery boundary for the gateway backend phase

This phase delivers native inference, durable relational call facts/query APIs, account-security APIs, GORM-first migration policy and implementation, and native endpoint development proxy support. New Playground/call/account screens and the correction of existing screens to the approved layouts are still being implemented and are not included in this backend commit. Functional browser tests of the in-progress screens do not establish acceptance of their design fidelity. Frontend reference alignment is a separate following commit with its own tests and browser evidence. The full goal remains active.

Gateway backend phase verification (isolated staged source, 2026-09-23): `go tool task check` passed with zero backend lint findings; `go tool task test` passed Go race, 22 Vitest, four Vite Host/proxy checks, development lifecycle, and production asset checks. `go tool task test-integration` passed PostgreSQL/MySQL with the handler suite at 43.536 seconds. `go tool task test-auth-lifecycle` passed both databases through provider verification, model publication, Key delivery, ordinary/SSE inference, call queries, process restart, and persistent revocation. On the local Node 26 host, tests used `NODE_OPTIONS=--no-experimental-webstorage` to retain jsdom storage behavior. Test Vite servers use isolated caches. These checks cover the backend phase; the separate UI and runtime phases retain their own acceptance gates.

### Approved UI implementation

Provider/model/Key pages now follow the approved table, detail-page, inline routing, and drawer structures. The workspace has separate member/management navigation, a collapsible sidebar, account menu, and mobile drawer. Profile/security, native Key Playground, and scoped call-list/detail pages are connected to their backend APIs. See [UI](UI.md) and [PLAYGROUND](PLAYGROUND.md).

The final frontend source passed 49 Vitest cases, four Node Host/native-proxy checks, TypeScript, ESLint (zero errors, two existing Fast Refresh warnings), and the production build. A disposable production browser run verified setup, provider tabs, model creation, persisted weight changes, profile/security, one-time Key confirmation, streamed output, safe call details, and mobile navigation/focus. It used a controlled upstream, not a real provider. The browser run preceded final small spacing, breadcrumb, loading-label, and initial-loading corrections; automated tests and build cover those corrections. Unsupported later-stage controls and fabricated analytics are not shown.

### Runtime, durable events, and governance backend

The runtime prepares credentials before requests, publishes validated immutable routing state, and refreshes authorization independently. Public mutations enforce reductions before acknowledging success. A bounded bbolt journal reserves durable call facts before upstream dispatch and replays completed/interrupted facts after outage or restart without replacing accepted usage. HTTP shutdown drains handlers before closing runtime, journal, and database resources. GORM migrations 6–8 persist registration/member/role governance, runtime publication metadata, and independent Team/Project ownership and grants. Governance UI and Project Key delivery are separate following packages.

The isolated v8 source passed full check (zero lint findings), Go race tests, 49 frontend tests, four Node tests, development lifecycle and production assets, PostgreSQL/MySQL integration (95.540 seconds), and black-box provider/inference/restart/revocation flows on both databases. Recorder integration explicitly covers database outage, interrupted admission, commit-before-acknowledgment replay, exact accepted usage, and private secret-free spool contents. Tests used the documented Node 26 storage flag. Logical journal capacity is 4,096 entries of at most 64 KiB; disk loss and measured production latency remain distinct acceptance limits.

### Project Key and history backend

Project Keys have immutable Project ownership and model-scope ceilings, confirmed one-time delivery, explicit expiry, enable/disable/revoke, and verified replacement retirement. Their effective access intersects current Project grants and active models, and requires an active Project with an enabled current manager. The creator is audit attribution only; creator departure does not revoke Project assets. Native inference and the durable journal preserve Project attribution without inserting those facts into personal history. Scoped Project call APIs and bounded manager/member/model pickers support the following UI phase.

The exact v9 backend source passed full check, Go race/unit/asset tests, 49 existing frontend tests, four Node checks, PostgreSQL/MySQL integration (99.222 seconds), and real-process inference/restart/revocation flows on both databases. Tests cover Project lifecycle, scope intersection, delivery and rotation, creator departure, concurrent manager removal/revocation, Project-only history, cross-Project denial, safe DTOs, and literal bounded candidate search. Project interfaces and the coordinated personal-Key rotation correction follow separately; external provider acceptance remains open.

### Governance interfaces

Member list/detail/creation and lifecycle actions, custom role permission assignment, and registration configuration/public signup now use the implemented governance APIs. Navigation and page gates query effective permissions; delegated readers cannot gain write controls or administrative routes merely through a role label. The sidebar and all forms/tables/drawers follow the approved layouts with local Base UI primitives.

The frontend passed 62 Vitest and four Node tests, TypeScript, production build, and lint (zero errors, two existing Fast Refresh warnings). A disposable browser verified role creation/assignment, permission changes, registration persistence, restricted delegated navigation, denied direct navigation, and ordinary-member signup. That run found an anonymous-session cleanup race hiding the enabled registration entry; the public query now lives in the retained auth namespace and a real AuthGate regression covers it. Team/Project interfaces, full Key lifecycle UI, and offboarding views continue in the next packages.

### Verified Key retirement and transactional offboarding

Personal Key confirmation activates the replacement while preserving the old Key.
A separate completion action requires a persisted successful call with the exact
replacement and attribution, then retires the old Key idempotently. Emergency
revocation remains independent. The UI exposes these distinct operations and
recoverable conflicts. Project completion retains the same verified/idempotent
boundary.

Migration 10 and the offboarding APIs implement reviewed inventory, explicit
successor assignment, stale-plan conflict detection, and password-verified emergency
handover. One transaction removes sessions, personal Keys, role assignments, the
base administrator role, and departing Team/Project relationships while preserving
Project assets and historical records. Explicit reactivation does not restore
removed authority. Planned timestamps do not imply an automatic scheduler. See
[OFFBOARDING](OFFBOARDING.md).

The exact v10 source passed full check, Go race/unit/production tests, 65 Vitest
cases, four Node checks, frontend lint (zero errors, two existing warnings),
PostgreSQL/MySQL integration (152.168 seconds), and process-restart inference and
revocation checks on both databases. Recorder tests additionally prove a full or
closed journal prevents upstream dispatch. Prior full-suite runs returned a 503
from runtime refresh during personal Key creation under a materially slower run;
a focused test, diagnostic full suite, and final unmodified full suite passed.
Diagnostics found no retained pool usage or lock waits. The initial cause remains
unproven; production deadlines were not weakened and no retries were added.

A disposable browser verified replacement creation and preservation of the old
Key. Browser control disconnected before final confirmation/retirement, so complete
browser retirement remains unverified despite UI and dual-database regression
coverage. Resource and offboarding interfaces, automatic execution, external Key
delivery, and full platform acceptance remain separate work.

### Bilingual interfaces and enforced frontend formatting

The implemented web surfaces use i18next/react-i18next with English as the initial
language and Chinese as the second supported locale. Authentication and workspace
selectors persist only the language preference. Labels, notices, validation,
accessible names, and date formatting update without discarding form state. Paired
catalog checks cover keys, interpolation, and plurals; user content and protocol
identifiers are preserved.

Prettier 3.9.9 formats frontend source, tests, styles, and configuration. The npm
`format`, `format:check`, and `lint:fix` commands provide automatic fixes and
read-only checks. Both `go tool task check` and CI enforce formatting and ESLint.
The initial check rejected 77 unformatted files; the formatted source passes.
Contributor and agent instructions require the same gate for subsequent work.

The exact staged source passed full check (including formatting, lint, types, and
module consistency), Go race/unit tests, 76 Vitest cases, four Node proxy/host
checks, development lifecycle, production build/assets, and actionlint. ESLint
retains only the two existing badge/button Fast Refresh warnings. A navigation
test now waits for permission-derived navigation itself, avoiding a race with the
independently rendered page title.

Chrome verified default English, Chinese switching and reload persistence, live
validation translation, retained form drafts, and workspace/header/account labels
using the production assets and a disposable controlled auth HTTP fixture. No
browser errors or warnings were captured. This browser check verifies rendering
and client behavior; it does not replace database-backed identity acceptance.
Unregistered resource interfaces remain in the following work package.

### Current text prices and exchange rates

Migration 11 adds one current price aggregate per provider model, finite text-token
rates, explicit currency conversion, and an optimistic catalogue ETag. The
`routex_text_v1` adapter calculates decimal quotes without floating-point rounding,
retains the exact price/FX basis, and rejects missing or unsupported usage. Scoped
read/write permissions, atomic updates, and bounded before/after audits are covered
by both supported databases. Quotes remain dry runs; gateway charges and quotas
are subsequent work. See [PRICING](PRICING.md).

The final source passed full check, Go race/unit tests, 76 Vitest cases, four Node
checks, development lifecycle, and production build/assets. PostgreSQL/MySQL
integration passed in 147.486 seconds, and process restart/inference/revocation
checks passed for both databases. A GORM field update now uses the mapped `ETag`
field rather than a guessed database column; a READ COMMITTED regression verifies
price/FX reads cannot mix catalogue generations on MySQL.

### Project model access requests

Migration 12 stores explicit model additions and their historical grant baseline.
Current Project managers submit requests; another authorized actor approves or
rejects them, and applicants can withdraw. Approval revalidates current identities
and models, adds to the latest grants without restoring removed baseline grants,
and publishes runtime authorization before reporting success. Existing Key model
ceilings never expand. A bounded manager-only candidate endpoint exposes requestable
model identities without global catalogue authority. See [PROJECT_REQUESTS](PROJECT_REQUESTS.md).

The exact final source passed full check, full tests (76 Vitest cases, four Node
checks, Go race/unit, development lifecycle, production build/assets), and
PostgreSQL/MySQL integration in 164.392 seconds. The migration-12 process suite
also passed restart/inference/revocation on both databases. Added HTTP tests caught
and fixed silently ignored request fields; strict decoding now rejects unknown
fields and trailing JSON values. HTTP filters, CSRF, candidates, workflow authority,
terminal decisions, idempotency, and concurrent approval are exercised. Quota and
rate-limit requests remain separate work.

### Team, Project, Key, and offboarding interfaces

Scoped and administrative resource lists now lead to addressable detail tabs,
real membership/model/manager updates, metadata and lifecycle actions, Project Key
delivery/rotation/revocation, Project call history, and reviewed offboarding.
Selectors retain bounded-search selections and exclude already-assigned people.
An active Project without model grants explains why Key creation is unavailable.
Offboarding dates remain informational; execution requires an explicit action.
Emergency passwords bypass mutation caches and are cleared before dispatch.

The isolated final source passed full check and full test with 105 Vitest cases,
four Node checks, Go race/unit, development lifecycle, production build/assets,
and only the two existing primitive Fast Refresh warnings. Controlled browser
checks covered compact lists and detail tabs, membership/model/metadata edits,
Project Key forms and empty states, Project call filters, and bilingual offboarding
review. No warnings or errors were captured. No real Key issuance or offboarding
completion was submitted in that browser fixture; those behaviors have focused
client tests and previously delivered dual-database backend coverage.

### Immutable gateway text assessments

Migration 13 adds nullable cache quantities, exact amount/currency, pricing status,
and a bounded immutable receipt to call facts. Runtime publication captures price
and FX alongside routes, and journal replay preserves the finalized receipt. Native
usage frames are never combined into fabricated totals. Complete final usage can
be assessed despite a later disconnect; unknown or unsupported usage remains null,
distinct from an explicit free price. See [METERING](METERING.md).

The exact source passed full check, full test (105 Vitest cases, four Node checks,
Go race/unit, development lifecycle, production assets), PostgreSQL/MySQL
integration in 266.300 seconds, and process restart/inference/revocation on both
databases. Controlled HTTP tests verify price publication, old/new exact amounts,
journal reopen before database delivery, concurrent replay idempotency, unsupported
tier classification, and member/admin receipt visibility. This establishes bounded
text assessment, not provider invoicing, quota enforcement, or all pricing metrics.

### Provider model prices and Project request interfaces

Provider model rows open stable detail pages with identity, authorized routing,
and a current-price component table. Editors preserve decimal strings and explicit
zero/disabled values, and require reloading and reviewing a retained draft after
an ETag conflict. Project model requests remain within Resource configuration,
with explicit additions, pending history, nonself decisions, and own withdrawal.
Pending requests never change effective grants.

The exact final source passed full check/test with 121 Vitest cases, four Node
checks, Go race/unit, development lifecycle, production build/assets, and only the
two existing primitive lint warnings. A controlled browser fixture verified exact
18-digit decimal drafts through conflict/reload/save, read-only price controls,
Project request submission with unchanged grants, Chinese history/detail, and no
applicant approval action. These client checks complement the committed database
contracts; they do not claim a deployed end-to-end environment.

### Atomic CSV price imports and explicit currency configuration

Price CSV preview validates all bounded rows without writes. Commit revalidates
captured source, catalogue ETag and preview digest atomically, preserves omitted
rates and explicit zero/disabled values, and records the import source. Export
fails rather than returning a silently truncated catalogue, and protects formula
cells. See [PRICE_IMPORTS](PRICE_IMPORTS.md) for the schema and limits.

The bilingual Currency page reviews a coherent FX generation and all enabled
pricing currencies, independently of catalogue pagination. Decimal strings remain
exact; currency switches clear old conversions and retain the self-rate of one.
Missing required conversions, stale generations and double submission are guarded.
Historical assessment amounts remain unchanged. See [CURRENCY](CURRENCY.md).

The source passed full check/test (128 Vitest cases, four Node checks, Go race/unit,
development lifecycle and production assets), PostgreSQL/MySQL integration in
167.654 seconds, and both process restart/inference/revocation suites. The first
integration run caught duplicate names in the bulk export fixture; the fixture
was corrected before the final passing run. Currency HTTP tests cover delegated
reads, denied access, and enabled-versus-disabled conversion requirements.
Controlled browser checks verified exact 18-digit FX, missing conversions,
confirmation payloads, read-only controls and Chinese copy. CSV upload interfaces
and spreadsheet formats follow as separate deliveries.

### CSV price management interface

The Prices page follows the download/edit/upload workflow and opens a paginated
old/new preview. It reports located row errors, retains the exact reviewed file,
and submits its ETag/digest only once. A conflict requires another preview;
uncertain publication responses direct the operator to reconcile current prices.
Read-only users can preview/export but cannot commit. The API dialog describes
the supported authenticated endpoints without storing credentials in the browser.

Full check/test passed with 136 Vitest cases, four Node checks, Go race/unit,
development lifecycle and production assets. Controlled browser checks verified
navigation, upload-card layout, bounds, the API dialog and Chinese copy. Browser
file upload was blocked by the extension's file-access permission; browser import
and download completion are not claimed. Automated file/preview/commit tests and
the separately verified dual-database import contract cover those workflows.

### Durable RPM, concurrency and source-IP enforcement

Migration 14 adds scoped Personal/Project aggregate and Key policies. Actual
gateway admission checks parent and child restrictions atomically with durable
call recording. Rotation retains the accounting identity; database acknowledgment
and restart cannot reset the rolling minute. Completion releases concurrency once.
Trusted proxy configuration defaults to no trusted proxies; forged forwarded
addresses cannot bypass the direct socket source policy. Policy writes require
current authority, ETag and reason, and publish before acknowledgment.

The database durably records its journal identity before the first filesystem
binding. Interrupted initialization can retry the same identity; established
installations reject missing or foreign journals instead of clearing usage.
Operate one gateway process and restore the matching database/journal pair.
See [RESOURCE_LIMITS](RESOURCE_LIMITS.md) for the implemented subset and later
Token/money/TPM, defaults, Team-context and approval work.

Full check/test passed (136 Vitest, four Node, Go race/unit, development lifecycle,
production assets), as did PostgreSQL/MySQL integration in 259.553 seconds and
both process restart/inference/revocation suites. Tests cover real HTTP policy
permissions/CSRF/ETags, actual upstream rejection, both rotation families, held
ordinary calls, SSE cancellation, inherited restrictions, forged forwarding
headers, interrupted journal initialization and retained RPM after SQL delivery.
This is single-process enforcement; distributed limits and full quota acceptance
remain open. The corresponding configuration interfaces are being implemented.

### Scoped resource-limit interfaces

Personal/member Settings and Project Resource configuration now expose stored and
effective RPM/concurrency/IP rules. Personal and Project Key detail drawers provide
an explicit restrictions editor; no new top-level layout is introduced. Inherited
null values remain distinct from zero, parent IP predicates are shown separately,
and effective publication state is visible. Writes require a reason and reviewed
ETag, guard double submission and preserve exact intent through uncertain retries.

Full check/test passed with 148 Vitest cases, four Node checks, Go race/unit,
development lifecycle and production assets. The first full run found a test
checking a lazy-route fallback before initialization; an event-driven router-ready
wait fixed it without relaxing assertions or timeouts, and the complete suite then
passed. Browser verification remains outstanding because the available browser
surfaces were disconnected; source tests and backend acceptance are separate
from browser or deployment proof.

### Provider model availability

Migration 15 preserves existing supply as enabled and adds optimistic state edits.
Provider details expose a reviewed availability switch before prices, with English
and Chinese copy. Disabling preserves bindings, weights and prices while excluding
supply from prepared and direct route selection. Remaining enabled positive
weights retain their relative proportions. No eligible supply fails before dispatch.
A denial and current authorization prevent older snapshots restoring disabled
supply; explicit reconciliation supports uncertain publication retries.
See [PROVIDER_MODELS](PROVIDER_MODELS.md).

Full checks and tests passed with the preceding limit interfaces included:
153 Vitest cases, four Node checks, Go race/unit, development lifecycle and
production assets. PostgreSQL/MySQL integration passed in 409.126 seconds and
both process restart/inference/revocation suites passed. The database tests include
retained-row migration/repeat, actual upstream exclusion/restoration, preserved
weights and audit; focused tests cover old-snapshot denial, zero-weight candidates,
ETag conflicts, one write per submission and publication reconciliation. Browser
availability-switch verification remains outstanding.

### Spreadsheet price imports

The Prices upload/review workflow now accepts CSV, XLSX and BIFF8 XLS through the
same atomic pricing service. Workbook amounts must be text, preserving up to 18
integer and 18 fractional digits. Preview records the original document and ETag;
commit repeats server validation and rejects stale reviews. Bounded archive/XML/OLE
parsers reject formulas, macros, encryption, external references and unsupported
workbook structures with sheet/cell locations. See [PRICE_IMPORTS](PRICE_IMPORTS.md).

Full check/test passed with 158 Vitest cases, four Node checks, Go race/unit,
development lifecycle and production assets. PostgreSQL/MySQL integration passed
in 224.017 seconds, and both process restart/inference/revocation suites passed.
Tests cover literal workbook formats, continued BIFF8 shared strings, exact decimal
storage, source audit, stale commits, located formula errors and unchanged state
after rejected writes. Actual browser upload/download completion remains unverified
because browser file-access/control was unavailable; automated UI tests separately
verify reviewed original-byte submission and error presentation.

### Canonical usage reports

Personal, Project and platform usage endpoints aggregate deduplicated immutable
call facts under current authorization in one repeatable-read snapshot. Reports
include trends, model distributions, Key rankings, explicit comparison ranges,
reported token coverage and exact historical amounts grouped by currency. Missing
usage remains unknown; current FX never rewrites old charges. Calendar/timezone
handling includes DST, and bounded queries reject overflow without partial totals.
See [USAGE](USAGE.md). Interfaces and broader Team/provider attribution remain open.

Full check/test passed: 158 Vitest cases, four Node checks, Go race/unit,
development lifecycle and production assets. PostgreSQL/MySQL integration passed
in 219.103 seconds, covering replay deduplication, Personal/Project/platform
isolation, archived Project manager access, membership revocation, guessed filters,
route redaction and combined comparison row limits. This slice adds read-only
queries without a schema or gateway admission change; the preceding process
restart/inference/revocation acceptance remains applicable.

### Authenticator and recovery verification

Frozen GORM migration 16 stores encrypted TOTP factors, expiring login/enrollment
challenges and single-use recovery digests. Password-only login cannot issue a
session for an enabled factor. Codes have persistent replay protection and failed
proofs share a durable cooldown across challenges. Password changes, account
disablement and completed offboarding invalidate challenges transactionally.
Enable/disable/regenerate rotate browser sessions and current CSRF state.

The existing login card and security dialogs now implement the complete challenge,
local QR/manual enrollment, one-time recovery and disable flows in English/Chinese.
Secrets remain transient component state, outside caches and browser storage.
See [MFA](MFA.md) for contracts, retention and recovery boundaries.

Full check/test passed with 168 Vitest cases, four Node checks, Go race/unit,
development lifecycle and production assets. PostgreSQL/MySQL integration passed
in 323.558 seconds; both process restart/inference/revocation suites passed.
Tests cover wrong/expired/replayed proofs, strict payloads, concurrent recovery
consumption, encryption-key mismatch, durable cooldown, lifecycle invalidation,
current-session rotation, login HTTP202, transient secret cleanup and local QR.
Browser control remained unavailable: browser inventory was visible, but creating
an isolated fixture tab failed. No rendered authenticator-app or external identity
acceptance is claimed; deterministic verifier and UI evidence are separate.

### Native Responses protocol

Foreground native Responses now has its own connection protocol, route group,
ordinary/SSE handling, finality and usage adapter. The gateway preserves native
parameters and events, rewrites model identity, rejects foreign resource access,
and never translates or falls back to Chat. Native terminal usage uses the
captured price basis; unknown/unsupported usage remains unpriced. Key-scoped model
metadata includes currently eligible protocols without exposing provider identities.
See [RESPONSES](RESPONSES.md) for precise supported and deferred endpoints.

Connection forms and catalog badges now reflect actual protocols, and member API
examples select the corresponding native endpoint/body. The existing Chat
Playground filters to eligible Chat routes pending its native Responses interface.
Unknown native API paths return JSON404 rather than SPA assets in both builds.

Full check/test passed: 172 Vitest cases, four Node checks, Go race/unit,
development lifecycle and production assets. PostgreSQL/MySQL integration passed
in 283.324 seconds, and both process restart/inference/revocation suites passed.
Controlled upstream tests cover ordinary/SSE/error, cancellation before/after
terminal usage, supply exclusion with independent Chat availability, exact receipts,
protocol-filtered usage and durable replay. No paid provider or rendered browser
acceptance is claimed. Stateful response retrieval and background APIs remain open.

### Usage report interfaces

Personal and platform navigation and the authorized Project Usage tab now render
real reports using the existing compact filters, four summary cards, trend,
distributions and Key ranking layout. Currency-separated receipts, unknown token
coverage, exact values and read freshness remain explicit. Scope/filter changes
clear the prior visible report; unauthorized routes issue no report request.

Full check/test passed with 187 Vitest cases, four Node checks, Go race/unit,
development lifecycle and production assets. Focused tests cover exact values
above JavaScript integer limits, mixed currencies, missing trend gaps, filters,
complete-query overflow, comparison, language switching, routed permissions,
archived current-manager access and Project switching. This frontend phase uses
the already verified usage APIs; no new schema or gateway behavior is introduced.
Browser control remained unavailable, so rendered chart/layout acceptance is open.

### Native Responses Playground

The Playground now selects eligible Chat or Responses protocols per model and
executes native ordinary or streamed calls without translation. Inline conversation
history, terminal status, usage, cancellation and transient Key handling follow
each native contract. Stateful response references remain unavailable.

Full check/test passed with 204 Vitest cases, four Node checks, Go race/unit,
development lifecycle and production asset tests. Focused cases cover native
request bodies, SSE finality, incomplete/error terminals, cancellation, duplicate
submission and protocol-specific model eligibility. No database change or external
provider/browser acceptance is claimed by this frontend phase.

### Parallel model comparison

The Playground now has conversation and comparison tabs with two to four native
model lanes. A shared prompt dispatches concurrently; each lane owns its history,
terminal usage, error and cancellation. Model/protocol changes reset only that
lane. Tab disposal aborts requests and clears transient credentials.

Full check/test passed with 211 Vitest cases, four Node checks, Go race/unit,
development lifecycle and production assets. Focused cases prove independent
failures/stops, history isolation, request identity, duplicate-send prevention,
maximum/minimum lanes and tab cleanup. No new database or gateway contract is
introduced; rendered browser acceptance remains open.

### Native Messages protocol

Messages has an independent native route, version/beta header validation, bounded
model discovery, ordinary/SSE handling, ownership guards and protocol-specific
usage/price assessment. Native parameters and events are preserved; credential
selection and caller authorization remain separate from Chat and Responses.
Connection forms, model examples and usage filters expose the actual protocol.
See [MESSAGES](MESSAGES.md) for supported content and explicit pricing exclusions.

Full check/test passed with 214 Vitest cases, four Node checks, Go race/unit,
development lifecycle and production assets. PostgreSQL/MySQL integration passed
in 235.163 seconds and both process restart/inference/revocation suites passed.
Controlled tests cover native errors/events, cancellation around final usage,
cache-token normalization, exact immutable charges, supply revocation and replay.
External paid-provider calls and rendered browser acceptance remain unverified.

### Site branding and announcements

System information now persists the site name, safe logo/service URLs, plain-text
footer and default language. Explicit browser language choices retain priority.
Authorized administrators can publish, edit and close announcements with revision
checks; members receive the bounded active feed and closed history is retained.
Schema migration 17 preserves existing deployments and adds separate permissions.
See [SITE](SITE.md) for the public, administrative and concurrency contracts.

Full check/test passed with 237 Vitest cases, four Node checks, Go race/unit,
development lifecycle and production assets. PostgreSQL/MySQL integration passed
in 234.601 seconds, and both process restart/inference/revocation suites passed.
After the final accessible-label adjustment, full check and 31 focused frontend
regressions passed and production assets rebuilt. An isolated production browser
session verified setup, saved branding after restart, literal footer/announcement
text, publish/close history, language switching and explicit preference precedence.
Browser findings fixed long-name sidebar overflow and public-site observer loss
on authentication transitions; malformed public responses now fail recoverably.

### Native Messages Playground

The conversation and comparison workbenches now issue native Messages requests
with inline system/history fields and transient Key credentials. Separate parsers
preserve native block lifecycles, terminal stop reasons, cache-token accounting and
unknown usage. Refused, incomplete and tool-handoff turns remain visible without
being silently replayed as completed text context.

Full check/test passed with 266 Vitest cases, four Node checks, Go race/unit,
development lifecycle and production assets. Focused coverage includes native
headers/bodies, ordinary/SSE finality, cancellation, malformed terminal usage,
independent comparison outcomes and bilingual interaction. This phase introduces
no schema or gateway changes; rendered workbench and external-provider acceptance
remain separate open gates.

### Administrative audit explorer

The audit log now exposes committed actions with permission-checked, bounded
pagination, time/category/literal-text filters and a read-only detail drawer.
Known pricing and limit changes use typed allowlisted projections. Unknown
historical IP, source, request identity and change metadata remain explicitly
unrecorded; arbitrary audit payloads are never exposed.

Full check/test passed with 274 Vitest cases, four Node checks, Go race/unit,
development lifecycle and production assets. After tightening cursor validation,
full check and focused audit regressions passed; PostgreSQL/MySQL integration
passed in 469.470 seconds and both process lifecycle suites passed. Controlled
browser acceptance verified real site/announcement events, filtered search and
the detail drawer. Failed-attempt recording and broader historical change metadata
remain open; this read interface does not fabricate them.
