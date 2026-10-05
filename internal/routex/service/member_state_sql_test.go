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

// Reuse the existing transactional SQL store, extending only lifecycle tables
// and exact current-admin COUNT. No database or runtime process is started.
type stateSQLFixture struct {
	*rolesSQLFixture
	locks     []string
	countSQL  string
	ownedTeam bool
}
type stateSQLConnector struct{ f *stateSQLFixture }

func (c stateSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return &stateSQLConnection{rolesSQLConnection: &rolesSQLConnection{f: c.f.rolesSQLFixture}, f: c.f}, nil
}
func (c stateSQLConnector) Driver() driver.Driver { return stateSQLDriver(c) }

type stateSQLDriver stateSQLConnector

func (d stateSQLDriver) Open(string) (driver.Conn, error) {
	return &stateSQLConnection{rolesSQLConnection: &rolesSQLConnection{f: d.f.rolesSQLFixture}, f: d.f}, nil
}

type stateSQLConnection struct {
	*rolesSQLConnection
	f *stateSQLFixture
}

func (c *stateSQLConnection) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	values := rolesSQLStrings(args)
	if strings.Contains(q, `FROM "users"`) && strings.Contains(q, "FOR UPDATE") {
		c.f.locks = append(c.f.locks, values[0])
	}
	switch {
	case strings.Contains(q, `SELECT count(*) FROM "users"`):
		c.f.queries = append(c.f.queries, q)
		c.f.countSQL = q
		var count int64
		for _, u := range c.current().users {
			if u.Role == entity.RoleAdmin && !u.Disabled && u.OffboardedAt == nil {
				count++
			}
		}
		return effectiveSQLRows([]struct{ Count int64 }{{count}})
	case strings.Contains(q, "FROM team_memberships m"):
		c.f.queries = append(c.f.queries, q)
		if c.f.ownedTeam && strings.HasPrefix(q, "SELECT m.team_id") {
			return effectiveSQLRows([]struct{ TeamID string }{{"tea_owned"}})
		}
		if strings.HasPrefix(q, "SELECT m.team_id") {
			return effectiveSQLRows([]struct{ TeamID string }{})
		}
		return effectiveSQLRows([]struct{ Count int64 }{{0}})
	case strings.Contains(q, "FROM project_managers m"):
		c.f.queries = append(c.f.queries, q)
		return effectiveSQLRows([]struct{ ProjectID string }{})
	case strings.Contains(q, `FROM "team_model_requests"`):
		c.f.queries = append(c.f.queries, q)
		return effectiveSQLRows([]entity.TeamModelRequest{})
	case strings.Contains(q, `FROM "personal_model_requests"`):
		c.f.queries = append(c.f.queries, q)
		return effectiveSQLRows([]entity.PersonalModelRequest{})
	case strings.Contains(q, `FROM "team_quota_requests"`):
		c.f.queries = append(c.f.queries, q)
		return effectiveSQLRows([]entity.TeamQuotaRequest{})
	case strings.Contains(q, `FROM "team_memberships"`):
		c.f.queries = append(c.f.queries, q)
		return effectiveSQLRows([]entity.TeamMembership{})
	case strings.Contains(q, `FROM "api_keys"`):
		c.f.queries = append(c.f.queries, q)
		return effectiveSQLRows([]entity.APIKey{})
	default:
		return c.rolesSQLConnection.QueryContext(ctx, q, args)
	}
}
func (c *stateSQLConnection) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if strings.HasPrefix(q, `DELETE FROM "sessions"`) || strings.HasPrefix(q, `DELETE FROM "mfa_challenges"`) {
		c.f.writes = append(c.f.writes, q)
		return driver.RowsAffected(1), nil
	}
	result, err := c.rolesSQLConnection.ExecContext(ctx, q, args)
	if err == nil && strings.HasPrefix(q, `UPDATE "users"`) && strings.Contains(q, `"offboarded_at"`) {
		values := rolesSQLStrings(args)
		id := values[len(values)-1]
		u := c.current().users[id]
		u.OffboardedAt = nil
		c.current().users[id] = u
	}
	return result, err
}
func memberStateSQLService(t *testing.T) (*Service, *stateSQLFixture) {
	t.Helper()
	_, base := roleSQLService(t, 0)
	f := &stateSQLFixture{rolesSQLFixture: base}
	pool := sql.OpenDB(stateSQLConnector{f})
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return &Service{db: db}, f
}
func TestMemberStateCanonicalLastAdminAndSortedLocks(t *testing.T) {
	for _, mode := range []string{"only_admin", "alias_admin", "offboarded_admin", "disabled_admin", "second_admin"} {
		t.Run(mode, func(t *testing.T) {
			s, f := memberStateSQLService(t)
			other := f.data.users["usr_target"]
			other.Role = entity.RoleAdmin
			switch mode {
			case "only_admin":
				other.Role = entity.RoleMember
			case "alias_admin":
				other.Role = "ADMIN"
			case "offboarded_admin":
				other.Disabled = true
				now := other.CreatedAt
				other.OffboardedAt = &now
			case "disabled_admin":
				other.Disabled = true
			}
			f.data.users[other.ID] = other
			review := memberStateRecord(f.data.users["usr_admin"], f.data.users["usr_admin"], true)
			next := entity.RoleMember
			mutation, err := s.mutateMemberState(context.Background(), "usr_admin", "usr_admin", review.ETag, MemberStateInput{Role: &next, Reason: "Reviewed demotion"}, true)
			if !strings.Contains(f.countSQL, "offboarded_at IS NULL") || !strings.Contains(f.countSQL, `"role" = $1`) {
				t.Fatal("noncanonical last-admin count", f.countSQL)
			}
			if mode != "second_admin" {
				if err != catalogConflict || mutation.changed || len(f.writes) != 0 {
					t.Fatal("last admin changed", err)
				}
				return
			}
			if err != nil || !mutation.changed || !mutation.invalidate || f.data.users["usr_admin"].Role != entity.RoleMember {
				t.Fatal("legitimate demotion", err)
			}
			if result, err := s.confirmMemberState(context.Background(), "usr_admin", "usr_admin", MemberStateInput{Role: &next, Reason: "Reviewed demotion"}); result != nil || err != apperrors.ErrForbidden {
				t.Fatal("self demotion invented confirmation", err)
			}
		})
	}
	s, f := memberStateSQLService(t)
	next := entity.RoleAdmin
	review := memberStateRecord(f.data.users["usr_admin"], f.data.users["usr_target"], true)
	if _, err := s.mutateMemberState(context.Background(), "usr_admin", "usr_target", review.ETag, MemberStateInput{Role: &next, Reason: "Promotion"}, true); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.locks, []string{"usr_admin", "usr_target"}) || !strings.Contains(f.queries[0], "governance_settings") {
		t.Fatal("governance then sorted exact locks", f.locks)
	}
}
func TestMemberStateDisableContinuityAndAtomicRollback(t *testing.T) {
	for _, mode := range []string{"disable", "continuity", "audit_failure", "reactivate"} {
		t.Run(mode, func(t *testing.T) {
			s, f := memberStateSQLService(t)
			target := f.data.users["usr_target"]
			if mode == "reactivate" {
				target.Disabled = true
				now := target.CreatedAt
				target.OffboardedAt = &now
				f.data.users[target.ID] = target
			}
			before := f.data.clone()
			f.ownedTeam = mode == "continuity"
			f.failAudit = mode == "audit_failure"
			desired := mode != "reactivate"
			input := MemberStateInput{Disabled: &desired, Reason: "Reviewed account access"}
			review := memberStateRecord(f.data.users["usr_admin"], target, true)
			mutation, err := s.mutateMemberState(context.Background(), "usr_admin", target.ID, review.ETag, input, true)
			if mode == "continuity" || mode == "audit_failure" {
				expected := catalogConflict
				if mode == "audit_failure" {
					expected = memberStateUnavailable
				}
				if err != expected || !reflect.DeepEqual(before, f.data) {
					t.Fatal("non-atomic rejected lifecycle", err)
				}
				return
			}
			persisted := f.data.users[target.ID]
			if err != nil || !mutation.changed || persisted.Disabled != desired || persisted.OffboardedAt != nil || persisted.MemberRoleRevision == target.MemberRoleRevision || len(f.data.audits) != 1 {
				t.Fatal("lifecycle persistence", err)
			}
			if mode == "disable" {
				if !mutation.invalidate {
					t.Fatal("reduction not fenced")
				}
				joined := strings.Join(f.writes, "\n")
				if !strings.Contains(joined, `DELETE FROM "sessions"`) || !strings.Contains(joined, `DELETE FROM "mfa_challenges"`) {
					t.Fatal("credential cleanup missing")
				}
				if result, err := s.confirmMemberState(context.Background(), target.ID, target.ID, input); err != apperrors.ErrUnauthorized || result != nil {
					t.Fatal("disabled current actor confirmed", err)
				}
			} else {
				if mutation.invalidate || len(f.writes) != 3 {
					t.Fatal("reactivation restored credentials or custom roles", f.writes)
				}
			}
			rt := &gatewayRuntime{done: make(chan struct{})}
			rt.auth.Store(&runtimeAuthorization{ValidUntil: time.Now().Add(time.Minute), UserProofs: map[string]runtimeUserProof{target.ID: {CreatedAt: target.CreatedAt, Enabled: !desired}}})
			s.runtime = rt
			result, err := s.confirmMemberState(context.Background(), "usr_admin", target.ID, input)
			if err != nil || result == nil || !result.AccountAccessRuntimeApplied || result.Effect != "current_account_access" {
				t.Fatal("fresh exact lifecycle confirmation", err)
			}
		})
	}
}
func TestMemberStatePostcommitPermissionLossAndLegacyReason(t *testing.T) {
	s, f := memberStateSQLService(t)
	role := entity.RoleAdmin
	review := memberStateRecord(f.data.users["usr_admin"], f.data.users["usr_target"], true)
	input := MemberStateInput{Role: &role, Reason: "Reviewed promotion"}
	if _, err := s.mutateMemberState(context.Background(), "usr_admin", "usr_target", review.ETag, input, true); err != nil {
		t.Fatal(err)
	}
	f.deny["members.write"] = true
	if result, err := s.confirmMemberState(context.Background(), "usr_admin", "usr_target", input); result != nil || !errors.Is(err, apperrors.ErrForbidden) {
		t.Fatal("lost permission returned success", err)
	}
	s, f = memberStateSQLService(t)
	if _, err := s.mutateMemberState(context.Background(), "usr_admin", "usr_target", "", MemberStateInput{Role: &role}, false); err != nil {
		t.Fatal(err)
	}
	if len(f.data.audits) != 1 || f.data.audits[0].Action != "member.update" || f.data.audits[0].DetailsJSON != nil {
		t.Fatal("legacy fabricated typed reason")
	}
}
