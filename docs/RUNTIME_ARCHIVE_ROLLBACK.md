# Gateway routing archive rollback

Commit `14a75fe` delivers reviewed routing-configuration rollback. The containing
phase passes mandatory checks, ordinary tests, authentication restart and complete
PostgreSQL/MySQL regression, including the exclusive archive worker. Browser,
genuine rollback-process restart and deployment acceptance remain separate.

An intrinsic administrator with current Session authority may review a retained
archive through `GET /api/v1/admin/runtime/archives/:archive_id/rollback`.
Independent `system.read`, `providers.read`, and `models.read_all` permissions
are required. The response contains only bounded current-versus-desired routing
fields and localized-consumer blocker codes; its quoted strong ETag belongs to
that exact actor birth, archive birth and authenticated content, topology, current
revisions and review.
An absent write permission permits review but makes `can_apply` false.

An explicit `POST` to the same route requires all three independent write
permissions (`system.write`, `providers.write`, `models.write`), fresh Session
and CSRF, the reviewed `If-Match`, and exactly `request_id` (UUIDv4) and `reason`.
The existing reason grammar allows at most 1,000 Unicode characters and 4,000
UTF-8 bytes, without control characters. No query parameters, free-form source,
secret material or browser-derived configuration are accepted.

Only these fields can change:

- Connection base URL, adapter, API version, egress mode and egress ID.
  Protocol remains current and any historical difference blocks restoration,
  including an empty disabled Connection. Existing discovered topology also
  keeps adapter locked. Changed transport requires a currently disabled Connection and receives fresh
  transport and metadata revisions. Current egress defaults, proxy settings,
  enablement and authentication remain authoritative; a named egress must still
  have the exact retained identity and birth.
- Explicit Provider-model image/PDF input declarations. Current disabled flags,
  upstream names, relationships and lifecycle remain authoritative. On changed
  transport, old capability, discovery and capacity proofs remain stale. A fresh
  actual Verify, declaration review and separate explicit Enable are required
  before eligibility. The operation performs no verification network request.
- Existing binding weights, as complete integer 0–100 sets totaling 100 per
  protocol for each changed Model. The Model must currently be active. Positive
  unchanged routes retain current readiness checks. Changed disabled transport
  may retain its existing weights, but cannot receive a new positive assignment
  through this operation.

Provider metadata and enabled flags, Model names/status, Connection names and
availability, Credentials, identities, Keys, Sessions, grants, quotas,
revocations, prices and secrets are never restored. Providers have no transport
columns of their own. Exact IDs, births and relationships must match the entire
bounded current routing topology (at most 1,000 rows per relation). Deleted,
reborn, extra or rebound resources require a new independent configuration
workflow; they are never recreated by an archive. Terminal Models remain
read-only. Already dispatched calls keep their immutable original attempts.

Frozen migration V116 adds a durable original-intent row with restrictive
archive and publication-claim foreign keys. The allowed SQL changes, exact
UUID/reason/review, archive reference, publication claim and audit commit
atomically. Complete-set weight history retains its original before/after
editor journal; the distinct archive command and audit identify the archive
rollback without fabricating a weight-version target. Cleanup review and
confirmation explicitly reject archives protected by rollback history, and the
foreign key also prevents out-of-band deletion.

Only a positively acknowledged new commit can dispatch one local refresh.
An unknown commit never dispatches; a later identical retry reads retained
history and never reapplies SQL or resumes the original refresh. Changing the
actor birth, archive, reviewed ETag or reason under the same UUID conflicts.
`GET /api/v1/admin/runtime/archive-rollbacks/:request_id` requires fresh current
read authority for the exact original actor. The receipt separates committed
configuration facts, a non-null original publication receipt, and fresh
`runtime_applied` / `superseded` facts. Publication status is `unknown`, `pending`,
`refresh_returned_nil` or `refresh_returned_error`. `pending` requires a matching
live local ticket; it is neither durable completion nor current application
proof. A superseding configuration is not undone
by an old request. The [V117 acknowledgement source candidate](RUNTIME_PUBLICATION_ACKNOWLEDGEMENT.md)
adds nullable command-bound historical local installation evidence separately
from original publication outcome and fresh application facts. Its qualification
is pending. The receipt does not establish current route health, automatic
verification, distributed acknowledgement or fleet rollback.

Each write, dispatch and final receipt phase is bounded to three seconds;
publication admission is nonblocking. The maintained exclusive PostgreSQL/MySQL
archive worker owns migration, current-authority, atomicity, unknown-commit,
replay, supersession, restrictive-reference and disabled-transport controls.
These controls pass on both databases in the containing phase. They establish
the tested local contract, without proving fleet rollback or route health.
