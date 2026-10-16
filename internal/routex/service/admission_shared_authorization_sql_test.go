package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type sharedAuthorizationSQLFixture struct {
	facts        sharedAuthorizationFacts
	managers     []entity.ProjectManager
	managerUser  entity.User
	managerUsers map[string]entity.User
	userReads    []string
	application  *entity.RegistrationApprovalApplication
	queries      []string
	missing      string
	failure      error
	binding      *entity.APIKeyVaultDeliveryBinding
	command      *entity.APIKeyDeliveryCommand
}

type sharedAuthorizationSQLConnector struct {
	f *sharedAuthorizationSQLFixture
}

func (c sharedAuthorizationSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return &sharedAuthorizationSQLConnection{rolesSQLConnection: &rolesSQLConnection{f: &rolesSQLFixture{}}, f: c.f}, nil
}
func (c sharedAuthorizationSQLConnector) Driver() driver.Driver {
	return sharedAuthorizationSQLDriver(c)
}

type sharedAuthorizationSQLDriver sharedAuthorizationSQLConnector

func (d sharedAuthorizationSQLDriver) Open(string) (driver.Conn, error) {
	return sharedAuthorizationSQLConnector(d).Connect(context.Background())
}

type sharedAuthorizationSQLConnection struct {
	*rolesSQLConnection
	f *sharedAuthorizationSQLFixture
}

func sharedAuthorizationSQLTestRows[T any](row *T) (driver.Rows, error) {
	if row == nil {
		return effectiveSQLRows([]T{})
	}
	return effectiveSQLRows([]T{*row})
}

func (c *sharedAuthorizationSQLConnection) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f := c.f
	f.queries = append(f.queries, q)
	if f.failure != nil {
		return nil, f.failure
	}
	if f.missing != "" && strings.Contains(q, `FROM "`+f.missing+`"`) {
		return &adminOverviewRows{columns: []string{"id"}}, nil
	}
	switch {
	case strings.Contains(q, `FROM "governance_settings"`):
		p := f.facts.Policy
		if p == nil {
			p = &entity.GovernanceSetting{ID: 1, AuthenticationPolicyRevision: strings.Repeat("a", 64)}
		}
		return sharedAuthorizationSQLTestRows(p)
	case strings.Contains(q, `FROM "users"`):
		if f.facts.Kind == "project_key" {
			id, _ := args[0].Value.(string)
			f.userReads = append(f.userReads, id)
			if f.managerUsers != nil {
				u, exists := f.managerUsers[id]
				if !exists {
					return effectiveSQLRows([]entity.User{})
				}
				return sharedAuthorizationSQLTestRows(&u)
			}
			return sharedAuthorizationSQLTestRows(&f.managerUser)
		}
		return sharedAuthorizationSQLTestRows(f.facts.User)
	case strings.Contains(q, `FROM "registration_approval_applications"`):
		return sharedAuthorizationSQLTestRows(f.application)
	case strings.Contains(q, `FROM "api_keys"`):
		return sharedAuthorizationSQLTestRows(f.facts.PersonalKey)
	case strings.Contains(q, `FROM "api_key_models"`):
		return effectiveSQLRows([]entity.APIKeyModel{{KeyID: f.facts.PersonalKey.ID, ModelID: f.facts.Model.ID}})
	case strings.Contains(q, `FROM "project_api_keys"`):
		return sharedAuthorizationSQLTestRows(f.facts.ProjectKey)
	case strings.Contains(q, `FROM "project_api_key_models"`):
		return effectiveSQLRows([]entity.ProjectKeyModel{{KeyID: f.facts.ProjectKey.ID, ModelID: f.facts.Model.ID}})
	case strings.Contains(q, `FROM "projects"`):
		return sharedAuthorizationSQLTestRows(f.facts.Project)
	case strings.Contains(q, `FROM "project_managers"`):
		if strings.Contains(q, "FOR UPDATE") {
			for _, row := range f.managers {
				if row.ID == args[0].Value {
					return sharedAuthorizationSQLTestRows(&row)
				}
			}
			return effectiveSQLRows([]entity.ProjectManager{})
		}
		return effectiveSQLRows(f.managers)
	case strings.Contains(q, `FROM "project_model_grants"`):
		return sharedAuthorizationSQLTestRows(f.facts.ProjectGrant)
	case strings.Contains(q, `FROM "sessions"`):
		return sharedAuthorizationSQLTestRows(f.facts.Session)
	case strings.Contains(q, `FROM "user_model_grants"`):
		return effectiveSQLRows(f.facts.PersonalRows)
	case strings.Contains(q, `FROM "teams"`):
		return sharedAuthorizationSQLTestRows(f.facts.Team)
	case strings.Contains(q, `FROM "team_memberships"`):
		return sharedAuthorizationSQLTestRows(f.facts.Membership)
	case strings.Contains(q, `FROM "team_model_grants"`):
		return sharedAuthorizationSQLTestRows(f.facts.TeamGrant)
	case strings.Contains(q, `FROM "models"`):
		return sharedAuthorizationSQLTestRows(&f.facts.Model)
	case strings.Contains(q, `FROM "user_mfa"`):
		return sharedAuthorizationSQLTestRows(f.facts.MFA)
	case strings.Contains(q, `FROM "api_key_vault_delivery_bindings"`):
		return sharedAuthorizationSQLTestRows(f.binding)
	case strings.Contains(q, `FROM "api_key_delivery_commands"`):
		return sharedAuthorizationSQLTestRows(f.command)
	default:
		return nil, errors.New("unexpected shared authorization SQL query")
	}
}

