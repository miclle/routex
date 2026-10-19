# Bounded Vault wire profile

This package parses the complete `vault_core_response_status_code` counter
family actually observed from the isolated Vault 2.2.0 fixture. It preserves
explicit zeros and rejects missing/duplicate declarations or series, unsupported
labels, noncanonical or unsafe numbers, resets, changed coverage and every
unaccounted increment. A missing series is never replaced with zero. Returned
maps do not alias retained parsed facts.

`RequireStatusDelta` checks every observed status class against an independent
expected bracket. It does not identify an endpoint or principal, authenticate a
capture, prove isolation or establish SDK acknowledgement.

`ParseAuditDelta` reads the complete appended byte range between original audit
snapshots. No unwanted caller or row is filtered away. Token mode requires four
paired requests; AppRole mode requires five in order: metrics, login, original
version read, destroy and metrics. Version HMAC must come from the real original
audit-hash response. The source-observed GET transition adds only
`mount_accessor` to the response request object; other fields remain identical.
Duplicate/case-alias JSON keys, unknown fields/transitions, extra or unpaired
requests, foreign identity/scope/query/policy, null defaults, plaintext/unknown
versions, truncation, prefix replacement and wider clocks refuse.

Logical audit operations are not HTTP methods or numeric status receipts.
Original source/command identity, complete namespace isolation, SQL, actual
application invocation and resource closure remain independent gates. No
F28/A16 completion follows from these parsers.

Run pure controls with `go test -race ./scripts/vault-cleanup-wire`. Actual-data
gates require private canonical original files and their independently reviewed
SHA-256 values:

- `ROUTEX_TEST_VAULT_METRICS_BEFORE` and `ROUTEX_TEST_VAULT_METRICS_BEFORE_SHA256`
- `ROUTEX_TEST_VAULT_METRICS_AFTER` and `ROUTEX_TEST_VAULT_METRICS_AFTER_SHA256`
- `ROUTEX_TEST_VAULT_METRICS_READ_RESPONSES`: the independent exact number of
  completed 200 responses before the final scrape, from 1 to 3; never derive it
  from whatever counter happens to be returned.
- `ROUTEX_TEST_VAULT_AUDIT_DESCRIPTOR` and
  `ROUTEX_TEST_VAULT_AUDIT_DESCRIPTOR_SHA256`: the private original-snapshot,
  original-clock, path/policy and version-HMAC expectation descriptor.

Missing every input skips only that actual-data gate; partial inputs fail.
Digests bind selected bytes rather than authenticate their producer. Synthetic
unit fixtures are not actual capture or runtime acceptance.
