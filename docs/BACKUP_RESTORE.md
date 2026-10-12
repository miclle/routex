# Cold backup and restore

This procedure covers a stopped, single-node RouteX installation, using the same
RouteX executable and compatible PostgreSQL or MySQL tools for capture and restore.
It does not provide a production backup CLI, online snapshots, database-engine
upgrades, migration rollback, external-store recovery or an RTO/RPO guarantee.

## Recovery unit

Keep the following together as one recorded checkpoint:

- A native dump of the complete RouteX SQL database, including migration receipts,
  installation identity, authentication, encrypted configuration and call history.
- The matching private bbolt call journal from the effective `event_queue_path`.
  It contains durable call facts, quota history, RPM state and admission recovery
  records. Relative paths resolve against the supplied configuration's directory.
- The exact RouteX executable and its hash, database/tool versions, capture time,
  and the non-secret bootstrap configuration needed to identify this checkpoint.

Keep required encryption root material in separately protected recovery storage.
SQL contains encrypted envelopes, not their bootstrap roots. Preserve the exact
root identities, legacy mapping and material required by this checkpoint; do not
generate replacement roots during restore. Historical backups can require roots
retired from the live installation. See [Secret recovery](SECRETS.md#receipts-rollback-and-recovery).
Do not place root bytes, passwords, Sessions or Keys in a checksum file or report.

S3 attachment bytes and Vault secret/authentication state are outside SQL and the
journal. Their configuration references and cleanup intent are retained in SQL;
the external objects are not. Coordinate their separate recovery before reopening
an installation that depends on them. A local SQL/journal restore does not prove
external object availability or recover external revocations.

## Establish a cold checkpoint

Stop incoming traffic, then stop the exact RouteX process through its deployment
supervisor. Wait for shutdown and process exit, including request drain, durable
workers and journal closure. Keep the database running for the native dump.
Prevent other RouteX processes and administrative writers from changing this
database or journal until both captures finish. A database transaction or a
matching journal identity alone cannot establish that two independently timed
copies are a consistent checkpoint.

Do not copy an open bbolt file with `cp`, delete the journal, clear installation
metadata or reset quota tables to make a backup start. A missing or foreign
journal must remain a startup failure. Never run two restored/original gateway
processes against the same installation.

The examples below run in one shell after that cold boundary. Replace every
placeholder with an explicitly reviewed path or database target. Use a private,
existing backup parent; the new directory must not be the live data directory.
The examples require `shasum`, native database clients and ordinary shell tools.
They do not stop services or choose database targets for you.

```bash
set -eu
umask 077
BACKUP_PARENT=/private/backup-parent
ROUTEX_BINARY=/path/to/qualified/routex
JOURNAL_PATH=/path/to/installation/data/calls.db
BACKUP_DIR=$(mktemp -d "$BACKUP_PARENT/routex-backup.XXXXXXXX")
chmod 700 "$BACKUP_DIR"
```

### PostgreSQL capture

Use a client version compatible with the source server. Supply authentication
through a private `PGPASSFILE` or another existing PostgreSQL authentication
mechanism, rather than a password in command arguments. Record the database
server version separately. This dump covers the application database; database
roles and deployment-level privileges must be provisioned separately.

```bash
export PGHOST=source-db.example.invalid
export PGPORT=5432
export PGUSER=routex_backup
export PGDATABASE=routex
export PGPASSFILE=/private/source-postgres.pgpass
pg_dump --format=custom --no-owner --no-acl \
  --file="$BACKUP_DIR/database.pgcustom"
pg_dump --version > "$BACKUP_DIR/database-client-version.txt"
DUMP_NAME=database.pgcustom
```

### MySQL capture

Use the supported MySQL tools for the source server. A private option file holds
the explicit source host, port, user and password under `[client]`; keep it
outside the backup bundle and shell history. Do not use `--databases`, so the
dump can be restored into a separately named validation database. RouteX remains
stopped even though the dump also requests a transactional snapshot.

```bash
SOURCE_MYSQL_CNF=/private/source-mysql.cnf
SOURCE_DB=routex
mysqldump --defaults-extra-file="$SOURCE_MYSQL_CNF" \
  --single-transaction --no-tablespaces --set-gtid-purged=OFF \
  "$SOURCE_DB" > "$BACKUP_DIR/database.sql"
mysqldump --version > "$BACKUP_DIR/database-client-version.txt"
DUMP_NAME=database.sql
```

### Complete the capture

After the selected native dump succeeds, copy the still-closed journal and the
exact executable. Keep failure output private. If any command fails, retain the
partial files for diagnosis and do not label that directory a completed backup.
Record the reviewed database server version and bootstrap references alongside
the capture time; retain bootstrap secrets separately.

```bash
test -s "$BACKUP_DIR/$DUMP_NAME"
test -s "$JOURNAL_PATH"
cp -p "$JOURNAL_PATH" "$BACKUP_DIR/calls.db"
cp -p "$ROUTEX_BINARY" "$BACKUP_DIR/routex"
chmod 600 "$BACKUP_DIR/$DUMP_NAME" "$BACKUP_DIR/calls.db"
chmod 700 "$BACKUP_DIR/routex"
date -u '+%Y-%m-%dT%H:%M:%SZ' > "$BACKUP_DIR/captured-at.txt"
(
  cd "$BACKUP_DIR"
  shasum -a 256 "$DUMP_NAME" calls.db routex \
    database-client-version.txt captured-at.txt > SHA256SUMS
)
```

The dump and journal are sensitive even when individual stored credentials are
encrypted. Protect the complete bundle in transit and at rest with the existing
backup storage controls. Checksums detect byte changes; they do not authenticate
an untrusted bundle or prove cold consistency. Keep a trusted copy of the hashes
with the checkpoint record. The original installation may resume only after the
complete checkpoint has been captured and its ownership is unambiguous.

## Restore into an isolated target first

Use a fresh, empty database and a new private journal directory. Explicitly select
the restore target and verify it is not the original database. Do not drop or
overwrite the live database or journal. Use the same server major version for
this drill; test engine upgrades separately.

Before starting RouteX, isolate the target from public traffic and original
Vault, S3, SMTP and inference endpoints. Durable workers can act before the HTTP
listener opens, so listener isolation alone is insufficient. Keep the original
installation's database, journal and process untouched. Provision the database
account and empty target through the deployment's existing administration tools.

Verify the trusted checkpoint hashes before importing or executing its files:

```bash
BACKUP_DIR=/private/backup-parent/routex-backup.CHECKPOINT
RESTORE_PARENT=/private/restore-parent
RESTORE_DIR=$(mktemp -d "$RESTORE_PARENT/routex-restore.XXXXXXXX")
chmod 700 "$RESTORE_DIR"
(cd "$BACKUP_DIR" && shasum -a 256 -c SHA256SUMS)
cp -p "$BACKUP_DIR/calls.db" "$RESTORE_DIR/calls.db"
chmod 600 "$RESTORE_DIR/calls.db"
```

For PostgreSQL, explicitly replace the source connection environment before
restoring. The target database must already exist and be empty; `--clean` is
deliberately unnecessary.

```bash
export PGHOST=isolated-restore-db.example.invalid
export PGPORT=5432
export PGUSER=routex_restore
export PGDATABASE=routex_restore_validation
export PGPASSFILE=/private/restore-postgres.pgpass
pg_restore --exit-on-error --no-owner --no-acl \
  --dbname="$PGDATABASE" "$BACKUP_DIR/database.pgcustom"
```

For MySQL, select a distinct private option file and an explicitly empty target:

```bash
RESTORE_MYSQL_CNF=/private/restore-mysql.cnf
RESTORE_DB=routex_restore_validation
mysql --defaults-extra-file="$RESTORE_MYSQL_CNF" \
  "$RESTORE_DB" < "$BACKUP_DIR/database.sql"
```

Create a private restore configuration with the correct driver, isolated target
DSN, loopback listener and explicit `event_queue_path` pointing to
`$RESTORE_DIR/calls.db`. Resolve the original required roots through the protected
bootstrap mechanism. The checked-in [configuration example](../cmd/routex/config.example.yaml)
shows environment references; keyring installations also require their exact
[keyring mapping](SECRETS.md#keyring-bootstrap). Do not reuse the example's
development database fallback. Keep the configuration private and do not print
its expanded values. Start the copied, hash-verified executable with that config:

```bash
RESTORE_CONFIG=/private/restore-config.yaml
"$BACKUP_DIR/routex" -c "$RESTORE_CONFIG"
```

Validate initialization and migration receipts, exact retained identities,
authorized Session behavior, captured revocations, encrypted configuration,
Call/Attempt history, quota usage and journal replay/deduplication before exposing
traffic. Record failures as failures; startup or HTTP200 alone is insufficient.
Stop and join this validation process before moving its journal or removing its
owned target resources. Reopening production is a separate deployment action.

### Backup age and revocation history

A backup restores facts only through its checkpoint. A Key or Session revoked
before capture must remain revoked after restore. Revocations, offboarding,
permission reductions and root retirement performed after capture can be absent
from that older database. Restoring it can revive older authority if traffic is
opened without reconciliation. Reestablish required current security reductions
through the authorized operational recovery process before reopening. Neither
hash equality nor journal identity proves current revocation freshness.

Do not use an older binary to reverse migrations. Prefer the captured executable
for validation, then perform a separately qualified forward upgrade or repair.
Do not bypass missing-root, journal-binding or quota-integrity failures to obtain
readiness.

## Dedicated local restore drill

```bash
ROUTEX_BACKUP_BINARY=/path/to/qualified/default/routex \
go tool task test-backup-restore
```

This dedicated entry point calls `scripts/test-backup-restore.sh`. It is separate
from ordinary tests and the complete integration matrix. Supply an absolute path
to the already qualified default production executable through
`ROUTEX_BACKUP_BINARY`; the drill does not build a substitute. Node and Docker
Compose are required. Native database clients run inside the pinned database
images. Cache `postgres:18-alpine` and `mysql:8.4` before the drill: its Compose
startup uses `--pull never`. Both drivers are exercised; there is no driver
selection argument.

`ROUTEX_BACKUP_OUTPUT` optionally selects a fresh canonical absolute private output
directory. Its existing parent must also be canonical, have no symbolic links,
belong to the current user, and grant no group or other permissions. Do not reuse
an existing run directory. The exercise captures only after the source
application has exited, restores into distinct databases and journal paths, and
checks retained Sessions, active/revoked Keys, exact Call/Attempt facts, finite
token quota and real controlled inline-credential inference. Separate restored
targets exercise missing and foreign journal refusal. Bootstrap material and
transient credentials are not backup-report fields. Private dumps and journals
remain sensitive recovery artifacts.

Application stdout has a finite 1 MiB limit, shared by the drill and its boundary
controls; stderr remains limited to 64 KiB. A diagnostic run recorded 73,497
stdout bytes against the previous 65,536-byte limit before readiness, triggering
owned termination and correctly refusing acceptance. The 1 MiB stdout limit uses
the existing finite command-output convention without changing SQL, HTTP, work
or cleanup bounds. Counts do not identify stdout contents or establish the cause
of the earlier run that lacked them. Only byte counts, limits and fixed failure
codes enter the result; raw application output remains transient and private.

Its scope is a controlled single-node restore drill, not a command for backing
up a production installation. Read the original exit status and retained
evidence, including cleanup failures, before reporting a pass.

## Controlled qualification (2026-10-12)

The actual PostgreSQL/MySQL drill passed with ten application births (five per
driver): three normal applications and two expected journal-refusal applications
on each driver. It verified the restored administrator and original Session,
active/revoked Keys, exact immutable Call/Attempt facts, finite quota continuity,
controlled native inference through an encrypted inline Credential, and refusal
of missing or foreign journals. Root observed original 70023 directly join zero.
Independent readback SHA-256:
`c63d7e464ff7ab8a07cf4e652563fcdc02afce76ad25f96b99212e46e6ebdd25`.
Independent physical closure SHA-256:
`2aee87ba868d85499606b570ce57c7692df80389d58fd22ab1e1445dc77306ba`.
All 55 owned workers, fourteen ports and project resources closed; source, binary
and the existing development process remained preserved.

The executed drill source floor was
`0ffe47e1459e4e7c0a8d9782a406bd426453a3104844eb05b9087d04ddc30b52`
(2,503 paths). The supplied qualified default binary was
`ce1a90cb286e37ec980d242a059105310484a467e8e174196b45a641c6263671`.
Its source floor was
`e811f8221f17467836e5d5cef73c235cbb1f6450ccda11c1e682793e6b3111c8`.
The drill source separately passed mandatory and complete ordinary checks.
Originals 69741 and 11970 remain failed attempts with separate positive physical
cleanup. The first has no established immediate cause; the second has the
count-only stdout-limit evidence recorded above. Neither failure is rewritten
by the later pass.

This acceptance covers controlled single-node SQL/journal restore only. It does
not establish production RTO/RPO, backup scheduling/retention, external Vault/S3
recovery, browser acceptance or full A20 release acceptance. Main adoption and
final main checking of this script/runbook delivery remain pending.
