package service

import (
	"context"
	"fmt"
	"slices"
	"sync/atomic"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var secretPolicyChanged = &apperrors.Error{Code: 503, Message: "secret_write_policy_changed"}

type secretPolicyView struct {
	epoch   uint64
	writeID string
	store   *secretstore.Store
	refs    atomic.Int64
	closed  atomic.Bool
}

// WithSecretRotationClock controls observation time only. It never extends an
// instance or authorization lease and never changes inventory/publication proof.
func WithSecretRotationClock(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.rootNow = now
		}
	}
}
func (s *Service) secretNow() time.Time {
	if s.rootNow != nil {
		return s.rootNow().UTC().Truncate(time.Microsecond)
	}
	return time.Now().UTC().Truncate(time.Microsecond)
}
func (s *Service) secretEpoch() uint64 {
	if p := s.rootPolicy.Load(); p != nil {
		return p.epoch
	}
	return 0
}
func (s *Service) sealSecret(reference, plaintext string) (string, error) {
	p := s.rootPolicy.Load()
	if p == nil {
		if s.secrets == nil {
			return "", secretStoreUnavailable
		}
		return s.secrets.Seal(reference, plaintext)
	}
	if p.closed.Load() {
		return "", secretPolicyChanged
	}
	return p.store.Seal(reference, plaintext)
}
func (s *Service) openSecret(reference, ciphertext string) (string, error) {
	if s.rootReadersClosed.Load() {
		return "", secretStoreUnavailable
	}
	s.rootReaders.Add(1)
	defer s.rootReaders.Add(-1)
	if s.rootReadersClosed.Load() {
		return "", secretStoreUnavailable
	}
	p := s.rootPolicy.Load()
	if p == nil {
		if s.secrets == nil {
			return "", secretStoreUnavailable
		}
		return s.secrets.Open(reference, ciphertext)
	}
	if p.closed.Load() {
		return "", secretStoreUnavailable
	}
	p.refs.Add(1)
	defer p.refs.Add(-1)
	if p.closed.Load() || s.rootPolicy.Load() != p {
		return "", secretStoreUnavailable
	}
	return p.store.Open(reference, ciphertext)
}
func lockedSecretPolicy(tx *gorm.DB) (entity.SecretWritePolicy, error) {
	var p entity.SecretWritePolicy
	err := tx.Session(&gorm.Session{}).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&p, 1).Error
	return p, err
}

// guardSecretWrite holds the singleton through commit. It never repairs or
// reseals stale prepared data: the whole caller transaction must fail.
func (s *Service) guardSecretWrite(tx *gorm.DB, epoch uint64, reference, ciphertext string) error {
	if ciphertext == "" {
		return nil
	}
	p, err := lockedSecretPolicy(tx)
	if err != nil {
		return err
	}
	if !p.Initialized {
		if epoch != 0 {
			return secretPolicyChanged
		}
		return nil
	}
	view := s.rootPolicy.Load()
	if view == nil || view.closed.Load() || view.epoch != p.Epoch || epoch != p.Epoch || p.WriteKeyID == nil || view.writeID != *p.WriteKeyID {
		return secretPolicyChanged
	}
	key, err := view.store.KeyID(reference, ciphertext)
	if err != nil || key != view.writeID {
		return secretPolicyChanged
	}
	return nil
}
func rootProofReference(id string) string { return "root-key-proof:" + id }

const rootProofPlaintext = "routex-root-key-proof-v1"

func (s *Service) installSecretPolicy(p entity.SecretWritePolicy, keys []entity.SecretRootKey) error {
	if !p.Initialized {
		s.rootPolicy.Store(nil)
		return nil
	}
	if s.secrets == nil || p.WriteKeyID == nil {
		return secretStoreUnavailable
	}
	write, err := s.secrets.WithWriteKey(*p.WriteKeyID)
	if err != nil {
		return secretStoreUnavailable
	}
	allowed := []string{}
	writeCount := 0
	for _, key := range keys {
		if key.State == "write" {
			writeCount++
			if key.KeyID != *p.WriteKeyID {
				return secretStoreUnavailable
			}
		}
	}
	if writeCount != 1 {
		return secretStoreUnavailable
	}
	for _, key := range keys {
		if key.State == "retired" {
			continue
		}
		plain, err := s.secrets.Open(rootProofReference(key.KeyID), key.ProofCiphertext)
		id, idErr := s.secrets.KeyID(rootProofReference(key.KeyID), key.ProofCiphertext)
		if err != nil || idErr != nil || plain != rootProofPlaintext || id != key.KeyID {
			return secretStoreUnavailable
		}
		allowed = append(allowed, key.KeyID)
	}
	view, err := write.WithAllowedKeys(allowed)
	if err != nil {
		return secretStoreUnavailable
	}
	s.rootPolicy.Store(&secretPolicyView{epoch: p.Epoch, writeID: *p.WriteKeyID, store: view})
	s.rootReadersClosed.Store(false)
	return nil
}
func (s *Service) loadSecretPolicy(ctx context.Context) error {
	var p entity.SecretWritePolicy
	var keys []entity.SecretRootKey
	if err := s.authDB(ctx).Take(&p, 1).Error; err != nil {
		return err
	}
	if err := s.authDB(ctx).Order("key_id").Find(&keys).Error; err != nil {
		return err
	}
	return s.installSecretPolicy(p, keys)
}

