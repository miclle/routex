package upstream

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestValidateEgress(t *testing.T) {
	valid := EgressConfig{Kind: "socks5", Host: "egress.example.com", Port: 1080}
	if err := ValidateEgress(valid, false); err != nil {
		t.Fatal(err)
	}
	cases := []EgressConfig{
		{Kind: "http", Host: valid.Host, Port: 80}, {Kind: "socks5h", Host: valid.Host, Port: 1080},
		{Kind: "https", Host: "http://host", Port: 443}, {Kind: "https", Host: "host:443", Port: 443},
		{Kind: "https", Host: "localhost", Port: 443}, {Kind: "https", Host: "127.0.0.1", Port: 443},
		{Kind: "https", Host: "::1", Port: 443}, {Kind: "https", Host: valid.Host, Port: 65536},
		{Kind: "https", Host: valid.Host, Port: 443, Username: "user"},
		{Kind: "https", Host: valid.Host, Port: 443, Username: "user:other", Password: "password"},
	}
	for _, config := range cases {
		if ValidateEgress(config, false) == nil {
			t.Errorf("accepted invalid endpoint kind=%s", config.Kind)
		}
	}
	if ValidateEgress(EgressConfig{Kind: "socks5", Host: "::1", Port: 1080}, true) != nil {
		t.Fatal("explicit private proxy rejected")
	}
	for _, host := range []string{"169.254.169.254", "fd00:ec2::254", "168.63.129.16", "224.0.0.1", "0.0.0.0"} {
		if ValidateEgress(EgressConfig{Kind: "socks5", Host: host, Port: 1080}, true) == nil {
			t.Fatal("metadata/non-unicast proxy accepted")
		}
	}
}

func TestHTTPSProxyPinnedTargetAndIndependentTLS(t *testing.T) {
	var targetSNI string
	var leaked atomic.Bool
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetSNI = r.TLS.ServerName
		if r.Header.Get("Proxy-Authorization") != "" {
			leaked.Store(true)
		}
		if r.Header.Get("Authorization") != "Bearer provider-test-only" {
			t.Error("provider authentication missing")
		}
		_, _ = io.WriteString(w, "target response")
	}))
	defer target.Close()
	targetURL, _ := url.Parse(target.URL)
	var authority string
	var proxyAuthenticated atomic.Bool
	proxy := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			t.Error("proxy received non-CONNECT")
			w.WriteHeader(400)
			return
		}
		if r.Header.Get("Authorization") != "" {
			leaked.Store(true)
		}
		authority = r.Host
		proxyAuthenticated.Store(r.Header.Get("Proxy-Authorization") == "Basic "+base64.StdEncoding.EncodeToString([]byte("proxy-user:proxy-test-only")))
		remote, err := net.Dial("tcp", targetURL.Host)
		if err != nil {
			w.WriteHeader(502)
			return
		}
		defer func() { _ = remote.Close() }()
		hijack, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error("hijack failed")
			return
		}
		defer func() { _ = hijack.Close() }()
		_, _ = io.WriteString(hijack, "HTTP/1.1 200 Connection Established\r\n\r\n")
		done := make(chan struct{})
		go func() { _, _ = io.Copy(remote, hijack); _ = remote.Close(); close(done) }()
		_, _ = io.Copy(hijack, remote)
		_ = hijack.Close()
		<-done
	}))
	defer proxy.Close()
	proxyURL, _ := url.Parse(proxy.URL)
	roots := x509.NewCertPool()
	roots.AddCert(proxy.Certificate())
	roots.AddCert(target.Certificate())
	lookup := func(_ context.Context, _ string, host string) ([]netip.Addr, error) {
		if host == "proxy.example.com" {
			return []netip.Addr{netip.MustParseAddr("93.184.216.35")}, nil
		}
		if host == "target.example.com" {
			return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
		}
		t.Error("unexpected DNS host")
		return nil, errors.New("unexpected lookup")
	}
	var dialed string
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		dialed = address
		var d net.Dialer
		return d.DialContext(ctx, network, proxyURL.Host)
	}
	config := EgressConfig{Kind: "https", Host: "proxy.example.com", Port: 443, Username: "proxy-user", Password: "proxy-test-only"}
	client, err := newEgressClient(false, false, config, lookup, dial, &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	client.Transport.(*policyTransport).base.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	request, _ := http.NewRequest(http.MethodGet, "https://target.example.com/v1/models", nil)
	request.Header.Set("Authorization", "Bearer provider-test-only")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal("guarded proxy request failed")
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if string(body) != "target response" || authority != "93.184.216.34:443" || dialed != "93.184.216.35:443" || targetSNI != "target.example.com" || !proxyAuthenticated.Load() || leaked.Load() {
		t.Fatal("proxy pinning, TLS or credential isolation failed")
	}
}

