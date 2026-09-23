package limits

import (
	"net/http/httptest"
	"net/netip"
	"reflect"
	"testing"
)

func ptr(value int64) *int64 { return &value }
func TestPolicyNormalizationAndNarrowing(t *testing.T) {
	policy, err := Normalize(Policy{RPM: ptr(0), Concurrency: ptr(2), IPMode: "allowlist", IPRanges: []string{"192.0.2.25/24", "192.0.2.0/24", "::ffff:127.0.0.1"}})
	if err != nil || !reflect.DeepEqual(policy.IPRanges, []string{"127.0.0.1/32", "192.0.2.0/24"}) {
		t.Fatalf("normalization: %v %+v", err, policy)
	}
	for _, invalid := range []Policy{{RPM: ptr(-1)}, {Concurrency: ptr(MaxInteger + 1)}, {IPMode: "bad"}, {IPMode: "allowlist"}, {IPMode: "none", IPRanges: []string{"127.0.0.1"}}, {IPMode: "denylist", IPRanges: []string{"example.invalid"}}} {
		if _, err := Normalize(invalid); err == nil {
			t.Fatal("invalid policy accepted")
		}
	}
	if Narrower(Policy{RPM: ptr(1)}, Policy{RPM: ptr(2)}) || !Narrower(Policy{RPM: ptr(0)}, Policy{}) {
		t.Fatal("inheritance/narrowing invalid")
	}
	if got := Minimum(ptr(0), nil); got == nil || *got != 0 {
		t.Fatal("zero became unlimited")
	}
	parent, _ := Normalize(Policy{IPMode: "allowlist", IPRanges: []string{"192.0.2.0/24"}})
	child, _ := Normalize(Policy{IPMode: "denylist", IPRanges: []string{"192.0.2.7"}})
	if Allows(parent, netip.MustParseAddr("198.51.100.1")) || Allows(child, netip.MustParseAddr("192.0.2.7")) || !Allows(parent, netip.MustParseAddr("::ffff:192.0.2.8")) {
		t.Fatal("IP predicate mismatch")
	}
	for _, raw := range []string{"fe80::1%en0", "[::1]", "192.0.2.1:80", "192.0.2.1/33", "::ffff:192.0.2.0/120", "192.0.2.1/", "", "2001:db8::/129"} {
		if _, err := ParseNetwork(raw); err == nil {
			t.Fatalf("accepted malformed IP %q", raw)
		}
	}
}
func TestTrustedProxyIdentity(t *testing.T) {
	trusted, err := ParseTrustedProxies([]string{"10.0.0.0/8", "2001:db8:1::/48"})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, remote, header, want string
		duplicate                  bool
	}{
		{"untrusted forged header", "192.0.2.8:8888", "garbage", "192.0.2.8", false},
		{"trusted multi hop", "10.0.0.1:80", "198.51.100.4, 10.0.0.2", "198.51.100.4", false},
		{"nearest untrusted", "10.0.0.1:80", "192.0.2.99, 198.51.100.4", "198.51.100.4", false},
		{"ipv6", "[2001:db8:1::1]:80", "2001:db8:2::9", "2001:db8:2::9", false},
		{"mapped peer", "[::ffff:192.0.2.8]:80", "198.51.100.9", "192.0.2.8", false},
		{"missing trusted chain", "10.0.0.1:80", "", "", false},
		{"all trusted", "10.0.0.1:80", "10.0.0.2", "", false},
		{"invalid chain", "10.0.0.1:80", "bad, 198.51.100.4", "", false},
		{"duplicate chain", "10.0.0.1:80", "198.51.100.4", "", true},
		{"bad socket", "not-an-address", "", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/", nil)
			req.RemoteAddr = test.remote
			req.Header.Set("X-Forwarded-For", test.header)
			req.Header.Set("X-Real-IP", "203.0.113.1")
			req.Header.Set("Forwarded", "for=203.0.113.1")
			if test.duplicate {
				req.Header.Add("X-Forwarded-For", test.header)
			}
			got, err := ClientIP(req, trusted)
			if test.want == "" {
				if err == nil {
					t.Fatal("untrusted identity accepted")
				}
				return
			}
			if err != nil || got.String() != test.want {
				t.Fatalf("identity %v %v", got, err)
			}
		})
	}
}
