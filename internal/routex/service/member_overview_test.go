package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/eventqueue"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
	"gorm.io/gorm/clause"
)

func TestAdminMemberOverviewRejectsUnsafeContextBeforeDatabase(t *testing.T) {
	s := &Service{}
	for _, value := range []string{"", strings.Repeat("x", 31), "usr_target ", "usr_target\n", "usr_target/other", "usr_目标"} {
		if _, err := s.MemberOverview(context.Background(), value, "usr_subject"); err != apperrors.ErrUnauthorized {
			t.Fatal(value, err)
		}
		if _, err := s.MemberOverview(context.Background(), "usr_reader", value); err != apperrors.ErrBadRequest {
			t.Fatal(value, err)
		}
	}
}

func TestAdminMemberOverviewQueriesAreExactMinimalAndIndependent(t *testing.T) {
	for _, dialect := range []gorm.Dialector{projectQuotaScopeDialector{}, personalLifecycleMySQLDialector{}} {
		t.Run(dialect.Name(), func(t *testing.T) {
			db, err := gorm.Open(dialect, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
			if err != nil {
				t.Fatal(err)
			}
			origin := db.Clauses(clause.Locking{Strength: "SHARE"})
			subject := memberOverviewSubjectQuery(origin, "usr_subject")
			if err := subject.Statement.Parse(&entity.User{}); err != nil {
				t.Fatal(err)
			}
			subject.Statement.BuildClauses = []string{"SELECT", "FROM", "WHERE", "FOR"}
			callbacks.BuildQuerySQL(subject)
			cols := subject.Statement.Clauses["SELECT"].Expression.(clause.Select).Columns
			want := []clause.Column{{Name: "id"}, {Name: "disabled"}, {Name: "offboarded_at"}, {Name: "created_at"}}
			if !reflect.DeepEqual(cols, want) || !reflect.DeepEqual(subject.Statement.Vars, []any{"usr_subject"}) {
				t.Fatal(subject.Statement.SQL.String(), cols, subject.Statement.Vars)
			}
			if err := db.Callback().Query().Register("test:member-overview-count", func(query *gorm.DB) {
				query.Statement.BuildClauses = []string{"SELECT", "FROM", "WHERE", "FOR"}
				callbacks.BuildQuerySQL(query)
			}); err != nil {
				t.Fatal(err)
			}
			count := memberOverviewKeyCountQuery(origin, "usr_subject").Count(new(int64))
			sql := count.Statement.SQL.String()
			if !strings.Contains(sql, `FROM "api_keys"`) || !strings.Contains(sql, "count(*)") || !reflect.DeepEqual(count.Statement.Vars, []any{"usr_subject"}) {
				t.Fatal(sql, count.Statement.Vars)
			}
			for _, forbidden := range []string{"status", "project", "token_hash", "created_at", "secret", "roles", "LIMIT"} {
				if strings.Contains(sql, forbidden) {
					t.Fatal("count leaked or filtered retained keys", sql)
				}
			}
			if dialect.Name() == "mysql" && strings.Count(sql, "AS BINARY") != 2 {
				t.Fatal("count lost exact ownership", sql)
			}
			if origin.Statement.Model != nil || origin.Statement.Clauses["WHERE"].Expression != nil {
				t.Fatal("query reused initialized statement", origin.Statement)
			}
		})
	}
}

func TestAdminMemberOverviewPublicationCapturesRetainedUserLifecycle(t *testing.T) {
	created := time.Now().UTC().Add(-time.Hour)
	offboarded := created.Add(time.Minute)
	data := &runtimeData{Users: []entity.User{{ID: "usr_subject", CreatedAt: created}, {ID: "usr_disabled", CreatedAt: created, Disabled: true}, {ID: "usr_offboarded", CreatedAt: created, OffboardedAt: &offboarded}}}
	auth := buildRuntimeAuthorization(data, time.Now().Add(time.Minute))
	if len(auth.UserProofs) != 3 || !auth.UserProofs["usr_subject"].Enabled || auth.UserProofs["usr_disabled"].Enabled || auth.UserProofs["usr_offboarded"].Enabled || !auth.UserProofs["usr_subject"].CreatedAt.Equal(created) {
		t.Fatal(auth.UserProofs)
	}
	data.Users[0].Disabled = true
	data.Users[0].CreatedAt = time.Now()
	if !auth.UserProofs["usr_subject"].Enabled || !auth.UserProofs["usr_subject"].CreatedAt.Equal(created) {
		t.Fatal("publication borrows mutable rows")
	}
}

func TestAdminMemberOverviewAppliedRequiresPublishedSubjectNotReader(t *testing.T) {
	for _, name := range []string{"current", "missing", "case_alias", "creation_alias", "subject_creation_alias", "quota_creation_alias", "target_alias", "captured_active_published_disabled", "raw_reenabled_published_disabled", "disabled", "offboarded", "expired", "generation", "tombstone", "policy", "calendar", "currency", "reader_tombstone"} {
		t.Run(name, func(t *testing.T) {
			s, auth, targets, setting := memberOverviewProofFixture(t)
			subject := entity.User{ID: targets[0].id, CreatedAt: targets[0].created}
			auth.UserProofs = map[string]runtimeUserProof{subject.ID: {CreatedAt: subject.CreatedAt, Enabled: true}}
			switch name {
			case "missing":
				delete(auth.UserProofs, subject.ID)
			case "case_alias":
				auth.UserProofs = map[string]runtimeUserProof{strings.ToUpper(subject.ID): {CreatedAt: subject.CreatedAt, Enabled: true}}
			case "creation_alias":
				auth.UserProofs[subject.ID] = runtimeUserProof{CreatedAt: subject.CreatedAt.Add(time.Microsecond), Enabled: true}
			case "subject_creation_alias":
				subject.CreatedAt = subject.CreatedAt.Add(time.Microsecond)
			case "quota_creation_alias":
				auth.Quota.Created[limitAccount("user", subject.ID)] = subject.CreatedAt.Add(time.Microsecond)
			case "target_alias":
				targets[0].id = strings.ToUpper(subject.ID)
			case "captured_active_published_disabled", "raw_reenabled_published_disabled":
				auth.UserProofs[subject.ID] = runtimeUserProof{CreatedAt: subject.CreatedAt, Enabled: false}
			case "disabled":
				subject.Disabled = true
			case "offboarded":
				now := time.Now()
				subject.OffboardedAt = &now
			case "expired":
				auth.ValidUntil = time.Now().Add(-time.Second)
			case "generation":
				other := *auth
				s.runtime.auth.Store(&other)
			case "tombstone":
				s.runtime.deniedUsers.Store(subject.ID, true)
			case "policy":
				auth.Quota.Revisions[limitAccount("user", subject.ID)] = "new_revision"
			case "calendar":
				setting.ETag = "new_calendar"
			case "currency":
				auth.Quota.Currency = "EUR"
			case "reader_tombstone":
				s.runtime.deniedUsers.Store("usr_reader", true)
			}
			if got := s.memberOverviewSubjectApplied(auth, subject, targets[0], setting, "USD"); got != (name == "current" || name == "reader_tombstone") {
				t.Fatal("reader or stale subject claimed current application", name, got)
			}
		})
	}
}

func TestAdminMemberOverviewKeepsSavedExactFactsDuringSubjectInactivity(t *testing.T) {
	s, auth, targets, setting := memberOverviewProofFixture(t)
	target := targets[0]
	zero, money := int64(0), "1.000000000000000001"
	target.row.TokensMonth = &zero
	target.row.MoneyMonth = &money
	target.row.Currency = "USD"
	target.row.ETag = "saved_revision"
	var err error
	target.policy, err = policyFromRow(target.row)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	account := limitAccount("user", target.id)
	batch := &eventqueue.QuotaUsageBatch{Active: true, AsOf: now, TimeZone: "UTC", CoverageStart: target.created, Accounts: map[string]eventqueue.AccountQuotaUsage{account: {AsOf: now, TimeZone: "UTC", CoverageStart: target.created, Month: eventqueue.QuotaUsage{TokensUsed: 9007199254740993, MoneyUsed: map[string]string{"EUR": "0.000000000000000001"}}, Active: eventqueue.QuotaUsage{TokensHeld: 5, MoneyHeld: map[string]string{"USD": "5.000000000000000002"}}}}}
	value := s.memberOverviewMonthlyAccount(target, []overviewAccountTarget{target}, batch, auth, target.id, setting, "USD")
	subject := entity.User{ID: target.id, CreatedAt: target.created, Disabled: true}
	value.RuntimeApplied = value.RuntimeApplied && s.memberOverviewSubjectApplied(auth, subject, target, setting, "USD")
	record := MemberOverviewRecord{UserID: target.id, ObservedAt: now, PlatformCurrency: "USD", Personal: value, TotalPersonalKeys: "9007199254740993"}
	raw, err := json.Marshal(record)
	if err != nil || value.RuntimeApplied || value.Usage == nil || value.Usage.TokensUsed != "9007199254740993" || value.ActiveReservations == nil || value.ActiveReservations.TokensHeld != "5" || value.TokensMonth == nil || *value.TokensMonth != "0" || value.MoneyMonth == nil || *value.MoneyMonth != money || !strings.Contains(string(raw), `"total_personal_keys":"9007199254740993"`) {
		t.Fatal(string(raw), err)
	}
	for _, forbidden := range []string{"email", "password", "key_id", "token_hash", "reason", "actor_user_id", "team_id"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatal("subject overview leaks unrelated private facts", string(raw))
		}
	}
	for _, selected := range []*eventqueue.QuotaUsageBatch{nil, {Active: false}} {
		value := s.memberOverviewMonthlyAccount(target, []overviewAccountTarget{target}, selected, auth, target.id, setting, "USD")
		if value.Usage != nil || value.ActiveReservations != nil || value.RuntimeApplied || value.MoneyMonth == nil || *value.MoneyMonth != money {
			t.Fatal("outage invented zero or erased saved policy", value)
		}
	}
}

