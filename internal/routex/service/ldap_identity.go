package service

import (
	"context"
	"database/sql"
	"encoding/base64"
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

type LDAPProof struct {
	Code         string `json:"code,omitempty"`
	RecoveryCode string `json:"recovery_code,omitempty"`
}
type LDAPIdentityInput struct {
	Password string    `json:"password"`
	Proof    LDAPProof `json:"proof"`
	Reason   string    `json:"reason"`
}
type LDAPAccountView struct {
	Available   bool   `json:"available"`
	Name        string `json:"name"`
	Bound       bool   `json:"bound"`
	ReviewETag  string `json:"review_etag"`
	MFARequired bool   `json:"mfa_required"`
}

func (v *LDAPProof) UnmarshalJSON(raw []byte) error {
	if len(raw) > 4096 || !utf8.Valid(raw) || !modelCreationUnicode(raw) {
		return apperrors.ErrBadRequest
	}
	f, e := memberRolesObject(raw)
	if e != nil || len(f) > 1 {
		return apperrors.ErrBadRequest
	}
	var out LDAPProof
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
func (v *LDAPIdentityInput) UnmarshalJSON(raw []byte) error {
	f, e := registrationStrictObject(raw, []string{"password", "proof", "reason"})
	if e != nil {
		return e
	}
	var out LDAPIdentityInput
	if json.Unmarshal(f["password"], &out.Password) != nil || json.Unmarshal(f["proof"], &out.Proof) != nil || json.Unmarshal(f["reason"], &out.Reason) != nil || !validPassword(out.Password) || !validRegistrationReason(out.Reason) {
		return apperrors.ErrBadRequest
	}
	*v = out
	return nil
}
func ldapSubjectDigest(attribute, subject string) string {
	return ldapDigest("ldap.subject.v1", "ldap", attribute, subject)
}
func ldapStoredSubject(b entity.LDAPBinding) bool {
	raw, e := base64.StdEncoding.DecodeString(b.Subject)
	return e == nil && base64.StdEncoding.EncodeToString(raw) == b.Subject && ldapSubjectValid(b.IdentityAttribute, raw) && b.SubjectDigest == ldapSubjectDigest(b.IdentityAttribute, b.Subject)
}
func ldapSubjectValid(attribute string, raw []byte) bool {
	if attribute == "objectGUID" {
		if len(raw) != 16 {
			return false
		}
		for _, v := range raw {
			if v != 0 {
				return true
			}
		}
		return false
	}
	if attribute != "entryUUID" || len(raw) != 36 {
		return false
	}
	nonzero := false
	for i, c := range raw {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
			continue
		}
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
		nonzero = nonzero || c != '0'
	}
	return nonzero
}
func ldapBindingByID(tx *gorm.DB, bindingID string) (entity.LDAPBinding, error) {
	var b entity.LDAPBinding
	e := tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "id"}, bindingID)).Take(&b).Error
	if errors.Is(e, gorm.ErrRecordNotFound) || e == nil && (b.ID != bindingID || b.ProviderID != "ldap" || !ldapStoredSubject(b) || b.CreatedAt.IsZero()) {
		e = apperrors.ErrUnauthorized
	}
	return b, e
}
func ldapUserBinding(tx *gorm.DB, u entity.User) (*entity.LDAPBinding, error) {
	var b entity.LDAPBinding
	e := tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "user_id"}, u.ID)).Take(&b).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	if b.ProviderID != "ldap" || !ldapStoredSubject(b) || b.UserID != u.ID || !b.UserCreatedAt.Equal(u.CreatedAt) || b.ID == "" || b.CreatedAt.IsZero() {
		return nil, apperrors.ErrUnauthorized
	}
	return &b, nil
}
func ldapAccountReview(p entity.LDAPProvider, u entity.User, m entity.UserMFA, a *Authentication, b *entity.LDAPBinding) string {
	var proof any
	if b != nil {
		proof = []any{b.ID, b.CreatedAt.UTC(), b.ConfigRevision, b.SubjectDigest}
	}
	return ldapDigest("ldap.account.review.v1", p.ReviewRevision, p.ConfigRevision, p.PolicyRevision, u.ID, u.CreatedAt.UTC(), a.Session.ID, a.Session.CreatedAt.UTC(), m.Enabled, m.Generation, proof)
}
func ldapAccountView(p entity.LDAPProvider, u entity.User, m entity.UserMFA, a *Authentication, b *entity.LDAPBinding) *LDAPAccountView {
	return &LDAPAccountView{Available: p.Enabled && ldapVerified(p), Name: p.Name, Bound: b != nil, ReviewETag: ldapAccountReview(p, u, m, a, b), MFARequired: m.Enabled}
}
func ldapCurrentSession(tx *gorm.DB, a *Authentication, u entity.User, lock bool) error {
	return oauthCurrentSession(tx, a, u, lock)
}
func (s *Service) AccountLDAP(ctx context.Context, a *Authentication) (*LDAPAccountView, error) {
	if a == nil {
		return nil, apperrors.ErrUnauthorized
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var out *LDAPAccountView
	e := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		u, e := registrationAdmittedUser(tx, a.User.ID, false)
		if e != nil {
			return e
		}
		if e = ldapCurrentSession(tx, a, u, false); e != nil {
			return e
		}
		p, e := ldapProvider(tx)
		if e != nil {
			return e
		}
		m, e := mfaState(tx, u.ID)
		if e != nil {
			return e
		}
		b, e := ldapUserBinding(tx, u)
		if e != nil {
			return e
		}
		out = ldapAccountView(p, u, m, a, b)
		return ctx.Err()
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if e != nil {
		return nil, ldapError(e)
	}
	return out, nil
}

func (s *Service) ldapLocalProof(tx *gorm.DB, a *Authentication, passwordHash string, proof LDAPProof) (entity.User, entity.UserMFA, bool, error) {
	return s.oauthLocalProof(tx, a, passwordHash, OAuthProof(proof))
}
func (s *Service) UnlinkLDAP(ctx context.Context, a *Authentication, etag string, in LDAPIdentityInput) (*LDAPAccountView, error) {
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
	var out *LDAPAccountView
	var revokedBinding *entity.LDAPBinding
	rejected := false
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		u, m, bad, e := s.ldapLocalProof(tx, a, hash, in.Proof)
		if e != nil {
			return e
		}
		if bad {
			rejected = true
			return nil
		}
		p, e := ldapProvider(tx)
		if e != nil {
			return e
		}
		b, e := ldapUserBinding(tx, u)
		if e != nil {
			return e
		}
		if ldapAccountReview(p, u, m, a, b) != etag {
			return catalogConflict
		}
		if b == nil {
			return catalogConflict
		}
		revokedBinding = b
		e = s.ldapRevoke(tx, b.ID)
		if e != nil {
			return e
		}
		if e = tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "id"}, b.ID)).Delete(&entity.LDAPBinding{}).Error; e != nil {
			return e
		}
		if e = ldapAudit(tx, u, "account.ldap.unlink", p, b, in.Reason); e != nil {
			return e
		}
		out = ldapAccountView(p, u, m, a, nil)
		return ctx.Err()
	})
	if e != nil {
		return nil, ldapError(e)
	}
	if rejected {
		return nil, apperrors.ErrUnauthorized
	}
	if revokedBinding != nil {
		s.invalidateRuntimeLDAPBinding(revokedBinding.ID, revokedBinding.CreatedAt)
		s.publishSessionMutation(ctx)
	}
	return out, nil
}
func ldapBind(tx *gorm.DB, p entity.LDAPProvider, u entity.User, attribute string, subject []byte) (entity.LDAPBinding, error) {
	if p.ID != "ldap" || p.IdentityAttribute != attribute || !ldapSubjectValid(attribute, subject) {
		return entity.LDAPBinding{}, apperrors.ErrUnauthorized
	}
	value := base64.StdEncoding.EncodeToString(subject)
	digest := ldapSubjectDigest(attribute, value)
	existing, e := ldapUserBinding(tx, u)
	if e != nil {
		return entity.LDAPBinding{}, e
	}
	if existing != nil {
		if existing.ConfigRevision != p.ConfigRevision || existing.IdentityAttribute != attribute || existing.Subject != value || existing.SubjectDigest != digest {
			return entity.LDAPBinding{}, catalogConflict
		}
		return *existing, nil
	}
	bid, e := id.NewPrefixed("ldb")
	if e != nil {
		return entity.LDAPBinding{}, e
	}
	b := entity.LDAPBinding{ID: bid, ProviderID: p.ID, IdentityAttribute: attribute, Subject: value, SubjectDigest: digest, UserID: u.ID, UserCreatedAt: u.CreatedAt, ConfigRevision: p.ConfigRevision, CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
	if e = tx.Create(&b).Error; errors.Is(e, gorm.ErrDuplicatedKey) {
		e = catalogConflict
	}
	return b, e
}
