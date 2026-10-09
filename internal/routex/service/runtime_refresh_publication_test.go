package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
)

// This witness also compiles against the predecessor source: the controlled
// read must not make an already-current published Provider proof unavailable.
func TestRuntimeRefreshDatabaseReadKeepsCurrentProviderProofObservable(t *testing.T) {
	s, _, _, fixture := providerMetadataSQLService(t)
	if err := s.RefreshRuntime(context.Background()); err != nil {
		t.Fatal(err)
	}
	row := fixture.base.provider
	auth, routes, status := s.runtime.auth.Load(), s.runtime.routes.Load(), s.runtime.status.Load()
	if auth == nil || routes == nil || !s.providerStatusRuntimeApplied(routes.Digest, s.egressGeneration.Load(), row) {
		t.Fatal("initial current Provider publication was not established")
	}
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	joined := make(chan struct{})
	var admitted, released sync.Once
	t.Cleanup(func() {
		released.Do(func() { close(release) })
		select {
		case <-joined:
		case <-time.After(2 * runtimeRefreshTimeout):
			t.Error("controlled refresh worker did not join during cleanup")
		}
	})
	if err := s.db.Callback().Query().Before("gorm:query").Register("test:runtime_refresh_pause_database_read", func(tx *gorm.DB) {
		if tx.Statement.Table != "providers" {
			return
		}
		admitted.Do(func() { close(entered) })
		select {
		case <-release:
		case <-tx.Statement.Context.Done():
			_ = tx.AddError(tx.Statement.Context.Err())
		}
	}); err != nil {
		t.Fatal(err)
	}
	go func() {
		defer close(joined)
		finished <- s.RefreshRuntime(context.Background())
	}()
	select {
	case <-entered:
	case <-time.After(2 * runtimeRefreshTimeout):
		t.Fatal("refresh did not reach the controlled database read")
	}
	// Keep the gate held during the observation, but always release and join
	// before reporting a failed predecessor witness.
	proofCurrent := s.providerStatusRuntimeApplied(routes.Digest, s.egressGeneration.Load(), row)
	exclusiveAdmitted := s.runtime.publication.TryLock()
	if exclusiveAdmitted {
		s.runtime.publication.Unlock()
	}
	unchanged := s.runtime.auth.Load() == auth && s.runtime.routes.Load() == routes && s.runtime.status.Load() == status
	released.Do(func() { close(release) })
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal("controlled refresh did not finish", err)
		}
	case <-time.After(2 * runtimeRefreshTimeout):
		t.Fatal("controlled refresh did not join")
	}
	if !proofCurrent || !unchanged || exclusiveAdmitted {
		t.Fatal("in-progress database read falsely rejected current publication or changed it before completion")
	}
	if !s.providerStatusRuntimeApplied(routes.Digest, s.egressGeneration.Load(), row) {
		t.Fatal("identical completed refresh lost current Provider application")
	}
}

func TestRuntimeRefreshStoppedWhileWaitingForPublicationMutexCannotPublish(t *testing.T) {
	s, fixture, _, _ := providerMetadataSQLService(t)
	auth, routes, status := providerStatusStoppedRuntimeFacts(s)
	readComplete, finished, joined := make(chan struct{}), make(chan error, 1), make(chan struct{})
	var observed, unlocked sync.Once
	s.runtime.mu.Lock()
	t.Cleanup(func() {
		unlocked.Do(s.runtime.mu.Unlock)
		select {
		case <-joined:
		case <-time.After(2 * runtimeRefreshTimeout):
			t.Error("waiting publication worker did not join during cleanup")
		}
	})
	if err := s.db.Callback().Query().After("gorm:query").Register("test:runtime_refresh_read_before_mutex", func(tx *gorm.DB) {
		if tx.Statement.Table == "reservation_bounds" {
			observed.Do(func() { close(readComplete) })
		}
	}); err != nil {
		t.Fatal(err)
	}
	go func() {
		defer close(joined)
		finished <- s.RefreshRuntime(context.Background())
	}()
	select {
	case <-readComplete:
	case <-time.After(2 * runtimeRefreshTimeout):
		t.Fatal("refresh did not finish its controlled read before publication admission")
	}
	close(s.runtime.done)
	unlocked.Do(s.runtime.mu.Unlock)
	select {
	case err := <-finished:
		if err != runtimeUnavailable {
			t.Fatal("shutdown while waiting for publication mutex was not rejected", err)
		}
	case <-time.After(2 * runtimeRefreshTimeout):
		t.Fatal("stopped publication worker did not finish")
	}
	assertProviderStatusStoppedRuntimeFacts(t, s, auth, routes, status)
	if len(fixture.writes) != 0 || len(fixture.data.audits) != 0 {
		t.Fatal("late stopped refresh persisted effects", fixture.writes, fixture.data.audits)
	}
}