func runSOCKSFixture(t *testing.T, username, password string, badAuth bool) (EgressConfig, <-chan []byte) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	targets := make(chan []byte, 16)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
				greeting := make([]byte, 3)
				if _, err := io.ReadFull(conn, greeting); err != nil {
					return
				}
				method := byte(0)
				if username != "" {
					method = 2
				}
				_, _ = conn.Write([]byte{5, method})
				if method == 2 {
					head := make([]byte, 2)
					if _, err := io.ReadFull(conn, head); err != nil {
						return
					}
					user := make([]byte, head[1])
					_, _ = io.ReadFull(conn, user)
					_, _ = io.ReadFull(conn, head[:1])
					pass := make([]byte, head[0])
					_, _ = io.ReadFull(conn, pass)
					if string(user) != username || string(pass) != password || badAuth {
						_, _ = conn.Write([]byte{1, 1})
						return
					}
					_, _ = conn.Write([]byte{1, 0})
				}
				head := make([]byte, 4)
				if _, err := io.ReadFull(conn, head); err != nil {
					return
				}
				size := 4
				if head[3] == 4 {
					size = 16
				}
				if head[3] != 1 && head[3] != 4 {
					return
				}
				address := make([]byte, size+2)
				if _, err := io.ReadFull(conn, address); err != nil {
					return
				}
				targets <- append(head, address...)
				_, _ = conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 80})
				request, err := http.ReadRequest(bufio.NewReader(conn))
				if err != nil {
					return
				}
				_ = request.Body.Close()
				if request.Header.Get("Proxy-Authorization") != "" {
					t.Error("proxy credentials reached target")
				}
				_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok")
			}()
		}
	}()
	host, port, _ := net.SplitHostPort(listener.Addr().String())
	number, _ := strconv.Atoi(port)
	return EgressConfig{Kind: "socks5", Host: host, Port: number, Username: username, Password: password}, targets
}

func TestSOCKS5IPv4IPv6AndAuthentication(t *testing.T) {
	for _, address := range []string{"127.0.0.1", "::1"} {
		t.Run(address, func(t *testing.T) {
			config, targets := runSOCKSFixture(t, "proxy-user", "proxy-test-only", false)
			client, err := NewEgressClient(true, true, config)
			if err != nil {
				t.Fatal(err)
			}
			defer client.CloseIdleConnections()
			response, err := client.Get("http://" + net.JoinHostPort(address, "8080") + "/v1/models")
			if err != nil {
				t.Fatal("SOCKS request failed")
			}
			_ = response.Body.Close()
			target := <-targets
			expected := byte(1)
			if address == "::1" {
				expected = 4
			}
			if target[3] != expected {
				t.Fatal("target sent as hostname or wrong address family")
			}
		})
	}
	config, _ := runSOCKSFixture(t, "proxy-user", "proxy-test-only", true)
	client, _ := NewEgressClient(true, true, config)
	defer client.CloseIdleConnections()
	_, err := client.Get("http://127.0.0.1:8080/v1/models")
	if err == nil || strings.Contains(err.Error(), "proxy-test-only") {
		t.Fatal("authentication failure was accepted or exposed credentials")
	}
}

