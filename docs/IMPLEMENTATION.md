# RouteX Implementation and Acceptance Index

Updated: 2026-10-04. This document records engineering contracts, work packages, and acceptance checks. Interfaces, tables, pages, and metrics marked as planned are not necessarily implemented; delivery evidence appears at the end. The active goal covers all F01–F30 capabilities and A01–A20 acceptance cases; completed stages do not end implementation. The full product is delivered incrementally through P0–P6.

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

### Capability delivery status

Status terms in this section are deliberately strict:

- **Completed** means the complete capability definition has current implementation and controlled acceptance evidence.
- **Partially completed** means material implementation exists, but one or more required behaviors or acceptance gates remain open. It does not mean that work has not started.
- **Not started** means no material implementation of the capability exists. A prerequisite or design note may still be present.

The binary capability count is 10 completed, 17 partially completed, and 3 not started. This is a completion count, not an effort percentage: several partially completed capabilities contain substantial delivered work. The user resumed implementation on 2026-10-02 and prioritized partially completed capabilities. Status changes require current implementation and acceptance evidence; a package delivery alone does not complete an entire capability.

| ID | Capability | Status | Delivered and remaining scope |
|---|---|---|---|
| F01 | Initialization, local identity, registration, and logout | Completed | Concurrent one-time setup, administrator creation, local login, registration control, durable sessions, and logout are implemented and tested. |
| F02 | Profile, password, MFA, recovery codes, and session revocation | Completed | Profile and password changes, authenticator enrollment, single-use recovery codes, and individual-session revocation are implemented. |
| F03 | Enterprise SSO, LDAP, OAuth, and emergency recovery | Not started | Provider configuration, callbacks, identity binding, enforced SSO, and enterprise recovery remain unimplemented. |
| F04 | Member administration, direct grants, roles, and resource policies | Partially completed | Member and role workflows plus scoped resource-limit interfaces exist; token, TPM, and exact-money controls with authoritative quota snapshots are available; the complete cross-domain resource-policy surface remains open. |
| F05 | Built-in and custom roles with composed permissions | Partially completed | Current-domain role and permission management is implemented; later enterprise and operations domains still require permission integration and negative acceptance. |
| F06 | Team membership, ownership, models, quotas, and member rules | Completed | Team membership/ownership, model relationships, finite aggregate/member policies and monthly requests are accepted. Durable Team-assigned roles, exact Team-only action unions, reviewed administrator assignment and the existing Roles interface have controlled dual-database and browser evidence. |
| F07 | Project lifecycle, managers, models, Keys, and requests | Partially completed | Project management, managers, model grants, Project Keys, resource limits, model requests, finite monthly quota and RPM/TPM/concurrency approvals exist; canonical current authority, stable manager identities and revocation-safe detail interfaces are hardened; initial multi-manager creation and authoritative scoped Overview have full controlled acceptance; initial direct resources and atomic combined creation requests have controlled dual-database/browser/native/restart acceptance; remaining Project list Key/policy summaries, name-or-ID search and legacy tab redirects remain open. |
| F08 | Personal and Project Key lifecycle | Completed | One-time delivery, confirmation, editing, rotation, revocation, expiration, scope, and history are implemented with controlled dual-database evidence. |
| F09 | Key Token, money, RPM, TPM, concurrency, and IP restrictions | Completed | Personal, Project, and Key admission policies are enforced in the native gateway for the documented single-node architecture. Multi-node enforcement remains a separate release-architecture gate. |
| F10 | Offboarding, inventory, handover, and emergency disable | Partially completed | Transactional local-account offboarding and continuity workflows exist; external-identity and complete enterprise continuity behavior remain open. |
| F11 | Provider, Connection, Credential, discovery, and rotation | Partially completed | The management workspace, encrypted credentials, controlled verification, and activation boundaries exist; explicit provider-model capacity attestations, credential-pool filtering/verification timestamps, reviewed name/priority editing, reviewed deletion, staged replacement preparation, immutable per-attempt Credential/publication attribution, and parser-owned native completion evidence are available; evidence-gated predecessor retirement with historical receipt/current-application separation is available; real-provider acceptance and complete pool operations remain open. |
| F12 | Model catalog, names, bindings, weights, and catalog assistance | Partially completed | Stable models, renames, bindings, weights, grants, and availability controls exist; complete public-catalog assistance and final routing acceptance remain open. |
| F13 | Four native protocols, streaming, health, retries, and failover | Partially completed | Chat Completions, Responses, Messages, and Gemini now use bounded replay-safe same-protocol failover with process-local health, one admission/settlement, durable ordered diagnostics, and no retry after a usable response. Real-provider and measured multi-node health acceptance remain open. |
| F14 | Managed egress and staged network diagnostics | Partially completed | Direct, default, SOCKS5, verified CONNECT, endpoint-bound saved authentication, complete-tunnel proxy-address fallback, and diagnostics exist. External proxy and production performance acceptance remain open. |
| F15 | Prices, spreadsheet/CSV workflows, API, and repository sync | Partially completed | Current prices, ETags, CSV/XLS/XLSX import, preview, commit, and export exist; external price-repository synchronization is not implemented. |
| F16 | Platform currency, exchange rates, and historical price snapshots | Completed | Decimal-string currency/rate management, exact quoting, and immutable per-call assessment are implemented. |
| F17 | User and Team defaults, overrides, budgets, alerts, and stop policy | Partially completed | Personal, Project, Key and Team aggregate/member enforcement plus budget/token/TPM controls, authoritative quota snapshots, and installation-calendar configuration exist; current-policy Personal/Project monthly settled-exhaustion inboxes have focused acceptance, and creation-default settings and explicit restores have complete local acceptance; templates, broader alerts, and configurable stop-calling policy remain open. |
| F18 | Quota, model, and request-limit approvals | Completed | Project model/monthly quota/RPM/TPM/concurrency and Team monthly Token/money requests have controlled acceptance: owner-first assignment, escalation, independent dimensions, read-only platform records, atomic final policies and immutable receipts with current application. Distributed acknowledgements belong to F30; release-wide acceptance remains separate. |
| F19 | Member overview, model sources, requests, and examples | Partially completed | Actor-scoped Personal/Team source attribution, native metadata, filters, details and examples are available; Project requests and four text-only native Team Session protocols exist; Personal single-Model requests and independent scoped review are available; Shared Team model requests and independently scoped review are available; Two-to-four native Team text comparison lanes are implemented; Team media/code export and broader overview/price/usage facts remain open. |
| F20 | Playground, comparison, attachments, and code examples | Partially completed | Four native conversation paths, two-to-four-lane comparison, cancellation, executable examples, per-protocol image/PDF discovery, user/Project attachment resolution, single/comparison attachment lifecycle interfaces, and conservative token/TPM/money admission with exact per-occurrence media prices exist; Team text comparison is implemented, while Team attachments/code export and parameter reset remain separate active packages; external acceptance remains open. |
| F21 | Personal, Project, and platform call records and CSV | Completed | Isolated list/detail queries, incremental loading, redacted drawers, bounded server-side CSV export, filter parity, formula protection, and bilingual download actions are implemented with dual-database evidence. |
| F22 | Usage trends, amounts, and multidimensional filters | Partially completed | Personal, Team, Project and platform usage interfaces plus immutable Provider attribution exist. Current-member Team aggregation, independent historical platform filters, revocation-safe interfaces and controlled replay/restart are accepted; complete freshness/capacity acceptance remains open. |
| F23 | Operations overview, quality, alerts, and notifications | Partially completed | The real-data operations overview, immutable Provider-attempt quality, revisioned success/P95 thresholds, grouped Provider-quality and route-unavailable alerts, recipient-isolated history, independent severity settings, and bounded durable operational SMTP delivery are implemented. Current-policy Personal/Project monthly settled-exhaustion inboxes have focused acceptance; external mail acceptance, bounce/inbox tracking, real-Provider quality acceptance, and broader quota/enterprise sources remain open. |
| F24 | Read-only AI operations analysis and saved reports | Not started | Authorized analysis queries, saved definitions, evaluation, exports, and hostile-input acceptance are not implemented. |
| F25 | Site presentation, language, and announcements | Completed | Durable site name, URL, logo, footer, default language, bilingual UI behavior, and announcement lifecycle are implemented. |
| F26 | Instances, heartbeats, resources, jobs, and offline cleanup | Completed | Distinct process generations, server-owned leases, nullable resource facts, bounded real system jobs, executor-loss reconciliation, revision-checked cleanup, audit evidence, and the bilingual administrative workspace are implemented. |
| F27 | S3, owned attachments, SMTP, and notifications | Partially completed | S3-compatible configuration and administration UI, explicit user/Project attachment APIs, cleanup recovery, Key-scoped inference reads, single/comparison attachment interfaces, SMTP administration/test delivery, and durable operational email intents exist. External storage/mail acceptance, bounce handling, and inbox tracking remain open. |
| F28 | Internal encryption, root-key rotation, and Vault switching | Partially completed | Internal provider, egress, SMTP, and storage secrets are encrypted. Root-key rotation, Vault identities, compensation, and storage switching remain open. |
| F29 | API Key Vault delivery and application identities | Not started | Application identities, Profiles, descriptors, coordinator state, and no-plaintext-fallback delivery are not implemented. |
| F30 | Configuration publication, acknowledgement, rollback, revocation, and audit | Partially completed | Immutable runtime publication, durable events, current revocation, and audit foundations exist; node acknowledgement, complete rollback, and distributed emergency-revocation acceptance remain open. |

### Acceptance-case status

A01 and A13 are fully accepted across their defined controlled scope. Other cases remain incomplete even when a delivered work package provides useful controlled evidence; final acceptance must cover the complete objects, integrations, and failure boundaries named by that case.

| ID | Acceptance case | Status | Current boundary |
|---|---|---|---|
| A01 | Concurrent first initialization | Completed | PostgreSQL and MySQL evidence confirms exactly one first administrator. |
| A02 | Unauthorized API, object, and export access | Partially completed | Current identity and delivered resource boundaries are covered; future enterprise and operations objects remain. |
| A03 | Key creation, delivery closure, rotation, revocation, and expiry | Partially completed | Controlled Personal and Project Key evidence exists; full release-flow acceptance remains open. |
| A04 | One model across Personal and multiple Team contexts | Partially completed | Explicit complete Team invocation context and unique debit behavior remain open. |
| A05 | Sole-manager offboarding and emergency disable | Partially completed | Local-account continuity is covered; external-identity continuity remains open. |
| A06 | Model rename, alias expiry, and historical-name reuse | Partially completed | Controlled model-name tests exist; final end-to-end release acceptance remains open. |
| A07 | Weights, new candidates, no healthy target, and credential failure | Partially completed | Active weights, credential priority, health cooldowns, current-policy rechecks, safe failover, exhaustion, and diagnostics have controlled coverage. Real-provider failure behavior and multi-node health remain open. |
| A08 | Stream failure, cancellation, and timeout | Partially completed | Controlled four-protocol evidence exists; real-provider and production-load evidence do not. |
| A09 | Invalid price files, stale ETags, and repository synchronization | Partially completed | File and ETag behavior exists; network synchronization does not. |
| A10 | In-flight price changes and historical reporting | Partially completed | Immutable assessments exist; final reporting and release acceptance remain open. |
| A11 | Concurrent quota, TPM, RPM, and concurrency contention | Partially completed | Single-process controlled enforcement exists; distributed and capacity bounds remain open. |
| A12 | IPv4, IPv6, CIDR, and forged forwarding headers | Partially completed | Controlled source-address enforcement exists; production proxy-topology acceptance remains open. |
| A13 | Self, repeated, concurrent approval and Team overflow | Completed | Real PostgreSQL/MySQL controlled tests reject self/duplicate/stale decisions, serialize owner approve/approve and approve/reject competitors, retain one winner and audit, atomically raise Team/member caps, roll back exact audit failure, and preserve receipts/current use through restart. This is controlled single-process acceptance. |
| A14 | Control Plane, Vault, analytics failure, invalid snapshots, and replay | Partially completed | Runtime and durable replay foundations exist; Vault and the complete failure matrix remain open. |
| A15 | SSO, OAuth, LDAP, MFA, and recovery | Partially completed | MFA is implemented; enterprise identity is not. |
| A16 | Vault compensation, rotation, and cleanup failure | Not started | Vault integration is not implemented. |
| A17 | Images/PDF, object authorization, and model comparison | Partially completed | Object backend, comparison, conservative route capability discovery, owner-bound user/Project byte reads, current-manager Project lifecycle, native inline rewriting, both Playground attachment interfaces, and attested token/TPM/money reservation with exact media occurrence settlement exist; external acceptance remains open. |
| A18 | Call queries, CSV, reports, and hostile analysis inputs | Partially completed | Call queries, usage views, and safe scoped call-record CSV exports have controlled evidence; AI analysis, saved reports, and their hostile-input acceptance remain open. |
| A19 | S3, SMTP, site, announcements, instances, and jobs | Partially completed | Controlled local coverage now includes storage, SMTP configuration/test and durable notification delivery, site/announcements, authoritative instances, and actual system jobs. External services, bounce/inbox behavior, production clock/capacity assumptions, and release acceptance remain open. |
| A20 | Fresh install, upgrade, backup/restore, and production SPA | Partially completed | Installation, migrations, restart, and SPA evidence exists; backup/restore and production release acceptance remain open. |

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
| P3 | In progress | Current token and image/PDF occurrence prices, FX, immutable call assessments, atomic CSV/XLS/XLSX imports, CSV exports, and single-process Personal/Project/Key and Team aggregate/member token and monetary quota admission are implemented; templates, broader alerts, distributed enforcement, additional billing dimensions, and synchronization remain open |
| P4–P6 | In progress | Four native inference protocols, conservative image/PDF capability discovery, user/Project attachment resolution, single/comparison attachment interfaces, attested token/TPM/money reservation with exact media occurrence settlement, SMTP configuration/test and durable operational delivery, object-storage administration/owned-attachment backend, and authoritative process/system-job operations are implemented; provider-specific media pricing, broader alert policy, remaining enterprise integrations, and final acceptance remain open |

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

