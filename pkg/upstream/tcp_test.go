package upstream

import (
	"context"
	"net/url"
	"testing"
)

func TestGuardedTCPExportPreservesLDAPAddressPolicy(t *testing.T) {
	for _, tc := range []struct {
		raw         string
		private, ok bool
	}{{"ldaps://directory.example:636", false, true}, {"ldaps://127.0.0.1:636", false, false}, {"ldaps://127.0.0.1:636", true, true}, {"ldaps://169.254.169.254:636", true, false}, {"ldaps://168.63.129.16:636", true, false}, {"ldap://directory.example:389", false, false}, {"ldaps://directory.example:636/", false, false}, {"ldaps://user:password@directory.example:636", false, false}, {"ldaps://directory.example:636?q=x", false, false}, {"ldaps://directory.example", false, false}} {
		u, e := url.Parse(tc.raw)
		if e != nil {
			t.Fatal(e)
		}
		if (ValidateLDAPEndpoint(u, tc.private) == nil) != tc.ok {
			t.Fatal("address policy", tc.raw)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	conn, e := NewTCPDialer(true)(ctx, "tcp", "127.0.0.1:636")
	if conn != nil || e == nil || !IsPreRequestFailure(e) {
		t.Fatal("canceled guarded connector")
	}
}
