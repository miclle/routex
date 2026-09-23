# Token and monetary quotas

Version 19 adds quota policy storage and gateway admission to the [durable ledger foundation](QUOTA_LEDGER.md). This is a single-process enforcement implementation. Source tests, dual-database acceptance, browser evidence, and measured production capacity are separate checkpoints. Team/session quotas, default templates, reset-to-template, quota approvals, alerts, and distributed enforcement remain outside this slice.

## Policy API

Existing resource-limit GET/PUT endpoints accept these additional fields:

```json
{
  "tokens_5h": 1000000,
  "tokens_7d": null,
  "tokens_month": 10000000,
  "tpm": 100000,
  "money_month": "25.000000000000000001",
  "currency": "USD",
  "rpm": 60,
  "concurrency": 4,
  "ip_mode": "none",
  "ip_ranges": [],
  "reason": "Application resource policy"
}
```

PUT is a complete policy replacement. Clients must retain the full supported policy when editing one section; omitted fields become null/unrestricted at an aggregate or inherited at a Key. A strong `If-Match`, CSRF, same-origin validation, current resource authority, and nonempty reason are required. Stale edits return 409. Identical retries with the prior ETag, same actor, normalized policy, and reason retry publication without another audit event.

Integers are nonnegative safe JSON integers. Money is an exact nonnegative decimal string with at most 18 integer and 18 fractional digits; scientific notation and numeric JSON money are rejected. A finite monetary policy must use the current platform currency. Null money clears its local currency. Zero closes that allowance. Personal and Project Key overrides can only narrow their respective owner aggregate. Overlapping rotation credentials share the oldest immutable ancestor's policy and counters.

A personal call checks user plus Key accounts. A Project call checks Project plus Key accounts, independently of its creator or managers' personal accounts. Policy edits, grants, disabling, re-enabling, and Key rotation do not clear history.

GET adds `quota_usage`, with `activated`, `time_zone`, `coverage_start`, `as_of`, and `active`, `minute`, `five_hours`, `seven_days`, and `month` windows. Windows are null before activation. Each populated window has `covered`, `tokens_used`, `tokens_held`, `tokens_unknown`, `money_used`, `money_held`, and `money_unknown`. Money maps use explicit currency codes and decimal values. Active holds are separate from terminal window counters and must be included when presenting available allowance. An uncovered window is incomplete history, not a zero historical balance.

## Installation calendar

| Endpoint | Authority |
| --- | --- |
| `GET /api/v1/admin/quota-settings` | `system.read` or `limits.settings.write` |
| `PUT /api/v1/admin/quota-settings` | `limits.settings.write`; CSRF and `If-Match` |

The write body is `{"time_zone":"America/New_York","reason":"Organization calendar"}`. The response includes `time_zone`, `etag`, `activated`, nullable `coverage_start`, and `editable`.

The default is UTC. A valid IANA time zone can be selected while the recorder is available and before first quota activation. First native admission freezes a persisted activation intent, then activates the durable ledger. A crash between those steps is retryable and cannot occur after upstream dispatch. Once activation has started, changing the time zone returns 409. Startup checks an already activated journal against SQL configuration; it never silently substitutes UTC or reinterprets old months.

The ledger starts tracking every admitted native request, including unconstrained requests. Older accounts cannot use a finite window that overlaps time before coverage. Accounts created after coverage have a provably empty earlier lifetime. There is no historical-usage forgiveness or bootstrap from incomplete asynchronous reports. An uncovered finite window returns 503 until it is covered.

## Provider-model capacity

| Endpoint | Authority |
| --- | --- |
| `GET /api/v1/admin/provider-models/:provider_model_id/reservation-bound` | `providers.read` |
| `PUT /api/v1/admin/provider-models/:provider_model_id/reservation-bound` | `providers.write`; CSRF and `If-Match` |

A first read returns `configured:false` and ETag `"0"`. The write body is:

```json
{
  "max_input_tokens": 128000,
  "max_output_tokens": 16384,
  "evidence": "Verified native model capacity and provider contract",
  "reason": "Enable bounded text admission"
}
```

The protocol is derived from the provider connection. Both maxima must be positive safe integers. Evidence and reason are trimmed nonempty strings of at most 2,000 bytes. The stored revision, actor, reason, and normalized before/after audit are transactional. Publication failures are retryable with the same prior ETag and body.

This is an administrator's explicit capacity attestation, not an inferred property of a name, a discovered model, a tokenizer estimate, or a successful sample call. The attested input maximum covers all billable input including caches and tool schemas. The output maximum covers all billable generation including reasoning/thought tokens. Deployments must verify that their actual compatible upstream enforces the native contract. The gateway rejects a request above the attested output maximum. If authoritative actual usage exceeds a reservation, it persists the full debt and invalidates that bound revision. A corrected attestation needs a new revision.

## Supported native reservation shapes