// This SQL driver supplies retained rows only; it exercises the actual service
// transaction, permission gate and fixed query budget without a database server.
type adminOverviewFixture struct {
	queries                                                  []string
	args                                                     [][]driver.NamedValue
	options                                                  driver.TxOptions
	created                                                  time.Time
	deniedActor, deniedPermission, aliasedSubject, failCount bool
}
type adminOverviewConnector struct{ fixture *adminOverviewFixture }

func (c adminOverviewConnector) Connect(context.Context) (driver.Conn, error) {
	return adminOverviewConnection(c), nil
}
func (c adminOverviewConnector) Driver() driver.Driver { return adminOverviewDriver(c) }

type adminOverviewDriver adminOverviewConnector

func (d adminOverviewDriver) Open(string) (driver.Conn, error) {
	return adminOverviewConnection(d), nil
}

type adminOverviewConnection adminOverviewConnector

func (adminOverviewConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepared statement")
}
func (adminOverviewConnection) Close() error { return nil }
func (c adminOverviewConnection) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c adminOverviewConnection) BeginTx(_ context.Context, options driver.TxOptions) (driver.Tx, error) {
	c.fixture.options = options
	return adminOverviewTransaction{}, nil
}

type adminOverviewTransaction struct{}

func (adminOverviewTransaction) Commit() error   { return nil }
func (adminOverviewTransaction) Rollback() error { return nil }

