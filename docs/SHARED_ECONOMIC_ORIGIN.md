# Shared economic calendar and origin storage

## Source-only boundary

This package defines one dormant Entity, an unregistered frozen V120 migration
candidate, structural validation, and test sources. It initializes no origin,
registers no migration, imports no local receipt, changes no previous schema or
history loader, and activates no shared runtime or monthly reader. Formatting
and source checks are not compilation, database, runtime or delivery acceptance.
Those execution gates remain pending.

The existing domain has initialized coverage and a monotonic clock but no
calendar provenance. A current frozen quota setting alone cannot prove which
calendar produced older monthly data. The separate origin record supplies the
storage boundary for a future explicit constructor without changing V119 or
making legacy domains appear to have a proven calendar.

## Frozen representation

`admission_economic_origins` contains one intended immutable record per exact
installation:

| Field | Contract |
| --- | --- |
| `installation_id` | 30-byte installation identity, primary key and restrictive economic-domain foreign key. |
| `format_version` | Exactly `1`. |
| `source_kind` | Exactly lower-case `fresh`. `local_v2` and all unknown kinds are unavailable in this format. |
| `time_zone` | Original exact spelling, at most 100 bytes. Structural validation requires a valid zone and rejects empty or host-dependent `Local`. |
| `calendar_revision_at_freeze` | Exact nonempty safe revision, at most 64 ASCII characters, including the existing initial revision `0`. This is historical provenance, not a permanent current-settings ETag requirement. |
| `coverage_start` | Explicit positive nanosecond instant, with seven days of representable headroom. |
| `clock_high_water_at_freeze` | Explicit original clock, equal to coverage start for the only supported fresh source. It never follows the mutable domain clock. |
| `boundary_at` | Explicit initialization boundary, also equal to fresh coverage start. |
| `evidence_digest` | Lower-case SHA-256 of the original canonical evidence bytes. |
| `evidence_body` | One nonempty canonical JSON body, at most 128 KiB, containing all eight version/identity/source/calendar/time coordinates above. |

The private `validateSharedEconomicOrigin` helper validates that representation
and the exact digest/body/row agreement. It performs no storage reads and issues
no initialization, import, authorization or dispatch proof. It preserves exact
nanoseconds without microsecond truncation. Equivalent time-zone aliases remain
different recorded spellings and produce different evidence digests.

The table enforces bounded physical fields, exact fresh source spelling,
positive/equal fresh time coordinates and a restrictive reference. SQL does not
validate IANA zone semantics, canonical JSON, safe revision syntax, or digest
content. The private validator supplies those structural checks. The MySQL
foreign key uses its frozen predecessor's VARCHAR collation; it is a referential
constraint, not independent proof of exact original bytes. A future constructor
must read and compare the canonical installation and exact domain identity in
its transaction. A case-insensitive matching row cannot establish that proof.

No update or deletion API is provided. The intended record is immutable, but
neither CHECK constraints nor a plain digest prevent a database writer from
changing every field coherently. A valid body/digest is not source authenticity,
an imported-history census, evidence that workers stopped, or an activation
receipt. This body deliberately contains no fabricated quiescence declarations.

The migration owns only the new table. Its FK target is a private frozen
ID-only stub, never an additional migration owner. It checks the existing frozen
V119 domain shape, repairs missing columns only in an empty interrupted table,
and rejects populated missing columns or incompatible physical representations.
It uses GORM migration APIs and the existing bounded schema-validation helpers.
The migration runner must retain its same-connection startup lock; MySQL DDL is
not assumed to roll back transactionally. No provenance row is seeded, including
when a previous economic domain is already initialized.

## Required future constructor

The next constructor must establish actual fresh-installation facts before
generating a record. An empty file or empty SQL economic table is insufficient.
It must verify the exact installation, no prior initialized local/shared mode,
no previous origin, domain/history, retained owners or calls, and no preexisting
local journal. A provisional immutable installation ID may be retained, but it
cannot conceal a previously initialized installation or replaced journal.

All old local execution and future startup must be excluded at the real
deployment boundary. Compatible startups intersect the SQL installation/mode
barrier; an already running local worker is not stopped by changing a SQL row.
Old binaries that do not honor that barrier require actual verified shutdown.
An expired heartbeat, a higher fence, an empty/replaced journal, or absence from
a bounded process list cannot prove executor closure or complete quiescence.
The constructor must fail closed until that exclusion is established. This
storage package defines no such shutdown, census or startup protocol.

