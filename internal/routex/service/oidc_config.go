package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/secret"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var oidcUnavailable = &apperrors.Error{Code: 503, Message: "identity authentication temporarily unavailable"}

type OIDCPublic struct {
	Available bool   `json:"available"`
	Name      string `json:"name"`
}
type OIDCProviderView struct {
	Name             string `json:"name"`
	Issuer           string `json:"issuer"`
	ClientID         string `json:"client_id"`
	CallbackURL      string `json:"callback_url"`
	SecretConfigured bool   `json:"secret_configured"`
	Enabled          bool   `json:"enabled"`
	Verified         bool   `json:"verified"`
	ReviewETag       string `json:"review_etag"`
	MFARequired      bool   `json:"mfa_required"`
}
type OIDCProviderInput struct {
	Name         string `json:"name"`
	Issuer       string `json:"issuer"`
	ClientID     string `json:"client_id"`
	CallbackURL  string `json:"callback_url"`
	SecretAction string `json:"secret_action"`
	ClientSecret string `json:"client_secret"`
	Reason       string `json:"reason"`
}
type OIDCStatusInput struct {
	Enabled bool   `json:"enabled"`
	Reason  string `json:"reason"`
}

func (in *OIDCProviderInput) UnmarshalJSON(raw []byte) error {
	f, err := registrationStrictObject(raw, []string{"name", "issuer", "client_id", "callback_url", "secret_action", "client_secret", "reason"})
	if err != nil {
		return err
	}
	var v OIDCProviderInput
	for k, d := range map[string]*string{"name": &v.Name, "issuer": &v.Issuer, "client_id": &v.ClientID, "callback_url": &v.CallbackURL, "secret_action": &v.SecretAction, "client_secret": &v.ClientSecret, "reason": &v.Reason} {
		if json.Unmarshal(f[k], d) != nil {
			return apperrors.ErrBadRequest
		}
	}
	if err = oidcValidateConfigInput(&v); err != nil {
		return err
	}
	*in = v
	return nil
}
func (in *OIDCStatusInput) UnmarshalJSON(raw []byte) error {
	f, err := registrationStrictObject(raw, []string{"enabled", "reason"})
	if err != nil {
		return err
	}
	var v OIDCStatusInput
	if json.Unmarshal(f["enabled"], &v.Enabled) != nil || json.Unmarshal(f["reason"], &v.Reason) != nil || !validRegistrationReason(v.Reason) {
		return apperrors.ErrBadRequest
	}
	*in = v
	return nil
}
func oidcURL(raw string, callback bool) bool {
	if raw == "" || len(raw) > 2048 || !utf8.ValidString(raw) || strings.ContainsFunc(raw, unicode.IsSpace) || strings.ContainsFunc(raw, unicode.IsControl) {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.Opaque == "" && u.User == nil && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && u.RawFragment == "" && (!callback || u.Path == "/api/v1/auth/oidc/callback" && u.RawPath == "")
}
func oidcValidateConfigInput(v *OIDCProviderInput) error {
	v.Name = strings.TrimSpace(v.Name)
	if !utf8.ValidString(v.Name) || utf8.RuneCountInString(v.Name) < 1 || utf8.RuneCountInString(v.Name) > 100 || strings.ContainsFunc(v.Name, unicode.IsControl) || !oidcURL(v.Issuer, false) || !oidcURL(v.CallbackURL, true) || len(v.ClientID) < 1 || len(v.ClientID) > 256 || !utf8.ValidString(v.ClientID) || strings.ContainsFunc(v.ClientID, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) || !validRegistrationReason(v.Reason) {
		return apperrors.ErrBadRequest
	}
	if v.SecretAction == "keep" {
		if v.ClientSecret != "" {
			return apperrors.ErrBadRequest
		}
	} else if v.SecretAction != "replace" || len(v.ClientSecret) < 1 || len(v.ClientSecret) > 4096 || !utf8.ValidString(v.ClientSecret) {
		return apperrors.ErrBadRequest
	}
	return nil
}
func oidcError(err error) error {
	if err == nil {
		return nil
	}
	var e *apperrors.Error
	if errors.As(err, &e) && (e.Code == 400 || e.Code == 401 || e.Code == 403 || e.Code == 409) {
		return e
	}
	return oidcUnavailable
}
func oidcProvider(tx *gorm.DB) (entity.OIDCProvider, error) {
	var p entity.OIDCProvider
	err := tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "id"}, "oidc")).Take(&p).Error
	if err == nil && (p.ID != "oidc" || p.CreatedAt.IsZero() || !validMemberRoleDigest(p.ReviewRevision) || !validMemberRoleDigest(p.ConfigRevision) || !validMemberRoleDigest(p.PolicyRevision)) {
		err = oidcUnavailable
	}
	return p, err
}
func oidcDigest(v ...any) string { raw, _ := json.Marshal(v); return secret.SHA256Hex(string(raw)) }
func oidcReview(p entity.OIDCProvider, u entity.User, m entity.UserMFA) string {
	return oidcDigest("oidc.config.review.v1", u.ID, u.CreatedAt.UTC(), p.ID, p.CreatedAt.UTC(), p.ReviewRevision, p.ConfigRevision, p.PolicyRevision, p.Name, p.Enabled, p.VerifiedConfigRevision, p.VerifiedBy, p.VerifiedBindingID, m.Enabled, m.Generation)
}
func oidcVerified(p entity.OIDCProvider) bool {
	return p.ConfigRevision != "" && p.VerifiedConfigRevision == p.ConfigRevision && p.VerifiedBy != "" && p.VerifiedUserCreatedAt != nil && !p.VerifiedUserCreatedAt.IsZero() && p.VerifiedBindingID != "" && p.VerifiedBindingCreatedAt != nil && !p.VerifiedBindingCreatedAt.IsZero()
}
func oidcConfigView(p entity.OIDCProvider, u entity.User, m entity.UserMFA) *OIDCProviderView {
	return &OIDCProviderView{Name: p.Name, Issuer: p.Issuer, ClientID: p.ClientID, CallbackURL: p.CallbackURL, SecretConfigured: p.AuthCiphertext != "", Enabled: p.Enabled, Verified: oidcVerified(p), ReviewETag: oidcReview(p, u, m), MFARequired: m.Enabled}
}
func oidcAdmin(tx *gorm.DB, id string) (entity.User, entity.UserMFA, error) {
	if err := registrationPolicyAuthority(tx, id); err != nil {
		return entity.User{}, entity.UserMFA{}, err
	}
	u, err := registrationAdmittedUser(tx, id, false)
	if err != nil {
		return u, entity.UserMFA{}, err
	}
	m, err := mfaState(tx, u.ID)
	return u, m, err
}
func (s *Service) PublicOIDC(ctx context.Context) (*OIDCPublic, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	p, err := oidcProvider(s.authDB(ctx))
	if err != nil {
		return nil, oidcError(err)
	}
	v := &OIDCPublic{Available: p.Enabled && oidcVerified(p)}
	if v.Available {
		v.Name = p.Name
	}
	return v, nil
}
func (s *Service) GetOIDCProvider(ctx context.Context, actorID string) (*OIDCProviderView, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var result *OIDCProviderView
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		u, m, e := oidcAdmin(tx, actorID)
		if e != nil {
			return e
		}
		p, e := oidcProvider(tx)
		if e != nil {
			return e
		}
		result = oidcConfigView(p, u, m)
		return ctx.Err()
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, oidcError(err)
	}
	return result, nil
}
func oidcNewRevision() (string, error) {
	v, e := secret.RandomURLSafe(32)
	if e != nil {
		return "", e
	}
	return secret.SHA256Hex(v), nil
}
func (s *Service) oidcRevoke(tx *gorm.DB, bindingID string) error {
	q := tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "primary_method"}, "oidc"))
	if bindingID != "" {
		q = q.Where(database.ExactText(tx, clause.Column{Name: "oidc_binding_id"}, bindingID))
	}
	if e := q.Delete(&entity.Session{}).Error; e != nil {
		return e
	}
	q = tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "primary_method"}, "oidc"))
	if bindingID != "" {
		q = q.Where(database.ExactText(tx, clause.Column{Name: "oidc_binding_id"}, bindingID))
	}
	if e := q.Delete(&entity.MFAChallenge{}).Error; e != nil {
		return e
	}
	q = tx.Session(&gorm.Session{NewDB: true}).Model(&entity.OIDCCeremony{})
	if bindingID != "" {
		q = q.Where(database.ExactText(tx, clause.Column{Name: "binding_id"}, bindingID))
	}
	return q.Where("status IN ?", []string{"pending", "exchanging", "verified"}).Update("status", "failed").Error
}
func (s *Service) oidcPublishPolicy(ctx context.Context, revision string) {
	if revision != "" {
		s.invalidateRuntimeOIDCPolicy(revision)
		s.publishSessionMutation(ctx)
	}
}
func (s *Service) SaveOIDCProvider(ctx context.Context, actorID, etag string, in OIDCProviderInput) (*OIDCProviderView, error) {
	if !validMemberRoleDigest(etag) || oidcValidateConfigInput(&in) != nil {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var ciphertext, generation string
	epoch := s.secretEpoch()
	var err error
	if in.SecretAction == "replace" {
		generation, err = oidcNewRevision()
		if err != nil {
			return nil, oidcUnavailable
		}
		ciphertext, err = s.sealSecret(rootReference("oidc_providers", "oidc", generation), in.ClientSecret)
		if err != nil {
			return nil, oidcUnavailable
		}
	}
	var result *OIDCProviderView
	var oldPolicy string
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		u, m, e := oidcAdmin(tx, actorID)
		if e != nil {
			return e
		}
		p, e := oidcProvider(tx)
		if e != nil {
			return e
		}
		if oidcReview(p, u, m) != etag {
			return catalogConflict
		}
		if in.SecretAction == "keep" && p.AuthCiphertext == "" {
			return apperrors.ErrBadRequest
		}
		security := p.Issuer != in.Issuer || p.ClientID != in.ClientID || p.CallbackURL != in.CallbackURL || in.SecretAction == "replace"
		changed := security || p.Name != in.Name
		if !changed {
			result = oidcConfigView(p, u, m)
			return ctx.Err()
		}
		p.Name = in.Name
		p.Issuer = in.Issuer
		p.ClientID = in.ClientID
		p.CallbackURL = in.CallbackURL
		p.ReviewRevision, e = oidcNewRevision()
		if e != nil {
			return e
		}
		if security {
			oldPolicy = p.PolicyRevision
			p.ConfigRevision, e = oidcNewRevision()
			if e != nil {
				return e
			}
			p.PolicyRevision, e = oidcNewRevision()
			if e != nil {
				return e
			}
			p.Enabled = false
			p.VerifiedConfigRevision = ""
			p.VerifiedBy = ""
			p.VerifiedUserCreatedAt = nil
			p.VerifiedBindingID = ""
			p.VerifiedBindingCreatedAt = nil
			e = s.oidcRevoke(tx, "")
			if e != nil {
				return e
			}
			if e = tx.Session(&gorm.Session{NewDB: true}).Where("1 = ?", 1).Delete(&entity.OIDCBinding{}).Error; e != nil {
				return e
			}
		}
		if ciphertext != "" {
			if e = s.guardSecretWrite(tx, epoch, rootReference("oidc_providers", "oidc", generation), ciphertext); e != nil {
				return e
			}
			p.AuthCiphertext = ciphertext
			p.SecretGeneration = generation
		}
		if e = tx.Save(&p).Error; e != nil {
			return e
		}
		if e = oidcAudit(tx, u, "identity.oidc.config.update", p, nil, in.Reason); e != nil {
			return e
		}
		result = oidcConfigView(p, u, m)
		return ctx.Err()
	})
	if err != nil {
		return nil, oidcError(err)
	}
	s.oidcPublishPolicy(ctx, oldPolicy)
	return result, nil
}
func (s *Service) SetOIDCEnabled(ctx context.Context, actorID, etag string, in OIDCStatusInput) (*OIDCProviderView, error) {
	if !validMemberRoleDigest(etag) || !validRegistrationReason(in.Reason) {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var result *OIDCProviderView
	var oldPolicy string
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		u, m, e := oidcAdmin(tx, actorID)
		if e != nil {
			return e
		}
		p, e := oidcProvider(tx)
		if e != nil {
			return e
		}
		if oidcReview(p, u, m) != etag {
			return catalogConflict
		}
		if in.Enabled {
			if !oidcVerified(p) {
				return catalogConflict
			}
			verifier, e := registrationAdmittedUser(tx, p.VerifiedBy, false)
			if e != nil {
				return e
			}
			if verifier.Role != entity.RoleAdmin || !verifier.CreatedAt.Equal(*p.VerifiedUserCreatedAt) {
				return catalogConflict
			}
			b, e := oidcBindingByID(tx, p.VerifiedBindingID)
			if e != nil {
				return e
			}
			if !b.CreatedAt.Equal(*p.VerifiedBindingCreatedAt) || b.UserID != verifier.ID || !b.UserCreatedAt.Equal(verifier.CreatedAt) || b.ConfigRevision != p.ConfigRevision || b.SubjectDigest != oidcSubjectDigest(p.Issuer, b.Subject) {
				return catalogConflict
			}
		}
		if p.Enabled != in.Enabled {
			oldPolicy = p.PolicyRevision
			p.Enabled = in.Enabled
			p.PolicyRevision, e = oidcNewRevision()
			if e != nil {
				return e
			}
			p.ReviewRevision, e = oidcNewRevision()
			if e != nil {
				return e
			}
			e = s.oidcRevoke(tx, "")
			if e != nil {
				return e
			}
			if e = tx.Save(&p).Error; e != nil {
				return e
			}
			if e = oidcAudit(tx, u, "identity.oidc.status.update", p, nil, in.Reason); e != nil {
				return e
			}
		}
		result = oidcConfigView(p, u, m)
		return ctx.Err()
	})
	if err != nil {
		return nil, oidcError(err)
	}
	s.oidcPublishPolicy(ctx, oldPolicy)
	return result, nil
}

