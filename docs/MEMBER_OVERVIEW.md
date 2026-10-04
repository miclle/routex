# Member Overview Monthly Accounts

The self-only monthly account package has complete local controlled acceptance.
Full main check/test/build, focused PostgreSQL/MySQL lifecycle, complete main
database regression and controlled production browser proof passed. The separate
administrative member Overview below also has complete local controlled source,
dual-driver, production/browser/restart and full-matrix acceptance. Its checked
delivery is represented by the commit containing this acceptance record; new
remote CI remains pending.

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

## Administrative member Overview

The existing administrative member detail Overview tab contains three cards:
Personal monthly Tokens, Personal monthly money and total retained Personal Keys.
The existing access-status section remains below them. This is separate from the
self-only monthly account table and its Team enumeration.

`GET /api/v1/admin/members/:user_id/overview` returns `user_id`, UTC `observed_at`,
`platform_currency`, `personal` using the monthly account shape above, and
`total_personal_keys` as an exact nonnegative integer string. No query parameters
are accepted. Subject IDs are bounded to 1–30 safe ASCII identifier bytes and are
matched exactly in persistence; aliases never substitute a different identity.
Responses use `Cache-Control: private, no-store`.

The enabled current reader requires independent `members.read` authority. Neither
administrator role nor member-write authority substitutes for that permission.
The retained target may be disabled or offboarded: saved policy, journal facts and
Key count remain read-only historical facts, while runtime application is false.
This endpoint never impersonates the target or calls self Overview with a
substituted actor. Missing targets return not found; failed count or governance
reads fail the response rather than fabricating zeros.

The Personal policy uses the existing default-aware resource-policy read and its
persisted creation/restoration basis and revision. Later default edits do not
replace an already captured subject policy. Monthly Tokens and money preserve
null versus explicit zero, exact currency and decimal strings. Known settled
usage, unresolved monthly facts, live reservations and unknown coverage remain
separate. Active accounting has both usage and active-reservation objects;
inactive or unavailable accounting has both null. Journal outages preserve known
SQL facts while leaving usage unknown. The cards infer no progress, combined
allowance or conversion across recorded currencies.

The Key card counts all retained Personal Key statuses, including disabled,
revoked and expired records. It excludes Project Keys and returns no Key IDs,
secret material, directory, actions or active-Key claim. This minimal count is
part of the authorized member read; no Key permission or mutation is added.

The bounded read has seven SQL reads in one repeatable-read, read-only snapshot:
reader, permission, target creation/lifecycle, target policy, platform currency,
quota calendar and Personal Key count. One coherent journal account batch supplies
monthly and live reservation facts without per-Key or per-account getters. SQL
`observed_at` and journal `usage.as_of` remain distinct observations. The request
has a five-second service deadline; no migration, accounting or native admission
behavior changes.

Runtime application additionally requires the private published exact User
creation/lifecycle proof, complete normalized policy/revision, current calendar
revision/timezone, monetary denomination, current publication identity, lease and
denial markers. Disabled, offboarded, missing, differently created or tombstoned
subjects cannot claim application even when the reader is authorized. An active
SQL snapshot against a newly published disabled subject, or a raw reenable against
a still-disabled publication, stays unapplied. This proof requires no subject
Session, Key or Model and does not change native eligibility.

Parent member detail and Overview reads share actor, exact target and successful
Session network-generation guards. Pending renewed reads, errors and target or
actor changes hide prior private facts. Structurally identical successful renewal
in the same millisecond still isolates the generation; obsolete reads and mutation
callbacks cannot restore old authority. The existing Session observer is reused;
manual CSRF cache replacement alone does not count as a network renewal. English
and Chinese labels, dates, empty/error states and accessible names use the paired
`governance` namespace.

The reviewed seventeen-path source is integrated on checked documentation
baseline `91861d3ef20f45102510ec96f5927401fd924b11`, following delivered V51 source
`5363d3ce53ca4d1248227b2f941aae642c433499`.

Full format/check/test/build passed with 2130 frontend cases in 112 files, Go
race/unit, Node development lifecycle and embedded asset checks. All 95 combined
protected source hashes and package/lock bytes were verified exact after restoring
only the known eighteen Linux libc optional-metadata differences. Production
binary SHA256: `11b959310c7e6b43a7c17c5185ccb387c057521f020afc238bdd1f76be7bf84d`.

The actual PostgreSQL/MySQL focus passed Handler 134.955s (parent 132.72s;
PostgreSQL 56.12s, MySQL 76.60s). New administrative member Overview and both V51
lifecycle/migration cases passed on both drivers. The member fixture retained eight
genuine native calls per driver; V51 retained its separate fixed five per driver.
All 95 source hashes stayed exact and every owned focus container, network and
volume was independently verified absent. This is focused acceptance, not a
complete regression or production/browser claim.

Controlled production/browser/restart acceptance passed with the unchanged
binary and all 95 source protections. Six initial native dispatches became seven
during the held call (six accepted immutable calls), seven accepted after release,
and eight dispatches/eight calls after the unknown-usage call. The subject's known
settled usage was `5` Tokens / `5.000000000000000001 USD`, then `10` Tokens /
`10.000000000000000002 USD`. Live reservation `5` Tokens /
`5.000000000000000003 USD` stayed separate and cleared after settlement.

