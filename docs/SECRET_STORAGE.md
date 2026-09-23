# Credential Storage and Upstream Network Policy

## Recoverable Upstream Credentials

`pkg/secretstore` stores recoverable upstream credentials using envelope encryption. Platform API keys have a separate purpose and must continue to use irreversible verification hashes; this package does not turn platform keys into recoverable secrets.

Create a store with `secretstore.New(key)` using exactly 32 bytes of root key material. Keep this root key outside the application database, provide the same value after restarting the application, and back it up through an access-controlled process. Losing it makes stored upstream credentials unrecoverable. The package does not read environment variables or provision keys; application bootstrap owns those responsibilities.

`Store.Seal(reference, plaintext)` creates a fresh random 32-byte data encryption key and independent nonces for every call. AES-256-GCM encrypts the credential under that data key and wraps the data key under the root key. Both operations authenticate the immutable credential reference and their distinct purposes. Use the credential record's opaque, stable identifier as the reference. Copying ciphertext to another record, changing authenticated ciphertext, or opening it with the wrong root key fails.

The persisted value is an unpadded base64url encoding of a versioned JSON envelope. Version 1 contains the wrapped key and its nonce, the payload nonce, and the authenticated payload. It contains neither plaintext key material nor the credential reference. Unsupported versions and malformed values are rejected. Empty references and empty credentials are rejected. Errors contain no plaintext, keys, identifiers, or ciphertext. A store supports concurrent callers and does not retain a reference to the caller's root-key buffer.

The root key remains available to the running process through its cryptographic state. Envelope encryption protects stored records; it does not protect credentials from an attacker who can read application memory or execute code in that process. Temporary byte buffers are cleared where practical, but Go strings and runtime copies prevent a guarantee of complete memory erasure.

Changing the root key requires explicitly decrypting and re-encrypting every stored credential with a controlled migration and retaining the old key until that migration and recovery checks complete. Version 1 does not implement automatic root-key rotation or a multi-key keyring. To rotate an upstream credential, create a new credential record and encrypt against its new identifier rather than moving the old envelope to that record.

## Upstream URL Validation

`upstream.ValidateBaseURL(raw, allowPrivate)` performs syntax and literal-address validation without making DNS requests. Base URLs must use HTTPS by default. User information, query strings, fragments, invalid ports, scoped IPv6 addresses, and malformed hostnames are rejected. A path prefix is supported. Internationalized hostnames must use their ASCII representation.

Public mode rejects loopback, private, link-local, multicast, unspecified, documentation, benchmarking, carrier-grade NAT, and reserved address ranges. Special-use ranges follow a conservative policy based on the IANA [IPv4](https://www.iana.org/assignments/iana-ipv4-special-registry/) and [IPv6](https://www.iana.org/assignments/iana-ipv6-special-registry/) registries; some protocol-specific globally reachable exceptions are intentionally excluded. Public IPv6 is restricted to ordinary global unicast and excludes special-use and translation ranges that could embed a blocked IPv4 destination. IPv4-mapped IPv6 addresses are evaluated using their IPv4 address.

Private mode is an explicit deployment opt-in for trusted self-hosted upstreams. It permits private and loopback destinations over HTTPS. Plain HTTP is limited to `localhost` or private/loopback literal IP addresses; arbitrary HTTP DNS hostnames and public HTTP destinations remain rejected. `localhost` must resolve only to loopback addresses. Link-local and multicast destinations remain blocked, as do the known special metadata addresses `fd00:ec2::254` and `168.63.129.16`. Private mode grants access to internal services and therefore must only be enabled in an environment whose administrators and upstream configuration are trusted.

## HTTP Client Enforcement

`upstream.NewClient(allowPrivate)` validates each request, disables environment-configured proxies, refuses redirects, and preserves standard TLS certificate and hostname verification. Request-level query parameters are supported for native provider protocols, while credentials in URL user information and conflicting `Host` overrides are rejected. A rejected redirect never forwards the original authorization header to its target.

DNS resolution occurs immediately before a new connection. Every returned address must satisfy the network policy; a mixed public/private answer is rejected in public mode. The connection then dials a validated numeric IP directly, eliminating a second hostname lookup that could rebind the destination. TLS still authenticates the original hostname. Existing keep-alive connections remain connected to their previously validated destination.

The default total request timeout is 30 seconds, the TCP connection and TLS handshake timeouts are 10 seconds, and the response-header timeout is 30 seconds. Streaming callers may change the returned client's `Timeout` while retaining its guarded transport. Request contexts cancel DNS resolution, connection setup, and active requests. Do not replace the transport to customize timeouts without preserving its network policy.

These controls validate destination addresses; they do not replace an outbound firewall or protect against an explicitly allowed upstream acting as a proxy to another service. Do not log raw HTTP errors, request URLs, headers, or bodies that could include provider secrets.

## Verification

```bash
go test -race -count=1 ./pkg/secretstore ./pkg/upstream
```

Tests cover randomized envelope round trips, key ownership, wrong keys and references, tampering, malformed envelopes, concurrent store access, IPv4/IPv6 policy boundaries, DNS pinning and mixed answers, redirect rejection, ignored proxy environment variables, TLS hostname validation, and request cancellation. HTTP tests use local controlled servers or injected DNS/dial functions and do not require external network access or provider credentials.

## Managed proxy credentials

Managed egress uses the same envelope store for proxy username/password pairs, with an authenticated reference containing the proxy ID and a fresh secret-generation ID. Read APIs expose only whether authentication is configured. Proxy credentials are decrypted while building the runtime transport snapshot, and never forwarded as target headers. The independent proxy endpoint policy and transport/diagnostic contracts are documented in [Managed Egress](EGRESS.md).
