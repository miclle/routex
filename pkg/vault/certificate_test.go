package vault

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type vaultCertificateAuthority struct {
	certificate *x509.Certificate
	key         *ecdsa.PrivateKey
	pem         []byte
}

func vaultCertificateCA(t *testing.T, name string) vaultCertificateAuthority {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: name}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), BasicConstraintsValid: true, IsCA: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return vaultCertificateAuthority{certificate: certificate, key: key, pem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}
}

func vaultCertificateLeaf(t *testing.T, issuer vaultCertificateAuthority, client bool) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "controlled-leaf"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature}
	if client {
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	} else {
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		template.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, issuer.certificate, &key.PublicKey, issuer.key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
}

func vaultCertificateFixture(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	serverCA, clientCA := vaultCertificateCA(t, "server issuer"), vaultCertificateCA(t, "client issuer")
	certificatePEM, keyPEM := vaultCertificateLeaf(t, serverCA, false)
	serverPair, err := tls.X509KeyPair(certificatePEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(clientCA.certificate)
	server := httptest.NewUnstartedServer(handler)
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.EnableHTTP2 = true
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{serverPair}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool}
	server.StartTLS()
	t.Cleanup(server.Close)
	certificatePEM, keyPEM = vaultCertificateLeaf(t, clientCA, true)
	client, err := NewCertificate(Descriptor{Endpoint: server.URL + "/base", Namespace: "acme/production", Mount: "kv", Prefix: "providers", DataField: "value"}, true, certificatePEM, keyPEM, serverCA.pem)
	if err != nil {
		t.Fatal(err)
	}
	clear(certificatePEM)
	clear(keyPEM)
	t.Cleanup(client.Close)
	return client
}

func TestCertificateLoginExactRoleTransportAndTransientLease(t *testing.T) {
	var calls atomic.Int32
	arrival := make(chan time.Time, 1)
	client := vaultCertificateFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		select {
		case arrival <- time.Now():
		default:
			t.Error("duplicate certificate login")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		var fields map[string]string
		if json.Unmarshal(body, &fields) != nil || len(fields) != 1 || fields["name"] != "Named.Role-1" {
			t.Error("certificate role body changed")
		}
		if r.Method != http.MethodPost || r.URL.Path != "/base/v1/auth/custom/team/login" || r.URL.RawQuery != "" || r.Header.Get("X-Vault-Token") != "" || r.Header.Get("X-Vault-Namespace") != "acme/production" || r.Header.Get("X-Vault-Request") != "true" || r.Header.Get("Content-Type") != "application/json" || !r.Close || r.ProtoMajor != 1 || r.TLS == nil || len(r.TLS.PeerCertificates) != 1 {
			t.Error("certificate login transport changed")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, loginResponse)
	})
	base := client.http.Transport
	deadlines := make(chan time.Time, 1)
	client.http.Transport = closeRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		deadline, ok := req.Context().Deadline()
		if !ok || req.GetBody != nil {
			t.Error("finite non-replayable login context missing")
		}
		select {
		case deadlines <- deadline:
		default:
			t.Error("duplicate certificate request")
		}
		return base.RoundTrip(req)
	})
	started := time.Now()
	token, obs, err := client.LoginCertificate(t.Context(), "custom/team", "Named.Role-1")
	if err != nil || token == nil || !obs.Attempted || !obs.Succeeded || obs.Failure != nil || obs.Duration <= 0 || calls.Load() != 1 {
		t.Fatalf("login: %+v %v", obs, err)
	}
	deadline := <-deadlines
	if deadline.Before(started) || deadline.After(time.Now().Add(10*time.Second)) {
		t.Fatal("login context escaped ten-second bound")
	}
	if client.http.Timeout != 10*time.Second || requestTimeout != 10*time.Second || token.expiresAt.Before(started.Add(120*time.Second)) || token.expiresAt.After((<-arrival).Add(120*time.Second)) {
		t.Fatal("finite login or conservative lease changed")
	}
	value, err := token.Token()
	if err != nil || value != "minted-fixture-token" {
		t.Fatal("exact transient token unavailable")
	}
	encoded, _ := json.Marshal(token)
	for _, printable := range []string{string(encoded), fmt.Sprintf("%v", token), fmt.Sprintf("%+v", token), fmt.Sprintf("%#v", token), fmt.Sprintf("%+v", obs)} {
		if strings.Contains(printable, "minted-fixture-token") || strings.Contains(printable, "not-public") {
			t.Fatal("authentication material escaped result")
		}
	}
	if client.ResponseCloseState() != (ResponseCloseState{Observed: true}) {
		t.Fatal("successful response not closed")
	}
	owned := token.token
	token.Close()
	token.Close()
	for _, b := range owned {
		if b != 0 {
			t.Fatal("owned token bytes retained")
		}
	}
	if value, err := token.Token(); err == nil || value != "" {
		t.Fatal("closed token reusable")
	}
	if calls.Load() != 1 {
		t.Fatal("local token close made remote request")
	}
}

