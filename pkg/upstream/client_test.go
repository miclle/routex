package upstream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestValidateBaseURL(t *testing.T) {
	for _, tc := range []struct {
		raw            string
		private, valid bool
	}{
		{"https://api.example.com/v1", false, true},
		{"https://8.8.8.8:443/v1/", false, true},
		{"https://[2606:4700:4700::1111]", false, true},
		{"https://127.0.0.1", false, false},
		{"http://127.0.0.1:8080/v1", true, true},
		{"http://[::1]:8080", true, true},
		{"http://192.168.1.1", true, true},
		{"http://[fd12::1]", true, true},
		{"http://localhost", true, true},
		{"https://internal.example.com", true, true},
		{"http://internal.example.com", true, false},
		{"http://8.8.8.8", true, false},
		{"http://localhost", false, false},
		{"http://api.example.com", false, false},
		{"https://169.254.169.254", true, false},
		{"http://[::ffff:127.0.0.1]", true, true},
		{"https://[::ffff:127.0.0.1]", false, false},
		{"https://[fe80::1%25en0]", true, false},
		{"https://user:password@api.example.com", true, false},
		{"https://api.example.com?key=secret", false, false},
		{"https://api.example.com?", false, false},
		{"https://api.example.com#", false, false},
		{"https://api.example.com#fragment", false, false},
		{"https://api.example.com:0", false, false},
		{"https://api.example.com:65536", false, false},
		{"https://api.example.com:", false, false},
		{"https://api.example.com:abc", false, false},
		{"https://", false, false},
		{"https:api.example.com", false, false},
		{"//api.example.com", false, false},
		{"ftp://api.example.com", false, false},
		{" https://api.example.com", false, false},
		{"https://a..example.com", false, false},
		{"https://-example.com", false, false},
		{"https://example_.com", false, false},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			_, err := ValidateBaseURL(tc.raw, tc.private)
			if (err == nil) != tc.valid {
				t.Fatalf("valid = %t, want %t", err == nil, tc.valid)
			}
		})
	}
}

func TestAddressPolicy(t *testing.T) {
	for _, raw := range []string{
		"0.0.0.0", "0.1.2.3", "10.0.0.1", "100.64.0.1", "127.0.0.1", "169.254.169.254", "172.16.1.1", "192.168.1.1",
		"192.0.0.1", "192.0.2.1", "192.88.99.1", "198.18.0.1", "198.51.100.1", "203.0.113.1", "224.0.0.1", "255.255.255.255",
		"::", "::1", "fe80::1", "ff02::1", "fc00::1", "::ffff:127.0.0.1", "64:ff9b::a00:1", "2001:db8::1", "3fff::1", "2002:7f00:1::1",
		"fd00:ec2::254", "168.63.129.16",
	} {
		t.Run(raw, func(t *testing.T) {
			ip := netip.MustParseAddr(raw)
			if allowedIP(ip, false) {
				t.Fatal("non-public address allowed")
			}
			privateAllowed := ip.Unmap().IsPrivate() || ip.Unmap().IsLoopback()
			if raw == "fd00:ec2::254" {
				privateAllowed = false
			}
			if allowedIP(ip, true) != privateAllowed {
				t.Fatal("unexpected private-mode policy")
			}
		})
	}
	for _, raw := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111", "::ffff:8.8.8.8"} {
		if !allowedIP(netip.MustParseAddr(raw), false) {
			t.Errorf("public IP blocked: %s", raw)
		}
	}
	if allowedIP(netip.Addr{}, true) || allowedIP(netip.MustParseAddr("fe80::1%en0"), true) {
		t.Fatal("invalid or scoped address allowed")
	}
}

func TestDialPinsDNSAndRejectsMixedAnswers(t *testing.T) {
	for _, tc := range []struct {
		name    string
		ips     []netip.Addr
		allowed bool
	}{
		{"public", []netip.Addr{netip.MustParseAddr("8.8.8.8")}, true},
		{"ipv6", []netip.Addr{netip.MustParseAddr("2606:4700:4700::1111")}, true},
		{"private", []netip.Addr{netip.MustParseAddr("127.0.0.1")}, false},
		{"mixed", []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("10.0.0.1")}, false},
		{"empty", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lookups, dials := 0, 0
			lookup := func(context.Context, string, string) ([]netip.Addr, error) { lookups++; return tc.ips, nil }
			dial := func(_ context.Context, _, address string) (net.Conn, error) {
				dials++
				if address != net.JoinHostPort(tc.ips[0].String(), "443") {
					t.Fatal("dial did not use the pinned numeric IP")
				}
				a, b := net.Pipe()
				if err := b.Close(); err != nil {
					t.Error(err)
				}
				return a, nil
			}
			conn, err := safeDial(false, lookup, dial)(context.Background(), "tcp", "rebind.example:443")
			if conn != nil {
				if err := conn.Close(); err != nil {
					t.Error(err)
				}
			}
			if (err == nil) != tc.allowed || lookups != 1 || (dials > 0) != tc.allowed {
				t.Fatal("unexpected DNS or dialing behavior")
			}
		})
	}
}

