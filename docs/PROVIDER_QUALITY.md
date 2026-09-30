# Provider quality

RouteX derives Provider quality from immutable upstream attempt facts. The
quality workspace does not join historical attempts back to the mutable
catalog, infer a Provider from the final logical call, or invent health from
browser timing.

## Attempt facts and metrics

Frozen migration 30 extends `call_attempts` with the Provider, Connection, and
upstream model names captured when the attempt ran, plus the complete attempt
duration in milliseconds. Existing rows keep blank Provider snapshots and a
null duration. They remain valid historical facts but are never reassigned to a
current Provider.

`GET /api/v1/admin/providers/{provider_id}/quality` returns a complete, bounded
view for the Provider's saved policy window. Providers without a saved policy
use the same 60-minute default returned by the policy API. It requires
`providers.read` and reports:

- `requests`: every attempt attributed to the selected Provider.
- `eligible_attempts`: attempts that reached the upstream outcome population,
  excluding `not_sent`, canceled, and credential-rejected attempts.
- `excluded_attempts`: `not_sent` and canceled attempts.
- `credential_rejected_attempts`: credential rejection reported separately
  from service quality.
- `successes` and `success_rate_bps`: successful eligible attempts and their
  exact basis-point rate.
- `rate_limited_attempts` and `server_error_attempts`: eligible HTTP 429 and
  HTTP 5xx outcomes.
- `p95_duration_ms`: nearest-rank P95 of known complete eligible attempt
  durations. It is not time to first byte.
- `known_duration_attempts` and `unknown_duration_attempts`: explicit duration
  coverage.
- `unknown_attribution_attempts`: platform-wide attempts in the same window
  whose historical Provider snapshot is unavailable. These attempts are not
  assigned to any Provider denominator.

`data_through` and `latest_completed_at` identify the newest persisted attempt
for the selected Provider and remain null when none exists. `may_lag` is true
only when the latest durable call-record delivery job is failed. The service
rejects a query that would need to inspect more than 100,000 selected plus
unknown-attribution attempts instead of returning an incomplete aggregate.

## Threshold policies

Each Provider can have one revisioned policy:

- enabled or disabled;
- a 5-to-1,440-minute UTC window;
- a minimum eligible-attempt count from 1 to 100,000;
- a minimum success rate from 0 to 10,000 basis points; and
- an optional maximum P95 duration from 1 to 3,600,000 milliseconds.

`GET /api/v1/admin/providers/{provider_id}/quality-policy` requires
`system.read`. A Provider without a saved policy receives disabled defaults and
ETag `0`. `PUT` on the same path requires `system.write`, CSRF and same-origin
validation, strict JSON, an exact ETag, and a non-empty change reason. Every
successful write records `provider_quality.policy.update` in the audit log.

Every successful policy revision closes the previous incident generation and
resets its evaluation state before the new ETag becomes active. Disabling a
policy therefore resolves an open quality alert; re-enabling or changing an
enabled policy allows a later breach under the new ETag to open one new
occurrence.

The evaluator closes aligned UTC windows after a five-minute grace period. It
locks and rechecks the enabled policy and exact ETag before it persists a
window, so a concurrent revision cannot publish stale thresholds. A
window is `insufficient_data` until the minimum sample is present, `degraded`
when the success-rate or P95 threshold is breached, and `healthy` otherwise.
Immutable windows keep the policy ETag and Provider name used for that
evaluation. `(provider_id, window_end)` prevents duplicate evaluations during
restarts or concurrent reconciliation, while
`(provider_id, policy_etag, window_end, id)` supports bounded current-generation
transition reads. Only `healthy` ends an incident; `insufficient_data` preserves
an already-open incident without creating another occurrence.

## Alerts

The first degraded window in a current incident creates one high-severity
`provider_quality_degraded` occurrence with a Provider subject. Continued bad
windows do not send repeated occurrences. A later healthy window resolves the
group and marks unread inbox projections read; a later breach reopens the same
group with a new occurrence.

Terminal logical calls with `no_candidates` or `attempt_budget_exhausted` also
create medium-severity `route_unavailable` occurrences. They are grouped by
model, protocol, and stop reason and carry the immutable Model subject. Source
IDs make reconciliation idempotent.

Alert, occurrence, notification, and SMTP-intent rows retain bounded subject
snapshots so the UI and delivery history can identify the affected Provider or
Model without resolving a mutable catalog record.

## Interface

Provider details default to the addressable Overview tab and preserve the
approved Overview, Connections, Credentials, Models, and Settings hierarchy.
Overview shows configuration readiness, connection facts, and real observed
quality coverage for the displayed window. Settings separates `system.read` visibility from
`system.write` controls and preserves a draft until the user explicitly reviews
a fresh ETag after a conflict.

The administration overview shows Provider quality state and P95 beside the
existing readiness facts. Quality is nullable: the first 100 Providers receive
bounded aggregate queries, while later Providers, over-limit ranges, and invalid
persisted policies return `query_budget`, `range_too_large`, or `invalid_policy`
as a fixed availability reason. The UI renders localized allowlisted copy for
those values and never displays a raw server reason. All visible and accessible copy is paired in the
English and Chinese `catalog` and `notifications` resources; English remains the
default. The implementation uses local shadcn-style primitives and Base UI
wrappers.

## Verification boundary

Unit and HTTP tests cover exact attribution, exclusion rules, P95, legacy
unknown coverage, query bounds, permissions, strict JSON, CSRF, ETag conflicts,
audit facts, evaluator transitions, recovery, re-breach, route-source dedupe,
and worker lifecycle. Migration tests cover empty creation, existing-data
upgrade, repeat execution, partial recovery, concurrent startup, indexes,
constraints, and PostgreSQL/MySQL behavior.

Controlled tests do not claim real Provider availability or production latency.
Those environment-backed checks remain part of release acceptance.
