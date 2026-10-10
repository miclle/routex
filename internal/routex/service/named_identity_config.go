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

var namedIdentityUnavailable = &apperrors.Error{Code: 503, Message: "identity authentication temporarily unavailable"}

type GitHubPublic struct {
	Available bool   `json:"available"`
	Name      string `json:"name"`
}
type GitHubProviderView struct {
	Name             string `json:"name"`
	ClientID         string `json:"client_id"`
	CallbackURL      string `json:"callback_url"`
	SecretConfigured bool   `json:"secret_configured"`
	Enabled          bool   `json:"enabled"`
	Verified         bool   `json:"verified"`
	ReviewETag       string `json:"review_etag"`
	MFARequired      bool   `json:"mfa_required"`
}
type GitHubProviderInput struct {
	Name         string `json:"name"`
	ClientID     string `json:"client_id"`
	CallbackURL  string `json:"callback_url"`
	SecretAction string `json:"secret_action"`
	ClientSecret string `json:"client_secret"`
	Reason       string `json:"reason"`
}
type GitHubStatusInput struct {
	Enabled bool   `json:"enabled"`
	Reason  string `json:"reason"`
}

func (in *GitHubProviderInput) UnmarshalJSON(raw []byte) error {
	f, err := registrationStrictObject(raw, []string{"name", "client_id", "callback_url", "secret_action", "client_secret", "reason"})
	if err != nil {
		return err
	}
	var v GitHubProviderInput
	for k, d := range map[string]*string{"name": &v.Name, "client_id": &v.ClientID, "callback_url": &v.CallbackURL, "secret_action": &v.SecretAction, "client_secret": &v.ClientSecret, "reason": &v.Reason} {
		if json.Unmarshal(f[k], d) != nil {
			return apperrors.ErrBadRequest
		}
	}
	if namedIdentityValidateConfigInput(&v) != nil {
		return apperrors.ErrBadRequest
	}
	*in = v
	return nil
}
func (in *GitHubStatusInput) UnmarshalJSON(raw []byte) error {
	f, err := registrationStrictObject(raw, []string{"enabled", "reason"})
	if err != nil {
		return err
	}
	var v GitHubStatusInput
	if json.Unmarshal(f["enabled"], &v.Enabled) != nil || json.Unmarshal(f["reason"], &v.Reason) != nil || !validRegistrationReason(v.Reason) {
		return apperrors.ErrBadRequest
	}
	*in = v
	return nil
}
func namedIdentityURLFor(providerID, raw string, callback bool) bool {
	d, ok := namedIdentityDescriptor(providerID)
	if !ok {
		return false
	}
	if raw == "" || len(raw) > 2048 || !utf8.ValidString(raw) || strings.Contains(raw, "\\") || strings.ContainsFunc(raw, unicode.IsSpace) || strings.ContainsFunc(raw, unicode.IsControl) {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.Opaque == "" && u.User == nil && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && u.RawFragment == "" && (!callback || u.Path == d.callbackPath && u.RawPath == "")
}
func namedIdentityText(v string, max int) bool {
	if v == "" || len(v) > max || !utf8.ValidString(v) {
		return false
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func namedIdentityValidateConfigInput(v *GitHubProviderInput) error {
	return namedIdentityValidateConfigInputFor(githubProviderID, v)
}
func namedIdentityValidateConfigInputFor(providerID string, v *GitHubProviderInput) error {
	v.Name = strings.TrimSpace(v.Name)
	if !utf8.ValidString(v.Name) || utf8.RuneCountInString(v.Name) < 1 || utf8.RuneCountInString(v.Name) > 100 || strings.ContainsFunc(v.Name, unicode.IsControl) || !namedIdentityURLFor(providerID, v.CallbackURL, true) || !namedIdentityText(v.ClientID, 256) || strings.ContainsFunc(v.ClientID, unicode.IsSpace) || strings.ContainsFunc(v.ClientID, unicode.IsControl) || !validRegistrationReason(v.Reason) {
		return apperrors.ErrBadRequest
	}
	if v.SecretAction == "keep" {
		if v.ClientSecret != "" {
			return apperrors.ErrBadRequest
		}
	} else if v.SecretAction != "replace" || !namedIdentityText(v.ClientSecret, 4096) {
		return apperrors.ErrBadRequest
	}
	return nil
}

func namedIdentityStoredConfig(p entity.NamedIdentityProvider) bool {
	if !namedIdentityProfile(p.ID, p.ProfileID, p.IdentityIssuer) {
		return false
	}
	if p.AuthCiphertext == "" {
		return p.Name == "" && p.ClientID == "" && p.CallbackURL == "" && !p.Enabled && !namedIdentityVerified(p)
	}
	in := GitHubProviderInput{Name: p.Name, ClientID: p.ClientID, CallbackURL: p.CallbackURL, SecretAction: "keep", Reason: "Validate stored configuration"}
	return namedIdentityValidateConfigInputFor(p.ID, &in) == nil
}
func namedIdentityError(err error) error {
	if err == nil {
		return nil
	}
	var e *apperrors.Error
	if errors.As(err, &e) && (e.Code == 400 || e.Code == 401 || e.Code == 403 || e.Code == 409) {
		return e
	}
	return namedIdentityUnavailable
}
func namedIdentityProvider(tx *gorm.DB, providerID string) (entity.NamedIdentityProvider, error) {
	var p entity.NamedIdentityProvider
	err := tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "id"}, providerID)).Take(&p).Error
	if err == nil && (p.ID != providerID || !namedIdentityProfile(p.ID, p.ProfileID, p.IdentityIssuer) || p.CreatedAt.IsZero() || !validMemberRoleDigest(p.ReviewRevision) || !validMemberRoleDigest(p.ConfigRevision) || !validMemberRoleDigest(p.PolicyRevision)) {
		err = namedIdentityUnavailable
	}
	if err == nil {
		ok := namedIdentityStoredConfig(p)
		if !ok {
			err = namedIdentityUnavailable
		}
	}
	return p, err
}
func namedIdentityDigest(v ...any) string {
	raw, _ := json.Marshal(v)
	return secret.SHA256Hex(string(raw))
}
func namedIdentityReview(p entity.NamedIdentityProvider, u entity.User, m entity.UserMFA) string {
	return namedIdentityDigest("named-identity.config.review.v1", u.ID, u.CreatedAt.UTC(), p.ID, p.ProfileID, p.IdentityIssuer, p.CreatedAt.UTC(), p.ReviewRevision, p.ConfigRevision, p.PolicyRevision, p.Name, p.Enabled, p.VerifiedConfigRevision, p.VerifiedBy, p.VerifiedBindingID, m.Enabled, m.Generation)
}
func namedIdentityVerified(p entity.NamedIdentityProvider) bool {
	return namedIdentityProfile(p.ID, p.ProfileID, p.IdentityIssuer) && p.ConfigRevision != "" && p.VerifiedConfigRevision == p.ConfigRevision && p.VerifiedBy != "" && p.VerifiedUserCreatedAt != nil && !p.VerifiedUserCreatedAt.IsZero() && p.VerifiedBindingID != "" && p.VerifiedBindingCreatedAt != nil && !p.VerifiedBindingCreatedAt.IsZero()
}
func namedIdentityConfigView(p entity.NamedIdentityProvider, u entity.User, m entity.UserMFA) *GitHubProviderView {
	return &GitHubProviderView{Name: p.Name, ClientID: p.ClientID, CallbackURL: p.CallbackURL, SecretConfigured: p.AuthCiphertext != "", Enabled: p.Enabled, Verified: namedIdentityVerified(p), ReviewETag: namedIdentityReview(p, u, m), MFARequired: m.Enabled}
}
func namedIdentityAdmin(tx *gorm.DB, id string) (entity.User, entity.UserMFA, error) {
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
func (s *Service) publicNamedIdentity(ctx context.Context, providerID string) (*GitHubPublic, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	p, err := namedIdentityProvider(s.authDB(ctx), providerID)
	if err != nil {
		return nil, namedIdentityError(err)
	}
	v := &GitHubPublic{Available: p.Enabled && namedIdentityVerified(p)}
	if v.Available {
		v.Name = p.Name
	}
	return v, nil
}
func (s *Service) getNamedIdentityProvider(ctx context.Context, actorID, providerID string) (*GitHubProviderView, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var result *GitHubProviderView
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		u, m, e := namedIdentityAdmin(tx, actorID)
		if e != nil {
			return e
		}
		p, e := namedIdentityProvider(tx, providerID)
		if e != nil {
			return e
		}
		result = namedIdentityConfigView(p, u, m)
		return ctx.Err()
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, namedIdentityError(err)
	}
	return result, nil
}
func namedIdentityNewRevision() (string, error) {
	v, e := secret.RandomURLSafe(32)
	if e != nil {
		return "", e
	}
	return secret.SHA256Hex(v), nil
}
func (s *Service) namedIdentityRevoke(tx *gorm.DB, providerID, bindingID string) error {
	q := tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "primary_method"}, providerID))
	if bindingID != "" {
		q = q.Where(database.ExactText(tx, clause.Column{Name: "named_identity_binding_id"}, bindingID))
	}
	if e := q.Delete(&entity.Session{}).Error; e != nil {
		return e
	}
	q = tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "primary_method"}, providerID))
	if bindingID != "" {
		q = q.Where(database.ExactText(tx, clause.Column{Name: "named_identity_binding_id"}, bindingID))
	}
	if e := q.Delete(&entity.MFAChallenge{}).Error; e != nil {
		return e
	}
	q = tx.Session(&gorm.Session{NewDB: true}).Model(&entity.NamedIdentityCeremony{}).Where(database.ExactText(tx, clause.Column{Name: "provider_id"}, providerID)).Where(database.ExactText(tx, clause.Column{Name: "profile_id"}, namedIdentityProfileID(providerID)))
	if bindingID != "" {
		q = q.Where(database.ExactText(tx, clause.Column{Name: "binding_id"}, bindingID))
	}
	return q.Where("status IN ?", []string{"pending", "exchanging", "verified"}).Update("status", "failed").Error
}
func (s *Service) namedIdentityPublishPolicy(ctx context.Context, providerID, revision string) {
	if revision != "" {
		s.invalidateRuntimeNamedIdentityPolicy(providerID, revision)
		s.publishSessionMutation(ctx)
	}
}
func (s *Service) saveNamedIdentityProvider(ctx context.Context, actorID, etag, providerID string, in GitHubProviderInput) (*GitHubProviderView, error) {
	if !validMemberRoleDigest(etag) || namedIdentityValidateConfigInputFor(providerID, &in) != nil {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var ciphertext, generation string
	epoch := s.secretEpoch()
	var err error
	if in.SecretAction == "replace" {
		generation, err = namedIdentityNewRevision()
		if err != nil {
			return nil, namedIdentityUnavailable
		}
		ciphertext, err = s.sealSecret(rootReference("named_identity_providers", providerID, generation), in.ClientSecret)
		if err != nil {
			return nil, namedIdentityUnavailable
		}
	}
	var result *GitHubProviderView
	var oldPolicy string
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		u, m, e := namedIdentityAdmin(tx, actorID)
		if e != nil {
			return e
		}
		p, e := namedIdentityProvider(tx, providerID)
		if e != nil {
			return e
		}
		if namedIdentityReview(p, u, m) != etag {
			return catalogConflict
		}
		if in.SecretAction == "keep" && p.AuthCiphertext == "" {
			return apperrors.ErrBadRequest
		}
		security := p.ClientID != in.ClientID || p.CallbackURL != in.CallbackURL || in.SecretAction == "replace"
		changed := security || p.Name != in.Name
		if !changed {
			result = namedIdentityConfigView(p, u, m)
			return ctx.Err()
		}
		p.Name = in.Name
		p.ClientID = in.ClientID
		p.CallbackURL = in.CallbackURL
		p.ReviewRevision, e = namedIdentityNewRevision()
		if e != nil {
			return e
		}
		if security {
			oldPolicy = p.PolicyRevision
			p.ConfigRevision, e = namedIdentityNewRevision()
			if e != nil {
				return e
			}
			p.PolicyRevision, e = namedIdentityNewRevision()
			if e != nil {
				return e
			}
			p.Enabled = false
			p.VerifiedConfigRevision = ""
			p.VerifiedBy = ""
			p.VerifiedUserCreatedAt = nil
			p.VerifiedBindingID = ""
			p.VerifiedBindingCreatedAt = nil
			e = s.namedIdentityRevoke(tx, providerID, "")
			if e != nil {
				return e
			}
			if e = tx.Session(&gorm.Session{NewDB: true}).Where("1 = ?", 1).Where("provider_id = ? AND profile_id = ?", providerID, namedIdentityProfileID(providerID)).Delete(&entity.NamedIdentityBinding{}).Error; e != nil {
				return e
			}
		}
		if ciphertext != "" {
			if e = s.guardSecretWrite(tx, epoch, rootReference("named_identity_providers", providerID, generation), ciphertext); e != nil {
				return e
			}
			p.AuthCiphertext = ciphertext
			p.SecretGeneration = generation
		}
		if e = tx.Save(&p).Error; e != nil {
			return e
		}
		if e = namedIdentityAudit(tx, u, "identity."+p.ID+".config.update", p, nil, in.Reason); e != nil {
			return e
		}
		result = namedIdentityConfigView(p, u, m)
		return ctx.Err()
	})
	if err != nil {
		return nil, namedIdentityError(err)
	}
	s.namedIdentityPublishPolicy(ctx, providerID, oldPolicy)
	return result, nil
}
func (s *Service) setNamedIdentityEnabled(ctx context.Context, actorID, etag, providerID string, in GitHubStatusInput) (*GitHubProviderView, error) {
	if !validMemberRoleDigest(etag) || !validRegistrationReason(in.Reason) {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var result *GitHubProviderView
	var oldPolicy string
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		u, m, e := namedIdentityAdmin(tx, actorID)
		if e != nil {
			return e
		}
		p, e := namedIdentityProvider(tx, providerID)
		if e != nil {
			return e
		}
		if namedIdentityReview(p, u, m) != etag {
			return catalogConflict
		}
		if in.Enabled {
			if !namedIdentityVerified(p) {
				return catalogConflict
			}
			verifier, e := registrationAdmittedUser(tx, p.VerifiedBy, false)
			if e != nil {
				return e
			}
			if verifier.Role != entity.RoleAdmin || !verifier.CreatedAt.Equal(*p.VerifiedUserCreatedAt) {
				return catalogConflict
			}
			b, e := namedIdentityBindingByID(tx, p.VerifiedBindingID)
			if e != nil {
				return e
			}
			if !b.CreatedAt.Equal(*p.VerifiedBindingCreatedAt) || b.UserID != verifier.ID || !b.UserCreatedAt.Equal(verifier.CreatedAt) || b.ProviderID != p.ID || !namedIdentityProfileSubject(p.ID, b.SubjectKind, b.Subject) || b.ConfigRevision != p.ConfigRevision || b.SubjectDigest != namedIdentitySubjectDigest(p.ID, b.SubjectKind, b.Subject) {
				return catalogConflict
			}
		}
		if p.Enabled != in.Enabled {
			oldPolicy = p.PolicyRevision
			p.Enabled = in.Enabled
			p.PolicyRevision, e = namedIdentityNewRevision()
			if e != nil {
				return e
			}
			p.ReviewRevision, e = namedIdentityNewRevision()
			if e != nil {
				return e
			}
			e = s.namedIdentityRevoke(tx, providerID, "")
			if e != nil {
				return e
			}
			if e = tx.Save(&p).Error; e != nil {
				return e
			}
			if e = namedIdentityAudit(tx, u, "identity."+p.ID+".status.update", p, nil, in.Reason); e != nil {
				return e
			}
		}
		result = namedIdentityConfigView(p, u, m)
		return ctx.Err()
	})
	if err != nil {
		return nil, namedIdentityError(err)
	}
	s.namedIdentityPublishPolicy(ctx, providerID, oldPolicy)
	return result, nil
}

