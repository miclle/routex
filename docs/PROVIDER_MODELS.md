# Provider model availability

A provider model identifies one upstream model on one connection. Its availability
is independent of credential verification, platform-model authorization, routing
weights and prices. Disabling supply preserves those relationships and historical
facts while excluding it from new gateway route selection.

## Persistence and API

Frozen GORM migration 15 adds `disabled` and `etag` to `provider_models`. Existing
rows remain enabled, with initial ETag `0`. Both manual entry and discovery retain
that default; rediscovery does not enable an explicitly disabled model. Released
migrations are unchanged.

Provider catalogue responses include `enabled` and `etag` for each provider model.
`PATCH /api/v1/admin/provider-models/:provider_model_id` requires a session,
`providers.write`, same-origin JSON, CSRF, and this body:

```json
{"enabled":false,"etag":"0"}
```

Both fields are required. Unknown fields are rejected. A stale generation returns
409 without a write. Successful state changes issue a new opaque ETag and an
audited `provider_model.enable` or `provider_model.disable` event. A matching
unchanged write retries runtime publication without creating another audit event.
Success is acknowledged only after publication. A publication failure may return
503 after the database commit; reload the current state before retrying.

## Gateway behavior

Configured weights still total 100 within a protocol. Selection excludes disabled
supply and draws among the remaining positive weights in their existing relative
proportions. Zero-weight candidates never receive traffic. If no enabled positive
supply remains, the gateway returns 503 before dispatch rather than selecting a
disabled implementation. No routing weights, model grants or prices are rewritten.

The prepared authorization snapshot independently carries provider-model state.
A local denial protects a committed disable while publication is retried; an old
routing snapshot cannot restore disabled supply. Already dispatched calls retain
their original route and assessment basis. The direct database path used by
controlled tests observes the same availability rule.

## Interface and verification

Provider model details display the current status and a separate availability
section before price settings. Readers can inspect state; authorized editors
review a switch and explicitly save. Conflicts retain the draft but require a
reload and review. Uncertain writes require reconciliation before another
publication attempt. Copy is available in English and Chinese.

The acceptance suite checks existing-row upgrades and repeated migrations on both
databases, HTTP authority/CSRF/strict input/ETags/audits, actual disabled upstream
exclusion in prepared and direct routing, restoration, preserved weights, and
old-snapshot denial. Pure routing tests cover disabled and zero-weight candidates.
Client tests cover exact reviewed writes, duplicate submissions, stale review,
uncertain publication, read-only controls and live language switching. Completed
execution evidence is recorded in [IMPLEMENTATION](IMPLEMENTATION.md).
