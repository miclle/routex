package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/secret"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const namedIdentityCeremonyLifetime = 5 * time.Minute
const namedIdentityCeremonyLiveLimit = 1024
const namedIdentityCeremonyPruneLimit = 128

type GitHubStart struct {
	AuthorizationURL string    `json:"authorization_url"`
	Cookie           string    `json:"-"`
	ExpiresAt        time.Time `json:"-"`
}

func namedIdentityBrowserValue(v string) bool {
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
func namedIdentityDerived(cookie, ceremonyID, domain string) string {
	return namedIdentityDerivedFor(githubProviderID, cookie, ceremonyID, domain)
}
func namedIdentityDerivedFor(providerID, cookie, ceremonyID, domain string) string {
	d, ok := namedIdentityDescriptor(providerID)
	if !ok {
		return ""
	}
	sum := sha256.Sum256([]byte("routex-named-identity:" + d.profile + ":" + domain + ":" + cookie + ":" + ceremonyID))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func namedIdentityCeremonyCurrent(p entity.NamedIdentityProvider, c entity.NamedIdentityCeremony, now time.Time) bool {
	return namedIdentityProfile(p.ID, p.ProfileID, p.IdentityIssuer) && c.ProviderID == p.ID && c.ProfileID == p.ProfileID && c.IdentityIssuer == p.IdentityIssuer && c.ProviderCreatedAt.Equal(p.CreatedAt) && c.ID != "" && !c.CreatedAt.IsZero() && !c.CreatedAt.After(now) && c.ExpiresAt.After(now) && c.ExpiresAt.Sub(c.CreatedAt) <= namedIdentityCeremonyLifetime && c.ConfigRevision == p.ConfigRevision && c.PolicyRevision == p.PolicyRevision && (c.Purpose == "verify" || p.Enabled && namedIdentityVerified(p)) && (c.Purpose == "login" || c.Purpose == "bind" || c.Purpose == "verify")
}
func namedIdentityOriginalSession(tx *gorm.DB, c entity.NamedIdentityCeremony) (entity.User, error) {
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
	if e = namedIdentityCapturedBinding(tx, c, u); e != nil {
		return u, e
	}
	return u, nil
}
func (s *Service) startNamedIdentity(ctx context.Context, a *Authentication, etag string, in GitHubIdentityInput, purpose, providerID string) (*GitHubStart, error) {
	admitted := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	localCtx, localCancel := namedIdentityLocalContext(ctx, admitted)
	defer localCancel()
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
		passwordHash, e = s.mfaPassword(localCtx, a.User.ID, in.Password)
		if e != nil {
			return nil, e
		}
	}
	state, e := secret.RandomURLSafe(32)
	if e != nil {
		return nil, namedIdentityUnavailable
	}
	cookie, e := secret.RandomURLSafe(32)
	if e != nil {
		return nil, namedIdentityUnavailable
	}
	cid, e := id.NewPrefixed("nic")
	if e != nil {
		return nil, namedIdentityUnavailable
	}
	var captured entity.NamedIdentityProvider
	var c entity.NamedIdentityCeremony
	rejected := false
	e = s.authDB(localCtx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		p, e := namedIdentityProvider(tx, providerID)
		if e != nil {
			return e
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		c = entity.NamedIdentityCeremony{ID: cid, ProviderID: p.ID, ProfileID: p.ProfileID, IdentityIssuer: p.IdentityIssuer, ProviderCreatedAt: p.CreatedAt, StateHash: secret.SHA256Hex(state), CookieHash: secret.SHA256Hex(cookie), Purpose: purpose, Status: "pending", ConfigRevision: p.ConfigRevision, PolicyRevision: p.PolicyRevision, CreatedAt: now, ExpiresAt: now.Add(namedIdentityCeremonyLifetime)}
		if purpose != "verify" && (!p.Enabled || !namedIdentityVerified(p)) {
			return apperrors.ErrUnauthorized
		}
		if purpose != "login" {
			u, m, bad, e := s.namedIdentityLocalProof(tx, a, passwordHash, in.Proof)
			if e != nil {
				return e
			}
			if bad {
				rejected = true
				return nil
			}
			b, e := namedIdentityUserBinding(tx, u, providerID)
			if e != nil {
				return e
			}
			if purpose == "verify" {
				if e = registrationPolicyAuthority(tx, u.ID); e != nil {
					return e
				}
				if namedIdentityReview(p, u, m) != etag {
					return catalogConflict
				}
			} else if namedIdentityAccountReview(p, u, m, a, b) != etag {
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
		if e = namedIdentityAdmitCeremony(tx, now); e != nil {
			return e
		}
		if purpose != "login" {
			c.Reason = in.Reason
		}
		captured = p
		return tx.Create(&c).Error
	})
	if e != nil {
		return nil, namedIdentityError(e)
	}
	if rejected {
		return nil, apperrors.ErrUnauthorized
	}
	client, closeClient, e := s.namedIdentityProfileProtocol(ctx, captured)
	if e != nil {
		s.namedIdentityFailCeremony(localCtx, c.ID)
		return nil, e
	}
	// The explicit close below gates success; this is only fallback cleanup.
	defer func() { _ = closeClient() }()
	authURL, e := client.AuthorizationURL(ctx, namedIdentityAuthorization{State: state, Nonce: namedIdentityDerivedFor(providerID, cookie, c.ID, "nonce"), PKCEVerifier: namedIdentityDerivedFor(providerID, cookie, c.ID, "pkce")})
	if closeErr := closeClient(); closeErr != nil {
		e = namedIdentityUnavailable
	}
	if e != nil {
		s.namedIdentityFailCeremony(localCtx, c.ID)
		return nil, namedIdentityUnavailable
	}
	e = s.authDB(localCtx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		p, e := namedIdentityProvider(tx, providerID)
		if e != nil {
			return e
		}
		if !namedIdentityCeremonyCurrent(p, c, time.Now().UTC()) {
			return catalogConflict
		}
		var current entity.NamedIdentityCeremony
		if e = tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "id"}, c.ID)).Take(&current).Error; e != nil {
			return e
		}
		if !namedIdentityCeremonyCurrent(p, current, time.Now().UTC()) || current.Status != "pending" || current.CookieHash != c.CookieHash || current.StateHash != c.StateHash {
			return catalogConflict
		}
		if c.Purpose != "login" {
			u, e := namedIdentityOriginalSession(tx, c)
			if e != nil {
				return e
			}
			if c.Purpose == "verify" {
				if e := registrationPolicyAuthority(tx, u.ID); e != nil {
					return e
				}
				return localCtx.Err()
			}
		}
		return localCtx.Err()
	})
	if e != nil {
		s.namedIdentityFailCeremony(localCtx, c.ID)
		return nil, namedIdentityError(e)
	}
	return &GitHubStart{AuthorizationURL: authURL, Cookie: cookie, ExpiresAt: c.ExpiresAt}, nil
}
func (s *Service) StartGitHubLogin(ctx context.Context) (*GitHubStart, error) {
	return s.startNamedIdentity(ctx, nil, "", GitHubIdentityInput{}, "login", githubProviderID)
}
func (s *Service) StartGitHubBinding(ctx context.Context, a *Authentication, etag string, in GitHubIdentityInput) (*GitHubStart, error) {
	return s.startNamedIdentity(ctx, a, etag, in, "bind", githubProviderID)
}
func (s *Service) StartGitHubVerification(ctx context.Context, a *Authentication, etag string, in GitHubIdentityInput) (*GitHubStart, error) {
	return s.startNamedIdentity(ctx, a, etag, in, "verify", githubProviderID)
}
func (s *Service) namedIdentityFailCeremony(ctx context.Context, cid string) {
	_ = s.authDB(ctx).Model(&entity.NamedIdentityCeremony{}).Where(database.ExactText(s.authDB(ctx), clause.Column{Name: "id"}, cid)).Where("status IN ?", []string{"pending", "exchanging", "verified"}).Update("status", "failed").Error
}
func (s *Service) receiveNamedIdentityCallback(ctx context.Context, cookie, state, code, remoteError, responseIssuer, providerID string) error {
	if providerID == googleProviderID && responseIssuer != googleIdentityIssuer {
		return apperrors.ErrUnauthorized
	}
	if !namedIdentityBrowserValue(cookie) || !namedIdentityBrowserValue(state) || (code == "") == (remoteError == "") || len(code) > 4096 {
		return apperrors.ErrUnauthorized
	}
	admitted := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	localCtx, localCancel := namedIdentityLocalContext(ctx, admitted)
	defer localCancel()
	defer cancel()
	var c entity.NamedIdentityCeremony
	var p entity.NamedIdentityProvider
	e := s.authDB(localCtx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		var e error
		p, e = namedIdentityProvider(tx, providerID)
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
		if c.Status != "pending" || !namedIdentityCeremonyCurrent(p, c, time.Now().UTC()) {
			return apperrors.ErrUnauthorized
		}
		if c.Purpose != "login" {
			u, e := namedIdentityOriginalSession(tx, c)
			if e != nil {
				return e
			}
			if c.Purpose == "verify" {
				if e = registrationPolicyAuthority(tx, u.ID); e != nil {
					return e
				}
			}
		}
		changed := tx.Session(&gorm.Session{NewDB: true}).Model(&entity.NamedIdentityCeremony{}).Where(database.ExactText(tx, clause.Column{Name: "id"}, c.ID)).Where("status = ? AND state_hash = ? AND cookie_hash = ?", "pending", c.StateHash, c.CookieHash).Update("status", "exchanging")
		if changed.Error != nil {
			return changed.Error
		}
		if changed.RowsAffected != 1 {
			return apperrors.ErrUnauthorized
		}
		c.Status = "exchanging"
		return localCtx.Err()
	})
	if e != nil {
		return namedIdentityError(e)
	}
	if remoteError != "" {
		s.namedIdentityFailCeremony(localCtx, c.ID)
		return apperrors.ErrUnauthorized
	}
	client, closeClient, e := s.namedIdentityProfileProtocol(ctx, p)
	if e != nil {
		s.namedIdentityFailCeremony(localCtx, c.ID)
		return e
	}
	// The explicit close below gates success; this is only fallback cleanup.
	defer func() { _ = closeClient() }()
	identity, e := client.Exchange(ctx, namedIdentityCallback{Code: code, State: state, ExpectedState: state, ExpectedNonce: namedIdentityDerivedFor(providerID, cookie, c.ID, "nonce"), PKCEVerifier: namedIdentityDerivedFor(providerID, cookie, c.ID, "pkce")})
	if closeErr := closeClient(); closeErr != nil {
		e = namedIdentityUnavailable
	}
	if e != nil {
		s.namedIdentityFailCeremony(localCtx, c.ID)
		if errors.Is(e, namedIdentityUnavailable) {
			return namedIdentityUnavailable
		}
		return apperrors.ErrUnauthorized
	}
	e = s.authDB(localCtx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		p, e := namedIdentityProvider(tx, providerID)
		if e != nil {
			return e
		}
		if !namedIdentityCeremonyCurrent(p, c, time.Now().UTC()) {
			return apperrors.ErrUnauthorized
		}
		if c.Purpose != "login" {
			u, e := namedIdentityOriginalSession(tx, c)
			if e != nil {
				return e
			}
			if c.Purpose == "verify" {
				if e = registrationPolicyAuthority(tx, u.ID); e != nil {
					return e
				}
			}
		}
		var loginUser entity.User
		var loginBinding entity.NamedIdentityBinding
		if c.Purpose == "login" {
			digest := namedIdentitySubjectDigest(p.ID, identity.Kind, identity.Subject)
			var bound entity.NamedIdentityBinding
			if e = tx.Session(&gorm.Session{NewDB: true}).Where("subject_digest = ?", digest).Take(&bound).Error; e != nil {
				return apperrors.ErrUnauthorized
			}
			if bound.ProviderID != p.ID || !namedIdentityProfile(bound.ProviderID, bound.ProfileID, bound.IdentityIssuer) || bound.SubjectKind != identity.Kind || bound.Subject != identity.Subject || bound.SubjectDigest != digest || bound.ConfigRevision != p.ConfigRevision {
				return apperrors.ErrUnauthorized
			}
			u, err := registrationAdmittedUser(tx, bound.UserID, true)
			if err != nil {
				return err
			}
			if !u.CreatedAt.Equal(bound.UserCreatedAt) || bound.CreatedAt.IsZero() {
				return apperrors.ErrUnauthorized
			}
			loginUser = u
			loginBinding = bound
		}
		var current entity.NamedIdentityCeremony
		if e = tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "id"}, c.ID)).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&current).Error; e != nil {
			return e
		}
		if !namedIdentityCeremonyCurrent(p, current, time.Now().UTC()) || current.Status != "exchanging" || current.CookieHash != c.CookieHash || current.StateHash != c.StateHash {
			return apperrors.ErrUnauthorized
		}
		if !namedIdentityProfileSubject(p.ID, identity.Kind, identity.Subject) {
			return apperrors.ErrUnauthorized
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		if c.Purpose == "login" {
			current.UserID = loginUser.ID
			current.UserCreatedAt = &loginUser.CreatedAt
			current.BindingID = loginBinding.ID
			current.BindingCreatedAt = &loginBinding.CreatedAt
		}

		current.SubjectKind = identity.Kind
		current.Subject = identity.Subject
		current.Status = "verified"
		current.VerifiedAt = &now
		if localCtx.Err() != nil {
			return localCtx.Err()
		}
		return tx.Save(&current).Error
	})
	if e != nil {
		s.namedIdentityFailCeremony(localCtx, c.ID)
	}
	return namedIdentityError(e)
}

