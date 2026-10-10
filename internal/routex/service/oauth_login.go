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

type OAuthCompletion struct {
	Kind           string
	Authentication *Authentication
	Challenge      *MFALoginChallenge
}

func oauthPrimary(tx *gorm.DB, method, bindingID string, bindingBirth *time.Time, config, policy, userID string, userBirth *time.Time) error {
	if method == "" {
		if bindingID != "" || bindingBirth != nil || config != "" || policy != "" || userBirth != nil {
			return apperrors.ErrUnauthorized
		}
		return nil
	}
	if method != "oauth" || bindingID == "" || bindingBirth == nil || bindingBirth.IsZero() || userBirth == nil || userBirth.IsZero() || !validMemberRoleDigest(config) || !validMemberRoleDigest(policy) {
		return apperrors.ErrUnauthorized
	}
	p, e := oauthProvider(tx)
	if e != nil {
		return e
	}
	if !p.Enabled || !oauthVerified(p) || p.ConfigRevision != config || p.PolicyRevision != policy {
		return apperrors.ErrUnauthorized
	}
	b, e := oauthBindingByID(tx, bindingID)
	if e != nil {
		return e
	}
	if b.ProviderID != p.ID || !oauthSubject(b.SubjectKind, b.Subject) || b.UserID != userID || !b.CreatedAt.Equal(*bindingBirth) || !b.UserCreatedAt.Equal(*userBirth) || b.ConfigRevision != config || b.SubjectDigest != oauthSubjectDigest(p.ID, b.SubjectKind, b.Subject) {
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
func oauthValidatePrimary(tx *gorm.DB, s entity.Session) error {
	if !primaryProofEmpty(s.OIDCBindingID, s.OIDCBindingCreatedAt, s.OIDCConfigRevision, s.OIDCPolicyRevision, s.OIDCUserCreatedAt) {
		return apperrors.ErrUnauthorized
	}
	return oauthPrimary(tx, s.PrimaryMethod, s.OAuthBindingID, s.OAuthBindingCreatedAt, s.OAuthConfigRevision, s.OAuthPolicyRevision, s.UserID, s.OAuthUserCreatedAt)
}
func oauthStampSession(tx *gorm.DB, a *Authentication, p entity.OAuthProvider, b entity.OAuthBinding) error {
	a.Session.PrimaryMethod = "oauth"
	a.Session.OAuthBindingID = b.ID
	a.Session.OAuthBindingCreatedAt = &b.CreatedAt
	a.Session.OAuthConfigRevision = p.ConfigRevision
	a.Session.OAuthPolicyRevision = p.PolicyRevision
	a.Session.OAuthUserCreatedAt = &b.UserCreatedAt
	return tx.Save(&a.Session).Error
}
func oauthStampChallenge(tx *gorm.DB, u entity.User, p entity.OAuthProvider, b entity.OAuthBinding, issued *MFALoginChallenge) error {
	var row entity.MFAChallenge
	e := tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "user_id"}, u.ID)).Where("purpose = ? AND token_hash = ?", "login", secret.SHA256Hex(issued.ChallengeToken)).Take(&row).Error
	if e != nil {
		return e
	}
	if row.UserID != u.ID || row.Purpose != "login" || row.TokenHash != secret.SHA256Hex(issued.ChallengeToken) {
		return apperrors.ErrUnauthorized
	}
	row.PrimaryMethod = "oauth"
	row.OAuthBindingID = b.ID
	row.OAuthBindingCreatedAt = &b.CreatedAt
	row.OAuthConfigRevision = p.ConfigRevision
	row.OAuthPolicyRevision = p.PolicyRevision
	row.OAuthUserCreatedAt = &u.CreatedAt
	return tx.Save(&row).Error
}
func oauthCapturedBinding(tx *gorm.DB, c entity.OAuthCeremony, u entity.User) error {
	b, e := oauthUserBinding(tx, u)
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
func (s *Service) CompleteOAuth(ctx context.Context, a *Authentication, cookie string) (*OAuthCompletion, error) {
	if !oauthBrowserValue(cookie) {
		return nil, apperrors.ErrUnauthorized
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out := &OAuthCompletion{}
	e := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		p, e := oauthProvider(tx)
		if e != nil {
			return e
		}
		var c entity.OAuthCeremony
		e = tx.Session(&gorm.Session{NewDB: true}).Where("cookie_hash = ?", secret.SHA256Hex(cookie)).Take(&c).Error
		if e != nil {
			return e
		}
		if !oauthSubject(c.SubjectKind, c.Subject) || c.Status != "verified" || c.VerifiedAt == nil || c.VerifiedAt.Before(c.CreatedAt) || c.VerifiedAt.After(time.Now().UTC()) || !oauthCeremonyCurrent(p, c, time.Now().UTC()) {
			return apperrors.ErrUnauthorized
		}
		var u entity.User
		var b entity.OAuthBinding
		if c.Purpose == "login" {
			if a != nil {
				return catalogConflict
			}
			digest := oauthSubjectDigest(p.ID, c.SubjectKind, c.Subject)
			e = tx.Session(&gorm.Session{NewDB: true}).Where("subject_digest = ?", digest).Take(&b).Error
			if e != nil {
				return e
			}
			if b.ProviderID != p.ID || b.SubjectKind != c.SubjectKind || b.Subject != c.Subject || b.SubjectDigest != digest || b.ConfigRevision != p.ConfigRevision {
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
			u, e = oauthOriginalSession(tx, c)
			if e != nil {
				return e
			}
			if e = oauthCurrentSession(tx, a, u, false); e != nil {
				return e
			}
			if e = oauthCapturedBinding(tx, c, u); e != nil {
				return e
			}
			if c.Purpose == "verify" {
				if e = registrationPolicyAuthority(tx, u.ID); e != nil {
					return e
				}
			}
			b, e = oauthBind(tx, p, u, c.SubjectKind, c.Subject)
			if e != nil {
				return e
			}
		}
		// Governance serializes lifecycle writers. Lock the ceremony after the User
		// and original Session so no caller inverts identity row lock ordering.
		var locked entity.OAuthCeremony
		e = tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "id"}, c.ID)).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&locked).Error
		if e != nil {
			return e
		}
		if locked.Status != "verified" || locked.CookieHash != c.CookieHash || locked.SubjectKind != c.SubjectKind || locked.Subject != c.Subject {
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
			p.ReviewRevision, e = oauthNewRevision()
			if e != nil {
				return e
			}
			if e = tx.Save(&p).Error; e != nil {
				return e
			}
			out.Kind = "verified"
			return oauthAudit(tx, u, "identity.oauth.verify", p, &b, c.Reason)
		}
		if c.Purpose == "bind" {
			out.Kind = "bound"
			return oauthAudit(tx, u, "account.oauth.bind", p, &b, c.Reason)
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
			if e = oauthStampChallenge(tx, u, p, b, issued); e != nil {
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
		if e = oauthStampSession(tx, out.Authentication, p, b); e != nil {
			return e
		}
		if e = recordSuccessfulLogin(tx, &out.Authentication.User); e != nil {
			return e
		}
		out.Kind = "session"
		return appendAudit(tx, u.ID, "identity.oauth.login", "user", u.ID)
	})
	if e != nil {
		if errors.Is(e, gorm.ErrRecordNotFound) {
			e = apperrors.ErrUnauthorized
		}
		return nil, oauthError(e)
	}
	if out.Authentication != nil {
		s.publishSessionMutation(ctx)
	}
	return out, nil
}

func (s *Service) oauthValidatePrimary(tx *gorm.DB, row entity.Session) error {
	return oauthValidatePrimary(tx, row)
}
