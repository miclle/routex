package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/url"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	oauthprotocol "github.com/miclle/routex/pkg/oauth"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/upstream"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const oauthCeremonyLifetime = 5 * time.Minute
const oauthCeremonyLiveLimit = 1024
const oauthCeremonyPruneLimit = 128

type OAuthStart struct {
	AuthorizationURL string    `json:"authorization_url"`
	Cookie           string    `json:"-"`
	ExpiresAt        time.Time `json:"-"`
}

func oauthBrowserValue(v string) bool {
	if len(v) != 43 {
		return false
	}
	for _, r := range v {
		valid := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_'
		if !valid {
			return false
		}
	}
	return true
}
func oauthDerived(cookie, ceremonyID, domain string) string {
	sum := sha256.Sum256([]byte("routex-oauth:" + domain + ":" + cookie + ":" + ceremonyID))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
func (s *Service) oauthProtocol(ctx context.Context, p entity.OAuthProvider) (*oauthprotocol.Client, func(), error) {
	scopes, path, ok := oauthStoredArrays(p)
	if !ok || p.AuthCiphertext == "" {
		return nil, func() {}, oauthUnavailable
	}
	client := upstream.NewNonReplayingClient(s.allowPrivateUpstream)
	closeClient := client.CloseIdleConnections
	plaintext, e := s.openSecret(rootReference("oauth_providers", p.ID, p.SecretGeneration), p.AuthCiphertext)
	if e != nil {
		closeClient()
		return nil, func() {}, oauthUnavailable
	}
	c, e := oauthprotocol.New(ctx, oauthprotocol.Config{AuthorizationURL: p.AuthorizationURL, TokenURL: p.TokenURL, UserInfoURL: p.UserInfoURL, RedirectURL: p.CallbackURL, ClientID: p.ClientID, ClientSecret: plaintext, ClientAuthMethod: oauthprotocol.ClientAuthMethod(p.ClientAuthMethod), Scopes: scopes, SubjectPath: path, Transport: client.Transport, EndpointPolicy: func(c context.Context, u *url.URL) error {
		if e := c.Err(); e != nil {
			return e
		}
		if u == nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.Opaque != "" {
			return apperrors.ErrBadRequest
		}
		v := *u
		// Only the engine constructs the fixed authorization query. Stored
		// endpoints stay query-free; guarded transport checks every dial.
		v.RawQuery = ""
		v.ForceQuery = false
		_, e := upstream.ValidateBaseURL(v.String(), s.allowPrivateUpstream)
		return e
	}})
	if e != nil {
		closeClient()
		return nil, func() {}, oauthUnavailable
	}
	return c, closeClient, nil
}
func oauthCeremonyCurrent(p entity.OAuthProvider, c entity.OAuthCeremony, now time.Time) bool {
	return p.ID == "oauth" && c.ProviderID == p.ID && c.ID != "" && !c.CreatedAt.IsZero() && !c.CreatedAt.After(now) && c.ExpiresAt.After(now) && c.ExpiresAt.Sub(c.CreatedAt) <= oauthCeremonyLifetime && c.ConfigRevision == p.ConfigRevision && c.PolicyRevision == p.PolicyRevision && (c.Purpose == "verify" || p.Enabled && oauthVerified(p)) && (c.Purpose == "login" || c.Purpose == "bind" || c.Purpose == "verify")
}
func oauthOriginalSession(tx *gorm.DB, c entity.OAuthCeremony) (entity.User, error) {
	if c.UserID == "" || c.UserCreatedAt == nil || c.SessionID == "" || c.SessionCreatedAt == nil {
		return entity.User{}, apperrors.ErrUnauthorized
	}
	u, e := registrationAdmittedUser(tx, c.UserID, true)
	if e != nil {
		return u, e
	}
	if !u.CreatedAt.Equal(*c.UserCreatedAt) || secret.SHA256Hex(u.PasswordHash) != c.PasswordDigest {
		return u, apperrors.ErrUnauthorized
	}
	var row entity.Session
	e = tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "id"}, c.SessionID)).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&row).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return u, apperrors.ErrUnauthorized
	}
	if e != nil {
		return u, e
	}
	if row.ID != c.SessionID || row.UserID != u.ID || !row.CreatedAt.Equal(*c.SessionCreatedAt) || !row.ExpiresAt.After(time.Now().UTC()) {
		return u, apperrors.ErrUnauthorized
	}
	if e = primaryValidateSession(tx, row); e != nil {
		return u, e
	}
	m, e := mfaState(tx, u.ID)
	if e != nil {
		return u, e
	}
	if m.Generation != c.MFAGeneration {
		return u, apperrors.ErrUnauthorized
	}
	if e = oauthCapturedBinding(tx, c, u); e != nil {
		return u, e
	}
	return u, nil
}
func (s *Service) startOAuth(ctx context.Context, a *Authentication, etag string, in OAuthIdentityInput, purpose string) (*OAuthStart, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var passwordHash string
	var e error
	if purpose != "login" {
		if a == nil {
			return nil, apperrors.ErrUnauthorized
		}
		if !validMemberRoleDigest(etag) || !validRegistrationReason(in.Reason) {
			return nil, apperrors.ErrBadRequest
		}
		passwordHash, e = s.mfaPassword(ctx, a.User.ID, in.Password)
		if e != nil {
			return nil, e
		}
	}
	state, e := secret.RandomURLSafe(32)
	if e != nil {
		return nil, oauthUnavailable
	}
	cookie, e := secret.RandomURLSafe(32)
	if e != nil {
		return nil, oauthUnavailable
	}
	cid, e := id.NewPrefixed("oac")
	if e != nil {
		return nil, oauthUnavailable
	}
	var captured entity.OAuthProvider
	var c entity.OAuthCeremony
	rejected := false
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		p, e := oauthProvider(tx)
		if e != nil {
			return e
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		c = entity.OAuthCeremony{ID: cid, ProviderID: p.ID, StateHash: secret.SHA256Hex(state), CookieHash: secret.SHA256Hex(cookie), Purpose: purpose, Status: "pending", ConfigRevision: p.ConfigRevision, PolicyRevision: p.PolicyRevision, CreatedAt: now, ExpiresAt: now.Add(oauthCeremonyLifetime)}
		if purpose != "verify" && (!p.Enabled || !oauthVerified(p)) {
			return apperrors.ErrUnauthorized
		}
		if purpose != "login" {
			u, m, bad, e := s.oauthLocalProof(tx, a, passwordHash, in.Proof)
			if e != nil {
				return e
			}
			if bad {
				rejected = true
				return nil
			}
			b, e := oauthUserBinding(tx, u)
			if e != nil {
				return e
			}
			if purpose == "verify" {
				if e = registrationPolicyAuthority(tx, u.ID); e != nil {
					return e
				}
				if oauthReview(p, u, m) != etag {
					return catalogConflict
				}
			} else if oauthAccountReview(p, u, m, a, b) != etag {
				return catalogConflict
			}
			c.UserID = u.ID
			c.UserCreatedAt = &u.CreatedAt
			c.PasswordDigest = secret.SHA256Hex(u.PasswordHash)
			c.MFAGeneration = m.Generation
			c.SessionID = a.Session.ID
			c.SessionCreatedAt = &a.Session.CreatedAt
			if b != nil {
				c.BindingID = b.ID
				c.BindingCreatedAt = &b.CreatedAt
			}
		}
		if p.AuthCiphertext == "" {
			return catalogConflict
		}
		if e = oauthAdmitCeremony(tx, now); e != nil {
			return e
		}
		if purpose != "login" {
			c.Reason = in.Reason
		}
		captured = p
		return tx.Create(&c).Error
	})
	if e != nil {
		return nil, oauthError(e)
	}
	if rejected {
		return nil, apperrors.ErrUnauthorized
	}
	client, closeClient, e := s.oauthProtocol(ctx, captured)
	if e != nil {
		s.oauthFailCeremony(ctx, c.ID)
		return nil, e
	}
	defer closeClient()
	authURL, e := client.AuthorizationURL(ctx, oauthprotocol.Authorization{State: state, PKCEVerifier: oauthDerived(cookie, c.ID, "pkce")})
	if e != nil {
		s.oauthFailCeremony(ctx, c.ID)
		return nil, oauthUnavailable
	}
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		p, e := oauthProvider(tx)
		if e != nil {
			return e
		}
		if !oauthCeremonyCurrent(p, c, time.Now().UTC()) {
			return catalogConflict
		}
		var current entity.OAuthCeremony
		if e = tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "id"}, c.ID)).Take(&current).Error; e != nil {
			return e
		}
		if current.Status != "pending" || current.CookieHash != c.CookieHash || current.StateHash != c.StateHash {
			return catalogConflict
		}
		if c.Purpose != "login" {
			u, e := oauthOriginalSession(tx, c)
			if e != nil {
				return e
			}
			if c.Purpose == "verify" {
				return registrationPolicyAuthority(tx, u.ID)
			}
		}
		return ctx.Err()
	})
	if e != nil {
		s.oauthFailCeremony(ctx, c.ID)
		return nil, oauthError(e)
	}
	return &OAuthStart{AuthorizationURL: authURL, Cookie: cookie, ExpiresAt: c.ExpiresAt}, nil
}
func (s *Service) StartOAuthLogin(ctx context.Context) (*OAuthStart, error) {
	return s.startOAuth(ctx, nil, "", OAuthIdentityInput{}, "login")
}
func (s *Service) StartOAuthBinding(ctx context.Context, a *Authentication, etag string, in OAuthIdentityInput) (*OAuthStart, error) {
	return s.startOAuth(ctx, a, etag, in, "bind")
}
func (s *Service) StartOAuthVerification(ctx context.Context, a *Authentication, etag string, in OAuthIdentityInput) (*OAuthStart, error) {
	return s.startOAuth(ctx, a, etag, in, "verify")
}
func (s *Service) oauthFailCeremony(ctx context.Context, cid string) {
	_ = s.authDB(ctx).Model(&entity.OAuthCeremony{}).Where(database.ExactText(s.authDB(ctx), clause.Column{Name: "id"}, cid)).Where("status IN ?", []string{"pending", "exchanging", "verified"}).Update("status", "failed").Error
}
func (s *Service) ReceiveOAuthCallback(ctx context.Context, cookie, state, code, remoteError string) error {
	if !oauthBrowserValue(cookie) || !oauthBrowserValue(state) || (code == "") == (remoteError == "") || len(code) > 4096 {
		return apperrors.ErrUnauthorized
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var c entity.OAuthCeremony
	var p entity.OAuthProvider
	e := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		var e error
		p, e = oauthProvider(tx)
		if e != nil {
			return e
		}
		e = tx.Session(&gorm.Session{NewDB: true}).Where("state_hash = ? AND cookie_hash = ?", secret.SHA256Hex(state), secret.SHA256Hex(cookie)).Take(&c).Error
		if errors.Is(e, gorm.ErrRecordNotFound) {
			return apperrors.ErrUnauthorized
		}
		if e != nil {
			return e
		}
		if c.Status != "pending" || !oauthCeremonyCurrent(p, c, time.Now().UTC()) {
			return apperrors.ErrUnauthorized
		}
		if c.Purpose != "login" {
			u, e := oauthOriginalSession(tx, c)
			if e != nil {
				return e
			}
			if c.Purpose == "verify" {
				if e = registrationPolicyAuthority(tx, u.ID); e != nil {
					return e
				}
			}
		}
		changed := tx.Session(&gorm.Session{NewDB: true}).Model(&entity.OAuthCeremony{}).Where(database.ExactText(tx, clause.Column{Name: "id"}, c.ID)).Where("status = ? AND state_hash = ? AND cookie_hash = ?", "pending", c.StateHash, c.CookieHash).Update("status", "exchanging")
		if changed.Error != nil {
			return changed.Error
		}
		if changed.RowsAffected != 1 {
			return apperrors.ErrUnauthorized
		}
		c.Status = "exchanging"
		return nil
	})
	if e != nil {
		return oauthError(e)
	}
	if remoteError != "" {
		s.oauthFailCeremony(ctx, c.ID)
		return apperrors.ErrUnauthorized
	}
	client, closeClient, e := s.oauthProtocol(ctx, p)
	if e != nil {
		s.oauthFailCeremony(ctx, c.ID)
		return e
	}
	defer closeClient()
	identity, e := client.Exchange(ctx, oauthprotocol.Callback{Code: code, State: state, ExpectedState: state, PKCEVerifier: oauthDerived(cookie, c.ID, "pkce")})
	if e != nil {
		s.oauthFailCeremony(ctx, c.ID)
		if errors.Is(e, oauthprotocol.ErrUnavailable) {
			return oauthUnavailable
		}
		return apperrors.ErrUnauthorized
	}
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		p, e := oauthProvider(tx)
		if e != nil {
			return e
		}
		if !oauthCeremonyCurrent(p, c, time.Now().UTC()) {
			return apperrors.ErrUnauthorized
		}
		if c.Purpose != "login" {
			u, e := oauthOriginalSession(tx, c)
			if e != nil {
				return e
			}
			if c.Purpose == "verify" {
				if e = registrationPolicyAuthority(tx, u.ID); e != nil {
					return e
				}
			}
		}
		var current entity.OAuthCeremony
		if e = tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "id"}, c.ID)).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&current).Error; e != nil {
			return e
		}
		if current.Status != "exchanging" || current.CookieHash != c.CookieHash || current.StateHash != c.StateHash {
			return apperrors.ErrUnauthorized
		}
		if !oauthSubject(string(identity.Kind), identity.Subject) {
			return apperrors.ErrUnauthorized
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		current.SubjectKind = string(identity.Kind)
		current.Subject = identity.Subject
		current.Status = "verified"
		current.VerifiedAt = &now
		return tx.Save(&current).Error
	})
	if e != nil {
		s.oauthFailCeremony(ctx, c.ID)
	}
	return oauthError(e)
}

