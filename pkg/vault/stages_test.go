package vault

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func fixtureStages(f *fixture) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.requests, ",")
}
func fixtureCount(f *fixture) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}
func prepare(t *testing.T, c *Client) *PreparedProbe {
	t.Helper()
	p, err := c.Prepare()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}
func stageWrite(t *testing.T, c *Client, p *PreparedProbe) WriteResult {
	t.Helper()
	r, err := c.Write(t.Context(), "writer-test-only", p)
	if err != nil || !r.Write.Succeeded || r.Version != 1 {
		t.Fatalf("write observation=%+v err=%v", r, err)
	}
	return r
}
func TestPlanDurableRecoveryAfterMarkerClose(t *testing.T) {
	f := &fixture{}
	c := newFixture(t, f)
	p := prepare(t, c)
	plan := p.Plan()
	marker := append([]byte(nil), p.state.marker...)
	defer clear(marker)
	hash := sha256.Sum256(marker)
	if len(marker) != 64 || plan.ExpectedSHA256 != hex.EncodeToString(hash[:]) || !c.validPlan(plan) || fixtureCount(f) != 0 {
		t.Fatal("invalid preparation or HTTP side effect")
	}
	serialized, err := json.Marshal(plan)
	if err != nil || strings.Contains(string(serialized), string(marker)) {
		t.Fatal("plan contains transient marker")
	}
	var recovered ProbePlan
	if json.Unmarshal(serialized, &recovered) != nil || recovered != plan {
		t.Fatal("plan recovery changed identity")
	}
	altered := p.Plan()
	altered.ProbeID = "bad"
	if p.Plan() != plan {
		t.Fatal("plan copy altered private state")
	}
	for _, object := range []any{p, *p} {
		encoded, err := json.Marshal(object)
		if err != nil || string(encoded) != "{}" {
			t.Fatal("prepared marker serialized")
		}
		for _, format := range []string{"%v", "%+v", "%#v"} {
			if fmt.Sprintf(format, object) != "<vault.PreparedProbe>" {
				t.Fatal("prepared diagnostic not redacted")
			}
		}
	}
	w := stageWrite(t, c, p)
	p.Close()
	if p.state.marker != nil || p.Plan() != plan {
		t.Fatal("close lost plan or retained marker")
	}
	fresh, err := New(c.descriptor, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(fresh.Close)
	r, err := fresh.ReadAndCleanup(t.Context(), "reader-test-only", "writer-test-only", recovered)
	if err != nil || !r.Read.Succeeded || r.Version != 1 || r.Cleanup.State != "acknowledged" || r.ProbeID != plan.ProbeID || !w.Write.Succeeded || fixtureStages(f) != "write,read,cleanup" {
		t.Fatal("durable read recovery failed")
	}
	encoded, _ := json.Marshal(r)
	if strings.Contains(string(encoded), string(marker)) || strings.Contains(string(encoded), "writer-test-only") || strings.Contains(string(encoded), "reader-test-only") {
		t.Fatal("result exposes transient data")
	}
}
func TestPreparedWriteOnceEvenThroughCopies(t *testing.T) {
	f := &fixture{}
	c := newFixture(t, f)
	p := prepare(t, c)
	alias := *p
	var wins, blocked atomic.Int32
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			target := p
			if i%2 == 0 {
				target = &alias
			}
			r, err := c.Write(t.Context(), "writer-test-only", target)
			if err == nil && r.Write.Succeeded {
				wins.Add(1)
			} else if r.Write.Failure != nil && r.Write.Failure.Code == "already_attempted" && !r.Write.Attempted {
				blocked.Add(1)
			} else {
				t.Error("unexpected once-only outcome")
			}
		})
	}
	wg.Wait()
	if wins.Load() != 1 || blocked.Load() != 19 || fixtureStages(f) != "write" {
		t.Fatal("prepared attempt replayed")
	}
}
func TestClosedPreparedCannotWriteButPlanSurvives(t *testing.T) {
	f := &fixture{}
	c := newFixture(t, f)
	p := prepare(t, c)
	plan := p.Plan()
	owned := p.state.marker
	p.Close()
	for _, b := range owned {
		if b != 0 {
			t.Fatal("owned marker buffer not cleared")
		}
	}
	r, err := c.Write(t.Context(), "writer-test-only", p)
	if err == nil || r.Write.Failure.Code != "prepared_closed" || r.Write.Attempted || fixtureCount(f) != 0 || p.Plan() != plan {
		t.Fatal("closed preparation dispatched")
	}
}