### Safe call-record CSV export

Personal, Project, and platform call workspaces export the complete applied filter through dedicated authenticated Blob requests; the browser never reconstructs a file from loaded cursor pages. The export action remains the final right-aligned control in the approved filter row and uses paired English/Chinese progress and result copy. Personal and Project files contain only the member-safe fact fields. Platform files add user and Project attribution while retaining route attempts, provider/connection identifiers, pricing snapshots, request/response content, and credentials behind the detail-only boundary.

Authorization and row selection share one read-only repeatable-read transaction. Personal scope excludes Project attribution, Project scope rechecks current-manager or delegated `calls.read_all` authority, and platform scope rechecks that permission even after route middleware. Export preserves null versus explicit zero, exact decimal strings, and UTC timestamps; every string cell receives spreadsheet formula protection before standard CSV quoting. Five-second, 10,000-row, and 8 MiB bounds fail as a complete `422` response rather than a partial download. Focused unit, UI, and PostgreSQL/MySQL lifecycle tests cover formula/control-prefix attacks, exact columns, all three scopes, delegated authority, filter parity, fixed headers and filenames, empty results, transient Blob cleanup, duplicate dispatch prevention, and live language switching.

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

### Provider model input capabilities

Migration 22 adds explicit default-false image and PDF input declarations to each
provider model using a frozen GORM schema. The existing provider-model detail
configuration now reviews availability and both input capabilities together,
submits them atomically with one ETag, publishes the resulting runtime digest, and
reconciles conflicts or uncertain publication before another write. Management,
member, and Key-scoped model responses expose the stored or effective capability
metadata without inferring support from provider, protocol, or model names.

Effective `input_capabilities` are keyed by native protocol and intersect every
currently ready, enabled, positive-weight route. A zero-weight, unavailable, or
disabled route cannot expand the declaration, while every route that can actually
receive weighted traffic must support a capability before RouteX advertises it.
This package established the safe discovery boundary. Later packages now provide
user- and Project-owned attachment resolution, native inline byte mapping, both
Playground workbenches, conservative token/TPM bounds, and durable expiry of
abandoned ready objects. Multimodal monetary pricing and real-provider acceptance
remain separate F20/A17 work.

Verification passed with `go tool task check`, `go tool task test` (Go race/unit,
396 Vitest cases, four Node checks, development lifecycle, and production assets),
and the isolated PostgreSQL/MySQL integration matrix (264.100 seconds). A focused
dual-database gateway and migration rerun passed in 71.835 seconds after the
upgrade fixture confirmed the historical GORM `e_tag` column and the new default-
false capability columns. An independent code review found no blocking issue.
External multimodal provider calls remain unverified. Browser attachment behavior
is covered by the later single-model interface package.

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

### Immutable Provider usage attribution

Call facts now snapshot the final attempted route's Provider ID and name,
Connection name, and upstream model name. Admission failures and cancellation
before the first attempt retain empty topology; a retry or failover records the
last route that actually entered execution. Frozen GORM migration 26 adds the
four fields and a Provider/time/request index without a catalog backfill, so
legacy rows remain explicit unknowns and historical reports do not depend on
mutable Provider, Connection, or provider-model names.

The platform usage endpoint accepts an exact `provider_id` filter and returns
Provider, provider-model, and Connection distributions with immutable historical
labels. Personal and Project endpoints reject that filter and omit all upstream
topology. The existing bilingual layout adds the Provider filter and distribution
only to the platform workspace, retains stable raw IDs when a legacy label is
missing, and renders empty IDs as localized unknown groups.

Focused service, handler, runtime, recorder, migration, and frontend tests cover
final-attempt failover, pre-attempt cancellation, legacy journal payloads,
rename-stable labels, invalid and unauthorized filters, topology redaction,
English/Chinese switching, empty-database creation, existing-data upgrade,
partial-DDL recovery, repeat execution, concurrent startup, and schema/index
restoration on PostgreSQL and MySQL. Team attribution, background rollups,
measured capacity, and a global ingestion watermark remain open.

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

### Native Gemini protocol

Gemini Generate Content now has independent native discovery, authentication,
ordinary and SSE routes, error handling, model-name guards, usage normalization
and immutable pricing assessment. The catalog, Provider workspace and usage
filters expose the actual protocol without translating requests through another
native API. See [GEMINI](GEMINI.md) for the supported native surface and explicit
exclusions.

Full check/test passed with 282 Vitest cases, four Node checks, Go race/unit,
development lifecycle and production assets. PostgreSQL/MySQL integration passed
in 463.184 seconds and both process restart/inference/revocation suites passed.
Controlled tests cover discovery bounds, query credential removal before logging,
ordinary/SSE finality, native errors, cancellation, usage/price settlement and
revocation. An isolated production browser completed a real local Gemini stream
through RouteX and the four-protocol comparison. Paid external-provider calls,
stateful APIs and production performance remain open.

### Native Gemini Playground

Conversation and comparison workbenches now send native Gemini contents,
system instructions and generation configuration to ordinary or SSE routes.
Protocol-specific parsing requires a valid finish reason or prompt block plus
clean stream completion. Only completed text enters later history; tool calls,
thoughts, safety blocks, malformed counters and interrupted output remain explicit.

Full check/test passed with 316 Vitest cases, four Node checks, Go race/unit,
development lifecycle and production assets. Focused cases cover native paths,
headers/bodies, split UTF-8, finality, candidate and safety outcomes, exact/unknown
usage, cancellation, comparison isolation and bilingual alias guidance. An
isolated production browser completed a Gemini conversation and a four-protocol
comparison against controlled local upstreams. This frontend phase adds no schema;
external-provider acceptance remains open.

### Managed network egress

Connections now select the platform default, explicit direct access, or a named
SOCKS5/verified HTTPS CONNECT proxy. Frozen migration 18 adds managed proxy
configuration, encrypted endpoint-bound authentication, permissions, revisioned
defaults and explicit connection selection. Runtime snapshots pin validated proxy
and target addresses, prevent fallback to direct access, and reject admissions
selected before an acknowledged transport change.

The administration interface follows the established RouteX layout with list,
edit, default-selection, connection-selection and measured diagnostic flows in
English and Chinese. Saved authentication is bound to its existing proxy kind,
host and port; updates and draft tests must explicitly remove or replace it before
changing that endpoint. Tunnel establishment tries every validated proxy address,
including failures during HTTPS TLS/CONNECT and SOCKS5 negotiation, while keeping
application bytes behind the successful tunnel. Tests cover those boundaries plus
redaction, credential replacement/removal, optimistic concurrency, cancellation,
IPv4/IPv6 target failover, diagnostics, ordinary/SSE routing, disabled-proxy
rejection and restart behavior. The original delivery passed full check/test with
335 Vitest cases, four Node checks, Go race/unit, development lifecycle and
production assets. Serialized PostgreSQL and MySQL integration plus both process
lifecycle suites passed. A controlled production browser saved and selected a
local SOCKS5 proxy, then completed a real native Gemini stream through that route;
the proxy observed the diagnostic and gateway tunnels. The endpoint-binding and
complete-tunnel proxy-address regressions were added during the resumed roadmap
work. That remediation passed focused race tests, the full check/test gate with
378 Vitest cases, four Node checks, development lifecycle and production assets,
and the PostgreSQL/MySQL integration suite with the handler matrix at 397.124
seconds. External proxy services and production performance remain open.

### Durable quota-ledger foundation

The local call journal now has an explicit version-2 quota format that can reserve
aggregate and child RPM, concurrency, rolling token windows, calendar-month token
and monetary limits in one synchronous bbolt transaction. Admission receipts bind
immutable policies, proven capacity bounds and pricing evidence; settlement handles
known usage, independent unknown token or money dimensions, overrun debt,
idempotent retries and crash recovery without treating asynchronous SQL reports as
enforcement authority.

Focused race and property tests cover 7,500 exact-rational price combinations,
aggregate rollback, historical coverage, time-zone and DST boundaries, clock
rollback, incompatible currencies, independent queue capacity, process kills and
corrupt history. Full check/test passed with 335 Vitest cases, four Node checks,
Go race/unit, development lifecycle and production assets. This phase is an
internal foundation only: existing gateway admission remains unchanged until the
version-19 policy, protocol-bound reservation and management integration is
landed and accepted.

### Native quota admission and settlement

Frozen migration 19 extends Personal, Project and Key resource policies with
rolling five-hour and seven-day token limits, monthly token and money limits,
TPM, and an explicit policy currency. Administrators can set the installation
calendar and attest immutable provider-model capacity bounds through permissioned,
CSRF-protected and revisioned APIs. The first native admission freezes the
calendar and activates the version-2 journal without treating older asynchronous
reports as complete history.

All four native inference protocols now reserve aggregate and Key allowances in
the durable journal before dispatch. Admission uses protocol-specific output caps,
attested input capacity, immutable pricing and currency evidence, and conservative
holds. Completion settles independently proven token and money dimensions; unknown
or incomplete evidence retains a visible hold or blocks finite-policy admission.
The existing resource-limit interface preserves these fields during complete
policy replacement; dedicated quota controls remain a later interface phase.

Focused tests cover protocol classification, arithmetic, scope attribution,
policy reduction, retained unknowns, restart recovery and audit projection. Full
check/test passed with 361 Vitest cases, Go race/unit coverage, development
lifecycle checks and production assets. The serialized PostgreSQL/MySQL lifecycle
suite passed in 257.255 seconds, and both database process suites passed
initialization, restart persistence, ordinary and streaming inference, reporting,
logout and revocation. This phase remains single-process; Team quotas, templates,
approvals, alerts, distributed enforcement and production capacity evidence remain
open.

### SMTP configuration and controlled test delivery

Frozen migration 20 adds one revisioned SMTP configuration, encrypted optional
authentication, a separately editable system sender, bounded test-attempt receipts,
and independent `smtp.read`, `smtp.write`, and `smtp.test` permissions. Private
SMTP destinations require their own bootstrap opt-in and do not broaden provider
or proxy address policy. Configuration writes verify changed enabled endpoints
before commit and never expose saved credentials.

The administration interface follows the existing RouteX settings layout using
local shadcn-style primitives and Base UI wrappers. English and Chinese resources
cover saved transport and sender summaries, configuration drawers, explicit
credential keep/replace/remove intent, revision review, and fixed test-mail status.
The test endpoint accepts only one recipient and server-defined content; it records
admission before network work, prevents duplicate submissions by request identity,
and distinguishes relay acceptance from inbox delivery.

