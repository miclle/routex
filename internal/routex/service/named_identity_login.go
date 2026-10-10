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

type GitHubCompletion struct {
	Kind           string
	Authentication *Authentication
	Challenge      *MFALoginChallenge
}

func namedIdentityPrimary(tx *gorm.DB, method, bindingID string, bindingBirth *time.Time, config, policy, userID string, userBirth *time.Time) error {
	if method == "" {
		if bindingID != "" || bindingBirth != nil || config != "" || policy != "" || userBirth != nil {
			return apperrors.ErrUnauthorized
		}
		return nil
	}
	if !namedIdentityMethod(method) || bindingID == "" || bindingBirth == nil || bindingBirth.IsZero() || userBirth == nil || userBirth.IsZero() || !validMemberRoleDigest(config) || !validMemberRoleDigest(policy) {
		return apperrors.ErrUnauthorized
	}
	p, e := namedIdentityProvider(tx, method)
	if e != nil {
		return e
	}
	if !p.Enabled || !namedIdentityVerified(p) || p.ConfigRevision != config || p.PolicyRevision != policy {
		return apperrors.ErrUnauthorized
	}
	b, e := namedIdentityBindingByID(tx, bindingID)
	if e != nil {
		return e
	}
	if b.ProviderID != p.ID || !namedIdentityProfile(b.ProviderID, b.ProfileID, b.IdentityIssuer) || !namedIdentityProfileSubject(p.ID, b.SubjectKind, b.Subject) || b.UserID != userID || !b.CreatedAt.Equal(*bindingBirth) || !b.UserCreatedAt.Equal(*userBirth) || b.ConfigRevision != config || b.SubjectDigest != namedIdentitySubjectDigest(p.ID, b.SubjectKind, b.Subject) {
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
func namedIdentityValidatePrimary(tx *gorm.DB, s entity.Session) error {
	if !namedIdentityMethod(s.PrimaryMethod) || s.NamedIdentityProviderID != s.PrimaryMethod || s.NamedIdentityProfileID != namedIdentityProfileID(s.PrimaryMethod) || !primaryProofEmpty(s.OIDCBindingID, s.OIDCBindingCreatedAt, s.OIDCConfigRevision, s.OIDCPolicyRevision, s.OIDCUserCreatedAt) || !primaryProofEmpty(s.OAuthBindingID, s.OAuthBindingCreatedAt, s.OAuthConfigRevision, s.OAuthPolicyRevision, s.OAuthUserCreatedAt) || !primaryProofEmpty(s.LDAPBindingID, s.LDAPBindingCreatedAt, s.LDAPConfigRevision, s.LDAPPolicyRevision, s.LDAPUserCreatedAt) || !primaryProofEmpty(s.SAMLBindingID, s.SAMLBindingCreatedAt, s.SAMLConfigRevision, s.SAMLPolicyRevision, s.SAMLUserCreatedAt) {
		return apperrors.ErrUnauthorized
	}
	return namedIdentityPrimary(tx, s.PrimaryMethod, s.NamedIdentityBindingID, s.NamedIdentityBindingCreatedAt, s.NamedIdentityConfigRevision, s.NamedIdentityPolicyRevision, s.UserID, s.NamedIdentityUserCreatedAt)
}
func namedIdentityStampSession(tx *gorm.DB, a *Authentication, p entity.NamedIdentityProvider, b entity.NamedIdentityBinding) error {
	a.Session.PrimaryMethod = p.ID
	a.Session.NamedIdentityProviderID = p.ID
	a.Session.NamedIdentityProfileID = p.ProfileID
	a.Session.NamedIdentityBindingID = b.ID
	a.Session.NamedIdentityBindingCreatedAt = &b.CreatedAt
	a.Session.NamedIdentityConfigRevision = p.ConfigRevision
	a.Session.NamedIdentityPolicyRevision = p.PolicyRevision
	a.Session.NamedIdentityUserCreatedAt = &b.UserCreatedAt
	return tx.Save(&a.Session).Error
}
func namedIdentityStampChallenge(tx *gorm.DB, u entity.User, p entity.NamedIdentityProvider, b entity.NamedIdentityBinding, issued *MFALoginChallenge) error {
	var row entity.MFAChallenge
	e := tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "user_id"}, u.ID)).Where("purpose = ? AND token_hash = ?", "login", secret.SHA256Hex(issued.ChallengeToken)).Take(&row).Error
	if e != nil {
		return e
	}
	if row.UserID != u.ID || row.Purpose != "login" || row.TokenHash != secret.SHA256Hex(issued.ChallengeToken) {
		return apperrors.ErrUnauthorized
	}
	row.PrimaryMethod = p.ID
	row.NamedIdentityProviderID = p.ID
	row.NamedIdentityProfileID = p.ProfileID
	row.NamedIdentityBindingID = b.ID
	row.NamedIdentityBindingCreatedAt = &b.CreatedAt
	row.NamedIdentityConfigRevision = p.ConfigRevision
	row.NamedIdentityPolicyRevision = p.PolicyRevision
	row.NamedIdentityUserCreatedAt = &u.CreatedAt
	return tx.Save(&row).Error
}
func namedIdentityCapturedBinding(tx *gorm.DB, c entity.NamedIdentityCeremony, u entity.User) error {
	b, e := namedIdentityUserBinding(tx, u, c.ProviderID)
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
func (s *Service) completeNamedIdentity(ctx context.Context, a *Authentication, cookie, providerID string) (*GitHubCompletion, error) {
	if !namedIdentityBrowserValue(cookie) {
		return nil, apperrors.ErrUnauthorized
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out := &GitHubCompletion{}
	e := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		p, e := namedIdentityProvider(tx, providerID)
		if e != nil {
			return e
		}
		var c entity.NamedIdentityCeremony
		e = tx.Session(&gorm.Session{NewDB: true}).Where("cookie_hash = ?", secret.SHA256Hex(cookie)).Take(&c).Error
		if e != nil {
			return e
		}
		if !namedIdentityProfileSubject(p.ID, c.SubjectKind, c.Subject) || c.Status != "verified" || c.VerifiedAt == nil || c.VerifiedAt.Before(c.CreatedAt) || c.VerifiedAt.After(time.Now().UTC()) || !namedIdentityCeremonyCurrent(p, c, time.Now().UTC()) {
			return apperrors.ErrUnauthorized
		}
		var u entity.User
		var b entity.NamedIdentityBinding
		if c.Purpose == "login" {
			if a != nil {
				return catalogConflict
			}
			digest := namedIdentitySubjectDigest(p.ID, c.SubjectKind, c.Subject)
			e = tx.Session(&gorm.Session{NewDB: true}).Where("subject_digest = ?", digest).Take(&b).Error
			if e != nil {
				return e
			}
			if b.ProviderID != p.ID || !namedIdentityProfile(b.ProviderID, b.ProfileID, b.IdentityIssuer) || b.SubjectKind != c.SubjectKind || b.Subject != c.Subject || b.SubjectDigest != digest || b.ConfigRevision != p.ConfigRevision {
				return apperrors.ErrUnauthorized
			}
			u, e = registrationAdmittedUser(tx, b.UserID, true)
			if e != nil {
				return e
			}
			if !u.CreatedAt.Equal(b.UserCreatedAt) || b.CreatedAt.IsZero() || c.UserID != u.ID || c.UserCreatedAt == nil || !c.UserCreatedAt.Equal(u.CreatedAt) || c.BindingID != b.ID || c.BindingCreatedAt == nil || !c.BindingCreatedAt.Equal(b.CreatedAt) {
				return apperrors.ErrUnauthorized
			}
		} else {
			if a == nil || a.Session.ID != c.SessionID || c.SessionCreatedAt == nil || !a.Session.CreatedAt.Equal(*c.SessionCreatedAt) {
				return apperrors.ErrUnauthorized
			}
			u, e = namedIdentityOriginalSession(tx, c)
			if e != nil {
				return e
			}
			if e = namedIdentityCurrentSession(tx, a, u, false); e != nil {
				return e
			}
			if e = namedIdentityCapturedBinding(tx, c, u); e != nil {
				return e
			}
			if c.Purpose == "verify" {
				if e = registrationPolicyAuthority(tx, u.ID); e != nil {
					return e
				}
			}
			b, e = namedIdentityBind(tx, p, u, c.SubjectKind, c.Subject)
			if e != nil {
				return e
			}
		}
		// Governance serializes lifecycle writers. Lock the ceremony after the User
		// and original Session so no caller inverts identity row lock ordering.
		var locked entity.NamedIdentityCeremony
		e = tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "id"}, c.ID)).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&locked).Error
		if e != nil {
			return e
		}
		if !namedIdentityCeremonyCurrent(p, locked, time.Now().UTC()) || locked.Status != "verified" || locked.CookieHash != c.CookieHash || locked.SubjectKind != c.SubjectKind || locked.Subject != c.Subject {
			return apperrors.ErrUnauthorized
		}
		locked.Status = "consumed"
		consumedAt := time.Now().UTC().Truncate(time.Microsecond)
		locked.ConsumedAt = &consumedAt
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
			p.ReviewRevision, e = namedIdentityNewRevision()
			if e != nil {
				return e
			}
			if e = tx.Save(&p).Error; e != nil {
				return e
			}
			out.Kind = "verified"
			return namedIdentityAudit(tx, u, "identity."+p.ID+".verify", p, &b, c.Reason)
		}
		if c.Purpose == "bind" {
			out.Kind = "bound"
			return namedIdentityAudit(tx, u, "account."+p.ID+".bind", p, &b, c.Reason)
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
			if e = namedIdentityStampChallenge(tx, u, p, b, issued); e != nil {
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
		if e = namedIdentityStampSession(tx, out.Authentication, p, b); e != nil {
			return e
		}
		if e = recordSuccessfulLogin(tx, &out.Authentication.User); e != nil {
			return e
		}
		out.Kind = "session"
		return appendAudit(tx, u.ID, "identity."+p.ID+".login", "user", u.ID)
	})
	if e != nil {
		if errors.Is(e, gorm.ErrRecordNotFound) {
			e = apperrors.ErrUnauthorized
		}
		return nil, namedIdentityError(e)
	}
	if out.Authentication != nil {
		s.publishSessionMutation(ctx)
	}
	return out, nil
}
