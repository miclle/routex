# Saved certificate acceptance

Run one explicitly reserved database lane at a time:

```sh
go tool task test-vault-certificate -- --exclusive-validation-lane --driver postgres
go tool task test-vault-certificate -- --exclusive-validation-lane --driver mysql
```

The coordinator builds fresh frontend assets, Linux application and native
fixtures, generates independent server/client CAs, and starts pinned Vault and
database images with Docker Compose. It publishes no host ports. The gateway
receives its bootstrap configuration and one run-owned durable call-buffer directory; plaintext certificate material and
Vault control credentials are inaccessible to it through mounts.

The actual administration API saves separate named certificate identities, writes
and reads one probe, rejects the wrong named role and server CA, rotates the root
key through the normal observation window, retires it, and restarts the same
gateway container with only replacement key material. Its durable call buffer stays
on the same run-owned private directory across the restart; putting it on tmpfs
would intentionally violate its persisted storage identity contract. The original Session and
saved integration must survive. An identical historical Read dispatches no Vault
request; a fresh Read performs native certificate login and KV access. The writer
deliberately lacks destroy permission, so cleanup remains pending and is never
reported as acknowledged. All disposable Vault/database state is destroyed only
after bounded readback.

Private evidence is retained under
`~/.local/share/routex/acceptance/saved-certificate/`. Each run records source
hashes, original command outcomes, native response manifests, paired remote audit
identities, restart facts and owned resource closure. Missing, cancelled, failed
or incomplete readback cannot pass. Run-owned binaries/images are removed after
verified closure; unique evidence is retained. This is controlled saved-identity
acceptance, not external application adoption, fleet compatibility, native AI
completion or complete F28 acceptance.

Requires a nonroot macOS/Linux host, the project Go/Node dependencies, and Docker
support for Linux arm64 images. The native fixtures accept no arbitrary endpoint,
token, database command or external path. The explicit role and CA contracts
follow the official [certificate API](https://developer.hashicorp.com/vault/api-docs/auth/cert)
and [TCP listener configuration](https://developer.hashicorp.com/vault/docs/configuration/listener/tcp).
