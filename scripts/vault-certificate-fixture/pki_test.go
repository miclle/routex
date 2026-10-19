package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/pkg/upstream"
)

func fixtureMaterials(t *testing.T) (map[string][]byte, time.Time) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	files, err := fixturePKI(strings.Repeat("a", 32), now)
	if err != nil {
		t.Fatal("PKI preparation failed")
	}
	return files, now
}

func fixtureRoots(t *testing.T, raw []byte) *x509.CertPool {
	t.Helper()
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(raw) {
		t.Fatal("fixture CA rejected")
	}
	return pool
}

func TestFixturePKISeparatesTrustIdentitiesAndKeys(t *testing.T) {
	files, now := fixtureMaterials(t)
	for _, role := range []string{"writer", "reader"} {
		block, rest := pem.Decode(files[role+".pem"])
		if block == nil || len(rest) != 0 {
			t.Fatal("invalid fixture certificate")
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil || cert.Subject.CommonName != "routex-"+role+"-"+strings.Repeat("a", 32) {
			t.Fatal("fixture identity changed")
		}
		options := x509.VerifyOptions{Roots: fixtureRoots(t, files["client-ca.pem"]), CurrentTime: now, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
		if _, err = cert.Verify(options); err != nil {
			t.Fatal("client issuer rejected")
		}
		options.Roots = fixtureRoots(t, files["server-ca.pem"])
		if _, err = cert.Verify(options); err == nil {
			t.Fatal("server trust incorrectly authenticates a client")
		}
		if _, err = tls.X509KeyPair(files[role+".pem"], files[role+".key"]); err != nil {
			t.Fatal("matching client key rejected")
		}
	}
	if _, err := tls.X509KeyPair(files["writer.pem"], files["reader.key"]); err == nil {
		t.Fatal("different client key accepted")
	}
}

func TestFixtureMaterialsExerciseActualMutualTLSAndRejectWrongServerCA(t *testing.T) {
	files, _ := fixtureMaterials(t)
	serverPair, err := tls.X509KeyPair(files["server.pem"], files["server.key"])
	if err != nil {
		t.Fatal("server key rejected")
	}
	var mu sync.Mutex
	var identities []string
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
			http.Error(w, "client identity missing", http.StatusForbidden)
			return
		}
		mu.Lock()
		identities = append(identities, r.TLS.PeerCertificates[0].Subject.CommonName)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{serverPair}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: fixtureRoots(t, files["client-ca.pem"])}
	server.StartTLS()
	defer server.Close()
	for _, role := range []string{"writer", "reader", "wrong-server-ca"} {
		certRole, ca := role, files["server-ca.pem"]
		if role == "wrong-server-ca" {
			certRole, ca = "writer", files["client-ca.pem"]
		}
		client, err := upstream.NewCertificateClient(true, files[certRole+".pem"], files[certRole+".key"], ca)
		if err != nil {
			t.Fatal("fixture client rejected")
		}
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
		response, callErr := client.Do(req)
		if response != nil {
			_ = response.Body.Close()
		}
		cancel()
		client.CloseIdleConnections()
		if role == "wrong-server-ca" {
			if callErr == nil {
				t.Fatal("untrusted server accepted")
			}
		} else if callErr != nil || response == nil || response.StatusCode != http.StatusNoContent {
			t.Fatal("valid mutual TLS rejected")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(identities) != 2 || identities[0] == identities[1] {
		t.Fatal("actual client certificate identities were not distinct")
	}
}

func TestPreparePKIRefusesOverwriteAndUnsafeParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the opt-in fixture requires POSIX private file permissions")
	}
	run := strings.Repeat("a", 32)
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "routex-vault-certificate-"+run)
	if os.Mkdir(root, 0700) != nil {
		t.Fatal("private parent creation failed")
	}
	dir := filepath.Join(root, "input")
	if preparePKI(dir, run, time.Now()) != nil {
		t.Fatal("private material preparation failed")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 9 {
		t.Fatal("material manifest incomplete")
	}
	for _, entry := range entries {
		info, err := os.Lstat(filepath.Join(dir, entry.Name()))
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
			t.Fatal("material permissions changed")
		}
	}
	if preparePKI(dir, run, time.Now()) == nil || preparePKI(filepath.Join(root, "other"), run, time.Now()) == nil {
		t.Fatal("existing or out-of-scope material path accepted")
	}
	link := filepath.Join(base, "link")
	if os.Symlink(root, link) != nil {
		t.Fatal("symlink fixture creation failed")
	}
	if preparePKI(filepath.Join(link, "input"), run, time.Now()) == nil {
		t.Fatal("symlink parent accepted")
	}
}
