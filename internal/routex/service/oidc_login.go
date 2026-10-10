package service

import (
	"context"
	"errors"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/secret"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type OIDCCompletion struct {
	Kind           string
	Authentication *Authentication
	Challenge      *MFALoginChallenge
}

func oidcPrimary(tx *gorm.DB, method, bindingID string, bindingBirth *time.Time, config, policy, userID string, userBirth *time.Time) error {
	if method == "" {
		if bindingID != "" || bindingBirth != nil || config != "" || policy != "" || userBirth != nil {
			return apperrors.ErrUnauthorized
		}
		return nil
	}
	if method != "oidc" || bindingID == "" || bindingBirth == nil || bindingBirth.IsZero() || userBirth == nil || userBirth.IsZero() || !validMemberRoleDigest(config) || !validMemberRoleDigest(policy) {
		return apperrors.ErrUnauthorized
	}
	p, e := oidcProvider(tx)
	if e != nil {
		return e
	}
	if !p.Enabled || !oidcVerified(p) || p.ConfigRevision != config || p.PolicyRevision != policy {
		return apperrors.ErrUnauthorized
	}
	b, e := oidcBindingByID(tx, bindingID)
	if e != nil {
		return e
	}
	if b.UserID != userID || !b.CreatedAt.Equal(*bindingBirth) || !b.UserCreatedAt.Equal(*userBirth) || b.ConfigRevision != config || b.SubjectDigest != oidcSubjectDigest(p.Issuer, b.Subject) {
		return apperrors.ErrUnauthorized
	}
	u, e := registrationAdmittedUser(tx, userID, false)
	if e != nil {
		return e
	}
	if !u.CreatedAt.Equal(*userBirth) {
		return apperrors.ErrUnauthorized
	}
	return nil
}
func oidcValidatePrimary(tx *gorm.DB, s entity.Session) error {
	return oidcPrimary(tx, s.PrimaryMethod, s.OIDCBindingID, s.OIDCBindingCreatedAt, s.OIDCConfigRevision, s.OIDCPolicyRevision, s.UserID, s.OIDCUserCreatedAt)
}
func oidcValidateChallengePrimary(tx *gorm.DB, c entity.MFAChallenge) error {
	return oidcPrimary(tx, c.PrimaryMethod, c.OIDCBindingID, c.OIDCBindingCreatedAt, c.OIDCConfigRevision, c.OIDCPolicyRevision, c.UserID, c.OIDCUserCreatedAt)
}
func oidcStampSession(tx *gorm.DB, a *Authentication, p entity.OIDCProvider, b entity.OIDCBinding) error {
	a.Session.PrimaryMethod = "oidc"
	a.Session.OIDCBindingID = b.ID
	a.Session.OIDCBindingCreatedAt = &b.CreatedAt
	a.Session.OIDCConfigRevision = p.ConfigRevision
	a.Session.OIDCPolicyRevision = p.PolicyRevision
	a.Session.OIDCUserCreatedAt = &b.UserCreatedAt
	return tx.Save(&a.Session).Error
}
func oidcStampChallenge(tx *gorm.DB, u entity.User, p entity.OIDCProvider, b entity.OIDCBinding, issued *MFALoginChallenge) error {
	var row entity.MFAChallenge
	e := tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "user_id"}, u.ID)).Where("purpose = ? AND token_hash = ?", "login", secret.SHA256Hex(issued.ChallengeToken)).Take(&row).Error
	if e != nil {
		return e
	}
	if row.UserID != u.ID || row.Purpose != "login" || row.TokenHash != secret.SHA256Hex(issued.ChallengeToken) {
		return apperrors.ErrUnauthorized
	}
	row.PrimaryMethod = "oidc"
	row.OIDCBindingID = b.ID
	row.OIDCBindingCreatedAt = &b.CreatedAt
	row.OIDCConfigRevision = p.ConfigRevision
	row.OIDCPolicyRevision = p.PolicyRevision
	row.OIDCUserCreatedAt = &u.CreatedAt
	return tx.Save(&row).Error
}
func oidcCapturedBinding(tx *gorm.DB, c entity.OIDCCeremony, u entity.User) error {
	b, e := oidcUserBinding(tx, u)
	if e != nil {
		return e
	}
	if c.BindingID == "" {
		if b != nil {
			return catalogConflict
		}
		return nil
	}
	if b == nil || b.ID != c.BindingID || c.BindingCreatedAt == nil || !b.CreatedAt.Equal(*c.BindingCreatedAt) || b.ConfigRevision != c.ConfigRevision {
		return catalogConflict
	}
	return nil
}
func (s *Service) CompleteOIDC(ctx context.Context, a *Authentication, cookie string) (*OIDCCompletion, error) {
	if !oidcBrowserValue(cookie) {
		return nil, apperrors.ErrUnauthorized
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out := &OIDCCompletion{}
	e := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		p, e := oidcProvider(tx)
		if e != nil {
			return e
		}
		var c entity.OIDCCeremony
		e = tx.Session(&gorm.Session{NewDB: true}).Where("cookie_hash = ?", secret.SHA256Hex(cookie)).Take(&c).Error
		if e != nil {
			return e
		}
		if c.Status != "verified" || c.VerifiedAt == nil || c.VerifiedAt.Before(c.CreatedAt) || c.VerifiedAt.After(time.Now().UTC()) || !oidcCeremonyCurrent(p, c, time.Now().UTC()) {
			return apperrors.ErrUnauthorized
		}
		var u entity.User
		var b entity.OIDCBinding
		if c.Purpose == "login" {
			if a != nil {
				return catalogConflict
			}
			digest := oidcSubjectDigest(p.Issuer, c.Subject)
			e = tx.Session(&gorm.Session{NewDB: true}).Where("subject_digest = ?", digest).Take(&b).Error
			if e != nil {
				return e
			}
			if b.Subject != c.Subject || b.SubjectDigest != digest || b.ConfigRevision != p.ConfigRevision {
				return apperrors.ErrUnauthorized
			}
			u, e = registrationAdmittedUser(tx, b.UserID, true)
			if e != nil {
				return e
			}
			if !u.CreatedAt.Equal(b.UserCreatedAt) || b.CreatedAt.IsZero() {
				return apperrors.ErrUnauthorized
			}
		} else {
			if a == nil || a.Session.ID != c.SessionID || c.SessionCreatedAt == nil || !a.Session.CreatedAt.Equal(*c.SessionCreatedAt) {
				return apperrors.ErrUnauthorized
			}
			u, e = oidcOriginalSession(tx, c)
			if e != nil {
				return e
			}
			if e = oidcCurrentSession(tx, a, u, false); e != nil {
				return e
			}
			if e = oidcCapturedBinding(tx, c, u); e != nil {
				return e
			}
			if c.Purpose == "verify" {
				if e = registrationPolicyAuthority(tx, u.ID); e != nil {
					return e
				}
			}
			b, e = oidcBind(tx, p, u, c.Subject)
			if e != nil {
				return e
			}
		}
		// Governance serializes lifecycle writers. Lock the ceremony after the User
		// and original Session so no caller inverts identity row lock ordering.
		var locked entity.OIDCCeremony
		e = tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "id"}, c.ID)).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&locked).Error
		if e != nil {
			return e
		}
		if locked.Status != "verified" || locked.CookieHash != c.CookieHash || locked.Subject != c.Subject {
			return apperrors.ErrUnauthorized
		}
		locked.Status = "consumed"
		locked.BindingID = b.ID
		locked.BindingCreatedAt = &b.CreatedAt
		if e = tx.Save(&locked).Error; e != nil {
			return e
		}
		if c.Purpose == "verify" {
			p.VerifiedConfigRevision = p.ConfigRevision
			p.VerifiedBy = u.ID
			p.VerifiedUserCreatedAt = &u.CreatedAt
			p.VerifiedBindingID = b.ID
			p.VerifiedBindingCreatedAt = &b.CreatedAt
			p.ReviewRevision, e = oidcNewRevision()
			if e != nil {
				return e
			}
			if e = tx.Save(&p).Error; e != nil {
				return e
			}
			out.Kind = "verified"
			return oidcAudit(tx, u, "identity.oidc.verify", p, &b, c.Reason)
		}
		if c.Purpose == "bind" {
			out.Kind = "bound"
			return oidcAudit(tx, u, "account.oidc.bind", p, &b, c.Reason)
		}
		m, e := mfaState(tx, u.ID)
		if e != nil {
			return e
		}
		if m.Enabled {
			issued, e := issueMFALoginChallenge(tx, u, m)
			if e != nil {
				return e
			}
			if e = oidcStampChallenge(tx, u, p, b, issued); e != nil {
				return e
			}
			out.Kind = "challenge"
			out.Challenge = issued
			return nil
		}
		out.Authentication, e = createSession(tx, u)
		if e != nil {
			return e
		}
		if e = oidcStampSession(tx, out.Authentication, p, b); e != nil {
			return e
		}
		if e = recordSuccessfulLogin(tx, &out.Authentication.User); e != nil {
			return e
		}
		out.Kind = "session"
		return appendAudit(tx, u.ID, "identity.oidc.login", "user", u.ID)
	})
	if e != nil {
		if errors.Is(e, gorm.ErrRecordNotFound) {
			e = apperrors.ErrUnauthorized
		}
		return nil, oidcError(e)
	}
	if out.Authentication != nil {
		s.publishSessionMutation(ctx)
	}
	return out, nil
}

func (s *Service) oidcValidatePrimary(tx *gorm.DB, row entity.Session) error {
	return oidcValidatePrimary(tx, row)
}
func (s *Service) oidcValidateChallengePrimary(tx *gorm.DB, row entity.MFAChallenge) error {
	return oidcValidateChallengePrimary(tx, row)
}
