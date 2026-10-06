package upstream

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestNonReplayingClientHTTP1Only(t *testing.T) {
	var requests atomic.Int32
	addresses := make(map[string]bool)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path == "/http2-control" {
			if r.ProtoMajor != 2 {
				t.Error("fixture did not offer HTTP/2")
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.ProtoMajor != 1 || !r.Close {
			t.Error("request did not use a closing HTTP/1 connection")
		}
		if addresses[r.RemoteAddr] {
			t.Error("request reused its connection")
		}
		addresses[r.RemoteAddr] = true
		w.WriteHeader(http.StatusNoContent)
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	client := NewNonReplayingClient(true)
	defer client.CloseIdleConnections()
	transport := client.Transport.(*policyTransport).base
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	transport.TLSClientConfig = &tls.Config{RootCAs: roots} // Trust only this test certificate.
	for _, method := range []string{http.MethodGet, http.MethodDelete, http.MethodPost, http.MethodGet} {
		req, err := http.NewRequestWithContext(t.Context(), method, server.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusNoContent {
			t.Fatal("unexpected status")
		}
	}
	if requests.Load() != 4 || len(addresses) != 4 {
		t.Fatal("unexpected request/connection count")
	}
	ordinaryClient := NewClient(true)
	defer ordinaryClient.CloseIdleConnections()
	ordinary := ordinaryClient.Transport.(*policyTransport).base
	if ordinary.DisableKeepAlives || !ordinary.ForceAttemptHTTP2 || ordinary.Protocols != nil {
		t.Fatal("ordinary client behavior changed")
	}
	ordinary.TLSClientConfig = &tls.Config{RootCAs: roots}
	response, err := ordinaryClient.Get(server.URL + "/http2-control")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.ProtoMajor != 2 || requests.Load() != 5 {
		t.Fatal("HTTP/2 control failed")
	}
}

func TestNonReplayingClientDroppedResponse(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodGet, http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				_, _ = io.Copy(io.Discard, r.Body)
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				_ = conn.Close() // Possible committed effect, no response acknowledgement.
			}))
			defer server.Close()
			client := NewNonReplayingClient(true)
			defer client.CloseIdleConnections()
			req, err := http.NewRequestWithContext(t.Context(), method, server.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			response, err := client.Do(req)
			if response != nil {
				_ = response.Body.Close()
			}
			if err == nil || requests.Load() != 1 {
				t.Fatalf("failed=%v requests=%d", err != nil, requests.Load())
			}
		})
	}
}

func TestNonReplayingClientPolicy(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected.Add(1) }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	client := NewNonReplayingClient(true)
	defer client.CloseIdleConnections()
	response, err := client.Get(server.URL)
	if response != nil {
		_ = response.Body.Close()
	}
	if err == nil || redirected.Load() != 0 {
		t.Fatal("redirect policy bypassed")
	}
	public := NewNonReplayingClient(false)
	defer public.CloseIdleConnections()
	response, err = public.Get(server.URL)
	if response != nil {
		_ = response.Body.Close()
	}
	if err == nil || !IsPreRequestFailure(err) {
		t.Fatal("address policy bypassed")
	}
	tlsServer := httptest.NewTLSServer(http.NotFoundHandler())
	defer tlsServer.Close()
	response, err = client.Get(tlsServer.URL)
	if response != nil {
		_ = response.Body.Close()
	}
	if err == nil {
		t.Fatal("untrusted TLS was accepted")
	}
}
