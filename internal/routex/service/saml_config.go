package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"encoding/pem"
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
	samlprotocol "github.com/miclle/routex/pkg/saml"
	"github.com/miclle/routex/pkg/secret"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var samlUnavailable = &apperrors.Error{Code: 503, Message: "identity authentication temporarily unavailable"}

type SAMLPublic struct {
	Available bool   `json:"available"`
	Name      string `json:"name"`
}
type SAMLProviderView struct {
	Name                  string `json:"name"`
	IDPIssuer             string `json:"idp_issuer"`
	SSOURL                string `json:"sso_url"`
	SPEntityID            string `json:"sp_entity_id"`
	ACSURL                string `json:"acs_url"`
	SigningCertificatePEM string `json:"signing_certificate_pem"`
	Enabled               bool   `json:"enabled"`
	Verified              bool   `json:"verified"`
	ReviewETag            string `json:"review_etag"`
	MFARequired           bool   `json:"mfa_required"`
}
type SAMLProviderInput struct {
	Name                  string `json:"name"`
	IDPIssuer             string `json:"idp_issuer"`
	SSOURL                string `json:"sso_url"`
	SPEntityID            string `json:"sp_entity_id"`
	ACSURL                string `json:"acs_url"`
	SigningCertificatePEM string `json:"signing_certificate_pem"`
	Reason                string `json:"reason"`
}
type SAMLStatusInput struct {
	Enabled bool   `json:"enabled"`
	Reason  string `json:"reason"`
}

func (in *SAMLProviderInput) UnmarshalJSON(raw []byte) error {
	f, e := registrationStrictObject(raw, []string{"name", "idp_issuer", "sso_url", "sp_entity_id", "acs_url", "signing_certificate_pem", "reason"})
	if e != nil {
		return e
	}
	var v SAMLProviderInput
	for k, d := range map[string]*string{"name": &v.Name, "idp_issuer": &v.IDPIssuer, "sso_url": &v.SSOURL, "sp_entity_id": &v.SPEntityID, "acs_url": &v.ACSURL, "signing_certificate_pem": &v.SigningCertificatePEM, "reason": &v.Reason} {
		if json.Unmarshal(f[k], d) != nil {
			return apperrors.ErrBadRequest
		}
	}
	if e = samlValidateConfigInput(&v); e != nil {
		return e
	}
	*in = v
	return nil
}
func (in *SAMLStatusInput) UnmarshalJSON(raw []byte) error {
	f, e := registrationStrictObject(raw, []string{"enabled", "reason"})
	if e != nil {
		return e
	}
	var v SAMLStatusInput
	if json.Unmarshal(f["enabled"], &v.Enabled) != nil || json.Unmarshal(f["reason"], &v.Reason) != nil || !validRegistrationReason(v.Reason) {
		return apperrors.ErrBadRequest
	}
	*in = v
	return nil
}
func samlIdentifier(v string) bool {
	if v == "" || len(v) > 2048 || !utf8.ValidString(v) || strings.ContainsAny(v, "\\") || strings.ContainsFunc(v, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return false
	}
	u, e := url.Parse(v)
	return e == nil && u.IsAbs() && u.User == nil && u.Fragment == ""
}
func samlEndpoint(v string, acs bool) bool {
	if !samlIdentifier(v) {
		return false
	}
	u, e := url.Parse(v)
	return e == nil && u.Scheme == "https" && u.Hostname() != "" && u.Opaque == "" && u.RawQuery == "" && !u.ForceQuery && (!acs || u.Path == "/api/v1/auth/saml/acs" && u.RawPath == "")
}

