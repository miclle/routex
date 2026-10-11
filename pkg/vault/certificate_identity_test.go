package vault

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type certificateIdentityFixture struct {
	descriptor               Descriptor
	writer, reader, operator *Client
	mu                       sync.Mutex
	calls                    []string
	stored                   map[string]map[string]string
	readerDER, writerDER     []byte
}

func certificateIdentities(t *testing.T, requireClient bool) *certificateIdentityFixture {
	t.Helper()
	serverCA, clientCA := vaultCertificateCA(t, "server trust"), vaultCertificateCA(t, "independent client issuer")
	serverPEM, serverKey := vaultCertificateLeaf(t, serverCA, false)
	serverPair, err := tls.X509KeyPair(serverPEM, serverKey)
	if err != nil {
		t.Fatal("server fixture TLS")
	}
	readerPEM, readerKey := vaultCertificateLeaf(t, clientCA, true)
	writerPEM, writerKey := vaultCertificateLeaf(t, clientCA, true)
	readerPair, err := tls.X509KeyPair(readerPEM, readerKey)
	if err != nil {
		t.Fatal("reader fixture TLS")
	}
	writerPair, err := tls.X509KeyPair(writerPEM, writerKey)
	if err != nil {
		t.Fatal("writer fixture TLS")
	}
	f := &certificateIdentityFixture{stored: map[string]map[string]string{}, readerDER: readerPair.Certificate[0], writerDER: writerPair.Certificate[0]}
	server := httptest.NewUnstartedServer(http.HandlerFunc(f.serve))
	pool := x509.NewCertPool()
	pool.AddCert(clientCA.certificate)
	auth := tls.VerifyClientCertIfGiven
	if requireClient {
		auth = tls.RequireAndVerifyClientCert
	}
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{serverPair}, ClientCAs: pool, ClientAuth: auth}
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.Config.ReadHeaderTimeout = time.Second
	server.Config.ReadTimeout = 2 * time.Second
	server.Config.WriteTimeout = 2 * time.Second
	server.StartTLS()
	t.Cleanup(server.Close)
	f.descriptor = Descriptor{Endpoint: server.URL, Namespace: "exact/namespace", Mount: "kv", Prefix: "providers", DataField: "value"}
	f.reader, err = NewCertificate(f.descriptor, true, readerPEM, readerKey, serverCA.pem)
	if err != nil {
		t.Fatal("reader construction")
	}
	f.writer, err = NewCertificate(f.descriptor, true, writerPEM, writerKey, serverCA.pem)
	if err != nil {
		t.Fatal("writer construction")
	}
	f.operator, err = NewServerCA(f.descriptor, true, serverCA.pem)
	if err != nil {
		t.Fatal("operator construction")
	}
	for _, client := range []*Client{f.reader, f.writer, f.operator} {
		t.Cleanup(client.Close)
	}
	return f
}
func (f *certificateIdentityFixture) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	identity := "operator"
	if r.TLS == nil || r.ProtoMajor != 1 || !r.Close || r.Header.Get("X-Vault-Namespace") != "exact/namespace" {
		w.WriteHeader(400)
		return
	}
	if len(r.TLS.PeerCertificates) != 0 {
		raw := r.TLS.PeerCertificates[0].Raw
		if bytes.Equal(raw, f.readerDER) {
			identity = "reader"
		} else if bytes.Equal(raw, f.writerDER) {
			identity = "writer"
		} else {
			w.WriteHeader(403)
			return
		}
	}
	f.calls = append(f.calls, identity+":"+r.Method+":"+r.URL.Path)
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/v1/auth/explicit/cert/login" {
		var body map[string]string
		if r.Method != "POST" || r.Header.Get("X-Vault-Token") != "" || json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body) != nil || len(body) != 1 || body["name"] != identity+"-role" || identity == "operator" {
			w.WriteHeader(403)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"auth": map[string]any{"client_token": identity + "-token", "lease_duration": 60}})
		return
	}
	if r.Header.Get("X-Vault-Token") != identity+"-token" {
		w.WriteHeader(403)
		return
	}
	dataPath := strings.Replace(r.URL.Path, "/destroy/", "/data/", 1)
	switch r.Method {
	case "POST":
		if identity != "writer" {
			w.WriteHeader(403)
			return
		}
		var body struct {
			Options struct {
				CAS int `json:"cas"`
			} `json:"options"`
			Data map[string]string `json:"data"`
		}
		if json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&body) != nil || body.Options.CAS != 0 || f.stored[dataPath] != nil {
			w.WriteHeader(409)
			return
		}
		f.stored[dataPath] = body.Data
		_, _ = io.WriteString(w, `{"data":{"version":1,"destroyed":false,"deletion_time":""}}`)
	case "GET":
		if identity != "reader" || r.URL.RawQuery != "version=1" {
			w.WriteHeader(403)
			return
		}
		data := f.stored[dataPath]
		if data == nil {
			w.WriteHeader(404)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"data": data, "metadata": map[string]any{"version": 1, "destroyed": false, "deletion_time": ""}}})
	case "PUT":
		if identity == "reader" {
			w.WriteHeader(403)
			return
		}
		if strings.Contains(dataPath, "routex-probe-") && identity != "writer" {
			w.WriteHeader(403)
			return
		}
		if strings.Contains(dataPath, "routex-credential-") && identity != "operator" {
			w.WriteHeader(403)
			return
		}
		var body struct {
			Versions []int `json:"versions"`
		}
		if json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&body) != nil || !reflect.DeepEqual(body.Versions, []int{1}) || f.stored[dataPath] == nil {
			w.WriteHeader(400)
			return
		}
		delete(f.stored, dataPath)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(405)
	}
}
func certificateIdentityLogin(t *testing.T, c *Client, role string) string {
	t.Helper()
	lease, obs, err := c.LoginCertificate(t.Context(), "explicit/cert", role+"-role")
	if err != nil || !obs.Succeeded || lease == nil {
		t.Fatal("controlled certificate login failed")
	}
	t.Cleanup(lease.Close)
	token, err := lease.Token()
	if err != nil {
		t.Fatal("fixture lease missing")
	}
	return token
}
func TestCertificateProbeUsesExactReaderAndWriterTransportForKV(t *testing.T) {
	f := certificateIdentities(t, false)
	writer := certificateIdentityLogin(t, f.writer, "writer")
	p, err := f.writer.Prepare()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if result, err := f.writer.Write(t.Context(), writer, p); err != nil || !result.Write.Succeeded {
		t.Fatal("writer TLS did not write")
	}
	reader := certificateIdentityLogin(t, f.reader, "reader")
	result, err := f.reader.ReadAndCleanupWithWriter(t.Context(), reader, writer, p.Plan(), f.writer)
	if err != nil || !result.Read.Succeeded || result.Cleanup.State != "acknowledged" {
		t.Fatal("separate certificate transports failed")
	}
	f.mu.Lock()
	calls := append([]string(nil), f.calls...)
	remaining := len(f.stored)
	f.mu.Unlock()
	if len(calls) != 5 || !strings.HasPrefix(calls[3], "reader:GET:") || !strings.HasPrefix(calls[4], "writer:PUT:") || remaining != 0 {
		t.Fatal("KV work crossed certificate identity")
	}
	for _, c := range []*Client{f.writer, f.reader} {
		state := c.ResponseCloseState()
		if !state.Observed || state.Pending != 0 || state.Failed {
			t.Fatal("identity response lifetime not joined")
		}
	}
}
func TestCertificateTwoClientPlanMismatchRejectsBeforeRead(t *testing.T) {
	f := certificateIdentities(t, false)
	p, err := f.writer.Prepare()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	other := f.descriptor
	other.Namespace = "different"
	mismatched, err := New(other, true)
	if err != nil {
		t.Fatal(err)
	}
	defer mismatched.Close()
	if _, err = f.reader.ReadAndCleanupWithWriter(t.Context(), "reader-token", "writer-token", p.Plan(), mismatched); err == nil {
		t.Fatal("cross-namespace plan admitted")
	}
	if _, err = f.reader.CleanupOwnedWithWriter(t.Context(), "reader-token", "writer-token", p.Plan(), nil); err == nil {
		t.Fatal("nil writer admitted")
	}
	f.mu.Lock()
	calls := len(f.calls)
	f.mu.Unlock()
	if calls != 0 {
		t.Fatal("invalid composition performed HTTP")
	}
}
func TestCertificateCredentialCleanupKeepsOperatorTokenTLSSeparate(t *testing.T) {
	for _, required := range []bool{false, true} {
		t.Run(map[bool]string{false: "optional-client-TLS", true: "hard-mTLS"}[required], func(t *testing.T) {
			f := certificateIdentities(t, required)
			writer := certificateIdentityLogin(t, f.writer, "writer")
			prepared, err := f.writer.PrepareCredential()
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Close()
			if result, err := f.writer.WriteCredential(t.Context(), writer, prepared, []byte("controlled-provider-value")); err != nil || !result.Write.Succeeded {
				t.Fatal("credential fixture write failed")
			}
			reader := certificateIdentityLogin(t, f.reader, "reader")
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			result, err := f.reader.CleanupCredentialOwnedWithClient(ctx, reader, "operator-token", prepared.Plan(), f.operator)
			f.mu.Lock()
			calls := append([]string(nil), f.calls...)
			remaining := len(f.stored)
			f.mu.Unlock()
			if required {
				if err == nil || !result.Ownership.Succeeded || result.Cleanup.State != "unknown" || remaining != 1 || len(calls) != 4 {
					t.Fatal("hard-mTLS borrowed reader identity or inferred cleanup")
				}
			} else if err != nil || result.Cleanup.State != "acknowledged" || remaining != 0 || len(calls) != 5 || !strings.HasPrefix(calls[4], "operator:PUT:") {
				t.Fatal("operator cleanup borrowed a certificate")
			}
		})
	}
}
