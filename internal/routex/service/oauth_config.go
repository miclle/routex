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

var oauthUnavailable = &apperrors.Error{Code: 503, Message: "identity authentication temporarily unavailable"}

type OAuthPublic struct {
	Available bool   `json:"available"`
	Name      string `json:"name"`
}
type OAuthProviderView struct {
	Name string `json:"name"`

	AuthorizationURL string   `json:"authorization_url"`
	TokenURL         string   `json:"token_url"`
	UserInfoURL      string   `json:"user_info_url"`
	ClientAuthMethod string   `json:"client_auth_method"`
	Scopes           []string `json:"scopes"`
	SubjectPath      []string `json:"subject_path"`
	ClientID         string   `json:"client_id"`
	CallbackURL      string   `json:"callback_url"`
	SecretConfigured bool     `json:"secret_configured"`
	Enabled          bool     `json:"enabled"`
	Verified         bool     `json:"verified"`
	ReviewETag       string   `json:"review_etag"`
	MFARequired      bool     `json:"mfa_required"`
}
type OAuthProviderInput struct {
	Name string `json:"name"`

	AuthorizationURL string   `json:"authorization_url"`
	TokenURL         string   `json:"token_url"`
	UserInfoURL      string   `json:"user_info_url"`
	ClientAuthMethod string   `json:"client_auth_method"`
	Scopes           []string `json:"scopes"`
	SubjectPath      []string `json:"subject_path"`
	ClientID         string   `json:"client_id"`
	CallbackURL      string   `json:"callback_url"`
	SecretAction     string   `json:"secret_action"`
	ClientSecret     string   `json:"client_secret"`
	Reason           string   `json:"reason"`
}
type OAuthStatusInput struct {
	Enabled bool   `json:"enabled"`
	Reason  string `json:"reason"`
}

