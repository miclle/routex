# Initial Shared Authorization Capture

Status: source-only, unwired candidate. Compilation, focused tests, real-driver
qualification, mandatory project checks and coherent delivery are pending. This
helper does not activate shared admission, authenticate a bearer, commit an
admission, publish routing, or establish historical coverage.

## Initial capture contract

`captureSharedAuthorizationSQL(tx, identity, now)` accepts one request-local
`gatewayIdentity` returned by existing Key, Personal Session or Team Session
authentication, with its selected Model ID. Personal and Project Keys remain
distinct principal families. A locator, resource ID or independently read durable
receipt is insufficient: the collector requires the transient authenticated
verifier and any private Session proof, then matches them against current locked
SQL rows before projecting durable authorization.

The caller owns one READ COMMITTED transaction. It must acquire governance before
any owner/fence locks, then invoke the collector inside that same transaction.
The collector reacquires governance intentionally and retains current authority
row locks until the caller closes the transaction. It checks that the supplied
handle is transactional; selecting READ COMMITTED and respecting the preceding
lock order remain caller obligations. It does not start or commit a transaction
and must not be called inside an older repeatable-read snapshot.

## Shared current facts

Initial capture and `validateSharedAuthorizationSQL` use the same current-facts
loader. The extraction preserves the validator's SQL queries, lock order,
bounded complete Personal grant set, exact identifiers, delivery acknowledgement,
Project manager continuity, current primary authentication and error behavior.
The current validator still compares every durable captured field after advancing
only evaluation time. Initial capture independently matches its transient input
and invokes the existing `newSharedCapturedAuthorization` constructor.

Key and Session token verifiers are compared in constant time and never enter
the returned projection. Key IDs, owner IDs, births, expiration and applicable
Personal lifecycle revision must match the locked row. A Personal Key's retained
grant revision is matched when the invocation already carries one; initial
authentication alone does not invent such a revision. Current SQL grants remain
required in either case.

Project runtime authentication returns common `APIKey` metadata with an empty
User ID. The collector loads the actual `ProjectKey` row, its actual Project,
current managers and exact grants. It uses the existing common metadata projection
only to compare authentication facts. It never builds a Project Key entity from
that neutral metadata, treats its creator as owner, borrows Personal grants, or
invents a Project lifecycle revision.

Personal Sessions retain the complete private neutral Session proof, User birth,
registration admission and Personal grant generation. Team Sessions retain their
complete available private primary-authentication proof, Session birth/expiry,
Team birth and exact membership ID. Current SQL supplies actual membership,
User/admission and Team grant facts that the runtime identity does not carry;
absent fields are not synthesized into a runtime proof. Every supported primary
authentication family is validated by the existing locked SQL path. Output
contains only the existing explicitly allowed durable authorization projection.

## Qualification source

The focused unit source uses the existing read-only SQL fixture, actual production
runtime authorization projection and actual runtime authentication methods. Four
principal tests compare the complete collector and validator query sequences,
exact returned projection and exclusion of private material. Additional cases
cover missing or ambiguous transient proof, nontransactional/cancelled work,
changed verifiers, births, expiry, owner, lifecycle and grant generation,
revocation, disabled resources and changed private primary-authentication fields.

`TestSharedAuthorizationInitialCaptureSQLIntegration` is a separate gated source
worker. It requires `ROUTEX_TEST_SHARED_AUTH_CAPTURE_SQL=1` and
`ROUTEX_TEST_SHARED_AUTH_CAPTURE_SQL_DRIVER=postgres` or `mysql`, using the existing
runtime archive integration connection configuration. Allocate a fresh,
exclusively owned disposable database; the worker rejects a preexisting migration
registry and has a 120-second context bound.

Its four real principal fixtures pass through actual runtime SQL loading and
authentication, then capture behind governance and current owner locks. It also
exercises committed verifier changes, retained current-row locking against a
competing revocation, and the 1,001-row Personal grant overflow. The worker neither
dispatches inference nor creates economic tables. A supervisor must still qualify
both supported drivers, preserve original failures, census named tests, verify
unchanged source and close its owned processes, databases and ports. The worker
is not yet registered in the maintained full integration runner.
