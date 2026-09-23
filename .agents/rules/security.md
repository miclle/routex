# Security Rules

These rules apply to backend, frontend, configuration, logging, and tests.

## Secrets

- Never commit tokens, credentials, private keys, real DSNs, cookies, or production configuration.
- Example config files must use placeholders or local-only defaults.
- Do not log Authorization headers, cookies, API keys, DSNs, private keys, or request bodies that may contain secrets.
- Long-lived credentials should be hashed or encrypted as appropriate; comparisons should avoid leaking secret values.

## HTTP And Data Access

- New protected routes must have an explicit authentication and authorization plan.
- Avoid leaking whether a resource exists when the caller is not allowed to access it.
- Database queries for user-owned or tenant-owned resources must include the relevant ownership scope.
- File and proxy endpoints must guard against path traversal and arbitrary upstream access.

## Frontend

- Do not store sensitive server-issued credentials in localStorage unless the product explicitly accepts that risk.
- Display secret material only at creation or rotation time, and only when the user needs to copy it.
- Error messages should be useful but should not expose internal stack traces, SQL, provider payloads, or secret values.

## Tests

- Security-sensitive changes should cover negative cases: missing credentials, invalid credentials, expired credentials, unauthorized access, and cross-scope access.
- Time-sensitive security logic should use injectable time where practical.
- Token or secret generation should be testable without relying on predictable production randomness.
