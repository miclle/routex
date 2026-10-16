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
	"github.com/miclle/routex/pkg/limits"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type insightsQuotaBatchSQLFixture struct {
	actor       entity.User
	permissions map[string]bool
	teams       []entity.Team
	policies    []entity.ResourceLimit
	queries     []string
	options     driver.TxOptions
	failTable   string
	writes      int
}

type insightsQuotaBatchConnector struct{ fixture *insightsQuotaBatchSQLFixture }

func (c insightsQuotaBatchConnector) Connect(context.Context) (driver.Conn, error) {
	return insightsQuotaBatchConnection(c), nil
}
func (c insightsQuotaBatchConnector) Driver() driver.Driver {
	return insightsQuotaBatchDriver(c)
}

type insightsQuotaBatchDriver insightsQuotaBatchConnector

func (c insightsQuotaBatchDriver) Open(string) (driver.Conn, error) {
	return insightsQuotaBatchConnection(c), nil
}

type insightsQuotaBatchConnection insightsQuotaBatchConnector

func (insightsQuotaBatchConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepared statement")
}
func (insightsQuotaBatchConnection) Close() error { return nil }
func (c insightsQuotaBatchConnection) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c insightsQuotaBatchConnection) BeginTx(_ context.Context, options driver.TxOptions) (driver.Tx, error) {
	c.fixture.options = options
	return adminOverviewTransaction{}, nil
}
func (c insightsQuotaBatchConnection) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	c.fixture.writes++
	return nil, errors.New("quota projection attempted a write")
}
func (c insightsQuotaBatchConnection) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f := c.fixture
	f.queries = append(f.queries, query)
	if f.failTable != "" && strings.Contains(query, f.failTable) {
		return nil, errors.New("controlled quota read failure")
	}
	switch {
	case strings.Contains(query, `FROM "users"`):
		return effectiveSQLRows([]entity.User{f.actor})
	case strings.Contains(query, "role_permissions AS permission"):
		for _, arg := range args {
			if permission, ok := arg.Value.(string); ok && f.permissions[permission] {
				return effectiveSQLRows([]exactPermissionIdentity{{RoleID: "rol_member", PermissionRoleID: "rol_member", Permission: permission}})
			}
		}
		return effectiveSQLRows([]exactPermissionIdentity{})
	case strings.Contains(query, `FROM "resource_limits"`):
		return effectiveSQLRows(f.policies)
	case strings.Contains(query, `FROM "teams"`):
		return effectiveSQLRows(f.teams)
	}
	return nil, fmt.Errorf("unexpected quota projection query: %s", query)
}

