package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/fox-gonic/fox"
	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
)

func testCredentialReplacementLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	store, err := secretstore.New(bytes.Repeat([]byte{77}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var verificationSecret, inferenceSecret atomic.Value
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/v1/models" {
			verificationSecret.Store(r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(`{"data":[{"id":"replacement-upstream"}]}`))
			return
		}
		inferenceSecret.Store(r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"model":"replacement-upstream","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer upstream.Close()
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"replacement@example.invalid","password":"test-only-replacement-password","name":"Replacement admin"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	reader, readCookie, readCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "replacement-reader", []string{"providers.read"})
	writer, writeCookie, writeCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "replacement-writer", []string{"providers.write"})
	_, otherCookie, otherCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "replacement-other-writer", []string{"providers.write"})
	provider, err := svc.CreateProvider(ctx, admin.User.ID, "Replacement provider", service.CreateConnectionInput{Name: "Replacement connection", BaseURL: upstream.URL + "/v1", Protocol: entity.ProtocolOpenAIChat, CredentialName: "Source", Secret: "test-only-source-secret"})
	if err != nil {
		t.Fatal(err)
	}
	source := provider.Connections[0].Credentials[0]
	connection := provider.Connections[0].Connection
	verification, err := svc.VerifyCredential(ctx, admin.User.ID, source.ID)
	if err != nil || !verification.Verified {
		t.Fatal("source controlled discovery failed")
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
	if err := db.First(&source, "id = ?", source.ID).Error; err != nil {
		t.Fatal(err)
	}
	var sourceAccess []entity.CredentialModelAccess
	if err := db.Where("credential_id = ?", source.ID).Find(&sourceAccess).Error; err != nil || len(sourceAccess) != 1 {
		t.Fatal("source discovery missing")
	}
	model, err := svc.CreateModel(ctx, admin.User.ID, "replacement-model", sourceAccess[0].ProviderModelID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetModelWeights(ctx, admin.User.ID, model.Model.ID, []service.ModelWeight{{BindingID: model.Bindings[0].Binding.ID, Weight: 100}}); err != nil {
		t.Fatal(err)
	}
	bearer := "rx_" + strings.Repeat("j", 43)
	for _, row := range []any{
		&entity.APIKey{ID: "key_replacement", UserID: admin.User.ID, Name: "Replacement Key", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(bearer), Status: entity.KeyActive},
		&entity.APIKeyModel{KeyID: "key_replacement", ModelID: model.Model.ID},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	svc.StopRuntime()
	if err := svc.StartCallRecorder(ctx, filepath.Join(t.TempDir(), "replacement.db")); err != nil {
		t.Fatal(err)
	}
	defer func() { svc.StopRuntime(); _ = svc.StopCallRecorder() }()
	path := "/api/v1/admin/credentials/" + source.ID + "/replacements"
	input := service.CredentialReplacementInput{RequestID: "87b043bb-cb96-419c-b9a2-cab04814b65e", Name: "Replacement", Secret: "test-only-replacement-secret", Reason: "Reviewed staged preparation"}
	request := func(target, raw, etag string, cookie *http.Cookie, csrf, origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "http://routex.test"+target, strings.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			req.AddCookie(cookie)
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		if etag != "" {
			req.Header.Set("If-Match", etag)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	prepare := func(candidate service.CredentialReplacementInput, validator string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(candidate)
		return request(path, string(raw), strconv.Quote(validator), writeCookie, writeCSRF, "")
	}
	decode := func(res *httptest.ResponseRecorder, status int) service.CredentialReplacementRecord {
		t.Helper()
		result := decodeCatalogResponse[service.CredentialReplacementRecord](t, res, status)
		var shape map[string]json.RawMessage
		if err := json.Unmarshal(res.Body.Bytes(), &shape); err != nil || len(shape) != 4 || result.StorageSource != "inline" || result.ID == source.ID || result.ConnectionID != connection.ID || result.ReplacesCredentialID != source.ID {
			t.Fatal("unsafe replacement acknowledgement shape")
		}
		for _, forbidden := range []string{"secret", "ciphertext", "request_hash", "runtime_applied", "verified", "enabled"} {
			if strings.Contains(res.Body.String(), forbidden) {
				t.Fatal("replacement response exposes sensitive data or implies later lifecycle state")
			}
		}
		return result
	}
	count := func(model any, clause string, values ...any) int64 {
		var result int64
		if err := db.Model(model).Where(clause, values...).Count(&result).Error; err != nil {
			t.Fatal(err)
		}
		return result
	}
	assertSource := func() {
		var current entity.ProviderCredential
		if err := db.First(&current, "id = ?", source.ID).Error; err != nil || !reflect.DeepEqual(source, current) {
			t.Fatal("replacement lifecycle changed source credential")
		}
		var currentAccess []entity.CredentialModelAccess
		if err := db.Where("credential_id = ?", source.ID).Find(&currentAccess).Error; err != nil || !reflect.DeepEqual(sourceAccess, currentAccess) {
			t.Fatal("replacement lifecycle changed source discovery")
		}
	}
	raw, _ := json.Marshal(input)
	expectStatus(t, request(path, string(raw), strconv.Quote(metadata.ETag), nil, "", ""), 401)
	expectStatus(t, request(path, string(raw), strconv.Quote(metadata.ETag), readCookie, readCSRF, ""), 403)
	expectStatus(t, request(path, string(raw), strconv.Quote(metadata.ETag), writeCookie, "", ""), 403)
	expectStatus(t, request(path, string(raw), strconv.Quote(metadata.ETag), writeCookie, writeCSRF, "https://foreign.example.invalid"), 403)
	if _, _, err := svc.CreateCredentialReplacement(ctx, reader.User.ID, source.ID, metadata.ETag, input); err == nil {
		t.Fatal("service accepted unauthorized preparation")
	}
	for _, candidate := range []service.CredentialReplacementInput{
		{Name: input.Name, Secret: input.Secret, Reason: input.Reason},
		{RequestID: strings.ToUpper(input.RequestID), Name: input.Name, Secret: input.Secret, Reason: input.Reason},
		{RequestID: "00000000-0000-0000-0000-000000000000", Name: input.Name, Secret: input.Secret, Reason: input.Reason},
		{RequestID: input.RequestID, Name: " ", Secret: input.Secret, Reason: input.Reason},
		{RequestID: input.RequestID, Name: "bad\nname", Secret: input.Secret, Reason: input.Reason},
		{RequestID: input.RequestID, Name: strings.Repeat("界", 101), Secret: input.Secret, Reason: input.Reason},
		{RequestID: input.RequestID, Name: input.Name, Reason: input.Reason},
		{RequestID: input.RequestID, Name: input.Name, Secret: strings.Repeat("s", 2049), Reason: input.Reason},
		{RequestID: input.RequestID, Name: input.Name, Secret: "bad\nsecret", Reason: input.Reason},
		{RequestID: input.RequestID, Name: input.Name, Secret: input.Secret, Reason: "bad\nreason"},
		{RequestID: input.RequestID, Name: input.Name, Secret: input.Secret, Reason: strings.Repeat("界", 342)},
	} {
		expectStatus(t, prepare(candidate, metadata.ETag), 400)
	}
	for _, body := range []string{string(raw) + `{}`, strings.TrimSuffix(string(raw), "}") + `,"priority":0}`, strings.TrimSuffix(string(raw), "}") + `,"connection_id":"forbidden"}`} {
		expectStatus(t, request(path, body, strconv.Quote(metadata.ETag), writeCookie, writeCSRF, ""), 400)
	}
	for _, validator := range []string{"", metadata.ETag, `W/"` + metadata.ETag + `"`, `*`, `"0"`} {
		expectStatus(t, request(path, string(raw), validator, writeCookie, writeCSRF, ""), 400)
	}
	expectStatus(t, prepare(input, strings.Repeat("0", 64)), 409)
	duplicate := input
	duplicate.Name = " source "
	expectStatus(t, prepare(duplicate, metadata.ETag), 409)
	missingID, err := id.NewPrefixed("crd")
	if err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request(strings.Replace(path, source.ID, missingID, 1), string(raw), strconv.Quote(metadata.ETag), writeCookie, writeCSRF, ""), 404)
	expectStatus(t, request(strings.Replace(path, source.ID, "crd_missing", 1), string(raw), strconv.Quote(metadata.ETag), writeCookie, writeCSRF, ""), 400)
	withoutStore, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := withoutStore.CreateCredentialReplacement(ctx, writer.User.ID, source.ID, metadata.ETag, input); err == nil {
		t.Fatal("preparation without secret storage accepted")
	}
	rollback := "test:replacement_receipt_rollback"
	if err := db.Callback().Create().After("gorm:create").Register(rollback, func(tx *gorm.DB) {
		if tx.Statement.Table == "credential_replacement_receipts" {
			_ = tx.AddError(errors.New("test-only receipt rollback"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, prepare(input, metadata.ETag), 500)
	if err := db.Callback().Create().Remove(rollback); err != nil {
		t.Fatal(err)
	}
	if count(&entity.ProviderCredential{}, "connection_id = ?", connection.ID) != 1 || count(&entity.CredentialReplacementReceipt{}, "request_id = ?", input.RequestID) != 0 || count(&entity.AuditEvent{}, "action = ?", "credential.replacement.create") != 0 {
		t.Fatal("failed receipt left partial replacement creation")
	}
	snapshot := svc.RuntimeStatus().SnapshotID
	beforeRequests := requests.Load()
	responses := make(chan *httptest.ResponseRecorder, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { responses <- prepare(input, metadata.ETag) })
	}
	wg.Wait()
	close(responses)
	statuses := map[int]int{}
	var prepared service.CredentialReplacementRecord
	for result := range responses {
		statuses[result.Code]++
		value := decode(result, result.Code)
		if prepared.ID != "" && prepared.ID != value.ID {
			t.Fatal("same request created different IDs")
		}
		prepared = value
	}
	if statuses[201] != 1 || statuses[200] != 1 || count(&entity.CredentialReplacementReceipt{}, "request_id = ?", input.RequestID) != 1 || count(&entity.AuditEvent{}, "action = ?", "credential.replacement.create") != 1 {
		t.Fatalf("concurrent preparation was not deduplicated: %v", statuses)
	}
	if requests.Load() != beforeRequests || svc.RuntimeStatus().SnapshotID != snapshot {
		t.Fatal("pending preparation performed upstream work or runtime publication")
	}
	assertSource()
	var replacement entity.ProviderCredential
	if err := db.First(&replacement, "id = ?", prepared.ID).Error; err != nil || replacement.Enabled || replacement.VerificationStatus != "pending" || replacement.VerifiedAt != nil || replacement.Priority != source.Priority || replacement.ReplacesCredentialID == nil || *replacement.ReplacesCredentialID != source.ID || count(&entity.CredentialModelAccess{}, "credential_id = ?", prepared.ID) != 0 {
		t.Fatal("new replacement received activation, verification, or copied coverage")
	}
	if plaintext, err := store.Open(replacement.ID, replacement.Ciphertext); err != nil || plaintext != input.Secret {
		t.Fatal("replacement encryption not bound to new identity")
	}
	if _, err := store.Open(source.ID, replacement.Ciphertext); err == nil {
		t.Fatal("replacement envelope accepted source identity")
	}
	if _, err := store.Open(replacement.ID, source.Ciphertext); err == nil {
		t.Fatal("source envelope accepted replacement identity")
	}
	for _, change := range []func(*service.CredentialReplacementInput){
		func(body *service.CredentialReplacementInput) { body.Name = "Other name" },
		func(body *service.CredentialReplacementInput) { body.Reason = "Other reason" },
		func(body *service.CredentialReplacementInput) { body.Secret = "different-secret" },
	} {
		candidate := input
		change(&candidate)
		expectStatus(t, prepare(candidate, metadata.ETag), 409)
	}
	expectStatus(t, prepare(input, strings.Repeat("0", 64)), 409)
	expectStatus(t, request(path, string(raw), strconv.Quote(metadata.ETag), otherCookie, otherCSRF, ""), 409)
	expectStatus(t, request(strings.Replace(path, source.ID, prepared.ID, 1), string(raw), strconv.Quote(metadata.ETag), writeCookie, writeCSRF, ""), 409)
	if _, _, err := withoutStore.CreateCredentialReplacement(ctx, writer.User.ID, source.ID, metadata.ETag, input); err == nil {
		t.Fatal("receipt retry without secret storage accepted")
	}
	restartDB, err := database.Open(ctx, db.Name(), os.Getenv("ROUTEX_TEST_"+strings.ToUpper(db.Name())+"_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	restartPool, err := restartDB.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = restartPool.Close() }()
	restartStore, err := secretstore.New(bytes.Repeat([]byte{77}, 32))
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := service.New(ctx, restartDB, service.WithCredentialStorage(restartStore), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	restartedRouter := fox.New()
	New(restarted).RegisterRoutes(restartedRouter)
	retryRequest := httptest.NewRequest("POST", "http://routex.test"+path, bytes.NewReader(raw))
	retryRequest.Header.Set("Content-Type", "application/json")
	retryRequest.Header.Set("If-Match", strconv.Quote(metadata.ETag))
	retryRequest.Header.Set("X-CSRF-Token", writeCSRF)
	retryRequest.AddCookie(writeCookie)
	retried := httptest.NewRecorder()
	restartedRouter.ServeHTTP(retried, retryRequest)
	if decode(retried, 200).ID != prepared.ID {
		t.Fatal("restarted service did not reuse durable receipt")
	}
	infer := func() {
		req := httptest.NewRequest("POST", "http://routex.test/v1/chat/completions", strings.NewReader(`{"model":"replacement-model","messages":[{"role":"user","content":"test"}]}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+bearer)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		expectStatus(t, res, 200)
		if inferenceSecret.Load() != "Bearer test-only-source-secret" {
			t.Fatal("replacement preparation changed active credential selection")
		}
	}
	infer()
	if _, err := svc.SetCredentialEnabled(ctx, admin.User.ID, prepared.ID, true); err == nil {
		t.Fatal("unverified replacement was enabled")
	}
	verification, err = svc.VerifyCredential(ctx, admin.User.ID, prepared.ID)
	if err != nil || !verification.Verified || verificationSecret.Load() != "Bearer "+input.Secret {
		t.Fatal("replacement verification did not use its actual new secret")
	}
	if err := db.First(&replacement, "id = ?", prepared.ID).Error; err != nil || replacement.Enabled || replacement.VerificationStatus != "verified" {
		t.Fatal("verification silently enabled replacement")
	}
	assertSource()
	if _, err := svc.SetCredentialEnabled(ctx, admin.User.ID, prepared.ID, true); err != nil {
		t.Fatal(err)
	}
	assertSource()
	infer() // Equal priority keeps the older source first; creation is not rotation completion.
	extra := input
	extra.RequestID, extra.Name = "07b043bb-cb96-419c-b9a2-cab04814b65e", "Second replacement"
	extraResult := decode(prepare(extra, metadata.ETag), 201)
	if extraResult.ID == prepared.ID {
		t.Fatal("different authorized intent could not prepare a distinct replacement")
	}
	var receipt entity.CredentialReplacementReceipt
	if err := db.First(&receipt, "request_id = ?", input.RequestID).Error; err != nil || receipt.ResultCredentialID != prepared.ID || receipt.ActorID != writer.User.ID || receipt.SourceCredentialID != source.ID || len(receipt.RequestHash) != 64 {
		t.Fatal("incorrect durable receipt")
	}
	var event entity.AuditEvent
	if err := db.First(&event, "action = ? AND resource_id = ?", "credential.replacement.create", prepared.ID).Error; err != nil || event.DetailsJSON == nil {
		t.Fatal("replacement audit missing")
	}
	for _, encoded := range []string{*event.DetailsJSON, receipt.RequestHash} {
		for _, forbidden := range []string{input.Secret, "test-only-source-secret", "ciphertext", "request_hash", secret.SHA256Hex(input.Secret)} {
			if strings.Contains(encoded, forbidden) {
				t.Fatal("audit or receipt retained secret material")
			}
		}
	}
	// Source deletion does not remove the receipt or immutable lineage, and the
	// exact preparation retry can still identify the existing result.
	if _, err := svc.DeleteCredential(ctx, admin.User.ID, source.ID, metadata.ETag, service.CredentialDeleteInput{Reason: "Independent source removal"}); err != nil {
		t.Fatal(err)
	}
	if decode(prepare(input, metadata.ETag), 200).ID != prepared.ID || count(&entity.CredentialReplacementReceipt{}, "source_credential_id = ?", source.ID) != 2 {
		t.Fatal("source deletion broke durable preparation retry")
	}
	providers := decodeCatalogResponse[ProvidersResponse](t, identityRequest(router, "GET", "/api/v1/admin/providers", "", adminCookie, ""), 200)
	lineageFound := false
	for _, connection := range providers.Items[0].Connections {
		for _, credential := range connection.Credentials {
			if credential.ID == prepared.ID {
				lineageFound = credential.ReplacesCredentialID != nil && *credential.ReplacesCredentialID == source.ID
			}
		}
	}
	if !lineageFound {
		t.Fatal("catalog lost historical replacement linkage after source deletion")
	}
	resultMetadata, err := svc.GetCredentialMetadata(ctx, admin.User.ID, prepared.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DeleteCredential(ctx, admin.User.ID, prepared.ID, resultMetadata.ETag, service.CredentialDeleteInput{Reason: "Independent result removal"}); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, prepare(input, metadata.ETag), 409)
	if count(&entity.CredentialReplacementReceipt{}, "request_id = ?", input.RequestID) != 1 || count(&entity.AuditEvent{}, "action = ? AND resource_id = ?", "credential.replacement.create", prepared.ID) != 1 || count(&entity.ProviderCredential{}, "connection_id = ?", connection.ID) != 1 {
		t.Fatal("deleted result receipt permitted another credential creation")
	}
	if _, err := svc.SetMemberRoles(ctx, admin.User.ID, writer.User.ID, nil); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, prepare(extra, metadata.ETag), 403)
	if _, _, err := svc.CreateCredentialReplacement(ctx, writer.User.ID, source.ID, metadata.ETag, extra); err == nil {
		t.Fatal("receipt retry bypassed revoked write authorization")
	}
}