func TestSOCKS5TriesEveryValidatedTargetAddress(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	attempts := make(chan byte, 2)
	go func() {
		var number atomic.Int32
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
				greeting := make([]byte, 3)
				if _, readErr := io.ReadFull(conn, greeting); readErr != nil {
					return
				}
				_, _ = conn.Write([]byte{5, 0})
				head := make([]byte, 4)
				if _, readErr := io.ReadFull(conn, head); readErr != nil {
					return
				}
				size := 4
				if head[3] == 4 {
					size = 16
				}
				address := make([]byte, size+2)
				if _, readErr := io.ReadFull(conn, address); readErr != nil {
					return
				}
				attempts <- head[3]
				if number.Add(1) == 1 {
					_, _ = conn.Write([]byte{5, 4, 0, 1, 127, 0, 0, 1, 0, 0})
					return
				}
				_, _ = conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 80})
				request, readErr := http.ReadRequest(bufio.NewReader(conn))
				if readErr != nil {
					return
				}
				_ = request.Body.Close()
				_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok")
			}()
		}
	}()
	host, port, _ := net.SplitHostPort(listener.Addr().String())
	portNumber, _ := strconv.Atoi(port)
	lookup := func(_ context.Context, _ string, host string) ([]netip.Addr, error) {
		if host != "target.example.com" {
			return nil, errors.New("unexpected lookup")
		}
		return []netip.Addr{netip.MustParseAddr("2606:4700:4700::1111"), netip.MustParseAddr("1.1.1.1")}, nil
	}
	dialer := &net.Dialer{}
	client, err := newEgressClient(true, true, EgressConfig{Kind: "socks5", Host: host, Port: portNumber}, lookup, dialer.DialContext, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	conn, err := client.Transport.(*policyTransport).base.DialContext(context.Background(), "tcp", "target.example.com:8080")
	if err != nil {
		t.Fatalf("second validated target was not attempted: %v", err)
	}
	_ = conn.Close()
	if first, second := <-attempts, <-attempts; first != 4 || second != 1 {
		t.Fatalf("target attempts = %d, %d; want IPv6 then IPv4", first, second)
	}
}

func TestProxyPolicyCannotBypassTargetPolicy(t *testing.T) {
	config, _ := runSOCKSFixture(t, "", "", false)
	client, err := NewEgressClient(false, true, config)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	if _, err := client.Get("https://127.0.0.1/v1/models"); err == nil {
		t.Fatal("private proxy enabled private target")
	}
	var dialed atomic.Bool
	lookup := func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("127.0.0.1")}, nil
	}
	dial := func(context.Context, string, string) (net.Conn, error) { dialed.Store(true); return nil, errProxy }
	config = EgressConfig{Kind: "socks5", Host: "proxy.example.com", Port: 1080}
	client, err = newEgressClient(false, false, config, lookup, dial, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	if _, err := client.Get("https://target.example.com/v1/models"); err == nil || dialed.Load() {
		t.Fatal("mixed DNS answer reached proxy")
	}
}

func TestProxyHandshakeCancellation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err == nil {
			defer func() { _ = conn.Close() }()
			_, _ = io.Copy(io.Discard, conn)
		}
	}()
	host, port, _ := net.SplitHostPort(listener.Addr().String())
	number, _ := strconv.Atoi(port)
	client, err := NewEgressClient(true, true, EgressConfig{Kind: "socks5", Host: host, Port: number})
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1/v1/models", nil)
	start := time.Now()
	_, err = client.Do(req)
	if err == nil || time.Since(start) > time.Second {
		t.Fatal("stalled proxy handshake did not cancel")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("canceled proxy connection leaked")
	}
}

func TestDiagnosticReportsRealStages(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) }))
	defer target.Close()
	req, _ := http.NewRequest(http.MethodGet, target.URL+"/models", nil)
	result, err := Diagnose(context.Background(), req, true, false, nil)
	if err != nil || !result.TransportOK || result.APIOK || result.HTTPStatus != 401 {
		t.Fatal("HTTP failure incorrectly marked successful")
	}
	for _, stage := range result.Stages {
		if stage.Stage == "target_dns" && stage.Status != "not_applicable" {
			t.Fatal("literal IP fabricated DNS stage")
		}
		if stage.Stage == "target_tls" && stage.Status != "not_applicable" {
			t.Fatal("plain HTTP fabricated TLS stage")
		}
		if stage.Stage == "api" && stage.Status != "failed" {
			t.Fatal("API rejection hidden")
		}
	}
	config, _ := runSOCKSFixture(t, "", "", false)
	result, err = Diagnose(context.Background(), req, true, true, &config)
	if err != nil || !result.TransportOK || !result.APIOK {
		t.Fatal("real SOCKS diagnostic failed")
	}
}