// Call only while holding governance. Expired rows can never be admitted,
// regardless of how many bounded cleanup batches remain. All unexpired rows
// count, including terminal rows, so failure cannot bypass the retained cap.
func oauthAdmitCeremony(tx *gorm.DB, now time.Time) error {
	type expiryRow struct {
		ID        string
		ExpiresAt time.Time
	}
	var expired []expiryRow
	e := tx.Session(&gorm.Session{NewDB: true}).Model(&entity.OAuthCeremony{}).Select("id", "expires_at").Where("expires_at <= ?", now).Order("expires_at ASC").Limit(oauthCeremonyPruneLimit).Find(&expired).Error
	if e != nil {
		return e
	}
	if len(expired) > oauthCeremonyPruneLimit {
		return oauthUnavailable
	}
	for _, row := range expired {
		if row.ID == "" || row.ExpiresAt.After(now) {
			return oauthUnavailable
		}
		if e = tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "id"}, row.ID)).Where("expires_at <= ?", now).Delete(&entity.OAuthCeremony{}).Error; e != nil {
			return e
		}
	}
	var live []expiryRow
	e = tx.Session(&gorm.Session{NewDB: true}).Model(&entity.OAuthCeremony{}).Select("id", "expires_at").Where("expires_at > ?", now).Order("expires_at ASC").Limit(oauthCeremonyLiveLimit).Find(&live).Error
	if e != nil {
		return e
	}
	for _, row := range live {
		if row.ID == "" || !row.ExpiresAt.After(now) {
			return oauthUnavailable
		}
	}
	if len(live) >= oauthCeremonyLiveLimit {
		return oauthUnavailable
	}
	return tx.Statement.Context.Err()
}
