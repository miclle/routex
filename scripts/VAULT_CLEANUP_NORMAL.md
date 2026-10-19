# Controlled normal Vault cleanup

This opt-in source producer exercises one fresh published Credential cleanup
through real RouteX APIs, PostgreSQL or MySQL, and Vault. Run only in the exclusive heavy
validation lane:

```sh
go tool task test-vault-cleanup-normal -- --exclusive-validation-lane --driver postgres
# Run only after the preceding lane has fully closed:
go tool task test-vault-cleanup-normal -- --exclusive-validation-lane --driver mysql
```

It rebuilds production assets and four Linux binaries, then builds a new
run-specific image. A private Compose network contains only the selected database, an
anchor, Vault and the gateway. No host port is published. The database and Vault
use fresh tmpfs data; all passwords, root keys, tokens, AppRole material,
Sessions, UUIDs, runtime source and audit receipts are generated for this run.
No existing service is stopped or reused. Private output is retained under
`~/.local/share/routex/acceptance/normal-vault/`, outside temporary storage. After
verified resource closure, the exact fresh image and four checked binaries are
removed; source bindings, raw observations and failed outcomes remain retained.
The omitted driver preserves the PostgreSQL default. Unknown or extra profile
arguments refuse before creating output. The selected driver is compiled into
both native commands and fixes their service, account and database names; input
JSON never selects a driver, endpoint or namespace. MySQL uses UTC timestamp
decoding and finite connection/read/write deadlines. Each driver requires its own
complete original run and independent readback.

The native client runs as the compiled nonroot host UID. A separate actual
AppRole wire preflight precedes original product setup, reviewed integration
and storage policy creation, and a real Provider/Credential creation. Before
cleanup, a bounded explicit read-only GORM projection captures the original
creation identities and proof inputs. A retained reference must refuse the
preview. Logical deletion uses the exact Credential metadata ETag and API.
Only the fresh eligible review authorizes the original cleanup UUID/reason.

The complete original audit append and status family bracket must agree with
one AppRole login, one version-one ownership read and one destroy. The original
200 acknowledgement is checked separately from the maintained native read-only
SQL evidence command. The identical token-free API replay must return the
original response without any additional Vault audit event. Original creation
history remains unchanged. No mutation transport retry or redirect is allowed.

Original input, HTTP responses, SQL expectations and raw audit stay in a new
protected output directory. Failures remain failures; unknown native identity
or closure preserves the namespace. The producer reports recorded process and
owned topology closure, not a complete descendant census. Independent readback
is still required; a producer terminal alone is not acceptance.

This is normal-path source tooling. It does not inject the F02 post-effect
acknowledgement fault, exercise restart, qualify external deployment isolation,
prove application adoption, register deferred migrations or complete F28/A16.
