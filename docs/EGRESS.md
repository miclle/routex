# Managed Egress

RouteX manages reusable outbound proxies separately from provider Connections. Each Connection selects `default`, `direct`, or a named `proxy`. The platform default is either a proxy ID or `null` for direct access. Migration 18 assigns existing Connections to the initially direct platform default. An explicitly direct Connection continues to bypass later default changes.

A disabled, missing, or invalid selected proxy makes the affected route unavailable. RouteX never falls back to direct access. Already admitted requests may finish on their existing transport; successful configuration changes prevent later admissions from using the previous selection.

## Supported transports

- `socks5`: SOCKS5 CONNECT with optional username/password authentication. Target addresses are resolved and validated by RouteX, then sent as numeric IPv4 or IPv6 addresses. Remote proxy DNS is not used.
- `https`: HTTP CONNECT over a TLS connection to the proxy, with normal proxy certificate/hostname verification and optional Basic proxy authentication. CONNECT uses the validated numeric target address. HTTPS requests still verify the target certificate against the original target hostname.

Plain HTTP proxies, SOCKS4, `socks5h`, custom CA uploads, and insecure TLS switches are not supported. Proxy authentication is sent only to the proxy; provider authentication is sent only through the resulting tunnel. Environment proxy variables and redirects are ignored or refused by the guarded HTTP client.

Both proxy and target DNS answers are checked independently. Mixed permitted/prohibited results fail closed; neither side can trigger a second unvalidated DNS lookup. Proxy tunneling tries the validated target addresses in resolver order, using a fresh proxy connection for each attempt. All proxy endpoint addresses may be attempted during TCP setup. IP address policy, metadata restrictions, HTTP restrictions, and cancellation follow [Credential Storage and Upstream Network Policy](SECRET_STORAGE.md).

`allow_private_egresses` is a bootstrap setting, defaulting to `false`. It accepts `${ROUTEX_ALLOW_PRIVATE_EGRESSES:-false}` in the example configuration. Enabling it allows private/loopback proxy endpoints without changing target policy. Private targets independently require `allow_private_upstreams`. Link-local, multicast, metadata, and other prohibited ranges remain blocked. Production network policy should also restrict the process's outbound access.

## Management API

All paths below are relative to `/api/v1/admin`. Mutation and diagnostic POST requests require the authenticated session's CSRF token, same-origin checks, JSON content, and strict request decoding. Unknown fields and trailing JSON are rejected.

| Method and path | Permission | Behavior |
| --- | --- | --- |
| `GET /egresses` | `egress.read` | List redacted configurations, last diagnostic, and associated providers |
| `POST /egresses` | `egress.write` | Validate transport and create; returns 201 |
| `PATCH /egresses/:egress_id` | `egress.write` | Compare revision, validate changed transport, and update |
| `POST /egresses/test` | `egress.test` | Diagnose an unsaved draft; optional existing `egress_id` supports keeping its encrypted authentication |
| `POST /egresses/:egress_id/test` | `egress.test` | Diagnose a saved revision without enabling it |
| `GET /egress-default` | `egress.read` | Read the default selection and revision |
| `PUT /egress-default` | `egress.write` | Set `{egress_id: string or null, etag}` |
| `GET /egress-options` | `providers.read` | Return selectable identity/status without endpoint or diagnostic details |
| `PATCH /connections/:connection_id/egress` | `providers.write` | Validate and save `{mode, egress_id, etag}` |

Proxy creation/update uses `name`, `kind`, `host`, `port`, optional `enabled`, `auth`, `test_target_base_url`, and `etag` for updates. `auth.action` is `keep`, `replace`, or `remove`. Only `replace` accepts `username` and `password`; both must be nonempty and no longer than 255 UTF-8 bytes. Username colons and credential line breaks/NULs are rejected. Omitting authentication defaults to keeping the current value; deletion is explicit. Keeping encrypted authentication is rejected when the proxy kind, host, or port changes, including draft tests, so a credential cannot be redirected to another endpoint.

