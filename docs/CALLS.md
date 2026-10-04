# Call Facts and Query API

Call facts record completed gateway requests without storing prompts, responses, credential secrets, or raw upstream diagnostics. Facts support Personal, Project or explicit Team Session attribution, exactly one subject per request. Supported text calls include immutable assessed amounts and bounded Personal/Project/platform CSV export. Team Session history has its own current-member own-actor list and detail endpoints; Own-Team CSV follows the same current-member own-actor scope. Team aggregate report interfaces remain separate work packages. Durable event ingestion is implemented for the single-process deployment.

## Recording Contract

The gateway assigns a canonical server-generated `RequestID` and records one `service.CallFact` after an authenticated request succeeds, fails, or is canceled. Each upstream attempt has a separate ID. A rejection before dialing an upstream has no attempt. Unauthenticated traffic has no trusted user attribution and is not stored as a personal call fact.

A fact contains stable user, Key, model, provider-model, and connection IDs; the public model name used for attribution; protocol; outcome; streaming flag; start and completion timestamps; duration; optional input/output and cache token counts; pricing status and nullable exact amount/currency; a safe error classification; a route stop reason; and ordered attempts. It contains no Authorization value, plaintext secret, request body, response content, or upstream error text. A logical call has no single Credential ID: different attempts can use different Credentials. Internal attempt attribution is described below. Supported protocols are `openai_chat`, `openai_responses`, `anthropic_messages`, and `gemini_generate_content`; outcomes are `success`, `error`, and `canceled`.

Unknown token usage remains `null`. It is not converted to zero or inferred from text length. The exception is an admitted call whose attempt evidence proves that no provider work began: it stores explicit zero token counters and the `no_work` pricing state while retaining diagnostic pricing dimensions. Supported complete text usage is assessed from a pre-dispatch price snapshot; unpriced amounts remain null. See [METERING](METERING.md) for completeness and pricing boundaries. An error code outside the predefined internal classifications is replaced with `upstream_error`, so an accidental provider message cannot become a stored diagnostic.

`RecordCall` uses a transaction and a unique request ID. Replaying that ID leaves the first accepted fact and its attempts unchanged. Concurrent duplicate delivery therefore cannot double usage. Attempt ID conflicts roll back the new fact instead of producing an incomplete record. Plain unique inserts establish deduplication independently of MySQL's affected-row behavior.

Storage uses microsecond timestamp precision on both databases. Schema version 5 introduces `call_records` and `call_attempts`; additive version 25 adds normalized route-stop and replay evidence through frozen GORM models. Attempts reference their canonical fact and are ordered by an explicit attempt number. Historical rows retain conservative empty/unknown defaults. IDs referencing users, Keys, and catalog resources are historical snapshots without cascading foreign keys, so later lifecycle changes cannot erase attribution. Queries never require the original resource to remain active or visible.

## Persistence Failure Boundary

The process opens a private bbolt journal before listening. It synchronously reserves one slot with a safe interruption fact and an atomic zero-work recovery settlement before the first upstream dispatch, checkpoints ordered attempt evidence before every later dispatch, then replaces that reservation with the final fact after completion. Checkpoints do not create another admission, quota receipt, RPM debit, or concurrency lease. A crash before the first dispatch or between attempts settles as zero work only when every completed attempt proves `not_sent` or `rejected_without_work`; entering an active attempt clears that zero recovery evidence, and active or ambiguous work remains unknown. Default synchronous bbolt commits remain enabled. The journal stores only the same allowlisted attribution, usage, and immutable pricing fields as the relational fact; it never stores request/response content or provider credentials.

The journal admits at most 4,096 entries, each at most 64 KiB. This bounds logical payload to 256 MiB, not physical file size: page metadata and the file high-water mark require additional disk space. A full or unwritable journal rejects new upstream dispatches with HTTP 503 and `event_buffer_unavailable`. A final journal-write failure cannot change an already sent response; the latest reserved fallback survives and is recovered as `process_interrupted`. Its economics are zero only when the durable checkpoint proves no provider work; otherwise usage remains unknown. Filesystem or device loss beyond successful durable commits is outside this guarantee.

A background worker delivers up to 64 facts per cycle, with a three-second timeout per fact and a one-second interval. It acknowledges a journal entry only after an idempotent relational transaction commits. A crash between commit and acknowledgment replays the entry without replacing accepted usage or duplicating attempts. Database outages retain ready facts for retry. Pending admissions left by a stopped process become explicit interruption facts during journal reopening.

