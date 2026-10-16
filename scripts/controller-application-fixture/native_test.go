package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const testSecret = "NeverLogThisControlledSecret123456"
const testRequestID = "req_01arz3ndektsv4rrffq69g5fav"

func peerRequest(p http.Handler, method, path, body, auth string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if auth != "" {
		r.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)
	return w
}
func validPeerBody() string {
	return `{"model":"controller-native-upstream","messages":[{"role":"user","content":"Controlled acceptance"}],"stream":false,"max_tokens":16}`
}
func TestPeerAuthenticatesAndBounds(t *testing.T) {
	var out bytes.Buffer
	p := newPeer([]byte(testSecret), []string{"length", "stop"}, 20, &out)
	auth := "Bearer " + testSecret
	if peerRequest(p, "GET", "/v1/models", "", "").Code != 401 {
		t.Fatal("unauthorized discovery")
	}
	if peerRequest(p, "POST", "/v1/chat/completions", validPeerBody(), "Bearer wrong").Code != 401 {
		t.Fatal("unauthorized inference")
	}
	if peerRequest(p, "GET", "/v1/models", "", auth).Code != 200 {
		t.Fatal("authorized discovery")
	}
	for _, path := range []string{"/v1/models?q=secret", "/v1/%6dodels", "/v1/chat/completions/", "/arbitrary"} {
		if peerRequest(p, "POST", path, validPeerBody(), auth).Code != 404 {
			t.Fatal("accepted path variant")
		}
	}
	if peerRequest(p, "POST", "/v1/chat/completions", strings.Repeat("x", maxBody+1), auth).Code != 413 {
		t.Fatal("body bound")
	}
	if peerRequest(p, "POST", "/v1/chat/completions", strings.Replace(validPeerBody(), "false", "true", 1), auth).Code != 400 {
		t.Fatal("stream accepted")
	}
	for _, want := range []string{"incomplete", "completed"} {
		w := peerRequest(p, "POST", "/v1/chat/completions", validPeerBody(), auth)
		if w.Code != 200 || observeNative(w.Body.Bytes()) != want {
			t.Fatal("wrong native outcome")
		}
	}
	if peerRequest(p, "POST", "/v1/chat/completions", validPeerBody(), auth).Code != 429 {
		t.Fatal("call bound")
	}
	if strings.Contains(out.String(), testSecret) || strings.Contains(out.String(), prompt) || strings.Contains(out.String(), "Authorization") {
		t.Fatal("peer output leaked input")
	}
	p = newPeer([]byte(testSecret), []string{"stop"}, 1, io.Discard)
	if peerRequest(p, "GET", "/v1/models", "", auth).Code != 200 || peerRequest(p, "GET", "/v1/models", "", auth).Code != 429 {
		t.Fatal("request bound")
	}
}
func validResponse(t *testing.T) []byte {
	t.Helper()
	p := newPeer([]byte(testSecret), []string{"stop"}, 1, io.Discard)
	return peerRequest(p, "POST", "/v1/chat/completions", validPeerBody(), "Bearer "+testSecret).Body.Bytes()
}
func TestNativeProofRejectsAmbiguousOrIncompleteResponse(t *testing.T) {
	good := validResponse(t)
	for name, mutate := range map[string]func(map[string]any){
		"no usage": func(m map[string]any) { delete(m, "usage") }, "bad sum": func(m map[string]any) { m["usage"].(map[string]any)["total_tokens"] = 6 }, "multiple choices": func(m map[string]any) { m["choices"] = append(m["choices"].([]any), m["choices"].([]any)[0]) }, "tool": func(m map[string]any) {
			m["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)["tool_calls"] = []any{map[string]any{"id": "x"}}
		}, "refusal": func(m map[string]any) {
			m["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)["refusal"] = "no"
		}, "no finish": func(m map[string]any) { delete(m["choices"].([]any)[0].(map[string]any), "finish_reason") },
	} {
		t.Run(name, func(t *testing.T) {
			var m map[string]any
			_ = json.Unmarshal(good, &m)
			mutate(m)
			raw, _ := json.Marshal(m)
			if observeNative(raw) != "unknown" {
				t.Fatal("ambiguous native proof accepted")
			}
		})
	}
	if observeNative(bytes.Replace(good, []byte(`"stop"`), []byte(`"length"`), 1)) != "incomplete" {
		t.Fatal("length promoted")
	}
}
func TestInvokeNoRedirectRetryAndSanitizedOutput(t *testing.T) {
	good := validResponse(t)
	key := []byte("rx_" + strings.Repeat("a", 43))
	for _, test := range []struct {
		name   string
		status int
		body   []byte
		id     string
		want   string
		native string
	}{
		{"stop", 200, good, testRequestID, "response", "completed"},
		{"length", 200, bytes.Replace(good, []byte(`"stop"`), []byte(`"length"`), 1), testRequestID, "response", "incomplete"},
		{"unauthorized", 401, []byte(testSecret), testRequestID, "response", "unknown"},
		{"oversize", 200, bytes.Repeat([]byte("x"), maxResponse+1), testRequestID, "response_invalid", "unknown"},
		{"bad id", 200, good, testSecret, "response_invalid", "unknown"},
		{"redirect", 302, []byte(testSecret), testRequestID, "response", "unknown"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int64
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Header.Get("Authorization") != "Bearer "+string(key) {
					t.Error("missing bearer")
				}
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Request-ID", test.id)
				w.Header().Set("Location", "/followed")
				w.WriteHeader(test.status)
				_, _ = w.Write(test.body)
			}))
			defer s.Close()
			r := invoke(context.Background(), s.URL, key, newClient())
			if requests.Load() != 1 || r.Result != test.want || r.Native != test.native {
				t.Fatalf("wrong result: %+v count %d", r, requests.Load())
			}
			var out bytes.Buffer
			emit(&out, r)
			if strings.Contains(out.String(), testSecret) || strings.Contains(out.String(), string(key)) {
				t.Fatal("secret leaked")
			}
		})
	}
}
func TestInvokeTransportFailureIsSanitizedAndOneShot(t *testing.T) {
	calls := 0
	client := newClient()
	client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, fmt.Errorf("private detail: %s", testSecret)
	})
	r := invoke(context.Background(), "http://127.0.0.1:19001", []byte(testSecret), client)
	var out bytes.Buffer
	emit(&out, r)
	if calls != 1 || r.Result != "request_failed" || strings.Contains(out.String(), testSecret) {
		t.Fatal("unsafe failure")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPeerBoundsUnknownLengthBodyAndDuplicateAuthorization(t *testing.T) {
	p := newPeer([]byte(testSecret), []string{"stop"}, 5, io.Discard)
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(strings.Repeat("x", maxBody+1)))
	r.ContentLength = -1
	r.Header.Set("Authorization", "Bearer "+testSecret)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)
	if w.Code != 413 {
		t.Fatal("unknown-length body bypassed bound")
	}
	r = httptest.NewRequest("GET", "/v1/models", nil)
	r.Header.Add("Authorization", "Bearer "+testSecret)
	r.Header.Add("Authorization", "Bearer "+testSecret)
	w = httptest.NewRecorder()
	p.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("duplicate authorization accepted")
	}
	if newClient().Transport.(*http.Transport).Proxy != nil {
		t.Fatal("environment proxy enabled")
	}
}
func TestServeClosesAfterOwnedCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out bytes.Buffer
	c := serveConfig{Listen: "127.0.0.1:0", LifetimeSeconds: 1, MaxRequests: 1, FinishReasons: []string{"stop"}}
	if serve(ctx, c, []byte(testSecret), &out) != 0 || !strings.Contains(out.String(), `"result":"shutdown"`) {
		t.Fatal("owned shutdown failed")
	}
	if strings.Contains(out.String(), testSecret) {
		t.Fatal("shutdown output leaked")
	}
}

func TestPredecessorProbeRequires401AndNeverLeaksSecret(t *testing.T) {
	key := []byte("rx_" + strings.Repeat("z", 43))
	for _, status := range []int{200, 401, 403, 302, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var count atomic.Int64
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count.Add(1)
				w.Header().Set("X-Request-ID", testRequestID)
				w.Header().Set("Location", "/redirect")
				w.WriteHeader(status)
				_, _ = w.Write(key)
			}))
			defer s.Close()
			r := probePredecessor(context.Background(), s.URL, key, newClient())
			if count.Load() != 1 || (r.Result == "revoked_rejected") != (status == 401) {
				t.Fatal("predecessor status was promoted or retried")
			}
			var out bytes.Buffer
			emitProbe(&out, r)
			if strings.Contains(out.String(), string(key)) || strings.Contains(out.String(), "native") || strings.Contains(out.String(), "Authorization") {
				t.Fatal("unsafe predecessor output")
			}
		})
	}
}
