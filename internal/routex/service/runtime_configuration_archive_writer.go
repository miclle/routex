package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

const runtimeArchiveWriterBudget = 3 * time.Second

type runtimeArchiveWriterResult struct {
	Sequence  uint64
	State     string
	ArchiveID string
}
type runtimeArchiveWriterState struct {
	admission sync.RWMutex
	closed    bool
	queue     chan *runtimeConfigurationCaptureV1
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	latest    atomic.Pointer[runtimeArchiveWriterResult]
}

// One queued immutable capture and one in-flight capture bound retained input.
// The worker owns its own finite deadline, never the refresh request's budget.
func (w *runtimeArchiveWriterState) start(ctx context.Context, write func(context.Context, *runtimeConfigurationCaptureV1) runtimeArchiveWriterResult) {
	w.ctx, w.cancel = context.WithCancel(ctx)
	w.queue = make(chan *runtimeConfigurationCaptureV1, 1)
	w.done = make(chan struct{})
	go func() {
		defer close(w.done)
		defer func() {
			for {
				select {
				case <-w.queue:
				default:
					return
				}
			}
		}()
		for {
			select {
			case <-w.ctx.Done():
				return
			case capture := <-w.queue:
				if w.ctx.Err() != nil {
					return
				}
				op, cancel := context.WithTimeout(w.ctx, runtimeArchiveWriterBudget)
				result := write(op, capture)
				cancel()
				if result.Sequence != capture.sequence {
					return
				}
				w.observe(result)
			}
		}
	}()
}
func (w *runtimeArchiveWriterState) observe(result runtimeArchiveWriterResult) {
	for {
		previous := w.latest.Load()
		if previous != nil && previous.Sequence > result.Sequence {
			return
		}
		next := result
		if w.latest.CompareAndSwap(previous, &next) {
			return
		}
	}
}
func (w *runtimeArchiveWriterState) submit(capture *runtimeConfigurationCaptureV1) {
	if capture == nil || w.ctx == nil {
		return
	}
	if !w.admission.TryRLock() {
		w.observe(runtimeArchiveWriterResult{Sequence: capture.sequence, State: "queue_full"})
		return
	}
	defer w.admission.RUnlock()
	if w.closed || w.ctx.Err() != nil {
		return
	}
	select {
	case w.queue <- capture:
	default:
		w.observe(runtimeArchiveWriterResult{Sequence: capture.sequence, State: "queue_full"})
	}
}
func (w *runtimeArchiveWriterState) stop() {
	if w.cancel == nil {
		return
	}
	w.admission.Lock()
	w.closed = true
	w.cancel()
	w.admission.Unlock()
	<-w.done
}
func runtimeArchiveCaptureProvenanceValidV1(c *runtimeConfigurationCaptureV1) bool {
	return c != nil && c.sequence > 0 && runtimeSnapshotID.MatchString(c.snapshotID) && !c.routesPublishedAt.IsZero() && !c.capturedAt.IsZero() && c.capturedAt.Equal(c.capturedAt.Truncate(time.Microsecond)) && !c.capturedAt.Before(c.routesPublishedAt.UTC().Truncate(time.Microsecond)) && runtimeRouteDigest.MatchString(c.sourceDigest) && runtimeRouteDigest.MatchString(c.authorizationDigest) && runtimeRouteDigest.MatchString(c.networkDigest) && runtimeRouteDigest.MatchString(c.contentDigest) && len(c.canonical) > 0 && len(c.canonical) <= runtimeConfigurationMaxBytesV1
}

func (s *Service) writeRuntimeConfigurationCaptureV1(ctx context.Context, capture *runtimeConfigurationCaptureV1) runtimeArchiveWriterResult {
	result := runtimeArchiveWriterResult{State: "failed"}
	if capture != nil {
		result.Sequence = capture.sequence
	}
	if !runtimeArchiveCaptureProvenanceValidV1(capture) || ctx == nil || ctx.Err() != nil {
		return result
	}
	facts, err := reconstructRuntimeConfigurationV1(ctx, capture.canonical, capture.contentDigest)
	if err != nil || facts == nil || facts.Document.SourceDigest != capture.sourceDigest || facts.Document.AuthorizationProjectionDigest != capture.authorizationDigest || facts.Document.NetworkPolicyDigest != capture.networkDigest {
		return result
	}
	identity, birth, err := newRuntimeArchiveIdentity()
	if err != nil {
		return result
	}
	saved, deduplicated, err := s.storeRuntimeArchiveIntentV1(ctx, identity, birth, facts.Document, capture)
	switch {
	case errors.Is(err, errRuntimeArchiveVersionCap):
		result.State = "paused_version_cap"
	case errors.Is(err, errRuntimeArchiveByteCap):
		result.State = "paused_byte_cap"
	case err != nil:
		return result
	default:
		result.ArchiveID = saved
		result.State = "stored"
		if deduplicated {
			result.State = "deduplicated"
		}
	}
	return result
}
