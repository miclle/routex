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
- Personal and administrative call records have separate cache keys. Only administrative detail views may render upstream attempts and diagnostic identifiers.
- Catalog mutations send the current session CSRF token and invalidate the affected resource queries. Administrative routes and navigation check the session role; backend authorization remains authoritative.
- One-time Key delivery holds secrets only in component state. Confirmation enables the pending Key; dismissing the delivery dialog revokes it before clearing the secret. Never place secret results in query/mutation caches or browser storage.
- Use the local Base UI `Dialog` wrapper for modal focus, keyboard dismissal, and pending-action locking.
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
