# Local Development and Database Testing

Go and Vite run on the host to preserve hot reload and debugging, while Docker Compose manages the databases. Install Go, Node.js, Docker Engine, and Docker Compose before starting. The database images use PostgreSQL 18 and MySQL 8.4.

## Start Development with PostgreSQL

```bash
go tool task install
go tool task db:up
go tool task dev
```

`db:up` waits for the database to become healthy. On its first run, `dev` creates `cmd/routex/config.local.yaml` from the example; it preserves existing configuration files. If your local configuration still uses the old port, update it using the table below or set `DATABASE_URL`. To use that variable, the local `dsn` field must retain the environment variable expression from the example.

| Setting | PostgreSQL (default) | MySQL (optional) |
|---|---|---|
| Start command | `go tool task db:up` | `go tool task db:mysql` |
| Address | `127.0.0.1:15433` | `127.0.0.1:13306` |
| Database | `routex` | `routex` |
| User | `routex` | `routex` |
| Development password | `routex-local` | `routex-local` |
| Compose profile | Default | `mysql` |

These fixed credentials are for local development only. Database ports bind exclusively to loopback. If a port is occupied, change the Compose mapping with `ROUTEX_POSTGRES_PORT` or `ROUTEX_MYSQL_PORT` and update the application's `DATABASE_URL` accordingly. The port variables do not automatically change existing application configuration.

```bash
# Use MySQL with a config.local.yaml that supports these environment variables.
go tool task db:mysql
ROUTEX_DB_DRIVER=mysql \
DATABASE_URL='routex:routex-local@tcp(127.0.0.1:13306)/routex?charset=utf8mb4&parseTime=True&loc=UTC' \
go tool task dev
```

The application listens on port 9000 and Vite on port 5173 by default. Override them with `ROUTEX_HTTP_PORT` and `ROUTEX_VITE_PORT`, respectively. Development services do not scan for or terminate processes belonging to other projects.

## Runtime Bootstrap and Event Storage

Before opening the HTTP listener, RouteX applies database migrations, publishes the initial in-memory gateway runtime, and opens its durable call-event buffer. A failed initial publication or unavailable buffer stops startup. `SIGINT` and `SIGTERM` stop new HTTP requests and allow active requests up to 15 seconds to finish before canceling their upstream work. The runtime publisher and recorder stop after HTTP shutdown, then the database pool closes. Interrupted admissions retain their durable fallback facts for recovery.

The optional `event_queue_path` bootstrap setting defaults to `data/calls.db` relative to the directory containing the supplied configuration file. An explicit relative path uses the same directory; an absolute path is used directly. Values expand `${NAME}` and `${NAME:-fallback}` after YAML parsing, as the other string settings do. The example supports `ROUTEX_EVENT_QUEUE_PATH`; separate configuration directories therefore get separate default buffers. Temporary lifecycle-test configurations keep their buffers inside the test's temporary directory.

The buffer also holds the current minute's RPM ledger and active concurrency leases. An initialized database is bound to its journal identity: restore the SQL database and matching persistent file together. Replacing or deleting an established buffer is not a supported quota reset; startup refuses a missing or foreign journal. Run one gateway process per installation.

`trusted_proxies` is an optional YAML list of literal IPv4/IPv6 addresses or CIDRs; each string expands environment variables after parsing. Its default is empty, so forwarded headers are ignored and socket addresses determine IP policy. When a direct peer is trusted, RouteX requires one valid `X-Forwarded-For` chain and selects the nearest untrusted hop. Trust only your actual reverse proxy networks; `Forwarded` and `X-Real-IP` do not override this source. See [Resource limits](RESOURCE_LIMITS.md) for supported policy fields and recovery constraints.

Keep the buffer's directory on a persistent writable volume, including in container deployments. The file is private to its OS user and exclusively locked by one RouteX process; only one gateway process per installation is supported. Preserve it across application restarts so completed and interrupted call facts can be replayed after database recovery. Deleting it without restoring the matching backup loses undelivered records and prevents normal startup. The buffer is separate from the database volumes managed by Compose.

## Stop Services, Inspect Status, and Preserve Data

```bash
go tool task db:status
go tool task db:down
```

`db:down` stops and removes this project's containers and network while preserving the PostgreSQL and MySQL named volumes. Existing data remains available when the databases restart. Do not add `--volumes` or `-v` during routine shutdown: those options delete the development databases. Before changing a database image's major version, back up the data and follow the database's upgrade procedure. When running multiple checkouts concurrently, assign each a different `COMPOSE_PROJECT_NAME` and different host ports.

## Automated Tests

```bash
go tool task check
go tool task test
go tool task test-integration
go tool task test-auth-lifecycle
go tool actionlint
```

`test` runs Go tests with the race detector, frontend behavior tests, development process lifecycle tests, and production asset tests. `test-integration` starts PostgreSQL and MySQL from `compose.test.yaml`, runs uncached Go tests with the race detector, and removes that run's containers and network. CI uses the same entry point. Each run uses a unique Compose project, random loopback ports, and temporary in-memory storage; it does not read or modify development database volumes. Failed runs print test database container logs, and cleanup failures cause the command to fail.

The script passes the test database addresses through these environment variables:

- `ROUTEX_TEST_POSTGRES_DSN`
- `ROUTEX_TEST_MYSQL_DSN`

Ordinary `go test` runs skip database-dependent tests when these variables are absent. If you provide them manually, use dedicated test databases: the tests write data and must never target development or production databases. The integration script generates its own DSNs instead of reusing the caller's values for these variables.

Database integration tests validate the migrations and business behavior implemented so far, including controlled ordinary/streaming gateway calls and cancellation. The process lifecycle suite also covers encrypted provider persistence, Key confirmation/revocation, inference, and call records across restarts. These results do not establish acceptance for quotas, real external providers, immutable runtime publication, or durable event buffering.

## Authentication and Process Restart Acceptance

`go tool task test-auth-lifecycle` uses another set of isolated empty databases and a temporarily compiled Go binary. It verifies the following sequence on both PostgreSQL and MySQL: initialize an empty database, read the session, stop the server process, restart it on the same port, confirm that the original session remains valid, log out, confirm that the old cookie receives HTTP 401, and log in again. Persistence is tested through real HTTP requests and actual process restarts. Write requests include a same-origin `Origin` header, and logout uses the CSRF token returned by the server.

This test does not require Vite, read local application configuration, or share storage with `test-integration` or development databases. The server listens on a random loopback port. Completion, failure, and termination signals trigger cleanup of the test's child processes, temporary directory, containers, and network. Logs do not print passwords, cookies, or hashes. Missing dependencies, startup failures, and failed assertions return a nonzero exit code. CI executes this entry point as a separate step.

## Node 26 test compatibility

When Node 26 exposes experimental Web Storage globals without a storage file, jsdom storage tests may fail before application behavior is exercised. On that host use `NODE_OPTIONS=--no-experimental-webstorage go tool task test`; this restores the test DOM's storage implementation. Do not disable the assertions or persist credentials in browser storage. Vite test servers each use a temporary private cache directory so parallel suites cannot rename one another's dependency cache.
