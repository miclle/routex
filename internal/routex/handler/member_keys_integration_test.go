package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

// Called only by the shared disposable PostgreSQL/MySQL harness after V52 and
// the exact administrative routes are integrated. This source is not acceptance.
func testMemberKeysLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	store, err := secretstore.New(bytes.Repeat([]byte{133}, 32))
	if err != nil {
		t.Fatal(err)
	}
	// Install callbacks before workers start and remove them only after shutdown.
	var rejectAudit atomic.Bool
	const callback = "test-member-key-audit-rollback"
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if rejectAudit.Load() && tx.Statement.Table == "audit_events" {
			_ = tx.AddError(errors.New("test-only audit rejection"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Callback().Create().Remove(callback); err != nil {
			t.Error(err)
		}
	}()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer member-key-fixture" {
			t.Error("unexpected upstream credential")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, chatCompletionFixture("stop", `{"role":"assistant","content":"Member response"}`, `{"prompt_tokens":2,"completion_tokens":1}`, false, 0))
	}))
	defer upstream.Close()
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	recorderPath := filepath.Join(t.TempDir(), "member-keys.db")
	defer func() {
		svc.StopRuntime()
		if err := svc.StopCallRecorder(); err != nil {
			t.Error(err)
		}
	}()
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"member-keys-admin@example.invalid","password":"member-keys-password","name":"Key administrator"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	subject, subjectCookie, subjectCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "member-keys-subject", nil)
	outsider, _, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "member-keys-outsider", nil)
	reader, readerCookie, readerCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "member-keys-reader", []string{"members.read"})
	writer, writerCookie, writerCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "member-keys-writer", []string{"members.keys.disable"})
	_, memberWriterCookie, memberWriterCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "member-keys-account-writer", []string{"members.write"})
	sealed, err := store.Seal("crd_member_keys", "member-key-fixture")
	if err != nil {
		t.Fatal(err)
	}
	modelID := "mdl_member_keys"
	for _, row := range []any{
		&entity.Provider{ID: "prv_member_keys", Name: "Provider"},
		&entity.ProviderConnection{ID: "con_member_keys", ProviderID: "prv_member_keys", Name: "Connection", Protocol: entity.ProtocolOpenAIChat, BaseURL: upstream.URL + "/v1"},
		&entity.ProviderCredential{ID: "crd_member_keys", ConnectionID: "con_member_keys", Name: "Credential", Ciphertext: sealed, Enabled: true, VerificationStatus: "verified"},
		&entity.ProviderModel{ID: "pmd_member_keys", ConnectionID: "con_member_keys", UpstreamName: "upstream"},
		&entity.CredentialModelAccess{CredentialID: "crd_member_keys", ProviderModelID: "pmd_member_keys"},
		&entity.Model{ID: modelID, Status: entity.ResourceActive},
		&entity.ModelName{Name: "member-key-model", ModelID: modelID, CurrentModelID: &modelID},
		&entity.ModelProviderBinding{ID: "bnd_member_keys", ModelID: modelID, ProviderModelID: "pmd_member_keys", Weight: 100},
		&entity.UserModelGrant{UserID: subject.User.ID, ModelID: modelID},
		&entity.UserModelGrant{UserID: outsider.User.ID, ModelID: modelID},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.StartCallRecorder(ctx, recorderPath); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	create := func(userID string) *service.CreatedKey {
		t.Helper()
		key, err := svc.CreatePersonalKey(ctx, userID, "Application", []string{modelID}, nil)
		if err != nil {
			t.Fatal(err)
		}
		return key
	}
	stored := func(keyID string) entity.APIKey {
		t.Helper()
		var key entity.APIKey
		if err := db.First(&key, "id = ?", keyID).Error; err != nil {
			t.Fatal(err)
		}
		if len(key.LifecycleRevision) != 30 || !strings.HasPrefix(key.LifecycleRevision, "kvr_") {
			t.Fatal("Key writer omitted persistent revision", keyID)
		}
		return key
	}
	changed := func(before entity.APIKey) entity.APIKey {
		t.Helper()
		after := stored(before.ID)
		if after.LifecycleRevision == before.LifecycleRevision {
			t.Fatal("writer reused lifecycle generation", before.ID)
		}
		return after
	}
	original := create(subject.User.ID)
	before := stored(original.Record.Key.ID)
	if _, err := svc.ConfirmKeyDelivery(ctx, subject.User.ID, before.ID); err != nil {
		t.Fatal(err)
	}
	before = changed(before)
	name := "Reviewed application"
	if _, err := svc.UpdatePersonalKey(ctx, subject.User.ID, before.ID, &name, nil); err != nil {
		t.Fatal(err)
	}
	before = changed(before)
	sibling := create(subject.User.ID)
	if _, err := svc.ConfirmKeyDelivery(ctx, subject.User.ID, sibling.Record.Key.ID); err != nil {
		t.Fatal(err)
	}
	foreign := create(outsider.User.ID)
	project := entity.Project{ID: "prj_member_keys", Name: "Independent Project", Status: entity.ResourceActive, CreatorID: subject.User.ID}
	projectKey := entity.ProjectKey{ID: "key_project_member_keys", ProjectID: project.ID, CreatorID: subject.User.ID, Name: "Project retained", Prefix: "rtx_project", TokenHash: secret.SHA256Hex("project-fixture"), Status: entity.KeyRevoked, DeliveryMode: "manual"}
	for _, row := range []any{&project, &projectKey} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.First(&projectKey, "id = ?", projectKey.ID).Error; err != nil {
		t.Fatal(err)
	}
	projectBefore := projectKey

	base := "/api/v1/admin/members/" + subject.User.ID + "/keys"
	request := func(cookie *http.Cookie, csrf, method, path, body, etag string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://routex.test"+path, strings.NewReader(body))
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
			req.Header.Set("If-Match", `"`+etag+`"`)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	review := func() service.MemberKeyRecord {
		return decodeCatalogResponse[service.MemberKeyRecord](t, request(readerCookie, "", "GET", base+"/"+before.ID, "", ""), 200)
	}
	page := decodeCatalogResponse[service.MemberKeyPage](t, request(readerCookie, "", "GET", base, "", ""), 200)
	if len(page.Items) != 2 {
		t.Fatal("member projection lost retained Personal Keys", page)
	}
	for _, row := range page.Items {
		if row.ID == foreign.Record.Key.ID || row.ID == projectKey.ID || row.DisableEligible || row.Limits == nil || row.Limits.QuotaUsage == nil || row.Limits.PlatformCurrency == "" {
			t.Fatal("read projection violated scope or authority", row)
		}
	}
	raw, _ := json.Marshal(page)
	for _, sensitive := range []string{original.Secret, before.TokenHash, before.Prefix, "lifecycle_revision", "token_hash", "delivery_expires_at"} {
		if strings.Contains(string(raw), sensitive) {
			t.Fatal("member list disclosed credential or internal material")
		}
	}
	target := review()
	expectStatus(t, request(writerCookie, "", "GET", base, "", ""), 403)
	expectStatus(t, request(readerCookie, readerCSRF, "POST", base+"/"+before.ID+"/disable", `{"reason":"Read alone"}`, target.ETag), 403)
	expectStatus(t, request(memberWriterCookie, memberWriterCSRF, "POST", base+"/"+before.ID+"/disable", `{"reason":"Account write alone"}`, target.ETag), 403)
	expectStatus(t, request(writerCookie, writerCSRF, "POST", base+"/"+foreign.Record.Key.ID+"/disable", `{"reason":"Foreign owner"}`, target.ETag), 404)
	expectStatus(t, request(writerCookie, writerCSRF, "POST", base+"/"+projectKey.ID+"/disable", `{"reason":"Project ownership"}`, target.ETag), 404)

	expectStatus(t, request(adminCookie, admin.CSRFToken, "POST", strings.Replace(base, subject.User.ID, strings.ToUpper(subject.User.ID), 1)+"/"+before.ID+"/disable", `{"reason":"Alias"}`, target.ETag), 404)
	expectStatus(t, request(writerCookie, "", "POST", base+"/"+before.ID+"/disable", `{"reason":"CSRF absent"}`, target.ETag), 403)
	// The owner retains an independent Session; no administrative impersonation.
	expectStatus(t, identityRequest(router, "GET", "/api/v1/auth/session", "", subjectCookie, ""), 200)
	call := func(bearer string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "http://routex.test/v1/chat/completions", strings.NewReader(`{"model":"member-key-model","messages":[{"role":"user","content":"Recorded call"}]}`))
		req.Header.Set("Authorization", "Bearer "+bearer)
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	expectStatus(t, call(original.Secret), 200)
	deadline := time.Now().Add(5 * time.Second)
	for {
		target = review()
		if target.LastUsedAt != nil && target.LastUseCoverage == "recorded" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("real recorded Personal call absent from exact last use")
		}
		time.Sleep(10 * time.Millisecond)
	}
	siblingBefore := stored(sibling.Record.Key.ID)
	var audits int64
	countAudits := func() int64 {
		t.Helper()
		if err := db.Model(&entity.AuditEvent{}).Where("action = ? AND resource_id = ?", "member.key.disable", before.ID).Count(&audits).Error; err != nil {
			t.Fatal(err)
		}
		return audits
	}
	if countAudits() != 0 {
		t.Fatal("fixture polluted disable audit")
	}
	rejectAudit.Store(true)
	expectStatus(t, request(writerCookie, writerCSRF, "POST", base+"/"+before.ID+"/disable", `{"reason":"Contain this Key"}`, target.ETag), 500)
	rejectAudit.Store(false)
	if !reflect.DeepEqual(stored(before.ID), before) || countAudits() != 0 {
		t.Fatal("failed audit escaped transactional rollback")
	}
	result := decodeCatalogResponse[service.MemberKeyDisableRecord](t, request(writerCookie, writerCSRF, "POST", base+"/"+before.ID+"/disable", `{"reason":"Contain this Key"}`, target.ETag), 200)
	before = changed(before)
	if !result.RuntimeApplied || result.Confirmation != "current_disabled_state" || result.Status != entity.KeyDisabled || before.Status != entity.KeyDisabled || countAudits() != 1 {
		t.Fatal("disable did not confirm exact current persisted publication", result)
	}
	var audit entity.AuditEvent
	if err := db.Where("action = ? AND resource_id = ?", "member.key.disable", before.ID).First(&audit).Error; err != nil {
		t.Fatal(err)
	}
	if audit.ActorID != writer.User.ID || audit.ActorID == reader.User.ID || audit.DetailsJSON == nil || strings.Contains(*audit.DetailsJSON, original.Secret) {
		t.Fatal("disable audit borrowed owner identity or disclosed secrets")
	}
	expectStatus(t, call(original.Secret), 401)
	expectStatus(t, call(sibling.Secret), 200)
	if !reflect.DeepEqual(stored(sibling.Record.Key.ID), siblingBefore) {
		t.Fatal("target disable changed sibling Key")
	}
	expectStatus(t, identityRequest(router, "GET", "/api/v1/auth/session", "", subjectCookie, ""), 200)
	retry := decodeCatalogResponse[service.MemberKeyDisableRecord](t, request(writerCookie, writerCSRF, "POST", base+"/"+before.ID+"/disable", `{"reason":"Contain this Key"}`, target.ETag), 200)
	if retry.ETag != result.ETag || retry.Confirmation != "current_disabled_state" || countAudits() != 1 {
		t.Fatal("current retry created an original historical receipt or duplicate audit")
	}
	currentWriter, err := svc.GetMember(ctx, admin.User.ID, writer.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetMemberRoles(ctx, admin.User.ID, writer.User.ID, []string{}); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request(writerCookie, writerCSRF, "POST", base+"/"+before.ID+"/disable", `{"reason":"Contain this Key"}`, target.ETag), 403)
	if _, err := svc.SetMemberRoles(ctx, admin.User.ID, writer.User.ID, currentWriter.RoleIDs); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stored(before.ID), before) || countAudits() != 1 {
		t.Fatal("revoked current authority changed reconciled Key")
	}
	enabled := true
	if _, err := svc.UpdatePersonalKey(ctx, subject.User.ID, before.ID, nil, &enabled); err != nil {
		t.Fatal(err)
	}
	before = changed(before)
	expectStatus(t, request(writerCookie, writerCSRF, "POST", base+"/"+before.ID+"/disable", `{"reason":"Contain this Key"}`, target.ETag), 409)
	expectStatus(t, call(original.Secret), 200)
	// Planned native rotation changes the retired predecessor generation once.
	replacement, err := svc.RotatePersonalKey(ctx, subject.User.ID, before.ID)
	if err != nil {
		t.Fatal(err)
	}
	replacementBefore := stored(replacement.Record.Key.ID)
	if _, err := svc.ConfirmKeyDelivery(ctx, subject.User.ID, replacementBefore.ID); err != nil {
		t.Fatal(err)
	}
	_ = changed(replacementBefore)
	expectStatus(t, call(replacement.Secret), 200)
	deadline = time.Now().Add(5 * time.Second)
	for {
		err = svc.CompletePersonalKeyRotation(ctx, subject.User.ID, before.ID, replacementBefore.ID)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("native completed replacement did not retire predecessor", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	before = changed(before)
	if before.Status != entity.KeyRevoked {
		t.Fatal("rotation predecessor not retired")
	}
	if err := svc.CompletePersonalKeyRotation(ctx, subject.User.ID, before.ID, replacementBefore.ID); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stored(before.ID), before) {
		t.Fatal("historical rotation retry changed Key revision")
	}
	revokeBefore := stored(sibling.Record.Key.ID)
	if err := svc.RevokePersonalKey(ctx, subject.User.ID, revokeBefore.ID); err != nil {
		t.Fatal(err)
	}
	_ = changed(revokeBefore)
	// Suspension, planned departure and emergency departure all advance the same
	// persisted lifecycle identity while preserving the independent owner scope.
	for _, mode := range []string{"suspension", "planned", "emergency"} {
		person, _, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "member-keys-"+mode, nil)
		if err := db.Create(&entity.UserModelGrant{UserID: person.User.ID, ModelID: modelID}).Error; err != nil {
			t.Fatal(err)
		}
		created := create(person.User.ID)
		departureBefore := stored(created.Record.Key.ID)
		switch mode {
		case "suspension":
			disabled := true
			_, err = svc.UpdateMember(ctx, admin.User.ID, person.User.ID, &disabled, nil)
		case "planned":
			inventory, e := svc.OffboardingInventory(ctx, admin.User.ID, person.User.ID)
			if e != nil {
				t.Fatal(e)
			}
			plan, e := svc.CreateOffboardingPlan(ctx, admin.User.ID, person.User.ID, service.OffboardingPlanInput{RequestID: "req_member_keys_planned", InventoryVersion: inventory.InventoryVersion, PlannedAt: time.Now().Add(time.Hour), Reason: "Planned departure"})
			if e != nil {
				t.Fatal(e)
			}
			_, err = svc.CompleteOffboarding(ctx, admin.User.ID, person.User.ID, plan.ID)
		case "emergency":
			_, err = svc.EmergencyOffboarding(ctx, admin.User.ID, person.User.ID, service.OffboardingEmergencyInput{RequestID: "req_member_keys_emergency", CurrentPassword: "member-keys-password", Reason: "Emergency departure"})
		}
		if err != nil {
			t.Fatal(mode, err)
		}
		if after := changed(departureBefore); after.Status != entity.KeyRevoked {
			t.Fatal(mode, "did not retire owned Key")
		}
	}
	// Restart reuses the durable journal and exact stored disabled current state.
	target = decodeCatalogResponse[service.MemberKeyRecord](t, request(adminCookie, "", "GET", base+"/"+replacementBefore.ID, "", ""), 200)
	svc.StopRuntime()
	if err := svc.StopCallRecorder(); err != nil {
		t.Fatal(err)
	}
	unpublished, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	resultUnavailable, err := unpublished.DisableMemberKey(ctx, writer.User.ID, subject.User.ID, replacementBefore.ID, target.ETag, service.MemberKeyDisableInput{Reason: "Restart reconciliation"})
	if err == nil || resultUnavailable != nil {
		t.Fatal("missing runtime publication claimed disabled application")
	}
	var httpErr *apperrors.Error
	if !errors.As(err, &httpErr) || httpErr.Code != 503 {
		t.Fatal("committed unpublished disable did not remain uncertain", err)
	}
	svc, err = service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.StartCallRecorder(ctx, recorderPath); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	current, err := svc.DisableMemberKey(ctx, writer.User.ID, subject.User.ID, replacementBefore.ID, target.ETag, service.MemberKeyDisableInput{Reason: "Restart reconciliation"})
	if err != nil || current == nil || !current.RuntimeApplied || current.Confirmation != "current_disabled_state" {
		t.Fatal("restart did not reconcile exact current disabled state", current, err)
	}
	if err := db.First(&projectKey, "id = ?", projectKey.ID).Error; err != nil || !reflect.DeepEqual(projectKey, projectBefore) {
		t.Fatal("Personal disable changed retained Project Key", err)
	}
	_ = subjectCSRF // The owner Session is never used as the administrator actor.
}

func TestMemberKeyLifecycleFixtureRemainsExplicitlyHarnessOwned(t *testing.T) {
	if reflect.ValueOf(testMemberKeysLifecycle).IsNil() {
		t.Fatal("missing future dual-database acceptance fixture")
	}
}
