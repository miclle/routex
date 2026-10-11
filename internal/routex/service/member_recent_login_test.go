package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestMemberRecentLoginMonotonePrecisionAndHistoricalUnknown(t *testing.T) {
	now := time.Date(2026, 10, 5, 1, 2, 3, 123456789, time.FixedZone("test", 3600))
	want := now.UTC().Truncate(time.Microsecond)
	got, err := nextRecentLogin(nil, now)
	if err != nil || got != want {
		t.Fatal(got, err)
	}
	later := want.Add(time.Hour)
	got, err = nextRecentLogin(&later, now)
	if err != nil || !got.Equal(later) {
		t.Fatal(got, err)
	}
	got, err = nextRecentLogin(&want, now)
	if err != nil || !got.Equal(want) {
		t.Fatal("invented epsilon", got, err)
	}
	if p, err := memberRecentLoginProjection(nil); p != nil || err != nil {
		t.Fatal(p, err)
	}
	for _, bad := range []time.Time{{}, want.Add(time.Nanosecond)} {
		if _, err := memberRecentLoginProjection(&bad); err != apperrors.ErrInternal {
			t.Fatal(bad, err)
		}
	}
	raw, err := jsonUserWithoutLogin(want)
	if err != nil || strings.Contains(raw, "LastLogin") || strings.Contains(raw, "last_login") {
		t.Fatal(raw, err)
	}
}

// Keep the business entity private even if an accidental generic JSON encode occurs.
func jsonUserWithoutLogin(stamp time.Time) (string, error) {
	raw, err := json.Marshal(entity.User{LastLoginAt: &stamp})
	return string(raw), err
}

type recentLoginSQLFixture struct {
	user                   entity.User
	permission             bool
	rows                   int64
	failWrite, failSession bool
	queries, execs         []string
	opts                   driver.TxOptions
	committed, rolledBack  bool
	roleCount              int
	roleAlias              bool
	timestampWrites        int
	mode                   string
	sessionColumns         []string
	sessionValues          []driver.Value
	sessionID              string
	sessionWrites          int
}
type recentLoginConnector struct{ f *recentLoginSQLFixture }

func (c recentLoginConnector) Connect(context.Context) (driver.Conn, error) {
	return recentLoginConnection(c), nil
}
func (c recentLoginConnector) Driver() driver.Driver { return recentLoginDriver(c) }

type recentLoginDriver recentLoginConnector

func (d recentLoginDriver) Open(string) (driver.Conn, error) { return recentLoginConnection(d), nil }

type recentLoginConnection recentLoginConnector

func (recentLoginConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (recentLoginConnection) Close() error { return nil }
func (c recentLoginConnection) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c recentLoginConnection) BeginTx(_ context.Context, o driver.TxOptions) (driver.Tx, error) {
	c.f.opts = o
	return recentLoginTransaction(c), nil
}

type recentLoginTransaction struct{ f *recentLoginSQLFixture }

