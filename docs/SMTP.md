# SMTP configuration and test delivery

RouteX persists one SMTP transport and one system sender identity. Transport and sender settings have separate forms/API writes but share a revision, so changes cannot silently overwrite one another. This implementation provides real connection verification and fixed test-message delivery. It does not yet schedule application notifications or provide a durable notification queue, automatic delivery retries, bounce processing, or inbox-delivery tracking.

## Security and persistence

Migration 20 creates `smtp_settings` and retained `smtp_tests`. The initial transport is disabled, has no hostname or credentials, and uses port 587/STARTTLS. It sends nothing until an authorized operator configures and explicitly enables a real endpoint. The sender name/email and optional reply-to address must also be saved before a test can run.

Username and password are encrypted together using the existing envelope store and an authenticated `smtp:1:<secret-generation>` reference. Reads expose only `auth_configured`. Replacement uses a fresh generation; credentials are never returned to the client or placed in SMTP results, logs, or audit details. Root-key availability and backup requirements follow [Credential Storage](SECRET_STORAGE.md).

Authentication actions are explicit: `keep` sends no new username/password, `replace` requires both, and `remove` sends neither and clears the envelope. A hostname, port, or security-mode change cannot keep an existing credential. The operator must deliberately replace or remove it; RouteX never automatically reuses an old credential against a different endpoint. Disabling or retaining an unchanged encrypted envelope does not need to decrypt it.

Supported connection security:

- `STARTTLS` requires the server to advertise STARTTLS, verifies TLS normally, and authenticates only after TLS succeeds. No opportunistic downgrade is allowed.
- `SSL_TLS` negotiates and verifies TLS before SMTP greeting or authentication.
- `NONE` permits unauthenticated SMTP only to `localhost` or a private/loopback literal address, and requires the independent `allow_private_smtp` bootstrap opt-in. Plaintext authentication is always rejected.

Encrypted connections support SMTP AUTH PLAIN when configured, plus unauthenticated servers when authentication is removed. Other AUTH mechanisms, custom CA uploads, and insecure certificate switches are not exposed. The process uses the host system's trusted certificate roots and validates the configured hostname.

`allow_private_smtp` defaults to false and accepts `${ROUTEX_ALLOW_PRIVATE_SMTP:-false}` in the example YAML. It does not inherit upstream or egress private-network settings. The shared guarded TCP dialer validates all DNS answers and dials a numeric approved address, preventing a second lookup from rebinding the destination. Metadata, link-local, multicast, mixed unsafe DNS answers, and other prohibited address ranges remain blocked. No environment proxy or managed HTTP egress is involved.

## Management API

Paths are relative to `/api/v1/admin`. All writes require session authentication, CSRF, same-origin checks, JSON content, strict fields, and current server-side permissions.

| Method and path | Permission | Payload |
| --- | --- | --- |
| `GET /smtp` | `smtp.read` | No query parameters |
| `PUT /smtp` | `smtp.write` | `enabled`, `host`, `port`, `security`, `auth`, `etag` |
| `PUT /smtp/sender` | `smtp.write` | `sender_name`, `sender_email`, `reply_to`, `etag` |
| `POST /smtp/test` | `smtp.test` | `etag`, `recipient`, `request_id` |

The settings response contains the redacted transport fields, `auth_configured`, sender fields, `etag`, `updated_at`, and nullable `last_test`. Neither username nor password is returned. Sender names contain 1–100 Unicode characters without controls; mailbox fields are one bare ASCII mailbox with a maximum of 254 bytes. Display-address lists, line breaks, and arbitrary recipient lists are rejected. The optional reply-to is one mailbox or an empty string.

Changed enabled endpoints/authentication and re-enabling perform a real bounded connection/TLS/AUTH/NOOP check before the configuration transaction. This check does not issue MAIL FROM or send a message. Failure returns 422 and leaves the previously saved configuration and ETag unchanged. Disabled configuration may be saved after syntax/address-policy validation without a network call. Sender writes validate syntax without sending mail. Every configuration transaction rechecks current permission and ETag and appends an audit event.

A stale revision or changed idempotent intent returns 409; invalid fields return 400. Missing authority returns 403. Unavailable encrypted storage or receipt persistence returns 503. The write response alone is not a claim that a message was delivered.

## Fixed test delivery

Tests always use the saved, enabled transport and sender revision. The only destination is the explicitly provided recipient. Subject and plaintext body are fixed RouteX content; callers cannot supply message text, headers, HTML, attachments, CC/BCC, or additional recipients. Credentials are not included in the message.

`request_id` must contain 16–80 ASCII letters, digits, underscores, or hyphens. It binds actor, configuration revision, and recipient digest. Repeating that exact intent returns the retained result without another SMTP submission. Changing its actor, revision, or recipient returns 409. Request IDs are case-sensitive, with a digest index preserving that behavior on both databases. A new test is limited to one admission per installation every 30 seconds; the cooldown persists across configuration changes and process restart. Permission is rechecked even when retrieving an existing result.

