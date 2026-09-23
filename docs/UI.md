# RouteX UI Structure

The application follows the approved product layouts using local shadcn-style components and Base UI behavior. It does not depend on Ant Design.

## Navigation and Page Structure

- Desktop navigation occupies a 248px sidebar, collapsible to 80px. Below 992px, navigation opens in a 280px left drawer. The page header is 64px high and the content area scrolls independently.
- Workspace and platform management have separate navigation. Profile and security have separate entries; account actions live in a menu at the bottom of the sidebar.
- Initialization and login use a centered, 500px-wide authentication surface.
- Providers use a table and addressable detail pages with connection, credential, and model tabs. Secret inputs remain write-only; verification and enabling remain separate actions.
- Administrative models use a table, a dedicated creation page, and addressable details. Route weights are edited directly in the detail table. Rename and grants remain focused modal actions.
- The model catalog supports cards and tables with a right-side API access drawer. It displays only actual authorized model data.
- API Keys use a table, status filters, model-scope drawer, creation form, and one-time delivery modal. Dismissing delivery revokes the pending Key before clearing the secret.
- Calls use compact filters, a table, incremental loading, and a right-side detail drawer. Member details do not render administrative diagnostics.
- Profile uses identity and editable information panels. Security contains password and session panels, with password changes in a modal.
- Playground uses a 320px configuration sidebar and a conversation workbench. Native gateway requests, cancellation, partial output, errors, request identifiers, and usage retain their real behavior.

## Data and Validation Boundaries

Only supported API fields and operations are exposed. Unavailable pricing, aggregate usage, organization preferences, identity-provider settings, notifications, and administrative controls must not be represented by fabricated values or successful placeholder actions. Layout alignment does not alter backend authorization, credential verification, Key delivery, or password/session semantics.

`Table` provides shared table styling. `Drawer`, `Menu`, `Dialog`, and `Tabs` wrap Base UI for accessible keyboard and focus behavior. API clients and React Query continue to own server state; private credentials never enter browser storage.

Run frontend type checking, lint, Vitest behavior tests, and the native gateway proxy test after modifying these flows. Browser verification should use disposable database and upstream fixtures and must avoid capturing credentials in screenshots.

## Member Governance

The management sidebar derives its visible entries from `GET /auth/permissions`. Pages check the corresponding read permission before mounting resource queries. Catalog write controls independently check write permissions, so a member with a custom read role can inspect the delegated resource without receiving mutation controls. Protected administrator identity remains required for role definition, role assignment, and registration policy.

Members use a filtered, paginated table and an addressable detail page with overview, role, and settings tabs. Creation requires an explicit initial password and does not claim to send an invitation. Password payloads are removed from retained mutation state after completion or dismissal. Suspension requires confirmation, reports continuity conflicts, and explains that restored accounts do not recover revoked sessions or Keys.

Roles use a table and grouped resource/action editing modal. Built-ins are read-only. The permission picker uses the server's assignable permission list; explicit custom-role assignment never submits built-in IDs. Assigned-role deletion conflicts remain visible and recoverable.

Registration settings use an authentication-method card and right configuration drawer. Public sign-up is shown only while registration is enabled, uses the centered authentication surface, validates the password byte bound, and clears previous private caches when the server establishes the new session. Closed registration remains blocked by the server even if the form was opened earlier.

## Verified personal Key rotation

The personal Key table shows each replacement relationship and whether its original Key has been revoked. Confirming one-time delivery keeps the original Key usable; copying or confirming a secret never marks a call as verified. Once an active replacement exists, the original row exposes an explicit completion dialog that selects the replacement and asks the server to verify its persisted successful call before retirement. An eligibility conflict leaves the dialog open and the original unchanged for retry. Canceling pending delivery revokes only the replacement; emergency revocation remains an independent action. Disabled-source replacements retain their disabled state until explicitly enabled.


## English and Chinese Interface

The application initializes `i18next` with English as its default. The authentication surfaces and app header expose a compact language selector without changing the approved page composition. Choosing English or Chinese updates labels, validation, notices, status text, accessible names, and locale-sensitive dates immediately. The non-sensitive `routex.language` preference survives reloads; unsupported saved values fall back to English. Switching remains usable when browser storage is unavailable.

Translation catalogs live under `website/src/i18n/locales/{en,zh}`. The `common`, `catalog`, `governance`, and `activity` namespaces cover shared surfaces, provider/model workflows, member governance, and account/call records. User-entered content, IDs, protocol identifiers, and sanitized server error messages retain their original values. Existing form drafts and sensitive in-memory state are not reset by language changes; no credential or conversation data is added to browser storage.

