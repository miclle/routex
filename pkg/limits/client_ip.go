package limits

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

func ParseTrustedProxies(raw []string) ([]netip.Prefix, error) {
	result := make([]netip.Prefix, 0, len(raw))
	if len(raw) > 128 {
		return nil, ErrInvalid
	}
	for _, entry := range raw {
		prefix, err := ParseNetwork(entry)
		if err != nil {
			return nil, err
		}
		result = append(result, prefix)
	}
	return result, nil
}
func ClientIP(request *http.Request, trusted []netip.Prefix) (netip.Addr, error) {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		return netip.Addr{}, ErrInvalid
	}
	peer, err := netip.ParseAddr(host)
	if err != nil || peer.Zone() != "" {
		return netip.Addr{}, ErrInvalid
	}
	peer = peer.Unmap()
	isTrusted := func(addr netip.Addr) bool {
		for _, prefix := range trusted {
			if prefix.Contains(addr) {
				return true
			}
		}
		return false
	}
	if !isTrusted(peer) {
		return peer, nil
	}
	values := request.Header.Values("X-Forwarded-For")
	if len(values) != 1 || len(values[0]) > 1024 {
		return netip.Addr{}, ErrInvalid
	}
	entries := strings.Split(values[0], ",")
	if len(entries) == 0 || len(entries) > 16 {
		return netip.Addr{}, ErrInvalid
	}
	chain := make([]netip.Addr, 0, len(entries))
	for _, entry := range entries {
		addr, err := netip.ParseAddr(strings.TrimSpace(entry))
		if err != nil || addr.Zone() != "" {
			return netip.Addr{}, ErrInvalid
		}
		chain = append(chain, addr.Unmap())
	}
	for index := len(chain) - 1; index >= 0; index-- {
		if !isTrusted(chain[index]) {
			return chain[index], nil
		}
	}
	// A fully trusted chain has no authenticated client boundary.
	return netip.Addr{}, ErrInvalid
}
