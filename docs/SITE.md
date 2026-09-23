# Site presentation and announcements

Frozen GORM migration 17 adds a singleton `site_settings` row and retained
`announcements`, plus `site.write` and `announcements.write` for the built-in
administrator role. Repeated migration preserves saved settings and history.
Existing installations begin with name `RouteX`, empty service/Logo/footer values
and default language `en`.

## Presentation settings

`GET /api/v1/site` is public so initialization and sign-in can display the site
identity. It returns `name`, `service_url`, `logo_url`, `footer`,
`default_language`, `etag`, and `updated_at`. These values must be suitable for
public display. `PUT /api/v1/admin/site` accepts the same editable fields and the
reviewed `etag`, requires current `site.write`, and returns the updated record.
A stale revision returns `409`; clients must reload and review before retrying.

Names contain 1–100 Unicode characters; footer text is limited to 500. URLs may
be empty or absolute HTTP(S), up to 2,048 bytes, with no embedded credentials,
fragments or control characters. Service URL trailing slashes are normalized.
Default language is exactly `en` or `zh`. The setting supplies the language for
visitors without an explicit preference; a stored user selection takes precedence.
Changing a server default must not record it as the visitor's explicit preference.

Settings are presentation data. They do not change listener addresses, proxy
trust, cookie security, request Origin validation, API routing or outbound gateway
policy. The server does not fetch the Logo or service URL. Clients render name and
footer as text and Logo as an image with no-referrer behavior, never executable
markup. Logo load failures use the built-in product mark. External integrations
must validate their own callback origins instead of trusting this display setting.

## Announcement lifecycle

| Route | Authority and contract |
| --- | --- |
| `GET /api/v1/announcements` | Current session; every active announcement, newest ID first. No query parameters. |
| `GET /api/v1/admin/announcements?cursor=...` | `system.read`; retained active/closed history in 50-row pages. |
| `POST /api/v1/admin/announcements` | `announcements.write`; `{content}`; returns `201` and the published record. |
| `PATCH /api/v1/admin/announcements/:id` | `announcements.write`; `{content,etag}`; edits content without changing its status. |
| `POST /api/v1/admin/announcements/:id/close` | `announcements.write`; `{etag}`; stops display while retaining the record. |

Lists return `{items,next_cursor}`. A record contains `id`, `content`, `status`,
`etag`, `created_at`, `updated_at` and nullable `closed_at`. Status is `active` or
`closed`. Content is trimmed, valid UTF-8 plaintext with 1–4,000 Unicode characters;
NUL is rejected. Newlines are preserved. HTML-like content remains literal text,
not markup or Markdown. Announcement content is not automatically translated.

At most 20 announcements may be active, making the complete member feed bounded.
Publishing at that limit returns `422`; close an existing announcement first.
History remains paginated rather than being deleted. Editing a closed announcement
keeps it closed and preserves its closure timestamp. Repeating close with the
current ETag is a no-op; stale writes conflict. There is no hard-delete or reopen
endpoint. Creation retry after an uncertain response requires refreshing history
before another publish, since creating a new record is not an idempotent write.

All mutations require same-origin JSON and current CSRF. Strict JSON rejects
unknown fields, extra JSON values and malformed bodies; management body limits
are 64 KiB. Current permissions are rechecked under the shared governance lock.
Writes, revision changes and secret-free audit events commit together. Audit
payloads do not duplicate site text or announcement content. These are direct
Control Plane reads, not gateway runtime policy publications.

## Verification

Pure tests cover UTF-8/character limits, normalization and unsafe URL forms.
The shared `testSiteLifecycle` PostgreSQL/MySQL helper covers public defaults,
restart/repeat migration preservation, CSRF, strict JSON, stale/concurrent writes,
active delivery, closed editing, idempotent closure, complete history pagination,
active capacity, immediate permission loss and transactional audit counts.
Rendered branding, language preference precedence and announcement interaction
require frontend and browser evidence separately from these backend checks.

## Interfaces

System information and announcement management are available at
`/admin/system-info` and `/admin/system-announcements`. Navigation and pages
require `system.read`; write controls independently require `site.write` or
`announcements.write`. The site form preserves drafts on a stale ETag and shows
the latest saved values for review. Announcement history supports incremental
loading, editing and close confirmation. Closed entries remain editable without
being republished. All content is rendered as plain text.

The shell and authentication pages use the saved site name, safe HTTP(S) logo
and footer. Failed images fall back to the built-in mark. Site language defaults
never write a browser preference, and a user's explicit choice takes priority,
including choices made while the settings request is pending. Signed-in shells
refresh the active announcement feed every minute.