Controlled SMTP tests cover plaintext private relay, verified STARTTLS and implicit
TLS, authentication, every submission stage, cancellation, ambiguous acceptance,
cooldown and restart recovery without contacting external mail services. Full
check/test passed with 378 Vitest cases, Go race/unit coverage, development
lifecycle checks and production assets. The serialized PostgreSQL/MySQL lifecycle
suite passed in 264.903 seconds, and both database process suites passed. External
relay acceptance, bounce handling, inbox tracking and production capacity remain
open.

### Operations overview and durable notifications

Frozen migration 29 adds grouped operational alerts, immutable source
occurrences, recipient-isolated inbox rows, personal external-email settings and
one immutable SMTP intent per occurrence and recipient. The GORM-first migration
uses private frozen structs, restrictive foreign keys and no demonstration data.
Source failures remain authoritative when notification publication fails; the
worker reconciles missing occurrences from durable failed system jobs and
credential-verification audit facts.

Only current enabled users with `system.read` receive these operational notifications. Inbox APIs
derive the recipient from the session, and the delivery worker rechecks the same
permission before network work. External email is disabled until an authorized
user explicitly saves severity choices. Delivery uses a database lease, at most
three attempts, and explicit `accepted`, `failed` and `unknown` terminal states;
an expired sending lease becomes unknown and is never replayed. Relay acceptance
does not claim recipient-inbox delivery.

The `/admin/overview` interface follows the approved four-card, trend/readiness,
top-models/alerts composition using real call facts and catalog state. Decimal
token strings and unknown coverage are preserved. The application header inbox,
alert details and revision-reviewed settings use local shadcn-style primitives,
Base UI wrappers and paired English/Chinese `notifications` resources. Read-only
operators cannot see write controls.

Controlled service, handler, migration and frontend tests cover source replay,
recipient isolation, permission changes, ETag conflicts, safe allowlisted
payloads, retry/lease uncertainty, dual-database schema behavior and live locale
switching.

Frozen GORM migration 30 extends immutable upstream attempts with Provider,
Connection, upstream-model and full-duration snapshots, while legacy attempts
remain explicitly unattributed. The Provider detail hierarchy now defaults to
Overview and adds real configured-window attempt quality, coverage and lag facts plus a
separate Settings tab. Revisioned Provider policies evaluate aligned closed
windows for minimum success rate and optional P95 duration. The first degraded
window opens one typed Provider alert, continued bad windows do not spam, a
healthy window resolves the group and inbox projection, and a later breach
reopens it. Terminal `no_candidates` and `attempt_budget_exhausted` calls add a
separate typed Model alert source.

The operations overview now includes measured Provider quality and P95. Alert
details and delivery history carry bounded affected-resource snapshots; the
header inbox provides unread and complete cursor-based history. Users can enable
high- and medium-severity email independently or disable both with an empty
address. See [Provider quality](PROVIDER_QUALITY.md) and
[Operational alerts and notifications](NOTIFICATIONS.md) for the current
contracts.

External SMTP acceptance, bounce handling, inbox tracking, real-Provider
availability and latency acceptance, and broader quota/enterprise alert sources
remain open, so F23 and A19 remain partially completed.

### Object storage and owned attachment backend

Frozen migration 21 adds revisioned S3-compatible configuration, encrypted static
credentials, owner-scoped attachment metadata, durable cleanup intent, and
independent `storage.read`, `storage.write`, and `storage.test` permissions. Private
storage endpoints require a separate bootstrap opt-in. The client pins validated
addresses, forbids redirects and ambient credentials, signs path-style requests,
and uses conditional writes with exact-version reads and deletion.

Session APIs accept one bounded PNG, JPEG or PDF multipart upload, return owner-only
metadata or verified bytes, and record deletion intent before cleanup. A background
worker resumes expired leases and uncertain uploads without bucket-wide discovery.
Configuration changes verify a candidate before publication, retain historical
credentials for existing objects, and support explicit rollback to verified
revisions. Storage authority never grants attachment-content access. The
`/admin/storage` interface reproduces the approved overview-card and large-drawer
composition with independent read/write/test permissions, transient credential
actions, exact ETag review, saved-descriptor probes, measured stages, cleanup state,
and explicit verified rollback in English and Chinese.

Controlled tests cover signed operations, format and size limits, DNS policy,
cancellation, encrypted revision binding, a real 70 KiB multipart upload, CSRF,
owner isolation, revision changes, rollback and cleanup replay. Full check/test
passed with 387 Vitest cases, Go race/unit coverage, development lifecycle checks
and production assets. The serialized PostgreSQL/MySQL lifecycle suite passed in
275.269 seconds, and both database process suites passed. The administration UI has
focused permission, secret-lifetime, conflict, probe, rollback, and language-switch
coverage. The single-model attachment lifecycle is delivered separately;
comparison attachments, public delivery, external-service acceptance and
production capacity remain open.

### Key-scoped native attachment resolution

The gateway now recognizes the reserved `routex://attachments/<object-id>` URI
only in protocol-owned image/PDF scalar positions for Chat Completions, Responses,
Messages and Gemini. It authenticates one Key, derives an immutable user or
Project owner scope, selects one route, checks that route's explicit capability,
performs a non-reserving finite-quota preflight, reads each unique ready object
once with exact storage integrity checks, and rewrites every occurrence to native
inline data before the single final admission and dispatch. A Project Key cannot
borrow a manager's personal object or another Project's object. Text, tool
arguments, schemas and unknown fields remain opaque.

The resolver caps requests at four occurrences, four unique objects, 8 MiB of raw
bytes and 12 MiB of expanded JSON. Missing, foreign, deleting and non-ready
objects share one safe not-found contract; storage failures remain redacted.
Focused service tests cover all four native mappings, opaque fields, malformed
references, media and size bounds, filename safety, user/Project isolation,
Project lifecycle changes during a remote read, route capability, quota ordering,
deduplication, and one dispatch. The dual-database gateway lifecycle exercises
real signed storage reads and native inline payloads. External-provider and
external-object-storage acceptance remain open.

### Single-model Playground attachment interface

The existing conversation composer now matches the approved attachment
composition: selected chips sit above a four-row message field, the paperclip is
at lower left, send/stop remains at lower right, and submitted filenames appear
under the user message. The picker accepts only the image/PDF types explicitly
advertised for the selected model and protocol, requires a verified user or
Project attachment scope, caps
the draft at four files and 2 MiB per file, and keeps missing capability metadata
text-only.

Uploads and deletions use the current session and CSRF token through a dedicated
Axios boundary. User objects use the personal endpoint; Project objects use a
Project-scoped endpoint that requires a current manager. Native inference still
omits cookies and uses only the entered Key. The Key never enters the session
request, query string, React Query, or browser storage. An optional Project
Overview link supplies only an expected Project ID and rejects a mismatched Key.
Files, object IDs, and references stay out of React Query and browser storage.
Pure builders create the exact Chat, Responses, Messages, and Gemini media
positions. Draft removal, context changes, unmount, cancellation, failure, and
successful settlement delete known one-invocation objects. Uploads accepted during
navigation are deleted after their response arrives, and every ready attachment
has a durable one-hour cleanup deadline for terminated clients. Completed history
remains text-only. Code export is disabled while a transient attachment draft is
selected.

Focused frontend coverage verifies multipart/CSRF isolation, typed user/Project
scope agreement, optional Project-context matching, neutral empty/unroutable model
lists, all native shapes, capability gating, validation, deduplication, removal,
filenames, cleanup, language switching, and browser-storage absence. Real
S3/provider behavior and browser visual acceptance remain open.

### Comparison Playground attachment interface

The existing shared comparison composer now reproduces the approved attachment
composition without moving controls into individual columns. Its accepted media
is the intersection of every selected model/protocol capability, and every model
uses the verified Key's one user or Project attachment scope. One shared
session/CSRF upload draft is encoded into each independent native column request.
Per-column
execution, cancellation, status, usage, and completed text history remain
isolated; object references are never replayed.

Changing the Key, a selected model/protocol, or the two-to-four-column set
invalidates and deletes the shared draft. Submission clears the visible draft,
waits for every native request to settle before starting deletion, and releases
the interaction lock before cleanup I/O so an unavailable object store cannot
block later messages. Late multi-file batches stop after context invalidation,
and server-returned media types are accepted only for PNG, JPEG, or PDF.

Focused comparison coverage verifies shared-composer placement, capability and
owner-scope intersection, all four native payloads, partial upload failure,
chip removal, context invalidation, independent cancellation, all-lane settlement,
non-blocking deletion, late-batch cleanup, and browser-storage absence.

### Conservative multimodal quota admission

The F20-E checkpoint separated token boundability from monetary priceability for
validated personal- and Project-Key image/PDF attachment requests. Each native protocol still requires its
explicit output cap and exact supported request shape. When a finite token or TPM
policy applies, the gateway reserves the selected provider model's complete
administrator-attested input maximum plus that output cap. The attachment plan is
the structural classification boundary, while the subsequent owner-bound storage
resolution is the authorization boundary. Arbitrary remote or inline media,
audio/video, hosted tools, unknown billing fields, and unsupported cache/tier
shapes do not inherit this allowance. No file size, page count,
dimensions, model name, or tokenizer estimate becomes quota evidence.

At that checkpoint, the existing text price adapter remained text-only. Attachment requests subject to
any finite monetary policy failed with `quota_price_unavailable` during the
non-reserving preflight, before storage bytes are read or an upstream request is
admitted. Complete authoritative native terminal input/output totals still settle
the token dimension independently on success, error, or cancellation. Missing or
nonterminal usage retains the conservative hold, and an overrun invalidates the
attested capacity revision. Pricing remains `unsupported` with no fabricated zero
amount. The final admission, immutable call fact, and one-dispatch invariants are
unchanged, and this package adds no schema or handwritten SQL.

Focused service and ledger coverage exercises all four attachment protocols,
token-window and TPM bounds, existing holds, RPM/concurrency exhaustion, absent
journals, arbitrary inline media, and money-policy rejection with zero storage
reads or upstream calls. The PostgreSQL/MySQL handler lifecycle follows a real
Chat attachment through owner-bound storage resolution, native terminal parsing,
immutable call persistence, exact token settlement, incomplete-usage hold
retention, overrun invalidation, and a subsequent pre-dispatch bound rejection.
Persisted multimodal pricing remained `unsupported` with a null charge until the
F20-G extension described below.

### Project-owned attachment identity and lifecycle

Frozen GORM migration 23 adds an explicit `owner_kind` discriminator to storage
objects, backfills every released row to `user`, enforces the `user`/`project`
domain, and adds a composite owner-scope index without replacing the released
owner-ID index. Business code queries every attachment by object ID, owner kind,
owner ID, and purpose. It never infers ownership from an identifier prefix,
creator, uploader, manager, or Key.

Project attachment session routes live under
`/api/v1/projects/:project_id/attachments`. A current enabled manager can upload
into an active Project and can inspect, read, or delete existing Project objects.
Broad Project and storage permissions do not grant content access. Upload
publication repeats manager and active-Project authorization after the verified
remote write; manager removal or Project disablement during upload records cleanup
intent instead of publishing. Disabled and archived Projects reject new uploads
and gateway inference while current managers retain recovery reads and deletion.
Ready objects survive creator departure, Key rotation, and manager replacement
because the immutable Project owns them.

Gateway model discovery exposes only `attachment_scope` and, for Project Keys,
`attachment_project_id`. The gateway derives the same scope from the authenticated
Key, performs finite-quota preflight before storage, reads each unique matching
object once, rechecks active Project continuity after remote I/O, rewrites every
native occurrence, and performs one final admission and dispatch. Personal,
cross-Project, foreign, and stale references share a safe not-found boundary.
Call facts and audits retain no attachment filename, URI, hash, bytes, or storage
coordinate.

Controlled coverage includes PostgreSQL/MySQL empty and concurrent migration,
V22 existing-data upgrade, partial-DDL recovery, repeat execution, constraint and
index checks, current/former/successor manager behavior, broad-administrator
denial, disabled/archived recovery, manager-removal upload cleanup, all four
native protocols, cross-scope zero-read rejection, Project finite-money zero-read
rejection, Project lifecycle changes during storage reads, deduplication, and one
dispatch. External S3 and provider acceptance remain open.

### Exact image/PDF occurrence pricing and settlement