| Protocol | Required positive output cap | Additional boundary |
| --- | --- | --- |
| `openai_chat` | `max_completion_tokens` | One choice; legacy `max_tokens` is insufficient; prediction and unknown extensions are rejected under a finite quota |
| `openai_responses` | `max_output_tokens` | Existing stateless ownership restrictions apply; hosted tools and unrecognized billing conditions are rejected |
| `anthropic_messages` | `max_tokens` | Version `2023-06-01`, no beta header; only the supported five-minute cache-write pricing shape |
| `gemini_generate_content` | `generationConfig.maxOutputTokens` or its supported snake-case form | One candidate; conflicting field aliases rejected; stateless generation has no explicit cache-write quantity |

Supported text inputs, opaque custom function schemas, and documented reasoning settings retain their native payloads. Hosted tools, non-text modalities, unsupported service tiers, one-hour/mixed cache writes, and unknown billing dimensions cannot receive a guessed bound or rate. These restrictions apply when a finite economic quota is present; unconstrained native behavior remains unchanged. An unconstrained request without adequate evidence is still recorded, with unknown bounds and independently assessed final usage.

The output-cap interpretation is based on the primary protocol documentation: [OpenAI token accounting](https://developers.openai.com/api/docs/guides/token-counting), [Responses reasoning limits](https://developers.openai.com/api/docs/guides/reasoning), [Messages thinking and cost](https://platform.claude.com/docs/en/build-with-claude/thinking-steering-and-cost), and [Gemini thinking limits](https://ai.google.dev/gemini-api/docs/generate-content/thinking). These caps include billable reasoning/thought output; a thinking-effort setting by itself is not a hard cap. Provider-specific compatibility still requires the capacity attestation described above.

The token reservation is the full attested input maximum plus the explicit native output cap. The money reservation maximizes every reachable tier and possible cache classification using the request's immutable rate/FX snapshot and conservative upward rounding. It never substitutes the ordinary input rate when another possible cache/tier rate is higher. Missing potentially applicable prices cause monetary admission to fail. A token-only policy can work without monetary pricing.

## Settlement and error behavior

The egress generation check remains outside the final admission section. Inside it, a policy read lock prevents a successful reduction acknowledgment from racing with admission under an earlier policy. Admission commits all aggregate/Key holds, RPM, concurrency leases, and pending call facts in one synchronous journal transaction before upstream dispatch.

Complete authoritative normalized input/output settles tokens. Complete supported pricing settles exact money using the captured basis. Each dimension is independent, and final evidence is assessed even when the request ended with cancellation or an error. Missing cache counters can leave money unknown while complete input/output tokens settle. Unknown values retain the captured conservative hold, or an explicit unbounded unknown when no bound was available. Neither becomes free usage.

| HTTP / code | Meaning |
| --- | --- |
| 400 `quota_request_unsupported` | A constrained request has no supported native cap/shape or exceeds the attested output maximum |
| 429 `quota_exceeded` | At least one aggregate or Key token/money allowance cannot cover the reservation |
| 503 `quota_bound_unavailable` | Missing or invalidated capacity evidence |
| 503 `quota_price_unavailable` | Required reservation rate/FX is unavailable |
| 503 `quota_history_incomplete` | A finite window overlaps untracked history |
| 503 `quota_usage_unknown` | That window contains an unbounded unknown dimension |
| 503 `quota_currency_mismatch` | Retained monetary usage/holds cannot be compared in the policy currency |
| 503 `event_buffer_unavailable` | Durable recording cannot accept the request |

Changing platform currency returns 409 while any finite monetary policy or live monetary hold exists. Historical nonzero amounts are never converted using a new FX table. A later policy in another currency can remain unavailable until its old-currency window expires. First activation intent also prevents a recorder-less service from claiming that an established ledger has no holds.

The internal legacy `AdmitGatewayCall` compatibility path can prepare format1 recording fixtures before quota activation. Actual native gateway dispatch always prepares user/Project and Key scopes and takes the quota-aware path, including its first request. Once format2 is active, direct admission without prepared quota scopes is rejected atomically. There are no public request fields that choose scopes, coverage dates, bounds, or settlement amounts.

## Acceptance boundaries

Focused tests exercise native cap classification, all four protocols, immutable bound arithmetic, exact money, independent finality, scope preparation before/after activation, format2 bypass rejection, policy reduction, retained unknowns, and restart recovery. `testQuotaLifecycle` is registered by the single PostgreSQL/MySQL harness and covers actual management HTTP, CSRF and permission negatives, configured calendar freezing, ETags, Project attribution, historical coverage, known tokens with unknown money, currency guards, and SQL acknowledgment/restart continuity.

The standalone ledger tests also kill real subprocesses after activation, admission, completion, and acknowledgment. The phase passed the full check and test suite with 361 Vitest cases, Go race coverage, development lifecycle checks, and production asset serving. The serialized PostgreSQL/MySQL lifecycle suite passed in 257.255 seconds, and both database process suites passed initialization, restart persistence, ordinary and streaming native inference, reporting, logout, and revocation. Production capacity measurements and the remaining Team/template/approval/alert scope are separate checkpoints, so this slice does not claim full F09/F17/F18 completion.
