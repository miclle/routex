# Historical Gateway source capture

The publisher prepares one private, complete historical configuration capture
after a successful local publication. It uses the original repeatable-read
source and canonical codec v1. This is an in-memory handoff for later encrypted
archive work; it performs no archive write, activation, rollback, public archive
API operation or fleet acknowledgement. F30 remains partial.

## Source and observation identity

A source ticket is admitted only after the publisher's final source mutation
and successful authorization and validated-route publication. Exact route,
authorization, success-status and publication-epoch identities must match;
recorded status metadata must agree with route birth, snapshot and authorization
lease. An unknown authorization projection or expired authorization rejects the
ticket. Fresh authorization beside retained older routes after route failure
never creates a ticket. Current denials retain their existing publication order.

The ticket owns the original loaded source for bounded post-lock preparation.
It does not reread current database rows, open secrets, retain clients or recover
historical credentials. Its snapshot ID, route publication birth and capture
time describe the earlier local observation. Canonical detached bytes and their
complete content digest survive preparation; raw source ownership ends when
that synchronous preparation returns.

The complete digest includes source facts absent from the older route digest.
An authorization-only change can therefore produce different complete content
under the same routing snapshot. Lease renewal, process observations and capture
time do not change logical content identity. Normal later refresh, expiry or
revocation does not retroactively invalidate proven historical facts. A retained
capture confers no current authorization, eligible route or installed fleet
state, and carries no invented historical validity period.

## Bounded preparation and lifetime

Preparation runs after every publisher lock is released and uses the original
refresh context. It never renews the existing installation projection/recording
deadline. One nonblocking preparation slot prevents concurrent archive clones;
there is no queue, goroutine or independent worker. A busy slot skips preparation
without delaying authorization or changing successful route publication.

A conservative original-source work preflight counts all fields, including
fields excluded by the frozen codec. It bounds raw string bytes and traversal
work before cloning, at 16 MiB and 262,144 entries with maximum depth 64. A source
that fits the codec may exceed this separate conservative preparation bound.
Cloning and canonical sorting operate only on a preflight-bounded source;
context checks before and after those operations prevent a cancelled result
from being published. The independent codec still enforces complete document,
JSON entry and encoded-byte bounds. No partial capture is stored on failure.

Only one latest completed historical capture is retained privately. Monotonic
ticket order prevents a late earlier preparation from replacing newer completed
history. Shutdown clears the retained source behind a separate short publication
gate and rejects late preparation. Cancellation, oversized source, invalid codec
source, contention or shutdown changes no live authorization, route or denial.
There is no database lookup or secret-store access in this preparation path.

## Bootstrap network policy fingerprint v1

A domain-separated canonical fingerprint covers private-upstream,
private-egress and private-storage policy, plus trusted proxy prefixes that
affect caller-IP admission. Prefixes are masked, sorted and deduplicated;
IPv4 and IPv6 remain distinct. Invalid prefixes and more than 128 configured
prefixes reject preparation. Missing and empty proxy lists mean the same policy.

SMTP policy, credential roots, clients and transports, resolved DNS addresses,
database driver/address, local process identity and publisher cadence are
excluded. The fingerprint is a bootstrap-policy observation, not evidence that
an old configuration can currently execute.

## Validation and remaining work

Focused race coverage exercises exact source/status identities, detached complete
reconstruction, excluded raw secrets, unchanged live publication, real refresh
success and route failure, source changes under an unchanged route digest, lease
renewal, out-of-order historical preparation, cancellation before/during/after
preparation, conservative bounds, busy preparation/publication, shutdown and
bootstrap fingerprint normalization. Existing runtime publication and denial
tests remain applicable. This slice changes no schema or persistence contract.

Primary focused race and complete Task tests passed, including 7,326 frontend
cases in 271 files, Go race/coverage, script controls and production assets.
Mandatory checking passed after narrowly documenting the deliberate private-JSON
privacy assertion for both Staticcheck entry points. The earlier failed checks
remain failures. Independent source review found no concrete defect.

Automatic durable capture still requires an approved archive retention/count/
encrypted-byte budget, serialized deduplication/backpressure and historical
capture provenance. The encrypted archive foundation remains separately
documented in [the archive contract](RUNTIME_CONFIGURATION_ARCHIVE.md). Reviewed
current-authority rollback and fleet acknowledgement remain subsequent work.
