package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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

// testResourceLimitLifecycle runs only under the isolated dual-database owner.
func testResourceLimitLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	store, err := secretstore.New(bytes.Repeat([]byte{61}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var dispatches atomic.Int32
	var hold atomic.Bool
	var streamMode atomic.Bool
	streamCanceled := make(chan struct{}, 1)
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseUpstream := func() { releaseOnce.Do(func() { close(release) }) }
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dispatches.Add(1)
		if streamMode.Load() {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"model\":\"limit-upstream\",\"choices\":[]}\n\n")
			w.(http.Flusher).Flush()
			entered <- struct{}{}
			<-r.Context().Done()
			streamCanceled <- struct{}{}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if hold.Load() {
			entered <- struct{}{}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		_, _ = io.WriteString(w, `{"model":"limit-upstream","choices":[],"usage":{"prompt_tokens":2,"completion_tokens":1}}`)
	}))
	defer upstream.Close()
	defer releaseUpstream()
	makeService := func() *service.Service {
		svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
		if err != nil {
			t.Fatal(err)
		}
		return svc
	}
	svc := makeService()
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"limits@example.invalid","password":"test-only-limit-password","name":"Limit admin"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, cookie := readIdentity(t, setup)
	member, err := svc.CreateMember(ctx, admin.User.ID, "outsider@example.invalid", "test-only-limit-password", "Outsider", "member")
	if err != nil {
		t.Fatal(err)
	}
	_ = member
	ciphertext, err := store.Seal("crd_limits", "test-only-provider")
	if err != nil {
		t.Fatal(err)
	}
	modelID := "mdl_limits"
	personalBearer := "rx_" + strings.Repeat("l", 43)
	projectBearer := "rxp_" + strings.Repeat("p", 43)
	for _, row := range []any{
		&entity.Provider{ID: "prv_limits", Name: "Limit provider"},
		&entity.ProviderConnection{ID: "con_limits", ProviderID: "prv_limits", Name: "Connection", BaseURL: upstream.URL + "/v1", Protocol: entity.ProtocolOpenAIChat},
		&entity.ProviderCredential{ID: "crd_limits", ConnectionID: "con_limits", Name: "Credential", Ciphertext: ciphertext, Enabled: true, VerificationStatus: "verified"},
		&entity.ProviderModel{ID: "pmd_limits", ConnectionID: "con_limits", UpstreamName: "limit-upstream"},
		&entity.CredentialModelAccess{CredentialID: "crd_limits", ProviderModelID: "pmd_limits"},
		&entity.Model{ID: modelID, Status: "active"}, &entity.ModelName{Name: "limit-model", ModelID: modelID, CurrentModelID: &modelID},
		&entity.ModelProviderBinding{ID: "bnd_limits", ModelID: modelID, ProviderModelID: "pmd_limits", Weight: 100},
		&entity.UserModelGrant{UserID: admin.User.ID, ModelID: modelID},
		&entity.APIKey{ID: "key_limits", UserID: admin.User.ID, Name: "Personal", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(personalBearer), Status: entity.KeyActive},
		&entity.APIKeyModel{KeyID: "key_limits", ModelID: modelID},
		&entity.Project{ID: "prj_limits", Name: "Limits project", Status: entity.ResourceActive, CreatorID: admin.User.ID},
		&entity.ProjectManager{ID: "pmg_limits", ProjectID: "prj_limits", UserID: admin.User.ID},
		&entity.ProjectModelGrant{ProjectID: "prj_limits", ModelID: modelID},
		&entity.ProjectKey{ID: "pky_limits", ProjectID: "prj_limits", CreatorID: admin.User.ID, Name: "Project key", Prefix: "rxp_masked", TokenHash: secret.SHA256Hex(projectBearer), Status: entity.KeyActive, DeliveryMode: "manual"},
		&entity.ProjectKeyModel{KeyID: "pky_limits", ModelID: modelID},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	spool := filepath.Join(t.TempDir(), "limits.db")
	if err := svc.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	defer func() { svc.StopRuntime(); _ = svc.StopCallRecorder() }()
	request := func(method, path string, body any, etag string) *httptest.ResponseRecorder {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, "http://routex.test"+path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", admin.CSRFToken)
		req.AddCookie(cookie)
		if etag != "" {
			req.Header.Set("If-Match", `"`+etag+`"`)
		}
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	userPath := "/api/v1/admin/members/" + admin.User.ID + "/limits"
	keyPath := "/api/v1/keys/key_limits/limits"
	projectPath := "/api/v1/projects/prj_limits/limits"
	projectKeyPath := "/api/v1/projects/prj_limits/keys/pky_limits/limits"
	read := func(path string) service.LimitRecord {
		return decodeCatalogResponse[service.LimitRecord](t, request("GET", path, nil, ""), 200)
	}
	write := func(path string, rpm, concurrency any, mode string, ranges []string) service.LimitRecord {
		before := read(path)
		body := map[string]any{"rpm": rpm, "concurrency": concurrency, "ip_mode": mode, "ip_ranges": ranges, "reason": "Controlled acceptance"}
		result := decodeCatalogResponse[service.LimitRecord](t, request("PUT", path, body, before.ETag), 200)
		if !result.Enforced || result.ETag == before.ETag {
			t.Fatal("successful policy write not published")
		}
		return result
	}
	gateway := func(bearer, remote, forwarded string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"limit-model","messages":[{"role":"user","content":"hello"}]}`))
		req.RemoteAddr = remote
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+bearer)
		req.Header.Set("X-Forwarded-For", forwarded)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	call := func(bearer string) *httptest.ResponseRecorder { return gateway(bearer, "192.0.2.8:4040", "") }
	empty := read(userPath)
	if empty.ETag != "0" || empty.Stored.RPM != nil {
		t.Fatal("existing account was restricted by migration")
	}
	expectStatus(t, identityRequest(router, "PUT", userPath, `{"rpm":0,"reason":"No CSRF"}`, cookie, ""), 403)
	expectStatus(t, request("PUT", userPath, map[string]any{"rpm": 1, "reason": "No ETag"}, ""), 400)
	expectStatus(t, request("PUT", userPath, map[string]any{"tokens_week": 100, "reason": "Unsupported"}, "0"), 400)
	write(userPath, 2, nil, "none", []string{})
	expectStatus(t, request("PUT", userPath, map[string]any{"rpm": 3, "reason": "Stale"}, "0"), 409)
	child := write(keyPath, 1, nil, "none", []string{})
	expectStatus(t, request("PUT", keyPath, map[string]any{"rpm": 3, "ip_mode": "none", "ip_ranges": []string{}, "reason": "Broader"}, child.ETag), 400)
	expectStatus(t, call(personalBearer), 200)
	beforeReject := dispatches.Load()
	expectStatus(t, call(personalBearer), 429)
	rotated, err := svc.RotatePersonalKey(ctx, admin.User.ID, "key_limits")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConfirmKeyDelivery(ctx, admin.User.ID, rotated.Record.Key.ID); err != nil {
		t.Fatal(err)
	}
	rotatedPath := "/api/v1/keys/" + rotated.Record.Key.ID + "/limits"
	if read(rotatedPath).AccountID != child.AccountID {
		t.Fatal("rotation changed quota account")
	}
	expectStatus(t, call(rotated.Secret), 429)
	if dispatches.Load() != beforeReject {
		t.Fatal("rejected request reached upstream")
	}
	write(rotatedPath, nil, nil, "none", []string{})
	expectStatus(t, call(rotated.Secret), 200)
	expectStatus(t, call(personalBearer), 429)
	write(projectPath, 1, nil, "none", []string{})
	expectStatus(t, call(projectBearer), 200)
	expectStatus(t, call(projectBearer), 429)
	projectRotation, err := svc.RotateProjectKey(ctx, admin.User.ID, "prj_limits", "pky_limits", "manual")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConfirmProjectKey(ctx, admin.User.ID, "prj_limits", projectRotation.Record.Key.ID); err != nil {
		t.Fatal(err)
	}
	if read("/api/v1/projects/prj_limits/keys/"+projectRotation.Record.Key.ID+"/limits").AccountID != read(projectKeyPath).AccountID {
		t.Fatal("Project rotation changed account")
	}
	expectStatus(t, call(projectRotation.Secret), 429)
	// SQL acknowledgment cannot reset local rolling counters; neither can restart.
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	svc.StopRuntime()
	if err := svc.StopCallRecorder(); err != nil {
		t.Fatal(err)
	}
	// Simulate a crash after filesystem binding and before SQL initialization
	// acknowledgment: the identity is already committed and must remain stable.
	var binding entity.LimitInstallation
	if err := db.First(&binding, 1).Error; err != nil || binding.JournalID == "" {
		t.Fatal("journal identity was not durably initialized", err)
	}
	if err := db.Model(&entity.LimitInstallation{}).Where("id = ?", 1).Update("Initialized", false).Error; err != nil {
		t.Fatal(err)
	}
	svc = makeService()
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	var recoveredBinding entity.LimitInstallation
	if err := db.First(&recoveredBinding, 1).Error; err != nil || !recoveredBinding.Initialized || recoveredBinding.JournalID != binding.JournalID {
		t.Fatal("interrupted initialization did not preserve durable identity", err)
	}
	router = fox.New()
	New(svc).RegisterRoutes(router)
	expectStatus(t, call(personalBearer), 429)
	expectStatus(t, call(projectBearer), 429)
	other := makeService()
	if err := other.StartCallRecorder(ctx, filepath.Join(t.TempDir(), "foreign.db")); err == nil {
		_ = other.StopCallRecorder()
		t.Fatal("missing established journal silently reset history")
	}
	write(userPath, nil, 1, "none", []string{})
	write(projectPath, nil, 1, "none", []string{})
	hold.Store(true)
	first := make(chan *httptest.ResponseRecorder, 1)
	go func() { first <- call(personalBearer) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("first request did not enter upstream")
	}
	hold.Store(false)
	expectStatus(t, call(rotated.Secret), 429)
	expectStatus(t, call(projectBearer), 200)
	releaseUpstream()
	expectStatus(t, <-first, 200)
	expectStatus(t, call(rotated.Secret), 200)
	// A streaming operation holds its lease until cancellation closes upstream.
	streamMode.Store(true)
	streamCtx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()
	streamRequest := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"limit-model","stream":true,"messages":[{"role":"user","content":"hello"}]}`)).WithContext(streamCtx)
	streamRequest.Header.Set("Content-Type", "application/json")
	streamRequest.Header.Set("Authorization", "Bearer "+personalBearer)
	streamDone := make(chan struct{})
	go func() { router.ServeHTTP(httptest.NewRecorder(), streamRequest); close(streamDone) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("stream never entered upstream")
	}
	expectStatus(t, call(rotated.Secret), 429)
	cancelStream()
	select {
	case <-streamCanceled:
	case <-time.After(3 * time.Second):
		t.Fatal("canceled stream retained upstream work")
	}
	select {
	case <-streamDone:
	case <-time.After(3 * time.Second):
		t.Fatal("canceled stream did not settle lease")
	}
	streamMode.Store(false)
	expectStatus(t, call(rotated.Secret), 200)
	write(userPath, nil, nil, "allowlist", []string{"192.0.2.0/24"})
	write(keyPath, nil, nil, "denylist", []string{"192.0.2.7"})
	beforeReject = dispatches.Load()
	expectStatus(t, gateway(personalBearer, "198.51.100.8:4040", "192.0.2.8"), 403)
	expectStatus(t, gateway(personalBearer, "192.0.2.7:4040", ""), 403)
	if dispatches.Load() != beforeReject {
		t.Fatal("IP rejection dispatched")
	}
	expectStatus(t, call(personalBearer), 200)
	write(userPath, 0, nil, "none", []string{})
	beforeReject = dispatches.Load()
	for range 5 {
		expectStatus(t, call(rotated.Secret), 429)
	}
	if dispatches.Load() != beforeReject {
		t.Fatal("acknowledged reduction admitted stale policy")
	}
	// A member cannot edit aggregate policies or another member's Key limits.
	login := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"outsider@example.invalid","password":"test-only-limit-password"}`, nil, "")
	expectStatus(t, login, 200)
	outsider, outsiderCookie := readIdentity(t, login)
	expectStatus(t, identityRequest(router, "GET", keyPath, "", outsiderCookie, ""), 404)
	req := httptest.NewRequest("PUT", "http://routex.test"+userPath, strings.NewReader(`{"rpm":99,"reason":"Unauthorized"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", `"`+read(userPath).ETag+`"`)
	req.Header.Set("X-CSRF-Token", outsider.CSRFToken)
	req.AddCookie(outsiderCookie)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	expectStatus(t, res, 403)
	var audits int64
	if err := db.Model(&entity.AuditEvent{}).Where("action = ?", "limits.update").Count(&audits).Error; err != nil || audits < 8 {
		t.Fatal("policy audit missing", err)
	}
}
