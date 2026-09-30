package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

func testGatewayAttachmentQuotaSettlementLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	secrets, err := secretstore.New(bytes.Repeat([]byte{83}, 32))
	if err != nil {
		t.Fatal(err)
	}
	storage, stored := newStorageFixture(t)
	const objectID = "obj_01arz3ndektsv4rrffq69g5fav"
	attachment := []byte("attachment image")
	version := "attachment-version"
	stored.objects["/routex-test/routex/"+objectID] = storageFixtureObject{data: attachment, owner: objectID, version: version}

	var responseMode atomic.Int32
	var dispatches atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		dispatches.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch responseMode.Load() {
		case 1:
			_, _ = io.WriteString(w, `{"object":"chat.completion","model":"native-attachment","choices":[{"finish_reason":"stop"}]}`)
		case 2:
			_, _ = io.WriteString(w, `{"object":"chat.completion","model":"native-attachment","choices":[{"finish_reason":"stop"}],"usage":{"prompt_tokens":15,"completion_tokens":10,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"image_tokens":1}}}`)
		default:
			_, _ = io.WriteString(w, `{"object":"chat.completion","model":"native-attachment","choices":[{"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0,"image_tokens":1}}}`)
		}
	}))
	defer upstream.Close()

	svc, err := service.New(ctx, db, service.WithCredentialStorage(secrets), service.WithUpstreamPolicy(true), service.WithStoragePolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, http.MethodPost, "/api/v1/setup", `{"email":"attachment-quota@example.invalid","password":"attachment-quota-password","name":"Attachment quota"}`, nil, "")
	expectStatus(t, setup, http.StatusCreated)
	admin, _ := readIdentity(t, setup)

	providerCiphertext, err := secrets.Seal("crd_attachment_quota", "provider-secret")
	if err != nil {
		t.Fatal(err)
	}
	storageAuth, _ := json.Marshal(map[string]string{"AccessKey": "test-only-access", "SecretKey": "test-only-secret"})
	storageCiphertext, err := secrets.Seal("storage:str_attachment_quota:generation-one", string(storageAuth))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	digest := sha256.Sum256(attachment)
	bearer := "rx_" + strings.Repeat("z", 43)
	for _, row := range []any{
		&entity.Provider{ID: "prv_attachment_quota", Name: "Attachment quota provider"},
		&entity.ProviderConnection{ID: "con_attachment_quota", ProviderID: "prv_attachment_quota", Name: "Native", Protocol: entity.ProtocolOpenAIChat, BaseURL: upstream.URL + "/v1"},
		&entity.ProviderCredential{ID: "crd_attachment_quota", ConnectionID: "con_attachment_quota", Name: "Verified", Ciphertext: providerCiphertext, Enabled: true, VerificationStatus: "verified"},
		&entity.ProviderModel{ID: "pmd_attachment_quota", ConnectionID: "con_attachment_quota", UpstreamName: "native-attachment", SupportsImageInput: true},
		&entity.CredentialModelAccess{CredentialID: "crd_attachment_quota", ProviderModelID: "pmd_attachment_quota"},
		&entity.Model{ID: "mdl_attachment_quota", Status: "active", CreatedAt: now},
		&entity.ModelName{Name: "attachment-model", ModelID: "mdl_attachment_quota", CurrentModelID: stringPointer("mdl_attachment_quota")},
		&entity.ModelProviderBinding{ID: "bnd_attachment_quota", ModelID: "mdl_attachment_quota", ProviderModelID: "pmd_attachment_quota", Weight: 100},
		&entity.UserModelGrant{UserID: admin.User.ID, ModelID: "mdl_attachment_quota"},
		&entity.APIKey{ID: "key_attachment_quota", UserID: admin.User.ID, Name: "Attachment", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(bearer), Status: entity.KeyActive, CreatedAt: now},
		&entity.APIKeyModel{KeyID: "key_attachment_quota", ModelID: "mdl_attachment_quota"},
		&entity.ModelPrice{ID: "prc_attachment_quota", ProviderModelID: "pmd_attachment_quota"},
		&entity.PriceRate{ID: "ptr_attachment_input", ModelPriceID: "prc_attachment_quota", Metric: pricing.Input, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "0", Enabled: true},
		&entity.PriceRate{ID: "ptr_attachment_output", ModelPriceID: "prc_attachment_quota", Metric: pricing.Output, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "0", Enabled: true},
		&entity.PriceRate{ID: "ptr_attachment_cache_read", ModelPriceID: "prc_attachment_quota", Metric: pricing.CacheRead, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "0", Enabled: true},
		&entity.PriceRate{ID: "ptr_attachment_cache_write", ModelPriceID: "prc_attachment_quota", Metric: pricing.CacheWrite, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "0", Enabled: true},
		&entity.PriceRate{ID: "ptr_attachment_image", ModelPriceID: "prc_attachment_quota", Metric: pricing.ImageInput, Tier: pricing.Base, Unit: pricing.ImageUnit, Currency: "USD", Amount: "2", Enabled: true},
		&entity.PriceRate{ID: "ptr_attachment_pdf", ModelPriceID: "prc_attachment_quota", Metric: pricing.PDFInput, Tier: pricing.Base, Unit: pricing.PDFUnit, Currency: "USD", Amount: "0", Enabled: true},
		&entity.ReservationBound{ProviderModelID: "pmd_attachment_quota", Protocol: entity.ProtocolOpenAIChat, MaxInputTokens: 10, MaxOutputTokens: 10, Evidence: "Controlled attachment quota fixture", ETag: "bound_attachment_quota", Reason: "Integration coverage", UpdatedAt: now},
		&entity.ResourceLimit{ScopeKind: "user", ScopeID: admin.User.ID, ETag: "limit_attachment_quota", Reason: "Integration coverage", Tokens5H: int64Pointer(100), MoneyMonth: stringPointer("100"), Currency: "USD"},
		&entity.StorageRevision{ID: "str_attachment_quota", Endpoint: storage.URL, Region: "us-east-1", Bucket: "routex-test", SecretGeneration: "generation-one", AuthCiphertext: storageCiphertext, VerifiedAt: &now, CreatedBy: admin.User.ID, CreatedAt: now},
		&entity.StorageObject{ID: objectID, OwnerID: admin.User.ID, RevisionID: "str_attachment_quota", Purpose: "attachment", State: "ready", Name: "image.png", MIME: "image/png", Size: int64(len(attachment)), SHA256: hex.EncodeToString(digest[:]), VersionID: version, NextCleanupAt: now.Add(time.Hour), CreatedAt: now},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Model(&entity.User{}).Where("id = ?", admin.User.ID).Update("created_at", now).Error; err != nil {
		t.Fatal(err)
	}
	startupStarted := time.Now()
	if err := svc.StartRuntime(ctx); err != nil {
		elapsed := time.Since(startupStarted).Round(time.Millisecond)
		diagnosticCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		var publication struct {
			Status    string
			ErrorCode string
		}
		diagnosticErr := db.WithContext(diagnosticCtx).Model(&entity.RuntimePublication{}).
			Select("status", "error_code").Order("created_at DESC").Take(&publication).Error
		cancel()
		if diagnosticErr != nil {
			t.Fatalf("runtime startup failed after %s: %v; publication metadata unavailable", elapsed, err)
		}
		t.Fatalf("runtime startup failed after %s: %v; publication status=%q error_code=%q", elapsed, err, publication.Status, publication.ErrorCode)
	}
	spool := filepath.Join(t.TempDir(), "attachment-quota.db")
	bootstrapJournal, err := eventqueue.Open(spool, 4096, 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	if err := bootstrapJournal.EnableQuota("UTC", now.Add(-time.Minute)); err != nil {
		_ = bootstrapJournal.Close()
		t.Fatal(err)
	}
	if err := bootstrapJournal.Close(); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	defer svc.StopRuntime()
	defer func() { _ = svc.StopCallRecorder() }()

	call := func(body string) (string, *httptest.ResponseRecorder) {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+bearer)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res.Header().Get("X-Request-ID"), res
	}
	attachmentBody := `{"model":"attachment-model","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"routex://attachments/` + objectID + `"}}]}],"max_completion_tokens":10}`
	unexpectedMediaBody := `{"model":"attachment-model","messages":[{"role":"user","content":"plain text"}],"max_completion_tokens":10}`

	unexpectedID, unexpected := call(unexpectedMediaBody)
	expectStatus(t, unexpected, http.StatusOK)
	completeID, complete := call(attachmentBody)
	expectStatus(t, complete, http.StatusOK)
	responseMode.Store(1)
	incompleteID, incomplete := call(attachmentBody)
	expectStatus(t, incomplete, http.StatusOK)
	responseMode.Store(2)
	overrunID, overrun := call(attachmentBody)
	expectStatus(t, overrun, http.StatusOK)
	beforeRejected := dispatches.Load()
	_, rejected := call(attachmentBody)
	expectStatus(t, rejected, http.StatusServiceUnavailable)
	if dispatches.Load() != beforeRejected {
		t.Fatal("invalidated reservation bound dispatched another request")
	}
	if !strings.Contains(rejected.Body.String(), `"code":"quota_bound_unavailable"`) {
		t.Fatalf("unexpected invalidated-bound response: %s", rejected.Body.String())
	}

	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	var unexpectedFact entity.CallRecord
	if err := db.First(&unexpectedFact, "request_id = ?", unexpectedID).Error; err != nil {
		t.Fatal(err)
	}
	if !equalOptionalInt64(unexpectedFact.ImageInputs, int64Pointer(0)) || !equalOptionalInt64(unexpectedFact.PDFInputs, int64Pointer(0)) || unexpectedFact.PricingStatus != "unsupported" || unexpectedFact.PricingSnapshotJSON == nil || !strings.Contains(*unexpectedFact.PricingSnapshotJSON, "response_non_text") {
		t.Fatalf("unexpected input media was not recorded durably: %+v", unexpectedFact)
	}
	for _, test := range []struct {
		requestID     string
		input, output *int64
		pricingStatus string
		chargeAmount  *string
	}{
		{requestID: completeID, input: int64Pointer(4), output: int64Pointer(1), pricingStatus: "priced", chargeAmount: stringPointer("2")},
		{requestID: incompleteID, pricingStatus: "not_final"},
		{requestID: overrunID, input: int64Pointer(15), output: int64Pointer(10), pricingStatus: "priced", chargeAmount: stringPointer("2")},
	} {
		var fact entity.CallRecord
		if err := db.First(&fact, "request_id = ?", test.requestID).Error; err != nil {
			t.Fatal(err)
		}
		if !equalOptionalInt64(fact.InputTokens, test.input) || !equalOptionalInt64(fact.OutputTokens, test.output) || !equalOptionalInt64(fact.ImageInputs, int64Pointer(1)) || !equalOptionalInt64(fact.PDFInputs, int64Pointer(0)) || fact.PricingStatus != test.pricingStatus || !equalOptionalString(fact.ChargeAmount, test.chargeAmount) {
			t.Fatalf("call fact %s = %+v", test.requestID, fact)
		}
	}

	svc.StopRuntime()
	if err := svc.StopCallRecorder(); err != nil {
		t.Fatal(err)
	}
	journal, err := eventqueue.Open(spool, 4096, 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = journal.Close() }()
	unexpectedReceipt, err := journal.QuotaReceipt(unexpectedID)
	if err != nil {
		t.Fatal(err)
	}
	if unexpectedReceipt.State != "complete" || unexpectedReceipt.Bound.Tokens == nil || *unexpectedReceipt.Bound.Tokens != 20 || unexpectedReceipt.Bound.Money == nil || *unexpectedReceipt.Bound.Money != "0" || unexpectedReceipt.Actual.Tokens == nil || *unexpectedReceipt.Actual.Tokens != 5 || unexpectedReceipt.Actual.Money != nil {
		t.Fatalf("unexpected input media did not release its reservation: %+v", unexpectedReceipt)
	}
	assertAttachmentQuotaReceipt(t, journal, completeID, 5, false, false, stringPointer("2"))
	assertAttachmentQuotaReceipt(t, journal, incompleteID, 0, true, false, nil)
	assertAttachmentQuotaReceipt(t, journal, overrunID, 25, false, true, stringPointer("2"))
}

