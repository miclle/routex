package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"gorm.io/gorm"
)

func modelLifecycleIntegrationRequest(router http.Handler, cookie *http.Cookie, csrf, method, target, body, etag string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "http://routex.test"+target, strings.NewReader(body))
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if etag != "" {
		req.Header.Set("If-Match", strconv.Quote(etag))
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// Invoked inside the existing catalogue scenario: no new registry entry, child
// census, Service/background worker or callback-registry mutation is introduced.
func testReviewedModelLifecycle(t *testing.T, db *gorm.DB, router http.Handler, modelID string, admin SessionResponse, adminCookie *http.Cookie, member SessionResponse, memberCookie *http.Cookie) CreatedKeyResponse {
	t.Helper()
	path := "/api/v1/admin/models/" + modelID + "/lifecycle"
	send := func(cookie *http.Cookie, csrf, method, target, body, etag string) *httptest.ResponseRecorder {
		return modelLifecycleIntegrationRequest(router, cookie, csrf, method, target, body, etag)
	}
	read := func() *service.ModelLifecycleReview {
		w := send(adminCookie, "", "GET", path, "", "")
		review := decodeCatalogResponse[service.ModelLifecycleReview](t, w, 200)
		if w.Header().Get("Cache-Control") != "private, no-store" || w.Header().Get("ETag") != strconv.Quote(review.ETag) || len(review.ETag) != 129 || review.ModelID != modelID {
			t.Fatal("invalid lifecycle review envelope")
		}
		return &review
	}
	expectStatus(t, send(nil, "", "GET", path, "", ""), 401)
	expectStatus(t, send(memberCookie, "", "GET", path, "", ""), 403)
	expectStatus(t, send(adminCookie, "", "GET", path+"?ignored=1", "", ""), 400)
	expectStatus(t, send(adminCookie, "", "GET", strings.Replace(path, modelID, strings.ToUpper(modelID), 1), "", ""), 400)
	review := read()
	if review.Status != "active" || !review.CanChange || !review.RuntimeApplied {
		t.Fatal("initial active lifecycle not applied", review)
	}
	body := `{"status":"disabled","reason":"Controlled lifecycle review"}`
	expectStatus(t, send(adminCookie, "", "PUT", path, body, review.ETag), 403)
	expectStatus(t, send(memberCookie, member.CSRFToken, "PUT", path, body, review.ETag), 403)
	expectStatus(t, send(adminCookie, admin.CSRFToken, "PUT", path, body, ""), 400)
	expectStatus(t, send(adminCookie, admin.CSRFToken, "PUT", path, `{"status":"archived","reason":"active cannot archive"}`, review.ETag), 409)
	expectStatus(t, send(adminCookie, admin.CSRFToken, "PUT", path, body, strings.Repeat("a", 64)+"."+strings.Repeat("b", 64)), 409)
	metadata := decodeCatalogResponse[ModelResponse](t, send(adminCookie, "", "GET", "/api/v1/admin/models/"+modelID, "", ""), 200)
	keyBody, _ := json.Marshal(map[string]any{"name": "Lifecycle retained Key", "model_ids": []string{modelID}})
	key := decodeCatalogResponse[CreatedKeyResponse](t, send(memberCookie, member.CSRFToken, "POST", "/api/v1/keys", string(keyBody), ""), 201)
	if key.Key.Status != entity.KeyPending {
		t.Fatal("lifecycle Key did not start pending delivery")
	}
	type nativeModelList struct {
		Object string                 `json:"object"`
		Data   []service.GatewayModel `json:"data"`
	}
	nativeModels := func() *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest("GET", "http://routex.test/v1/models", nil)
		req.Header.Set("Authorization", "Bearer "+key.Secret)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	// Model admission must be tested with a delivered, authenticated Key; the
	// pending-Key denial is a separate boundary and performs no inference.
	expectStatus(t, nativeModels(), 401)
	confirmed := decodeCatalogResponse[KeyResponse](t, send(memberCookie, member.CSRFToken, "POST", "/api/v1/keys/"+key.Key.ID+"/confirm", "", ""), 200)
	if confirmed.ID != key.Key.ID || confirmed.Status != entity.KeyActive || !reflect.DeepEqual(confirmed.ModelIDs, []string{modelID}) {
		t.Fatal("lifecycle Key delivery did not preserve active identity/scope")
	}
	admitted := decodeCatalogResponse[nativeModelList](t, nativeModels(), 200)
	if admitted.Object != "list" || len(admitted.Data) != 1 || admitted.Data[0].ID != metadata.Name {
		t.Fatal("confirmed lifecycle Key did not admit the target Model")
	}

	// Retained pending records are cancelled atomically without deleting request
	// history or any approved grant. Their historical scope need not be current.
	personal := entity.PersonalModelRequest{ID: "pmr_lifecycle_pending", RequestID: "f88cde44-4444-4444-8444-444444444444", RequestHash: strings.Repeat("a", 64), ApplicantUserID: member.User.ID, ApplicantName: member.User.Name, ModelID: modelID, ModelName: metadata.Name, ReviewETag: strings.Repeat("b", 64), Reason: "Pending model request", Status: "pending"}
	team := entity.TeamModelRequest{ID: "tmr_lifecycle_pending", RequestID: "f88cde44-5555-4555-8555-555555555555", RequestHash: strings.Repeat("a", 64), TeamID: "tea_lifecycle_history", TeamName: "Retained historical team", ApplicantUserID: member.User.ID, ApplicantName: member.User.Name, ApplicantMembershipID: "tmb_lifecycle_history", ModelID: modelID, ModelName: metadata.Name, ReviewETag: strings.Repeat("b", 64), Reason: "Pending model request", Status: "pending"}
	for _, row := range []any{&personal, &team, &entity.PersonalModelRequestPendingSlot{ApplicantUserID: member.User.ID, ModelID: modelID, RequestID: personal.ID, CreatedAt: time.Now().UTC()}, &entity.TeamModelRequestPendingSlot{TeamID: team.TeamID, ModelID: modelID, RequestID: team.ID, CreatedAt: time.Now().UTC()}} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	disabled := decodeCatalogResponse[service.ModelLifecycleResult](t, send(adminCookie, admin.CSRFToken, "PUT", path, body, review.ETag), 200)
	if !disabled.Changed || !disabled.RuntimeApplied || !disabled.Model.RuntimeApplied || disabled.Model.Status != "disabled" || disabled.Model.ETag == review.ETag {
		t.Fatal("disable did not apply", disabled)
	}
	retry := decodeCatalogResponse[service.ModelLifecycleResult](t, send(adminCookie, admin.CSRFToken, "PUT", path, body, review.ETag), 200)
	if retry.Changed || !retry.RuntimeApplied || retry.Model.Status != "disabled" {
		t.Fatal("uncertain retry repeated mutation", retry)
	}
	// Stable-birth mismatch must fail even when the requested current state agrees.
	wrongBirth := strings.Repeat("c", 64) + review.ETag[64:]
	expectStatus(t, send(adminCookie, admin.CSRFToken, "PUT", path, body, wrongBirth), 409)
	var audits int64
	if err := db.Model(&entity.AuditEvent{}).Where("resource_id = ? AND action = ?", modelID, "model.lifecycle.update").Count(&audits).Error; err != nil || audits != 1 {
		t.Fatal("retry duplicated lifecycle audit", audits, err)
	}
	after := decodeCatalogResponse[ModelResponse](t, send(adminCookie, "", "GET", "/api/v1/admin/models/"+modelID, "", ""), 200)
	if !reflect.DeepEqual(metadata.Names, after.Names) || !reflect.DeepEqual(metadata.Bindings, after.Bindings) || !reflect.DeepEqual(metadata.GrantedUserIDs, after.GrantedUserIDs) || after.CreatedAt == nil || metadata.CreatedAt == nil || !after.CreatedAt.Equal(*metadata.CreatedAt) {
		t.Fatal("disable changed retained identity/configuration")
	}
	var p entity.PersonalModelRequest
	var tr entity.TeamModelRequest
	if err := db.Take(&p, "id = ?", personal.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Take(&tr, "id = ?", team.ID).Error; err != nil {
		t.Fatal(err)
	}
	if p.Status != "cancelled" || tr.Status != "cancelled" || p.CancelledReason != "model_unavailable" || tr.CancelledReason != "model_unavailable" || p.ResolvedAt == nil || tr.ResolvedAt == nil {
		t.Fatal("pending requests not cancelled")
	}
	for _, table := range []string{"personal_model_request_pending_slots", "team_model_request_pending_slots"} {
		var count int64
		if err := db.Table(table).Where("model_id = ?", modelID).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("pending slot survived disable", table, count, err)
		}
	}
	visible := decodeCatalogResponse[VisibleModelsResponse](t, send(memberCookie, "", "GET", "/api/v1/models", "", ""), 200)
	for _, m := range visible.Items {
		if m.ID == modelID {
			t.Fatal("disabled Model visible for new native calls")
		}
	}
	retainedAdmission := decodeCatalogResponse[nativeModelList](t, nativeModels(), 200)
	if retainedAdmission.Object != "list" || len(retainedAdmission.Data) != 0 {
		t.Fatal("disabled Model changed Key authentication or retained model admission")
	}
	var beforeAttempts, afterAttempts int64
	if err := db.Model(&entity.CallAttempt{}).Count(&beforeAttempts).Error; err != nil {
		t.Fatal(err)
	}
	native := httptest.NewRequest("POST", "http://routex.test/v1/chat/completions", strings.NewReader(`{"model":"`+metadata.Name+`","messages":[{"role":"user","content":"disabled"}]}`))
	native.Header.Set("Content-Type", "application/json")
	native.Header.Set("Authorization", "Bearer "+key.Secret)
	nw := httptest.NewRecorder()
	router.ServeHTTP(nw, native)
	expectStatus(t, nw, 404)
	if err := db.Model(&entity.CallAttempt{}).Count(&afterAttempts).Error; err != nil || afterAttempts != beforeAttempts {
		t.Fatal("disabled Model dispatched a native attempt", err)
	}
	enableBody := `{"status":"active","reason":"Restore reviewed model"}`
	expectStatus(t, send(adminCookie, admin.CSRFToken, "PUT", path, enableBody, review.ETag), 409)
	fresh := read()
	enabled := decodeCatalogResponse[service.ModelLifecycleResult](t, send(adminCookie, admin.CSRFToken, "PUT", path, enableBody, fresh.ETag), 200)
	if !enabled.Changed || !enabled.RuntimeApplied || enabled.Model.Status != "active" {
		t.Fatal("re-enable did not apply", enabled)
	}
	restored := decodeCatalogResponse[VisibleModelsResponse](t, send(memberCookie, "", "GET", "/api/v1/models", "", ""), 200)
	found := false
	for _, m := range restored.Items {
		found = found || m.ID == modelID
	}
	if !found {
		t.Fatal("retained grant did not restore")
	}
	var retainedKey entity.APIKey
	if err := db.Take(&retainedKey, "id = ?", key.Key.ID).Error; err != nil || retainedKey.Status != entity.KeyActive {
		t.Fatal("lifecycle changed Key", err)
	}
	if err := db.Model(&entity.AuditEvent{}).Where("resource_id = ? AND action = ?", modelID, "model.lifecycle.update").Count(&audits).Error; err != nil || audits != 2 {
		t.Fatal("unexpected lifecycle audit count", audits, err)
	}
	return key
}

// Runs last, after the existing mutable routing/grant controls have completed.
func testReviewedModelArchive(t *testing.T, db *gorm.DB, router http.Handler, modelID string, admin SessionResponse, adminCookie *http.Cookie, member SessionResponse, key CreatedKeyResponse) {
	t.Helper()
	base := "/api/v1/admin/models/" + modelID
	send := func(method, path string, value any, etag string) *httptest.ResponseRecorder {
		var body []byte
		if value != nil {
			var err error
			body, err = json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
		}
		return modelLifecycleIntegrationRequest(router, adminCookie, admin.CSRFToken, method, path, string(body), etag)
	}
	read := func() service.ModelLifecycleReview {
		return decodeCatalogResponse[service.ModelLifecycleReview](t, send("GET", base+"/lifecycle", nil, ""), 200)
	}
	beforeRename := decodeCatalogResponse[ModelResponse](t, send("GET", base, nil, ""), 200)
	// Retain a live compatibility alias and the scenario's already expired alias.
	decodeCatalogResponse[ModelResponse](t, send("POST", base+"/rename", map[string]any{"name": "archived-lifecycle-model", "alias_expires_at": time.Now().Add(time.Hour)}, ""), 200)
	projectPending := entity.ProjectModelRequest{ID: "prq_lifecycle_history", RequestID: "f88cde44-6666-4666-8666-666666666666", RequestHash: strings.Repeat("d", 64), ProjectID: "prj_lifecycle_history", ApplicantUserID: member.User.ID, Kind: entity.ProjectRequestModelAccess, BaselineJSON: "[]", RequestedJSON: `["` + modelID + `"]`, Reason: "Retained pending Project history", Status: entity.ProjectRequestPending}
	if err := db.Create(&projectPending).Error; err != nil {
		t.Fatal(err)
	}
	var projectBefore entity.ProjectModelRequest
	if err := db.Take(&projectBefore, "id = ?", projectPending.ID).Error; err != nil {
		t.Fatal(err)
	}
	fresh := read()
	disable := map[string]string{"status": "disabled", "reason": "Prepare terminal archival"}
	disabled := decodeCatalogResponse[service.ModelLifecycleResult](t, send("PUT", base+"/lifecycle", disable, fresh.ETag), 200)
	beforeArchive := decodeCatalogResponse[ModelResponse](t, send("GET", base, nil, ""), 200)
	archive := map[string]string{"status": "archived", "reason": "Archive retained model"}
	expectStatus(t, send("PUT", base+"/lifecycle", archive, fresh.ETag), 409)
	archived := decodeCatalogResponse[service.ModelLifecycleResult](t, send("PUT", base+"/lifecycle", archive, disabled.Model.ETag), 200)
	if !archived.Changed || archived.Model.Status != "archived" || archived.Model.CanChange || !archived.RuntimeApplied || !archived.Model.RuntimeApplied || !archived.Model.CreatedAt.Equal(fresh.CreatedAt) {
		t.Fatal("terminal archive not applied", archived)
	}
	review := read()
	if review.CanChange || review.Status != "archived" || !review.RuntimeApplied {
		t.Fatal("archived review not query-only", review)
	}
	var audits int64
	if err := db.Model(&entity.AuditEvent{}).Where("resource_id = ? AND action = ?", modelID, "model.lifecycle.update").Count(&audits).Error; err != nil || audits != 4 {
		t.Fatal("archive audit count", audits, err)
	}
	retry := decodeCatalogResponse[service.ModelLifecycleResult](t, send("PUT", base+"/lifecycle", archive, disabled.Model.ETag), 200)
	if retry.Changed || retry.Model.CanChange || !retry.RuntimeApplied || retry.Model.Status != "archived" {
		t.Fatal("archive retry was not exact current-state reconciliation", retry)
	}
	expectStatus(t, send("PUT", base+"/lifecycle", archive, strings.Repeat("f", 64)+disabled.Model.ETag[64:]), 409)
	for _, status := range []string{"active", "disabled"} {
		expectStatus(t, send("PUT", base+"/lifecycle", map[string]string{"status": status, "reason": "No terminal restoration"}, review.ETag), 409)
	}
	retained := decodeCatalogResponse[ModelResponse](t, send("GET", base, nil, ""), 200)
	if !reflect.DeepEqual(retained.Names, beforeArchive.Names) || !reflect.DeepEqual(retained.Bindings, beforeRename.Bindings) || !reflect.DeepEqual(retained.GrantedUserIDs, beforeRename.GrantedUserIDs) || retained.CreatedAt == nil || beforeRename.CreatedAt == nil || !retained.CreatedAt.Equal(*beforeRename.CreatedAt) {
		t.Fatal("archive changed retained supply/grants/birth")
	}
	var storedBefore entity.Model
	if err := db.Take(&storedBefore, "id = ?", modelID).Error; err != nil {
		t.Fatal(err)
	}
	var beforeAudits int64
	if err := db.Model(&entity.AuditEvent{}).Where("resource_id = ?", modelID).Count(&beforeAudits).Error; err != nil {
		t.Fatal(err)
	}
	if len(retained.Bindings) == 0 {
		t.Fatal("missing retained binding guard witness")
	}
	// Every formerly unguarded legacy write rejects both changes and no-ops.
	expectStatus(t, send("POST", base+"/bindings", map[string]string{"provider_model_id": retained.Bindings[0].ProviderModelID}, ""), 409)
	weights := make([]ModelWeightRequest, 0, len(retained.Bindings))
	for _, row := range retained.Bindings {
		weights = append(weights, ModelWeightRequest{BindingID: row.ID, Weight: row.Weight})
	}
	expectStatus(t, send("PUT", base+"/weights", map[string]any{"weights": weights}, ""), 409)
	for _, name := range []string{retained.Name, "forbidden-archived-rename"} {
		expectStatus(t, send("POST", base+"/rename", map[string]string{"name": name}, ""), 409)
	}
	for _, users := range [][]string{retained.GrantedUserIDs, {}} {
		expectStatus(t, send("PUT", base+"/grants", map[string]any{"user_ids": users}, ""), 409)
	}
	states := map[string]bool{}
	for _, name := range retained.Names {
		if name.IsCurrent {
			continue
		}
		alias := decodeCatalogResponse[service.ModelAliasRetirementReview](t, send("GET", base+"/alias-retirement?name="+name.Name, nil, ""), 200)
		if alias.CanRetire {
			t.Fatal("archived alias offered mutation", alias)
		}
		states[alias.State] = true
		expectStatus(t, send("POST", base+"/alias-retirement", map[string]string{"name": name.Name, "reason": "No terminal alias mutation"}, alias.ETag), 409)
	}
	if !states["compatibility"] || !states["retired"] {
		t.Fatal("missing both terminal alias guard states", states)
	}
	var storedAfter entity.Model
	if err := db.Take(&storedAfter, "id = ?", modelID).Error; err != nil || !reflect.DeepEqual(storedBefore, storedAfter) {
		t.Fatal("rejected terminal write changed Model", err)
	}
	after := decodeCatalogResponse[ModelResponse](t, send("GET", base, nil, ""), 200)
	if !reflect.DeepEqual(retained, after) {
		t.Fatal("rejected terminal write changed retained catalogue")
	}
	var afterAudits int64
	if err := db.Model(&entity.AuditEvent{}).Where("resource_id = ?", modelID).Count(&afterAudits).Error; err != nil || afterAudits != beforeAudits {
		t.Fatal("rejected/no-op terminal writes appended audits", afterAudits, beforeAudits, err)
	}
	var projectAfter entity.ProjectModelRequest
	if err := db.Take(&projectAfter, "id = ?", projectPending.ID).Error; err != nil || !reflect.DeepEqual(projectBefore, projectAfter) {
		t.Fatal("archive rewrote retained pending Project history", err)
	}
	var p entity.PersonalModelRequest
	var team entity.TeamModelRequest
	if err := db.Take(&p, "id = ?", "pmr_lifecycle_pending").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Take(&team, "id = ?", "tmr_lifecycle_pending").Error; err != nil {
		t.Fatal(err)
	}
	if p.Status != "cancelled" || team.Status != "cancelled" {
		t.Fatal("archive rewrote prior cancelled request history")
	}
	var retainedKey entity.APIKey
	if err := db.Take(&retainedKey, "id = ?", key.Key.ID).Error; err != nil || retainedKey.Status != entity.KeyActive {
		t.Fatal("archive changed Key", err)
	}
	var attempts int64
	if err := db.Model(&entity.CallAttempt{}).Count(&attempts).Error; err != nil {
		t.Fatal(err)
	}
	native := httptest.NewRequest("POST", "http://routex.test/v1/chat/completions", strings.NewReader(`{"model":"`+retained.Name+`","messages":[{"role":"user","content":"archived"}]}`))
	native.Header.Set("Content-Type", "application/json")
	native.Header.Set("Authorization", "Bearer "+key.Secret)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, native)
	expectStatus(t, w, 404)
	var finalAttempts int64
	if err := db.Model(&entity.CallAttempt{}).Count(&finalAttempts).Error; err != nil || finalAttempts != attempts {
		t.Fatal("archived Model dispatched a native attempt", err)
	}
}
