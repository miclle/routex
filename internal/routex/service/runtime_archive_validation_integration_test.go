package service

import (
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/secret"
	"gorm.io/gorm"
)

// Called only by the exclusive archive worker, with no service workers started.
func testRuntimeArchiveValidationDatabase(t *testing.T, db *gorm.DB, svc *Service, identity string) {
	t.Helper()
	birth := time.Now().UTC().Truncate(time.Microsecond)
	actor := entity.User{ID: configurationTestIDV1("usr", 750), Email: "archive-validation@example.test", Name: "Archive validation administrator", PasswordHash: "test-only", Role: entity.RoleAdmin, CreatedAt: birth}
	token := "archive-validation-test-token"
	session := entity.Session{ID: configurationTestIDV1("ses", 750), UserID: actor.ID, TokenHash: secret.SHA256Hex(token), CreatedAt: birth, ExpiresAt: birth.Add(time.Hour)}
	if err := db.Create(&actor).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	auth := &Authentication{User: actor, Session: session, Token: token}
	var expire atomic.Bool
	const callback = "archive-validation-final-session-expiry"
	if err := db.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
		_, loaded := tx.Statement.Dest.(*[]entity.RuntimeConfigurationArchiveChunk)
		if loaded && tx.Statement.Schema != nil && tx.Statement.Schema.Table == "runtime_configuration_archive_chunks" && expire.CompareAndSwap(true, false) {
			if err := tx.Session(&gorm.Session{NewDB: true}).Model(&entity.Session{}).Where("id = ?", session.ID).Update("expires_at", time.Now().UTC().Add(-time.Second)).Error; err != nil {
				_ = tx.AddError(err)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		expire.Store(false)
		if err := db.Callback().Query().Remove(callback); err != nil {
			t.Error(err)
		}
	}()
	var original []entity.RuntimeConfigurationArchiveChunk
	if err := personalExact(db, "archive_id", identity).Order("ordinal").Find(&original).Error; err != nil || len(original) == 0 {
		t.Fatal("validation fixture chunks", err)
	}
	view, err := svc.ValidateRuntimeArchive(t.Context(), auth, identity)
	if err != nil || view == nil || view.ArchiveID != identity || view.Scope != "archive_integrity_only" || view.Integrity != "authenticated" || view.CodecVersion != 1 {
		t.Fatal("complete cold validation", err)
	}
	if result, err := svc.ValidateRuntimeArchive(t.Context(), auth, configurationTestIDV1("rcv", 751)); result != nil || !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatal("missing exact archive did not remain missing", err)
	}
	writePermission := entity.RolePermission{RoleID: "rol_admin", Permission: "system.write"}
	if err := db.Where("role_id = ? AND permission = ?", writePermission.RoleID, writePermission.Permission).Take(&writePermission).Error; err != nil {
		t.Fatal("missing independent write control", err)
	}
	defer func() {
		var count int64
		if err := db.Model(&entity.RolePermission{}).Where("role_id = ? AND permission = ?", writePermission.RoleID, writePermission.Permission).Count(&count).Error; err != nil {
			t.Error("read write-permission cleanup", err)
			return
		}
		if count == 0 {
			if err := db.Create(&writePermission).Error; err != nil {
				t.Error("restore write permission", err)
			}
		}
	}()
	if err := db.Where("role_id = ? AND permission = ?", writePermission.RoleID, writePermission.Permission).Delete(&entity.RolePermission{}).Error; err != nil {
		t.Fatal(err)
	}
	if result, err := svc.ValidateRuntimeArchive(t.Context(), auth, identity); err != nil || result == nil || result.Integrity != "authenticated" {
		t.Fatal("independent read required write authority", err)
	}
	if err := db.Create(&writePermission).Error; err != nil {
		t.Fatal(err)
	}
	var after []entity.RuntimeConfigurationArchiveChunk
	if err := personalExact(db, "archive_id", identity).Order("ordinal").Find(&after).Error; err != nil || !reflect.DeepEqual(after, original) {
		t.Fatal("validation changed archive", err)
	}
	assertDenied := func(auth *Authentication, expected error) {
		t.Helper()
		if result, err := svc.ValidateRuntimeArchive(t.Context(), auth, identity); result != nil || !errors.Is(err, expected) {
			t.Fatal("obsolete authority exposed integrity", err)
		}
	}
	obsolete := *auth
	obsolete.Session.CreatedAt = session.CreatedAt.Add(time.Microsecond)
	assertDenied(&obsolete, apperrors.ErrUnauthorized)
	obsolete = *auth
	obsolete.User.CreatedAt = birth.Add(time.Microsecond)
	assertDenied(&obsolete, apperrors.ErrUnauthorized)
	obsolete = *auth
	obsolete.Token = "different-session-proof"
	assertDenied(&obsolete, apperrors.ErrUnauthorized)
	defer func() {
		if err := db.Model(&entity.User{}).Where("id = ?", actor.ID).Update("role", entity.RoleAdmin).Error; err != nil {
			t.Error("restore validation actor", err)
		}
	}()
	if err := db.Model(&entity.User{}).Where("id = ?", actor.ID).Update("role", entity.RoleMember).Error; err != nil {
		t.Fatal(err)
	}
	assertDenied(auth, apperrors.ErrForbidden)
	if err := db.Model(&entity.User{}).Where("id = ?", actor.ID).Update("role", entity.RoleAdmin).Error; err != nil {
		t.Fatal(err)
	}
	permission := entity.RolePermission{RoleID: "rol_admin", Permission: "system.read"}
	defer func() {
		var count int64
		q := db.Where("role_id = ? AND permission = ?", permission.RoleID, permission.Permission)
		if err := q.Model(&entity.RolePermission{}).Count(&count).Error; err != nil {
			t.Error("read validation permission cleanup", err)
			return
		}
		if count == 0 {
			if err := db.Create(&permission).Error; err != nil {
				t.Error("restore validation permission", err)
			}
		}
	}()
	if err := db.Where("role_id = ? AND permission = ?", permission.RoleID, permission.Permission).Delete(&entity.RolePermission{}).Error; err != nil {
		t.Fatal(err)
	}
	assertDenied(auth, apperrors.ErrForbidden)
	if err := db.Create(&permission).Error; err != nil {
		t.Fatal(err)
	}
	if result, err := (&Service{db: db}).ValidateRuntimeArchive(t.Context(), auth, identity); result != nil || !errors.Is(err, runtimeUnavailable) {
		t.Fatal("missing root produced integrity", err)
	}
	chunk := original[0]
	defer func() {
		if err := db.Model(&entity.RuntimeConfigurationArchiveChunk{}).Where("id = ?", chunk.ID).Update("auth_ciphertext", chunk.AuthCiphertext).Error; err != nil {
			t.Error("restore validation envelope", err)
		}
	}()
	if err := db.Model(&entity.RuntimeConfigurationArchiveChunk{}).Where("id = ?", chunk.ID).Update("auth_ciphertext", "corrupt-envelope").Error; err != nil {
		t.Fatal(err)
	}
	assertDenied(auth, runtimeUnavailable)
	if err := db.Model(&entity.RuntimeConfigurationArchiveChunk{}).Where("id = ?", chunk.ID).Update("auth_ciphertext", chunk.AuthCiphertext).Error; err != nil {
		t.Fatal(err)
	}
	expire.Store(true)
	assertDenied(auth, apperrors.ErrUnauthorized)
	if expire.Load() {
		t.Fatal("controlled final-read expiry did not occur")
	}
	var retained entity.Session
	if err := db.Where("id = ?", session.ID).Take(&retained).Error; err != nil || !retained.ExpiresAt.Equal(session.ExpiresAt) {
		t.Fatal("failed validation committed controlled expiry", err)
	}
	if _, err := svc.ValidateRuntimeArchive(t.Context(), auth, identity); err != nil {
		t.Fatal("fixture not restored", err)
	}
}
