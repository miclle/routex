# Third-party license notices

RouteX uses these unmodified Go modules to read OLE compound-file containers:

| Module | Version | License | Copyright |
| --- | --- | --- | --- |
| `github.com/richardlehane/mscfb` | v1.0.8 | Apache-2.0 | Copyright 2013 Richard Lehane. All rights reserved. |
| `github.com/richardlehane/msoleps` | v1.0.3 | Apache-2.0 | Copyright 2014 Richard Lehane. All rights reserved. |

The original license texts are included in this directory. Keep them with
redistributions of RouteX. Neither module includes a separate upstream NOTICE
file. RouteX does not vendor or modify their source. Its BIFF8 and OOXML price
import decoders are independently implemented against the file format contracts.

## Local enrollment QR rendering

The frontend uses `qrcode.react` 4.2.0 under ISC, including its bundled QR Code
Generator under MIT. The unmodified notices are retained in
[`website/public/licenses/qrcode.react-4.2.0.txt`](../../website/public/licenses/qrcode.react-4.2.0.txt)
and included in the embedded frontend assets at `/licenses/qrcode.react-4.2.0.txt`.
QR encoding runs locally; no enrollment secret is sent to a QR service.

## OpenID Connect verification

`github.com/coreos/go-oidc/v3` v3.21.0 is used unmodified under Apache-2.0.
Its original license is retained in `go-oidc-APACHE-2.0.txt`; the module contains
no separate NOTICE file. Signature parsing also uses the existing
`github.com/go-jose/go-jose/v4` v4.1.4 dependency, and authorization-code requests
use the existing `golang.org/x/oauth2` v0.36.0 dependency.

## LDAP protocol decoding

`github.com/go-ldap/ldap/v3` v3.4.15 and
`github.com/go-asn1-ber/asn1-ber` v1.5.8 are used unmodified under MIT.
Their exact original license texts are retained in `go-ldap-MIT.txt` and
`asn1-ber-MIT.txt`. Keep these notices with redistributed RouteX binaries.
RouteX adds bounded verified-LDAPS transport and response admission around the
maintained protocol decoder; it does not vendor or modify either dependency.

The LDAP module also links unmodified `github.com/Azure/go-ntlmssp` v0.1.1
under MIT. Its original notice is retained in `go-ntlmssp-MIT.txt`. RouteX uses
simple bind over verified LDAPS; it does not expose NTLM authentication.
