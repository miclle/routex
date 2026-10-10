package service

import (
	"context"
	"encoding/base64"
	"errors"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	samlprotocol "github.com/miclle/routex/pkg/saml"
	"github.com/miclle/routex/pkg/secret"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const samlCeremonyLifetime = 5 * time.Minute
const samlCeremonyLiveLimit = 1024
const samlCeremonyPruneLimit = 128

// Capture once at operation admission and reuse across all local phases,
// including failure cleanup. Signature work retains the outer operation budget.
func samlLocalContext(ctx context.Context, admitted time.Time) (context.Context, context.CancelFunc) {
	return context.WithDeadline(ctx, admitted.Add(5*time.Second))
}

type SAMLStart struct {
	AuthorizationURL string    `json:"authorization_url"`
	Cookie           string    `json:"-"`
	ExpiresAt        time.Time `json:"-"`
}
type SAMLDelivery struct {
	Cookie    string    `json:"-"`
	ExpiresAt time.Time `json:"-"`
}

func samlBrowserValue(v string) bool {
	b, e := base64.RawURLEncoding.Strict().DecodeString(v)
	return e == nil && len(b) == 32 && len(v) == 43 && base64.RawURLEncoding.EncodeToString(b) == v
}
func samlCeremonyCurrent(p entity.SAMLProvider, c entity.SAMLCeremony, now time.Time) bool {
	return c.ID != "" && !c.CreatedAt.IsZero() && !c.CreatedAt.After(now) && c.ExpiresAt.After(now) && c.ExpiresAt.Sub(c.CreatedAt) <= samlCeremonyLifetime && c.ProviderCreatedAt.Equal(p.CreatedAt) && c.ConfigRevision == p.ConfigRevision && c.PolicyRevision == p.PolicyRevision && (c.Purpose == "verify" || p.Enabled && samlVerified(p)) && (c.Purpose == "login" || c.Purpose == "bind" || c.Purpose == "verify")
}
func samlOriginalSession(tx *gorm.DB, c entity.SAMLCeremony) (entity.User, error) {
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
	if e = samlCapturedBinding(tx, c, u); e != nil {
		return u, e
	}
	return u, nil
}
func (s *Service) startSAML(ctx context.Context, a *Authentication, etag string, in SAMLIdentityInput, purpose string) (*SAMLStart, error) {
	admitted := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	localCtx, localCancel := samlLocalContext(ctx, admitted)
	defer localCancel()
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
	relay, e := secret.RandomURLSafe(32)
	if e != nil {
		return nil, samlUnavailable
	}
	cookie, e := secret.RandomURLSafe(32)
	if e != nil {
		return nil, samlUnavailable
	}
	cid, e := id.NewPrefixed("smc")
	if e != nil {
		return nil, samlUnavailable
	}
	request, e := secret.RandomURLSafe(32)
	if e != nil {
		return nil, samlUnavailable
	}
	var captured entity.SAMLProvider
	var c entity.SAMLCeremony
	rejected := false
	e = s.authDB(localCtx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		p, e := samlProvider(tx)
		if e != nil {
			return e
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		c = entity.SAMLCeremony{ID: cid, RequestID: "_" + request, RelayHash: secret.SHA256Hex(relay), ProviderCreatedAt: p.CreatedAt, CookieHash: secret.SHA256Hex(cookie), Purpose: purpose, Status: "pending", ConfigRevision: p.ConfigRevision, PolicyRevision: p.PolicyRevision, CreatedAt: now, ExpiresAt: now.Add(samlCeremonyLifetime)}
		if purpose != "verify" && (!p.Enabled || !samlVerified(p)) {
			return apperrors.ErrUnauthorized
		}
		if purpose != "login" {
			u, m, bad, e := s.samlLocalProof(tx, a, passwordHash, in.Proof)
			if e != nil {
				return e
			}
			if bad {
				rejected = true
				return nil
			}
			b, e := samlUserBinding(tx, u)
			if e != nil {
				return e
			}
			if purpose == "verify" {
				if e = registrationPolicyAuthority(tx, u.ID); e != nil {
					return e
				}
				if samlReview(p, u, m) != etag {
					return catalogConflict
				}
			} else if samlAccountReview(p, u, m, a, b) != etag {
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
		if p.SigningCertificatePEM == "" {
			return catalogConflict
		}
		if e = samlAdmitCeremony(tx, now); e != nil {
			return e
		}
		if purpose != "login" {
			c.Reason = in.Reason
		}
		captured = p
		return tx.Create(&c).Error
	})
	if e != nil {
		return nil, samlError(e)
	}
	if rejected {
		return nil, apperrors.ErrUnauthorized
	}
	client, e := samlProtocol(captured)
	if e != nil {
		s.samlFailCeremony(localCtx, c.ID)
		return nil, e
	}
	authURL, e := client.AuthorizationURL(ctx, samlprotocol.Authorization{RequestID: c.RequestID, RelayState: relay})
	if e != nil {
		s.samlFailCeremony(localCtx, c.ID)
		return nil, samlUnavailable
	}
	e = s.authDB(localCtx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		p, e := samlProvider(tx)
		if e != nil {
			return e
		}
		if !samlCeremonyCurrent(p, c, time.Now().UTC()) {
			return catalogConflict
		}
		var current entity.SAMLCeremony
		if e = tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "id"}, c.ID)).Take(&current).Error; e != nil {
			return e
		}
		if current.Status != "pending" || current.CookieHash != c.CookieHash || current.RelayHash != c.RelayHash || current.RequestID != c.RequestID {
			return catalogConflict
		}
		if c.Purpose != "login" {
			u, e := samlOriginalSession(tx, c)
			if e != nil {
				return e
			}
			if c.Purpose == "verify" {
				return registrationPolicyAuthority(tx, u.ID)
			}
		}
		return localCtx.Err()
	})
	if e != nil {
		s.samlFailCeremony(localCtx, c.ID)
		return nil, samlError(e)
	}
	return &SAMLStart{AuthorizationURL: authURL, Cookie: cookie, ExpiresAt: c.ExpiresAt}, nil
}
func (s *Service) StartSAMLLogin(ctx context.Context) (*SAMLStart, error) {
	return s.startSAML(ctx, nil, "", SAMLIdentityInput{}, "login")
}
func (s *Service) StartSAMLBinding(ctx context.Context, a *Authentication, etag string, in SAMLIdentityInput) (*SAMLStart, error) {
	return s.startSAML(ctx, a, etag, in, "bind")
}
func (s *Service) StartSAMLVerification(ctx context.Context, a *Authentication, etag string, in SAMLIdentityInput) (*SAMLStart, error) {
	return s.startSAML(ctx, a, etag, in, "verify")
}