func (in *OAuthProviderInput) UnmarshalJSON(raw []byte) error {
	f, err := registrationStrictObject(raw, []string{"name", "authorization_url", "token_url", "user_info_url", "client_auth_method", "scopes", "subject_path", "client_id", "callback_url", "secret_action", "client_secret", "reason"})
	if err != nil {
		return err
	}
	var v OAuthProviderInput
	for k, d := range map[string]*string{"name": &v.Name, "authorization_url": &v.AuthorizationURL, "token_url": &v.TokenURL, "user_info_url": &v.UserInfoURL, "client_auth_method": &v.ClientAuthMethod, "client_id": &v.ClientID, "callback_url": &v.CallbackURL, "secret_action": &v.SecretAction, "client_secret": &v.ClientSecret, "reason": &v.Reason} {
		if json.Unmarshal(f[k], d) != nil {
			return apperrors.ErrBadRequest
		}
	}
	if json.Unmarshal(f["scopes"], &v.Scopes) != nil || json.Unmarshal(f["subject_path"], &v.SubjectPath) != nil || oauthValidateConfigInput(&v) != nil {
		return apperrors.ErrBadRequest
	}
	*in = v
	return nil
}
func (in *OAuthStatusInput) UnmarshalJSON(raw []byte) error {
	f, err := registrationStrictObject(raw, []string{"enabled", "reason"})
	if err != nil {
		return err
	}
	var v OAuthStatusInput
	if json.Unmarshal(f["enabled"], &v.Enabled) != nil || json.Unmarshal(f["reason"], &v.Reason) != nil || !validRegistrationReason(v.Reason) {
		return apperrors.ErrBadRequest
	}
	*in = v
	return nil
}
func oauthURL(raw string, callback bool) bool {
	if raw == "" || len(raw) > 2048 || !utf8.ValidString(raw) || strings.Contains(raw, "\\") || strings.ContainsFunc(raw, unicode.IsSpace) || strings.ContainsFunc(raw, unicode.IsControl) {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.Opaque == "" && u.User == nil && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && u.RawFragment == "" && (!callback || u.Path == "/api/v1/auth/oauth/callback" && u.RawPath == "")
}
func oauthText(v string, max int) bool {
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
func oauthArrays(scopes, subjectPath []string) bool {
	if scopes == nil || subjectPath == nil || len(scopes) > 16 || len(subjectPath) < 1 || len(subjectPath) > 16 {
		return false
	}
	total := 0
	seen := map[string]bool{}
	for _, v := range scopes {
		if v == "" || len(v) > 128 || seen[v] {
			return false
		}
		for _, r := range v {
			validScopeCharacter := r == 0x21 || r >= 0x23 && r <= 0x5b || r >= 0x5d && r <= 0x7e
			if !validScopeCharacter {
				return false
			}
		}
		seen[v] = true
		total += len(v)
	}
	if total > 1024 {
		return false
	}
	total = 0
	for _, v := range subjectPath {
		if !oauthText(v, 128) {
			return false
		}
		total += len(v)
	}
	return total <= 1024
}
func oauthValidateConfigInput(v *OAuthProviderInput) error {
	v.Name = strings.TrimSpace(v.Name)
	if !utf8.ValidString(v.Name) || utf8.RuneCountInString(v.Name) < 1 || utf8.RuneCountInString(v.Name) > 100 || strings.ContainsFunc(v.Name, unicode.IsControl) || !oauthURL(v.AuthorizationURL, false) || !oauthURL(v.TokenURL, false) || !oauthURL(v.UserInfoURL, false) || !oauthURL(v.CallbackURL, true) || !oauthText(v.ClientID, 256) || !oauthArrays(v.Scopes, v.SubjectPath) || (v.ClientAuthMethod != "client_secret_basic" && v.ClientAuthMethod != "client_secret_post") || !validRegistrationReason(v.Reason) {
		return apperrors.ErrBadRequest
	}
	if v.SecretAction == "keep" {
		if v.ClientSecret != "" {
			return apperrors.ErrBadRequest
		}
	} else if v.SecretAction != "replace" || !oauthText(v.ClientSecret, 4096) {
		return apperrors.ErrBadRequest
	}
	return nil
}
func oauthArrayJSON(v []string) string { raw, _ := json.Marshal(v); return string(raw) }
func oauthStoredArrays(p entity.OAuthProvider) ([]string, []string, bool) {
	scopes, path := []string{}, []string{}
	if p.AuthCiphertext == "" {
		return scopes, path, p.AuthorizationURL == "" && p.TokenURL == "" && p.UserInfoURL == "" && p.CallbackURL == "" && p.ClientID == "" && p.ClientAuthMethod == "" && (p.ScopesJSON == "" || p.ScopesJSON == "[]") && (p.SubjectPathJSON == "" || p.SubjectPathJSON == "[]")
	}
	if !utf8.ValidString(p.ScopesJSON) || !utf8.ValidString(p.SubjectPathJSON) || !modelCreationUnicode([]byte(p.ScopesJSON)) || !modelCreationUnicode([]byte(p.SubjectPathJSON)) || json.Unmarshal([]byte(p.ScopesJSON), &scopes) != nil || json.Unmarshal([]byte(p.SubjectPathJSON), &path) != nil {
		return nil, nil, false
	}
	in := OAuthProviderInput{Name: p.Name, AuthorizationURL: p.AuthorizationURL, TokenURL: p.TokenURL, UserInfoURL: p.UserInfoURL, CallbackURL: p.CallbackURL, ClientID: p.ClientID, ClientAuthMethod: p.ClientAuthMethod, Scopes: scopes, SubjectPath: path, SecretAction: "keep", Reason: "Validate stored configuration"}
	return scopes, path, oauthValidateConfigInput(&in) == nil && p.ScopesJSON == oauthArrayJSON(scopes) && p.SubjectPathJSON == oauthArrayJSON(path)
}
func oauthError(err error) error {
	if err == nil {
		return nil
	}
	var e *apperrors.Error
	if errors.As(err, &e) && (e.Code == 400 || e.Code == 401 || e.Code == 403 || e.Code == 409) {
		return e
	}
	return oauthUnavailable
}
func oauthProvider(tx *gorm.DB) (entity.OAuthProvider, error) {
	var p entity.OAuthProvider
	err := tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "id"}, "oauth")).Take(&p).Error
	if err == nil && (p.ID != "oauth" || p.CreatedAt.IsZero() || !validMemberRoleDigest(p.ReviewRevision) || !validMemberRoleDigest(p.ConfigRevision) || !validMemberRoleDigest(p.PolicyRevision)) {
		err = oauthUnavailable
	}
	if err == nil {
		_, _, ok := oauthStoredArrays(p)
		if !ok {
			err = oauthUnavailable
		}
	}
	return p, err
}
func oauthDigest(v ...any) string { raw, _ := json.Marshal(v); return secret.SHA256Hex(string(raw)) }
func oauthReview(p entity.OAuthProvider, u entity.User, m entity.UserMFA) string {
	return oauthDigest("oauth.config.review.v1", u.ID, u.CreatedAt.UTC(), p.ID, p.CreatedAt.UTC(), p.ReviewRevision, p.ConfigRevision, p.PolicyRevision, p.Name, p.Enabled, p.VerifiedConfigRevision, p.VerifiedBy, p.VerifiedBindingID, m.Enabled, m.Generation)
}
func oauthVerified(p entity.OAuthProvider) bool {
	return p.ConfigRevision != "" && p.VerifiedConfigRevision == p.ConfigRevision && p.VerifiedBy != "" && p.VerifiedUserCreatedAt != nil && !p.VerifiedUserCreatedAt.IsZero() && p.VerifiedBindingID != "" && p.VerifiedBindingCreatedAt != nil && !p.VerifiedBindingCreatedAt.IsZero()
}
func oauthConfigView(p entity.OAuthProvider, u entity.User, m entity.UserMFA) *OAuthProviderView {
	scopes, path, _ := oauthStoredArrays(p)
	return &OAuthProviderView{Name: p.Name, AuthorizationURL: p.AuthorizationURL, TokenURL: p.TokenURL, UserInfoURL: p.UserInfoURL, ClientAuthMethod: p.ClientAuthMethod, Scopes: scopes, SubjectPath: path, ClientID: p.ClientID, CallbackURL: p.CallbackURL, SecretConfigured: p.AuthCiphertext != "", Enabled: p.Enabled, Verified: oauthVerified(p), ReviewETag: oauthReview(p, u, m), MFARequired: m.Enabled}
}
func oauthAdmin(tx *gorm.DB, id string) (entity.User, entity.UserMFA, error) {
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
func (s *Service) PublicOAuth(ctx context.Context) (*OAuthPublic, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	p, err := oauthProvider(s.authDB(ctx))
	if err != nil {
		return nil, oauthError(err)
	}
	v := &OAuthPublic{Available: p.Enabled && oauthVerified(p)}
	if v.Available {
		v.Name = p.Name
	}
	return v, nil
}
func (s *Service) GetOAuthProvider(ctx context.Context, actorID string) (*OAuthProviderView, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var result *OAuthProviderView
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		u, m, e := oauthAdmin(tx, actorID)
		if e != nil {
			return e
		}
		p, e := oauthProvider(tx)
		if e != nil {
			return e
		}
		result = oauthConfigView(p, u, m)
		return ctx.Err()
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, oauthError(err)
	}
	return result, nil
}
func oauthNewRevision() (string, error) {
	v, e := secret.RandomURLSafe(32)
	if e != nil {
		return "", e
	}
	return secret.SHA256Hex(v), nil
}
func (s *Service) oauthRevoke(tx *gorm.DB, bindingID string) error {
	q := tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "primary_method"}, "oauth"))
	if bindingID != "" {
		q = q.Where(database.ExactText(tx, clause.Column{Name: "oauth_binding_id"}, bindingID))
	}
	if e := q.Delete(&entity.Session{}).Error; e != nil {
		return e
	}
	q = tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "primary_method"}, "oauth"))
	if bindingID != "" {
		q = q.Where(database.ExactText(tx, clause.Column{Name: "oauth_binding_id"}, bindingID))
	}
	if e := q.Delete(&entity.MFAChallenge{}).Error; e != nil {
		return e
	}
	q = tx.Session(&gorm.Session{NewDB: true}).Model(&entity.OAuthCeremony{})
	if bindingID != "" {
		q = q.Where(database.ExactText(tx, clause.Column{Name: "binding_id"}, bindingID))
	}
	return q.Where("status IN ?", []string{"pending", "exchanging", "verified"}).Update("status", "failed").Error
}
func (s *Service) oauthPublishPolicy(ctx context.Context, revision string) {
	if revision != "" {
		s.invalidateRuntimeOAuthPolicy(revision)
		s.publishSessionMutation(ctx)
	}
}
func (s *Service) SaveOAuthProvider(ctx context.Context, actorID, etag string, in OAuthProviderInput) (*OAuthProviderView, error) {
	if !validMemberRoleDigest(etag) || oauthValidateConfigInput(&in) != nil {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var ciphertext, generation string
	epoch := s.secretEpoch()
	var err error
	if in.SecretAction == "replace" {
		generation, err = oauthNewRevision()
		if err != nil {
			return nil, oauthUnavailable
		}
		ciphertext, err = s.sealSecret(rootReference("oauth_providers", "oauth", generation), in.ClientSecret)
		if err != nil {
			return nil, oauthUnavailable
		}
	}
	var result *OAuthProviderView
	var oldPolicy string
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		u, m, e := oauthAdmin(tx, actorID)
		if e != nil {
			return e
		}
		p, e := oauthProvider(tx)
		if e != nil {
			return e
		}
		if oauthReview(p, u, m) != etag {
			return catalogConflict
		}
		if in.SecretAction == "keep" && p.AuthCiphertext == "" {
			return apperrors.ErrBadRequest
		}
		security := p.AuthorizationURL != in.AuthorizationURL || p.TokenURL != in.TokenURL || p.UserInfoURL != in.UserInfoURL || p.ClientAuthMethod != in.ClientAuthMethod || p.ScopesJSON != oauthArrayJSON(in.Scopes) || p.SubjectPathJSON != oauthArrayJSON(in.SubjectPath) || p.ClientID != in.ClientID || p.CallbackURL != in.CallbackURL || in.SecretAction == "replace"
		changed := security || p.Name != in.Name
		if !changed {
			result = oauthConfigView(p, u, m)
			return ctx.Err()
		}
		p.Name = in.Name
		p.AuthorizationURL = in.AuthorizationURL
		p.TokenURL = in.TokenURL
		p.UserInfoURL = in.UserInfoURL
		p.ClientAuthMethod = in.ClientAuthMethod
		p.ScopesJSON = oauthArrayJSON(in.Scopes)
		p.SubjectPathJSON = oauthArrayJSON(in.SubjectPath)
		p.ClientID = in.ClientID
		p.CallbackURL = in.CallbackURL
		p.ReviewRevision, e = oauthNewRevision()
		if e != nil {
			return e
		}
		if security {
			oldPolicy = p.PolicyRevision
			p.ConfigRevision, e = oauthNewRevision()
			if e != nil {
				return e
			}
			p.PolicyRevision, e = oauthNewRevision()
			if e != nil {
				return e
			}
			p.Enabled = false
			p.VerifiedConfigRevision = ""
			p.VerifiedBy = ""
			p.VerifiedUserCreatedAt = nil
			p.VerifiedBindingID = ""
			p.VerifiedBindingCreatedAt = nil
			e = s.oauthRevoke(tx, "")
			if e != nil {
				return e
			}
			if e = tx.Session(&gorm.Session{NewDB: true}).Where("1 = ?", 1).Delete(&entity.OAuthBinding{}).Error; e != nil {
				return e
			}
		}
		if ciphertext != "" {
			if e = s.guardSecretWrite(tx, epoch, rootReference("oauth_providers", "oauth", generation), ciphertext); e != nil {
				return e
			}
			p.AuthCiphertext = ciphertext
			p.SecretGeneration = generation
		}
		if e = tx.Save(&p).Error; e != nil {
			return e
		}
		if e = oauthAudit(tx, u, "identity.oauth.config.update", p, nil, in.Reason); e != nil {
			return e
		}
		result = oauthConfigView(p, u, m)
		return ctx.Err()
	})
	if err != nil {
		return nil, oauthError(err)
	}
	s.oauthPublishPolicy(ctx, oldPolicy)
	return result, nil
}
func (s *Service) SetOAuthEnabled(ctx context.Context, actorID, etag string, in OAuthStatusInput) (*OAuthProviderView, error) {
	if !validMemberRoleDigest(etag) || !validRegistrationReason(in.Reason) {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var result *OAuthProviderView
	var oldPolicy string
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		u, m, e := oauthAdmin(tx, actorID)
		if e != nil {
			return e
		}
		p, e := oauthProvider(tx)
		if e != nil {
			return e
		}
		if oauthReview(p, u, m) != etag {
			return catalogConflict
		}
		if in.Enabled {
			if !oauthVerified(p) {
				return catalogConflict
			}
			verifier, e := registrationAdmittedUser(tx, p.VerifiedBy, false)
			if e != nil {
				return e
			}
			if verifier.Role != entity.RoleAdmin || !verifier.CreatedAt.Equal(*p.VerifiedUserCreatedAt) {
				return catalogConflict
			}
			b, e := oauthBindingByID(tx, p.VerifiedBindingID)
			if e != nil {
				return e
			}
			if !b.CreatedAt.Equal(*p.VerifiedBindingCreatedAt) || b.UserID != verifier.ID || !b.UserCreatedAt.Equal(verifier.CreatedAt) || b.ProviderID != p.ID || !oauthSubject(b.SubjectKind, b.Subject) || b.ConfigRevision != p.ConfigRevision || b.SubjectDigest != oauthSubjectDigest(p.ID, b.SubjectKind, b.Subject) {
				return catalogConflict
			}
		}
		if p.Enabled != in.Enabled {
			oldPolicy = p.PolicyRevision
			p.Enabled = in.Enabled
			p.PolicyRevision, e = oauthNewRevision()
			if e != nil {
				return e
			}
			p.ReviewRevision, e = oauthNewRevision()
			if e != nil {
				return e
			}
			e = s.oauthRevoke(tx, "")
			if e != nil {
				return e
			}
			if e = tx.Save(&p).Error; e != nil {
				return e
			}
			if e = oauthAudit(tx, u, "identity.oauth.status.update", p, nil, in.Reason); e != nil {
				return e
			}
		}
		result = oauthConfigView(p, u, m)
		return ctx.Err()
	})
	if err != nil {
		return nil, oauthError(err)
	}
	s.oauthPublishPolicy(ctx, oldPolicy)
	return result, nil
}