After any required process-local writer barriers, transaction lock order is:

1. Governance, acquired by the caller in one bounded real SQL transaction.
2. Installation binding and current/absent mode, with genuine fresh preconditions.
3. Current quota-setting row and any previously ordered policy/pricing/route locks
   required by that operation.
4. Economic domain, then origin record.
5. Existing historical receipt and sorted exact account/contribution locks, if
   a later operation needs them.

The future constructor must atomically freeze the reviewed quota setting and
write matching domain/origin facts. It must not acquire governance or the quota
setting after taking the domain lock. Absent-origin insertion is serialized by
the preceding existing installation/domain barriers, not by a portable promise
that selecting a missing row locks a gap.

Identical retries must reconcile the exact original body, digest, installation,
boundary and freeze revision. They must never generate a new clock/boundary,
overwrite an origin or reinterpret a lost response as a fresh initialization.
A different existing record is a conflict. An uncertain commit permits only
explicit exact-intent reconciliation, never runtime activation on assumption.
Proof can be published only after the owning transaction commits successfully.

## Legacy history and later readers

A domain without an origin remains unproven for future monthly reads and new
live shared operations. Migration must not backfill its origin from the current
quota setting. Fresh coverage begins at the explicit boundary and never asserts
an earlier empty history. Windows that predate coverage retain the established
coverage/account-birth rules; unknown birth is not replaced by the boundary.

Future monthly readers must calculate bounds from the immutable origin zone,
then independently validate current exact installation/mode and the current
recorded calendar freeze. A same-zone save may change the current settings ETag;
that does not rewrite `calendar_revision_at_freeze`. Current reviewed operations
keep their separate current ETag checks. Historical settlement continues using
the accepted receipt's original bounds independently of current configuration.
The generic domain/history loader must not be changed to require this new row
for retaining historical terminal evidence.

Local v2 metadata records only its time zone and coverage start, not a historical
SQL calendar ETag. Its receipt also lacks shared owner/fence, captured current
authorization and reservation-manifest evidence. Its unknown legacy account
births cannot become invented known dates. A future importer needs an explicitly
supported original-local-receipt representation, complete semantic validation,
retained original month bounds and a verified stable/quiescent source. It cannot
manufacture live shared receipts or use asynchronous Call Records as an economic
source. `local_v2`, local v1 and unknown histories are rejected by this foundation.
Time-zone-rule mismatches must fail closed rather than recalculate old facts.

## Pending verification

Unit source covers frozen/current scalar and FK shapes, no defaults or public
DTO, unregistered migration status, canonical row/body/digest matching, exact
zone/revision spelling, safe initial revision, nanosecond bounds, unsupported
sources, malformed/oversized/duplicate/unknown JSON and coherent digest tampering.

`TestAdmissionEconomicOriginV120Integration` requires one initially empty,
exclusively owned `routex_test` database per driver:

```text
ROUTEX_TEST_ECONOMIC_ORIGIN_SCHEMA=1
ROUTEX_TEST_ECONOMIC_ORIGIN_SCHEMA_DRIVER=postgres|mysql
ROUTEX_TEST_ECONOMIC_ORIGIN_SCHEMA_DSN=<owned disposable database>
ROUTEX_TEST_ECONOMIC_ORIGIN_SCHEMA_OWNED=1
```

The dedicated worker explicitly applies the separate dormant V119 predecessor,
then this one table. It runs outside the complete matrix and writes no numbered
V119/V120 ledger entries. Its same-connection lock mirrors the production
migration runner; advisory-lock SQL remains test-only in the database layer
because GORM has no portable session advisory-lock API.

Pending scenarios include absent-predecessor refusal without creation; empty
creation/repeat without seeds; empty interrupted guarded/unguarded column and FK
repair; existing released data and initialized V119 history without inferred
origins; populated exact repeat; two-session serialized application; fresh time,
source, body and FK constraints; incompatible existing column rejection; and
populated missing-body refusal without synthesis. Both real drivers, mandatory
project checks and later constructor/runtime acceptance must pass independently
before this foundation can be described as delivered or active.
