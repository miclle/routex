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

Attachment ownership is explicit and immutable. `owner_kind=user` binds a personal object to one user ID; `owner_kind=project` binds a Project object to one Project ID. Existing rows were backfilled to `user` by frozen GORM migration 23, which also adds the portable owner-kind constraint and `(owner_kind, owner_id)` index while retaining the released owner-ID index. Ownership is never inferred from an ID prefix, uploader, creator, manager, or Key ID.

A currently active account can access only its personal object IDs. Project routes require a current enabled manager and never grant a bypass through `projects.read_all`, `projects.write`, or `storage.*`. Active Projects permit upload, metadata, content, and deletion. Disabled or archived Projects reject new uploads and gateway inference while current managers retain metadata/content access and idempotent deletion for recovery and data minimization.

| Method and path | Behavior |
| --- | --- |
| `POST /api/v1/attachments` | Multipart form with exactly one `file`, no other fields; returns 201 metadata after verified storage |
| `GET /api/v1/attachments/:attachment_id` | Owner-only metadata and lifecycle state |
| `GET /api/v1/attachments/:attachment_id/content` | Owner-only bounded bytes for a ready attachment |
| `DELETE /api/v1/attachments/:attachment_id` | Idempotently records deletion intent and immediately blocks further content reads |
| `POST /api/v1/projects/:project_id/attachments` | Current-manager upload into the active Project's immutable scope |
| `GET /api/v1/projects/:project_id/attachments/:attachment_id` | Current-manager metadata for one Project-owned object |
| `GET /api/v1/projects/:project_id/attachments/:attachment_id/content` | Current-manager bounded bytes for a ready Project-owned object |
| `DELETE /api/v1/projects/:project_id/attachments/:attachment_id` | Current-manager deletion intent, including disabled or archived recovery |

All routes require a session. Mutations require same-origin protection and CSRF. Upload middleware accepts multipart rather than JSON and caps the entire request at 2 MiB plus 64 KiB of framing. File contents are limited to 2 MiB. Filename length is 1–200 Unicode characters with path separators and control terminators rejected. PNG and JPEG headers/dimensions are validated (maximum 8192 on either side and 32 million pixels); PDF requires its header and terminal marker. This is bounded format validation, not malware scanning or a complete PDF parser. PDFs are never executed or rendered by the backend. Caller-provided MIME labels do not decide the accepted type.

User/Project metadata contains `id`, `name`, `mime`, `size`, `state`, and `created_at`, without owner identifiers. Team metadata also returns the exact Team, creator and membership IDs plus immutable expiry so the client can verify its captured context. No response exposes a bucket key, storage credential or version identifier. Returned content uses attachment disposition, `private, no-store`, `nosniff`, and a sandbox policy. Reads verify recorded size, SHA-256 digest, and storage ownership metadata, then recheck current user or Project authority and object state after remote I/O before returning bytes. The implementation caps each immutable user or Project scope at 128 non-deleted attachments, including pending cleanup, to bound retained content.

Project upload creation holds the governance lock and locks the Project row before recording intent. It verifies the Project is active and the uploader is a current enabled manager before the remote write, then repeats those checks before publishing the verified object as ready. Manager removal or Project disablement during upload records cleanup intent instead of publishing. Once ready, the object remains Project-owned across creator departure, Key rotation, and manager replacement; a successor manager can govern it without transferring ownership.

Team attachment routes accept no query or workspace selector and derive the
Team only from the authorized path and captured Session identity. A current
enabled creator and exact active membership in an active Team are required on
upload, metadata, content and deletion, including after remote I/O. Peers, Team
owners and platform administrators cannot borrow or govern another creator's
objects. A renewed Session for the same creator and membership can access an
unexpired object; removal/rejoin never restores its old membership proof. Personal
and Project Keys cannot resolve Team objects, and Team native requests cannot
borrow Personal or Project objects. Frozen V46 preserves these owner/creator
fields and the existing user/Project semantics.