F20-G extends the current provider-model price catalogue with base-only
`IMAGE_INPUT / 1_IMAGE` and `PDF_INPUT / 1_PDF` rates. The quantity is the exact
number of strictly validated RouteX attachment references forwarded upstream;
repeated references are billed repeatedly while their object bytes remain
deduplicated during storage reads. An enabled zero rate is an explicit
administrator declaration that the provider's aggregate input-token price already
covers that media kind. Missing or disabled rates do not become free usage.

The gateway records only image and PDF occurrence counts, never attachment IDs,
names, hashes, storage coordinates, bytes, pages, pixels, or dimensions. It builds
the immutable request plan and captures the price/FX basis before storage access.
A finite monetary policy reserves the maximum attested token price plus exact
media components, and rejects missing media rates or FX with zero storage reads
and zero upstream dispatches. Final settlement combines authoritative native
terminal token/cache usage with the immutable occurrence counts. Missing or
nonfinal usage retains the conservative money hold.

`routex_text_v1` remains compatible for legacy and token-only price snapshots;
schedules containing media rates produce `routex_multimodal_v1` quotes. Frozen
GORM migration 24 adds nullable image/PDF counts to call records so historical
rows remain explicitly unknown while every new parsed request stores a known
zero or positive count. The provider-model price table and file maintenance flow
reuse the existing layout, ETag review, decimal-string inputs, currency contract,
local shadcn/Base UI components, and paired English/Chinese catalogues.

Controlled coverage exercises exact arithmetic and rounding, explicit-free media,
rate/FX/ETag conflicts, immutable snapshot replay, all four ordinary/streaming
native usage adapters, image/PDF/mixed requests, repeated references, pre-read
failure, retained incomplete holds, and PostgreSQL/MySQL migration and lifecycle
behavior. Per-page, per-pixel, per-byte, provider-specific media-token splits and
paid external-provider equivalence remain open acceptance gates.

### Active native route attempts

The immutable `pkg/routeattempt` plan is now active for published-runtime Chat
Completions, Responses, Messages, and Gemini requests. One logical request can
execute at most four same-protocol attempts. Credential rejection remains on the
same target when a lower-priority credential is eligible; proven pre-request
Connection failure and strict native rate rejection move to another Connection.
Bare HTTP status, timeouts, target TLS failure, reset, `5xx`, missing finality,
emitted output, final usage, and unknown work never authorize replay.

Preparation rechecks the current Key/owner/model grant, supply authorization,
snapshot and egress revision, process-local health, limits, quota capacity, and
price evidence before every dispatch. Candidate-wide maximum token and exact
decimal money bounds are reserved once. The durable journal checkpoints ordered
attempt evidence without another RPM/concurrency debit or quota receipt, and
zero-work evidence releases held dimensions on finalization or recovery between
attempts. Schema version 25 stores normalized stop/evidence fields; the existing
administrator call drawer renders them while member APIs remain redacted.

Focused race and controlled-upstream tests cover deterministic weights, same-target
credential fallback, Connection failover, strict and ambiguous native errors,
revocation and policy changes between attempts, cooldown recovery, crash
checkpoints, one final receipt, no replay after a valid streaming response, and
ordered final facts. PostgreSQL/MySQL migration and persistence coverage remains
part of the isolated integration suite. Paid real-provider behavior, measured
capacity, and multi-node health coordination remain open acceptance gates.

### Executable Playground request examples

Each conversation and comparison lane can now open a shared request-code dialog
with cURL, Python and JavaScript examples for the selected native protocol. The
builder captures the current model, parameters, system instruction, successful
history and draft while reading credentials only from `ROUTEX_API_KEY` in the
caller's environment. It preserves native routes and authentication for Chat,
Responses, Messages and Gemini without embedding the transient Playground Key.

Focused tests execute generated shell, Python and JavaScript programs against
controlled stubs, including Unicode, newlines, apostrophes, literal command
substitution text, redirect rejection and invalid Gemini identities. Full
check/test passed with 361 Vitest cases, four Node checks, Go race/unit,
development lifecycle and production assets; Actionlint also passed with CI
requiring Python 3 so interpreter cases cannot be silently skipped. Generated
examples make no automatic retry or external provider request.

### Authoritative system status and actual operational jobs

Frozen GORM migrations 27 and 28 add process-generation registrations and a
bounded operational-job history on PostgreSQL and MySQL. Every process start
creates a new `ins_` identity with a private lease token, server-owned heartbeat
revision, truthful `combined` role, bounded build/runtime metadata, and nullable
CPU, memory, and durable-journal-filesystem samples. Graceful shutdown marks the
generation stopped after request and background-worker drain. Expired leases are
offline; the client does not infer liveness or fabricate resource percentages.

`system.read` protects the combined instance/job workspace and both list APIs.
`system.write` independently protects cleanup and is seeded only for the built-in
administrator role. Cleanup locks the exact reviewed IDs and heartbeat revisions
in one transaction, rejects current, live, recent, retired, missing, or changed
candidates without partial work, and emits one target-addressable allowlisted
audit event per retired instance in that transaction. A valid
lease-token heartbeat can recover a registration if a surviving process resumes
after retirement.

Only runtime publication, durable call delivery, and storage cleanup create
system jobs. Codes, outcomes, counts, and details are constrained or allowlisted;
request content, local paths, credentials, and raw errors are excluded. Running
jobs whose executor is missing, stopped, retired, or lease-expired reconcile to
the safe `executor_lost` failure. The `/admin/system-status` page reproduces the
approved Instances and System jobs cards with independent query states,
permission-aware cleanup, exact revision review, bounded batches, conflict and
uncertain-result handling, English/Chinese copy, and locale-sensitive dates.
Known cleanup audit details render through the existing read-only audit workspace
without exposing arbitrary JSON. See [SYSTEM_STATUS.md](SYSTEM_STATUS.md) for the
complete contract and remaining production assumptions.

The final source passed `go tool task check`, `go tool task test` with 458 Vitest
cases in 42 files and four Node checks, `go tool task test-integration` on
PostgreSQL and MySQL, `go tool task test-auth-lifecycle` on both databases,
`go tool actionlint`, and `git diff --check`. Focused dual-database job tests and
independent backend and frontend reviews closed with no remaining findings.
Browser verification confirmed both cards, authoritative live/offline and job
data, the approved table hierarchy, and live English/Chinese switching before
restoring English.


### Reviewed Credential metadata and deletion

The existing Provider Credentials table now offers a compact row menu with
verification, separate enablement, name/priority editing, and deletion. Metadata
reads and writes retain independent authority, exact resource identities, a
strong non-secret representation ETag, strict Unicode/integer/reason validation,
portable connection-local name comparison, and a transactional typed audit event.
Ciphertext, Connection identity, verification and discovery coverage stay intact.
Matching targets reconcile current state and publication without claiming a
historical operation receipt.

Deletion uses the reviewed metadata ETag and required reason. One GORM transaction
removes the exact Credential and its discovery references and records an
allowlisted audit event. A post-commit tombstone excludes the deleted ID from new
dispatch before runtime refresh. Only confirmed current absence and runtime
application return success; missing runtime, failed publication and failed retries
remain uncertain. Existing calls/attempts, models, weights and grants are retained;
last-ready deletion can make routes unavailable. No migration or dependency was
added. Local Base UI dialogs retain conflict drafts and original uncertain intent,
with no secret fields in metadata editing and no optimistic deletion.

Exact final source passed `go tool task check`, `go tool task test` (584 Vitest
cases in 50 files, Go race/unit, four Node checks, development lifecycle and
production embedded assets), and `go tool task test-integration` on PostgreSQL and
MySQL (Handler 386.887 seconds, Service 5.144 seconds). An initial database run
failed before deletion because pgx retained a cached `SELECT *` shape across
schema-reset fixtures; the harness now reopens its production-configured pool
between lifecycles. Migration and behavioral assertions remain intact. Independent
backend review reported no remaining finding. Final embedded browser assets
confirmed table/menu layout, English/Chinese edit fields, draft cancellation and
Chinese deletion preview/reason/cancellation in an isolated database; no browser
write or real upstream call was submitted. Fixture resources were removed.

F11 remains partial. Durable new-Credential invocation
and configuration evidence, complete pool operations, and real-provider acceptance
remain separate work. Discovery, model success or a globally ready runtime cannot
establish that a replacement Credential has successfully served an inference.


### Staged Credential replacement preparation

A replacement creates a distinct encrypted Credential in the reviewed source
Connection, with inherited priority, immutable historical predecessor identity,
and pending/disabled state. It never overwrites, disables or copies discovery
coverage from the source. Frozen GORM version 31 adds nullable lineage and durable
creation receipts with no live foreign keys to deletable Credentials.

The independently authorized strict endpoint requires reviewed source If-Match,
a stable UUIDv4 request ID, name, secret and reason. A transaction writes the new
record, non-secret intent receipt and typed audit together. First creation returns
201; an exact retry returns 200 with the same three-field identity receipt. Current
write authority is rechecked before reconciliation, and the exact result's
immutable encrypted secret is compared inside the service. Changed actor/body/
secret or a deleted result cannot create another record. Source deletion does not
prevent reconciliation of the still-existing result. Distinct named successors
are allowed. Preparation does not refresh runtime or claim invocation readiness.

The existing Credential table/menu and local Base UI dialog retain reviewed
drafts, component-only secret state, explicit conflict review and immutable
uncertain UUID/body/ETag retries. A rejected retry never clears original creation
uncertainty; source readback cannot resolve it. Lineage appears in the existing
name cell. Verification and enablement remain separate operations, and no
predecessor retirement is inferred from discovery or global runtime readiness.

Focused interface tests passed 128 cases, including 35 new replacement cases.
The full check and test passed with 619 Vitest cases in 51 files, Go race/unit,
four Node checks, development lifecycle and embedded production assets.
Independent contract review found no remaining defect. An isolated production
binary created one record (201), then reconciled the exact request after an
independent process restart with the same database and root key (200, same ID).
Changed-secret reuse returned 409. Both rows and historical lineage were confirmed
in the embedded English/Chinese interface; dialog cancellation preserved the
rows, English was restored, and fixture resources were removed. No real-provider
request was made. Focused real PostgreSQL/MySQL V31 migration and replacement lifecycle tests
passed under the race detector (127.168 seconds). The first full matrix exposed
a pinned GORM PostgreSQL index-removal syntax issue in the upgrade fixture. Two
fixed allowlisted test-only index statements correct the fault injection, without
changing production/released migrations or weakening assertions. The final full
PostgreSQL/MySQL matrix passed (Handler 404.327 seconds, Service 5.012 seconds),
with owned Compose containers/network removed. No timeout or assertion was
relaxed. F11 remains partial: per-Credential invocation/configuration evidence,
native completion proof and planned predecessor retirement remain separate work.


### Immutable per-attempt Credential and publication attribution

Internal attempts now preserve exact selected Credential and published Snapshot
IDs through failed attempts, final native results and fsynced interruption
checkpoints. Same-Connection fallback keeps distinct Credentials; cross-Provider
retry cannot replace earlier attribution with the final logical route. Frozen
GORM V32 adds bounded non-null columns with empty historical defaults and an
ordered Credential/publication/completion lookup index, without live catalog or
publication foreign keys. Existing database/journal blanks stay unknown, never
backfilled from a parent call or mutable catalog. No-attempt failures fabricate
no dispatch. The compatibility path without a runtime records no publication.

RecordCall validates each bounded optional ID and copies exact per-attempt values.
Its transaction, immutable RequestID replay, quota admission/settlement and bounded
journal format remain unchanged. Public Personal/Project/platform call DTOs and
CSV exports remain unchanged; neither new identifier leaks through them. No
secret, ciphertext or request/response content is recorded.

