package vault

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const apiKeyWriteFixtureResponse = `{"request_id":"test-request","lease_id":"","lease_duration":0,"renewable":false,"data":{"created_time":"2026-10-10T00:00:00Z","version":1,"destroyed":false,"deletion_time":"","custom_metadata":null},"auth":null,"wrap_info":null,"warnings":null,"errors":[]}`
const apiKeyWriteFixtureToken = "writer-token-private-sentinel"
const apiKeyWriteFixtureBearer = "rx_key-private-sentinel"

type apiKeyWriteFixture struct {
	mu      sync.Mutex
	paths   []string
	valid   bool
	respond func(http.ResponseWriter, *http.Request)
}

func (f *apiKeyWriteFixture) serve(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxAPIKeyWriteBodyBytes+1))
	_ = r.Body.Close()
	defer clear(raw)
	var body map[string]json.RawMessage
	var options map[string]json.RawMessage
	var data map[string]string
	valid := err == nil && len(raw) <= maxAPIKeyWriteBodyBytes && json.Unmarshal(raw, &body) == nil && len(body) == 2 && json.Unmarshal(body["options"], &options) == nil && len(options) == 1 && string(options["cas"]) == "0" && json.Unmarshal(body["data"], &data) == nil && len(data) == 1 && data["key"] == apiKeyWriteFixtureBearer && r.Method == http.MethodPost && r.URL.RawQuery == "" && r.Header.Get("X-Vault-Token") == apiKeyWriteFixtureToken && r.Header.Get("X-Vault-Request") == "true" && r.Header.Get("X-Vault-Namespace") == "applications/test" && r.Header.Get("Content-Type") == "application/json" && r.Close && r.ProtoMajor == 1
	f.mu.Lock()
	f.paths = append(f.paths, r.URL.Path)
	f.valid = f.valid && valid
	f.mu.Unlock()
	if !valid {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if f.respond != nil {
		f.respond(w, r)
		return
	}
	_, _ = io.WriteString(w, apiKeyWriteFixtureResponse)
}

func (f *apiKeyWriteFixture) snapshot() ([]string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.paths...), f.valid
}

func apiKeyWriteFixtureClient(t *testing.T, respond func(http.ResponseWriter, *http.Request)) (*APIKeyWriter, *apiKeyWriteFixture) {
	t.Helper()
	f := &apiKeyWriteFixture{valid: true, respond: respond}
	s := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(s.Close)
	w, err := NewAPIKeyWriter(Descriptor{Endpoint: s.URL + "/root", Namespace: "applications/test", Mount: "kv", Prefix: "personal/owner/application/test", DataField: "key"}, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)
	return w, f
}