// pem.Decode searches past malformed blocks. Admit precisely the first bounded
// certificate span, so a later valid block cannot conceal a malformed prefix.
func samlCertificate(v string) ([]byte, string, error) {
	if len(v) == 0 || len(v) > 24*1024 || !utf8.ValidString(v) {
		return nil, "", apperrors.ErrBadRequest
	}
	raw := bytes.TrimSpace([]byte(v))
	begin := []byte("-----BEGIN CERTIFICATE-----")
	end := []byte("-----END CERTIFICATE-----")
	if !bytes.HasPrefix(raw, begin) {
		return nil, "", apperrors.ErrBadRequest
	}
	at := bytes.Index(raw, end)
	if at < 0 {
		return nil, "", apperrors.ErrBadRequest
	}
	span := raw[:at+len(end)]
	if bytes.Contains(span[len(begin):], []byte("-----BEGIN")) || len(bytes.TrimSpace(raw[len(span):])) != 0 {
		return nil, "", apperrors.ErrBadRequest
	}
	block, rest := pem.Decode(span)
	if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 || len(rest) != 0 || len(block.Bytes) == 0 || len(block.Bytes) > 16*1024 {
		return nil, "", apperrors.ErrBadRequest
	}
	return block.Bytes, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: block.Bytes})), nil
}
func samlValidateConfigInput(v *SAMLProviderInput) error {
	v.Name = strings.TrimSpace(v.Name)
	if !utf8.ValidString(v.Name) || utf8.RuneCountInString(v.Name) < 1 || utf8.RuneCountInString(v.Name) > 100 || strings.ContainsFunc(v.Name, unicode.IsControl) || !samlIdentifier(v.IDPIssuer) || !samlIdentifier(v.SPEntityID) || !samlEndpoint(v.SSOURL, false) || !samlEndpoint(v.ACSURL, true) || !validRegistrationReason(v.Reason) {
		return apperrors.ErrBadRequest
	}
	der, canonical, e := samlCertificate(v.SigningCertificatePEM)
	if e != nil {
		return e
	}
	if _, e = samlprotocol.New(samlprotocol.Config{IDPIssuer: v.IDPIssuer, SSOURL: v.SSOURL, SPEntityID: v.SPEntityID, ACSURL: v.ACSURL, SigningCertificateDER: der}); e != nil {
		return apperrors.ErrBadRequest
	}
	v.SigningCertificatePEM = canonical
	return nil
}
func samlProtocol(p entity.SAMLProvider) (*samlprotocol.Client, error) {
	der, _, e := samlCertificate(p.SigningCertificatePEM)
	if e != nil {
		return nil, apperrors.ErrUnauthorized
	}
	c, e := samlprotocol.New(samlprotocol.Config{IDPIssuer: p.IDPIssuer, SSOURL: p.SSOURL, SPEntityID: p.SPEntityID, ACSURL: p.ACSURL, SigningCertificateDER: der})
	if e != nil {
		return nil, apperrors.ErrUnauthorized
	}
	return c, nil
}
func samlError(err error) error {
	if err == nil {
		return nil
	}
	var e *apperrors.Error
	if errors.As(err, &e) && (e.Code == 400 || e.Code == 401 || e.Code == 403 || e.Code == 409) {
		return e
	}
	return samlUnavailable
}
func samlProvider(tx *gorm.DB) (entity.SAMLProvider, error) {
	var p entity.SAMLProvider
	err := tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "id"}, "saml")).Take(&p).Error
	if err == nil && (p.ID != "saml" || p.CreatedAt.IsZero() || !validMemberRoleDigest(p.ReviewRevision) || !validMemberRoleDigest(p.ConfigRevision) || !validMemberRoleDigest(p.PolicyRevision)) {
		err = samlUnavailable
	}
	return p, err
}
func samlDigest(v ...any) string { raw, _ := json.Marshal(v); return secret.SHA256Hex(string(raw)) }
func samlReview(p entity.SAMLProvider, u entity.User, m entity.UserMFA) string {
	return samlDigest("saml.config.review.v1", u.ID, u.CreatedAt.UTC(), p.ID, p.CreatedAt.UTC(), p.ReviewRevision, p.ConfigRevision, p.PolicyRevision, p.Name, p.Enabled, p.VerifiedConfigRevision, p.VerifiedBy, p.VerifiedBindingID, m.Enabled, m.Generation)
}
func samlVerified(p entity.SAMLProvider) bool {
	return p.ConfigRevision != "" && p.VerifiedConfigRevision == p.ConfigRevision && p.VerifiedBy != "" && p.VerifiedUserCreatedAt != nil && !p.VerifiedUserCreatedAt.IsZero() && p.VerifiedBindingID != "" && p.VerifiedBindingCreatedAt != nil && !p.VerifiedBindingCreatedAt.IsZero()
}
func samlConfigView(p entity.SAMLProvider, u entity.User, m entity.UserMFA) *SAMLProviderView {
	return &SAMLProviderView{Name: p.Name, IDPIssuer: p.IDPIssuer, SSOURL: p.SSOURL, SPEntityID: p.SPEntityID, ACSURL: p.ACSURL, SigningCertificatePEM: p.SigningCertificatePEM, Enabled: p.Enabled, Verified: samlVerified(p), ReviewETag: samlReview(p, u, m), MFARequired: m.Enabled}
}
func samlAdmin(tx *gorm.DB, id string) (entity.User, entity.UserMFA, error) {
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
func (s *Service) PublicSAML(ctx context.Context) (*SAMLPublic, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	p, err := samlProvider(s.authDB(ctx))
	if err != nil {
		return nil, samlError(err)
	}
	v := &SAMLPublic{Available: p.Enabled && samlVerified(p)}
	if v.Available {
		v.Name = p.Name
	}
	return v, nil
}
func (s *Service) GetSAML(ctx context.Context, actorID string) (*SAMLProviderView, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var result *SAMLProviderView
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		u, m, e := samlAdmin(tx, actorID)
		if e != nil {
			return e
		}
		p, e := samlProvider(tx)
		if e != nil {
			return e
		}
		result = samlConfigView(p, u, m)
		return ctx.Err()
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, samlError(err)
	}
	return result, nil
}
func samlNewRevision() (string, error) {
	v, e := secret.RandomURLSafe(32)
	if e != nil {
		return "", e
	}
	return secret.SHA256Hex(v), nil
}
func (s *Service) samlRevoke(tx *gorm.DB, bindingID string) error {
	q := tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "primary_method"}, "saml"))
	if bindingID != "" {
		q = q.Where(database.ExactText(tx, clause.Column{Name: "saml_binding_id"}, bindingID))
	}
	if e := q.Delete(&entity.Session{}).Error; e != nil {
		return e
	}
	q = tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "primary_method"}, "saml"))
	if bindingID != "" {
		q = q.Where(database.ExactText(tx, clause.Column{Name: "saml_binding_id"}, bindingID))
	}
	if e := q.Delete(&entity.MFAChallenge{}).Error; e != nil {
		return e
	}
	q = tx.Session(&gorm.Session{NewDB: true}).Model(&entity.SAMLCeremony{})
	if bindingID != "" {
		q = q.Where(database.ExactText(tx, clause.Column{Name: "binding_id"}, bindingID))
	}
	return q.Where("status IN ?", []string{"pending", "validating", "verified"}).Update("status", "failed").Error
}
func (s *Service) samlPublishPolicy(ctx context.Context, revision string) {
	if revision != "" {
		s.invalidateRuntimeSAMLPolicy(revision)
		s.publishSessionMutation(ctx)
	}
}
func (s *Service) SaveSAML(ctx context.Context, actorID, etag string, in SAMLProviderInput) (*SAMLProviderView, error) {
	if !validMemberRoleDigest(etag) || samlValidateConfigInput(&in) != nil {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var err error
	var result *SAMLProviderView
	var oldPolicy string
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		u, m, e := samlAdmin(tx, actorID)
		if e != nil {
			return e
		}
		p, e := samlProvider(tx)
		if e != nil {
			return e
		}
		if samlReview(p, u, m) != etag {
			return catalogConflict
		}
		security := p.IDPIssuer != in.IDPIssuer || p.SSOURL != in.SSOURL || p.SPEntityID != in.SPEntityID || p.ACSURL != in.ACSURL || p.SigningCertificatePEM != in.SigningCertificatePEM
		changed := security || p.Name != in.Name
		if !changed {
			result = samlConfigView(p, u, m)
			return ctx.Err()
		}
		p.Name = in.Name
		p.IDPIssuer = in.IDPIssuer
		p.SSOURL = in.SSOURL
		p.SPEntityID = in.SPEntityID
		p.ACSURL = in.ACSURL
		p.SigningCertificatePEM = in.SigningCertificatePEM
		p.ReviewRevision, e = samlNewRevision()
		if e != nil {
			return e
		}
		if security {
			oldPolicy = p.PolicyRevision
			p.ConfigRevision, e = samlNewRevision()
			if e != nil {
				return e
			}
			p.PolicyRevision, e = samlNewRevision()
			if e != nil {
				return e
			}
			p.Enabled = false
			p.VerifiedConfigRevision = ""
			p.VerifiedBy = ""
			p.VerifiedUserCreatedAt = nil
			p.VerifiedBindingID = ""
			p.VerifiedBindingCreatedAt = nil
			e = s.samlRevoke(tx, "")
			if e != nil {
				return e
			}
			if e = tx.Session(&gorm.Session{NewDB: true}).Where("1 = ?", 1).Delete(&entity.SAMLBinding{}).Error; e != nil {
				return e
			}
		}
		if e = tx.Save(&p).Error; e != nil {
			return e
		}
		if e = samlAudit(tx, u, "identity.saml.config.update", p, nil, in.Reason); e != nil {
			return e
		}
		result = samlConfigView(p, u, m)
		return ctx.Err()
	})
	if err != nil {
		return nil, samlError(err)
	}
	s.samlPublishPolicy(ctx, oldPolicy)
	return result, nil
}
func (s *Service) SetSAMLStatus(ctx context.Context, actorID, etag string, in SAMLStatusInput) (*SAMLProviderView, error) {
	if !validMemberRoleDigest(etag) || !validRegistrationReason(in.Reason) {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var result *SAMLProviderView
	var oldPolicy string
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		u, m, e := samlAdmin(tx, actorID)
		if e != nil {
			return e
		}
		p, e := samlProvider(tx)
		if e != nil {
			return e
		}
		if samlReview(p, u, m) != etag {
			return catalogConflict
		}
		if in.Enabled {
			if !samlVerified(p) {
				return catalogConflict
			}
			verifier, e := registrationAdmittedUser(tx, p.VerifiedBy, false)
			if e != nil {
				return e
			}
			if verifier.Role != entity.RoleAdmin || !verifier.CreatedAt.Equal(*p.VerifiedUserCreatedAt) {
				return catalogConflict
			}
			b, e := samlBindingByID(tx, p.VerifiedBindingID)
			if e != nil {
				return e
			}
			if !b.CreatedAt.Equal(*p.VerifiedBindingCreatedAt) || b.UserID != verifier.ID || !b.UserCreatedAt.Equal(verifier.CreatedAt) || b.ConfigRevision != p.ConfigRevision || b.ProviderID != p.ID || b.Issuer != p.IDPIssuer || b.SubjectDigest != samlSubjectDigest(p.IDPIssuer, b.Subject) {
				return catalogConflict
			}
		}
		if p.Enabled != in.Enabled {
			oldPolicy = p.PolicyRevision
			p.Enabled = in.Enabled
			p.PolicyRevision, e = samlNewRevision()
			if e != nil {
				return e
			}
			p.ReviewRevision, e = samlNewRevision()
			if e != nil {
				return e
			}
			e = s.samlRevoke(tx, "")
			if e != nil {
				return e
			}
			if e = tx.Save(&p).Error; e != nil {
				return e
			}
			if e = samlAudit(tx, u, "identity.saml.status.update", p, nil, in.Reason); e != nil {
				return e
			}
		}
		result = samlConfigView(p, u, m)
		return ctx.Err()
	})
	if err != nil {
		return nil, samlError(err)
	}
	s.samlPublishPolicy(ctx, oldPolicy)
	return result, nil
}

