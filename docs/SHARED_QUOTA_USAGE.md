# Shared quota usage preparation

`eventqueue.QuotaCounterUsage` provides the existing journal's exact conversion
from validated storage counters to detached public usage values. Stored money
uses arbitrary-size integer units of 10^-18; displayed money stays a decimal
string. Settled values, bounded holds and unknown counts remain independent.
Malformed data returns an error and no partial usage.

This helper opens no database or journal, expires no projection, and establishes
no account identity, accounting coverage, coherent sampling or current authority.
It is a prerequisite for the shared SQL usage reader, not that reader itself.
The existing local sampling and conversion paths are unchanged.

The SQL reader still needs an explicitly established calendar and coverage,
coherent domain and sorted account locks, bounded projection expiry, exact
retained account births and current-mode validation. Missing or uninitialized
accounting must remain unavailable rather than become known zero usage. Currency
change guards must inspect all live shared money holds independently of the
call-report projections. No shared usage or admission runtime is activated here.

Focused test source covers exact large and smallest-unit decimal values,
independent consumption/holds/unknowns, detached maps, malformed counter rejection
and the distinction between recorded zero and unknown usage. Execution remains
pending the exclusive validation lane.

The dormant live economic store also requires recorded
`QuotaSetting.AccountingStarted`, under the same retained SQL row lock as the
reviewed calendar revision/time zone. Matching mutable calendar fields alone
cannot permit shared admission or dispatch. Historical settlement retains the
original receipt calendar independently of current configuration. Focused test
source checks the freeze, changed revisions/zones, transaction boundaries and
cancellation; the real-driver Accept worker explicitly freezes its owned fixture
and adds a changed-freeze refusal that leaves all seven economic tables unchanged.

This guard is necessary but does not establish imported calendar provenance.
An explicit future constructor must atomically freeze and verify the calendar
before initializing the shared domain, including every imported monthly fact.
No constructor, import, domain activation or monthly usage reader is implemented
by this change.
