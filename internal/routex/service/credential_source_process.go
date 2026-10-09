package service

import (
	"context"
	"sync"
)

// The registry belongs to one Service/process generation and is never reopened.
// Both native ownership and finite SDK lifetimes contribute to this join.
type credentialProcessTicket struct {
	registry          *credentialSourceHolders
	once              sync.Once
	claimed, released bool
}

func (r *credentialSourceHolders) processAcquire() (*credentialProcessTicket, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.processClosed {
		return nil, vaultUnavailable
	}
	r.processHolders++
	return &credentialProcessTicket{registry: r}, nil
}
func (r *credentialSourceHolders) processDrainedLocked() {
	if r.processClosed && r.processHolders == 0 && !r.processDrainClosed {
		r.processDrainClosed = true
		close(r.processDrained)
	}
}
func (r *credentialSourceHolders) processAdmissionClosed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.processClosed
}
func (r *credentialSourceHolders) closeProcessAdmission() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.processClosed {
		r.processClosed = true
		r.processDrained = make(chan struct{})
	}
	r.processDrainedLocked()
}
func (r *credentialSourceHolders) processJoined() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.processClosed && r.processHolders == 0 && !r.processPoisoned
}
func (r *credentialSourceHolders) joinProcess(ctx context.Context) error {
	r.closeProcessAdmission()
	r.mu.Lock()
	done := r.processDrained
	r.mu.Unlock()
	select {
	case <-done:
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !r.processJoined() {
			return vaultUnavailable
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (t *credentialProcessTicket) claim(r *credentialSourceHolders) bool {
	if t == nil || t.registry != r {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if t.claimed || t.released {
		return false
	}
	t.claimed = true
	return true
}
func (t *credentialProcessTicket) poison() {
	if t == nil {
		return
	}
	t.registry.mu.Lock()
	t.registry.processPoisoned = true
	t.registry.mu.Unlock()
}
func (t *credentialProcessTicket) release() {
	if t == nil {
		return
	}
	t.once.Do(func() {
		t.registry.mu.Lock()
		defer t.registry.mu.Unlock()
		t.released = true
		t.registry.processHolders--
		t.registry.processDrainedLocked()
	})
}

// BeginCredentialSourceShutdown closes admission synchronously, without DB or
// waiting. Already admitted native bodies/SDK lifetimes must still really join.
func (s *Service) BeginCredentialSourceShutdown() { s.credentialSources.closeProcessAdmission() }