// OAuth audit facts are intentionally independent of remote identity claims.
type oauthAuditDetails struct {
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

func oauthAuditJSON(u entity.User, p entity.OAuthProvider, b *entity.OAuthBinding, reason string) (string, error) {
	if !validRegistrationReason(reason) || u.ID == "" || u.CreatedAt.IsZero() || p.ID != "oauth" || p.CreatedAt.IsZero() || !validMemberRoleDigest(p.ReviewRevision) || !validMemberRoleDigest(p.ConfigRevision) || !validMemberRoleDigest(p.PolicyRevision) {
		return "", apperrors.ErrBadRequest
	}
	v := oauthAuditDetails{Version: 1, Reason: strings.TrimSpace(reason), ActorCreatedAt: u.CreatedAt.UTC(), ProviderID: p.ID, ProviderCreatedAt: p.CreatedAt.UTC(), ReviewRevision: p.ReviewRevision, ConfigRevision: p.ConfigRevision, PolicyRevision: p.PolicyRevision}
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
func oauthAudit(tx *gorm.DB, u entity.User, action string, p entity.OAuthProvider, b *entity.OAuthBinding, reason string) error {
	resourceType, resourceID := "oauth_provider", p.ID
	switch action {
	case "identity.oauth.config.update", "identity.oauth.status.update", "identity.oauth.verify":
	case "account.oauth.bind", "account.oauth.unlink":
		if b == nil {
			return apperrors.ErrBadRequest
		}
		resourceType, resourceID = "oauth_binding", b.ID
	default:
		return apperrors.ErrBadRequest
	}
	raw, e := oauthAuditJSON(u, p, b, reason)
	if e != nil {
		return e
	}
	aid, e := id.NewPrefixed("aud")
	if e != nil {
		return e
	}
	return tx.Create(&entity.AuditEvent{ID: aid, ActorID: u.ID, Action: action, ResourceType: resourceType, ResourceID: resourceID, DetailsJSON: &raw}).Error
}