type adminOverviewRows struct {
	columns []string
	values  [][]driver.Value
}

func (r *adminOverviewRows) Columns() []string { return r.columns }
func (*adminOverviewRows) Close() error        { return nil }
func (r *adminOverviewRows) Next(dest []driver.Value) error {
	if len(r.values) == 0 {
		return io.EOF
	}
	copy(dest, r.values[0])
	r.values = r.values[1:]
	return nil
}
func (c adminOverviewConnection) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f := c.fixture
	f.queries = append(f.queries, query)
	f.args = append(f.args, append([]driver.NamedValue(nil), args...))
	switch {
	case strings.Contains(query, `FROM "users"`):
		if args[0].Value == "usr_reader" {
			rows := &adminOverviewRows{columns: []string{"id", "role", "created_at"}}
			if !f.deniedActor {
				rows.values = [][]driver.Value{{"usr_reader", entity.RoleAdmin, f.created}}
			}
			return rows, nil
		}
		id := "usr_subject"
		if f.aliasedSubject {
			id = "USR_SUBJECT"
		}
		return &adminOverviewRows{columns: []string{"id", "disabled", "offboarded_at", "created_at"}, values: [][]driver.Value{{id, true, f.created.Add(time.Minute), f.created}}}, nil
	case strings.Contains(query, "role_permissions AS permission"):
		rows := &adminOverviewRows{columns: []string{"role_id", "permission_role_id", "permission", "assignment_role_id", "assignment_user_id"}}
		if !f.deniedPermission {
			rows.values = [][]driver.Value{{"rol_custom", "rol_custom", "members.read", "rol_custom", "usr_reader"}}
		}
		return rows, nil
	case strings.Contains(query, `FROM "resource_limits"`):
		return &adminOverviewRows{columns: []string{"scope_kind", "scope_id", "e_tag", "tokens_month", "money_month", "currency", "applied_default_e_tag"}, values: [][]driver.Value{{"user", "usr_subject", "saved_default_revision", int64(100), "100.000000000000000001", "USD", "original_default_basis"}}}, nil
	case strings.Contains(query, `FROM "pricing_settings"`):
		return &adminOverviewRows{columns: []string{"platform_currency"}, values: [][]driver.Value{{"USD"}}}, nil
	case strings.Contains(query, `FROM "quota_settings"`):
		return &adminOverviewRows{columns: []string{"time_zone", "e_tag"}, values: [][]driver.Value{{"UTC", "current_calendar"}}}, nil
	case strings.Contains(query, `FROM "api_keys"`):
		if f.failCount {
			return nil, errors.New("private SQL failure")
		}
		return &adminOverviewRows{columns: []string{"count"}, values: [][]driver.Value{{int64(9007199254740993)}}}, nil
	}
	return nil, errors.New("unexpected overview query")
}