Team expiry is exactly creation plus one hour. Cleanup retries cannot extend that
deadline. Metadata, content and native resolution return unavailable after expiry,
while DELETE remains permitted only to the current exact creator/membership.
Deletion can remain pending when current authority or storage is unavailable;
the durable worker uses recorded cleanup intent and expiry independently of the
browser's ability to delete. No client clock grants access or proves cleanup.

Disabling storage stops new uploads. Existing owned content remains readable through its original descriptor; deletion and cleanup continue. Switching bucket, prefix, or credentials never reassigns historical objects to the new descriptor.

## Durable cleanup and recovery

A `storage_objects` row is committed before every external upload, including probes. States are `uploading`, `ready`, `delete_pending`, and `deleted`. The process starts a bounded cleanup worker before listening and joins it after HTTP shutdown. The worker claims due records with database leases, acts only on recorded opaque object IDs, and records completion or bounded retry backoff. There is no bucket-wide discovery or deletion. Database and root-key availability are required for progress.

Interrupted uploads become cleanup candidates after one minute. Confirmed uploads can be considered deleted when their recorded version is already absent. For an unconfirmed upload, a current 404 cannot prove the remote server will never finish accepting the earlier request. Such intent remains `delete_pending` with `upload_uncertain`; it is retried with bounded backoff until an owned object is observed and removed. This intentionally retains unresolved intent rather than reporting false cleanup success. Repeated deletion is idempotent. Failure receipts remain in the database; `deleted` metadata is retained for audit and recovery evidence.

If the request is canceled, cleanup intent is persisted with a separate bounded context. A worker crash leaves a lease that expires, allowing another run to resume. A failed cleanup never changes a model grant, API Key, routing snapshot, or attachment owner. Object Lock, legal hold, revoked credentials, and provider outages can keep deletion pending; RouteX does not bypass those policies.

## Gateway resolution and validation

Personal and Project API Keys can reference an owned ready object with the exact URI
`routex://attachments/<object-id>` in supported native media scalar positions:

- Chat Completions: `image_url.url` and `file.file_data`
- Responses: `input_image.image_url` and `input_file.file_data`
- Messages: base64 image/document `source.data`
- Gemini: `inlineData.data` or its accepted protobuf JSON alias

The reserved URI is inert in text, tool arguments, schemas and unknown fields.
RouteX never fetches arbitrary remote URLs and never forwards an object ID as a
provider file ID.

Resolution authenticates the Key, derives its immutable `user` or `project`
attachment scope, parses an immutable occurrence plan, authorizes the public
model, and selects one route. Personal Keys can resolve only their user's objects;
Project Keys can resolve only objects owned by their exact Project and never a
manager's personal objects. Every media occurrence must match the selected route's
explicit image or PDF declaration. A non-reserving preflight uses the selected
provider-model/protocol capacity attestation to compute and check the complete
input maximum plus the request's explicit output cap before storage access. It
also counts the exact validated image and PDF reference occurrences. A finite
monetary policy requires an enabled base media rate for every kind present and
adds those exact occurrence components to the conservative token price bound;
missing rates or FX fail before object storage access. RouteX never derives token
or money bounds from object bytes, pages, dimensions, object metadata, model
names, or sample calls. The final rate, concurrency and quota admission repeats
all checks and reserves once, after resolution and immediately before the single
upstream dispatch.

Team resolution derives an immutable `team`/creator/membership scope from the
captured published Session identity. Only canonical managed
`routex://attachments/obj_<ULID>` references are eligible in the native scalar
positions above. Raw inline base64, remote URLs, provider file IDs, audio/video
and unsupported native media fail before object storage or upstream dispatch;
opaque text and tool/function JSON remain opaque. Discovery reports actual
per-protocol image/PDF intersections across eligible routes with non-secret Team
and membership IDs. Missing or foreign objects, old memberships and expired
objects cannot be made readable by a valid reference earlier in the same request:
the complete metadata set is validated before any remote read. Team/model/creator
and membership authority is rechecked after reads and before final admission.
Finite-policy capacity and per-occurrence monetary requirements remain the same;
there is no media-token estimate or additional admission/dispatch.

