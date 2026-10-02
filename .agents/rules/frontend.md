# Frontend Rules

These rules apply to files under `website/src/`.

## Structure

- API calls go in `website/src/api/` and use the shared Axios client.
- Shared HTTP contract types go in `website/src/types/`.
- Page components go in `website/src/views/`.
- App-wide shell and composition components belong in `website/src/components/app/`.
- Reusable UI primitives belong in `website/src/components/ui/`.
- Route changes should update `website/src/router.tsx`, navigation, and NotFound behavior as needed.

## Server State

- Use React Query for server state.
- Keep query keys stable and scoped to the resource being fetched.
- Invalidate or update relevant queries after mutations.
- Authentication state uses the `auth/setup` and `auth/session` React Query keys. Keep session cookies HttpOnly; never persist session or CSRF tokens in browser storage.
- Route guards own authentication redirects. API errors must reach forms; protected 401 responses clear private query and mutation caches. Login and logout also clear previous account data.
- Do not hard-code backend origins in components; use the shared API client and Vite proxy.

## UI

- Reuse existing shadcn-style primitives, Tailwind tokens, Lucide icons, and local layout patterns.
- Use Base UI for headless accessible behavior when the interaction is non-trivial, such as tabs, menus, dialogs, comboboxes, or scroll areas.
- Account password changes replace cached session/CSRF data before subsequent writes. Clear password form values and mutation payloads after success; current-session revocation clears private caches and returns to login.
- Playground uses abortable native fetch requests with an in-memory Key; leaving the page cancels inference. Do not persist Key values or conversation payloads.
- Playground attachments keep the Mockup's chips-above-textarea, lower-left paperclip, and lower-right send/stop composition in `views/playground/attachments.tsx`. Comparison mode retains one shared composer and uses the selected lanes' conservative capability intersection while each native request and cancellation remains independent. Model discovery supplies one verified user or Project attachment scope for the transient Key. Upload personal objects through `/attachments` and Project objects through `/projects/:project_id/attachments` using only the session and CSRF token; never send the inference Key to either control-plane route. Treat `?project=` as an optional expected Project context, clear mismatched models and drafts, build native references through `lib/playground-attachments.ts`, delete one-invocation objects after settlement, and keep code export disabled while an attachment draft is selected.
- Personal and administrative call records have separate cache keys. Only administrative detail views may render upstream attempts and diagnostic identifiers.
- Catalog mutations send the current session CSRF token and invalidate the affected resource queries. Administrative routes, navigation, and write controls use the effective `/auth/permissions` query; built-in administrator checks remain only for reserved role/registration powers. Backend authorization remains authoritative.
- One-time Key delivery holds secrets only in component state. Confirmation enables the pending Key; dismissing the delivery dialog revokes it before clearing the secret. Never place secret results in query/mutation caches or browser storage.
- Use the local Base UI `Dialog` wrapper for modal focus, keyboard dismissal, and pending-action locking.
- Use local `Drawer` for side-panel details and mobile navigation, `Menu` for account and header notification actions, and `Table` for resource lists. Use the `Menu` side/alignment options instead of importing Base UI directly for different placements. The sidebar owns workspace/management navigation, collapse behavior, and a bottom account menu.
- Keep provider/model details addressable through resource routes. Profile and security are separate pages; model weights are edited within the routing table.
- Provider credential tables retain the existing add/verify/toggle workflow and use compact conjunctive metadata filters. Search names literally, reset filters on Provider change, and render only recorded verification timestamps with the selected language. Keep read and write permissions independent and every mutation bound to the exact credential ID; never expose secret fragments or invent pool statistics.
- Provider detail defaults to the addressable Overview and preserves the Overview, Connections, Credentials, Models, and Settings hierarchy. Quality cards use immutable attempt facts and full-attempt duration; never infer Provider attribution from a final logical call or mutable catalog record. Policy controls require independent `system.read`/`system.write` gates, an exact reviewed ETag, and a non-empty reason. Preserve drafts through explicit conflict review.
- Form inputs use the local Base UI `Input` wrapper with explicit labels, autocomplete, pending/disabled states, and accessible errors.
- Wrap Base UI components in local `website/src/components/ui/*` modules before pages import them.
- Keep page components focused on product state and composition instead of repeating primitive styling.
- Cover loading, empty, pending, success, and error states for user-facing data flows.
- Use semantic controls: buttons for actions, labels for form fields, and accessible names for icon-only controls.
- Do not display secrets or tokens in lists or logs; only show them once at creation if the feature requires it.

