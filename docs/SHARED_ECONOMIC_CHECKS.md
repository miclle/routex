# Shared economic quota and rate checks

`checkSharedEconomicAdmissionSQL` is a dormant, read-only admission calculation.
Its caller must hold the initialized economic domain and exact sorted account
locks in one SQL transaction, expire those accounts' projections at the supplied
monotonic instant, and separately establish current authorization, original
ordered policies, calendar coordinates and the proven reservation bound.
The helper creates no account, coverage, receipt, contribution or reservation.
Passing it does not grant dispatch or activate shared admission.

The input requires the complete one- or two-account set, exact case-sensitive
account identities and digest keys, retained microsecond births, nonnegative
independent RPM/executor counts, initialized coverage and an instant at least
the domain clock. Supplied month coordinates are not inferred from the machine
timezone. Unknown or mismatched births never become the current time.

Rejection precedence matches the existing local queue. In original policy
order, RPM is checked before logical executor concurrency; all rate checks
precede the quota-policy loop. Quota checks keep the original policy order,
read that policy's validated active counter, then use
`eventqueue.NewQuotaPolicyRules.Check` with lazy validated SQL window reads.
Minute, five-hour, seven-day and admission-month token checks precede monthly
money and invalid-bound lookup. Token and money checks read their month
separately; no eager window loading or rate/usage counter updates occur.

Every parent/root policy remains conjunctive. Unset ceilings differ from hard
zero, and monthly `alert_only` bypasses only its own ceiling. Coverage, known
bound, unknown usage and currency checks still apply. Money is kept as exact
decimal reservation strings and validated arbitrary-size integer-unit counter
strings; it is never narrowed through floating point. Token and money unknown
dimensions remain independent.

Invalid-bound lookup uses the exact installation and SHA-256 of the original
revision, then validates the original revision bytes, causing request identity
and digest, and the first recorded instant. It never updates that cause,
deletes invalidation history, or acquires an old receipt lock after account
locks. A missing invalidation row does not establish bound authority. An empty
revision still fails the constrained quota rule.

Focused test sources cover lazy read/rejection order, original parent order
despite sorted lock order, immutable inputs, independent rate/executor counts,
missing/case-changed/reborn accounts, coverage and exact newborn exceptions,
one-unit monetary boundaries, monthly alerts with hard parent constraints,
unknown dimensions, counter/read failures and exact first-cause identities.
These tests are authored only. Compilation, focused execution and real-driver
qualification remain pending; this calculation is not a complete SQL economic
authority or a two-process admission acceptance result.
