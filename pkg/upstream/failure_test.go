package upstream

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestPreRequestFailureClassification(t *testing.T) {
	t.Run("policy", func(t *testing.T) {
		client := NewClient(false)
		defer client.CloseIdleConnections()
		_, err := client.Get("https://127.0.0.1/models")
		if !IsPreRequestFailure(err) || !errors.Is(err, errAddress) {
			t.Fatalf("policy error = %v, want pre-request address failure", err)
		}
	})

	t.Run("direct DNS", func(t *testing.T) {
		lookup := func(context.Context, string, string) ([]netip.Addr, error) { return nil, errors.New("DNS unavailable") }
		dial := func(context.Context, string, string) (net.Conn, error) { t.Fatal("unexpected dial"); return nil, nil }
		_, err := safeDial(false, lookup, dial)(context.Background(), "tcp", "provider.example:443")
		if !IsPreRequestFailure(err) || !errors.Is(err, errAddress) {
			t.Fatalf("DNS error = %v, want pre-request address failure", err)
		}
	})

	t.Run("direct dial and cancellation", func(t *testing.T) {
		dialErr := errors.New("dial unavailable")
		lookup := func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
		}
		dial := func(context.Context, string, string) (net.Conn, error) { return nil, dialErr }
		_, err := safeDial(false, lookup, dial)(context.Background(), "tcp", "provider.example:443")
		if !IsPreRequestFailure(err) || !errors.Is(err, dialErr) {
			t.Fatalf("dial error = %v, want wrapped pre-request cause", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err = safeDial(false, lookup, dial)(ctx, "tcp", "provider.example:443")
		if !IsPreRequestFailure(err) || !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation error = %v, want classified cancellation", err)
		}
	})

	t.Run("managed egress DNS and tunnel", func(t *testing.T) {
		config := EgressConfig{Kind: "socks5", Host: "proxy.example.com", Port: 1080}
		lookup := func(_ context.Context, _ string, host string) ([]netip.Addr, error) {
			if host == "target.example.com" {
				return nil, errors.New("target DNS unavailable")
			}
			return []netip.Addr{netip.MustParseAddr("93.184.216.35")}, nil
		}
		dial := func(context.Context, string, string) (net.Conn, error) { t.Fatal("unexpected dial"); return nil, nil }
		client, err := newEgressClient(false, false, config, lookup, dial, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.Transport.(*policyTransport).base.DialContext(context.Background(), "tcp", "target.example.com:443")
		if !IsPreRequestFailure(err) || !errors.Is(err, errAddress) {
			t.Fatalf("target DNS error = %v, want pre-request address failure", err)
		}

		lookup = func(_ context.Context, _ string, host string) ([]netip.Addr, error) {
			if host == "target.example.com" {
				return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
			}
			return nil, errors.New("proxy DNS unavailable")
		}
		client, err = newEgressClient(false, false, config, lookup, dial, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.Transport.(*policyTransport).base.DialContext(context.Background(), "tcp", "target.example.com:443")
		if !IsPreRequestFailure(err) || !errors.Is(err, errAddress) {
			t.Fatalf("proxy DNS error = %v, want pre-request address failure", err)
		}

		lookup = func(_ context.Context, _ string, host string) ([]netip.Addr, error) {
			if host == "target.example.com" {
				return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
			}
			return []netip.Addr{netip.MustParseAddr("93.184.216.35")}, nil
		}
		dial = func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("proxy unavailable") }
		client, err = newEgressClient(false, false, config, lookup, dial, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.Transport.(*policyTransport).base.DialContext(context.Background(), "tcp", "target.example.com:443")
		if !IsPreRequestFailure(err) || !errors.Is(err, errProxy) {
			t.Fatalf("proxy tunnel error = %v, want pre-request proxy failure", err)
		}
	})
}

func TestPostRequestFailuresAreNotClassified(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Error("server cannot hijack connection")
			return
		}
		conn, _, err := hijacker.Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = conn.Close()
	}))
	defer server.Close()
	client := NewClient(true)
	defer client.CloseIdleConnections()
	_, err := client.Get(server.URL + "/models")
	if err == nil || IsPreRequestFailure(err) {
		t.Fatalf("response reset error = %v, want unclassified post-request failure", err)
	}
	if IsPreRequestFailure(errors.New("arbitrary RoundTrip failure")) {
		t.Fatal("arbitrary error was classified")
	}

	tlsClient := NewClient(true)
	defer tlsClient.CloseIdleConnections()
	_, err = tlsClient.Get("https://" + strings.TrimPrefix(server.URL, "http://") + "/models")
	if err == nil || IsPreRequestFailure(err) {
		t.Fatalf("target TLS error = %v, want unclassified failure", err)
	}
}
