package vault

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fixture struct {
	mu          sync.Mutex
	requests    []string
	path, value string
	version     int64
	mutate      func(string, http.ResponseWriter, *http.Request) bool
}

func (f *fixture) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	stage := "read"
	if r.Method == http.MethodPost {
		stage = "write"
	}
	if r.Method == http.MethodPut {
		stage = "cleanup"
	}
	f.requests = append(f.requests, stage)
	wantToken := "writer-test-only"
	if stage == "read" {
		wantToken = "reader-test-only"
	}
	if r.Header.Get("X-Vault-Token") != wantToken || r.Header.Get("X-Vault-Namespace") != "acme/production" || r.Header.Get("X-Vault-Request") != "true" || !r.Close {
		w.WriteHeader(403)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if f.mutate != nil && f.mutate(stage, w, r) {
		return
	}
	switch stage {
	case "write":
		var data struct {
			Data    map[string]string `json:"data"`
			Options struct {
				CAS *int `json:"cas"`
			} `json:"options"`
		}
		if r.URL.RawQuery != "" || json.NewDecoder(r.Body).Decode(&data) != nil || data.Options.CAS == nil || *data.Options.CAS != 0 || len(data.Data) != 1 || !regexp.MustCompile(`^/vault/v1/kv/team/data/providers/routex-probe-[a-f0-9]{32}$`).MatchString(r.URL.Path) {
			w.WriteHeader(400)
			return
		}
		f.path = r.URL.Path
		f.value = data.Data["credential"]
		if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(f.value) {
			w.WriteHeader(400)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"version": f.version, "destroyed": false, "deletion_time": "", "created_time": "2026-10-07T00:00:00Z"}})
	case "read":
		if r.URL.Path != f.path || r.URL.RawQuery != "version=1" {
			w.WriteHeader(400)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"data": map[string]string{"credential": f.value}, "metadata": map[string]any{"version": 1, "destroyed": false, "deletion_time": ""}}})
	case "cleanup":
		var payload struct {
			Versions []int `json:"versions"`
		}
		if json.NewDecoder(r.Body).Decode(&payload) != nil || len(payload.Versions) != 1 || payload.Versions[0] != 1 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.URL.Path != strings.Replace(f.path, "/data/", "/destroy/", 1) || r.URL.RawQuery != "" {
			w.WriteHeader(400)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
func newFixture(t *testing.T, f *fixture) *Client {
	t.Helper()
	f.version = 1
	s := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(s.Close)
	c, err := New(Descriptor{Endpoint: s.URL + "/vault", Namespace: "acme/production", Mount: "kv/team", Prefix: "providers", DataField: "credential"}, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}
func probe(c *Client) (Result, error) {
	return c.Probe(context.Background(), "writer-test-only", "reader-test-only")
}
func TestProbeExactNativeFlowAndTransientIdentity(t *testing.T) {
	f := &fixture{}
	c := newFixture(t, f)
	for range 2 {
		previous := f.path
		r, err := probe(c)
		if err != nil || r.Version != 1 || !r.Write.Succeeded || !r.Read.Succeeded || r.Cleanup.State != "acknowledged" || !r.Cleanup.Observation.Succeeded || !strings.HasSuffix(f.path, r.ProbeID) || f.path == previous {
			t.Fatalf("flow: %+v %v", r, err)
		}
		if r.Write.Duration <= 0 || r.Read.Duration <= 0 || r.Cleanup.Observation.Duration <= 0 {
			t.Fatal("missing measured stage duration")
		}
	}
	if strings.Join(f.requests, ",") != "write,read,cleanup,write,read,cleanup" {
		t.Fatal("extra/missing requests")
	}
	result, err := probe(c)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), "test-only") || strings.Contains(string(encoded), f.value) {
		t.Fatal("result exposes authentication or probe value")
	}
}
func TestInvalidDescriptorAndTokensNeverDispatch(t *testing.T) {
	base := Descriptor{Endpoint: "https://vault.example", Mount: "secret", Prefix: "providers", DataField: "value"}
	invalid := []Descriptor{}
	for _, endpoint := range []string{"http://vault.example", "https://name:password@vault.example", "https://vault.example?token=secret", "https://vault.example/#x", "https://vault.example/a/../b", "https://vault.example/a%2fb", "https://vault.example//a", "https://127.0.0.1", "https://168.63.129.16", " https://vault.example", strings.Repeat("x", 2049)} {
		d := base
		d.Endpoint = endpoint
		invalid = append(invalid, d)
	}
	for _, path := range []string{"../secret", "secret/", "/secret", "a//b", "a/%2f", "a?x", "x\nheader", "a.", strings.Repeat("a", 129)} {
		d := base
		d.Mount = path
		invalid = append(invalid, d)
	}
	d := base
	d.Prefix = ""
	invalid = append(invalid, d)
	d = base
	d.Namespace = "/bad"
	invalid = append(invalid, d)
	d = base
	d.DataField = "nested/value"
	invalid = append(invalid, d)
	for i, d := range invalid {
		c, err := New(d, false)
		if err == nil || c != nil || err.Error() != "vault prepare: invalid_descriptor" {
			t.Fatalf("descriptor%d accepted", i)
		}
	}
	f := &fixture{}
	c := newFixture(t, f)
	for _, tokens := range [][2]string{{"same", "same"}, {"", "reader"}, {"writer", ""}, {"writer\r\nX:token", "reader"}, {"writer", "reader token"}, {strings.Repeat("a", 4097), "reader"}, {"writer", "读"}} {
		r, err := c.Probe(context.Background(), tokens[0], tokens[1])
		if err == nil || r.Write.Attempted || r.Cleanup.State != "not_attempted" {
			t.Fatal("invalid tokens accepted")
		}
	}
	if len(f.requests) != 0 {
		t.Fatal("invalid input reached server")
	}
}
func TestAmbiguousWriteNeverDeletesUnprovenTarget(t *testing.T) {
	for _, body := range []string{`{}`, `{"data":{"version":2,"destroyed":false,"deletion_time":""}}`, `{"data":{"version":1,"destroyed":null,"deletion_time":""}}`, `{"data":{"version":1,"destroyed":false,"deletion_time":null}}`, `{"data":{"version":1,"destroyed":false}}`, `{"data":{"version":1,"version":2,"destroyed":false,"deletion_time":""}}`, `{"data":{"version":1,"destroyed":false,"deletion_time":""}} {}`, `{"errors":["private token URL response"],"data":{"version":1,"destroyed":false,"deletion_time":""}}`} {
		t.Run(body, func(t *testing.T) {
			f := &fixture{mutate: func(stage string, w http.ResponseWriter, _ *http.Request) bool {
				if stage == "write" {
					_, _ = io.WriteString(w, body)
					return true
				}
				return false
			}}
			c := newFixture(t, f)
			r, err := probe(c)
			if err == nil || r.ProbeID == "" || r.Write.Succeeded || r.Cleanup.State != "unknown" || strings.Join(f.requests, ",") != "write" {
				t.Fatal("ambiguous target deleted or accepted")
			}
			if strings.Contains(err.Error(), "private") {
				t.Fatal("response leaked")
			}
		})
	}
}
func TestReadFailurePreservesWriteWithoutDestroy(t *testing.T) {
	for _, body := range []string{`{}`, `{"data":{"data":{"credential":"wrong"},"metadata":{"version":1,"destroyed":false,"deletion_time":""}}}`, `{"data":{"data":{"credential":"wrong"},"metadata":{"version":2,"destroyed":false,"deletion_time":""}}}`, `{"data":{"data":{"credential":"wrong"},"metadata":{"version":1,"destroyed":true,"deletion_time":""}}}`, `{"data":{"data":{"credential":"wrong"},"metadata":{"version":1,"destroyed":false,"deletion_time":"now"}}}`} {
		t.Run(body, func(t *testing.T) {
			f := &fixture{mutate: func(stage string, w http.ResponseWriter, _ *http.Request) bool {
				if stage == "read" {
					_, _ = io.WriteString(w, body)
					return true
				}
				return false
			}}
			c := newFixture(t, f)
			r, err := probe(c)
			if err == nil || !r.Write.Succeeded || r.Read.Succeeded || r.Cleanup.State != "unknown" || r.Cleanup.Observation.Attempted || strings.Join(f.requests, ",") != "write,read" {
				t.Fatal("read failure destroyed an unconfirmed current target")
			}
		})
	}
}
func TestHTTPFailureNoRetryAndSanitizedDiagnostics(t *testing.T) {
	for _, stage := range []string{"write", "read", "cleanup"} {
		for _, status := range []int{400, 403, 404, 412, 429, 503} {
			t.Run(stage+"/"+http.StatusText(status), func(t *testing.T) {
				f := &fixture{mutate: func(s string, w http.ResponseWriter, _ *http.Request) bool {
					if s == stage {
						w.WriteHeader(status)
						_, _ = io.WriteString(w, "writer-test-only https://private-url data body")
						return true
					}
					return false
				}}
				c := newFixture(t, f)
				r, err := probe(c)
				var failure *Failure
				if !errors.As(err, &failure) || failure.Stage != stage || failure.HTTPStatus != status || strings.Contains(err.Error(), "test-only") || strings.Contains(err.Error(), "private") {
					t.Fatal("unsafe/missing failure")
				}
				wantRequests := map[string]int{"write": 1, "read": 2, "cleanup": 3}[stage]
				if len(f.requests) != wantRequests {
					t.Fatal("HTTP retry or missing cleanup")
				}
				if stage == "cleanup" && (!r.Read.Succeeded || r.Cleanup.State == "acknowledged") {
					t.Fatal("cleanup failure hid successful read")
				}
			})
		}
	}
}
func TestCleanupAcknowledgementAndUnknown(t *testing.T) {
	for _, body := range []string{`{"warnings":["bounded warning"]}`, `{"request_id":"test-request","lease_id":"","lease_duration":0,"renewable":false,"data":null,"auth":null,"wrap_info":null,"mount_type":"kv","warnings":["bounded warning"],"errors":[]}`} {
		f := &fixture{mutate: func(s string, w http.ResponseWriter, _ *http.Request) bool {
			if s == "cleanup" {
				_, _ = io.WriteString(w, body)
				return true
			}
			return false
		}}
		c := newFixture(t, f)
		r, err := probe(c)
		if err != nil || r.Cleanup.State != "acknowledged" {
			t.Fatal("valid200 cleanup rejected")
		}
	}
	f := &fixture{mutate: func(s string, w http.ResponseWriter, _ *http.Request) bool {
		if s == "cleanup" {
			_, _ = io.WriteString(w, `{"errors":["failed"]}`)
			return true
		}
		return false
	}}
	c := newFixture(t, f)
	r, err := probe(c)
	if err == nil || r.Cleanup.State != "unknown" || !r.Read.Succeeded {
		t.Fatal("ambiguouscleanup accepted")
	}
}
func TestResponseBoundsMIMEAndJSON(t *testing.T) {
	bodies := []string{strings.Repeat(" ", maxResponseBytes+1), `{"data":` + strings.Repeat("[", 17) + `0` + strings.Repeat("]", 17) + `}`, string([]byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'})}
	for i, body := range bodies {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			f := &fixture{mutate: func(s string, w http.ResponseWriter, _ *http.Request) bool {
				if s == "write" {
					_, _ = io.WriteString(w, body)
					return true
				}
				return false
			}}
			c := newFixture(t, f)
			r, err := probe(c)
			if err == nil || r.Cleanup.State != "unknown" || len(f.requests) != 1 {
				t.Fatal("bad/overboundJSON accepted")
			}
		})
	}
	f := &fixture{mutate: func(s string, w http.ResponseWriter, _ *http.Request) bool {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "{}")
		return true
	}}
	c := newFixture(t, f)
	if _, err := probe(c); err == nil {
		t.Fatal("HTML accepted")
	}
}
func TestRedirectNeverForwardsTokens(t *testing.T) {
	var reached atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached.Add(1); w.WriteHeader(204) }))
	defer target.Close()
	f := &fixture{mutate: func(_ string, w http.ResponseWriter, r *http.Request) bool {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
		return true
	}}
	c := newFixture(t, f)
	r, err := probe(c)
	if err == nil || reached.Load() != 0 || len(f.requests) != 1 || r.Cleanup.State != "unknown" {
		t.Fatal("redirect forwarded credential/retried")
	}
}
func TestTLSVerificationAndPublicAddressPolicy(t *testing.T) {
	var reached atomic.Int64
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached.Add(1) }))
	defer s.Close()
	d := Descriptor{Endpoint: s.URL, Mount: "secret", Prefix: "providers", DataField: "value"}
	if _, err := New(d, false); err == nil {
		t.Fatal("public mode accepted loopback")
	}
	c, err := New(d, true)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	r, err := probe(c)
	if err == nil || reached.Load() != 0 || r.Write.Succeeded {
		t.Fatal("TLS verification disabled")
	}
}
func TestCancellationNeverStartsOrDetachesCleanup(t *testing.T) {
	f := &fixture{}
	c := newFixture(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, err := c.Probe(ctx, "writer-test-only", "reader-test-only")
	if err == nil || r.Write.Attempted || len(f.requests) != 0 {
		t.Fatal("canceled preflight dispatched")
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	f.mutate = func(stage string, w http.ResponseWriter, r *http.Request) bool {
		if stage == "read" {
			cancel()
			<-r.Context().Done()
			return true
		}
		return false
	}
	r, err = c.Probe(ctx, "writer-test-only", "reader-test-only")
	if err == nil || !r.Write.Succeeded || r.Read.Succeeded || r.Cleanup.State != "unknown" || r.Cleanup.Observation.Attempted || strings.Join(f.requests, ",") != "write,read" {
		t.Fatal("cancellation hidden or detached cleanup")
	}
}
func TestConcurrentProbesHaveUniqueOwnedPaths(t *testing.T) {
	var seen sync.Map
	var count atomic.Int64
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "POST" {
			path := r.URL.Path
			if _, loaded := seen.LoadOrStore(path, true); loaded {
				t.Error("duplicate probe")
			}
			count.Add(1)
			_, _ = io.WriteString(w, `{}`)
			return
		}
		t.Error("unproven target accessed")
	}))
	defer s.Close()
	c, err := New(Descriptor{Endpoint: s.URL, Mount: "secret", Prefix: "providers", DataField: "value"}, true)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			r, err := probe(c)
			if err == nil || r.ProbeID == "" || r.Cleanup.State != "unknown" {
				t.Error("bad concurrent result")
			}
		})
	}
	wg.Wait()
	if count.Load() != 12 {
		t.Fatal("missing/concurrent replay")
	}
}

