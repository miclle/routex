package service

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestRuntimeRefreshIntervalBootstrapDefaultAndBounds(t *testing.T) {
	s, err := New(context.Background(), &gorm.DB{})
	if err != nil || s.runtimeRefreshInterval != time.Second {
		t.Fatal("default publisher cadence changed", err)
	}
	for _, interval := range []time.Duration{time.Millisecond, time.Second, time.Hour, 24 * time.Hour} {
		configured, err := New(context.Background(), &gorm.DB{}, WithRuntimeRefreshInterval(interval))
		if err != nil || configured.runtimeRefreshInterval != interval {
			t.Fatal("safe bootstrap interval rejected", interval, err)
		}
	}
	for _, interval := range []time.Duration{0, -time.Second, time.Nanosecond, time.Millisecond - time.Nanosecond, 24*time.Hour + time.Nanosecond} {
		if configured, err := New(context.Background(), &gorm.DB{}, WithRuntimeRefreshInterval(interval)); configured != nil || err == nil {
			t.Fatal("unsafe bootstrap interval admitted", interval, err)
		}
	}
}

func TestRuntimeRefreshConfiguredLivePublisherAllowsManualRefreshAndJoinedStop(t *testing.T) {
	base, _, _, _ := providerMetadataSQLService(t)
	s, err := New(context.Background(), base.db, WithRuntimeRefreshInterval(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	// This SQL fixture has no Vault credentials. A bootstrap view prevents this
	// scheduler test from exercising the separate secret-store initialization.
	s.rootPolicy.Store(&secretPolicyView{})
	if err := s.StartRuntime(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.StopRuntime)
	select {
	case <-s.runtime.done:
		t.Fatal("configured publisher stopped instead of remaining live")
	default:
	}
	if err := s.RefreshRuntime(context.Background()); err != nil {
		t.Fatal("live publisher rejected explicit refresh", err)
	}
	s.StopRuntime()
	select {
	case <-s.runtime.done:
	default:
		t.Fatal("StopRuntime did not join its configured scheduler")
	}
	if err := s.RefreshRuntime(context.Background()); err != runtimeUnavailable {
		t.Fatal("configured scheduler revived after shutdown", err)
	}
}
