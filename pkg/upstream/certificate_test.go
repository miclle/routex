package upstream

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type certificateTestAuthority struct {
	certificate *x509.Certificate
	key         *ecdsa.PrivateKey
	pem         []byte
}

func newCertificateTestAuthority(t *testing.T, name string) certificateTestAuthority {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	model := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: name}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, model, model, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return certificateTestAuthority{certificate, key, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}
}

func certificateTestLeaf(t *testing.T, authority certificateTestAuthority, client, expired, wrongHost bool) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	model := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "controlled-leaf"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature}
	if client {
		model.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	} else {
		model.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		model.DNSNames = []string{"example.com"}
		if wrongHost {
			model.DNSNames = []string{"wrong.example"}
		} else {
			model.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
		}
	}
	if expired {
		model.NotAfter = time.Now().Add(-time.Minute)
	}
	der, err := x509.CreateCertificate(rand.Reader, model, authority.certificate, &key.PublicKey, authority.key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
}

func certificateTestServer(t *testing.T, serverCA, clientCA certificateTestAuthority, expired, wrongHost bool, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	cert, key := certificateTestLeaf(t, serverCA, false, expired, wrongHost)
	pair, err := tls.X509KeyPair(cert, key)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(clientCA.certificate)
	server := httptest.NewUnstartedServer(handler)
	server.EnableHTTP2 = true
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: roots}
	server.StartTLS()
	t.Cleanup(server.Close)
	return server
}

func TestCertificateClientExactTLSAndGuardedPolicy(t *testing.T) {
	serverCA := newCertificateTestAuthority(t, "server-root")
	clientCA := newCertificateTestAuthority(t, "client-root")
	cert, key := certificateTestLeaf(t, clientCA, true, false, false)
	var calls, proxyCalls atomic.Int32
	server := certificateTestServer(t, serverCA, clientCA, false, false, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.TLS == nil || len(r.TLS.PeerCertificates) != 1 || r.ProtoMajor != 1 || !r.Close {
			t.Error("missing exact closing certificate connection")
		}
		w.WriteHeader(http.StatusNoContent)
	})
	proxy := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { proxyCalls.Add(1) }))
	defer proxy.Close()
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("HTTPS_PROXY", proxy.URL)
	t.Setenv("ALL_PROXY", proxy.URL)
	originalCert, originalKey, originalCA := bytes.Clone(cert), bytes.Clone(key), bytes.Clone(serverCA.pem)
	client, err := NewCertificateClient(true, cert, key, serverCA.pem)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	if !bytes.Equal(cert, originalCert) || !bytes.Equal(key, originalKey) || !bytes.Equal(serverCA.pem, originalCA) {
		t.Fatal("constructor modified borrowed PEM")
	}
	transport := client.Transport.(*policyTransport)
	if !transport.httpsOnly || transport.base.Proxy != nil || transport.base.TLSClientConfig.InsecureSkipVerify || transport.base.TLSClientConfig.ServerName != "" || transport.base.TLSClientConfig.MinVersion != tls.VersionTLS12 || !transport.base.DisableKeepAlives || transport.base.ForceAttemptHTTP2 || !transport.base.Protocols.HTTP1() || transport.base.Protocols.HTTP2() {
		t.Fatal("TLS policy weakened")
	}
	clear(cert)
	clear(key)
	clear(serverCA.pem) // Parsed client must not borrow the caller's PEM buffers.
	for range 2 {
		response, err := client.Get(server.URL)
		if err != nil {
			t.Fatal(err)
		}
		if err := response.Body.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 2 || proxyCalls.Load() != 0 {
		t.Fatal("request or proxy count")
	}
	response, err := client.Get(strings.Replace(server.URL, "https:", "http:", 1))
	if response != nil {
		_ = response.Body.Close()
	}
	if err == nil || !IsPreRequestFailure(err) || calls.Load() != 2 {
		t.Fatal("HTTP admitted")
	}
}

