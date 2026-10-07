package vault

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

type credentialFixture struct {
	mu     sync.Mutex
	stages []string
	path   string
	data   map[string]string
	mutate func(string, http.ResponseWriter, *http.Request) bool
}

func (f *credentialFixture) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	stage := "read"
	token := "reader-test"
	if r.Method == http.MethodPost {
		stage = "write"
		token = "writer-test"
	}
	if r.Method == http.MethodPut {
		stage = "cleanup"
		token = "cleanup-test"
	}
	f.stages = append(f.stages, stage)
	if r.Header.Get("X-Vault-Token") != token || r.Header.Get("X-Vault-Namespace") != "test/ns" || r.Header.Get("X-Vault-Request") != "true" || !r.Close {
		w.WriteHeader(403)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if f.mutate != nil && f.mutate(stage, w, r) {
		return
	}
	switch stage {
	case "write":
		var body struct {
			Options struct {
				CAS *int `json:"cas"`
			} `json:"options"`
			Data map[string]string `json:"data"`
		}
		if r.Method != http.MethodPost || !strings.HasPrefix(r.URL.Path, "/root/v1/kv/data/providers/routex-credential-") || !lowerHex(strings.TrimPrefix(r.URL.Path, "/root/v1/kv/data/providers/routex-credential-"), 32) || r.URL.RawQuery != "" || r.Header.Get("Content-Type") != "application/json" || json.NewDecoder(r.Body).Decode(&body) != nil || body.Options.CAS == nil || *body.Options.CAS != 0 || len(body.Data) != 2 || !lowerHex(body.Data[credentialMarkerField], 64) {
			w.WriteHeader(400)
			return
		}
		f.path = r.URL.Path
		f.data = body.Data
		_, _ = io.WriteString(w, `{"data":{"version":1,"destroyed":false,"deletion_time":""}}`)
	case "read":
		if r.URL.Path != f.path || r.URL.RawQuery != "version=1" {
			w.WriteHeader(404)
			return
		}
		f.read(w, f.data, map[string]any{"version": 1, "destroyed": false, "deletion_time": ""})
	case "cleanup":
		var body struct {
			Versions []int `json:"versions"`
		}
		if r.URL.Path != strings.Replace(f.path, "/data/", "/destroy/", 1) || r.URL.RawQuery != "" || json.NewDecoder(r.Body).Decode(&body) != nil || !reflect.DeepEqual(body.Versions, []int{1}) {
			w.WriteHeader(400)
			return
		}
		w.WriteHeader(204)
	}
}
func (f *credentialFixture) read(w http.ResponseWriter, data any, metadata any) {
	_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"data": data, "metadata": metadata}})
}
func (f *credentialFixture) count() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.stages) }
func (f *credentialFixture) stagesCopy() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.stages...)
}
func credentialClient(t *testing.T, f *credentialFixture) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(server.Close)
	c, err := New(Descriptor{Endpoint: server.URL + "/root", Namespace: "test/ns", Mount: "kv", Prefix: "providers", DataField: "credential"}, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}
func preparedCredential(t *testing.T, c *Client) *PreparedCredential {
	t.Helper()
	p, err := c.PrepareCredential()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}
func writeCredential(t *testing.T, c *Client, p *PreparedCredential) CredentialWriteResult {
	t.Helper()
	r, err := c.WriteCredential(t.Context(), "writer-test", p, []byte("secret-test-only"))
	if err != nil || !r.Write.Succeeded || r.Version != 1 {
		t.Fatalf("unexpected safe observation: %+v %v", r, err)
	}
	return r
}

