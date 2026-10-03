package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/limits"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
)

// The shared isolated harness runs creation and scoped Overview on both databases.
func testProjectCreationOverviewLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	store, err := secretstore.New(bytes.Repeat([]byte{127}, 32))
	if err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer overview-upstream" || r.Header.Get("Cookie") != "" {
			t.Error("unexpected upstream authentication")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, chatCompletionFixture("stop", `{"role":"assistant","content":"Project response"}`, `{"prompt_tokens":2,"completion_tokens":1}`, false, 0))
	}))
	defer upstream.Close()
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		svc.StopRuntime()
		if err := svc.StopCallRecorder(); err != nil {
			t.Error(err)
		}
	}()
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"overview-admin@example.invalid","password":"test-only-overview-password","name":"Overview administrator"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	manager, managerCookie, managerCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "overview-manager", nil)
	second, secondCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "overview-second", nil)
	outsider, outsiderCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "overview-outsider", nil)
	reader, readerCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "overview-reader", []string{"projects.read_all"})
	modelReviewer, modelCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "overview-model-reviewer", []string{"projects.models.write"})
	_, quotaCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "overview-quota-reviewer", []string{"projects.limits.write"})
	writer, writerCookie, writerCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "overview-writer", []string{"projects.write"})
	request := func(cookie *http.Cookie, csrf, method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		return identityRequest(router, method, path, string(encoded), cookie, csrf)
	}
	create := func(cookie *http.Cookie, csrf string, ids ...string) ProjectResponse {
		t.Helper()
		body := map[string]any{"name": "Scoped Project", "description": "Real Overview"}
		if ids != nil {
			body["manager_ids"] = ids
		}
		return decodeCatalogResponse[ProjectResponse](t, request(cookie, csrf, "POST", "/api/v1/projects", body), 201)
	}
	managerIDs := func(project ProjectResponse) []string {
		result := make([]string, 0, len(project.Managers))
		for _, person := range project.Managers {
			result = append(result, person.UserID)
		}
		slices.Sort(result)
		return result
	}
	candidates := decodeCatalogResponse[ResourceCandidatesResponse](t, request(managerCookie, "", "GET",
		"/api/v1/projects/creation-manager-candidates?q="+url.QueryEscape(second.User.Email), nil), 200)
	if len(candidates.Items) != 1 || candidates.Items[0].ID != second.User.ID {
		t.Fatalf("scoped creation picker did not find the exact candidate: %+v", candidates)
	}
	literal := decodeCatalogResponse[ResourceCandidatesResponse](t, request(managerCookie, "", "GET",
		"/api/v1/projects/creation-manager-candidates?q=%25", nil), 200)
	if len(literal.Items) != 0 {
		t.Fatal("literal percent search exposed the member directory")
	}
	expectStatus(t, request(managerCookie, "", "GET", "/api/v1/admin/members", nil), 403)
	type persistedState struct {
		Projects, Managers, Grants, Keys, Limits, Requests, Audits int64
		Snapshot                                                   string
	}
	capture := func() persistedState {
		t.Helper()
		result := persistedState{Snapshot: svc.RuntimeStatus().SnapshotID}
		queries := []*gorm.DB{
			db.Model(&entity.Project{}).Count(&result.Projects),
			db.Model(&entity.ProjectManager{}).Count(&result.Managers),
			db.Model(&entity.ProjectModelGrant{}).Count(&result.Grants),
			db.Model(&entity.ProjectKey{}).Count(&result.Keys),
			db.Model(&entity.ResourceLimit{}).Where("scope_kind = ?", "project").Count(&result.Limits),
			db.Model(&entity.ProjectModelRequest{}).Count(&result.Requests),
			db.Model(&entity.AuditEvent{}).Count(&result.Audits),
		}
		for _, query := range queries {
			if query.Error != nil {
				t.Fatal(query.Error)
			}
		}
		return result
	}
	unchanged := func(label string, operation func()) {
		t.Helper()
		before := capture()
		operation()
		if after := capture(); before != after {
			t.Fatalf("%s changed persistent state: before=%+v after=%+v", label, before, after)
		}
	}
	for _, ids := range []any{nil, []string{}, []string{second.User.ID, second.User.ID}, []string{strings.ToUpper(second.User.ID)}, []string{"usr_missing_overview"}} {
		unchanged("invalid initial manager selection", func() {
			expectStatus(t, request(managerCookie, managerCSRF, "POST", "/api/v1/projects",
				map[string]any{"name": "Rejected Project", "manager_ids": ids}), 400)
		})
	}
	for _, change := range []map[string]any{{"disabled": true}, {"offboarded_at": time.Now().UTC()}} {
		if err := db.Model(&entity.User{}).Where("id = ?", second.User.ID).Updates(change).Error; err != nil {
			t.Fatal(err)
		}
		unchanged("inactive selected manager", func() {
			expectStatus(t, request(managerCookie, managerCSRF, "POST", "/api/v1/projects",
				map[string]any{"name": "Rejected Project", "manager_ids": []string{second.User.ID}}), 400)
		})
		if err := db.Model(&entity.User{}).Where("id = ?", second.User.ID).Updates(map[string]any{"disabled": false, "offboarded_at": nil}).Error; err != nil {
			t.Fatal(err)
		}
	}
	legacy := create(managerCookie, managerCSRF)
	if ids := managerIDs(legacy); !reflect.DeepEqual(ids, []string{manager.User.ID}) || legacy.CreatorID != manager.User.ID {
		t.Fatalf("legacy creation changed creator-only semantics: %+v", legacy)
	}
	beforeJournal := decodeCatalogResponse[service.ProjectOverview](t, request(managerCookie, "", "GET", "/api/v1/projects/"+legacy.ID+"/overview", nil), 200)
	if beforeJournal.MonthlyQuota == nil || beforeJournal.MonthlyQuota.Usage != nil || beforeJournal.LastCallAt != nil {
		t.Fatalf("missing ledger became authoritative zero usage: %+v", beforeJournal)
	}
	platform := create(writerCookie, writerCSRF, second.User.ID)
	if ids := managerIDs(platform); !reflect.DeepEqual(ids, []string{second.User.ID}) || platform.CreatorID != writer.User.ID {
		t.Fatalf("platform creation lost explicit manager selection: %+v", platform)
	}
	if _, err := svc.SetMemberRoles(ctx, admin.User.ID, writer.User.ID, []string{}); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "/keys", "/usage", "/overview"} {
		expectStatus(t, request(writerCookie, "", "GET", "/api/v1/projects/"+platform.ID+suffix, nil), 404)
	}
	const auditFailure = "project_creation_overview_audit_failure"
	if err := db.Callback().Create().Before("gorm:create").Register(auditFailure, func(tx *gorm.DB) {
		if tx.Statement.Table == "audit_events" {
			_ = tx.AddError(errors.New("controlled Project creation audit outage"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	unchanged("atomic Project creation failure", func() {
		expectStatus(t, request(managerCookie, managerCSRF, "POST", "/api/v1/projects",
			map[string]any{"name": "Rolled back Project", "manager_ids": []string{second.User.ID}}), 500)
	})
	if err := db.Callback().Create().Remove(auditFailure); err != nil {
		t.Fatal(err)
	}

	// Start real journal coverage before the new application's birth.
	modelID := "mdl_project_overview"
	ciphertext, err := store.Seal("crd_project_overview", "overview-upstream")
	if err != nil {
		t.Fatal(err)
	}
	personalSecret := "rx_" + strings.Repeat("o", 43)
	for _, row := range []any{
		&entity.Provider{ID: "prv_project_overview", Name: "Overview provider"},
		&entity.ProviderConnection{ID: "con_project_overview", ProviderID: "prv_project_overview", Name: "Chat", Protocol: entity.ProtocolOpenAIChat, BaseURL: upstream.URL + "/v1"},
		&entity.ProviderCredential{ID: "crd_project_overview", ConnectionID: "con_project_overview", Name: "Verified", Ciphertext: ciphertext, Enabled: true, VerificationStatus: "verified"},
		&entity.ProviderModel{ID: "pmd_project_overview", ConnectionID: "con_project_overview", UpstreamName: "overview-native"},
		&entity.CredentialModelAccess{CredentialID: "crd_project_overview", ProviderModelID: "pmd_project_overview"},
		&entity.ReservationBound{ProviderModelID: "pmd_project_overview", Protocol: entity.ProtocolOpenAIChat,
			MaxInputTokens: 2, MaxOutputTokens: 1, ETag: "bnd_overview_capacity", Evidence: "Controlled native maximum", Reason: "Overview acceptance"},
		&entity.Model{ID: modelID, Status: entity.ResourceActive},
		&entity.ModelName{Name: "overview-model", ModelID: modelID, CurrentModelID: &modelID},
		&entity.ModelProviderBinding{ID: "bnd_project_overview", ModelID: modelID, ProviderModelID: "pmd_project_overview", Weight: 100},
		&entity.UserModelGrant{UserID: admin.User.ID, ModelID: modelID},
		&entity.APIKey{ID: "key_overview_warmup", UserID: admin.User.ID, Name: "Coverage warmup", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(personalSecret), Status: entity.KeyActive},
		&entity.APIKeyModel{KeyID: "key_overview_warmup", ModelID: modelID},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.StartCallRecorder(ctx, filepath.Join(t.TempDir(), "project-overview.db")); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	svc.StopRuntime()
	native := func(bearer string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "http://routex.test/v1/chat/completions",
			strings.NewReader(`{"model":"overview-model","messages":[{"role":"user","content":"Scoped usage"}],"max_completion_tokens":1}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+bearer)
		result := httptest.NewRecorder()
		router.ServeHTTP(result, req)
		return result
	}
	expectStatus(t, native(personalSecret), 200)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	project := create(managerCookie, managerCSRF, second.User.ID)
	expectedManagers := []string{manager.User.ID, second.User.ID}
	slices.Sort(expectedManagers)
	if ids := managerIDs(project); !reflect.DeepEqual(ids, expectedManagers) || project.CreatorID != manager.User.ID || len(project.ModelIDs) != 0 {
		t.Fatalf("ordinary creation lost creator retention or invented grants: %+v", project)
	}
	path := "/api/v1/projects/" + project.ID
	read := func(cookie *http.Cookie) service.ProjectOverview {
		t.Helper()
		return decodeCatalogResponse[service.ProjectOverview](t, request(cookie, "", "GET", path+"/overview", nil), 200)
	}
	unchanged("empty read-only Overview", func() {
		empty := read(managerCookie)
		if empty.Counts.Managers != 2 || empty.Counts.Models != 0 ||
			empty.Counts.ActiveKeys == nil || *empty.Counts.ActiveKeys != 0 ||
			empty.Counts.PendingRequests == nil || *empty.Counts.PendingRequests != 0 || !empty.CallsAvailable || empty.LastCallAt != nil {
			t.Fatalf("new application counts were not actual known zeros: %+v", empty)
		}
		quota := empty.MonthlyQuota
		if quota == nil || quota.TokensMonth != nil || quota.MoneyMonth != nil || quota.Currency != "" ||
			quota.PlatformCurrency != "USD" || quota.Usage == nil || !quota.Usage.Covered || quota.Usage.TokensUsed != "0" {
			t.Fatalf("new Project inherited defaults or fabricated coverage: %+v", quota)
		}
	})
	var implicit int64
	if err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "project", project.ID).Count(&implicit).Error; err != nil || implicit != 0 {
		t.Fatalf("creation copied defaults into Project: count=%d error=%v", implicit, err)
	}
	if _, err := svc.SetResourceModels(ctx, admin.User.ID, service.ProjectResource, project.ID, []string{modelID}); err != nil {
		t.Fatal(err)
	}
	target := service.LimitTarget{Kind: "project", ID: project.ID}
	initialLimit, err := svc.GetResourceLimit(ctx, admin.User.ID, target)
	if err != nil {
		t.Fatal(err)
	}
	cap := int64(6)
	if _, err := svc.SetResourceLimit(ctx, admin.User.ID, target, initialLimit.ETag,
		service.LimitInput{Policy: limits.Policy{TokensMonth: &cap}, Reason: "Recorded Project monthly cap"}); err != nil {
		t.Fatal(err)
	}
	createdKey := decodeCatalogResponse[CreatedProjectKeyResponse](t, request(managerCookie, managerCSRF, "POST", path+"/keys",
		map[string]any{"name": "Overview application", "model_ids": []string{modelID}, "delivery_mode": "manual"}), 201)
	expectStatus(t, request(managerCookie, managerCSRF, "POST", path+"/keys/"+createdKey.Key.ID+"/confirm", nil), 200)
	expectStatus(t, native(createdKey.Secret), 200)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	var actual entity.CallRecord
	if err := db.Where("project_id = ?", project.ID).Take(&actual).Error; err != nil {
		t.Fatal(err)
	}
	if actual.InputTokens == nil || *actual.InputTokens != 2 || actual.OutputTokens == nil || *actual.OutputTokens != 1 {
		t.Fatalf("native Project metering was not persisted: %+v", actual)
	}
	currentLimit, err := svc.GetResourceLimit(ctx, admin.User.ID, target)
	if err != nil {
		t.Fatal(err)
	}
	exactMoney := "123456789012345678.123456789012345678"
	currentLimit, err = svc.SetResourceLimit(ctx, admin.User.ID, target, currentLimit.ETag,
		service.LimitInput{Policy: limits.Policy{TokensMonth: &cap, MoneyMonth: &exactMoney, Currency: "USD"}, Reason: "Exact Project budget snapshot"})
	if err != nil {
		t.Fatal(err)
	}
	// Immutable history may outlive its resource; exact scope cannot borrow a
	// differently cased historical Project identity under a folded collation.
	aliasCall := actual
	aliasCall.RequestID = "overview_aliased_project_call"
	aliasCall.ProjectID = strings.ToUpper(project.ID)
	aliasCall.CompletedAt = time.Now().UTC()
	if err := db.Create(&aliasCall).Error; err != nil {
		t.Fatal(err)
	}
	expired := time.Now().UTC().Add(-time.Hour)
	for i, status := range []string{entity.KeyPending, entity.KeyDisabled, entity.KeyRevoked, entity.KeyActive} {
		row := entity.ProjectKey{ID: "pky_overview_excluded_" + string(rune('a'+i)), ProjectID: project.ID,
			CreatorID: manager.User.ID, Name: "Excluded", Prefix: "rxp_masked", TokenHash: secret.SHA256Hex("overview-excluded-" + status), Status: status, DeliveryMode: "manual"}
		if status == entity.KeyActive {
			row.ExpiresAt = &expired
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i, kind := range []string{entity.ProjectRequestModelAccess, entity.ProjectRequestQuota, entity.ProjectRequestRateLimit} {
		row := entity.ProjectModelRequest{ID: "pmr_overview_" + string(rune('a'+i)), RequestID: "overview-request-" + kind,
			RequestHash: secret.SHA256Hex(kind), ProjectID: project.ID, ApplicantUserID: manager.User.ID, Kind: kind,
			BaselineJSON: "[]", RequestedJSON: "[]", Reason: "Controlled pending history", Status: entity.ProjectRequestPending}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	private := `{"secret":"overview-hidden-secret","email":"overview-private@example.invalid","reason":"overview-private-reason"}`
	for i, action := range []string{"resource.update", "project.managers.replace", "resource.models.replace", "limits.update", "resource.update", "project_key.create", "RESOURCE.UPDATE"} {
		kind := "projects"
		if action == "limits.update" {
			kind = "project"
		}
		row := entity.AuditEvent{ID: "aud_overview_" + string(rune('a'+i)), ActorID: admin.User.ID,
			Action: action, ResourceType: kind, ResourceID: project.ID, DetailsJSON: &private}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	unchanged("populated read-only Overview", func() {
		response := request(managerCookie, "", "GET", path+"/overview", nil)
		result := decodeCatalogResponse[service.ProjectOverview](t, response, 200)
		if result.ProjectID != project.ID || result.Counts.Managers != 2 || result.Counts.Models != 1 ||
			result.Counts.ActiveKeys == nil || *result.Counts.ActiveKeys != 1 ||
			result.Counts.PendingRequests == nil || *result.Counts.PendingRequests != 3 ||
			!result.CallsAvailable || result.LastCallAt == nil || !result.LastCallAt.Equal(actual.CompletedAt) {
			t.Fatalf("Overview did not preserve actual scoped facts: %+v", result)
		}
		usage := result.MonthlyQuota.Usage
		if result.MonthlyQuota.PolicyETag != currentLimit.ETag || result.MonthlyQuota.TokensMonth == nil || *result.MonthlyQuota.TokensMonth != cap ||
			result.MonthlyQuota.MoneyMonth == nil || *result.MonthlyQuota.MoneyMonth != exactMoney || result.MonthlyQuota.Currency != "USD" {
			t.Fatalf("Overview rounded or lost the recorded monthly policy: %+v", result.MonthlyQuota)
		}
		if usage == nil || !usage.Covered || usage.TokensUsed != "3" || usage.TokensHeld != "0" || usage.TokensUnknown != 0 || usage.MoneyUnknown != 1 {
			t.Fatalf("Project usage borrowed Personal calls or guessed money: %+v", usage)
		}
		if len(result.Activities) != 5 {
			t.Fatalf("latest typed activity bound not respected: %+v", result.Activities)
		}
		for _, item := range result.Activities {
			if item.ActorName != nil || item.Status != "committed" ||
				!slices.Contains([]string{"project_created", "project_updated", "managers_changed", "models_changed", "limits_changed"}, item.Kind) {
				t.Fatalf("unknown or synthesized activity escaped: %+v", item)
			}
		}
		for _, forbidden := range []string{"overview-hidden-secret", "overview-private@example.invalid", "overview-private-reason", "provider_id", "key_id", "user_id", "details_json", "actor_id", "request_id"} {
			if strings.Contains(response.Body.String(), forbidden) {
				t.Fatalf("Overview leaked %q: %s", forbidden, response.Body)
			}
		}
	})
	for _, relation := range []entity.ProjectManager{
		{ID: "pmg_overview_alias_user", ProjectID: project.ID, UserID: strings.ToUpper(outsider.User.ID)},
		{ID: "pmg_overview_alias_project", ProjectID: strings.ToUpper(project.ID), UserID: outsider.User.ID},
	} {
		if err := db.Create(&relation).Error; err != nil {
			if !errors.Is(err, gorm.ErrForeignKeyViolated) {
				t.Fatal(err)
			}
			continue
		}
		expectStatus(t, request(outsiderCookie, "", "GET", path+"/overview", nil), 404)
		if scoped := read(managerCookie); scoped.Counts.Managers != 2 {
			t.Fatal("noncanonical association became a Project manager count")
		}
		if err := db.Delete(&relation).Error; err != nil {
			t.Fatal(err)
		}
	}
	global := read(readerCookie)
	if global.Counts.ActiveKeys != nil || global.CallsAvailable || global.LastCallAt != nil ||
		global.MonthlyQuota == nil || global.Counts.PendingRequests == nil || *global.Counts.PendingRequests != 3 {
		t.Fatalf("Project read_all borrowed call or Key authority: %+v", global)
	}
	partial := read(modelCookie)
	if partial.Counts.Managers != 2 || partial.Counts.Models != 1 || partial.Counts.ActiveKeys != nil ||
		partial.Counts.PendingRequests != nil || partial.MonthlyQuota != nil || partial.CallsAvailable || partial.LastCallAt != nil {
		t.Fatalf("model-only reviewer received independent section facts: %+v", partial)
	}
	expectStatus(t, request(quotaCookie, "", "GET", path, nil), 200)
	expectStatus(t, request(quotaCookie, "", "GET", path+"/overview", nil), 404)
	expectStatus(t, request(outsiderCookie, "", "GET", path+"/overview", nil), 404)
	expectStatus(t, request(adminCookie, "", "GET", "/api/v1/projects/"+strings.ToUpper(project.ID)+"/overview", nil), 404)
	expectStatus(t, request(managerCookie, "", "GET", path+"/overview?user_id="+outsider.User.ID, nil), 400)
	if _, err := svc.ProjectOverview(ctx, strings.ToUpper(reader.User.ID), project.ID); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("noncanonical actor not rejected: %v", err)
	}
	if err := db.Model(&entity.User{}).Where("id = ?", modelReviewer.User.ID).Update("disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ProjectOverview(ctx, modelReviewer.User.ID, project.ID); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("disabled reviewer retained authority: %v", err)
	}
	if _, err := svc.SetProjectManagers(ctx, admin.User.ID, project.ID, []string{second.User.ID}); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request(managerCookie, "", "GET", path+"/overview", nil), 404)
	remaining := read(secondCookie)
	if remaining.Counts.Managers != 1 || remaining.LastCallAt == nil || !remaining.LastCallAt.Equal(actual.CompletedAt) {
		t.Fatal("removing creator rewrote historical call attribution")
	}
	if err := db.Model(&entity.ProjectKey{}).Where("id = ?", createdKey.Key.ID).Update("status", entity.KeyRevoked).Error; err != nil {
		t.Fatal(err)
	}
	remaining = read(secondCookie)
	if remaining.Counts.ActiveKeys == nil || *remaining.Counts.ActiveKeys != 0 || remaining.LastCallAt == nil ||
		!remaining.LastCallAt.Equal(actual.CompletedAt) || remaining.MonthlyQuota.Usage.TokensUsed != "3" {
		t.Fatal("live Key lifecycle changed immutable call history or settled usage")
	}
}