The HTTP shutdown drains requests before closing the journal. Ready facts do not need to finish delivery before shutdown because they remain on disk. Configure `event_queue_path` on persistent local writable storage; one process exclusively owns each journal file. Do not share a journal between instances or delete it during an outage. Journal files are created with mode 0600 and new directories with mode 0700. Relational schemas and business data continue to use GORM with PostgreSQL/MySQL; bbolt is only the bounded local transport journal.

Authorization during a primary-database outage is separately bounded by the five-second lease in [RUNTIME](RUNTIME.md). Buffering does not extend authorization indefinitely.

## Internal attempt attribution

Each actual attempt preserves its selected `CredentialID` and published
`SnapshotID` alongside Provider/Connection attribution. Failed attempts, final
native results and fsynced active interruption checkpoints copy their own exact
dispatch context. A later retry cannot replace earlier IDs. No-attempt rejections
do not fabricate a dispatch. The database compatibility path without an active
runtime can retain a Credential ID with an unknown publication snapshot.

Frozen GORM version 32 adds two non-null `VARCHAR(30)` columns with empty defaults
and a `(credential_id,snapshot_id,completed_at,id)` lookup index. IDs are historical
metadata with no live catalog/publication foreign keys. Legacy database and journal
records retain empty/unknown values; neither the parent logical call snapshot nor
the mutable catalog is a fallback. Service validation rejects unsafe or oversized
non-empty IDs. Relational delivery copies each attempt exactly and preserves the
existing transaction, first-fact deduplication and quota settlement contracts.

The existing journal JSON carries these bounded fields without changing its
format, capacity or payload limit. The direct-database compatibility path also
checkpoints before dispatch; a checkpoint failure returns 503 before any upstream
request and clears the unstarted attempt identity. Interrupted attempts remain
`process_interrupted` with unknown work, even if an HTTP response had been accepted
before a crash. Attribution alone does not prove native completion, current
configuration eligibility or safe planned predecessor retirement. HTTP/call
success and token-usage completeness are not native completion evidence.

These fields remain internal: Personal, Project, platform call DTOs and CSV exports
are unchanged. No Credential secret, ciphertext or request/response content is
recorded. Full checks and the PostgreSQL/MySQL matrix passed for this package;
see the [implementation record](IMPLEMENTATION.md) for exact evidence.

## Native completion evidence

The internal attempt field `NativeCompletionEvidence` records one exact
server-owned value: unknown, completed, handoff, blocked or incomplete. It is
independent of HTTP/call status, work evidence, output delivery and final token
usage. Legacy database and journal fields normalize to unknown; no successful
status or complete usage can backfill a native terminal. Public DTOs/CSV stay
unchanged, and no native content is retained.

Protocol parsers own these observations. Chat requires a recognized assistant
choice and finish reason for every requested bounded choice; empty objects,
missing finishes, usage-only frames and a bare `[DONE]` remain unknown. Responses
uses its native terminal status and bounded recognized output semantics: refusal
remains blocked, tool handoff remains handoff, and weak/future outputs stay unknown.
Messages requires its validated message-stop
sequence. Gemini records evidence only after clean EOF and complete candidate or
prompt-block semantics; transport failure or cancellation cannot convert an
earlier finish into completion proof. Tool handoff, blocking and output limits
remain distinct from normal completion. Unknown future reasons preserve existing
forwarding behavior and remain unknown evidence.

A recorded terminal does not override the actual final attempt status. Any future
planned-retirement gate must independently require a successful completed attempt
for the exact successor Credential and current publication, plus scoped eligibility.
The complete format/check/test and PostgreSQL/MySQL matrix passed for this
package; see the [implementation record](IMPLEMENTATION.md) for exact evidence.

## Access and HTTP API

All paths are relative to `/api/v1` and require a valid session. Personal endpoints always add the current user ID and exclude Project- and Team-attributed rows. Platform endpoints require `calls.read_all`, which may come from a built-in or custom role. A request for another user's fact returns the same `404` as a missing fact.