func apiKeyWritePrepared(t *testing.T, w *APIKeyWriter, id string) *PreparedAPIKeyWrite {
	t.Helper()
	p, err := w.Prepare(id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}

func apiKeyWriteAssertUnknown(t *testing.T, r APIKeyWriteResult, err error) {
	t.Helper()
	if err == nil || !r.Consumed || r.Outcome != APIKeyWriteUnknown || r.Version != 0 || !r.Write.Attempted || r.Write.Succeeded || r.Write.Failure == nil {
		t.Fatal("unproven write was acknowledged or hidden", r.Outcome)
	}
	for _, value := range []string{apiKeyWriteFixtureToken, apiKeyWriteFixtureBearer, "remote-private-detail"} {
		encoded, marshalErr := json.Marshal(r)
		if marshalErr != nil || strings.Contains(string(encoded), value) || strings.Contains(err.Error(), value) || strings.Contains(fmt.Sprintf("%+v", r), value) {
			t.Fatal("private material escaped")
		}
	}
}

func TestAPIKeyWriteExactCAS0MetadataAndStableDestination(t *testing.T) {
	w, f := apiKeyWriteFixtureClient(t, nil)
	for _, id := range []string{"key_01stable", "pky_01stable"} {
		p := apiKeyWritePrepared(t, w, id)
		again := apiKeyWritePrepared(t, w, id)
		if p.Plan() != again.Plan() || p.Plan().ResourceID != id || !lowerHex(p.Plan().DescriptorSHA256, 64) {
			t.Fatal("unstable nonsecret plan")
		}
		before, _ := f.snapshot()
		if len(before) != map[string]int{"key_01stable": 0, "pky_01stable": 1}[id] {
			t.Fatal("preparation made a request")
		}
		encoded, err := json.Marshal(p)
		if err != nil || string(encoded) != "{}" || strings.Contains(fmt.Sprintf("%#v", *p), id) {
			t.Fatal("handle formatting not redacted")
		}
		value := []byte(apiKeyWriteFixtureBearer)
		r, err := w.Write(t.Context(), apiKeyWriteFixtureToken, p, value)
		clear(value)
		if err != nil || r.Outcome != APIKeyWriteAcknowledged || !r.Consumed || r.Version != 1 || !r.Write.Attempted || !r.Write.Succeeded || r.Write.Failure != nil || r.Plan != p.Plan() || r.Write.Duration <= 0 {
			t.Fatal("known creation not acknowledged", err)
		}
		if state := w.ResponseCloseState(); !state.Observed || state.Pending != 0 || state.Failed {
			t.Fatal("body not proven closed")
		}
	}
	paths, valid := f.snapshot()
	if !valid || !reflect.DeepEqual(paths, []string{"/root/v1/kv/data/personal/owner/application/test/routex-api-key-key_01stable", "/root/v1/kv/data/personal/owner/application/test/routex-api-key-pky_01stable"}) {
		t.Fatal("wrong or extra requests")
	}
	// Copy-only Plan cannot act as a restored capability.
	r, err := w.Write(t.Context(), apiKeyWriteFixtureToken, &PreparedAPIKeyWrite{}, []byte(apiKeyWriteFixtureBearer))
	if err == nil || r.Consumed || r.Write.Attempted {
		t.Fatal("restored zero handle admitted")
	}
}

func TestAPIKeyWriteSharedClaimClosedAndDescriptorAdmission(t *testing.T) {
	w, f := apiKeyWriteFixtureClient(t, nil)
	p := apiKeyWritePrepared(t, w, "key_concurrent")
	copied := *p
	var wg sync.WaitGroup
	var acknowledgements atomic.Int32
	start := make(chan struct{})
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			h := p
			if i%2 == 0 {
				h = &copied
			}
			r, err := w.Write(t.Context(), apiKeyWriteFixtureToken, h, []byte(apiKeyWriteFixtureBearer))
			if err == nil && r.Outcome == APIKeyWriteAcknowledged {
				acknowledgements.Add(1)
				return
			}
			if err == nil || r.Consumed || r.Write.Attempted || r.Write.Failure.Code != "already_attempted" {
				t.Error("duplicate call lost local rejection")
			}
		}()
	}
	close(start)
	wg.Wait()
	paths, valid := f.snapshot()
	if !valid || len(paths) != 1 || acknowledgements.Load() != 1 {
		t.Fatal("copy/concurrent attempt replayed")
	}
	closed := apiKeyWritePrepared(t, w, "key_closed")
	closedCopy := *closed
	closed.Close()
	r, err := w.Write(t.Context(), apiKeyWriteFixtureToken, &closedCopy, []byte(apiKeyWriteFixtureBearer))
	if err == nil || r.Consumed || r.Write.Attempted || r.Write.Failure.Code != "prepared_closed" {
		t.Fatal("Close did not fence copy")
	}
	other, _ := apiKeyWriteFixtureClient(t, nil)
	foreign := apiKeyWritePrepared(t, other, "key_foreign")
	r, err = w.Write(t.Context(), apiKeyWriteFixtureToken, foreign, []byte(apiKeyWriteFixtureBearer))
	if err == nil || r.Consumed || r.Write.Attempted || r.Write.Failure.Code != "invalid_plan" {
		t.Fatal("foreign descriptor accepted")
	}
	paths, _ = f.snapshot()
	if len(paths) != 1 {
		t.Fatal("rejected input dispatched")
	}
}

