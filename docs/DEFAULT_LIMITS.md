# User and Team Default Limits

Defaults are durable creation templates for Personal User accounts and Team
aggregate accounts. They contain rolling five-hour/seven-day Tokens, monthly
Tokens/money, TPM, RPM and concurrency. A null cap is unlimited; zero is a real
cap. Money remains an exact decimal string with an explicit platform currency.
There are no Project, Key or Team-member defaults, IP templates, alert thresholds
or implicit model grants in this package.

## Creation and existing accounts

First administrator setup, local registration, administrator-created members and
new Teams copy the current applicable rule into an ordinary `resource_limits` row
inside the creation transaction. Relationship creation, copied policy and audit
commit together. A missing, invalid or incompatible template rolls back creation.
The runtime consumes the saved policy, never a live template fallback.

Changing a template affects later creations only. Existing accounts, overrides,
Keys, Team-member policies and usage remain unchanged. Schema V42 seeds both rules
unlimited and leaves historical resource policies untouched. Copied policies retain
`applied_default_etag` provenance; ordinary direct policy writes clear it and the
last reset review marker.

## Settings API and interface

All API paths are relative to `/api/v1`. Sessions, same-origin checks and current
CSRF tokens protect writes. Both target kinds must use the exact lowercase value.

| Method and path | Authority and behavior |
| --- | --- |
| `GET /admin/default-limits/:kind` | `system.read` or `limits.settings.write`; one coherent rule/currency snapshot |
| `PUT /admin/default-limits/:kind` | `limits.settings.write`; complete seven-field `policy` plus `currency` and a required `reason`, with strong reviewed `If-Match` |
| `GET /admin/members/:user_id/limits/default-reset` | Exact current User, `members.read` or `limits.users.write`; independently authorized review with server-owned editability |
| `POST /admin/members/:user_id/limits/default-reset` | `limits.users.write`; required `reason` and strong reviewed reset-context `If-Match` |
| `GET /teams/:team_id/limits/default-reset` | Independently authorized Team policy reader; server-owned editability |
| `POST /teams/:team_id/limits/default-reset` | All three `teams.tokens.write`, `teams.money.write` and `teams.rates.write`; active exact Team and reviewed context |

A rule response contains `kind`, `rule_etag`, representation `etag`, `policy`,
`platform_currency`, `editable` and `updated_at`. The representation validator
binds the current rule and pricing/currency generation. GETs use read-only
repeatable-read snapshots. Writers reauthorize exact enabled actors and direct
permission associations inside the governance transaction. Unknown fields,
duplicate JSON keys, omitted nullable fields, reasons over 1024 UTF-8 bytes or
containing control characters, fractional/out-of-range integers,
numeric money, exponents, invalid currencies and unsupported targets are rejected.

The `/admin/limits` workspace retains User/Team tabs and grouped Budget, Tokens and
Rate limits rows with inline Edit/Cancel/Save. Its navigation and read access use
the same any-of permission boundary; writes remain independent. Local shadcn/ui
and Base UI components supply existing controls. English is the default, and paired
`defaultLimits` translations update drafts and notices when switching language.
Rule-save success confirms persistence and future creation behavior; it does not
claim a retroactive change or runtime enforcement.

## Explicit restore

Existing User and Team aggregate limit interfaces offer Restore defaults only
with the relevant write authority. A server-derived review context contains the
current `limit`, `default_rule`, exact target, current provenance, `editable` and
a strong `etag`. Its validator binds target lifecycle, stored policy and IP,
policy revision, current default generation and current pricing/currency generation.
A Base UI confirmation requires a reason and submits that exact reviewed context.

Restore replaces only the seven supported caps. User IP rules remain exactly
conjunctive with Key restrictions. It never resets quota usage, holds, coverage,
calendar, resource birth time, relationships or Key identity/history. Zero and null
remain distinct. Current target/default/currency changes require explicit review
while retaining the draft; unavailable or revoked targets hide old private data.

A successful response separates `saved`, `applied_default_etag`,
`default_reset_etag`, the current `limit` and `runtime_applied`. Exact current
runtime revision, normalized policy, valid lease, currency and revocation state
must confirm enforcement. A saved policy with pending application is displayed
as pending. Unknown quota coverage remains unknown and native admission retains
its existing fail-closed behavior.

A failed publication can follow a committed reset. Retry only the original target,
review token and reason. The saved last-intent marker reconciles the original
copied rule even if defaults have since changed, without another audit or reset.
A later ordinary write clears that marker; an old retry then returns conflict and
cannot overwrite the newer policy. A rejected retry does not resolve the original
uncertainty. Current-target reconciliation is not a permanent historical receipt.

## Currency, audit and migrations

Configured finite default money joins the existing denomination-change guard.
Changing platform denomination requires explicitly clearing both default money
caps and satisfying existing finite-policy/hold restrictions. No amount is silently
converted or reinterpreted. Creation and restore reject contradictory stored money
currency before changing the resource.

Typed audit facts use `limits.defaults.update`, `limits.default.apply` and
`limits.default.reset`. A reset creates one event; an identical retry creates none.
The read-only audit workspace exposes only validated, bounded known fields.
Missing historical policy facts remain unknown; arbitrary JSON and secrets are
never projected. Reset audit preserves the recorded IP policy.

