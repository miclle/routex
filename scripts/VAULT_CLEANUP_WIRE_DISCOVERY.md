# Controlled Vault cleanup wire discovery

Run `node scripts/vault-cleanup-wire-discovery.mjs --exclusive-validation-lane`
only after the shared heavy validation lane is free. Docker Compose creates one
fresh Vault server with a digest-pinned official image, no published ports,
internal network, nonroot UID, read-only root and volatile tmpfs data. There is
no database or RouteX process in this discovery scope.

Add `--app-role-reader` after `--exclusive-validation-lane` for an independent
fresh AppRole reader profile. Its original metrics/login/version-read/destroy/
metrics bracket has five complete pairs; Token mode has four. The native client
retains exact before/after audit snapshots and checks the complete status family
and appended audit bracket through `scripts/vault-cleanup-wire`.

A freshly compiled Linux arm64 client runs inside the captured server namespace
and reaches only its fixed loopback endpoint. The official image's declared
volumes are explicitly overridden with tmpfs. The server is freshly initialized
and unsealed. Independent writer, reader and
destroy-only tokens are minted for a unique dummy object; no host Vault token,
old database, key, port or runtime receipt is reused. The producer records one
original literal HTTP 204 destroy, the original read, version-one destroyed
metadata, raw before/after Prometheus responses and the original server audit.
The dummy destroy is never retried. Startup reads are separate from mutation.

Protected original create/read/metadata responses remain available for independent
readback. The fresh audit device also hashes the original literal version 1; its
returned hash must be compared with the audited version rather than decoding an
opaque audit string. Only the native terminal's fixed file manifest is read back,
with exact byte counts and SHA-256 values. Missing native completion preserves
the namespace for investigation.

All outputs, including bootstrap and scoped secrets, remain in a fresh private
directory. Only the result and directory path are printed. Do not publish that
directory. Failed, missing or uncertain results remain failed; unknown container
or network ownership prevents automatic cleanup. Exact original commands,
generation-bound stopped container metadata and resource closure require
independent readback after the producer exits.

This qualifies the bounded observed metrics/audit profile in controlled native
Token and AppRole preflight runs. It does **not** invoke the product cleanup API,
prove SQL durability, establish real deployment isolation, exercise restart/replay,
or complete F28/A16. Keep normal product acknowledgement and fault/restart
acceptance as subsequent independent gates.