func TestAPIKeyWriteInvalidInputsNeverDispatch(t *testing.T) {
	base := Descriptor{Endpoint: "https://vault.example", Mount: "kv", Prefix: "keys", DataField: "key"}
	for _, field := range []string{credentialMarkerField, "", "../key", "a/b", strings.Repeat("a", 65)} {
		d := base
		d.DataField = field
		if w, err := NewAPIKeyWriter(d, false); err == nil || w != nil {
			t.Fatal("invalid field admitted")
		}
	}
	for _, endpoint := range []string{"http://vault.example", "https://vault.example?x=y", "https://u:p@vault.example", "https://vault.example/#x", "https://vault.example/%2f", "https://vault.example/a/../b", "https://168.63.129.16", "https://127.0.0.1", "https://vault.example//a"} {
		d := base
		d.Endpoint = endpoint
		if w, err := NewAPIKeyWriter(d, false); err == nil || w != nil {
			t.Fatal("unsafe endpoint admitted")
		}
	}
	w, f := apiKeyWriteFixtureClient(t, nil)
	for _, id := range []string{"", ".", "..", "a/b", "a%2fb", " key", "key.", strings.Repeat("a", 129), "é"} {
		if p, err := w.Prepare(id); err == nil || p != nil {
			t.Fatal("invalid resource admitted")
		}
	}
	p := apiKeyWritePrepared(t, w, "key_inputs")
	for _, token := range []string{"", " token", "token\n", strings.Repeat("a", 4097), "é"} {
		r, err := w.Write(t.Context(), token, p, []byte(apiKeyWriteFixtureBearer))
		if err == nil || r.Consumed || r.Write.Attempted || r.Outcome != APIKeyWriteNotAttempted {
			t.Fatal("invalid token dispatched")
		}
	}
	for _, value := range [][]byte{nil, {}, []byte("key with space"), []byte("key\x00"), []byte("é"), []byte(strings.Repeat("a", 4097))} {
		r, err := w.Write(t.Context(), apiKeyWriteFixtureToken, p, value)
		if err == nil || r.Consumed || r.Write.Attempted || r.Outcome != APIKeyWriteNotAttempted {
			t.Fatal("invalid value dispatched")
		}
	}
	//lint:ignore SA1012 The public nil-context guard is deliberately exercised.
	//nolint:staticcheck // Deliberate invalid API input.
	r, err := w.Write(nil, apiKeyWriteFixtureToken, p, []byte(apiKeyWriteFixtureBearer))
	if err == nil || r.Consumed || r.Write.Attempted {
		t.Fatal("nil context admitted")
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	r, err = w.Write(cancelled, apiKeyWriteFixtureToken, p, []byte(apiKeyWriteFixtureBearer))
	if err == nil || r.Consumed || r.Write.Attempted || r.Write.Failure.Code != "canceled" {
		t.Fatal("canceled input dispatched")
	}
	past, stop := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer stop()
	r, err = w.Write(past, apiKeyWriteFixtureToken, p, []byte(apiKeyWriteFixtureBearer))
	if err == nil || r.Consumed || r.Write.Attempted || r.Write.Failure.Code != "timed_out" {
		t.Fatal("expired input dispatched")
	}
	if paths, _ := f.snapshot(); len(paths) != 0 {
		t.Fatal("invalid input made HTTP request")
	}
	// Invalid local inputs did not consume the valid original handle.
	r, err = w.Write(t.Context(), apiKeyWriteFixtureToken, p, []byte(apiKeyWriteFixtureBearer))
	if err != nil || r.Outcome != APIKeyWriteAcknowledged {
		t.Fatal("invalid preflight consumed handle")
	}
}

func TestAPIKeyWriteExactResponseAdmission(t *testing.T) {
	valid := `{"data":{"version":1,"destroyed":false,"deletion_time":""}}`
	cases := []struct {
		name, body   string
		status       int
		mime         string
		acknowledged bool
	}{
		{"normal_metadata", apiKeyWriteFixtureResponse, 200, "application/json", true},
		{"replacement_character", strings.Replace(valid, `{"data":`, `{"note":"�","data":`, 1), 200, "application/json", true},
		{"surrogate_pair", strings.Replace(valid, `{"data":`, `{"note":"\ud83d\ude00","data":`, 1), 200, "application/json", true},
		{"escaped_backslash", strings.Replace(valid, `{"data":`, `{"note":"\\ud800","data":`, 1), 200, "application/json", true},
		{"version_two", strings.Replace(valid, `"version":1`, `"version":2`, 1), 200, "application/json", false},
		{"version_decimal", strings.Replace(valid, `"version":1`, `"version":1.0`, 1), 200, "application/json", false},
		{"version_exponent", strings.Replace(valid, `"version":1`, `"version":1e0`, 1), 200, "application/json", false},
		{"version_null", strings.Replace(valid, `"version":1`, `"version":null`, 1), 200, "application/json", false},
		{"version_string", strings.Replace(valid, `"version":1`, `"version":"1"`, 1), 200, "application/json", false},
		{"version_missing", strings.Replace(valid, `"version":1,`, ``, 1), 200, "application/json", false},
		{"version_overflow", strings.Replace(valid, `"version":1`, `"version":9223372036854775808`, 1), 200, "application/json", false},
		{"destroyed_true", strings.Replace(valid, `"destroyed":false`, `"destroyed":true`, 1), 200, "application/json", false},
		{"destroyed_null", strings.Replace(valid, `"destroyed":false`, `"destroyed":null`, 1), 200, "application/json", false},
		{"destroyed_missing", strings.Replace(valid, `"destroyed":false,`, ``, 1), 200, "application/json", false},
		{"deleted", strings.Replace(valid, `"deletion_time":""`, `"deletion_time":"now"`, 1), 200, "application/json", false},
		{"deletion_null", strings.Replace(valid, `"deletion_time":""`, `"deletion_time":null`, 1), 200, "application/json", false},
		{"deletion_missing", strings.Replace(valid, `,"deletion_time":""`, ``, 1), 200, "application/json", false},
		{"duplicate", `{"data":{"version":2,"version":1,"destroyed":false,"deletion_time":""}}`, 200, "application/json", false},
		{"escaped_duplicate", `{"data":{"version":2,"\u0076ersion":1,"destroyed":false,"deletion_time":""}}`, 200, "application/json", false},
		{"nested_duplicate", strings.Replace(valid, `{"data":`, `{"extra":{"a":1,"a":2},"data":`, 1), 200, "application/json", false},
		{"high_surrogate", strings.Replace(valid, `{"data":`, `{"note":"\ud800","data":`, 1), 200, "application/json", false},
		{"low_surrogate_key", strings.Replace(valid, `{"data":`, `{"\udc00":1,"data":`, 1), 200, "application/json", false},
		{"nonpair", strings.Replace(valid, `{"data":`, `{"note":"\ud800\u0041","data":`, 1), 200, "application/json", false},
		{"invalid_utf8", strings.Replace(valid, `{"data":`, "{\"note\":\"\xff\",\"data\":", 1), 200, "application/json", false},
		{"errors", strings.Replace(valid, `{"data":`, `{"errors":["remote-private-detail"],"data":`, 1), 200, "application/json", false},
		{"errors_null", strings.Replace(valid, `{"data":`, `{"errors":null,"data":`, 1), 200, "application/json", false},
		{"wrapped", strings.Replace(valid, `{"data":`, `{"wrap_info":{"token":"remote-private-detail"},"data":`, 1), 200, "application/json", false},
		{"auth", strings.Replace(valid, `{"data":`, `{"auth":{},"data":`, 1), 200, "application/json", false},
		{"trailing", valid + `{}`, 200, "application/json", false},
		{"array", `[]`, 200, "application/json", false},
		{"null", `null`, 200, "application/json", false},
		{"html", valid, 200, "text/html", false},
		{"empty", ``, 200, "application/json", false},
		{"created", valid, 201, "application/json", false},
		{"no_content", ``, 204, "application/json", false},
		{"denied", `{"errors":["remote-private-detail"]}`, 403, "application/json", false},
		{"cas_conflict", valid, 409, "application/json", false},
		{"rate_limited", valid, 429, "application/json", false},
		{"server_error", valid, 500, "application/json", false},
		{"at_limit", valid + strings.Repeat(" ", maxResponseBytes-len(valid)), 200, "application/json", true},
		{"over_limit", valid + strings.Repeat(" ", maxResponseBytes-len(valid)+1), 200, "application/json", false},
		{"deep", strings.Replace(valid, `{"data":`, `{"extra":`+strings.Repeat(`[`, 17)+`0`+strings.Repeat(`]`, 17)+`,"data":`, 1), 200, "application/json", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, f := apiKeyWriteFixtureClient(t, func(rw http.ResponseWriter, _ *http.Request) {
				rw.Header().Set("Content-Type", tc.mime)
				rw.WriteHeader(tc.status)
				_, _ = io.WriteString(rw, tc.body)
			})
			p := apiKeyWritePrepared(t, w, "key_response")
			r, err := w.Write(t.Context(), apiKeyWriteFixtureToken, p, []byte(apiKeyWriteFixtureBearer))
			if tc.acknowledged {
				if err != nil || r.Outcome != APIKeyWriteAcknowledged || r.Version != 1 || !r.Write.Succeeded {
					t.Fatal("valid metadata rejected", err)
				}
			} else {
				apiKeyWriteAssertUnknown(t, r, err)
			}
			paths, valid := f.snapshot()
			if !valid || len(paths) != 1 {
				t.Fatal("response triggered retry or read")
			}
			if state := w.ResponseCloseState(); !state.Observed || state.Pending != 0 || state.Failed {
				t.Fatal("returned response not closed")
			}
		})
	}
}