// GitHub audit facts are intentionally independent of remote identity claims.
type namedIdentityAuditDetails struct {
	ProfileID         string     `json:"profile_id"`
	IdentityIssuer    string     `json:"identity_issuer"`
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

func namedIdentityAuditJSON(u entity.User, p entity.NamedIdentityProvider, b *entity.NamedIdentityBinding, reason string) (string, error) {
	if !validRegistrationReason(reason) || u.ID == "" || u.CreatedAt.IsZero() || !namedIdentityProfile(p.ID, p.ProfileID, p.IdentityIssuer) || p.CreatedAt.IsZero() || !validMemberRoleDigest(p.ReviewRevision) || !validMemberRoleDigest(p.ConfigRevision) || !validMemberRoleDigest(p.PolicyRevision) {
		return "", apperrors.ErrBadRequest
	}
	v := namedIdentityAuditDetails{Version: 1, ProfileID: p.ProfileID, IdentityIssuer: p.IdentityIssuer, Reason: strings.TrimSpace(reason), ActorCreatedAt: u.CreatedAt.UTC(), ProviderID: p.ID, ProviderCreatedAt: p.CreatedAt.UTC(), ReviewRevision: p.ReviewRevision, ConfigRevision: p.ConfigRevision, PolicyRevision: p.PolicyRevision}
	if b != nil {
		if b.ID == "" || b.CreatedAt.IsZero() || b.UserID != u.ID || !b.UserCreatedAt.Equal(u.CreatedAt) || b.ProviderID != p.ID || !namedIdentityProfile(b.ProviderID, b.ProfileID, b.IdentityIssuer) || b.ConfigRevision != p.ConfigRevision {
			return "", apperrors.ErrBadRequest
		}
		birth := b.CreatedAt.UTC()
		v.BindingID = b.ID
		v.BindingCreatedAt = &birth
	}
	raw, e := json.Marshal(v)
	return string(raw), e
}
func namedIdentityAudit(tx *gorm.DB, u entity.User, action string, p entity.NamedIdentityProvider, b *entity.NamedIdentityBinding, reason string) error {
	resourceType, resourceID := "named_identity_provider", p.ID
	switch action {
	case "identity." + p.ID + ".config.update", "identity." + p.ID + ".status.update", "identity." + p.ID + ".verify":
	case "account." + p.ID + ".bind", "account." + p.ID + ".unlink":
		if b == nil {
			return apperrors.ErrBadRequest
		}
		resourceType, resourceID = "named_identity_binding", b.ID
	default:
		return apperrors.ErrBadRequest
	}

	raw, e := namedIdentityAuditJSON(u, p, b, reason)
	if e != nil {
		return e
	}
	aid, e := id.NewPrefixed("aud")
	if e != nil {
		return e
	}
	return tx.Create(&entity.AuditEvent{ID: aid, ActorID: u.ID, Action: action, ResourceType: resourceType, ResourceID: resourceID, DetailsJSON: &raw}).Error
}
