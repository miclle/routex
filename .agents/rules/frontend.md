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

Credential retirement readiness stays in the replacement row action menu and
existing Base UI dialog composition. The advisory GET requires independent read
authority and never disables either record. Validate exact source/replacement/
Connection IDs, coherent ETag, nullable configuration/evidence and bounded route
count. Hide previous eligibility during refresh or error. Localize known blockers
and use generic localized guidance for new codes; expose no caller, Key, request
content or secret. Readiness concerns the current processing instance only.

Personal and Project Key rotation dialogs explain the native-completion gate.
Delivery, HTTP acceptance, known usage, tool handoff, blocked/truncated responses
and unknown history do not verify a replacement. The server owns eligibility;
keep secrets transient, retain the selected replacement across language changes
and preserve historical completion retries and current authorization.

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

Project lists show total retained Project Key counts, including pending, revoked
and expired records, rather than Model counts or active-only Key counts. Read
counts only from server `key_count`; current exact managers or `projects.write`
may receive them, while `projects.read_all` alone receives null/Unknown.
Administrative lists require `projects.read_all` and show only server-projected
stored monthly Tokens, exact decimal money/currency, RPM and TPM. Personal lists
return no policy summary. Keep explicit zero, null/Not set, no stored policy,
unavailable and unknown distinct; never infer effective defaults, unlimited
capacity or remaining allowance, round money through JavaScript numbers, or add
per-row Overview/limit/directory reads. Administrative Project search accepts
literal names and case-sensitive canonical ID fragments without changing scope.
Hide list facts, actions and pending lifecycle dialogs during renewed Session
or list reads, errors and actor changes; administrative lists also wait for fresh
read permissions. Stale replies cannot restore private rows, and lifecycle
dispatch requires fresh independent write authority. After fresh authorized Project detail and Session reads, replace
legacy managers/models/limits tab URLs with settings/resources/resources while
preserving other query parameters. Denied reads never redirect; Team counts and
tabs stay unchanged.

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

Admission controls live in the addressable Member Limits tab, Project Resource configuration, and existing Key detail/restriction surfaces. `views/resource-limits` shares the implemented rolling five-hour/seven-day token, monthly token/money, TPM, RPM, concurrency, and IP policy editor; aggregate edits are inline and Key restrictions use the local dialog. Keep null/inherited values distinct from zero, display stored/effective policies and the complete IP conjunction, and show publication status separately from persistence. Writes require a reason and strong If-Match. Retain immutable submission intent for uncertain publication retries; stale policies require explicit reload/review without discarding the draft. Read the platform denomination from the resource-authorized `platform_currency` field and preserve exact money strings. Changed currency requires explicit review of retained drafts. Display authoritative quota windows, coverage, holds, and unknown values separately; never invent remaining allowance. Default templates and broader alerts remain outside this policy editor.

Team monthly exhaustion notifications freeze current-policy aggregate settled
Tokens/money and the then-current enabled owner/member recipients. Current exact
membership controls inbox/read access; rejoin retains original read state, while
new members receive no historical fanout. Session network generations hide stale
rows/count/read actions; preserve manual same-actor CSRF cache replacement.

Private Team member notices use the same menu and existing Session inbox routes.
Keep team_member distinct from Team aggregate exhaustion: validate the stable
pair digest, frozen Team/User proof fields and exact current recipient before
rendering a recorded Team name or team_id fallback. Only the exhausted member
receives their own stored child-policy notice; owners/admins gain no peer access.
Current enabled active membership governs list/count/read/mark-all. Removal hides
original rows; same-user rejoin may restore their original read state without new
recipients or native replay. Derive exhaustion only from that child's covered,
known settled monthly journal and exact applied policy/calendar/currency. Never
sum parent/child quotas or treat live holds as settlement. Preserve exact decimal
strings, paired notifications copy and current Session-generation guards. Invalid
or foreign member proofs fail closed; no member directory or new endpoint is used.

Team resource limits use the addressable Limits tab and existing member action
menu/dialog. Aggregate policies support rolling/monthly Tokens, monthly money,
RPM, TPM and concurrency; members support monthly Tokens/money and request rates.
Use independent `teams.tokens.write`, `teams.money.write`, and
`teams.rates.write` permissions, sparse presence-aware writes, exact decimal money,
server-owned editable fields and a composite reviewed ETag. Owners receive no
implicit direct write authority. Keep the limits-only workspace free of member/model
directories. Stable Team/User policy and journal identities survive removal/rejoin;
aggregate and member admission is atomic. Preserve drafts and original uncertain
intent through incidental focus/reconnect events, with fresh authority on mount
and dispatch. Runtime confirmation includes the complete policy, revision, current
membership and monetary denomination; persistence alone never proves enforcement.

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


Credential planned retirement extends the existing replacement readiness dialog.
Keep providers.read and providers.write independent, require a reason and explicit
Base UI confirmation, and dispatch the exact reviewed replacement, native attempt,
configuration and aggregate ETag with one UUIDv4 intent. Retain that immutable
intent through uncertain or rejected retries; refreshing readiness never resolves
uncertainty. A durable receipt confirms historical commit independently of current
runtime application. Never optimistically disable rows, treat a re-enabled or
missing predecessor as currently applied, or claim supplier/fleet revocation.