func TestAPIKeyWriteLostCommittedResponseAndRedirectNeverReplay(t *testing.T) {
	var committed atomic.Bool
	w, f := apiKeyWriteFixtureClient(t, func(rw http.ResponseWriter, _ *http.Request) {
		conn, _, err := rw.(http.Hijacker).Hijack()
		if err != nil {
			t.Error("controlled hijack failed")
			return
		}
		committed.Store(true)
		_ = conn.Close()
	})
	p := apiKeyWritePrepared(t, w, "key_lost")
	r, err := w.Write(t.Context(), apiKeyWriteFixtureToken, p, []byte(apiKeyWriteFixtureBearer))
	apiKeyWriteAssertUnknown(t, r, err)
	if !committed.Load() {
		t.Fatal("fixture never committed before response loss")
	}
	r, err = w.Write(t.Context(), apiKeyWriteFixtureToken, p, []byte(apiKeyWriteFixtureBearer))
	if err == nil || r.Consumed || r.Write.Attempted || r.Write.Failure.Code != "already_attempted" {
		t.Fatal("lost response replayed")
	}
	if paths, _ := f.snapshot(); len(paths) != 1 {
		t.Fatal("lost write read or replayed")
	}
	for _, status := range []int{301, 302, 303, 307, 308} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var targetCalls atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { targetCalls.Add(1) }))
			t.Cleanup(target.Close)
			w, f := apiKeyWriteFixtureClient(t, func(rw http.ResponseWriter, r *http.Request) { http.Redirect(rw, r, target.URL, status) })
			r, err := w.Write(t.Context(), apiKeyWriteFixtureToken, apiKeyWritePrepared(t, w, "key_redirect"), []byte(apiKeyWriteFixtureBearer))
			apiKeyWriteAssertUnknown(t, r, err)
			if paths, _ := f.snapshot(); len(paths) != 1 || targetCalls.Load() != 0 {
				t.Fatal("redirect followed or replayed")
			}
			// With GetBody absent, Go returns 307/308 directly to the SDK;
			// the redirect policy handles 301/302/303 and Go closes internally.
			wantClose := ResponseCloseState{Failed: true}
			wantCode, wantStatus := "transport", 0
			if status == http.StatusTemporaryRedirect || status == http.StatusPermanentRedirect {
				wantClose = ResponseCloseState{Observed: true}
				wantCode, wantStatus = "http_status", status
			}
			if state := w.ResponseCloseState(); state != wantClose {
				t.Fatalf("redirect %d closure state: observed=%t pending=%d failed=%t; want observed=%t pending=%d failed=%t", status, state.Observed, state.Pending, state.Failed, wantClose.Observed, wantClose.Pending, wantClose.Failed)
			}
			if failure := r.Write.Failure; failure.Code != wantCode || failure.HTTPStatus != wantStatus {
				t.Fatalf("redirect %d failure: code=%s status=%d; want code=%s status=%d", status, failure.Code, failure.HTTPStatus, wantCode, wantStatus)
			}
		})
	}
}

