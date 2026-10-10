# Audit history

`GET /api/v1/admin/audit` requires a current session and `audit.read`. It reads
committed transactional business events without writing or triggering gateway
publication. A browser's hidden controls do not replace the server permission
check. Personal or Project ownership alone grants no audit-history access.

## Query contract

Optional single-valued parameters are `q`, `range`, `category` and `cursor`.
Unknown or repeated parameters return `400`. `range` is `24h`, `7d` (default) or
`30d`; it is a rolling UTC interval evaluated for each page. `q` is a literal,
case-insensitive substring of the action, resource ID, actor ID or current actor
name, limited to 100 Unicode characters. Percent and underscore are literal, not
wildcards. The category is `all`, `models`, `keys`, `limits`, `credentials`,
`pricing`, `identity`, `site` or `system`.

Responses contain `items` and a nullable `next_cursor`. Pages contain at most 50
events ordered by descending stable event ID. A cursor continues below that ID;
newer events require a fresh query. Changing filters must reset pagination and
selection. Historical actors without a current profile remain identifiable by ID.
Displayed actor names are current profile values, not historical name snapshots.

Each event contains `id`, `actor_id`, `actor_name`, `action`, `resource_type`,
`resource_id`, `created_at`, `result`, `source`, `ip`, `request_id` and `changes`.
The result is `committed`: these rows prove successful database transactions.
Failed authorization, rejected writes and rolled-back operations are not present
in this existing ledger. A missing event does not prove that no attempt occurred.

## Recorded and unavailable metadata

Historical rows do not record client IP, request ID or universal entrypoint;
these fields return null and interfaces display **Not recorded**. Do not infer
an IP from current sessions or fabricate request IDs from event IDs. Price import
source is returned only for the known `api`, `csv`, `xlsx` and `xls` values.

Only known typed price, currency, limit, Credential lifecycle, Team request, OIDC/OAuth
configuration/binding and system-instance cleanup schemas are projected into `changes`. Price, currency, and limit events use
lowercase `before`/`after` and applicable `reason`/`etag`. Cleanup events expose
one event per retired instance: `resource_id` is the exact process-generation ID
and typed `changes` contains only the reviewed heartbeat revision. A multi-row
cleanup commits all retirement rows and their target-addressable events in the
same transaction.
Team request creation and approve/reject/withdraw events record only the exact
Team/applicant IDs, dimension, decimal-string target, currency, step ID, stage,
resulting status and reason. They appear in the `limits` category. An owner
approval can record advancement to the platform stage without policy application;
the historical event does not prove that a later quota revision is enforced.
`model.alias.retire` projects only the exact Model/name, previous and shortened
UTC deadline, and required reason. The row proves its database transaction;
current non-callability or runtime publication is a separate review, and a
matching target retry never creates an original-operation receipt.
OIDC and custom OAuth events project only a validated versioned reason. Provider configuration,
claims, subjects, cookies, codes and tokens are never audit readback fields.
Unknown detail fields, unknown action payloads, malformed JSON and oversized
payloads are not exposed. Provider credentials, request bodies and arbitrary
stored JSON never become an audit readback API. Other events retain their action
and target but return null changes; there is no reconstruction from current data.

Persistent request metadata, broader before/after capture and failed-attempt
security events remain separate work. They require explicit collection and
retention contracts rather than invented historical values.

## Verification

Focused tests cover filter validation and typed detail redaction. The shared
PostgreSQL/MySQL audit lifecycle verifies authentication, pagination, literal
wildcard searches, actor-name lookup, category/time filters, strict queries,
redaction and immediate permission loss. Browser and external operational
acceptance must be recorded separately from these controlled tests.

## Web interface

The operations audit page uses a compact literal search, time-range/category selectors, an eight-column horizontally scrollable event table, and a bordered details drawer. Rows and their accessible actor buttons open the same authoritative event. The page requires current `audit.read` before mounting its list query. It displays committed successful actions only; actor names are current profile labels, and stable actor IDs remain visible in details.

Filters issue real scoped API requests, reset cursor pagination, close the selected drawer, and cancel obsolete requests through `AbortSignal`. A late response from an older filter cannot replace the current table. Cursor pagination retains loaded rows during a next-page failure and offers a real retry. An empty result is distinct from a failed request.

Missing source, IP, request ID, and change metadata display **Not recorded** in both table and drawer. The interface does not infer old values from current resources or manufacture failure events. Typed before/after values, reason/ETag data, and reviewed cleanup identities render as plain text; exact price decimal strings remain unchanged. Operator names, resource identifiers, action codes, and change content never become HTML. Dates and labels follow the selected English/Chinese locale without translating stored identities.

Frontend tests use controlled API adapters for permission denial, initial filters, cursor append/reset, stale-response cancellation, retry, Unicode search bounds, malicious text, precise prices, missing metadata, stable IDs, drawer dates, and live language switching. These tests are independent of the real database lifecycle and browser acceptance gates.

Guided creation emits one typed `model.batch_create` event containing reviewed
Connection, request intent, reason and bounded created Model/binding facts.
Projection validates exact resource ownership and safe scalar fields; arbitrary
JSON and credential material are never exposed. The event and durable receipt
share the creation transaction, and same-intent retries do not duplicate either.
