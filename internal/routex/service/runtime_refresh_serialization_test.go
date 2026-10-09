package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

type runtimeRefreshSerializationContext struct{}

type runtimeRefreshSerializationWorker struct {
	result chan error
	joined chan struct{}
	cancel context.CancelFunc
}

type runtimeRefreshSerializationGate struct {
	firstRead        chan struct{}
	releaseFirst     chan struct{}
	secondRead       chan struct{}
	releaseSecond    chan struct{}
	firstObserved    sync.Once
	secondObserved   sync.Once
	firstReleased    sync.Once
	secondReleased   sync.Once
	workers          []runtimeRefreshSerializationWorker
	publicationCodes []string
}

// A is paused after the complete snapshot read. B is stopped before its first
// SQL query, so even the predecessor witness cannot race mutable SQL fixtures.
func newRuntimeRefreshSerializationGate(t *testing.T, s *Service, failFirst bool) *runtimeRefreshSerializationGate {
	t.Helper()
	gate := &runtimeRefreshSerializationGate{
		firstRead: make(chan struct{}), releaseFirst: make(chan struct{}),
		secondRead: make(chan struct{}), releaseSecond: make(chan struct{}),
	}
	t.Cleanup(func() {
		gate.firstReleased.Do(func() { close(gate.releaseFirst) })
		// Release/join A before B can execute any shared-fixture SQL, even when a
		// predecessor assertion or an intermediate stage fails.
		if len(gate.workers) > 0 {
			select {
			case <-gate.workers[0].joined:
			case <-time.After(2 * runtimeRefreshTimeout):
				for _, worker := range gate.workers {
					worker.cancel()
				}
				t.Error("earlier serialization worker did not join; later SQL remains fenced")
				return
			}
		}
		gate.secondReleased.Do(func() { close(gate.releaseSecond) })
		for _, worker := range gate.workers {
			select {
			case <-worker.joined:
			case <-time.After(2 * runtimeRefreshTimeout):
				worker.cancel()
				t.Error("controlled serialization worker did not join")
			}
		}
	})
	if err := s.db.Callback().Create().After("gorm:create").Register("test:refresh_serialization_effect_order", func(tx *gorm.DB) {
		if row, ok := tx.Statement.Dest.(*entity.RuntimePublication); ok {
			gate.publicationCodes = append(gate.publicationCodes, row.ErrorCode)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.db.Callback().Query().After("gorm:query").Register("test:refresh_serialization_complete_read", func(tx *gorm.DB) {
		if tx.Statement.Context.Value(runtimeRefreshSerializationContext{}) != "first" || tx.Statement.Table != "reservation_bounds" {
			return
		}
		gate.firstObserved.Do(func() { close(gate.firstRead) })
		select {
		case <-gate.releaseFirst:
			if failFirst {
				_ = tx.AddError(errors.New("controlled earlier complete snapshot read failure"))
			}
		case <-tx.Statement.Context.Done():
			_ = tx.AddError(tx.Statement.Context.Err())
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.db.Callback().Query().Before("gorm:query").Register("test:refresh_serialization_first_query", func(tx *gorm.DB) {
		if tx.Statement.Context.Value(runtimeRefreshSerializationContext{}) != "second" || tx.Statement.Table != "users" {
			return
		}
		gate.secondObserved.Do(func() { close(gate.secondRead) })
		select {
		case <-gate.releaseSecond:
		case <-tx.Statement.Context.Done():
			_ = tx.AddError(tx.Statement.Context.Err())
		}
	}); err != nil {
		t.Fatal(err)
	}
	return gate
}

func (g *runtimeRefreshSerializationGate) start(s *Service, ctx context.Context, tag string) (runtimeRefreshSerializationWorker, <-chan struct{}) {
	ctx, cancel := context.WithCancel(ctx)
	worker := runtimeRefreshSerializationWorker{result: make(chan error, 1), joined: make(chan struct{}), cancel: cancel}
	called := make(chan struct{})
	g.workers = append(g.workers, worker)
	go func() {
		defer close(worker.joined)
		defer cancel()
		close(called)
		worker.result <- s.RefreshRuntime(context.WithValue(ctx, runtimeRefreshSerializationContext{}, tag))
	}()
	return worker, called
}

func waitRuntimeRefreshSerializationSignal(t *testing.T, signal <-chan struct{}, stage string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * runtimeRefreshTimeout):
		t.Fatal("controlled refresh stage did not finish", stage)
	}
}

func waitRuntimeRefreshSerializationResult(t *testing.T, worker runtimeRefreshSerializationWorker) error {
	t.Helper()
	select {
	case err := <-worker.result:
		waitRuntimeRefreshSerializationSignal(t, worker.joined, "worker join")
		return err
	case <-time.After(2 * runtimeRefreshTimeout):
		t.Fatal("controlled refresh worker did not return")
		return nil
	}
}

// This test compiles against immutable R3 without new fields or APIs. Its old
// failure is premature B read admission, not concurrent fake-driver mutation.
func TestRuntimeRefreshSerializesCompleteReadsAndDrainsEarlierFailure(t *testing.T) {
	for _, failFirst := range []bool{false, true} {
		name := "complete_read"
		if failFirst {
			name = "failed_read"
		}
		t.Run(name, func(t *testing.T) {
			s, fixture, _, state := providerMetadataSQLService(t)
			if err := s.RefreshRuntime(context.Background()); err != nil {
				t.Fatal(err)
			}
			auth, routes, status := s.runtime.auth.Load(), s.runtime.routes.Load(), s.runtime.status.Load()
			provider := state.base.provider
			gate := newRuntimeRefreshSerializationGate(t, s, failFirst)
			first, _ := gate.start(s, context.Background(), "first")
			waitRuntimeRefreshSerializationSignal(t, gate.firstRead, "earlier complete read")
			second, called := gate.start(s, context.Background(), "second")
			waitRuntimeRefreshSerializationSignal(t, called, "queued refresh call")
			premature := false
			select {
			case <-gate.secondRead:
				premature = true
			case <-time.After(100 * time.Millisecond):
			}
			currentProof := s.providerStatusRuntimeApplied(routes.Digest, s.egressGeneration.Load(), provider)
			unchanged := s.runtime.auth.Load() == auth && s.runtime.routes.Load() == routes && s.runtime.status.Load() == status
			gate.firstReleased.Do(func() { close(gate.releaseFirst) })
			firstErr := waitRuntimeRefreshSerializationResult(t, first)
			if failFirst {
				if firstErr != runtimeUnavailable || s.RuntimeStatus().ErrorCode != "database_unavailable" {
					t.Fatal("earlier controlled failure was not drained before the later read", firstErr)
				}
			} else if firstErr != nil {
				t.Fatal("earlier refresh failed", firstErr)
			}
			// B cannot touch the shared fake SQL state until A has positively joined.
			waitRuntimeRefreshSerializationSignal(t, gate.secondRead, "later first query")
			gate.secondReleased.Do(func() { close(gate.releaseSecond) })
			if err := waitRuntimeRefreshSerializationResult(t, second); err != nil {
				t.Fatal("later refresh failed", err)
			}
			if premature || !currentProof || !unchanged {
				t.Fatal("later refresh entered its database read before the earlier read/effects drained, or hid the current proof")
			}
			latest := s.runtime.routes.Load()
			if latest == nil || !s.providerStatusRuntimeApplied(latest.Digest, s.egressGeneration.Load(), provider) || s.RuntimeStatus().ErrorCode != "" {
				t.Fatal("later completed refresh did not restore exact current publication")
			}
			if len(gate.publicationCodes) != 2 || gate.publicationCodes[1] != "" ||
				!failFirst && gate.publicationCodes[0] != "" || failFirst && gate.publicationCodes[0] != "database_unavailable" ||
				len(fixture.data.audits) != 0 {
				t.Fatal("draining changed effect order or manufactured a domain audit", gate.publicationCodes)
			}

		})
	}
}

func TestRuntimeRefreshQueuedCancellationCannotBorrowDatabase(t *testing.T) {
	s, fixture, _, _ := providerMetadataSQLService(t)
	if err := s.RefreshRuntime(context.Background()); err != nil {
		t.Fatal(err)
	}
	gate := newRuntimeRefreshSerializationGate(t, s, false)
	first, _ := gate.start(s, context.Background(), "first")
	waitRuntimeRefreshSerializationSignal(t, gate.firstRead, "held earlier snapshot")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	second, called := gate.start(s, ctx, "second")
	waitRuntimeRefreshSerializationSignal(t, called, "queued cancelling call")
	waitRuntimeRefreshSerializationSignal(t, ctx.Done(), "queued deadline")
	premature := false
	select {
	case <-gate.secondRead:
		premature = true
	default:
	}
	gate.firstReleased.Do(func() { close(gate.releaseFirst) })
	if err := waitRuntimeRefreshSerializationResult(t, first); err != nil {
		t.Fatal(err)
	}
	queries, writes := len(fixture.queries), len(fixture.writes)
	if err := waitRuntimeRefreshSerializationResult(t, second); err != runtimeUnavailable {
		t.Fatal("cancelled queued refresh was not rejected", err)
	}
	if premature || len(fixture.queries) != queries || len(fixture.writes) != writes {
		t.Fatal("cancelled queued refresh borrowed the database or changed publication")
	}
}

func TestRuntimeRefreshQueuedStopPreservesPublicationAndDenials(t *testing.T) {
	s, fixture, _, _ := providerMetadataSQLService(t)
	if err := s.RefreshRuntime(context.Background()); err != nil {
		t.Fatal(err)
	}
	auth, routes, status := providerStatusStoppedRuntimeFacts(s)
	queriesBefore, writesBefore := len(fixture.queries), len(fixture.writes)
	gate := newRuntimeRefreshSerializationGate(t, s, false)
	first, _ := gate.start(s, context.Background(), "first")
	waitRuntimeRefreshSerializationSignal(t, gate.firstRead, "held snapshot before stop")
	second, called := gate.start(s, context.Background(), "second")
	waitRuntimeRefreshSerializationSignal(t, called, "queued stopped call")
	premature := false
	select {
	case <-gate.secondRead:
		premature = true
	case <-time.After(100 * time.Millisecond):
	}
	close(s.runtime.done)
	gate.firstReleased.Do(func() { close(gate.releaseFirst) })
	if err := waitRuntimeRefreshSerializationResult(t, first); err != runtimeUnavailable {
		t.Fatal("stopped earlier refresh published", err)
	}
	queries := len(fixture.queries)
	if err := waitRuntimeRefreshSerializationResult(t, second); err != runtimeUnavailable {
		t.Fatal("stopped queued refresh published", err)
	}
	if premature || queries <= queriesBefore || len(fixture.queries) != queries || len(fixture.writes) != writesBefore {
		t.Fatal("stopped queued refresh read or persisted effects")
	}
	assertProviderStatusStoppedRuntimeFacts(t, s, auth, routes, status)
}

func TestRuntimeRefreshSerializationWaitDoesNotPinPublicationOrDatabase(t *testing.T) {
	// No database exists: crossing the cancelled admission check would panic.
	s := &Service{runtime: &gatewayRuntime{done: make(chan struct{})}}
	auth, routes, status := providerStatusStoppedRuntimeFacts(s)
	s.runtime.refreshMu.Lock()
	var released sync.Once
	joined, finished := make(chan struct{}), make(chan error, 1)
	t.Cleanup(func() {
		released.Do(s.runtime.refreshMu.Unlock)
		waitRuntimeRefreshSerializationSignal(t, joined, "serialization waiter cleanup")
	})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	go func() { defer close(joined); finished <- s.RefreshRuntime(ctx) }()
	waitRuntimeRefreshSerializationSignal(t, ctx.Done(), "serialization wait deadline")
	exclusive := s.runtime.publication.TryLock()
	if exclusive {
		s.runtime.publication.Unlock()
	}
	released.Do(s.runtime.refreshMu.Unlock)
	select {
	case err := <-finished:
		if err != runtimeUnavailable {
			t.Fatal("cancelled serialization waiter was not rejected", err)
		}
	case <-time.After(2 * runtimeRefreshTimeout):
		t.Fatal("serialization waiter did not join")
	}
	waitRuntimeRefreshSerializationSignal(t, joined, "serialization waiter join")
	if !exclusive {
		t.Fatal("queued refresh pinned publication admission before serialization")
	}
	assertProviderStatusStoppedRuntimeFacts(t, s, auth, routes, status)
}