func TestDeadlineDuringReadReturnsUnknownCleanup(t *testing.T) {
	f := &fixture{mutate: func(stage string, w http.ResponseWriter, r *http.Request) bool {
		if stage == "read" {
			<-r.Context().Done()
			return true
		}
		return false
	}}
	c := newFixture(t, f)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	result, err := c.Probe(ctx, "writer-test-only", "reader-test-only")
	if err == nil || result.Read.Failure == nil || result.Read.Failure.Code != "timed_out" || !result.Write.Succeeded || result.Cleanup.State != "unknown" || result.Cleanup.Observation.Attempted {
		t.Fatal("deadline lost ownership or detached cleanup")
	}
}

func TestReadNativeMetadataAndExactFieldAreIndependentProofs(t *testing.T) {
	for _, variant := range []string{"version", "destroyed", "deleted", "null_version", "missing_metadata", "extra_field", "numeric_value", "wrong_field"} {
		t.Run(variant, func(t *testing.T) {
			f := &fixture{}
			f.mutate = func(stage string, w http.ResponseWriter, r *http.Request) bool {
				if stage != "read" {
					return false
				}
				metadata := map[string]any{"version": 1, "destroyed": false, "deletion_time": ""}
				fields := map[string]any{"credential": f.value}
				data := map[string]any{"data": fields, "metadata": metadata}
				switch variant {
				case "version":
					metadata["version"] = 2
				case "destroyed":
					metadata["destroyed"] = true
				case "deleted":
					metadata["deletion_time"] = "2026-10-07T00:00:00Z"
				case "null_version":
					metadata["version"] = nil
				case "missing_metadata":
					delete(data, "metadata")
				case "extra_field":
					fields["unrequested"] = "extra"
				case "numeric_value":
					fields["credential"] = 1
				case "wrong_field":
					delete(fields, "credential")
					fields["Credential"] = f.value
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
				return true
			}
			c := newFixture(t, f)
			result, err := probe(c)
			if err == nil || !result.Write.Succeeded || result.Read.Succeeded || result.Read.Failure.Code != "verification_failed" || result.Cleanup.State != "unknown" || result.Cleanup.Observation.Attempted || strings.Join(f.requests, ",") != "write,read" {
				t.Fatal("independent exact read proof accepted")
			}
		})
	}
}
func TestCleanupDefiniteRejectionVersusUnknownResponse(t *testing.T) {
	for _, test := range []struct {
		status int
		state  string
	}{{403, "failed"}, {404, "failed"}, {503, "unknown"}} {
		t.Run(http.StatusText(test.status), func(t *testing.T) {
			f := &fixture{mutate: func(stage string, w http.ResponseWriter, _ *http.Request) bool {
				if stage == "cleanup" {
					w.WriteHeader(test.status)
					return true
				}
				return false
			}}
			c := newFixture(t, f)
			result, err := probe(c)
			if err == nil || !result.Read.Succeeded || result.Cleanup.State != test.state || !result.Cleanup.Observation.Attempted || result.Cleanup.Observation.Succeeded {
				t.Fatal("cleanup evidence conflated")
			}
		})
	}
}
func TestPossiblyCommittedWriteConnectionLossDoesNotRetryOrDelete(t *testing.T) {
	var calls atomic.Int64
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = conn.Close()
	}))
	defer s.Close()
	c, err := New(Descriptor{Endpoint: s.URL, Mount: "secret", Prefix: "providers", DataField: "value"}, true)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	result, err := probe(c)
	if err == nil || calls.Load() != 1 || result.ProbeID == "" || result.Cleanup.State != "unknown" || result.Write.Succeeded || result.Cleanup.Observation.Attempted {
		t.Fatal("possibly committed write retried/deleted")
	}
}