func (t recentLoginTransaction) Commit() error   { t.f.committed = true; return nil }
func (t recentLoginTransaction) Rollback() error { t.f.rolledBack = true; return nil }
func (f *recentLoginSQLFixture) insertSession(q string, args []driver.NamedValue) error {
	f.sessionWrites++
	if f.failSession {
		return errors.New("controlled Session failure")
	}
	_, tail, ok := strings.Cut(q, " (")
	columns, _, valid := strings.Cut(tail, ") VALUES ")
	f.sessionColumns = strings.Split(columns, ",")
	if !ok || !valid || len(f.sessionColumns) != len(args) {
		return errors.New("unexpected Session insert shape")
	}
	f.sessionValues = make([]driver.Value, len(args))
	for i, column := range f.sessionColumns {
		f.sessionColumns[i] = strings.Trim(column, `"`)
		f.sessionValues[i] = args[i].Value
		if f.sessionColumns[i] == "id" {
			f.sessionID, _ = args[i].Value.(string)
		}
	}
	if f.sessionID == "" {
		return errors.New("missing authored Session identity")
	}
	return nil
}
func (c recentLoginConnection) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	c.f.execs = append(c.f.execs, q)
	if strings.HasPrefix(q, `INSERT INTO "sessions"`) {
		if err := c.f.insertSession(q, args); err != nil {
			return nil, err
		}
		return driver.RowsAffected(1), nil
	}
	c.f.timestampWrites++
	if !strings.Contains(q, `UPDATE "users" SET "last_login_at"=`) || strings.Contains(q, "updated_at") || strings.Contains(q, "personal_grant_revision") {
		return nil, errors.New("unexpected timestamp mutation")
	}
	if c.f.failWrite {
		return nil, errors.New("controlled timestamp failure")
	}
	if c.f.rows == 1 {
		v := args[0].Value.(time.Time)
		c.f.user.LastLoginAt = &v
	}
	return driver.RowsAffected(c.f.rows), nil
}
func (c recentLoginConnection) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	f := c.f
	f.queries = append(f.queries, q)
	switch {
	case strings.Contains(q, `FROM "governance_settings"`):
		return effectiveSQLRows([]entity.GovernanceSetting{{ID: 1, AuthenticationPolicyRevision: strings.Repeat("0", 64)}})
	case strings.HasPrefix(q, `INSERT INTO "sessions"`):
		if err := f.insertSession(q, args); err != nil {
			return nil, err
		}
		return &adminOverviewRows{columns: f.sessionColumns, values: [][]driver.Value{f.sessionValues}}, nil
	case strings.Contains(q, `FROM "sessions"`):
		// Take binds both the explicit ID and populated primary key, then LIMIT 1.
		if q != `SELECT * FROM "sessions" WHERE id = $1 AND "sessions"."id" = $2 LIMIT $3` || f.sessionID == "" || len(args) != 3 {
			return nil, errors.New("unexpected Session readback shape")
		}
		if args[0].Value != f.sessionID || args[1].Value != f.sessionID || args[2].Value != int64(1) {
			return nil, errors.New("unexpected Session readback identity or limit")
		}
		return &adminOverviewRows{columns: f.sessionColumns, values: [][]driver.Value{f.sessionValues}}, nil
	case strings.Contains(q, `FROM "user_mfa"`):
		return effectiveSQLRows([]entity.UserMFA{})
	case strings.Contains(q, "role_permissions AS permission"):
		if f.permission {
			return effectiveSQLRows([]exactPermissionIdentity{{RoleID: "rol_admin", PermissionRoleID: "rol_admin", Permission: "members.read"}})
		}
		return effectiveSQLRows([]exactPermissionIdentity{})
	case strings.Contains(q, `FROM "user_roles"`):
		count := f.roleCount
		if count == 0 {
			count = 1
		}
		rows := make([]entity.UserRole, count)
		for i := range rows {
			role := "role_legacy"
			if count > 1 {
				role = fmt.Sprintf("Legacy_role_%04d", i)
			}
			user := f.user.ID
			if f.roleAlias {
				user = strings.ToUpper(user)
			}
			rows[i] = entity.UserRole{UserID: user, RoleID: role}
		}
		return effectiveSQLRows(rows)
	case strings.Contains(q, `FROM "users"`):
		if f.mode == "detail" && args[0].Value == "usr_reader" {
			return effectiveSQLRows([]entity.User{{ID: "usr_reader", Role: entity.RoleAdmin, CreatedAt: time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)}})
		}
		return effectiveSQLRows([]entity.User{f.user})
	}
	return nil, errors.New("unexpected retained query")
}
func recentLoginSQL(t *testing.T, f *recentLoginSQLFixture) *gorm.DB {
	t.Helper()
	conn := sql.OpenDB(recentLoginConnector{f})
	t.Cleanup(func() { _ = conn.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: conn}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	return db
}
func TestMemberRecentLoginActualPasswordSuccessAndWriteRollback(t *testing.T) {
	password := "Controlled password 123"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"success", "timestamp_failure", "session_failure", "wrong_password"} {
		t.Run(mode, func(t *testing.T) {
			f := &recentLoginSQLFixture{user: entity.User{ID: "usr_subject", Role: entity.RoleMember, Email: "subject@example.test", PasswordHash: string(hash), CreatedAt: time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)}, rows: 1, failWrite: mode == "timestamp_failure", failSession: mode == "session_failure"}
			s := &Service{db: recentLoginSQL(t, f)}
			submitted := password
			if mode == "wrong_password" {
				submitted = "Wrong password 123"
			}
			auth, challenge, err := s.BeginLogin(context.Background(), f.user.Email, submitted)
			if mode == "success" {
				if err != nil || auth == nil || challenge != nil || auth.User.LastLoginAt == nil || !f.committed || f.rolledBack || f.timestampWrites != 1 {
					t.Fatal(auth != nil, err, f.committed, f.rolledBack, len(f.execs), len(f.queries))
				}
			} else {
				if err == nil || auth != nil && auth.User.LastLoginAt != nil || f.committed {
					t.Fatal(mode, err, f.committed, f.rolledBack)
				}
				if mode != "wrong_password" && !f.rolledBack {
					t.Fatal("failed transaction committed")
				}
			}
			wantSessionWrites := 1
			if mode == "wrong_password" {
				wantSessionWrites = 0
			}
			if f.sessionWrites != wantSessionWrites || mode == "timestamp_failure" && f.timestampWrites != 1 {
				t.Fatal("login did not reach controlled failure stage", mode, f.sessionWrites, f.timestampWrites)
			}
			if mode == "session_failure" || mode == "wrong_password" {
				if f.timestampWrites != 0 {
					t.Fatal("rejected login recorded observation")
				}
			}
		})
	}
}
func TestMemberRecentLoginNoOpAndExactChangedRowsReconciliation(t *testing.T) {
	future := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	f := &recentLoginSQLFixture{user: entity.User{ID: "usr_subject", LastLoginAt: &future}, rows: 0}
	db := recentLoginSQL(t, f)
	if err := recordSuccessfulLogin(db, &f.user); err != nil || len(f.execs) != 0 || len(f.queries) != 0 {
		t.Fatal("no-op fabricated write", err, f)
	}
	for _, mode := range []string{"already_same", "different", "missing", "foreign"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Microsecond)
			stored := now
			f := &recentLoginSQLFixture{user: entity.User{ID: "usr_subject", LastLoginAt: &stored}, rows: 0}
			switch mode {
			case "different":
				stored = now.Add(-time.Hour)
			case "missing":
				f.user.LastLoginAt = nil
			case "foreign":
				f.user.ID = "USR_SUBJECT"
			}
			db := recentLoginSQL(t, f)
			locked := entity.User{ID: "usr_subject"}
			err := persistRecentLogin(db, &locked, now)
			if (err == nil) != (mode == "already_same") || len(f.execs) != 1 || len(f.queries) != 1 {
				t.Fatal(mode, err, len(f.execs), len(f.queries))
			}
			if mode == "already_same" && (locked.LastLoginAt == nil || !locked.LastLoginAt.Equal(now)) {
				t.Fatal("exact matched value not reconciled")
			}
			if mode != "already_same" && locked.LastLoginAt != nil {
				t.Fatal("foreign/missing observation borrowed")
			}
		})
	}
}
func TestMemberRecentLoginDetailExactReadOnlyPermissionAndRetainedStatus(t *testing.T) {
	stamp := time.Now().UTC().Truncate(time.Microsecond)
	for _, mode := range []string{"recorded", "historical", "disabled", "offboarded", "alias", "denied", "bad_time"} {
		t.Run(mode, func(t *testing.T) {
			f := &recentLoginSQLFixture{mode: "detail", user: entity.User{ID: "usr_subject", Role: entity.RoleMember, LastLoginAt: &stamp, CreatedAt: stamp}, permission: mode != "denied"}
			if mode == "historical" {
				f.user.LastLoginAt = nil
			}
			if mode == "disabled" {
				f.user.Disabled = true
			}
			if mode == "offboarded" {
				f.user.OffboardedAt = &stamp
			}
			if mode == "alias" {
				f.user.ID = "USR_SUBJECT"
			}
			if mode == "bad_time" {
				bad := time.Time{}
				f.user.LastLoginAt = &bad
			}
			s := &Service{db: recentLoginSQL(t, f)}
			got, err := s.GetMemberDetail(context.Background(), "usr_reader", "usr_subject")
			if mode == "denied" || mode == "alias" || mode == "bad_time" {
				if err == nil || got != nil {
					t.Fatal("read borrowed authority", mode, got, err)
				}
				return
			}
			if err != nil || got == nil || len(f.queries) != 4 || !f.opts.ReadOnly || f.opts.Isolation != driver.IsolationLevel(sql.LevelRepeatableRead) || len(f.execs) != 0 || !reflect.DeepEqual(got.RoleIDs, []string{"role_legacy"}) {
				t.Fatal(got != nil, err, len(f.queries), f.opts, len(f.execs))
			}
			if (got.LastLoginStatus == "historical_unavailable") != (mode == "historical") || (got.LastLoginAt == nil) != (mode == "historical") {
				t.Fatal(got)
			}
			if strings.Contains(strings.Join(f.queries, " "), "password_hash") {
				t.Fatal("detail loaded credential material")
			}
		})
	}
}

