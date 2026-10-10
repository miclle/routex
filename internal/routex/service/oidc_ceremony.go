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
	oidcprotocol "github.com/miclle/routex/pkg/oidc"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/upstream"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const oidcCeremonyLifetime = 5 * time.Minute
const oidcCeremonyLiveLimit = 1024
const oidcCeremonyPruneLimit = 128

type OIDCStart struct {
	AuthorizationURL string    `json:"authorization_url"`
	Cookie           string    `json:"-"`
	ExpiresAt        time.Time `json:"-"`
}

func oidcBrowserValue(v string) bool {
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
func oidcDerived(cookie, ceremonyID, domain string) string {
	sum := sha256.Sum256([]byte("routex-oidc:" + domain + ":" + cookie + ":" + ceremonyID))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
func (s *Service) oidcProtocol(ctx context.Context, p entity.OIDCProvider) (*oidcprotocol.Client, func(), error) {
	client := upstream.NewNonReplayingClient(s.allowPrivateUpstream)
	closeClient := client.CloseIdleConnections
	plaintext, e := s.openSecret(rootReference("oidc_providers", p.ID, p.SecretGeneration), p.AuthCiphertext)
	if e != nil {
		closeClient()
		return nil, func() {}, oidcUnavailable
	}
	c, e := oidcprotocol.Discover(ctx, oidcprotocol.Config{Issuer: p.Issuer, ClientID: p.ClientID, ClientSecret: plaintext, RedirectURL: p.CallbackURL, Transport: client.Transport, EndpointPolicy: func(c context.Context, u *url.URL) error {
		if e := c.Err(); e != nil {
			return e
		}
		if u == nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.Opaque != "" {
			return apperrors.ErrBadRequest
		}
		v := *u
		v.RawQuery = ""
		v.ForceQuery = false
		_, e := upstream.ValidateBaseURL(v.String(), s.allowPrivateUpstream)
		return e
	}})
	if e != nil {
		closeClient()
		return nil, func() {}, oidcUnavailable
	}
	return c, closeClient, nil
}
func oidcCeremonyCurrent(p entity.OIDCProvider, c entity.OIDCCeremony, now time.Time) bool {
	return c.ID != "" && !c.CreatedAt.IsZero() && !c.CreatedAt.After(now) && c.ExpiresAt.After(now) && c.ExpiresAt.Sub(c.CreatedAt) <= oidcCeremonyLifetime && c.ConfigRevision == p.ConfigRevision && c.PolicyRevision == p.PolicyRevision && (c.Purpose == "verify" || p.Enabled && oidcVerified(p)) && (c.Purpose == "login" || c.Purpose == "bind" || c.Purpose == "verify")
}
func oidcOriginalSession(tx *gorm.DB, c entity.OIDCCeremony) (entity.User, error) {
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
	if e = oidcValidatePrimary(tx, row); e != nil {
		return u, e
	}
	m, e := mfaState(tx, u.ID)
	if e != nil {
		return u, e
	}
	if m.Generation != c.MFAGeneration {
		return u, apperrors.ErrUnauthorized
	}
	if e = oidcCapturedBinding(tx, c, u); e != nil {
		return u, e
	}
	return u, nil
}
func (s *Service) startOIDC(ctx context.Context, a *Authentication, etag string, in OIDCIdentityInput, purpose string) (*OIDCStart, error) {
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
		return nil, oidcUnavailable
	}
	cookie, e := secret.RandomURLSafe(32)
	if e != nil {
		return nil, oidcUnavailable
	}
	cid, e := id.NewPrefixed("oic")
	if e != nil {
		return nil, oidcUnavailable
	}
	var captured entity.OIDCProvider
	var c entity.OIDCCeremony
	rejected := false
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		p, e := oidcProvider(tx)
		if e != nil {
			return e
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		c = entity.OIDCCeremony{ID: cid, StateHash: secret.SHA256Hex(state), CookieHash: secret.SHA256Hex(cookie), Purpose: purpose, Status: "pending", ConfigRevision: p.ConfigRevision, PolicyRevision: p.PolicyRevision, CreatedAt: now, ExpiresAt: now.Add(oidcCeremonyLifetime)}
		if purpose != "verify" && (!p.Enabled || !oidcVerified(p)) {
			return apperrors.ErrUnauthorized
		}
		if purpose != "login" {
			u, m, bad, e := s.oidcLocalProof(tx, a, passwordHash, in.Proof)
			if e != nil {
				return e
			}
			if bad {
				rejected = true
				return nil
			}
			b, e := oidcUserBinding(tx, u)
			if e != nil {
				return e
			}
			if purpose == "verify" {
				if e = registrationPolicyAuthority(tx, u.ID); e != nil {
					return e
				}
				if oidcReview(p, u, m) != etag {
					return catalogConflict
				}
			} else if oidcAccountReview(p, u, m, a, b) != etag {
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
		if e = oidcAdmitCeremony(tx, now); e != nil {
			return e
		}
		if purpose != "login" {
			c.Reason = in.Reason
		}
		captured = p
		return tx.Create(&c).Error
	})
	if e != nil {
		return nil, oidcError(e)
	}
	if rejected {
		return nil, apperrors.ErrUnauthorized
	}
	client, closeClient, e := s.oidcProtocol(ctx, captured)
	if e != nil {
		s.oidcFailCeremony(ctx, c.ID)
		return nil, e
	}
	defer closeClient()
	authURL, e := client.AuthorizationURL(ctx, oidcprotocol.Authorization{State: state, Nonce: oidcDerived(cookie, c.ID, "nonce"), PKCEVerifier: oidcDerived(cookie, c.ID, "pkce")})
	if e != nil {
		s.oidcFailCeremony(ctx, c.ID)
		return nil, oidcUnavailable
	}
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		p, e := oidcProvider(tx)
		if e != nil {
			return e
		}
		if !oidcCeremonyCurrent(p, c, time.Now().UTC()) {
			return catalogConflict
		}
		var current entity.OIDCCeremony
		if e = tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "id"}, c.ID)).Take(&current).Error; e != nil {
			return e
		}
		if current.Status != "pending" || current.CookieHash != c.CookieHash || current.StateHash != c.StateHash {
			return catalogConflict
		}
		if c.Purpose != "login" {
			u, e := oidcOriginalSession(tx, c)
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
		s.oidcFailCeremony(ctx, c.ID)
		return nil, oidcError(e)
	}
	return &OIDCStart{AuthorizationURL: authURL, Cookie: cookie, ExpiresAt: c.ExpiresAt}, nil
}
func (s *Service) StartOIDCLogin(ctx context.Context) (*OIDCStart, error) {
	return s.startOIDC(ctx, nil, "", OIDCIdentityInput{}, "login")
}
func (s *Service) StartOIDCBinding(ctx context.Context, a *Authentication, etag string, in OIDCIdentityInput) (*OIDCStart, error) {
	return s.startOIDC(ctx, a, etag, in, "bind")
}
func (s *Service) StartOIDCVerification(ctx context.Context, a *Authentication, etag string, in OIDCIdentityInput) (*OIDCStart, error) {
	return s.startOIDC(ctx, a, etag, in, "verify")
}
func (s *Service) oidcFailCeremony(ctx context.Context, cid string) {
	_ = s.authDB(ctx).Model(&entity.OIDCCeremony{}).Where(database.ExactText(s.authDB(ctx), clause.Column{Name: "id"}, cid)).Where("status IN ?", []string{"pending", "exchanging", "verified"}).Update("status", "failed").Error
}
func (s *Service) ReceiveOIDCCallback(ctx context.Context, cookie, state, code, remoteError, responseIssuer string) error {
	if !oidcBrowserValue(cookie) || !oidcBrowserValue(state) || (code == "") == (remoteError == "") || len(code) > 4096 {
		return apperrors.ErrUnauthorized
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var c entity.OIDCCeremony
	var p entity.OIDCProvider
	e := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		var e error
		p, e = oidcProvider(tx)
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
		if c.Status != "pending" || !oidcCeremonyCurrent(p, c, time.Now().UTC()) || responseIssuer != "" && responseIssuer != p.Issuer {
			return apperrors.ErrUnauthorized
		}
		if c.Purpose != "login" {
			u, e := oidcOriginalSession(tx, c)
			if e != nil {
				return e
			}
			if c.Purpose == "verify" {
				if e = registrationPolicyAuthority(tx, u.ID); e != nil {
					return e
				}
			}
		}
		changed := tx.Session(&gorm.Session{NewDB: true}).Model(&entity.OIDCCeremony{}).Where(database.ExactText(tx, clause.Column{Name: "id"}, c.ID)).Where("status = ? AND state_hash = ? AND cookie_hash = ?", "pending", c.StateHash, c.CookieHash).Update("status", "exchanging")
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
		return oidcError(e)
	}
	if remoteError != "" {
		s.oidcFailCeremony(ctx, c.ID)
		return apperrors.ErrUnauthorized
	}
	client, closeClient, e := s.oidcProtocol(ctx, p)
	if e != nil {
		s.oidcFailCeremony(ctx, c.ID)
		return e
	}
	defer closeClient()
	identity, e := client.Exchange(ctx, oidcprotocol.Callback{Code: code, State: state, ExpectedState: state, ExpectedNonce: oidcDerived(cookie, c.ID, "nonce"), PKCEVerifier: oidcDerived(cookie, c.ID, "pkce")})
	if e != nil {
		s.oidcFailCeremony(ctx, c.ID)
		if errors.Is(e, oidcprotocol.ErrUnavailable) {
			return oidcUnavailable
		}
		return apperrors.ErrUnauthorized
	}
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		p, e := oidcProvider(tx)
		if e != nil {
			return e
		}
		if !oidcCeremonyCurrent(p, c, time.Now().UTC()) || identity.Issuer != p.Issuer {
			return apperrors.ErrUnauthorized
		}
		if c.Purpose != "login" {
			u, e := oidcOriginalSession(tx, c)
			if e != nil {
				return e
			}
			if c.Purpose == "verify" {
				if e = registrationPolicyAuthority(tx, u.ID); e != nil {
					return e
				}
			}
		}
		var current entity.OIDCCeremony
		if e = tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "id"}, c.ID)).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&current).Error; e != nil {
			return e
		}
		if current.Status != "exchanging" || current.CookieHash != c.CookieHash || current.StateHash != c.StateHash {
			return apperrors.ErrUnauthorized
		}
		if identity.Subject == "" || len(identity.Subject) > 255 {
			return apperrors.ErrUnauthorized
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		current.Subject = identity.Subject
		current.Status = "verified"
		current.VerifiedAt = &now
		return tx.Save(&current).Error
	})
	if e != nil {
		s.oidcFailCeremony(ctx, c.ID)
	}
	return oidcError(e)
}

