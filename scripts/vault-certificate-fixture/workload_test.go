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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/pkg/upstream"
)

func TestWorkloadPKIRequiresIntermediateAndExactURI(t *testing.T) {
	run := strings.Repeat("b", 32)
	now := time.Now().UTC()
	files, err := workloadPKI(run, now)
	if err != nil || len(files) != 14 {
		t.Fatal("workload fixture incomplete")
	}
	for _, role := range []string{"writer", "reader", "outsider", "cnless"} {
		leaf, rest := pem.Decode(files[role+".pem"])
		intermediate, remainder := pem.Decode(rest)
		if leaf == nil || intermediate == nil || len(remainder) != 0 {
			t.Fatal("exact leaf/intermediate chain missing")
		}
		cert, err := x509.ParseCertificate(leaf.Bytes)
		if err != nil || len(cert.URIs) != 1 {
			t.Fatal("workload URI identity missing")
		}
		want := workloadURI(run, role)
		wantCN := "routex-" + role + "-" + run
		if role == "outsider" {
			want = "spiffe://other-" + run + ".invalid/vault/writer"
			wantCN = "routex-writer-" + run
		} else if role == "cnless" {
			want, wantCN = workloadURI(run, "writer"), ""
		}
		if cert.URIs[0].String() != want || cert.Subject.CommonName != wantCN {
			t.Fatal("workload trust domain or role changed")
		}
		options := x509.VerifyOptions{Roots: fixtureRoots(t, files["client-ca.pem"]), CurrentTime: now, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
		if _, err = cert.Verify(options); err == nil {
			t.Fatal("missing intermediate accepted")
		}
		options.Intermediates = fixtureRoots(t, files["client-intermediate.pem"])
		if chains, err := cert.Verify(options); err != nil || len(chains) != 1 || len(chains[0]) != 3 {
			t.Fatal("complete three-level chain rejected")
		}
		options.Roots = fixtureRoots(t, files["server-ca.pem"])
		if _, err = cert.Verify(options); err == nil {
			t.Fatal("server root authenticated a workload")
		}
	}
}

func TestWorkloadChainsUseActualTLSIncludingEmptyCN(t *testing.T) {
	run := strings.Repeat("c", 32)
	files, err := workloadPKI(run, time.Now().UTC())
	if err != nil {
		t.Fatal("workload fixture failed")
	}
	for _, missingServer := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete-server-chain", true: "missing-server-intermediate"}[missingServer], func(t *testing.T) {
			pair, err := tls.X509KeyPair(files["server.pem"], files["server.key"])
			if err != nil {
				t.Fatal("server fixture rejected")
			}
			if missingServer {
				pair.Certificate = pair.Certificate[:1]
			}
			var mu sync.Mutex
			var seen []string
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				leaf := r.TLS.PeerCertificates[0]
				if len(r.TLS.PeerCertificates) != 2 || len(leaf.URIs) != 1 {
					http.Error(w, "unexpected identity", http.StatusForbidden)
					return
				}
				mu.Lock()
				seen = append(seen, leaf.URIs[0].String())
				mu.Unlock()
				w.WriteHeader(http.StatusNoContent)
			}))
			server.Config.ErrorLog = log.New(io.Discard, "", 0)
			server.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: fixtureRoots(t, files["client-ca.pem"])}
			server.StartTLS()
			defer server.Close()
			for _, role := range []string{"writer", "reader", "cnless", "missing-client-intermediate"} {
				identity := role
				if role == "missing-client-intermediate" {
					identity = "writer"
				}
				certificate := files[identity+".pem"]
				if role == "missing-client-intermediate" {
					leaf, _ := pem.Decode(certificate)
					certificate = pem.EncodeToMemory(leaf)
				}
				client, err := upstream.NewCertificateClient(true, certificate, files[identity+".key"], files["server-ca.pem"])
				if err != nil {
					t.Fatal("workload client rejected")
				}
				ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
				req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
				response, callErr := client.Do(req)
				if response != nil {
					_ = response.Body.Close()
				}
				cancel()
				client.CloseIdleConnections()
				if missingServer || role == "missing-client-intermediate" {
					if callErr == nil {
						t.Fatal("incomplete chain reached application")
					}
				} else if callErr != nil || response == nil || response.StatusCode != http.StatusNoContent {
					t.Fatal("complete workload chain rejected")
				}
			}
			mu.Lock()
			defer mu.Unlock()
			if missingServer {
				if len(seen) != 0 {
					t.Fatal("untrusted server reached handler")
				}
			} else if len(seen) != 3 || seen[0] != workloadURI(run, "writer") || seen[1] != workloadURI(run, "reader") || seen[2] != workloadURI(run, "writer") {
				t.Fatal("actual URI identities changed")
			}
		})
	}
}
