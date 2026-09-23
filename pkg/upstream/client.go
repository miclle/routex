// Package upstream provides HTTP clients that enforce an outbound address policy.
package upstream

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	errURL      = errors.New("invalid upstream URL")
	errAddress  = errors.New("upstream address is not permitted")
	errRedirect = errors.New("upstream redirects are not permitted")
)

// ValidateBaseURL checks URL syntax and literal addresses without performing DNS
// lookups. NewClient additionally checks DNS results at connection time.
// Plain HTTP is allowed only in private mode for localhost or private/loopback
// literal addresses; private mode never enables metadata or multicast addresses.
func ValidateBaseURL(raw string, allowPrivate bool) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || raw != strings.TrimSpace(raw) || u.Opaque != "" || u.User != nil || u.Host == "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#") {
		return nil, errURL
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return nil, errURL
	}
	host := u.Hostname()
	if !validHostname(host) {
		return nil, errURL
	}
	if port := u.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return nil, errURL
		}
	} else if strings.HasSuffix(u.Host, ":") {
		return nil, errURL
	}
	ip, literal := parseIP(host)
	if literal && !allowedIP(ip, allowPrivate) {
		return nil, errAddress
	}
	localhost := strings.EqualFold(host, "localhost")
	if localhost && !allowPrivate {
		return nil, errAddress
	}
	if u.Scheme == "http" && (!allowPrivate || (!localhost && (!literal || (!ip.IsPrivate() && !ip.IsLoopback())))) {
		return nil, errAddress
	}
	return u, nil
}

// NewClient creates a client with a 30-second total timeout, standard TLS
// verification, no environment proxy, and no redirect following. Its transport
// validates every request and pins each connection to a validated DNS result.
// Callers may adjust Timeout for streaming while retaining the transport policy.
func NewClient(allowPrivate bool) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	return newClient(allowPrivate, net.DefaultResolver.LookupNetIP, dialer.DialContext)
}

type lookupFunc func(context.Context, string, string) ([]netip.Addr, error)
type dialFunc func(context.Context, string, string) (net.Conn, error)

func newClient(allowPrivate bool, lookup lookupFunc, dial dialFunc) *http.Client {
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           safeDial(allowPrivate, lookup, dial),
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
	}
	return &http.Client{
		Transport: &policyTransport{base: transport, allowPrivate: allowPrivate},
		Timeout:   30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errRedirect
		},
	}
}

type policyTransport struct {
	base            *http.Transport
	allowPrivate    bool
	bindDialContext bool
}

func (t *policyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL == nil || req.URL.User != nil || req.URL.Fragment != "" || req.URL.Opaque != "" {
		return nil, errURL
	}
	// Request paths may include provider query parameters. Base URLs may not.
	base := *req.URL
	base.RawQuery, base.ForceQuery = "", false
	if _, err := ValidateBaseURL(base.String(), t.allowPrivate); err != nil {
		return nil, err
	}
	// Host overrides can change an upstream virtual host after policy validation.
	if req.Host != "" && req.Host != req.URL.Host {
		return nil, errURL
	}
	if t.bindDialContext {
		req = req.Clone(context.WithValue(req.Context(), egressRequestContextKey{}, req.Context()))
		req.Header.Del("Proxy-Authorization")
	}
	return t.base.RoundTrip(req)
}

func (t *policyTransport) CloseIdleConnections() { t.base.CloseIdleConnections() }

func safeDial(allowPrivate bool, lookup lookupFunc, dial dialFunc) dialFunc {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		if network != "tcp" && network != "tcp4" && network != "tcp6" {
			return nil, errAddress
		}
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, errAddress
		}
		var ips []netip.Addr
		if ip, ok := parseIP(host); ok {
			ips = []netip.Addr{ip}
		} else {
			ips, err = lookup(ctx, "ip", host)
			if err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				return nil, errAddress
			}
		}
		if len(ips) == 0 {
			return nil, errAddress
		}
		// Reject a mixed public/private answer rather than relying on answer order.
		for _, ip := range ips {
			if !allowedIP(ip, allowPrivate) || (strings.EqualFold(host, "localhost") && !ip.Unmap().IsLoopback()) {
				return nil, errAddress
			}
		}
		for _, ip := range ips {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			// Dial the numeric address, never the hostname, to prevent a second DNS
			// resolution from rebinding the validated name to an internal address.
			conn, err := dial(ctx, network, net.JoinHostPort(ip.Unmap().String(), port))
			if err == nil {
				return conn, nil
			}
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("cannot connect to upstream")
	}
}

func parseIP(host string) (netip.Addr, bool) {
	ip, err := netip.ParseAddr(host)
	if err != nil || ip.Zone() != "" {
		return netip.Addr{}, false
	}
	return ip.Unmap(), true
}

func validHostname(host string) bool {
	if host == "" || strings.Contains(host, "%") {
		return false
	}
	if _, ok := parseIP(host); ok {
		return true
	}
	if len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(strings.TrimSuffix(host, "."), ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			switch {
			case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-':
			default:
				return false
			}
		}
	}
	return true
}

var nonPublic = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
}

func allowedIP(ip netip.Addr, allowPrivate bool) bool {
	if !ip.IsValid() || ip.Zone() != "" {
		return false
	}
	ip = ip.Unmap()
	if ip == netip.MustParseAddr("fd00:ec2::254") || ip == netip.MustParseAddr("168.63.129.16") {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() {
		return allowPrivate
	}
	if !ip.IsGlobalUnicast() || ip.IsLinkLocalUnicast() {
		return false
	}
	// Limit public IPv6 to global unicast; this also excludes NAT64 translation
	// prefixes that could encode otherwise prohibited IPv4 destinations.
	if ip.Is6() && !netip.MustParsePrefix("2000::/3").Contains(ip) {
		return false
	}
	for _, prefix := range nonPublic {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}