func TestCleanupPreservesConcurrentVersionAndMetadata(t *testing.T) {
	versions := map[int]string{}
	metadataRetained := false
	f := &fixture{mutate: func(stage string, w http.ResponseWriter, r *http.Request) bool {
		if stage == "read" {
			// Another writer appended a different version after this probe's CAS=0.
			versions[1] = "owned-first-version"
			versions[2] = "unproven-later-version"
			metadataRetained = true
		}
		if stage == "cleanup" {
			if r.Method != http.MethodPut || !strings.Contains(r.URL.Path, "/destroy/") {
				t.Error("unsafe metadata cleanup")
				w.WriteHeader(http.StatusBadRequest)
				return true
			}
			var body struct {
				Versions []int `json:"versions"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || len(body.Versions) != 1 || body.Versions[0] != 1 {
				t.Error("unproven cleanup version")
				w.WriteHeader(http.StatusBadRequest)
				return true
			}
			for _, version := range body.Versions {
				delete(versions, version)
			}
			w.WriteHeader(http.StatusNoContent)
			return true
		}
		return false
	}}
	result, err := probe(newFixture(t, f))
	f.mu.Lock()
	defer f.mu.Unlock()
	if err != nil || !result.Read.Succeeded || result.Cleanup.State != "acknowledged" || versions[1] != "" || versions[2] != "unproven-later-version" || !metadataRetained {
		t.Fatal("cleanup affected unrelated version or lost evidence")
	}
}

func TestCleanupRejectsUnexpectedSuccessPayload(t *testing.T) {
	for i, body := range []string{
		`{}`, `{"data":{"version":99}}`, `{"warnings":[]}`, `{"warnings":null}`, `{"warnings":"warning"}`, `{"warnings":[1]}`, `{"warnings":[""]}`,
		`{"warnings":["warning"],"data":{}}`, `{"warnings":["warning"],"auth":{}}`, `{"warnings":["warning"],"wrap_info":{}}`,
		`{"warnings":["warning"],"unknown_semantic_payload":true}`, `{"warnings":["warning"],"errors":["error"]}`, `{"warnings":["warning"],"errors":null}`,
		`{"warnings":["warning"],"request_id":[]}`, `{"warnings":["warning"],"request_id":""}`, `{"warnings":["warning"],"mount_type":"other"}`,
		`{"warnings":["warning"],"lease_id":"new-lease"}`, `{"warnings":["warning"],"lease_duration":1}`, `{"warnings":["warning"],"renewable":true}`, `{"warnings":["warning"],"renewable":null}`,
		`{"warnings":["warning"],"warnings":["duplicate"]}`, `{"warnings":["` + strings.Repeat("x", 1025) + `"]}`,
		`{"warnings":[` + strings.TrimSuffix(strings.Repeat(`"x",`, 33), ",") + `]}`,
		strings.Repeat(" ", maxResponseBytes+1), `{"warnings":["warning"],"data":` + strings.Repeat("[", 17) + `0` + strings.Repeat("]", 17) + `}`,
	} {
		t.Run(fmt.Sprintf("case-%d", i), func(t *testing.T) {
			f := &fixture{mutate: func(stage string, w http.ResponseWriter, _ *http.Request) bool {
				if stage == "cleanup" {
					_, _ = io.WriteString(w, body)
					return true
				}
				return false
			}}
			result, err := probe(newFixture(t, f))
			if err == nil || !result.Write.Succeeded || !result.Read.Succeeded || result.Cleanup.State != "unknown" || result.Cleanup.Observation.Succeeded || result.Cleanup.Observation.Failure == nil || strings.Join(f.requests, ",") != "write,read,cleanup" {
				t.Fatal("unexpected payload acknowledged or lost read/no-retry evidence")
			}
		})
	}
}

func TestConvenienceProbeRetainsReplacedVersionAfterFailedOwnershipRead(t *testing.T) {
	for _, variant := range []string{"replaced", "denied", "malformed", "overlimit", "bad_mime"} {
		t.Run(variant, func(t *testing.T) {
			replacement := "unproven-replacement"
			current := ""
			destroyed := false
			f := &fixture{}
			f.mutate = func(stage string, w http.ResponseWriter, _ *http.Request) bool {
				if stage == "read" {
					current = replacement
					switch variant {
					case "replaced":
						_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"data": map[string]string{"credential": current}, "metadata": map[string]any{"version": 1, "destroyed": false, "deletion_time": ""}}})
					case "denied":
						w.WriteHeader(http.StatusForbidden)
					case "malformed":
						_, _ = io.WriteString(w, "{")
					case "overlimit":
						_, _ = io.WriteString(w, strings.Repeat(" ", maxResponseBytes+1))
					case "bad_mime":
						w.Header().Set("Content-Type", "text/html")
						_, _ = io.WriteString(w, "{}")
					}
					return true
				}
				if stage == "cleanup" {
					destroyed = true
					current = ""
					w.WriteHeader(http.StatusNoContent)
					return true
				}
				return false
			}
			c := newFixture(t, f)
			result, err := probe(c)
			f.mu.Lock()
			defer f.mu.Unlock()
			if err == nil || result.ProbeID == "" || result.Version != 1 || !result.Write.Succeeded || !result.Read.Attempted || result.Read.Succeeded || result.Read.Failure == nil || result.Cleanup.State != "unknown" || result.Cleanup.Observation.Attempted || result.Cleanup.Observation.Succeeded || destroyed || current != replacement || strings.Join(f.requests, ",") != "write,read" {
				t.Fatal("failed ownership read erased a replacement or invented cleanup", result, err, f.requests)
			}
		})
	}
}