func insightsQuotaBatchSQLDB(t *testing.T, fixture *insightsQuotaBatchSQLFixture) *gorm.DB {
	t.Helper()
	pool := sql.OpenDB(insightsQuotaBatchConnector{fixture: fixture})
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestInsightsQuotaTeamsKeepConstantReadBudgetAndExistingAuthority(t *testing.T) {
	for _, permission := range []string{"membership", "teams.read_all", "teams.tokens.write", "teams.money.write", "teams.rates.write"} {
		for _, count := range []int{0, 1, 50} {
			t.Run(fmt.Sprintf("%s/%d", permission, count), func(t *testing.T) {
				birth := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
				fixture := &insightsQuotaBatchSQLFixture{actor: entity.User{ID: "usr_reader", Role: entity.RoleMember, CreatedAt: birth}, permissions: map[string]bool{permission: true}}
				for i := range count {
					id := fmt.Sprintf("tem_batch_%02d", i)
					fixture.teams = append(fixture.teams, entity.Team{ID: id, Name: "Recorded Team", Status: entity.ResourceActive, CreatedAt: birth})
					fixture.policies = append(fixture.policies, entity.ResourceLimit{ScopeKind: "team", ScopeID: id, TokensMonth: limitNumber(int64(i))})
				}
				db := insightsQuotaBatchSQLDB(t, fixture)
				err := db.Transaction(func(tx *gorm.DB) error {
					targets, more, err := insightsQuotaTeams(tx, "usr_reader", "", 50)
					if err != nil {
						return err
					}
					if len(targets) != count || more {
						t.Fatal("bounded Team page changed", len(targets), more)
					}
					for i, target := range targets {
						if target.policy.TokensMonth == nil || *target.policy.TokensMonth != int64(i) {
							t.Fatal("batch policy was attached to another Team")
						}
					}
					return nil
				}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
				if err != nil {
					t.Fatal(err)
				}
				budget := 6
				if count != 0 {
					budget++
				}
				if len(fixture.queries) != budget || fixture.writes != 0 || !fixture.options.ReadOnly || fixture.options.Isolation != driver.IsolationLevel(sql.LevelRepeatableRead) {
					t.Fatal("Team page performed per-row reads or left its read-only snapshot", len(fixture.queries), budget, fixture.options)
				}
				teamSQL := fixture.queries[5]
				if strings.Contains(teamSQL, "EXISTS (") != (permission == "membership") {
					t.Fatal("existing Team authority changed its selection scope", permission, teamSQL)
				}
			})
		}
	}
}

func TestInsightsQuotaPolicyHydrationUsesOneReadIncludingKeyLookahead(t *testing.T) {
	for _, count := range []int{1, 20, 51} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			fixture := &insightsQuotaBatchSQLFixture{}
			targets := make([]insightsQuotaTarget, count)
			for i := range targets {
				id := fmt.Sprintf("key_batch_%02d", i)
				targets[i] = insightsQuotaTarget{kind: "key", row: InsightsQuotaRow{ID: id}}
				fixture.policies = append(fixture.policies, entity.ResourceLimit{ScopeKind: "key", ScopeID: id, TokensMonthBehavior: "alert_only", TokensMonth: limitNumber(int64(i))})
			}
			if err := hydrateInsightsQuotaPolicies(insightsQuotaBatchSQLDB(t, fixture), targets); err != nil {
				t.Fatal(err)
			}
			if len(fixture.queries) != 1 || fixture.writes != 0 {
				t.Fatal("root policies were queried individually", len(fixture.queries))
			}
			for i, target := range targets {
				if target.policy.TokensMonth == nil || *target.policy.TokensMonth != int64(i) || target.policy.TokensMonthBehavior != "alert_only" {
					t.Fatal("Key sentinel lost its original exact policy")
				}
			}
		})
	}
	t.Run("corrupt_lookahead_rejects_the_whole_page", func(t *testing.T) {
		fixture := &insightsQuotaBatchSQLFixture{}
		targets := make([]insightsQuotaTarget, 51)
		for i := range targets {
			id := fmt.Sprintf("key_batch_%02d", i)
			targets[i] = insightsQuotaTarget{kind: "key", row: InsightsQuotaRow{ID: id}}
			fixture.policies = append(fixture.policies, entity.ResourceLimit{ScopeKind: "key", ScopeID: id, TokensMonth: limitNumber(int64(i))})
		}
		fixture.policies[50].TokensMonthBehavior = "unknown"
		if err := hydrateInsightsQuotaPolicies(insightsQuotaBatchSQLDB(t, fixture), targets); err == nil || len(fixture.queries) != 1 {
			t.Fatal("corrupt lookahead was omitted or retried individually", err)
		}
		for _, target := range targets {
			if target.policy.TokensMonth != nil {
				t.Fatal("invalid lookahead published a partially hydrated page")
			}
		}
	})
}

func TestInsightsQuotaBatchSQLFailuresDoNotPublishPartialPolicies(t *testing.T) {
	for _, failure := range []string{"query", "corrupt_row", "cancelled"} {
		t.Run(failure, func(t *testing.T) {
			fixture := &insightsQuotaBatchSQLFixture{}
			if failure == "query" {
				fixture.failTable = "resource_limits"
			} else {
				fixture.policies = []entity.ResourceLimit{{ScopeKind: "key", ScopeID: "KEY_OTHER"}}
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if failure == "cancelled" {
				cancel()
			}
			targets := []insightsQuotaTarget{{kind: "key", row: InsightsQuotaRow{ID: "key_one"}, policy: limits.Policy{TokensMonth: limitNumber(7)}}}
			err := hydrateInsightsQuotaPolicies(insightsQuotaBatchSQLDB(t, fixture).WithContext(ctx), targets)
			if err == nil || targets[0].policy.TokensMonth == nil || *targets[0].policy.TokensMonth != 7 || fixture.writes != 0 {
				t.Fatal("failed batch replaced original policy or wrote state", err)
			}
			if failure == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation became a successful absent-policy default", err)
			}
		})
	}
}