func (s *Service) samlFailCeremony(ctx context.Context, cid string) {
	_ = s.authDB(ctx).Model(&entity.SAMLCeremony{}).Where(database.ExactText(s.authDB(ctx), clause.Column{Name: "id"}, cid)).Where("status IN ?", []string{"pending", "validating", "verified"}).Update("status", "failed").Error
}

// Timestamp columns retain microseconds. Round replay retention upward, while
// completion admission rounds proof expiry downward, so precision cannot open a
// replay interval just before the signed expiry.
func samlReceiptExpiry(v time.Time) time.Time {
	u := v.UTC()
	rounded := u.Truncate(time.Microsecond)
	if rounded.Before(u) {
		rounded = rounded.Add(time.Microsecond)
	}
	return rounded
}
func samlAssertionDigest(issuer, assertionID string) string {
	return samlDigest("saml.assertion.receipt.v1", "saml", issuer, assertionID)
}

// ACS has no Session or start-cookie authority. It may stage only a verified
// proof; both independent browser secrets are required by same-origin Complete.
func (s *Service) ReceiveSAMLAssertion(ctx context.Context, relay, response string) (*SAMLDelivery, error) {
	if !samlBrowserValue(relay) || response == "" || len(response) > ((samlprotocol.MaxResponseBytes+2)/3)*4 {
		return nil, apperrors.ErrUnauthorized
	}
	admitted := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	localCtx, localCancel := samlLocalContext(ctx, admitted)
	defer localCancel()
	var c entity.SAMLCeremony
	var captured entity.SAMLProvider
	e := s.authDB(localCtx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		p, e := samlProvider(tx)
		if e != nil {
			return e
		}
		e = tx.Session(&gorm.Session{NewDB: true}).Where("relay_hash = ?", secret.SHA256Hex(relay)).Take(&c).Error
		if errors.Is(e, gorm.ErrRecordNotFound) {
			return apperrors.ErrUnauthorized
		}
		if e != nil {
			return e
		}
		if c.Status != "pending" || !samlCeremonyCurrent(p, c, time.Now().UTC()) {
			return apperrors.ErrUnauthorized
		}
		if c.Purpose != "login" {
			u, e := samlOriginalSession(tx, c)
			if e != nil {
				return e
			}
			if c.Purpose == "verify" {
				if e = registrationPolicyAuthority(tx, u.ID); e != nil {
					return e
				}
			}
		}
		changed := tx.Session(&gorm.Session{NewDB: true}).Model(&entity.SAMLCeremony{}).Where(database.ExactText(tx, clause.Column{Name: "id"}, c.ID)).Where("status = ? AND relay_hash = ?", "pending", c.RelayHash).Update("status", "validating")
		if changed.Error != nil {
			return changed.Error
		}
		if changed.RowsAffected != 1 {
			return apperrors.ErrUnauthorized
		}
		c.Status = "validating"
		captured = p
		return localCtx.Err()
	})
	if e != nil {
		return nil, samlError(e)
	}
	client, e := samlProtocol(captured)
	if e != nil {
		s.samlFailCeremony(localCtx, c.ID)
		return nil, e
	}
	identity, e := client.Verify(ctx, samlprotocol.Callback{SAMLResponse: response, RequestID: c.RequestID})
	if e != nil {
		s.samlFailCeremony(localCtx, c.ID)
		if errors.Is(e, samlprotocol.ErrUnavailable) {
			return nil, samlUnavailable
		}
		return nil, apperrors.ErrUnauthorized
	}
	delivery, e := secret.RandomURLSafe(32)
	if e != nil {
		s.samlFailCeremony(localCtx, c.ID)
		return nil, samlUnavailable
	}
	expiry := identity.ExpiresAt.UTC().Truncate(time.Microsecond)
	if c.ExpiresAt.Before(expiry) {
		expiry = c.ExpiresAt
	}
	e = s.authDB(localCtx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		p, e := samlProvider(tx)
		if e != nil {
			return e
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		if !samlCeremonyCurrent(p, c, now) || identity.Issuer != p.IDPIssuer || identity.RequestID != c.RequestID || identity.Subject == "" || identity.AssertionID == "" || !expiry.After(now) {
			return apperrors.ErrUnauthorized
		}
		if c.Purpose != "login" {
			u, e := samlOriginalSession(tx, c)
			if e != nil {
				return e
			}
			if c.Purpose == "verify" {
				if e = registrationPolicyAuthority(tx, u.ID); e != nil {
					return e
				}
			}
		}
		var current entity.SAMLCeremony
		if e = tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "id"}, c.ID)).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&current).Error; e != nil {
			return e
		}
		if current.Status != "validating" || current.RelayHash != c.RelayHash || current.CookieHash != c.CookieHash || current.RequestID != c.RequestID {
			return apperrors.ErrUnauthorized
		}
		if e = samlAdmitAssertionReceipt(tx, now); e != nil {
			return e
		}
		digest := samlAssertionDigest(identity.Issuer, identity.AssertionID)
		// Retain the full verified proof expiry, even if this ceremony expires first.
		receipt := entity.SAMLAssertionReceipt{Digest: digest, ExpiresAt: samlReceiptExpiry(identity.ExpiresAt), CreatedAt: now}
		if e = tx.Create(&receipt).Error; e != nil {
			if errors.Is(e, gorm.ErrDuplicatedKey) {
				return apperrors.ErrUnauthorized
			}
			return e
		}
		current.Subject = identity.Subject
		current.Issuer = identity.Issuer
		current.AssertionDigest = digest
		current.DeliveryHash = secret.SHA256Hex(delivery)
		current.ProofExpiresAt = &expiry
		current.Status = "verified"
		current.VerifiedAt = &now
		if e = tx.Save(&current).Error; e != nil {
			return e
		}
		if !expiry.After(time.Now().UTC()) {
			return apperrors.ErrUnauthorized
		}
		return localCtx.Err()
	})
	if e != nil {
		s.samlFailCeremony(localCtx, c.ID)
		return nil, samlError(e)
	}
	return &SAMLDelivery{Cookie: delivery, ExpiresAt: expiry}, nil
}

