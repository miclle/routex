package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func offboardingAdmissionCandidate() (entity.User, entity.RegistrationApprovalApplication) {
	now := time.Date(2026, 10, 5, 1, 2, 3, 456789000, time.UTC)
	applicationID, actor, reason := "raa_01j00000000000000000000000", "usr_admin", "Reviewed successor"
	user := entity.User{ID: "usr_a", CreatedAt: now, ApprovalApplicationID: &applicationID}
	app := entity.RegistrationApprovalApplication{ID: applicationID, UserID: user.ID, UserCreatedAt: now, CreatedAt: now, State: "approved", Revision: memberRoleBaseline, DecidedAt: &now, DecisionActorID: &actor, DecisionReason: &reason}
	return user, app
}

func TestEmergencyOffboardingAdmittedCandidateSelection(t *testing.T) {
	cases := []struct {
		name      string
		change    func(*entity.User, *entity.RegistrationApprovalApplication)
		available bool
		want      string
	}{
		{"approved", func(*entity.User, *entity.RegistrationApprovalApplication) {}, true, "usr_a"},
		{"pending_first", func(_ *entity.User, a *entity.RegistrationApprovalApplication) {
			a.State = "pending"
			a.DecidedAt = nil
			a.DecisionActorID = nil
			a.DecisionReason = nil
		}, true, "usr_z"},
		{"rejected_first", func(_ *entity.User, a *entity.RegistrationApprovalApplication) { a.State = "rejected" }, true, "usr_z"},
		{"approved_disabled", func(u *entity.User, _ *entity.RegistrationApprovalApplication) { u.Disabled = true }, true, "usr_z"},
		{"approved_offboarded", func(u *entity.User, _ *entity.RegistrationApprovalApplication) { u.OffboardedAt = &u.CreatedAt }, true, "usr_z"},
		{"application_missing", func(*entity.User, *entity.RegistrationApprovalApplication) {}, false, "usr_z"},
		{"creation_changed", func(_ *entity.User, a *entity.RegistrationApprovalApplication) {
			a.UserCreatedAt = a.UserCreatedAt.Add(time.Microsecond)
		}, true, "usr_z"},
		{"application_user_alias", func(_ *entity.User, a *entity.RegistrationApprovalApplication) { a.UserID = strings.ToUpper(a.UserID) }, true, "usr_z"},
		{"unmanaged", func(u *entity.User, _ *entity.RegistrationApprovalApplication) { u.ApprovalApplicationID = nil }, false, "usr_a"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			user, app := offboardingAdmissionCandidate()
			c.change(&user, &app)
			apps := map[string]entity.RegistrationApprovalApplication{}
			if c.available {
				apps[app.ID] = app
			}
			admission, _ := registrationAdmission(user, apps)
			inventory := &OffboardingInventory{UserID: "usr_departing", Teams: []OffboardingResource{{ID: "tea_sole", RequiresSuccessor: true, People: []OffboardingPerson{{UserID: "usr_departing", Status: entity.ResourceActive}, {UserID: "usr_a", Status: entity.ResourceActive}, {UserID: "usr_z", Status: entity.ResourceActive}}}}}
			got := emergencyOffboardingAssignments(inventory, "usr_admin", OffboardingAssignments{}, map[string]bool{user.ID: admission.AdmissionEligible, "usr_z": true})
			if len(got.Teams) != 1 || !slices.Equal(got.Teams[0].OwnerUserIDs, []string{c.want}) {
				t.Fatalf("selected wrong successor: %+v", got.Teams)
			}
		})
	}
	inventory := &OffboardingInventory{UserID: "usr_departing", Teams: []OffboardingResource{{ID: "tea_sole", RequiresSuccessor: true, People: []OffboardingPerson{{UserID: "usr_departing", Status: entity.ResourceActive}, {UserID: "usr_a", Status: entity.ResourceDisabled}, {UserID: "usr_z", Disabled: true, Status: entity.ResourceActive}}}}}
	got := emergencyOffboardingAssignments(inventory, "usr_admin", OffboardingAssignments{}, map[string]bool{"usr_departing": true, "usr_a": true, "usr_z": true})
	if len(got.Teams) != 0 {
		t.Fatal("departing/disabled membership borrowed admission")
	}
	inventory.Teams[0].People = []OffboardingPerson{{UserID: "usr_a", Status: entity.ResourceActive}}
	if got := emergencyOffboardingAssignments(inventory, "usr_admin", OffboardingAssignments{}, nil); len(got.Teams) != 0 {
		t.Fatal("missing complete admission facts selected a successor")
	}
	explicit := OffboardingAssignments{Teams: []OffboardingTeamAssignment{{TeamID: "tea_sole", OwnerUserIDs: []string{"usr_explicit"}, AddMemberUserIDs: []string{"usr_explicit"}}}}
	if got := emergencyOffboardingAssignments(inventory, "usr_admin", explicit, map[string]bool{"usr_a": true}); !reflect.DeepEqual(got, explicit) {
		t.Fatal("automatic selection replaced explicit reviewed intent")
	}
}

type offboardingAdmissionSQLFixture struct {
	*rolesSQLFixture
	users        []entity.User
	applications []entity.RegistrationApprovalApplication
	failTable    string
}
type offboardingAdmissionSQLConnector struct {
	f *offboardingAdmissionSQLFixture
}

func (c offboardingAdmissionSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return &offboardingAdmissionSQLConnection{rolesSQLConnection: &rolesSQLConnection{f: c.f.rolesSQLFixture}, f: c.f}, nil
}
func (c offboardingAdmissionSQLConnector) Driver() driver.Driver {
	return offboardingAdmissionSQLDriver(c)
}

type offboardingAdmissionSQLDriver offboardingAdmissionSQLConnector