func TestDialCancellationFallbackAndLocalhost(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	lookup := func(ctx context.Context, _, _ string) ([]netip.Addr, error) { return nil, ctx.Err() }
	noDial := func(context.Context, string, string) (net.Conn, error) { t.Fatal("unexpected dial"); return nil, nil }
	if _, err := safeDial(false, lookup, noDial)(ctx, "tcp", "example.com:443"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	if _, err := safeDial(true, lookup, noDial)(ctx, "udp", "127.0.0.1:80"); err == nil {
		t.Fatal("UDP allowed")
	}
	if _, err := safeDial(true, lookup, noDial)(ctx, "tcp", "invalid"); err == nil {
		t.Fatal("invalid address allowed")
	}
	publicLookup := func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
	}
	if _, err := safeDial(true, publicLookup, noDial)(context.Background(), "tcp", "localhost:80"); err == nil {
		t.Fatal("localhost rebound to a public address")
	}
	attempts := 0
	ips := []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("1.1.1.1")}
	lookup = func(context.Context, string, string) ([]netip.Addr, error) { return ips, nil }
	dial := func(context.Context, string, string) (net.Conn, error) {
		attempts++
		return nil, errors.New("test failure")
	}
	if _, err := safeDial(false, lookup, dial)(context.Background(), "tcp", "example.com:443"); err == nil || attempts != 2 {
		t.Fatal("validated address fallback did not run")
	}
}

func TestLocalRequestAndRedirectProtection(t *testing.T) {
	var reached atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { reached.Add(1); w.WriteHeader(200) }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-only" {
			t.Error("authorization did not reach intended upstream")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	t.Setenv("HTTP_PROXY", destination.URL)
	t.Setenv("HTTPS_PROXY", destination.URL)
	t.Setenv("ALL_PROXY", destination.URL)
	client := NewClient(true)
	defer client.CloseIdleConnections()
	for _, path := range []string{"/safe?version=1", "/redirect"} {
		req, err := http.NewRequest(http.MethodGet, server.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer test-only")
		response, err := client.Do(req)
		if path == "/redirect" {
			if !errors.Is(err, errRedirect) {
				t.Fatalf("redirect policy: %v", err)
			}
		} else if err != nil || response.StatusCode != http.StatusNoContent {
			t.Fatalf("safe request failed: %v", err)
		}
		if response != nil {
			if err := response.Body.Close(); err != nil {
				t.Error(err)
			}
		}
	}
	if reached.Load() != 0 {
		t.Fatal("redirect target or environment proxy received a request")
	}
	blocked := NewClient(false)
	defer blocked.CloseIdleConnections()
	if response, err := blocked.Get(server.URL); err == nil {
		if err := response.Body.Close(); err != nil {
			t.Error(err)
		}
		t.Fatal("public client reached loopback HTTP")
	}
	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "other.example"
	if response, err := client.Do(req); err == nil {
		if err := response.Body.Close(); err != nil {
			t.Error(err)
		}
		t.Fatal("Host override accepted")
	}
}

func TestPublicTLSUsesHostnameVerification(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "verified") }))
	defer server.Close()
	lookup := func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
	}
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "8.8.8.8:443" {
			t.Fatal("unexpected pinned address")
		}
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	client := newClient(false, lookup, dial)
	defer client.CloseIdleConnections()
	transport := client.Transport.(*policyTransport).base
	transport.TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	// httptest's certificate has example.com as a DNS name. Never disable checks.
	transport.TLSClientConfig.InsecureSkipVerify = false
	response, err := client.Get("https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := response.Body.Close(); err != nil {
		t.Error(err)
	}
	if response, err := client.Get("https://wrong.example"); err == nil {
		if err := response.Body.Close(); err != nil {
			t.Error(err)
		}
		t.Fatal("TLS hostname mismatch accepted")
	}
	if transport.Proxy != nil || client.Timeout != 30*time.Second {
		t.Fatal("unsafe client defaults")
	}
}

func TestRequestCancellation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done() }))
	defer server.Close()
	client := NewClient(true)
	defer client.CloseIdleConnections()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		response, err := client.Do(req)
		if response != nil {
			if err := response.Body.Close(); err != nil {
				t.Error(err)
			}
		}
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("request cancellation: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("request did not cancel")
	}
}

func ExampleValidateBaseURL() {
	u, err := ValidateBaseURL("https://api.example.com/v1", false)
	fmt.Println(err == nil && strings.HasSuffix(u.Path, "/v1"))
	// Output: true
}