func assertAttachmentQuotaReceipt(t *testing.T, journal *eventqueue.Queue, requestID string, actual int64, actualUnknown, overrun bool, money *string) {
	t.Helper()
	receipt, err := journal.QuotaReceipt(requestID)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.State != "complete" || receipt.Bound.Tokens == nil || *receipt.Bound.Tokens != 20 || receipt.Bound.Money == nil || receipt.Overrun != overrun || !equalOptionalString(receipt.Actual.Money, money) {
		t.Fatalf("quota receipt %s = %+v", requestID, receipt)
	}
	if actualUnknown {
		if receipt.Actual.Tokens != nil {
			t.Fatalf("incomplete usage fabricated actual tokens: %+v", receipt)
		}
		return
	}
	if receipt.Actual.Tokens == nil || *receipt.Actual.Tokens != actual {
		t.Fatalf("quota receipt %s actual = %+v, want %d", requestID, receipt.Actual, actual)
	}
}

func equalOptionalInt64(actual, expected *int64) bool {
	if actual == nil || expected == nil {
		return actual == nil && expected == nil
	}
	return *actual == *expected
}

func equalOptionalString(actual, expected *string) bool {
	if actual == nil || expected == nil {
		return actual == nil && expected == nil
	}
	return *actual == *expected
}

func int64Pointer(value int64) *int64    { return &value }
func stringPointer(value string) *string { return &value }
