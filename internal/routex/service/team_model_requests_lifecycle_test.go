package service

import (
	"reflect"
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestTeamModelRequestLifecycleKeepsExactLegacyScopeAndOnlyPending(t *testing.T) {
	for _, dialect := range []gorm.Dialector{projectQuotaScopeDialector{}, personalLifecycleMySQLDialector{}} {
		for _, operation := range []struct {
			name string
			call func(*gorm.DB) error
			vars []any
		}{
			{"user", func(tx *gorm.DB) error {
				return CancelTeamModelRequestsForUser(tx, "usr_legacy_admin", "usr_departing", "applicant_unavailable")
			}, []any{"usr_departing", entity.TeamModelRequestPending}},
			{"Team", func(tx *gorm.DB) error {
				return CancelTeamModelRequestsForTeam(tx, "usr_legacy_admin", "tem_legacy", "team_unavailable")
			}, []any{"tem_legacy", entity.TeamModelRequestPending}},
			{"member", func(tx *gorm.DB) error {
				return CancelTeamModelRequestsForMember(tx, "usr_legacy_admin", "tem_legacy", "usr_departing", "membership_unavailable")
			}, []any{"tem_legacy", "usr_departing", entity.TeamModelRequestPending}},
			{"Model", func(tx *gorm.DB) error {
				return CancelTeamModelRequestsForModel(tx, "usr_legacy_admin", "mdl_legacy", "model_unavailable")
			}, []any{"mdl_legacy", entity.TeamModelRequestPending}},
		} {
			t.Run(dialect.Name()+"/"+operation.name, func(t *testing.T) {
				db, err := gorm.Open(dialect, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
				if err != nil {
					t.Fatal(err)
				}
				queried := false
				if err := db.Callback().Query().Register("test:team-model-lifecycle", func(tx *gorm.DB) {
					queried = true
					tx.Statement.Clauses["WHERE"].Build(tx.Statement)
					if !reflect.DeepEqual(tx.Statement.Vars, operation.vars) || strings.Contains(tx.Statement.SQL.String(), " OR ") {
						t.Fatal("lifecycle broadened identity or affected terminal shared grants", tx.Statement.SQL.String(), tx.Statement.Vars)
					}
					if dialect.Name() == "mysql" && strings.Count(tx.Statement.SQL.String(), "AS BINARY") != len(operation.vars)*2 {
						t.Fatal("lifecycle lost exact MySQL context", tx.Statement.SQL.String())
					}
				}); err != nil {
					t.Fatal(err)
				}
				if err := operation.call(db); err != nil || !queried {
					t.Fatal("legacy lifecycle rejected before pending query", queried, err)
				}
			})
		}
	}
}
