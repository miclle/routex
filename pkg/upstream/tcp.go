package upstream

import (
	"context"
	"net"
	"time"
)

// NewTCPDialer applies the same independent address validation and numeric DNS
// pinning as guarded HTTP transport. It never consults environment proxies.
func NewTCPDialer(allowPrivate bool) func(context.Context, string, string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	return safeDial(allowPrivate, net.DefaultResolver.LookupNetIP, dialer.DialContext)
}
