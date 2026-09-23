package service

import (
	"context"
	"errors"
	"net/netip"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/limits"
)

func limitNumber(value int64) *int64 { return &value }
func TestRotationLimitAccountIntegrity(t *testing.T) {
	parent := "key_old"
	second := "key_new"
	roots, err := personalLimitRoots([]entity.APIKey{{ID: parent, UserID: "usr_one"}, {ID: second, UserID: "usr_one", ReplacesKeyID: &parent}, {ID: "key_third", UserID: "usr_one", ReplacesKeyID: &second}})
	if err != nil || roots["key_third"] != parent || roots[second] != parent {
		t.Fatal("rotation resets account", err)
	}
	for _, nodes := range []map[string]rotationNode{
		{"key_new": {owner: "one", parent: "missing"}},
		{"key_one": {owner: "one", parent: "key_two"}, "key_two": {owner: "one", parent: "key_one"}},
		{"key_one": {owner: "one", parent: "key_two"}, "key_two": {owner: "other"}},
	} {
		if _, err := rotationRoots(nodes); err == nil {
			t.Fatal("corrupt ancestry accepted")
		}
	}
}
func TestRuntimeAdmissionRechecksReducedPolicyAndIP(t *testing.T) {
	svc, data, _ := runtimeFixture(t, "http://127.0.0.1/v1")
	queue, err := eventqueue.Open(filepath.Join(t.TempDir(), "calls.db"), 20, callQueuePayloadLimit)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = queue.Close() }()
	svc.recorder = &callRecorder{queue: queue}
	result := func() *GatewayResult {
		return &GatewayResult{UserID: "usr_one", KeyID: "key_one", ModelID: "mdl_one", ModelName: "public-model"}
	}
	// Simulate a request authorized before a policy write, then block its final
	// admission behind the writer. It must read the newly published restriction.
	svc.limitMu.Lock()
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		done <- svc.admitLimitedGatewayCall(context.Background(), "req_after_reduction", result())
	}()
	<-started
	data.Limits = []entity.ResourceLimit{{ScopeKind: "user", ScopeID: "usr_one", RPM: limitNumber(0), IPMode: "none", IPRangesJSON: "[]"}}
	if err := compileRuntimeLimits(data); err != nil {
		t.Fatal(err)
	}
	svc.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	svc.limitMu.Unlock()
	if err := <-done; err == nil {
		t.Fatal("stale policy admitted after reduction")
	}
	if depth, _ := queue.Depth(); depth != 0 {
		t.Fatal("rejected admission reserved journal")
	}
	data.Limits = []entity.ResourceLimit{{ScopeKind: "user", ScopeID: "usr_one", IPMode: "allowlist", IPRangesJSON: `["192.0.2.0/24"]`}, {ScopeKind: "key", ScopeID: "key_one", IPMode: "denylist", IPRangesJSON: `["192.0.2.7/32"]`}}
	if err := compileRuntimeLimits(data); err != nil {
		t.Fatal(err)
	}
	svc.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	for _, ip := range []string{"198.51.100.1", "192.0.2.7"} {
		err := svc.admitLimitedGatewayCall(WithGatewayClientIP(context.Background(), netip.MustParseAddr(ip)), "req_blocked_ip", result())
		var gateway *GatewayError
		if !errors.As(err, &gateway) || gateway.Status != 403 {
			t.Fatal("IP intersection bypass", err)
		}
	}
	if err := svc.admitLimitedGatewayCall(WithGatewayClientIP(context.Background(), netip.MustParseAddr("192.0.2.8")), "req_allowed_ip", result()); err != nil {
		t.Fatal(err)
	}
	svc.denyLimitScope("user", "usr_one")
	if err := svc.admitLimitedGatewayCall(WithGatewayClientIP(context.Background(), netip.MustParseAddr("192.0.2.8")), "req_denied_policy", result()); !errors.Is(err, runtimeUnavailable) {
		t.Fatal("unpublished policy reduction did not deny", err)
	}
}
func TestRuntimeConcurrentAggregateAndRotatedKey(t *testing.T) {
	svc, data, _ := runtimeFixture(t, "http://127.0.0.1/v1")
	old := "key_one"
	data.Keys = append(data.Keys, entity.APIKey{ID: "key_rotated", UserID: "usr_one", ReplacesKeyID: &old, Status: entity.KeyActive})
	data.Limits = []entity.ResourceLimit{{ScopeKind: "key", ScopeID: old, Concurrency: limitNumber(1), IPMode: "none", IPRangesJSON: "[]"}}
	if err := compileRuntimeLimits(data); err != nil {
		t.Fatal(err)
	}
	svc.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	queue, err := eventqueue.Open(filepath.Join(t.TempDir(), "calls.db"), 10, callQueuePayloadLimit)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = queue.Close() }()
	svc.recorder = &callRecorder{queue: queue}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, key := range []string{old, "key_rotated"} {
		wg.Go(func() {
			results <- svc.admitLimitedGatewayCall(context.Background(), "req_"+key, &GatewayResult{UserID: "usr_one", KeyID: key, ModelID: "mdl_one", ModelName: "public-model"})
		})
	}
	wg.Wait()
	close(results)
	accepted := 0
	for err := range results {
		if err == nil {
			accepted++
		} else {
			var gateway *GatewayError
			if !errors.As(err, &gateway) || gateway.Code != "concurrency_limit_exceeded" {
				t.Fatal(err)
			}
		}
	}
	if accepted != 1 {
		t.Fatal("rotation broadened concurrent access", accepted)
	}
}
func TestUnsupportedPolicyRowsFailPublication(t *testing.T) {
	_, data, _ := runtimeFixture(t, "http://127.0.0.1/v1")
	data.Limits = []entity.ResourceLimit{{ScopeKind: "key", ScopeID: "key_one", RPM: limitNumber(-1)}}
	if !errors.Is(compileRuntimeLimits(data), limits.ErrInvalid) {
		t.Fatal("invalid snapshot policy accepted")
	}
}
