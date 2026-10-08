package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

func testModelRecordedMetadataLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	store, err := secretstore.New([]byte(strings.Repeat("m", 32)))
	if err != nil {
		t.Fatal(err)
	}
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"recorded-metadata@example.invalid","password":"recorded-metadata-password","name":"Metadata admin"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, cookie := readIdentity(t, setup)
	request := func(method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		raw, e := json.Marshal(body)
		if e != nil {
			t.Fatal(e)
		}
		return identityRequest(router, method, path, string(raw), cookie, admin.CSRFToken)
	}
	create := func(rows ...any) {
		t.Helper()
		for _, row := range rows {
			if err := db.Create(row).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	const connectionID = "con_recorded"
	cipher, err := store.Seal("crd_recorded", "controlled-metadata-secret")
	if err != nil {
		t.Fatal(err)
	}
	create(&entity.Provider{ID: "prv_recorded", Name: "Recorded supplier"}, &entity.ProviderConnection{ID: connectionID, ProviderID: "prv_recorded", Name: "Recorded connection", BaseURL: "http://127.0.0.1:1/v1", Protocol: entity.ProtocolOpenAIChat, EgressMode: "direct"}, &entity.ProviderCredential{ID: "crd_recorded", ConnectionID: connectionID, Name: "Recorded coverage", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified"})
	for _, pm := range []string{"pmd_recorded_a", "pmd_recorded_b"} {
		create(&entity.ProviderModel{ID: pm, ConnectionID: connectionID, UpstreamName: pm}, &entity.CredentialModelAccess{CredentialID: "crd_recorded", ProviderModelID: pm})
	}
	model := decodeCatalogResponse[ModelResponse](t, request("POST", "/api/v1/admin/models", map[string]string{"name": "recorded-original", "provider_model_id": "pmd_recorded_a"}), 201)
	read := func(id string) entity.Model {
		t.Helper()
		var value entity.Model
		if err := db.Take(&value, "id = ?", id).Error; err != nil {
			t.Fatal(err)
		}
		return value
	}
	stored := read(model.ID)
	if model.CreatedAt == nil || model.ConfigUpdatedAt == nil || stored.ConfigUpdatedAt == nil || !model.CreatedAt.Equal(stored.CreatedAt) || !stored.ConfigUpdatedAt.Equal(stored.CreatedAt) || !model.ConfigUpdatedAt.Equal(*stored.ConfigUpdatedAt) {
		t.Fatal("creation metadata is not the persisted exact birth")
	}
	path := "/api/v1/admin/models/" + model.ID
	assertChanged := func(before entity.Model) entity.Model {
		t.Helper()
		after := read(before.ID)
		if after.ConfigUpdatedAt == nil || before.ConfigUpdatedAt == nil || !after.ConfigUpdatedAt.After(*before.ConfigUpdatedAt) || !after.CreatedAt.Equal(before.CreatedAt) {
			t.Fatal("real configuration change lost recorded time or changed birth")
		}
		return after
	}
	assertSame := func(before entity.Model) {
		t.Helper()
		after := read(before.ID)
		if before.ConfigUpdatedAt == nil || after.ConfigUpdatedAt == nil || !before.ConfigUpdatedAt.Equal(*after.ConfigUpdatedAt) || !before.CreatedAt.Equal(after.CreatedAt) {
			t.Fatal("no-op, replay or rollback changed Model metadata")
		}
	}
	model = decodeCatalogResponse[ModelResponse](t, request("POST", path+"/bindings", map[string]string{"provider_model_id": "pmd_recorded_b"}), 201)
	stored = assertChanged(stored)
	weights := func(a, b int) map[string]any {
		return map[string]any{"weights": []map[string]any{{"binding_id": model.Bindings[0].ID, "weight": a}, {"binding_id": model.Bindings[1].ID, "weight": b}}}
	}
	expectStatus(t, request("PUT", path+"/weights", weights(50, 50)), 200)
	stored = assertChanged(stored)
	expectStatus(t, request("PUT", path+"/weights", weights(50, 50)), 200)
	assertSame(stored)
	expectStatus(t, request("POST", path+"/rename", map[string]string{"name": "recorded-original"}), 200)
	assertSame(stored)
	expectStatus(t, request("PUT", path+"/weights", weights(50, 49)), 400)
	assertSame(stored)
	const fault = "test:model_metadata_audit_failure"
	var injected atomic.Bool
	if err := db.Callback().Create().Before("gorm:create").Register(fault, func(tx *gorm.DB) {
		if tx.Statement.Table == "audit_events" {
			injected.Store(true)
			_ = tx.AddError(errors.New("controlled audit rollback"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	removed := false
	t.Cleanup(func() {
		if !removed {
			if err := db.Callback().Create().Remove(fault); err != nil {
				t.Error(err)
			}
		}
	})
	failure := request("PUT", path+"/weights", weights(60, 40))
	if err := db.Callback().Create().Remove(fault); err != nil {
		t.Fatal(err)
	}
	removed = true
	expectStatus(t, failure, 500)
	if !injected.Load() {
		t.Fatal("rollback fixture did not reach the real audit write")
	}
	assertSame(stored)
	var rolledBack []entity.ModelProviderBinding
	if err := db.Where("model_id = ?", model.ID).Find(&rolledBack).Error; err != nil || len(rolledBack) != 2 || rolledBack[0].Weight != 50 || rolledBack[1].Weight != 50 {
		t.Fatal("configuration and recorded timestamp did not roll back together", err)
	}
	future := time.Now().UTC().Add(time.Hour)
	expectStatus(t, request("POST", path+"/rename", map[string]any{"name": "recorded-current", "alias_expires_at": future}), 200)
	stored = assertChanged(stored)
	review, err := svc.GetModelAliasRetirement(ctx, admin.User.ID, model.ID, "recorded-original")
	if err != nil {
		t.Fatal(err)
	}
	retired, err := svc.RetireModelAlias(ctx, admin.User.ID, model.ID, review.ETag, service.ModelAliasRetirementInput{Name: "recorded-original", Reason: "Stop recorded compatibility alias"})
	if err != nil || !retired.Changed {
		t.Fatal("alias retirement failed", err)
	}
	stored = assertChanged(stored)
	if _, err := svc.RetireModelAlias(ctx, admin.User.ID, model.ID, review.ETag, service.ModelAliasRetirementInput{Name: "recorded-original", Reason: "Stop recorded compatibility alias"}); err != nil {
		t.Fatal(err)
	}
	assertSame(stored)
	// A backup must use a different Provider on the target's existing protocol.
	const backupConnectionID = "con_recorded_backup"
	backupCipher, err := store.Seal("crd_recorded_backup", "controlled-metadata-backup-secret")
	if err != nil {
		t.Fatal(err)
	}
	create(&entity.Provider{ID: "prv_recorded_backup", Name: "Recorded backup supplier"}, &entity.ProviderConnection{ID: backupConnectionID, ProviderID: "prv_recorded_backup", Name: "Recorded backup connection", BaseURL: "http://127.0.0.1:1/v1", Protocol: entity.ProtocolOpenAIChat, EgressMode: "direct"}, &entity.ProviderCredential{ID: "crd_recorded_backup", ConnectionID: backupConnectionID, Name: "Recorded backup coverage", Ciphertext: backupCipher, Enabled: true, VerificationStatus: "verified"})
	for _, pm := range []string{"pmd_recorded_c", "pmd_recorded_d"} {
		create(&entity.ProviderModel{ID: pm, ConnectionID: backupConnectionID, UpstreamName: pm}, &entity.CredentialModelAccess{CredentialID: "crd_recorded_backup", ProviderModelID: pm})
	}
	// Existing-target binding and new-target creation both record configuration;
	// the identical durable receipt retry cannot update either timestamp.
	items := []service.ModelCreationItem{{ProviderModelID: "pmd_recorded_c", Target: "existing", ModelID: model.ID}, {ProviderModelID: "pmd_recorded_d", Target: "new", Name: "recorded-batch"}}
	preview, err := svc.PreviewModelCreationBatch(ctx, admin.User.ID, backupConnectionID, service.ModelCreationPreviewInput{Items: items})
	if err != nil || !preview.CanCommit {
		t.Fatal("batch review unavailable", err)
	}
	if len(preview.Items) != 2 || preview.Items[0].InitialWeight != 0 || preview.Items[1].InitialWeight != 100 {
		t.Fatal("reviewed backup changed the complete protocol total or new-target weight")
	}
	input := service.ModelCreationBatchInput{RequestID: "0ddc250d-4b17-4bc2-a548-7630dbe68529", Reason: "Record actual configuration changes", Items: items}
	batch, err := svc.CreateModelBatch(ctx, admin.User.ID, backupConnectionID, preview.ReviewETag, input)
	if err != nil {
		t.Fatal(err)
	}
	stored = assertChanged(stored)
	var targetBindings []entity.ModelProviderBinding
	if err := db.Where("model_id = ?", model.ID).Find(&targetBindings).Error; err != nil {
		t.Fatal(err)
	}
	wantWeights := map[string]int{"pmd_recorded_a": 50, "pmd_recorded_b": 50, "pmd_recorded_c": 0}
	if len(targetBindings) != len(wantWeights) {
		t.Fatal("batch backup lost the complete retained binding set")
	}
	for _, binding := range targetBindings {
		weight, ok := wantWeights[binding.ProviderModelID]
		if !ok || binding.Weight != weight {
			t.Fatal("batch backup changed a retained protocol weight")
		}
		delete(wantWeights, binding.ProviderModelID)
	}
	if len(wantWeights) != 0 {
		t.Fatal("batch backup repeated or omitted a retained binding")
	}
	var created entity.Model
	for _, item := range batch.Receipt.Items {
		if item.CreatedModel {
			created = read(item.ModelID)
		}
	}
	if created.ID == "" || created.ConfigUpdatedAt == nil || !created.ConfigUpdatedAt.Equal(created.CreatedAt) {
		t.Fatal("batch creation metadata missing")
	}
	var createdBinding entity.ModelProviderBinding
	if err := db.Where("model_id = ?", created.ID).Take(&createdBinding).Error; err != nil || createdBinding.ProviderModelID != "pmd_recorded_d" || createdBinding.Weight != 100 {
		t.Fatal("new batch target did not retain its exact reviewed complete weight", err)
	}
	if _, err := svc.CreateModelBatch(ctx, admin.User.ID, backupConnectionID, preview.ReviewETag, input); err != nil {
		t.Fatal("receipt replay failed", err)
	}
	assertSame(stored)
	assertSame(created)
	// One persisted logical fact per request; attempts and aliases are unrelated.
	now := time.Now().UTC()
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	for i, status := range []string{"success", "error", "canceled"} {
		create(&entity.CallRecord{RequestID: []string{"req_recorded_one", "req_recorded_two", "req_recorded_three"}[i], ModelID: model.ID, Status: status, StartedAt: month, CompletedAt: month.Add(time.Second)})
	}
	create(&entity.CallRecord{RequestID: "req_recorded_prior", ModelID: model.ID, Status: "success", StartedAt: month.Add(-time.Hour), CompletedAt: month.Add(-time.Hour).Add(time.Second)})
	query := "/api/v1/admin/model-monthly-requests?model_id=" + url.QueryEscape(model.ID) + "&model_id=" + url.QueryEscape(created.ID)
	counts := decodeCatalogResponse[service.ModelMonthlyRequests](t, request("GET", query, nil), 200)
	if len(counts.Items) != 2 || counts.Items[0].Requests != "3" || counts.Items[1].Requests != "0" || counts.Source != "persisted_call_records" || !counts.MayLag || !counts.PeriodFrom.Equal(month) || !counts.AsOf.Equal(counts.PeriodTo) {
		t.Fatal("monthly persisted logical request projection invalid", counts)
	}
	_, catalogCookie, catalogCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "recorded-catalog-reader", []string{"models.read_all"})
	expectStatus(t, identityRequest(router, "GET", path, "", catalogCookie, catalogCSRF), 200)
	expectStatus(t, identityRequest(router, "GET", query, "", catalogCookie, catalogCSRF), 403)
	_, callsCookie, callsCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "recorded-calls-reader", []string{"calls.read_all"})
	expectStatus(t, identityRequest(router, "GET", query, "", callsCookie, callsCSRF), 403)
	for _, suffix := range []string{"&actor_id=other", "&model_id=" + url.QueryEscape(model.ID)} {
		expectStatus(t, request("GET", query+suffix, nil), 400)
	}
}