Focused service/handler race tests passed, including same-Connection and cross-
Provider fallback, separate prior/active snapshots, legacy journal fields staying
unknown and interrupted active attempts staying error/unknown after accepted
HTTP 2xx. Full format/check/test passed with 619 Vitest cases in 51 files, Go
race/unit, four Node checks, development lifecycle and production embedded assets.
Focused real PostgreSQL/MySQL migration and attribution lifecycle passed under
race detection in 121.681 seconds. Two initial fixture assumptions were corrected:
pinned PostgreSQL GORM index introspection lacks physical ordinal order, and
member-scoped service reads intentionally hide attempt diagnostics. Parameterized
test-only catalog introspection now verifies actual index order, while internal
administrator reads assert stored attempts and separate real HTTP tests preserve
member/platform DTO privacy. No production SQL or privacy boundary changed.
The first complete matrix exposed an older recorder fixture with an overlong
Credential placeholder. The corrected bounded fixture now asserts stable IDs in
private journal recovery while retaining secret/content exclusions and immutable
replay. Independent review found a missing pre-dispatch durable checkpoint in the
direct-database compatibility path. That path now fails before any upstream
request when checkpoint persistence fails, and held-request recovery preserves
its exact Credential with an unknown publication. Focused race tests passed;
the final full format/check/test and PostgreSQL/MySQL matrix passed (Handler
410.182 seconds, Service 5.252 seconds). All owned Compose resources were removed.
The focused independent repair review found no remaining actionable defect.
The new real-database compatibility fixture confirms interrupted checkpoint
recovery and immutable SQL replay without a fabricated publication.

F11 remains partial. Credential/configuration attribution alone does not prove
native inference completion or current eligibility. Parser-owned completion proof,
scoped runtime readback and evidence-gated planned retirement remain separate work
at this attribution checkpoint. The subsequent package below delivers native
completion evidence.

### Parser-owned native completion evidence

Independent parser/runtime and persistence/schema owners implemented an
internal attempt marker with exact unknown/completed/handoff/blocked/incomplete
values and frozen GORM V33. Legacy database/journal history remains unknown
independently of status and usage. Native observations remain separate from
GatewayUsage; no forwarding, quota, public DTO or CSV behavior is changed.

Chat requires complete bounded requested-choice coverage and a recognized native
finish; Responses and Messages require their native terminal; Gemini requires
clean EOF. Tool handoff, blocking and output limits are distinct. Weak/unsupported
shapes stay unknown. A terminal observation never overrides cancellation/error
status or proves scoped current eligibility. Mandatory check found two lint
issues, corrected without changing behavior. Native review identified Responses
status-only promotion of refusal/tool handoff. Bounded terminal-output observation
and ordinary/SSE regressions now distinguish those outcomes, weak/future/empty/
nonterminal and contradictory outputs. The focused re-review closed the finding;
independent persistence review also found no remaining issue.

Final focused race tests passed (Service 4.486 seconds, Handler 2.994 seconds).
Full format/check/test passed with 619 Vitest cases in 51 files, Go race/unit,
four Node checks, development lifecycle and production assets. Final-source
focused PostgreSQL/MySQL V33 migration/persistence and corrected Responses
acceptance passed in 140.280 seconds; owned Compose resources were removed.
An earlier seven-lifecycle run passed in 146.843 seconds on the prior parser copy;
it does not accept the correction. The final complete PostgreSQL/MySQL matrix
passed under race detection (Handler 415.087 seconds, Service 5.164 seconds),
including all native fixtures, durable replay and migration constraints. Owned
Compose resources were removed. Local Markdown references and whitespace
checks passed.

F11 remains partial. Scoped readiness and a separate durable receipt for planned
predecessor retirement remain separate packages. The existing Personal/Project
Key retirement gates still count generic successful calls; a separate next fix
will require successful terminal completed attempts while preserving historical
rotation replay and current ownership/scope/expiry checks. This package does not
claim that gate is hardened or that F11 is fully accepted.


### Personal and Project Key retirement proof hardening

The existing F08 retirement gates now use a shared bounded GORM projection of
immutable logical calls and their last attempt. New retirement requires exact
Personal or Project ownership, replacement Key and request IDs, the creation
boundary, successful call/attempt statuses and exact native completed evidence.
HTTP success, empty/unknown results, handoff, blocking, truncation, cancellation,
missing or superseded attempts and case-folded/corrupt facts cannot qualify.
A corrupt latest candidate conservatively requires fresh proof. No schema, public
API or native forwarding contract changes. Current scope/expiry/authority and
historical completed-audit retry behavior are preserved; emergency revocation
remains independent.

Independent Personal and Project owners implemented and froze the service/real-
HTTP fixture changes. Read-only review found no actionable defect. Existing dialog
copy now explains the gate in English and Chinese; live language switching keeps
the chosen Project replacement and does not dispatch. Focused UI tests passed
44/44. Full format/check/test passed with 619 Vitest cases in 51 files, Go race/
unit, four Node checks, development lifecycle and embedded production assets.
Focused real PostgreSQL/MySQL lifecycle acceptance passed under race detection
in 146.174 seconds, including actual HTTP 200 exclusions, adversarial immutable
facts, independent-pool concurrent completion, one durable audit and historical
retries. Owned Compose resources were removed. The final complete PostgreSQL/MySQL race matrix passed (Handler 417.693
seconds, Service 5.211 seconds), and all owned Compose resources were removed.
Final whitespace and local Markdown-reference checks passed.

This repairs an existing proof gap without changing capability inventory counts.
F11 scoped readiness and receipt-backed Provider retirement remain open.


### Checked F11 package: read-only replacement readiness

Independent runtime and API owners implemented bounded advisory readiness through
the existing replacement row menu and Base UI dialog. The contract combines
exact lineage/metadata, verified explicit enablement, full required native-route
coverage, current locally coherent publication and current database scope, and
immutable successful terminal completed proof from the exact successor/current
configuration. Unknown history, weak HTTP acceptance, stale publication, missing
coverage and unavailable transport remain blockers. Capture/recapture avoids
blocking runtime/DB pool inversion. No migration, receipt or retirement write is
part of this phase; controlled tests and independent review passed.


The existing English/Chinese row-menu/dialog composition validates exact target
IDs and strong header/body ETags, hides prior eligibility during refresh/failure,
shows unconfirmed configuration/counts as unknown and localizes safe blockers.
The final focused UI/i18n run passed 28 cases; the Provider suite/i18n passed
128 cases before the final count-display adjustment. Full format/check/test
passed on the combined source (639 Vitest cases in 52 files, Go race/unit, four
Node checks, development lifecycle and embedded production assets). Runtime
race tests passed after readability-only formatting (1.733 seconds), with lint
zero issues. Focused real PostgreSQL/MySQL acceptance passed in 160.016 seconds,
including actual native HTTP fixtures, current-config proof, committed-but-not-
published scope, no GET mutation/upstream contact, single-connection concurrency
and bounded overflow. The final complete race matrix passed (Handler 429.433
seconds, Service 5.336 seconds); all owned Compose resources were removed.
Independent frozen review found no actionable defect. Markdown references and
whitespace checks passed. No paid supplier call, fleet acknowledgment or planned
retirement write is claimed. F11 remains partial; receipt-backed retirement is
the next separately checked package.


### Checked F11 package: receipt-backed predecessor retirement

Extend the existing readiness dialog with a required reason, explicit confirmation
and immutable request retry. The server binds a UUIDv4 intent to reviewed exact
lineage, aggregate ETag, pre-disable configuration and native completed AttemptID.
Frozen GORM V34 retains historical receipts without live foreign keys. One
predecessor disable, receipt and typed audit commit atomically; exact authorized
replay never disables a re-enabled predecessor or duplicates audit. Current local
application is reported independently from historical saved intent, without a
fresh inference requirement after the known disable/restart. No supplier/fleet
acceptance or full F11 completion is implied.

Three owners delivered transaction/migration/HTTP fixtures, runtime publication
pin/exact proof/application, and UI/audit projection/docs in parallel. Final
format/check/test passed (650 Vitest cases in 53 files, Go race/unit, four Node
checks, development lifecycle and production assets). Focused frontend passed
39 cases. Focused real PostgreSQL/MySQL acceptance passed in 159.203 seconds;
complete race acceptance passed (Handler 449.057 seconds, Service 5.332 seconds).
Independent frozen review found no remaining actionable defect. Final pre-commit
check passed. Local Markdown references and whitespace passed.

A disposable production process with PostgreSQL and a controlled native upstream
passed built-in-browser required reason, exact proof confirmation, saved receipt
and current local application. The predecessor became disabled while the successor
remained enabled; English/Chinese switching and console checks passed. The first
durable call activates quota accounting and changes the configuration, so the
fixture obtained its qualifying completion after observing that publication.
Owned processes, browser tab and Compose resources were removed. External supplier
and fleet acceptance remain open; F11 and the full objective remain partial.
Readiness baseline `a9d8dc3` remote CI 36984325423, Actionlint 36984325353 and
GolangCI-Lint 36984325458 all subsequently succeeded, including dual databases,
independent-process session restart and build artifacts. Those results are separate
from this retirement package.


### F19 current native availability correction

The member catalogue now counts actual supported eligible protocols separately
from visible models. Explicit empty/unknown protocols and disabled/archived models
show localized unavailable guidance and no fabricated Chat endpoint or cURL. Copy
is disabled and unavailable models do not suggest Key creation as a routing fix.
The drawer selects a stable model ID from refreshed data, so eligibility/status
changes remove stale examples. Four native request shapes, legacy supported
protocol fallback only when the plural field is absent, and Gemini name checks
remain. Fifteen dedicated tests and complete format/check/test passed (665 Vitest
cases in 54 files, Go race/unit, Node checks, development lifecycle and embedded
production assets). No new schema/API, Team invocation or source directory is
delivered by this correction. F19 remains partial; actor-scoped source/detail
work follows separately.


### F19 actor-scoped source directory and details

Added session-authorized model-catalog list/detail APIs without changing native
model discovery or Personal Key grant ceilings. Explicit direct grants and active
Team membership/grants produce deduplicated source records; platform operators
receive no implicit personal grants. Complete lists fail closed beyond 1,000
models, 100 grant-bearing Teams or 5,000 source rows. Details bound only the
requested model and reauthorize each read. Exact identity/name ownership checks,
read-only repeatable-read snapshots and connection release before metadata reads
preserve authority and single-connection safety on both supported databases.

The approved cards/table/drawer now show actual source/protocol/availability
statistics, literal name search, conjunctive source/protocol/image/PDF filters,
two source labels with complete overflow, recorded creation time and explicit
unknown price/global usage slots. Independently keyed actor/model details hide
stale data during refresh or error. A ready Team-only model remains visible with
real protocol/capability metadata but has no personal cURL, copy or Key suggestion.
No new schema, dependency or implicit Team invocation is introduced.

Dedicated frontend tests passed 27 cases plus eight i18n cases. Complete
format/check/test passed (677 Vitest cases in 54 files, Go race/unit, four Node
checks, development lifecycle and production assets). Focused PostgreSQL/MySQL
acceptance passed in 160.920 seconds. A disposable production browser flow passed
source overflow, Team-only denial, personal native examples, revoked-detail
freshness, table/image filters and bilingual switching with no console errors.
Full F19 remains partial: Personal/Team requests, explicit Team invocation and
broader price/usage contracts are still open.

The complete PostgreSQL/MySQL race matrix passed (Handler 488.717 seconds,
Service 5.727 seconds), including migration/upgrade and existing lifecycle cases.
Independent backend review found no remaining actionable production defect.
Local Markdown references and whitespace checks passed.


### F23 monthly settled-exhaustion inbox: focused acceptance

Three owners implemented separate durable quota observations/projections, scoped
merged inbox APIs, and the existing bilingual bell menu. Frozen GORM V35 keeps
the operational notification foreign key unchanged. Current finite Personal and
Project aggregate policies can produce an immutable monthly limit-reached
snapshot only from covered journal-settled usage and a fresh exact applied
policy revision. Unknown dimensions, incomplete coverage, holds and denomination
mismatch do not establish exhaustion. Zero and unlimited remain distinct; no
crossing, warning threshold, historical backfill, Team/Key quota source, quota
SMTP intent or additional admission rule is claimed.

Observation and recipient projection commit atomically with deduplication by
scope/dimension/month/revision/currency. Recorded scope, policy, calendar,
amounts, currency and Project name remain historical snapshots. Replay never
resets read state or adds a new manager. The bounded keyset worker advances
through finite policies rather than repeatedly scanning only the first batch.

Enabled current recipients can read an empty inbox without `system.read`.
Personal quota records require the exact owner; Project records require both a
recorded recipient and current enabled management of an active Project.
Operational records still require exact current `system.read` authority, and
delivery settings keep their existing read/write permissions. Source predicates
apply before merged pagination, unread counts and governance-serialized read
mutations. Exact recipient, role, permission and association checks reject
case-folded aliases. Removed managers, Project creators, global operators and
newly appointed managers receive no implicit historical quota access. Typed
quota snapshots preserve decimal strings and token currency null; no quota
source is projected into an operational alert or external email.

