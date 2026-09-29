# System status

The System Status workspace reports authoritative RouteX process registrations
and actual background work. It is an operational view of the current deployment,
not a synthetic dashboard. Unknown resource values stay unknown, stopped or
expired registrations stay distinguishable from current processes, and only real
RouteX workers can create job rows.

## Authority

`system.read` grants the combined read workspace and both list endpoints.
`system.write` grants offline-registration cleanup but does not grant the read
workspace by itself. The interface queries data only after `system.read` is
confirmed and hides cleanup without `system.write`. The server repeats every
permission check in the same database transaction as the protected operation.

The administrative API is:

| Endpoint | Permission | Contract |
|---|---|---|
| `GET /api/v1/admin/system/instances` | `system.read` | Bounded process registrations, server observation time, and freshness thresholds |
| `GET /api/v1/admin/system/jobs` | `system.read` | Bounded current and recent runs for actual RouteX background workers |
| `POST /api/v1/admin/system/instances/cleanup` | `system.write` | Revision-checked retirement of the reviewed offline registrations |

Management writes require same-origin and CSRF validation. Unknown JSON fields,
empty candidate lists, duplicate candidates, and unsafe identifiers are rejected.
Responses never expose database connection strings,
credentials, local paths, request content, or arbitrary internal errors.

## Process registrations

Every process start creates a new `ins_` generation. A restart never assumes the
identity of the prior process. Each registration records a bounded display name
and hostname, the truthful `combined` role of the RouteX monolith, build commit
and build time, Go version, operating system and architecture, start time, latest
heartbeat, monotonic heartbeat revision, optional graceful-stop time, and
nullable measured resource facts.

RouteX has no leader election in this architecture, so the API never labels a
process primary, leader, or worker. Online state comes from an unexpired
server-side database lease. The client renders the returned state and does not
infer it from its own clock. A database heartbeat failure does not fabricate a
new timestamp or resource sample. Resource collection failure leaves the affected
value `null`; the interface renders an em dash rather than zero.

CPU, memory, and storage values identify their measurement scope. Percentage is
returned only when both a meaningful used value and authoritative finite total
are available. Storage corresponds to the filesystem that holds the durable call
journal. Offline values are the last reported sample and are visibly stale.

## Offline cleanup

Cleanup operates on the exact registrations reviewed in the confirmation dialog.
Each candidate includes its process-generation ID and heartbeat revision. The
interface reviews at most 100 eligible registrations per operation and reports
how many remain for a later batch. The service locks and rechecks every row in
one transaction. It rejects the whole
request when any candidate is missing, current, unexpired, too recent, already
retired, or changed since review. It also protects the serving instance even if
its lease appears stale.

A heartbeat committed before cleanup changes the revision and produces a
conflict. The transaction never commits a partial candidate set. Successful
cleanup retires registrations and records one allowlisted audit event per exact
retired process-generation ID and reviewed heartbeat revision. A process that
resumes after retirement can recover its own registration on its next valid
lease-token heartbeat. A response-loss or
`5xx` result remains uncertain even after the client refetches the bounded list.
A normal response must contain the exact reviewed IDs and count, and the client
must then refetch authoritative state before reporting success.

## System jobs

System jobs describe only real RouteX work: runtime publication, durable call
delivery, and storage cleanup. Static demonstration jobs are not part of the
product contract. Each bounded row has a stable `job_` run ID, stable type and
detail codes, status, nullable progress, executor process-generation ID, and
start/update/finish timestamps. The server returns codes and interpolation data;
the selected UI language supplies all human-readable labels.

Statuses are `running`, `completed`, and `failed`. Progress is present only when
the worker has an authoritative finite denominator. A worker crash or expired
executor never leaves a row presented as actively running indefinitely;
list-time reconciliation marks the durable run failed with the safe
`executor_lost` detail code. Bounded retention keeps recent evidence without
turning the table into a general log store.

## Interface

`/admin/system-status` is the first item under System administration. It retains
the existing application shell and page bar. The body follows the approved
composition with an Instances card followed by a System jobs card.

The Instances table columns are Instance, Status, Role, CPU, Memory, Storage,
Version, Runtime, Started, and Last heartbeat. The card shows the online count,
on-demand refresh, an offline warning, and cleanup only when eligible rows exist.
Resource percentages use compact accessible circular indicators; unknown values
remain explicit.

The System jobs table columns are Job, Status, Progress, Executor instance,
Updated, and Details. It has its own refresh and in-progress count. Each card has
independent loading, error, retry, and empty states and retains prior rows while a
refresh is pending or fails. All timestamps use the selected locale. Names,
hostnames, versions, runtime identifiers, IDs, and unknown stable codes remain
verbatim.

Cleanup uses the local Base UI dialog. It reviews the exact offline candidates,
states that online instances cannot be removed, warns about recovery, prevents
duplicate submission, and never removes rows optimistically. The service locks
and rechecks every row in one transaction and rejects the whole request when any
candidate is no longer eligible. A conflict refetches the list and requires a
new review.

## Verification boundary

Controlled acceptance covers fresh, upgrade, repeat, concurrent, and partial-DDL
migration behavior on PostgreSQL and MySQL; independent process generations;
heartbeat renewal and expiry; graceful stop and restart; nullable resource
collection; read/write permission separation; CSRF and strict JSON; current/live
protection; all-or-nothing cleanup under a heartbeat race; audit evidence; actual
job lifecycle and restart reconciliation; bilingual UI behavior; accessibility;
and the production SPA deep link.

These checks do not establish production clock synchronization, multi-node
capacity, backup/restore, or external infrastructure acceptance. Online and
cleanup thresholds are operational policy and must remain server-owned.
