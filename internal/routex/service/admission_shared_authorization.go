package service

import (
	"slices"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

// This is a request snapshot, not an authorization receipt. A future SQL
// authority must check current rows at Accept and every MarkDispatch. Only the
// explicitly projected fields below may enter the spool or admission digest.
type sharedCapturedAuthorization struct {
	Version        int
	EvaluatedAt    time.Time
	Kind           string
	Model          sharedAuthorizationBirth
	User           *sharedAuthorizationUser
	PersonalGrants *sharedAuthorizationPersonalGrants
	Key            *sharedAuthorizationKey
	Project        *sharedAuthorizationBirth
	Session        *sharedAuthorizationSession
	Team           *sharedAuthorizationTeam
}

type sharedAuthorizationBirth struct {
	ID        string
	CreatedAt time.Time
}

type sharedAuthorizationUser struct {
	sharedAuthorizationBirth
	AdmissionState       string
	ApplicationID        string
	ApplicationCreatedAt *time.Time
	AdmissionRevision    string
}

type sharedAuthorizationPersonalGrants struct {
	Revision string
	Hash     string
}

type sharedAuthorizationKey struct {
	sharedAuthorizationBirth
	ExpiresAt         *time.Time
	LifecycleRevision string // Personal Keys only; Project Keys have no such column.
}

type sharedAuthorizationSession struct {
	sharedAuthorizationBirth
	ExpiresAt                    time.Time
	AuthenticationPolicyRevision string
	ForcedSSO                    bool
	Primary                      sharedAuthorizationPrimary
}

type sharedAuthorizationPrimary struct {
	Method    string // The existing empty method denotes local authentication.
	Binding   *sharedAuthorizationBinding
	Emergency *sharedAuthorizationEmergency
}

type sharedAuthorizationBinding struct {
	sharedAuthorizationBirth
	ConfigRevision string
	PolicyRevision string
	UserCreatedAt  time.Time
	ProviderID     string // Present only for the named profiles.
	ProfileID      string
}

type sharedAuthorizationEmergency struct {
	PolicyRevision    string
	UserCreatedAt     time.Time
	MFAGeneration     string
	FactorCompletedAt time.Time
}

type sharedAuthorizationTeam struct {
	sharedAuthorizationBirth
	MembershipID         string
	JoinedAt             *time.Time
	Role                 string
	GrantRequestID       *string
	GrantCreationReceipt *string
}

// Facts must come from one coherently captured source. This pure constructor
// neither reads SQL nor authenticates a bearer. Existing entities are input
// only: whole entities and their credential-derived fields are never serialized.
// Team/Project grant rows have no revision or birth; none is synthesized.
type sharedAuthorizationFacts struct {
	Kind           string
	Model          entity.Model
	User           *entity.User
	Admission      *runtimeAdmissionProof
	PersonalGrants *runtimePersonalGrantState
	PersonalRows   []entity.UserModelGrant
	PersonalKey    *entity.APIKey
	ProjectKey     *entity.ProjectKey
	KeyModels      []string
	Project        *entity.Project
	ProjectGrant   *entity.ProjectModelGrant
	Session        *entity.Session
	Policy         *entity.GovernanceSetting
	MFA            *entity.UserMFA
	Team           *entity.Team
	Membership     *entity.TeamMembership
	TeamGrant      *entity.TeamModelGrant
}

func newSharedCapturedAuthorization(f sharedAuthorizationFacts, evaluatedAt time.Time) (sharedCapturedAuthorization, error) {
	a := sharedCapturedAuthorization{Version: 1, EvaluatedAt: evaluatedAt.UTC(), Kind: f.Kind,
		Model: sharedAuthorizationBirth{ID: f.Model.ID, CreatedAt: f.Model.CreatedAt.UTC()}}
	invalid := func() (sharedCapturedAuthorization, error) {
		return sharedCapturedAuthorization{}, errSharedAdmissionInvalid
	}
	if evaluatedAt.IsZero() || f.Model.Status != entity.ResourceActive || !sharedAuthorizationBorn(a.Model, evaluatedAt) {
		return invalid()
	}
	personal := f.Kind == "personal_key" || f.Kind == "personal_session"
	session := f.Kind == "personal_session" || f.Kind == "team_session"
	project := f.Kind == "project_key"
	team := f.Kind == "team_session"
	if !personal && !project && !team || (f.PersonalKey != nil) != (f.Kind == "personal_key") || (f.ProjectKey != nil) != project ||
		(f.Session != nil) != session || (f.Policy != nil) != session || (f.User != nil) != !project || (f.Admission != nil) != !project ||
		(f.PersonalGrants != nil) != personal || !personal && len(f.PersonalRows) != 0 || (f.Project != nil) != project || (f.ProjectGrant != nil) != project ||
		(f.Team != nil) != team || (f.Membership != nil) != team || (f.TeamGrant != nil) != team || session && len(f.KeyModels) != 0 {
		return invalid()
	}
	if f.User != nil {
		u, admission := *f.User, *f.Admission
		if u.Disabled || u.OffboardedAt != nil || !admission.Eligible || !u.CreatedAt.Equal(admission.CreatedAt) {
			return invalid()
		}
		a.User = &sharedAuthorizationUser{sharedAuthorizationBirth: sharedAuthorizationBirth{u.ID, u.CreatedAt.UTC()},
			AdmissionState: admission.State, ApplicationID: admission.ApplicationID, AdmissionRevision: admission.Revision}
		if u.ApprovalApplicationID != nil {
			if *u.ApprovalApplicationID != admission.ApplicationID {
				return invalid()
			}
			a.User.ApplicationCreatedAt = sharedAuthorizationTime(&admission.ApplicationCreatedAt)
		} else if admission.State != "not_required" || admission.ApplicationID != "" || !admission.ApplicationCreatedAt.IsZero() || admission.Revision != "" {
			return invalid()
		}
	}
	if personal {
		g := *f.PersonalGrants
		if !g.Enabled || !g.CreatedAt.Equal(f.User.CreatedAt) || g.Revision != f.User.PersonalGrantRevision || g.Hash != personalGrantHash(f.PersonalRows) {
			return invalid()
		}
		selected := false
		seen := map[string]bool{}
		for _, row := range f.PersonalRows {
			if row.UserID != f.User.ID || !safeTeamSessionID(row.ModelID) || seen[row.ModelID] || row.CreatedAt.IsZero() || row.CreatedAt.After(evaluatedAt) {
				return invalid()
			}
			seen[row.ModelID] = true
			selected = selected || row.ModelID == f.Model.ID
		}
		if !selected {
			return invalid()
		}
		a.PersonalGrants = &sharedAuthorizationPersonalGrants{Revision: g.Revision, Hash: g.Hash}
	}
	if f.PersonalKey != nil {
		k := *f.PersonalKey
		if k.UserID != f.User.ID || k.Status != entity.KeyActive || !slices.Contains(f.KeyModels, f.Model.ID) {
			return invalid()
		}
		a.Key = &sharedAuthorizationKey{sharedAuthorizationBirth{k.ID, k.CreatedAt.UTC()}, sharedAuthorizationTime(k.ExpiresAt), k.LifecycleRevision}
	}
	if project {
		p, k, grant := *f.Project, *f.ProjectKey, *f.ProjectGrant
		if p.Status != entity.ResourceActive || k.Status != entity.KeyActive || k.ProjectID != p.ID || grant.ProjectID != p.ID || grant.ModelID != f.Model.ID || !slices.Contains(f.KeyModels, f.Model.ID) {
			return invalid()
		}
		a.Project = &sharedAuthorizationBirth{p.ID, p.CreatedAt.UTC()}
		a.Key = &sharedAuthorizationKey{sharedAuthorizationBirth{k.ID, k.CreatedAt.UTC()}, sharedAuthorizationTime(k.ExpiresAt), ""}
	}
	if session {
		s, p := *f.Session, *f.Policy
		if s.UserID != f.User.ID || p.ID != 1 || !validMemberRoleDigest(p.AuthenticationPolicyRevision) ||
			p.ForcedSSOEnabled && (p.EmergencyAdminID == "" || p.EmergencyAdminCreatedAt == nil || p.EmergencyAdminCreatedAt.IsZero()) ||
			!p.ForcedSSOEnabled && (p.EmergencyAdminID != "" || p.EmergencyAdminCreatedAt != nil) || !authenticationMethodAllowed(p, s.PrimaryMethod) {
			return invalid()
		}
		primary, err := sharedAuthorizationSessionPrimary(s, p, *f.User, f.MFA, evaluatedAt)
		if err != nil {
			return invalid()
		}
		a.Session = &sharedAuthorizationSession{sharedAuthorizationBirth{s.ID, s.CreatedAt.UTC()}, s.ExpiresAt.UTC(), p.AuthenticationPolicyRevision, p.ForcedSSOEnabled, primary}
	} else if f.MFA != nil {
		return invalid()
	}
	if team {
		t, m, g := *f.Team, *f.Membership, *f.TeamGrant
		if t.Status != entity.ResourceActive || m.Status != entity.ResourceActive || m.TeamID != t.ID || m.UserID != f.User.ID || g.TeamID != t.ID || g.ModelID != f.Model.ID {
			return invalid()
		}
		a.Team = &sharedAuthorizationTeam{sharedAuthorizationBirth{t.ID, t.CreatedAt.UTC()}, m.ID, sharedAuthorizationTime(m.JoinedAt), m.Role, sharedAuthorizationString(g.SourceRequestID), sharedAuthorizationString(g.SourceCreationReceiptID)}
	}
	if !sharedAuthorizationValid(a) {
		return invalid()
	}
	return a, nil
}

func sharedAuthorizationSessionPrimary(s entity.Session, p entity.GovernanceSetting, u entity.User, mfa *entity.UserMFA, at time.Time) (sharedAuthorizationPrimary, error) {
	result := sharedAuthorizationPrimary{Method: s.PrimaryMethod}
	if s.PrimaryMethod == "emergency" {
		if mfa == nil || !emergencyCurrent(p, s, u, *mfa, at, false) {
			return result, errSharedAdmissionInvalid
		}
		result.Emergency = &sharedAuthorizationEmergency{s.EmergencyPolicyRevision, s.EmergencyUserCreatedAt.UTC(), s.EmergencyMFAGeneration, s.EmergencyFactorCompletedAt.UTC()}
		return result, nil
	}
	if !emergencyProofEmpty(s) || mfa != nil {
		return result, errSharedAdmissionInvalid
	}
	type family struct {
		method, id        string
		birth             *time.Time
		config, policy    string
		userBirth         *time.Time
		provider, profile string
	}
	families := []family{
		{"oidc", s.OIDCBindingID, s.OIDCBindingCreatedAt, s.OIDCConfigRevision, s.OIDCPolicyRevision, s.OIDCUserCreatedAt, "", ""},
		{"oauth", s.OAuthBindingID, s.OAuthBindingCreatedAt, s.OAuthConfigRevision, s.OAuthPolicyRevision, s.OAuthUserCreatedAt, "", ""},
		{"ldap", s.LDAPBindingID, s.LDAPBindingCreatedAt, s.LDAPConfigRevision, s.LDAPPolicyRevision, s.LDAPUserCreatedAt, "", ""},
		{"saml", s.SAMLBindingID, s.SAMLBindingCreatedAt, s.SAMLConfigRevision, s.SAMLPolicyRevision, s.SAMLUserCreatedAt, "", ""},
		{s.NamedIdentityProviderID, s.NamedIdentityBindingID, s.NamedIdentityBindingCreatedAt, s.NamedIdentityConfigRevision, s.NamedIdentityPolicyRevision, s.NamedIdentityUserCreatedAt, s.NamedIdentityProviderID, s.NamedIdentityProfileID},
	}
	for _, f := range families {
		if primaryProofEmpty(f.id, f.birth, f.config, f.policy, f.userBirth) && f.provider == "" && f.profile == "" {
			continue
		}
		if result.Binding != nil || f.method == "" || f.method != s.PrimaryMethod || f.birth == nil || f.userBirth == nil {
			return result, errSharedAdmissionInvalid
		}
		result.Binding = &sharedAuthorizationBinding{sharedAuthorizationBirth{f.id, f.birth.UTC()}, f.config, f.policy, f.userBirth.UTC(), f.provider, f.profile}
	}
	return result, nil
}

func sharedAuthorizationValid(a sharedCapturedAuthorization) bool {
	if a.Version != 1 || a.EvaluatedAt.IsZero() || !sharedAuthorizationBorn(a.Model, a.EvaluatedAt) {
		return false
	}
	personal := a.Kind == "personal_key" || a.Kind == "personal_session"
	project := a.Kind == "project_key"
	session := a.Kind == "personal_session" || a.Kind == "team_session"
	team := a.Kind == "team_session"
	if !personal && !project && !team || (a.User != nil) != !project || (a.Key != nil) != !session || (a.Project != nil) != project || (a.Session != nil) != session || (a.Team != nil) != team || (a.PersonalGrants != nil) != personal {
		return false
	}
	if a.User != nil {
		u := a.User
		if !sharedAuthorizationBorn(u.sharedAuthorizationBirth, a.EvaluatedAt) {
			return false
		}
		switch u.AdmissionState {
		case "not_required":
			if u.ApplicationID != "" || u.ApplicationCreatedAt != nil || u.AdmissionRevision != "" {
				return false
			}
		case "approved":
			if !registrationApplicationID(u.ApplicationID) || u.ApplicationCreatedAt == nil || !sharedAuthorizationBorn(sharedAuthorizationBirth{u.ApplicationID, *u.ApplicationCreatedAt}, a.EvaluatedAt) || !validMemberRoleDigest(u.AdmissionRevision) {
				return false
			}
		default:
			return false
		}
	}
	if a.PersonalGrants != nil && (!personalModelETag(a.PersonalGrants.Revision) || !personalModelETag(a.PersonalGrants.Hash)) {
		return false
	}
	if a.Key != nil {
		k := a.Key
		if !sharedAuthorizationBorn(k.sharedAuthorizationBirth, a.EvaluatedAt) || k.ExpiresAt != nil && (!a.EvaluatedAt.Before(*k.ExpiresAt) || !k.CreatedAt.Before(*k.ExpiresAt)) ||
			personal && (!memberKeyRevision.MatchString(k.LifecycleRevision) || k.CreatedAt.Before(a.User.CreatedAt)) || project && (k.LifecycleRevision != "" || !sharedAuthorizationBorn(*a.Project, a.EvaluatedAt) || k.CreatedAt.Before(a.Project.CreatedAt)) {
			return false
		}
	}
	if a.Team != nil {
		t := a.Team
		if !sharedAuthorizationBorn(t.sharedAuthorizationBirth, a.EvaluatedAt) || !safeTeamSessionID(t.MembershipID) || t.Role != entity.TeamOwner && t.Role != entity.TeamMember ||
			t.JoinedAt != nil && (t.JoinedAt.IsZero() || t.JoinedAt.After(a.EvaluatedAt) || t.JoinedAt.Before(t.CreatedAt) || t.JoinedAt.Before(a.User.CreatedAt)) ||
			t.GrantRequestID != nil && !safeTeamSessionID(*t.GrantRequestID) || t.GrantCreationReceipt != nil && !credentialReplacementRequestID.MatchString(*t.GrantCreationReceipt) {
			return false
		}
	}
	return a.Session == nil || sharedAuthorizationSessionValid(*a.Session, *a.User, a.EvaluatedAt)
}

func sharedAuthorizationSessionValid(s sharedAuthorizationSession, u sharedAuthorizationUser, at time.Time) bool {
	if !sharedAuthorizationBorn(s.sharedAuthorizationBirth, at) || s.CreatedAt.Before(u.CreatedAt) || !at.Before(s.ExpiresAt) || !s.CreatedAt.Before(s.ExpiresAt) || !validMemberRoleDigest(s.AuthenticationPolicyRevision) {
		return false
	}
	p := s.Primary
	if s.ForcedSSO && p.Method != "oidc" && p.Method != "ldap" && p.Method != "emergency" || !s.ForcedSSO && p.Method == "emergency" {
		return false
	}
	switch p.Method {
	case "":
		return p.Binding == nil && p.Emergency == nil
	case "emergency":
		e := p.Emergency
		return p.Binding == nil && e != nil && e.PolicyRevision == s.AuthenticationPolicyRevision && e.UserCreatedAt.Equal(u.CreatedAt) && authenticationMFAGeneration(e.MFAGeneration) && !e.FactorCompletedAt.IsZero() && !e.FactorCompletedAt.After(s.CreatedAt)
	case "oidc", "oauth", "ldap", "saml", "github", "google", "discord", "telegram":
		b := p.Binding
		if p.Emergency != nil || b == nil || !sharedAuthorizationBorn(b.sharedAuthorizationBirth, at) || !b.UserCreatedAt.Equal(u.CreatedAt) || b.CreatedAt.Before(u.CreatedAt) || !validMemberRoleDigest(b.ConfigRevision) || !validMemberRoleDigest(b.PolicyRevision) {
			return false
		}
		if namedIdentityMethod(p.Method) {
			return b.ProviderID == p.Method && b.ProfileID == namedIdentityProfileID(p.Method)
		}
		return b.ProviderID == "" && b.ProfileID == ""
	default:
		return false
	}
}

func sharedAuthorizationBorn(b sharedAuthorizationBirth, at time.Time) bool {
	return safeTeamSessionID(b.ID) && !b.CreatedAt.IsZero() && !b.CreatedAt.After(at)
}

func sharedAuthorizationTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	v := value.UTC()
	return &v
}

func sharedAuthorizationString(value *string) *string {
	if value == nil {
		return nil
	}
	v := *value
	return &v
}

func (a sharedCapturedAuthorization) matchesCall(f CallFact) bool {
	if a.Model.ID != f.ModelID {
		return false
	}
	user, key, project, team, member := "", "", "", "", ""
	if a.User != nil {
		user = a.User.ID
	}
	if a.Key != nil {
		key = a.Key.ID
	}
	if a.Project != nil {
		project = a.Project.ID
	}
	if a.Team != nil {
		team, member = a.Team.ID, a.Team.MembershipID
	}
	return f.UserID == user && f.KeyID == key && f.ProjectID == project && f.TeamID == team && f.TeamMembershipID == member
}