func TestCertificateLoginAdmissionNeverFallsBackToUnnamedRole(t *testing.T) {
	var calls atomic.Int32
	client := vaultCertificateFixture(t, func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(http.StatusTeapot) })
	cases := []struct{ mount, role string }{{"cert", ""}, {"cert", " named"}, {"cert", "named "}, {"cert", "../role"}, {"cert", "name\nrole"}, {"cert", "角色"}, {"cert", strings.Repeat("a", 129)}, {"", "named"}, {"../cert", "named"}}
	for _, tc := range cases {
		token, obs, err := client.LoginCertificate(t.Context(), tc.mount, tc.role)
		if err == nil || token != nil || obs.Attempted || obs.Succeeded || obs.Failure == nil || obs.Failure.Code != "invalid_auth" || err.Error() != "vault prepare: invalid_auth" {
			t.Fatalf("invalid admission: %+v %v", obs, err)
		}
	}
	ordinary := loginFixture(t, func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(http.StatusTeapot) })
	if token, obs, err := ordinary.LoginCertificate(t.Context(), "cert", "named"); err == nil || token != nil || obs.Attempted || obs.Failure == nil || obs.Failure.Code != "invalid_auth" {
		t.Fatal("ordinary client supplied unproven certificate authentication")
	}
	if calls.Load() != 0 || client.ResponseCloseState() != (ResponseCloseState{}) {
		t.Fatal("invalid role performed HTTP")
	}
	for _, endpoint := range []string{"http://127.0.0.1:1", "https://127.0.0.1:1"} {
		c, err := NewCertificate(Descriptor{Endpoint: endpoint, Mount: "kv", Prefix: "providers", DataField: "value"}, true, []byte("private certificate"), []byte("private key"), nil)
		want := "vault prepare: invalid_tls"
		if strings.HasPrefix(endpoint, "http:") {
			want = "vault prepare: invalid_descriptor"
		}
		if c != nil || err == nil || err.Error() != want {
			t.Fatalf("constructor failure: %v", err)
		}
	}
}

func TestCertificateLoginCancellationAndDeadlineAreFiniteWithoutRetry(t *testing.T) {
	for _, mode := range []string{"before", "during", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			entered, release := make(chan struct{}), make(chan struct{})
			defer close(release)
			client := vaultCertificateFixture(t, func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				close(entered)
				<-release
				w.WriteHeader(http.StatusOK)
			})
			var ctx context.Context
			var cancel context.CancelFunc
			if mode == "deadline" {
				ctx, cancel = context.WithTimeout(t.Context(), 100*time.Millisecond)
			} else {
				ctx, cancel = context.WithCancel(t.Context())
			}
			defer cancel()
			if mode == "before" {
				cancel()
			}
			type result struct {
				token *LoginToken
				obs   Observation
				err   error
			}
			done := make(chan result, 1)
			go func() {
				token, obs, err := client.LoginCertificate(ctx, "cert", "named")
				done <- result{token, obs, err}
			}()
			if mode == "during" {
				select {
				case <-entered:
				case <-time.After(2 * time.Second):
					t.Fatal("request did not arrive")
				}
				cancel()
			}
			var got result
			select {
			case got = <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("finite login did not join")
			}
			want := "canceled"
			if mode == "deadline" {
				want = "timed_out"
			}
			if got.token != nil || got.err == nil || got.obs.Succeeded || got.obs.Failure == nil || got.obs.Failure.Code != want || got.err.Error() != "vault prepare: "+want {
				t.Fatalf("context boundary: %+v %v", got.obs, got.err)
			}
			switch mode {
			case "before":
				if got.obs.Attempted || calls.Load() != 0 || client.ResponseCloseState() != (ResponseCloseState{}) {
					t.Fatal("pre-cancellation dispatched login")
				}
			case "during":
				if !got.obs.Attempted || calls.Load() != 1 || client.ResponseCloseState() != (ResponseCloseState{Failed: true}) {
					t.Fatal("canceled attempt replayed or invented response closure")
				}
			default:
				// A loaded scheduler may exhaust the parent deadline before dispatch.
				wantState := ResponseCloseState{}
				if got.obs.Attempted {
					wantState.Failed = true
				}
				if calls.Load() > 1 || client.ResponseCloseState() != wantState {
					t.Fatal("deadline replayed or invented response closure")
				}
			}
		})
	}
}

