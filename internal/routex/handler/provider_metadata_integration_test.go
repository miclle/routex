package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secretstore"
)

// Proposed appended scenario after the current complete parent. Root owns its
// registry integration and real PostgreSQL/MySQL execution; no native call is made.
func testProviderMetadataLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	store, err := secretstore.New(bytes.Repeat([]byte{93}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var nativeCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { nativeCalls.Add(1); w.WriteHeader(500) }))
	defer upstream.Close()
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"provider-metadata@example.invalid","password":"test-only-provider-password","name":"Provider admin"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, cookie := readIdentity(t, setup)
	reader, readCookie, readCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "provider-reader", []string{"providers.read"})
	writer, writeCookie, writeCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "provider-writer", []string{"providers.read", "providers.write"})
	_, deniedCookie, deniedCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "provider-denied", []string{"models.read_all"})
	cipher, err := store.Seal("crd_provider_meta", "test-only-provider-secret")
	if err != nil {
		t.Fatal(err)
	}
	modelID := "mdl_provider_meta"
	for _, row := range []any{
		&entity.Provider{ID: "prv_provider_meta", Name: "Original"},
		&entity.ProviderConnection{ID: "con_provider_meta", ProviderID: "prv_provider_meta", Name: "Retained connection", BaseURL: upstream.URL + "/v1", Protocol: entity.ProtocolOpenAIChat, EgressMode: "direct", ETag: "rev_provider_connection"},
		&entity.ProviderCredential{ID: "crd_provider_meta", ConnectionID: "con_provider_meta", Name: "Retained credential", Ciphertext: cipher, Priority: 7, Enabled: false, VerificationStatus: "pending"},
		&entity.ProviderModel{ID: "pmd_provider_meta", ConnectionID: "con_provider_meta", UpstreamName: "provider-model", Disabled: true, SupportsImageInput: true},
		&entity.Model{ID: modelID, Status: "active"},
		&entity.ModelName{Name: "provider-model", ModelID: modelID, CurrentModelID: &modelID},
		&entity.ModelProviderBinding{ID: "bnd_provider_meta", ModelID: modelID, ProviderModelID: "pmd_provider_meta", Weight: 0},
		&entity.ProviderQualityPolicy{ProviderID: "prv_provider_meta", ETag: "rev_provider_quality", WindowMinutes: 5, MinimumAttempts: 2},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	var failAudit, failPublication, failRead atomic.Bool
	queryCallback, createCallback := "test:provider_metadata_query", "test:provider_metadata_audit"
	if err := db.Callback().Query().Before("gorm:query").Register(queryCallback, func(tx *gorm.DB) {
		if failPublication.Load() && tx.Statement.Table == "models" || failRead.Load() && tx.Statement.Table == "providers" {
			_ = tx.AddError(errors.New("test-only Provider read unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Callback().Query().Remove(queryCallback) }()
	if err := db.Callback().Create().Before("gorm:create").Register(createCallback, func(tx *gorm.DB) {
		if row, ok := tx.Statement.Dest.(*entity.AuditEvent); ok && failAudit.Load() && row.Action == "provider.metadata.update" {
			_ = tx.AddError(errors.New("test-only Provider audit unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Callback().Create().Remove(createCallback) }()
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	defer svc.StopRuntime()
	path := "/api/v1/admin/providers/prv_provider_meta/metadata"
	request := func(method, target, raw, etag string, session *http.Cookie, csrf string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://routex.test"+target, strings.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		if session != nil {
			req.AddCookie(session)
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		if etag != "" {
			req.Header.Set("If-Match", etag)
		}
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		if res.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("private denial/success header missing", res.Code)
		}
		return res
	}
	get := func(session *http.Cookie) service.ProviderMetadataRecord {
		res := request("GET", path, "", "", session, "")
		row := decodeCatalogResponse[service.ProviderMetadataRecord](t, res, 200)
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(res.Body.Bytes(), &fields); err != nil || len(fields) != 4 || row.ID != "prv_provider_meta" || res.Header().Get("ETag") != strconv.Quote(row.ETag) || len(row.ETag) != 129 {
			t.Fatal("exact metadata fields/header", err, res.Body.String())
		}
		for _, key := range []string{"id", "name", "etag", "can_edit"} {
			if fields[key] == nil {
				t.Fatal("missing field", key)
			}
		}
		return row
	}
	put := func(session *http.Cookie, csrf, etag, name, reason string, status int) service.ProviderMetadataWriteResult {
		raw, err := json.Marshal(service.ProviderMetadataInput{Name: name, Reason: reason})
		if err != nil {
			t.Fatal(err)
		}
		res := request("PUT", path, string(raw), strconv.Quote(etag), session, csrf)
		expectStatus(t, res, status)
		if status != 200 {
			return service.ProviderMetadataWriteResult{}
		}
		row := decodeCatalogResponse[service.ProviderMetadataWriteResult](t, res, 200)
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(res.Body.Bytes(), &fields); err != nil || len(fields) != 3 || !row.RuntimeApplied || row.Provider.ID != "prv_provider_meta" || row.Provider.Name != name || !row.Provider.CanEdit || res.Header().Get("ETag") != strconv.Quote(row.Provider.ETag) {
			t.Fatal("exact current confirmation", err, res.Body.String())
		}
		if state := svc.RuntimeStatus(); !state.Ready || state.ErrorCode != "" || state.SnapshotID == "" {
			t.Fatal("no current live publication", state)
		}
		return row
	}
	readProvider := func() entity.Provider {
		var row entity.Provider
		if err := db.First(&row, "id = ?", "prv_provider_meta").Error; err != nil {
			t.Fatal(err)
		}
		return row
	}
	auditCount := func() int64 {
		var n int64
		if err := db.Model(&entity.AuditEvent{}).Where("action = ? AND resource_id = ?", "provider.metadata.update", "prv_provider_meta").Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}
	// Complete child and quality rows must remain byte-for-byte configuration facts.
	childSnapshot := func() string {
		var connections []entity.ProviderConnection
		var credentials []entity.ProviderCredential
		var models []entity.ProviderModel
		var bindings []entity.ModelProviderBinding
		var quality []entity.ProviderQualityPolicy
		for _, query := range []struct {
			dest         any
			field, value string
		}{{&connections, "provider_id", "prv_provider_meta"}, {&credentials, "connection_id", "con_provider_meta"}, {&models, "connection_id", "con_provider_meta"}, {&bindings, "provider_model_id", "pmd_provider_meta"}, {&quality, "provider_id", "prv_provider_meta"}} {
			if err := db.Where(query.field+" = ?", query.value).Order(query.field).Find(query.dest).Error; err != nil {
				t.Fatal(err)
			}
		}
		raw, err := json.Marshal([]any{connections, credentials, models, bindings, quality})
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	original, children := readProvider(), childSnapshot()
	expectStatus(t, request("GET", path, "", "", nil, ""), 401)
	expectStatus(t, request("GET", path, "", "", deniedCookie, ""), 403)
	expectStatus(t, request("GET", "/api/v1/admin/providers/prv_missing/metadata", "", "", deniedCookie, ""), 403)
	readReview := get(readCookie)
	if readReview.CanEdit {
		t.Fatal("reader got write")
	}
	put(readCookie, readCSRF, readReview.ETag, "Forbidden", "read only", 403)
	put(deniedCookie, deniedCSRF, readReview.ETag, "Forbidden", "no authority", 403)
	review, writerReview := get(cookie), get(writeCookie)
	if !review.CanEdit || !writerReview.CanEdit || review.ETag == writerReview.ETag {
		t.Fatal("actor-bound write proof missing")
	}
	put(cookie, admin.CSRFToken, writerReview.ETag, "Cross actor", "identity mismatch", 409)
	expectStatus(t, request("GET", "/api/v1/admin/providers/prv_PROVIDER_meta/metadata", "", "", cookie, ""), 404)
	expectStatus(t, request("GET", "/api/v1/admin/providers/prv_provider_meta%20/metadata", "", "", cookie, ""), 400)
	var app *apperrors.Error
	if _, err := svc.GetProviderMetadata(ctx, "usr_"+strings.ToUpper(strings.TrimPrefix(reader.User.ID, "usr_")), "prv_provider_meta"); !errors.As(err, &app) || app.Code != 401 {
		t.Fatal("actor alias borrowed authority", err)
	}
	for _, raw := range []string{`{"name":"Bad","reason":"why","enabled":true}`, `{"name":"Bad","reason":"why","name":"Other"}`, `{"name":"Bad"}`, `{"name":null,"reason":"why"}`, `{"name":"Bad","reason":""}`} {
		expectStatus(t, request("PUT", path, raw, strconv.Quote(review.ETag), cookie, admin.CSRFToken), 400)
	}
	expectStatus(t, request("PUT", path, `{"name":"Bad","reason":"why"}`, "", cookie, admin.CSRFToken), 400)
	expectStatus(t, request("PUT", path, `{"name":"Bad","reason":"why"}`, "W/"+strconv.Quote(review.ETag), cookie, admin.CSRFToken), 400)
	expectStatus(t, request("PUT", path, `{"name":"Bad","reason":"why"}`, strconv.Quote(review.ETag), cookie, ""), 403)
	expectStatus(t, request("GET", path+"?unexpected=true", "", "", cookie, ""), 400)
	if !reflect.DeepEqual(readProvider(), original) || auditCount() != 0 {
		t.Fatal("invalid request mutated")
	}
	failRead.Store(true)
	expectStatus(t, request("GET", path, "", "", cookie, ""), 503)
	failRead.Store(false)
	result := put(cookie, admin.CSRFToken, review.ETag, "\ufeffRenamed\ufeff", "\ufeffReviewed rename\ufeff", 200)
	if !result.Changed || auditCount() != 1 {
		t.Fatal("rename did not commit one audit")
	}
	put(cookie, admin.CSRFToken, review.ETag, "Stale replacement", "old review", 409)
	if row := put(cookie, admin.CSRFToken, review.ETag, result.Provider.Name, "\ufeffReviewed rename\ufeff", 200); row.Changed || auditCount() != 1 {
		t.Fatal("exact retry duplicated audit")
	}
	put(writeCookie, writeCSRF, review.ETag, result.Provider.Name, "old other actor", 409)
	writerReview = get(writeCookie)
	var assignment entity.UserRole
	if err := db.First(&assignment, "user_id = ?", writer.User.ID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveRole(ctx, admin.User.ID, assignment.RoleID, "System provider-writer", []string{"providers.write"}); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request("GET", path, "", "", writeCookie, ""), 403)
	if row := put(writeCookie, writeCSRF, writerReview.ETag, "Writer rename", "independent writer", 200); !row.Changed {
		t.Fatal("writer-only update not applied")
	}
	if _, err := svc.SaveRole(ctx, admin.User.ID, assignment.RoleID, "System provider-writer", []string{"providers.read"}); err != nil {
		t.Fatal(err)
	}
	put(writeCookie, writeCSRF, writerReview.ETag, "Writer rename", "independent writer", 403)
	beforeFault, beforeAudit := readProvider(), auditCount()
	review = get(cookie)
	failAudit.Store(true)
	put(cookie, admin.CSRFToken, review.ETag, "Rollback", "audit persistence fault", 500)
	failAudit.Store(false)
	if !reflect.DeepEqual(beforeFault, readProvider()) || auditCount() != beforeAudit {
		t.Fatal("audit rollback lost original row")
	}
	// A content return is intentionally the same validator, not an operation receipt.
	current := get(cookie)
	put(cookie, admin.CSRFToken, current.ETag, "Temporary", "content change", 200)
	temporary := get(cookie)
	returned := put(cookie, admin.CSRFToken, temporary.ETag, current.Name, "content return", 200)
	if returned.Provider.ETag != current.ETag {
		t.Fatal("current-content ABA contract changed")
	}
	review = get(cookie)
	for _, target := range []struct {
		model any
		id    string
	}{{&entity.User{}, admin.User.ID}, {&entity.Provider{}, "prv_provider_meta"}} {
		var birth struct{ CreatedAt time.Time }
		if err := db.Model(target.model).Where("id = ?", target.id).Select("created_at").Take(&birth).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(target.model).Where("id = ?", target.id).Update("created_at", birth.CreatedAt.Add(time.Millisecond)).Error; err != nil {
			t.Fatal(err)
		}
		put(cookie, admin.CSRFToken, review.ETag, review.Name, "old birth cannot reconcile", 409)
		if err := db.Model(target.model).Where("id = ?", target.id).Update("created_at", birth.CreatedAt).Error; err != nil {
			t.Fatal(err)
		}
	}
	failPublication.Store(true)
	beforeAudit = auditCount()
	put(cookie, admin.CSRFToken, review.ETag, "Pending publication", "immutable pending intent", 503)
	if get(cookie).Name != "Pending publication" || auditCount() != beforeAudit+1 {
		t.Fatal("saved uncertain configuration missing")
	}
	put(cookie, admin.CSRFToken, review.ETag, "Pending publication", "immutable pending intent", 503)
	if auditCount() != beforeAudit+1 {
		t.Fatal("uncertain retry duplicated audit")
	}
	failPublication.Store(false)
	if row := put(cookie, admin.CSRFToken, review.ETag, "Pending publication", "immutable pending intent", 200); row.Changed || auditCount() != beforeAudit+1 {
		t.Fatal("exact current retry not reconciled")
	}
	expected := original
	expected.Name = "Pending publication"
	if !reflect.DeepEqual(readProvider(), expected) || childSnapshot() != children {
		t.Fatal("rename changed identities/children/quality policy")
	}
	page := decodeCatalogResponse[service.AuditPage](t, identityRequest(router, "GET", "/api/v1/admin/audit?category=credentials&q=provider.metadata.update", "", cookie, ""), 200)
	if len(page.Items) != int(auditCount()) {
		t.Fatal("typed provider audit missing", len(page.Items), auditCount())
	}
	for _, row := range page.Items {
		var detail struct {
			Before, After map[string]string
			Reason        string
		}
		if err := json.Unmarshal(row.Changes, &detail); err != nil || len(detail.Before) != 1 || len(detail.After) != 1 || detail.Reason == "" || row.ResourceType != "provider" || row.ResourceID != "prv_provider_meta" || row.Action != "provider.metadata.update" || row.Result != "committed" {
			t.Fatal("unsafe typed provider audit", err)
		}
		for _, forbidden := range []string{"ciphertext", "test-only-provider-secret", "protocol", "base_url", "priority", "enabled"} {
			if strings.Contains(string(row.Changes), forbidden) {
				t.Fatal("unrelated audit data", forbidden)
			}
		}
	}
	// Disabled actors cannot read or reconcile an already-current name.
	if err := db.Model(&entity.User{}).Where("id = ?", writer.User.ID).Update("disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request("GET", path, "", "", writeCookie, ""), 401)
	put(writeCookie, writeCSRF, writerReview.ETag, "Pending publication", "disabled current retry", 401)
	if err := db.Model(&entity.User{}).Where("id = ?", writer.User.ID).Update("disabled", false).Error; err != nil {
		t.Fatal(err)
	}
	if nativeCalls.Load() != 0 {
		t.Fatal("metadata caused upstream request", nativeCalls.Load())
	}
	svc.StopRuntime()
	put(cookie, admin.CSRFToken, review.ETag, "Pending publication", "immutable pending intent", 503)
}