Frozen GORM V42 defines `default_limit_rules` and the two nullable provenance
columns. Explicit new column tags keep acronym/digit names consistent across
models, migrations and constraints. The migration reconciles checks and seeds
idempotently, including partially applied MySQL DDL. Existing numbered migrations
are unchanged. See [Database policy](DATABASE.md), [resource limits](QUOTAS.md) and
[Team limits](TEAM_LIMITS.md) for existing storage, admission and policy boundaries.

## Acceptance status

Full check/test passed with 1036 frontend cases in 72 files, Go race/unit,
development lifecycle and embedded production assets. Corrected PostgreSQL/MySQL
focus passed in 243.032 seconds, including frozen V42 creation, upgrade,
repeat/concurrent startup, partially applied schema reconciliation, constraints,
creation snapshots, permissions, exact money, reset conflicts, uncertain retries,
and real Personal/Team quota admission.

Controlled production/browser acceptance used an owned isolated PostgreSQL
instance and native local upstream. Browser saving monthly Tokens 5 was copied
into a subsequently created User. After settling 5, the next call returned 429
without dispatch. Changing the default to 10 left the existing policy unchanged.
A reviewed browser reset preserved the exact IP allowlist and settled 5, confirmed
runtime application, permitted only the remaining 5 and rejected the next call.
Rule and reset provenance, Sessions, policies and settled usage survived process
restart. English/Chinese switching passed, English was restored, browser error
logs were empty and owned temporary processes/Compose resources were removed.

Both real-process PostgreSQL/MySQL authentication and native-call lifecycles
passed, including restart persistence and revocation. Final main full database
regression found two legacy audit-count/retry-revision fixture assumptions, now
adapted without removing the new audit event or weakening retry guards. Corrected default/audit/quota focus passed on both engines in 246.02 seconds.
The final complete PostgreSQL/MySQL matrix passed (Handler 808.330 seconds;
Service 6.035 seconds), including frozen migration creation, upgrade, repeated
execution, concurrent startup and constraint checks. A supplemental
real-database assertion passed both engines in 274.847 seconds and checks
that a finite default budget blocks denomination changes before any account
copies it and leaves the exact amount and currency generation unchanged. Initial fixture failures were corrected without
weakening production policy: explicit frozen column tags preserve constraints,
native Chat uses max_completion_tokens and finite accounts are created only after
real journal coverage starts.

Broader F17 alerts, configurable stop policy, named templates, distributed
enforcement and production capacity acceptance remain open.

## Transient Session-error recovery

A dispatched Restore request retains only its reviewed non-secret target, reason,
If-Match and historical default context in the private AuthGate lifetime boundary.
A temporary same-actor Session read failure unmounts private resource data and
controls. Manual recovery requires a fresh real Session, permissions and target
review; it never automatically posts. Explicit retry uses the original request
with the current CSRF token, even when current defaults differ. Rejected retries
retain uncertainty. Logout, definitive expiry, actor/resource/tab changes and
explicit abandonment clear the intent; no credentials, Session tokens or
private query snapshots enter browser storage or mutation caches.

Isolated source checking, complete Task/build and controlled PostgreSQL bilingual
browser/restart acceptance passed. Seven actual reset requests included two real
Member conflicts, reviewed restoration, a labeled observer pre-forward412,
committed200 withheld as503 and real Session200 withheld as500. Private DOM and
actions disappeared during that gate error. Manual recovery retained the reviewed
Team default200 while current defaults were201; original Sessions and the exact
request survived process restart, with explicit retry200 and no extra audit.
Exact money20.000000000000000001 USD, Member IP and independent child Tokens77
remained intact. Project and Team-member restore controls stayed absent.

There were zero native Calls/Attempts. Owned resources were independently absent;
earlier fixture/module preparations remain failed. Source/build and browser
artifact identities are separate from later main composition checks. A second
UI tab proved post-restart Session/permission/Team reads; that observation does
not prove the original React Query callback generation. MySQL browser acceptance
is not claimed. Backend/schema/runtime publication contracts are unchanged.

Main composition on checked Member warnings passed mandatory checking, all
3,407 frontend cases/153 files and four Node checks, production asset race
tests and a rebuilt embedded executable. All16 accepted UI afterimages remain
exact; the Member backend/schema is unchanged. The prior controlled browser
artifact remains separately identified from this later composed build. The
containing commit records this UI recovery phase.

## Reviewed Team creation

Team creation copies the explicitly reviewed current Team defaults within the
creation transaction. The form displays only fields the current actor may read
and override; token, money and rate authority stay independent. Sparse omitted,
null and zero values remain distinct, and money strings retain exact precision.
A changed default/context generation requires explicit review before a new
creation intent. Reconciliation of an already dispatched intent uses its original
reviewed request, not a rewritten current default. Historical creation commit
and renewed runtime application are separate confirmations. See
[Team creation](RESOURCES.md#team-creation-and-initial-limits).

Corrected R9 passed all ten selected PostgreSQL/MySQL cases, with exact
source/index and independently verified cleanup. Unfiltered full115 then passed
115 ordered scenarios per database, eight constraints and 3,963 named RUN/PASS
events on the exact source/index. Controlled PostgreSQL bilingual production
and original-Session restart also passed: stale generation conflicts preserve
the original request, explicit abandonment permits fresh review, and a lost
committed response reconciles by exact retry after defaults change. Prior focused failures remain
failed. Existing Restore and monthly-warning contracts are preserved by this
phase.