// Call only while holding governance. Expired rows can never be admitted,
// regardless of how many bounded cleanup batches remain. All unexpired rows
// count, including terminal rows, so failure cannot bypass the retained cap.
func namedIdentityAdmitCeremony(tx *gorm.DB, now time.Time) error {
	type expiryRow struct {
		ID        string
		ExpiresAt time.Time
	}
	var expired []expiryRow
	e := tx.Session(&gorm.Session{NewDB: true}).Model(&entity.NamedIdentityCeremony{}).Select("id", "expires_at").Where("expires_at <= ?", now).Order("expires_at ASC").Limit(namedIdentityCeremonyPruneLimit).Find(&expired).Error
	if e != nil {
		return e
	}
	if len(expired) > namedIdentityCeremonyPruneLimit {
		return namedIdentityUnavailable
	}
	for _, row := range expired {
		if row.ID == "" || row.ExpiresAt.After(now) {
			return namedIdentityUnavailable
		}
		if e = tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "id"}, row.ID)).Where("expires_at <= ?", now).Delete(&entity.NamedIdentityCeremony{}).Error; e != nil {
			return e
		}
	}
	var live []expiryRow
	e = tx.Session(&gorm.Session{NewDB: true}).Model(&entity.NamedIdentityCeremony{}).Select("id", "expires_at").Where("expires_at > ?", now).Order("expires_at ASC").Limit(namedIdentityCeremonyLiveLimit).Find(&live).Error
	if e != nil {
		return e
	}
	for _, row := range live {
		if row.ID == "" || !row.ExpiresAt.After(now) {
			return namedIdentityUnavailable
		}
	}
	if len(live) >= namedIdentityCeremonyLiveLimit {
		return namedIdentityUnavailable
	}
	return tx.Statement.Context.Err()
}

// Every local phase shares one admission deadline; remote work never renews it.
func namedIdentityLocalContext(parent context.Context, admitted time.Time) (context.Context, context.CancelFunc) {
	return context.WithDeadline(parent, admitted.Add(5*time.Second))
}
