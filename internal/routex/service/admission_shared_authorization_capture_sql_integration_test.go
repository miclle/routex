package service

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

// Dedicated opt-in worker, not registered in the full driver matrix. It owns
// one fresh disposable schema and never enables economic or gateway admission.
func TestSharedAuthorizationInitialCaptureSQLIntegration(t *testing.T) {
	if os.Getenv("ROUTEX_TEST_SHARED_AUTH_CAPTURE_SQL") != "1" {
		t.Skip("dedicated initial shared authorization capture SQL worker is not enabled")
	}
	driver := os.Getenv("ROUTEX_TEST_SHARED_AUTH_CAPTURE_SQL_DRIVER")
	if driver != "postgres" && driver != "mysql" {
		t.Fatal("choose one dedicated postgres or mysql worker")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	db := runtimeArchiveIntegrationDB(t, driver).WithContext(ctx)
	if db.Migrator().HasTable("schema_migrations") {
		t.Fatal("initial capture worker requires a fresh exclusively owned schema")
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal("current predecessor schema", err)
	}
	owner := sharedEconomicHistorySQLOwner(t, db)
	secrets, err := secretstore.New(bytes.Repeat([]byte{47}, 32))
	if err != nil {
		t.Fatal(err)
	}
	ordinal := 4100
	fixture := func(t *testing.T, kind string) (sharedAuthorizationFacts, gatewayIdentity) {
		t.Helper()
		ordinal++
		facts, _ := sharedAuthorizationSQLIntegrationFixture(t, db, kind, ordinal)
		raw := fmt.Sprintf("%043d", ordinal)
		var table any = &entity.Session{}
		var id string
		if facts.Session != nil {
			id = facts.Session.ID
		}
		if facts.PersonalKey != nil {
			raw, table, id = "rx_"+raw, &entity.APIKey{}, facts.PersonalKey.ID
			facts.PersonalKey.TokenHash = secret.SHA256Hex(raw)
		}
		if facts.ProjectKey != nil {
			raw, table, id = "rxp_"+raw, &entity.ProjectKey{}, facts.ProjectKey.ID
			facts.ProjectKey.TokenHash = secret.SHA256Hex(raw)
		}
		if facts.Session != nil {
			facts.Session.TokenHash = secret.SHA256Hex(raw)
		}
		sharedEconomicHistorySQLChanged(t, db.Model(table).Where("id = ?", id).Update("token_hash", secret.SHA256Hex(raw)))
		svc := &Service{db: db, secrets: secrets, runtime: &gatewayRuntime{}}
		data, err := svc.loadRuntimeData(ctx)
		if err != nil {
			t.Fatal("actual coherent runtime SQL read", err)
		}
		svc.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(runtimeAuthorizationLease)))
		return facts, sharedAuthorizationCaptureAuthenticate(t, svc, kind, raw, facts)
	}
	capture := func(tx *gorm.DB, identity gatewayIdentity, now time.Time) (sharedCapturedAuthorization, error) {
		if err := lockGovernance(sharedAuthorizationDB(tx)); err != nil {
			return sharedCapturedAuthorization{}, err
		}
		if err := (&admissionOwnerStore{db: db}).CheckCurrentOwner(tx, owner.Owner, owner.InstanceStartedAt); err != nil {
			return sharedCapturedAuthorization{}, err
		}
		return captureSharedAuthorizationSQL(tx, identity, now)
	}
	captureOnce := func(identity gatewayIdentity, now time.Time) (sharedCapturedAuthorization, error) {
		var got sharedCapturedAuthorization
		err := db.Transaction(func(tx *gorm.DB) error {
			var err error
			got, err = capture(tx, identity, now)
			return err
		}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		return got, err
	}
	for _, kind := range []string{"personal_key", "project_key", "personal_session", "team_session"} {
		t.Run(kind, func(t *testing.T) {
			facts, identity := fixture(t, kind)
			now := time.Now().UTC()
			var got sharedCapturedAuthorization
			if err := db.Transaction(func(tx *gorm.DB) error {
				var err error
				got, err = capture(tx, identity, now)
				if err != nil {
					return err
				}
				return validateSharedAuthorizationSQL(tx, got, now)
			}, &sql.TxOptions{Isolation: sql.LevelReadCommitted}); err != nil {
				t.Fatal("real authenticated initial capture and exact validator", err)
			}
			want, err := newSharedCapturedAuthorization(facts, now)
			if err != nil || !reflect.DeepEqual(want, got) {
				t.Fatal("real SQL projection differs from exact stored principal", err)
			}
			if kind == "project_key" && (got.User != nil || identity.key.Key.UserID != "" || got.Key.LifecycleRevision != "") {
				t.Fatal("Project authentication fabricated User or lifecycle authority")
			}
			if kind == "team_session" && got.PersonalGrants != nil {
				t.Fatal("Team authentication borrowed Personal grant scope")
			}
		})
		t.Run(kind+"_changed_verifier", func(t *testing.T) {
			_, identity := fixture(t, kind)
			var table any = &entity.Session{}
			id := ""
			if identity.key != nil {
				table, id = &entity.APIKey{}, identity.key.Key.ID
				if identity.key.ProjectID != "" {
					table = &entity.ProjectKey{}
				}
			} else if identity.personal != nil {
				id = identity.personal.SessionID
			} else {
				id = identity.team.SessionID
			}
			sharedEconomicHistorySQLChanged(t, db.Model(table).Where("id = ?", id).Update("token_hash", secret.SHA256Hex("changed-"+id)))
			err := db.Transaction(func(tx *gorm.DB) error {
				got, err := capture(tx, identity, time.Now().UTC())
				if !reflect.DeepEqual(got, sharedCapturedAuthorization{}) {
					t.Fatal("changed verifier returned durable authority")
				}
				return err
			}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
			if err == nil {
				t.Fatal("old authenticated verifier matched a new SQL verifier")
			}
		})
	}
	t.Run("current_row_lock_blocks_revocation_until_transaction_closes", func(t *testing.T) {
		facts, identity := fixture(t, "personal_key")
		held := db.Begin(&sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if held.Error != nil {
			t.Fatal(held.Error)
		}
		defer func() { _ = held.Rollback().Error }()
		if _, err := capture(held, identity, time.Now().UTC()); err != nil {
			t.Fatal("held initial capture", err)
		}
		blocked, cancelBlocked := context.WithTimeout(ctx, 750*time.Millisecond)
		write := db.WithContext(blocked).Model(&entity.APIKey{}).Where("id = ?", facts.PersonalKey.ID).Update("status", entity.KeyRevoked)
		deadline := blocked.Err()
		cancelBlocked()
		if write.Error == nil || write.RowsAffected != 0 || deadline != context.DeadlineExceeded {
			t.Fatal("revocation crossed the initial capture's retained Key lock", write.Error, write.RowsAffected, deadline)
		}
		if err := held.Rollback().Error; err != nil {
			t.Fatal(err)
		}
		sharedEconomicHistorySQLChanged(t, db.Model(&entity.APIKey{}).Where("id = ?", facts.PersonalKey.ID).Update("status", entity.KeyRevoked))
		if _, err := captureOnce(identity, time.Now().UTC()); err == nil {
			t.Fatal("initial capture survived committed Key revocation")
		}
	})
	t.Run("bounded_personal_grants_fail_closed", func(t *testing.T) {
		facts, identity := fixture(t, "personal_session")
		rows := make([]entity.UserModelGrant, 1000)
		models := make([]entity.Model, len(rows))
		for i := range rows {
			modelID := configurationTestIDV1("mdl", 5000+i)
			models[i] = entity.Model{ID: modelID, Status: entity.ResourceActive, CreatedAt: facts.User.CreatedAt}
			rows[i] = entity.UserModelGrant{UserID: facts.User.ID, ModelID: modelID, CreatedAt: facts.User.CreatedAt}
		}
		if err := db.CreateInBatches(models, 100).Error; err != nil {
			t.Fatal("owned overflow Models", err)
		}
		// These retained rows exercise the 1,001-row complete-set read.
		if err := db.CreateInBatches(rows, 100).Error; err != nil {
			t.Fatal("owned bounded overflow fixture", err)
		}
		if got, err := captureOnce(identity, time.Now().UTC()); err == nil || !reflect.DeepEqual(got, sharedCapturedAuthorization{}) {
			t.Fatal("initial capture accepted an incomplete Personal grant set", err)
		}
	})
}
