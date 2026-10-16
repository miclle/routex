package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPersonalAndProjectKeyGrammarRemainBounded(t *testing.T) {
	for _, prefix := range []string{"rx_", "rxp_"} {
		key := prefix + strings.Repeat("a", 43)
		if !validKey([]byte(key)) {
			t.Fatal("current Personal or Project key grammar rejected")
		}
		for _, bad := range []string{key + "\n", key + "a", key[:len(key)-1], strings.ToUpper(prefix) + strings.Repeat("a", 43), prefix + strings.Repeat("a", 42) + "/"} {
			if validKey([]byte(bad)) {
				t.Fatal("unbounded or noncanonical Key accepted")
			}
		}
	}
	for _, bad := range []string{"", "rx", "rxp", "key_" + strings.Repeat("a", 43), "pky_" + strings.Repeat("a", 43)} {
		if validKey([]byte(bad)) {
			t.Fatal("record identity or truncated prefix accepted as a bearer")
		}
	}
}

func TestProjectKeyInvocationPreservesNativeProofAndSingleDispatch(t *testing.T) {
	key := []byte("rxp_" + strings.Repeat("a", 43))
	if !validKey(key) {
		t.Fatal("Project bearer refused before invocation")
	}
	good := validResponse(t)
	for _, native := range []string{"incomplete", "completed"} {
		t.Run(native, func(t *testing.T) {
			body := good
			if native == "incomplete" {
				body = bytes.Replace(good, []byte(`"stop"`), []byte(`"length"`), 1)
			}
			var count atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count.Add(1)
				if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer "+string(key) {
					t.Error("Project invocation lost the exact installed bearer")
				}
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Request-ID", testRequestID)
				_, _ = w.Write(body)
			}))
			defer server.Close()
			observed := invoke(context.Background(), server.URL, key, newClient())
			if count.Load() != 1 || observed.Result != "response" || observed.Status != 200 || observed.Native != native || observed.RequestID != testRequestID {
				t.Fatal("Project native response was promoted, lost or replayed")
			}
			var out bytes.Buffer
			emit(&out, observed)
			if strings.Contains(out.String(), string(key)) || strings.Contains(out.String(), "Authorization") || strings.Contains(out.String(), prompt) {
				t.Fatal("Project invocation output leaked sensitive input")
			}
		})
	}
}

func TestProjectPredecessorProbeRequires401WithoutRetryOrSecretOutput(t *testing.T) {
	key := []byte("rxp_" + strings.Repeat("z", 43))
	if !validKey(key) {
		t.Fatal("Project predecessor bearer rejected")
	}
	for _, status := range []int{200, 401, 403, 302, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var count atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count.Add(1)
				if r.Header.Get("Authorization") != "Bearer "+string(key) {
					t.Error("Project predecessor probe lost the exact preserved bearer")
				}
				w.Header().Set("X-Request-ID", testRequestID)
				w.Header().Set("Location", "/must-not-follow")
				w.WriteHeader(status)
				_, _ = w.Write(key)
			}))
			defer server.Close()
			observed := probePredecessor(context.Background(), server.URL, key, newClient())
			if count.Load() != 1 || (observed.Result == "revoked_rejected") != (status == 401) {
				t.Fatal("Project predecessor refusal was promoted or retried")
			}
			var out bytes.Buffer
			emitProbe(&out, observed)
			if strings.Contains(out.String(), string(key)) || strings.Contains(out.String(), "native") || strings.Contains(out.String(), "Authorization") {
				t.Fatal("Project predecessor result leaked input or completion claims")
			}
		})
	}
}