Member catalogue availability counts only active models with eligible supported
native protocols. Explicit empty protocols and unknown/disabled models must never
fabricate a Chat endpoint or cURL example. Keep localized unavailable guidance,
disabled copy and the native Gemini name guard; Key creation cannot repair route
availability. Team visibility and Personal Key authority remain separate.


Member source visibility uses actor-scoped `model-catalog` queries, separate from
the direct personal `/models` Key selector. Deduplicate models and actual sources;
Team visibility never grants Personal Key or Team invocation authority. Keep
conjunctive literal/source/protocol/explicit image-PDF filters, two-plus-all source
labels, and the existing card/table/520px drawer. List, discovery and detail cache
keys include the current actor and successful Session network generation; detail
also includes the exact Model. Cancel obsolete reads and hide private facts/actions
during renewal, errors or revocation. Key navigation requires confirmed current
personal availability. Render missing price/member/request facts as unknown.

Member Model examples select an exact Personal or named Team source separately
from list filters. A sole source may initialize the selector; multiple sources
require an explicit choice. Use the selected source's ready native protocols,
including non-Chat Team sources. Removed sources or unavailable protocols require
explicit reselection without fallback. Team cURL examples use the existing
standalone builder with nonsecret request data only, sign in separately through
execution-time ROUTEX_EMAIL/ROUTEX_PASSWORD, require Python 3 standard-library
bootstrap, and stop on login challenges. Never export a browser Session/CSRF or
accept a live Key. Generation/copying performs no login, inference or grant write.
Guard copy/navigation synchronously against current authority; late clipboard
completion cannot restore stale notices. Manual same-actor CSRF cache replacement
is not a successful network renewal. Preserve actor/Model-scoped request captures
through incidental renewal without replay; actor/Model changes destroy them.
Invalidate generation-suffixed candidate drawers by their complete actor/Model
prefix. Keep all visible example guidance in the paired catalog translations.


Monthly quota exhaustion uses the existing notification menu and recipient-scoped
inbox routes. Enabled members may read their own Personal notices; Project
notices require current enabled manager authority and an active Project at each
read/count/action. Operational visibility still requires `system.read`; delivery
settings and SMTP permissions remain independent. Display only typed server-owned
settled amounts, limits, currency, calendar boundaries and observation times,
with exact strings and paired `notifications` translations. Never infer threshold
crossings, percentages, remaining allowance or exhaustion from holds. Hide stale
rows, unread counts and actions during refresh or authorization failure, and bind
read retries/cache invalidation to the captured actor.


Project monthly quota requests reuse Resource configuration and scoped request
history. QUOTA requests accept finite monthly targets only: blank omits a field,
zero is valid and money remains an exact decimal string. Preserve independent
model/quota reviewer permissions and current-manager authority. Bind submission
and approval to reviewed composite policy/currency validators; retain original
UUID, payload and validator through uncertain retries. Fresh details separate
historical baseline, requested targets and current policy. Saved approval never
proves runtime application, and superseded approval retries never restore an old
policy. Keep paired projectRequests copy and explicit conflict review.


Project rate-limit requests extend the existing Resource adjustment form with
RPM, TPM and concurrency fields. RATE_LIMIT and QUOTA use independent request
records, permissions and immutable mutation intents; a combined form reports each
saved, failed or uncertain result separately. Never resend a saved half. A fresh
explicit policy review may renew only a definitively unsaved intent; a prior
unknown outcome remains unresolved even if a later retry returns a conflict.
Omitted fields preserve the current control, zero is a cap, and explicit null is
unsupported. Use coherent manager-authorized request-limits context, actor-scoped
queries, exact decision receipts and current same-actor CSRF. Preserve model/quota
compatibility and distinguish historical approval from actual current application.

Team Session Playground controls retain the approved credential-source selector
position with API Key as the default and explicit named current active Teams.
Discover eligible Chat, Responses, Messages and Gemini models from the exact
Team runtime endpoint, using stable model_id only for verified navigation and
native model names for invocation. Preserve each protocol's terminal/usage parser
and completed inline text history; never fall back across protocols.
Separate cookie/current-CSRF Session transport from Key credentials:omit; never
fallback across sources. Four-protocol text conversation, comparison and code
export are available; Team creator-private attachments require explicit current capabilities. Source/Team/model/actor changes and
unmount abort and clear sensitive state; late callbacks must check the current
actor/Team/generation. Team Calls uses current-member own-actor endpoints and
separate actor/Team cache keys, with cached rows/details hidden on refresh failure.


Team monthly quota requests use `views/team-requests`, `/quota-requests` and the
read-only `/admin/quota-requests` workspace. Preserve owner-first review, independent
Token/money platform stages, no self-approval, exact string targets and server-owned
current actions. Bind immutable creation/decision UUIDs to the reviewed context and
exact step; replaying an owner decision never approves a later platform stage. Keep
uncertain intent through rejected retries, explicitly review conflicts, and display
saved approval separately from current runtime application. Use local Base UI
dialogs/drawers and paired `teamRequests` translations. Approval confirmation must
render the server-issued stage effect preview and require the same reviewed ETag;
owner escalation explicitly changes no quota. Global records expose a workspace
link only when the server confirms a current assigned reviewer. Never fetch a global Team
directory for the personal workspace or enable mutations in global records.


## Team role scope

