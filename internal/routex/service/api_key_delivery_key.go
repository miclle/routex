package service

import (
	"errors"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"slices"
	"time"
)

func apiKeyDeliveryKeyDigest(kind, id string, birth time.Time) string {
	return apiKeyDeliveryDigest("api-key-delivery:key:v1", struct {
		Kind, ID string
		Birth    time.Time
	}{kind, id, birth.UTC()})
}
func apiKeyDeliveryConfirmMode(mode, id string, birth time.Time, ack *entity.APIKeyVaultDeliveryBinding) error {
	switch mode {
	case "manual":
		if ack == nil {
			return nil
		}
	case "managed_vault":
		if ack != nil && ack.KeyID == id && ack.KeyBirth.Equal(birth) && ack.Version == 1 && ack.AcknowledgedAt != nil && ack.KeyDigest == apiKeyDeliveryKeyDigest(ack.KeyKind, id, birth) {
			return nil
		}
	}
	return errKeyConflict
}
func apiKeyDeliveryAcknowledged(tx *gorm.DB, kind, id string, birth time.Time) (entity.APIKeyVaultDeliveryBinding, error) {
	var b entity.APIKeyVaultDeliveryBinding
	digest := apiKeyDeliveryKeyDigest(kind, id, birth)
	if e := personalExact(vaultDB(tx), "key_digest", digest).Take(&b).Error; e != nil {
		return b, e
	}
	if b.KeyKind != kind || b.KeyID != id || !b.KeyBirth.Equal(birth) || b.KeyDigest != digest || b.Version != 1 || b.AcknowledgedAt == nil {
		return b, errKeyConflict
	}
	if b.OwnerKind != kind || b.OwnerDigest != apiKeyDeliveryOwnerDigest(kind, b.OwnerID, b.OwnerBirth) {
		return b, errKeyConflict
	}
	switch kind {
	case "personal":
		var k entity.APIKey
		if e := personalExact(vaultDB(tx), "id", id).Take(&k).Error; e != nil {
			return b, e
		}
		var u entity.User
		if e := personalExact(vaultDB(tx), "id", b.OwnerID).Take(&u).Error; e != nil {
			return b, e
		}
		if k.ID != id || k.UserID != b.OwnerID || k.DeliveryMode != "managed_vault" || !k.CreatedAt.Equal(birth) || u.ID != b.OwnerID || !u.CreatedAt.Equal(b.OwnerBirth) {
			return b, errKeyConflict
		}
	case "project":
		var k entity.ProjectKey
		if e := personalExact(vaultDB(tx), "id", id).Take(&k).Error; e != nil {
			return b, e
		}
		var p entity.Project
		if e := personalExact(vaultDB(tx), "id", b.OwnerID).Take(&p).Error; e != nil {
			return b, e
		}
		if k.ID != id || k.ProjectID != b.OwnerID || k.DeliveryMode != "managed_vault" || !k.CreatedAt.Equal(birth) || p.ID != b.OwnerID || !p.CreatedAt.Equal(b.OwnerBirth) {
			return b, errKeyConflict
		}
	default:
		return b, errKeyConflict
	}
	// Version alone never authorizes a forged or revoked acknowledgement.
	var c entity.APIKeyDeliveryCommand
	if e := personalExact(vaultDB(tx), "binding_id", b.ID).Take(&c).Error; e != nil {
		return b, e
	}
	if c.BindingID != b.ID || c.KeyID != id || c.KeyBirth == nil || !c.KeyBirth.Equal(birth) || c.OwnerKind != kind || c.OwnerDigest != b.OwnerDigest || c.OwnerID != b.OwnerID || !c.OwnerBirth.Equal(b.OwnerBirth) || c.ProfileID != b.ProfileID || !c.ProfileBirth.Equal(b.ProfileBirth) || c.PrincipalID != b.PrincipalID || !c.PrincipalBirth.Equal(b.PrincipalBirth) || c.IsolationRevision != b.IsolationRevision || c.DeliveryStatus != "acknowledged" || c.AcknowledgedAt == nil || !c.AcknowledgedAt.Equal(*b.AcknowledgedAt) || c.ClaimedAt == nil || c.ClaimProcessBirth == nil || c.ClaimProcessID == "" || c.RevokedAt != nil {
		return b, errKeyConflict
	}
	return b, nil
}
func apiKeyDeliveryRequireAcknowledged(tx *gorm.DB, kind, id string, birth time.Time, mode string) error {
	if mode == "manual" {
		return nil
	}
	if mode != "managed_vault" {
		return errKeyConflict
	}
	_, e := apiKeyDeliveryAcknowledged(tx, kind, id, birth)
	return e
}
func filterAPIKeyDeliveryRuntime(tx *gorm.DB, data *runtimeData) error {
	if data == nil {
		return apiKeyDeliveryUnavailable
	}
	keys := data.Keys[:0]
	for _, k := range data.Keys {
		if k.Status == entity.KeyActive && k.DeliveryMode != "manual" {
			if e := apiKeyDeliveryRequireAcknowledged(tx, "personal", k.ID, k.CreatedAt, k.DeliveryMode); e != nil {
				if errors.Is(e, gorm.ErrRecordNotFound) || e == errKeyConflict {
					continue
				}
				return e
			}
		}
		keys = append(keys, k)
	}
	data.Keys = keys
	if data.ProjectData != nil {
		keys := data.ProjectData.Keys[:0]
		for _, k := range data.ProjectData.Keys {
			if k.Status == entity.KeyActive && k.DeliveryMode != "manual" {
				if e := apiKeyDeliveryRequireAcknowledged(tx, "project", k.ID, k.CreatedAt, k.DeliveryMode); e != nil {
					if errors.Is(e, gorm.ErrRecordNotFound) || e == errKeyConflict {
						continue
					}
					return e
				}
			}
			keys = append(keys, k)
		}
		data.ProjectData.Keys = keys
	}
	return nil
}
func apiKeyDeliveryRuntimeMatches(auth *runtimeAuthorization, k entity.APIKey, project string, models []string, now time.Time) bool {
	if auth == nil || !now.Before(auth.ValidUntil) || k.Status != entity.KeyActive || k.DeliveryMode != "managed_vault" {
		return false
	}
	got, ok := auth.KeysByID[k.ID]
	if !ok || got.ProjectID != project || got.Key.ID != k.ID || got.Key.UserID != k.UserID || !got.Key.CreatedAt.Equal(k.CreatedAt) || got.Key.TokenHash != k.TokenHash || got.Key.Status != k.Status || got.Key.DeliveryMode != k.DeliveryMode || got.Key.LifecycleRevision != k.LifecycleRevision || !sameExpiry(got.Key.ExpiresAt, k.ExpiresAt) {
		return false
	}
	if k.ExpiresAt != nil && !now.Before(*k.ExpiresAt) {
		return false
	}
	a, b := slices.Clone(got.Models), slices.Clone(models)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}
func (s *Service) apiKeyDeliveryPublished(k entity.APIKey, project string, models []string) bool {
	if s.runtime == nil {
		return false
	}
	s.runtime.mu.Lock()
	defer s.runtime.mu.Unlock()
	if runtimeDenied(&s.runtime.deniedKeys, k.ID) || project != "" && runtimeDenied(&s.runtime.deniedProjects, project) || project == "" && (runtimeDenied(&s.runtime.deniedUsers, k.UserID) || runtimeDenied(&s.runtime.deniedPersonalGrants, k.UserID)) {
		return false
	}
	for _, m := range models {
		if runtimeDenied(&s.runtime.deniedModels, m) {
			return false
		}
	}
	return apiKeyDeliveryRuntimeMatches(s.runtime.auth.Load(), k, project, models, time.Now())
}