func TestMemberRecentLoginDoesNotChangeLabelReviewOrRuntimeSource(t *testing.T) {
	stamp := time.Now().UTC().Truncate(time.Microsecond)
	actor := entity.User{ID: "usr_reader", Role: entity.RoleAdmin}
	subject := entity.User{ID: "usr_subject", Name: "Subject", Role: entity.RoleMember, CreatedAt: stamp, UpdatedAt: stamp}
	before := memberMetadataRecord(actor, subject, true).ETag
	subject.LastLoginAt = &stamp
	if memberMetadataRecord(actor, subject, true).ETag != before {
		t.Fatal("login changed reviewed metadata")
	}
	data := &runtimeData{Users: []entity.User{subject}}
	a, err := runtimeDigest(data)
	if err != nil {
		t.Fatal(err)
	}
	newTime := stamp.Add(time.Hour)
	data.Users[0].LastLoginAt = &newTime
	b, err := runtimeDigest(data)
	if err != nil || a != b {
		t.Fatal("login changed routing digest", err)
	}
}

func TestMemberRecentLoginRetainedRolesCompleteBoundAndOwnership(t *testing.T) {
	for _, count := range []int{101, 10000, 10001} {
		f := &recentLoginSQLFixture{mode: "detail", user: entity.User{ID: "usr_subject", Role: entity.RoleMember, CreatedAt: time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)}, permission: true, roleCount: count}
		s := &Service{db: recentLoginSQL(t, f)}
		got, err := s.GetMemberDetail(context.Background(), "usr_reader", "usr_subject")
		if count <= 10000 {
			if err != nil || got == nil || len(got.RoleIDs) != count || len(f.queries) != 4 {
				t.Fatal(count, err)
			}
		} else if err != memberListRoleOverflow || got != nil {
			t.Fatal("role overflow silently truncated", err)
		}
	}
	f := &recentLoginSQLFixture{mode: "detail", user: entity.User{ID: "usr_subject", Role: entity.RoleMember, CreatedAt: time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)}, permission: true, roleAlias: true}
	s := &Service{db: recentLoginSQL(t, f)}
	if got, err := s.GetMemberDetail(context.Background(), "usr_reader", "usr_subject"); got != nil || err != apperrors.ErrInternal {
		t.Fatal("aliased relationship borrowed roles", err)
	}
	for _, bad := range []string{"", "usr_subject ", strings.Repeat("x", 31), "usr_目标"} {
		empty := &Service{}
		if _, err := empty.GetMemberDetail(context.Background(), bad, "usr_subject"); err != apperrors.ErrUnauthorized {
			t.Fatal(err)
		}
		if _, err := empty.GetMemberDetail(context.Background(), "usr_reader", bad); err != apperrors.ErrBadRequest {
			t.Fatal(err)
		}
	}
}
