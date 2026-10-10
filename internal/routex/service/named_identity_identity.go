package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type GitHubProof struct {
	Code         string `json:"code,omitempty"`
	RecoveryCode string `json:"recovery_code,omitempty"`
}
type GitHubIdentityInput struct {
	Password string      `json:"password"`
	Proof    GitHubProof `json:"proof"`
	Reason   string      `json:"reason"`
}
type GitHubAccountView struct {
	Available   bool   `json:"available"`
	Name        string `json:"name"`
	Bound       bool   `json:"bound"`
	ReviewETag  string `json:"review_etag"`
	MFARequired bool   `json:"mfa_required"`
}

func (v *GitHubProof) UnmarshalJSON(raw []byte) error {
	if len(raw) > 4096 || !utf8.Valid(raw) || !modelCreationUnicode(raw) {
		return apperrors.ErrBadRequest
	}
	f, e := memberRolesObject(raw)
	if e != nil || len(f) > 1 {
		return apperrors.ErrBadRequest
	}
	var out GitHubProof
	for k, b := range f {
		if string(b) == "null" {
			return apperrors.ErrBadRequest
		}
		var x string
		if json.Unmarshal(b, &x) != nil || x == "" || len(x) > 128 {
			return apperrors.ErrBadRequest
		}
		switch k {
		case "code":
			out.Code = x
		case "recovery_code":
			out.RecoveryCode = x
		default:
			return apperrors.ErrBadRequest
		}
	}
	*v = out
	return nil
}
func (v *GitHubIdentityInput) UnmarshalJSON(raw []byte) error {
	f, e := registrationStrictObject(raw, []string{"password", "proof", "reason"})
	if e != nil {
		return e
	}
	var out GitHubIdentityInput
	if json.Unmarshal(f["password"], &out.Password) != nil || json.Unmarshal(f["proof"], &out.Proof) != nil || json.Unmarshal(f["reason"], &out.Reason) != nil || !validPassword(out.Password) || !validRegistrationReason(out.Reason) {
		return apperrors.ErrBadRequest
	}
	*v = out
	return nil
}
func namedIdentitySubject(kind, subject string) bool {
	if kind != "integer" || !namedIdentityText(subject, 256) || len(subject) > 1 && subject[0] == '0' {
		return false
	}
	for _, c := range subject {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
func namedIdentitySubjectDigest(provider, kind, subject string) string {
	d, ok := namedIdentityDescriptor(provider)
	if !ok {
		return ""
	}
	return namedIdentityDigest("named-identity.subject.v1", provider, d.profile, d.issuer, kind, subject)
}
func namedIdentityBindingByID(tx *gorm.DB, bindingID string) (entity.NamedIdentityBinding, error) {
	var b entity.NamedIdentityBinding
	e := tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "id"}, bindingID)).Take(&b).Error
	if errors.Is(e, gorm.ErrRecordNotFound) || e == nil && (b.ID != bindingID || !namedIdentityProfile(b.ProviderID, b.ProfileID, b.IdentityIssuer) || !namedIdentityProfileSubject(b.ProviderID, b.SubjectKind, b.Subject) || b.CreatedAt.IsZero()) {
		e = apperrors.ErrUnauthorized
	}
	return b, e
}
func namedIdentityUserBinding(tx *gorm.DB, u entity.User, providerID string) (*entity.NamedIdentityBinding, error) {
	var b entity.NamedIdentityBinding
	e := tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "user_id"}, u.ID)).Where(database.ExactText(tx, clause.Column{Name: "provider_id"}, providerID)).Take(&b).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	if b.ProviderID != providerID || !namedIdentityProfile(b.ProviderID, b.ProfileID, b.IdentityIssuer) || !namedIdentityProfileSubject(b.ProviderID, b.SubjectKind, b.Subject) || b.UserID != u.ID || !b.UserCreatedAt.Equal(u.CreatedAt) || b.ID == "" || b.CreatedAt.IsZero() {
		return nil, apperrors.ErrUnauthorized
	}
	return &b, nil
}
func namedIdentityAccountReview(p entity.NamedIdentityProvider, u entity.User, m entity.UserMFA, a *Authentication, b *entity.NamedIdentityBinding) string {
	var proof any
	if b != nil {
		proof = []any{b.ID, b.CreatedAt.UTC(), b.ConfigRevision, b.SubjectDigest}
	}
	return namedIdentityDigest("named-identity.account.review.v1", p.ID, p.ProfileID, p.IdentityIssuer, p.CreatedAt.UTC(), p.ReviewRevision, p.ConfigRevision, p.PolicyRevision, u.ID, u.CreatedAt.UTC(), a.Session.ID, a.Session.CreatedAt.UTC(), m.Enabled, m.Generation, proof)
}
func namedIdentityAccountView(p entity.NamedIdentityProvider, u entity.User, m entity.UserMFA, a *Authentication, b *entity.NamedIdentityBinding) *GitHubAccountView {
	return &GitHubAccountView{Available: p.Enabled && namedIdentityVerified(p), Name: p.Name, Bound: b != nil, ReviewETag: namedIdentityAccountReview(p, u, m, a, b), MFARequired: m.Enabled}
}
func namedIdentityCurrentSession(tx *gorm.DB, a *Authentication, u entity.User, lock bool) error {
	return identityCurrentSession(tx, a, u, lock)
}
func (s *Service) accountNamedIdentity(ctx context.Context, a *Authentication, providerID string) (*GitHubAccountView, error) {
	if a == nil {
		return nil, apperrors.ErrUnauthorized
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var out *GitHubAccountView
	e := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		u, e := registrationAdmittedUser(tx, a.User.ID, false)
		if e != nil {
			return e
		}
		if e = namedIdentityCurrentSession(tx, a, u, false); e != nil {
			return e
		}
		p, e := namedIdentityProvider(tx, providerID)
		if e != nil {
			return e
		}
		m, e := mfaState(tx, u.ID)
		if e != nil {
			return e
		}
		b, e := namedIdentityUserBinding(tx, u, providerID)
		if e != nil {
			return e
		}
		out = namedIdentityAccountView(p, u, m, a, b)
		return ctx.Err()
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if e != nil {
		return nil, namedIdentityError(e)
	}
	return out, nil
}

// The rejected flag lets failure counters commit without admitting an operation.
func (s *Service) namedIdentityLocalProof(tx *gorm.DB, a *Authentication, passwordHash string, proof GitHubProof) (entity.User, entity.UserMFA, bool, error) {
	return s.identityLocalProof(tx, a, passwordHash, MFAProof(proof))
}
func (s *Service) unlinkNamedIdentity(ctx context.Context, a *Authentication, etag, providerID string, in GitHubIdentityInput) (*GitHubAccountView, error) {
	if a == nil {
		return nil, apperrors.ErrUnauthorized
	}
	if !validMemberRoleDigest(etag) || !validRegistrationReason(in.Reason) {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	hash, e := s.mfaPassword(ctx, a.User.ID, in.Password)
	if e != nil {
		return nil, e
	}
	var out *GitHubAccountView
	var revokedBinding *entity.NamedIdentityBinding
	rejected := false
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		u, m, bad, e := s.namedIdentityLocalProof(tx, a, hash, in.Proof)
		if e != nil {
			return e
		}
		if bad {
			rejected = true
			return nil
		}
		p, e := namedIdentityProvider(tx, providerID)
		if e != nil {
			return e
		}
		b, e := namedIdentityUserBinding(tx, u, providerID)
		if e != nil {
			return e
		}
		if namedIdentityAccountReview(p, u, m, a, b) != etag {
			return catalogConflict
		}
		if b == nil {
			return catalogConflict
		}
		revokedBinding = b
		e = s.namedIdentityRevoke(tx, providerID, b.ID)
		if e != nil {
			return e
		}
		if e = tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "id"}, b.ID)).Delete(&entity.NamedIdentityBinding{}).Error; e != nil {
			return e
		}
		if e = namedIdentityAudit(tx, u, "account."+p.ID+".unlink", p, b, in.Reason); e != nil {
			return e
		}
		out = namedIdentityAccountView(p, u, m, a, nil)
		return ctx.Err()
	})
	if e != nil {
		return nil, namedIdentityError(e)
	}
	if rejected {
		return nil, apperrors.ErrUnauthorized
	}
	if revokedBinding != nil {
		s.invalidateRuntimeNamedIdentityBinding(providerID, revokedBinding.ID, revokedBinding.CreatedAt)
		s.publishSessionMutation(ctx)
	}
	return out, nil
}
func namedIdentityBind(tx *gorm.DB, p entity.NamedIdentityProvider, u entity.User, kind, subject string) (entity.NamedIdentityBinding, error) {
	if !namedIdentityProfile(p.ID, p.ProfileID, p.IdentityIssuer) || !namedIdentityProfileSubject(p.ID, kind, subject) {
		return entity.NamedIdentityBinding{}, apperrors.ErrUnauthorized
	}
	digest := namedIdentitySubjectDigest(p.ID, kind, subject)
	existing, e := namedIdentityUserBinding(tx, u, p.ID)
	if e != nil {
		return entity.NamedIdentityBinding{}, e
	}
	if existing != nil {
		if existing.ProviderID != p.ID || existing.ConfigRevision != p.ConfigRevision || existing.SubjectKind != kind || existing.Subject != subject || existing.SubjectDigest != digest {
			return entity.NamedIdentityBinding{}, catalogConflict
		}
		return *existing, nil
	}
	bid, e := id.NewPrefixed("nib")
	if e != nil {
		return entity.NamedIdentityBinding{}, e
	}
	b := entity.NamedIdentityBinding{ID: bid, ProviderID: p.ID, ProfileID: p.ProfileID, IdentityIssuer: p.IdentityIssuer, SubjectKind: kind, CreatedAt: time.Now().UTC().Truncate(time.Microsecond), UserID: u.ID, UserCreatedAt: u.CreatedAt, ConfigRevision: p.ConfigRevision, Subject: subject, SubjectDigest: digest}
	if e = tx.Create(&b).Error; e != nil {
		if errors.Is(e, gorm.ErrDuplicatedKey) {
			return b, catalogConflict
		}
		return b, e
	}
	return b, nil
}
