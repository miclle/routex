package service

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/secret"
	"gorm.io/gorm"
)

// This opt-in worker is intentionally not registered in the full matrix. Its
// operator must allocate an exclusively owned disposable database per driver.
func TestSharedAuthorizationSQLIntegration(t *testing.T) {
	if os.Getenv("ROUTEX_TEST_SHARED_AUTH_SQL") != "1" {
		t.Skip("dedicated shared authorization SQL worker is not enabled")
	}
	driver := os.Getenv("ROUTEX_TEST_SHARED_AUTH_SQL_DRIVER")
	if driver != "postgres" && driver != "mysql" {
		t.Fatal("choose one dedicated postgres or mysql worker")
	}
	db := runtimeArchiveIntegrationDB(t, driver)
	if err := database.Migrate(t.Context(), db); err != nil {
		t.Fatal("current composed schema", err)
	}
	for ordinal, kind := range []string{"personal_key", "project_key", "personal_session", "team_session"} {
		t.Run(kind, func(t *testing.T) {
			f, original := sharedAuthorizationSQLIntegrationFixture(t, db, kind, 800+ordinal)
			if err := sharedAuthorizationSQLIntegrationValidate(t, db, original); err != nil {
				t.Fatal("current principal", err)
			}
			// A Project Key is owned by its Project, even when its creator is no
			// longer an admitted User. Team grants do not consult Personal grants.
			if kind == "project_key" && f.ProjectKey.CreatorID == f.Project.CreatorID {
				var creator entity.User
				if err := db.Take(&creator, "id = ?", f.Project.CreatorID).Error; err != nil || !creator.Disabled {
					t.Fatal("disabled creator fixture", err)
				}
			}
			if kind == "team_session" && len(f.PersonalRows) != 0 {
				t.Fatal("Team fixture must have no Personal model grant")
			}
		})
	}
	cases := []struct {
		name, kind string
		write      func(*gorm.DB, sharedAuthorizationFacts) *gorm.DB
	}{
		{"personal_key_revocation", "personal_key", func(db *gorm.DB, f sharedAuthorizationFacts) *gorm.DB {
			return db.Model(&entity.APIKey{}).Where("id = ?", f.PersonalKey.ID).Update("status", entity.KeyRevoked)
		}},
		{"personal_session_revocation", "personal_session", func(db *gorm.DB, f sharedAuthorizationFacts) *gorm.DB {
			return db.Where("id = ?", f.Session.ID).Delete(&entity.Session{})
		}},
		{"project_key_revocation", "project_key", func(db *gorm.DB, f sharedAuthorizationFacts) *gorm.DB {
			return db.Model(&entity.ProjectKey{}).Where("id = ?", f.ProjectKey.ID).Update("status", entity.KeyRevoked)
		}},
		{"team_membership_revocation", "team_session", func(db *gorm.DB, f sharedAuthorizationFacts) *gorm.DB {
			return db.Model(&entity.TeamMembership{}).Where("id = ?", f.Membership.ID).Update("status", entity.ResourceDisabled)
		}},
		{"personal_grant_revocation", "personal_session", func(db *gorm.DB, f sharedAuthorizationFacts) *gorm.DB {
			return db.Where("user_id = ? AND model_id = ?", f.User.ID, f.Model.ID).Delete(&entity.UserModelGrant{})
		}},
		{"project_grant_revocation", "project_key", func(db *gorm.DB, f sharedAuthorizationFacts) *gorm.DB {
			return db.Where("project_id = ? AND model_id = ?", f.Project.ID, f.Model.ID).Delete(&entity.ProjectModelGrant{})
		}},
		{"team_grant_revocation", "team_session", func(db *gorm.DB, f sharedAuthorizationFacts) *gorm.DB {
			return db.Where("team_id = ? AND model_id = ?", f.Team.ID, f.Model.ID).Delete(&entity.TeamModelGrant{})
		}},
		{"subject_suspension", "personal_key", func(db *gorm.DB, f sharedAuthorizationFacts) *gorm.DB {
			return db.Model(&entity.User{}).Where("id = ?", f.User.ID).Update("disabled", true)
		}},
		{"model_disable", "team_session", func(db *gorm.DB, f sharedAuthorizationFacts) *gorm.DB {
			return db.Model(&entity.Model{}).Where("id = ?", f.Model.ID).Update("status", entity.ResourceDisabled)
		}},
		{"manager_continuity_revocation", "project_key", func(db *gorm.DB, f sharedAuthorizationFacts) *gorm.DB {
			return db.Where("project_id = ?", f.Project.ID).Delete(&entity.ProjectManager{})
		}},
		{"project_disable", "project_key", func(db *gorm.DB, f sharedAuthorizationFacts) *gorm.DB {
			return db.Model(&entity.Project{}).Where("id = ?", f.Project.ID).Update("status", entity.ResourceDisabled)
		}},
	}
	for ordinal, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			f, original := sharedAuthorizationSQLIntegrationFixture(t, db, test.kind, 820+ordinal)
			held := db.WithContext(t.Context()).Begin(&sql.TxOptions{Isolation: sql.LevelReadCommitted})
			if held.Error != nil {
				t.Fatal(held.Error)
			}
			defer func() { _ = held.Rollback().Error }()
			if err := validateSharedAuthorizationSQL(held, original, time.Now().UTC()); err != nil {
				t.Fatal("held current authority", err)
			}
			// Execute a real competing statement on another connection with a
			// bounded deadline. It cannot complete while the loader's transaction
			// owns its exact row lock, including writers without governance locks.
			writerCtx, cancel := context.WithTimeout(t.Context(), 750*time.Millisecond)
			defer cancel()
			result := make(chan *gorm.DB, 1)
			go func() { result <- test.write(db.WithContext(writerCtx), f) }()
			var blocked *gorm.DB
			select {
			case blocked = <-result:
			case <-time.After(5 * time.Second):
				cancel()
				_ = held.Rollback().Error
				select {
				case <-result:
				case <-time.After(5 * time.Second):
					t.Fatal("competing writer did not join after cancellation and release")
				}
				t.Fatal("competing writer exceeded bounded deadline")
			}
			if blocked.Error == nil || blocked.RowsAffected != 0 || writerCtx.Err() != context.DeadlineExceeded {
				t.Fatal("revocation crossed held authority lock", blocked.Error, blocked.RowsAffected, writerCtx.Err())
			}
			if err := held.Rollback().Error; err != nil {
				t.Fatal("release held authorization transaction", err)
			}
			written := test.write(db.WithContext(t.Context()), f)
			if written.Error != nil || written.RowsAffected != 1 {
				t.Fatal("revocation after release", written.Error, written.RowsAffected)
			}
			if err := sharedAuthorizationSQLIntegrationValidate(t, db, original); err == nil {
				t.Fatal("captured authority survived committed revocation")
			}
		})
	}
	for ordinal, test := range cases[:4] {
		t.Run(test.name+"_writer_first", func(t *testing.T) {
			f, original := sharedAuthorizationSQLIntegrationFixture(t, db, test.kind, 850+ordinal)
			writer := db.WithContext(t.Context()).Begin(&sql.TxOptions{Isolation: sql.LevelReadCommitted})
			if writer.Error != nil {
				t.Fatal(writer.Error)
			}
			defer func() { _ = writer.Rollback().Error }()
			if result := test.write(writer, f); result.Error != nil || result.RowsAffected != 1 {
				t.Fatal("held revocation write", result.Error, result.RowsAffected)
			}
			readCtx, cancel := context.WithTimeout(t.Context(), 750*time.Millisecond)
			defer cancel()
			result := make(chan error, 1)
			go func() {
				result <- db.WithContext(readCtx).Transaction(func(tx *gorm.DB) error {
					return validateSharedAuthorizationSQL(tx, original, time.Now().UTC())
				}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
			}()
			select {
			case err := <-result:
				if err == nil || readCtx.Err() != context.DeadlineExceeded {
					t.Fatal("loader bypassed held current revocation", err, readCtx.Err())
				}
			case <-time.After(5 * time.Second):
				cancel()
				_ = writer.Rollback().Error
				select {
				case <-result:
				case <-time.After(5 * time.Second):
					t.Fatal("blocked loader failed bounded join after release")
				}
				t.Fatal("blocked loader exceeded deadline")
			}
			if err := writer.Commit().Error; err != nil {
				t.Fatal("commit held revocation", err)
			}
			if err := sharedAuthorizationSQLIntegrationValidate(t, db, original); err == nil {
				t.Fatal("committed writer-first revocation was not observed")
			}
		})
	}
}