// Governance serializes bounded pruning and receipt insertion. Retention never
// depends on security configuration, so a config change cannot reset replay.
func samlAdmitAssertionReceipt(tx *gorm.DB, now time.Time) error {
	var expired []entity.SAMLAssertionReceipt
	if e := tx.Session(&gorm.Session{NewDB: true}).Where("expires_at <= ?", now).Order("expires_at ASC").Limit(samlCeremonyPruneLimit).Find(&expired).Error; e != nil {
		return e
	}
	for _, r := range expired {
		if !validMemberRoleDigest(r.Digest) || r.ExpiresAt.After(now) {
			return samlUnavailable
		}
		if e := tx.Session(&gorm.Session{NewDB: true}).Where("digest = ? AND expires_at <= ?", r.Digest, now).Delete(&entity.SAMLAssertionReceipt{}).Error; e != nil {
			return e
		}
	}
	var live []struct{ Digest string }
	if e := tx.Session(&gorm.Session{NewDB: true}).Model(&entity.SAMLAssertionReceipt{}).Select("digest").Where("expires_at > ?", now).Limit(samlCeremonyLiveLimit).Find(&live).Error; e != nil {
		return e
	}
	if len(live) >= samlCeremonyLiveLimit {
		return samlUnavailable
	}
	return tx.Statement.Context.Err()
}