// Call only while holding governance. Expired rows can never be admitted,
// regardless of how many bounded cleanup batches remain. All unexpired rows
// count, including terminal rows, so failure cannot bypass the retained cap.
func oidcAdmitCeremony(tx *gorm.DB, now time.Time) error {
	type expiryRow struct {
		ID        string
		ExpiresAt time.Time
	}
	var expired []expiryRow
	e := tx.Session(&gorm.Session{NewDB: true}).Model(&entity.OIDCCeremony{}).Select("id", "expires_at").Where("expires_at <= ?", now).Order("expires_at ASC").Limit(oidcCeremonyPruneLimit).Find(&expired).Error
	if e != nil {
		return e
	}
	if len(expired) > oidcCeremonyPruneLimit {
		return oidcUnavailable
	}
	for _, row := range expired {
		if row.ID == "" || row.ExpiresAt.After(now) {
			return oidcUnavailable
		}
		if e = tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "id"}, row.ID)).Where("expires_at <= ?", now).Delete(&entity.OIDCCeremony{}).Error; e != nil {
			return e
		}
	}
	var live []expiryRow
	e = tx.Session(&gorm.Session{NewDB: true}).Model(&entity.OIDCCeremony{}).Select("id", "expires_at").Where("expires_at > ?", now).Order("expires_at ASC").Limit(oidcCeremonyLiveLimit).Find(&live).Error
	if e != nil {
		return e
	}
	for _, row := range live {
		if row.ID == "" || !row.ExpiresAt.After(now) {
			return oidcUnavailable
		}
	}
	if len(live) >= oidcCeremonyLiveLimit {
		return oidcUnavailable
	}
	return tx.Statement.Context.Err()
}
