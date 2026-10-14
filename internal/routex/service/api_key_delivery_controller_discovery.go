package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type apiKeyDeliveryDiscoveryCursor struct {
	Kind       string    `json:"kind"`
	Parent     string    `json:"parent"`
	Birth      time.Time `json:"birth"`
	After      string    `json:"after"`
	AfterBirth time.Time `json:"after_birth"`
}

func apiKeyDeliveryDiscoveryIdentity(value, prefix string) bool {
	return apiKeyDeliveryControllerID.MatchString(value) && strings.HasPrefix(value, prefix+"_")
}

func apiKeyDeliveryDiscoveryEncode(kind, parent string, birth time.Time, after string, afterBirth time.Time) string {
	raw, _ := json.Marshal(apiKeyDeliveryDiscoveryCursor{kind, parent, birth.UTC(), after, afterBirth.UTC()})
	return base64.RawURLEncoding.EncodeToString(raw)
}

func apiKeyDeliveryDiscoveryAfter(cursor, kind, parent string, birth time.Time, prefix string) (apiKeyDeliveryDiscoveryCursor, error) {
	if cursor == "" {
		return apiKeyDeliveryDiscoveryCursor{}, nil
	}
	if len(cursor) > 512 {
		return apiKeyDeliveryDiscoveryCursor{}, apperrors.ErrBadRequest
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	var v apiKeyDeliveryDiscoveryCursor
	if err != nil || json.Unmarshal(raw, &v) != nil || v.Kind != kind || v.Parent != parent || !v.Birth.Equal(birth) || !apiKeyDeliveryDiscoveryIdentity(v.After, prefix) || !apiKeyDeliveryBirth(v.AfterBirth) || apiKeyDeliveryDiscoveryEncode(v.Kind, v.Parent, v.Birth, v.After, v.AfterBirth) != cursor {
		return apiKeyDeliveryDiscoveryCursor{}, apperrors.ErrBadRequest
	}
	return v, nil
}

func apiKeyDeliveryDiscoverySlotValid(slot entity.APIKeyDeliverySlot) bool {
	if !apiKeyDeliveryDiscoveryIdentity(slot.ID, "kds") || !apiKeyDeliveryDiscoveryIdentity(slot.ProfileID, "vap") || !apiKeyDeliveryBirth(slot.CreatedAt) || !apiKeyDeliveryBirth(slot.ProfileBirth) || !apiKeyDeliveryBirth(slot.OwnerBirth) || !rootSafeIdentity(slot.OwnerID, 30) || !rootSafeIdentity(slot.ChainID, 30) {
		return false
	}
	if slot.OwnerKind != "personal" && slot.OwnerKind != "project" || slot.DescriptorRevision == 0 || slot.DescriptorRevision > uint64(1<<63-1) || slot.OwnerDigest != apiKeyDeliveryOwnerDigest(slot.OwnerKind, slot.OwnerID, slot.OwnerBirth) {
		return false
	}
	return slot.SlotDigest == apiKeyDeliveryDigest("api-key-delivery:slot:v1", struct{ Owner, Profile, Chain string }{slot.OwnerDigest, slot.ProfileID, slot.ChainID})
}

func apiKeyDeliveryDiscoverySlot(tx *gorm.DB, slotID string) (entity.APIKeyDeliverySlot, error) {
	var slot entity.APIKeyDeliverySlot
	if !apiKeyDeliveryDiscoveryIdentity(slotID, "kds") {
		return slot, apperrors.ErrBadRequest
	}
	if err := personalExact(vaultDB(tx), "id", slotID).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&slot).Error; err != nil {
		return slot, err
	}
	if slot.ID != slotID || !apiKeyDeliveryDiscoverySlotValid(slot) {
		return slot, apiKeyDeliveryUnavailable
	}
	return slot, nil
}

