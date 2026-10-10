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
	"github.com/miclle/routex/pkg/upstream"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ldapUnavailable = &apperrors.Error{Code: 503, Message: "identity authentication temporarily unavailable"}

type LDAPPublic struct {
	Available bool   `json:"available"`
	Name      string `json:"name"`
}
type LDAPProviderView struct {
	Name              string `json:"name"`
	Endpoint          string `json:"endpoint"`
	BindDN            string `json:"bind_dn"`
	BaseDN            string `json:"base_dn"`
	UserFilter        string `json:"user_filter"`
	IdentityAttribute string `json:"identity_attribute"`
	SecretConfigured  bool   `json:"secret_configured"`
	Enabled           bool   `json:"enabled"`
	Verified          bool   `json:"verified"`
	ReviewETag        string `json:"review_etag"`
	MFARequired       bool   `json:"mfa_required"`
}
type LDAPProviderInput struct {
	Name              string `json:"name"`
	Endpoint          string `json:"endpoint"`
	BindDN            string `json:"bind_dn"`
	BaseDN            string `json:"base_dn"`
	UserFilter        string `json:"user_filter"`
	IdentityAttribute string `json:"identity_attribute"`
	SecretAction      string `json:"secret_action"`
	BindPassword      string `json:"bind_password"`
	Reason            string `json:"reason"`
}
type LDAPStatusInput struct {
	Enabled bool   `json:"enabled"`
	Reason  string `json:"reason"`
}