Team roles use the existing detail Roles tab and compact assignment table, local
permission dialog, candidate picker and explicit Save action. Keep the current
Team/actor query independent of global authenticated permissions and navigation.
Only exact `teams.write` and `teams.models.write` actions may be inherited, for the
assigned active Team and its current enabled members. Owner responsibility alone
grants no management action; global directories, Team quota-administrator dimensions,
Project/Key authority and role-assignment powers stay independent. Team lifecycle
writes retain direct platform permission checks.

Use target-specific member/model/role candidates and server-owned actor actions.
Only protected current platform administrators can assign roles. Preserve the
complete selection, required reason and reviewed If-Match through uncertain retries;
a current GET describes saved state rather than a historical operation receipt.
Changed role definitions require explicit review. Never query role or candidate
data from a minimal quota-only projection. Keep English/Chinese role copy in
`resources`, hide stale authority on refresh/denial, and distinguish saved role
assignment from runtime policy publication.


## Team usage reports

Team aggregate usage belongs in the existing usage workspace with a compact
Personal/own-Team selector. Fetch only the caller's scoped, paginated active Team
list; an expected `?team=` context never authorizes access. Keep actor, exact Team
and filters in query identities, cancel obsolete reads, and suppress stale private
reports during renewed reads or errors. Validate the exact `team_id` echo and
model-only dimensions. Team mode omits Key controls, rankings and provider
breakdowns while preserving existing cards, trends, model groups, historical
currency amounts and unknown coverage. Shared Team reports never expand actor-only
Team call history. Administrative Team filtering requires independent
`calls.read_all` and cannot combine Personal-user or Project subject selectors.

## Current resource detail authority

Resource details use actor- and target-scoped queries, zero stale/cache retention,
and renewed authorization on mount. Hide all cached private details, tabs and
actions while fetching or after an error. An absent or failed current Session
cannot supply an actor. Late prior-actor or prior-target responses cannot restore
private content. Keep existing localized loading/error states and resource layouts;
server-side exact Project permissions remain authoritative.

## User and Team defaults

`views/default-limits` preserves two User/Team tabs and grouped Budget, Tokens and
Rate limits rows with inline editing. Keep read/write settings authority separate,
server-owned editability, complete nullable policies, exact money strings and
reviewed currency generations. Paired `defaultLimits` translations cover all visible
and accessible copy. Template saves describe future creation only.

Existing User/Team aggregate editors use a server-derived restore context and
Base UI confirmation with reason and exact If-Match. User restore requires
`limits.users.write`; Team restore requires all token/money/rate writes. Preserve
IP/usage/window/holds and display saved versus exact current runtime application
separately. Explicitly review changed target/default/currency generations; preserve
original reset intent after uncertain publication, even after a rejected retry.
Scope queries to current actor/target and hide old private previews during reads
or denial. A changed actor cannot consume a late acknowledgement.

## Project creation and Overview

Retain the established create form with the purpose-specific initial manager
picker. Ordinary creators cannot remove themselves; direct `projects.write`
administrators may submit a complete explicit selection. Preserve selected users
outside the bounded search response and recheck exact enabled targets on the server.

Use the existing Project Overview composition for monthly quota, operating
counts, pending guidance, authorized common actions and bounded typed metadata
activity. Fetch the Project-specific snapshot without borrowing platform data.
Unknown or unauthorized sections remain null, known empty counts remain zero,
held and unknown usage remain separate and money stays exact. Refreshing or denied
authority hides previous private sections. Unknown creation does not permit
automatic replay or success inference.

## Personal model requests

Preserve the catalog card/table composition and 520px Model drawer. Use the
separate minimal candidate endpoint for the Available to request filter. Request
Personal access in the existing footer with a bounded reason and actor/model
review; a Team grant never substitutes for a Personal grant. Candidate metadata
is not a claim of Key or route eligibility.

Keep request history/review within the Member Models tab and independent scoped
workspace. `members.models.write` alone must not fetch a Member DTO, email, role
directory, Keys or limits. Use current actor/target query keys and suppress
cached private headers/cards while authority is renewed or denied. Preserve
original UUID/body/If-Match through uncertain outcomes, explicit conflicts and
rejected retries. Approval adds one Personal grant; present saved decisions and
current applied/pending/superseded status separately. Never replay or restore a
revoked grant automatically. Register paired personalModelRequests translations
and test default English, live Chinese, nonself/permission boundaries and
uncertain retries.

Personal request history remains accessible through the existing catalogue action
area regardless of current grants. Retain mounted history through renewed actor
reads while hiding private rows, and refresh exact candidate detail independently
of discovery. A captured committed response is historical; current application
copy comes only from fresh authorized request detail.

Team Model requests extend the existing Model drawer with explicit Personal/Team
scope, minimal membership-scoped Team selection and shared Team/Model pending
slots. Own history remains available after membership loss, while current facts
require fresh independent resource authority. Use `teams.models.write` scoped
review without owner-only authority or self-review. Approval adds only a missing
shared Team grant; unchanged canonical grants preserve request provenance and
ordinary removal/re-addition never revives historical proof. Pending eligibility
loss cancels requests atomically; approved shared grants survive applicant
departure. Keep immutable uncertain intents, current/historical status separation,
local Base UI confirmations and paired `teamModelRequests` translations. The
minimal review route is `/teams/:resourceId/model-requests`; never fetch a global
Team/member directory for request workflows.