Focused Go race tests and staticcheck passed. Isolated actual PostgreSQL/MySQL
race acceptance passed in 173.349 seconds, including every migration prefix
through V35 and existing operational HTTP behavior. The new lifecycle covers
native authoritative settlement, held/unknown exclusions, exact money, zero/new
revision identity, deduplication, frozen Project metadata, current-recipient
privacy, mixed-source cursors/counts, raw collation aliases, read isolation,
restart persistence and `MaxOpenConns=1`. Owned Compose resources were removed.
Final format/check/test passed (724 Vitest cases in 56 files plus Go/Node,
development and production checks). The production browser passed real
Personal/Project settled snapshots, bilingual rendering and revoked-Project
read failure with refreshed scoped history; owned resources were removed.
The complete PostgreSQL/MySQL race matrix passed (Handler 499.925 seconds,
Service 5.851 seconds); its owned containers/network were removed. F23 and F17 remain partially completed;
the capability inventory stays 8 completed / 19 partially completed / 3 not
started, and A01 remains the only fully accepted case.


### Project monthly quota requests

The existing Resource configuration and scoped request history now support
MODEL_ACCESS and finite monthly QUOTA applications. Current enabled managers of
active Projects submit explicit fields; quota reviewers independently require
projects.limits.write, cannot approve themselves and must provide a reason.
Monthly Tokens remain safe integers, zero is a cap, omission preserves a field,
null is rejected and money stays exact decimal text. Per-kind authorization
predicates apply before pagination; quota-only reviewers receive a minimal Project
request workspace. Pending requests leave enforcement unchanged.

Strong composite submission/approval validators bind complete normalized policy,
raw policy revision, immutable request intent and platform currency generation.
Approval serializes with admission and governance and atomically saves the full
patched policy, immutable approval evidence and typed audit. It preserves other
limits, usage and Project Key ceilings. Original decision replay never reapplies
a superseded policy; fresh details distinguish saved approval from current applied,
pending or superseded state. Same-actor retries use the current Session CSRF while
retaining original business intent. Malformed HTTP 200 receipts do not claim
success. Existing MODEL creation hashes and approval semantics stay compatible.

Frozen additive GORM V36 passed both supported databases: empty creation, V35
upgrade, concurrent/repeat execution, six missing-column repairs, constraints,
indexes, historical model/approved quota preservation and immutable approval
requirements. Targeted race acceptance passed in 184.141 seconds, including old
model requests, new quota requests and shared monthly inbox authority. The complete
PostgreSQL/MySQL race matrix passed (Handler 504.548 seconds, Service 5.267 seconds);
owned Compose resources were removed. Final check and test passed with 759
frontend cases in 57 files, Go race/unit tests, Node checks, development lifecycle
and embedded production assets. The focused request/resource/i18n suite passed
71 cases, including malformed HTTP 200 receipts and valid terminal creation replay.
Actionlint passed.

An owned built binary/PostgreSQL/native fixture and in-app browser verified a
manager submission with baseline 5 and target 10, independent approval, actual
settlement to 10 and next-call refusal, later direct policy 15, exact historical
approval replay after restart preserving 15, bilingual details and a quota-only
reviewer's minimal workspace. Owned browser/runtime/database resources were
removed. Browser proof is separate from external provider and complete product
acceptance. A fresh build containing the final receipt validation repeated manager
submission, independent approval, native enforcement, superseded replay after
restart and bilingual detail acceptance; its owned resources were removed.

F18 remains partial; Team approvals/escalation and global request workspaces
remain open. A13 now has partial Project approval evidence rather than being
unstarted; its Team overflow boundary is still unaccepted. Capability totals stay
8 complete, 19 partial and 3 not started. A01 remains the only fully accepted case.


### Checked F18 package: Project request-rate approval

Finite RPM, TPM and concurrency requests reuse the Resource adjustment form and
scoped history. Monthly and rate changes produce independent records and retained
intents; partial saved, failed and uncertain results remain explicit. Current
managers submit; independent nonself projects.limits.write reviewers approve.
The bounded shared limit lifecycle preserves historical MODEL/QUOTA digest bytes,
full-policy/currency review, settled use, Key ceilings and unrelated policies.
Saved decisions never restore later policy changes.

Frozen GORM V37 adds only safely ordered checks and preserves released V1–V36.
Final check/test passed: 800 frontend cases in 58 files, Go race/unit, Node,
development lifecycle and production embedded assets. Focused UI: 112 cases.
Focused actual PostgreSQL/MySQL race acceptance: 210.144 seconds; full matrix:
Handler 529.867 seconds, Service 5.063 seconds. Actionlint passed. Independent
review identified and fixed late decision callbacks after actor changes/unmount.

Owned production/PostgreSQL/browser acceptance verified combined monthly/rate
submission, unchanged pending enforcement, separate approvals, real native rate
rejection, later policy preservation through original retries and restart, and
English/Chinese details. All owned resources were removed. Team session invocation
and immutable call attribution are the next assessed prerequisite before Team
limits and approval. F18/A13 remain partial; totals and full acceptance are unchanged.

### Checked partial package: explicit Team Session invocation

The typed Session identity shares the native text-only Chat attempt pipeline with
Key inference while retaining exact active Team/member/model authority from private
short-leased publication. Local revocation tombstones cover committed Session,
account, membership, Team and grant changes. Frozen additive GORM V38 retains
immutable Team/member/User call facts; separate Team and stable Team/User journal
accounts never debit Personal or Key use. Own-Team call list/detail requires current
active membership and exposes only the actor's own facts, including for owners and
administrators. Personal history, usage and CSV exclude Team calls.

The approved Playground source selector uses explicit named Team Sessions and
separate cookie/CSRF transport. Source/actor/context changes cancel requests and
clear transient state; refreshed authority failures hide cached histories. Team
comparison, attachments, code export and additional Session protocols remain open.
Recorded Chat input/output stays visible when upstream omits total usage; total is
unknown rather than estimated. See [Team Session inference](TEAM_INFERENCE.md).

Final check/test passed with 843 frontend cases in 61 files, Go race/unit, Node,
development lifecycle and production embedded assets. Focused actual dual-database
race acceptance passed in 261.152 seconds; complete matrix: Handler 610.579 seconds,
Service 5.598 seconds. Real-process authentication lifecycle and Actionlint passed.
Owned production/browser acceptance verified native SSE, bilingual exact/unknown
usage, own history/detail, removal denial, rejoin and persistent restart; owned
resources were removed. Finite Team aggregate/member policies and subsequent Team
approval are the next partial package. Capability totals remain 8 complete,
19 partial and 3 not started; A01 remains the only fully accepted case.


### Checked partial package: finite Team aggregate and member policies

Three owners delivered independently authorized direct Team policies, stable-account
atomic native enforcement and the approved bilingual Limits/member-adjustment
interface. Frozen GORM V39 preserves historical policy values while widening the
stable scope ID and seeding separate token/money/rate authority. Owners receive no
implicit direct-write permission. Sparse edits preserve inaccessible fields; strong
composite review binds full parent/current policy, currency and lifecycle. Current
application proof, unchanged retry intent, exact money, incomplete coverage and
stable use through rejoin/restart remain explicit. See [Team resource limits](TEAM_LIMITS.md).

Final check/test passed with 876 Vitest cases in 63 files, Go race/unit, four Node
checks, development lifecycle and production assets. Actual PostgreSQL/MySQL race
acceptance passed (Handler 623.428 seconds, Service 6.559 seconds), including both
new policy/native fixtures and V39 upgrade/constraint recovery. Real-process
Session/Key lifecycle and Actionlint passed. Controlled browser proof confirms
child/aggregate no-dispatch exhaustion, independent Personal use, bilingual saves,
retained conflict drafts, rejoin and persisted restart. Owned resources were removed.

Acceptance fixed mapped GORM previous-revision update, explicit primary-key
nullability, money-pointer aliasing and exact monetary denomination proof. The
frontend late-response regression avoids racing timer-based GC. F06 remains partial
for Team-assigned roles/scoped permission union; F17 defaults and F18 owner/platform
request stages remain open. Capability and full-acceptance totals are unchanged.


### Monthly Team member quota requests

Three owners delivered dedicated request/step/pending-slot persistence, scoped
owner-first/platform escalation service APIs, and the approved own/pending workspace
plus read-only platform records. Frozen V40 must preserve historical requests and
released V1–V39. Independent `teams.quota_requests.read_all` gates global records;
current owner identity authorizes only nonself workflow decisions, while platform
Token/money decisions use their respective dimension permission.

Immutable submission and step receipts, current authority, atomic aggregate/member
application and current publication proof remain distinct. Fresh actions and server
effect previews require matching request/step stages and contiguous history; known
receipt reconciliation retains its earlier position. The confirmation dialog renders
server-owned before/after values, and global records expose a workspace link only
for a current assigned reviewer. F18 and A13 now have complete controlled evidence
for their defined scope. Full product, distributed-node and release acceptance
remain open.


Monthly Team request source is checked with 922 Vitest cases in 65 files, Go
race/unit, Node, dev lifecycle and production assets. The full actual PostgreSQL/
MySQL race matrix passed (Handler 704.846 seconds; Service 6.084 seconds), and
both real-database process suites passed restart, persisted Session/revocation
and controlled ordinary/streaming inference. V40 migration prefixes, request
permissions, exact money, ordered steps, lifecycle ABA cancellation, offboarding,
malformed-stage denial and immutable receipts are included. The controlled native
fixture verifies unchanged owner escalation, concurrent owners, atomic rollback
after final audit failure, final 15/15 admission, superseded replay and same-journal
restart without Personal debit.

The actual bilingual browser creates target 15 from member 5/Team 10, confirms
unchanged owner escalation and explicit final 15/15 effects, and observes approved
current application. Three five-Token native calls succeed; the fourth stops before
upstream dispatch. An independent restarted production binary preserves the same
receipts, caps and used 15. Terminal platform records are read-only with no workspace
shortcut. Owned tabs/processes/Compose/config/journal are removed and developer
services are untouched. Six legacy-route cases preserve auth/destination gates.
No external provider, distributed acknowledgement, load or full release acceptance
is claimed. The next assessed partial capability is F06 Team-assigned roles, exact
Team-only action union, role workspace and current scoped candidate authorization.


The final added approve/reject competition passed on real PostgreSQL and MySQL
(native fixtures 5.92/5.94 seconds; complete focused runner 208.460 seconds). One
current owner wins, the other receives 409, and the request/step/slot, exact actor/
decision receipt and one audit follow that winner. Either rejection or escalation
leaves aggregate 25/member 20 and used 20 unchanged; rejected native calls cannot
dispatch. No production code changed after the full matrix. Required check/test
passed again for the final test fixture with 922 frontend cases. F18 moves to
Completed and A13 to Completed; capability totals are 9/18/3. A01 and A13 are fully
accepted in their stated controlled scope; other acceptance cases remain open.


## Checked F06 Team-assigned roles, 2026-10-03

Frozen GORM V41 adds restrictive Team/Role relationships without handwritten SQL.
Current active members inherit only `teams.write` and `teams.models.write` within
the exact Team. Direct platform permissions remain independent; ownership alone
adds no management authority. Team lifecycle, quotas, Projects, Keys, global
navigation and invocation grants never expand through Team assignments.

Actual protected administrators use complete role replacement with strong reviewed
If-Match and a required reason. Role definitions and candidate generations bind
the review; monotonic portable persisted Team timestamps prevent assignment ABA.
Assigned roles cannot be deleted. Typed transactional audit strips unrelated JSON.
The existing Roles tab preserves table, permission dialog, add/remove picker and
explicit Save with English/Chinese copy and target-specific controls.

Final check and test passed: 957 Vitest cases in 67 files, Go race/unit tests,
four Node checks, development lifecycle and production embedded assets. Real
PostgreSQL/MySQL migration focus passed (202.479 seconds), corrected role workflow
focus passed (238.34 seconds), and the complete final matrix passed (Handler
723.801 seconds; Service 6.167 seconds). Both actual database process auth/native
lifecycles passed. Initial failures were confined to owned fixture cleanup,
reserved Role permissions and a singular resource-kind query; production guards
were preserved. Earlier stopped runs are not accepted evidence.