func (in *LDAPProviderInput) UnmarshalJSON(raw []byte) error {
	f, err := registrationStrictObject(raw, []string{"name", "endpoint", "bind_dn", "base_dn", "user_filter", "identity_attribute", "secret_action", "bind_password", "reason"})
	if err != nil {
		return err
	}
	var v LDAPProviderInput
	for k, d := range map[string]*string{"name": &v.Name, "endpoint": &v.Endpoint, "bind_dn": &v.BindDN, "base_dn": &v.BaseDN, "user_filter": &v.UserFilter, "identity_attribute": &v.IdentityAttribute, "secret_action": &v.SecretAction, "bind_password": &v.BindPassword, "reason": &v.Reason} {
		if json.Unmarshal(f[k], d) != nil {
			return apperrors.ErrBadRequest
		}
	}
	if ldapValidateConfigInput(&v) != nil {
		return apperrors.ErrBadRequest
	}
	*in = v
	return nil
}
func (in *LDAPStatusInput) UnmarshalJSON(raw []byte) error {
	f, err := registrationStrictObject(raw, []string{"enabled", "reason"})
	if err != nil {
		return err
	}
	var v LDAPStatusInput
	if json.Unmarshal(f["enabled"], &v.Enabled) != nil || json.Unmarshal(f["reason"], &v.Reason) != nil || !validRegistrationReason(v.Reason) {
		return apperrors.ErrBadRequest
	}
	*in = v
	return nil
}
func ldapValidateConfigInput(v *LDAPProviderInput) error {
	v.Name = strings.TrimSpace(v.Name)
	if !utf8.ValidString(v.Name) || utf8.RuneCountInString(v.Name) < 1 || utf8.RuneCountInString(v.Name) > 100 || strings.ContainsFunc(v.Name, unicode.IsControl) || !validRegistrationReason(v.Reason) {
		return apperrors.ErrBadRequest
	}
	password := "configuration-validation-only"
	if v.SecretAction == "keep" {
		if v.BindPassword != "" {
			return apperrors.ErrBadRequest
		}
	} else if v.SecretAction != "replace" {
		return apperrors.ErrBadRequest
	} else {
		if !ldapPassword(v.BindPassword) {
			return apperrors.ErrBadRequest
		}
		password = v.BindPassword
	}
	u, parseErr := url.Parse(v.Endpoint)
	if parseErr != nil || upstream.ValidateLDAPEndpoint(u, true) != nil {
		return apperrors.ErrBadRequest
	}
	_, err := ldapComponent(v.Endpoint, v.BindDN, password, v.BaseDN, v.UserFilter, v.IdentityAttribute, false)
	if err != nil {
		return apperrors.ErrBadRequest
	}
	return nil
}
func ldapStoredConfig(p entity.LDAPProvider) bool {
	if p.AuthCiphertext == "" {
		return p.ConfigRevision == strings.Repeat("0", 64) && p.SecretGeneration == "0" && !p.Enabled && p.Endpoint == "" && p.BindDN == "" && p.BaseDN == "" && p.UserFilter == "" && p.IdentityAttribute == ""
	}
	in := LDAPProviderInput{Name: p.Name, Endpoint: p.Endpoint, BindDN: p.BindDN, BaseDN: p.BaseDN, UserFilter: p.UserFilter, IdentityAttribute: p.IdentityAttribute, SecretAction: "keep", Reason: "Validate stored configuration"}
	return ldapValidateConfigInput(&in) == nil && validMemberRoleDigest(p.SecretGeneration)
}
func ldapError(err error) error {
	if err == nil {
		return nil
	}
	var e *apperrors.Error
	if errors.As(err, &e) && (e.Code == 400 || e.Code == 401 || e.Code == 403 || e.Code == 409) {
		return e
	}
	return ldapUnavailable
}
func ldapProvider(tx *gorm.DB) (entity.LDAPProvider, error) {
	var p entity.LDAPProvider
	err := tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "id"}, "ldap")).Take(&p).Error
	if err == nil && (p.ID != "ldap" || p.CreatedAt.IsZero() || !validMemberRoleDigest(p.ReviewRevision) || !validMemberRoleDigest(p.ConfigRevision) || !validMemberRoleDigest(p.PolicyRevision)) {
		err = ldapUnavailable
	}
	if err == nil {
		ok := ldapStoredConfig(p)
		if !ok {
			err = ldapUnavailable
		}
	}
	return p, err
}
func ldapDigest(v ...any) string { raw, _ := json.Marshal(v); return secret.SHA256Hex(string(raw)) }
func ldapReview(p entity.LDAPProvider, u entity.User, m entity.UserMFA) string {
	return ldapDigest("ldap.config.review.v1", u.ID, u.CreatedAt.UTC(), p.ID, p.CreatedAt.UTC(), p.ReviewRevision, p.ConfigRevision, p.PolicyRevision, p.Name, p.Enabled, p.VerifiedConfigRevision, p.VerifiedBy, p.VerifiedUserCreatedAt, p.VerifiedBindingID, p.VerifiedBindingCreatedAt, m.Enabled, m.Generation)
}
func ldapVerified(p entity.LDAPProvider) bool {
	return p.ConfigRevision != "" && p.VerifiedConfigRevision == p.ConfigRevision && p.VerifiedBy != "" && p.VerifiedUserCreatedAt != nil && !p.VerifiedUserCreatedAt.IsZero() && p.VerifiedBindingID != "" && p.VerifiedBindingCreatedAt != nil && !p.VerifiedBindingCreatedAt.IsZero()
}
func ldapConfigView(p entity.LDAPProvider, u entity.User, m entity.UserMFA) *LDAPProviderView {
	return &LDAPProviderView{Name: p.Name, Endpoint: p.Endpoint, BindDN: p.BindDN, BaseDN: p.BaseDN, UserFilter: p.UserFilter, IdentityAttribute: p.IdentityAttribute, SecretConfigured: p.AuthCiphertext != "", Enabled: p.Enabled, Verified: ldapVerified(p), ReviewETag: ldapReview(p, u, m), MFARequired: m.Enabled}
}
func ldapAdmin(tx *gorm.DB, id string) (entity.User, entity.UserMFA, error) {
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
func (s *Service) PublicLDAP(ctx context.Context) (*LDAPPublic, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	p, err := ldapProvider(s.authDB(ctx))
	if err != nil {
		return nil, ldapError(err)
	}
	v := &LDAPPublic{Available: p.Enabled && ldapVerified(p)}
	if v.Available {
		v.Name = p.Name
	}
	return v, nil
}
func (s *Service) GetLDAPProvider(ctx context.Context, actorID string) (*LDAPProviderView, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var result *LDAPProviderView
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		u, m, e := ldapAdmin(tx, actorID)
		if e != nil {
			return e
		}
		p, e := ldapProvider(tx)
		if e != nil {
			return e
		}
		result = ldapConfigView(p, u, m)
		return ctx.Err()
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, ldapError(err)
	}
	return result, nil
}
func ldapNewRevision() (string, error) {
	v, e := secret.RandomURLSafe(32)
	if e != nil {
		return "", e
	}
	return secret.SHA256Hex(v), nil
}
func (s *Service) ldapRevoke(tx *gorm.DB, bindingID string) error {
	q := tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "primary_method"}, "ldap"))
	if bindingID != "" {
		q = q.Where(database.ExactText(tx, clause.Column{Name: "ldap_binding_id"}, bindingID))
	}
	if e := q.Delete(&entity.Session{}).Error; e != nil {
		return e
	}
	q = tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "primary_method"}, "ldap"))
	if bindingID != "" {
		q = q.Where(database.ExactText(tx, clause.Column{Name: "ldap_binding_id"}, bindingID))
	}
	if e := q.Delete(&entity.MFAChallenge{}).Error; e != nil {
		return e
	}
	return nil
}
func (s *Service) ldapPublishPolicy(ctx context.Context, revision string) {
	if revision != "" {
		s.invalidateRuntimeLDAPPolicy(revision)
		s.publishSessionMutation(ctx)
	}
}
func (s *Service) SaveLDAPProvider(ctx context.Context, actorID, etag string, in LDAPProviderInput) (*LDAPProviderView, error) {
	if !validMemberRoleDigest(etag) || ldapValidateConfigInput(&in) != nil {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var ciphertext, generation string
	epoch := s.secretEpoch()
	var err error
	if in.SecretAction == "replace" {
		generation, err = ldapNewRevision()
		if err != nil {
			return nil, ldapUnavailable
		}
		ciphertext, err = s.sealSecret(rootReference("ldap_providers", "ldap", generation), in.BindPassword)
		if err != nil {
			return nil, ldapUnavailable
		}
	}
	var result *LDAPProviderView
	var oldPolicy string
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		u, m, e := ldapAdmin(tx, actorID)
		if e != nil {
			return e
		}
		p, e := ldapProvider(tx)
		if e != nil {
			return e
		}
		if ldapReview(p, u, m) != etag {
			return catalogConflict
		}
		if in.SecretAction == "keep" && p.AuthCiphertext == "" {
			return apperrors.ErrBadRequest
		}
		security := p.Endpoint != in.Endpoint || p.BindDN != in.BindDN || p.BaseDN != in.BaseDN || p.UserFilter != in.UserFilter || p.IdentityAttribute != in.IdentityAttribute || in.SecretAction == "replace"
		if !security && p.Name == in.Name {
			result = ldapConfigView(p, u, m)
			return ctx.Err()
		}
		p.Name = in.Name
		p.Endpoint = in.Endpoint
		p.BindDN = in.BindDN
		p.BaseDN = in.BaseDN
		p.UserFilter = in.UserFilter
		p.IdentityAttribute = in.IdentityAttribute
		p.ReviewRevision, e = ldapNewRevision()
		if e != nil {
			return e
		}
		if security {
			oldPolicy = p.PolicyRevision
			p.ConfigRevision, e = ldapNewRevision()
			if e != nil {
				return e
			}
			p.PolicyRevision, e = ldapNewRevision()
			if e != nil {
				return e
			}
			p.Enabled = false
			p.VerifiedConfigRevision = ""
			p.VerifiedBy = ""
			p.VerifiedUserCreatedAt = nil
			p.VerifiedBindingID = ""
			p.VerifiedBindingCreatedAt = nil
			e = s.ldapRevoke(tx, "")
			if e != nil {
				return e
			}
			if e = tx.Session(&gorm.Session{NewDB: true}).Where("1 = ?", 1).Delete(&entity.LDAPBinding{}).Error; e != nil {
				return e
			}
		}
		if ciphertext != "" {
			if e = s.guardSecretWrite(tx, epoch, rootReference("ldap_providers", "ldap", generation), ciphertext); e != nil {
				return e
			}
			p.AuthCiphertext = ciphertext
			p.SecretGeneration = generation
		}
		if e = tx.Save(&p).Error; e != nil {
			return e
		}
		if e = ldapAudit(tx, u, "identity.ldap.config.update", p, nil, in.Reason); e != nil {
			return e
		}
		result = ldapConfigView(p, u, m)
		return ctx.Err()
	})
	if err != nil {
		return nil, ldapError(err)
	}
	s.ldapPublishPolicy(ctx, oldPolicy)
	return result, nil
}
func (s *Service) SetLDAPEnabled(ctx context.Context, actorID, etag string, in LDAPStatusInput) (*LDAPProviderView, error) {
	if !validMemberRoleDigest(etag) || !validRegistrationReason(in.Reason) {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var result *LDAPProviderView
	var oldPolicy string
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		u, m, e := ldapAdmin(tx, actorID)
		if e != nil {
			return e
		}
		p, e := ldapProvider(tx)
		if e != nil {
			return e
		}
		if ldapReview(p, u, m) != etag {
			return catalogConflict
		}
		if in.Enabled {
			if !ldapVerified(p) {
				return catalogConflict
			}
			verifier, e := registrationAdmittedUser(tx, p.VerifiedBy, false)
			if e != nil {
				return e
			}
			if verifier.Role != entity.RoleAdmin || !verifier.CreatedAt.Equal(*p.VerifiedUserCreatedAt) {
				return catalogConflict
			}
			b, e := ldapBindingByID(tx, p.VerifiedBindingID)
			if e != nil {
				return e
			}
			if !b.CreatedAt.Equal(*p.VerifiedBindingCreatedAt) || b.UserID != verifier.ID || !b.UserCreatedAt.Equal(verifier.CreatedAt) || b.ProviderID != p.ID || b.IdentityAttribute != p.IdentityAttribute || !ldapStoredSubject(b) || b.ConfigRevision != p.ConfigRevision || b.SubjectDigest != ldapSubjectDigest(b.IdentityAttribute, b.Subject) {
				return catalogConflict
			}
		}
		if p.Enabled != in.Enabled {
			oldPolicy = p.PolicyRevision
			p.Enabled = in.Enabled
			p.PolicyRevision, e = ldapNewRevision()
			if e != nil {
				return e
			}
			p.ReviewRevision, e = ldapNewRevision()
			if e != nil {
				return e
			}
			e = s.ldapRevoke(tx, "")
			if e != nil {
				return e
			}
			if e = tx.Save(&p).Error; e != nil {
				return e
			}
			if e = ldapAudit(tx, u, "identity.ldap.status.update", p, nil, in.Reason); e != nil {
				return e
			}
		}
		result = ldapConfigView(p, u, m)
		return ctx.Err()
	})
	if err != nil {
		return nil, ldapError(err)
	}
	s.ldapPublishPolicy(ctx, oldPolicy)
	return result, nil
}