// OIDC audit facts are intentionally independent of remote identity claims.
type oidcAuditDetails struct {
	Version           int        `json:"version"`
	Reason            string     `json:"reason"`
	ActorCreatedAt    time.Time  `json:"actor_created_at"`
	ProviderID        string     `json:"provider_id"`
	ProviderCreatedAt time.Time  `json:"provider_created_at"`
	ReviewRevision    string     `json:"review_revision"`
	ConfigRevision    string     `json:"config_revision"`
	PolicyRevision    string     `json:"policy_revision"`
	BindingID         string     `json:"binding_id,omitempty"`
	BindingCreatedAt  *time.Time `json:"binding_created_at,omitempty"`
}

func oidcAuditJSON(u entity.User, p entity.OIDCProvider, b *entity.OIDCBinding, reason string) (string, error) {
	if !validRegistrationReason(reason) || u.ID == "" || u.CreatedAt.IsZero() || p.ID != "oidc" || p.CreatedAt.IsZero() || !validMemberRoleDigest(p.ReviewRevision) || !validMemberRoleDigest(p.ConfigRevision) || !validMemberRoleDigest(p.PolicyRevision) {
		return "", apperrors.ErrBadRequest
	}
	v := oidcAuditDetails{Version: 1, Reason: strings.TrimSpace(reason), ActorCreatedAt: u.CreatedAt.UTC(), ProviderID: p.ID, ProviderCreatedAt: p.CreatedAt.UTC(), ReviewRevision: p.ReviewRevision, ConfigRevision: p.ConfigRevision, PolicyRevision: p.PolicyRevision}
	if b != nil {
		if b.ID == "" || b.CreatedAt.IsZero() || b.UserID != u.ID || !b.UserCreatedAt.Equal(u.CreatedAt) || b.ConfigRevision != p.ConfigRevision {
			return "", apperrors.ErrBadRequest
		}
		birth := b.CreatedAt.UTC()
		v.BindingID = b.ID
		v.BindingCreatedAt = &birth
	}
	raw, e := json.Marshal(v)
	return string(raw), e
}
func oidcAudit(tx *gorm.DB, u entity.User, action string, p entity.OIDCProvider, b *entity.OIDCBinding, reason string) error {
	resourceType, resourceID := "oidc_provider", p.ID
	switch action {
	case "identity.oidc.config.update", "identity.oidc.status.update", "identity.oidc.verify":
	case "account.oidc.bind", "account.oidc.unlink":
		if b == nil {
			return apperrors.ErrBadRequest
		}
		resourceType, resourceID = "oidc_binding", b.ID
	default:
		return apperrors.ErrBadRequest
	}
	raw, e := oidcAuditJSON(u, p, b, reason)
	if e != nil {
		return e
	}
	aid, e := id.NewPrefixed("aud")
	if e != nil {
		return e
	}
	return tx.Create(&entity.AuditEvent{ID: aid, ActorID: u.ID, Action: action, ResourceType: resourceType, ResourceID: resourceID, DetailsJSON: &raw}).Error
}