The captured default was `100` Tokens / `100.000000000000000001 USD`. A zero-policy
subject retained explicit `0`/`0` with genuine known-zero usage. Explicit unlimited
policy and one unknown-usage record preserved the known subtotal. The count stayed
five retained Personal Keys across statuses, excluded Project Keys and exposed no
Key identity or secret. Peer, Project and Team attribution did not enter the
subject's Personal card.

English, live Chinese and the selected target stayed correct. Disabling the
subject retained history with runtime application false and made the old Session
return 401. Reenabling did not restore that Session or revoked Keys. Revoking the
reader's permission produced helper 403 and renewed-browser denial with private
cards absent; restoring it required a fresh authorized read. Same binary/config,
database and journal restart retained eight calls/attempts and fresh real-browser
Session data without inference replay. Observer evidence was 22 browser Session
HTTP 200 responses plus one initial anonymous 401 and 15 Overview HTTP 200 reads;
it does not establish minute automatic renewal. Console errors/warnings were zero.
All owned browser, app, proxy, port and Compose resources were removed.

The first helper run failed its warmup with HTTP 503 because it omitted explicit
binding activation: real Model creation correctly leaves a candidate at weight
zero. That cleaned run is not acceptance. The corrected temporary helper first
asserted the exact zero-weight single Chat candidate, then used the real binding
write to set 100 and confirm readiness. No product source changed; the original
helper was preserved. Accepted helper SHA256:
`f0699386648a542f4580eee6dcea36564b53c5e8da97fd98ab6c8ce895f0ac9e`.

The complete 85-case-per-driver PostgreSQL/MySQL integration matrix passed
Handler 1482.117s and Service 8.317s. Runner and coordinator independently verified
all 95 protected source hashes unchanged and every owned matrix container,
network and volume absent. Full source check/test/build, focused dual-driver,
controlled production/browser and same-artifact restart gates are locally
accepted. Prior self Overview and V51 acceptance remain independent; formal
totals are unchanged.

The checked implementation is delivered by the commit containing this acceptance
record; consult Git history for its SHA. Remote CI for that new commit remains
pending.

## Personal thirty-day usage

The existing identity header and monthly resource accounts are followed by three
Personal cards for recorded requests, Tokens and success rate, a full-width Token
trend, and Model/API Key detail tabs. This report uses the existing Personal
Usage API with `period=30d`, `timezone=UTC`, `granularity=day`, `compare=false`.
The server captures one half-open `[now - 720h, now)` interval. Calendar buckets
retain partial edges, so the returned report need not have exactly 30 buckets.

All recorded requests enter the existing success-rate denominator, including
cancellations and admission failures. Token values remain exact strings; known
subtotals and unknown coverage remain separate, and unknown trend buckets are
gaps. Shares use bounded BigInt ratios only for known positive totals. Historical
Model/Key labels and IDs come from the report without a directory lookup. The
view shows returned range, query observation, latest selected completion and
durable-delivery lag; it never presents this report as quota or combined Team
allowance. Actor and successful Session generation scope queries; renewal, errors
and obsolete reads cannot restore old private facts. Localized complete-query
overflow links to Usage without silently reducing the request.

The isolated source passed full check/test/build and 1625 frontend cases/93
files. The four-case PostgreSQL/MySQL focus passed in 117.082 seconds, including
existing Usage, Team Usage and monthly Overview. These seeded historical facts
are separate from native invocation proof. The package is now carried onto the
checked Team-media main baseline. Rebuilt-main check/test/build passed with 1681
frontend cases in 95 files, Go race, development lifecycle and embedded production
assets. Complete main PostgreSQL/MySQL regression passed (Handler 1123.214 seconds;
Service 7.545 seconds), including the new report and all existing lifecycle and
migration cases. The final required check passed; owned matrix resources were
removed. The bounded thirty-day package has complete local controlled acceptance.

Controlled production/browser acceptance passed on that rebuilt binary. Seven
actual local upstream dispatches produced eight immutable facts across Personal,
Team, Project and administrator scopes. The Personal report contains four
requests, two successes, one cancellation and one admission failure: 50% success,
known Token subtotal `5`, three unknown Token/amount records and exact known
`4.000000123456789012 USD`. The pre-admission 429 has no attempt or dispatch and
retains `not_captured` with null counters and charge; it is not a known zero.
The monthly account journal remains independent, with two unknown records rather
than the report's three. Team aggregate `10` and this member's `5` are separate.

The bilingual browser verified cards, unknown trend gaps and Model/Key tabs,
including recorded Model names after a live rename and both historical Key IDs
after revocation. Membership removal hides the Team row without changing Personal
facts; rejoin restores independent historical Team accounting. Real-process
restart retains sessions/facts without replay. Revoking the browser Session
returns to sign-in and clears private composition; a fresh sign-in restores only
current-authorized data. English was restored, warning/error logs were empty and
all owned browser/process/Compose resources were removed.

Two earlier setup runs failed an incorrect helper assumption that a pre-admission
failure had known zero usage. The helper was corrected to assert actual null
coverage and zero attempts/dispatch; production semantics were unchanged. The
successful run's assertion checked three unknown records, while its printed
summary initially retained a stale value of two. That reporting field is corrected
for future runs; it is not used as coverage proof.