Team Session native inference uses Cookie/current CSRF through exact Team resource
endpoints for Chat, Responses, Messages and Gemini. Native Key headers and extra
query/workspace selectors are rejected and scrubbed before request logging. Team
model discovery exposes actual ready protocols and per-protocol media capabilities;
member catalogue Team links use source-specific protocols without Personal Key
authority. Team comparison uses two to four independent native text lanes and a
shared composer. Team creator-private attachments use exact Session scope. Team code export captures
only non-secret native request data and generates independent environment-driven
login programs. Renewed
Session/Team authority clears stale selection and transcript while retaining an
unsent prompt; model loading and dispatch remain explicit. A native 401 triggers
a bounded no-store Session probe, and only its active authoritative 401 expires
the Session. Other native failures never establish logout. Cancellation, late
callbacks and noncompleted turns never enter completed text history.

Project creation uses the existing name/description/manager form with optional
initial models and monthly Tokens/money, RPM, TPM and concurrency controls. Keep
direct model/limit permissions independent. The optional initial-request mode
requires the applicant in the explicit initial manager selection and records
independent MODEL_ACCESS, QUOTA and RATE_LIMIT requests atomically at creation.
Use only minimal creation context/model candidates and exact decimal money with
a reviewed currency. Enhanced creation retains one UUIDv4/body/If-Match intent
through uncertainty; explicit retry reconciles the historical receipt without
restoring managers, grants, policies or pending children. Current Project facts
and runtime application require renewed resource authority, and receipt navigation
is explicit. Keep legacy creation compatible and preserve Team creation.

Administrative Model detail uses an exact actor/Model-scoped read, independently
of the global Model directory. Existing routing-table input/output prices are
read-only base INPUT_TOKEN/OUTPUT_TOKEN rates for each exact Provider-model. Keep
models.read_all and prices.read independent, preserve decimal strings, denomination,
1M_TOKEN, known zero, disabled and absent rates, and never synthesize a logical
Model price or infer media/long-context rates. Renew Model authority before price
reads/retries; hide old detail, prices and actions during Session/permission refresh,
errors, target changes and late replies. Price editing remains in Provider-model
details. Use paired catalog/pricing copy and existing Table components.

Team request code reuses the existing conversation/comparison code dialog.
Require fresh actor/Team/Session authority and an exact discovered model; clear
captured code on renewal, conflict, context changes or teardown. Each generated
program independently signs in with ROUTEX_EMAIL/ROUTEX_PASSWORD, verifies an
authenticated current Session and CSRF, then invokes exactly one Team-native
request. MFA HTTP 202 never becomes a Session. Redirects, failed proofs and
malformed authority stop before inference without retries. Keep passwords,
cookies and CSRF out of arguments, files, browser storage, logs and captured
snippet data. Python uses its private in-memory CookieJar; cURL requires Python 3
for private authentication then receives native curl configuration through stdin;
JavaScript uses Node's native fetch. Existing Key examples remain unchanged.

The conversation parameter Reset action restores Temperature 0.7, Top P 1,
maximum output Tokens 2048 and an empty system prompt. Preserve credential source,
Team, model, protocol, streaming, completed history and the unsent prompt; dispatch
no request. Disable parameter Reset while an invocation is active. Keep all
labels/notices bilingual and code previews aligned with the current defaults.

Team authority renewal observes every successful network Session read, including
structurally identical data with an unchanged timestamp. Clear Team models,
completed history and captured code, abort pending inference, preserve unsent
drafts, and require explicit model rediscovery. Observe the existing Session query
without creating an additional network observer. Manual same-actor CSRF cache
replacement is not a Team authority renewal and preserves completed history.

Member Overview keeps the approved identity header and monthly account table.
The self-only /overview/accounts endpoint exposes bounded current Personal and
Team accounts, with separate aggregate and stable Team/User member policies.
Preserve exact decimal strings, null versus zero, recorded currency, runtime
application, coverage and unknown usage. Monthly settled/retained facts and live
active_reservations are separate snapshots from the same coherent batch; never
add them or infer a remaining allowance. Only covered, known settled usage with
a positive finite Token cap may produce a percentage. Reauthorize the Session on
mount and every completed read, hide private rows/actions during refresh/error,
and ignore obsolete actor/generation pages. Keep usage links scoped and paired
overview translations; never fetch resource/model/member directories to fill the
monthly table.

Administrative member detail keeps the existing Overview tab and three cards for
Personal monthly Tokens, Personal monthly money and total retained Personal Keys.
Read only the exact member-scoped /admin/members/:user_id/overview endpoint under
independent members.read authority. The count includes every retained Personal
Key status, excludes Project Keys, and exposes no Key IDs, secrets or actions;
it does not establish active or callable Keys. Preserve count and money strings,
settled usage, unresolved monthly facts, live reservations, unknown coverage and
server-owned runtime application separately. No progress or remaining allowance
is inferred. Scope both parent detail and Overview queries to actor, target and
successful Session network generation; hide old cards during renewed authority,
pending/error reads and target changes. Reject obsolete responses and callbacks,
including structurally identical same-millisecond renewal. Observe the existing
Session query without another network observer; manual CSRF replacement alone
is not a network renewal. Keep paired governance translations and the existing
access-status section; this read-only view grants no member or Key mutation.

