package service

import (
	"context"
	"database/sql"
	"net/netip"
	"os"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/secret"
	"gorm.io/gorm"
)

type sharedPolicySQLIntegrationState struct {
	authorization sharedCapturedAuthorization
	policies      []eventqueue.QuotaLimit
	calendar      entity.QuotaSetting
	pricing       entity.PricingSetting
}

// This dedicated worker requires an exclusively owned disposable database.
// It is deliberately not registered in the existing full integration matrix.
func TestSharedAdmissionPoliciesSQLIntegration(t *testing.T) {
	if os.Getenv("ROUTEX_TEST_SHARED_POLICY_SQL") != "1" {
		t.Skip("dedicated current policy SQL worker is not enabled")
	}
	driver := os.Getenv("ROUTEX_TEST_SHARED_POLICY_SQL_DRIVER")
	if driver != "postgres" && driver != "mysql" {
		t.Fatal("choose one dedicated postgres or mysql worker")
	}
	db := runtimeArchiveIntegrationDB(t, driver)
	if err := database.Migrate(t.Context(), db); err != nil {
		t.Fatal("coherent composed schema", err)
	}
	for ordinal, kind := range []string{"personal_key", "project_key", "personal_session", "team_session"} {
		t.Run(kind, func(t *testing.T) {
			state := sharedPolicySQLIntegrationFixture(t, db, kind, 1900+ordinal)
			if err := sharedPolicySQLIntegrationValidate(t, db, state); err != nil {
				t.Fatal("current policies and retained inactive quota root", err)
			}
		})
	}
	cases := []struct {
		name, kind string
		write      func(*gorm.DB, sharedPolicySQLIntegrationState) *gorm.DB
	}{
		{"personal_root_policy", "personal_key", func(tx *gorm.DB, state sharedPolicySQLIntegrationState) *gorm.DB {
			return tx.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "key", state.policies[1].Account[len("key_"):]).Update("ETag", "lim_replaced")
		}},
		{"project_root_ip", "project_key", func(tx *gorm.DB, state sharedPolicySQLIntegrationState) *gorm.DB {
			return tx.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "key", state.policies[1].Account[len("key_"):]).Updates(map[string]any{"ip_mode": "denylist", "ip_ranges_json": `["198.51.100.9/32"]`, "ETag": "lim_ip_replaced"})
		}},
		{"team_member_policy", "team_session", func(tx *gorm.DB, state sharedPolicySQLIntegrationState) *gorm.DB {
			return tx.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "team_member", teamMemberLimitScopeID(state.authorization.Team.ID, state.authorization.User.ID)).Update("ETag", "lim_member_replaced")
		}},
		{"calendar", "personal_session", func(tx *gorm.DB, state sharedPolicySQLIntegrationState) *gorm.DB {
			return tx.Model(&entity.QuotaSetting{}).Where("id = ?", 1).Update("ETag", state.calendar.ETag+"x")
		}},
		{"denomination", "personal_session", func(tx *gorm.DB, state sharedPolicySQLIntegrationState) *gorm.DB {
			currency := "EUR"
			if state.pricing.PlatformCurrency == currency {
				currency = "USD"
			}
			return tx.Model(&entity.PricingSetting{}).Where("id = ?", 1).Update("platform_currency", currency)
		}},
	}
	for ordinal, test := range cases {
		t.Run(test.name+"_reader_first", func(t *testing.T) {
			state := sharedPolicySQLIntegrationFixture(t, db, test.kind, 1920+ordinal)
			heldCtx, cancelHeld := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancelHeld()
			held := db.WithContext(heldCtx).Begin(&sql.TxOptions{Isolation: sql.LevelReadCommitted})
			if held.Error != nil {
				t.Fatal(held.Error)
			}
			defer func() { _ = held.Rollback().Error }()
			if err := sharedPolicySQLIntegrationCheck(held, state); err != nil {
				t.Fatal("held current authorization and policies", err)
			}
			// Direct competing row writes intentionally omit governance. The exact
			// policy/calendar/denomination row locks must still retain authority.
			writerCtx, cancel := context.WithTimeout(t.Context(), 750*time.Millisecond)
			defer cancel()
			result := make(chan *gorm.DB, 1)
			go func() { result <- test.write(db.WithContext(writerCtx), state) }()
			select {
			case blocked := <-result:
				if blocked.Error == nil || blocked.RowsAffected != 0 || writerCtx.Err() != context.DeadlineExceeded {
					t.Fatal("writer crossed retained current row lock", blocked.Error, blocked.RowsAffected, writerCtx.Err())
				}
			case <-time.After(5 * time.Second):
				cancel()
				_ = held.Rollback().Error
				select {
				case <-result:
				case <-time.After(5 * time.Second):
					t.Fatal("competing writer failed bounded join after release")
				}
				t.Fatal("competing writer exceeded bounded deadline")
			}
			if err := held.Rollback().Error; err != nil {
				t.Fatal("release held policy transaction", err)
			}
			writeCtx, cancelWrite := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancelWrite()
			written := test.write(db.WithContext(writeCtx), state)
			if written.Error != nil || written.RowsAffected != 1 {
				t.Fatal("current-row revocation after release", written.Error, written.RowsAffected)
			}
			if err := sharedPolicySQLIntegrationValidate(t, db, state); err == nil {
				t.Fatal("original policy survived committed current-row change")
			}
		})
		t.Run(test.name+"_writer_first", func(t *testing.T) {
			state := sharedPolicySQLIntegrationFixture(t, db, test.kind, 1940+ordinal)
			writerCtx, cancelWriter := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancelWriter()
			writer := db.WithContext(writerCtx).Begin(&sql.TxOptions{Isolation: sql.LevelReadCommitted})
			if writer.Error != nil {
				t.Fatal(writer.Error)
			}
			defer func() { _ = writer.Rollback().Error }()
			written := test.write(writer, state)
			if written.Error != nil || written.RowsAffected != 1 {
				t.Fatal("held policy change", written.Error, written.RowsAffected)
			}
			readCtx, cancel := context.WithTimeout(t.Context(), 750*time.Millisecond)
			defer cancel()
			result := make(chan error, 1)
			go func() {
				result <- db.WithContext(readCtx).Transaction(func(tx *gorm.DB) error {
					return sharedPolicySQLIntegrationCheck(tx, state)
				}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
			}()
			select {
			case err := <-result:
				if err == nil || readCtx.Err() != context.DeadlineExceeded {
					t.Fatal("policy loader bypassed held current-row change", err, readCtx.Err())
				}
			case <-time.After(5 * time.Second):
				cancel()
				_ = writer.Rollback().Error
				select {
				case <-result:
				case <-time.After(5 * time.Second):
					t.Fatal("blocked policy loader failed bounded join after release")
				}
				t.Fatal("blocked policy loader exceeded deadline")
			}
			if err := writer.Commit().Error; err != nil {
				t.Fatal("commit held policy change", err)
			}
			if err := sharedPolicySQLIntegrationValidate(t, db, state); err == nil {
				t.Fatal("committed writer-first change was not observed")
			}
		})
	}
}

