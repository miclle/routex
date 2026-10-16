# Runtime publication acknowledgement

**SOURCE CANDIDATE — qualification pending.** This document describes the V117
successor source contract. It makes no delivery, database, browser, restart,
external-service or fleet acceptance claim. The candidate depends on the V116
routing rollback and Controller composition; final delivered-source comparison
and new qualification remain required.

## Original local installation evidence

A positively committed original publication claim can dispatch one refresh.
Only that dispatch passes its exact live in-memory publication ticket into the
successful installation seam. The seam runs with publication admission and the
runtime mutex held, after actual routing and authorization publication. It does
not acknowledge a desired database configuration or an earlier routing-only
observation.

The recorder checks the same ticket and original context, exact command and actor
birth, claiming process ID and birth, current route and authorization pointers,
publication epoch, ready status, authorization lease and process lease/token.
Inside its transaction it locks the exact process and command, requires the
command still to be claimed, and finds exactly one valid V2 installation
observation for the actual routing snapshot and authorization projection.
It rechecks these conditions before and after inserting the acknowledgement and
after commit. The acknowledgement timestamp is not earlier than the original
claim or installation observation.

An unchanged routing snapshot can receive a different installation observation
when authorization changes. In particular, a new denial is not treated as an
old acknowledgement merely because the route snapshot ID is unchanged. Already
dispatched calls retain their original attempts.

## Receipt projection

The original publication receipt gains the required `acknowledgement` member.
It is explicit `null` when no recorded command-bound evidence exists. Historical
commands are not backfilled; null means historical installation is unknown,
including when the original refresh returned nil.

A non-null acknowledgement has exactly these seven fields:

| Field | Meaning |
| --- | --- |
| `scope` | Always `single_process_gateway_admission` |
| `installation_id` | Exact immutable V2 routing-and-authorization observation |
| `instance_id` | Original claiming process identity |
| `instance_started_at` | Exact original process birth, in UTC |
| `snapshot_id` | Actual installed routing snapshot |
| `routes_published_at` | That snapshot's original publication time, in UTC |
| `acknowledged_at` | Command-bound historical acknowledgement time, in UTC |

This immutable historical proof is independent from the original function
outcome (`pending`, `unknown`, `refresh_returned_nil` or
`refresh_returned_error`). It does not establish present serving state, route
health, other processes, fleet application, rollback completion everywhere or
absence of effects. An interrupted terminal receipt can remain `unknown` while
a genuinely committed local acknowledgement remains readable. Current runtime
and application observations require their separate current checks.

## Bounded failure and recovery

Projection, existing routing/application observations and acknowledgement share
one original evidence deadline of at most 250 ms, also bounded by the caller and
authorization lease. The recorder receives no renewed timeout. Evidence failure
or exhaustion cannot undo actual installation or relabel the original refresh
return; it leaves acknowledgement unknown unless a real immutable row committed.

A missing or uncertain database commit response is not positive in-memory proof.
A later authorized `GET /api/v1/admin/runtime/publication-commands/:requestID`
can recover an actually committed acknowledgement after validating its exact
command and installation relationships. A failed original claim commit never
authorizes dispatch. Receipt reads require current intrinsic-administrator,
Session and original actor/birth authority.

Ordinary/background refreshes have no original ticket and cannot synthesize
command evidence. Identical retries read retained history; they neither dispatch
again nor fill missing acknowledgement. Restart does not recreate the original
ticket, resume a claim, or manufacture a historical acknowledgement.

## Reviewed rollback consumer

The [routing archive rollback](RUNTIME_ARCHIVE_ROLLBACK.md) receipt includes the
same nullable member under `publication`. The strict client decoder requires
that member and, when non-null, exactly the seven fields, fixed scope, canonical
installation/snapshot IDs, exact claiming process/birth and valid UTC timestamp
ordering. It preserves sub-millisecond ordering during validation. Missing,
extra, malformed or contradictory evidence is rejected; the client does not
construct evidence from a matching current state.

The existing rollback confirmation dialog displays historical acknowledgement
or unknown separately from original publication outcome and current
`runtime_applied` / `superseded` facts. It retains the existing reviewed request,
permission, actor/Session and uncertainty boundaries. This additive display
performs no publication, retry, rollback or fleet action of its own.

## Frozen migration and persistence

V117 adds `runtime_publication_acknowledgements` through private frozen GORM
schema definitions and Migrator APIs. Prior migrations remain unchanged. The
request ID is the primary key; restrictive foreign keys reference the original
publication command and installation observation. The service inserts only,
without upsert, update, deletion or historical backfill.

Interrupted empty-table DDL can resume; missing columns in a nonempty partial
table are rejected. Column shape, primary keys and index shape are validated; missing declared
references are created through GORM, and retained rows are checked before frozen
check constraints are reconciled and the ledger is accepted. Historical fixtures
verify the complete empty acknowledgement overlay and exact ledger before
peeling V117 ahead of predecessor overlays; retained command evidence prevents
that peel. Real PostgreSQL/MySQL empty, upgrade, repeat, concurrent and constraint
qualification for this composed candidate remains pending.

See [Runtime](RUNTIME.md#manual-publication-command) for the existing command and
[Runtime installations](RUNTIME_INSTALLATIONS.md) for actual installation
observation boundaries.
