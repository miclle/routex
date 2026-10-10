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
	"github.com/miclle/routex/pkg/secret"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type OIDCProof struct {
	Code         string `json:"code,omitempty"`
	RecoveryCode string `json:"recovery_code,omitempty"`
}
type OIDCIdentityInput struct {
	Password string    `json:"password"`
	Proof    OIDCProof `json:"proof"`
	Reason   string    `json:"reason"`
}
type OIDCAccountView struct {
	Available   bool   `json:"available"`
	Name        string `json:"name"`
	Bound       bool   `json:"bound"`
	ReviewETag  string `json:"review_etag"`
	MFARequired bool   `json:"mfa_required"`
}

func (v *OIDCProof) UnmarshalJSON(raw []byte) error {
	if len(raw) > 4096 || !utf8.Valid(raw) || !modelCreationUnicode(raw) {
		return apperrors.ErrBadRequest
	}
	f, e := memberRolesObject(raw)
	if e != nil || len(f) > 1 {
		return apperrors.ErrBadRequest
	}
	var out OIDCProof
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
func (v *OIDCIdentityInput) UnmarshalJSON(raw []byte) error {
	f, e := registrationStrictObject(raw, []string{"password", "proof", "reason"})
	if e != nil {
		return e
	}
	var out OIDCIdentityInput
	if json.Unmarshal(f["password"], &out.Password) != nil || json.Unmarshal(f["proof"], &out.Proof) != nil || json.Unmarshal(f["reason"], &out.Reason) != nil || !validPassword(out.Password) || !validRegistrationReason(out.Reason) {
		return apperrors.ErrBadRequest
	}
	*v = out
	return nil
}
func oidcSubjectDigest(issuer, subject string) string {
	return oidcDigest("oidc.subject.v1", issuer, subject)
}
func oidcBindingByID(tx *gorm.DB, bindingID string) (entity.OIDCBinding, error) {
	var b entity.OIDCBinding
	e := tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "id"}, bindingID)).Take(&b).Error
	if errors.Is(e, gorm.ErrRecordNotFound) || e == nil && (b.ID != bindingID || b.CreatedAt.IsZero()) {
		e = apperrors.ErrUnauthorized
	}
	return b, e
}
func oidcUserBinding(tx *gorm.DB, u entity.User) (*entity.OIDCBinding, error) {
	var b entity.OIDCBinding
	e := tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "user_id"}, u.ID)).Take(&b).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	if b.UserID != u.ID || !b.UserCreatedAt.Equal(u.CreatedAt) || b.ID == "" || b.CreatedAt.IsZero() {
		return nil, apperrors.ErrUnauthorized
	}
	return &b, nil
}
func oidcAccountReview(p entity.OIDCProvider, u entity.User, m entity.UserMFA, a *Authentication, b *entity.OIDCBinding) string {
	var proof any
	if b != nil {
		proof = []any{b.ID, b.CreatedAt.UTC(), b.ConfigRevision, b.SubjectDigest}
	}
	return oidcDigest("oidc.account.review.v1", p.ReviewRevision, p.ConfigRevision, p.PolicyRevision, u.ID, u.CreatedAt.UTC(), a.Session.ID, a.Session.CreatedAt.UTC(), m.Enabled, m.Generation, proof)
}
func oidcAccountView(p entity.OIDCProvider, u entity.User, m entity.UserMFA, a *Authentication, b *entity.OIDCBinding) *OIDCAccountView {
	return &OIDCAccountView{Available: p.Enabled && oidcVerified(p), Name: p.Name, Bound: b != nil, ReviewETag: oidcAccountReview(p, u, m, a, b), MFARequired: m.Enabled}
}
func oidcCurrentSession(tx *gorm.DB, a *Authentication, u entity.User, lock bool) error {
	if a == nil || a.User.ID != u.ID || !a.User.CreatedAt.Equal(u.CreatedAt) || a.Session.ID == "" || a.Session.CreatedAt.IsZero() {
		return apperrors.ErrUnauthorized
	}
	var row entity.Session
	q := tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "id"}, a.Session.ID))
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	e := q.Take(&row).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return apperrors.ErrUnauthorized
	}
	if e != nil {
		return e
	}
	if row.ID != a.Session.ID || row.UserID != u.ID || !row.CreatedAt.Equal(a.Session.CreatedAt) || row.TokenHash != secret.SHA256Hex(a.Token) || !row.ExpiresAt.After(time.Now().UTC()) {
		return apperrors.ErrUnauthorized
	}
	return oidcValidatePrimary(tx, row)
}
func (s *Service) AccountOIDC(ctx context.Context, a *Authentication) (*OIDCAccountView, error) {
	if a == nil {
		return nil, apperrors.ErrUnauthorized
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var out *OIDCAccountView
	e := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		u, e := registrationAdmittedUser(tx, a.User.ID, false)
		if e != nil {
			return e
		}
		if e = oidcCurrentSession(tx, a, u, false); e != nil {
			return e
		}
		p, e := oidcProvider(tx)
		if e != nil {
			return e
		}
		m, e := mfaState(tx, u.ID)
		if e != nil {
			return e
		}
		b, e := oidcUserBinding(tx, u)
		if e != nil {
			return e
		}
		out = oidcAccountView(p, u, m, a, b)
		return ctx.Err()
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if e != nil {
		return nil, oidcError(e)
	}
	return out, nil
}

// The rejected flag lets failure counters commit without admitting an operation.
func (s *Service) oidcLocalProof(tx *gorm.DB, a *Authentication, passwordHash string, proof OIDCProof) (entity.User, entity.UserMFA, bool, error) {
	if a == nil {
		return entity.User{}, entity.UserMFA{}, false, apperrors.ErrUnauthorized
	}
	u, e := registrationAdmittedUser(tx, a.User.ID, true)
	if e != nil {
		return u, entity.UserMFA{}, false, e
	}
	if u.PasswordHash != passwordHash {
		return u, entity.UserMFA{}, false, apperrors.ErrUnauthorized
	}
	if e = oidcCurrentSession(tx, a, u, true); e != nil {
		return u, entity.UserMFA{}, false, e
	}
	m, e := mfaState(tx, u.ID)
	if e != nil {
		return u, m, false, e
	}
	now := time.Now().UTC()
	if !m.Enabled {
		if proof.Code != "" || proof.RecoveryCode != "" {
			return u, m, false, apperrors.ErrBadRequest
		}
		return u, m, false, nil
	}
	if (proof.Code == "") == (proof.RecoveryCode == "") {
		return u, m, false, apperrors.ErrBadRequest
	}
	if mfaLocked(&m, now) {
		return u, m, false, apperrors.ErrUnauthorized
	}
	valid, e := s.consumeMFAProof(tx, &m, MFAProof(proof), now)
	if e != nil {
		return u, m, false, e
	}
	if !valid {
		return u, m, true, mfaFailure(tx, &m, now)
	}
	resetMFAFailures(&m)
	e = tx.Save(&m).Error
	return u, m, false, e
}
func (s *Service) UnlinkOIDC(ctx context.Context, a *Authentication, etag string, in OIDCIdentityInput) (*OIDCAccountView, error) {
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
	var out *OIDCAccountView
	var revokedBinding *entity.OIDCBinding
	rejected := false
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		u, m, bad, e := s.oidcLocalProof(tx, a, hash, in.Proof)
		if e != nil {
			return e
		}
		if bad {
			rejected = true
			return nil
		}
		p, e := oidcProvider(tx)
		if e != nil {
			return e
		}
		b, e := oidcUserBinding(tx, u)
		if e != nil {
			return e
		}
		if oidcAccountReview(p, u, m, a, b) != etag {
			return catalogConflict
		}
		if b == nil {
			return catalogConflict
		}
		revokedBinding = b
		e = s.oidcRevoke(tx, b.ID)
		if e != nil {
			return e
		}
		if e = tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "id"}, b.ID)).Delete(&entity.OIDCBinding{}).Error; e != nil {
			return e
		}
		if e = oidcAudit(tx, u, "account.oidc.unlink", p, b, in.Reason); e != nil {
			return e
		}
		out = oidcAccountView(p, u, m, a, nil)
		return ctx.Err()
	})
	if e != nil {
		return nil, oidcError(e)
	}
	if rejected {
		return nil, apperrors.ErrUnauthorized
	}
	if revokedBinding != nil {
		s.invalidateRuntimeOIDCBinding(revokedBinding.ID, revokedBinding.CreatedAt)
		s.publishSessionMutation(ctx)
	}
	return out, nil
}
func oidcBind(tx *gorm.DB, p entity.OIDCProvider, u entity.User, subject string) (entity.OIDCBinding, error) {
	if subject == "" || len(subject) > 255 || !utf8.ValidString(subject) {
		return entity.OIDCBinding{}, apperrors.ErrUnauthorized
	}
	digest := oidcSubjectDigest(p.Issuer, subject)
	existing, e := oidcUserBinding(tx, u)
	if e != nil {
		return entity.OIDCBinding{}, e
	}
	if existing != nil {
		if existing.ConfigRevision != p.ConfigRevision || existing.Subject != subject || existing.SubjectDigest != digest {
			return entity.OIDCBinding{}, catalogConflict
		}
		return *existing, nil
	}
	bid, e := id.NewPrefixed("oib")
	if e != nil {
		return entity.OIDCBinding{}, e
	}
	b := entity.OIDCBinding{ID: bid, CreatedAt: time.Now().UTC().Truncate(time.Microsecond), UserID: u.ID, UserCreatedAt: u.CreatedAt, ConfigRevision: p.ConfigRevision, Subject: subject, SubjectDigest: digest}
	if e = tx.Create(&b).Error; e != nil {
		if errors.Is(e, gorm.ErrDuplicatedKey) {
			return b, catalogConflict
		}
		return b, e
	}
	return b, nil
}