Vitest checks locale key and interpolation parity, literal-key resolution, plural handling, default English, persisted language selection, blocked-storage fallback, and live-switch behavior on authentication, governance, account, and calls. Workflow tests use English by default. Prettier provides automatic formatting through `npm run format`; `npm run format:check` and ESLint are mandatory checks. Resource routes are registered only after their functional acceptance is complete.

## Team and Project Interfaces

Team and Project pages use separate scoped and global routes. Personal lists never fetch the global directory. Administrative list navigation requires `teams.read_all` or `projects.read_all`; resource detail authority is enforced by the server. Team owners receive scoped read access only. Current Project managers may edit metadata and manager assignments, while lifecycle and model changes require their explicit platform permissions.

Resource lists retain compact filters, tables, and action menus. Details use addressable tabs; Team members use identity/status rows and action menus, model access uses allowed/available tables, and Project settings include the manager table. Candidate pickers call only the authorized resource-specific endpoint. Existing selections outside a bounded search response remain selected. Unknown model names render stable IDs without querying an unauthorized catalog.

Project call tables reuse the call-record component with Project-specific endpoints and cache keys; they do not render administrator diagnostics or personal history. Metadata, continuity conflicts, terminal archival, and complete model/relationship replacements remain real API operations. New resources receive no implicit model grants. UI copy is paired in the `resources` namespace and follows the same English-default localization contract.


Verification includes focused behavior tests for scoped list authorization, Team creation with an owner picker and CSRF, read-only Team membership, manager-only Project settings, scoped manager candidate lookup, full model replacement preserving unseen IDs, recoverable continuity conflicts, disabled-before-archive lifecycle, live language switching, and Project call cache isolation. Quota, budget, and request-policy sections are introduced only when their backing operations are available.

### Provider model prices

Provider model names open a stable detail URL under their provider. The detail
keeps the identity card, authorized routing relationships, and price settings in
that order. Price settings use a compact component table and an editor dialog;
permission to read prices never implies permission to write them. Edits preserve
decimal strings, require an explicit zero for free rates, and submit the captured
catalogue ETag. A conflict blocks resubmission until current prices are reloaded
and the retained draft is reviewed. Only supported text-token conditions are
available. Import, repository synchronization, and other pricing dimensions are
separate workflows still under implementation.

### Platform currency and exchange rates

The currency page keeps the current-configuration card, conversion table, and separate confirmation dialog. It appears in the Prices and exchange rates navigation group. Readers can inspect configuration; `prices.write` is required for edits. Required currencies come from the entire enabled price catalogue through one coherent metadata response, rather than a paginated price-table sample.

Selecting a new platform currency clears other draft rates and fixes self-conversion at `1`. Validation requires positive exact decimal strings for all enabled-price currencies. Confirmation states that historical amounts and original model prices remain unchanged. Concurrent catalogue updates, including background refreshes, block saving until current requirements and the ETag are reviewed together; the draft survives that review. Pending confirmation cannot dispatch duplicate writes.

Focused tests cover read-only access, no initial writes, navigation order, currency-switch clearing, required rates, decimal validation and preservation, confirmation, conflict recovery, coherent background refreshes, duplicate dispatch prevention, and live language switching.

### Price file maintenance

`/admin/prices` provides a price-file maintenance surface, rather than a separate model browser. Its upload card preserves download, edit, select-file, validate, and confirm steps. The management API dialog documents the active authenticated endpoints. The file selector supports CSV, XLSX, and the bounded BIFF8 XLS subset; export remains CSV. Export downloads the complete server snapshot through an authenticated Blob request and releases the temporary object URL.

The file selector accepts one UTF-8 CSV up to 32 KiB or XLSX/XLS workbook up to 512 KiB. Workbook filenames remain intact and must fit 255 UTF-8 bytes. The server enforces one visible sheet, text-only exact amounts, supported literal fields, 160 rows, 20 models, normalized text bounds, and all structural/header/semantic rules. The UI explains rejection of formulas, active content, encryption, hidden/extra sheets, and unsupported legacy XLS features. Preview uses authoritative stable model identities and displays every located error, exact before/after rates, threshold changes, explicit zero/disabled state, and repository-follow changes. Difference rows paginate eight at a time. Preview/export require read permission; application requires write permission and CSRF.

Commit submits the unchanged captured document with the server-returned ETag and digest: either CSV text or the exact original workbook filename and standard base64 bytes. The browser never parses workbook cells, calculates formulas, or converts decimal amounts. Preview displays the server-reported worksheet and every supplied sheet/cell error location alongside physical row/column information. Selecting another file discards the prior preview. HTTP 409 requires a fresh preview; 422 displays all returned errors. An uncertain 503 never displays success and instructs the operator to reload and reconcile catalogue/runtime state. Pending operations cannot dispatch duplicate commits, and no file content is persisted in browser storage.

## Admission policy controls