Creator-private Team media uses only exact Team Session/CSRF attachment routes and
canonical managed references in the four native image/PDF scalar positions. Bind
objects to exact Team, creator user and active membership, with a fixed deadline
one hour after creation; peers, owners, administrators and rejoined memberships
receive no bypass. A renewed Session may remain server-eligible for the same
creator/membership, while the UI clears unsent media and aborts uploads without
restoration or replay. Keep existing chips/picker/shared composer, intersect every
selected lane's capabilities, reject mismatched response ownership and retain
submitted objects until all lanes settle, including cancellation and unmount.
Cleanup denial defers to durable expiry. Keep files, credentials and references
transient, never borrow Personal/Project uploads, preserve completed plaintext
history only and disable code export while media is selected.

The member Home Overview preserves its identity header and monthly resource-account table, followed by three Personal thirty-day usage cards, a full-width Token trend, and Model/API Key detail tabs. Request only the existing Personal Usage API with the server-anchored 30d preset, UTC daily buckets and no comparison. Preserve exact returned range, observed query time, recorded completion and durable-delivery lag. Keep the authoritative success-rate denominator, including cancellations and admission failures, explicit. Render exact token strings and unknown coverage separately; leave unknown trend buckets as gaps and derive bounded shares only from known positive totals with BigInt. Historical identities come from the report alone. Scope queries to the current actor and successful Session generation, hide private facts during renewal/errors, abort obsolete reads, and ignore late responses. Keep overflow guidance localized and link to Usage without fabricating narrower results, remaining allowance, combined Team usage, or Key secrets.

Usage CSV export remains the final action in the existing filter row and uses
only the last applied scoped filters. Request one authorized server CSV; never
serialize cached report data or fetch a directory to fill it. Preserve the
reversible `apostrophe_text_v1` exact-text encoding, null versus zero, recorded
currencies and independent Personal/Project/Team/platform dimensions. Scope the
transient download to actor, target, applied filters and successful Session
generation; abort obsolete work and reject late callbacks without replay. Keep
401/403/404 authority loss separate from bounded-complete-export failures. Blob
URLs stay transient and are revoked promptly. A prepared download is not proof
that a file was saved. Keep paired `usage` guidance and independent server gates.
Preserve non-sensitive applied filters and independent raw drafts in an actor-and-exact-source owner through successful
Session or permission renewal. Generation-bound report/export subtrees still
unmount during authority reads; actor or Personal/Team/Project/platform source
changes clear filter intent. Never persist drafts in browser storage or private
query caches.

Internal secret storage uses `views/secrets` at `/admin/secrets` and addressable
rotation URLs. Require a current administrator and independent `secrets.read`
and `secrets.rotate` authority. Scope reads to actor, target and successful
Session generation; hide old private state during renewed reads/errors and
ignore obsolete replies. Keep the approved internal status card and local Base
UI confirmation dialog, exact configured key IDs, string epochs/counts and
server-owned blockers/observation times. Never display root material, private
proofs, secret subjects, invented progress or fleet acknowledgement. Mutations
retain the reviewed strong ETag, UUIDv4, body and reason through uncertain and
rejected retries; refresh never proves the original operation. Historical
receipts and current write-policy/publication application remain separate. Keep
paired `secrets` translations and clear transient action state on teardown.

Addressable historical root-rotation details expose only that recorded job's
server-allowed actions. Global Start belongs to `/admin/secrets` and is never
prepared from a historical task URL; the shell retains the Credential storage
title on addressable rotation routes.

Repository price maintenance uses the existing `views/price-imports` workspace
and a local `repository` composition above the file workflow. Configure explicit
stable-ID mappings before previewing selected models; configuration never applies
prices. Preserve reviewed source/configuration/catalogue generations, independent
read/write permission gates and immutable UUIDv4 intents through uncertain results
and temporary authority reads. Retain private workflow owners while hiding cached
fragments during renewed reads. Selected rate restoration belongs in the existing
Provider-model price table and uses only server-derived differences after explicit
confirmation. Keep exact decimal strings, per-rate custom zero/disabled/same-amount
protection, separate threshold ownership and historical receipts distinct from
current configuration/runtime proof. Register paired `priceImports` and `pricing`
copy; previews and retries never invent last-success metadata.

Model compatibility-name Early stop belongs in the existing Model information
card and local Base UI review/confirmation dialog. Keep models.read_all and
models.write independent; select one exact retained name with a reviewed strong
If-Match and required reason. Retain the exact original body/reason/ETag through
uncertain or rejected retries, hide private facts during renewed authority reads,
and clear ownership on actor/Model changes. Fresh review and matching current
retirement confirm only present non-callability/runtime application; they never
prove the original historical operation. Preserve stable Model/grants/bindings,
permanent historical-name reservation and already-dispatched calls. Register
paired catalog copy, render only recorded UTC deadlines with the selected locale,
and never infer eligibility from the browser clock.

Guided Model creation uses the existing creation page and the `modelCreation`
namespace. Select one exact authorized Connection and up to 50 Provider-model
items; retain selections outside bounded picker responses. Keep models.read_all /
providers.read separate from models.write. Preview server-derived new100,
same-protocol backup0 and first-protocol100 weights, then require reason and
explicit Base UI confirmation with a reviewed ETag and UUIDv4 intent. Preserve
that exact intent across uncertainty, renewed authority reads and rejected retries.
Receipts prove historical commit independently of current configuration/runtime
application; pending/superseded/unavailable are not success. New Models receive
no implicit grants and existing Keys never expand. Do not infer traffic readiness
from saved zero-weight configuration or render credential material.