func TestCertificateLoginDoesNotFollowRedirectOrReplayLostResponse(t *testing.T) {
	for _, status := range []int{http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect, 0} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var calls, targetCalls atomic.Int32
			client := vaultCertificateFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/redirect-target" {
					targetCalls.Add(1)
					w.WriteHeader(http.StatusOK)
					return
				}
				calls.Add(1)
				if status != 0 {
					w.Header().Set("Location", "/redirect-target")
					w.WriteHeader(status)
					return
				}
				connection, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				_ = connection.Close()
			})
			token, obs, err := client.LoginCertificate(t.Context(), "cert", "named")
			wantCode, wantStatus := "transport", 0
			wantClose := ResponseCloseState{Failed: true}
			if status == http.StatusTemporaryRedirect || status == http.StatusPermanentRedirect {
				wantCode, wantStatus = "http_status", status
				wantClose = ResponseCloseState{Observed: true}
				if obs.responseCloseFailed {
					t.Fatal("returned redirect response Close failed")
				}
			}
			if token != nil || err == nil || !obs.Attempted || obs.Succeeded || obs.Failure == nil || obs.Failure.Code != wantCode || obs.Failure.HTTPStatus != wantStatus || err.Error() != "vault prepare: "+wantCode || calls.Load() != 1 || targetCalls.Load() != 0 {
				t.Fatalf("redirect or replay: %+v %v", obs, err)
			}
			if client.ResponseCloseState() != wantClose {
				t.Fatal("redirect or transport response close proof differs")
			}
		})
	}
}

func TestCertificateLoginStrictResponseAndLeaseBounds(t *testing.T) {
	cases := []struct {
		name, body, content, code string
		status                    int
	}{
		{"denied", `{"errors":["private upstream text"]}`, "application/json", "http_status", 403},
		{"html", loginResponse, "text/html", "invalid_response", 200},
		{"zero-lease", `{"auth":{"client_token":"ok","lease_duration":0}}`, "application/json", "invalid_response", 200},
		{"overflow-lease", `{"auth":{"client_token":"ok","lease_duration":9223372037}}`, "application/json", "invalid_response", 200},
		{"duplicate", `{"auth":{"client_token":"ok","client_token":"other","lease_duration":1}}`, "application/json", "invalid_response", 200},
		{"errors", `{"auth":{"client_token":"ok","lease_duration":1},"errors":["private"]}`, "application/json", "invalid_response", 200},
		{"oversize", strings.Repeat("x", maxResponseBytes+1), "application/json", "response_too_large", 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			client := vaultCertificateFixture(t, func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", tc.content)
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			})
			token, obs, err := client.LoginCertificate(t.Context(), "cert", "named")
			if token != nil || err == nil || obs.Succeeded || !obs.Attempted || obs.Failure == nil || obs.Failure.Code != tc.code || obs.Failure.HTTPStatus != tc.status || calls.Load() != 1 {
				t.Fatalf("strict response: %+v %v", obs, err)
			}
			if err.Error() != "vault prepare: "+tc.code || strings.Contains(fmt.Sprintf("%+v", obs), "private") {
				t.Fatal("remote failure material escaped")
			}
			if client.ResponseCloseState() != (ResponseCloseState{Observed: true}) {
				t.Fatal("rejected response not closed")
			}
		})
	}
}

func TestCertificateLoginResponseCloseMustJoinAndKeepsKnownLeaseSeparate(t *testing.T) {
	for _, closeFails := range []bool{false, true} {
		t.Run(fmt.Sprint(closeFails), func(t *testing.T) {
			client := vaultCertificateFixture(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, loginResponse)
			})
			var closes atomic.Int32
			entered, release := make(chan struct{}), make(chan struct{})
			defer func() {
				select {
				case <-release:
				default:
					close(release)
				}
			}()
			client.http.Transport = closeTransport{base: client.http.Transport, wrap: func(_ *http.Request, body io.ReadCloser) io.ReadCloser {
				var closeErr error
				if closeFails {
					closeErr = errors.New("private closure detail")
				}
				return &countedCloseBody{ReadCloser: body, closes: &closes, entered: entered, release: release, err: closeErr}
			}}
			type result struct {
				token *LoginToken
				obs   Observation
				err   error
			}
			done := make(chan result, 1)
			go func() {
				token, obs, err := client.LoginCertificate(t.Context(), "cert", "named")
				done <- result{token, obs, err}
			}()
			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				t.Fatal("response Close did not begin")
			}
			pending := ResponseCloseState{Observed: true, Pending: 1}
			if client.ResponseCloseState() != pending {
				t.Fatal("pending response ownership lost")
			}
			client.Close()
			if client.ResponseCloseState() != pending {
				t.Fatal("idle Close certified active response")
			}
			select {
			case <-done:
				t.Fatal("login returned before response Close joined")
			default:
			}
			close(release)
			var got result
			select {
			case got = <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("response Close not joined")
			}
			if got.err != nil || got.token == nil || !got.obs.Succeeded || got.obs.Failure != nil || got.obs.responseCloseFailed != closeFails {
				t.Fatal("known login lease conflated with close evidence")
			}
			got.token.Close()
			want := ResponseCloseState{Observed: true, Failed: closeFails}
			if closes.Load() != 1 || client.ResponseCloseState() != want {
				t.Fatal("response Close evidence incorrect")
			}
			client.Close()
			if client.ResponseCloseState() != want {
				t.Fatal("idle Close cleared sticky evidence")
			}
		})
	}
}
