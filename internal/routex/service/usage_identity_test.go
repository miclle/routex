package service

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

func TestUsageHistoricalSelectorsUseExactBoundIdentities(t *testing.T) {
	for _, dialect := range []gorm.Dialector{projectQuotaScopeDialector{}, personalLifecycleMySQLDialector{}} {
		for _, value := range []string{"legacy_exact", "LEGACY_EXACT"} {
			t.Run(dialect.Name()+"/"+value, func(t *testing.T) {
				db, err := gorm.Open(dialect, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
				if err != nil {
					t.Fatal(err)
				}
				filter := UsageFilter{ModelID: value, KeyID: value, UserID: value, ProjectID: value, ProviderID: value, ProviderModelID: value, ConnectionID: value, Status: "success", Protocol: entity.ProtocolOpenAIChat}
				query := usageQueryFilters(db.Model(&entity.CallRecord{}), filter)
				query.Statement.Clauses["WHERE"].Build(query.Statement)
				sql := query.Statement.SQL.String()
				columns := []string{"model_id", "key_id", "user_id", "project_id", "provider_id", "provider_model_id", "connection_id"}
				for _, column := range columns {
					expected := `"` + column + `" = ?`
					if dialect.Name() == "mysql" {
						expected = `CAST("` + column + `" AS BINARY) = CAST(? AS BINARY)`
					}
					if !strings.Contains(sql, expected) {
						t.Fatalf("historical identity %s can alias: %s", column, sql)
					}
				}
				if !reflect.DeepEqual(query.Statement.Vars, []any{value, value, value, value, value, value, value, "success", entity.ProtocolOpenAIChat}) || !strings.HasSuffix(sql, " AND status = ? AND protocol = ?") {
					t.Fatal("selectors normalized or canonical enums changed", sql, query.Statement.Vars)
				}
			})
		}
	}
}

func TestUsageScopeFactsKeepExactPersonalProjectAndTeamAttribution(t *testing.T) {
	for _, dialect := range []gorm.Dialector{projectQuotaScopeDialector{}, personalLifecycleMySQLDialector{}} {
		for _, test := range []struct {
			scope   string
			columns []string
			values  []any
		}{
			{"personal", []string{"user_id", "project_id", "team_id"}, []any{"usr_exact", "", ""}},
			{"project", []string{"project_id"}, []any{"prj_exact"}},
			{"team", []string{"team_id", "project_id", "key_id"}, []any{"prj_exact", "", ""}},
			{"admin", nil, nil},
		} {
			t.Run(dialect.Name()+"/"+test.scope, func(t *testing.T) {
				db, err := gorm.Open(dialect, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
				if err != nil {
					t.Fatal(err)
				}
				query := usageScopeFacts(db.Model(&entity.CallRecord{}), "usr_exact", test.scope, "prj_exact")
				if len(test.columns) > 0 {
					query.Statement.Clauses["WHERE"].Build(query.Statement)
				}
				sql := query.Statement.SQL.String()
				if !slices.Equal(query.Statement.Vars, test.values) {
					t.Fatal("scope attribution changed", sql, query.Statement.Vars)
				}
				for _, column := range test.columns {
					expected := `"` + column + `" = ?`
					if dialect.Name() == "mysql" {
						expected = `CAST("` + column + `" AS BINARY) = CAST(? AS BINARY)`
					}
					if !strings.Contains(sql, expected) {
						t.Fatal("scope can borrow collated attribution", sql)
					}
				}
			})
		}
	}
}

func TestUsagePersonalRequiresCurrentExactEnabledActorWithoutPermissionExpansion(t *testing.T) {
	for _, test := range []struct {
		actor string
		found bool
		want  error
	}{
		{"usr_exact", true, nil}, {"USR_EXACT", true, apperrors.ErrUnauthorized}, {"usr_exact", false, apperrors.ErrUnauthorized},
	} {
		t.Run(test.actor+"/"+map[bool]string{true: "present", false: "unavailable"}[test.found], func(t *testing.T) {
			db, err := gorm.Open(personalLifecycleMySQLDialector{}, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
			if err != nil {
				t.Fatal(err)
			}
			reads := 0
			if err := db.Callback().Query().Register("test:usage-actor", func(query *gorm.DB) {
				reads++
				query.Statement.Clauses["WHERE"].Build(query.Statement)
				if !strings.Contains(query.Statement.SQL.String(), `CAST("id" AS BINARY) = CAST(? AS BINARY)`) || !strings.Contains(query.Statement.SQL.String(), "disabled = ? AND offboarded_at IS NULL") {
					t.Fatal("actor lookup lost exact/current guard", query.Statement.SQL.String())
				}
				actor, ok := query.Statement.Dest.(*entity.User)
				if !ok {
					t.Fatal("personal read fetched permission/directory instead of own actor")
				}
				if test.found {
					*actor = entity.User{ID: "usr_exact", Role: entity.RoleMember}
					query.RowsAffected = 1
				} else {
					_ = query.AddError(gorm.ErrRecordNotFound)
				}
			}); err != nil {
				t.Fatal(err)
			}
			if err := authorizeUsage(db, test.actor, "personal", ""); err != test.want || reads != 1 {
				t.Fatal("personal authority borrowed alias or permissions", err, reads)
			}
		})
	}
}
