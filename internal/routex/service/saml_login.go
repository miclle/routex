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

type SAMLCompletion struct {
	Kind           string
	Authentication *Authentication
	Challenge      *MFALoginChallenge
}

func samlPrimary(tx *gorm.DB, method, bindingID string, bindingBirth *time.Time, config, policy, userID string, userBirth *time.Time) error {
	if method == "" {
		if bindingID != "" || bindingBirth != nil || config != "" || policy != "" || userBirth != nil {
			return apperrors.ErrUnauthorized
		}
		return nil
	}
	if method != "saml" || bindingID == "" || bindingBirth == nil || bindingBirth.IsZero() || userBirth == nil || userBirth.IsZero() || !validMemberRoleDigest(config) || !validMemberRoleDigest(policy) {
		return apperrors.ErrUnauthorized
	}
	p, e := samlProvider(tx)
	if e != nil {
		return e
	}
	if !p.Enabled || !samlVerified(p) || p.ConfigRevision != config || p.PolicyRevision != policy {
		return apperrors.ErrUnauthorized
	}
	b, e := samlBindingByID(tx, bindingID)
	if e != nil {
		return e
	}
	if b.UserID != userID || !b.CreatedAt.Equal(*bindingBirth) || !b.UserCreatedAt.Equal(*userBirth) || b.ConfigRevision != config || b.ProviderID != p.ID || b.Issuer != p.IDPIssuer || !samlSubject(b.Subject) || b.SubjectDigest != samlSubjectDigest(p.IDPIssuer, b.Subject) {
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
func samlValidatePrimary(tx *gorm.DB, s entity.Session) error {
	return samlPrimary(tx, s.PrimaryMethod, s.SAMLBindingID, s.SAMLBindingCreatedAt, s.SAMLConfigRevision, s.SAMLPolicyRevision, s.UserID, s.SAMLUserCreatedAt)
}
func samlStampSession(tx *gorm.DB, a *Authentication, p entity.SAMLProvider, b entity.SAMLBinding) error {
	a.Session.PrimaryMethod = "saml"
	a.Session.SAMLBindingID = b.ID
	a.Session.SAMLBindingCreatedAt = &b.CreatedAt
	a.Session.SAMLConfigRevision = p.ConfigRevision
	a.Session.SAMLPolicyRevision = p.PolicyRevision
	a.Session.SAMLUserCreatedAt = &b.UserCreatedAt
	return tx.Save(&a.Session).Error
}
func samlStampChallenge(tx *gorm.DB, u entity.User, p entity.SAMLProvider, b entity.SAMLBinding, issued *MFALoginChallenge) error {
	var row entity.MFAChallenge
	e := tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "user_id"}, u.ID)).Where("purpose = ? AND token_hash = ?", "login", secret.SHA256Hex(issued.ChallengeToken)).Take(&row).Error
	if e != nil {
		return e
	}
	if row.UserID != u.ID || row.Purpose != "login" || row.TokenHash != secret.SHA256Hex(issued.ChallengeToken) {
		return apperrors.ErrUnauthorized
	}
	row.PrimaryMethod = "saml"
	row.SAMLBindingID = b.ID
	row.SAMLBindingCreatedAt = &b.CreatedAt
	row.SAMLConfigRevision = p.ConfigRevision
	row.SAMLPolicyRevision = p.PolicyRevision
	row.SAMLUserCreatedAt = &u.CreatedAt
	return tx.Save(&row).Error
}
func samlCapturedBinding(tx *gorm.DB, c entity.SAMLCeremony, u entity.User) error {
	b, e := samlUserBinding(tx, u)
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
func (s *Service) CompleteSAML(ctx context.Context, a *Authentication, startCookie, deliveryCookie string) (*SAMLCompletion, error) {
	if !samlBrowserValue(startCookie) || !samlBrowserValue(deliveryCookie) {
		return nil, apperrors.ErrUnauthorized
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out := &SAMLCompletion{}
	e := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		p, e := samlProvider(tx)
		if e != nil {
			return e
		}
		var c entity.SAMLCeremony
		e = tx.Session(&gorm.Session{NewDB: true}).Where("cookie_hash = ? AND delivery_hash = ?", secret.SHA256Hex(startCookie), secret.SHA256Hex(deliveryCookie)).Take(&c).Error
		if e != nil {
			return e
		}
		if c.ProofExpiresAt == nil || !c.ProofExpiresAt.After(time.Now().UTC()) || c.Issuer != p.IDPIssuer || !validMemberRoleDigest(c.AssertionDigest) || c.Status != "verified" || c.VerifiedAt == nil || c.VerifiedAt.Before(c.CreatedAt) || c.VerifiedAt.After(time.Now().UTC()) || !samlCeremonyCurrent(p, c, time.Now().UTC()) {
			return apperrors.ErrUnauthorized
		}
		var receipt entity.SAMLAssertionReceipt
		if e = tx.Session(&gorm.Session{NewDB: true}).Where("digest = ?", c.AssertionDigest).Take(&receipt).Error; e != nil {
			return e
		}
		if !receipt.ExpiresAt.After(time.Now().UTC()) {
			return apperrors.ErrUnauthorized
		}
		var u entity.User
		var b entity.SAMLBinding
		if c.Purpose == "login" {
			if a != nil {
				return catalogConflict
			}
			digest := samlSubjectDigest(p.IDPIssuer, c.Subject)
			e = tx.Session(&gorm.Session{NewDB: true}).Where("subject_digest = ?", digest).Take(&b).Error
			if e != nil {
				return e
			}
			if b.Subject != c.Subject || b.SubjectDigest != digest || b.ConfigRevision != p.ConfigRevision || b.ProviderID != p.ID || b.Issuer != p.IDPIssuer || b.CreatedAt.After(c.CreatedAt) {
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
			u, e = samlOriginalSession(tx, c)
			if e != nil {
				return e
			}
			if e = samlCurrentSession(tx, a, u, false); e != nil {
				return e
			}
			if e = samlCapturedBinding(tx, c, u); e != nil {
				return e
			}
			if c.Purpose == "verify" {
				if e = registrationPolicyAuthority(tx, u.ID); e != nil {
					return e
				}
			}
			b, e = samlBind(tx, p, u, c.Subject)
			if e != nil {
				return e
			}
		}
		// Governance serializes lifecycle writers. Lock the ceremony after the User
		// and original Session so no caller inverts identity row lock ordering.
		var locked entity.SAMLCeremony
		e = tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "id"}, c.ID)).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&locked).Error
		if e != nil {
			return e
		}
		if locked.Status != "verified" || locked.CookieHash != c.CookieHash || locked.DeliveryHash != c.DeliveryHash || locked.AssertionDigest != c.AssertionDigest || locked.RequestID != c.RequestID || locked.Subject != c.Subject || locked.ProofExpiresAt == nil || !locked.ProofExpiresAt.After(time.Now().UTC()) || !samlCeremonyCurrent(p, locked, time.Now().UTC()) {
			return apperrors.ErrUnauthorized
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		locked.ConsumedAt = &now
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
			p.ReviewRevision, e = samlNewRevision()
			if e != nil {
				return e
			}
			if e = tx.Save(&p).Error; e != nil {
				return e
			}
			out.Kind = "verified"
			if e = samlAudit(tx, u, "identity.saml.verify", p, &b, c.Reason); e != nil {
				return e
			}
			return samlCompletionCurrent(ctx, c)
		}
		if c.Purpose == "bind" {
			out.Kind = "bound"
			if e = samlAudit(tx, u, "account.saml.bind", p, &b, c.Reason); e != nil {
				return e
			}
			return samlCompletionCurrent(ctx, c)
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
			if e = samlStampChallenge(tx, u, p, b, issued); e != nil {
				return e
			}
			out.Kind = "challenge"
			out.Challenge = issued
			return samlCompletionCurrent(ctx, c)
		}
		out.Authentication, e = createSession(tx, u)
		if e != nil {
			return e
		}
		if e = samlStampSession(tx, out.Authentication, p, b); e != nil {
			return e
		}
		if e = recordSuccessfulLogin(tx, &out.Authentication.User); e != nil {
			return e
		}
		out.Kind = "session"
		if e = appendAudit(tx, u.ID, "identity.saml.login", "user", u.ID); e != nil {
			return e
		}
		return samlCompletionCurrent(ctx, c)
	})
	if e != nil {
		if errors.Is(e, gorm.ErrRecordNotFound) {
			e = apperrors.ErrUnauthorized
		}
		return nil, samlError(e)
	}
	if out.Authentication != nil {
		s.publishSessionMutation(ctx)
	}
	return out, nil
}

func samlCompletionCurrent(ctx context.Context, c entity.SAMLCeremony) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	now := time.Now().UTC()
	if c.ProofExpiresAt == nil || !c.ProofExpiresAt.After(now) || !c.ExpiresAt.After(now) {
		return apperrors.ErrUnauthorized
	}
	return nil
}
