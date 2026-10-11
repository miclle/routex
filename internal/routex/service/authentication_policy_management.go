package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/secret"
	"gorm.io/gorm"
)

type AuthenticationPolicyPublic struct {
	Enforced              bool `json:"enforced"`
	PasswordLoginEnabled  bool `json:"password_login_enabled"`
	EmergencyEntryEnabled bool `json:"emergency_entry_enabled"`
}
type AuthenticationPolicyCandidate struct {
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"created_at"`
	MFAEnabled bool      `json:"mfa_enabled"`
}
type AuthenticationPolicyView struct {
	Enforced                bool                           `json:"enforced"`
	EmergencyAdminID        *string                        `json:"emergency_admin_id"`
	EmergencyAdminCreatedAt *time.Time                     `json:"emergency_admin_created_at"`
	EmergencyMFAEnabled     *bool                          `json:"emergency_mfa_enabled"`
	OIDCAvailable           bool                           `json:"oidc_available"`
	LDAPAvailable           bool                           `json:"ldap_available"`
	ReviewEmergencyAdmin    *AuthenticationPolicyCandidate `json:"review_emergency_admin"`
	ReviewETag              string                         `json:"review_etag"`
}
type AuthenticationPolicyInput struct {
	Enforced         bool   `json:"enforced"`
	EmergencyAdminID string `json:"emergency_admin_id"`
	Reason           string `json:"reason"`
}
type AuthenticationPolicyResult struct {
	AuthenticationPolicyView
	Confirmation             string `json:"confirmation"`
	RuntimeApplied           bool   `json:"runtime_applied"`
	ReauthenticationRequired bool   `json:"reauthentication_required"`
}

func (v *AuthenticationPolicyInput) UnmarshalJSON(raw []byte) error {
	f, e := registrationStrictObject(raw, []string{"enforced", "emergency_admin_id", "reason"})
	if e != nil {
		return e
	}
	var n AuthenticationPolicyInput
	if json.Unmarshal(f["enforced"], &n.Enforced) != nil || json.Unmarshal(f["emergency_admin_id"], &n.EmergencyAdminID) != nil || json.Unmarshal(f["reason"], &n.Reason) != nil || !validRegistrationReason(n.Reason) || n.Enforced && !authenticationAdminID(n.EmergencyAdminID) || !n.Enforced && n.EmergencyAdminID != "" {
		return apperrors.ErrBadRequest
	}
	*v = n
	return nil
}
func authenticationAdminID(id string) bool { return memberKeyUserID.MatchString(id) }

type authenticationPolicyReview struct {
	Policy          entity.GovernanceSetting
	Candidate       *entity.User
	CandidateMFA    entity.UserMFA
	DesignationMFA  entity.UserMFA
	DesignationUser *entity.User
	OIDC            entity.OIDCProvider
	LDAP            entity.LDAPProvider
}

func loadAuthenticationPolicyReview(tx *gorm.DB, candidateID string) (authenticationPolicyReview, error) {
	var r authenticationPolicyReview
	var err error
	r.Policy, err = authenticationPolicy(tx)
	if err != nil {
		return r, err
	}
	if r.Policy.EmergencyAdminID != "" {
		var u entity.User
		ue := tx.Session(&gorm.Session{NewDB: true}).Take(&u, "id = ?", r.Policy.EmergencyAdminID).Error
		if ue != nil && !errors.Is(ue, gorm.ErrRecordNotFound) {
			return r, ue
		}
		if ue == nil && u.ID == r.Policy.EmergencyAdminID {
			r.DesignationUser = &u
		}
		r.DesignationMFA, err = mfaState(tx, r.Policy.EmergencyAdminID)
		if err != nil {
			return r, err
		}
	}
	if candidateID != "" {
		if !authenticationAdminID(candidateID) {
			return r, apperrors.ErrBadRequest
		}
		u, e := registrationAdmittedUser(tx, candidateID, false)
		if e != nil {
			return r, e
		}
		if u.ID != candidateID || u.Role != entity.RoleAdmin || u.CreatedAt.IsZero() {
			return r, apperrors.ErrForbidden
		}
		r.Candidate = &u
		r.CandidateMFA, e = mfaState(tx, u.ID)
		if e != nil {
			return r, e
		}
	}
	r.OIDC, err = oidcProvider(tx)
	if err != nil {
		return r, err
	}
	r.LDAP, err = ldapProvider(tx)
	return r, err
}
func authenticationReviewETag(r authenticationPolicyReview) string {
	var candidate any
	var designation any
	if r.DesignationUser != nil {
		u := r.DesignationUser
		designation = []any{u.ID, u.CreatedAt.UTC(), u.Role, u.Disabled, u.OffboardedAt}
	}
	if r.Candidate != nil {
		candidate = []any{r.Candidate.ID, r.Candidate.CreatedAt.UTC(), r.Candidate.Role, r.Candidate.Disabled, r.Candidate.OffboardedAt, r.CandidateMFA.Enabled, r.CandidateMFA.Generation}
	}
	p, o, l := r.Policy, r.OIDC, r.LDAP
	raw, _ := json.Marshal([]any{"authentication.policy.review.v1", p.ForcedSSOEnabled, p.AuthenticationPolicyRevision, p.EmergencyAdminID, p.EmergencyAdminCreatedAt, p.RegistrationPolicyRevision, p.RegistrationEnabled, p.RegistrationApprovalRequired, p.RegistrationAllowedEmailDomains, designation, r.DesignationMFA.Enabled, r.DesignationMFA.Generation, candidate, o.ID, o.CreatedAt.UTC(), o.ConfigRevision, o.PolicyRevision, o.Enabled, o.VerifiedConfigRevision, o.VerifiedBy, o.VerifiedUserCreatedAt, o.VerifiedBindingID, o.VerifiedBindingCreatedAt, l.ID, l.CreatedAt.UTC(), l.ConfigRevision, l.PolicyRevision, l.Enabled, l.VerifiedConfigRevision, l.VerifiedBy, l.VerifiedUserCreatedAt, l.VerifiedBindingID, l.VerifiedBindingCreatedAt})
	return secret.SHA256Hex(string(raw))
}
func authenticationReviewView(tx *gorm.DB, r authenticationPolicyReview) (AuthenticationPolicyView, error) {
	p := r.Policy
	v := AuthenticationPolicyView{Enforced: p.ForcedSSOEnabled, OIDCAvailable: r.OIDC.Enabled && oidcVerified(r.OIDC), LDAPAvailable: r.LDAP.Enabled && ldapVerified(r.LDAP), ReviewETag: authenticationReviewETag(r)}
	if p.ForcedSSOEnabled {
		id := p.EmergencyAdminID
		v.EmergencyAdminID = &id
		v.EmergencyAdminCreatedAt = p.EmergencyAdminCreatedAt
		var u entity.User
		e := tx.Session(&gorm.Session{NewDB: true}).Take(&u, "id = ?", id).Error
		if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
			return v, e
		}
		if e == nil && u.ID == id && p.EmergencyAdminCreatedAt != nil && u.CreatedAt.Equal(*p.EmergencyAdminCreatedAt) {
			m, e := mfaState(tx, id)
			if e != nil {
				return v, e
			}
			enabled := m.Enabled
			v.EmergencyMFAEnabled = &enabled
		}
	}
	if r.Candidate != nil {
		v.ReviewEmergencyAdmin = &AuthenticationPolicyCandidate{ID: r.Candidate.ID, CreatedAt: r.Candidate.CreatedAt, MFAEnabled: r.CandidateMFA.Enabled}
	}
	return v, nil
}
func (s *Service) PublicAuthenticationPolicy(ctx context.Context) (*AuthenticationPolicyPublic, error) {
	_, local, cancel := authenticationContexts(ctx)
	defer cancel()
	p, e := authenticationPolicy(s.authDB(local))
	if e != nil {
		return nil, authenticationError(e)
	}
	return &AuthenticationPolicyPublic{Enforced: p.ForcedSSOEnabled, PasswordLoginEnabled: !p.ForcedSSOEnabled, EmergencyEntryEnabled: p.ForcedSSOEnabled}, nil
}

// An empty candidateID always reviews only the stored policy, never a candidate.
func (s *Service) GetAuthenticationPolicy(ctx context.Context, a *Authentication, candidateID string) (*AuthenticationPolicyView, error) {
	_, local, cancel := authenticationContexts(ctx)
	defer cancel()
	var result AuthenticationPolicyView
	e := s.authDB(local).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		if e := authenticationManagementAuthority(tx, a); e != nil {
			return e
		}
		r, e := loadAuthenticationPolicyReview(tx, candidateID)
		if e != nil {
			return e
		}
		result, e = authenticationReviewView(tx, r)
		if e != nil {
			return e
		}
		return local.Err()
	})
	if e != nil {
		return nil, authenticationError(e)
	}
	return &result, nil
}
func authenticationManagementAuthority(tx *gorm.DB, a *Authentication) error {
	if a == nil {
		return apperrors.ErrUnauthorized
	}
	if e := registrationPolicyAuthority(tx, a.User.ID); e != nil {
		return e
	}
	if _, e := registrationAdmittedUser(tx, a.User.ID, true); e != nil {
		return e
	}
	return mfaSession(tx, a, time.Now().UTC())
}
func (s *Service) SetAuthenticationPolicy(ctx context.Context, a *Authentication, etag string, in AuthenticationPolicyInput) (*AuthenticationPolicyResult, error) {
	if !validMemberRoleDigest(etag) || !validRegistrationReason(in.Reason) || in.Enforced && !authenticationAdminID(in.EmergencyAdminID) || !in.Enforced && in.EmergencyAdminID != "" {
		return nil, apperrors.ErrBadRequest
	}
	_, local, cancel := authenticationContexts(ctx)
	defer cancel()
	var result AuthenticationPolicyResult
	var savedPolicy entity.GovernanceSetting
	release := s.pinPersonalKeyMutation()
	defer release()
	e := s.authDB(local).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		if e := authenticationManagementAuthority(tx, a); e != nil {
			return e
		}
		r, e := loadAuthenticationPolicyReview(tx, in.EmergencyAdminID)
		if e != nil {
			return e
		}
		if authenticationReviewETag(r) != etag {
			return catalogConflict
		}
		verifiedSSO := r.OIDC.Enabled && oidcVerified(r.OIDC) || r.LDAP.Enabled && ldapVerified(r.LDAP)
		if in.Enforced && (r.Candidate == nil || !r.CandidateMFA.Enabled || !authenticationMFAGeneration(r.CandidateMFA.Generation) || !verifiedSSO) {
			return catalogConflict
		}
		p := r.Policy
		changed := p.ForcedSSOEnabled != in.Enforced || p.EmergencyAdminID != in.EmergencyAdminID || in.Enforced && (p.EmergencyAdminCreatedAt == nil || !p.EmergencyAdminCreatedAt.Equal(r.Candidate.CreatedAt))
		if changed {
			revision, e := newMemberRoleRevision()
			if e != nil {
				return e
			}
			p.AuthenticationPolicyRevision = revision
			p.ForcedSSOEnabled = in.Enforced
			p.EmergencyAdminID = in.EmergencyAdminID
			p.EmergencyAdminCreatedAt = nil
			if in.Enforced {
				birth := r.Candidate.CreatedAt
				p.EmergencyAdminCreatedAt = &birth
				p.RegistrationEnabled = false
				p.RegistrationPolicyRevision, e = newMemberRoleRevision()
				if e != nil {
					return e
				}
			}
			if e = tx.Session(&gorm.Session{NewDB: true}).Save(&p).Error; e != nil {
				return e
			}
			sessionQuery := tx.Session(&gorm.Session{NewDB: true}).Where("primary_method = ?", "emergency")
			if in.Enforced {
				sessionQuery = tx.Session(&gorm.Session{NewDB: true}).Where("primary_method NOT IN ?", []string{"oidc", "ldap"})
			}
			if e = sessionQuery.Delete(&entity.Session{}).Error; e != nil {
				return e
			}
			challengeQuery := tx.Session(&gorm.Session{NewDB: true}).Where("purpose = ?", "emergency")
			if in.Enforced {
				challengeQuery = tx.Session(&gorm.Session{NewDB: true}).Where("purpose = ? OR (purpose IN ? AND primary_method NOT IN ?)", "emergency", []string{"login"}, []string{"oidc", "ldap"}).Or("purpose = ? AND session_id NOT IN (?)", "enrollment", tx.Session(&gorm.Session{NewDB: true}).Model(&entity.Session{}).Select("id"))
			}
			if e = challengeQuery.Delete(&entity.MFAChallenge{}).Error; e != nil {
				return e
			}
			if in.Enforced {
				// Terminalize excluded login ceremonies too: disabling later must not
				// revive an authorization response admitted before the enforcement cutoff.
				for _, table := range []any{&entity.OAuthCeremony{}, &entity.NamedIdentityCeremony{}} {
					if e = tx.Session(&gorm.Session{NewDB: true}).Model(table).Where("purpose = ? AND status IN ?", "login", []string{"pending", "exchanging", "verified"}).Update("status", "failed").Error; e != nil {
						return e
					}
				}
				if e = tx.Session(&gorm.Session{NewDB: true}).Model(&entity.SAMLCeremony{}).Where("purpose = ? AND status IN ?", "login", []string{"pending", "validating", "verified"}).Update("status", "failed").Error; e != nil {
					return e
				}
			}
			if e = appendAuthenticationPolicyAudit(tx, a.User.ID, p, in.Reason); e != nil {
				return e
			}
		}
		r, e = loadAuthenticationPolicyReview(tx, in.EmergencyAdminID)
		if e != nil {
			return e
		}
		result.AuthenticationPolicyView, e = authenticationReviewView(tx, r)
		if e != nil {
			return e
		}
		// Deletion is part of this transaction, so the saved receipt is built without
		// requiring another protected read by a caller whose Session was just revoked.
		result.ReauthenticationRequired = changed && (a.Session.PrimaryMethod == "emergency" || in.Enforced && a.Session.PrimaryMethod != "oidc" && a.Session.PrimaryMethod != "ldap")
		if !result.ReauthenticationRequired {
			if e = mfaSession(tx, a, time.Now().UTC()); e != nil {
				return e
			}
		}
		savedPolicy = p
		result.Confirmation = "saved"
		return local.Err()
	})
	if e != nil {
		return nil, authenticationError(e)
	}
	if s.runtime != nil {
		policy := runtimePolicy(savedPolicy)
		s.runtime.authenticationPolicyFence.Store(&policy)
		s.runtime.epoch.Add(1)
	}
	release()
	if s.runtime != nil && s.RefreshRuntime(local) == nil {
		auth := s.runtime.auth.Load()
		result.RuntimeApplied = auth != nil && auth.AuthenticationPolicy.Revision == savedPolicy.AuthenticationPolicyRevision
	}
	return &result, nil
}
func appendAuthenticationPolicyAudit(tx *gorm.DB, actor string, p entity.GovernanceSetting, reason string) error {
	raw, e := json.Marshal(struct {
		Version  int    `json:"version"`
		Enforced bool   `json:"enforced"`
		Revision string `json:"revision"`
		Reason   string `json:"reason"`
	}{1, p.ForcedSSOEnabled, p.AuthenticationPolicyRevision, strings.TrimSpace(reason)})
	if e != nil {
		return e
	}
	aid, e := id.NewPrefixed("aud")
	if e != nil {
		return e
	}
	detail := string(raw)
	return tx.Create(&entity.AuditEvent{ID: aid, ActorID: actor, Action: "authentication.policy.update", ResourceType: "authentication_policy", ResourceID: "1", DetailsJSON: &detail}).Error
}
