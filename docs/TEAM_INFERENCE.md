# Team Session Inference

This bounded implementation has local controlled acceptance. Team Session inference uses an
explicit active Team and the signed-in member's current Session. It does not create
a Team Key, expand Personal Key grants or choose a Team from a union catalogue.

## Native endpoints

- `GET /api/v1/teams/:team_id/inference-models` returns native `{object,data}`
  model discovery restricted to current eligible Chat routes. `id` is the public
  model name; `model_id` is its stable identity for verified navigation.
- `POST /api/v1/teams/:team_id/chat/completions` accepts native Chat JSON and
  returns ordinary JSON or native SSE. Authentication is the existing HttpOnly
  cookie, same-origin checks and the current cookie-derived `X-CSRF-Token`.
- `GET /api/v1/teams/:team_id/calls` and `/:request_id` expose content-free facts
  for the current member's own actor within that Team. List filters are cursor,
  limit, status, model and time. Nonempty user/Key filters are rejected. Owners and
  platform directory administrators receive no implicit broader read authority.

The Gateway authenticates private short-leased Session hashes and current exact
User, Team, membership and model grants from publication. It does not query Control
Plane tables for each native request. Local revocation tombstones block subsequent
attempts after committed Session, account, Team, membership or grant changes.
Already dispatched work may finish. Other processes are bounded by the existing
authorization lease; no immediate fleet revocation is claimed. Expired or absent
publication fails closed. Session-only publication leaves Provider configuration
digests and Personal/Project Key semantics intact.

## Invocation and accounting

Text-only Chat, Responses, Messages and Gemini use their native Team Session
endpoints and independent terminal/usage semantics. Images, PDF input and attachment
scalar references are rejected before storage reads or upstream dispatch. Opaque
text and tool payloads retain their meaning; content positions are bounded and
validated separately for each protocol. Team comparison uses independently dispatched native text lanes. Attachments remain unfinished for this authentication source. Request code uses independent environment-driven Session authentication.

The same native attempt pipeline retains one durable logical admission, native
terminal evidence, immutable usage and assessed prices. Team aggregate and stable
Team/User pair journal accounts are separate from Personal and Key accounts.
Default unlimited accounts establish attribution and replay. The separate
[finite Team policy package](TEAM_LIMITS.md) adds aggregate and member token, money
and request-rate enforcement. Membership removal/rejoin cannot reset pair identity
or use. [Monthly Team member requests](TEAM_REQUESTS.md) separately review quota increases; pending and escalated steps never change native admission.

Frozen GORM V38 adds historical `team_id` and `team_membership_id` fields with empty
defaults, a Team actor cursor index and a guard against mixed Team/Project/Key
subjects. Team facts retain the acting `user_id` and blank Project/Key IDs; old
facts remain unknown without inferred ownership. No mutable Team or membership
foreign key can rewrite or delete history. Personal calls, usage and CSV export
exclude Team facts. Platform history/export retains immutable Team attribution;
Team Session calls do not become a fabricated Key ranking.

## Interface boundary

The existing Playground source selector offers API Key by default and explicit
named Team Sessions from the member's own active Team list. Team discovery and
native transports use separate actor/Team identities and current CSRF; Key transport
continues to omit cookies. Changing source, Team, model, actor or page aborts
requests and clears transient conversation, credentials and selected attachments.
Late responses must not restore data from the previous authority. Team call tables
use distinct actor/Team query keys and hide cached facts on authorization failure.

## Verification and remaining acceptance

Final `go tool task check` and `go tool task test` passed, including 843 frontend
cases in 61 files, Go race/unit, four Node checks, development lifecycle and
production embedded assets. Actionlint passed. Focused actual PostgreSQL/MySQL
race acceptance passed in 261.152 seconds; the full matrix passed (Handler
610.579 seconds, Service 5.598 seconds). `go tool task test-auth-lifecycle` passed
on both databases with real process restart, persisted Session and native Key
regression coverage. Owned Compose resources were removed.

The migration/authentication/native/history fixtures cover empty, upgraded,
repeat, concurrent and partially applied V38; ordinary/SSE exact usage; durable
restart/replay; current Team-only authority; actor/cross-Team isolation; Session,
member and grant revocation; DB outage and authorization lease expiry; Personal
ledger/history/export exclusion. Background recorder fault simulation uses a
single fixed callback with atomic switches rather than mutating GORM callbacks
while work is running.

An owned production binary/PostgreSQL/native fixture and in-app browser verified
explicit Team selection without a Key, real SSE input 4/output 1, unknown omitted
total, live English/Chinese switching, own-actor history/detail, denied invocation
and hidden history after membership removal, new relation after rejoin, and
persisted history/new invocation after restart. Owned resources were removed.
This is controlled proof; external Provider acceptance, complete Session protocol
coverage and full product acceptance remain open. The later finite-policy evidence
is recorded independently in TEAM_LIMITS.md.

## Native text protocol extension

Session-authenticated `POST /api/v1/teams/:team_id/responses`, `/messages` and
`/models/:model_action` join existing `/chat/completions`. Gemini streaming accepts
only `alt=sse`; other native Team queries, Key credentials and workspace selectors
are rejected. Middleware removes native credential headers and query values before
logging/recovery, including malformed native paths. Messages retains its native
version header and Gemini retains native contents/systemInstruction/generationConfig.
No handler converts a protocol to Chat or borrows Personal attachment ownership.

Discovery returns only current ready native protocol routes with empty input
capabilities and explicit Team scope. Member catalogue Team source links expose
these protocols independently of Personal Key availability. Each native attempt
uses existing immutable Team/membership facts, aggregate/member admission and
settlement. Unknown terminal usage remains unknown, and finite policies fail closed.

The Playground retains its existing source/model/protocol controls and uses the
matching parser with transient completed text histories. Renewed Session reads hide
old grants; authorization failure clears stale selection/transcript. Cancellation
and late responses cannot enter history or replay a request. Final mandatory check and full test passed 1151 frontend cases in 77 files.
Complete PostgreSQL/MySQL race regression passed (Handler 929.393 seconds; Service
7.045 seconds), and both authentication/native process lifecycles passed.
Controlled production/browser/restart proof passed each ordinary/streaming native
protocol, terminal usage, completed history, truncation failure, native-only
catalogue links, revocation without dispatch and persisted Sessions.
English/Chinese passed with English restored, no console errors and owned
resource cleanup. These results do not establish external-provider acceptance.


## Independent native comparison lanes

The existing comparison workbench supports two to four current Team-native lanes
with one shared prompt. Each lane has its own protocol request, cancellation,
request ID, native completion and authoritative usage. Only completed plaintext
turns enter that lane's future history. Finite admission/settlement remain separate
Team aggregate and stable Team/User accounts; no Personal or Key attribution is
introduced. Current model/member changes and renewals require fresh explicit
discovery before new dispatch. Restart preserves the server Session and durable
call facts while browser workbench credentials/history remain transient.

Native 401 handling uses a bounded current Session probe; upstream rejection
cannot establish Session expiry. Cancellation or late probe results cannot clear
a newer authority. Source-specific media remains separate. Request code uses the existing dialog and independent environment-driven authentication, as described in [Playground](PLAYGROUND.md).


Team authority renewal observes every successful network Session read, including
structurally identical data with an unchanged timestamp. Clear Team models,
completed history and captured code, abort pending inference, preserve unsent
drafts, and require explicit model rediscovery. Observe the existing Session query
without creating an additional network observer. Manual same-actor CSRF cache
replacement is not a Team authority renewal and preserves completed history.
