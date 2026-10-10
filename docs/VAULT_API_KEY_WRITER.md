# Vault API-Key writer SDK

The `pkg/vault` API-Key writer is a dedicated restricted transport component.
Component qualification: mandatory check passed (`eeb6dd7c7b6937c24c8ff02d95d5a8e900f12c5f7ea79aaee583012cf305b77b`), complete Task passed (`0b5451fb3676c1fddbc6b2834c79c4dc231d1607e5e44557c44c729af6becec0`; 6,378 frontend tests in 237 files, Go race/coverage, development lifecycle and production assets) and
focused component race regression passed (`619174b116ef73d7dbc856f6f13b174fa12c8eeb4a7c9e306e7e58987b9af67e`). It does not deliver saved authentication identities,
Profiles, API-Key activation or a durable creation/recovery workflow. No real
Vault, application, database, browser or fleet acceptance is established here.

## Restricted capability

`NewAPIKeyWriter(descriptor, allowPrivate)` privately composes the guarded Vault
client. It exposes preparation and one write, nonsecret results, response-close
observations and idle transport disposal. It exposes no reader, listing, probe,
login, overwrite, delete or destroy operation.

`Prepare(resourceID)` performs local validation only. The caller supplies an exact
admitted, pre-generated personal or Project Key ID as one ASCII segment of at
most 128 bytes. The sole destination is
`<Prefix>/routex-api-key-<ResourceID>`. A SHA-256 plan binds the canonical
descriptor and exact resource ID under a separate API-Key domain. The plan is
bookkeeping, not authorization or a capability that can restore a prepared handle.

Copies of a prepared handle share one consumed/closed claim. Its `Close` prevents
future claims; it does not cancel or join a write already claimed. Invalid local
inputs do not consume the handle. Once claimed, every subsequent outcome consumes
it. A new handle for the same resource is not an approved recovery strategy.

## One CAS-zero write

`Write(ctx, writerToken, prepared, value)` sends one POST to the derived KV-v2
`data` path, with an empty query and exactly `options: {cas: 0}` plus the configured
single data field. It adds no Provider ownership marker. Token and bearer value
are verbatim, printable non-space ASCII, each 1–4096 bytes. The encoded request
body is capped at 32 KiB.

The writer uses the existing guarded DNS/address policy, normal TLS chain and
hostname verification, no environment proxy and no redirects. Private HTTP
requires the existing explicit deployment opt-in; TLS verification is retained.
Fresh HTTP/1 connections and absent request replay material prevent automatic
write replay. Failures never trigger retries, alternate destinations, reads,
compensating cleanup or plaintext fallback.

## Acknowledgement and uncertainty

An acknowledgement requires HTTP 200, JSON MIME, one bounded valid object and
normal KV-v2 write metadata: exact integer `version: 1`, `destroyed: false` and
`deletion_time: ""`. It requires explicit non-null metadata fields, rejects
malformed/nonempty errors and non-null authentication or wrapping results, and
admits additional bounded normal envelope/metadata fields without returning them.
The response limit is 64 KiB; decoded duplicate keys, depth over 16, invalid UTF-8
and unpaired UTF-16 escapes are rejected. A read-shaped payload is not required.

Only a successful response-body Close and a still-live original context permit
`acknowledged`, version 1 and `Write.Succeeded`. Every failure after entry into
HTTP `Do` is `unknown`, with version 0 and no success claim. `Consumed` records the
local claim; `Write.Attempted` records entry into `Do`, not network dispatch or
commit. A pre-Do rejection is `not_attempted`, including a consumed operation
canceled before entry. None of these outcomes proves another process did not
previously write the destination.

## Lifetime and application boundary

One parent-shortened context of at most ten seconds covers local preparation,
network I/O, bounded response reading and final acknowledgement admission. There
is no renewed stage or cleanup budget. Every returned body closes synchronously
once; a close failure or cancellation prevents acknowledgement. The SDK does not
expose an arbitrary caller transport or promise cancellation of a deliberately
blocking test body. Writer `Close` disposes idle connections only. Callers must
join in-flight writes; `ResponseCloseState` is observation, not a join.

Tokens, bearer values and raw responses are neither retained in plans/results nor
logged. Owned byte buffers are cleared where possible; Go strings and HTTP
internal copies prevent a guarantee of physical memory erasure. Errors expose
fixed diagnostic labels and observed HTTP status without remote error text.

A future service must separately authorize the owner and descriptor, prove the
remote Token's least privilege, enforce a durable command claim and preserve
unknown outcomes across restart. This SDK supplies no Profile/Provisioner choice,
saved identity, Reader-verified equality, retained ownership, Key activation,
runtime publication or durable recovery. See [secret storage](SECRET_STORAGE.md)
and [Vault integrations](VAULT_TOKEN_INTEGRATIONS.md) for separate contracts.
