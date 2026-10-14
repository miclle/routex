package service

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/secret"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const apiKeyDeliveryControllerMaxLifetime = 30 * 24 * time.Hour

func apiKeyDeliveryControllerBearerDigest(bearer string) (string, error) {
	if !strings.HasPrefix(bearer, "rxdctl_") || len(bearer) != 50 {
		return "", apperrors.ErrUnauthorized
	}
	raw, err := base64.RawURLEncoding.DecodeString(bearer[7:])
	if err != nil || len(raw) != 32 || base64.RawURLEncoding.EncodeToString(raw) != bearer[7:] {
		return "", apperrors.ErrUnauthorized
	}
	return secret.SHA256Hex("api-key-delivery:controller-bearer:v1\n" + bearer), nil
}

func apiKeyDeliveryControllerLive(g entity.APIKeyDeliveryControllerGrant, now time.Time) bool {
	return g.RevokedAt == nil && now.Before(g.ExpiresAt) && !now.Before(g.CreatedAt) && g.ExpiresAt.Sub(g.CreatedAt) <= apiKeyDeliveryControllerMaxLifetime
}

func apiKeyDeliveryControllerScope(g entity.APIKeyDeliveryControllerGrant, f apiKeyDeliveryControllerConfigurationFacts) bool {
	return g.OwnerKind == f.Owner.Kind && g.OwnerID == f.Owner.ID && g.OwnerBirth.Equal(f.Owner.Birth) && g.OwnerDigest == f.Owner.Digest && g.SlotID == f.View.SlotID && g.SlotBirth.Equal(f.SlotBirth) && g.ProfileID == f.ProfileID && g.ProfileBirth.Equal(f.ProfileBirth) && g.PrincipalID == f.PrincipalID && g.PrincipalBirth.Equal(f.PrincipalBirth)
}

func apiKeyDeliveryControllerAssigned(g entity.APIKeyDeliveryControllerGrant, f apiKeyDeliveryControllerConfigurationFacts) bool {
	return apiKeyDeliveryControllerScope(g, f) && strconv.FormatUint(g.ReviewedDescriptorRevision, 10) == f.View.DescriptorRevision && g.ConfigurationDigest == f.View.ConfigurationDigest && g.ReviewedBindingID == f.BindingID && g.ReviewedBindingBirth.Equal(f.BindingBirth) && g.ReviewedKeyID == f.View.KeyID && g.ReviewedKeyBirth.Equal(f.KeyBirth)
}

func apiKeyDeliveryControllerConfigurationReview(f apiKeyDeliveryControllerConfigurationFacts, a *Authentication) string {
	return apiKeyDeliveryDigest("api-key-delivery:controller-enrollment-review:v1", struct {
		Configuration            string
		Actor, Session           string
		ActorBirth, SessionBirth time.Time
	}{f.View.ConfigurationDigest, a.User.ID, a.Session.ID, a.User.CreatedAt.UTC(), a.Session.CreatedAt.UTC()})
}

func apiKeyDeliveryControllerReview(g entity.APIKeyDeliveryControllerGrant, v APIKeyDeliveryControllerView, a *Authentication) string {
	current := ""
	if v.CurrentConfiguration != nil {
		current = v.CurrentConfiguration.ConfigurationDigest
	}
	return apiKeyDeliveryDigest("api-key-delivery:controller-grant-review:v1", struct {
		ID, Revision, Status, Current, Actor, Session string
		Birth, ActorBirth, SessionBirth               time.Time
	}{g.ID, g.ReviewRevision, v.ConfigurationStatus, current, a.User.ID, a.Session.ID, g.CreatedAt.UTC(), a.User.CreatedAt.UTC(), a.Session.CreatedAt.UTC()})
}

func apiKeyDeliveryControllerGrant(tx *gorm.DB, grantID string) (entity.APIKeyDeliveryControllerGrant, error) {
	var g entity.APIKeyDeliveryControllerGrant
	if !apiKeyDeliveryControllerID.MatchString(grantID) || !strings.HasPrefix(grantID, "kcg_") {
		return g, apperrors.ErrBadRequest
	}
	if err := personalExact(vaultDB(tx), "id", grantID).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&g).Error; err != nil {
		return g, err
	}
	if g.ID != grantID || !apiKeyDeliveryBirth(g.CreatedAt) || !personalModelETag(g.ReviewRevision) || !personalModelETag(g.CredentialDigest) || !personalModelETag(g.ConfigurationDigest) || g.OwnerDigest != apiKeyDeliveryOwnerDigest(g.OwnerKind, g.OwnerID, g.OwnerBirth) || g.ExpiresAt.Sub(g.CreatedAt) <= 0 || g.ExpiresAt.Sub(g.CreatedAt) > apiKeyDeliveryControllerMaxLifetime {
		return g, apiKeyDeliveryUnavailable
	}
	return g, nil
}