func TestCredentialKVExactRequestsDurableRecoveryAndTransientValue(t *testing.T) {
	f := &credentialFixture{}
	c := credentialClient(t, f)
	p := preparedCredential(t, c)
	plan := p.Plan()
	marker := append([]byte(nil), p.state.marker...)
	defer clear(marker)
	digest := sha256.Sum256(marker)
	if f.count() != 0 || plan.ExpectedMarkerSHA256 != hex.EncodeToString(digest[:]) || !c.validCredentialPlan(plan) {
		t.Fatal("not a separate random marker plan")
	}
	raw, _ := json.Marshal(plan)
	if strings.Contains(string(raw), string(marker)) {
		t.Fatal("marker persisted")
	}
	var recovered CredentialPlan
	if json.Unmarshal(raw, &recovered) != nil || recovered != plan {
		t.Fatal("changed plan")
	}
	value := []byte("  \ufeffcredential\"value  ")
	original := append([]byte(nil), value...)
	defer clear(original)
	w, err := c.WriteCredential(t.Context(), "writer-test", p, value)
	if err != nil || !w.Write.Succeeded || w.Version != 1 || !reflect.DeepEqual(value, original) {
		t.Fatal("write changed value or failed")
	}
	p.Close()
	v, r, err := c.ReadCredential(t.Context(), "reader-test", recovered)
	if err != nil || !r.Read.Succeeded || r.Version != 1 || !reflect.DeepEqual(v.Bytes(), original) {
		t.Fatal("exact resolution failed")
	}
	for _, object := range []any{p, *p, v, *v} {
		for _, format := range []string{"%v", "%+v", "%#v"} {
			formatted := fmt.Sprintf(format, object)
			if strings.Contains(formatted, "credential\"value") || strings.Contains(formatted, string(marker)) {
				t.Fatal("diagnostic exposes material")
			}
		}
		encoded, _ := json.Marshal(object)
		if string(encoded) != "{}" {
			t.Fatal("sensitive object serialized")
		}
	}
	copyValue := v.Bytes()
	copyValue[0] = 'X'
	if reflect.DeepEqual(copyValue, v.Bytes()) {
		t.Fatal("returned bytes alias private owner")
	}
	clear(copyValue)
	alias := *v
	owned := v.state.value
	alias.Close()
	if v.Bytes() != nil {
		t.Fatal("copied owner not cleared")
	}
	for _, b := range owned {
		if b != 0 {
			t.Fatal("owned value uncleared")
		}
	}
	clean, err := c.CleanupCredentialOwned(t.Context(), "reader-test", "cleanup-test", plan)
	if err != nil || !clean.Ownership.Succeeded || clean.Version != 1 || clean.Cleanup.State != "acknowledged" || !reflect.DeepEqual(f.stagesCopy(), []string{"write", "read", "read", "cleanup"}) {
		t.Fatal("cleanup request separation failed")
	}
	encoded, _ := json.Marshal([]any{w, r, clean})
	for _, forbidden := range []string{string(marker), string(original), "writer-test", "reader-test", "cleanup-test", c.endpoint.String(), c.credentialPath(plan)} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatal("result leaks private data")
		}
	}
}
func TestCredentialKVPreparedCopiesConsumeExactlyOneWrite(t *testing.T) {
	f := &credentialFixture{}
	c := credentialClient(t, f)
	p := preparedCredential(t, c)
	alias := *p
	var wins, denials atomic.Int32
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			target := p
			if i%2 == 0 {
				target = &alias
			}
			r, err := c.WriteCredential(t.Context(), "writer-test", target, []byte("secret-test-only"))
			if err == nil && r.Write.Succeeded {
				wins.Add(1)
			} else if r.Write.Failure != nil && r.Write.Failure.Code == "already_attempted" && !r.Write.Attempted {
				denials.Add(1)
			} else {
				t.Error("unexpected write claim")
			}
		})
	}
	wg.Wait()
	if wins.Load() != 1 || denials.Load() != 19 || f.count() != 1 {
		t.Fatal("write replayed")
	}
}
func TestCredentialKVValidationDoesNotConsumeValidPreparation(t *testing.T) {
	f := &credentialFixture{}
	c := credentialClient(t, f)
	p := preparedCredential(t, c)
	for _, value := range [][]byte{nil, []byte("\n"), []byte("\r"), {0xff}, []byte(strings.Repeat("x", 2049))} {
		r, err := c.WriteCredential(t.Context(), "writer-test", p, value)
		if err == nil || r.Write.Attempted {
			t.Fatal("invalid value dispatched")
		}
	}
	r, err := c.WriteCredential(t.Context(), "invalid token", p, []byte("valid"))
	if err == nil || r.Write.Attempted || f.count() != 0 {
		t.Fatal("invalid token dispatched")
	}
	writeCredential(t, c, p)
}
func TestCredentialKVReservedFieldOnlyAffectsCredentialOperations(t *testing.T) {
	f := &credentialFixture{}
	c := credentialClient(t, f)
	descriptor := c.descriptor
	descriptor.DataField = credentialMarkerField
	other, err := New(descriptor, true)
	if err != nil {
		t.Fatal("probe-compatible constructor changed")
	}
	defer other.Close()
	probe, err := other.Prepare()
	if err != nil || !other.validPlan(probe.Plan()) {
		t.Fatal("existing probe rejected")
	}
	probe.Close()
	p, err := other.PrepareCredential()
	if err == nil || p != nil || f.count() != 0 {
		t.Fatal("reserved field collision accepted")
	}
}
func TestCredentialKVPlanScopeAndProbeSeparation(t *testing.T) {
	f := &credentialFixture{}
	c := credentialClient(t, f)
	p := preparedCredential(t, c)
	plan := p.Plan()
	probe := prepare(t, c)
	if plan.DescriptorSHA256 == probe.Plan().DescriptorSHA256 {
		t.Fatal("probe and credential plans aliased")
	}
	for _, mutate := range []func(*CredentialPlan){func(x *CredentialPlan) { x.ReferenceID = "../escape" }, func(x *CredentialPlan) { x.ReferenceID = "A" + x.ReferenceID[1:] }, func(x *CredentialPlan) { x.ExpectedMarkerSHA256 = "bad" }, func(x *CredentialPlan) { x.DescriptorSHA256 = probe.Plan().DescriptorSHA256 }} {
		bad := plan
		mutate(&bad)
		v, r, err := c.ReadCredential(t.Context(), "reader-test", bad)
		if err == nil || v != nil || r.Read.Attempted {
			t.Fatal("invalid plan read")
		}
		clean, err := c.CleanupCredentialOwned(t.Context(), "reader-test", "cleanup-test", bad)
		if err == nil || clean.Ownership.Attempted || clean.Cleanup.Observation.Attempted {
			t.Fatal("invalid cleanup")
		}
	}
	for _, field := range []string{"endpoint", "namespace", "mount", "prefix", "field"} {
		other := *c
		d := c.descriptor
		e := *c.endpoint
		switch field {
		case "endpoint":
			e.Path += "/other"
		case "namespace":
			d.Namespace = "other"
		case "mount":
			d.Mount = "other"
		case "prefix":
			d.Prefix = "other"
		case "field":
			d.DataField = "other"
		}
		other.descriptor = d
		other.endpoint = &e
		r, err := other.WriteCredential(t.Context(), "writer-test", p, []byte("valid"))
		if err == nil || r.Write.Attempted {
			t.Fatal("foreign descriptor write")
		}
		_, read, err := other.ReadCredential(t.Context(), "reader-test", plan)
		if err == nil || read.Read.Attempted {
			t.Fatal("foreign descriptor read")
		}
	}
	if f.count() != 0 {
		t.Fatal("plan validation made HTTP")
	}
	writeCredential(t, c, p)
}
func TestCredentialKVWriteAmbiguityNeverReplaysOrCleans(t *testing.T) {
	for _, variant := range []string{"lost", "version2", "null_version", "malformed", "oversized", "http403", "http503"} {
		t.Run(variant, func(t *testing.T) {
			f := &credentialFixture{}
			f.mutate = func(stage string, w http.ResponseWriter, r *http.Request) bool {
				if stage != "write" {
					return false
				}
				switch variant {
				case "lost":
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
					} else {
						_ = conn.Close()
					}
				case "version2":
					_, _ = io.WriteString(w, `{"data":{"version":2,"destroyed":false,"deletion_time":""}}`)
				case "null_version":
					_, _ = io.WriteString(w, `{"data":{"version":null,"destroyed":false,"deletion_time":""}}`)
				case "malformed":
					_, _ = io.WriteString(w, `{"data":`)
				case "oversized":
					_, _ = io.WriteString(w, strings.Repeat("x", maxResponseBytes+1))
				case "http403":
					w.WriteHeader(403)
					_, _ = io.WriteString(w, "private-value writer-test path")
				case "http503":
					w.WriteHeader(503)
				}
				return true
			}
			c := credentialClient(t, f)
			p := preparedCredential(t, c)
			r, err := c.WriteCredential(t.Context(), "writer-test", p, []byte("secret-test-only"))
			if err == nil || r.Write.Succeeded || !r.Write.Attempted || r.Version != 0 || strings.Contains(err.Error(), "private-value") {
				t.Fatal("ambiguous result accepted/leaked")
			}
			_, err = c.WriteCredential(t.Context(), "writer-test", p, []byte("secret-test-only"))
			if err == nil || !reflect.DeepEqual(f.stagesCopy(), []string{"write"}) {
				t.Fatal("ambiguous effect replay/cleanup")
			}
		})
	}
}
func TestCredentialKVReadAndCleanupRejectEveryUnprovenOwnership(t *testing.T) {
	for _, variant := range []string{"wrong_marker", "wrong_digest", "wrong_version", "destroyed", "deleted", "missing_metadata", "missing_marker", "extra_field", "null_value", "invalid_value", "invalid_marker", "duplicate_field", "oversized", "404", "503"} {
		t.Run(variant, func(t *testing.T) {
			f := &credentialFixture{}
			c := credentialClient(t, f)
			p := preparedCredential(t, c)
			writeCredential(t, c, p)
			f.mutate = func(stage string, w http.ResponseWriter, _ *http.Request) bool {
				if stage != "read" {
					return false
				}
				data := map[string]any{"credential": f.data["credential"], credentialMarkerField: f.data[credentialMarkerField]}
				meta := map[string]any{"version": 1, "destroyed": false, "deletion_time": ""}
				switch variant {
				case "wrong_marker":
					data[credentialMarkerField] = strings.Repeat("0", 64)
				case "wrong_version":
					meta["version"] = 2
				case "destroyed":
					meta["destroyed"] = true
				case "deleted":
					meta["deletion_time"] = "recorded"
				case "missing_metadata":
					meta = nil
				case "missing_marker":
					delete(data, credentialMarkerField)
				case "extra_field":
					data["extra"] = "unknown"
				case "null_value":
					data["credential"] = nil
				case "invalid_value":
					data["credential"] = "bad\n"
				case "invalid_marker":
					data[credentialMarkerField] = "bad"
				case "duplicate_field":
					_, _ = io.WriteString(w, `{"data":{},"data":{}}`)
					return true
				case "oversized":
					_, _ = io.WriteString(w, strings.Repeat("x", maxResponseBytes+1))
					return true
				case "404":
					w.WriteHeader(404)
					return true
				case "503":
					w.WriteHeader(503)
					return true
				}
				f.read(w, data, meta)
				return true
			}
			plan := p.Plan()
			if variant == "wrong_digest" {
				plan.ExpectedMarkerSHA256 = strings.Repeat("0", 64)
			}
			v, r, err := c.ReadCredential(t.Context(), "reader-test", plan)
			if err == nil || v != nil || r.Read.Succeeded || r.Version != 0 {
				t.Fatal("unproven value returned")
			}
			clean, err := c.CleanupCredentialOwned(t.Context(), "reader-test", "cleanup-test", plan)
			if err == nil || clean.Ownership.Succeeded || clean.Cleanup.State != "unknown" || clean.Cleanup.Observation.Attempted || !reflect.DeepEqual(f.stagesCopy(), []string{"write", "read", "read"}) {
				t.Fatal("unproven ownership destroyed")
			}
		})
	}
}
func TestCredentialKVCleanupOutcomesKeepOwnershipSeparate(t *testing.T) {
	for _, variant := range []string{"204", "warnings200", "empty200", "403", "404", "503", "lost"} {
		t.Run(variant, func(t *testing.T) {
			f := &credentialFixture{}
			c := credentialClient(t, f)
			p := preparedCredential(t, c)
			writeCredential(t, c, p)
			f.mutate = func(stage string, w http.ResponseWriter, _ *http.Request) bool {
				if stage != "cleanup" {
					return false
				}
				switch variant {
				case "204":
					w.WriteHeader(204)
				case "warnings200":
					_, _ = io.WriteString(w, `{"warnings":["test-only"]}`)
				case "empty200":
					_, _ = io.WriteString(w, `{}`)
				case "lost":
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
					} else {
						_ = conn.Close()
					}
				case "403":
					w.WriteHeader(403)
				case "404":
					w.WriteHeader(404)
				case "503":
					w.WriteHeader(503)
				}
				return true
			}
			r, err := c.CleanupCredentialOwned(t.Context(), "reader-test", "cleanup-test", p.Plan())
			want := "unknown"
			if variant == "204" || variant == "warnings200" {
				want = "acknowledged"
			}
			if variant == "403" || variant == "404" {
				want = "failed"
			}
			if !r.Ownership.Succeeded || r.Version != 1 || r.Cleanup.State != want || (err == nil) != (want == "acknowledged") || !reflect.DeepEqual(f.stagesCopy(), []string{"write", "read", "cleanup"}) {
				t.Fatal("cleanup certainty/ownership/request count changed")
			}
			encoded, _ := json.Marshal(r)
			if strings.Contains(string(encoded), `"Read"`) {
				t.Fatal("cleanup invented read-test receipt")
			}
		})
	}
}
func TestCredentialKVLiteralTokenSeparationAndClosedPreparation(t *testing.T) {
	f := &credentialFixture{}
	c := credentialClient(t, f)
	p := preparedCredential(t, c)
	for _, tokens := range [][2]string{{"same", "same"}, {"", "cleanup-test"}, {"reader-test", "invalid token"}} {
		r, err := c.CleanupCredentialOwned(t.Context(), tokens[0], tokens[1], p.Plan())
		if err == nil || r.Ownership.Attempted || r.Cleanup.Observation.Attempted {
			t.Fatal("invalid token pair dispatched")
		}
	}
	marker := p.state.marker
	p.Close()
	r, err := c.WriteCredential(t.Context(), "writer-test", p, []byte("valid"))
	if err == nil || r.Write.Attempted || f.count() != 0 {
		t.Fatal("closed prep dispatched")
	}
	for _, b := range marker {
		if b != 0 {
			t.Fatal("marker uncleared")
		}
	}
}
func TestCredentialKVDeadlineAndCancellationNoDetachedEffects(t *testing.T) {
	for _, stage := range []string{"write", "read", "cleanup"} {
		t.Run(stage, func(t *testing.T) {
			f := &credentialFixture{}
			c := credentialClient(t, f)
			p := preparedCredential(t, c)
			if stage != "write" {
				writeCredential(t, c, p)
			}
			release := make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer unblock()
			f.mutate = func(current string, _ http.ResponseWriter, r *http.Request) bool {
				if current != stage {
					return false
				}
				select {
				case <-r.Context().Done():
				case <-release:
				}
				return true
			}
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
			defer cancel()
			var err error
			if stage == "write" {
				r, e := c.WriteCredential(ctx, "writer-test", p, []byte("valid"))
				err = e
				if r.Write.Succeeded {
					t.Fatal("timedout write succeeded")
				}
			} else {
				r, e := c.CleanupCredentialOwned(ctx, "reader-test", "cleanup-test", p.Plan())
				err = e
				if r.Cleanup.State != "unknown" || r.Cleanup.Observation.Succeeded {
					t.Fatal("timeout cleanup succeeded")
				}
			}
			unblock()
			if err == nil || !strings.Contains(err.Error(), "timed_out") {
				t.Fatal("deadline not sanitized")
			}
			want := 1
			if stage == "read" {
				want = 2
			}
			if stage == "cleanup" {
				want = 3
			}
			if f.count() != want {
				t.Fatal("extra effect on deadline")
			}
		})
	}
	f := &credentialFixture{}
	c := credentialClient(t, f)
	p := preparedCredential(t, c)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r, err := c.WriteCredential(ctx, "writer-test", p, []byte("valid"))
	if err == nil || r.Write.Attempted || f.count() != 0 {
		t.Fatal("canceled write sent")
	}
	_, err = c.WriteCredential(t.Context(), "writer-test", p, []byte("valid"))
	if err == nil || f.count() != 0 {
		t.Fatal("canceled prepared write replayed")
	}
}
func TestCredentialKVCleanupLostResponseThenMissingNeverAcknowledges(t *testing.T) {
	f := &credentialFixture{}
	c := credentialClient(t, f)
	p := preparedCredential(t, c)
	writeCredential(t, c, p)
	destroyed := false
	f.mutate = func(stage string, w http.ResponseWriter, _ *http.Request) bool {
		if stage == "read" && destroyed {
			w.WriteHeader(404)
			return true
		}
		if stage != "cleanup" {
			return false
		}
		destroyed = true
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
		} else {
			_ = conn.Close()
		}
		return true
	}
	first, err := c.CleanupCredentialOwned(t.Context(), "reader-test", "cleanup-test", p.Plan())
	if err == nil || first.Cleanup.State != "unknown" || !first.Ownership.Succeeded {
		t.Fatal("lost destroy falsely acknowledged")
	}
	later, err := c.CleanupCredentialOwned(t.Context(), "reader-test", "cleanup-test", p.Plan())
	if err == nil || later.Cleanup.State != "unknown" || later.Cleanup.Observation.Attempted || !reflect.DeepEqual(f.stagesCopy(), []string{"write", "read", "cleanup", "read"}) {
		t.Fatal("missing value invented cleanup success")
	}
}