func apiKeyDeliveryDiscoveryGrantValid(g entity.APIKeyDeliveryControllerGrant, slot entity.APIKeyDeliverySlot) bool {
	if !apiKeyDeliveryDiscoveryIdentity(g.ID, "kcg") || !apiKeyDeliveryBirth(g.CreatedAt) || !apiKeyDeliveryDiscoveryIdentity(g.PrincipalID, "vpb") || !apiKeyDeliveryBirth(g.PrincipalBirth) || !personalModelETag(g.ReviewRevision) || !personalModelETag(g.CredentialDigest) || !personalModelETag(g.ConfigurationDigest) || !apiKeyDeliveryText(g.Name, 100) {
		return false
	}
	if g.ExpiresAt.Sub(g.CreatedAt) <= 0 || g.ExpiresAt.Sub(g.CreatedAt) > apiKeyDeliveryControllerMaxLifetime {
		return false
	}
	return g.SlotID == slot.ID && g.SlotBirth.Equal(slot.CreatedAt) && g.ProfileID == slot.ProfileID && g.ProfileBirth.Equal(slot.ProfileBirth) && g.OwnerKind == slot.OwnerKind && g.OwnerID == slot.OwnerID && g.OwnerBirth.Equal(slot.OwnerBirth) && g.OwnerDigest == slot.OwnerDigest
}

func apiKeyDeliveryDiscoveryController(g entity.APIKeyDeliveryControllerGrant, now time.Time) APIKeyDeliveryControllerSummary {
	status := "active"
	if g.RevokedAt != nil {
		status = "revoked"
	} else if !apiKeyDeliveryControllerLive(g, now) {
		status = "expired"
	}
	return APIKeyDeliveryControllerSummary{ID: g.ID, Birth: g.CreatedAt, Name: g.Name, SlotID: g.SlotID, ExpiresAt: g.ExpiresAt, RevokedAt: g.RevokedAt, LifecycleStatus: status, ConfigurationStatus: "not_reviewed"}
}