Public Model name assistance stays inside the guided creation name control. The
versioned local reference file contains only reviewed official identities, source
URLs and review dates. The local Base UI Autocomplete accepts custom names and
requires explicit selection; malformed reference metadata disables suggestions
without disabling input. Suggestions never infer Connection/ProviderModel
selection, routing readiness, protocols, capabilities, prices or grants. Preserve
exact actor/Connection/row authority and the existing server preview, reason and
immutable creation intent; close obsolete suggestion interactions on renewed
reads or resource changes. Keep paired modelCreation copy.

Autocomplete dismissal labels use the paired common close translation. Base UI
1.8.0 exposes no public label prop for its two native hidden dismiss controls;
the local wrapper localizes only the exact ref-bound input/popup sibling labels.
Preserve native dismissal handlers, focus guards and custom input. Never use a
global query, observe the document, hide accessibility controls or disable focus
management. Keep open-popup language switching, both native dismiss actions,
Escape, reopening, row isolation and StrictMode covered by tests; a changed
native sibling structure must fail the focused compatibility tests.

Administrative Member Keys remain in the addressable Member detail Keys tab. Use
the exact Personal-only metadata projection with independent members.read and
members.keys.disable gates; never fetch a model directory, expose credential
material or add owner/Project lifecycle controls. Preserve exact-string quota
counters and currency amounts, monthly coverage and live holds, and shared
rotation roots without inventing remaining allowance. Review the exact Key and
strong persistent ETag in the local Base UI danger dialog with a required reason.
Same-actor/target renewed reads hide private rows/actions/dialogs while retaining
only the original transient uncertain reason and revision for explicit retry;
actor/target changes abort obsolete work and destroy that intent. Only the exact
authorized disable response confirms current disabled runtime state, never an
original historical operation. Keep paired governance copy.

Row-menu review dialogs may pass the native Base UI Popup `finalFocus` through the
local Dialog and use the local Menu `triggerRef` for their exact current row.
Member Key Escape and Cancel return focus only to a connected trigger with fresh
Session, permission, target and list authority. Skip disconnected or hidden rows
when authority changes or successful disable refreshes the list; never delay
private-data hiding or force focus through timers, global queries or stale DOM
references. Callers without these optional props retain native default behavior.

Administrative Member budgets, quotas and limits use `?tab=limits`; Settings
retains identity and lifecycle actions. Mount the Member-only limits composition
beside the other detail content with a stable actor/target identity. Hide policy,
edit and reset controls during Session, permission, target or policy refreshes and
errors without destroying the same-target unsent draft or original uncertain
request. Fresh `members.read` governs reads and `limits.users.write` independently
governs writes. Reuse the existing policy editor and default-restoration controls
with parent-managed authority; do not add Session observers. Guard review,
dispatch and response synchronously against current query generations, submit
original uncertain bodies and strong If-Match values only on explicit retry with
current CSRF, and never let a rejected retry clear uncertainty. Actor, target,
logout and tab changes destroy local private intent; explicit reset dismissal
retains its existing uncertainty warning. Preserve legacy Project, Key, Team and
default-limit callers and confirm runtime application before reporting enforcement.

Administrative Member Teams uses the addressable Teams tab with independent members.read and teams.read_all authority. Its six compact columns show retained relationships, exact member usage and stored caps, separate Team restrictions, and recorded nullable join dates. Reuse the parent Session generation and exact target queries; hide rows, links and private tooltip portals during renewed reads or errors and guard events against current cache authority. Never query directories or per-row policies, combine parent/member amounts, infer remaining allowance, or convert historical currencies. Local Tooltip wraps Base UI with keyboard dismissal, translated accessible labels and an optional synchronous authority guard; no private popup may outlive its authorized table.

Administrative Member Models uses the addressable Models tab and two compact Personal-grant tables with nine metadata columns. Keep members.read and members.models.write independent, preserve nullable unknown metadata and exact decimal price states, and never fetch unauthorized directories or infer Type, semantic update time, route price or Team ownership. Parent-managed Session/target generations hide private rows and dialogs during renewed authority reads while retaining same-target drafts and original uncertain bodies/If-Match for explicit retry with current CSRF. Actor, target, logout and tab changes destroy that intent. Only a matching authorized mutation response confirms current Personal grants and runtime application; it is not a historical receipt. Request review retains its separate nonself approval rules and uses the existing parent authority without another Session observer.

Own-Team call CSV uses the existing call filter row and member-safe export endpoint. Capture the exact actor, Team, successful Session generation, fresh scoped list authority and applied filters for each explicit download. Renewed/error authority, logout, scope/filter changes and unmount abort and discard obsolete responses before creating an object URL. Keep one enabled Team Session observer, no Blob cache or browser storage, a duplicate-click lock, fixed scoped filenames and object-URL cleanup. Preserve Personal, Project and platform scope contracts; Team export never requests Key/user filters or a directory.

Call tables preserve readable header and model/request widths inside their existing horizontal scroll container; narrow viewports must not compress identifiers into single-character columns.

