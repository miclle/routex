package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

// The sole real-driver lifecycle harness owns execution. All discovery/coverage
// rows below are explicit controlled persisted fixtures, not external readiness.
func testModelCreationBatchLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	var dispatches atomic.Int32
	var hold atomic.Bool
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var releaseOnce sync.Once
	var dispatchMu sync.Mutex
	upstreamModels := []string{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		dispatches.Add(1)
		var input map[string]any
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error("controlled native payload", err)
			w.WriteHeader(400)
			return
		}
		model, _ := input["model"].(string)
		dispatchMu.Lock()
		upstreamModels = append(upstreamModels, model)
		dispatchMu.Unlock()
		if model == "batch-primary" && hold.Load() {
			entered <- struct{}{}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/chat/completions":
			if r.Header.Get("Authorization") == "" {
				t.Error("Chat credential missing")
			}
			_, _ = io.WriteString(w, modelCreationBatchNativeBody(entity.ProtocolOpenAIChat))
		case "/v1/responses":
			if r.Header.Get("Authorization") == "" {
				t.Error("Responses credential missing")
			}
			_, _ = io.WriteString(w, modelCreationBatchNativeBody(entity.ProtocolOpenAIResponses))
		case "/v1/messages":
			if r.Header.Get("x-api-key") == "" {
				t.Error("Messages credential missing")
			}
			_, _ = io.WriteString(w, modelCreationBatchNativeBody(entity.ProtocolAnthropicMessages))
		default:
			if !strings.HasPrefix(r.URL.Path, "/v1beta/models/") || !strings.HasSuffix(r.URL.Path, ":generateContent") || r.Header.Get("x-goog-api-key") == "" {
				t.Error("unexpected Gemini route/auth", r.URL.Path)
				w.WriteHeader(400)
				return
			}
			_, _ = io.WriteString(w, modelCreationBatchNativeBody(entity.ProtocolGeminiGenerateContent))
		}
	}))
	defer func() { releaseOnce.Do(func() { close(release) }); upstream.Close() }()
	store, err := secretstore.New(bytes.Repeat([]byte{153}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var auditFailure, armPublication, publicationFailure atomic.Bool
	const auditCallback, publicationCallback = "test:model-batch-audit", "test:model-batch-publication"
	if err := db.Callback().Create().Before("gorm:create").Register(auditCallback, func(tx *gorm.DB) {
		row, ok := tx.Statement.Dest.(*entity.AuditEvent)
		if !ok || row.Action != "model.batch_create" {
			return
		}
		if auditFailure.Load() {
			_ = tx.AddError(errors.New("controlled batch audit failure"))
			return
		}
		if armPublication.Load() {
			publicationFailure.Store(true)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Query().Before("gorm:query").Register(publicationCallback, func(tx *gorm.DB) {
		if publicationFailure.Load() && tx.Statement.Table == "api_keys" {
			_ = tx.AddError(errors.New("controlled runtime publication outage"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, item := range []struct {
			name   string
			remove func(string) error
		}{{auditCallback, db.Callback().Create().Remove}, {publicationCallback, db.Callback().Query().Remove}} {
			if err := item.remove(item.name); err != nil {
				t.Error(err)
			}
		}
	}()
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"model-batch-admin@example.invalid","password":"model-batch-password","name":"Batch administrator"}`, nil, "")
	admin, cookie := readIdentity(t, setup)
	expectStatus(t, setup, 201)
	request := func(authCookie *http.Cookie, csrf, method, path string, body any, etag string) *httptest.ResponseRecorder {
		t.Helper()
		raw := ""
		if body != nil {
			encoded, e := json.Marshal(body)
			if e != nil {
				t.Fatal(e)
			}
			raw = string(encoded)
		}
		req := httptest.NewRequest(method, "http://routex.test"+path, strings.NewReader(raw))
		if raw != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if authCookie != nil {
			req.AddCookie(authCookie)
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		if etag != "" {
			req.Header.Set("If-Match", `"`+etag+`"`)
		}
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	send := func(method, path string, body any, etag string) *httptest.ResponseRecorder {
		return request(cookie, admin.CSRFToken, method, path, body, etag)
	}
	reader, readCookie, readCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "batch-reader", []string{"models.read_all", "providers.read"})
	writer, writeCookie, writeCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "batch-writer", []string{"models.write"})
	_, modelCookie, modelCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "batch-model-read", []string{"models.read_all"})
	_, providerCookie, providerCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "batch-provider-read", []string{"providers.read"})
	_, foreignCookie, foreignCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "batch-foreign", []string{"models.read_all", "providers.read", "models.write"})
	create := func(rows ...any) {
		t.Helper()
		for _, row := range rows {
			if err := db.Create(row).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	const connectionID = "con_batch_Main"
	protocols := []string{entity.ProtocolOpenAIChat, entity.ProtocolOpenAIResponses, entity.ProtocolAnthropicMessages, entity.ProtocolGeminiGenerateContent}
	create(&entity.Provider{ID: "prv_batch_main", Name: "Batch supplier"}, &entity.Provider{ID: "prv_batch_old", Name: "Original supplier"})
	seedConnection := func(id, providerID, protocol string, models ...string) {
		t.Helper()
		base := upstream.URL + "/v1"
		if protocol == entity.ProtocolGeminiGenerateContent {
			base = upstream.URL + "/v1beta"
		}
		create(&entity.ProviderConnection{ID: id, ProviderID: providerID, Name: "Controlled " + protocol, BaseURL: base, Protocol: protocol, EgressMode: "direct"})
		credentialID := "crd_" + strings.TrimPrefix(id, "con_")
		cipher, e := store.Seal(credentialID, "test-only-batch-secret")
		if e != nil {
			t.Fatal(e)
		}
		create(&entity.ProviderCredential{ID: credentialID, ConnectionID: id, Name: "Controlled verified coverage", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified"})
		for _, modelID := range models {
			create(&entity.ProviderModel{ID: modelID, ConnectionID: id, UpstreamName: "upstream-" + modelID}, &entity.CredentialModelAccess{CredentialID: credentialID, ProviderModelID: modelID})
		}
	}
	seedConnection(connectionID, "prv_batch_main", protocols[0], "pmd_batch_A", "pmd_batch_B", "pmd_batch_C", "pmd_batch_D", "pmd_batch_E", "pmd_batch_F", "pmd_batch_G", "pmd_batch_H", "pmd_batch_I")
	create(&entity.ProviderModel{ID: "pmd_batch_literal", ConnectionID: connectionID, UpstreamName: "literal%_marker"}, &entity.CredentialModelAccess{CredentialID: "crd_batch_Main", ProviderModelID: "pmd_batch_literal"})
	seedConnection("con_batch_old", "prv_batch_old", protocols[0], "pmd_batch_old")
	seedConnection("con_batch_response_old", "prv_batch_old", protocols[1], "pmd_batch_response_old")
	for index, protocol := range protocols[1:] {
		seedConnection(fmt.Sprintf("con_batch_native_%d", index), "prv_batch_main", protocol, fmt.Sprintf("pmd_batch_native_%d", index))
	}
	if err := db.Model(&entity.ProviderModel{}).Where("id = ?", "pmd_batch_old").Update("upstream_name", "batch-primary").Error; err != nil {
		t.Fatal(err)
	}
	seedModel := func(id, name, pmID, binding string, weight int) {
		create(&entity.Model{ID: id, Status: entity.ResourceActive}, &entity.ModelName{Name: name, ModelID: id, CurrentModelID: &id}, &entity.ModelProviderBinding{ID: binding, ModelID: id, ProviderModelID: pmID, Weight: weight})
	}
	seedModel("mdl_batch_existing", "batch-existing", "pmd_batch_old", "bnd_batch_old", 100)
	seedModel("mdl_batch_first", "batch-first", "pmd_batch_response_old", "bnd_batch_response_old", 100)
	seedModel("mdl_batch_zero", "batch-zero", "pmd_batch_old", "bnd_batch_zero", 0)
	seedModel("mdl_batch_duplicate", "batch-duplicate", "pmd_batch_I", "bnd_batch_duplicate", 100)
	create(&entity.UserModelGrant{UserID: admin.User.ID, ModelID: "mdl_batch_existing"})
	oldKey, err := svc.CreatePersonalKey(ctx, admin.User.ID, "Original immutable ceiling", []string{"mdl_batch_existing"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConfirmKeyDelivery(ctx, admin.User.ID, oldKey.Record.Key.ID); err != nil {
		t.Fatal(err)
	}
	queuePath := filepath.Join(t.TempDir(), "model-batch-calls.db")
	if err := svc.StartCallRecorder(ctx, queuePath); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		svc.StopRuntime()
		if err := svc.StopCallRecorder(); err != nil {
			t.Error(err)
		}
	}()
	waitApplied := func(requestID string) service.ModelCreationBatchResult {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		var value service.ModelCreationBatchResult
		for time.Now().Before(deadline) {
			value = decodeCatalogResponse[service.ModelCreationBatchResult](t, send("GET", "/api/v1/admin/model-creation/receipts/"+requestID, nil, ""), 200)
			if value.RuntimeApplied && value.ApplicationStatus == "applied" {
				return value
			}
			time.Sleep(25 * time.Millisecond)
		}
		t.Fatal("bounded read-only publication proof did not become applied", value.ApplicationStatus)
		return value
	}
	path := "/api/v1/admin/connections/" + connectionID + "/model-creation"
	readPath := "/api/v1/admin/model-creation/connections"
	expectStatus(t, request(nil, "", "GET", readPath, nil, ""), 401)
	for _, auth := range []struct {
		cookie *http.Cookie
		csrf   string
	}{{writeCookie, writeCSRF}, {modelCookie, modelCSRF}, {providerCookie, providerCSRF}} {
		expectStatus(t, request(auth.cookie, auth.csrf, "GET", readPath, nil, ""), 403)
	}
	page := decodeCatalogResponse[service.ModelCreationConnectionPage](t, request(readCookie, readCSRF, "GET", readPath+"?limit=1", nil, ""), 200)
	if len(page.Items) != 1 || page.NextCursor == nil {
		t.Fatal("bounded connection pagination missing", page)
	}
	second := decodeCatalogResponse[service.ModelCreationConnectionPage](t, send("GET", readPath+"?limit=1&cursor="+*page.NextCursor, nil, ""), 200)
	if len(second.Items) != 1 || second.Items[0].ID == page.Items[0].ID {
		t.Fatal("cursor repeated connection", second)
	}
	for _, query := range []string{"?limit=51", "?q=one&q=two", "?actor_id=other", "?cursor=bad", "?limit="} {
		expectStatus(t, send("GET", readPath+query, nil, ""), 400)
	}
	contextView := decodeCatalogResponse[service.ModelCreationContext](t, send("GET", path, nil, ""), 200)
	if contextView.Connection.ID != connectionID || !contextView.CanCreate {
		t.Fatal("exact context missing", contextView)
	}
	if _, err := svc.GetModelCreationContext(ctx, strings.ToUpper(admin.User.ID), connectionID); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("aliased actor borrowed authority", err)
	}
	for _, alias := range []string{strings.ToLower(connectionID), connectionID + "%20"} {
		res := send("GET", "/api/v1/admin/connections/"+alias+"/model-creation", nil, "")
		if res.Code != 400 && res.Code != 404 {
			t.Fatal("aliased Connection borrowed current subject", res.Code)
		}
	}
	candidates := decodeCatalogResponse[service.ModelCreationProviderModelPage](t, send("GET", path+"/provider-models?limit=2", nil, ""), 200)
	if len(candidates.Items) != 2 || candidates.NextCursor == nil {
		t.Fatal("provider model picker unbounded", candidates)
	}
	literal := decodeCatalogResponse[service.ModelCreationProviderModelPage](t, send("GET", path+"/provider-models?q=%25_", nil, ""), 200)
	if len(literal.Items) != 1 || literal.Items[0].ID != "pmd_batch_literal" {
		t.Fatal("literal picker query treated input as a wildcard", literal)
	}
	for _, res := range []*httptest.ResponseRecorder{send("GET", readPath, nil, ""), send("GET", path+"/provider-models", nil, ""), send("GET", path+"/models", nil, "")} {
		expectStatus(t, res, 200)
		for _, private := range []string{"ciphertext", "test-only-batch-secret", "granted_user_ids", "password_hash", "token_hash"} {
			if strings.Contains(res.Body.String(), private) {
				t.Fatal("picker leaked unrelated facts", private)
			}
		}
	}
	items := []service.ModelCreationItem{{ProviderModelID: "pmd_batch_C", Target: "existing", ModelID: "mdl_batch_first"}, {ProviderModelID: "pmd_batch_A", Target: "new", Name: "batch-created"}, {ProviderModelID: "pmd_batch_B", Target: "existing", ModelID: "mdl_batch_existing"}}
	preview := func(rows []service.ModelCreationItem) service.ModelCreationPreview {
		return decodeCatalogResponse[service.ModelCreationPreview](t, send("POST", path+"/preview", service.ModelCreationPreviewInput{Items: rows}, ""), 200)
	}
	reviewed := preview(items)
	if !reviewed.CanCommit || len(reviewed.Items) != 3 || len(reviewed.ReviewETag) != 64 {
		t.Fatal("eligible batch not reviewed", reviewed)
	}
	for _, item := range reviewed.Items {
		want := 100
		if item.ModelID != nil && *item.ModelID == "mdl_batch_existing" {
			want = 0
		}
		if item.InitialWeight != want || len(item.BlockerCodes) != 0 {
			t.Fatal("automatic weights changed topology", item)
		}
	}
	// Temporary supply unavailability does not free a configured 100-weight slot.
	if err := db.Model(&entity.ProviderModel{}).Where("id = ?", "pmd_batch_old").Update("disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	disabledOld := preview([]service.ModelCreationItem{{ProviderModelID: "pmd_batch_B", Target: "existing", ModelID: "mdl_batch_existing"}})
	if !disabledOld.CanCommit || disabledOld.Items[0].InitialWeight != 0 {
		t.Fatal("disabled original100 was silently rebalanced", disabledOld)
	}
	if err := db.Model(&entity.ProviderModel{}).Where("id = ?", "pmd_batch_old").Update("disabled", false).Error; err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"mdl_batch_zero", "mdl_batch_duplicate"} {
		blocked := preview([]service.ModelCreationItem{{ProviderModelID: "pmd_batch_D", Target: "existing", ModelID: target}})
		if blocked.CanCommit || len(blocked.Items[0].BlockerCodes) == 0 {
			t.Fatal("invalid saved topology allowed commit", target, blocked)
		}
	}
	// Invalid exact identities and duplicate rows cannot mutate catalogue.
	for _, rows := range [][]service.ModelCreationItem{{{ProviderModelID: "pmd_batch_a", Target: "new", Name: "batch-alias"}}, {{ProviderModelID: "pmd_batch_response_old", Target: "new", Name: "batch-foreign"}}} {
		expectStatus(t, send("POST", path+"/preview", service.ModelCreationPreviewInput{Items: rows}, ""), 404)
	}
	counts := func() []int64 {
		t.Helper()
		values := []int64{}
		for _, table := range []string{"models", "model_names", "model_provider_bindings", "user_model_grants", "api_key_models", "model_creation_batch_receipts"} {
			var n int64
			if err := db.Table(table).Count(&n).Error; err != nil {
				t.Fatal(err)
			}
			values = append(values, n)
		}
		var n int64
		if err := db.Model(&entity.AuditEvent{}).Where("action = ?", "model.batch_create").Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		values = append(values, n)
		for _, table := range []string{"team_model_grants", "project_model_grants", "project_api_key_models"} {
			var extra int64
			if err := db.Table(table).Count(&extra).Error; err != nil {
				t.Fatal(err)
			}
			values = append(values, extra)
		}
		return values
	}
	before := counts()
	for _, rows := range [][]service.ModelCreationItem{
		{{ProviderModelID: "pmd_batch_A", Target: "new", Name: "duplicate-a"}, {ProviderModelID: "pmd_batch_A", Target: "new", Name: "duplicate-b"}},
		{{ProviderModelID: "pmd_batch_A", Target: "new", Name: "duplicate-name"}, {ProviderModelID: "pmd_batch_B", Target: "new", Name: "duplicate-name"}},
		{{ProviderModelID: "pmd_batch_A", Target: "existing", ModelID: "mdl_batch_existing"}, {ProviderModelID: "pmd_batch_B", Target: "existing", ModelID: "mdl_batch_existing"}},
	} {
		expectStatus(t, send("POST", path+"/preview", service.ModelCreationPreviewInput{Items: rows}, ""), 400)
	}
	reserved := preview([]service.ModelCreationItem{{ProviderModelID: "pmd_batch_A", Target: "new", Name: "batch-existing"}})
	if reserved.CanCommit || !slices.Contains(reserved.Items[0].BlockerCodes, "name_reserved") {
		t.Fatal("reserved exact public name allowed fresh creation", reserved)
	}
	for _, raw := range []string{`{"items":null}`, `{"items":[],"items":[]}`, `{"items":[{"provider_model_id":"pmd_batch_A","target":"new","name":"bad","weight":100}]}`} {
		expectStatus(t, identityRequest(router, "POST", path+"/preview", raw, cookie, admin.CSRFToken), 400)
	}
	if !slices.Equal(counts(), before) {
		t.Fatal("invalid preview wrote state")
	}
	body := service.ModelCreationBatchInput{RequestID: "50000000-1111-4111-8111-111111111111", Reason: "Create explicitly reviewed native topology", Items: items}
	expectStatus(t, request(readCookie, readCSRF, "POST", path, body, reviewed.ReviewETag), 403)
	expectStatus(t, request(cookie, "", "POST", path, body, reviewed.ReviewETag), 403)
	expectStatus(t, send("POST", path, body, ""), 400)
	stale := preview(items)
	if err := db.Model(&entity.ProviderModel{}).Where("id = ?", "pmd_batch_A").Update("disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, send("POST", path, body, stale.ReviewETag), 409)
	if !slices.Equal(counts(), before) {
		t.Fatal("stale preview partially wrote rows")
	}
	if err := db.Model(&entity.ProviderModel{}).Where("id = ?", "pmd_batch_A").Update("disabled", false).Error; err != nil {
		t.Fatal(err)
	}
	reviewed = preview(items)
	auditFailure.Store(true)
	expectStatus(t, send("POST", path, body, reviewed.ReviewETag), 500)
	auditFailure.Store(false)
	if !slices.Equal(counts(), before) {
		t.Fatal("audit failure committed part of batch", counts(), before)
	}
	// Keep one actual attempt entered before the additive batch. Its captured
	// route remains valid; a zero-weight backup receives no dispatch.
	invoke := func(secret, protocol, name string) *httptest.ResponseRecorder {
		target := "/v1/chat/completions"
		payload := map[string]any{"model": name, "stream": false, "messages": []map[string]string{{"role": "user", "content": "Complete controlled native call"}}}
		switch protocol {
		case entity.ProtocolOpenAIResponses:
			target = "/v1/responses"
			payload = map[string]any{"model": name, "input": "Complete controlled native call", "stream": false}
		case entity.ProtocolAnthropicMessages:
			target = "/v1/messages"
			payload["max_tokens"] = 2
		case entity.ProtocolGeminiGenerateContent:
			target = "/v1beta/models/" + name + ":generateContent"
			payload = map[string]any{"contents": []map[string]any{{"role": "user", "parts": []map[string]string{{"text": "Complete controlled native call"}}}}}
		}
		raw, _ := json.Marshal(payload)
		req := httptest.NewRequest("POST", "http://routex.test"+target, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+secret)
		if protocol == entity.ProtocolAnthropicMessages {
			req.Header.Set("anthropic-version", "2023-06-01")
		}
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	hold.Store(true)
	oldCall := make(chan *httptest.ResponseRecorder, 1)
	oldCallDone := false
	defer func() {
		releaseOnce.Do(func() { close(release) })
		if !oldCallDone {
			select {
			case <-oldCall:
			case <-time.After(10 * time.Second):
				t.Error("owned native request did not stop")
			}
		}
	}()
	go func() { oldCall <- invoke(oldKey.Secret, protocols[0], "batch-existing") }()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("original call did not enter native attempt")
	}
	var wg sync.WaitGroup
	responses := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		wg.Go(func() { responses <- send("POST", path, body, reviewed.ReviewETag) })
	}
	wg.Wait()
	close(responses)
	var receipt service.ModelCreationReceipt
	statuses := []int{}
	for res := range responses {
		statuses = append(statuses, res.Code)
		if res.Code != 200 && res.Code != 201 {
			t.Fatal("concurrent original intent", res.Code, res.Body.String())
		}
		result := decodeCatalogResponse[service.ModelCreationBatchResult](t, res, res.Code)
		if !result.Committed || result.RuntimeApplied != (result.ApplicationStatus == "applied") || (result.ApplicationStatus != "applied" && result.ApplicationStatus != "pending") || result.Changed != (res.Code == 201) {
			t.Fatal("commit/application conflated", result)
		}
		if receipt.RequestID == "" {
			receipt = result.Receipt
		} else if !reflect.DeepEqual(receipt, result.Receipt) {
			t.Fatal("concurrent UUID changed receipt")
		}
	}
	slices.Sort(statuses)
	if !slices.Equal(statuses, []int{200, 201}) {
		t.Fatal("same UUID created more than once", statuses)
	}
	releaseOnce.Do(func() { close(release) })
	hold.Store(false)
	select {
	case res := <-oldCall:
		oldCallDone = true
		expectStatus(t, res, 200)
	case <-time.After(10 * time.Second):
		t.Fatal("already entered call did not complete")
	}
	if !reflect.DeepEqual(waitApplied(body.RequestID).Receipt, receipt) {
		t.Fatal("publication wait changed original receipt")
	}
	after := counts()
	if after[0] != before[0]+1 || after[1] != before[1]+1 || after[2] != before[2]+3 || after[3] != before[3] || after[4] != before[4] || after[5] != before[5]+1 || after[6] != before[6]+1 || !slices.Equal(after[7:], before[7:]) {
		t.Fatal("batch not atomic or inserted grant/Key scope", after, before)
	}
	var scopes []entity.APIKeyModel
	if err := db.Where("key_id = ?", oldKey.Record.Key.ID).Find(&scopes).Error; err != nil || len(scopes) != 1 || scopes[0].ModelID != "mdl_batch_existing" {
		t.Fatal("batch expanded original Key ceiling", scopes, err)
	}
	result := decodeCatalogResponse[service.ModelCreationBatchResult](t, send("GET", "/api/v1/admin/model-creation/receipts/"+body.RequestID, nil, ""), 200)
	if !reflect.DeepEqual(result.Receipt, receipt) || result.Changed {
		t.Fatal("read-only receipt changed durable intent", result)
	}
	expectStatus(t, request(foreignCookie, foreignCSRF, "GET", "/api/v1/admin/model-creation/receipts/"+body.RequestID, nil, ""), 404)
	expectStatus(t, request(foreignCookie, foreignCSRF, "POST", path, body, reviewed.ReviewETag), 409)
	changedBody := body
	changedBody.Reason = "Different immutable intent"
	expectStatus(t, send("POST", path, changedBody, reviewed.ReviewETag), 409)
	if !slices.Equal(counts(), after) {
		t.Fatal("receipt replay wrote catalogue or another audit")
	}
	var createdID, backupID string
	for _, item := range receipt.Items {
		if item.CreatedModel {
			createdID = item.ModelID
		}
		if item.ModelID == "mdl_batch_existing" {
			backupID = item.BindingID
			if item.Weight != 0 {
				t.Fatal("backup not zero", item)
			}
		}
	}
	if createdID == "" || backupID == "" {
		t.Fatal("receipt missing immutable identities")
	}
	nativeBefore := dispatches.Load()
	expectStatus(t, invoke(oldKey.Secret, protocols[0], "batch-created"), 404)
	if dispatches.Load() != nativeBefore {
		t.Fatal("implicit model access dispatched new Model")
	}
	expectStatus(t, invoke(oldKey.Secret, protocols[0], "batch-existing"), 200)
	dispatchMu.Lock()
	if len(upstreamModels) != 2 || upstreamModels[0] != "batch-primary" || upstreamModels[1] != "batch-primary" {
		t.Fatal("zero backup received traffic", upstreamModels)
	}
	dispatchMu.Unlock()
	// Three other native protocols are independently created and applied. Explicit
	// grants and new Keys below are separate actions, never effects of the batch.
	nativeModels := []string{createdID}
	nativeNames := []string{"batch-created"}
	for index := range protocols[1:] {
		target := "/api/v1/admin/connections/" + fmt.Sprintf("con_batch_native_%d", index) + "/model-creation"
		rows := []service.ModelCreationItem{{ProviderModelID: fmt.Sprintf("pmd_batch_native_%d", index), Target: "new", Name: fmt.Sprintf("batch-native-%d", index)}}
		p := decodeCatalogResponse[service.ModelCreationPreview](t, send("POST", target+"/preview", service.ModelCreationPreviewInput{Items: rows}, ""), 200)
		input := service.ModelCreationBatchInput{RequestID: fmt.Sprintf("50000000-2222-4222-8222-%012d", index+1), Reason: "Explicit protocol-specific creation", Items: rows}
		r := decodeCatalogResponse[service.ModelCreationBatchResult](t, send("POST", target, input, p.ReviewETag), 201)
		if !r.Committed || r.Receipt.Items[0].Protocol != protocols[index+1] || r.Receipt.Items[0].Weight != 100 {
			t.Fatal("protocol-specific creation not applied", r)
		}
		if !reflect.DeepEqual(waitApplied(input.RequestID).Receipt, r.Receipt) {
			t.Fatal("protocol publication changed receipt")
		}
		nativeModels = append(nativeModels, r.Receipt.Items[0].ModelID)
		nativeNames = append(nativeNames, rows[0].Name)
	}
	for _, modelID := range nativeModels {
		var n int64
		if err := db.Model(&entity.UserModelGrant{}).Where("model_id = ?", modelID).Count(&n).Error; err != nil || n != 0 {
			t.Fatal("created Model granted implicitly", n, err)
		}
		create(&entity.UserModelGrant{UserID: admin.User.ID, ModelID: modelID})
	}
	newKey, err := svc.CreatePersonalKey(ctx, admin.User.ID, "Explicit new native ceiling", nativeModels, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConfirmKeyDelivery(ctx, admin.User.ID, newKey.Record.Key.ID); err != nil {
		t.Fatal(err)
	}
	for index, protocol := range protocols {
		if err := svc.RefreshRuntime(ctx); err != nil {
			t.Fatal(err)
		}
		expectStatus(t, invoke(newKey.Secret, protocol, nativeNames[index]), 200)
	}
	if dispatches.Load() != 6 {
		t.Fatal("native count includes denied/backup or missing protocol", dispatches.Load())
	}
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	var successful []entity.CallRecord
	if err := db.Where("status = ?", "success").Order("started_at,request_id").Find(&successful).Error; err != nil || len(successful) != 6 {
		t.Fatal("actual immutable completed facts missing", len(successful), err)
	}
	requestIDs := map[string]bool{}
	for _, call := range successful {
		if requestIDs[call.RequestID] || call.UserID != admin.User.ID || call.TeamID != "" || call.ProjectID != "" || call.SnapshotID == "" {
			t.Fatal("immutable call identity changed", call.RequestID)
		}
		requestIDs[call.RequestID] = true
		if call.InputTokens == nil || *call.InputTokens != 3 || call.OutputTokens == nil || *call.OutputTokens != 2 {
			t.Fatal("completed native usage lost authoritative counters", call.RequestID)
		}
		if call.ModelID == "mdl_batch_existing" {
			if call.KeyID != oldKey.Record.Key.ID || call.ProviderModelID != "pmd_batch_old" || call.ConnectionID != "con_batch_old" || call.Protocol != protocols[0] {
				t.Fatal("original native attempt borrowed new route or Key", call.RequestID)
			}
		} else {
			index := slices.Index(nativeModels, call.ModelID)
			if index < 0 || call.KeyID != newKey.Record.Key.ID || call.Protocol != protocols[index] {
				t.Fatal("new native attempt lost independently selected protocol/Key", call.RequestID)
			}
		}
		var attempts []entity.CallAttempt
		if err := db.Where("request_id = ?", call.RequestID).Find(&attempts).Error; err != nil || len(attempts) != 1 || attempts[0].Status != "success" || !attempts[0].FinalUsageKnown || attempts[0].NativeCompletionEvidence != "completed" || attempts[0].ProviderModelID != call.ProviderModelID || attempts[0].SnapshotID != call.SnapshotID || attempts[0].CredentialID == "" {
			t.Fatal("native completion/route facts substituted", attempts, err)
		}
	}
	// A lost publication response preserves the original UUID/body/ETag. Receipt
	// GET does not itself retry publication; the exact POST can reconcile it.
	uncertainRows := []service.ModelCreationItem{{ProviderModelID: "pmd_batch_E", Target: "new", Name: "batch-uncertain"}}
	uncertainPreview := preview(uncertainRows)
	uncertainBody := service.ModelCreationBatchInput{RequestID: "50000000-3333-4333-8333-333333333333", Reason: "Preserve original uncertain batch", Items: uncertainRows}
	armPublication.Store(true)
	expectStatus(t, send("POST", path, uncertainBody, uncertainPreview.ReviewETag), 503)
	armPublication.Store(false)
	uncertain := decodeCatalogResponse[service.ModelCreationBatchResult](t, send("GET", "/api/v1/admin/model-creation/receipts/"+uncertainBody.RequestID, nil, ""), 200)
	if !uncertain.Committed || uncertain.RuntimeApplied || uncertain.ApplicationStatus != "pending" {
		t.Fatal("saved receipt fabricated publication", uncertain)
	}
	publicationFailure.Store(false)
	reconciled := decodeCatalogResponse[service.ModelCreationBatchResult](t, send("POST", path, uncertainBody, uncertainPreview.ReviewETag), 200)
	if reconciled.Changed || !reconciled.Committed || !reflect.DeepEqual(reconciled.Receipt, uncertain.Receipt) {
		t.Fatal("unknown retry recreated batch", reconciled)
	}
	if !reflect.DeepEqual(waitApplied(uncertainBody.RequestID).Receipt, uncertain.Receipt) {
		t.Fatal("uncertain publication changed immutable receipt")
	}
	// Later mutations remain current truth; old intent must not rename or reset
	// reviewed weights, and receipt labels/IDs remain historical.
	if _, err := svc.RenameModel(ctx, admin.User.ID, createdID, "batch-renamed", nil); err != nil {
		t.Fatal(err)
	}
	replay := decodeCatalogResponse[service.ModelCreationBatchResult](t, send("POST", path, body, reviewed.ReviewETag), 200)
	if replay.ApplicationStatus != "superseded" || replay.RuntimeApplied || !reflect.DeepEqual(replay.Receipt, receipt) {
		t.Fatal("old receipt silently renamed current Model", replay)
	}
	if _, err := svc.SetModelWeights(ctx, admin.User.ID, "mdl_batch_existing", []service.ModelWeight{{BindingID: "bnd_batch_old", Weight: 40}, {BindingID: backupID, Weight: 60}}); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, send("POST", path, body, reviewed.ReviewETag), 200)
	var backup entity.ModelProviderBinding
	if err := db.Where("id = ?", backupID).Take(&backup).Error; err != nil || backup.Weight != 60 {
		t.Fatal("old receipt restored original0", backup, err)
	}
	if err := db.Where("id = ?", uncertain.Receipt.Items[0].BindingID).Delete(&entity.ModelProviderBinding{}).Error; err != nil {
		t.Fatal(err)
	}
	missing := decodeCatalogResponse[service.ModelCreationBatchResult](t, send("POST", path, uncertainBody, uncertainPreview.ReviewETag), 200)
	if missing.ApplicationStatus != "unavailable" || missing.CurrentItems != nil || missing.RuntimeApplied {
		t.Fatal("missing original binding fabricated current application", missing)
	}
	// Write-only authority can create but receives no mutable read facts. Losing
	// write authority denies even exact historical POST retries before lookup.
	writeRows := []service.ModelCreationItem{{ProviderModelID: "pmd_batch_F", Target: "new", Name: "batch-write-only"}}
	writeBody := service.ModelCreationBatchInput{RequestID: "50000000-4444-4444-8444-444444444444", Reason: "Independent write permission", Items: writeRows}
	var writerRoles []entity.UserRole
	if err := db.Where("user_id = ?", writer.User.ID).Find(&writerRoles).Error; err != nil {
		t.Fatal(err)
	}
	roleIDs := []string{}
	for _, role := range writerRoles {
		roleIDs = append(roleIDs, role.RoleID)
	}
	var readerRoles []entity.UserRole
	if err := db.Where("user_id = ?", reader.User.ID).Find(&readerRoles).Error; err != nil {
		t.Fatal(err)
	}
	if len(roleIDs) != 1 || len(readerRoles) != 1 {
		t.Fatal("independent writer and reader roles missing", roleIDs, readerRoles)
	}
	// Reviews are actor-bound. Temporarily grant the existing reader role so
	// this writer captures its own review, then remove read authority before
	// exercising independent write permission.
	reviewRoles := append(slices.Clone(roleIDs), readerRoles[0].RoleID)
	if _, err := svc.SetMemberRoles(ctx, admin.User.ID, writer.User.ID, reviewRoles); err != nil {
		t.Fatal(err)
	}
	writePreview := decodeCatalogResponse[service.ModelCreationPreview](t, request(writeCookie, writeCSRF, "POST", path+"/preview", service.ModelCreationPreviewInput{Items: writeRows}, ""), 200)
	if !writePreview.CanCommit || writePreview.ReviewETag == "" {
		t.Fatal("writer did not capture its own committable review", writePreview)
	}
	if _, err := svc.SetMemberRoles(ctx, admin.User.ID, writer.User.ID, roleIDs); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request(writeCookie, writeCSRF, "GET", readPath, nil, ""), 403)
	beforeDeniedWrite := counts()
	if _, err := svc.SetMemberRoles(ctx, admin.User.ID, writer.User.ID, []string{}); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request(writeCookie, writeCSRF, "POST", path, writeBody, writePreview.ReviewETag), 403)
	if !slices.Equal(counts(), beforeDeniedWrite) {
		t.Fatal("permission loss between preview and commit wrote rows")
	}
	if _, err := svc.SetMemberRoles(ctx, admin.User.ID, writer.User.ID, roleIDs); err != nil {
		t.Fatal(err)
	}
	writeResult := decodeCatalogResponse[service.ModelCreationBatchResult](t, request(writeCookie, writeCSRF, "POST", path, writeBody, writePreview.ReviewETag), 201)
	if !writeResult.Committed || writeResult.CurrentItems != nil || writeResult.RuntimeApplied || writeResult.ApplicationStatus != "unavailable" {
		t.Fatal("write-only authority leaked current directory", writeResult)
	}
	if _, err := svc.SetMemberRoles(ctx, admin.User.ID, writer.User.ID, []string{}); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request(writeCookie, writeCSRF, "POST", path, writeBody, writePreview.ReviewETag), 403)
	// The exact supported batch ceiling commits once; an over-ceiling preview
	// remains transport-invalid and cannot insert even a partial prefix.
	maximumIDs := make([]string, 50)
	maximumItems := make([]service.ModelCreationItem, 50)
	for index := range maximumIDs {
		maximumIDs[index] = fmt.Sprintf("pmd_batch_max_%02d", index)
		maximumItems[index] = service.ModelCreationItem{ProviderModelID: maximumIDs[index], Target: "new", Name: fmt.Sprintf("batch-maximum-%02d", index)}
	}
	seedConnection("con_batch_maximum", "prv_batch_main", protocols[0], maximumIDs...)
	maximumPath := "/api/v1/admin/connections/con_batch_maximum/model-creation"
	maximumPreview := decodeCatalogResponse[service.ModelCreationPreview](t, send("POST", maximumPath+"/preview", service.ModelCreationPreviewInput{Items: maximumItems}, ""), 200)
	if !maximumPreview.CanCommit || len(maximumPreview.Items) != 50 {
		t.Fatal("maximum batch not reviewed", len(maximumPreview.Items))
	}
	maximumBefore := counts()
	maximumBody := service.ModelCreationBatchInput{RequestID: "50000000-5555-4555-8555-555555555555", Reason: "Exact bounded maximum creation", Items: maximumItems}
	maximum := decodeCatalogResponse[service.ModelCreationBatchResult](t, send("POST", maximumPath, maximumBody, maximumPreview.ReviewETag), 201)
	if !maximum.Committed || !maximum.Changed || len(maximum.Receipt.Items) != 50 {
		t.Fatal("maximum batch did not commit atomically", len(maximum.Receipt.Items))
	}
	maximumAfter := counts()
	if maximumAfter[0] != maximumBefore[0]+50 || maximumAfter[1] != maximumBefore[1]+50 || maximumAfter[2] != maximumBefore[2]+50 || maximumAfter[3] != maximumBefore[3] || maximumAfter[4] != maximumBefore[4] || maximumAfter[5] != maximumBefore[5]+1 || maximumAfter[6] != maximumBefore[6]+1 || !slices.Equal(maximumAfter[7:], maximumBefore[7:]) {
		t.Fatal("maximum batch changed grants or partially committed", maximumBefore, maximumAfter)
	}
	for _, item := range maximum.Receipt.Items {
		if !item.CreatedModel || item.Weight != 100 || item.Protocol != protocols[0] {
			t.Fatal("maximum batch inherited client topology", item)
		}
	}
	tooMany := append(slices.Clone(maximumItems), service.ModelCreationItem{ProviderModelID: "pmd_batch_extra", Target: "new", Name: "batch-extra"})
	expectStatus(t, send("POST", maximumPath+"/preview", map[string]any{"items": tooMany}, ""), 400)
	if !slices.Equal(counts(), maximumAfter) || dispatches.Load() != 6 {
		t.Fatal("over-bound preview wrote state or dispatched inference")
	}
	// A real Service/runtime/recorder restart preserves historical receipts and
	// all immutable native facts. It performs no inference replay.
	svc.StopRuntime()
	if err := svc.StopCallRecorder(); err != nil {
		t.Fatal(err)
	}
	svc, err = service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.StartCallRecorder(ctx, queuePath); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	router = fox.New()
	New(svc).RegisterRoutes(router)
	restarted := decodeCatalogResponse[service.ModelCreationBatchResult](t, send("GET", "/api/v1/admin/model-creation/receipts/"+body.RequestID, nil, ""), 200)
	if !reflect.DeepEqual(restarted.Receipt, receipt) || restarted.ApplicationStatus != "superseded" || dispatches.Load() != 6 {
		t.Fatal("restart rewrote/replayed saved intent", restarted)
	}
	var afterCalls []entity.CallRecord
	if err := db.Where("status = ?", "success").Order("started_at,request_id").Find(&afterCalls).Error; err != nil || !reflect.DeepEqual(afterCalls, successful) {
		t.Fatal("restart changed immutable call facts", err)
	}
}

func modelCreationBatchNativeBody(protocol string) string {
	switch protocol {
	case entity.ProtocolOpenAIChat:
		return `{"id":"chat_batch","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"Completed"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`
	case entity.ProtocolOpenAIResponses:
		return `{"id":"resp_batch","object":"response","model":"controlled-responses","status":"completed","output":[{"id":"msg_batch","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"Completed","annotations":[]}]}],"usage":{"input_tokens":3,"output_tokens":2}}`
	case entity.ProtocolAnthropicMessages:
		return `{"id":"msg_batch","type":"message","model":"controlled-messages","role":"assistant","content":[{"type":"text","text":"Completed"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":2}}`
	case entity.ProtocolGeminiGenerateContent:
		return `{"candidates":[{"index":0,"content":{"role":"model","parts":[{"text":"Completed"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"cachedContentTokenCount":0,"candidatesTokenCount":2,"thoughtsTokenCount":0,"totalTokenCount":5}}`
	}
	return ""
}

// Calibrate the exact controlled payloads with production native finishers.
// Known terminal usage and completed text are separate assertions.
func TestModelCreationBatchNativeFixtures(t *testing.T) {
	for _, protocol := range []string{entity.ProtocolOpenAIChat, entity.ProtocolOpenAIResponses, entity.ProtocolAnthropicMessages, entity.ProtocolGeminiGenerateContent} {
		t.Run(protocol, func(t *testing.T) {
			raw := []byte(modelCreationBatchNativeBody(protocol))
			var observed gatewayObservation
			var err error
			switch protocol {
			case entity.ProtocolOpenAIChat:
				observed = parseGatewayUsage(raw)
			case entity.ProtocolOpenAIResponses:
				var encoded []byte
				var status string
				encoded, status, err = rewriteResponsesObject(raw, "public-model", false)
				observed = observeGatewayUsage(service.ParseResponsesUsage(encoded))
				observed.NativeCompletionEvidence = responsesCompletionEvidence(encoded, status)
			case entity.ProtocolAnthropicMessages:
				var encoded []byte
				encoded, err = rewriteMessagesObject(raw, "public-model", true)
				observed = observeGatewayUsage(service.ParseMessagesUsage(encoded, true))
				observed.NativeCompletionEvidence = observedMessagesCompletion(encoded)
			case entity.ProtocolGeminiGenerateContent:
				state := geminiStreamState{expected: 1}
				var encoded []byte
				encoded, err = state.object(raw, "public-model", "req_batch_fixture")
				observed = observeGatewayUsage(service.ParseGeminiUsage(encoded, true))
				observed.NativeCompletionEvidence = state.completionEvidence()
			}
			if err != nil || observed.NativeCompletionEvidence != "completed" {
				t.Fatal("controlled payload lacks native completion", observed.NativeCompletionEvidence, err)
			}
			if !observed.Complete || observed.Input == nil || *observed.Input != 3 || observed.Output == nil || *observed.Output != 2 {
				t.Fatal("controlled payload lost exact authoritative usage", observed)
			}
		})
	}
}