## Tests

- Prefer Vitest for API clients, hooks, route helpers, and non-trivial UI state derivation.
- Add focused Vitest coverage when introducing or changing shared UI primitives with behavior.
- Pure display components can skip tests when the behavior is low risk, but they must pass lint and type checks.

## Mandatory Design Fidelity

- Reproduce the approved product Mockup's navigation, page hierarchy, tables, drawers, forms, spacing, and interactions. Do not create a substitute layout.
- Use local shadcn/ui primitives and Base UI wrappers for that design; Ant Design/antd is prohibited.
- Keep real API and permission behavior while preserving the specified composition. Document necessary domain/security differences and verify the resulting pages against the reference.

- Governance screens use addressable member detail tabs, grouped role permissions, and a registration configuration drawer. `Switch` wraps Base UI; controlled registration uses the same centered authentication surface. Keep initial passwords out of retained mutation state.

Credential metadata editing stays in the existing Provider Credentials table and
local Base UI dialog. Use the resource-scoped metadata query, trimmed Unicode
name, integer priority from 0 to 10,000, and a required reason bounded to 1,024
UTF-8 bytes. Submit the reviewed strong If-Match, preserve drafts through explicit
conflict review, and retry uncertain publication with the captured original
request. Rejected retries leave the original uncertainty unresolved. Current
target reconciliation confirms current name/priority and runtime publication,
not who applied a historical operation. Keep secret material, verification,
enablement, and discovered model coverage outside the edit form; reset the form
on Credential or Provider change and use paired `catalog` translations.

Credential deletion uses the existing row action menu and a Base UI danger
confirmation with reviewed metadata and a required reason. Keep exact-ID,
If-Match, and reason intent across uncertain retries, including rejected retries.
Metadata GET 404 alone is never success; only an authorized DELETE response
confirming current absence and runtime application completes the workflow. Never
remove rows optimistically. Explain loss of availability after deleting the last
ready credential and preserve already dispatched requests and immutable history.

Credential replacement preparation keeps the approved dialog composition and
existing row menu. The new secret and UUIDv4/body/If-Match intent stay in component
state only. Validate the exact saved-result DTO and 201/200 status; pending disabled
creation never claims runtime application or completed rotation. Preserve the
predecessor, Connection and priority; real Verify and explicit Enable remain
separate. Normal conflicts require explicit source review and a new intent.
Uncertain retries retain the original intent after every rejection; source GET
cannot prove creation absent or permit another intent. Clear sensitive state on
success, dismissal, resource change and unmount. Show historical predecessor IDs
in the existing name-cell composition without secret fragments or new resource
queries. Keep paired `catalog` translations and independent read/write authority.

## Internationalization and Formatting

- Use `i18next` and `react-i18next` for every visible label, validation message, status, empty state, and accessible name. Supported languages are `en` and `zh`; English is the default regardless of browser locale.
- Keep paired English and Chinese catalogs in `website/src/i18n/locales/`. Register each namespace in `website/src/i18n/index.ts`. Shared/auth/Key/Playground copy uses `common`; catalog, governance, and account/calls use `catalog`, `governance`, and `activity` respectively.
- Use interpolation and plural forms instead of concatenating translated fragments. Render notices from translation keys so switching language updates existing messages without clearing user input. Format dates with the selected language; preserve user content, identifiers, protocol values, and sanitized server messages.
- `LanguageSwitcher` is available on authentication surfaces and in the app header. Persist only the non-sensitive `routex.language` preference. Browser storage failures must not prevent switching. Never persist credentials, session/CSRF tokens, or Playground content.
- Keep default-English workflow tests, live language-switch tests, key/interpolation parity, and missing-translation coverage passing when copy or namespaces change.
- Run `npm --prefix website run format` or `npm --prefix website run lint:fix` before each implementation phase is submitted. Prettier owns layout formatting; do not hand-compress JSX or add broad formatter ignores. `go tool task check` enforces formatting, ESLint, and types before commits.

## Team and Project Interfaces

Team and Project pages use separate scoped and global routes. Personal lists never fetch the global directory. Administrative list navigation requires `teams.read_all` or `projects.read_all`; resource detail authority is enforced by the server. Team owners receive scoped read access only. Current Project managers may edit metadata and manager assignments, while lifecycle and model changes require their explicit platform permissions.

