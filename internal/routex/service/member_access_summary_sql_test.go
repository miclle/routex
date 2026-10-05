package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type accessSQLFixture struct {
	actor, subject entity.User
	permissions    map[string]bool
	assignments    []entity.UserRole
	roles          []entity.Role
	memberships    []entity.TeamMembership
	teams          []entity.Team
	queries        []string
	options        driver.TxOptions
	failTable      string
}
type accessSQLConnector struct{ fixture *accessSQLFixture }

func (c accessSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return accessSQLConnection(c), nil
}
func (c accessSQLConnector) Driver() driver.Driver { return accessSQLDriver(c) }

type accessSQLDriver accessSQLConnector

func (d accessSQLDriver) Open(string) (driver.Conn, error) { return accessSQLConnection(d), nil }

type accessSQLConnection accessSQLConnector

func (accessSQLConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepared statement")
}
func (accessSQLConnection) Close() error { return nil }
func (c accessSQLConnection) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c accessSQLConnection) BeginTx(_ context.Context, options driver.TxOptions) (driver.Tx, error) {
	c.fixture.options = options
	return adminOverviewTransaction{}, nil
}
func (c accessSQLConnection) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f := c.fixture
	f.queries = append(f.queries, query)
	if f.failTable != "" && strings.Contains(query, f.failTable) {
		return nil, errors.New("controlled selected read failure")
	}
	switch {
	case strings.Contains(query, `FROM "users"`):
		if args[0].Value == "usr_reader" {
			return effectiveSQLRows([]entity.User{f.actor})
		}
		return effectiveSQLRows([]entity.User{f.subject})
	case strings.Contains(query, "role_permissions AS permission"):
		for _, arg := range args {
			if permission, ok := arg.Value.(string); ok && f.permissions[permission] {
				return effectiveSQLRows([]exactPermissionIdentity{{RoleID: "rol_admin", PermissionRoleID: "rol_admin", Permission: permission}})
			}
		}
		return effectiveSQLRows([]exactPermissionIdentity{})
	case strings.Contains(query, `FROM "user_roles"`):
		return effectiveSQLRows(f.assignments)
	case strings.Contains(query, `FROM "roles"`):
		return effectiveSQLRows(f.roles)
	case strings.Contains(query, `FROM "team_memberships"`):
		return effectiveSQLRows(f.memberships)
	case strings.Contains(query, `FROM "teams"`):
		return effectiveSQLRows(f.teams)
	}
	return nil, fmt.Errorf("unexpected selected query %s", query)
}
func TestMemberAccessMeasuredBoundedReadOnlySnapshot(t *testing.T) {
	for _, name := range []string{"empty", "one", "hundred", "denied_sections", "no_member_read", "roles_overflow", "teams_overflow", "read_failure", "actor_alias", "subject_alias", "unknown_updated"} {
		t.Run(name, func(t *testing.T) {
			when := time.Date(2026, 10, 5, 8, 0, 0, 123456000, time.FixedZone("controlled", 8*3600))
			f := &accessSQLFixture{actor: entity.User{ID: "usr_reader", Role: "admin", CreatedAt: when}, subject: entity.User{ID: "usr_subject", Role: "member", CreatedAt: when, UpdatedAt: when}, permissions: map[string]bool{"members.read": true, "roles.read": true, "teams.read_all": true}}
			count := 1
			if name == "empty" {
				count = 0
			}
			if name == "hundred" {
				count = 100
			}
			for i := range count {
				roleID, teamID := fmt.Sprintf("rol_%03d", i), fmt.Sprintf("tea_%03d", i)
				f.assignments = append(f.assignments, entity.UserRole{UserID: f.subject.ID, RoleID: roleID})
				f.roles = append(f.roles, entity.Role{ID: roleID, Name: "Recorded role"})
				f.memberships = append(f.memberships, entity.TeamMembership{ID: fmt.Sprintf("tmm_%03d", i), UserID: f.subject.ID, TeamID: teamID, Role: "member", Status: "active"})
				f.teams = append(f.teams, entity.Team{ID: teamID, Name: "Recorded team", Status: "active"})
			}
			switch name {
			case "denied_sections":
				f.permissions["roles.read"], f.permissions["teams.read_all"] = false, false
			case "no_member_read":
				f.permissions["members.read"] = false
			case "roles_overflow":
				f.assignments = make([]entity.UserRole, 10001)
			case "teams_overflow":
				f.memberships = make([]entity.TeamMembership, 1001)
			case "read_failure":
				f.failTable = `FROM "teams"`
			case "actor_alias":
				f.actor.ID = "USR_READER"
			case "subject_alias":
				f.subject.ID = "USR_SUBJECT"
			case "unknown_updated":
				f.subject.UpdatedAt = time.Time{}
			}
			pool := sql.OpenDB(accessSQLConnector{f})
			t.Cleanup(func() { _ = pool.Close() })
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true})
			if err != nil {
				t.Fatal(err)
			}
			result, err := (&Service{db: db}).GetMemberAccessSummary(context.Background(), "usr_reader", "usr_subject")
			if f.options.Isolation != driver.IsolationLevel(sql.LevelRepeatableRead) || !f.options.ReadOnly {
				t.Fatal(f.options)
			}
			switch name {
			case "no_member_read":
				if err != apperrors.ErrForbidden || result != nil || len(f.queries) != 2 {
					t.Fatal(err, result, f.queries)
				}
				return
			case "actor_alias":
				if err != apperrors.ErrUnauthorized || result != nil || len(f.queries) != 1 {
					t.Fatal(err, result, f.queries)
				}
				return
			case "subject_alias":
				if err != apperrors.ErrNotFound || result != nil || len(f.queries) != 3 {
					t.Fatal(err, result, f.queries)
				}
				return
			case "read_failure":
				if err != memberAccessUnavailable || result != nil {
					t.Fatal(err, result)
				}
				return
			}
			if err != nil || result == nil || result.UserID != "usr_subject" || result.ObservedAt.IsZero() || result.ObservedAt.Location() != time.UTC {
				t.Fatal(result, err)
			}
			if name == "unknown_updated" {
				if result.UpdatedAt != nil {
					t.Fatal("invented updated time", result)
				}
			} else if result.UpdatedAt == nil || result.UpdatedAt.Location() != time.UTC || !result.UpdatedAt.Equal(when) {
				t.Fatal(result)
			}
			expectedReads := 9
			switch name {
			case "empty":
				expectedReads = 7
				if result.Roles.Items == nil || result.Teams.Items == nil {
					t.Fatal(result)
				}
			case "denied_sections":
				expectedReads = 5
				if result.Roles.Status != "not_authorized" || result.Roles.Items != nil || result.Teams.Items != nil {
					t.Fatal(result)
				}
			case "roles_overflow":
				expectedReads = 8
				if result.Roles.Status != "overflow" || result.Roles.Items != nil || result.Teams.Status != "available" {
					t.Fatal(result)
				}
			case "teams_overflow":
				expectedReads = 8
				if result.Teams.Status != "overflow" || result.Teams.Items != nil || result.Roles.Status != "available" {
					t.Fatal(result)
				}
			}
			if len(f.queries) != expectedReads {
				t.Fatal(name, len(f.queries), f.queries)
			}
		})
	}
}

func TestMemberAccessCanceledReadReturnsNoPartialProjection(t *testing.T) {
	f := &accessSQLFixture{}
	pool := sql.OpenDB(accessSQLConnector{f})
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := (&Service{db: db}).GetMemberAccessSummary(ctx, "usr_reader", "usr_subject")
	if err != memberAccessUnavailable || result != nil || len(f.queries) != 0 {
		t.Fatal("canceled read returned data or performed queries", result, err, f.queries)
	}
}