func TestAdminMemberOverviewTransactionSeparatesReaderAndRetainedSubject(t *testing.T) {
	for _, name := range []string{"retained_subject", "disabled_actor", "admin_without_permission", "subject_alias", "count_outage"} {
		t.Run(name, func(t *testing.T) {
			f := &adminOverviewFixture{created: time.Now().UTC().Add(-time.Hour), deniedActor: name == "disabled_actor", deniedPermission: name == "admin_without_permission", aliasedSubject: name == "subject_alias", failCount: name == "count_outage"}
			pool := sql.OpenDB(adminOverviewConnector{f})
			t.Cleanup(func() { _ = pool.Close() })
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true})
			if err != nil {
				t.Fatal(err)
			}
			s := &Service{db: db}
			result, err := s.MemberOverview(context.Background(), "usr_reader", "usr_subject")
			wantErr := map[string]error{"disabled_actor": apperrors.ErrUnauthorized, "admin_without_permission": apperrors.ErrForbidden, "subject_alias": apperrors.ErrNotFound, "count_outage": apperrors.ErrInternal}[name]
			wantReads := map[string]int{"retained_subject": 7, "disabled_actor": 1, "admin_without_permission": 2, "subject_alias": 3, "count_outage": 7}[name]
			if err != wantErr || len(f.queries) != wantReads || f.options.Isolation != driver.IsolationLevel(sql.LevelRepeatableRead) || !f.options.ReadOnly {
				t.Fatal(name, err, len(f.queries), f.options)
			}
			if wantErr != nil {
				if result != nil {
					t.Fatal("failure exposed a partial subject", result)
				}
				return
			}
			if result.UserID != "usr_subject" || result.ObservedAt.Location() != time.UTC || result.TotalPersonalKeys != "9007199254740993" || result.Personal.AccountID != limitAccount("user", "usr_subject") || result.Personal.PolicyETag != "saved_default_revision" || result.Personal.TokensMonth == nil || *result.Personal.TokensMonth != "100" || result.Personal.MoneyMonth == nil || *result.Personal.MoneyMonth != "100.000000000000000001" || result.Personal.RuntimeApplied || result.Personal.UsageStatus != "unavailable" || result.Personal.Usage != nil || result.Personal.ActiveReservations != nil {
				t.Fatal(result)
			}
			if !strings.Contains(f.queries[6], "count(*)") || f.args[6][0].Value != "usr_subject" {
				t.Fatal("minimal count used reader or enumerated Keys", f.queries[6], f.args[6])
			}
			for _, query := range f.queries {
				for _, forbidden := range []string{"team_memberships", "project_keys", "default_limit_settings"} {
					if strings.Contains(query, forbidden) {
						t.Fatal("overview expanded to unrelated or mutable default scope", query)
					}
				}
			}
		})
	}
}