| Endpoint | Result |
|---|---|
| `GET /calls` | Current user's call facts |
| `GET /calls/export.csv` | Complete bounded CSV for the current user's filtered facts |
| `GET /calls/:request_id` | Current user's safe call detail |
| `GET /admin/calls` | All users' call facts, with actor IDs |
| `GET /admin/calls/export.csv` | Complete bounded platform CSV, with recorded User/Project/Team/membership attribution |
| `GET /admin/calls/:request_id` | Administrator detail with safe routing and attempt metadata |
| `GET /projects/:project_id/calls` | Authorized Project call facts |
| `GET /projects/:project_id/calls/export.csv` | Complete bounded CSV for an authorized Project |
| `GET /projects/:project_id/calls/:request_id` | Authorized Project safe call detail |
| `GET /teams/:team_id/calls/export.csv` | Complete bounded current-member own-actor Team CSV |

Lists return `{items: [...], next_cursor: string | null}`. Supported filters are `status`, `model_id`, `key_id`, `from`, and `to`. Timestamps use RFC 3339 and bounds are inclusive. Platform queries may also filter the exact immutable acting `user_id`, including that user's Personal and Team facts. Project Key facts have an empty User ID; neither their creator nor a manager is substituted. Case aliases do not match, and unsafe identifiers (including trailing whitespace) are rejected. Personal and Project queries reject that parameter.

`limit` defaults to 40 and must be between 1 and 100. `cursor` is opaque to clients. Pagination orders by `started_at DESC, request_id DESC`, so equal timestamps have deterministic order. Filters apply on the server before pagination. Clients must preserve the same filters while following a cursor.

Member items and details contain:

```text
request_id, model_id, model_name, key_id, protocol, status, stream,
started_at, completed_at, duration_ms, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
image_inputs, pdf_inputs, pricing_status, charge_amount, charge_currency
```

Platform list items additionally contain `user_id`, `project_id`, `team_id`, and `team_membership_id` when applicable. Platform detail also contains `provider_model_id`, `connection_id`, `error_code`, `route_stop_reason`, `attempts`, `price_etag`, and `pricing_snapshot`. The latter contains normalized rate/FX inputs and the assessed quote, not content or credentials. Each attempt contains its ID, ordinal, provider-model and connection IDs, outcome, failure class, replay work evidence, actual output/final-usage flags, an allowlisted evidence code, HTTP status, safe error code, and timestamps. Member DTOs never include upstream route IDs, attempt diagnostics, error codes, or other users' identities.

CSV export applies the same filters without a cursor or page limit and orders the captured rows by `started_at DESC, request_id DESC`. Personal and Project files use exactly the member-safe fields above. Platform files add only `user_id`, `project_id`, `team_id`, and `team_membership_id`; detail-only route and attempt diagnostics remain excluded. Nullable usage and amounts stay empty, explicit zero remains `0`, decimal strings remain exact, booleans use `true` or `false`, and timestamps use UTC RFC 3339 with nanosecond precision.

Export authorization and row selection share one read-only repeatable-read transaction. Generation has a five-second execution bound, a 10,000-row limit, and an 8 MiB encoded limit. The server builds the complete file before writing the response; an overflow returns `422` rather than a truncated download. Empty results return a header-only file. Every string cell is checked after leading Unicode whitespace and control characters; values beginning with `=`, `+`, `-`, or `@` receive an apostrophe before standard CSV quoting. Downloads use fixed RouteX filenames, UTF-8 without a BOM, `private, no-store`, and `nosniff`.

Authentication responses and these protected API responses use the shared no-store policy. Invalid filters return a sanitized `400`; unauthorized access returns `401` or `403`; scoped misses return `404`.

## Verification

`testCallLifecycle` runs against PostgreSQL and MySQL through the single isolated database integration lifecycle. It covers concurrent duplicate delivery, immutable accepted facts, attempt conflict rollback, V25 upgrade/reentry, ordered diagnostics, fresh-service persistence, unknown usage, ownership filtering, indistinguishable cross-user misses, member/platform DTO boundaries, safe error classification, deterministic cursor pagination, and time/status/model/Key/user filters.

`testCallExportLifecycle` covers all three authenticated export scopes on PostgreSQL and MySQL, including delegated platform permission, Project-manager isolation, exact acting-user JSON/CSV filter parity across Personal and Team facts, captured Team/membership IDs, empty Project User attribution, formula protection, exact headers, fixed download metadata, header-only empty results, and rejection of cursor/page inputs. The expanded parity fixture passed on both real drivers (focused Handler 58.742s). It uses seeded immutable historical facts and proves projection parity, not new native recording behavior. Unit tests cover exact database-adapter predicates, nullable versus zero encoding, exact decimals and timestamps, safe member/platform columns, CSV syntax, formula prefixes after control or Unicode whitespace, and complete failure at row or byte limits.

