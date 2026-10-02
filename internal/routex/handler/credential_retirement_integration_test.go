package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
)

func testCredentialRetirementLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	store, err := secretstore.New(bytes.Repeat([]byte{84}, 32))
	if err != nil {
		t.Fatal(err)
	}
	completed := chatCompletionFixture("stop", `{"role":"assistant","content":"Complete"}`, `{"prompt_tokens":1,"completion_tokens":2}`, false, 0)
	var responseBody atomic.Value
	responseBody.Store(completed)
	var sourceDispatches, replacementDispatches atomic.Int32
	var blockSource atomic.Bool
	sourceEntered := make(chan struct{}, 1)
	releaseSource := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(releaseSource) })
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/v1/models" {
			_, _ = w.Write([]byte(`{"data":[{"id":"retirement-upstream"}]}`))
			return
		}
		switch r.Header.Get("Authorization") {
		case "Bearer test-only-retirement-source-secret":
			sourceDispatches.Add(1)
			if blockSource.Load() {
				sourceEntered <- struct{}{}
				select {
				case <-releaseSource:
				case <-r.Context().Done():
					return
				}
			}
		case "Bearer test-only-retirement-successor-secret":
			replacementDispatches.Add(1)
		default:
			t.Error("unexpected upstream credential")
		}
		_, _ = w.Write([]byte(responseBody.Load().(string)))
	}))
	defer func() { releaseOnce.Do(func() { close(releaseSource) }); upstream.Close() }()
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"retirement@example.invalid","password":"test-only-retirement-password","name":"Retirement admin"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	reader, readCookie, readCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "retirement-reader", []string{"providers.read"})
	writer, writeCookie, writeIdentity := createSystemStatusMember(t, svc, router, admin.User.ID, "retirement-writer", []string{"providers.write"})
	_ = reader
	provider, err := svc.CreateProvider(ctx, admin.User.ID, "Retirement provider", service.CreateConnectionInput{Name: "Retirement connection", BaseURL: upstream.URL + "/v1", Protocol: entity.ProtocolOpenAIChat, CredentialName: "Source", Secret: "test-only-retirement-source-secret"})
	if err != nil {
		t.Fatal(err)
	}
	source := provider.Connections[0].Credentials[0]
	connection := provider.Connections[0].Connection
	if verified, err := svc.VerifyCredential(ctx, admin.User.ID, source.ID); err != nil || !verified.Verified {
		t.Fatal("source discovery failed", err)
	}
	if _, err := svc.SetCredentialEnabled(ctx, admin.User.ID, source.ID, true); err != nil {
		t.Fatal(err)
	}
	metadata, err := svc.GetCredentialMetadata(ctx, admin.User.ID, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	priority := 13
	metadata, err = svc.WriteCredentialMetadata(ctx, admin.User.ID, source.ID, metadata.ETag, service.CredentialMetadataInput{Name: source.Name, Priority: &priority, Reason: "Review source priority"})
	if err != nil {
		t.Fatal(err)
	}
	var access entity.CredentialModelAccess
	if err := db.First(&access, "credential_id = ?", source.ID).Error; err != nil {
		t.Fatal(err)
	}
	model, err := svc.CreateModel(ctx, admin.User.ID, "retirement-model", access.ProviderModelID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetModelWeights(ctx, admin.User.ID, model.Model.ID, []service.ModelWeight{{BindingID: model.Bindings[0].Binding.ID, Weight: 100}}); err != nil {
		t.Fatal(err)
	}
	bearer := "rx_" + strings.Repeat("r", 43)
	for _, row := range []any{&entity.APIKey{ID: "key_retirement", UserID: admin.User.ID, Name: "Retirement Key", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(bearer), Status: entity.KeyActive}, &entity.APIKeyModel{KeyID: "key_retirement", ModelID: model.Model.ID}} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	defer svc.StopRuntime()
	call := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "http://routex.test/v1/chat/completions", strings.NewReader(`{"model":"retirement-model","messages":[{"role":"user","content":"Review real completed inference"}]}`))
		req.Header.Set("Authorization", "Bearer "+bearer)
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	// Calls already dispatched with the old secret are allowed to finish.
	blockSource.Store(true)
	oldCall := make(chan *httptest.ResponseRecorder, 1)
	oldCallDone := false
	defer func() {
		releaseOnce.Do(func() { close(releaseSource) })
		if !oldCallDone {
			<-oldCall
		}
	}()
	go func() { oldCall <- call() }()
	select {
	case <-sourceEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("old call never dispatched")
	}
	post := func(targetRouter *fox.Engine, path string, input any, etag string, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
		raw, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("POST", "http://routex.test"+path, bytes.NewReader(raw))
		if cookie != nil {
			req.AddCookie(cookie)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", csrf)
		if etag != "" {
			req.Header.Set("If-Match", strconv.Quote(etag))
		}
		res := httptest.NewRecorder()
		targetRouter.ServeHTTP(res, req)
		return res
	}
	prepared := decodeCatalogResponse[service.CredentialReplacementRecord](t, post(router, "/api/v1/admin/credentials/"+source.ID+"/replacements", service.CredentialReplacementInput{RequestID: "81b043bb-cb96-419c-b9a2-cab04814b65e", Name: "Successor", Secret: "test-only-retirement-successor-secret", Reason: "Prepare reviewed replacement"}, metadata.ETag, adminCookie, admin.CSRFToken), 201)
	expectStatus(t, post(router, "/api/v1/admin/credentials/"+prepared.ID+"/verify", nil, "", adminCookie, admin.CSRFToken), 200)
	expectStatus(t, identityRequest(router, "PATCH", "/api/v1/admin/credentials/"+prepared.ID, `{"enabled":true}`, adminCookie, admin.CSRFToken), 200)
	replacementMetadata, err := svc.GetCredentialMetadata(ctx, admin.User.ID, prepared.ID)
	if err != nil {
		t.Fatal(err)
	}
	priority = 0
	if _, err := svc.WriteCredentialMetadata(ctx, admin.User.ID, prepared.ID, replacementMetadata.ETag, service.CredentialMetadataInput{Name: replacementMetadata.Name, Priority: &priority, Reason: "Test actual successor inference"}); err != nil {
		t.Fatal(err)
	}
	// Deterministic fixtures explicitly publish without a background refresh race.
	svc.StopRuntime()
	path := "/api/v1/admin/credentials/" + source.ID + "/retire"
	readinessPath := "/api/v1/admin/credentials/" + source.ID + "/retirement-readiness?replacement_credential_id=" + prepared.ID
	read := func() service.CredentialRetirementReadiness {
		return decodeCatalogResponse[service.CredentialRetirementReadiness](t, identityRequest(router, "GET", readinessPath, "", readCookie, ""), 200)
	}
	for _, body := range []string{`{"choices":[],"usage":{"prompt_tokens":1,"completion_tokens":2}}`, chatCompletionFixture("content_filter", `{"role":"assistant","content":"Declined"}`, "null", false, 0), chatCompletionFixture("tool_calls", `{"role":"assistant","content":null,"tool_calls":[{"type":"function","function":{"name":"lookup","arguments":"{}"}}]}`, "null", false, 0), chatCompletionFixture("length", `{"role":"assistant","content":"Partial"}`, "null", false, 0)} {
		responseBody.Store(body)
		expectStatus(t, call(), 200)
		var attempt entity.CallAttempt
		if err := db.Where("credential_id = ?", prepared.ID).Order("completed_at DESC,id DESC").First(&attempt).Error; err != nil {
			t.Fatal(err)
		}
		current := read()
		if current.Eligible || current.Evidence != nil {
			t.Fatal("weak HTTP200 proved native completion")
		}
		invalid := service.CredentialRetirementInput{RequestID: "91b043bb-cb96-419c-b9a2-cab04814b65e", ReplacementCredentialID: prepared.ID, EvidenceAttemptID: attempt.ID, SnapshotID: svc.RuntimeStatus().SnapshotID, Reason: "Reject incomplete inference"}
		expectStatus(t, post(router, path, invalid, current.ETag, adminCookie, admin.CSRFToken), 409)
	}
	responseBody.Store(completed)
	expectStatus(t, call(), 200)
	ready := read()
	if !ready.Eligible || ready.Evidence == nil || ready.SnapshotID == nil {
		t.Fatalf("completed successor not ready: %+v", ready)
	}
	intent := service.CredentialRetirementInput{RequestID: "a1b043bb-cb96-419c-b9a2-cab04814b65e", ReplacementCredentialID: prepared.ID, EvidenceAttemptID: ready.Evidence.AttemptID, SnapshotID: *ready.SnapshotID, Reason: "Retire reviewed source after actual completion"}
	etag := ready.ETag
	expectStatus(t, post(router, path, intent, etag, nil, ""), 401)
	expectStatus(t, post(router, path, intent, etag, readCookie, readCSRF), 403)
	expectStatus(t, post(router, path, intent, etag, adminCookie, ""), 403)
	raw, _ := json.Marshal(intent)
	for _, body := range []string{"null", string(raw[:len(raw)-1]) + `,"secret":"forbidden"}`, string(raw) + ` {}`, `{"request_id":1}`} {
		expectStatus(t, identityRequest(router, "POST", path, body, adminCookie, admin.CSRFToken), 400)
	}
	for _, header := range []string{"", etag, "W/" + strconv.Quote(etag), "*", strconv.Quote(strings.ToUpper(etag))} {
		req := httptest.NewRequest("POST", "http://routex.test"+path, bytes.NewReader(raw))
		req.AddCookie(adminCookie)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", admin.CSRFToken)
		if header != "" {
			req.Header.Set("If-Match", header)
		}
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		expectStatus(t, res, 400)
	}
	crossOrigin := httptest.NewRequest("POST", "http://routex.test"+path, bytes.NewReader(raw))
	crossOrigin.AddCookie(adminCookie)
	crossOrigin.Header.Set("Content-Type", "application/json")
	crossOrigin.Header.Set("X-CSRF-Token", admin.CSRFToken)
	crossOrigin.Header.Set("If-Match", strconv.Quote(etag))
	crossOrigin.Header.Set("Origin", "https://another.invalid")
	originRes := httptest.NewRecorder()
	router.ServeHTTP(originRes, crossOrigin)
	expectStatus(t, originRes, 403)
	for _, modify := range []func(*service.CredentialRetirementInput){func(v *service.CredentialRetirementInput) { v.RequestID = "00000000-0000-0000-0000-000000000000" }, func(v *service.CredentialRetirementInput) { v.ReplacementCredentialID = source.ID }, func(v *service.CredentialRetirementInput) { v.EvidenceAttemptID = "" }, func(v *service.CredentialRetirementInput) { v.EvidenceAttemptID = strings.Repeat("a", 65) }, func(v *service.CredentialRetirementInput) { v.SnapshotID = "cfg_invented" }, func(v *service.CredentialRetirementInput) { v.Reason = " " }, func(v *service.CredentialRetirementInput) { v.Reason = "bad\nreason" }, func(v *service.CredentialRetirementInput) { v.Reason = strings.Repeat("界", 342) }} {
		changed := intent
		modify(&changed)
		expectStatus(t, post(router, path, changed, etag, adminCookie, admin.CSRFToken), 400)
	}
	wrongProof := intent
	wrongProof.EvidenceAttemptID = "attempt_not_recorded"
	expectStatus(t, post(router, path, wrongProof, etag, adminCookie, admin.CSRFToken), 409)
	if err := db.Model(&entity.ModelProviderBinding{}).Where("id = ?", model.Bindings[0].Binding.ID).Update("weight", 90).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, post(router, path, intent, etag, adminCookie, admin.CSRFToken), 409)
	if err := db.Model(&entity.ModelProviderBinding{}).Where("id = ?", model.Bindings[0].Binding.ID).Update("weight", 100).Error; err != nil {
		t.Fatal(err)
	}
	// A later successful call must not silently replace the reviewed attempt.
	expectStatus(t, call(), 200)
	if latest := read(); latest.Evidence == nil || latest.Evidence.AttemptID == intent.EvidenceAttemptID {
		t.Fatal("fixture did not create newer proof")
	}
	var beforeSource, beforeReplacement entity.ProviderCredential
	if err := db.First(&beforeSource, "id = ?", source.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&beforeReplacement, "id = ?", prepared.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	// A row lock may delay fresh preparation, but cannot pin all publication
	// beyond its two-second DB budget. The waiter must leave no saved mutation.
	lockTx := db.Begin()
	defer func() { _ = lockTx.Rollback().Error }()
	if lockTx.Error != nil {
		t.Fatal(lockTx.Error)
	}
	var governance entity.GovernanceSetting
	if err := lockTx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&governance, 1).Error; err != nil {
		t.Fatal(err)
	}
	lockAttempted := make(chan struct{}, 1)
	lockCallback := "test_retirement_bounded_lock_wait"
	if err := db.Callback().Query().Before("gorm:query").Register(lockCallback, func(tx *gorm.DB) {
		if tx.Statement.Table == "governance_settings" && tx.Statement.Clauses["FOR"].Expression != nil {
			select {
			case lockAttempted <- struct{}{}:
			default:
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	blockedResult := make(chan *httptest.ResponseRecorder, 1)
	go func() { blockedResult <- post(router, path, intent, etag, writeCookie, writeIdentity) }()
	select {
	case <-lockAttempted:
	case <-time.After(5 * time.Second):
		t.Fatal("retirement did not reach contended governance lock")
	}
	publisherResult := make(chan error, 1)
	publisherCtx, publisherCancel := context.WithTimeout(ctx, 5*time.Second)
	defer publisherCancel()
	go func() { publisherResult <- svc.RefreshRuntime(publisherCtx) }()
	select {
	case res := <-blockedResult:
		expectStatus(t, res, 503)
	case <-time.After(4 * time.Second):
		t.Fatal("fresh retirement retained publication pin past DB budget")
	}
	if err := lockTx.Rollback().Error; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-publisherResult:
		if err != nil {
			t.Fatal("publisher could not resume after bounded retirement", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("publication gate was leaked after timeout")
	}
	// Callback registration is shared by every GORM session; remove it only
	// after the publisher has finished using the callback processor.
	if err := db.Callback().Query().Remove(lockCallback); err != nil {
		t.Fatal(err)
	}
	var timedOutReceipts int64
	if err := db.Model(&entity.CredentialRetirementReceipt{}).Count(&timedOutReceipts).Error; err != nil || timedOutReceipts != 0 {
		t.Fatal("timed-out preparation claimed commit", err)
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	// Audit failure must roll back the source reduction and receipt together.
	callback := "test_retirement_audit_failure"
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "audit_events" {
			_ = tx.AddError(fmt.Errorf("test-only retirement audit failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	failed := post(router, path, intent, etag, adminCookie, admin.CSRFToken)
	if err := db.Callback().Create().Remove(callback); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, failed, 500)
	var rollbackSource entity.ProviderCredential
	var receipts int64
	if err := db.First(&rollbackSource, "id = ?", source.ID).Error; err != nil || !rollbackSource.Enabled {
		t.Fatal("failed audit persisted disable", err)
	}
	if err := db.Model(&entity.CredentialRetirementReceipt{}).Count(&receipts).Error; err != nil || receipts != 0 {
		t.Fatal("failed audit persisted receipt", err)
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	// A query outage begins only after the atomic retirement audit is saved.
	// The response acknowledges its durable receipt but cannot claim publication.
	var publicationOutage atomic.Bool
	publicationCallback := "test_retirement_publication_outage"
	if err := db.Callback().Query().Before("gorm:query").Register(publicationCallback, func(tx *gorm.DB) {
		if publicationOutage.Load() && tx.Statement.Table == "provider_credentials" {
			_ = tx.AddError(fmt.Errorf("test-only publication database outage"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().After("gorm:create").Register(publicationCallback, func(tx *gorm.DB) {
		if event, ok := tx.Statement.Dest.(*entity.AuditEvent); ok && event.Action == "credential.retire" && tx.Error == nil {
			publicationOutage.Store(true)
		}
	}); err != nil {
		t.Fatal(err)
	}
	pendingPublication := decodeCatalogResponse[service.CredentialRetirementRecord](t, post(router, path, intent, etag, writeCookie, writeIdentity), 200)
	publicationOutage.Store(false)
	if err := db.Callback().Query().Remove(publicationCallback); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().Remove(publicationCallback); err != nil {
		t.Fatal(err)
	}
	if !pendingPublication.Committed || pendingPublication.RuntimeApplied || pendingPublication.CurrentSnapshotID != nil || !slices.Contains(pendingPublication.Blockers, "runtime_unavailable") {
		t.Fatal("postcommit outage lost historical truth or claimed publication")
	}
	// Independent writers submit the same exact intent; pin contention may reject
	// an uncommitted request, but every retry reconciles the single durable result.
	var wg sync.WaitGroup
	responses := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		wg.Go(func() { responses <- post(router, path, intent, etag, writeCookie, writeIdentity) })
	}
	wg.Wait()
	close(responses)
	for res := range responses {
		if res.Code != 200 && res.Code != 503 {
			t.Fatalf("unexpected concurrent retirement status %d: %s", res.Code, res.Body.String())
		}
	}
	saved := decodeCatalogResponse[service.CredentialRetirementRecord](t, post(router, path, intent, etag, writeCookie, writeIdentity), 200)
	if !saved.Committed || !saved.RuntimeApplied || saved.CurrentSnapshotID == nil || saved.RequestID != intent.RequestID || saved.SourceCredentialID != source.ID || saved.ReplacementCredentialID != prepared.ID || len(saved.Blockers) != 0 {
		t.Fatalf("retirement was not saved/applied: %+v", saved)
	}
	var afterSource, afterReplacement entity.ProviderCredential
	if err := db.First(&afterSource, "id = ?", source.ID).Error; err != nil || afterSource.Enabled || afterSource.Ciphertext != beforeSource.Ciphertext || afterSource.VerificationStatus != beforeSource.VerificationStatus || afterSource.Priority != beforeSource.Priority {
		t.Fatal("retirement changed source beyond enablement", err)
	}
	if err := db.First(&afterReplacement, "id = ?", prepared.ID).Error; err != nil || afterReplacement.Ciphertext != beforeReplacement.Ciphertext || !afterReplacement.Enabled || afterReplacement.VerificationStatus != beforeReplacement.VerificationStatus {
		t.Fatal("retirement changed successor", err)
	}
	var receipt entity.CredentialRetirementReceipt
	if err := db.First(&receipt, "request_id = ?", intent.RequestID).Error; err != nil || receipt.ActorID != writer.User.ID || receipt.EvidenceAttemptID != intent.EvidenceAttemptID || receipt.SourceETag != ready.Source.ETag || receipt.ReplacementETag != ready.Replacement.ETag || receipt.ReadinessETag != etag || !receipt.CommittedAt.Equal(saved.CommittedAt) {
		t.Fatal("receipt lost reviewed historical identity", err)
	}
	var audits int64
	if err := db.Model(&entity.AuditEvent{}).Where("action = ? AND resource_id = ?", "credential.retire", source.ID).Count(&audits).Error; err != nil || audits != 1 {
		t.Fatal("retirement audit is not exactly once", err)
	}
	expectStatus(t, post(router, path, intent, etag, adminCookie, admin.CSRFToken), 409)
	changed := intent
	changed.Reason = "Changed historical reason"
	expectStatus(t, post(router, path, changed, etag, writeCookie, writeIdentity), 409)
	changed = intent
	changed.RequestID = "c1b043bb-cb96-419c-b9a2-cab04814b65e"
	expectStatus(t, post(router, path, changed, etag, writeCookie, writeIdentity), 409)
	expectStatus(t, post(router, path, intent, strings.Repeat("b", 64), writeCookie, writeIdentity), 409)
	oldSourceDispatches := sourceDispatches.Load()
	expectStatus(t, call(), 200)
	if sourceDispatches.Load() != oldSourceDispatches || replacementDispatches.Load() == 0 {
		t.Fatal("new inference dispatched retired credential")
	}
	releaseOnce.Do(func() { close(releaseSource) })
	finishedOld := <-oldCall
	oldCallDone = true
	expectStatus(t, finishedOld, 200)
	var historicalSourceAttempt entity.CallAttempt
	if err := db.First(&historicalSourceAttempt, "request_id = ?", finishedOld.Header().Get("X-Request-ID")).Error; err != nil || historicalSourceAttempt.CredentialID != source.ID || historicalSourceAttempt.Status != "success" || historicalSourceAttempt.NativeCompletionEvidence != "completed" {
		t.Fatal("retirement interrupted or rewrote dispatched source call", err)
	}
	// Live evidence can disappear; durable receipt has no FK and current
	// application does not require a second inference after rotation or restart.
	if err := db.Delete(&entity.CallAttempt{}, "id = ?", intent.EvidenceAttemptID).Error; err != nil {
		t.Fatal(err)
	}
	dsn := os.Getenv("ROUTEX_TEST_" + strings.ToUpper(db.Name()) + "_DSN")
	freshDB, err := database.Open(ctx, db.Name(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	freshPool, err := freshDB.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := freshPool.Close(); err != nil {
			t.Error(err)
		}
	}()
	freshPool.SetMaxOpenConns(1)
	freshPool.SetMaxIdleConns(1)
	freshStore, err := secretstore.New(bytes.Repeat([]byte{84}, 32))
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := service.New(ctx, freshDB, service.WithCredentialStorage(freshStore), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	freshRouter := fox.New()
	New(fresh).RegisterRoutes(freshRouter)
	unavailable := decodeCatalogResponse[service.CredentialRetirementRecord](t, post(freshRouter, path, intent, etag, writeCookie, writeIdentity), 200)
	if !unavailable.Committed || unavailable.RuntimeApplied || !slices.Contains(unavailable.Blockers, "runtime_unavailable") {
		t.Fatal("absent runtime hid history or claimed application")
	}
	if err := fresh.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	defer fresh.StopRuntime()
	restarted := decodeCatalogResponse[service.CredentialRetirementRecord](t, post(freshRouter, path, intent, etag, writeCookie, writeIdentity), 200)
	if !restarted.Committed || !restarted.RuntimeApplied || restarted.CurrentSnapshotID == nil || *restarted.CurrentSnapshotID == intent.SnapshotID || !restarted.CommittedAt.Equal(saved.CommittedAt) {
		t.Fatalf("fresh pool/process state did not reconcile history: %+v", restarted)
	}
	if _, err := fresh.SetCredentialEnabled(ctx, admin.User.ID, source.ID, true); err != nil {
		t.Fatal(err)
	}
	reenabling := decodeCatalogResponse[service.CredentialRetirementRecord](t, post(freshRouter, path, intent, etag, writeCookie, writeIdentity), 200)
	if !reenabling.Committed || reenabling.RuntimeApplied || !slices.Contains(reenabling.Blockers, "source_reenabled") {
		t.Fatal("historical replay claimed re-enabled source applied")
	}
	currentSource, err := fresh.GetCredentialMetadata(ctx, admin.User.ID, source.ID)
	if err != nil || !currentSource.Enabled {
		t.Fatal("historical replay disabled re-enabled source", err)
	}
	// The replay must not install a tombstone that silently impairs a revived
	// source. Prefer it explicitly and prove a real dispatch after the retry.
	priority = 0
	if _, err := fresh.WriteCredentialMetadata(ctx, admin.User.ID, source.ID, currentSource.ETag, service.CredentialMetadataInput{Name: currentSource.Name, Priority: &priority, Reason: "Prefer revived source"}); err != nil {
		t.Fatal(err)
	}
	successorForPriority, err := fresh.GetCredentialMetadata(ctx, admin.User.ID, prepared.ID)
	if err != nil {
		t.Fatal(err)
	}
	priority = 13
	if _, err := fresh.WriteCredentialMetadata(ctx, admin.User.ID, prepared.ID, successorForPriority.ETag, service.CredentialMetadataInput{Name: successorForPriority.Name, Priority: &priority, Reason: "Review revived source dispatch"}); err != nil {
		t.Fatal(err)
	}
	_ = decodeCatalogResponse[service.CredentialRetirementRecord](t, post(freshRouter, path, intent, etag, writeCookie, writeIdentity), 200)
	router = freshRouter
	revivedBefore := sourceDispatches.Load()
	expectStatus(t, call(), 200)
	if sourceDispatches.Load() != revivedBefore+1 {
		t.Fatal("historical replay tombstoned revived source")
	}
	// A new UUID and fresh review may retire a genuinely re-enabled source.
	currentSource, err = fresh.GetCredentialMetadata(ctx, admin.User.ID, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	priority = 13
	if _, err := fresh.WriteCredentialMetadata(ctx, admin.User.ID, source.ID, currentSource.ETag, service.CredentialMetadataInput{Name: currentSource.Name, Priority: &priority, Reason: "Review another retirement"}); err != nil {
		t.Fatal(err)
	}
	successorForPriority, err = fresh.GetCredentialMetadata(ctx, admin.User.ID, prepared.ID)
	if err != nil {
		t.Fatal(err)
	}
	priority = 0
	if _, err := fresh.WriteCredentialMetadata(ctx, admin.User.ID, prepared.ID, successorForPriority.ETag, service.CredentialMetadataInput{Name: successorForPriority.Name, Priority: &priority, Reason: "Prefer successor again"}); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, call(), 200)
	secondReview := read()
	if !secondReview.Eligible || secondReview.Evidence == nil || secondReview.SnapshotID == nil {
		t.Fatal("fresh review after re-enable not eligible")
	}
	secondIntent := service.CredentialRetirementInput{RequestID: "b1b043bb-cb96-419c-b9a2-cab04814b65e", ReplacementCredentialID: prepared.ID, EvidenceAttemptID: secondReview.Evidence.AttemptID, SnapshotID: *secondReview.SnapshotID, Reason: "Freshly retire re-enabled source"}
	secondResponses := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		wg.Go(func() {
			secondResponses <- post(freshRouter, path, secondIntent, secondReview.ETag, writeCookie, writeIdentity)
		})
	}
	wg.Wait()
	close(secondResponses)
	for res := range secondResponses {
		if res.Code != 200 && res.Code != 503 {
			t.Fatalf("unexpected concurrent fresh intent %d: %s", res.Code, res.Body.String())
		}
	}
	secondSaved := decodeCatalogResponse[service.CredentialRetirementRecord](t, post(freshRouter, path, secondIntent, secondReview.ETag, writeCookie, writeIdentity), 200)
	if !secondSaved.Committed || !secondSaved.RuntimeApplied {
		t.Fatalf("fresh intent after re-enable not saved: %+v", secondSaved)
	}
	if _, err := fresh.SetCredentialEnabled(ctx, admin.User.ID, prepared.ID, false); err != nil {
		t.Fatal(err)
	}
	successorDisabled := decodeCatalogResponse[service.CredentialRetirementRecord](t, post(freshRouter, path, intent, etag, writeCookie, writeIdentity), 200)
	if !successorDisabled.Committed || successorDisabled.RuntimeApplied || !slices.Contains(successorDisabled.Blockers, "replacement_disabled") {
		t.Fatal("disabled successor was accepted as applied")
	}
	successorMetadata, err := fresh.GetCredentialMetadata(ctx, admin.User.ID, prepared.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.DeleteCredential(ctx, admin.User.ID, prepared.ID, successorMetadata.ETag, service.CredentialDeleteInput{Reason: "Delete historical successor"}); err != nil {
		t.Fatal(err)
	}
	goneSuccessor := decodeCatalogResponse[service.CredentialRetirementRecord](t, post(freshRouter, path, intent, etag, writeCookie, writeIdentity), 200)
	if !goneSuccessor.Committed || goneSuccessor.RuntimeApplied || !slices.Contains(goneSuccessor.Blockers, "replacement_missing") {
		t.Fatal("successor deletion erased historical commit")
	}
	currentSource, err = fresh.GetCredentialMetadata(ctx, admin.User.ID, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.DeleteCredential(ctx, admin.User.ID, source.ID, currentSource.ETag, service.CredentialDeleteInput{Reason: "Delete historical source"}); err != nil {
		t.Fatal(err)
	}
	goneSource := decodeCatalogResponse[service.CredentialRetirementRecord](t, post(freshRouter, path, intent, etag, writeCookie, writeIdentity), 200)
	if !goneSource.Committed || goneSource.RuntimeApplied || !slices.Contains(goneSource.Blockers, "source_missing") {
		t.Fatal("source deletion erased historical commit")
	}
	if err := db.Model(&entity.CredentialRetirementReceipt{}).Count(&receipts).Error; err != nil || receipts != 2 {
		t.Fatal("reconciliation duplicated or removed receipt", err)
	}
	if err := db.Model(&entity.AuditEvent{}).Where("action = ? AND resource_id = ?", "credential.retire", source.ID).Count(&audits).Error; err != nil || audits != 2 {
		t.Fatal("reconciliation duplicated audit", err)
	}
	var binding entity.ModelProviderBinding
	if err := db.First(&binding, "id = ?", model.Bindings[0].Binding.ID).Error; err != nil || binding.Weight != 100 || binding.ProviderModelID != access.ProviderModelID {
		t.Fatal("retirement changed routing weights/binding", err)
	}
	var retainedAttempt entity.CallAttempt
	if err := db.First(&retainedAttempt, "id = ?", historicalSourceAttempt.ID).Error; err != nil || retainedAttempt.CredentialID != source.ID || retainedAttempt.Status != "success" {
		t.Fatal("catalog deletion erased immutable call attribution", err)
	}
	if _, err := fresh.SetMemberRoles(ctx, admin.User.ID, writer.User.ID, nil); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, post(freshRouter, path, intent, etag, writeCookie, writeIdentity), 403)
	if connection.ID == "" {
		t.Fatal("catalog fixture missing")
	}
}
