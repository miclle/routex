# Object storage and owned attachments

RouteX stores attachments in an explicitly configured S3-compatible bucket. The database retains configuration revisions, ownership metadata, and durable cleanup intent. Object bytes stay outside the database. Storage is disabled by default. Configuration and file operations run outside gateway routing and authorization; a storage outage does not change model permissions or API Key scopes.

## Configuration and authority

`GET /api/v1/admin/storage` requires `storage.read`. It returns `enabled`, `etag`, the current `revision` (or null), and up to 20 verified historical `revisions`, newest first. Each revision contains `id`, `endpoint`, `region`, `bucket`, `prefix`, `credentials_configured`, `verified_at`, and `created_at`. Neither credential is returned.

`PUT /api/v1/admin/storage` requires `storage.write`, a session, same-origin protection, and CSRF. Its JSON body is:

```json
{
  "enabled": true,
  "endpoint": "https://s3.example.com",
  "region": "us-east-1",
  "bucket": "routex-attachments",
  "prefix": "attachments",
  "etag": "current-revision-token",
  "auth": {
    "action": "replace",
    "access_key": "operator-supplied-access-key",
    "secret_key": "operator-supplied-secret-key"
  }
}
```

Authentication actions are `keep`, `replace`, and `remove`. Replacement requires both values. Keeping credentials while changing the endpoint is rejected; operators must deliberately replace credentials for the new endpoint. Removal is allowed only in a disabled draft. Existing objects retain their original encrypted revision, including credentials needed to read and delete them. Removing credentials from the current draft does not destroy historical encryption material or delete attachments.

Credentials use envelope encryption with the installation root key and authenticated binding to the immutable revision and secret generation. Root-key loss prevents reads, verification, and cleanup that require decryption. Disabling the unchanged configuration remains possible without decryption. Back up the root key with the database using the same controls described in [Secret storage](SECRET_STORAGE.md).

The candidate descriptor is committed before an external write. Enabling a candidate performs a real write, read-back comparison, and exact-version delete. Only success publishes it as active. A verification failure returns 422 and leaves the previous active configuration and ETag intact. Publication rechecks current authority and ETag transactionally; a concurrent update returns 409 rather than replacing newer settings. Disabled drafts can be saved without network verification and cannot accept uploads.

`POST /api/v1/admin/storage/test` requires `storage.test` and `{ "etag": "..." }`. It tests the saved descriptor with a new opaque probe object and returns `success`, `cleanup_pending`, and actual measured `put`, `get`, and `delete` stages (`passed` or `failed`, `duration_ms`). A skipped read is omitted after a failed write. It does not enable storage or grant reusable proof for another save. Tests and changes are audited without credentials or content.

`POST /api/v1/admin/storage/rollback` requires `storage.write` and `{ "revision_id": "...", "etag": "...", "enabled": true }`. Only previously verified revisions qualify. Enabling a rollback repeats real verification; failure preserves current settings. Revision IDs can be retained by operators even when they fall outside the 20-item history window.

## Network and bucket boundary

The implementation uses path-style S3 requests with explicit static credentials and AWS Signature V4. It does not load environment credentials, instance metadata credentials, proxy environment variables, or the AWS shared configuration chain. It does not create buckets, list bucket contents, set ACLs, expose public object URLs, or create presigned URLs. General-purpose S3-compatible buckets are supported; directory buckets and access-point-specific endpoints are outside this contract.

HTTPS and normal certificate verification are required by default. Endpoints must be origins without userinfo, path, query, or fragment. Each connection validates and pins DNS results; redirects are forbidden. Private, loopback, link-local, unspecified, and multicast targets are denied by default.

Bootstrap `allow_private_storage: "${ROUTEX_ALLOW_PRIVATE_STORAGE:-false}"` is independent of upstream, egress, and SMTP network policies. It permits private or loopback HTTPS destinations and local HTTP only for `localhost` or private/loopback literal IPs. Metadata/link-local and multicast targets remain denied. This option supports controlled local S3 fixtures and deliberately configured internal storage; it does not disable TLS verification.

Keys contain only the configured prefix, `routex/`, and an application-generated opaque object ID. Filenames never become object keys. Uploads use conditional creation (`If-None-Match: *`) and no automatic SDK retry. Required bucket permissions are PutObject, GetObject/HeadObject, and DeleteObject; versioned buckets additionally require access to GetObjectVersion and DeleteObjectVersion. S3 credentials should be scoped to the configured bucket and prefix.

