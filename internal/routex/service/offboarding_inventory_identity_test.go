package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// This driver deliberately returns a collating match. Exact identity must be
// checked in Go before any retained asset or historical case can be selected.
type offboardingInventoryIdentityFixture struct {
	*rolesSQLFixture
	subject     entity.User
	permissions []string
}
type offboardingInventoryIdentityConnector struct {
	f *offboardingInventoryIdentityFixture
}

func (c offboardingInventoryIdentityConnector) Connect(context.Context) (driver.Conn, error) {
	return &offboardingInventoryIdentityConnection{rolesSQLConnection: &rolesSQLConnection{f: c.f.rolesSQLFixture}, f: c.f}, nil
}
func (c offboardingInventoryIdentityConnector) Driver() driver.Driver {
	return offboardingInventoryIdentityDriver(c)
}

type offboardingInventoryIdentityDriver offboardingInventoryIdentityConnector

func (d offboardingInventoryIdentityDriver) Open(string) (driver.Conn, error) {
	return offboardingInventoryIdentityConnector(d).Connect(context.Background())
}

type offboardingInventoryIdentityConnection struct {
	*rolesSQLConnection
	f *offboardingInventoryIdentityFixture
}

func (c *offboardingInventoryIdentityConnection) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.f.queries = append(c.f.queries, q)
	values := rolesSQLStrings(args)
	switch {
	case strings.Contains(q, `FROM "users"`):
		if len(values) > 0 && values[0] == "usr_reader" {
			return effectiveSQLRows([]entity.User{{ID: "usr_reader", Role: entity.RoleMember, CreatedAt: c.f.subject.CreatedAt}})
		}
		if len(values) > 0 && strings.EqualFold(strings.TrimSpace(values[0]), c.f.subject.ID) {
			return effectiveSQLRows([]entity.User{c.f.subject})
		}
		return effectiveSQLRows([]entity.User{})
	case strings.Contains(q, "SELECT DISTINCT p.permission"):
		rows := []struct{ Permission string }{}
		for _, permission := range c.f.permissions {
			rows = append(rows, struct{ Permission string }{permission})
		}
		return effectiveSQLRows(rows)
	case strings.Contains(q, `FROM "api_keys"`):
		return effectiveSQLRows([]OffboardingKey{})
	case strings.Contains(q, "FROM projects p"):
		return effectiveSQLRows([]entity.Project{})
	case strings.Contains(q, `FROM "team_memberships"`):
		return effectiveSQLRows([]entity.TeamMembership{})
	case strings.Contains(q, `FROM "user_roles"`):
		return effectiveSQLRows([]entity.UserRole{})
	case strings.Contains(q, `FROM "offboarding_cases"`):
		return effectiveSQLRows([]entity.OffboardingCase{{ID: "obc_history", UserID: c.f.subject.ID, ActorID: "usr_original", Mode: "planned", Status: "completed", Reason: "Recorded reason", CreatedAt: c.f.subject.CreatedAt, AssignmentsJSON: `{"project_assignments":[],"team_assignments":[]}`}})
	default:
		return nil, errors.New("unexpected inventory identity query")
	}
}
func offboardingInventoryIdentityDB(t *testing.T, f *offboardingInventoryIdentityFixture) *gorm.DB {
	t.Helper()
	f.rolesSQLFixture = &rolesSQLFixture{data: rolesSQLData{users: map[string]entity.User{}, roles: map[string]entity.Role{}, permissions: map[string][]string{}}}
	pool := sql.OpenDB(offboardingInventoryIdentityConnector{f: f})
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	return db
}
func TestOffboardingInventoryRejectsCollatingIdentityBeforeDisclosure(t *testing.T) {
	for _, id := range []string{"USR_TARGET", "usr_targeT", "usr_target ", "usr_target"} {
		t.Run(id, func(t *testing.T) {
			now := time.Date(2026, 10, 5, 1, 2, 3, 0, time.UTC)
			f := &offboardingInventoryIdentityFixture{subject: entity.User{ID: "usr_target", Role: entity.RoleMember, CreatedAt: now}, permissions: []string{"members.read"}}
			db := offboardingInventoryIdentityDB(t, f)
			got, err := (&Service{db: db}).OffboardingInventory(context.Background(), "usr_reader", id)
			if id == f.subject.ID {
				if err != nil || got == nil || got.UserID != id || len(got.Cases) != 1 {
					t.Fatalf("canonical history unavailable: %v", err)
				}
			} else {
				if got != nil || !errors.Is(err, apperrors.ErrNotFound) {
					t.Fatalf("collating alias exposed inventory or wrong error: %v", err)
				}
				if len(f.queries) != 3 {
					t.Fatal("alias selected inventory, admission application or historical cases")
				}
			}
			if len(f.writes) != 0 || len(f.transactions) != 1 || !f.transactions[0].ReadOnly || f.transactions[0].Isolation != driver.IsolationLevel(sql.LevelRepeatableRead) {
				t.Fatal("inventory changed data or readonly isolation")
			}
		})
	}
}
func TestOffboardingInventoryRetainedLifecycleAndReadPermission(t *testing.T) {
	for _, state := range []string{"active", "disabled", "offboarded"} {
		for _, permission := range []string{"members.read", "members.write", ""} {
			t.Run(state+"/"+permission, func(t *testing.T) {
				now := time.Date(2026, 10, 5, 1, 2, 3, 0, time.UTC)
				user := entity.User{ID: "usr_target", Role: entity.RoleMember, CreatedAt: now, Disabled: state != "active"}
				if state == "offboarded" {
					user.OffboardedAt = &now
				}
				f := &offboardingInventoryIdentityFixture{subject: user, permissions: []string{permission}}
				db := offboardingInventoryIdentityDB(t, f)
				got, err := (&Service{db: db}).OffboardingInventory(context.Background(), "usr_reader", user.ID)
				if permission != "members.read" {
					if got != nil || !errors.Is(err, apperrors.ErrForbidden) || len(f.queries) != 2 {
						t.Fatalf("read permission was borrowed: %v", err)
					}
				} else if err != nil || got == nil || got.Disabled != user.Disabled || !reflect.DeepEqual(got.OffboardedAt, user.OffboardedAt) || len(got.Cases) != 1 || got.Cases[0].UserID != user.ID || got.InventoryVersion == "" {
					t.Fatalf("retained lifecycle/history changed: %v", err)
				}
				if len(f.writes) != 0 || !reflect.DeepEqual(f.subject, user) {
					t.Fatal("read mutated retained identity")
				}
			})
		}
	}
}

func TestOffboardingInventoryPreservesExactRetainedIDsAndMissingTarget(t *testing.T) {
	for _, id := range []string{"USR_Target", "legacy-User"} {
		t.Run(id, func(t *testing.T) {
			now := time.Date(2026, 10, 5, 1, 2, 3, 0, time.UTC)
			f := &offboardingInventoryIdentityFixture{subject: entity.User{ID: id, Role: entity.RoleMember, CreatedAt: now}, permissions: []string{"members.read"}}
			db := offboardingInventoryIdentityDB(t, f)
			svc := &Service{db: db}
			got, err := svc.OffboardingInventory(context.Background(), "usr_reader", id)
			if err != nil || got == nil || got.UserID != id {
				t.Fatalf("exact historical identity was normalized or rejected: %v", err)
			}
			f.queries = nil
			got, err = svc.OffboardingInventory(context.Background(), "usr_reader", "missing_user")
			if got != nil || !errors.Is(err, apperrors.ErrNotFound) || len(f.queries) != 3 {
				t.Fatal("missing identity disclosed inventory or borrowed retained history")
			}
		})
	}
}