// SAML audit facts are intentionally independent of remote identity claims.
type samlAuditDetails struct {
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

func samlAuditJSON(u entity.User, p entity.SAMLProvider, b *entity.SAMLBinding, reason string) (string, error) {
	if !validRegistrationReason(reason) || u.ID == "" || u.CreatedAt.IsZero() || p.ID != "saml" || p.CreatedAt.IsZero() || !validMemberRoleDigest(p.ReviewRevision) || !validMemberRoleDigest(p.ConfigRevision) || !validMemberRoleDigest(p.PolicyRevision) {
		return "", apperrors.ErrBadRequest
	}
	v := samlAuditDetails{Version: 1, Reason: strings.TrimSpace(reason), ActorCreatedAt: u.CreatedAt.UTC(), ProviderID: p.ID, ProviderCreatedAt: p.CreatedAt.UTC(), ReviewRevision: p.ReviewRevision, ConfigRevision: p.ConfigRevision, PolicyRevision: p.PolicyRevision}
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
func samlAudit(tx *gorm.DB, u entity.User, action string, p entity.SAMLProvider, b *entity.SAMLBinding, reason string) error {
	resourceType, resourceID := "saml_provider", p.ID
	switch action {
	case "identity.saml.config.update", "identity.saml.status.update", "identity.saml.verify":
	case "account.saml.bind", "account.saml.unlink":
		if b == nil {
			return apperrors.ErrBadRequest
		}
		resourceType, resourceID = "saml_binding", b.ID
	default:
		return apperrors.ErrBadRequest
	}
	raw, e := samlAuditJSON(u, p, b, reason)
	if e != nil {
		return e
	}
	aid, e := id.NewPrefixed("aud")
	if e != nil {
		return e
	}
	return tx.Create(&entity.AuditEvent{ID: aid, ActorID: u.ID, Action: action, ResourceType: resourceType, ResourceID: resourceID, DetailsJSON: &raw}).Error
}
