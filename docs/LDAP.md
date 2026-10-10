# Operation-local LDAP identity component

The `pkg/ldap` component authenticates and rechecks one directory
identity attribute for an operation. It does not enable an application login
method, save directory configuration, provision members, link accounts, assign
roles, enforce SSO or issue a Session. F03 and A15 remain Partial. Dependency
integration, formatting, compilation, controlled TLS race tests and
complete project testing pass. Application and real-directory acceptance remain
open; controlled protocol qualification does not establish either gate.

## Explicit identity and authentication flow

Configuration must choose exactly `entryUUID` or `objectGUID`. There is no
guessed default or arbitrary username/email identity attribute. The component
returns `Identity{DN, Attribute, Subject}`: the DN is transient, the attribute is
the canonical selected enum, and Subject is a fresh owned copy of exact opaque
bytes. No application binding or member authorization follows from this result.

A fresh connection performs verified LDAPS and four operations under the same
original deadline:

1. Bind the nonempty configured service DN and password.
2. Search the configured subtree with an escaped username, selecting only the
   configured identity attribute. Require exactly one complete entry within the
   parsed base, exactly one attribute and one valid value; copy the reviewed facts.
3. Bind that returned DN with the nonempty user password.
4. As the bound user, reread that exact DN using base-object scope, fixed
   `(objectClass=*)` and only the same identity attribute. Require the original DN,
   canonical attribute and identical opaque bytes before returning.

Both searches disable alias dereferencing, enforce a size limit of two, use a
finite server time limit and select only the configured attribute. Missing,
duplicate, multivalued or unexpected attributes, zero or multiple entries,
invalid values, changed DN/subject and post-bind ACL denial cannot authenticate.
There is no rebind or fallback after a denied reread.

The username filter contains exactly one `{username}` in an assertion value.
The maintained LDAP parser validates it, and `EscapeFilter` substitutes verbatim
validated username bytes. A username is never interpolated into a DN. Returned
DNs are parsed and conservatively checked against the base: attribute types are
case-insensitive, while attribute values remain exact.

Attribute names accept equal-byte-length ASCII case-insensitive matches only;
Unicode folding, attribute options, OID aliases and unrelated names are rejected.
The selected enum remains canonical. Subject bytes never normalize:

- `entryUUID` must be exactly 36 ASCII bytes in the standard hexadecimal/hyphen
  shape and must not be the nil UUID. Preserve the original letter case.
- `objectGUID` must be exactly 16 bytes and must not be all zero. Preserve byte
  order; do not convert directory bytes into another UUID representation.

A subject case or byte-order change fails the exact reread even if another
consumer could treat the representations as equivalent. [RFC 4530](https://www.rfc-editor.org/rfc/rfc4530.html)
defines `entryUUID` as single-valued, immutable and not user-modifiable for an
entry lifetime. [Microsoft's objectGUID definition](https://learn.microsoft.com/en-us/windows/win32/adschema/a-objectguid)
describes its system-assigned identity. Application ownership, binding lifecycle
and directory trust remain a separate contract, not an inferred feature.

There is no anonymous bind, plaintext LDAP, StartTLS, referral following, paging,
retry or connection pool. Passwords remain nonempty verbatim byte strings, never
trimmed. Fixed public error sentinels contain no remote diagnostic, username,
password, DN or subject material.

## Connector and verified trust

The caller must provide a guarded `DialContext` and endpoint policy. Policy is
checked freshly before the operation's single dial. The endpoint is one explicit
`ldaps://host:port`, without userinfo, path, query or fragment. The component does
not obtain a proxy or connector from the environment.

TLS verifies the server chain and target hostname, uses system trust unless
explicit roots are supplied, and requires TLS 1.2 or later. Configuration and
root pools are cloned; `InsecureSkipVerify` is rejected and the target hostname
owns `ServerName`. Session resumption caching and renegotiation are disabled.
Caller-provided TLS callbacks and key objects retain ordinary Go ownership
semantics. Application integration must retain the existing network-policy
boundary; this component grants no additional network authority.

## Finite I/O and parsing bounds

One parent-shortened ten-second context covers policy, dial, TLS handshake and
all four LDAP operations. Raw connection deadline and cancellation-close own
network I/O; LDAP calls are synchronous without an adapter request goroutine.

The complete raw TLS operation, including handshake traffic, permits at most
256 KiB incoming and 32 KiB outgoing. Decrypted LDAP has the same aggregate
bounds, at most 16 incoming frames and a 64 KiB maximum per frame. Complete
frames are admitted before maintained BER decoding: definite lengths only,
maximum depth 32 and 4,096 nodes per frame. No global parser limits are changed.
Message IDs and operation tags match the current request; response controls,
referrals and duplicate terminal messages are rejected. Mandatory DN, attribute,
SET and OCTET STRING wrappers are validated before consuming identity values.

Endpoints and DNs are bounded at 2,048 bytes; usernames at 256 UTF-8 bytes;
passwords at 4,096 bytes; and the configured filter at 4,096 bytes, with an
8,192-byte substituted-filter bound. The fixed identity values have the tighter
bounds above. These are protocol safety limits, not directory health, capacity
or deployment-compatibility claims.

## Honest closure boundary

Cleanup has a separate one-second observation allowance, without another LDAP
operation or renewed retry budget. Raw network I/O is closed before the maintained
client's synchronous `Close`. The component stops or joins its own cancellation
callback and rejects authenticated success after failed or late observed cleanup.
It never spawns an unjoined `Close` goroutine to bypass a blocked close.

The reviewed dependency API does not expose a join of every private reader and
timer worker. Its `Close` wait group tracks the close owner, and its internal
confirmation wait can time out. This component therefore claims owned raw I/O
closure and its own callback join, not a positive join of all dependency workers.
Requests use raw deadlines instead of dependency request timers; final `Close`
receives a finite timeout. A synchronous dependency `Close` cannot be preempted
by this adapter; no hard return guarantee follows from that timeout.

## Dependencies and controlled qualification

Source preparation uses maintained `github.com/go-ldap/ldap/v3` v3.4.15 and its
exact `github.com/go-asn1-ber/asn1-ber` v1.5.8 dependency. Both carry MIT licenses;
their original copyright and permission notices are retained in
`third_party/licenses/`. The linked Azure NTLM module also retains its MIT notice;
RouteX exposes only simple bind over verified LDAPS, not NTLM authentication.

Focused race testing passes 137 named results across 20 parent tests, including
independent verified-TLS peers, cancellation, exact sequence, stable UUID/GUID
values, post-bind reread, malformed responses and owned closure. Mandatory project
checking and complete Task pass, including Go race/coverage, all 6,104 frontend
cases in 228 files, development lifecycle and production assets. LDAP statement
coverage is 89.9%. The 2,214-path source/mode floor remains unchanged during these
gates. Two initial lint failures are retained; narrowly equivalent predicate,
fixture-close and deliberate nil-context negative-test corrections pass the
successor check and complete tests. This component phase adds no schema, saved
auth method, application route, authentication UI or real-directory acceptance.
