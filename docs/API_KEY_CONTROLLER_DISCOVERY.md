# API Key Controller Discovery

Status: delivered in `14a75fe` with controlled database and ordinary qualification.
Real browser and Agent acceptance remain pending. This additive
boundary depends on the frozen V114 controller schema and server contract. It
adds no migration and leaves the standalone controller executable unchanged.

## Administrator navigation

The existing API Key delivery Profile review can discover retained Slots and
independently enrolled Controllers through three Session-authenticated reads:

| Method | Resource                                                                           | Response                                    |
| ------ | ---------------------------------------------------------------------------------- | ------------------------------------------- |
| GET    | `/api/v1/admin/api-key-delivery/targets/:target_id/slots`                          | Retained Slot summaries                     |
| GET    | `/api/v1/admin/api-key-delivery/slots/:slot_id/controllers`                        | Retained Controller summaries               |
| GET    | `/api/v1/admin/api-key-delivery/slots/:slot_id/controller-enrollments/:request_id` | Original-actor nonsecret enrollment receipt |

Every read requires intrinsic administrator authority and independent
`secrets.read`. Having `secrets.write` alone is insufficient. Responses retain
private/no-store and nosniff headers, including errors. No endpoint enumerates a
global Controller directory, accesses Vault, returns a bearer, or performs a
configuration assignment, revocation, inference call or Agent action.

Lists accept only `cursor` and `limit`: the default is 20, with a maximum of 50
and minimum of 1. Use canonical decimal limits, one nonempty value per key and
canonical URL encoding in alphabetical parameter order (`cursor`, then `limit`).
Bare query markers, unknown filters, duplicate keys and malformed encodings are
rejected. `next_cursor` is an opaque canonical base64url value scoped to the
collection, exact parent ID/birth and retained anchor ID/birth. The next read
checks that the anchor still exists in the same scope. Cursors do not provide
write authority, a stable historical snapshot or cross-resource access.

Profile Slot discovery validates the exact retained Profile/principal relationship
and Profile/owner birth attribution. It does not require an enabled Profile or
currently enabled owner. Controller discovery uses the exact retained Slot
birth and owner/Profile attribution; changed or unavailable current configuration
and replaced principals do not remove a retained grant. Inconsistent identities
fail closed. A recreated Slot cannot borrow grants from an earlier birth.

## Facts and review boundaries

Slot summaries contain exact IDs/birth, owner and Profile identifiers and the
stored descriptor revision as a decimal string. Controller summaries contain
exact IDs/birth, name, expiry, recorded revocation and lifecycle status
(`active`, `expired`, `revoked`). `active` describes the retained credential
lifetime only; it does not prove its configuration remains usable.

Both summaries report `configuration_status: not_reviewed`. They contain no
configuration HCL, last report, reviewed mutation ETag, readiness prediction or
application/fleet status. Each list uses a bounded retained-row query rather than
running configuration review for every row. The existing single-Controller GET
remains the independent current configuration/revocation review boundary.

The exact enrollment lookup additionally requires the original enrolling actor
ID and birth. Other administrators can discover the grant summary but cannot
reopen its original actor-scoped enrollment receipt. Missing, different-actor
and replaced-actor receipts return the same not-found boundary. A receipt
contains the original request UUID, immutable intent digest and enrollment review,
plus the retained Controller summary. It always returns `bearer: null` and
`secret_status: not_recoverable`. An exact local intent digest and original review
match can reconcile that enrollment; a generic summary or matching current
configuration cannot establish the original operation. Lost bearers require
explicit review/revocation and a separate new enrollment, never reconstruction.

## Qualification

Delivered unit tests cover canonical query parsing, bounds, collection/parent
birth cursor isolation and lifecycle-only projection. A real-driver fixture is
registered for scoped pagination, cross-collection denial, original-actor birth,
independent read permission, retained revoked/unavailable records, private wire
responses and unchanged external-operation counters.

Commit `14a75fe` passes the containing phase's mandatory checks, ordinary tests,
authentication restart and complete PostgreSQL/MySQL regression, including these
registered controls. Browser interaction and real Agent execution remain pending;
directory facts never prove delivery, application use or fleet acceptance.
