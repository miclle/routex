# Shared quota calculation primitives

`pkg/eventqueue/quota_rules.go` provides storage-independent validation, exact
money/token arithmetic, calendar-month calculation and ordered policy decisions.
The local queue uses the same implementations; its bbolt reads, error ordering,
reservation, settlement and startup recovery remain unchanged.

The exported boundary validates standalone inputs. Missing bound or settlement
dimensions remain unknown. Recovery checks validate both representations before
comparing known dimensions; a structurally valid bound is not current policy,
price, authorization, coverage or capacity proof.

Money amounts retain the existing unsigned 60-integer/18-fractional decimal
contract. Accumulators are canonical nonnegative integer strings in units of
10^-18 and may grow beyond one amount's size. Updates return the original
accumulator on failure; no floating-point or narrower pricing amount type is
used. Token updates reject overflow and underflow. Calendar calculation uses an
explicit instant and timezone, including daylight-saving month boundaries;
it neither reads the current clock nor establishes accounting coverage.

`NewQuotaPolicyRules` captures copies of every policy/bound pointer and explicit
accounting coordinates. `Check` consumes a validated active counter and lazy,
read-only window/invalid-bound accessors from one coherent transaction. Token
windows retain minute, five-hour, seven-day and calendar-month order, followed by
money and invalid-bound checks. Token and money constraints read the month
separately; no eager reads or counter mutation occur. `QuotaCounter` preserves
existing JSON and integer-unit money fields; it is not the decimal display DTO.

Local admission retains policy validation, exact birth checking/persistence and
active-counter reading before the shared decision. The standalone constructor
also validates its bound; local callers retain their prior bound validation
order. Alert-only monthly policies bypass their own ceiling only, preserving
coverage, unknown, currency and bound checks and every other parent/window.
Callers remain responsible for current authority, exact account births, calendar
validity, counter validation/locking and atomic reservation across all accounts.
Passing a calculation does not establish coverage or grant an admission permit.

This extraction does not implement SQL economics, shared activation, executor
closure, owner takeover, historical settlement reconciliation or runtime wiring.
Local startup lease recovery is not proof that a shared executor has closed.

Authored boundary and local-queue parity tests cover precision, accumulated
amounts, invalid/overflowing deltas, independent unknown dimensions, currency
mismatch, recovery bounds, zero and calendar boundaries. Policy tests also cover
lazy rejection order, independent unknown dimensions, parent-conjunctive alerts,
immutable captured inputs, read-only counters and local transaction rollback.
Execution and coherent checks remain pending; source preparation alone is not
qualification.

## Counter contributions

`QuotaCounterDelta` reuses the original local token/money contribution arithmetic
for a validated, complete counter projection. It returns independently copied
money maps and preserves the input on any error. `ValidateQuotaCounter` checks
nonnegative token/unknown fields and canonical arbitrary-size integer-unit money
maps; accumulated totals are not narrowed to one amount's decimal precision.

An active contribution uses its original conservative bound. A terminal
contribution uses each explicitly known settlement dimension independently; a
missing terminal dimension keeps its original hold or unknown count. Explicit
zero is known usage. Economic terminal state does not prove executor closure or
native completion, and this helper does not change RPM or concurrency.

Local journal reads and writes retain their original ordering and reuse the same
private arithmetic. Future SQL adapters own coherent authority, projection
replacement and expiry in one transaction. Immutable receipts must survive
projection expiry; a counter delta alone does not provide idempotency or reserve
capacity. New independent-dimension, unknown/zero, failed-delta immutability and
large-accumulator test sources are authored; execution remains pending.