// Simulate a committed write whose HTTP acknowledgement is lost or malformed.
func TestAmbiguousStageWriteNeedsExplicitRead(t *testing.T) {
	for _, variant := range []string{"lost_response", "version2", "missing_metadata"} {
		t.Run(variant, func(t *testing.T) {
			f := &fixture{}
			// Capture through a closure only after the fixture exists.
			f.mutate = func(stage string, w http.ResponseWriter, r *http.Request) bool {
				if stage != "write" {
					return false
				}
				var body struct {
					Data map[string]string `json:"data"`
				}
				if json.NewDecoder(r.Body).Decode(&body) != nil {
					t.Error("fixture body")
					w.WriteHeader(http.StatusBadRequest)
					return true
				}
				f.path = r.URL.Path
				f.value = body.Data["credential"]
				switch variant {
				case "lost_response":
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return true
					}
					_ = conn.Close()
				case "version2":
					_, _ = io.WriteString(w, `{"data":{"version":2,"destroyed":false,"deletion_time":""}}`)
				default:
					_, _ = io.WriteString(w, `{}`)
				}
				return true
			}
			c := newFixture(t, f)
			p := prepare(t, c)
			plan := p.Plan()
			w, err := c.Write(t.Context(), "writer-test-only", p)
			if err == nil || w.Write.Succeeded || !w.Write.Attempted || w.Version != 0 || w.ProbeID != plan.ProbeID || fixtureStages(f) != "write" {
				t.Fatal("ambiguous write falsely passed or cleaned")
			}
			_, err = c.Write(t.Context(), "writer-test-only", p)
			if err == nil || fixtureCount(f) != 1 {
				t.Fatal("ambiguous write replayed")
			}
			p.Close()
			r, err := c.ReadAndCleanup(t.Context(), "reader-test-only", "writer-test-only", plan)
			if err != nil || !r.Read.Succeeded || r.Cleanup.State != "acknowledged" || w.Write.Succeeded || fixtureStages(f) != "write,read,cleanup" {
				t.Fatal("explicit recovery rewrote historical write or failed")
			}
		})
	}
}
func TestStagePlanDescriptorAndSyntaxFence(t *testing.T) {
	f := &fixture{}
	c := newFixture(t, f)
	p := prepare(t, c)
	plan := p.Plan()
	for _, change := range []func(*ProbePlan){func(p *ProbePlan) { p.ProbeID = "../wrong" }, func(p *ProbePlan) { p.ProbeID = "A" + p.ProbeID[1:] }, func(p *ProbePlan) { p.ExpectedSHA256 = "bad" }, func(p *ProbePlan) { p.DescriptorSHA256 = strings.Repeat("0", 64) }} {
		bad := plan
		change(&bad)
		r, err := c.ReadAndCleanup(t.Context(), "reader-test-only", "writer-test-only", bad)
		if err == nil || r.ProbeID != "" || r.Read.Attempted || r.Cleanup.Observation.Attempted {
			t.Fatal("invalid plan dispatched/echoed")
		}
	}
	if fixtureCount(f) != 0 {
		t.Fatal("plan validation reached server")
	}
	for _, field := range []string{"endpoint", "namespace", "mount", "prefix", "field"} {
		other := *c
		descriptor := c.descriptor
		endpoint := *c.endpoint
		switch field {
		case "endpoint":
			endpoint.Path += "/other"
		case "namespace":
			descriptor.Namespace = "other"
		case "mount":
			descriptor.Mount = "other"
		case "prefix":
			descriptor.Prefix = "other"
		case "field":
			descriptor.DataField = "other"
		}
		other.descriptor = descriptor
		other.endpoint = &endpoint
		r, err := other.Write(t.Context(), "writer-test-only", p)
		if err == nil || r.Write.Attempted {
			t.Fatal("foreign descriptor write allowed")
		}
		r2, err := other.CleanupOwned(t.Context(), "reader-test-only", "writer-test-only", plan)
		if err == nil || r2.Ownership.Attempted {
			t.Fatal("foreign cleanup descriptor allowed")
		}
	}
	if fixtureCount(f) != 0 {
		t.Fatal("descriptor validation reached server")
	}
	stageWrite(t, c, p) // Invalid descriptor attempts do not consume the original.
}
func TestStageOwnershipMismatchNeverDestroys(t *testing.T) {
	for _, variant := range []string{"content", "version", "destroyed", "deleted", "extra_field", "missing_field", "null", "malformed", "404", "expected_digest"} {
		t.Run(variant, func(t *testing.T) {
			f := &fixture{}
			f.mutate = func(stage string, w http.ResponseWriter, r *http.Request) bool {
				if stage != "read" {
					return false
				}
				data := map[string]any{"credential": f.value}
				meta := map[string]any{"version": 1, "destroyed": false, "deletion_time": ""}
				switch variant {
				case "content":
					data["credential"] = strings.Repeat("0", 64)
				case "version":
					meta["version"] = 2
				case "destroyed":
					meta["destroyed"] = true
				case "deleted":
					meta["deletion_time"] = "recorded"
				case "extra_field":
					data["extra"] = "unproven"
				case "missing_field":
					delete(data, "credential")
				case "null":
					data["credential"] = nil
				case "malformed":
					_, _ = io.WriteString(w, `{"data":`)
					return true
				case "404":
					w.WriteHeader(http.StatusNotFound)
					return true
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"data": data, "metadata": meta}})
				return true
			}
			c := newFixture(t, f)
			p := prepare(t, c)
			stageWrite(t, c, p)
			plan := p.Plan()
			if variant == "expected_digest" {
				plan.ExpectedSHA256 = strings.Repeat("0", 64)
			}
			r, err := c.CleanupOwned(t.Context(), "reader-test-only", "writer-test-only", plan)
			if err == nil || r.Version != 0 || r.Ownership.Succeeded || r.Cleanup.State != "unknown" || r.Cleanup.Observation.Attempted || fixtureStages(f) != "write,read" {
				t.Fatal("unproven ownership destroyed")
			}
		})
	}
}
func TestStageReadSuccessCleanupUnknownOrFailed(t *testing.T) {
	for _, variant := range []string{"unexpected200", "http403", "http503"} {
		t.Run(variant, func(t *testing.T) {
			f := &fixture{mutate: func(stage string, w http.ResponseWriter, _ *http.Request) bool {
				if stage != "cleanup" {
					return false
				}
				switch variant {
				case "unexpected200":
					_, _ = io.WriteString(w, `{"data":{"version":99}}`)
				case "http403":
					w.WriteHeader(http.StatusForbidden)
				case "http503":
					w.WriteHeader(http.StatusServiceUnavailable)
				}
				return true
			}}
			c := newFixture(t, f)
			p := prepare(t, c)
			stageWrite(t, c, p)
			r, err := c.ReadAndCleanup(t.Context(), "reader-test-only", "writer-test-only", p.Plan())
			want := "unknown"
			if variant == "http403" {
				want = "failed"
			}
			if err == nil || !r.Read.Succeeded || r.Version != 1 || r.Cleanup.State != want || r.Cleanup.Observation.Succeeded || fixtureCount(f) != 3 {
				t.Fatal("cleanup lost read fact or retried")
			}
		})
	}
}
func TestStageCancellationNoDetachedCleanup(t *testing.T) {
	f := &fixture{}
	c := newFixture(t, f)
	p := prepare(t, c)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r, err := c.Write(ctx, "writer-test-only", p)
	if err == nil || r.Write.Attempted || fixtureCount(f) != 0 {
		t.Fatal("canceled write dispatched")
	}
	// A consumed local attempt cannot become an implicit retry after cancellation.
	_, err = c.Write(t.Context(), "writer-test-only", p)
	if err == nil || fixtureCount(f) != 0 {
		t.Fatal("canceled prepared attempt replayed")
	}
	p2 := prepare(t, c)
	stageWrite(t, c, p2)
	ctx, cancel = context.WithCancel(t.Context())
	f.mutate = func(stage string, w http.ResponseWriter, _ *http.Request) bool {
		if stage == "read" {
			cancel()
		}
		return false
	}
	r2, err := c.ReadAndCleanup(ctx, "reader-test-only", "writer-test-only", p2.Plan())
	if err == nil || r2.Cleanup.State != "unknown" || r2.Cleanup.Observation.Attempted || fixtureStages(f) != "write,read" {
		t.Fatal("cancellation detached cleanup")
	}
}
func TestCleanupOwnershipIsNotReadTestReceipt(t *testing.T) {
	f := &fixture{}
	c := newFixture(t, f)
	p := prepare(t, c)
	stageWrite(t, c, p)
	r, err := c.CleanupOwned(t.Context(), "reader-test-only", "writer-test-only", p.Plan())
	encoded, _ := json.Marshal(r)
	if err != nil || !r.Ownership.Succeeded || r.Cleanup.State != "acknowledged" || strings.Contains(string(encoded), `"Read"`) {
		t.Fatal("cleanup fabricated explicit read test")
	}
}