The controlled production API/browser confirmed target-only management, global
and cross-Team denials, role-definition revocation, assignment removal, unchanged
native grants, process restart and bilingual read-only member views. A committed
assignment whose response was deliberately lost remained unknown after a rejected
exact retry; a fresh read and explicit current-state confirmation discarded only
local intent. Consecutive saves used matching persisted validators and fresh
candidates. Owned acceptance resources were removed. No external-provider,
distributed enforcement or full-release acceptance is inferred.

F06 is complete in its stated capability scope. A02 remains partial because other
enterprise and operations boundaries remain open. Capability totals are 10
completed, 17 partially completed and 3 not started. The objective remains active;
F22 Team aggregate usage is proceeding in an isolated worktree with independent
report/API, database-fixture and frontend owners.


## Checked F22 Team usage reports, 2026-10-03

The existing usage workspace defaults to Personal and offers named own active
Teams with paginated selection and explicit expected context. Team reports use
current exact enabled membership and active Team authority in the same read
snapshot as immutable fact selection. Historical contributors remain counted
without current membership joins. Canonical Team attribution excludes Personal,
Project and Key facts before limits. Only model/trend/currency statistics appear;
actor-only Team call history remains separate.

Platform `team_id` filters require independent exact current `calls.read_all` and
retain historical archived or missing-catalogue Team attribution. They cannot
combine Personal-user, Project or Key filters. Authorized Provider dimensions
remain available; Team-filtered platform reports do not advertise or fabricate
Keys. The member interface validates exact target echo, cancels obsolete reads,
resets filters on scope change and hides stale private data during renewed reads
or errors. English-default and live Chinese workflows preserve exact counters,
unknown coverage, decimal amounts and historical currency grouping.

Final check/test passed with 990 Vitest cases in 69 files, Go race/unit,
development lifecycle and production embedding. The real PostgreSQL/MySQL workflow
focus passed (215.926 seconds), and the final complete matrix passed (Handler
745.019 seconds; Service 6.692 seconds), including the final platform-dimension
correction. No migration or new dependency was introduced.

Controlled production native calls from two actors yielded two Team requests and
ten known Tokens; member Personal usage stayed empty and each actor's Team call
history contained one own call. Actual browser revocation removed old totals,
rejoin restored immutable attribution, and process restart preserved the report.
The platform Team filter retained authorized provider groups and omitted Keys.
English was restored; browser errors were empty in the final administrator view;
owned acceptance services, files and Compose resources were removed.

F22 remains partial for complete freshness/capacity acceptance and broader scope.
Capability totals remain 10 completed, 17 partial and 3 not started. Exact F06
CI 37127402456, Actionlint 37127402424 and GolangCI-Lint 37127402443 succeeded.
The overall objective continues with isolated F07 Project authority hardening.

## Checked F07 canonical authority package, 2026-10-03

Exact enabled actors and current direct Project permissions or manager associations
now gate target reads, manager replacement and model replacement. Exact selected
users/models and canonical retained manager IDs prevent collation aliases from
lending authority or historical relationship identity. Creator attribution remains
immutable and supplies no permanent management rights. Disabled Project governance
remains available; archived Projects remain terminal. No schema/API/dependency
change is introduced.

The existing actor/target-scoped detail hides private cached content, tabs and
actions during renewed reads and errors. Eight focused cases cover cached remounts,
401/403/404, changed actor/target and late responses. Full check/test passed with
998 Vitest cases in 70 files, Go race/unit, development lifecycle and production
embedding. Corrected real PostgreSQL/MySQL focused acceptance passed (226.497 seconds);
the final complete matrix passed (Handler 760.155 seconds; Service 6.376
seconds). Controlled production-browser and process
restart acceptance passed.

The first actual database focus found a fixture expectation mismatch: failed
publication retains a Project authentication tombstone and returns native 401,
not a model-scope 403. The fixture now follows the existing runtime contract and
still requires zero dispatch. Additional assertions preserve the original Project
Key after creator management removal and successful republication. Both supported
databases passed the corrected focus before the final complete matrix passed.

F07 remains partial. Initial multi-manager creation and complete Project overview
are open. Capability totals remain 10 completed, 17 partial and 3 not started.

Controlled F07 production acceptance used an isolated owned PostgreSQL database.
Current-manager settings and the manager table were verified in English/Chinese;
actual manager removal produced detail/candidate 404 and mutation 403, and browser
refresh hid the old private data and actions. Restoring current management and
restarting the independent binary preserved Sessions, immutable creator identity
and exact current authority. Browser Retry restored the authorized detail, English
was restored and final browser error logs were empty. The temporary tab was closed;
owned process/database/network/files were removed by the acceptance script.

The final mandatory check passed after the fixture correction. Owned focused/full
Compose resources and production acceptance resources were removed. All 82 local
Markdown references and whitespace checks passed. This F07 package is ready for
phased main delivery; its exact commit is identified through Git history. The
separate F17 worktree continues seven-field User/Team default settings, atomic
creation snapshots and explicitly reviewed resets with frozen GORM V42.

## Checked F17 User/Team defaults, 2026-10-04

An isolated schema/acceptance, backend/API and frontend team implemented two
creation templates, frozen GORM V42, atomic new User/Team policy copies and
explicit current-default restore. The runtime consumes ordinary saved policies;
template changes do not modify existing resources. Restore preserves local IP,
usage, holds, calendar and immutable resource/Key/relationship identities, binds
current target/default/currency review, and separates persistence from exact runtime
application. Unknown publication retries retain their original copied revision;
ordinary later writes clear the last-reset marker.

Focused frontend checks passed 79 cases in five files, TypeScript, scoped ESLint and
Prettier. Backend focused/full service race and pinned lint passed. Initial full
frontend acceptance found two old navigation expectations after the new settings
entry; corrected permission/default UI focus passed 29 cases in two files. Initial
real database acceptance found GORM acronym/digit column naming differences in
V42 checks; explicit model/frozen/fixture column mappings preserve the required
constraints. Corrected PostgreSQL/MySQL focus passed in 243.032 seconds, including
real Personal/Team admission. Final full check/test passed with 1036 Vitest cases
in 72 files. Controlled production/native/browser/restart acceptance passed
creation-only defaults, preserved IP/settled usage, exact reset provenance and
quota rejection. The final full PostgreSQL/MySQL matrix passed (Handler 808.330 seconds;
Service 6.035 seconds). Both real-process authentication/native lifecycles passed
restart persistence and revocation. Mandatory final check, full frontend and
production embedding checks passed on the corrected source. F17 and the full objective remain
partial; totals stay 10 completed, 17 partial and 3 not started.


## Checked F07 initial managers and Overview, 2026-10-04

The next isolated package has been integrated after the F17 main delivery.
Projects accept presence-aware complete initial manager selections, with exact
enabled candidates and ordinary creator retention. Independent current direct
permission permits a complete selection without the creator. Relationships,
Project and audit commit atomically; no implicit grants, policies, Keys or
applications are introduced.

The addressable Overview uses a scoped repeatable-read snapshot with independently
authorized operating counts, authoritative quota windows/holds/coverage, exact
historical money and actual immutable last-call facts. At most five typed
metadata activities never borrow arbitrary audit JSON or current directory names.
The interface preserves the existing creation form, monthly and operating cards,
pending notice and activity table, with fresh actor/Project privacy boundaries.

Source frontend focus passed 62 cases in four files. Actual PostgreSQL/MySQL
creation/Overview focus passed in 254.50 seconds. Final main checks, full test and
full real-database matrix are running. Controlled browser creation passed exact
two-manager selection and no implicit resources. A QA-only wait condition was
corrected to await both journal settlement and independent SQL call delivery
without resending inference. Full production/browser/revocation/restart proof
remains a delivery gate. F07 remains partial; totals stay 10 complete, 17 partial
and 3 unstarted.


Final F07 main acceptance passed mandatory check, Go race/development lifecycle
and production embedding tests, 1057 frontend cases in 73 files, complete
PostgreSQL/MySQL regression (Handler 815.082 seconds; Service 6.616 seconds)
and both process authentication/native lifecycles. Controlled production/browser
proof passed exact two-manager creation, no implicit resources, real Project use
5/10, the exact immutable last call, independent pending request count, creator
access revocation without erasing attribution, peer continuity, explicit
restoration and process restart. English/Chinese switching passed. The QA-only
wait separately observes journal settlement and durable call delivery, comparing
the same UTC instant without replaying inference. Initial resource configuration
and combined creation requests remain open; F07 is not marked complete.


### F19 Personal Model requests: verified delivery, 2026-10-04

Frozen GORM V43 stores request history, unique pending slots and nullable direct
grant provenance. [Personal Model requests](PERSONAL_MODEL_REQUESTS.md) use
independent `members.models.write`, exact scoped review, additions-only approval
and immutable terminal receipts. Approval does not expand an existing Key's
Model ceiling. Disablement and offboarding cancel pending requests atomically.
Current published provenance distinguishes original application from a superseded
approval; historical retries never recreate revoked grants.

The existing catalogue drawer and Member Models workspace retain their layout.
An independently authorized minimal workspace does not expose Member directory,
profile, role or Key data. English/Chinese current and historical status copy
remains separate. Browser acceptance found and repaired a session-refresh remount
loop and stale captured-response application copy, with focused regressions.

Final mandatory check and full test passed 1089 frontend cases in 75 files, Go
race tests, development lifecycle and production embedding. The five affected
actual database fixtures passed on both drivers (363.965 seconds). The final
complete PostgreSQL/MySQL race matrix passed (Handler 834.672 seconds; Service
6.110 seconds), and both real-process authentication/native lifecycles passed.
Controlled production browser/native/restart proof passed pending no-access,
independent reviewer access, old-Key ceilings, new-Key completion, revocation,
original receipt retry without restoration, stable pending history and fresh
superseded status. English was restored, console errors were absent and owned
processes, tabs and Compose resources were removed. No external-provider
acceptance is claimed.

Earlier fixture failures involved pinned PostgreSQL DropIndex syntax, the alias
authentication error expectation and a missing persisted receipt comparison
baseline. The first complete matrix also exposed legacy persisted User IDs
rejected by the new lifecycle cancellation helper and omitted prior Project
creation/Overview routes. Exact safe persisted identities are now accepted only
at that internal lifecycle boundary; public request identity rules remain strict.
The prior routes are restored. The failed 815.437-second matrix remains a failed
run; the corrected final gates above supply delivery evidence.

The preceding `5006126` exact CI 37138830271, Actionlint 37138830243 and
GolangCI-Lint 37138830247 succeeded. F19 remains partial for Team requests,
further Team protocols and broader overview/price/usage facts. Three owners are
implementing Team schema/acceptance, API and interface work separately. Capability
totals remain 10 complete, 17 partial and 3 unstarted; the full goal remains active.

### F19 shared Team Model requests: verified delivery, 2026-10-04

The preceding Personal package is committed and pushed as `4dffc88`; exact
CI 37143100600, Actionlint 37143100595 and GolangCI-Lint 37143100576 succeeded.

[Team Model requests](TEAM_MODEL_REQUESTS.md) add explicit exact membership,
shared pending uniqueness, independently scoped `teams.models.write`, no
self-review and immutable receipts. Pending eligibility loss cancels requests;
approved shared grants survive applicant departure. Unchanged canonical grant
replacements retain provenance; ordinary removal/re-addition does not restore
original application proof. The existing catalogue drawer/history and Team Models
composition use paired English/Chinese copy and fresh resource authority.

The first actual focus passed five accompanying fixtures on both drivers but
failed Team creation reconciliation after membership loss (398.02 seconds).
Exact enabled-actor intent reconciliation now precedes current membership checks;
fresh requests still require membership. The repaired Team migration/lifecycle
focus passed both drivers under race detection (Handler 283.375 seconds).
An initial full frontend run found two legacy catalogue assertions that confused
the new request-scope selector with the protocol selector. Accessible protocol
labels and semantic assertions correct this; the failed run is not delivery proof.

Final formatting, mandatory check and full test passed: 1121 Vitest cases in
77 files, Go race/unit, development lifecycle and production embedded assets.
The complete PostgreSQL/MySQL matrix passed (Handler 951.498 seconds; Service
5.827 seconds), including frozen V44 creation, upgrade, repeat/concurrent startup,
constraints, preserved rows and governing lifecycle changes. Both real-process
authentication/native lifecycles passed.