// LDAP audit facts are intentionally independent of remote identity claims.
type ldapAuditDetails struct {
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

func ldapAuditJSON(u entity.User, p entity.LDAPProvider, b *entity.LDAPBinding, reason string) (string, error) {
	if !validRegistrationReason(reason) || u.ID == "" || u.CreatedAt.IsZero() || p.ID != "ldap" || p.CreatedAt.IsZero() || !validMemberRoleDigest(p.ReviewRevision) || !validMemberRoleDigest(p.ConfigRevision) || !validMemberRoleDigest(p.PolicyRevision) {
		return "", apperrors.ErrBadRequest
	}
	v := ldapAuditDetails{Version: 1, Reason: strings.TrimSpace(reason), ActorCreatedAt: u.CreatedAt.UTC(), ProviderID: p.ID, ProviderCreatedAt: p.CreatedAt.UTC(), ReviewRevision: p.ReviewRevision, ConfigRevision: p.ConfigRevision, PolicyRevision: p.PolicyRevision}
	if b != nil {
		if b.ID == "" || b.CreatedAt.IsZero() || b.UserID != u.ID || !b.UserCreatedAt.Equal(u.CreatedAt) || b.ProviderID != p.ID || b.ConfigRevision != p.ConfigRevision {
			return "", apperrors.ErrBadRequest
		}
		birth := b.CreatedAt.UTC()
		v.BindingID = b.ID
		v.BindingCreatedAt = &birth
	}
	raw, e := json.Marshal(v)
	return string(raw), e
}
func ldapAudit(tx *gorm.DB, u entity.User, action string, p entity.LDAPProvider, b *entity.LDAPBinding, reason string) error {
	resourceType, resourceID := "ldap_provider", p.ID
	switch action {
	case "identity.ldap.config.update", "identity.ldap.status.update", "identity.ldap.verify":
	case "account.ldap.bind", "account.ldap.unlink":
		if b == nil {
			return apperrors.ErrBadRequest
		}
		resourceType, resourceID = "ldap_binding", b.ID
	default:
		return apperrors.ErrBadRequest
	}
	raw, e := ldapAuditJSON(u, p, b, reason)
	if e != nil {
		return e
	}
	aid, e := id.NewPrefixed("aud")
	if e != nil {
		return e
	}
	return tx.Create(&entity.AuditEvent{ID: aid, ActorID: u.ID, Action: action, ResourceType: resourceType, ResourceID: resourceID, DetailsJSON: &raw}).Error
}
