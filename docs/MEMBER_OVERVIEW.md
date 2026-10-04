# Member Overview Monthly Accounts

This bounded package has complete local controlled acceptance. Full main check/test/build, focused PostgreSQL/MySQL lifecycle, complete main database regression and controlled production browser proof passed.

The member Overview retains its identity header and monthly resource-account
card/table. The self-only `GET /api/v1/overview/accounts` returns Personal plus
cursor-bounded current active Team accounts. It never accepts another actor or
borrows platform directory permissions. Default page size is 10, maximum 50.

Each Team row preserves separate aggregate and stable Team/User member facts.
The aggregate cap and local member cap are conjunctive; they are never added or
flattened into one allowance. Current membership identity accompanies the row,
while the stable member account retains its historical policy and quota journal
identity through authorized removal/rejoin. Neither history nor an old Session
restores current access.

Stored monthly Token and money controls, exact recorded currency and policy
revision are separate from runtime application. Token counters, unknown counters
and money amounts retain decimal strings. Null, explicit zero, absent policy,
inactive accounting, unavailable journal, partial coverage and held amounts stay
distinct. Known settled subtotals never establish complete coverage. Progress is
available only for a finite positive Token cap and covered, known settled usage;
no unsafe numeric coercion, fabricated zero percent or remaining allowance is
permitted. Money remains grouped by recorded currency.

Every monthly account also returns nullable `active_reservations`:
`{tokens_held: string, money_held: {[currency]: decimal_string}}`. These live
reservations come from the same captured journal batch as `usage`, with the same
`usage.as_of`; they require no additional journal or database read. An active,
valid usage snapshot has a reservation object, including exact known zero and an
empty money map when no reservation exists. Inactive or unavailable accounting
returns both `usage: null` and `active_reservations: null`; absence is never
converted into zero. The monthly `usage.tokens_held` and `usage.money_held` retain
the journal's unresolved monthly facts. They never include the separate live
reservation amounts. Aggregate, member and Personal reservations remain distinct,
just as their settled usage does.

The response uses one repeatable-read Control Plane snapshot and batched policies
and account creation bases. A bounded journal batch captures the selected accounts
coherently without per-row database lookups or resource-directory reads. The
bounded query plan uses five governance reads regardless of page size and at most
101 journal accounts: Personal plus aggregate/member pairs for fifty Teams. The
SQL observation timestamp and the journal's shared `as_of` remain separate.

Calendar projection selects the `QuotaSetting` Go fields `TimeZone` and `ETag`
through GORM. The released physical revision column is `quota_settings.e_tag`,
not `etag`. A focused regression checks the actual schema field mapping and the
generated SELECT columns for both supported dialects, without changing the
released migration. Fixture policy and calendar mutations likewise use the
mapped `ETag` field.

Runtime application requires the complete current publication, policy revisions,
normalized policies, calendar, monetary denomination, actor, Team and membership
identities, leases and denial markers. A saved policy alone proves no enforcement.

The interface uses actor/Session-generation scoped queries and hides private rows
and actions during renewed reads, errors or context changes. Obsolete responses
cannot restore them. Pagination stays bounded. Usage links retain `/usage` or
`/usage?team=<canonical-team-id>` scope. English is the default; both catalogs
cover all labels, dates, status, empty/error and accessible names.

Thirty-day cards, trends and Model/Key breakdowns are separate unfinished work.

Source verification completed with `go tool task check`, `go tool task test` and
`go tool task build`. The isolated frontend suite passed 83 files and 1,408 tests; the integrated main
suite passed 90 files and 1,584 tests. Go race,
development lifecycle and production asset tests also passed. The production
binary is built from the candidate with the reservation DTO and calendar mapping
repair. These results do not establish real database or browser acceptance.

Focused controlled PostgreSQL/MySQL lifecycle verification passed, with Handler
82.517 seconds. The complete fixture covers separate live holds, settlement,
unknown native usage, stable account/rejoin, fifty-row bounds and constant query
count, exact retained relationships, stale publication/leases, lifecycle changes,
unavailable journal, restart and offboarding. Six unchanged related fixtures also
passed on both drivers in preceding runs.

Failed earlier runs exposed calendar column mapping, an overlong fixture price ID,
an omitted recorded quota denial and a storage-normalized excess trailing space.
The repaired fixtures retain both denied and successful immutable facts and prove
no dispatch for admission denial. When a database retains canonical full-width ID
bytes after discarding an excess trailing space, the fixture confirms canonical
current authority rather than claiming persisted-alias denial. Successfully
retained aliases still fail exact authorization. Production constraints and
collation checks remain intact.

Controlled production browser acceptance passed against the same integrated
binary. English/Chinese controls, exact decimal strings, Personal/Team usage
links and ten-plus-one pagination passed. A separate earlier controlled run
proved live aggregate/member reservations and their settlement; its subsequent
history-count helper failed and does not count as whole-run acceptance. The
final complete run retained five actual native dispatches, a durable quota denial
with no attempt or upstream dispatch, and explicit unknown usage. Unknown Team
usage disabled percentages while known Personal usage stayed at ten percent.
Current membership removal hid the Team account; authorized rejoin restored its
stable history under a new membership ID. A real-process restart preserved the
Session, journal and exact account facts without replay. Browser error/warning
logs were empty, English was restored and owned resources were removed.

Earlier browser helpers also exposed DTO-list/detail assertion mistakes and an
operator-delayed held request that exceeded its fixture lifetime. Those failed
runs do not establish acceptance or weaken runtime bounds. The final denial
checks use the exact administrator call detail rather than unavailable list
fields. Complete main PostgreSQL/MySQL regression passed with Handler 1091.619 seconds
and Service 7.953 seconds. Final mandatory check passed, and owned Compose
resources were removed. No external-provider or deployment acceptance is claimed.
