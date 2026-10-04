package service

import (
	"sync"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/id"
)

// All Personal Key writers serialize commit and reductions against publication.
// Release before RefreshRuntime, which takes the shared publication gate itself.
// In particular an older disable cannot install its tombstone after a newer
// owner re-enable has committed. Already dispatched attempts are not replayed.
func (s *Service) pinPersonalKeyMutation() func() {
	if s.runtime == nil {
		return func() {}
	}
	runtime := s.runtime
	runtime.publication.Lock()
	var once sync.Once
	return func() { once.Do(runtime.publication.Unlock) }
}

// A stored random revision distinguishes lifecycle ABA changes independently of
// database timestamp precision. Every product writer advances this identity.
func advancePersonalKeyRevision(key *entity.APIKey) error {
	revision, err := id.NewPrefixed("kvr")
	if err != nil {
		return err
	}
	key.LifecycleRevision = revision
	return nil
}

func changePersonalKeyStatus(tx *gorm.DB, key *entity.APIKey, status string) error {
	if err := advancePersonalKeyRevision(key); err != nil {
		return err
	}
	key.Status = status
	return tx.Model(key).Where("user_id = ?", key.UserID).Updates(map[string]any{"status": status, "lifecycle_revision": key.LifecycleRevision}).Error
}

func revokeOwnerPersonalKeys(tx *gorm.DB, userID string) error {
	var keys []entity.APIKey
	if err := tx.Select("id", "user_id", "status").Where("user_id = ? AND status <> ?", userID, entity.KeyRevoked).Order("id").Find(&keys).Error; err != nil {
		return err
	}
	for index := range keys {
		if keys[index].UserID != userID {
			return errKeyConflict
		}
		if err := changePersonalKeyStatus(tx, &keys[index], entity.KeyRevoked); err != nil {
			return err
		}
	}
	return nil
}
