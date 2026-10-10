package upstream

import (
	"context"
	"net"
	"net/url"
	"time"
)

// NewTCPDialer applies the same independent address validation and numeric DNS
// pinning as guarded HTTP transport. It never consults environment proxies.
func NewTCPDialer(allowPrivate bool) func(context.Context, string, string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	return safeDial(allowPrivate, net.DefaultResolver.LookupNetIP, dialer.DialContext)
}

// ValidateLDAPEndpoint checks an LDAPS target through the existing address policy.
// Connection-time DNS resolution remains guarded independently by the dialer.
func ValidateLDAPEndpoint(u *url.URL, allowPrivate bool) error {
	if u == nil || u.Scheme != "ldaps" || u.Path != "" || u.RawPath != "" || u.Port() == "" {
		return errURL
	}
	v := *u
	v.Scheme = "https"
	_, e := ValidateBaseURL(v.String(), allowPrivate)
	return e
}