func (s *Service) ListAPIKeyDeliverySlots(parent context.Context, a *Authentication, targetID, cursor string, limit int) (*APIKeyDeliverySlotList, error) {
	if !apiKeyDeliveryDiscoveryIdentity(targetID, "vap") || limit < 1 || limit > 50 {
		return nil, apperrors.ErrBadRequest
	}
	result := APIKeyDeliverySlotList{Items: []APIKeyDeliverySlotSummary{}}
	err := s.apiKeyDeliveryControllerAdmin(parent, a, "secrets.read", true, func(tx *gorm.DB, _ *Authentication, _ entity.User) error {
		profile, _, err := apiKeyDeliveryProfile(tx, targetID)
		if err != nil {
			return err
		}
		page, err := apiKeyDeliveryDiscoveryAfter(cursor, "slots", targetID, profile.CreatedAt, "kds")
		if err != nil {
			return err
		}
		query := personalExact(vaultDB(tx), "profile_id", targetID).Where("profile_birth = ?", profile.CreatedAt)
		if page.After != "" {
			var anchor entity.APIKeyDeliverySlot
			if err = personalExact(query.Session(&gorm.Session{}), "id", page.After).Take(&anchor).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return apperrors.ErrBadRequest
				}
				return err
			}
			if !anchor.CreatedAt.Equal(page.AfterBirth) || anchor.ID != page.After || !apiKeyDeliveryDiscoverySlotValid(anchor) || anchor.ProfileID != profile.ID || !anchor.ProfileBirth.Equal(profile.CreatedAt) {
				return apperrors.ErrBadRequest
			}
			query = query.Where(database.ByteAfter(tx, clause.Column{Name: "id"}, page.After))
		}
		var slots []entity.APIKeyDeliverySlot
		if err = query.Clauses(clause.OrderBy{Expression: database.ByteOrder(tx, clause.Column{Name: "id"})}).Limit(limit + 1).Find(&slots).Error; err != nil {
			return err
		}
		for i, slot := range slots {
			if !apiKeyDeliveryDiscoverySlotValid(slot) || slot.ProfileID != profile.ID || !slot.ProfileBirth.Equal(profile.CreatedAt) || slot.OwnerKind != profile.OwnerKind || slot.OwnerID != profile.OwnerID || !slot.OwnerBirth.Equal(profile.OwnerBirth) || slot.OwnerDigest != profile.OwnerDigest {
				return apiKeyDeliveryUnavailable
			}
			if i == limit {
				result.NextCursor = apiKeyDeliveryDiscoveryEncode("slots", targetID, profile.CreatedAt, result.Items[len(result.Items)-1].ID, result.Items[len(result.Items)-1].Birth)
				break
			}
			result.Items = append(result.Items, APIKeyDeliverySlotSummary{ID: slot.ID, Birth: slot.CreatedAt, ProfileID: slot.ProfileID, OwnerKind: slot.OwnerKind, OwnerID: slot.OwnerID, DescriptorRevision: strconv.FormatUint(slot.DescriptorRevision, 10), ConfigurationStatus: "not_reviewed"})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (s *Service) ListAPIKeyDeliveryControllers(parent context.Context, a *Authentication, slotID, cursor string, limit int) (*APIKeyDeliveryControllerList, error) {
	if !apiKeyDeliveryDiscoveryIdentity(slotID, "kds") || limit < 1 || limit > 50 {
		return nil, apperrors.ErrBadRequest
	}
	result := APIKeyDeliveryControllerList{Items: []APIKeyDeliveryControllerSummary{}}
	err := s.apiKeyDeliveryControllerAdmin(parent, a, "secrets.read", true, func(tx *gorm.DB, _ *Authentication, _ entity.User) error {
		slot, err := apiKeyDeliveryDiscoverySlot(tx, slotID)
		if err != nil {
			return err
		}
		page, err := apiKeyDeliveryDiscoveryAfter(cursor, "controllers", slotID, slot.CreatedAt, "kcg")
		if err != nil {
			return err
		}
		query := personalExact(vaultDB(tx), "slot_id", slotID).Where("slot_birth = ?", slot.CreatedAt)
		if page.After != "" {
			var anchor entity.APIKeyDeliveryControllerGrant
			if err = personalExact(query.Session(&gorm.Session{}), "id", page.After).Take(&anchor).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return apperrors.ErrBadRequest
				}
				return err
			}
			if !anchor.CreatedAt.Equal(page.AfterBirth) || anchor.ID != page.After || !apiKeyDeliveryDiscoveryGrantValid(anchor, slot) {
				return apperrors.ErrBadRequest
			}
			query = query.Where(database.ByteAfter(tx, clause.Column{Name: "id"}, page.After))
		}
		var grants []entity.APIKeyDeliveryControllerGrant
		if err = query.Clauses(clause.OrderBy{Expression: database.ByteOrder(tx, clause.Column{Name: "id"})}).Limit(limit + 1).Find(&grants).Error; err != nil {
			return err
		}
		now := time.Now().UTC()
		for i, g := range grants {
			if !apiKeyDeliveryDiscoveryGrantValid(g, slot) {
				return apiKeyDeliveryUnavailable
			}
			if i == limit {
				result.NextCursor = apiKeyDeliveryDiscoveryEncode("controllers", slotID, slot.CreatedAt, result.Items[len(result.Items)-1].ID, result.Items[len(result.Items)-1].Birth)
				break
			}
			result.Items = append(result.Items, apiKeyDeliveryDiscoveryController(g, now))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (s *Service) APIKeyDeliveryControllerEnrollmentReceipt(parent context.Context, a *Authentication, slotID, requestID string) (*APIKeyDeliveryControllerEnrollmentReceipt, error) {
	if !apiKeyDeliveryDiscoveryIdentity(slotID, "kds") || !credentialReplacementRequestID.MatchString(requestID) {
		return nil, apperrors.ErrBadRequest
	}
	var result APIKeyDeliveryControllerEnrollmentReceipt
	err := s.apiKeyDeliveryControllerAdmin(parent, a, "secrets.read", true, func(tx *gorm.DB, _ *Authentication, u entity.User) error {
		slot, err := apiKeyDeliveryDiscoverySlot(tx, slotID)
		if err != nil {
			return err
		}
		var g entity.APIKeyDeliveryControllerGrant
		if err = personalExact(personalExact(vaultDB(tx), "slot_id", slotID), "enrollment_request_id", requestID).Take(&g).Error; err != nil {
			return err
		}
		// Other administrators may discover the grant, but cannot reopen another
		// actor's original enrollment receipt or borrow a recreated actor identity.
		if g.CreatedBy != u.ID || !g.CreatedByBirth.Equal(u.CreatedAt) {
			return gorm.ErrRecordNotFound
		}
		if g.EnrollmentRequestID != requestID || !apiKeyDeliveryDiscoveryGrantValid(g, slot) || !personalModelETag(g.EnrollmentIntentDigest) || !personalModelETag(g.EnrollmentReview) {
			return apiKeyDeliveryUnavailable
		}
		result = APIKeyDeliveryControllerEnrollmentReceipt{RequestID: requestID, IntentDigest: g.EnrollmentIntentDigest, EnrollmentReview: g.EnrollmentReview, Controller: apiKeyDeliveryDiscoveryController(g, time.Now().UTC()), SecretStatus: "not_recoverable"}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}
