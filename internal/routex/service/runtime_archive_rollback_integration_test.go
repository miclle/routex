package service

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/secret"
	"gorm.io/gorm"
)

// The maintained exclusive archive worker supplies the real disposable driver.
// No public listener, upstream request, paid inference or external Vault is used.
func testRuntimeArchiveRollbackDatabase(t *testing.T, db *gorm.DB, writer *Service) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	svc, err := New(ctx, db, WithCredentialStorage(writer.secrets), WithRuntimeRefreshInterval(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	birth := time.Now().UTC().Add(-time.Second).Truncate(time.Millisecond)
	actor := entity.User{ID: configurationTestIDV1("usr", 950), Email: "archive-routing@example.test", Name: "Routing administrator", Role: entity.RoleAdmin, PasswordHash: "test-only", CreatedAt: birth}
	session := entity.Session{ID: configurationTestIDV1("ses", 950), UserID: actor.ID, TokenHash: secret.SHA256Hex("routing-test-token"), CreatedAt: birth, ExpiresAt: birth.Add(time.Hour)}
	provider := entity.Provider{ID: configurationTestIDV1("prv", 950), Name: "Routing supplier", Enabled: true, ETag: "0", CreatedAt: birth}
	connection := entity.ProviderConnection{ID: configurationTestIDV1("con", 950), ProviderID: provider.ID, Name: "Routing transport", BaseURL: "https://upstream.example.test", Protocol: entity.ProtocolOpenAIChat, Adapter: entity.AdapterNative, TransportGeneration: "0", EgressMode: "direct", Enabled: true, ETag: "0", CreatedAt: birth}
	pm := entity.ProviderModel{ID: configurationTestIDV1("pmd", 950), ConnectionID: connection.ID, UpstreamName: "routing-model", ETag: "0", CapabilityTransportGeneration: "0", CreatedAt: birth}
	pm2 := pm
	pm2.ID = configurationTestIDV1("pmd", 951)
	pm2.UpstreamName = "routing-model-two"
	model := newRecordedModel(configurationTestIDV1("mdl", 950), entity.ResourceActive, birth)
	name := entity.ModelName{Name: "archive-routing-test", ModelID: model.ID, CurrentModelID: &model.ID, CreatedAt: birth}
	bindings := []entity.ModelProviderBinding{{ID: configurationTestIDV1("bnd", 950), ModelID: model.ID, ProviderModelID: pm.ID, Weight: 70, CreatedAt: birth}, {ID: configurationTestIDV1("bnd", 951), ModelID: model.ID, ProviderModelID: pm2.ID, Weight: 30, CreatedAt: birth}}
	credential := entity.ProviderCredential{ID: configurationTestIDV1("crd", 950), ConnectionID: connection.ID, Name: "Current secret", Enabled: true, VerificationStatus: "verified", VerifiedTransportGeneration: "0", VerifiedAt: &birth, StorageSource: "inline", CreatedAt: birth}
	credential.Ciphertext, err = writer.sealSecret(credential.ID, "current-secret-never-restored")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []any{&actor, &session, &provider, &connection, &pm, &pm2, &model, &name, &bindings, &credential, &entity.CredentialModelAccess{CredentialID: credential.ID, ProviderModelID: pm.ID}, &entity.CredentialModelAccess{CredentialID: credential.ID, ProviderModelID: pm2.ID}} {
		if err = db.Create(row).Error; err != nil {
			t.Fatal("routing fixture", err)
		}
	}
	if err = db.Take(&actor, "id = ?", actor.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.Take(&session, "id = ?", session.ID).Error; err != nil {
		t.Fatal(err)
	}
	auth := &Authentication{User: actor, Session: session, Token: "routing-test-token"}
	// Register observers/faults before any Service worker exists. Atomic arming
	// changes only exact one-shot behavior, never the live GORM callback registry.
	var loads atomic.Int64
	var cancelBeforeCommit atomic.Pointer[context.CancelFunc]
	const queryObserver = "routing-rollback-load-counter"
	const createFault = "routing-rollback-before-commit-cancel"
	if err = db.Callback().Query().Before("gorm:query").Register(queryObserver, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*[]entity.User); ok && len(tx.Statement.Selects) > 0 {
			loads.Add(1)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Query().Remove(queryObserver); err != nil {
			t.Error(err)
		}
	})
	if err = db.Callback().Create().After("gorm:create").Register(createFault, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "runtime_archive_rollbacks" {
			if f := cancelBeforeCommit.Swap(nil); f != nil {
				(*f)()
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancelBeforeCommit.Store(nil)
		if err := db.Callback().Create().Remove(createFault); err != nil {
			t.Error(err)
		}
	})
	// Cleanup is registered before either startup: a failed second startup cannot
	// leave the first worker or its source process lease alive.
	t.Cleanup(func() {
		svc.StopRuntime()
		stop, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		if err := svc.StopSystemInstance(stop); err != nil {
			t.Error(err)
		}
	})
	metadata := SystemInstanceMetadata{Name: "archive-routing-test", Hostname: "localhost", Version: "test", GoVersion: "test", OS: "test", Arch: "test"}
	if err = svc.StartSystemInstance(ctx, metadata); err != nil {
		t.Fatal("routing process", err)
	}
	if err = svc.StartRuntime(ctx); err != nil {
		t.Fatal("routing publisher", err)
	}
	archive := func(t *testing.T, modify func(*runtimeData)) string {
		t.Helper()
		data, e := svc.loadRuntimeData(ctx)
		if e != nil {
			t.Fatal(e)
		}
		modify(data)
		digest, e := runtimeDigest(data)
		if e != nil {
			t.Fatal(e)
		}
		doc, e := freezeRuntimeConfigurationV1(data, digest, strings.Repeat("a", 64), strings.Repeat("b", 64))
		if e != nil {
			t.Fatal(e)
		}
		identity, created, e := newRuntimeArchiveIdentity()
		if e != nil {
			t.Fatal(e)
		}
		if e = svc.storeRuntimeConfigurationArchiveV1(ctx, identity, created, doc); e != nil {
			t.Fatal("routing archive", e)
		}
		return identity
	}
	input := func(n string) RuntimeArchiveRollbackInput {
		return RuntimeArchiveRollbackInput{"605670ca-5a6a-4f98-8021-000000000" + n, "Restore reviewed Gateway routing fields"}
	}
	review := func(t *testing.T, identity string) *RuntimeArchiveRollbackPreview {
		t.Helper()
		p, e := svc.PreviewRuntimeArchiveRollback(ctx, auth, identity)
		if e != nil {
			t.Fatal("routing review", e)
		}
		return p
	}
	count := func(t *testing.T, table, request string) int64 {
		t.Helper()
		var n int64
		if e := db.Table(table).Where("request_id = ?", request).Count(&n).Error; e != nil {
			t.Fatal(e)
		}
		return n
	}
	weightArchive := archive(t, func(data *runtimeData) {
		for i := range data.Bindings {
			if data.Bindings[i].ID == bindings[0].ID {
				data.Bindings[i].Weight = 40
			}
			if data.Bindings[i].ID == bindings[1].ID {
				data.Bindings[i].Weight = 60
			}
		}
	})
	t.Run("reviewed_atomic_weights_original_publication_and_replay", func(t *testing.T) {
		p := review(t, weightArchive)
		if !p.CanApply || len(p.Weights) != 2 {
			t.Fatal("complete weight review", p.BlockerCodes)
		}
		command := input("001")
		result, e := svc.RollbackRuntimeArchive(ctx, auth, weightArchive, p.ETag, command)
		if e != nil || result == nil || !result.Changed || !result.RuntimeApplied || result.Superseded || result.Publication.Status != "refresh_returned_nil" {
			t.Fatal("atomic routing publication", e, result)
		}
		var saved []entity.ModelProviderBinding
		if e = db.Where("model_id = ?", model.ID).Order("id").Find(&saved).Error; e != nil || saved[0].Weight != 40 || saved[1].Weight != 60 {
			t.Fatal("persisted complete weights", e)
		}
		var versions []entity.ModelWeightVersion
		if e = db.Where("model_id = ?", model.ID).Order("sequence").Find(&versions).Error; e != nil || len(versions) != 2 || versions[0].Source != "observed_baseline" || versions[1].Source != "legacy_editor" {
			t.Fatal("before/after history lost", e)
		}
		before := loads.Load()
		retry, e := svc.RollbackRuntimeArchive(ctx, auth, weightArchive, p.ETag, command)
		// One load belongs to current receipt proof; a second would be an illicit
		// repeated RefreshRuntime. This is a direct loader witness, not timestamps.
		if e != nil || retry == nil || !retry.AppliedAt.Equal(result.AppliedAt) || !reflect.DeepEqual(retry.Publication, result.Publication) || loads.Load()-before != 1 || count(t, "runtime_archive_rollbacks", command.RequestID) != 1 || count(t, "runtime_publication_commands", command.RequestID) != 1 {
			t.Fatal("exact replay repeated or changed original operation", e)
		}
		var audit entity.AuditEvent
		if e = db.Where("action = ? AND resource_id = ?", "runtime.archive.routing.rollback", weightArchive).Take(&audit).Error; e != nil || audit.DetailsJSON == nil || audit.ResourceType != "runtime_configuration_archive" || !strings.Contains(*audit.DetailsJSON, command.RequestID) || !strings.Contains(*audit.DetailsJSON, command.Reason) {
			t.Fatal("durable exact archive audit", e)
		}
		changed := command
		changed.Reason += " different"
		if _, e = svc.RollbackRuntimeArchive(ctx, auth, weightArchive, p.ETag, changed); !errors.Is(e, runtimeArchiveRollbackConflict) {
			t.Fatal("changed original intent admitted", e)
		}
		selection := RuntimeArchiveCleanupSelection{ArchiveIDs: []string{weightArchive}}
		if _, e = svc.PreviewRuntimeArchiveCleanup(ctx, auth, selection); !errors.Is(e, runtimeArchiveCleanupProtected) {
			t.Fatal("protected archive preview did not explain conflict", e)
		}
		if _, e = svc.CleanupRuntimeArchives(ctx, auth, strings.Repeat("a", 64), RuntimeArchiveCleanupInput{ArchiveIDs: selection.ArchiveIDs, RequestID: input("090").RequestID, Reason: "Attempt protected deletion"}); !errors.Is(e, runtimeArchiveCleanupProtected) {
			t.Fatal("confirmation failed protection recheck", e)
		}
		if e = db.Where("id = ?", weightArchive).Delete(&entity.RuntimeConfigurationArchive{}).Error; !errors.Is(e, gorm.ErrForeignKeyViolated) {
			t.Fatal("retained archive FK allowed deletion", e)
		}
		// Superseding current configuration cannot be undone by an old UUID.
		if e = db.Model(&entity.ModelProviderBinding{}).Where("id = ?", bindings[0].ID).Update("weight", 70).Error; e != nil {
			t.Fatal(e)
		}
		before = loads.Load()
		old, e := svc.RollbackRuntimeArchive(ctx, auth, weightArchive, p.ETag, command)
		if e != nil || old == nil || !old.Superseded || old.RuntimeApplied || loads.Load() != before {
			t.Fatal("superseded replay dispatched or claimed application", e)
		}
		if e = db.Model(&entity.ModelProviderBinding{}).Where("id = ?", bindings[1].ID).Update("weight", 30).Error; e != nil {
			t.Fatal(e)
		}
		if e = svc.RefreshRuntime(ctx); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("precommit_cancel_and_unknown_commit_never_dispatch", func(t *testing.T) {
		p := review(t, weightArchive)
		cancelled, done := context.WithCancel(ctx)
		cancelBeforeCommit.Store(&done)
		command := input("002")
		before := loads.Load()
		result, e := svc.RollbackRuntimeArchive(cancelled, auth, weightArchive, p.ETag, command)
		done()
		cancelBeforeCommit.Store(nil)
		if result != nil || !errors.Is(e, runtimeArchiveRollbackUnavailable) || count(t, "runtime_archive_rollbacks", command.RequestID) != 0 || count(t, "runtime_publication_commands", command.RequestID) != 0 || loads.Load() != before {
			t.Fatal("precommit cancellation dispatched or retained partial intent", e)
		}
		pool, e := db.DB()
		if e != nil {
			t.Fatal(e)
		}
		faultDB := db.Session(&gorm.Session{NewDB: true, Context: ctx})
		faultDB.Statement.ConnPool = runtimeArchiveCommitPool{DB: pool, uncertain: true}
		fault, e := New(ctx, faultDB, WithCredentialStorage(writer.secrets))
		if e != nil {
			t.Fatal(e)
		}
		fault.runtime = svc.runtime
		fault.instance = svc.runtimePublicationLease()
		fault.rootPolicy.Store(svc.rootPolicy.Load())
		command = input("003")
		before = loads.Load()
		if result, e = fault.RollbackRuntimeArchive(ctx, auth, weightArchive, p.ETag, command); result != nil || !errors.Is(e, runtimeArchiveRollbackUnavailable) || loads.Load() != before || count(t, "runtime_archive_rollbacks", command.RequestID) != 1 || count(t, "runtime_publication_commands", command.RequestID) != 1 {
			t.Fatal("unknown commit dispatched or lost original claim", e)
		}
		before = loads.Load()
		result, e = svc.RollbackRuntimeArchive(ctx, auth, weightArchive, p.ETag, command)
		if e != nil || result == nil || result.Publication.Status != "unknown" || result.RuntimeApplied || loads.Load()-before != 1 {
			t.Fatal("unknown claim resumed or borrowed application", e)
		}
	})
	t.Run("disabled_transport_preparation_preserves_current_authority_and_proofs", func(t *testing.T) {
		// This ordinary current change is authoritative; rollback may not re-enable.
		if e := db.Model(&entity.ProviderConnection{}).Where("id = ?", connection.ID).Update("enabled", false).Error; e != nil {
			t.Fatal(e)
		}
		identity := archive(t, func(data *runtimeData) {
			for i := range data.Connections {
				if data.Connections[i].ID == connection.ID {
					data.Connections[i].BaseURL = "https://restored.example.test"
					data.Connections[i].Enabled = true
				}
			}
			for i := range data.ProviderModels {
				if data.ProviderModels[i].ID == pm.ID {
					data.ProviderModels[i].SupportsImageInput = true
					data.ProviderModels[i].Disabled = true
				}
			}
		})
		p := review(t, identity)
		if !p.CanApply || len(p.Connections) != 1 || !p.Connections[0].RequiresVerification {
			t.Fatal("disabled transport preparation rejected", p.BlockerCodes)
		}
		result, e := svc.RollbackRuntimeArchive(ctx, auth, identity, p.ETag, input("004"))
		if e != nil || result == nil || !result.RuntimeApplied {
			t.Fatal("prepared disabled transport not published", e)
		}
		var c entity.ProviderConnection
		var pmSaved entity.ProviderModel
		var cr entity.ProviderCredential
		if e = db.Take(&c, "id = ?", connection.ID).Error; e != nil {
			t.Fatal(e)
		}
		if e = db.Take(&pmSaved, "id = ?", pm.ID).Error; e != nil {
			t.Fatal(e)
		}
		if e = db.Take(&cr, "id = ?", credential.ID).Error; e != nil {
			t.Fatal(e)
		}
		if c.Enabled || c.Protocol != connection.Protocol || c.BaseURL != "https://restored.example.test" || c.TransportGeneration == "0" || pmSaved.Disabled || !pmSaved.SupportsImageInput || pmSaved.CapabilityTransportGeneration != "0" || cr.Ciphertext != credential.Ciphertext || cr.VerifiedTransportGeneration != "0" || verifiedTransportCurrent(cr, c) || capabilityTransportCurrent(pmSaved, c) {
			t.Fatal("historical authority/secret/verification restored")
		}
		// Generic publication keeps its original ordinary API/receipt behavior after
		// private extraction. Exact generic retries do not rerun the loader.
		generic := RuntimePublicationCommandInput{RequestID: input("005").RequestID, Reason: "Original ordinary publication"}
		receipt, e := svc.PublishRuntimeCommand(ctx, auth, generic)
		if e != nil || receipt == nil || receipt.Status != "refresh_returned_nil" {
			t.Fatal("generic publication extraction changed", e)
		}
		before := loads.Load()
		retry, e := svc.PublishRuntimeCommand(ctx, auth, generic)
		if e != nil || !reflect.DeepEqual(receipt, retry) || loads.Load() != before {
			t.Fatal("generic replay changed after extraction", e)
		}
		if _, e = svc.RollbackRuntimeArchive(ctx, auth, identity, p.ETag, RuntimeArchiveRollbackInput(generic)); !errors.Is(e, runtimeArchiveRollbackConflict) {
			t.Fatal("generic UUID repurposed as rollback", e)
		}
	})
	t.Run("current_session_birth_permissions_and_stale_review_fail_closed", func(t *testing.T) {
		p := review(t, weightArchive)
		if e := db.Model(&entity.ProviderConnection{}).Where("id = ?", connection.ID).Update("ETag", configurationTestIDV1("rev", 999)).Error; e != nil {
			t.Fatal(e)
		}
		priorDenial, priorDenied := svc.runtime.deniedConnections.Load(connection.ID)
		if _, e := svc.RollbackRuntimeArchive(ctx, auth, weightArchive, p.ETag, input("006")); !errors.Is(e, runtimeArchiveRollbackConflict) {
			t.Fatal("superseded reviewed tuple admitted", e)
		}
		afterDenial, afterDenied := svc.runtime.deniedConnections.Load(connection.ID)
		if priorDenied != afterDenied || !reflect.DeepEqual(priorDenial, afterDenial) {
			t.Fatal("rejected review invalidated an untouched current transport")
		}
		stale := *auth
		stale.Session.CreatedAt = stale.Session.CreatedAt.Add(time.Microsecond)
		if _, e := svc.PreviewRuntimeArchiveRollback(ctx, &stale, weightArchive); !errors.Is(e, apperrors.ErrUnauthorized) {
			t.Fatal("old Session birth admitted", e)
		}
		stale = *auth
		stale.User.CreatedAt = stale.User.CreatedAt.Add(time.Millisecond)
		if _, e := svc.PreviewRuntimeArchiveRollback(ctx, &stale, weightArchive); !errors.Is(e, apperrors.ErrUnauthorized) {
			t.Fatal("reborn actor admitted", e)
		}
		permission := entity.RolePermission{RoleID: "rol_admin", Permission: "providers.write"}
		if e := db.Take(&permission, "role_id = ? AND permission = ?", permission.RoleID, permission.Permission).Error; e != nil {
			t.Fatal(e)
		}
		if e := db.Where("role_id = ? AND permission = ?", permission.RoleID, permission.Permission).Delete(&entity.RolePermission{}).Error; e != nil {
			t.Fatal(e)
		}
		defer func() {
			if e := db.Create(&permission).Error; e != nil {
				t.Error(e)
			}
		}()
		readonly, e := svc.PreviewRuntimeArchiveRollback(ctx, auth, weightArchive)
		if e != nil || readonly == nil || readonly.CanApply {
			t.Fatal("independent read depended on write or offered mutation", e)
		}
		if _, e = svc.RollbackRuntimeArchive(ctx, auth, weightArchive, p.ETag, input("007")); !errors.Is(e, apperrors.ErrForbidden) {
			t.Fatal("missing independent write dispatched", e)
		}
	})
}