Gateway tests separately establish that real controlled-upstream success, failure, stream completion, and cancellation produce the corresponding facts. Data-store tests alone do not prove gateway recording behavior. Run `go tool task test-integration` for the real database lifecycle and consult [the implementation record](IMPLEMENTATION.md) for current evidence and remaining acceptance work.

`testRecorderLifecycle` verifies database-outage buffering, process reopen, pending interruption recovery, commit-before-acknowledgment replay, private file permissions, secret exclusion, and exact-once accepted facts on both databases. Journal unit tests cover capacity, concurrent admission, process locking, and atomic completion. These correctness tests do not establish the production event-latency target.

## Project history

`GET /projects/:project_id/calls` and `GET /projects/:project_id/calls/:request_id` expose sanitized facts to current Project managers or holders of `calls.read_all`. Lists use the existing filters and pagination, with the Project forced by the path. A former creator or unrelated user receives the same 404 as a missing resource. Disabled and archived Projects retain readable history for authorized readers. Project facts contain a Project ID and an empty user ID; they never appear in the creator's personal history. Administrative fact DTOs include `project_id` when applicable. Migration 9 adds this historical attribution column without altering accepted personal facts.


## Team Session attribution and history

Frozen V38 preserves optional historical Team and membership IDs with the acting
User ID. Team facts have blank Project and Key IDs and cannot mix subjects. No live
Team or membership foreign key rewrites or deletes immutable facts. Personal
history, usage and CSV exclude Team calls; platform call DTOs and CSV retain the
recorded Team IDs. Existing unknown historical Key attribution remains unknown.

Team list and detail endpoints require a current enabled active exact member and
expose only that member's own acting User ID in that Team. Owners and directory
administrators have the same own-actor boundary. User/Key filter expansion is
rejected; model/status/time filters and cursor pagination remain scoped. Membership
removal stops new reads without rewriting history. See [Team Session inference](TEAM_INFERENCE.md).


## Own-Team call CSV

The Team export requires the exact current enabled member and active path Team.
Owners retain the same own-actor scope; platform permissions do not bypass
membership. Removal or inactivity denies new reads, while rejoin retains the
original historical MembershipID in immutable call facts. Authorization and row
selection share one read-only repeatable-read transaction.

Only `status`, `model_id`, `from` and `to` filters are accepted. Duplicate,
malformed, unknown, foreign and page selectors fail, including supplied-empty
unsupported keys. The complete file uses the existing member-safe 19 columns,
fixed `routex-team-calls.csv` name, exact decimals, null/zero distinction, UTC
timestamps and formula protection. Five-second, 10,000-row and 8 MiB limits
return a complete JSON failure; empty results contain only headers. Team usage
report CSV is separate.

The existing filter-row action captures fresh actor, Team, successful Session
generation, scoped list authority and applied filters. Renewed/error reads,
logout, scope/filter changes and unmount abort and discard obsolete responses
before object-URL creation. Blobs stay transient, duplicate clicks share one
pending dispatch, and the temporary URL is released.

The integrated implementation passed 2381 frontend tests in 125 files, four
Node development tests, Go race, development/production asset tests and mandatory
checks. The focused real-driver Team/export lifecycles and complete ordered
91-case-per-driver regression with eight constraints passed on PostgreSQL and
MySQL, followed by both-driver authentication and restart checks.

Controlled native/process acceptance passed four completed calls with known
usage and exact immutable attribution, including Personal/peer/other-Team
separation, membership removal denial, retained history after rejoin and restart.
CSV reads preserved protected policies, grants, Keys, credentials, audits and call
facts. A fresh production artifact after the mobile-width correction passed the
same four-call process checks. Bilingual in-app browser checks verified scoped
rows, filters, reset/refresh, explicit export and readable internally scrolling
mobile columns without console errors. The browser reported prepared downloads,
but its download event did not expose a saved file and Chrome was unavailable;
file landing and browser-file byte comparison remain unverified. Actual HTTP CSV
bytes, complete headers and exact decimals were verified separately. Earlier
helper-only timestamp-spelling and unsupported-status failures remain historical.
Seeded projection fixtures do not establish native completion evidence.
