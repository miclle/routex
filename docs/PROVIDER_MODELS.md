# Provider model availability and input capabilities

A provider model identifies one upstream model on one connection. Its availability
is independent of credential verification, platform-model authorization, routing
weights and prices. Disabling supply preserves those relationships and historical
facts while excluding it from new gateway route selection.

Image and PDF input support are explicit properties of each provider model. They
are never inferred from a protocol, provider, or model name. Both capabilities
default to false until an authorized operator reviews the upstream contract.

## Persistence and API

Frozen GORM migration 15 adds `disabled` and `e_tag` to `provider_models`. Frozen
GORM migration 22 adds `supports_image_input` and `supports_pdf_input`. Existing
rows remain enabled, with initial ETag `0`. Both manual entry and discovery retain
that availability default, while both input capabilities remain false. Rediscovery
does not enable an explicitly disabled model or invent capabilities. Released
migrations are unchanged.

Provider catalogue responses include `enabled`, `supports_image_input`,
`supports_pdf_input`, and `etag` for each provider model.
`PATCH /api/v1/admin/provider-models/:provider_model_id` requires a session,
`providers.write`, same-origin JSON, CSRF, and this body:

```json
{
  "etag": "0",
  "enabled": true,
  "supports_image_input": true,
  "supports_pdf_input": false
}
```

The ETag and at least one mutable field are required. Omitted mutable fields retain
their reviewed values, and unknown fields are rejected. A stale generation returns
409 without a write. Successful changes issue one new opaque ETag and one audited
event for the atomic configuration change. A matching unchanged write retries
runtime publication without creating another audit event. Success is acknowledged
only after publication. A publication failure may return 503 after the database
commit; reload the current state before retrying.

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

Model listings expose `input_capabilities` as a protocol-keyed map with stable
`image` and `pdf` values. A capability is advertised only when every currently
ready, enabled, positive-weight route for that model and protocol declares it.
This intersection prevents weighted routing from selecting an implementation that
cannot accept a capability shown to the caller. Zero-weight, disabled, or unready
routes do not expand the effective declaration. Capability changes participate in
the immutable runtime digest and publication boundary.

## Interface and verification

Provider model details display the current availability and accepted input types
in one reviewed section before price settings. Readers can inspect state;
authorized editors review three switches and explicitly save one atomic
configuration. Conflicts retain the draft but require a reload and review.
Uncertain writes require reconciliation before another publication attempt. Copy
is available in English and Chinese.

The acceptance suite checks existing-row upgrades and repeated migrations on both
databases, HTTP authority/CSRF/strict input/ETags/audits, actual disabled upstream
exclusion in prepared and direct routing, restoration, preserved weights, and
old-snapshot denial. Capability tests cover default-false migration, atomic ETag
writes, runtime digest publication, safe route intersection, management/member/
gateway responses, malformed metadata, read-only controls, duplicate submissions,
stale review, uncertain publication, and live language switching. Completed
execution evidence is recorded in [IMPLEMENTATION](IMPLEMENTATION.md).