Administrative Member Settings keeps the Basic information name editor and disabled parent-authorized email separate from role and lifecycle actions. Read exact member metadata through current parent Session, permissions, target and successful network generation; add no Session observer. Keep the editor mounted for the same actor/target during renewed reads or errors while hiding private form/dialog state. Review the strong ETag with a required reason, capture only name/reason/If-Match and preserve that original transient intent through uncertain or rejected retries. Retry explicitly with current CSRF; a matching GET never resolves uncertainty. Only current_member_name confirms present name, never a historical receipt or runtime application. Actor, target, tab, logout and unmount destroy intent; explicit dismissal abandons it. Cancel/Escape focus only the current connected authorized trigger. Preserve protected-target server editability, name codepoint/reason UTF-8-byte bounds, retained escaped labels and paired live translations.

Administrative Member lists retain the approved eleven-column composition and compact row menu. Read summaries only from the bounded list response: exact retained Personal Key count, persisted update time, stored monthly Personal controls and journal facts with separate live holds, unknown coverage and historical currencies. Recent login uses the recorded nullable timestamp of the last successful local sign-in; historical-unavailable/null remains explicit and is never inferred from Sessions or other timestamps. Team names require independent teams.read_all and typed overflow never returns partial names. Use one actor/generation/filter/cursor-chain list authority, hide cached rows, menus, tooltips and status dialogs during renewed reads/errors, and verify the exact fresh returned row before status dispatch or accepting a late result. Keep the existing detail and offboarding contracts separate. The call shortcut opens the independently authorized platform workspace without implying a member filter.

Administrative Member Overview keeps the existing account cards and access information before its read-only effective-model table. The ten columns add Authorization source after Model. Read only the exact member-scoped endpoint with parent-managed Session/actor/target generations and fresh members.read; add no Session observer, mutation, directory or inference action. Independent teams.read_all permits complete active Team enrichment; otherwise show only Personal sources and generic unknown union completeness, without Team counts/IDs/names or overlapping Team-derived facts. Provider labels and exact decimal prices retain independent read permissions. Hide rows and private source tooltip portals synchronously during parent or own-query invalidation, renewal, errors and permission loss; reject obsolete replies and guard callbacks against current cache generations/invalidation. Preserve unknown Type/semantic Updated, recorded Created dates, all four native protocols, conservative per-source availability and inactive retained configuration. Availability is advisory, never a Key ceiling, admission guarantee, historical receipt or native completion. Keep paired live governance translations. Preserve every exact monetary string and retained model identifier by wrapping within its fixed table cell; never let text overlap adjacent columns or truncate decimal precision.

Administrative Member Access keeps exactly Roles, Teams, Recent login, Created, Updated and Status inside the existing Overview card. One parent-owned member-scoped access query supplies its explicit custom-role header and read-only summary; global Roles directory reads belong only to the Roles tab. Keep members.read independent from roles.read and teams.read_all, with server-owned complete/denied/overflow/unavailable sections; show no denied IDs, names, counts or partial overflow rows. Use nullable recorded Updated and last-login facts, never browser clocks or observed_at as edit/login history. Reuse current parent Session/permission/target generations and synchronous QueryCache invalidation guards without another Session observer. Hide private header/card values during renewed reads, invalidation, errors and obsolete resource responses. Preserve existing account cards, effective-model table and lifecycle controls; add no role/Team writer or quota inference. Keep paired governance translations.

Member direct role assignment uses the addressable Roles tab, an assigned-only
four-column table, scoped permission dialogs and the local Base UI MultiSelect
wrapper for staged Add/Remove with explicit Save. Require independent members.read
and roles.read for private reads; only an exact enabled, nonoffboarded platform
administrator may replace custom assignments. Preserve the unremovable builtin
identity, complete retained reads, server editability, literal bounded candidates,
reviewed assignment and definition validators, reason and exact immutable uncertain
intent. A successful reviewed PUT confirms current database effect only; GET cannot
resolve uncertainty or prove the original operation. Keep saved effective platform
permissions distinct from an unsaved draft and Team/model/Key authority. Historical
assignments above1000 remain read-only; complete replacement is at most100 and
full typed audits are bounded60KiB without truncation. Actor/target/Session/read
renewal hides private rows and portals synchronously; no global directory fallback
or additional Session observer is allowed. Keep paired governance copy and source,
driver, migration and zero-inference process/restart/browser evidence separate.

Member base identity and account access retain the existing Settings cards and
Member list action menu. Read the minimal state review only for the invoked exact
target, using the parent real Session and independent read/write authority. Keep
same-actor/target editors mounted through renewed reads and errors while hiding
private facts and actions. Every PATCH changes exactly one base role or disabled
field with the reviewed strong If-Match, required reason and explicit confirmation.
Keep Metadata, offboarding and custom Roles separate; never fall back to the
unreviewed legacy endpoint, create a per-row review query or infer pending approval.
Preserve the immutable original transient intent after every failed dispatched
request, including the first conflict response. Fresh GET, Cancel and Escape never
resolve it; only the exact explicit retry may confirm current_member_state, without
claiming an original historical operation. Explicit Abandon drops only local retry
intent, leaves the original outcome unknown and retains the draft for current
review and a new explicit confirmation. Block abandonment while a request is pending
or current write authority is unavailable.
Account-access success requires the server's current runtime application proof;
base-role success confirms current database identity independently. Reactivation
clears the current offboarding marker without restoring old Sessions, Keys or
custom roles. Self-disable and self-demotion can commit before authority is lost:
401 uses established authentication cleanup;403 hides private facts while retaining
same-context intent, never claims success or retries automatically. Actor, target,
tab, logout or unmount destroys transient state. Keep paired live translations,
reason UTF-8 bounds, conflict drafts, exact dispatch identities and connected
focus restoration covered by tests.