Responses contain `auth_configured`, never a username, password, ciphertext, or root-key material. Encrypted authentication uses an authenticated reference containing the proxy ID and a fresh secret-generation ID. Replacement creates a new envelope; copying it between proxies or generations fails authentication.

Creation, changes to endpoint/protocol/authentication, and re-enabling repeat a real bounded transport check during save. A previous draft test is informative, not a reusable authorization token. Name-only edits and disabling do not require a fresh network check. The caller supplies an explicit permitted target base URL; RouteX appends `/models`. No external service is probed implicitly. Connection selection changes validate against that Connection's configured base URL before saving.

Stale revisions return 409. Invalid configuration returns 400. A completed save-time probe that cannot establish transport returns 422. Unavailable secret storage or runtime publication returns 503. A publication failure can occur after a durable configuration commit; clients must reread the current revision before retrying.

## Diagnostics

Saved diagnostics take `{etag, target_base_url}` or `{etag, connection_id}`. The Connection form additionally requires current `providers.write` authority and uses one enabled, verified provider credential with the protocol's authentication header. It rechecks Connection/credential state and authority before persisting results. An explicit target check sends no provider credential.

A diagnostic returns `transport_ok`, `api_ok`, optional `http_status`, total `duration_ms`, `stale`, and ordered stages:

`target_dns`, `proxy_dns`, `tcp`, `proxy_tls`, `proxy_auth`, `proxy_connect`, `target_tls`, `api`.

Each stage has `passed`, `failed`, `skipped`, or `not_applicable` status, an optional measured duration, and a generic error code. Literal addresses do not claim a DNS measurement. Direct connections have no proxy stages. SOCKS5 has no proxy TLS stage. HTTPS proxy authentication is inferred from CONNECT success/failure and has no independent duration; its work is included in the CONNECT measurement. The API measurement begins after connection acquisition, while total duration includes setup and a bounded response read. Measurements may overlap where one protocol operation includes another.

Receiving an HTTP response proves transport reachability. Only a successful 2xx response with a successful bounded body read passes the API check; a 401 can establish transport while failing API authentication. Checks have a ten-second deadline. They retain neither response bodies nor raw peer errors. Last diagnostic state describes that check only; it is not continuous health monitoring, average latency, supply verification, or permission to enable a credential. Results from a superseded configuration return `stale: true` and do not replace the current diagnostic metadata.

## Runtime publication

Runtime snapshots resolve inherited selections and decrypt proxy secrets before publication. Gateway requests use immutable guarded clients, with no new proxy-secret or database read on the runtime hot path. Provider discovery and credential verification use the same selected transport policy.

Transport revisions include the effective default selection, Connection identity/base URL, and actual proxy configuration and encrypted secret generation. A failed new route build cannot authorize a last-valid route whose transport policy no longer matches. Local mutations advance an admission generation under a shared admission/configuration lock before acknowledging success. A request selected before the change must recheck that generation before durable admission. Runtime publication remains single-process; background refresh handles external changes according to the existing bounded authorization lease in [Runtime Publication](RUNTIME.md).

## Validation

Focused tests use controlled local HTTP/TLS/SOCKS5 peers and injected DNS/dial functions; no provider account or external probe is required. They cover independent endpoint policy, IPv4/IPv6 numeric tunneling, proxy and target TLS names, credential separation, authentication failure, canceled/stalled negotiation, redirects, measured diagnostic states, encrypted storage, and admission ordering.

The serialized PostgreSQL/MySQL integration helper exercises encrypted CRUD and strict HTTP requests, ETag conflicts, initial direct routing, default proxy routing for ordinary/SSE calls, disabled-proxy rejection, explicit direct override, stale diagnostic rejection, and service restart. Full dual-database acceptance is recorded by the coordinating test pipeline; focused package tests alone are not a claim of database acceptance.
