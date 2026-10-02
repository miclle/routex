# Project resource requests

Current enabled managers of an active Project can request additional model access
or finite monthly quota changes. Requests preserve the original baseline and the
explicit additions or quota patch. Pending requests never change grants or quota
enforcement. All endpoints require a console session; mutations also require the
existing Origin and CSRF protections.

## Authority and history

Model reviewers need `projects.models.write`; quota reviewers independently need
`projects.limits.write`. Manager status grants no approval authority, and no actor
can approve their own request. Approval revalidates the active Project, enabled
applicant and current manager relationship. An enabled applicant may withdraw
their own pending request after losing management or after Project deactivation.
Rejection and withdrawal do not change grants or limits.

Current managers and `projects.read_all` readers see both request kinds. Reviewers
without either authority see only the kind they can review; these predicates apply
before pagination. Former managers lose history access unless independently
authorized. A quota-only reviewer can open the existing Project Resource
configuration through a minimal `request_workspace_only` response containing
only the Project ID, name, description and status. It exposes no manager directory,
creator identity, timestamps or model grants and grants no global Project listing
authority. Withdrawal receipts do not expose current policy to a departed manager.

History accepts `status`, `cursor` and `limit`, defaults to 40 records and caps
the page at 100. Records are ordered by descending ID; responses contain `items`
and an optional `next_cursor`. A request's `kind` is `MODEL_ACCESS` or `QUOTA`,
and its status is `pending`, `approved`, `rejected` or `withdrawn`.

