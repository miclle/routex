# Team Usage

Team usage reports provide shared aggregate statistics for a current Team. They
read immutable, deduplicated call facts and do not expose another member's call
history, API Keys, identity, or provider diagnostics. Controlled implementation and acceptance are available; complete platform
release acceptance remains separate.

## Authority and attribution

`GET /api/v1/teams/:team_id/usage` requires a current enabled account, an active
Team and an exact active membership in the same repeatable-read snapshot as its
fact selection. Owners have the same report scope as members. Platform authority
does not bypass membership on this endpoint. Removing or disabling membership,
offboarding the account or deactivating the Team prevents subsequent reads.

The report aggregates every canonical fact attributed to the exact Team, including
historical contributors whose membership changed. It never joins current member
relationships to rewrite historical usage. Personal and Project facts and records
with contradictory Project or Key attribution are excluded before row limits.
The separate Team call-history endpoint remains limited to the current actor.

`GET /api/v1/admin/usage?team_id=...` uses independent current `calls.read_all`
authority. Its Team selector filters immutable attribution, including archived
Team history; it does not require current membership or a live catalogue row.
A Team filter cannot be combined with a Personal-user, Project or Key filter. Existing
authorized platform diagnostic dimensions remain available only on this endpoint.

## Report and interface

The member endpoint echoes the exact `team_id` and returns the existing summary,
trend, model groups, comparison and historical currency amounts. Its
`available_dimensions` is `["model"]`, Key groups are empty arrays, and provider,
provider-model and Connection groups are omitted. Contributor identities, request
IDs, membership IDs, Keys, raw call payloads and price snapshots never appear.

Supported filters retain the existing time range, timezone, granularity,
comparison, model, status, native protocol and streaming contract. Member Team
queries reject Key and administrative subject or route selectors; the path owns
the Team scope. Unknown, repeated and explicitly empty parameters remain errors.

The existing personal usage page defaults to Personal and offers named active
Teams from the caller's own paginated Team list. An optional `?team=` expresses
expected context only; the server always authorizes the selected Team. Switching
sources resets filters and visible results. Queries are scoped by actor, Team and
filters, and failures or renewed authorization suppress stale private reports.
Team mode keeps the existing summary cards, trends, model distribution and charge
summary while omitting Key filters, distributions and rankings. Paired English and
Chinese text preserves the English default.

## Accounting and verification

Reuse bounded complete reads and the existing exact decimal-string arithmetic.
Unknown token coverage stays distinct from known subtotals; currencies remain
separate, and missing trend coverage stays unknown. Persisted reports may lag the
durable journal and are not admission counters. Overflows return an error without
partial totals. No new schema or migration is required.

Acceptance covers real PostgreSQL/MySQL scope isolation, current revocation,
case-sensitive identities, historical contributors, canonical replay, process
reopen, unknown coverage, exact money, strict selectors, independent platform
history and complete-read bounds. Frontend tests cover actor/Team cache isolation,
late responses, explicit target validation, English/Chinese switching and removal
of Key and provider details. Controlled browser and runtime evidence are recorded
separately from full release or external-provider acceptance.


## Accepted controlled evidence

Final check/test passed with 990 Vitest cases in 69 files, Go race/unit,
development lifecycle and production embedded assets. Actual PostgreSQL/MySQL
workflow focus passed (215.926 seconds), and the final complete matrix passed
(Handler 745.019 seconds; Service 6.692 seconds). Controlled native calls from two
actors produced a shared two-request/ten-Token Team aggregate, empty member
Personal usage and one own Team call per actor. Browser revocation hid the old
report; rejoin and process restart preserved immutable attribution. Platform
Team filtering retained authorized provider groups without Key dimensions.
English/Chinese views passed and English was restored. Owned acceptance resources
were removed. Capacity, broader freshness and full release gates remain open.
