package service

import (
	"errors"
	"reflect"
	"slices"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// validateSharedAuthorizationSQL retains current authority locks in the supplied
// READ COMMITTED transaction. The caller must acquire governance BEFORE owner
// fencing locks; reacquiring that same lock here is intentional. Success applies
// only to this transaction, not a receipt, transport permit, or bearer login.
func validateSharedAuthorizationSQL(tx *gorm.DB, original sharedCapturedAuthorization, now time.Time) error {
	if tx == nil || tx.Statement == nil || tx.Error != nil || now.IsZero() || now.Before(original.EvaluatedAt) || !sharedAuthorizationValid(original) {
		return errSharedAdmissionInvalid
	}
	if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok {
		return errSharedAdmissionInvalid
	}
	if err := tx.Statement.Context.Err(); err != nil {
		return err
	}
	if err := lockGovernance(sharedAuthorizationDB(tx)); err != nil {
		return sharedAuthorizationSQLError(err)
	}
	f, err := loadSharedAuthorizationFactsSQL(tx, sharedAuthorizationTarget(original), now)
	if err != nil {
		return err
	}
	current, err := newSharedCapturedAuthorization(f, now)
	if err != nil {
		return errSharedAdmissionConflict
	}
	// Evaluation time advances; every captured durable identity and proof stays
	// exact. No runtime epoch or synthetic Project/Team grant revision is added.
	original.EvaluatedAt = current.EvaluatedAt
	if !reflect.DeepEqual(original, current) {
		return errSharedAdmissionConflict
	}
	return tx.Statement.Context.Err()
}

// A target selects current locking reads only. It is neither authentication nor
// durable proof; each caller must independently validate its input authority.
type sharedAuthorizationSQLTarget struct {
	kind, modelID, userID, projectID, teamID, keyID, sessionID, membershipID string
}

func sharedAuthorizationTarget(a sharedCapturedAuthorization) sharedAuthorizationSQLTarget {
	t := sharedAuthorizationSQLTarget{kind: a.Kind, modelID: a.Model.ID}
	if a.User != nil {
		t.userID = a.User.ID
	}
	if a.Project != nil {
		t.projectID = a.Project.ID
	}
	if a.Team != nil {
		t.teamID, t.membershipID = a.Team.ID, a.Team.MembershipID
	}
	if a.Key != nil {
		t.keyID = a.Key.ID
	}
	if a.Session != nil {
		t.sessionID = a.Session.ID
	}
	return t
}

// Caller owns the READ COMMITTED transaction and governance/owner locks. This
// shared loader preserves the same current-row order for initial capture and
// later exact-proof validation; it must never be used as a bearer lookup.
func loadSharedAuthorizationFactsSQL(tx *gorm.DB, target sharedAuthorizationSQLTarget, now time.Time) (sharedAuthorizationFacts, error) {
	f := sharedAuthorizationFacts{Kind: target.kind}
	if target.userID != "" {
		u, admission, err := sharedAuthorizationSQLUser(tx, target.userID)
		if err != nil {
			return f, err
		}
		f.User, f.Admission = &u, &admission
	}
	if target.projectID != "" {
		// The governance barrier freezes manager membership before User locks.
		// Lock all candidate Users in sorted order before locking the Project.
		if err := sharedAuthorizationSQLProjectManagers(tx, target.projectID); err != nil {
			return f, err
		}
		f.Project = &entity.Project{}
		if err := sharedAuthorizationSQLRow(tx, "id", target.projectID, f.Project); err != nil {
			return f, err
		}
		if f.Project.ID != target.projectID {
			return f, errSharedAdmissionConflict
		}
	}
	if target.teamID != "" {
		f.Team = &entity.Team{}
		if err := sharedAuthorizationSQLRow(tx, "id", target.teamID, f.Team); err != nil {
			return f, err
		}
		if f.Team.ID != target.teamID {
			return f, errSharedAdmissionConflict
		}
	}
	switch target.kind {
	case "personal_key":
		f.PersonalKey = &entity.APIKey{}
		if err := sharedAuthorizationSQLRow(tx, "id", target.keyID, f.PersonalKey); err != nil {
			return f, err
		}
		k := f.PersonalKey
		if k.ID != target.keyID || k.UserID != f.User.ID {
			return f, errSharedAdmissionConflict
		}
		var relation entity.APIKeyModel
		if err := sharedAuthorizationDB(tx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("key_id = ? AND model_id = ?", k.ID, target.modelID).Take(&relation).Error; err != nil {
			return f, sharedAuthorizationSQLError(err)
		}
		if relation.KeyID != k.ID || relation.ModelID != target.modelID {
			return f, errSharedAdmissionConflict
		}
		f.KeyModels = []string{relation.ModelID}
		if err := sharedAuthorizationSQLDelivery(tx, "personal", k.ID, k.CreatedAt, k.DeliveryMode); err != nil {
			return f, err
		}
	case "project_key":
		f.ProjectKey = &entity.ProjectKey{}
		if err := sharedAuthorizationSQLRow(tx, "id", target.keyID, f.ProjectKey); err != nil {
			return f, err
		}
		k := f.ProjectKey
		if k.ID != target.keyID || k.ProjectID != f.Project.ID {
			return f, errSharedAdmissionConflict
		}
		var relation entity.ProjectKeyModel
		if err := sharedAuthorizationDB(tx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("key_id = ? AND model_id = ?", k.ID, target.modelID).Take(&relation).Error; err != nil {
			return f, sharedAuthorizationSQLError(err)
		}
		if relation.KeyID != k.ID || relation.ModelID != target.modelID {
			return f, errSharedAdmissionConflict
		}
		f.KeyModels = []string{relation.ModelID}
		f.ProjectGrant = &entity.ProjectModelGrant{}
		if err := sharedAuthorizationDB(tx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("project_id = ? AND model_id = ?", f.Project.ID, target.modelID).Take(f.ProjectGrant).Error; err != nil {
			return f, sharedAuthorizationSQLError(err)
		}
		if err := sharedAuthorizationSQLDelivery(tx, "project", k.ID, k.CreatedAt, k.DeliveryMode); err != nil {
			return f, err
		}
	case "personal_session", "team_session":
		f.Session = &entity.Session{}
		if err := sharedAuthorizationSQLRow(tx, "id", target.sessionID, f.Session); err != nil {
			return f, err
		}
		if f.Session.ID != target.sessionID || f.Session.UserID != f.User.ID {
			return f, errSharedAdmissionConflict
		}
		policy, err := authenticationPolicy(sharedAuthorizationDB(tx))
		if err != nil {
			return f, sharedAuthorizationSQLError(err)
		}
		f.Policy = &policy
		if err := sharedAuthorizationSQLPrimary(tx, &f, now); err != nil {
			return f, err
		}
	}
	if target.kind == "personal_key" || target.kind == "personal_session" {
		q := sharedAuthorizationDB(tx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", f.User.ID).Order("model_id").Limit(1001)
		if err := q.Find(&f.PersonalRows).Error; err != nil {
			return f, err
		}
		if len(f.PersonalRows) > 1000 {
			return f, errSharedAdmissionConflict
		}
		f.PersonalGrants = &runtimePersonalGrantState{CreatedAt: f.User.CreatedAt, Enabled: f.Admission.Eligible,
			Revision: f.User.PersonalGrantRevision, Hash: personalGrantHash(f.PersonalRows)}
	}
	if target.teamID != "" {
		f.Membership = &entity.TeamMembership{}
		if err := sharedAuthorizationSQLRow(tx, "id", target.membershipID, f.Membership); err != nil {
			return f, err
		}
		if f.Membership.ID != target.membershipID {
			return f, errSharedAdmissionConflict
		}
		f.TeamGrant = &entity.TeamModelGrant{}
		if err := sharedAuthorizationDB(tx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("team_id = ? AND model_id = ?", f.Team.ID, target.modelID).Take(f.TeamGrant).Error; err != nil {
			return f, sharedAuthorizationSQLError(err)
		}
	}
	if err := sharedAuthorizationSQLRow(tx, "id", target.modelID, &f.Model); err != nil {
		return f, err
	}
	return f, nil
}

func sharedAuthorizationDB(tx *gorm.DB) *gorm.DB {
	return tx.Session(&gorm.Session{NewDB: true})
}

func sharedAuthorizationSQLError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errSharedAdmissionConflict
	}
	return err
}

func sharedAuthorizationSQLRow(tx *gorm.DB, column, id string, row any) error {
	err := personalExact(sharedAuthorizationDB(tx), column, id).Clauses(clause.Locking{Strength: "UPDATE"}).Take(row).Error
	return sharedAuthorizationSQLError(err)
}

func sharedAuthorizationSQLUser(tx *gorm.DB, id string) (entity.User, runtimeAdmissionProof, error) {
	var u entity.User
	if err := sharedAuthorizationSQLRow(tx, "id", id, &u); err != nil {
		return u, runtimeAdmissionProof{}, err
	}
	if u.ID != id {
		return u, runtimeAdmissionProof{}, errSharedAdmissionConflict
	}
	apps := map[string]entity.RegistrationApprovalApplication{}
	if u.ApprovalApplicationID != nil {
		var app entity.RegistrationApprovalApplication
		if err := sharedAuthorizationSQLRow(tx, "id", *u.ApprovalApplicationID, &app); err != nil {
			return u, runtimeAdmissionProof{}, err
		}
		apps[app.ID] = app
	}
	_, proof := registrationAdmission(u, apps)
	return u, proof, nil
}

func sharedAuthorizationSQLProjectManagers(tx *gorm.DB, projectID string) error {
	var rows []entity.ProjectManager
	q := personalExact(sharedAuthorizationDB(tx), "project_id", projectID).Order("user_id,id").Limit(1001)
	if err := q.Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) == 0 || len(rows) > 1000 {
		return errSharedAdmissionConflict
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.ProjectID != projectID || !safeTeamSessionID(row.ID) || !safeTeamSessionID(row.UserID) {
			return errSharedAdmissionConflict
		}
		ids = append(ids, row.UserID)
	}
	slices.Sort(ids)
	if len(slices.Compact(slices.Clone(ids))) != len(ids) {
		return errSharedAdmissionConflict
	}
	eligible := false
	for _, id := range ids {
		_, proof, err := sharedAuthorizationSQLUser(tx, id)
		if err != nil {
			return err
		}
		eligible = eligible || proof.Eligible
	}
	for _, row := range rows {
		var current entity.ProjectManager
		if err := sharedAuthorizationSQLRow(tx, "id", row.ID, &current); err != nil {
			return err
		}
		if current != row {
			return errSharedAdmissionConflict
		}
	}
	if !eligible {
		return errSharedAdmissionConflict
	}
	return nil
}

func sharedAuthorizationSQLDelivery(tx *gorm.DB, kind, id string, birth time.Time, mode string) error {
	if mode == "managed_vault" {
		// Owner and Key locks precede Command then binding, matching revocation
		// and acknowledged-outcome writers. The existing validator checks every
		// linked non-secret receipt fact after these current locking reads.
		var binding entity.APIKeyVaultDeliveryBinding
		digest := apiKeyDeliveryKeyDigest(kind, id, birth)
		if err := personalExact(sharedAuthorizationDB(tx), "key_digest", digest).Take(&binding).Error; err != nil {
			return sharedAuthorizationSQLError(err)
		}
		var command entity.APIKeyDeliveryCommand
		if err := sharedAuthorizationSQLRow(tx, "binding_id", binding.ID, &command); err != nil {
			return err
		}
		if err := sharedAuthorizationSQLRow(tx, "id", binding.ID, &binding); err != nil {
			return err
		}
	}
	return sharedAuthorizationSQLError(apiKeyDeliveryRequireAcknowledged(sharedAuthorizationDB(tx), kind, id, birth, mode))
}

func sharedAuthorizationSQLPrimary(tx *gorm.DB, f *sharedAuthorizationFacts, now time.Time) error {
	s := *f.Session
	if s.PrimaryMethod == "emergency" {
		f.MFA = &entity.UserMFA{}
		if err := sharedAuthorizationSQLRow(tx, "user_id", f.User.ID, f.MFA); err != nil {
			return err
		}
		// The existing SQL wrapper uses time.Now; the pure predicate keeps this
		// transaction's explicitly supplied time as its single evaluation clock.
		if !emergencyCurrent(*f.Policy, s, *f.User, *f.MFA, now, false) {
			return errSharedAdmissionConflict
		}
		return nil
	}
	var provider, binding any
	var bindingID string
	switch s.PrimaryMethod {
	case "":
	case "oidc":
		provider, binding, bindingID = &entity.OIDCProvider{}, &entity.OIDCBinding{}, s.OIDCBindingID
	case "oauth":
		provider, binding, bindingID = &entity.OAuthProvider{}, &entity.OAuthBinding{}, s.OAuthBindingID
	case "ldap":
		provider, binding, bindingID = &entity.LDAPProvider{}, &entity.LDAPBinding{}, s.LDAPBindingID
	case "saml":
		provider, binding, bindingID = &entity.SAMLProvider{}, &entity.SAMLBinding{}, s.SAMLBindingID
	case "github", "google", "discord", "telegram":
		provider, binding, bindingID = &entity.NamedIdentityProvider{}, &entity.NamedIdentityBinding{}, s.NamedIdentityBindingID
	default:
		return errSharedAdmissionConflict
	}
	if provider != nil {
		if err := sharedAuthorizationSQLRow(tx, "id", s.PrimaryMethod, provider); err != nil {
			return err
		}
		if err := sharedAuthorizationSQLRow(tx, "id", bindingID, binding); err != nil {
			return err
		}
	}
	return sharedAuthorizationSQLError(primaryValidateSession(sharedAuthorizationDB(tx), s))
}