// Call only while holding governance. Expired rows can never be admitted,
// regardless of how many bounded cleanup batches remain. All unexpired rows
// count, including terminal rows, so failure cannot bypass the retained cap.
func samlAdmitCeremony(tx *gorm.DB, now time.Time) error {
	type expiryRow struct {
		ID        string
		ExpiresAt time.Time
	}
	var expired []expiryRow
	e := tx.Session(&gorm.Session{NewDB: true}).Model(&entity.SAMLCeremony{}).Select("id", "expires_at").Where("expires_at <= ?", now).Order("expires_at ASC").Limit(samlCeremonyPruneLimit).Find(&expired).Error
	if e != nil {
		return e
	}
	if len(expired) > samlCeremonyPruneLimit {
		return samlUnavailable
	}
	for _, row := range expired {
		if row.ID == "" || row.ExpiresAt.After(now) {
			return samlUnavailable
		}
		if e = tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "id"}, row.ID)).Where("expires_at <= ?", now).Delete(&entity.SAMLCeremony{}).Error; e != nil {
			return e
		}
	}
	var live []expiryRow
	e = tx.Session(&gorm.Session{NewDB: true}).Model(&entity.SAMLCeremony{}).Select("id", "expires_at").Where("expires_at > ?", now).Order("expires_at ASC").Limit(samlCeremonyLiveLimit).Find(&live).Error
	if e != nil {
		return e
	}
	for _, row := range live {
		if row.ID == "" || !row.ExpiresAt.After(now) {
			return samlUnavailable
		}
	}
	if len(live) >= samlCeremonyLiveLimit {
		return samlUnavailable
	}
	return tx.Statement.Context.Err()
}
