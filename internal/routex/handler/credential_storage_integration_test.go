package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

func testProviderCredentialStorageLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	ring, err := secretstore.NewKeyring(map[string][]byte{"old": bytes.Repeat([]byte{125}, 32), "next": bytes.Repeat([]byte{126}, 32)}, "old", "old")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	values := map[string]map[string]string{}
	var writes, reads, discovery atomic.Int32
	var lostWrite, denyRead atomic.Bool
	vaultStub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !strings.HasPrefix(r.URL.Path, "/v1/kv/data/") {
			t.Error("unexpected Vault operation")
			w.WriteHeader(400)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case "POST":
			writes.Add(1)
			if r.Header.Get("X-Vault-Token") != "storage-writer" {
				t.Error("writer identity was not used")
				w.WriteHeader(403)
				return
			}
			var body struct {
				Data    map[string]string `json:"data"`
				Options struct {
					CAS int `json:"cas"`
				} `json:"options"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || len(body.Data) != 2 || body.Options.CAS != 0 {
				t.Error("invalid credential CAS0 envelope")
				w.WriteHeader(400)
				return
			}
			if _, exists := values[r.URL.Path]; exists {
				t.Error("creation Write replayed")
				w.WriteHeader(409)
				return
			}
			// The actual database must already contain a durable owned claim before
			// this effect. No transaction or publication lock is held by the caller.
			var count int64
			if e := db.Model(&entity.CredentialStorageOperation{}).Where("state = ? AND claim <> ?", "writing", "").Count(&count).Error; e != nil || count < 1 {
				t.Error("remote Write preceded durable claim")
			}
			values[r.URL.Path] = body.Data
			if lostWrite.Swap(false) {
				w.WriteHeader(503)
				return
			}
			_, _ = w.Write([]byte(`{"data":{"version":1,"destroyed":false,"deletion_time":""}}`))
		case "GET":
			reads.Add(1)
			if r.Header.Get("X-Vault-Token") != "storage-reader" || r.URL.RawQuery != "version=1" {
				t.Error("exact retained reader/version not used")
				w.WriteHeader(403)
				return
			}
			if denyRead.Load() {
				w.WriteHeader(503)
				return
			}
			data, ok := values[r.URL.Path]
			if !ok {
				w.WriteHeader(404)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"data": data, "metadata": map[string]any{"version": 1, "destroyed": false, "deletion_time": ""}}})
		default:
			t.Error("unexpected destructive/write operation")
			w.WriteHeader(400)
		}
	}))
	defer vaultStub.Close()
	supply := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/v1/models" {
			t.Error("unexpected native inference")
			w.WriteHeader(400)
			return
		}
		discovery.Add(1)
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer fixture-storage-secret") {
			t.Error("source value not used for verification")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"storage-upstream"}]}`))
	}))
	defer supply.Close()
	svc, err := service.New(ctx, db, service.WithCredentialStorage(ring), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.InitializeSecretStore(ctx); err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"storage-admin@example.invalid","password":"test-only-storage-password","name":"Storage admin"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, cookie := readIdentity(t, setup)
	_, writerCookie, writerCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "storage-writer", []string{"providers.write"})
	_, readerCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "storage-reader", []string{"providers.read"})
	_, otherCookie, otherCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "storage-other-writer", []string{"providers.write"})
	request := func(method, path string, body any, etag string, session *http.Cookie, csrf string) *httptest.ResponseRecorder {
		t.Helper()
		raw := ""
		if body != nil {
			data, e := json.Marshal(body)
			if e != nil {
				t.Fatal(e)
			}
			raw = string(data)
		}
		req := httptest.NewRequest(method, "http://routex.test"+path, strings.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		if session != nil {
			req.AddCookie(session)
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		if method != "GET" {
			req.Header.Set("Origin", "http://routex.test")
		}
		if etag != "" {
			req.Header.Set("If-Match", fmt.Sprintf("%q", etag))
		}
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		// Public responses never carry the submitted values/auth or private refs.
		for _, forbidden := range []string{"fixture-storage-secret", "storage-writer\"", "storage-reader\"", "auth_ciphertext", "expected_marker_sha256", "descriptor_sha256", "reference_id"} {
			if strings.Contains(out.Body.String(), forbidden) {
				t.Fatal("private source material in public response")
			}
		}
		return out
	}
	contextRead := func(session *http.Cookie) service.CredentialStorageContext {
		t.Helper()
		r := request("GET", "/api/v1/admin/provider-credential-storage-context", nil, "", session, "")
		expectStatus(t, r, 200)
		var v service.CredentialStorageContext
		if json.Unmarshal(r.Body.Bytes(), &v) != nil || r.Header().Get("ETag") != fmt.Sprintf("%q", v.ETag) || len(v.ETag) != 64 || !strings.Contains(r.Header().Get("Cache-Control"), "no-store") {
			t.Fatal("raw JSON/quoted header context contract")
		}
		return v
	}
	policyPath := "/api/v1/admin/secrets/provider-storage"
	policyRead := func() service.CredentialStoragePolicyView {
		t.Helper()
		r := request("GET", policyPath, nil, "", cookie, "")
		expectStatus(t, r, 200)
		var v service.CredentialStoragePolicyView
		if json.Unmarshal(r.Body.Bytes(), &v) != nil || r.Header().Get("ETag") != fmt.Sprintf("%q", v.ETag) {
			t.Fatal("policy header contract")
		}
		return v
	}
	policySave := func(mode string, integration, revision *string) {
		t.Helper()
		v := policyRead()
		r := request("PUT", policyPath, service.CredentialStoragePolicyInput{Mode: mode, IntegrationID: integration, RevisionID: revision, Reason: "Reviewed future writes"}, v.ETag, cookie, admin.CSRFToken)
		expectStatus(t, r, 200)
	}
	expectStatus(t, request("GET", policyPath, nil, "", writerCookie, ""), 403)
	expectStatus(t, request("GET", "/api/v1/admin/provider-credential-storage-context", nil, "", readerCookie, ""), 403)
	initialContext := contextRead(writerCookie)
	if initialContext.StorageSource != "inline" {
		t.Fatal("legacy default is not inline")
	}
	uuid := func(n int) string { return fmt.Sprintf("77000000-1111-4111-8111-%012d", n) }
	bootstrap := map[string]any{"name": "Inline provider", "connection_name": "Inline Connection", "base_url": supply.URL + "/v1", "protocol": "openai_chat", "credential_name": "Inline Credential", "secret": "fixture-storage-secret-inline", "request_id": uuid(1), "storage_policy_etag": initialContext.ETag}
	first := request("POST", "/api/v1/admin/providers", bootstrap, "", writerCookie, writerCSRF)
	expectStatus(t, first, 201)
	var inline ProviderResponse
	if json.Unmarshal(first.Body.Bytes(), &inline) != nil || len(inline.Connections) != 1 || len(inline.Connections[0].Credentials) != 1 || inline.Connections[0].Credentials[0].StorageSource != "inline" || !inline.Connections[0].Enabled {
		t.Fatal("exact inline bootstrap projection")
	}
	again := request("POST", "/api/v1/admin/providers", bootstrap, "", writerCookie, writerCSRF)
	expectStatus(t, again, 201)
	var dedup ProviderResponse
	_ = json.Unmarshal(again.Body.Bytes(), &dedup)
	if !reflect.DeepEqual(inline, dedup) || writes.Load() != 0 {
		t.Fatal("explicit UUID inline creation did not deduplicate")
	}
	integrations, err := svc.ListVaultIntegrations(ctx, admin.User.ID, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	config := service.VaultConfigInput{RequestID: uuid(2), Name: "Credential Vault", Descriptor: service.VaultDescriptor{Endpoint: vaultStub.URL, Mount: "kv", Prefix: "provider-secrets", DataField: "value"}, WriterAuth: service.VaultAuthInput{Action: "replace", Token: "storage-writer"}, ReaderAuth: service.VaultAuthInput{Action: "replace", Token: "storage-reader"}, Reason: "Reviewed credential source"}
	saved, err := svc.SaveVaultIntegration(ctx, admin.User.ID, "", integrations.ReviewETag, config)
	if err != nil {
		t.Fatal(err)
	}
	policySave("vault", &saved.IntegrationID, &saved.RevisionID)
	originalContext := contextRead(writerCookie)
	if originalContext.StorageSource != "vault" {
		t.Fatal("new policy context not Vault")
	}
	beforeWrites := writes.Load()
	stale := map[string]any{"name": "Stale", "secret": "fixture-storage-secret-stale", "priority": 1, "request_id": uuid(3), "storage_policy_etag": initialContext.ETag}
	credentialPath := "/api/v1/admin/connections/" + inline.Connections[0].ID + "/credentials"
	expectStatus(t, request("POST", credentialPath, stale, "", writerCookie, writerCSRF), 409)
	delete(stale, "request_id")
	stale["storage_policy_etag"] = originalContext.ETag
	expectStatus(t, request("POST", credentialPath, stale, "", writerCookie, writerCSRF), 400)
	if writes.Load() != beforeWrites {
		t.Fatal("stale/missing intent caused external effect")
	}
	vaultProviderBody := map[string]any{"name": "Vault provider", "connection_name": "Vault Connection", "base_url": supply.URL + "/v1", "protocol": "openai_chat", "credential_name": "Vault first", "secret": "fixture-storage-secret-provider", "request_id": uuid(4), "storage_policy_etag": originalContext.ETag}
	out := request("POST", "/api/v1/admin/providers", vaultProviderBody, "", writerCookie, writerCSRF)
	expectStatus(t, out, 201)
	var provider ProviderResponse
	if json.Unmarshal(out.Body.Bytes(), &provider) != nil || len(provider.Connections) != 1 || len(provider.Connections[0].Credentials) != 1 || provider.Connections[0].Credentials[0].StorageSource != "vault" || !provider.Connections[0].Enabled {
		t.Fatal("Vault Provider bootstrap/source contract")
	}
	connectionBody := map[string]any{"name": "Second Vault Connection", "base_url": supply.URL + "/v1", "protocol": "openai_chat", "credential_name": "Vault second", "secret": "fixture-storage-secret-connection", "request_id": uuid(5), "storage_policy_etag": originalContext.ETag}
	out = request("POST", "/api/v1/admin/providers/"+provider.ID+"/connections", connectionBody, "", writerCookie, writerCSRF)
	expectStatus(t, out, 201)
	var connection ConnectionResponse
	if json.Unmarshal(out.Body.Bytes(), &connection) != nil || len(connection.Credentials) != 1 || connection.Credentials[0].StorageSource != "vault" || !connection.Enabled {
		t.Fatal("Vault Connection bootstrap/source contract")
	}
	credentialBody := map[string]any{"name": "Additional Vault Credential", "secret": "fixture-storage-secret-credential", "priority": 7, "request_id": uuid(6), "storage_policy_etag": originalContext.ETag}
	out = request("POST", credentialPath, credentialBody, "", writerCookie, writerCSRF)
	expectStatus(t, out, 201)
	var credential CredentialResponse
	if json.Unmarshal(out.Body.Bytes(), &credential) != nil || credential.StorageSource != "vault" || credential.Enabled || credential.VerificationStatus != "pending" {
		t.Fatal("Vault Credential creation changed lifecycle")
	}
	metadata, err := svc.GetCredentialMetadata(ctx, admin.User.ID, inline.Connections[0].Credentials[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	replacementBody := service.CredentialReplacementInput{RequestID: uuid(7), Name: "Staged replacement", Secret: "fixture-storage-secret-replacement", Reason: "Reviewed replacement", StoragePolicyETag: originalContext.ETag}
	replacementPath := "/api/v1/admin/credentials/" + metadata.ID + "/replacements"
	out = request("POST", replacementPath, replacementBody, metadata.ETag, writerCookie, writerCSRF)
	expectStatus(t, out, 201)
	var replacement service.CredentialReplacementRecord
	if json.Unmarshal(out.Body.Bytes(), &replacement) != nil || replacement.StorageSource != "vault" || replacement.ReplacesCredentialID != metadata.ID {
		t.Fatal("replacement source receipt")
	}
	w0 := writes.Load()
	out = request("POST", replacementPath, replacementBody, metadata.ETag, writerCookie, writerCSRF)
	expectStatus(t, out, 200)
	if writes.Load() != w0 {
		t.Fatal("replacement replay wrote again")
	}
	// Unknown first write is durably retained. Recovery reads the original source
	// after future policy changes; it does not produce another CAS effect.
	unknownBody := map[string]any{"name": "Recovered original source", "secret": "fixture-storage-secret-recovered", "priority": 2, "request_id": uuid(8), "storage_policy_etag": originalContext.ETag}
	lostWrite.Store(true)
	expectStatus(t, request("POST", credentialPath, unknownBody, "", writerCookie, writerCSRF), 503)
	var op entity.CredentialStorageOperation
	if err = db.Take(&op, "request_id = ?", uuid(8)).Error; err != nil || op.State != "unknown" {
		t.Fatal("lost response not retained as unknown", err)
	}
	originalOp := op
	policySave("inline", nil, nil)
	w0 = writes.Load()
	out = request("POST", credentialPath, unknownBody, "", writerCookie, writerCSRF)
	expectStatus(t, out, 201)
	var recovered CredentialResponse
	_ = json.Unmarshal(out.Body.Bytes(), &recovered)
	if recovered.StorageSource != "vault" || writes.Load() != w0 {
		t.Fatal("recovery followed new policy or replayed Write")
	}
	if err = db.Take(&op, "request_id = ?", uuid(8)).Error; err != nil || op.RevisionID != originalOp.RevisionID || op.ReferenceID != originalOp.ReferenceID || op.WriteJSON != originalOp.WriteJSON || op.IntentJSON != originalOp.IntentJSON {
		t.Fatal("recovery rewrote original source/observation")
	}
	expectStatus(t, request("GET", "/api/v1/admin/provider-credential-storage-operations/"+uuid(8), nil, "", otherCookie, ""), 404)
	expectStatus(t, request("POST", credentialPath, unknownBody, "", otherCookie, otherCSRF), 409)
	if writes.Load() != w0 {
		t.Fatal("another actor caused replay")
	}
	// Inline UUID receipts remain exact and source-stable after policy switches.
	out = request("POST", "/api/v1/admin/providers", bootstrap, "", writerCookie, writerCSRF)
	expectStatus(t, out, 201)
	var inlineAgain ProviderResponse
	_ = json.Unmarshal(out.Body.Bytes(), &inlineAgain)
	if inlineAgain.ID != inline.ID || inlineAgain.Connections[0].Credentials[0].StorageSource != "inline" {
		t.Fatal("old inline receipt followed new policy")
	}
	verification, err := svc.VerifyCredential(ctx, admin.User.ID, credential.ID)
	if err != nil || !verification.Verified {
		t.Fatal("source-aware Verify failed", err)
	}
	if _, err = svc.SetCredentialEnabled(ctx, admin.User.ID, credential.ID, true); err != nil {
		t.Fatal(err)
	}
	if err = svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	svc.StopRuntime()
	defer svc.StopRuntime()
	beforeReads := reads.Load()
	if err = svc.RefreshRuntime(ctx); err != nil || reads.Load() != beforeReads {
		t.Fatal("runtime refresh read external secret", err)
	}
	// A new descriptor revision does not repoint existing source refs; new
	// creations require a separately reviewed future-write policy against it.
	policySave("vault", &saved.IntegrationID, &saved.RevisionID)
	oldContext := contextRead(writerCookie)
	integration, err := svc.GetVaultIntegration(ctx, admin.User.ID, saved.IntegrationID)
	if err != nil {
		t.Fatal(err)
	}
	config.RequestID = uuid(9)
	config.Descriptor.Prefix = "future-secrets"
	config.WriterAuth = service.VaultAuthInput{Action: "keep"}
	config.ReaderAuth = service.VaultAuthInput{Action: "keep"}
	next, err := svc.SaveVaultIntegration(ctx, admin.User.ID, saved.IntegrationID, integration.ReviewETag, config)
	if err != nil {
		t.Fatal(err)
	}
	stale["request_id"] = uuid(10)
	stale["storage_policy_etag"] = oldContext.ETag
	w0 = writes.Load()
	expectStatus(t, request("POST", credentialPath, stale, "", writerCookie, writerCSRF), 409)
	if writes.Load() != w0 {
		t.Fatal("creator implicitly selected latest revision")
	}
	verification, err = svc.VerifyCredential(ctx, admin.User.ID, credential.ID)
	if err != nil || !verification.Verified {
		t.Fatal("original revision lost Verify after descriptor advance", err)
	}
	var ref entity.CredentialVaultReference
	if err = db.Take(&ref, "credential_id = ?", credential.ID).Error; err != nil || ref.RevisionID != saved.RevisionID {
		t.Fatal("retained source was repointed")
	}
	// Missing current auth revokes publication without altering retained refs.
	integration, err = svc.GetVaultIntegration(ctx, admin.User.ID, saved.IntegrationID)
	if err != nil {
		t.Fatal(err)
	}
	config.RequestID = uuid(11)
	config.ReaderAuth = service.VaultAuthInput{Action: "remove"}
	if _, err = svc.SaveVaultIntegration(ctx, admin.User.ID, saved.IntegrationID, integration.ReviewETag, config); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.VerifyCredential(ctx, admin.User.ID, credential.ID); err == nil {
		t.Fatal("removed current reader accepted Verify")
	}
	if _, err = svc.SetCredentialEnabled(ctx, admin.User.ID, credential.ID, true); err == nil {
		t.Fatal("removed current reader accepted Enable")
	}
	integration, err = svc.GetVaultIntegration(ctx, admin.User.ID, saved.IntegrationID)
	if err != nil {
		t.Fatal(err)
	}
	config.RequestID = uuid(12)
	config.ReaderAuth = service.VaultAuthInput{Action: "replace", Token: "storage-reader"}
	restored, err := svc.SaveVaultIntegration(ctx, admin.User.ID, saved.IntegrationID, integration.ReviewETag, config)
	if err != nil {
		t.Fatal(err)
	}
	policySave("vault", &restored.IntegrationID, &restored.RevisionID)
	// Actual restart prepares only live enabled Vault rows. A failed remote read
	// keeps that supply closed while control-plane and inline recovery remain usable.
	fresh, err := service.New(ctx, db, service.WithCredentialStorage(ring), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	denyRead.Store(true)
	if err = fresh.StartRuntime(ctx); err != nil {
		t.Fatal("one Vault outage blocked process startup", err)
	}
	fresh.StopRuntime()
	if _, err = fresh.GetCredentialStorageContext(ctx, admin.User.ID); err != nil {
		t.Fatal("control plane unavailable during Vault outage", err)
	}
	denyRead.Store(false)
	fresh, err = service.New(ctx, db, service.WithCredentialStorage(ring), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	if err = fresh.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	fresh.StopRuntime()
	beforeReads = reads.Load()
	if err = fresh.RefreshRuntime(ctx); err != nil || reads.Load() != beforeReads {
		t.Fatal("periodic publication retried Vault", err)
	}

	// Deleting a pending Vault Credential preserves its unresolved external
	// reference and historical auth inventory; no destruction is authorized.
	deletedID := provider.Connections[0].Credentials[0].ID
	deletedMetadata, err := svc.GetCredentialMetadata(ctx, admin.User.ID, deletedID)
	if err != nil {
		t.Fatal(err)
	}
	deletion, err := svc.DeleteCredential(ctx, admin.User.ID, deletedID, deletedMetadata.ETag, service.CredentialDeleteInput{Reason: "Remove unused pending Credential"})
	if err != nil || !deletion.Absent || !deletion.RuntimeApplied {
		t.Fatal("owned deletion not applied", err)
	}
	var deletedReference entity.CredentialVaultReference
	if err = db.Take(&deletedReference, "credential_id = ?", deletedID).Error; err != nil || deletedReference.RevisionID != saved.RevisionID {
		t.Fatal("deleted Credential lost retained external reference", err)
	}
	beforeReads = reads.Load()
	var originalAuth entity.VaultReaderAuth
	if err = db.Take(&originalAuth, "id = ?", saved.RevisionID).Error; err != nil {
		t.Fatal(err)
	}
	if err = fresh.StartSystemInstance(ctx, service.SystemInstanceMetadata{Name: "Credential source rotation", Hostname: "storage.invalid", Version: "test", GoVersion: "test", OS: "test", Arch: "test"}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := fresh.StopSystemInstance(ctx); err != nil {
			t.Error(err)
		}
	}()
	if err = fresh.RunSecretRotationOnce(ctx); err != nil {
		t.Fatal("publish actual process proof", err)
	}
	rootView, err := fresh.GetSecretStore(ctx, admin.User.ID)
	if err != nil || !rootView.Process.Verified {
		t.Fatal("actual root process not verified", err)
	}
	rotation, err := fresh.StartSecretRotation(ctx, admin.User.ID, rootView.ReviewETag, service.SecretRotationStartInput{RequestID: uuid(13), TargetKeyID: "next", Reason: "Rewrap retained auth and inline credentials"})
	if err != nil || !rotation.Committed || !rotation.PublicationApplied {
		t.Fatal("root rotation start", err)
	}
	observing := false
	for range 40 {
		if err = fresh.RunSecretRotationOnce(ctx); err != nil {
			t.Fatal("bounded root inventory page", err)
		}
		current, e := fresh.GetSecretRotation(ctx, admin.User.ID, rotation.Receipt.RotationID)
		if e != nil || current.Rotation == nil {
			t.Fatal("root job missing", e)
		}
		if current.Rotation.Status == "blocked" {
			t.Fatal("valid source inventory blocked", current.Rotation.BlockerCodes)
		}
		if current.Rotation.Status == "observing" && current.Rotation.Phase == "observation" {
			if current.InventoryVersion != 2 || len(current.Rotation.Domains) != 7 {
				t.Fatal("source created an extra/partial root domain")
			}
			for _, d := range current.Rotation.Domains {
				if d.Blocked != "0" {
					t.Fatal("valid source blocked retained inventory", d.Code)
				}
			}
			observing = true
			break
		}
	}
	if !observing {
		t.Fatal("root migration did not complete bounded inventory pages")
	}
	var rewrapped entity.VaultReaderAuth
	if err = db.Take(&rewrapped, "id = ?", saved.RevisionID).Error; err != nil || rewrapped.SecretGeneration != originalAuth.SecretGeneration || rewrapped.AuthCiphertext == originalAuth.AuthCiphertext {
		t.Fatal("original immutable reader not rewrapped", err)
	}
	if key, e := ring.KeyID("vault-reader:"+rewrapped.ID+":"+rewrapped.SecretGeneration, rewrapped.AuthCiphertext); e != nil || key != "next" {
		t.Fatal("retained reader was not authenticated under target root", e)
	}
	if err = fresh.RefreshRuntime(ctx); err != nil || reads.Load() != beforeReads {
		t.Fatal("root rewrap invalidated prepared values or read Vault at publication", err)
	}
	if err = db.Take(&deletedReference, "credential_id = ?", deletedID).Error; err != nil || deletedReference.RevisionID != saved.RevisionID {
		t.Fatal("rewrap deleted or repointed retained orphan", err)
	}
	// Keep this actual SQL lifecycle native-free. Separate controlled-native/root
	// process acceptance remains an explicit later gate.
	var calls, attempts int64
	if err = db.Model(&entity.CallRecord{}).Count(&calls).Error; err != nil || calls != 0 {
		t.Fatal("unexpected call history", err)
	}
	if err = db.Model(&entity.CallAttempt{}).Count(&attempts).Error; err != nil || attempts != 0 {
		t.Fatal("unexpected native attempt", err)
	}
	if writes.Load() != 5 || discovery.Load() != 2 || reads.Load() < 7 {
		t.Fatal("bounded source effects differ", writes.Load(), discovery.Load(), reads.Load())
	}
	if next.RevisionID == saved.RevisionID {
		t.Fatal("descriptor update did not create an independent revision")
	}
}
