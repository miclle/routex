# Shared Admission Restart Recovery Qualification

Status: source-only candidate. This suite has not been compiled or executed on
PostgreSQL or MySQL. It does not enable shared admission, qualify the economic
schema migration, establish historical cutover coverage, or prove fleet behavior.

## Dedicated worker and fixture

The `real_sql_real_file_restart_recovery` group runs inside
`TestSharedEconomicAcceptSQLIntegration`, using its exclusively owned fresh
database, actual authenticated runtime fixture, and seven bounded test-only
economic tables. It uses the same opt-in environment variables and 120-second
worker context described in `SHARED_ECONOMIC_ACCEPT_TESTS.md`. A separate database
is required for each driver. No additional worker or production migration is
registered by this source slice.

Each case opens a real `eventqueue.SharedSpool` file and admits through production
SQL/live authority. Before restart recovery, it closes the file, discards the
original coordinator and live store, and reopens the file under the exact original
owner identity. Recovery receives only the historical interface of a SQL store
without live authority. No bearer, reconstructed executor, or transport permission
is created from persisted history. The missing-receipt case reserves only a local
payload and deliberately never calls SQL Accept.

## Required census

The parent group contains exactly seven child cases:

- `terminal_missing_dispatch_ack_no_transport`: SQL dispatch commits, the local
  acknowledgement fails, transport is never invoked, and the original durable
  terminal recovers without adding a forbidden post-terminal dispatch row.
- `terminal_uncertain_sql_settlement_before_commit`: the fault does not invoke
  SQL Settle; recovery applies the original terminal once.
- `terminal_uncertain_sql_settlement_after_commit`: the fault invokes production
  SQL Settle and discards its acknowledgement; recovery leaves the complete
  economic snapshot unchanged while confirming local readiness.
- `revoked_key_historical_only_recovery`: committed current Key revocation cannot
  prevent historical terminal settlement through the store without live authority.
- `ordered_fallback_prefix_after_restart`: two actual SQL dispatch barriers retain
  their exact ordered selected-attempt bodies and original no-work checkpoint.
- `pending_without_terminal_retains_executor_and_unknown_holds`: restart does not
  invent closure, release concurrency, settle usage, or make the record deliverable.
- `missing_sql_receipt_preserves_local_reservation`: explicit missing SQL history
  leaves all economic tables and the original local payload unchanged.

Both settlement fault seams wrap the production SQL operation. They do not use
synthetic successful receipts or patch runtime authorization maps. Dispatch
callbacks count test-only invocations; no upstream HTTP request or paid inference
is performed.

## Evidence requirements

Every positive recovery compares original admission, ordered attempt, dispatch,
checkpoint and terminal bodies across real file close/reopen. Terminal assertions
retain unknown usage separately from the finite original maximum, release the
logical executor once, retain one RPM contribution, and compare exact SQL admission
and terminal bytes. Two additional exact recovery passes leave every field of all
seven economic row sets and local readiness unchanged.

Driver execution, complete emitted test-name census, source immutability checks,
process/database closure evidence, and the mandatory project checks remain pending.