func (d offboardingAdmissionSQLDriver) Open(string) (driver.Conn, error) {
	return offboardingAdmissionSQLConnector(d).Connect(context.Background())
}

type offboardingAdmissionSQLConnection struct {
	*rolesSQLConnection
	f *offboardingAdmissionSQLFixture
}

func (c *offboardingAdmissionSQLConnection) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.f.queries = append(c.f.queries, q)
	if c.f.failTable != "" && strings.Contains(q, c.f.failTable) {
		return nil, errors.New("controlled admission hydration outage")
	}
	ids := rolesSQLStrings(args)
	switch {
	case strings.Contains(q, `FROM "users"`):
		rows := []entity.User{}
		for _, u := range c.f.users {
			if slices.Contains(ids, u.ID) || slices.Contains(ids, strings.ToLower(u.ID)) {
				rows = append(rows, u)
			}
		}
		return effectiveSQLRows(rows)
	case strings.Contains(q, `FROM "registration_approval_applications"`):
		rows := []entity.RegistrationApprovalApplication{}
		for _, a := range c.f.applications {
			if slices.Contains(ids, a.ID) {
				rows = append(rows, a)
			}
		}
		return effectiveSQLRows(rows)
	default:
		return nil, errors.New("unexpected auto-selection query")
	}
}
func offboardingAdmissionDB(t *testing.T, f *offboardingAdmissionSQLFixture) *gorm.DB {
	t.Helper()
	f.rolesSQLFixture = &rolesSQLFixture{}
	pool := sql.OpenDB(offboardingAdmissionSQLConnector{f: f})
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db
}
func TestEmergencyOffboardingHydratesCompleteCandidateAdmission(t *testing.T) {
	for _, scenario := range []string{"pending_first", "missing_application", "user_alias", "users_outage", "applications_outage", "approved_disabled", "approved_offboarded"} {
		t.Run(scenario, func(t *testing.T) {
			user, app := offboardingAdmissionCandidate()
			app.State = "pending"
			app.DecidedAt = nil
			app.DecisionActorID = nil
			app.DecisionReason = nil
			f := &offboardingAdmissionSQLFixture{users: []entity.User{user, {ID: "usr_z", CreatedAt: user.CreatedAt}}, applications: []entity.RegistrationApprovalApplication{app}}
			switch scenario {
			case "missing_application":
				f.applications = nil
			case "user_alias":
				f.users[0].ID = "USR_A"
			case "users_outage":
				f.failTable = `FROM "users"`
			case "applications_outage":
				f.failTable = `FROM "registration_approval_applications"`
			case "approved_disabled", "approved_offboarded":
				u, a := offboardingAdmissionCandidate()
				if scenario == "approved_disabled" {
					u.Disabled = true
				} else {
					u.OffboardedAt = &u.CreatedAt
				}
				f.users[0] = u
				f.applications[0] = a
			}
			db := offboardingAdmissionDB(t, f)
			inventory := &OffboardingInventory{UserID: "usr_departing", Teams: []OffboardingResource{{ID: "tea_sole", RequiresSuccessor: true, People: []OffboardingPerson{{UserID: "usr_a", Status: entity.ResourceActive}, {UserID: "usr_z", Status: entity.ResourceActive}}}}}
			// Initialized query state must not bleed into either candidate batch.
			got, err := prepareEmergencyOffboardingAssignments(db.Where("id = ?", "usr_obsolete").Limit(1), inventory, "usr_admin", OffboardingAssignments{})
			if f.failTable != "" {
				if err == nil || len(got.Teams) != 0 {
					t.Fatal("hydration outage produced assignments")
				}
				return
			}
			if err != nil || len(got.Teams) != 1 || !slices.Equal(got.Teams[0].OwnerUserIDs, []string{"usr_z"}) {
				t.Fatalf("candidate selection failed: %v %+v", err, got.Teams)
			}
			if len(f.queries) != 2 || len(f.writes) != 0 {
				t.Fatal("selection did not use bounded complete read-only hydration")
			}
			for _, q := range f.queries {
				if strings.Contains(q, `"id" =`) {
					t.Fatal("obsolete initialized query predicate leaked")
				}
			}
		})
	}
}
func TestEmergencyOffboardingCandidateBatchAndExplicitSelection(t *testing.T) {
	f := &offboardingAdmissionSQLFixture{}
	inventory := &OffboardingInventory{UserID: "usr_departing", Teams: []OffboardingResource{{ID: "tea_sole", RequiresSuccessor: true}}}
	now := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	for i := range 501 {
		id := fmt.Sprintf("usr_%04d", i)
		f.users = append(f.users, entity.User{ID: id, CreatedAt: now})
		inventory.Teams[0].People = append(inventory.Teams[0].People, OffboardingPerson{UserID: id, Status: entity.ResourceActive})
	}
	db := offboardingAdmissionDB(t, f)
	got, err := prepareEmergencyOffboardingAssignments(db, inventory, "usr_admin", OffboardingAssignments{})
	if err != nil || len(got.Teams) != 1 || got.Teams[0].OwnerUserIDs[0] != "usr_0000" || len(f.queries) != 2 || len(f.writes) != 0 {
		t.Fatal("candidate batch lost stable order/bounds", err)
	}
	explicit := OffboardingAssignments{Teams: []OffboardingTeamAssignment{{TeamID: "tea_sole", OwnerUserIDs: []string{"usr_explicit"}}}}
	f.queries = nil
	f.failTable = `FROM "users"`
	got, err = prepareEmergencyOffboardingAssignments(db, inventory, "usr_admin", explicit)
	if err != nil || len(f.queries) != 0 || !reflect.DeepEqual(got, explicit) {
		t.Fatal("explicit selection performed unrelated hydration")
	}
}