type apiKeyWriteRoundTripper func(*http.Request) (*http.Response, error)

func (f apiKeyWriteRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type apiKeyWriteBody struct {
	io.Reader
	read   int
	closes int
	close  func() error
}

func (b *apiKeyWriteBody) Read(p []byte) (int, error) {
	n, e := b.Reader.Read(p)
	b.read += n
	return n, e
}
func (b *apiKeyWriteBody) Close() error {
	b.closes++
	if b.close != nil {
		return b.close()
	}
	return nil
}

func TestAPIKeyWriteCloseAndOriginalDeadlineAdmission(t *testing.T) {
	for _, mode := range []string{"success", "sdk_cap", "close_error", "cancel_on_close", "deadline_on_close", "read_error", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			w, _ := apiKeyWriteFixtureClient(t, nil)
			parentBudget := 3 * time.Second
			if mode == "sdk_cap" {
				parentBudget = 30 * time.Second
			}
			ctx, cancel := context.WithTimeout(t.Context(), parentBudget)
			defer cancel()
			var capturedDeadline time.Time
			calls := 0
			body := &apiKeyWriteBody{Reader: strings.NewReader(apiKeyWriteFixtureResponse)}
			if mode == "read_error" {
				body.Reader = apiKeyWriteReadError{}
			}
			if mode == "oversize" {
				body.Reader = strings.NewReader(strings.Repeat("x", maxResponseBytes*3))
			}
			body.close = func() error {
				if mode == "close_error" {
					return errors.New("remote-private-detail")
				}
				if mode == "cancel_on_close" {
					cancel()
				}
				if mode == "deadline_on_close" {
					<-ctx.Done()
				}
				return nil
			}
			w.client.http.Transport = apiKeyWriteRoundTripper(func(req *http.Request) (*http.Response, error) {
				calls++
				parentDeadline, _ := ctx.Deadline()
				actual, ok := req.Context().Deadline()
				capturedDeadline = actual
				if !ok || mode != "sdk_cap" && !actual.Equal(parentDeadline) || mode == "sdk_cap" && !actual.Before(parentDeadline) {
					t.Error("original bounded deadline changed")
				}
				if req.GetBody != nil || !req.Close || req.URL.RawQuery != "" {
					t.Error("replayable request")
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: body}, nil
			})
			admission := time.Now()
			r, err := w.Write(ctx, apiKeyWriteFixtureToken, apiKeyWritePrepared(t, w, "key_close"), []byte(apiKeyWriteFixtureBearer))
			returned := time.Now()
			if mode == "sdk_cap" && (capturedDeadline.Before(admission.Add(requestTimeout)) || capturedDeadline.After(returned.Add(requestTimeout))) {
				t.Fatal("SDK deadline not captured once at admission")
			}
			if mode == "success" || mode == "sdk_cap" {
				if err != nil || r.Outcome != APIKeyWriteAcknowledged {
					t.Fatal("closed response rejected", err)
				}
			} else {
				apiKeyWriteAssertUnknown(t, r, err)
			}
			if mode == "close_error" && r.Write.Failure.Code != "response_close_failed" {
				t.Fatal("close error lost")
			}
			if mode == "cancel_on_close" && r.Write.Failure.Code != "canceled" {
				t.Fatal("late cancellation lost")
			}
			if mode == "deadline_on_close" && r.Write.Failure.Code != "timed_out" {
				t.Fatal("original deadline expiry lost")
			}
			if calls != 1 || body.closes != 1 {
				t.Fatal("body close/request repeated")
			}
			if mode == "oversize" && body.read != maxResponseBytes+1 {
				t.Fatal("unbounded draining")
			}
			state := w.ResponseCloseState()
			if !state.Observed || state.Pending != 0 || state.Failed != (mode == "close_error") {
				t.Fatal("incorrect closure evidence")
			}
			w.Close()
			if w.ResponseCloseState() != state {
				t.Fatal("idle disposal reset closure evidence")
			}
		})
	}
}