func sharedAuthorizationSQLIntegrationValidate(t *testing.T, db *gorm.DB, original sharedCapturedAuthorization) error {
	t.Helper()
	return db.WithContext(t.Context()).Transaction(func(tx *gorm.DB) error {
		return validateSharedAuthorizationSQL(tx, original, time.Now().UTC())
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
}

func sharedAuthorizationSQLIntegrationFixture(t *testing.T, db *gorm.DB, kind string, ordinal int) (sharedAuthorizationFacts, sharedCapturedAuthorization) {
	t.Helper()
	f := sharedAuthorizationTestFacts(kind)
	now := time.Now().UTC().Truncate(time.Microsecond)
	birth, expires := now.Add(-time.Hour), now.Add(time.Hour)
	f.Model.ID, f.Model.CreatedAt = configurationTestIDV1("mdl", ordinal), birth
	rows := []any{}
	if f.User != nil {
		f.User.ID, f.User.CreatedAt = configurationTestIDV1("usr", ordinal), birth
		f.User.Email, f.User.Name = fmt.Sprintf("shared-auth-%d@example.test", ordinal), "Shared authorization fixture"
		f.Admission.CreatedAt = birth
		rows = append(rows, f.User)
	}
	if f.PersonalGrants != nil {
		f.PersonalRows = []entity.UserModelGrant{{UserID: f.User.ID, ModelID: f.Model.ID, CreatedAt: birth}}
		f.PersonalGrants.CreatedAt, f.PersonalGrants.Hash = birth, personalGrantHash(f.PersonalRows)
	}
	if f.Project != nil {
		creator := entity.User{ID: configurationTestIDV1("usr", ordinal+200), CreatedAt: birth,
			Email: fmt.Sprintf("shared-creator-%d@example.test", ordinal), Name: "Disabled audit creator", Role: entity.RoleMember, Disabled: true, PasswordHash: "test-only"}
		manager := entity.User{ID: configurationTestIDV1("usr", ordinal), CreatedAt: birth,
			Email: fmt.Sprintf("shared-manager-%d@example.test", ordinal), Name: "Current manager", Role: entity.RoleMember, PasswordHash: "test-only"}
		f.Project.ID, f.Project.CreatedAt, f.Project.CreatorID = configurationTestIDV1("prj", ordinal), birth, creator.ID
		f.ProjectGrant.ProjectID, f.ProjectGrant.ModelID = f.Project.ID, f.Model.ID
		f.ProjectKey.ID, f.ProjectKey.CreatedAt, f.ProjectKey.ProjectID, f.ProjectKey.CreatorID = configurationTestIDV1("pky", ordinal), birth.Add(time.Second), f.Project.ID, creator.ID
		f.ProjectKey.DeliveryMode, f.ProjectKey.TokenHash = "manual", secret.SHA256Hex(fmt.Sprintf("shared-project-%d", ordinal))
		rows = append(rows, &creator, &manager, f.Project,
			&entity.ProjectManager{ID: configurationTestIDV1("pmg", ordinal), ProjectID: f.Project.ID, UserID: manager.ID})
	}
	if f.Team != nil {
		f.Team.ID, f.Team.CreatedAt = configurationTestIDV1("tea", ordinal), birth
		f.Membership.ID, f.Membership.TeamID, f.Membership.UserID = configurationTestIDV1("tmb", ordinal), f.Team.ID, f.User.ID
		f.TeamGrant.TeamID, f.TeamGrant.ModelID = f.Team.ID, f.Model.ID
		rows = append(rows, f.Team, f.Membership)
	}
	rows = append(rows, &f.Model)
	if f.PersonalKey != nil {
		k := f.PersonalKey
		k.ID, k.UserID, k.CreatedAt, k.ExpiresAt, k.DeliveryMode = configurationTestIDV1("key", ordinal), f.User.ID, birth, &expires, "manual"
		k.TokenHash = secret.SHA256Hex(fmt.Sprintf("shared-personal-%d", ordinal))
		rows = append(rows, k, &entity.APIKeyModel{KeyID: k.ID, ModelID: f.Model.ID})
	}
	if f.ProjectKey != nil {
		rows = append(rows, f.ProjectKey, &entity.ProjectKeyModel{KeyID: f.ProjectKey.ID, ModelID: f.Model.ID}, f.ProjectGrant)
	}
	if f.Session != nil {
		f.Session.ID, f.Session.UserID, f.Session.CreatedAt, f.Session.ExpiresAt = configurationTestIDV1("ses", ordinal), f.User.ID, birth, expires
		f.Session.TokenHash = secret.SHA256Hex(fmt.Sprintf("shared-session-%d", ordinal))
		p, err := authenticationPolicy(db.WithContext(t.Context()))
		if err != nil {
			t.Fatal("existing local authentication policy", err)
		}
		f.Policy = &p
		rows = append(rows, f.Session)
	}
	if f.TeamGrant != nil {
		rows = append(rows, f.TeamGrant)
	}
	for i := range f.PersonalRows {
		rows = append(rows, &f.PersonalRows[i])
	}
	f.KeyModels = nil
	if f.PersonalKey != nil || f.ProjectKey != nil {
		f.KeyModels = []string{f.Model.ID}
	}
	for _, row := range rows {
		if err := db.WithContext(t.Context()).Create(row).Error; err != nil {
			t.Fatal("owned authorization fixture", err)
		}
	}
	// Capture recorded births: legacy table precision can differ from the input timestamp.
	if f.Project != nil {
		if err := db.WithContext(t.Context()).Where("id = ?", f.Project.ID).Take(f.Project).Error; err != nil {
			t.Fatal("recorded Project birth", err)
		}
		if err := db.WithContext(t.Context()).Where("id = ?", f.ProjectKey.ID).Take(f.ProjectKey).Error; err != nil {
			t.Fatal("recorded Project Key birth", err)
		}
		if f.ProjectKey.CreatedAt.Before(f.Project.CreatedAt) {
			t.Fatal("recorded Project Key predates Project")
		}
	}
	if f.Team != nil {
		if err := db.WithContext(t.Context()).Where("id = ?", f.Team.ID).Take(f.Team).Error; err != nil {
			t.Fatal("recorded Team birth", err)
		}
	}
	original, err := newSharedCapturedAuthorization(f, now)
	if err != nil {
		t.Fatal("capture fixture authority", err)
	}
	return f, original
}
