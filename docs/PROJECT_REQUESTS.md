# Project model access requests

A current, enabled Project manager can request additional models for an active Project. Creating a request does not change effective grants. The request stores the grant baseline and the explicit additions separately; callers send only additional model IDs, never the complete desired grant set. Empty requests and models already granted at submission are rejected.

A different enabled member with `projects.models.write` can approve or reject the request. Manager status alone does not grant approval authority, and even a platform administrator cannot approve their own request. Rejection requires a reason. An enabled applicant can withdraw their own pending request, including after losing the manager relationship or after the Project becomes inactive. Rejection and withdrawal never alter grants.

Approval revalidates the active Project, the applicant's active account and current manager relationship, and every requested model's active status. It adds the explicitly requested models to the latest effective grants. It never restores a removed baseline grant or removes a grant introduced after submission. Existing Project Key model ceilings remain unchanged; keys gain access only when their existing ceilings include a newly approved model.

## API

All endpoints require a console session. Mutations also require the existing Origin and CSRF protections.

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/api/v1/projects/:project_id/request-model-candidates` | Current manager only; active ungranted model identities, literal `q` search, maximum 50 |
| GET | `/api/v1/projects/:project_id/requests` | Scoped history, with `status`, `cursor`, and `limit` filters |
| POST | `/api/v1/projects/:project_id/requests` | Submit an additional model request |
| POST | `/api/v1/projects/:project_id/requests/:request_id/decision` | Approve, reject, or withdraw |

Submission body:

```json
{
  "request_id": "req_client_generated_unique_id",
  "model_ids": ["mdl_additional_model"],
  "reason": "Required for a new application workload"
}
```

The caller-generated `request_id` is an idempotency key, not the generated request record ID. It is bound to the actor, Project, sorted model IDs, and trimmed reason. An identical authorized retry returns the existing record; reuse with different intent returns HTTP 409. The successful response has HTTP 201 and a generated `pmr_...` ID.

Decision body:

```json
{
  "action": "approve",
  "reason": "Approved for this workload"
}
```

Actions are `approve`, `reject`, and `withdraw`. Responses expose `kind: "MODEL_ACCESS"`, `baseline_model_ids`, `requested_model_ids`, the original reason, applicant and Project IDs, timestamps, and decision actor/reason. Status is `pending`, `approved`, `rejected`, or `withdrawn`. Decision reasons are optional except for rejection. Reasons are stored as business history and should not contain credentials.

Current managers and members with `projects.models.write` or `projects.read_all` can read a Project's history. Former managers lose that access unless separately authorized. Listing defaults to 40 records and accepts at most 100; records are ordered by descending ID. The response contains `items` and an optional `next_cursor`.

## Consistency and operational scope

Creation and decisions serialize with Project lifecycle, manager, grant, and account governance changes. The first valid terminal decision wins. Only the same actor retrying the same action and normalized reason can replay a terminal result; all conflicting decisions return HTTP 409. Transactions persist the request and its audit event together. Approval and its grant additions are atomic, with synchronous runtime publication before a successful response. If publication fails after commit, the caller receives HTTP 503 and can retry the same decision; committed history is retained.

The request baseline is historical evidence, not a replacement policy. Request records keep historical identifiers without cascading deletion. This phase implements model access only. Quota and rate limit requests, scheduled decisions, notifications, and a global approval inbox are not implemented by these endpoints.

The isolated PostgreSQL and MySQL lifecycle harness covers manager/approval permissions, idempotency, unchanged pending/rejected/withdrawn grants, stale baseline preservation, immutable key ceilings, immediate runtime publication, account/Project/model/manager revalidation, scoped history, and concurrent terminal decisions. Pure unit tests cover input normalization and decision validation.

Candidate visibility permits requesting access only; it does not grant invocation or expose provider configuration. Inactive Projects cannot request candidates. HTTP integration additionally verifies session/CSRF enforcement, unsupported fields and kinds, bound status/cursor/limit queries, and exclusion of granted models.
