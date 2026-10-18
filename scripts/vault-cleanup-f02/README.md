# Controlled published cleanup acknowledgement fault

This opt-in source tooling creates a separate fresh PostgreSQL/Vault/gateway
namespace. It deliberately rejects the durable acknowledgement after one real
remote destroy. It is a controlled expected-failure/restart test, never a normal
cleanup acknowledgement, fleet-isolation proof or F28/A16 completion.

First qualify a fresh normal acknowledgement/replay using
`scripts/vault-cleanup-normal.py`, and a separate real PostgreSQL 18 safe-error
append with a typed 23514 refusal and checked rollback. Independent readback must
produce the exact metadata contracts consumed by this runner. Copy those two
metadata receipts into a new private directory (0700, current UID; files 0600,
single links) and supply their separately reviewed SHA-256 values:

```sh
python3 -B scripts/vault-cleanup-f02/run.py --exclusive-validation-lane \
  --normal-receipt /absolute/private/normal-receipt.json \
  --normal-receipt-sha256 REVIEWED_SHA256 \
  --logging-receipt /absolute/private/logging-receipt.json \
  --logging-receipt-sha256 REVIEWED_SHA256
```

A supplied digest binds bytes, not the authenticity or completeness of their
producer. This runner does not create the independent prerequisite receipts or
unlock the earlier coordinator. Missing or uncertain prerequisite qualification
must stop dependent work. The native clients accept no endpoint, SQL, database,
credential, UID or input-path override.

Every run rebuilds frontend assets, nine Linux artifacts and a unique image.
Compose uses four exact owned containers on an internal bridge, no host ports,
fresh PostgreSQL/Vault tmpfs data and a fresh private journal bind directory.
The same journal, root key, database and binary survive the intentional restart
within this run. No resource from an earlier run is reused. Bootstrap, Session,
AppRole, creation/command UUIDs and all other runtime inputs are newly generated.

The private frozen GORM installer adds only its named fixture check after
verifying the exact creation, absence of cleanup commands, retained released
checks and unchanged migration ledger. No released step or business table is
created or altered through AutoMigrate. Its extra check remains installed
through restart; it is not removed to make a replay succeed.

The original product cleanup must return 503 after a complete five-pair
AppRole/ownership/destroy metrics-and-audit bracket and the real typed 23514
acknowledgement refusal. The native read-only evidence command must preserve
pending, no FinishedAt, one claim audit, no observe audit and known_no_effect
false. After a positive original process join and same-binary restart, the
original natural deadline expires. The same Session refreshes CSRF/authority
and sends the identical token-free intent. Require 200/unknown/Running=false,
the unchanged durable pending tuple, two exact source generations with positive
closure/current lease proof and zero additional Vault events. No new cleanup
UUID, deadline reset, login, remote retry or synthetic clock is allowed.

All private raw responses, compiled inputs and original channels remain in a
new protected output directory. Fixed native artifact manifests are checked
before original tmpfs closure. Unknown identity, original join or terminal
preserves the topology. Independently verify every original process and exact
owned resource afterward; the producer terminal alone is not acceptance.

`installer` is Linux-only and uses a protected fixed native input path. The
PostgreSQL diagnostic preserves exact typed server errors by disabling GORM
translation only in that diagnostic; production translation is unchanged. Its
`settings-f02` mode performs only fixed identity/settings reads. Native pure
controls and actual-data parsers remain separate from the real fault flow.