Local registration approval retains the existing registration drawer and Member
list/Overview review entry. A register HTTP202 is anonymous pending guidance,
never a Session, MFA challenge or authenticated navigation. Pending/rejected
applications deny account use without inventing historical approval for unmanaged
accounts. Dedicated decisions require current admitted intrinsic administrator,
members.read and members.approvals.write; policy edits independently require
registration.write. Keep strong reviewed If-Match/reason, exact application
continuity and current admission/runtime confirmation separate from lifecycle,
grants, Keys, recorded decisions and historical receipts. Retain every failed
dispatched intent through fresh reads, dismissal and permission renewal; explicit
Abandon leaves the previous outcome unknown and requires current review before
another confirmation. Approval creates no Session or implicit grants. Render only
the bounded server summary in ordinary Member rows/details; keep application and
reviewer metadata within the dedicated authorized dialog. Preserve pending202,
fresh authority, exact retry, independent permissions and paired translations.

Global Role definition review preserves the existing Roles table and Edit/View dialogs. Read exact role-scoped definitions with fresh roles.read; only a current admitted platform administrator may replace a custom definition. Capture its birth proof, strong If-Match, complete name/permission definition and required reason before explicit confirmation. Preserve every dispatched failed intent, including conflicts, through dismissal, renewal and explicit retries with current CSRF; a matching GET never confirms an operation. Explicit abandonment enables a new review without claiming rollback. Only the exact authorized mutation response confirms the current database definition, never an original historical receipt or fleet application. Keep safe retained permission codes case-preserved and nonassignable unknowns literal; aliases never grant authority. Team-role reviews bind persistent definition generations. Reuse parent Session generations, local Base UI dialogs and PermissionRows; hide private data synchronously during renewed/error authority and reject obsolete callbacks. Keep paired governance copy, existing Create/Delete behavior and layout.

Role permission checkboxes use the existing local Base UI `Input` wrapper with compact checkbox classes. Preserve native label, checked, keyboard, form and disabled semantics, along with current reviewed authority and immutable-intent guards.

Role permission groups expose current assignable select-all controls and localized resource/action summaries. Preserve partial checked state, unknown recorded permissions and immutable failed-request guards; group toggles never introduce unavailable codes. Reuse compact local Base UI Input wrappers and narrow checkbox refs before setting native indeterminate state.

Member offboarding uses one page-owned Session observer and cache-only successful Session generations. Keep inventory, Member, permissions and successor reads scoped to the exact actor, target and generation, and hide private facts and dialogs during renewed or failed authority reads. A stable same-owner controller retains unsent drafts and every failed dispatched UUID/case/body; modal dismissal or matching current GET facts never resolves uncertainty. Explicit retries use current CSRF and a newly entered transient emergency password, and only a matching authorized POST receipt resolves the action. Starting a different action requires explicit local abandonment with unknown-outcome guidance and fresh review. Actor/target/logout/unmount destroys local state; obsolete replies, candidate events and request finalizers cannot affect another scope. Return dialog focus only to a connected, currently authorized invoking control. Historical completion remains recorded history after reactivation, including another original creator or completer; it does not claim current account disablement.

Member Settings displays recorded offboarding separately from current account access. Reuse the resource-authorized offboarding read with parent-managed actor, target and successful Session generation; hide facts/navigation during parent or own renewed/error/invalidation reads and fence queued navigation callbacks. The recent100 window prioritizes the first saved plan, otherwise the first completed case; a saved plan is not applied handover or current completion eligibility. Count only recorded assignment entries; historical Personal Key count is Not recorded. History GET never resolves an uncertain mutation or proves current runtime application. Preserve all existing Settings editors, drafts and immutable intent lifetimes, and add no Session observer or inline offboarding writer.

Registration email-domain policy uses the existing configuration drawer and sign-up card. Show only the server-authorized normalized exact domains, preserve complete reviewed If-Match/reason replacement and conflict drafts, and keep browser validation advisory. Role-list member counts are server-owned retained assignment totals; absent or invalid counts stay unknown, and the list never fetches private member directories.

Personal monthly quota warnings use the existing recipient-scoped notification
menu. Render only validated server-recorded 80%/90% levels, exact settled/limit
strings, calendar, currency and revision; keep unknown snapshots explicit.
History never proves current allowance or a historical crossing. Preserve
English-default/live-Chinese copy, recipient/session generation gates and
ordinary read/history actions; warnings change neither admission nor SMTP.

Team aggregate monthly warnings share the existing notification menu. Validate
`team-monthly-80-90-v1`, exact Team subject/scope and `two_`/`twi_` identities.
Render the recorded Team name or stable ID without a directory read. Individual
read state remains recipient-owned; current membership and exact recorded birth
are server-authorized. Leaving hides history, rejoining the same identity restores
its original read state, and new members receive no historical fanout.