// InitializeSecretStore binds pre-provisioned material to durable IDs before any
// runtime/worker starts. An initialized policy can never fall back to legacy.
func (s *Service) InitializeSecretStore(ctx context.Context) error {
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		p, err := lockedSecretPolicy(tx)
		if err != nil {
			return err
		}
		if !p.Initialized && (s.secrets == nil || len(s.secrets.KeyIDs()) == 0) {
			return nil
		}
		if s.secrets == nil || len(s.secrets.KeyIDs()) == 0 {
			return secretStoreUnavailable
		}
		var saved []entity.SecretRootKey
		if err := tx.Order("key_id").Find(&saved).Error; err != nil {
			return err
		}
		for _, key := range saved {
			if key.State != "retired" {
				plain, err := s.secrets.Open(rootProofReference(key.KeyID), key.ProofCiphertext)
				if err != nil || plain != rootProofPlaintext {
					return secretStoreUnavailable
				}
			}
		}
		for _, keyID := range s.secrets.KeyIDs() {
			if slices.ContainsFunc(saved, func(v entity.SecretRootKey) bool { return v.KeyID == keyID }) {
				continue
			}
			view, err := s.secrets.WithWriteKey(keyID)
			if err != nil {
				return secretStoreUnavailable
			}
			proof, err := view.Seal(rootProofReference(keyID), rootProofPlaintext)
			if err != nil {
				return secretStoreUnavailable
			}
			row := entity.SecretRootKey{KeyID: keyID, State: "decrypt_only", ProofCiphertext: proof, CreatedAt: s.secretNow()}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
		if !p.Initialized {
			id := s.secrets.WriteKeyID()
			p.Initialized = true
			p.Epoch = 1
			p.WriteKeyID = &id
			p.ETag = rootHash(fmt.Sprintf("policy:%d:%s", p.Epoch, id))
			p.UpdatedAt = s.secretNow()
			if err := tx.Save(&p).Error; err != nil {
				return err
			}
			if err := personalExact(tx.Model(&entity.SecretRootKey{}), "key_id", id).Update("state", "write").Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return catalogError(err)
	}
	if err = s.loadSecretPolicy(ctx); err != nil {
		return catalogError(err)
	}
	// Full bounded authenticated inventory on bootstrap detects missing/corrupt or
	// retired dependencies, including retained inactive/history records.
	if s.rootPolicy.Load() != nil {
		for _, domain := range rootDomains {
			cursor := ""
			for {
				rows, err := s.rootInventoryPage(ctx, domain, cursor)
				if err != nil {
					return catalogError(err)
				}
				for _, row := range rows {
					if _, err := s.openSecret(row.reference, row.ciphertext); err != nil {
						return secretStoreUnavailable
					}
					cursor = row.id
				}
				if len(rows) < 50 {
					break
				}
			}
		}
	}
	return nil
}
func (s *Service) drainSecretView(ctx context.Context, p *secretPolicyView) error {
	if p == nil || !s.rootReadersClosed.CompareAndSwap(false, true) {
		return secretStoreUnavailable
	}
	if !p.closed.CompareAndSwap(false, true) {
		s.rootReadersClosed.Store(false)
		return secretStoreUnavailable
	}
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for p.refs.Load() != 0 || s.rootReaders.Load() != 0 {
		select {
		case <-ctx.Done():
			p.closed.Store(false)
			s.rootReadersClosed.Store(false)
			return ctx.Err()
		case <-tick.C:
		}
	}
	return nil
}