Controlled production browser/native/restart proof passed pending no-grant,
independent review without global directories, shared member native completion,
unchanged old Personal Key ceilings, applicant departure with retained own history
and unavailable current facts, peer grant survival, revocation and exact historical
retry without restoration. Restart preserved Sessions, receipts and revocation.
English/Chinese passed with English restored and no console errors. Only owned
tabs, processes and Compose resources were removed. No external-provider or
whole-F19 acceptance is claimed. Totals remain 10 complete, 17 partial and 3
unstarted. The full goal remains active; further Team protocols are the next
bounded package, followed by remaining overview/price/usage facts.

### F19 Team native text protocols: verified delivery, 2026-10-04

The preceding shared Team requests are pushed as `ea73a05`; exact CI 37145348320,
Actionlint 37145348264 and GolangCI-Lint 37145348262 succeeded.

Three parallel owners implemented native backend, frontend/source links and
actual-driver fixtures. Responses, Messages and Gemini extend Team Session text
invocation alongside Chat with independent native finality, current Session/
membership/grant checks and atomic aggregate/member accounting. Model discovery
and catalogue links expose each ready native protocol, including non-Chat models,
without Personal Key authority or media capability. Native credential/query
redaction precedes logging and recovery; invalid selectors fail before dispatch.

The first actual focus failed test-only media-envelope assertions and callback
cleanup lifetime (301.986 seconds); the next failed an over-specific Gemini
message expectation for earlier native resource rejection (311.702 seconds).
Corrected native-specific assertions and worker-safe cleanup preserve zero-
storage-read/dispatch checks. These failed runs are not acceptance evidence.

Final formatting, mandatory check and full test passed 1151 frontend cases in
77 files, Go race/unit, development lifecycle and production embedding. Complete
PostgreSQL/MySQL race regression passed (Handler 929.393 seconds; Service 7.045
seconds), covering four exclusive protocol discoveries, finite token/money/rate
settlement, refusals, native terminal states/unknown usage, membership and Session
revocation, durable restart and retained original Key ceilings. Both real-process
authentication/native lifecycles passed.

Controlled production proof passed eight ordinary/streaming calls and browser
completion on all four native protocols with authoritative 5 Tokens per reply.
Completed Gemini inline history, model/protocol clearing, truncated stream failure
with unknown usage, explicit recovery, native-only catalogue navigation, grant
revocation with no dispatch and persisted Session/revocation after restart passed.
Browser acceptance found a stale selection after native 404; native 401/403/404
now clear grants/transcript and require renewed discovery, with focused regressions
and rebuilt production verification. English/Chinese passed with English restored,
no console errors and owned process/tab/Compose cleanup. Team comparison/media/
code export, broader member facts, external-provider and full F19 acceptance remain
open. Totals stay 10 complete, 17 partial and 3 unstarted; the full goal is active.

### F07 initial Project resources: controlled acceptance, 2026-10-04

Three parallel owners implemented frozen GORM V45 and atomic creation resources/
requests, the existing Project form and minimal API, and actual migration/lifecycle
fixtures. Root adds publication proof, bounded typed audit and shared registration.
[Project creation and initial resources](PROJECT_CREATION.md) preserve legacy
creation, independent permissions, exact decimal money, reviewed currency/context
and original uncertain intents. Initial pending records never grant resources.
The completed gates and controlled evidence are recorded below; the remaining F07 list/navigation package is tracked separately.

The initial actual V45 focus failed before lifecycle execution because GORM
inferred review_e_tag while a check constraint used review_etag. Entity/frozen
schema and private partial-upgrade fixture mappings now specify column:review_etag;
the frozen schema test verifies actual field DB names. Failed-run Compose resources
were removed. Initial full source validation passed 1180 Vitest cases in 78 files
and final schema race tests passed; repaired real-driver results are recorded below.

Final full local check/test passed 1181 Vitest cases in 78 files, Go race/unit,
development lifecycle and embedded production assets. Subsequent failed focused
runs corrected fixture audit cardinality/resource types and Project-native
attribution expectations; these runs are not acceptance evidence. The complete PostgreSQL/MySQL matrix subsequently passed against the repaired
fixture, as recorded below.

Controlled production/browser/native acceptance passed direct initial resources,
all three pending request types, exact retry reconciliation, finite quota and
independent approval. Existing Keys retained their original scope; later policy
and grant changes, native revocation, and lost manager authority never restored
original resources through receipt retry. Two process restarts preserved Sessions
and historical receipts while reporting current superseded/unavailable status.
English/Chinese passed, English was restored, console errors were empty and owned
process/tab/Compose resources were removed. F07 remains partial for Project list Key/policy summaries, name-or-ID search
and legacy tab redirects. Broader platform acceptance is separate.

Exact remote checks for native Team source d29ffa8 passed: CI 37146984375,
Actionlint 37146984325 and GolangCI-Lint 37146984327.

The repaired complete PostgreSQL/MySQL matrix passed under race detection:
Handler 971.977 seconds and Service 7.062 seconds. Both drivers exercised V45
upgrade/partial-DDL/repeat/concurrent startup and lifecycle constraints. Owned
containers/network were removed. Both PostgreSQL/MySQL real-process
authentication and native gateway lifecycles passed restart, persisted Session/Key facts and revocation; owned resources were
removed. Mandatory check was rerun after the final fixture correction. All required
local gates passed before phase delivery, with 107 checked local Markdown links.


### Combined-router API 404 rendering, 2026-10-04

Controlled browser setup exposed an unsupported API path returning sanitized 500.
The helper was corrected to use the documented Provider list; separately, the
combined application's custom error renderer was found to recognize Fox binding
400 errors but not Fox's fallback 404. The renderer now preserves sanitized JSON
404 for unsupported API/native paths while retaining generic 500 sanitization.
No route, permission, authentication lifecycle or database contract changed.

Combined-router regression reproduced 44 failures before repair. Focused race
checks passed in development (1.942 seconds) and production (1.956 seconds), with
44 unsupported method/path cases, 11 error mappings and four production SPA
fallback cases. Staticcheck passed both modes and pinned scoped lint found zero
issues. Standalone asset checks remain separate from the combined-router proof.

Mandatory full check passed; complete handler race/unit tests passed in development
(4.374 seconds) and production (4.220 seconds). This repair changes no persistent
authentication or migration behavior.


### Administrative Model route prices, 2026-10-04

Exact actor/Model-scoped detail reads replace the global-directory lookup for
resource URLs. Existing routing-table base input/output columns show each exact
Provider-model schedule, preserving decimal strings, currency, unit, known zero,
disabled and missing values. Model, price and Provider-directory permissions stay
independent. No consolidated Model quote, new navigation or price write is added.

Focused PostgreSQL/MySQL catalogue, scoped price and call-assessment regression
passed under race detection (129.542 seconds). Full carried-source check/test
passed 1207 Vitest cases in 79 files, Go race/unit, development lifecycle and
production embedded assets. Controlled bilingual production browser/native-free
proof passed exact display, absent schedules, separate read authority, disabled
writes, restart persistence and removal of stale details/actions after revocation.
English was restored, browser errors were empty and owned QA resources removed.
The complete main PostgreSQL/MySQL matrix passed under race detection (Handler
971.759 seconds, Service 7.422 seconds). Owned Compose resources were removed;
all required local gates passed before delivery.

The integration harness now prepares fresh schemas inside selected lifecycle
subtests. Full matrix behavior and per-case driver reconnects remain unchanged;
filtered cases no longer reset/migrate every skipped lifecycle. Focused Team
comparison evidence is separate: both drivers passed (60.079 seconds); its isolated
full local test passed 1266 Vitest cases in 79 files, with production/runtime
comparison acceptance still pending.

Initial Project source `4a32986` has green Actionlint 37150472987 and GolangCI-Lint
37150472989. CI 37150472951 was canceled by the newer main push under the existing
branch concurrency policy; the succeeding main CI must confirm the combined
lineage. Local F07 gates passed and are unaffected by this remote cancellation.


### Remaining F07 list/navigation acceptance

A source/reference audit identified a bounded final package: both Project lists
need total Project-owned Key counts; the administrative list also needs configured
monthly Token/money/currency/RPM/TPM summaries. These are authorized total Key
records and stored policies, not Overview's active-Key count or remaining usage.
Project search must support literal names or IDs. Legacy managers/models/limits
tabs must redirect to settings/resources after current authorization. Team behavior
stays separate. Accepted creation/managers/lifecycle/resources/requests/Overview
and member entry require no additional broad release gate. Keep F07 partial for
these named gaps; external identity/provider/distributed delivery belong elsewhere.


### Disabled Model supply readiness, 2026-10-04

Logical Model list/detail readiness now includes the Provider-model enabled flag.
A private credential-coverage fact preserves the existing positive-weight
activation rule; temporary supply disabling changes neither weights nor grants,
names, prices, Model status, schema or runtime authorization. Focused actual
PostgreSQL/MySQL race proof passed in 101.402 seconds, including enable/disable/
re-enable list/detail agreement, unchanged price generation, covered weight
writes, uncovered rejection and disabled native zero dispatch. Current read
permission revocation remains authoritative. Full main check/test passed 1207 Vitest cases in 79 files, Go race/unit,
development lifecycle and embedded production assets. The complete
PostgreSQL/MySQL race matrix passed (Handler 1012.087 seconds, Service 7.001
seconds). Owned Compose resources were removed; final mandatory check passed.
Exact Model-price source `3e8a159` CI 37152547296, Actionlint 37152547292 and
GolangCI-Lint 37152547317 also passed.


### Team native comparison and conversation finality, 2026-10-04

The approved comparison workbench supports two to four independent Team Session
text lanes across Chat, Responses, Messages and Gemini, sharing one composer.
Every lane retains its exact native request, current Team/membership, independent
request ID, cancellation and accounting. Team media and code export remain
separate packages. A native 401 triggers one bounded active no-store Session
probe; only its current authoritative 401 establishes platform logout.

Conversation history and exported Key requests now require nonempty completed
plaintext without refusal or nontext output. HTTP success, usage, truncation,
tool handoff, blocking and unknown finality do not establish native completion.
Four native parsers retain their independent terminal semantics. Renewed Session
or Team authority aborts requests and clears old models/history, preserving the
unsent draft until explicit discovery. CSRF-only replacement preserves an
otherwise fresh confirmed context.

Carried-source full checks/test/build passed 1345 frontend cases in 81 files, Go
race/unit, development lifecycle and embedded production assets. Focused actual
PostgreSQL/MySQL comparison proof passed in 60.079 seconds. Controlled production
browser proof confirmed four-protocol completed history, independent cancellation,
truncated Responses and Messages handoff exclusion, upstream 401 with a valid
platform Session, exact Team/User/membership attribution and zero Personal calls.
The final owned rerun independently confirmed eight dispatched call identities,
four revoked zero-attempt records, revoked discovery, persistent Sessions after
restart and zero request replay. English/Chinese rendering passed; English was
restored, console errors were empty and owned resources were removed.

Earlier QA-only failures are not full acceptance: finite Team resources were
initially created before accounting activation; a zero activation policy rejected
before history evaluation; the call-count fixture conflated rejected records with
upstream dispatch; its repaired detail read then used a nonexistent id instead of
request_id. Source contracts were unchanged by these fixture repairs. Successful
intermediate observations remain separate from the final complete capture. The
first complete main PostgreSQL/MySQL matrix failed (Handler 1005.991 seconds;
Service 7.504 seconds passed) in the existing MySQL credential retirement
readiness fixture. Its immediate coherent-read assertion raced the periodic
publisher: the advisory capture intentionally returns runtime_unavailable on
mutex contention, and validation rejects changed publication pointers. The
fixture now bounds retries to those transient read blockers only; it never
replays inference or mutation and retains domain, native evidence, lease and
explicit single-pool contention assertions. The repaired dual-driver readiness/comparison focus passed (66.650 seconds).
The complete main PostgreSQL/MySQL matrix then passed under race detection
(Handler 1032.267 seconds; Service 8.273 seconds). Owned Compose resources were
removed. Final formatting and mandatory check passed; source and acceptance
documents are submitted together. The earlier failed matrix remains failed
evidence and does not count as acceptance.
