package service

import (
	"crypto/subtle"
	"slices"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

// captureSharedAuthorizationSQL is initial capture, not bearer authentication.
// The caller supplies a real authenticated request-local identity and owns one
// READ COMMITTED transaction, with governance acquired BEFORE any owner fence.
// This function retains current authority locks in that same transaction. It
// neither opens/commits a transaction nor returns the transient token verifier.
func captureSharedAuthorizationSQL(tx *gorm.DB, identity gatewayIdentity, now time.Time) (sharedCapturedAuthorization, error) {
	if tx == nil || tx.Statement == nil || tx.Error != nil || now.IsZero() {
		return sharedCapturedAuthorization{}, errSharedAdmissionInvalid
	}
	if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok {
		return sharedCapturedAuthorization{}, errSharedAdmissionInvalid
	}
	if err := tx.Statement.Context.Err(); err != nil {
		return sharedCapturedAuthorization{}, err
	}
	target, err := sharedAuthenticatedAuthorizationTarget(identity)
	if err != nil {
		return sharedCapturedAuthorization{}, err
	}
	if err := lockGovernance(sharedAuthorizationDB(tx)); err != nil {
		return sharedCapturedAuthorization{}, sharedAuthorizationSQLError(err)
	}
	facts, err := loadSharedAuthorizationFactsSQL(tx, target, now)
	if err != nil {
		return sharedCapturedAuthorization{}, err
	}
	if !sharedAuthenticatedAuthorizationMatches(identity, facts) {
		return sharedCapturedAuthorization{}, errSharedAdmissionConflict
	}
	captured, err := newSharedCapturedAuthorization(facts, now)
	if err != nil {
		return sharedCapturedAuthorization{}, errSharedAdmissionConflict
	}
	if err := tx.Statement.Context.Err(); err != nil {
		return sharedCapturedAuthorization{}, err
	}
	return captured, nil
}

// Locators select locking reads only. Nonempty IDs never replace a verifier or
// private Session proof; both are matched against the locked rows before capture.
func sharedAuthenticatedAuthorizationTarget(identity gatewayIdentity) (sharedAuthorizationSQLTarget, error) {
	target := sharedAuthorizationSQLTarget{modelID: identity.selectedModelID}
	count := 0
	if identity.key != nil {
		count++
		key := identity.key
		target.kind, target.keyID, target.userID = "personal_key", key.Key.ID, key.Key.UserID
		if key.ProjectID != "" {
			target.kind, target.projectID = "project_key", key.ProjectID
			if key.Key.UserID != "" || identity.personalGrantRevision != "" {
				return target, errSharedAdmissionInvalid
			}
		} else if !safeTeamSessionID(target.userID) {
			return target, errSharedAdmissionInvalid
		}
		if !safeTeamSessionID(target.keyID) || key.Key.CreatedAt.IsZero() || !teamSessionDigest.MatchString(key.Key.TokenHash) || !slices.Contains(key.ModelIDs, target.modelID) {
			return target, errSharedAdmissionInvalid
		}
	}
	if identity.personal != nil {
		count++
		personal := identity.personal
		target.kind, target.userID, target.sessionID = "personal_session", personal.UserID, personal.SessionID
		if personal.SessionID != personal.session.ID || personal.UserID != personal.session.UserID || personal.session.CreatedAt.IsZero() || !teamSessionDigest.MatchString(personal.session.TokenHash) || !slices.Contains(personal.models, target.modelID) {
			return target, errSharedAdmissionInvalid
		}
	}
	if identity.team != nil {
		count++
		team := identity.team
		target.kind, target.userID, target.sessionID = "team_session", team.UserID, team.SessionID
		target.teamID, target.membershipID = team.TeamID, team.TeamMembershipID
		if team.createdAt.IsZero() || team.teamCreatedAt.IsZero() || !teamSessionDigest.MatchString(team.tokenHash) {
			return target, errSharedAdmissionInvalid
		}
	}
	if count != 1 || !safeTeamSessionID(target.modelID) {
		return target, errSharedAdmissionInvalid
	}
	for _, id := range []string{target.userID, target.projectID, target.teamID, target.sessionID, target.membershipID} {
		if id != "" && !safeTeamSessionID(id) {
			return target, errSharedAdmissionInvalid
		}
	}
	if target.kind == "personal_session" || target.kind == "team_session" {
		if target.userID == "" || target.sessionID == "" || target.kind == "team_session" && (target.teamID == "" || target.membershipID == "") {
			return target, errSharedAdmissionInvalid
		}
	}
	return target, nil
}

func sharedAuthenticatedAuthorizationMatches(identity gatewayIdentity, f sharedAuthorizationFacts) bool {
	if identity.key != nil {
		authenticated := identity.key.Key
		var current entity.APIKey
		if f.ProjectKey != nil {
			// Project runtime authentication deliberately returns neutral metadata.
			// Project ownership and grants come from the actual locked ProjectKey.
			current = commonProjectKeyMetadata(*f.ProjectKey)
			if f.Project == nil || identity.key.ProjectID != f.Project.ID || f.ProjectKey.ProjectID != f.Project.ID {
				return false
			}
		} else if f.PersonalKey != nil {
			current = *f.PersonalKey
			if identity.personalGrantRevision != "" && identity.personalGrantRevision != f.PersonalGrants.Revision {
				return false
			}
		} else {
			return false
		}
		return sharedAuthorizationVerifierEqual(authenticated.TokenHash, current.TokenHash) && authenticated.ID == current.ID &&
			authenticated.UserID == current.UserID && authenticated.CreatedAt.Equal(current.CreatedAt) &&
			authenticated.Status == current.Status && authenticated.LifecycleRevision == current.LifecycleRevision &&
			authenticationInstantEqual(authenticated.ExpiresAt, current.ExpiresAt)
	}
	if f.Session == nil || f.User == nil || f.Admission == nil {
		return false
	}
	session := sharedAuthorizationSQLSessionProof(*f.Session)
	if identity.personal != nil {
		personal := identity.personal
		if !sharedAuthorizationVerifierEqual(personal.session.TokenHash, session.TokenHash) {
			return false
		}
		// Compare the verifier separately in constant time, then the full neutral
		// Session proof, including every supported primary authentication method.
		original := personal.session
		original.TokenHash, session.TokenHash = "", ""
		return samePersonalSessionProof(original, session) && personal.user.CreatedAt.Equal(f.User.CreatedAt) &&
			personal.user.Enabled == (!f.User.Disabled && f.User.OffboardedAt == nil) && samePersonalAdmission(personal.admission, *f.Admission) &&
			f.PersonalGrants != nil && personal.grants.CreatedAt.Equal(f.PersonalGrants.CreatedAt) &&
			personal.grants.Enabled == f.PersonalGrants.Enabled && personal.grants.Revision == f.PersonalGrants.Revision && personal.grants.Hash == f.PersonalGrants.Hash
	}
	if identity.team != nil && f.Team != nil && f.Membership != nil {
		team := identity.team
		return sharedAuthorizationVerifierEqual(team.tokenHash, session.TokenHash) &&
			team.SessionID == session.ID && team.UserID == session.UserID && team.createdAt.Equal(session.CreatedAt) && team.expiresAt.Equal(session.ExpiresAt) &&
			team.primaryMethod == session.PrimaryMethod && team.emergencyPolicyRevision == session.EmergencyPolicyRevision &&
			authenticationInstantEqual(team.emergencyUserCreatedAt, session.EmergencyUserCreatedAt) && team.emergencyMFAGeneration == session.EmergencyMFAGeneration &&
			team.emergencyPasswordDigest == session.EmergencyPasswordDigest && authenticationInstantEqual(team.emergencyFactorCompletedAt, session.EmergencyFactorCompletedAt) &&
			team.oidcPolicyRevision == session.OIDCPolicyRevision && team.oidcBindingKey == session.OIDCBindingKey &&
			team.oauthPolicyRevision == session.OAuthPolicyRevision && team.oauthBindingKey == session.OAuthBindingKey &&
			team.ldapPolicyRevision == session.LDAPPolicyRevision && team.ldapBindingKey == session.LDAPBindingKey &&
			team.samlPolicyRevision == session.SAMLPolicyRevision && team.samlBindingKey == session.SAMLBindingKey &&
			team.namedIdentityPolicyKey == session.NamedIdentityPolicyKey && team.namedIdentityBindingKey == session.NamedIdentityBindingKey &&
			team.TeamID == f.Team.ID && team.teamCreatedAt.Equal(f.Team.CreatedAt) && team.TeamMembershipID == f.Membership.ID
	}
	return false
}

func sharedAuthorizationVerifierEqual(a, b string) bool {
	return teamSessionDigest.MatchString(a) && teamSessionDigest.MatchString(b) && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// This is the existing runtime's neutral Session projection. Config, binding,
// admission and grant validity are independently checked from locked SQL facts.
func sharedAuthorizationSQLSessionProof(s entity.Session) runtimeTeamSession {
	return runtimeTeamSession{PrimaryMethod: s.PrimaryMethod, CreatedAt: s.CreatedAt,
		EmergencyPolicyRevision: s.EmergencyPolicyRevision, EmergencyUserCreatedAt: s.EmergencyUserCreatedAt,
		EmergencyMFAGeneration: s.EmergencyMFAGeneration, EmergencyPasswordDigest: s.EmergencyPasswordDigest, EmergencyFactorCompletedAt: s.EmergencyFactorCompletedAt,
		ID: s.ID, UserID: s.UserID, TokenHash: s.TokenHash, ExpiresAt: s.ExpiresAt,
		OIDCPolicyRevision: s.OIDCPolicyRevision, OIDCBindingKey: oidcRuntimeBindingKey(s.OIDCBindingID, s.OIDCBindingCreatedAt),
		OAuthPolicyRevision: s.OAuthPolicyRevision, OAuthBindingKey: oauthRuntimeBindingKey(s.OAuthBindingID, s.OAuthBindingCreatedAt),
		LDAPPolicyRevision: s.LDAPPolicyRevision, LDAPBindingKey: ldapRuntimeBindingKey(s.LDAPBindingID, s.LDAPBindingCreatedAt),
		SAMLPolicyRevision: s.SAMLPolicyRevision, SAMLBindingKey: samlRuntimeBindingKey(s.SAMLBindingID, s.SAMLBindingCreatedAt),
		NamedIdentityPolicyKey:  namedIdentityRuntimePolicyKeyFor(s.PrimaryMethod, s.NamedIdentityPolicyRevision),
		NamedIdentityBindingKey: namedIdentityRuntimeBindingKeyFor(s.PrimaryMethod, s.NamedIdentityBindingID, s.NamedIdentityBindingCreatedAt)}
}
