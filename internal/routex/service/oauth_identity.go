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

type OAuthProof struct {
	Code         string `json:"code,omitempty"`
	RecoveryCode string `json:"recovery_code,omitempty"`
}
type OAuthIdentityInput struct {
	Password string     `json:"password"`
	Proof    OAuthProof `json:"proof"`
	Reason   string     `json:"reason"`
}
type OAuthAccountView struct {
	Available   bool   `json:"available"`
	Name        string `json:"name"`
	Bound       bool   `json:"bound"`
	ReviewETag  string `json:"review_etag"`
	MFARequired bool   `json:"mfa_required"`
}

func (v *OAuthProof) UnmarshalJSON(raw []byte) error {
	if len(raw) > 4096 || !utf8.Valid(raw) || !modelCreationUnicode(raw) {
		return apperrors.ErrBadRequest
	}
	f, e := memberRolesObject(raw)
	if e != nil || len(f) > 1 {
		return apperrors.ErrBadRequest
	}
	var out OAuthProof
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
func (v *OAuthIdentityInput) UnmarshalJSON(raw []byte) error {
	f, e := registrationStrictObject(raw, []string{"password", "proof", "reason"})
	if e != nil {
		return e
	}
	var out OAuthIdentityInput
	if json.Unmarshal(f["password"], &out.Password) != nil || json.Unmarshal(f["proof"], &out.Proof) != nil || json.Unmarshal(f["reason"], &out.Reason) != nil || !validPassword(out.Password) || !validRegistrationReason(out.Reason) {
		return apperrors.ErrBadRequest
	}
	*v = out
	return nil
}
func oauthSubject(kind, subject string) bool {
	if !oauthText(subject, 256) {
		return false
	}
	if kind == "string" {
		return true
	}
	if kind != "integer" || len(subject) > 1 && subject[0] == '0' {
		return false
	}
	for _, c := range subject {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
func oauthSubjectDigest(provider, kind, subject string) string {
	return oauthDigest("oauth.subject.v1", provider, kind, subject)
}
func oauthBindingByID(tx *gorm.DB, bindingID string) (entity.OAuthBinding, error) {
	var b entity.OAuthBinding
	e := tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "id"}, bindingID)).Take(&b).Error
	if errors.Is(e, gorm.ErrRecordNotFound) || e == nil && (b.ID != bindingID || b.ProviderID != "oauth" || !oauthSubject(b.SubjectKind, b.Subject) || b.CreatedAt.IsZero()) {
		e = apperrors.ErrUnauthorized
	}
	return b, e
}
func oauthUserBinding(tx *gorm.DB, u entity.User) (*entity.OAuthBinding, error) {
	var b entity.OAuthBinding
	e := tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "user_id"}, u.ID)).Take(&b).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	if b.ProviderID != "oauth" || !oauthSubject(b.SubjectKind, b.Subject) || b.UserID != u.ID || !b.UserCreatedAt.Equal(u.CreatedAt) || b.ID == "" || b.CreatedAt.IsZero() {
		return nil, apperrors.ErrUnauthorized
	}
	return &b, nil
}
func oauthAccountReview(p entity.OAuthProvider, u entity.User, m entity.UserMFA, a *Authentication, b *entity.OAuthBinding) string {
	var proof any
	if b != nil {
		proof = []any{b.ID, b.CreatedAt.UTC(), b.ConfigRevision, b.SubjectDigest}
	}
	return oauthDigest("oauth.account.review.v1", p.ReviewRevision, p.ConfigRevision, p.PolicyRevision, u.ID, u.CreatedAt.UTC(), a.Session.ID, a.Session.CreatedAt.UTC(), m.Enabled, m.Generation, proof)
}
func oauthAccountView(p entity.OAuthProvider, u entity.User, m entity.UserMFA, a *Authentication, b *entity.OAuthBinding) *OAuthAccountView {
	return &OAuthAccountView{Available: p.Enabled && oauthVerified(p), Name: p.Name, Bound: b != nil, ReviewETag: oauthAccountReview(p, u, m, a, b), MFARequired: m.Enabled}
}
func oauthCurrentSession(tx *gorm.DB, a *Authentication, u entity.User, lock bool) error {
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
	return primaryValidateSession(tx, row)
}
func (s *Service) AccountOAuth(ctx context.Context, a *Authentication) (*OAuthAccountView, error) {
	if a == nil {
		return nil, apperrors.ErrUnauthorized
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var out *OAuthAccountView
	e := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		u, e := registrationAdmittedUser(tx, a.User.ID, false)
		if e != nil {
			return e
		}
		if e = oauthCurrentSession(tx, a, u, false); e != nil {
			return e
		}
		p, e := oauthProvider(tx)
		if e != nil {
			return e
		}
		m, e := mfaState(tx, u.ID)
		if e != nil {
			return e
		}
		b, e := oauthUserBinding(tx, u)
		if e != nil {
			return e
		}
		out = oauthAccountView(p, u, m, a, b)
		return ctx.Err()
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if e != nil {
		return nil, oauthError(e)
	}
	return out, nil
}

// The rejected flag lets failure counters commit without admitting an operation.
func (s *Service) oauthLocalProof(tx *gorm.DB, a *Authentication, passwordHash string, proof OAuthProof) (entity.User, entity.UserMFA, bool, error) {
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
	if e = oauthCurrentSession(tx, a, u, true); e != nil {
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
func (s *Service) UnlinkOAuth(ctx context.Context, a *Authentication, etag string, in OAuthIdentityInput) (*OAuthAccountView, error) {
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
	var out *OAuthAccountView
	var revokedBinding *entity.OAuthBinding
	rejected := false
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		u, m, bad, e := s.oauthLocalProof(tx, a, hash, in.Proof)
		if e != nil {
			return e
		}
		if bad {
			rejected = true
			return nil
		}
		p, e := oauthProvider(tx)
		if e != nil {
			return e
		}
		b, e := oauthUserBinding(tx, u)
		if e != nil {
			return e
		}
		if oauthAccountReview(p, u, m, a, b) != etag {
			return catalogConflict
		}
		if b == nil {
			return catalogConflict
		}
		revokedBinding = b
		e = s.oauthRevoke(tx, b.ID)
		if e != nil {
			return e
		}
		if e = tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "id"}, b.ID)).Delete(&entity.OAuthBinding{}).Error; e != nil {
			return e
		}
		if e = oauthAudit(tx, u, "account.oauth.unlink", p, b, in.Reason); e != nil {
			return e
		}
		out = oauthAccountView(p, u, m, a, nil)
		return ctx.Err()
	})
	if e != nil {
		return nil, oauthError(e)
	}
	if rejected {
		return nil, apperrors.ErrUnauthorized
	}
	if revokedBinding != nil {
		s.invalidateRuntimeOAuthBinding(revokedBinding.ID, revokedBinding.CreatedAt)
		s.publishSessionMutation(ctx)
	}
	return out, nil
}
func oauthBind(tx *gorm.DB, p entity.OAuthProvider, u entity.User, kind, subject string) (entity.OAuthBinding, error) {
	if p.ID != "oauth" || !oauthSubject(kind, subject) {
		return entity.OAuthBinding{}, apperrors.ErrUnauthorized
	}
	digest := oauthSubjectDigest(p.ID, kind, subject)
	existing, e := oauthUserBinding(tx, u)
	if e != nil {
		return entity.OAuthBinding{}, e
	}
	if existing != nil {
		if existing.ProviderID != p.ID || existing.ConfigRevision != p.ConfigRevision || existing.SubjectKind != kind || existing.Subject != subject || existing.SubjectDigest != digest {
			return entity.OAuthBinding{}, catalogConflict
		}
		return *existing, nil
	}
	bid, e := id.NewPrefixed("oab")
	if e != nil {
		return entity.OAuthBinding{}, e
	}
	b := entity.OAuthBinding{ID: bid, ProviderID: p.ID, SubjectKind: kind, CreatedAt: time.Now().UTC().Truncate(time.Microsecond), UserID: u.ID, UserCreatedAt: u.CreatedAt, ConfigRevision: p.ConfigRevision, Subject: subject, SubjectDigest: digest}
	if e = tx.Create(&b).Error; e != nil {
		if errors.Is(e, gorm.ErrDuplicatedKey) {
			return b, catalogConflict
		}
		return b, e
	}
	return b, nil
}