func TestCertificateClientRejectsUntrustedWrongAndExpiredTLS(t *testing.T) {
	serverCA := newCertificateTestAuthority(t, "server-root")
	clientCA := newCertificateTestAuthority(t, "client-root")
	otherCA := newCertificateTestAuthority(t, "other-root")
	for _, name := range []string{"system-trust", "wrong-server-ca", "client-issuer-is-not-server-trust", "wrong-client", "expired-client", "expired-server", "wrong-host"} {
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			server := certificateTestServer(t, serverCA, clientCA, name == "expired-server", name == "wrong-host", func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(204) })
			issuer := clientCA
			if name == "wrong-client" {
				issuer = otherCA
			}
			cert, key := certificateTestLeaf(t, issuer, true, name == "expired-client", false)
			ca := serverCA.pem
			switch name {
			case "system-trust":
				ca = nil
			case "wrong-server-ca":
				ca = otherCA.pem
			case "client-issuer-is-not-server-trust":
				ca = clientCA.pem
			}
			client, err := NewCertificateClient(true, cert, key, ca)
			if err != nil {
				t.Fatal(err)
			}
			defer client.CloseIdleConnections()
			if name == "system-trust" && client.Transport.(*policyTransport).base.TLSClientConfig.RootCAs != nil {
				t.Fatal("omitted CA replaced system trust")
			}
			response, err := client.Get(server.URL)
			if response != nil {
				_ = response.Body.Close()
			}
			if err == nil || calls.Load() != 0 {
				t.Fatal("unverified TLS reached application")
			}
		})
	}
}

func TestCertificateClientBoundedPEMAndKeyMismatch(t *testing.T) {
	ca := newCertificateTestAuthority(t, "root")
	cert, key := certificateTestLeaf(t, ca, true, false, false)
	_, wrongKey := certificateTestLeaf(t, ca, true, false, false)
	for _, tc := range []struct {
		name          string
		cert, key, ca []byte
	}{
		{"missing-cert", nil, key, ca.pem}, {"missing-key", cert, nil, ca.pem},
		{"oversize-cert", bytes.Repeat([]byte("x"), maxCertificatePEMBytes+1), key, ca.pem},
		{"oversize-key", cert, bytes.Repeat([]byte("x"), maxCertificatePEMBytes+1), ca.pem},
		{"oversize-ca", cert, key, bytes.Repeat([]byte("x"), maxCertificatePEMBytes+1)},
		{"key-mismatch", cert, wrongKey, ca.pem}, {"invalid-key", cert, []byte("private-key-not-pem"), ca.pem},
		{"invalid-cert", []byte("private-certificate-not-pem"), key, ca.pem},
		{"invalid-ca", cert, key, []byte("private-ca-not-pem")}, {"empty-ca", cert, key, []byte(" ")},
		{"leaf-as-ca", cert, key, cert}, {"cert-trailing", append(append([]byte(nil), cert...), 'x'), key, ca.pem},
		{"key-trailing", cert, append(append([]byte(nil), key...), 'x'), ca.pem},
		{"ca-trailing", cert, key, append(append([]byte(nil), ca.pem...), 'x')},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, err := NewCertificateClient(true, tc.cert, tc.key, tc.ca)
			if client != nil || err == nil || err.Error() != "invalid upstream certificate material" {
				t.Fatal("invalid PEM not redacted")
			}
		})
	}
}

