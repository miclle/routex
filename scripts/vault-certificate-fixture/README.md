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

## Controlled intermediate-chain workload identities

The additive `prepare-workload <private-input-directory>` mode uses the same
compile-bound run/source/UID/time and private-directory admission as `prepare`.
It creates fourteen PEM/key files and a hash manifest. Server and client trust use
independent root/intermediate chains. Client leaves carry one exact `spiffe://routex-<run>.invalid/vault/<role>` URI SAN and a distinct CN alias. An additional valid empty-CN leaf
records Vault 2.2 cert-auth alias refusal as HTTP 500; it is not a successful
workload login. Issuer private
keys remain in the preparation process. The outsider leaf uses a different trust
domain while retaining the trusted client issuer and the writer CN, so a matching CN cannot
replace the URI constraint. Existing direct-root fixtures
and their default command remain unchanged.

After the existing native discovery preflight initializes a fresh owned Vault,
the Linux `workload` phase configures named cert-auth roles constrained by
`allowed_uri_sans`. It checks writer and reader logins, missing client
intermediate, reader-as-writer and wrong trust-domain denial, plus the empty-CN alias incompatibility. Every case retains
one exact native audit request/response pair; no fallback role or KV request is
allowed. Login tokens remain transient and are never serialized. The enclosing
coordinator must capture `workload-terminal.json` and its hashed files before
tearing down the owned Vault. Run this phase before configuring the saved
identities through the application. It does not rotate the storage root.

`go test -race ./scripts/vault-certificate-fixture -run TestWorkload -count=1`
checks the three-level chains and URI identities with actual mutual TLS,
including missing client and server intermediate rejection. Real Vault audit and
application/browser readback remain separate from these tests. This controlled
cert-auth fixture does not certify SPIRE, Vault's SPIFFE auth backend, Kubernetes
TokenReview, JWT/OIDC issuers, external PKI, automated identity renewal or
application adoption.
