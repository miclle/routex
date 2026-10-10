package service

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"time"
	"unicode/utf8"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	directory "github.com/miclle/routex/pkg/ldap"
	"github.com/miclle/routex/pkg/upstream"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type LDAPLoginInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
}
type LDAPBindingInput struct {
	Password          string    `json:"password"`
	Proof             LDAPProof `json:"proof"`
	Reason            string    `json:"reason"`
	Username          string    `json:"username"`
	DirectoryPassword string    `json:"directory_password"`
}
type LDAPResult struct {
	Kind string `json:"kind"`
}
type LDAPLoginResult struct {
	Authentication *Authentication
	Challenge      *MFALoginChallenge
}

func ldapUsername(v string) bool {
	if v == "" || len(v) > 256 || !utf8.ValidString(v) {
		return false
	}
	for _, r := range v {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}
func ldapPassword(v string) bool { return len(v) > 0 && len(v) <= 4096 && utf8.ValidString(v) }
func (in *LDAPLoginInput) UnmarshalJSON(raw []byte) error {
	f, e := registrationStrictObject(raw, []string{"username", "password"})
	if e != nil {
		return e
	}
	var v LDAPLoginInput
	if json.Unmarshal(f["username"], &v.Username) != nil || json.Unmarshal(f["password"], &v.Password) != nil || !ldapUsername(v.Username) || !ldapPassword(v.Password) {
		return apperrors.ErrBadRequest
	}
	*in = v
	return nil
}
func (in *LDAPBindingInput) UnmarshalJSON(raw []byte) error {
	f, e := registrationStrictObject(raw, []string{"password", "proof", "reason", "username", "directory_password"})
	if e != nil {
		return e
	}
	var v LDAPBindingInput
	if json.Unmarshal(f["password"], &v.Password) != nil || json.Unmarshal(f["proof"], &v.Proof) != nil || json.Unmarshal(f["reason"], &v.Reason) != nil || json.Unmarshal(f["username"], &v.Username) != nil || json.Unmarshal(f["directory_password"], &v.DirectoryPassword) != nil || !validPassword(v.Password) || !validRegistrationReason(v.Reason) || !ldapUsername(v.Username) || !ldapPassword(v.DirectoryPassword) {
		return apperrors.ErrBadRequest
	}
	*in = v
	return nil
}

// Only bootstrap network policy is supplied. Application configuration cannot
// persist an insecure TLS switch, custom trust roots or an alternate connector.
func ldapComponent(endpoint, bindDN, password, baseDN, filter, attribute string, allowPrivate bool) (*directory.Client, error) {
	return directory.New(directory.Config{Endpoint: endpoint, BindDN: bindDN, BindPassword: password, BaseDN: baseDN, UserFilter: filter, IdentityAttribute: directory.IdentityAttribute(attribute), TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12}, DialContext: upstream.NewTCPDialer(allowPrivate), EndpointPolicy: func(ctx context.Context, u *url.URL) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		return upstream.ValidateLDAPEndpoint(u, allowPrivate)
	}})
}
func (s *Service) ldapAuthenticate(ctx context.Context, p entity.LDAPProvider, username, password string) (directory.Identity, error) {
	plain, e := s.openSecret(rootReference("ldap_providers", p.ID, p.SecretGeneration), p.AuthCiphertext)
	if e != nil {
		return directory.Identity{}, ldapUnavailable
	}
	c, e := ldapComponent(p.Endpoint, p.BindDN, plain, p.BaseDN, p.UserFilter, p.IdentityAttribute, s.allowPrivateUpstream)
	if e != nil {
		return directory.Identity{}, ldapUnavailable
	}
	identity, e := c.Authenticate(ctx, username, password)
	if errors.Is(e, directory.ErrAuthentication) || errors.Is(e, directory.ErrInvalidInput) {
		return directory.Identity{}, apperrors.ErrUnauthorized
	}
	if e != nil || ctx.Err() != nil || string(identity.Attribute) != p.IdentityAttribute || !ldapSubjectValid(p.IdentityAttribute, identity.Subject) {
		return directory.Identity{}, ldapUnavailable
	}
	return identity, nil
}

// A request-local snapshot contains no user directory password or returned user DN. No transaction
// or governance lock spans directory I/O; the final transaction compares all
// exact captured authority before consuming a local one-time MFA proof.
type ldapIdentityCapture struct {
	Provider entity.LDAPProvider
	User     entity.User
	MFA      entity.UserMFA
	Binding  *entity.LDAPBinding
	Review   string
}

