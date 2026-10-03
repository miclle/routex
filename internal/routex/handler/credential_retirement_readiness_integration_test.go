package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
)

func testCredentialRetirementReadinessLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	store, err := secretstore.New(bytes.Repeat([]byte{83}, 32))
	if err != nil {
		t.Fatal(err)
	}
	completed := chatCompletionFixture("stop", `{"role":"assistant","content":"Ready"}`, `{"prompt_tokens":1,"completion_tokens":2}`, false, 0)
	var responseBody, inferenceAuthorization atomic.Value
	responseBody.Store(completed)
	var requestCount atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/v1/models" {
			_, _ = w.Write([]byte(`{"data":[{"id":"readiness-upstream"}]}`))
			return
		}
		inferenceAuthorization.Store(r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(responseBody.Load().(string)))
	}))
	defer upstream.Close()
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"readiness@example.invalid","password":"test-only-readiness-password","name":"Readiness admin"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	_, readCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "readiness-reader", []string{"providers.read"})
	_, writeCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "readiness-writer", []string{"providers.write"})
	provider, err := svc.CreateProvider(ctx, admin.User.ID, "Readiness provider", service.CreateConnectionInput{Name: "Readiness connection", BaseURL: upstream.URL + "/v1", Protocol: entity.ProtocolOpenAIChat, CredentialName: "Source", Secret: "test-only-readiness-source-secret"})
	if err != nil {
		t.Fatal(err)
	}
	source := provider.Connections[0].Credentials[0]
	connection := provider.Connections[0].Connection
	if verified, err := svc.VerifyCredential(ctx, admin.User.ID, source.ID); err != nil || !verified.Verified {
		t.Fatal("controlled source discovery failed")
	}
	if _, err := svc.SetCredentialEnabled(ctx, admin.User.ID, source.ID, true); err != nil {
		t.Fatal(err)
	}
	metadata, err := svc.GetCredentialMetadata(ctx, admin.User.ID, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	priority := 13
	metadata, err = svc.WriteCredentialMetadata(ctx, admin.User.ID, source.ID, metadata.ETag, service.CredentialMetadataInput{Name: source.Name, Priority: &priority, Reason: "Reviewed source priority"})
	if err != nil {
		t.Fatal(err)
	}
	var access entity.CredentialModelAccess
	if err := db.First(&access, "credential_id = ?", source.ID).Error; err != nil {
		t.Fatal(err)
	}
	model, err := svc.CreateModel(ctx, admin.User.ID, "readiness-model", access.ProviderModelID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetModelWeights(ctx, admin.User.ID, model.Model.ID, []service.ModelWeight{{BindingID: model.Bindings[0].Binding.ID, Weight: 100}}); err != nil {
		t.Fatal(err)
	}
	bearer := "rx_" + strings.Repeat("m", 43)
	for _, row := range []any{&entity.APIKey{ID: "key_readiness", UserID: admin.User.ID, Name: "Readiness Key", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(bearer), Status: entity.KeyActive}, &entity.APIKeyModel{KeyID: "key_readiness", ModelID: model.Model.ID}} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	defer svc.StopRuntime()
	post := func(path string, input any, etag string) *httptest.ResponseRecorder {
		raw, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("POST", "http://routex.test"+path, bytes.NewReader(raw))
		req.AddCookie(adminCookie)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", admin.CSRFToken)
		if etag != "" {
			req.Header.Set("If-Match", strconv.Quote(etag))
		}
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	prepared := decodeCatalogResponse[service.CredentialReplacementRecord](t, post("/api/v1/admin/credentials/"+source.ID+"/replacements", service.CredentialReplacementInput{RequestID: "81b043bb-cb96-419c-b9a2-cab04814b65e", Name: "Replacement", Secret: "test-only-readiness-replacement-secret", Reason: "Reviewed preparation"}, metadata.ETag), 201)
	path := "/api/v1/admin/credentials/" + source.ID + "/retirement-readiness?replacement_credential_id=" + prepared.ID
	read := func(cookie *http.Cookie) service.CredentialRetirementReadiness {
		t.Helper()
		res := identityRequest(router, "GET", path, "", cookie, "")
		result := decodeCatalogResponse[service.CredentialRetirementReadiness](t, res, 200)
		var shape map[string]json.RawMessage
		if err := json.Unmarshal(res.Body.Bytes(), &shape); err != nil || len(shape) != 8 || result.Source.ID != source.ID || result.Replacement.ID != prepared.ID || len(result.ETag) != 64 || res.Header().Get("ETag") != strconv.Quote(result.ETag) {
			t.Fatal("unsafe/readiness wire or validator shape")
		}
		for _, forbidden := range []string{"test-only-readiness-source-secret", "test-only-readiness-replacement-secret", "ciphertext", "request_id", "user_id", "key_id", "usage", "token", "scope_digest", "source_digest", "replaces_credential_id"} {
			if strings.Contains(res.Body.String(), forbidden) {
				t.Fatalf("readiness leaked %s", forbidden)
			}
		}
		return result
	}
	// Advisory reads intentionally fail closed when publication is busy or changes
	// across their transaction. Retry only these transient reads, never inference
	// or mutations; domain blockers and missing completion evidence still fail
	// immediately. Explicit contention tests below retain their original behavior.
	readCoherent := func(cookie *http.Cookie) service.CredentialRetirementReadiness {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			result := read(cookie)
			transient := slices.Contains(result.Blockers, "runtime_unavailable") || slices.Contains(result.Blockers, "runtime_stale")
			if !transient || !time.Now().Before(deadline) {
				return result
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	expectStatus(t, identityRequest(router, "GET", path, "", nil, ""), 401)
	expectStatus(t, identityRequest(router, "GET", path, "", writeCookie, ""), 403)
	pending := read(readCookie)
	if pending.Eligible || pending.Evidence != nil || !slices.Contains(pending.Blockers, "replacement_disabled") || !slices.Contains(pending.Blockers, "replacement_unverified") {
		t.Fatal("pending preparation claimed retirement readiness")
	}
	for _, query := range []string{"", "?replacement_credential_id=", "?replacement_credential_id=" + prepared.ID + "&replacement_credential_id=" + prepared.ID, "?replacement_credential_id=" + prepared.ID + "&unknown=1", "?replacement_credential_id=%ZZ", "?replacement_credential_id=" + strings.Repeat("a", 257), "?replacement_credential_id=" + source.ID} {
		expectStatus(t, identityRequest(router, "GET", "/api/v1/admin/credentials/"+source.ID+"/retirement-readiness"+query, "", readCookie, ""), 400)
	}
	expectStatus(t, identityRequest(router, "GET", "/api/v1/admin/credentials/crd_00000000000000000000000000/retirement-readiness?replacement_credential_id="+prepared.ID, "", readCookie, ""), 404)
	unrelated, err := svc.CreateCredential(ctx, admin.User.ID, connection.ID, "Unrelated", "test-only-unrelated-secret", 20)
	if err != nil {
		t.Fatal(err)
	}
	expectStatus(t, identityRequest(router, "GET", "/api/v1/admin/credentials/"+source.ID+"/retirement-readiness?replacement_credential_id="+unrelated.ID, "", readCookie, ""), 409)
	expectStatus(t, post("/api/v1/admin/credentials/"+prepared.ID+"/verify", nil, ""), 200)
	toggle := identityRequest(router, "PATCH", "/api/v1/admin/credentials/"+prepared.ID, `{"enabled":true}`, adminCookie, admin.CSRFToken)
	expectStatus(t, toggle, 200)
	replacementMetadata, err := svc.GetCredentialMetadata(ctx, admin.User.ID, prepared.ID)
	if err != nil {
		t.Fatal(err)
	}
	priority = 0
	if _, err := svc.WriteCredentialMetadata(ctx, admin.User.ID, prepared.ID, replacementMetadata.ETag, service.CredentialMetadataInput{Name: replacementMetadata.Name, Priority: &priority, Reason: "Route reviewed replacement for actual inference"}); err != nil {
		t.Fatal(err)
	}
	withoutProof := readCoherent(readCookie)
	if withoutProof.Eligible || withoutProof.Evidence != nil || !slices.Contains(withoutProof.Blockers, "evidence_missing") {
		t.Fatal("verification/enablement alone became completed inference")
	}
	call := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "http://routex.test/v1/chat/completions", strings.NewReader(`{"model":"readiness-model","messages":[{"role":"user","content":"Review actual inference"}]}`))
		req.Header.Set("Authorization", "Bearer "+bearer)
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		expectStatus(t, res, 200)
		if inferenceAuthorization.Load() != "Bearer test-only-readiness-replacement-secret" {
			t.Fatal("inference did not use actual replacement")
		}
		return res
	}
	for _, body := range []string{`{"choices":[],"usage":{"prompt_tokens":1,"completion_tokens":2}}`, chatCompletionFixture("content_filter", `{"role":"assistant","content":"Declined"}`, "null", false, 0), chatCompletionFixture("tool_calls", `{"role":"assistant","content":null,"tool_calls":[{"type":"function","function":{"name":"lookup","arguments":"{}"}}]}`, "null", false, 0), chatCompletionFixture("length", `{"role":"assistant","content":"Partial"}`, "null", false, 0)} {
		responseBody.Store(body)
		call()
		if status := read(readCookie); status.Eligible || status.Evidence != nil {
			t.Fatal("weak/blocked/tool/truncated HTTP200 became readiness proof")
		}
	}
	responseBody.Store(completed)
	actual := call()
	ready := readCoherent(readCookie)
	if !ready.Eligible || ready.Evidence == nil || ready.SnapshotID == nil || len(ready.Blockers) != 0 || ready.EligibleRouteCount != 1 {
		t.Fatalf("actual completed replacement inference not eligible: %+v", ready)
	}
	var recorded entity.CallAttempt
	if err := db.First(&recorded, "request_id = ?", actual.Header().Get("X-Request-ID")).Error; err != nil || ready.Evidence.AttemptID != recorded.ID || recorded.CredentialID != prepared.ID || recorded.SnapshotID != *ready.SnapshotID || recorded.NativeCompletionEvidence != "completed" {
		t.Fatal("readiness selected another credential/configuration/proof")
	}
	var auditsBefore, auditsAfter int64
	if err := db.Model(&entity.AuditEvent{}).Count(&auditsBefore).Error; err != nil {
		t.Fatal(err)
	}
	requestsBefore := requestCount.Load()
	for range 3 {
		read(readCookie)
	}
	if err := db.Model(&entity.AuditEvent{}).Count(&auditsAfter).Error; err != nil || auditsBefore != auditsAfter || requestsBefore != requestCount.Load() {
		t.Fatal("readiness mutated audit or contacted upstream")
	}
	var sourceAfter, replacementAfter entity.ProviderCredential
	if err := db.First(&sourceAfter, "id = ?", source.ID).Error; err != nil || !sourceAfter.Enabled {
		t.Fatal("readiness disabled source")
	}
	if err := db.First(&replacementAfter, "id = ?", prepared.ID).Error; err != nil || !replacementAfter.Enabled || replacementAfter.VerificationStatus != "verified" {
		t.Fatal("readiness mutated replacement lifecycle")
	}
	// One connection plus a concurrent publisher exposes DB/runtime lock inversion.
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	failures := make(chan error, 4)
	wg.Go(func() { failures <- svc.RefreshRuntime(bounded) })
	for range 3 {
		wg.Go(func() {
			_, err := svc.GetCredentialRetirementReadiness(bounded, admin.User.ID, source.ID, prepared.ID)
			failures <- err
		})
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatalf("single-pool readiness/publication failed: %v", err)
		}
	}
	pool.SetMaxOpenConns(10)
	pool.SetMaxIdleConns(10)
	// Freeze the refresh ticker; retained immutable publications still have
	// their current lease and can be explicitly republished for these fixtures.
	svc.StopRuntime()
	if err := db.Model(&entity.ModelProviderBinding{}).Where("id = ?", model.Bindings[0].Binding.ID).Update("weight", 90).Error; err != nil {
		t.Fatal(err)
	}
	stale := read(readCookie)
	if stale.Eligible || stale.Evidence != nil || stale.SnapshotID != nil || !slices.Contains(stale.Blockers, "runtime_stale") {
		t.Fatal("commit-before-publication DB scope was treated as coherent")
	}
	if err := db.Model(&entity.ModelProviderBinding{}).Where("id = ?", model.Bindings[0].Binding.ID).Update("weight", 100).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("credential_id = ? AND provider_model_id = ?", prepared.ID, access.ProviderModelID).Delete(&entity.CredentialModelAccess{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	if status := read(readCookie); status.Eligible || !slices.Contains(status.Blockers, "coverage_missing") {
		t.Fatal("missing replacement model coverage was ignored")
	}
	if err := db.Create(&entity.CredentialModelAccess{CredentialID: prepared.ID, ProviderModelID: access.ProviderModelID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	if status := read(readCookie); status.Eligible || !slices.Contains(status.Blockers, "evidence_missing") {
		t.Fatal("proof from a different configuration qualified")
	}
	call()
	if status := read(readCookie); !status.Eligible {
		t.Fatal("fresh current configuration proof did not reconcile")
	}
	svc.InvalidateRuntimeCredential(prepared.ID)
	if status := read(readCookie); status.Eligible || status.Evidence != nil || !slices.Contains(status.Blockers, "route_unavailable") {
		t.Fatal("credential tombstone was ignored")
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	fresh, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	unavailable, err := fresh.GetCredentialRetirementReadiness(ctx, admin.User.ID, source.ID, prepared.ID)
	if err != nil || unavailable.Eligible || unavailable.SnapshotID != nil || unavailable.Evidence != nil || !slices.Contains(unavailable.Blockers, "runtime_unavailable") {
		t.Fatal("uninitialized runtime claimed readiness")
	}
	// All Connection routes are bounded, even when a Key permits just one model.
	var models []entity.Model
	var bindings []entity.ModelProviderBinding
	for index := range 257 {
		id := fmt.Sprintf("mdl_readiness_overflow_%03d", index)
		models = append(models, entity.Model{ID: id, Status: "active"})
		bindings = append(bindings, entity.ModelProviderBinding{ID: fmt.Sprintf("bnd_readiness_overflow_%03d", index), ModelID: id, ProviderModelID: access.ProviderModelID, Weight: 100})
	}
	if err := db.Create(&models).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&bindings).Error; err != nil {
		t.Fatal(err)
	}
	if status := read(readCookie); status.Eligible || status.Evidence != nil || !slices.Contains(status.Blockers, "scope_overflow") {
		t.Fatal("Connection route overflow was silently truncated")
	}
}