type apiKeyWriteReadError struct{}

func (apiKeyWriteReadError) Read([]byte) (int, error) { return 0, errors.New("remote-private-detail") }

func TestAPIKeyWritePendingCloseJoinsBeforeAcknowledgement(t *testing.T) {
	w, _ := apiKeyWriteFixtureClient(t, nil)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	body := &apiKeyWriteBody{Reader: strings.NewReader(apiKeyWriteFixtureResponse), close: func() error { close(entered); <-release; return nil }}
	w.client.http.Transport = apiKeyWriteRoundTripper(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: body}, nil
	})
	p := apiKeyWritePrepared(t, w, "key_held_close")
	type answer struct {
		r   APIKeyWriteResult
		err error
	}
	done := make(chan answer, 1)
	joined := false
	defer func() {
		once.Do(func() { close(release) })
		if !joined {
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Error("owned held-Close request failed to join")
			}
		}
	}()
	go func() {
		r, err := w.Write(t.Context(), apiKeyWriteFixtureToken, p, []byte(apiKeyWriteFixtureBearer))
		done <- answer{r, err}
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("Close not entered")
	}
	state := w.ResponseCloseState()
	if !state.Observed || state.Pending != 1 || state.Failed {
		t.Fatal("pending Close not tracked")
	}
	w.Close()
	if w.ResponseCloseState() != state {
		t.Fatal("idle disposal claimed join")
	}
	select {
	case <-done:
		t.Fatal("returned while Close held")
	default:
	}
	once.Do(func() { close(release) })
	select {
	case a := <-done:
		joined = true
		if a.err != nil || a.r.Outcome != APIKeyWriteAcknowledged {
			t.Fatal("finite close not admitted", a.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("owned request not joined")
	}
	if body.closes != 1 || w.ResponseCloseState().Pending != 0 {
		t.Fatal("Close not completed once")
	}
}

func TestAPIKeyWriteCancellationClosesRealRequestAndTLSRemainsStrict(t *testing.T) {
	entered, returned := make(chan struct{}), make(chan struct{})
	w, f := apiKeyWriteFixtureClient(t, func(rw http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		close(returned)
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	type answer struct {
		r   APIKeyWriteResult
		err error
	}
	done := make(chan answer, 1)
	p := apiKeyWritePrepared(t, w, "key_cancel")
	joined := false
	defer func() {
		cancel()
		if !joined {
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Error("owned canceled request failed to join")
			}
		}
	}()
	go func() {
		r, err := w.Write(ctx, apiKeyWriteFixtureToken, p, []byte(apiKeyWriteFixtureBearer))
		done <- answer{r, err}
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not enter")
	}
	cancel()
	select {
	case a := <-done:
		joined = true
		apiKeyWriteAssertUnknown(t, a.r, a.err)
		if a.r.Write.Failure.Code != "canceled" {
			t.Fatal("cancellation lost")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("request did not stop")
	}
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("peer request not joined")
	}
	if paths, _ := f.snapshot(); len(paths) != 1 {
		t.Fatal("canceled write retried or read")
	}
	var tlsRequests atomic.Int32
	tlsPeer := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { tlsRequests.Add(1) }))
	t.Cleanup(tlsPeer.Close)
	strict, err := NewAPIKeyWriter(Descriptor{Endpoint: tlsPeer.URL, Mount: "kv", Prefix: "keys", DataField: "key"}, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(strict.Close)
	r, err := strict.Write(t.Context(), apiKeyWriteFixtureToken, apiKeyWritePrepared(t, strict, "key_tls"), []byte(apiKeyWriteFixtureBearer))
	apiKeyWriteAssertUnknown(t, r, err)
	if tlsRequests.Load() != 0 {
		t.Fatal("untrusted certificate bypassed")
	}
}