func apiKeyDeliveryControllerAcknowledgementView(v entity.APIKeyDeliveryControllerAcknowledgement) *APIKeyDeliveryControllerAcknowledgementView {
	return &APIKeyDeliveryControllerAcknowledgementView{RequestID: v.RequestID, DescriptorRevision: strconv.FormatUint(v.DescriptorRevision, 10), ConfigurationDigest: v.ConfigurationDigest, AgentState: v.AgentState, OutputState: v.OutputState, RecordedAt: v.RecordedAt.UTC(), EvidenceKind: "controller_report"}
}

func (s *Service) apiKeyDeliveryControllerView(tx *gorm.DB, g entity.APIKeyDeliveryControllerGrant, a *Authentication) (APIKeyDeliveryControllerView, error) {
	v := APIKeyDeliveryControllerView{ID: g.ID, Name: g.Name, SlotID: g.SlotID, OwnerKind: g.OwnerKind, OwnerID: g.OwnerID, ExpiresAt: g.ExpiresAt, RevokedAt: g.RevokedAt, AssignedDescriptorRevision: strconv.FormatUint(g.ReviewedDescriptorRevision, 10), AssignedConfigurationDigest: g.ConfigurationDigest, ConfigurationStatus: "unavailable"}
	switch {
	case g.RevokedAt != nil:
		v.ConfigurationStatus = "revoked"
	case !apiKeyDeliveryControllerLive(g, time.Now().UTC()):
		v.ConfigurationStatus = "expired"
	default:
		f, err := s.apiKeyDeliveryControllerConfiguration(tx, g.SlotID)
		if err == nil && apiKeyDeliveryControllerScope(g, f) {
			v.CurrentConfiguration = &f.View
			v.ConfigurationStatus = "review_required"
			if apiKeyDeliveryControllerAssigned(g, f) {
				v.ConfigurationStatus = "assigned"
			}
		} else if err != nil {
			var app *apperrors.Error
			if !errors.As(err, &app) && !errors.Is(err, gorm.ErrRecordNotFound) {
				return v, err
			}
			// Current domain denial may hide a configuration, but never makes a
			// retained grant disappear or prevents its explicit revocation.
		}
	}
	var acknowledgements []entity.APIKeyDeliveryControllerAcknowledgement
	if err := personalExact(vaultDB(tx), "grant_id", g.ID).Order("recorded_at DESC").Order("id DESC").Limit(1).Find(&acknowledgements).Error; err != nil {
		return v, err
	}
	if len(acknowledgements) != 0 {
		ack := acknowledgements[0]
		if ack.GrantID != g.ID || !ack.GrantBirth.Equal(g.CreatedAt) || !credentialReplacementRequestID.MatchString(ack.RequestID) {
			return v, apiKeyDeliveryUnavailable
		}
		v.LastAcknowledgement = apiKeyDeliveryControllerAcknowledgementView(ack)
	}
	v.ReviewETag = apiKeyDeliveryControllerReview(g, v, a)
	return v, nil
}

func (s *Service) apiKeyDeliveryControllerAdmin(parent context.Context, a *Authentication, permission string, read bool, fn func(*gorm.DB, *Authentication, entity.User) error) error {
	a = apiKeyDeliveryCopyAuthentication(a)
	ctx, cancel, err := apiKeyDeliveryContext(parent, read)
	if err != nil {
		return err
	}
	defer cancel()
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		u, err := apiKeyDeliveryAdmin(tx, a, permission, false)
		if err != nil {
			return err
		}
		if err = fn(tx, a, u); err != nil {
			return err
		}
		return ctx.Err()
	})
	return apiKeyDeliveryError(err)
}

