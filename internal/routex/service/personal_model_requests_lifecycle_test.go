package service

import (
	"reflect"
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

// Statement callbacks exercise lifecycle validation and the exact query without
// a SQL driver/server. Existing lifecycle callers already prove stored identity.
type personalLifecycleMySQLDialector struct{ projectQuotaScopeDialector }

func (personalLifecycleMySQLDialector) Name() string { return "mysql" }

func TestPersonalModelCancellationSupportsExactPersistedLegacyIdentity(t *testing.T) {
	for _, dialect := range []gorm.Dialector{projectQuotaScopeDialector{}, personalLifecycleMySQLDialector{}} {
		for _, userID := range []string{"usr_team_auth", "usr_departing", "usr_mfa_member", personalTestUser, "USR_LEGACY", "legacy-user"} {
			t.Run(dialect.Name()+"/"+userID, func(t *testing.T) {
				db, err := gorm.Open(dialect, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
				if err != nil {
					t.Fatal(err)
				}
				queried := false
				if err := db.Callback().Query().Register("test:personal-lifecycle-exact", func(query *gorm.DB) {
					queried = true
					query.Statement.Clauses["WHERE"].Build(query.Statement)
					want := `WHERE "applicant_user_id" = ? AND "status" = ?`
					if dialect.Name() == "mysql" {
						want = `WHERE CAST("applicant_user_id" AS BINARY) = CAST(? AS BINARY) AND CAST("status" AS BINARY) = CAST(? AS BINARY)`
					}
					if query.Statement.SQL.String() != want || !reflect.DeepEqual(query.Statement.Vars, []any{userID, entity.PersonalModelRequestPending}) {
						t.Fatal("cancellation aliased, normalized or broadened persisted identity", query.Statement.SQL.String(), query.Statement.Vars)
					}
				}); err != nil {
					t.Fatal(err)
				}
				if err := CancelPersonalModelRequestsForUser(db, "usr_legacy_admin", userID, "applicant_unavailable"); err != nil || !queried {
					t.Fatal("valid legacy lifecycle blocked before pending lookup", queried, err)
				}
			})
		}
	}
}

func TestPersonalModelCancellationRejectsUnsafeContextBeforeQuery(t *testing.T) {
	for _, unsafe := range []string{"", strings.Repeat("a", 31), "usr_target ", "usr_target\n", "usr_target/other", "usr_目标", string([]byte{0xff})} {
		// A nil database makes accidental persistence access observable.
		if err := CancelPersonalModelRequestsForUser(nil, "usr_legacy_admin", unsafe, "applicant_unavailable"); err != apperrors.ErrBadRequest {
			t.Fatal("unsafe target reached persistence", unsafe, err)
		}
		db, err := gorm.Open(projectQuotaScopeDialector{}, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
		if err != nil {
			t.Fatal(err)
		}
		queried := false
		if err := db.Callback().Query().Register("test:personal-lifecycle-rejected", func(*gorm.DB) { queried = true }); err != nil {
			t.Fatal(err)
		}
		if err := CancelPersonalModelRequestsForUser(db, unsafe, "usr_legacy_target", "applicant_unavailable"); err != apperrors.ErrBadRequest || queried {
			t.Fatal("unsafe actor reached persistence", unsafe, queried, err)
		}
	}
	if personalModelID("usr_team_auth", "usr") || personalModelID("usr_legacy_admin", "usr") {
		t.Fatal("lifecycle compatibility widened public request identities")
	}
}