Successful writes retain `VersionId` when supplied. Cleanup checks the object's ownership metadata and deletes that exact version. This avoids treating a delete marker as permanent removal. An ambiguous write resolves its owned version through HeadObject before deletion. Objects with mismatched ownership metadata are never deleted. See the official [PutObject](https://docs.aws.amazon.com/AmazonS3/latest/API/API_PutObject.html) and [DeleteObject](https://docs.aws.amazon.com/AmazonS3/latest/API/API_DeleteObject.html) contracts.

## Attachment API

Attachment ownership is personal and immutable. A currently active account can access only its own object IDs; administrators receive no content-reading bypass. Team or Project attachment ownership is not inferred from a creator relationship.

| Method and path | Behavior |
| --- | --- |
| `POST /api/v1/attachments` | Multipart form with exactly one `file`, no other fields; returns 201 metadata after verified storage |
| `GET /api/v1/attachments/:attachment_id` | Owner-only metadata and lifecycle state |
| `GET /api/v1/attachments/:attachment_id/content` | Owner-only bounded bytes for a ready attachment |
| `DELETE /api/v1/attachments/:attachment_id` | Idempotently records deletion intent and immediately blocks further content reads |

All routes require a session. Mutations require same-origin protection and CSRF. Upload middleware accepts multipart rather than JSON and caps the entire request at 2 MiB plus 64 KiB of framing. File contents are limited to 2 MiB. Filename length is 1–200 Unicode characters with path separators and control terminators rejected. PNG and JPEG headers/dimensions are validated (maximum 8192 on either side and 32 million pixels); PDF requires its header and terminal marker. This is bounded format validation, not malware scanning or a complete PDF parser. PDFs are never executed or rendered by the backend. Caller-provided MIME labels do not decide the accepted type.

Metadata contains `id`, `name`, `mime`, `size`, `state`, and `created_at`. It contains no bucket key, storage credential, or version identifier. Returned content uses attachment disposition, `private, no-store`, `nosniff`, and a sandbox policy. Reads verify recorded size, SHA-256 digest, and storage ownership metadata, then recheck current account and object state before returning bytes. The implementation caps each account at 128 non-deleted attachments, including pending cleanup, to bound retained content.

Disabling storage stops new uploads. Existing owned content remains readable through its original descriptor; deletion and cleanup continue. Switching bucket, prefix, or credentials never reassigns historical objects to the new descriptor.

## Durable cleanup and recovery

A `storage_objects` row is committed before every external upload, including probes. States are `uploading`, `ready`, `delete_pending`, and `deleted`. The process starts a bounded cleanup worker before listening and joins it after HTTP shutdown. The worker claims due records with database leases, acts only on recorded opaque object IDs, and records completion or bounded retry backoff. There is no bucket-wide discovery or deletion. Database and root-key availability are required for progress.

Interrupted uploads become cleanup candidates after one minute. Confirmed uploads can be considered deleted when their recorded version is already absent. For an unconfirmed upload, a current 404 cannot prove the remote server will never finish accepting the earlier request. Such intent remains `delete_pending` with `upload_uncertain`; it is retried with bounded backoff until an owned object is observed and removed. This intentionally retains unresolved intent rather than reporting false cleanup success. Repeated deletion is idempotent. Failure receipts remain in the database; `deleted` metadata is retained for audit and recovery evidence.

If the request is canceled, cleanup intent is persisted with a separate bounded context. A worker crash leaves a lease that expires, allowing another run to resume. A failed cleanup never changes a model grant, API Key, routing snapshot, or attachment owner. Object Lock, legal hold, revoked credentials, and provider outages can keep deletion pending; RouteX does not bypass those policies.

## Gateway boundary and validation

This slice provides storage and session-authorized bytes. It does not add arbitrary remote-file fetching to the gateway or accept attachment IDs as provider file IDs. A later client integration must fetch owned bytes and construct supported native protocol content. Models and APIs continue to enforce their own image/PDF capabilities. Finite token, monetary, or TPM policies must reject requests whose multimodal usage cannot be safely bounded; stored attachments do not bypass that requirement or supply guessed token costs.

Focused tests use controlled loopback HTTP services only. They cover signed requests, conditional creation, version-aware deletion, foreign metadata protection, redirect rejection, bounded reads, cancellation, secret-envelope revision binding, configuration policy isolation, and accepted/rejected formats. The dual-database lifecycle helper covers HTTP upload/download, CSRF, owner isolation, revision changes and rollback, failed verification preserving active state, and cleanup replay including a late accepted ambiguous upload. The completed backend phase passed the full check and test suite with 378 Vitest cases, Go race coverage, development lifecycle checks, and production asset serving. The PostgreSQL/MySQL lifecycle suite passed in 275.269 seconds, and both database process suites passed initialization, restart persistence, ordinary and streaming native inference, reporting, logout, and revocation. Administration and attachment web interfaces have not been started in this phase.