func sharedPolicySQLIntegrationCheck(tx *gorm.DB, state sharedPolicySQLIntegrationState) error {
	now := time.Now().UTC()
	if err := validateSharedAuthorizationSQL(tx, state.authorization, now); err != nil {
		return err
	}
	return validateSharedAdmissionPoliciesSQL(tx, state.authorization, state.policies, state.calendar.ETag, state.calendar.TimeZone, netip.MustParseAddr("198.51.100.9"), now)
}

func sharedPolicySQLIntegrationValidate(t *testing.T, db *gorm.DB, state sharedPolicySQLIntegrationState) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return sharedPolicySQLIntegrationCheck(tx, state)
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
}

func sharedPolicySQLIntegrationFixture(t *testing.T, db *gorm.DB, kind string, ordinal int) sharedPolicySQLIntegrationState {
	t.Helper()
	facts, _ := sharedAuthorizationSQLIntegrationFixture(t, db, kind, ordinal)
	f := &sharedPolicySQLFixture{auth: &sharedAuthorizationSQLFixture{facts: facts}, rows: map[string]entity.ResourceLimit{}}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if facts.PersonalKey != nil {
		rootID := configurationTestIDV1("key", ordinal+100)
		root := entity.APIKey{ID: rootID, UserID: facts.User.ID, Name: "Inactive continuity root", Status: entity.KeyRevoked, DeliveryMode: "manual", CreatedAt: facts.User.CreatedAt,
			LifecycleRevision: configurationTestIDV1("kvr", ordinal+100), TokenHash: secret.SHA256Hex(rootID)}
		if err := db.Create(&root).Error; err != nil {
			t.Fatal("retained Personal root", err)
		}
		if err := db.Take(&root, "id = ?", root.ID).Error; err != nil {
			t.Fatal("recorded Personal root birth", err)
		}
		key := facts.PersonalKey
		key.ReplacesKeyID, key.CreatedAt = &rootID, now.Add(-30*time.Minute)
		if err := db.Model(&entity.APIKey{}).Where("id = ?", key.ID).Updates(map[string]any{"replaces_key_id": rootID, "created_at": key.CreatedAt}).Error; err != nil {
			t.Fatal("owned replacement fixture", err)
		}
		if err := db.Take(key, "id = ?", key.ID).Error; err != nil {
			t.Fatal("recorded replacement birth", err)
		}
		f.keys = []entity.APIKey{root, *key}
	}
	if facts.ProjectKey != nil {
		rootID := configurationTestIDV1("pky", ordinal+100)
		root := entity.ProjectKey{ID: rootID, ProjectID: facts.Project.ID, CreatorID: facts.Project.CreatorID, Name: "Inactive continuity root", Status: entity.KeyDisabled,
			DeliveryMode: "manual", CreatedAt: facts.Project.CreatedAt, TokenHash: secret.SHA256Hex(rootID)}
		if err := db.Create(&root).Error; err != nil {
			t.Fatal("retained Project root", err)
		}
		if err := db.Take(&root, "id = ?", root.ID).Error; err != nil {
			t.Fatal("recorded Project root birth", err)
		}
		key := facts.ProjectKey
		key.ReplacesKeyID, key.CreatedAt = &rootID, now.Add(-30*time.Minute)
		if err := db.Model(&entity.ProjectKey{}).Where("id = ?", key.ID).Updates(map[string]any{"replaces_key_id": rootID, "created_at": key.CreatedAt}).Error; err != nil {
			t.Fatal("owned Project replacement fixture", err)
		}
		if err := db.Take(key, "id = ?", key.ID).Error; err != nil {
			t.Fatal("recorded Project replacement birth", err)
		}
		f.projectKeys = []entity.ProjectKey{root, *key}
	}
	authorization, err := newSharedCapturedAuthorization(facts, now)
	if err != nil {
		t.Fatal("capture exact current principal", err)
	}
	state := sharedPolicySQLIntegrationState{authorization: authorization}
	if err := db.Take(&state.calendar, 1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Take(&state.pricing, 1).Error; err != nil {
		t.Fatal(err)
	}
	state.policies = sharedPolicySQLFixturePolicies(f, authorization)
	for i := range state.policies {
		policy := &state.policies[i]
		policy.Currency = state.pricing.PlatformCurrency
		row := f.rows[policy.Account]
		row.Currency = policy.Currency
		if err := db.Create(&row).Error; err != nil {
			t.Fatal("exact stored policy fixture", err)
		}
	}
	return state
}