func (s *Service) ReviewAPIKeyDeliveryControllerConfiguration(parent context.Context, a *Authentication, slotID string) (*APIKeyDeliveryControllerReviewView, error) {
	if !apiKeyDeliveryControllerID.MatchString(slotID) || !strings.HasPrefix(slotID, "kds_") {
		return nil, apperrors.ErrBadRequest
	}
	var result APIKeyDeliveryControllerReviewView
	err := s.apiKeyDeliveryControllerAdmin(parent, a, "secrets.read", true, func(tx *gorm.DB, a *Authentication, _ entity.User) error {
		f, err := s.apiKeyDeliveryControllerConfiguration(tx, slotID)
		if err != nil {
			return err
		}
		result = APIKeyDeliveryControllerReviewView{Configuration: f.View, ReviewETag: apiKeyDeliveryControllerConfigurationReview(f, a)}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (s *Service) APIKeyDeliveryController(parent context.Context, a *Authentication, grantID string) (*APIKeyDeliveryControllerView, error) {
	var result APIKeyDeliveryControllerView
	err := s.apiKeyDeliveryControllerAdmin(parent, a, "secrets.read", true, func(tx *gorm.DB, a *Authentication, _ entity.User) error {
		g, err := apiKeyDeliveryControllerGrant(tx, grantID)
		if err != nil {
			return err
		}
		result, err = s.apiKeyDeliveryControllerView(tx, g, a)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func apiKeyDeliveryControllerAssignment(g *entity.APIKeyDeliveryControllerGrant, f apiKeyDeliveryControllerConfigurationFacts, u entity.User, reason string, now time.Time) error {
	revision, err := strconv.ParseUint(f.View.DescriptorRevision, 10, 64)
	if err != nil || revision == 0 || revision > uint64(1<<63-1) {
		return apiKeyDeliveryUnavailable
	}
	g.ReviewedDescriptorRevision, g.ReviewedBindingID, g.ReviewedBindingBirth = revision, f.BindingID, f.BindingBirth
	g.ReviewedKeyID, g.ReviewedKeyBirth, g.ConfigurationDigest = f.View.KeyID, f.KeyBirth, f.View.ConfigurationDigest
	g.ConfigurationReviewActorID, g.ConfigurationReviewBirth, g.ConfigurationReviewReason, g.ConfigurationReviewedAt = u.ID, u.CreatedAt, reason, now
	g.ReviewRevision, err = apiKeyDeliveryRevision()
	g.UpdatedAt = now
	return err
}

func (s *Service) EnrollAPIKeyDeliveryController(parent context.Context, a *Authentication, slotID, etag string, in APIKeyDeliveryControllerEnrollmentInput) (*APIKeyDeliveryControllerEnrollmentResult, error) {
	if !apiKeyDeliveryControllerID.MatchString(slotID) || !strings.HasPrefix(slotID, "kds_") || !personalModelETag(etag) || !credentialReplacementRequestID.MatchString(in.RequestID) || !apiKeyDeliveryText(in.Name, 100) || !validRegistrationReason(in.Reason) || len(in.Reason) > 1000 || !in.OptIn || !personalModelETag(in.ConfigurationDigest) || !apiKeyDeliveryBirth(in.ExpiresAt) {
		return nil, apperrors.ErrBadRequest
	}
	in.ExpiresAt = in.ExpiresAt.UTC()
	intent := apiKeyDeliveryDigest("api-key-delivery:controller-enrollment-intent:v1", in)
	var result APIKeyDeliveryControllerEnrollmentResult
	err := s.apiKeyDeliveryControllerAdmin(parent, a, "secrets.write", false, func(tx *gorm.DB, a *Authentication, u entity.User) error {
		var retained entity.APIKeyDeliveryControllerGrant
		err := personalExact(vaultDB(tx), "slot_id", slotID).Where("enrollment_request_id = ?", in.RequestID).Take(&retained).Error
		if err == nil {
			if retained.SlotID != slotID || retained.EnrollmentRequestID != in.RequestID || retained.EnrollmentIntentDigest != intent || retained.EnrollmentReview != etag || retained.CreatedBy != u.ID || !retained.CreatedByBirth.Equal(u.CreatedAt) {
				return apiKeyDeliveryConflict
			}
			result.Controller, err = s.apiKeyDeliveryControllerView(tx, retained, a)
			// A retained receipt never re-displays or reconstructs the bearer.
			return err
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		f, err := s.apiKeyDeliveryControllerConfiguration(tx, slotID)
		if err != nil {
			return err
		}
		if etag != apiKeyDeliveryControllerConfigurationReview(f, a) || in.DescriptorRevision != f.View.DescriptorRevision || in.ConfigurationDigest != f.View.ConfigurationDigest {
			return apiKeyDeliveryConflict
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		if in.ExpiresAt.Sub(now) < time.Hour || in.ExpiresAt.Sub(now) > apiKeyDeliveryControllerMaxLifetime {
			return apperrors.ErrBadRequest
		}
		grantID, err := newAPIKeyDeliveryID("kcg")
		if err != nil {
			return err
		}
		random, err := secret.RandomURLSafe(32)
		if err != nil {
			return apiKeyDeliveryUnavailable
		}
		bearer := "rxdctl_" + random
		digest, err := apiKeyDeliveryControllerBearerDigest(bearer)
		if err != nil {
			return apiKeyDeliveryUnavailable
		}
		g := entity.APIKeyDeliveryControllerGrant{ID: grantID, Name: in.Name, CredentialDigest: digest, EnrollmentRequestID: in.RequestID, EnrollmentIntentDigest: intent, EnrollmentReview: etag, OwnerKind: f.Owner.Kind, OwnerID: f.Owner.ID, OwnerBirth: f.Owner.Birth, OwnerDigest: f.Owner.Digest, SlotID: slotID, SlotBirth: f.SlotBirth, ProfileID: f.ProfileID, ProfileBirth: f.ProfileBirth, PrincipalID: f.PrincipalID, PrincipalBirth: f.PrincipalBirth, ExpiresAt: in.ExpiresAt, CreatedBy: u.ID, CreatedByBirth: u.CreatedAt, CreationReason: in.Reason, CreatedAt: now, AssignmentRequestID: in.RequestID, AssignmentIntentDigest: intent, AssignmentReview: etag}
		if err = apiKeyDeliveryControllerAssignment(&g, f, u, in.Reason, now); err != nil {
			return err
		}
		if err = vaultDB(tx).Create(&g).Error; err != nil {
			return err
		}
		if err = apiKeyDeliveryAudit(tx, u.ID, "api_key_delivery_controller_enrolled", grantID, in.RequestID, in.Reason); err != nil {
			return err
		}
		result.Controller, err = s.apiKeyDeliveryControllerView(tx, g, a)
		if err != nil {
			return err
		}
		result.Bearer, result.Created = bearer, true
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (s *Service) AssignAPIKeyDeliveryControllerConfiguration(parent context.Context, a *Authentication, grantID, etag string, in APIKeyDeliveryControllerAssignmentInput) (*APIKeyDeliveryControllerView, error) {
	if !personalModelETag(etag) || !credentialReplacementRequestID.MatchString(in.RequestID) || !in.OptIn || !validRegistrationReason(in.Reason) || len(in.Reason) > 1000 || !personalModelETag(in.ConfigurationDigest) {
		return nil, apperrors.ErrBadRequest
	}
	intent := apiKeyDeliveryDigest("api-key-delivery:controller-assignment-intent:v1", in)
	var result APIKeyDeliveryControllerView
	err := s.apiKeyDeliveryControllerAdmin(parent, a, "secrets.write", false, func(tx *gorm.DB, a *Authentication, u entity.User) error {
		g, err := apiKeyDeliveryControllerGrant(tx, grantID)
		if err != nil {
			return err
		}
		if !apiKeyDeliveryControllerLive(g, time.Now().UTC()) {
			return apiKeyDeliveryConflict
		}
		f, err := s.apiKeyDeliveryControllerConfiguration(tx, g.SlotID)
		if err != nil {
			return err
		}
		if !apiKeyDeliveryControllerScope(g, f) || in.DescriptorRevision != f.View.DescriptorRevision || in.ConfigurationDigest != f.View.ConfigurationDigest {
			return apiKeyDeliveryConflict
		}
		if g.AssignmentRequestID == in.RequestID {
			if g.AssignmentIntentDigest != intent || g.AssignmentReview != etag || g.ConfigurationReviewActorID != u.ID || !g.ConfigurationReviewBirth.Equal(u.CreatedAt) || !apiKeyDeliveryControllerAssigned(g, f) {
				return apiKeyDeliveryConflict
			}
			result, err = s.apiKeyDeliveryControllerView(tx, g, a)
			return err
		}
		v, err := s.apiKeyDeliveryControllerView(tx, g, a)
		if err != nil {
			return err
		}
		if v.ReviewETag != etag {
			return apiKeyDeliveryConflict
		}
		if err = apiKeyDeliveryControllerAssignment(&g, f, u, in.Reason, time.Now().UTC().Truncate(time.Microsecond)); err != nil {
			return err
		}
		g.AssignmentRequestID, g.AssignmentIntentDigest, g.AssignmentReview = in.RequestID, intent, etag
		if err = vaultDB(tx).Save(&g).Error; err != nil {
			return err
		}
		if err = apiKeyDeliveryAudit(tx, u.ID, "api_key_delivery_controller_assigned", g.ID, in.RequestID, in.Reason); err != nil {
			return err
		}
		result, err = s.apiKeyDeliveryControllerView(tx, g, a)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (s *Service) RevokeAPIKeyDeliveryController(parent context.Context, a *Authentication, grantID, etag, reason string) (*APIKeyDeliveryControllerView, error) {
	if !personalModelETag(etag) || !validRegistrationReason(reason) || len(reason) > 1000 {
		return nil, apperrors.ErrBadRequest
	}
	var result APIKeyDeliveryControllerView
	err := s.apiKeyDeliveryControllerAdmin(parent, a, "secrets.write", false, func(tx *gorm.DB, a *Authentication, u entity.User) error {
		g, err := apiKeyDeliveryControllerGrant(tx, grantID)
		if err != nil {
			return err
		}
		if g.RevokedAt != nil {
			if g.RevocationReview != etag || g.RevocationReason != reason || g.RevocationActorID != u.ID || g.RevocationActorBirth == nil || !g.RevocationActorBirth.Equal(u.CreatedAt) {
				return apiKeyDeliveryConflict
			}
			result, err = s.apiKeyDeliveryControllerView(tx, g, a)
			return err
		}
		v, err := s.apiKeyDeliveryControllerView(tx, g, a)
		if err != nil {
			return err
		}
		if v.ReviewETag != etag {
			return apiKeyDeliveryConflict
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		g.RevokedAt, g.RevocationReason, g.RevocationReview, g.UpdatedAt = &now, reason, etag, now
		g.RevocationActorID, g.RevocationActorBirth = u.ID, &u.CreatedAt
		g.ReviewRevision, err = apiKeyDeliveryRevision()
		if err != nil {
			return err
		}
		if err = vaultDB(tx).Save(&g).Error; err != nil {
			return err
		}
		if err = apiKeyDeliveryAudit(tx, u.ID, "api_key_delivery_controller_revoked", g.ID, "", reason); err != nil {
			return err
		}
		result, err = s.apiKeyDeliveryControllerView(tx, g, a)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (s *Service) apiKeyDeliveryControllerMachine(parent context.Context, bearer string, read bool, fn func(*gorm.DB, entity.APIKeyDeliveryControllerGrant, apiKeyDeliveryControllerConfigurationFacts) error) error {
	digest, err := apiKeyDeliveryControllerBearerDigest(bearer)
	if err != nil {
		return err
	}
	ctx, cancel, err := apiKeyDeliveryContext(parent, read)
	if err != nil {
		return err
	}
	defer cancel()
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		var g entity.APIKeyDeliveryControllerGrant
		if err := personalExact(vaultDB(tx), "credential_digest", digest).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&g).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return apperrors.ErrUnauthorized
			}
			return err
		}
		if subtle.ConstantTimeCompare([]byte(g.CredentialDigest), []byte(digest)) != 1 || !apiKeyDeliveryControllerLive(g, time.Now().UTC()) {
			return apperrors.ErrUnauthorized
		}
		validated, err := apiKeyDeliveryControllerGrant(tx, g.ID)
		if err != nil {
			return err
		}
		f, err := s.apiKeyDeliveryControllerConfiguration(tx, validated.SlotID)
		if err != nil {
			return err
		}
		if !apiKeyDeliveryControllerAssigned(validated, f) {
			return apiKeyDeliveryConflict
		}
		if err = fn(tx, validated, f); err != nil {
			return err
		}
		// A report admitted before expiry must still be within its bounded
		// lifetime at commit; an expired completion rolls back the write.
		if !apiKeyDeliveryControllerLive(validated, time.Now().UTC()) {
			return apperrors.ErrUnauthorized
		}
		return ctx.Err()
	})
	return apiKeyDeliveryError(err)
}

func (s *Service) PullAPIKeyDeliveryControllerConfiguration(parent context.Context, bearer string) (*APIKeyDeliveryControllerConfiguration, error) {
	var result APIKeyDeliveryControllerConfiguration
	err := s.apiKeyDeliveryControllerMachine(parent, bearer, true, func(_ *gorm.DB, _ entity.APIKeyDeliveryControllerGrant, f apiKeyDeliveryControllerConfigurationFacts) error {
		result = f.View
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func apiKeyDeliveryControllerAcknowledgementValid(in APIKeyDeliveryControllerAcknowledgementInput) bool {
	revision, err := strconv.ParseUint(in.DescriptorRevision, 10, 64)
	return err == nil && revision > 0 && revision <= uint64(1<<63-1) && strconv.FormatUint(revision, 10) == in.DescriptorRevision && credentialReplacementRequestID.MatchString(in.RequestID) && personalModelETag(in.ConfigurationDigest) && (in.AgentState == "unknown" || in.AgentState == "running" || in.AgentState == "failed") && (in.OutputState == "unknown" || in.OutputState == "written" || in.OutputState == "failed")
}

func (s *Service) AcknowledgeAPIKeyDeliveryControllerConfiguration(parent context.Context, bearer string, in APIKeyDeliveryControllerAcknowledgementInput) (*APIKeyDeliveryControllerAcknowledgementView, error) {
	if !apiKeyDeliveryControllerAcknowledgementValid(in) {
		return nil, apperrors.ErrBadRequest
	}
	intent := apiKeyDeliveryDigest("api-key-delivery:controller-ack-intent:v1", in)
	var result *APIKeyDeliveryControllerAcknowledgementView
	err := s.apiKeyDeliveryControllerMachine(parent, bearer, false, func(tx *gorm.DB, g entity.APIKeyDeliveryControllerGrant, f apiKeyDeliveryControllerConfigurationFacts) error {
		if in.DescriptorRevision != f.View.DescriptorRevision || in.ConfigurationDigest != f.View.ConfigurationDigest {
			return apiKeyDeliveryConflict
		}
		var retained entity.APIKeyDeliveryControllerAcknowledgement
		err := personalExact(vaultDB(tx), "grant_id", g.ID).Where("request_id = ?", in.RequestID).Take(&retained).Error
		if err == nil {
			if retained.GrantID != g.ID || !retained.GrantBirth.Equal(g.CreatedAt) || retained.RequestID != in.RequestID || retained.IntentDigest != intent || retained.BindingID != f.BindingID || !retained.BindingBirth.Equal(f.BindingBirth) || retained.KeyID != f.View.KeyID || !retained.KeyBirth.Equal(f.KeyBirth) || retained.ConfigurationDigest != f.View.ConfigurationDigest || retained.DescriptorRevision != g.ReviewedDescriptorRevision || retained.AgentState != in.AgentState || retained.OutputState != in.OutputState {
				return apiKeyDeliveryConflict
			}
			result = apiKeyDeliveryControllerAcknowledgementView(retained)
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var count int64
		if err = personalExact(vaultDB(tx), "grant_id", g.ID).Model(&entity.APIKeyDeliveryControllerAcknowledgement{}).Count(&count).Error; err != nil {
			return err
		}
		if count >= 1000 {
			return apiKeyDeliveryConflict
		}
		ackID, err := newAPIKeyDeliveryID("kca")
		if err != nil {
			return err
		}
		ack := entity.APIKeyDeliveryControllerAcknowledgement{ID: ackID, GrantID: g.ID, GrantBirth: g.CreatedAt, RequestID: in.RequestID, IntentDigest: intent, DescriptorRevision: g.ReviewedDescriptorRevision, ConfigurationDigest: f.View.ConfigurationDigest, BindingID: f.BindingID, BindingBirth: f.BindingBirth, KeyID: f.View.KeyID, KeyBirth: f.KeyBirth, AgentState: in.AgentState, OutputState: in.OutputState, RecordedAt: time.Now().UTC().Truncate(time.Microsecond)}
		if err = vaultDB(tx).Create(&ack).Error; err != nil {
			return err
		}
		result = apiKeyDeliveryControllerAcknowledgementView(ack)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