The admission transaction validates the current enabled revision and sender, records a pending attempt, advances the cooldown, and writes an audit event before opening a connection. Disabling prevents later admissions; an already admitted test may finish using its captured revision. No automatic retry is scheduled.

Results contain `request_id`, `config_etag`, `status`, optional generic `code`, total `duration_ms`, measured `stages`, `started_at`, and nullable `completed_at`. Stages identify only work actually performed: `connect` (DNS and TCP together), `greeting`, `tls`, `auth`, `sender`, `recipient`, `data`, and `acceptance`. Save-time checks additionally use `noop`. Each measured stage has `passed` or `failed` status, duration, and an optional generic code. There are no invented DNS timings or raw SMTP reply strings.

Status semantics:

- `pending`: the durable test admission exists and may still be running.
- `accepted`: the configured SMTP relay returned success after DATA. This proves relay acceptance, not delivery to a recipient's inbox. A later QUIT error does not undo acceptance.
- `failed`: the test was rejected or failed before potentially submitting DATA content. Failure codes distinguish connection, TLS, authentication, sender, recipient, DATA-command, cancellation, and timeout stages.
- `unknown`: DATA content may have reached the relay but the final response was lost, or an interrupted pending attempt exceeded its 30-second observation bound. It is never automatically sent again.

SMTP operations share a 15-second total context/deadline. Cancellation closes the underlying connection. Incoming server replies have a fixed 64 KiB session budget. A completion receipt is persisted with a bounded context independent of client cancellation. If persistence fails after sending, a subsequent same-ID request still cannot resend: its retained pending record eventually reads as unknown. Restarts retain every test identity; there is no cleanup job that forgets idempotency keys.

Retained test rows contain the actor ID, configuration revision, recipient digest, generic result, and timestamps. Audit events use an internal opaque test ID and contain no recipient, message, credential, or peer response. The test interface is a bounded administrative diagnostic, not a general relay endpoint.

## Validation boundary

Controlled local SMTP fixtures exercise real plaintext private relay, verified STARTTLS and implicit TLS, encrypted AUTH, fixed message content, each SMTP rejection stage, cancellation/deadline, untrusted TLS rejection, and ambiguous final acceptance. All recipient addresses use reserved test domains; tests do not contact external email services.

The serialized dual-database helper covers configuration/credential persistence, host-change credential protection, failed-save preservation, strict HTTP and permissions, one real test message, exact replay deduplication, cooldown, interrupted receipt recovery, restart persistence, and disabled dispatch rejection. The completed phase passed the full check and test suite with 378 Vitest cases, Go race coverage, development lifecycle checks, and production asset serving. The PostgreSQL/MySQL lifecycle suite passed in 264.903 seconds, and both database process suites passed initialization, restart persistence, ordinary and streaming native inference, reporting, logout, and revocation. The current feature must not be presented as durable notification delivery until notification job persistence, retry policy, recipient policy, and delivery tracking are implemented and tested separately.

## Control-plane interface

The email settings page presents separate saved SMTP and system-sender summary cards. Each opens its own configuration drawer. Readers can inspect the saved values; `smtp.write` enables configuration edits, and independent `smtp.test` authority enables the test form in the server drawer. Authentication summaries expose only whether credentials exist.

Server edits explicitly keep, replace, or remove authentication. Changing the host, port, or security while authentication is configured requires replacement or removal. Replacement username and password exist only in the open component; closing the drawer discards them. Credential-bearing writes use direct API calls with the current session CSRF token, sanitized status-only errors, and no mutation cache or browser storage. Unencrypted connections cannot retain or replace authentication. Save-time verification failure preserves the previously saved summary.

A conflict or uncertain save requires reloading and reviewing the saved revision before another save. Reloading preserves the user's draft while replacing the baseline and ETag. The visible current configuration makes concurrent endpoint and sender changes reviewable.

The explicit test form accepts one bare ASCII mailbox and sends only the server-defined message using the saved enabled configuration. Unsaved server changes prevent a new test. The first submit captures an immutable recipient, revision, and random request ID; a ref guard prevents duplicate in-flight submissions. Checking a retained result reuses that exact intent, including after an uncertain response. A new test requires an explicit separate action. The interface never retries automatically, never claims inbox delivery, and displays only measured stages and server-returned generic codes. Cooldown and revision conflicts are shown separately; conflicting configuration can be reloaded before preparing another test.

Focused frontend tests use controlled HTTP adapters, not an SMTP connection. They cover permission separation, credential replacement/removal and dismissal, CSRF, sender writes, revision review with preserved drafts, verification failures, malformed recipients, request identity and double-submit protection, accepted versus unknown outcomes, cooldown, and English/Chinese switching. These tests do not substitute for the backend's controlled SMTP and dual-database acceptance evidence.
