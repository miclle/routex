package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
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

func testCredentialMetadataLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	store, err := secretstore.New(bytes.Repeat([]byte{74}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var upstreamCalls atomic.Int32
	var selectedSecret atomic.Value
	var holdVerification atomic.Bool
	verificationEntered := make(chan struct{}, 1)
	verificationRelease := make(chan struct{})
	var verificationReleaseOnce sync.Once
	releaseVerification := func() { verificationReleaseOnce.Do(func() { close(verificationRelease) }) }
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/v1/models" {
			if holdVerification.Load() {
				verificationEntered <- struct{}{}
				select {
				case <-verificationRelease:
				case <-r.Context().Done():
					return
				}
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"metadata-upstream"}]}`))
			return
		}
		selectedSecret.Store(r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"model":"metadata-upstream","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1}}`))
	}))
	defer upstream.Close()
	defer releaseVerification()
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"metadata@example.invalid","password":"test-only-metadata-password","name":"Metadata admin"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, cookie := readIdentity(t, setup)
	reader, readCookie, readCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "metadata-reader", []string{"providers.read"})
	_, writeCookie, writeCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "metadata-writer", []string{"providers.write"})
	_, deniedCookie, deniedCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "metadata-denied", []string{"models.read_all"})
	verified := time.Now().UTC().Truncate(time.Microsecond)
	firstCipher, err := store.Seal("crd_metadata_first", "test-only-metadata-first")
	if err != nil {
		t.Fatal(err)
	}
	secondCipher, err := store.Seal("crd_metadata_second", "test-only-metadata-second")
	if err != nil {
		t.Fatal(err)
	}
	modelID := "mdl_metadata"
	bearer := "rx_" + strings.Repeat("m", 43)
	for _, row := range []any{
		&entity.Provider{ID: "prv_metadata", Name: "Metadata provider"},
		&entity.ProviderConnection{ID: "con_metadata", ProviderID: "prv_metadata", Name: "Metadata connection", BaseURL: upstream.URL + "/v1", Protocol: entity.ProtocolOpenAIChat},
		&entity.ProviderConnection{ID: "con_metadata_other", ProviderID: "prv_metadata", Name: "Other connection", BaseURL: upstream.URL + "/v1", Protocol: entity.ProtocolOpenAIChat},
		&entity.ProviderCredential{ID: "crd_metadata_first", ConnectionID: "con_metadata", Name: "Primary", Ciphertext: firstCipher, Priority: 10, Enabled: true, VerificationStatus: "verified", VerifiedAt: &verified},
		&entity.ProviderCredential{ID: "crd_metadata_second", ConnectionID: "con_metadata", Name: "Secondary", Ciphertext: secondCipher, Priority: 20, Enabled: true, VerificationStatus: "verified", VerifiedAt: &verified},
		&entity.ProviderModel{ID: "pmd_metadata", ConnectionID: "con_metadata", UpstreamName: "metadata-upstream"},
		&entity.CredentialModelAccess{CredentialID: "crd_metadata_first", ProviderModelID: "pmd_metadata"},
		&entity.CredentialModelAccess{CredentialID: "crd_metadata_second", ProviderModelID: "pmd_metadata"},
		&entity.Model{ID: modelID, Status: "active"},
		&entity.ModelName{Name: "metadata-model", ModelID: modelID, CurrentModelID: &modelID},
		&entity.ModelProviderBinding{ID: "bnd_metadata", ModelID: modelID, ProviderModelID: "pmd_metadata", Weight: 100},
		&entity.UserModelGrant{UserID: admin.User.ID, ModelID: modelID},
		&entity.APIKey{ID: "key_metadata", UserID: admin.User.ID, Name: "Metadata Key", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(bearer), Status: entity.KeyActive},
		&entity.APIKeyModel{KeyID: "key_metadata", ModelID: modelID},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	// GORM callback registries are shared by all sessions. Install the fault
	// hook before starting readers and remove it only after their workers join.
	var failPublication atomic.Bool
	callback := "test:credential_metadata_publication"
	if err := db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if failPublication.Load() && tx.Statement.Table == "models" {
			_ = tx.AddError(errors.New("test-only publication unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Callback().Query().Remove(callback) }()
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	defer svc.StopRuntime()
	if err := svc.StartCallRecorder(ctx, filepath.Join(t.TempDir(), "metadata.db")); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = svc.StopCallRecorder() }()
	path := "/api/v1/admin/credentials/crd_metadata_first/metadata"
	request := func(method, target, raw, etag string, sessionCookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://routex.test"+target, strings.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		if sessionCookie != nil {
			req.AddCookie(sessionCookie)
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		if etag != "" {
			req.Header.Set("If-Match", etag)
		}
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	read := func(target string) service.CredentialMetadataRecord {
		res := request("GET", target, "", "", readCookie, "")
		value := decodeCatalogResponse[service.CredentialMetadataRecord](t, res, 200)
		if res.Header().Get("ETag") != strconv.Quote(value.ETag) || len(value.ETag) != 64 {
			t.Fatal("missing strong response validator")
		}
		for _, forbidden := range []string{"ciphertext", "test-only-metadata", firstCipher, secondCipher, "secret"} {
			if strings.Contains(res.Body.String(), forbidden) {
				t.Fatal("metadata response leaked credential material")
			}
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(res.Body.Bytes(), &fields); err != nil || len(fields) != 8 {
			t.Fatal("unexpected metadata response shape")
		}
		return value
	}
	write := func(target, name string, priority int, etag string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]any{"name": name, "priority": priority, "reason": "Reviewed metadata ordering"})
		return request("PUT", target, string(body), strconv.Quote(etag), writeCookie, writeCSRF)
	}
	initial := read(path)
	expectStatus(t, request("GET", path, "", "", nil, ""), 401)
	expectStatus(t, request("GET", path, "", "", writeCookie, ""), 403)
	expectStatus(t, request("GET", strings.Replace(path, "crd_metadata_first", "crd_missing", 1), "", "", deniedCookie, ""), 403)
	body := `{"name":"Renamed","priority":0,"reason":"Reviewed"}`
	zero := 0
	if _, err := svc.WriteCredentialMetadata(ctx, reader.User.ID, initial.ID, initial.ETag, service.CredentialMetadataInput{Name: "Renamed", Priority: &zero, Reason: "Direct service authorization"}); err == nil {
		t.Fatal("service accepted unauthorized metadata write")
	}
	expectStatus(t, request("PUT", path, body, strconv.Quote(initial.ETag), nil, ""), 401)
	expectStatus(t, request("PUT", path, body, strconv.Quote(initial.ETag), readCookie, readCSRF), 403)
	expectStatus(t, request("PUT", path, body, strconv.Quote(initial.ETag), deniedCookie, deniedCSRF), 403)
	expectStatus(t, request("PUT", path, body, strconv.Quote(initial.ETag), cookie, ""), 403)
	for _, invalid := range []string{
		`{"name":"Renamed","reason":"Reviewed"}`, `{"name":"Renamed","priority":null,"reason":"Reviewed"}`,
		`{"name":"Renamed","priority":-1,"reason":"Reviewed"}`, `{"name":"Renamed","priority":10001,"reason":"Reviewed"}`,
		`{"name":"Renamed","priority":0.5,"reason":"Reviewed"}`, `{"name":"Renamed","priority":0,"reason":""}`,
		`{"name":"Renamed","priority":0,"reason":"bad\nreason"}`, `{"name":" ","priority":0,"reason":"Reviewed"}`,
		`{"name":"bad\u0000name","priority":0,"reason":"Reviewed"}`, `{"name":"Renamed","priority":0,"reason":"Reviewed","secret":"forbidden"}`,
		body + `{}`, `{"name":"Renamed","priority":0,"reason":"` + strings.Repeat("界", 342) + `"}`,
		`{"name":"` + strings.Repeat("a", 101) + `","priority":0,"reason":"Reviewed"}`,
	} {
		expectStatus(t, request("PUT", path, invalid, strconv.Quote(initial.ETag), writeCookie, writeCSRF), 400)
	}
	for _, header := range []string{"", initial.ETag, `W/"` + initial.ETag + `"`, `*`, `"first","second"`} {
		expectStatus(t, request("PUT", path, body, header, writeCookie, writeCSRF), 400)
	}
	expectStatus(t, write(path, " secondary ", 0, initial.ETag), 409)
	// Creation and metadata share the same name rules, including trimming/case.
	createPath := "/api/v1/admin/connections/con_metadata/credentials"
	duplicate := `{"name":" primary ","priority":1,"secret":"test-only-unused-secret"}`
	expectStatus(t, identityRequest(router, "POST", createPath, duplicate, cookie, admin.CSRFToken), 409)
	expectStatus(t, identityRequest(router, "POST", strings.Replace(createPath, "con_metadata", "con_metadata_other", 1), duplicate, cookie, admin.CSRFToken), 201)
	var before entity.ProviderCredential
	if err := db.First(&before, "id = ?", initial.ID).Error; err != nil {
		t.Fatal(err)
	}
	var accessBefore []entity.CredentialModelAccess
	if err := db.Where("credential_id = ?", initial.ID).Find(&accessBefore).Error; err != nil {
		t.Fatal(err)
	}
	saved := decodeCatalogResponse[service.CredentialMetadataRecord](t, write(path, " Renamed ", 0, initial.ETag), 200)
	if saved.Name != "Renamed" || saved.Priority != 0 || saved.ETag == initial.ETag || saved.ConnectionID != initial.ConnectionID {
		t.Fatal("metadata was not normalized and updated")
	}
	var after entity.ProviderCredential
	if err := db.First(&after, "id = ?", initial.ID).Error; err != nil {
		t.Fatal(err)
	}
	after.Name, after.Priority = before.Name, before.Priority
	if !reflect.DeepEqual(before, after) {
		t.Fatal("metadata changed ciphertext, identity, verification, activation, or creation")
	}
	var accessAfter []entity.CredentialModelAccess
	if err := db.Where("credential_id = ?", initial.ID).Find(&accessAfter).Error; err != nil || !reflect.DeepEqual(accessBefore, accessAfter) {
		t.Fatal("metadata changed model access")
	}
	auditCount := func() int64 {
		var count int64
		if err := db.Model(&entity.AuditEvent{}).Where("action = ? AND resource_id = ?", "credential.metadata.update", initial.ID).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		return count
	}
	if auditCount() != 1 {
		t.Fatal("metadata write did not audit once")
	}
	decodeCatalogResponse[service.CredentialMetadataRecord](t, write(path, "Renamed", 0, initial.ETag), 200)
	expectStatus(t, write(path, "Stale overwrite", 2, initial.ETag), 409)
	if auditCount() != 1 {
		t.Fatal("noop or conflict duplicated audit")
	}
	// Verification and enabled state participate in optimistic concurrency.
	if err := db.Model(&entity.ProviderCredential{}).Where("id = ?", initial.ID).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, write(path, "Wrong state", 0, saved.ETag), 409)
	current := read(path)
	if current.Enabled || current.ETag == saved.ETag {
		t.Fatal("activation change did not invalidate metadata validator")
	}
	decodeCatalogResponse[service.CredentialMetadataRecord](t, write(path, current.Name, current.Priority, saved.ETag), 200)
	if auditCount() != 1 {
		t.Fatal("target reconciliation changed metadata history")
	}
	if err := db.Model(&entity.ProviderCredential{}).Where("id = ?", initial.ID).Update("enabled", true).Error; err != nil {
		t.Fatal(err)
	}
	current = read(path)
	// Concurrent writers reviewed one state; exactly one different target wins.
	responses := make(chan int, 2)
	var wg sync.WaitGroup
	for _, name := range []string{"Concurrent A", "Concurrent B"} {
		wg.Go(func() { responses <- write(path, name, 0, current.ETag).Code })
	}
	wg.Wait()
	close(responses)
	statuses := map[int]int{}
	for status := range responses {
		statuses[status]++
	}
	if statuses[200] != 1 || statuses[409] != 1 || auditCount() != 2 {
		t.Fatalf("concurrent metadata writes = %v", statuses)
	}
	// Historical duplicate labels remain valid for true no-ops and priority edits.
	current = read(path)
	if err := db.Model(&entity.ProviderCredential{}).Where("id = ?", "crd_metadata_second").Update("name", current.Name).Error; err != nil {
		t.Fatal(err)
	}
	decodeCatalogResponse[service.CredentialMetadataRecord](t, write(path, current.Name, current.Priority, current.ETag), 200)
	decodeCatalogResponse[service.CredentialMetadataRecord](t, write(path, current.Name, 30, current.ETag), 200)
	if upstreamCalls.Load() != 0 {
		t.Fatal("metadata operations dispatched upstream requests")
	}
	infer := func(want string) {
		req := httptest.NewRequest("POST", "http://routex.test/v1/chat/completions", strings.NewReader(`{"model":"metadata-model","messages":[{"role":"user","content":"test"}]}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+bearer)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		expectStatus(t, res, 200)
		if selectedSecret.Load() != "Bearer "+want {
			t.Fatal("runtime did not apply credential priority")
		}
	}
	infer("test-only-metadata-second")
	current = read(path)
	decodeCatalogResponse[service.CredentialMetadataRecord](t, write(path, current.Name, 0, current.ETag), 200)
	infer("test-only-metadata-first")
	// Discovery performs remote I/O before taking the catalog locks. Metadata
	// edited during that I/O must survive the verification's selective updates.
	holdVerification.Store(true)
	verificationResult := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		verificationResult <- identityRequest(router, "POST", "/api/v1/admin/credentials/"+initial.ID+"/verify", `{}`, writeCookie, writeCSRF)
	}()
	select {
	case <-verificationEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("controlled verification never entered upstream")
	}
	current = read(path)
	decodeCatalogResponse[service.CredentialMetadataRecord](t, write(path, "Verification preserved", 1, current.ETag), 200)
	releaseVerification()
	select {
	case result := <-verificationResult:
		verification := decodeCatalogResponse[VerifyCredentialResponse](t, result, 200)
		if !verification.Verified {
			t.Fatal("controlled concurrent verification failed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent verification did not complete")
	}
	current = read(path)
	if current.Name != "Verification preserved" || current.Priority != 1 || current.VerificationStatus != "verified" || !current.Enabled {
		t.Fatal("verification overwrote concurrent metadata or activation")
	}
	// Publication can fail after a committed write. Exact retries reconcile the
	// saved target, refresh runtime, and never manufacture a second audit event.
	current = read(path)
	priorAuditCount := auditCount()
	failPublication.Store(true)
	expectStatus(t, write(path, "Publication retry", 1, current.ETag), 503)
	if stored := read(path); stored.Name != "Publication retry" || stored.Priority != 1 || auditCount() != priorAuditCount+1 {
		t.Fatal("failed publication obscured committed metadata")
	}
	failPublication.Store(false)
	decodeCatalogResponse[service.CredentialMetadataRecord](t, write(path, "Publication retry", 1, current.ETag), 200)
	if auditCount() != priorAuditCount+1 {
		t.Fatal("publication reconciliation duplicated audit")
	}
	// A creation racing a rename must use the same connection-local exclusion.
	current = read(path)
	raceResults := make(chan int, 2)
	wg.Go(func() { raceResults <- write(path, "Race label", 1, current.ETag).Code })
	wg.Go(func() {
		raceResults <- identityRequest(router, "POST", createPath, `{"name":" race LABEL ","priority":1,"secret":"test-only-race-secret"}`, cookie, admin.CSRFToken).Code
	})
	wg.Wait()
	close(raceResults)
	statuses = map[int]int{}
	for status := range raceResults {
		statuses[status]++
	}
	if statuses[409] != 1 || statuses[200]+statuses[201] != 1 {
		t.Fatalf("creation/metadata name race = %v", statuses)
	}
	var connectionCredentials []entity.ProviderCredential
	if err := db.Select("name").Where("connection_id = ?", initial.ConnectionID).Find(&connectionCredentials).Error; err != nil {
		t.Fatal(err)
	}
	matches := 0
	for _, credential := range connectionCredentials {
		if strings.EqualFold(strings.TrimSpace(credential.Name), "Race label") {
			matches++
		}
	}
	if matches != 1 {
		t.Fatal("concurrent creation/metadata introduced duplicate label")
	}
	var audit entity.AuditEvent
	if err := db.Where("action = ? AND resource_id = ?", "credential.metadata.update", initial.ID).Order("created_at DESC, id DESC").First(&audit).Error; err != nil || audit.DetailsJSON == nil {
		t.Fatal("missing typed metadata audit")
	}
	for _, forbidden := range []string{"ciphertext", "test-only-metadata", "enabled", "verification_status"} {
		if strings.Contains(*audit.DetailsJSON, forbidden) {
			t.Fatal("metadata audit contains unrelated or secret fields")
		}
	}
}