## API

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/api/v1/projects/:project_id/request-model-candidates` | Current manager; active ungranted models, literal `q` search, maximum 50 |
| GET | `/api/v1/projects/:project_id/request-quota-context` | Current manager of an active Project; reviewed monthly policy and currency context |
| GET | `/api/v1/projects/:project_id/requests` | Authorized, kind-scoped history |
| GET | `/api/v1/projects/:project_id/requests/:request_id` | Authorized detail; quota details include fresh approval and application context |
| POST | `/api/v1/projects/:project_id/requests` | Submit an explicit model addition or finite monthly quota patch |
| POST | `/api/v1/projects/:project_id/requests/:request_id/decision` | Approve, reject or withdraw |

### Model access

```json
{
  "request_id": "req_client_generated_unique_id",
  "kind": "MODEL_ACCESS",
  "model_ids": ["mdl_additional_model"],
  "reason": "Required for a new application workload"
}
```

Omitting `kind` retains the existing model-request API. Empty additions and models
already granted at submission are rejected. Callers submit only additional IDs,
never a complete desired grant set. Responses preserve `baseline_model_ids` and
`requested_model_ids` separately.

Approval revalidates every requested model's active status and adds those models
to the latest effective grants. It never restores a removed baseline grant or
removes a grant added after submission. Existing Project Key ceilings stay intact;
a Key gains access only when its existing ceiling includes an approved model.

### Monthly quota

Read `/request-quota-context` before submission. The response supplies
`current_quota`, `policy_etag`, `platform_currency` and a strong `review_etag`.
Send that exact composite validator as quoted `If-Match`:

```json
{
  "request_id": "req_client_generated_unique_id",
  "kind": "QUOTA",
  "quota": {
    "tokens_month": 100000,
    "money_month": "10.000000000000000001",
    "currency": "USD"
  },
  "reason": "Required for the reviewed monthly workload"
}
```

At least one quota field is required. Omitted fields stay unchanged; explicit
`null` is rejected. Tokens are non-negative safe integers and zero is a real cap.
Money remains decimal text, with at most 18 integer and 18 fractional digits.
Money requires the current platform currency; a Tokens-only patch must omit
`currency`. Unknown fields and fields mixed across request kinds are rejected.
The original monthly baseline, requested patch and baseline policy revision are
immutable history.

Quota details return current monthly values and platform currency. Pending
details also provide `approval_review_etag`, which binds the immutable request
intent to the complete current normalized policy, policy revision and platform
currency generation. Approval requires this exact quoted `If-Match` and a
non-empty reason. A changed policy or currency generation requires a fresh,
explicit review; preserving a draft does not accept a newer validator.

Approval patches only requested fields into the current full normalized policy.
It preserves rolling quotas, RPM, TPM, concurrency and IP restrictions, usage,
Project Key ceilings and all other policy fields. The complete approved finite
money policy must match the locked platform denomination. Saved approval records
retain the complete normalized policy, approved revision, original review
validator and decision intent hash.

Approved details expose `approved_quota`, `approved_policy_etag` and fresh
`runtime_applied` / `application_status`. `applied` requires the current policy,
local runtime publication, leases and denomination to match the saved approval;
`superseded` means a later policy has replaced it. `pending` means application is
not confirmed, not that a background approval job has been queued. History lists
do not claim live enforcement from a saved decision alone.

### Decisions and retries

```json
{
  "action": "approve",
  "reason": "Approved for this workload"
}
```

Actions are `approve`, `reject` and `withdraw`. Rejection always requires a reason;
quota approval also requires one. Existing model approval and withdrawal retain
their optional-reason contract. Reasons are business history and must not contain
credentials.

The caller-generated `request_id` identifies creation intent rather than the
generated `pmr_...` record. Identical authorized retries return that record; reuse
with a different actor, Project, body or review returns HTTP 409. The legacy model
creation digest remains compatible with historical receipts. Quota creation
replays keep the original body and reviewed header even if current policy changes.

The first valid terminal decision wins. Only the same actor with the same action,
normalized reason and original quota approval validator can replay it; conflicting
decisions return HTTP 409. Exact retries reconcile current publication and never
reapply a historical quota patch after a later direct policy edit. Approval
history therefore stays valid even when its policy is now superseded.

## Consistency and operational scope

Creation and decisions serialize with Project lifecycle, managers, grants and
account governance. Quota approval also serializes with admission and policy
updates, persists policy/request/typed audit atomically, and synchronously
publishes runtime state. A post-commit publication failure returns HTTP 503;
committed history remains durable and the original intent may be retried.

Frozen additive migration 36 extends existing request history through GORM;
historical rows default to `MODEL_ACCESS`. It introduces no live identity foreign
keys, cascading history deletion or startup migration against evolving entities.
Team approval/escalation, request-rate approvals, global approval inboxes,
scheduled decisions and request notifications remain unfinished scope.

## Web workflow

The existing Project Resource configuration retains request history, status
filters, cursor pagination and detail dialogs. Managers use separate model and
monthly-quota application actions. The monthly form shows reviewed current values,
uses blank fields to preserve a value, accepts zero and preserves exact decimal
money. Details separate the original baseline, requested patch, current policy,
saved approval and current application status.

Read and decision permissions stay independent for each kind. Self-approval is
disabled; inactive Projects disable new submissions and approvals while retaining
authorized rejection, withdrawal and history. Minimal quota-review workspaces
fetch only authorized request information. Actor-scoped caches and failed
reauthorization hide stale recipient data.

Uncertain mutations retain the original request ID, body and header for identical
retry while the dialog is open. Authorization uses the current Session CSRF for
the same captured actor; session rotation never changes the saved business intent.
Cancel remains available when no request is busy; dismissal does not report success or resolve uncertainty, and the parent retains
an unknown-outcome notice while refreshing real history. Conflict review requires
an explicit fresh review. English and Chinese labels, validation and notices use
the paired `projectRequests` namespace and English remains the default.

## Verification

The combined PostgreSQL/MySQL focused race suite passed in 184.141 seconds,
including every migration prefix through V36, existing model requests, quota
requests and the monthly-notification permission regression. It covers old
creation-digest compatibility, strict union/header validation, reviewed currency
generation, exact role/manager identities, per-kind pagination, no self-approval,
concurrent first-terminal decisions, departed withdrawal, disabled resources,
publication failure and exact replay, later direct edits, and restart. Controlled
native calls establish token and precise money enforcement after approval while
preserving existing settled use and conservative monetary reservation rounding.

Full check and test passed: 759 frontend cases in 57 files, Go race/unit tests,
Node checks, development lifecycle and embedded production assets. Focused UI
coverage includes minimum-permission workspace privacy, saved/pending publication
retry, same-actor CSRF rotation, immutable uncertain intent and terminal notices.
The 71-case focused suite also rejects malformed HTTP 200 quota receipts and
accepts valid terminal creation replays without changing legacy model responses.
Independent review found and fixed inherited GORM OR grouping; a generated-query
regression keeps Project, kind, status and cursor conditions conjunctive.

An owned production/PostgreSQL/browser fixture confirmed the existing application
and approval workflow, native enforcement at 10 tokens, direct policy 15 and an
original approval replay after restart retaining 15. English and Chinese details
separate baseline 5, saved approval 10 and current 15. A quota-only reviewer sees
only the authorized request workspace. The owned tab, binary, upstream, database
container and network were removed. The complete PostgreSQL/MySQL race matrix also passed
(Handler 504.548 seconds, Service 5.267 seconds), with owned resources removed.
External provider and full product acceptance remain separate.