Personal aggregate policies are visible on the signed-in overview and editable by `limits.users.write` in member Settings. Project aggregate policies appear inside Resource configuration: managers and authorized global readers may inspect them, while active-Project writes require `projects.limits.write`. Existing Personal and Project Key detail drawers show restrictions and an edit dialog; pending/revoked Keys are read-only, and inactive Projects cannot edit Key policy. No new top-level limits page or unsupported Token, money, TPM, or approval control is exposed.

The request and IP sections preserve the distinction between a blank aggregate field (unrestricted), blank Key field (inherit), and explicit zero (deny admission). Stored and effective values, parent/local IP conjunctions, stable rotation account IDs, current rolling-minute admissions, and active requests come from the scoped server response. Usage is never reset by editing. Unknown counters and unconfirmed runtime application remain explicit.

Every write captures the complete supported policy, audit reason, and quoted If-Match revision. A changed local or parent revision blocks a pending draft until explicit reload/review; the draft remains available. An uncertain transport/publication result locks the draft and offers an identical-body/original-ETag retry or fresh reconciliation. A successful response reports enforcement only when `enforced` is true. Session CSRF comes from current authenticated state. English and Chinese labels and existing validation messages switch without losing input.

## Two-step verification

Security settings retain the password, two-step verification, and sessions card order. Enrollment requires the current password, then displays a real locally generated QR code and the server-issued manual secret with its expiry. Only a verified six-digit authenticator code enables the factor. Closing or expiration clears enrollment material; a pending server enrollment can be explicitly canceled. No third-party QR service receives the URI. The pinned QR dependency's ISC and bundled MIT notices ship under `/licenses/qrcode.react-4.2.0.txt`.

Enabled accounts can disable the factor or regenerate recovery codes using their current password and a fresh authenticator or recovery proof. Newly generated recovery codes appear once in a separate acknowledgment dialog, only after confirmed success. Closing that dialog removes the plaintext. These successful operations replace current session/CSRF data, reset private query data, and reload safe metadata. Generic proof failures remain local and refresh session status without treating every incorrect code as session expiration. Uncertain failures show no recovery codes or success claim.

Sign-in HTTP 202 keeps the existing centered card on the sign-in route and offers authenticator or recovery-code verification. The challenge expires, can be restarted, and never enters the session cache. Successful verification alone completes authentication. Passwords, challenges, proof values, enrollment tokens/secrets, and recovery codes never enter React Query mutation state or browser storage. English/Chinese labels, validation, and expiry dates use the selected locale.

Focused tests cover HTTP status discrimination, challenge expiry/navigation cancellation, proof failure and retry, real SVG generation from the server URI, enrollment cancellation, one-time recovery delivery, duplicate dispatch protection, CSRF rotation/private cache reset, failure cleanup, and live language switching. Browser rendering and real backend TOTP verification remain separate acceptance checks.

## Native protocol catalog

Native protocol catalog controls display actual connection and model protocols. Create connections with an explicit protocol, preserve per-protocol binding weights, and generate matching member API examples. The current Chat Playground only offers models with eligible Chat routes from the Key-scoped model list. Unsupported `/v1` and `/v1beta` paths return JSON 404 rather than SPA HTML.

## Usage reports

Usage reports preserve Personal, Project and platform scope in query keys and authorization. Render unknown token coverage separately from known subtotals, keep historical amounts as decimal strings grouped by currency, and leave unknown trend buckets as gaps. Use the existing filter/card/trend/distribution/ranking layout and Project tab, with bilingual controls and explicit complete-query overflow errors.

## Native Responses Playground

The Playground selects native Chat Completions or Responses from eligible model protocols and sends matching inline-history payloads. Keep protocol parsers separate, require their native terminal events, retain authoritative terminal usage and show accepted/incomplete/failed states explicitly. Model/protocol changes clear the conversation; cancellation and duplicate-submit guards must not replay requests. Keys and history remain transient local state.

The Playground uses native conversation and model comparison tabs. `views/playground/chat.tsx` owns single-model interaction; `compare.tsx` owns two to four independent native lanes with shared prompt submission and per-lane cancellation. Leaving a tab destroys its transient credentials and requests.

Site presentation is read through the public `site` query. `SitePresentation` applies the document title and the server language default without overwriting explicit language preferences; `SiteLogo` and `SiteFooter` compose the existing shell/auth layouts. Auth cleanup preserves public site settings. System information and announcement pages retain separate read/write permissions, and only authenticated shells mount the active announcement feed.

Native Messages is available in conversation and comparison lanes. `api/playground-transport.ts` owns bounded native HTTP/SSE transport; protocol clients retain independent event/finality and usage semantics. Messages histories include only completed text turns, with transient credentials and no cross-protocol fallback.
