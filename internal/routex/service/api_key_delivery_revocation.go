package service

import (
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"time"
)

func apiKeyDeliveryRevokeCommands(tx *gorm.DB, kind, key string, birth time.Time) error {
	now := time.Now().UTC().Truncate(time.Microsecond)
	q := vaultDB(tx).Model(&entity.APIKeyDeliveryCommand{}).Where("owner_kind = ? AND key_id = ? AND key_birth = ? AND revoked_at IS NULL", kind, key, birth)
	// Preserve acknowledgement/publication history. Unknown requires claimed and
	// no acknowledgement; not_attempted requires no claim. Every update is finite
	// and set-based, with exact owner locks already held by its caller.
	for _, state := range []struct {
		where    string
		delivery string
	}{{"acknowledged_at IS NOT NULL", "acknowledged"}, {"acknowledged_at IS NULL AND claimed_at IS NOT NULL", "unknown"}, {"acknowledged_at IS NULL AND claimed_at IS NULL", "not_attempted"}} {
		if e := q.Session(&gorm.Session{}).Where(state.where).Updates(map[string]any{"delivery_status": state.delivery, "runtime_status": "unknown", "revoked_at": now, "terminal_at": now, "failure_code": "key_revoked"}).Error; e != nil {
			return e
		}
	}
	return nil
}
func apiKeyDeliveryOffboarding(tx *gorm.DB, user string) error {
	// Project-owned resources are intentionally not selected by personal departure.
	if e := vaultDB(tx).Model(&entity.VaultAccessProfile{}).Where("owner_kind = ? AND owner_id = ?", "personal", user).Updates(map[string]any{"enabled": false, "status": "pending", "readiness_digest": "", "readiness_at": nil}).Error; e != nil {
		return e
	}
	var u entity.User
	if e := personalExact(vaultDB(tx), "id", user).Take(&u).Error; e != nil {
		return e
	}
	if u.ID != user {
		return apiKeyDeliveryConflict
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	for _, state := range []struct {
		where    string
		delivery string
	}{{"acknowledged_at IS NOT NULL", "acknowledged"}, {"acknowledged_at IS NULL AND claimed_at IS NOT NULL", "unknown"}, {"acknowledged_at IS NULL AND claimed_at IS NULL", "not_attempted"}} {
		if e := vaultDB(tx).Model(&entity.APIKeyDeliveryCommand{}).Where("owner_kind = ? AND owner_id = ? AND owner_birth = ? AND purpose <> ? AND revoked_at IS NULL", "personal", user, u.CreatedAt, "resolve").Where(state.where).Updates(map[string]any{"delivery_status": state.delivery, "runtime_status": "unknown", "revoked_at": now, "terminal_at": now, "failure_code": "owner_offboarded"}).Error; e != nil {
			return e
		}
	}
	return nil
}