Resource lists retain compact filters, tables, and action menus. Details use addressable tabs; Team members use identity/status rows and action menus, model access uses allowed/available tables, and Project settings include the manager table. Candidate pickers call only the authorized resource-specific endpoint. Existing selections outside a bounded search response remain selected. Unknown model names render stable IDs without querying an unauthorized catalog.

Project call tables reuse the call-record component with Project-specific endpoints and cache keys; they do not render administrator diagnostics or personal history. Metadata, continuity conflicts, terminal archival, and complete model/relationship replacements remain real API operations. New resources receive no implicit model grants. UI copy is paired in the `resources` namespace and follows the same English-default localization contract.

Provider model price editing belongs in the provider-model detail view. Reuse the
local price table and Base UI dialog wrapper, preserve decimal strings through
API submission, and require explicit conflict review before replacing an ETag.
Media input prices reuse that table as base-only `IMAGE_INPUT / 1_IMAGE` and
`PDF_INPUT / 1_PDF` rows. Render each row's actual unit, treat enabled zero as an
explicit configured rate, preserve the token context threshold, and do not add
page, pixel, byte, or provider-specific media controls without a supported backend
contract.

Project model requests belong inside Resource configuration. Keep scoped history, additions-only submission, current-manager checks, and independent reviewer permissions in `views/project-requests`; register paired `projectRequests` translations. Pending requests never change effective grants.

Platform currency settings use the dedicated complete-catalogue currency metadata endpoint. Keep a coherent reviewed ETag, currency, and required-currency snapshot; preserve drafts but block submission when a newer generation appears until explicit review. Preserve decimal strings and fixed self-conversion, and confirm configuration changes before dispatch.

Price file maintenance belongs in `views/price-imports` at `/admin/prices`, with download/upload steps and a separate server-derived difference preview. Apply only the captured UTF-8 CSV or original XLSX/XLS filename and base64 bytes, returned ETag, and preview digest after explicit confirmation. Keep CSV at 32 KiB and workbooks at 512 KiB; the server owns workbook parsing, text-only amount validation, and sheet/cell error locations. Never convert workbook amounts in JavaScript. Never synthesize a preview from edited data or treat an uncertain publication result as success. Keep file limits, read/write permission differences, all located errors, and paired `priceImports` translations covered by tests.

Admission controls live in member Settings, Project Resource configuration, and existing Key detail/restriction surfaces. `views/resource-limits` shares the implemented rolling five-hour/seven-day token, monthly token/money, TPM, RPM, concurrency, and IP policy editor; aggregate edits are inline and Key restrictions use the local dialog. Keep null/inherited values distinct from zero, display stored/effective policies and the complete IP conjunction, and show publication status separately from persistence. Writes require a reason and strong If-Match. Retain immutable submission intent for uncertain publication retries; stale policies require explicit reload/review without discarding the draft. Read the platform denomination from the resource-authorized `platform_currency` field and preserve exact money strings. Changed currency requires explicit review of retained drafts. Display authoritative quota windows, coverage, holds, and unknown values separately; never invent remaining allowance. Team/default/alert/request controls remain outside the implemented API.

Provider-model availability and input capabilities belong in the existing detail page before prices. Treat image and PDF input support as explicit provider-model declarations rather than inferring them from names or protocols. Save availability and capabilities atomically with one reviewed ETag, keep them separate from routing weights, and reconcile uncertain publication before retrying. Public model capability metadata must be the per-protocol intersection across every ready, enabled, positive-weight route.

Two-step verification uses the existing sign-in card and security settings card/dialogs. A login HTTP 202 is a transient challenge, never a Session or authenticated navigation. Keep challenges, proofs, enrollment material, and one-time recovery codes in component state only; sensitive operations must not use mutation caches or browser storage. Render the server-issued authenticator URI locally with the pinned QR library, without external QR services. Clear sensitive state on completion, dismissal, expiry, and unmount. Handle generic proof failures locally, refresh the real session when appropriate, and replace the current Session/CSRF while resetting private queries after successful MFA changes. Keep paired `mfa` translations and license notices.

Native protocol catalog controls display actual connection and model protocols. Create connections with an explicit protocol, preserve per-protocol binding weights, and generate matching member API examples. The current Chat Playground only offers models with eligible Chat routes from the Key-scoped model list. Unsupported `/v1` and `/v1beta` paths return JSON 404 rather than SPA HTML.

