package service

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

// A logical source key excludes root ciphertext and current configuration.
// Closing a key is local admission only, never a fleet or remote cleanup proof.
type credentialSourceKey struct {
	CredentialID, CredentialBirth, ReferenceID        string
	IntegrationID, IntegrationBirth, RevisionID       string
	ReaderGeneration, ReaderMethod, Descriptor, Proof string
	PhysicalObject                                    string
	Marker                                            string
}

func credentialHolderKey(c entity.ProviderCredential) (credentialSourceKey, error) {
	if c.StorageSource == "" || c.StorageSource == "inline" {
		return credentialSourceKey{}, nil
	}
	proof := credentialSourceProof(c)
	if proof == "" || c.VaultReference == nil || !vaultStoredMethod(c.VaultReference.ReaderMethod) || !personalModelETag(c.VaultReference.PhysicalObject) {
		return credentialSourceKey{}, vaultUnavailable
	}
	r := c.VaultReference
	return credentialSourceKey{CredentialID: c.ID, CredentialBirth: c.CreatedAt.UTC().Format(time.RFC3339Nano), ReferenceID: r.ReferenceID,
		IntegrationID: r.IntegrationID, IntegrationBirth: r.IntegrationBirth.UTC().Format(time.RFC3339Nano), RevisionID: r.RevisionID,
		ReaderGeneration: r.ReaderGeneration, ReaderMethod: r.ReaderMethod, Descriptor: r.DescriptorSHA256, Proof: proof, PhysicalObject: r.PhysicalObject, Marker: r.ExpectedMarkerSHA256}, nil
}

type credentialSourceHolders struct {
	mu                                                 sync.Mutex
	states                                             map[credentialSourceKey]*credentialSourceState
	processClosed, processPoisoned, processDrainClosed bool
	processHolders                                     int
	processDrained                                     chan struct{}
}

type credentialSourceState struct {
	mu       sync.Mutex
	closed   bool
	poisoned bool
	holders  int
	drained  chan struct{}
}

type credentialSourceHolder struct {
	registry              *credentialSourceHolders
	key                   credentialSourceKey
	state                 *credentialSourceState
	mu                    sync.Mutex
	released, transferred bool
}

// stateLocked requires r.mu. Open idle entries are reclaimed on final release;
// closed entries remain tombstones and count against the unchanged bound.
func physicalHolderKey(key credentialSourceKey) credentialSourceKey {
	return credentialSourceKey{PhysicalObject: key.PhysicalObject}
}

func (r *credentialSourceHolders) stateLocked(key credentialSourceKey) (*credentialSourceState, error) {
	if !personalModelETag(key.PhysicalObject) {
		return nil, vaultUnavailable
	}
	key = physicalHolderKey(key)
	if r.states == nil {
		r.states = map[credentialSourceKey]*credentialSourceState{}
	}
	if state := r.states[key]; state != nil {
		return state, nil
	}
	// Retain closed generations. Capacity pressure cannot reopen a denied source.
	if len(r.states) >= 5000 {
		return nil, vaultUnavailable
	}
	state := &credentialSourceState{drained: make(chan struct{})}
	r.states[key] = state
	return state, nil
}