func TestStageCleanupPreservesLaterVersion(t *testing.T) {
	versions := map[int]string{}
	f := &fixture{}
	f.mutate = func(stage string, w http.ResponseWriter, r *http.Request) bool {
		if stage == "read" {
			versions[1] = "owned"
			versions[2] = "later"
		}
		if stage != "cleanup" {
			return false
		}
		var body struct {
			Versions []int `json:"versions"`
		}
		if r.Method != http.MethodPut || !strings.Contains(r.URL.Path, "/destroy/") || json.NewDecoder(r.Body).Decode(&body) != nil || len(body.Versions) != 1 || body.Versions[0] != 1 {
			t.Error("unproven cleanup")
			w.WriteHeader(http.StatusBadRequest)
			return true
		}
		delete(versions, 1)
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	c := newFixture(t, f)
	p := prepare(t, c)
	stageWrite(t, c, p)
	r, err := c.ReadAndCleanup(t.Context(), "reader-test-only", "writer-test-only", p.Plan())
	f.mu.Lock()
	defer f.mu.Unlock()
	if err != nil || !r.Read.Succeeded || r.Cleanup.State != "acknowledged" || versions[1] != "" || versions[2] != "later" {
		t.Fatal("stage cleanup erased later version")
	}
}
func TestStagesRejectInvalidOrSharedTokensWithoutDispatch(t *testing.T) {
	f := &fixture{}
	c := newFixture(t, f)
	p := prepare(t, c)
	w, err := c.Write(t.Context(), "invalid token", p)
	if err == nil || w.Write.Attempted {
		t.Fatal("invalid writer dispatched")
	}
	for _, tokens := range [][2]string{{"same", "same"}, {"", "valid"}, {"valid", "invalid token"}} {
		r, err := c.ReadAndCleanup(t.Context(), tokens[0], tokens[1], p.Plan())
		if err == nil || r.Read.Attempted || r.Cleanup.Observation.Attempted {
			t.Fatal("invalid identity pair dispatched")
		}
	}
	if fixtureCount(f) != 0 {
		t.Fatal("token validation reached server")
	}
}

func TestLostCleanupReceiptNeverBecomesInventedAcknowledgement(t *testing.T) {
	destroyed := false
	f := &fixture{}
	f.mutate = func(stage string, w http.ResponseWriter, r *http.Request) bool {
		if stage == "read" && destroyed {
			w.WriteHeader(http.StatusNotFound)
			return true
		}
		if stage == "cleanup" {
			destroyed = true // Remote effect happened, but its response/receipt was lost.
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return true
			}
			_ = conn.Close()
			return true
		}
		return false
	}
	c := newFixture(t, f)
	p := prepare(t, c)
	stageWrite(t, c, p)
	plan := p.Plan()
	p.Close()
	r, err := c.ReadAndCleanup(t.Context(), "reader-test-only", "writer-test-only", plan)
	if err == nil || !r.Read.Succeeded || r.Cleanup.State != "unknown" || fixtureStages(f) != "write,read,cleanup" {
		t.Fatal("lost cleanup response claimed pass or retried")
	}
	later, err := c.CleanupOwned(t.Context(), "reader-test-only", "writer-test-only", plan)
	if err == nil || later.Ownership.Succeeded || later.Cleanup.State != "unknown" || later.Cleanup.Observation.Attempted || fixtureStages(f) != "write,read,cleanup,read" || !r.Read.Succeeded {
		t.Fatal("missing ownership manufactured acknowledgement or erased original read")
	}
}
