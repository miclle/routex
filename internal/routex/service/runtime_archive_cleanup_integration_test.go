package service

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/secret"
	"gorm.io/gorm"
)

// Invoked only inside the maintained exclusive real-driver archive worker.
func testRuntimeArchiveCleanupDatabase(t *testing.T, db *gorm.DB, svc *Service) {
	t.Helper()
	birth := time.Now().UTC().Truncate(time.Microsecond)
	actor := entity.User{ID: configurationTestIDV1("usr", 701), Email: "archive-cleanup@example.test", Name: "Archive cleanup administrator", PasswordHash: "test-only", Role: entity.RoleAdmin, CreatedAt: birth}
	token := "archive-cleanup-local-test-token"
	session := entity.Session{ID: configurationTestIDV1("ses", 701), UserID: actor.ID, TokenHash: secret.SHA256Hex(token), CreatedAt: birth, ExpiresAt: birth.Add(time.Hour)}
	if err := db.Create(&actor).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	auth := &Authentication{User: actor, Session: session, Token: token}
	write := func(t *testing.T) string {
		t.Helper()
		identity, created, err := newRuntimeArchiveIdentity()
		if err != nil {
			t.Fatal(err)
		}
		if err = svc.storeRuntimeConfigurationArchiveV1(t.Context(), identity, created, configurationFixtureV1(t)); err != nil {
			t.Fatal(err)
		}
		return identity
	}
	preview := func(t *testing.T, ids ...string) *RuntimeArchiveCleanupPreview {
		t.Helper()
		value, err := svc.PreviewRuntimeArchiveCleanup(t.Context(), auth, RuntimeArchiveCleanupSelection{ArchiveIDs: ids})
		if err != nil {
			t.Fatal("cleanup preview", err)
		}
		return value
	}
	request := func(ids []string, n int) RuntimeArchiveCleanupInput {
		return RuntimeArchiveCleanupInput{ArchiveIDs: ids, RequestID: "605670ca-5a6a-4f98-8021-" + strings.Repeat("0", 11) + string(rune('0'+n)), Reason: "Remove reviewed archive storage"}
	}
	ledger := func(t *testing.T) entity.RuntimeArchiveRetention {
		t.Helper()
		var row entity.RuntimeArchiveRetention
		if err := db.Take(&row, 1).Error; err != nil {
			t.Fatal(err)
		}
		return row
	}
	assertAbsent := func(t *testing.T, identity string) {
		t.Helper()
		var count int64
		if err := personalExact(db.Model(&entity.RuntimeConfigurationArchive{}), "id", identity).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("header retained after exact cleanup", err)
		}
		if err := personalExact(db.Model(&entity.RuntimeConfigurationArchiveChunk{}), "archive_id", identity).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("chunk retained after exact cleanup", err)
		}
	}

	t.Run("preview_no_delete_and_atomic_receipt_retry", func(t *testing.T) {
		first, second := write(t), write(t)
		view := preview(t, first, second)
		before := ledger(t)
		if view.SelectedEncryptedBytes < 1 || view.SelectedCount != 2 || view.EncryptedBytes != before.EncryptedBytes || view.VersionCap != 100 || view.EncryptedByteCap != 256<<20 {
			t.Fatal("actual encrypted-byte review lost", view)
		}
		var retained entity.RuntimeConfigurationArchive
		if err := personalExact(db, "id", first).Take(&retained).Error; err != nil {
			t.Fatal("preview deleted archive", err)
		}
		input := request([]string{second, first}, 1)
		result, err := svc.CleanupRuntimeArchives(t.Context(), auth, view.ETag, input)
		if err != nil || result.DeletedEncryptedBytes != view.SelectedEncryptedBytes || result.DeletedCount != 2 {
			t.Fatal(result, err)
		}
		assertAbsent(t, first)
		assertAbsent(t, second)
		after := ledger(t)
		if after.RetainedCount != before.RetainedCount-2 || after.EncryptedBytes != before.EncryptedBytes-view.SelectedEncryptedBytes {
			t.Fatal("ledger release did not match actual envelopes")
		}
		retry, err := svc.CleanupRuntimeArchives(t.Context(), auth, view.ETag, input)
		if err != nil || retry.RequestID != result.RequestID || !retry.CompletedAt.Equal(result.CompletedAt) || ledger(t) != after {
			t.Fatal("identical retry changed cleanup", err)
		}
		var audits int64
		if err = db.Model(&entity.AuditEvent{}).Where("resource_id = ? AND action = ?", view.ArchiveIDs[0], "runtime.archive.cleanup").Count(&audits).Error; err != nil || audits != 1 {
			t.Fatal("duplicate or missing audit", err)
		}
		var audit entity.AuditEvent
		if err = db.Where("resource_id = ? AND action = ?", view.ArchiveIDs[0], "runtime.archive.cleanup").Take(&audit).Error; err != nil || audit.ResourceType != "runtime_configuration_archive" || audit.DetailsJSON == nil {
			t.Fatal("cleanup audit target lost", err)
		}
		var details struct {
			RequestID             string   `json:"request_id"`
			ArchiveIDs            []string `json:"archive_ids"`
			DeletedEncryptedBytes int64    `json:"deleted_encrypted_bytes"`
			Reason                string   `json:"reason"`
		}
		if err = json.Unmarshal([]byte(*audit.DetailsJSON), &details); err != nil || details.RequestID != input.RequestID || !slices.Equal(details.ArchiveIDs, view.ArchiveIDs) || details.DeletedEncryptedBytes != result.DeletedEncryptedBytes || details.Reason != input.Reason {
			t.Fatal("cleanup audit intent or complete selection lost", err)
		}
		input.Reason = "Different reason"
		if _, err = svc.CleanupRuntimeArchives(t.Context(), auth, view.ETag, input); !errors.Is(err, runtimeArchiveCleanupConflict) {
			t.Fatal("changed retry admitted", err)
		}
	})
	t.Run("new_archive_invalidates_review", func(t *testing.T) {
		identity := write(t)
		view := preview(t, identity)
		_ = write(t)
		if _, err := svc.CleanupRuntimeArchives(t.Context(), auth, view.ETag, request([]string{identity}, 2)); !errors.Is(err, runtimeArchiveCleanupConflict) {
			t.Fatal("stale ledger review admitted", err)
		}
		var row entity.RuntimeConfigurationArchive
		if err := personalExact(db, "id", identity).Take(&row).Error; err != nil {
			t.Fatal("conflict deleted storage", err)
		}
	})
	t.Run("exact_session_and_current_authority_on_retry", func(t *testing.T) {
		identity := write(t)
		view := preview(t, identity)
		input := request([]string{identity}, 3)
		if _, err := svc.CleanupRuntimeArchives(t.Context(), auth, view.ETag, input); err != nil {
			t.Fatal(err)
		}
		obsolete := *auth
		obsolete.Session.CreatedAt = obsolete.Session.CreatedAt.Add(time.Microsecond)
		if _, err := svc.CleanupRuntimeArchives(t.Context(), &obsolete, view.ETag, input); !errors.Is(err, apperrors.ErrUnauthorized) {
			t.Fatal("obsolete session recovered receipt", err)
		}
		if err := db.Model(&entity.User{}).Where("id = ?", actor.ID).Update("disabled", true).Error; err != nil {
			t.Fatal(err)
		}
		_, err := svc.CleanupRuntimeArchives(t.Context(), auth, view.ETag, input)
		if err == nil {
			t.Fatal("disabled administrator recovered receipt")
		}
		if err := db.Model(&entity.User{}).Where("id = ?", actor.ID).Update("disabled", false).Error; err != nil {
			t.Fatal(err)
		}
	})
	t.Run("audit_failure_rolls_back_every_fact", func(t *testing.T) {
		identity := write(t)
		view := preview(t, identity)
		before := ledger(t)
		name := "runtime-archive-cleanup-audit-failure"
		if err := db.Callback().Create().Before("gorm:create").Register(name, func(tx *gorm.DB) {
			if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "audit_events" {
				_ = tx.AddError(errors.New("controlled private cleanup audit error"))
			}
		}); err != nil {
			t.Fatal(err)
		}
		input := request([]string{identity}, 4)
		_, failure := svc.CleanupRuntimeArchives(t.Context(), auth, view.ETag, input)
		if err := db.Callback().Create().Remove(name); err != nil {
			t.Fatal(err)
		}
		if failure != runtimeArchiveCleanupUnavailable || ledger(t) != before {
			t.Fatal("audit failure changed ledger", failure)
		}
		var row entity.RuntimeConfigurationArchive
		if err := personalExact(db, "id", identity).Take(&row).Error; err != nil {
			t.Fatal("audit rollback lost archive", err)
		}
		var count int64
		if err := db.Model(&entity.RuntimeArchiveCleanup{}).Where("request_id = ?", input.RequestID).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("failed receipt committed", err)
		}
	})
	t.Run("session_expiry_before_preview_returns_no_review", func(t *testing.T) {
		identity := write(t)
		before := ledger(t)
		callback := "runtime-archive-cleanup-preview-controlled-expiry"
		reached := false
		if err := db.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
			if tx.Statement.Schema == nil || tx.Statement.Schema.Table != "runtime_archive_contents" || tx.Error != nil {
				return
			}
			reached = true
			if e := tx.Session(&gorm.Session{NewDB: true}).Model(&entity.Session{}).Where("id = ?", session.ID).Update("expires_at", time.Now().UTC().Add(-time.Microsecond)).Error; e != nil {
				_ = tx.AddError(e)
			}
		}); err != nil {
			t.Fatal(err)
		}
		view, failure := svc.PreviewRuntimeArchiveCleanup(t.Context(), auth, RuntimeArchiveCleanupSelection{ArchiveIDs: []string{identity}})
		if err := db.Callback().Query().Remove(callback); err != nil {
			t.Fatal(err)
		}
		if !reached || view != nil || !errors.Is(failure, apperrors.ErrUnauthorized) || ledger(t) != before {
			t.Fatal("expired final Session admitted preview", reached, failure)
		}
		var saved entity.Session
		if db.Take(&saved, "id = ?", session.ID).Error != nil || !saved.ExpiresAt.Equal(session.ExpiresAt) {
			t.Fatal("controlled preview Session update escaped rollback")
		}
	})
	t.Run("session_expiry_before_commit_rolls_back_cleanup", func(t *testing.T) {
		identity := write(t)
		view := preview(t, identity)
		before := ledger(t)
		callback := "runtime-archive-cleanup-controlled-expiry"
		reached := false
		if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
			if tx.Statement.Schema == nil || tx.Statement.Schema.Table != "audit_events" {
				return
			}
			reached = true
			// Change only this exact owned Session inside the pending transaction.
			// Final expiry denial must also roll back this controlled fixture update.
			if e := tx.Session(&gorm.Session{NewDB: true}).Model(&entity.Session{}).Where("id = ?", session.ID).Update("expires_at", time.Now().UTC().Add(-time.Microsecond)).Error; e != nil {
				_ = tx.AddError(e)
			}
		}); err != nil {
			t.Fatal(err)
		}
		input := request([]string{identity}, 7)
		receipt, failure := svc.CleanupRuntimeArchives(t.Context(), auth, view.ETag, input)
		if err := db.Callback().Create().Remove(callback); err != nil {
			t.Fatal(err)
		}
		if !reached || receipt != nil || !errors.Is(failure, apperrors.ErrUnauthorized) || ledger(t) != before {
			t.Fatal("expired final Session admitted cleanup", reached, failure)
		}
		var header entity.RuntimeConfigurationArchive
		if err := personalExact(db, "id", identity).Take(&header).Error; err != nil {
			t.Fatal("expiry rollback lost archive", err)
		}
		var count int64
		if db.Model(&entity.RuntimeArchiveCleanup{}).Where("request_id = ?", input.RequestID).Count(&count).Error != nil || count != 0 {
			t.Fatal("expiry committed cleanup receipt")
		}
		var saved entity.Session
		if db.Take(&saved, "id = ?", session.ID).Error != nil || !saved.ExpiresAt.Equal(session.ExpiresAt) {
			t.Fatal("controlled Session update escaped rollback")
		}
	})
	t.Run("lost_commit_ack_replays_exact_receipt", func(t *testing.T) {
		identity := write(t)
		view := preview(t, identity)
		input := request([]string{identity}, 5)
		pool, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		faultDB := db.Session(&gorm.Session{NewDB: true, Context: t.Context()})
		faultDB.Statement.ConnPool = runtimeArchiveCommitPool{DB: pool, uncertain: true}
		fault := &Service{db: faultDB, secrets: svc.secrets}
		if receipt, err := fault.CleanupRuntimeArchives(t.Context(), auth, view.ETag, input); receipt != nil || err != runtimeArchiveCleanupUnavailable {
			t.Fatal("unknown commit reported success", err)
		}
		assertAbsent(t, identity)
		before := ledger(t)
		result, err := svc.CleanupRuntimeArchives(t.Context(), auth, view.ETag, input)
		if err != nil || result.DeletedEncryptedBytes != view.SelectedEncryptedBytes || ledger(t) != before {
			t.Fatal("lost-ack recovery replayed deletion", err)
		}
	})
	t.Run("authenticated_legacy_duplicate_rebind", func(t *testing.T) {
		first, second := write(t), write(t)
		var header entity.RuntimeConfigurationArchive
		if err := personalExact(db, "id", first).Take(&header).Error; err != nil {
			t.Fatal(err)
		}
		// Make the selected version the exact canonical target, as a retained
		// legacy duplicate could already be referenced before this cleanup.
		if err := personalExact(db.Model(&entity.RuntimeArchiveContent{}), "content_digest", header.ContentDigest).Update("archive_id", first).Error; err != nil {
			t.Fatal(err)
		}
		view := preview(t, first)
		if _, err := svc.CleanupRuntimeArchives(t.Context(), auth, view.ETag, request([]string{first}, 6)); err != nil {
			t.Fatal(err)
		}
		assertAbsent(t, first)
		var target entity.RuntimeArchiveContent
		if err := personalExact(db, "content_digest", header.ContentDigest).Take(&target).Error; err != nil || target.ArchiveID == first {
			t.Fatal("deleted canonical target retained", err)
		}
		if facts, err := svc.readRuntimeConfigurationArchiveV1(t.Context(), target.ArchiveID); err != nil || facts == nil {
			t.Fatal("rebound facts unauthenticated", err)
		}
		if facts, err := svc.readRuntimeConfigurationArchiveV1(t.Context(), second); err != nil || facts == nil {
			t.Fatal("surviving duplicate changed", err)
		}
		var row entity.RuntimeArchiveCleanup
		// Earlier migration fixtures remove and recreate columns. An explicit
		// projection avoids reusing a PostgreSQL SELECT * result shape from before
		// that test-only DDL while still reading every persisted receipt field.
		if err := db.Select("request_id", "actor_id", "actor_created_at", "review_etag", "reason", "archive_ids_json", "deleted_count", "deleted_bytes", "completed_at").Where("request_id = ?", request([]string{first}, 6).RequestID).Take(&row).Error; err != nil {
			t.Fatal(err)
		}
		var ids []string
		if json.Unmarshal([]byte(row.ArchiveIDsJSON), &ids) != nil || !slices.Equal(ids, []string{first}) {
			t.Fatal("canonical original intent lost")
		}
	})
}