func ldapSameProvider(a, b entity.LDAPProvider) bool {
	return a.ID == b.ID && a.CreatedAt.Equal(b.CreatedAt) && a.ConfigRevision == b.ConfigRevision && a.PolicyRevision == b.PolicyRevision && a.ReviewRevision == b.ReviewRevision && a.AuthCiphertext == b.AuthCiphertext && a.SecretGeneration == b.SecretGeneration
}
func ldapSameBinding(a, b *entity.LDAPBinding) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.ID == b.ID && a.CreatedAt.Equal(b.CreatedAt) && a.UserID == b.UserID && a.UserCreatedAt.Equal(b.UserCreatedAt) && a.ProviderID == b.ProviderID && a.ConfigRevision == b.ConfigRevision && a.IdentityAttribute == b.IdentityAttribute && a.Subject == b.Subject && a.SubjectDigest == b.SubjectDigest
}
func (s *Service) BindLDAP(ctx context.Context, a *Authentication, etag string, in LDAPBindingInput) (*LDAPResult, error) {
	return s.ldapIdentity(ctx, a, etag, in, false)
}
func (s *Service) VerifyLDAP(ctx context.Context, a *Authentication, etag string, in LDAPBindingInput) (*LDAPResult, error) {
	return s.ldapIdentity(ctx, a, etag, in, true)
}
func (s *Service) ldapIdentity(parent context.Context, a *Authentication, etag string, in LDAPBindingInput, verify bool) (*LDAPResult, error) {
	if a == nil {
		return nil, apperrors.ErrUnauthorized
	}
	if !validMemberRoleDigest(etag) || !validRegistrationReason(in.Reason) || !ldapUsername(in.Username) || !ldapPassword(in.DirectoryPassword) {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	hash, e := s.mfaPassword(ctx, a.User.ID, in.Password)
	if e != nil {
		return nil, e
	}
	var captured ldapIdentityCapture
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		u, e := registrationAdmittedUser(tx, a.User.ID, false)
		if e != nil {
			return e
		}
		if u.PasswordHash != hash {
			return apperrors.ErrUnauthorized
		}
		if e = ldapCurrentSession(tx, a, u, false); e != nil {
			return e
		}
		m, e := mfaState(tx, u.ID)
		if e != nil {
			return e
		}
		if mfaLocked(&m, time.Now().UTC()) {
			return apperrors.ErrUnauthorized
		}
		if m.Enabled {
			if (in.Proof.Code == "") == (in.Proof.RecoveryCode == "") {
				return apperrors.ErrBadRequest
			}
		} else if in.Proof.Code != "" || in.Proof.RecoveryCode != "" {
			return apperrors.ErrBadRequest
		}
		p, e := ldapProvider(tx)
		if e != nil {
			return e
		}
		b, e := ldapUserBinding(tx, u)
		if e != nil {
			return e
		}
		review := ldapAccountReview(p, u, m, a, b)
		if verify {
			if e = registrationPolicyAuthority(tx, u.ID); e != nil {
				return e
			}
			review = ldapReview(p, u, m)
		} else if !p.Enabled || !ldapVerified(p) {
			return apperrors.ErrUnauthorized
		}
		if p.AuthCiphertext == "" || review != etag {
			return catalogConflict
		}
		captured = ldapIdentityCapture{Provider: p, User: u, MFA: m, Binding: b, Review: review}
		return ctx.Err()
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if e != nil {
		return nil, ldapError(e)
	}
	identity, e := s.ldapAuthenticate(ctx, captured.Provider, in.Username, in.DirectoryPassword)
	if e != nil {
		return nil, ldapError(e)
	}
	rejected := false
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		if e := lockGovernance(tx); e != nil {
			return e
		}
		u, e := registrationAdmittedUser(tx, a.User.ID, true)
		if e != nil {
			return e
		}
		if !u.CreatedAt.Equal(captured.User.CreatedAt) || u.PasswordHash != captured.User.PasswordHash {
			return apperrors.ErrUnauthorized
		}
		if e = ldapCurrentSession(tx, a, u, true); e != nil {
			return e
		}
		m, e := mfaState(tx, u.ID)
		if e != nil {
			return e
		}
		if m.Enabled != captured.MFA.Enabled || m.Generation != captured.MFA.Generation {
			return apperrors.ErrUnauthorized
		}
		p, e := ldapProvider(tx)
		if e != nil {
			return e
		}
		if !ldapSameProvider(p, captured.Provider) {
			return catalogConflict
		}
		b, e := ldapUserBinding(tx, u)
		if e != nil {
			return e
		}
		if !ldapSameBinding(b, captured.Binding) {
			return catalogConflict
		}
		review := ldapAccountReview(p, u, m, a, b)
		if verify {
			if e = registrationPolicyAuthority(tx, u.ID); e != nil {
				return e
			}
			review = ldapReview(p, u, m)
		} else if !p.Enabled || !ldapVerified(p) {
			return apperrors.ErrUnauthorized
		}
		if review != captured.Review {
			return catalogConflict
		}
		_, _, bad, e := s.ldapLocalProof(tx, a, hash, in.Proof)
		if e != nil {
			return e
		}
		if bad {
			rejected = true
			return nil
		}
		bound, e := ldapBind(tx, p, u, string(identity.Attribute), identity.Subject)
		if e != nil {
			return e
		}
		if verify {
			p.VerifiedConfigRevision = p.ConfigRevision
			p.VerifiedBy = u.ID
			p.VerifiedUserCreatedAt = &u.CreatedAt
			p.VerifiedBindingID = bound.ID
			p.VerifiedBindingCreatedAt = &bound.CreatedAt
			p.ReviewRevision, e = ldapNewRevision()
			if e != nil {
				return e
			}
			if e = tx.Save(&p).Error; e != nil {
				return e
			}
			if e = ldapAudit(tx, u, "identity.ldap.verify", p, &bound, in.Reason); e != nil {
				return e
			}
		} else if e = ldapAudit(tx, u, "account.ldap.bind", p, &bound, in.Reason); e != nil {
			return e
		}
		return ctx.Err()
	})
	if e != nil {
		return nil, ldapError(e)
	}
	if rejected {
		return nil, apperrors.ErrUnauthorized
	}
	kind := "bound"
	if verify {
		kind = "verified"
	}
	return &LDAPResult{Kind: kind}, nil
}
func (s *Service) LoginLDAP(parent context.Context, in LDAPLoginInput) (*LDAPLoginResult, error) {
	if !ldapUsername(in.Username) || !ldapPassword(in.Password) {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	var captured entity.LDAPProvider
	started := time.Now().UTC()
	e := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		p, e := ldapProvider(tx)
		if e != nil {
			return e
		}
		if !p.Enabled || !ldapVerified(p) {
			return apperrors.ErrUnauthorized
		}
		captured = p
		return ctx.Err()
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if e != nil {
		return nil, ldapError(e)
	}
	identity, e := s.ldapAuthenticate(ctx, captured, in.Username, in.Password)
	if e != nil {
		return nil, ldapError(e)
	}
	out := &LDAPLoginResult{}
	e = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		if e := lockGovernance(tx); e != nil {
			return e
		}
		p, e := ldapProvider(tx)
		if e != nil {
			return e
		}
		if !ldapSameProvider(p, captured) || !p.Enabled || !ldapVerified(p) {
			return apperrors.ErrUnauthorized
		}
		subject := base64.StdEncoding.EncodeToString(identity.Subject)
		digest := ldapSubjectDigest(string(identity.Attribute), subject)
		var b entity.LDAPBinding
		if e = tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "subject_digest"}, digest)).Take(&b).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return apperrors.ErrUnauthorized
			}
			return e
		}
		if b.ID == "" || b.CreatedAt.IsZero() || b.CreatedAt.After(started) || b.ProviderID != p.ID || b.ConfigRevision != p.ConfigRevision || b.IdentityAttribute != string(identity.Attribute) || b.Subject != subject || b.SubjectDigest != digest || !ldapStoredSubject(b) {
			return apperrors.ErrUnauthorized
		}
		u, e := registrationAdmittedUser(tx, b.UserID, true)
		if e != nil {
			return e
		}
		if !u.CreatedAt.Equal(b.UserCreatedAt) {
			return apperrors.ErrUnauthorized
		}
		m, e := mfaState(tx, u.ID)
		if e != nil {
			return e
		}
		if m.Enabled {
			out.Challenge, e = issueMFALoginChallenge(tx, u, m)
			if e != nil {
				return e
			}
			if e = ldapStampChallenge(tx, u, p, b, out.Challenge); e != nil {
				return e
			}
		} else {
			out.Authentication, e = createSession(tx, u)
			if e != nil {
				return e
			}
			if e = ldapStampSession(tx, out.Authentication, p, b); e != nil {
				return e
			}
			if e = recordSuccessfulLogin(tx, &out.Authentication.User); e != nil {
				return e
			}
			if e = appendAudit(tx, u.ID, "identity.ldap.login", "user", u.ID); e != nil {
				return e
			}
		}
		return ctx.Err()
	})
	if e != nil {
		return nil, ldapError(e)
	}
	if out.Authentication != nil {
		s.publishSessionMutation(ctx)
	}
	return out, nil
}
