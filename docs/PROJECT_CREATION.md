# Project Creation and Initial Resources

Project creation retains the approved name, business description and complete
initial manager selection. No User or Team default policy is copied into a new
Project. Empty initial resource fields leave its model grants empty and limits
unlimited; zero is a finite cap, not an omitted value.

## Reviewed creation context

`GET /api/v1/project-creation-resources` returns only the enabled actor's reviewed
context, platform currency and independent model/limit/request capabilities.
`GET /api/v1/project-creation-models` returns bounded active public Model IDs/names;
it does not expose Provider, Credential, Member or global Project directories.
Model selection does not itself grant Personal access or native availability.

Enhanced `POST /api/v1/projects` binds a UUIDv4 `creation_id` and the context's
strong `If-Match`. It accepts the existing name/description/manager_ids plus one
mutually exclusive optional resource mode:

| Mode | Fields and effect |
| --- | --- |
| `initial_resources` | Optional model_ids, tokens_month, money_month/currency, rpm, tpm and concurrency, with a required reason. Model writes independently require projects.models.write; limit writes require projects.limits.write. |
| `initial_request` | The same requested values and a required reason. The actor must occur in the explicitly submitted initial manager selection. Submitted dimensions produce separate MODEL_ACCESS, QUOTA and RATE_LIMIT pending records. |

Omitted values preserve the creation defaults. Explicit scalar null, duplicate or
unknown fields, unsafe/noncanonical IDs and incompatible currency are rejected.
Money remains a decimal string through the browser and API. A changed context or
currency requires explicit review before a fresh intent may be dispatched.

## Atomic persistence and reconciliation

One governance transaction stores the Project, canonical manager relationships,
direct grants/policy or independent pending requests, typed audit and immutable
creation receipt. No existing Key scope expands. Pending requests do not apply
resources and continue through their existing independent approval workflows.

The receipt survives current resource changes without live foreign keys. The
same enabled actor may explicitly retry only the exact original UUID, body and
If-Match. Reconciliation reads recorded history and current authorized facts; it
never recreates a deleted/archived Project, restores managers/grants, changes a
policy or creates missing request children. Another actor or changed intent fails.

Enhanced results separate the nullable current Project, historical receipt and
committed flag from runtime_applied/application_status. Current authority is
renewed; lost access produces no current Project. Application requires the exact
published active Project, original manager generations and enabled users, model
set, normalized policy/revision and applicable denomination under a live local
lease. It does not prove native route availability or approval of pending requests.
Incidental name/description edits do not undo the recorded creation receipt.
Legacy requests without creation_id retain their original flat response.

## Interface and acceptance

The Project form retains the approved initial resource fields and optional
simultaneous-request checkbox. It uses local shadcn/Base UI controls, paired
resources translations and English by default. Managers absent from a bounded
search remain selected. Permission and currency reviews are independent.

Uncertain creation retains the exact dispatched intent and offers explicit retry;
Session renewal preserves that intent without replay. Current application claims
need renewed receipt reconciliation. Opening a created Project is explicit and
requires current authorized resource facts. Team creation remains separate.

Frozen GORM V45 creates the non-FK receipt table and portable uniqueness/check
constraints. The actual column mapping is frozen explicitly as review_etag.

Full local checks and tests passed 1181 Vitest cases in 78 files, Go race/unit,
development lifecycle and embedded production assets. Controlled production
browser acceptance covered direct initial resources, all three pending request
kinds, exact retry reconciliation, English/Chinese controls and two process
restarts with persisted Sessions. Native calls verified a finite Project cap,
independent approvals, immutable Key scope, grant revocation and zero extra
upstream dispatch on rejection. Later resource changes retained historical
receipts without restoring grants or policies. Loss of manager authority returned
no current Project and removed the browser's Project link.

The complete PostgreSQL/MySQL matrix passed under race detection (Handler
971.977 seconds; Service 7.062 seconds), including migration creation/upgrade,
partial-DDL repair, repeat/concurrent startup and lifecycle constraints. Both
drivers also passed real-process authentication and native gateway lifecycles, including restart, persisted Session/Key facts and revocation. Earlier
failed focuses corrected test-only audit cardinality/resource type and Project
attribution expectations, as well as the explicit GORM column mapping. They are
not successful acceptance evidence. Owned browser/process/Compose resources were
removed. No external-provider or full-platform acceptance is claimed.
