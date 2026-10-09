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
