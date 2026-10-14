//go:build darwin || linux

package deliverycontroller

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func networkConfig(t *testing.T, origin string) Config {
	t.Helper()
	cfg := fixturePins()
	cfg.RouteXURL, cfg.AllowLoopbackHTTP = origin, true
	cfg.BearerFile = filepath.Join(privateDirectory(t), "bearer")
	bearer := "rxdctl_" + base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x24}, 32))
	writeFile(t, cfg.BearerFile, bearer, 0600)
	return cfg
}

func TestMachineClientNeverFollowsRedirects(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			var targetCalls, sourceCalls atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				targetCalls.Add(1)
				w.WriteHeader(http.StatusOK)
			}))
			defer target.Close()
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sourceCalls.Add(1)
				http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
			}))
			defer origin.Close()
			machine, err := newMachineClient(networkConfig(t, origin.URL))
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err = machine.request(context.Background(), method, machinePath+"/configuration", []byte(`{"test":"intent"}`)); err == nil {
				t.Fatal("redirect accepted")
			}
			if sourceCalls.Load() != 1 || targetCalls.Load() != 0 {
				t.Fatal("redirect replayed machine bearer or request")
			}
		})
	}
}

func TestMachineClientVerifiedTLSAndRedactedErrors(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("untrusted TLS endpoint received a request")
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	machine, err := newMachineClient(networkConfig(t, server.URL))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = machine.pull(context.Background()); err == nil {
		t.Fatal("untrusted TLS certificate accepted")
	}
	if strings.Contains(err.Error(), server.URL) || strings.Contains(err.Error(), "rxdctl_") {
		t.Fatal("raw transport details leaked")
	}
	for _, formatted := range []string{fmt.Sprint(machine), fmt.Sprintf("%#v", machine)} {
		if strings.Contains(formatted, "rxdctl_") {
			t.Fatal("machine formatter leaked bearer")
		}
	}
	transport, ok := machine.client.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil || !transport.DisableKeepAlives || transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("unsafe proxy, connection replay or TLS configuration")
	}
}

func TestMachineClientRejectsUnboundAndOversizedReplies(t *testing.T) {
	valid, err := json.Marshal(fixtureConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		status int
		mime   string
		etag   string
		body   string
	}{
		{name: "weak etag", status: 200, mime: "application/json", etag: "W/\"" + fixtureDigest + "\"", body: string(valid)},
		{name: "missing etag", status: 200, mime: "application/json", body: string(valid)},
		{name: "other etag", status: 200, mime: "application/json", etag: "\"" + strings.Repeat("a", 64) + "\"", body: string(valid)},
		{name: "HTML", status: 200, mime: "text/html", etag: "\"" + fixtureDigest + "\"", body: "raw-remote-secret-canary"},
		{name: "oversized", status: 200, mime: "application/json", etag: "\"" + fixtureDigest + "\"", body: strings.Repeat("x", maxJSONBytes+1)},
		{name: "revoked", status: 401, mime: "application/json", body: `{"error":"raw-remote-secret-canary"}`},
		{name: "changed assignment", status: 409, mime: "application/json", body: `{"error":"raw-remote-secret-canary"}`},
		{name: "duplicate identity", status: 200, mime: "application/json", etag: "\"" + fixtureDigest + "\"", body: strings.TrimSuffix(string(valid), "}") + `,"slot_id":"` + fixtureSlot + `"}`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", test.mime)
				w.Header().Set("Cache-Control", "private, no-store")
				w.Header().Set("X-Content-Type-Options", "nosniff")
				if test.etag != "" {
					w.Header().Set("ETag", test.etag)
				}
				w.WriteHeader(test.status)
				if _, err := w.Write([]byte(test.body)); err != nil {
					t.Errorf("fixture reply: %v", err)
				}
			}))
			defer server.Close()
			machine, err := newMachineClient(networkConfig(t, server.URL))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = machine.pull(context.Background()); err == nil {
				t.Fatal("unbound or invalid reply accepted")
			}
			if strings.Contains(err.Error(), "raw-remote-secret-canary") {
				t.Fatal("server secret leaked through error")
			}
		})
	}
}