func (r *credentialSourceHolders) acquire(key credentialSourceKey) (*credentialSourceHolder, error) {
	if key == (credentialSourceKey{}) {
		return nil, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.processClosed {
		return nil, vaultUnavailable
	}
	state, err := r.stateLocked(key)
	if err != nil {
		return nil, err
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.closed {
		return nil, vaultUnavailable
	}
	state.holders++
	r.processHolders++
	return &credentialSourceHolder{registry: r, key: key, state: state}, nil
}

func (s *Service) acquireCredentialSource(c entity.ProviderCredential) (*credentialSourceHolder, error) {
	key, err := credentialHolderKey(c)
	if err != nil {
		return nil, err
	}
	return s.credentialSources.acquire(key)
}

// closeOnly installs a sticky local denial without waiting, including during
// registration. A poisoned zero-holder state never becomes successful drain.
func (r *credentialSourceHolders) closeOnly(object string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	state, err := r.stateLocked(credentialSourceKey{PhysicalObject: object})
	if err != nil {
		return false
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if !state.closed {
		state.closed = true
		if state.holders == 0 {
			close(state.drained)
		}
	}
	return state.holders == 0 && !state.poisoned
}
func (r *credentialSourceHolders) joined(object string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.states[credentialSourceKey{PhysicalObject: object}]
	if state == nil {
		return false
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.closed && state.holders == 0 && !state.poisoned
}

// closeAndWait joins only local holders. The published cleanup coordinator must
// separately prove durable denial and every registered generation acknowledgement.
func (r *credentialSourceHolders) closeAndWait(ctx context.Context, key credentialSourceKey) error {
	if key == (credentialSourceKey{}) {
		return vaultUnavailable
	}
	r.mu.Lock()
	state, err := r.stateLocked(key)
	if err != nil {
		r.mu.Unlock()
		return err
	}
	state.mu.Lock()
	if !state.closed {
		state.closed = true
		if state.holders == 0 {
			close(state.drained)
		}
	}
	state.mu.Unlock()
	r.mu.Unlock()
	select {
	case <-state.drained:
		state.mu.Lock()
		defer state.mu.Unlock()
		if state.poisoned {
			return vaultUnavailable
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *credentialSourceHolder) release() {
	if h == nil {
		return
	}
	h.mu.Lock()
	if h.released {
		h.mu.Unlock()
		return
	}
	h.released = true
	h.mu.Unlock()
	h.registry.mu.Lock()
	defer h.registry.mu.Unlock()
	h.state.mu.Lock()
	h.state.holders--
	h.registry.processHolders--
	h.registry.processDrainedLocked()
	if h.state.holders == 0 {
		if h.state.closed {
			close(h.state.drained)
		} else if !h.state.poisoned {
			delete(h.registry.states, physicalHolderKey(h.key))
		}
	}
	h.state.mu.Unlock()
}

// Poison is sticky for the physical object, including its other logical aliases.
// Publish it before releasing ownership: joined zero is never successful drain.
func (h *credentialSourceHolder) poison() {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.released {
		return
	}
	h.registry.mu.Lock()
	defer h.registry.mu.Unlock()
	h.state.mu.Lock()
	defer h.state.mu.Unlock()
	h.state.poisoned = true
	h.registry.processPoisoned = true
}

func (h *credentialSourceHolder) releasePlan() {
	if h == nil {
		return
	}
	h.mu.Lock()
	transferred := h.transferred
	h.mu.Unlock()
	if !transferred {
		h.release()
	}
}

// This short source-local admission boundary precedes Attempt creation or
// prepared commit. A holder admitted before closure may finish; the closer
// must join it and recheck current dependencies afterward. No lock spans DB,
// HTTP, body consumption or drain waiting.
func (h *credentialSourceHolder) admitUse() bool {
	if h == nil {
		return true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.registry.mu.Lock()
	defer h.registry.mu.Unlock()
	h.state.mu.Lock()
	defer h.state.mu.Unlock()
	return !h.released && !h.state.closed && !h.registry.processClosed
}

type credentialHeldBody struct {
	io.ReadCloser
	holder *credentialSourceHolder
	once   sync.Once
	err    error
}

func (h *credentialSourceHolder) body(body io.ReadCloser) io.ReadCloser {
	if h == nil {
		return body
	}
	h.mu.Lock()
	h.transferred = true
	h.mu.Unlock()
	return &credentialHeldBody{ReadCloser: body, holder: h}
}

func (b *credentialHeldBody) Close() error {
	b.once.Do(func() {
		b.err = b.ReadCloser.Close()
		if b.err != nil {
			b.holder.poison()
		}
		b.holder.release()
	})
	return b.err
}