func (c *sharedAuthorizationSQLConnection) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return nil, errors.New("authorization loader must not write rows")
}

func sharedAuthorizationSQLTestDB(t *testing.T, kind string) (*gorm.DB, *sharedAuthorizationSQLFixture, sharedCapturedAuthorization) {
	t.Helper()
	f := &sharedAuthorizationSQLFixture{facts: sharedAuthorizationTestFacts(kind)}
	if f.facts.PersonalKey != nil {
		f.facts.PersonalKey.DeliveryMode = "manual"
	}
	if f.facts.ProjectKey != nil {
		f.facts.ProjectKey.DeliveryMode = "manual"
		f.managerUser = entity.User{ID: "usr_manager", CreatedAt: f.facts.Project.CreatedAt}
		f.managers = []entity.ProjectManager{{ID: "pmg_current", ProjectID: f.facts.Project.ID, UserID: f.managerUser.ID}}
	}
	original, err := newSharedCapturedAuthorization(f.facts, sharedAuthorizationTestTime())
	if err != nil {
		t.Fatal(err)
	}
	pool := sql.OpenDB(sharedAuthorizationSQLConnector{f})
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	return db, f, original
}

func TestSharedAuthorizationSQLFourCurrentPrincipals(t *testing.T) {
	for _, kind := range []string{"personal_key", "project_key", "personal_session", "team_session"} {
		t.Run(kind, func(t *testing.T) {
			db, f, original := sharedAuthorizationSQLTestDB(t, kind)
			err := db.Transaction(func(tx *gorm.DB) error {
				return validateSharedAuthorizationSQL(tx, original, sharedAuthorizationTestTime().Add(time.Minute))
			}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
			if err != nil {
				t.Fatal("current exact authority", err)
			}
			if len(f.queries) == 0 || !strings.Contains(f.queries[0], `FROM "governance_settings"`) || !strings.Contains(f.queries[0], "FOR UPDATE") {
				t.Fatal("governance must be first retained lock")
			}
			for _, table := range []string{"models", "sessions", "api_keys", "project_api_keys", "teams", "projects", "team_memberships"} {
				for _, q := range f.queries {
					if strings.Contains(q, `FROM "`+table+`"`) && !strings.Contains(q, "FOR UPDATE") {
						t.Fatalf("current %s read lacked its revocation lock", table)
					}
				}
			}
			if kind == "team_session" || kind == "project_key" {
				for _, q := range f.queries {
					if strings.Contains(q, "user_model_grants") {
						t.Fatal("resource principal borrowed Personal grants")
					}
				}
			}
		})
	}
}

func TestSharedAuthorizationSQLRejectsChangedCurrentFacts(t *testing.T) {
	cases := []struct {
		name, kind string
		change     func(*sharedAuthorizationSQLFixture)
	}{
		{"user_disabled", "personal_key", func(f *sharedAuthorizationSQLFixture) { f.facts.User.Disabled = true }},
		{"user_birth", "personal_session", func(f *sharedAuthorizationSQLFixture) {
			f.facts.User.CreatedAt = f.facts.User.CreatedAt.Add(time.Second)
		}},
		{"user_case_alias", "personal_key", func(f *sharedAuthorizationSQLFixture) { f.facts.User.ID = "USR_SHARED" }},
		{"key_revoked", "personal_key", func(f *sharedAuthorizationSQLFixture) { f.facts.PersonalKey.Status = entity.KeyRevoked }},
		{"key_revision", "personal_key", func(f *sharedAuthorizationSQLFixture) {
			f.facts.PersonalKey.LifecycleRevision = "kvr_01arz3ndektsv4rrffq69g5faz"
		}},
		{"key_expired", "personal_key", func(f *sharedAuthorizationSQLFixture) {
			now := sharedAuthorizationTestTime()
			f.facts.PersonalKey.ExpiresAt = &now
		}},
		{"key_owner", "personal_key", func(f *sharedAuthorizationSQLFixture) { f.facts.PersonalKey.UserID = "usr_other" }},
		{"selected_key_grant_missing", "personal_key", func(f *sharedAuthorizationSQLFixture) { f.missing = "api_key_models" }},
		{"personal_grant_revoked", "personal_session", func(f *sharedAuthorizationSQLFixture) { f.facts.PersonalRows = nil }},
		{"personal_grant_provenance", "personal_key", func(f *sharedAuthorizationSQLFixture) {
			source := "mar_changed"
			f.facts.PersonalRows[0].SourceRequestID = &source
		}},
		{"personal_revision", "personal_session", func(f *sharedAuthorizationSQLFixture) { f.facts.User.PersonalGrantRevision = strings.Repeat("c", 64) }},
		{"model_disabled", "team_session", func(f *sharedAuthorizationSQLFixture) { f.facts.Model.Status = entity.ResourceDisabled }},
		{"model_birth", "project_key", func(f *sharedAuthorizationSQLFixture) {
			f.facts.Model.CreatedAt = f.facts.Model.CreatedAt.Add(time.Second)
		}},
		{"project_disabled", "project_key", func(f *sharedAuthorizationSQLFixture) { f.facts.Project.Status = entity.ResourceDisabled }},
		{"project_manager_missing", "project_key", func(f *sharedAuthorizationSQLFixture) { f.managers = nil }},
		{"project_manager_ineligible", "project_key", func(f *sharedAuthorizationSQLFixture) { f.managerUser.Disabled = true }},
		{"project_grant_missing", "project_key", func(f *sharedAuthorizationSQLFixture) { f.facts.ProjectGrant = nil }},
		{"project_key_owner", "project_key", func(f *sharedAuthorizationSQLFixture) { f.facts.ProjectKey.ProjectID = "prj_other" }},
		{"session_deleted", "personal_session", func(f *sharedAuthorizationSQLFixture) { f.missing = "sessions" }},
		{"session_expired", "team_session", func(f *sharedAuthorizationSQLFixture) { f.facts.Session.ExpiresAt = sharedAuthorizationTestTime() }},
		{"session_policy", "personal_session", func(f *sharedAuthorizationSQLFixture) {
			f.facts.Policy.AuthenticationPolicyRevision = strings.Repeat("c", 64)
		}},
		{"session_mixed_primary", "personal_session", func(f *sharedAuthorizationSQLFixture) { f.facts.Session.OIDCBindingID = "oib_mixed" }},
		{"team_disabled", "team_session", func(f *sharedAuthorizationSQLFixture) { f.facts.Team.Status = entity.ResourceDisabled }},
		{"membership_disabled", "team_session", func(f *sharedAuthorizationSQLFixture) { f.facts.Membership.Status = entity.ResourceDisabled }},
		{"membership_identity", "team_session", func(f *sharedAuthorizationSQLFixture) { f.facts.Membership.ID = "tmb_other" }},
		{"membership_user", "team_session", func(f *sharedAuthorizationSQLFixture) { f.facts.Membership.UserID = "usr_other" }},
		{"membership_role", "team_session", func(f *sharedAuthorizationSQLFixture) { f.facts.Membership.Role = entity.TeamOwner }},
		{"team_grant_missing", "team_session", func(f *sharedAuthorizationSQLFixture) { f.facts.TeamGrant = nil }},
		{"team_grant_provenance", "team_session", func(f *sharedAuthorizationSQLFixture) {
			source := "mar_changed"
			f.facts.TeamGrant.SourceRequestID = &source
		}},
		{"managed_key_without_ack", "personal_key", func(f *sharedAuthorizationSQLFixture) { f.facts.PersonalKey.DeliveryMode = "managed_vault" }},
		{"managed_project_key_without_ack", "project_key", func(f *sharedAuthorizationSQLFixture) { f.facts.ProjectKey.DeliveryMode = "managed_vault" }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			db, f, original := sharedAuthorizationSQLTestDB(t, test.kind)
			test.change(f)
			err := db.Transaction(func(tx *gorm.DB) error {
				return validateSharedAuthorizationSQL(tx, original, sharedAuthorizationTestTime().Add(time.Minute))
			}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
			if err == nil {
				t.Fatal("changed current authority accepted captured proof")
			}
		})
	}
}

func TestSharedAuthorizationSQLRequiresTransactionAndPropagatesFailure(t *testing.T) {
	db, f, original := sharedAuthorizationSQLTestDB(t, "personal_key")
	if err := validateSharedAuthorizationSQL(db, original, sharedAuthorizationTestTime()); err == nil || len(f.queries) != 0 {
		t.Fatal("base DB reached authority reads")
	}
	if err := validateSharedAuthorizationSQL(nil, original, sharedAuthorizationTestTime()); err == nil {
		t.Fatal("nil transaction accepted")
	}
	fault := errors.New("controlled current authority read failure")
	f.failure = fault
	err := db.Transaction(func(tx *gorm.DB) error {
		return validateSharedAuthorizationSQL(tx, original, sharedAuthorizationTestTime())
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if !errors.Is(err, fault) {
		t.Fatal("SQL failure replaced by authority absence", err)
	}
}

func TestSharedAuthorizationSQLManagedAcknowledgement(t *testing.T) {
	for _, kind := range []string{"personal_key", "project_key"} {
		for _, corruption := range []string{"", "revoked_command", "unacknowledged", "owner_birth", "binding_birth"} {
			t.Run(kind+"/"+corruption, func(t *testing.T) {
				db, f, original := sharedAuthorizationSQLTestDB(t, kind)
				ownerKind, ownerID, birth, keyID := "personal", "", sharedAuthorizationTestTime().Add(-time.Hour), ""
				if kind == "personal_key" {
					f.facts.PersonalKey.DeliveryMode = "managed_vault"
					ownerID, keyID = f.facts.User.ID, f.facts.PersonalKey.ID
				} else {
					ownerKind, ownerID, keyID = "project", f.facts.Project.ID, f.facts.ProjectKey.ID
					f.facts.ProjectKey.DeliveryMode = "managed_vault"
				}
				ack, claim := sharedAuthorizationTestTime().Add(-time.Minute), sharedAuthorizationTestTime().Add(-2*time.Minute)
				f.binding = &entity.APIKeyVaultDeliveryBinding{ID: "vdb_shared", KeyKind: ownerKind, KeyID: keyID, KeyBirth: birth,
					OwnerKind: ownerKind, OwnerID: ownerID, OwnerBirth: birth, OwnerDigest: apiKeyDeliveryOwnerDigest(ownerKind, ownerID, birth),
					KeyDigest: apiKeyDeliveryKeyDigest(ownerKind, keyID, birth), Version: 1, AcknowledgedAt: &ack,
					ProfileID: "vap_shared", ProfileBirth: birth, PrincipalID: "vai_shared", PrincipalBirth: birth, IsolationRevision: strings.Repeat("a", 64)}
				b := f.binding
				f.command = &entity.APIKeyDeliveryCommand{ID: "vdc_shared", BindingID: b.ID, KeyID: keyID, KeyBirth: &birth,
					OwnerKind: b.OwnerKind, OwnerID: b.OwnerID, OwnerBirth: b.OwnerBirth, OwnerDigest: b.OwnerDigest,
					ProfileID: b.ProfileID, ProfileBirth: b.ProfileBirth, PrincipalID: b.PrincipalID, PrincipalBirth: b.PrincipalBirth,
					IsolationRevision: b.IsolationRevision, DeliveryStatus: "acknowledged", AcknowledgedAt: &ack,
					ClaimedAt: &claim, ClaimProcessBirth: &birth, ClaimProcessID: "sys_shared"}
				switch corruption {
				case "revoked_command":
					f.command.RevokedAt = &ack
				case "unacknowledged":
					f.binding.AcknowledgedAt = nil
				case "owner_birth":
					f.binding.OwnerBirth = birth.Add(time.Second)
				case "binding_birth":
					f.command.KeyBirth = &ack
				}
				err := db.Transaction(func(tx *gorm.DB) error {
					return validateSharedAuthorizationSQL(tx, original, sharedAuthorizationTestTime())
				}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
				if (err == nil) != (corruption == "") {
					t.Fatal("managed acknowledgement proof", err)
				}
			})
		}
	}
}

func TestSharedAuthorizationSQLEmergencyUsesExplicitTime(t *testing.T) {
	db, f, _ := sharedAuthorizationSQLTestDB(t, "personal_session")
	sharedAuthorizationTestPrimary(&f.facts, "emergency")
	original, err := newSharedCapturedAuthorization(f.facts, sharedAuthorizationTestTime())
	if err != nil {
		t.Fatal(err)
	}
	validate := func() error {
		return db.Transaction(func(tx *gorm.DB) error {
			return validateSharedAuthorizationSQL(tx, original, sharedAuthorizationTestTime())
		}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	}
	if err := validate(); err != nil {
		t.Fatal("emergency authority incorrectly used wall clock", err)
	}
	f.facts.MFA.Generation = strings.Repeat("x", 43)
	if err := validate(); err == nil {
		t.Fatal("changed MFA generation authorized emergency Session")
	}
}

func TestSharedAuthorizationSQLApprovedAdmission(t *testing.T) {
	for _, changed := range []string{"", "revision", "decision", "application_birth", "subject_birth", "removed_link"} {
		t.Run(changed, func(t *testing.T) {
			db, f, _ := sharedAuthorizationSQLTestDB(t, "personal_session")
			applicationID, actor, reason := "raa_01j00000000000000000000000", "usr_reviewer", "Approved exact original subject"
			birth, decided := f.facts.User.CreatedAt, sharedAuthorizationTestTime().Add(-time.Minute)
			f.facts.User.ApprovalApplicationID = &applicationID
			f.application = &entity.RegistrationApprovalApplication{ID: applicationID, UserID: f.facts.User.ID, UserCreatedAt: birth,
				CreatedAt: birth, State: "approved", Revision: strings.Repeat("b", 64), DecidedAt: &decided, DecisionActorID: &actor, DecisionReason: &reason}
			_, proof := registrationAdmission(*f.facts.User, map[string]entity.RegistrationApprovalApplication{applicationID: *f.application})
			f.facts.Admission = &proof
			original, err := newSharedCapturedAuthorization(f.facts, sharedAuthorizationTestTime())
			if err != nil {
				t.Fatal(err)
			}
			switch changed {
			case "revision":
				f.application.Revision = strings.Repeat("c", 64)
			case "decision":
				f.application.State = "rejected"
			case "application_birth":
				f.application.CreatedAt = birth.Add(time.Second)
			case "subject_birth":
				f.application.UserCreatedAt = birth.Add(time.Second)
			case "removed_link":
				f.facts.User.ApprovalApplicationID = nil
			}
			err = db.Transaction(func(tx *gorm.DB) error {
				return validateSharedAuthorizationSQL(tx, original, sharedAuthorizationTestTime())
			}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
			if (err == nil) != (changed == "") {
				t.Fatal("linked admission proof", err)
			}
		})
	}
}

func TestSharedAuthorizationSQLManagerLockOrder(t *testing.T) {
	db, f, original := sharedAuthorizationSQLTestDB(t, "project_key")
	f.managerUsers = map[string]entity.User{}
	f.managers = nil
	for _, id := range []string{"usr_c", "usr_a", "usr_b"} {
		f.managerUsers[id] = entity.User{ID: id, CreatedAt: f.managerUser.CreatedAt}
		f.managers = append(f.managers, entity.ProjectManager{ID: "pmg_" + id, ProjectID: f.facts.Project.ID, UserID: id})
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		return validateSharedAuthorizationSQL(tx, original, sharedAuthorizationTestTime())
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil || strings.Join(f.userReads, ",") != "usr_a,usr_b,usr_c" {
		t.Fatal("manager Users must be locked deterministically before Project", err, f.userReads)
	}
}
