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
