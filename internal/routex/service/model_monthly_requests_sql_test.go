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
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type modelMonthlySQLFixture struct {
	roles       *rolesSQLFixture
	state       *providerBindingsSQLState
	facts       []modelMonthlyFact
	factQueries int
	fail        bool
}

type modelMonthlyConnector struct{ f *modelMonthlySQLFixture }

func (c modelMonthlyConnector) Connect(context.Context) (driver.Conn, error) {
	return &modelMonthlySQLConnection{providerBindingsConnection: &providerBindingsConnection{rolesSQLConnection: &rolesSQLConnection{f: c.f.roles}, state: c.f.state}, f: c.f}, nil
}
func (c modelMonthlyConnector) Driver() driver.Driver { return modelMonthlyDriver(c) }

type modelMonthlyDriver modelMonthlyConnector

func (c modelMonthlyDriver) Open(string) (driver.Conn, error) {
	return modelMonthlyConnector(c).Connect(context.Background())
}

type modelMonthlySQLConnection struct {
	*providerBindingsConnection
	f *modelMonthlySQLFixture
}

func (c *modelMonthlySQLConnection) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(query, `FROM "call_records"`) {
		c.f.factQueries++
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if c.f.fail {
			return nil, errors.New("controlled monthly facts outage")
		}
		if !strings.Contains(query, "LIMIT") || !strings.Contains(query, "model_id IN") || !strings.Contains(query, "started_at >=") || !strings.Contains(query, "started_at <") {
			return nil, errors.New("unbounded or unscoped monthly facts read")
		}
		return effectiveSQLRows(c.f.facts)
	}
	return c.providerBindingsConnection.QueryContext(ctx, query, args)
}

func (c *modelMonthlySQLConnection) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if strings.Contains(query, `UPDATE "models"`) && strings.Contains(query, `"config_updated_at"`) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return driver.RowsAffected(0), nil
	}
	return c.providerBindingsConnection.ExecContext(ctx, query, args)
}

func TestModelConfigurationStampZeroChangedRowsRequireExactRecordedTarget(t *testing.T) {
	for _, scenario := range []string{"same_microsecond", "missing", "collation_alias", "different_recorded_time"} {
		t.Run(scenario, func(t *testing.T) {
			now := time.Date(2026, 10, 1, 0, 0, 0, 123456000, time.UTC)
			model := entity.Model{ID: "mdl_target", ConfigUpdatedAt: &now}
			f := &modelMonthlySQLFixture{roles: &rolesSQLFixture{data: rolesSQLData{}}, state: &providerBindingsSQLState{models: []entity.Model{model}}}
			switch scenario {
			case "missing":
				f.state.models = nil
			case "collation_alias":
				f.state.models[0].ID = "mdl_TARGET"
			case "different_recorded_time":
				other := now.Add(-time.Second)
				f.state.models[0].ConfigUpdatedAt = &other
			}
			pool := sql.OpenDB(modelMonthlyConnector{f})
			t.Cleanup(func() { _ = pool.Close() })
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil {
				t.Fatal(err)
			}
			err = db.Transaction(func(tx *gorm.DB) error { return stampModelConfiguration(tx, "mdl_target", now) })
			if (err == nil) != (scenario == "same_microsecond") {
				t.Fatal("zero-row write falsely recorded a missing, aliased or different target", err)
			}
		})
	}
}

func TestModelMonthlyRequestsSQLIndependentAuthorityAndFixedBatch(t *testing.T) {
	for _, scenario := range []string{"one", "twenty", "calls_denied", "models_denied", "actor_alias", "missing_model", "collation_superset", "overflow", "outage", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			actor, _, _ := connectionMetadataTestRows()
			actor.Role = entity.RoleMember
			f := &modelMonthlySQLFixture{roles: &rolesSQLFixture{data: rolesSQLData{users: map[string]entity.User{actor.ID: actor}}, deny: map[string]bool{}}, state: &providerBindingsSQLState{}}
			ids := []string{"mdl_000"}
			if scenario == "twenty" {
				for i := 1; i < 20; i++ {
					ids = append(ids, fmt.Sprintf("mdl_%03d", i))
				}
			}
			for _, id := range ids {
				f.state.models = append(f.state.models, entity.Model{ID: id})
			}
			now := time.Now().UTC()
			month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
			f.facts = []modelMonthlyFact{{RequestID: "req_001", ModelID: ids[0], StartedAt: month}}
			ctx := context.Background()
			wantFailure := scenario != "one" && scenario != "twenty" && scenario != "collation_superset"
			switch scenario {
			case "calls_denied":
				f.roles.deny["calls.read_all"] = true
			case "models_denied":
				f.roles.deny["models.read_all"] = true
			case "actor_alias":
				f.roles.actorAlias = true
			case "missing_model":
				f.state.models = nil
			case "collation_superset":
				f.facts = append(f.facts, modelMonthlyFact{"req_other", strings.ToUpper(ids[0]), month})
			case "overflow":
				f.facts = make([]modelMonthlyFact, usageRowLimit+1)
			case "outage":
				f.fail = true
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			pool := sql.OpenDB(modelMonthlyConnector{f})
			t.Cleanup(func() { _ = pool.Close() })
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil {
				t.Fatal(err)
			}
			got, err := (&Service{db: db}).AdminModelMonthlyRequests(ctx, actor.ID, ids)
			if wantFailure {
				if scenario == "overflow" {
					var status interface{ StatusCode() int }
					if !errors.As(err, &status) || status.StatusCode() != 422 {
						t.Fatal("complete-query overflow lost explicit status", err)
					}
				}
				if err == nil || got != nil {
					t.Fatal("failure manufactured a complete report", got, err)
				}
				if (strings.Contains(scenario, "denied") || scenario == "actor_alias" || scenario == "missing_model" || scenario == "cancelled") && f.factQueries != 0 {
					t.Fatal("facts read before complete authority and exact targets")
				}
				return
			}
			if err != nil || got == nil || len(got.Items) != len(ids) || got.Items[0].Requests != "1" || got.Source != "persisted_call_records" || !got.MayLag || got.Timezone != "UTC" || !got.AsOf.Equal(got.PeriodTo) {
				t.Fatal("incoherent complete monthly report", got, err)
			}
			if len(f.state.queries) != 1 || f.factQueries != 1 || len(f.roles.writes) != 0 || len(f.roles.transactions) != 1 || !f.roles.transactions[0].ReadOnly || f.roles.transactions[0].Isolation != driver.IsolationLevel(sql.LevelRepeatableRead) || !f.state.deadline {
				t.Fatal("batch query/authority/read-only/deadline contract changed")
			}
			for _, item := range got.Items[1:] {
				if item.Requests != "0" {
					t.Fatal("known empty Model lost zero")
				}
			}
		})
	}
}