func TestCertificateClientPreservesDNSPinningAndRejectsMixedAnswers(t *testing.T) {
	serverCA := newCertificateTestAuthority(t, "server-root")
	clientCA := newCertificateTestAuthority(t, "client-root")
	cert, key := certificateTestLeaf(t, clientCA, true, false, false)
	server := certificateTestServer(t, serverCA, clientCA, false, false, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	for _, mixed := range []bool{false, true} {
		client, err := NewCertificateClient(false, cert, key, serverCA.pem)
		if err != nil {
			t.Fatal(err)
		}
		var dials int
		lookup := func(context.Context, string, string) ([]netip.Addr, error) {
			addresses := []netip.Addr{netip.MustParseAddr("8.8.8.8")}
			if mixed {
				addresses = append(addresses, netip.MustParseAddr("10.0.0.1"))
			}
			return addresses, nil
		}
		dial := func(ctx context.Context, network, address string) (net.Conn, error) {
			dials++
			if address != "8.8.8.8:443" {
				t.Error("DNS was not numerically pinned")
			}
			return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
		}
		client.Transport.(*policyTransport).base.DialContext = safeDial(false, lookup, dial)
		response, err := client.Get("https://example.com")
		if response != nil {
			_ = response.Body.Close()
		}
		client.CloseIdleConnections()
		if mixed && (err == nil || !IsPreRequestFailure(err) || dials != 0) || !mixed && (err != nil || dials != 1) {
			t.Fatal("certificate client bypassed DNS policy")
		}
	}
}

func TestCertificateClientRejectsSkippedLeadingPEMForEveryMaterialRole(t *testing.T) {
	ca := newCertificateTestAuthority(t, "root")
	cert, key := certificateTestLeaf(t, ca, true, false, false)
	malformedCertificate := []byte("-----BEGIN CERTIFICATE-----\nnot-base64!\n-----END CERTIFICATE-----\n")
	malformedKey := []byte("-----BEGIN PRIVATE KEY-----\nnot-base64!\n-----END PRIVATE KEY-----\n")
	nestedCertificate := []byte("-----BEGIN CERTIFICATE-----\nnot-base64!\n")
	nestedKey := []byte("-----BEGIN PRIVATE KEY-----\nnot-base64!\n")
	join := func(prefix, valid []byte) []byte { return append(append([]byte(nil), prefix...), valid...) }
	for _, tc := range []struct {
		name          string
		prefix, valid []byte
		role          string
	}{
		{"certificate-malformed-first", malformedCertificate, cert, "certificate"},
		{"ca-malformed-first", malformedCertificate, ca.pem, "ca"},
		{"key-malformed-first", malformedKey, key, "key"},
		{"certificate-nested-begin", nestedCertificate, cert, "certificate"},
		{"ca-nested-begin", nestedCertificate, ca.pem, "ca"},
		{"key-nested-begin", nestedKey, key, "key"},
		{"certificate-preamble", []byte("ignored text\n"), cert, "certificate"},
		{"ca-preamble", []byte("ignored text\n"), ca.pem, "ca"},
		{"key-preamble", []byte("ignored text\n"), key, "key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := join(tc.prefix, tc.valid)
			// The standard decoder deliberately accepts a later block. This
			// control makes each fixture exercise the admission gap directly.
			block, rest := pem.Decode(raw)
			if block == nil || len(bytes.TrimSpace(rest)) != 0 {
				t.Fatal("fixture did not exercise PEM skip-ahead")
			}
			if block, _ := firstPEMBlock(raw); block != nil {
				t.Fatal("first-block admission skipped malformed material")
			}
			certificatePEM, keyPEM, serverCAPEM := cert, key, ca.pem
			switch tc.role {
			case "certificate":
				certificatePEM = raw
			case "key":
				keyPEM = raw
			case "ca":
				serverCAPEM = raw
			}
			client, err := NewCertificateClient(true, certificatePEM, keyPEM, serverCAPEM)
			if client != nil || err == nil || err.Error() != "invalid upstream certificate material" {
				t.Fatal("skipped leading PEM admitted or exposed")
			}
		})
	}
	for _, raw := range [][]byte{cert, key, ca.pem} {
		wrapped := join([]byte(" \r\n\t"), append(append([]byte(nil), raw...), []byte(" \r\n\t")...))
		block, rest := firstPEMBlock(wrapped)
		if block == nil || len(bytes.TrimSpace(rest)) != 0 {
			t.Fatal("ordinary PEM or outer whitespace rejected")
		}
	}
	chain := join(cert, ca.pem)
	if !certificateBlocks(chain) || privateKeyBlock(join(key, key)) {
		t.Fatal("chain or single-key cardinality changed")
	}
	client, err := NewCertificateClient(true, chain, key, join(ca.pem, ca.pem))
	if err != nil || client == nil {
		t.Fatal("valid chain and CA bundle rejected")
	}
	client.CloseIdleConnections()
}
