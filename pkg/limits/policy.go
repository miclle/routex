// Package limits defines supported admission policies and trusted network identity.
package limits

import (
	"errors"
	"net/netip"
	"sort"
)

var ErrInvalid = errors.New("invalid admission policy")

const MaxInteger int64 = 9007199254740991

type Policy struct {
	RPM         *int64   `json:"rpm"`
	Concurrency *int64   `json:"concurrency"`
	IPMode      string   `json:"ip_mode"`
	IPRanges    []string `json:"ip_ranges"`
}

func Normalize(p Policy) (Policy, error) {
	for _, value := range []*int64{p.RPM, p.Concurrency} {
		if value != nil && (*value < 0 || *value > MaxInteger) {
			return Policy{}, ErrInvalid
		}
	}
	if p.IPMode == "" {
		p.IPMode = "none"
	}
	if p.IPMode != "none" && p.IPMode != "allowlist" && p.IPMode != "denylist" {
		return Policy{}, ErrInvalid
	}
	if len(p.IPRanges) > 128 || (p.IPMode == "none" && len(p.IPRanges) > 0) || (p.IPMode != "none" && len(p.IPRanges) == 0) {
		return Policy{}, ErrInvalid
	}
	unique := map[string]bool{}
	for _, raw := range p.IPRanges {
		prefix, err := ParseNetwork(raw)
		if err != nil {
			return Policy{}, err
		}
		unique[prefix.String()] = true
	}
	p.IPRanges = []string{}
	for value := range unique {
		p.IPRanges = append(p.IPRanges, value)
	}
	sort.Strings(p.IPRanges)
	return p, nil
}
func ParseNetwork(raw string) (netip.Prefix, error) {
	if addr, err := netip.ParseAddr(raw); err == nil {
		if addr.Zone() != "" {
			return netip.Prefix{}, ErrInvalid
		}
		addr = addr.Unmap()
		return netip.PrefixFrom(addr, addr.BitLen()), nil
	}
	prefix, err := netip.ParsePrefix(raw)
	if err != nil || prefix.Addr().Zone() != "" || prefix.Addr().Is4In6() {
		return netip.Prefix{}, ErrInvalid
	}
	return prefix.Masked(), nil
}
func Allows(p Policy, addr netip.Addr) bool {
	if p.IPMode == "none" || p.IPMode == "" {
		return true
	}
	if !addr.IsValid() || addr.Zone() != "" {
		return false
	}
	matched := false
	for _, raw := range p.IPRanges {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			return false
		}
		if prefix.Contains(addr.Unmap()) {
			matched = true
		}
	}
	return (p.IPMode == "allowlist" && matched) || (p.IPMode == "denylist" && !matched)
}
func Minimum(a, b *int64) *int64 {
	if a == nil {
		return b
	}
	if b == nil || *a < *b {
		return a
	}
	return b
}
func Narrower(parent, child Policy) bool {
	for _, pair := range [][2]*int64{{parent.RPM, child.RPM}, {parent.Concurrency, child.Concurrency}} {
		if pair[0] != nil && pair[1] != nil && *pair[1] > *pair[0] {
			return false
		}
	}
	return true
}