Usage reports preserve Personal, Project and platform scope in query keys and authorization. Render unknown token coverage separately from known subtotals, keep historical amounts as decimal strings grouped by currency, and leave unknown trend buckets as gaps. Use the existing filter/card/trend/distribution/ranking layout and Project tab, with bilingual controls and explicit complete-query overflow errors.

The Playground selects native Chat Completions or Responses from eligible model protocols and sends matching inline-history payloads. Keep protocol parsers separate, require their native terminal events, retain authoritative terminal usage and show accepted/incomplete/failed states explicitly. Model/protocol changes clear the conversation; cancellation and duplicate-submit guards must not replay requests. Keys and history remain transient local state.

The Playground uses native conversation and model comparison tabs. `views/playground/chat.tsx` owns single-model interaction; `compare.tsx` owns two to four independent native lanes with shared prompt submission and per-lane cancellation. Leaving a tab destroys its transient credentials and requests.

Quota calendar configuration stays within System information. Read access requires `system.read` or `limits.settings.write`; only the latter permits calendar writes. Calendar-only access never fetches or renders the site editor. Shared permission requirements may use a non-empty any-of list; an empty list grants nothing. Use server-owned editability, a reviewed ETag and reason, explicit Base UI confirmation, preserved conflict drafts, and immutable uncertain publication retries. A rejected retry cannot resolve the original uncertainty. Never reset usage or derive freeze status from the browser.

Site presentation is read through the public `site` query. `SitePresentation` applies the document title and the server language default without overwriting explicit language preferences; `SiteLogo` and `SiteFooter` compose the existing shell/auth layouts. Auth cleanup preserves public site settings. System information and announcement pages retain separate read/write permissions, and only authenticated shells mount the active announcement feed.

Native Messages is available in conversation and comparison lanes. `api/playground-transport.ts` owns bounded native HTTP/SSE transport; protocol clients retain independent event/finality and usage semantics. Messages histories include only completed text turns, with transient credentials and no cross-protocol fallback.

The audit workspace is a read-only, permission-gated table with filters, cursor pagination and a details drawer. Historical metadata that was not recorded stays unknown; arbitrary audit JSON is never rendered or exposed. Filter changes clear stale rows and selected details before fetching.

Native Gemini is available in conversation and comparison lanes. Requests preserve native contents/systemInstruction/generationConfig and one-candidate output. Histories contain only completed text, and final usage requires clean EOF with a native candidate finish or explicit prompt block. Unsafe model path names are rejected before dispatch with localized guidance.

Managed egress uses the `/admin/egress` workspace and `egress` translation namespace. Provider Connection controls select platform default, direct, or a named proxy with revision checks. Proxy diagnostics display measured stages, and authentication edits use explicit keep/replace/remove semantics with transient secret inputs. Read, write, and diagnostic permissions remain separate.

Storage administration uses `/admin/storage` with the approved overview card and large configuration drawer. Keep `storage.read`, `storage.write`, and `storage.test` independent; keep credentials transient and require explicit keep/replace/remove actions. Preserve exact ETags through conflict review, run probes only against saved descriptors, and report rollback success only after the target revision is verified and published.

Playground code dialogs expose working cURL, Python and JavaScript examples from the current native request in single or comparison mode. Shared snippet builders never accept an API Key; generated programs read an environment variable. Completed text history and current parameters are preserved, and an empty prompt uses an explicit editable placeholder.

System Status uses `/admin/system-status` with the approved Instances and System
jobs cards. Keep `system.read` and `system.write` independent, derive online and
cleanup eligibility from server-owned leases, and submit the exact reviewed
instance IDs and heartbeat revisions through the Base UI confirmation dialog.
Never infer state from the browser clock, remove rows optimistically, invent
resource percentages, process roles, or demonstration jobs, or report uncertain
cleanup as success. Only actual runtime publication, durable call delivery, and
storage cleanup work may appear as system jobs. Register paired `systemStatus`
translations and preserve independent refresh/error/empty states for both cards.

Provider-model capacity attestations belong in the existing detail view before prices. Read and write permissions remain independent. Use positive safe integer maxima, explicit evidence and reason, reviewed If-Match, retained drafts on conflicts, and immutable intent for uncertain retries. A configured record means a saved attestation, not proof that its revision is currently valid or enforced. Keep paired `pricing` translations.