func TestCredentialKVCommittedWriteResponseLossExplicitResolution(t *testing.T) {
	f := &credentialFixture{}
	f.mutate = func(stage string, w http.ResponseWriter, r *http.Request) bool {
		if stage != "write" {
			return false
		}
		var body struct {
			Data map[string]string `json:"data"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("invalid fixture body")
			w.WriteHeader(400)
			return true
		}
		f.path = r.URL.Path
		f.data = body.Data
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
		} else {
			_ = conn.Close()
		}
		return true
	}
	c := credentialClient(t, f)
	p := preparedCredential(t, c)
	plan := p.Plan()
	original, err := c.WriteCredential(t.Context(), "writer-test", p, []byte("secret-test-only"))
	if err == nil || original.Write.Succeeded || original.Version != 0 || !original.Write.Attempted {
		t.Fatal("lost write became proof")
	}
	_, err = c.WriteCredential(t.Context(), "writer-test", p, []byte("secret-test-only"))
	if err == nil || f.count() != 1 {
		t.Fatal("lost write replayed")
	}
	p.Close()
	v, read, err := c.ReadCredential(t.Context(), "reader-test", plan)
	if err != nil || !read.Read.Succeeded || read.Version != 1 || string(v.Bytes()) != "secret-test-only" {
		t.Fatal("exact recovery failed")
	}
	v.Close()
	if original.Write.Succeeded || original.Version != 0 || !reflect.DeepEqual(f.stagesCopy(), []string{"write", "read"}) {
		t.Fatal("recovery rewrote history/added effect")
	}
}
func TestCredentialKVCleanupPreservesLaterVersionAndMetadata(t *testing.T) {
	f := &credentialFixture{}
	c := credentialClient(t, f)
	p := preparedCredential(t, c)
	writeCredential(t, c, p)
	versions := map[int]string{1: "owned", 2: "later"}
	metadataExists := true
	f.mutate = func(stage string, w http.ResponseWriter, r *http.Request) bool {
		if stage != "cleanup" {
			return false
		}
		var body struct {
			Versions []int `json:"versions"`
		}
		if r.Method != http.MethodPut || !strings.Contains(r.URL.Path, "/destroy/") || json.NewDecoder(r.Body).Decode(&body) != nil || !reflect.DeepEqual(body.Versions, []int{1}) {
			t.Error("incorrect destruction target")
			w.WriteHeader(400)
			return true
		}
		delete(versions, 1)
		w.WriteHeader(204)
		return true
	}
	result, err := c.CleanupCredentialOwned(t.Context(), "reader-test", "cleanup-test", p.Plan())
	f.mu.Lock()
	defer f.mu.Unlock()
	if err != nil || result.Cleanup.State != "acknowledged" || versions[1] != "" || versions[2] != "later" || !metadataExists {
		t.Fatal("later version/metadata altered")
	}
}
func TestCredentialKVMaximumValueRoundTripsWithoutNormalization(t *testing.T) {
	f := &credentialFixture{}
	c := credentialClient(t, f)
	p := preparedCredential(t, c)
	exact := []byte(strings.Repeat("x", maxCredentialBytes))
	write, err := c.WriteCredential(t.Context(), "writer-test", p, exact)
	if err != nil || !write.Write.Succeeded {
		t.Fatal("maximum supported value rejected")
	}
	value, read, err := c.ReadCredential(t.Context(), "reader-test", p.Plan())
	if err != nil || !read.Read.Succeeded || !reflect.DeepEqual(value.Bytes(), exact) {
		t.Fatal("maximum value altered")
	}
	value.Close()
}