Each unique object is read once per request with the invocation's captured
owner kind and owner ID, including exact creator/membership for Team Sessions. The read repeats the session API's owner, ready-state,
storage revision, exact version, object metadata, size and SHA-256 checks, then
rechecks ownership and readiness. Project reads also recheck that the Project is
active and retains an enabled current manager after remote I/O. Missing, foreign,
cross-scope, deleting and non-ready objects share the same safe not-found response.
Storage and descriptor failures return one generic unavailable response without
endpoint, bucket or credential details.

One request permits at most four reference occurrences and four unique objects,
8 MiB of unique raw bytes and 12 MiB of rewritten JSON. Each uploaded object is
still limited to 2 MiB. Rewriting preserves native occurrence order, uses data
URLs for OpenAI image/file fields, raw base64 plus canonical MIME metadata for
Messages and Gemini, and a sanitized stored filename for OpenAI PDF fields.
Cancellation stops remaining reads and prevents upstream dispatch.

Stored attachments do not bypass model capabilities or finite quota policy and do
not supply guessed token costs. The single-model and comparison Playgrounds use
the user or Project session upload API while keeping the API Key and object
references transient. Known
one-invocation objects are deleted when the draft is removed, its context changes,
the workbench closes, or inference settles. Every ready attachment also expires
after one hour through the existing durable cleanup worker, so a terminated client
cannot leave a permanent ready object or consume the owner limit indefinitely.

Focused tests use controlled loopback HTTP services only. They cover signed requests, conditional creation, version-aware deletion, foreign metadata protection, redirect rejection, bounded reads, cancellation, secret-envelope revision binding, configuration policy isolation, and accepted/rejected formats. The dual-database lifecycle helper covers personal and Project HTTP upload/download, CSRF, manager and owner isolation, manager-removal upload races, successor access, disabled/archived recovery, revision changes and rollback, failed verification preserving active state, and cleanup replay including a late accepted ambiguous upload. Migration coverage exercises empty creation, a V22 existing-row upgrade, partially applied DDL recovery, repeat execution, concurrent startup, constraints, and both owner indexes on PostgreSQL and MySQL.

The `/admin/storage` web interface exposes the saved status card and configuration drawer, independent read/write/test authority, transient credential actions, exact ETag conflict review, saved-descriptor probe stages, cleanup-pending state, and verified revision rollback in English and Chinese. It refetches complete history after writes and never treats an uncertain response as success. User/Project native inference resolution, single-model and comparison attachment lifecycles, conservative token/TPM admission, and explicit per-occurrence media monetary pricing are implemented separately from administration. Per-page, per-pixel, per-byte and external-service acceptance remain open.

## Team media interface and verification boundary

The Playground preserves the existing chips and picker above one shared composer.
Team uploads require exact returned Team/creator/membership metadata; unexpected
ownership is never adopted or deleted by the client. Image/PDF controls use the
intersection of every selected lane's native capabilities. Renewed authority
hides and clears unsent media, aborts pending uploads and preserves unsent text,
without automatic restoration or replay. Submitted objects keep their captured
Team target until all requests settle; cleanup denial or uncertainty falls back
to durable expiry. Selected media disables code export, and completed plaintext
history never replays earlier object references. There is no shared Team library
or browser-storage persistence.

The focused Team media API/UI suites cover those boundaries with controlled
responses. The 497-case Playground/native/snippet/localization gate includes 56
media cases and passed alongside TypeScript, scoped ESLint and Prettier. Focused Team lifecycle/V46 migration passed both drivers (76.817 seconds).
Rebuilt-main check/test/build passed, including 1640 frontend cases/92 files.
Controlled bilingual production proof passed 16 distinct Team native requests,
creator/membership isolation, fixed one-hour deadlines, shared retention during
independent cancellation and exact-version deletion. Four denied calls after
grant revocation started no upstream attempts; restart retained Sessions and call
facts without replay. Complete rebuilt-main PostgreSQL/MySQL acceptance passed (Handler 1137.119
seconds; Service 7.537 seconds);
the historical personal/Project verification above is not evidence for it. See
[Team Session inference](TEAM_INFERENCE.md) and [Playground](PLAYGROUND.md).
