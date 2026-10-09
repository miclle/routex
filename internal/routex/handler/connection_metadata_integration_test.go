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

// The central integration owner supplies a freshly migrated disposable database.
// No native inference, verification, browser or production process is invoked.
func testConnectionMetadataLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	store, err := secretstore.New(bytes.Repeat([]byte{79}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var nativeCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "completions") || strings.Contains(r.URL.Path, "responses") || strings.Contains(r.URL.Path, "messages") {
			nativeCalls.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer upstream.Close()
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"connection-metadata@example.invalid","password":"test-only-connection-password","name":"Connection admin"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, cookie := readIdentity(t, setup)
	reader, readCookie, readCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "connection-reader", []string{"providers.read"})
	writer, writeCookie, writeCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "connection-writer", []string{"providers.read", "providers.write"})
	_, deniedCookie, deniedCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "connection-denied", []string{"models.read_all"})
	cipher, err := store.Seal("crd_connection_meta", "test-only-connection-secret")
	if err != nil {
		t.Fatal(err)
	}
	verified := time.Now().UTC().Truncate(time.Microsecond)
	modelID := "mdl_connection_meta"
	for _, row := range []any{
		&entity.Provider{ID: "prv_connection_meta", Name: "Connection provider"},
		&entity.Egress{ID: "egr_connection_meta", Name: "Disabled proxy", Kind: "socks5", Host: "127.0.0.1", Port: 9, Enabled: false, ETag: "rev_connection_proxy"},
		&entity.ProviderConnection{ID: "con_connection_meta", ProviderID: "prv_connection_meta", Name: "Original", BaseURL: upstream.URL + "/v1", Protocol: entity.ProtocolOpenAIChat, EgressMode: "direct", ETag: "rev_connection_initial"},
		&entity.ProviderConnection{ID: "con_connection_disabled", ProviderID: "prv_connection_meta", Name: "Disabled route", BaseURL: upstream.URL + "/v1", Protocol: entity.ProtocolOpenAIChat, EgressMode: "proxy", EgressID: ptrConnectionMetadata("egr_connection_meta"), ETag: "rev_connection_disabled"},
		&entity.ProviderCredential{ID: "crd_connection_meta", ConnectionID: "con_connection_meta", Name: "Retained credential", Ciphertext: cipher, Priority: 7, Enabled: true, VerificationStatus: "verified", VerifiedAt: &verified},
		&entity.ProviderModel{ID: "pmd_connection_meta", ConnectionID: "con_connection_meta", UpstreamName: "connection-model"},
		&entity.CredentialModelAccess{CredentialID: "crd_connection_meta", ProviderModelID: "pmd_connection_meta"},
		&entity.Model{ID: modelID, Status: "active"},
		&entity.ModelName{Name: "connection-model", ModelID: modelID, CurrentModelID: &modelID},
		&entity.ModelProviderBinding{ID: "bnd_connection_meta", ModelID: modelID, ProviderModelID: "pmd_connection_meta", Weight: 100},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	// Shared GORM callbacks are installed before any runtime worker, removed after
	// StopRuntime joins it. Faults reject exactly one existing persistence seam.
	var failPublication, failAudit, failRead atomic.Bool
	var lastSQLError atomic.Value
	lastSQLError.Store("")
	diagnosticCallback := "test:connection_metadata_error"
	diagnostic := func(tx *gorm.DB) {
		if tx.Error != nil {
			lastSQLError.Store(tx.Error.Error())
		}
	}
	if err := db.Callback().Query().After("gorm:query").Register(diagnosticCallback, diagnostic); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Callback().Query().Remove(diagnosticCallback) }()
	if err := db.Callback().Create().After("gorm:create").Register(diagnosticCallback, diagnostic); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Callback().Create().Remove(diagnosticCallback) }()
	if err := db.Callback().Update().After("gorm:update").Register(diagnosticCallback, diagnostic); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Callback().Update().Remove(diagnosticCallback) }()
	queryCallback := "test:connection_metadata_query"
	createCallback := "test:connection_metadata_audit"
	if err := db.Callback().Query().Before("gorm:query").Register(queryCallback, func(tx *gorm.DB) {
		if failPublication.Load() && tx.Statement.Table == "models" || failRead.Load() && tx.Statement.Table == "provider_connections" {
			_ = tx.AddError(errors.New("test-only connection read unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Callback().Query().Remove(queryCallback) }()
	if err := db.Callback().Create().Before("gorm:create").Register(createCallback, func(tx *gorm.DB) {
		if row, ok := tx.Statement.Dest.(*entity.AuditEvent); ok && failAudit.Load() && row.Action == "connection.metadata.update" {
			_ = tx.AddError(errors.New("test-only connection audit unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Callback().Create().Remove(createCallback) }()
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err, svc.RuntimeStatus())
	}
	defer svc.StopRuntime()
	path := "/api/v1/admin/connections/con_connection_meta/metadata"
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
		return res
	}
	get := func(session *http.Cookie) service.ConnectionMetadataRecord {
		res := request("GET", path, "", "", session, "")
		row := decodeCatalogResponse[service.ConnectionMetadataRecord](t, res, 200)
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(res.Body.Bytes(), &fields); err != nil || len(fields) != 14 || res.Header().Get("Cache-Control") != "private, no-store" || res.Header().Get("ETag") != strconv.Quote(row.ETag) || len(row.ETag) != 129 || row.ETag[64] != '.' {
			t.Fatal("metadata projection/header contract", err, res.Body.String())
		}
		for _, key := range []string{"id", "provider_id", "name", "protocol", "base_url", "egress_mode", "egress_id", "etag", "can_edit", "adapter", "api_version", "transport_generation", "can_edit_transport", "transport_locked"} {
			if _, ok := fields[key]; !ok {
				t.Fatal("missing metadata field", key)
			}
		}
		if row.Adapter != "native" || row.APIVersion != nil || string(fields["api_version"]) != "null" {
			t.Fatal("native metadata adapter/API version contract")
		}
		if row.ID != "con_connection_meta" || row.ProviderID != "prv_connection_meta" || row.BaseURL != upstream.URL+"/v1" || row.Protocol != entity.ProtocolOpenAIChat {
			t.Fatal("metadata identity changed")
		}
		return row
	}
	put := func(session *http.Cookie, csrf, etag, name, reason string, status int) service.ConnectionMetadataWriteResult {
		raw, err := json.Marshal(service.ConnectionMetadataInput{Name: name, Reason: reason})
		if err != nil {
			t.Fatal(err)
		}
		lastSQLError.Store("")
		res := request("PUT", path, string(raw), strconv.Quote(etag), session, csrf)
		if res.Code != status {
			t.Fatalf("name %q status %d want %d; SQL error %s; response %s", name, res.Code, status, lastSQLError.Load(), res.Body.String())
		}
		expectStatus(t, res, status)
		if status != 200 {
			return service.ConnectionMetadataWriteResult{}
		}
		row := decodeCatalogResponse[service.ConnectionMetadataWriteResult](t, res, 200)
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(res.Body.Bytes(), &fields); err != nil || len(fields) != 3 || !row.RuntimeApplied || row.Connection.Name != name || row.Connection.ID != "con_connection_meta" || res.Header().Get("ETag") != strconv.Quote(row.Connection.ETag) || res.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("write confirmation contract", err, res.Body.String())
		}
		if status := svc.RuntimeStatus(); !status.Ready || status.ErrorCode != "" || status.SnapshotID == "" {
			t.Fatal("applied response lacks current live publication", status)
		}
		return row
	}
	auditCount := func() int64 {
		var n int64
		if err := db.Model(&entity.AuditEvent{}).Where("action = ? AND resource_id = ?", "connection.metadata.update", "con_connection_meta").Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}
	readConnection := func() entity.ProviderConnection {
		var row entity.ProviderConnection
		if err := db.First(&row, "id = ?", "con_connection_meta").Error; err != nil {
			t.Fatal(err)
		}
		return row
	}
	original := readConnection()
	var credential entity.ProviderCredential
	var providerModel entity.ProviderModel
	var binding entity.ModelProviderBinding
	var accesses []entity.CredentialModelAccess
	var egress entity.Egress
	if err := db.First(&credential, "id = ?", "crd_connection_meta").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&providerModel, "id = ?", "pmd_connection_meta").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&binding, "id = ?", "bnd_connection_meta").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("credential_id = ?", credential.ID).Find(&accesses).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&egress, "id = ?", "egr_connection_meta").Error; err != nil {
		t.Fatal(err)
	}
	preserved := func() {
		row := readConnection()
		row.Name, row.ETag = original.Name, original.ETag
		if !reflect.DeepEqual(row, original) {
			t.Fatal("name update changed URL/protocol/egress/identity", row, original)
		}
		var c entity.ProviderCredential
		var p entity.ProviderModel
		var b entity.ModelProviderBinding
		var a []entity.CredentialModelAccess
		var e entity.Egress
		for _, query := range []struct {
			dest any
			id   string
		}{{&c, credential.ID}, {&p, providerModel.ID}, {&b, binding.ID}, {&e, egress.ID}} {
			if err := db.First(query.dest, "id = ?", query.id).Error; err != nil {
				t.Fatal(err)
			}
		}
		if err := db.Where("credential_id = ?", credential.ID).Find(&a).Error; err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(c, credential) || !reflect.DeepEqual(p, providerModel) || !reflect.DeepEqual(b, binding) || !reflect.DeepEqual(a, accesses) || !reflect.DeepEqual(e, egress) {
			t.Fatal("name update changed retained children/weights/egress")
		}
	}
	expectStatus(t, request("GET", path, "", "", nil, ""), 401)
	expectStatus(t, request("GET", path, "", "", deniedCookie, ""), 403)
	expectStatus(t, request("GET", "/api/v1/admin/connections/con_missing/metadata", "", "", deniedCookie, ""), 403)
	readReview := get(readCookie)
	if readReview.CanEdit {
		t.Fatal("read authority implied write")
	}
	put(readCookie, readCSRF, readReview.ETag, "Forbidden", "read only", 403)
	put(deniedCookie, deniedCSRF, readReview.ETag, "Forbidden", "no provider authority", 403)
	adminReview := get(cookie)
	writerReview := get(writeCookie)
	if !adminReview.CanEdit || !writerReview.CanEdit || adminReview.ETag == writerReview.ETag {
		t.Fatal("actor-bound independent edit proof")
	}
	put(cookie, admin.CSRFToken, writerReview.ETag, "Cross actor", "identity mismatch", 409)
	for _, alias := range []string{"con_CONNECTION_meta", "con_connection_meta "} {
		expectStatus(t, request("GET", "/api/v1/admin/connections/"+strings.ReplaceAll(alias, " ", "%20")+"/metadata", "", "", cookie, ""), map[bool]int{true: 400, false: 404}[strings.HasSuffix(alias, " ")])
	}
	var app *apperrors.Error
	if _, err := svc.GetConnectionMetadata(ctx, "usr_"+strings.ToUpper(strings.TrimPrefix(reader.User.ID, "usr_")), "con_connection_meta"); !errors.As(err, &app) || app.Code != 401 {
		t.Fatal("actor alias borrowed authority", err)
	}
	for _, raw := range []string{`{"name":"Bad","reason":"why","protocol":"openai_responses"}`, `{"name":"Bad","name":"Other","reason":"why"}`, `{"name":"Bad"}`, `{"name":null,"reason":"why"}`, `{"name":"Bad","reason":""}`} {
		expectStatus(t, request("PUT", path, raw, strconv.Quote(adminReview.ETag), cookie, admin.CSRFToken), 400)
	}
	expectStatus(t, request("PUT", path, `{"name":"Bad","reason":"why"}`, "", cookie, admin.CSRFToken), 400)
	expectStatus(t, request("PUT", path, `{"name":"Bad","reason":"why"}`, "W/"+strconv.Quote(adminReview.ETag), cookie, admin.CSRFToken), 400)
	expectStatus(t, request("PUT", path, `{"name":"Bad","reason":"why"}`, strconv.Quote(adminReview.ETag), cookie, ""), 403)
	expectStatus(t, request("GET", path+"?unexpected=true", "", "", cookie, ""), 400)
	if readConnection() != original || auditCount() != 0 {
		t.Fatal("invalid/denied input mutated configuration")
	}
	failRead.Store(true)
	expectStatus(t, request("GET", path, "", "", cookie, ""), 503)
	failRead.Store(false)
	result := put(cookie, admin.CSRFToken, adminReview.ETag, "Renamed", "reviewed rename", 200)
	if !result.Changed || auditCount() != 1 || readConnection().ETag == original.ETag {
		t.Fatal("rename did not advance one revision/audit")
	}
	preserved()
	put(cookie, admin.CSRFToken, adminReview.ETag, "Stale replacement", "stale review", 409)
	if row := put(cookie, admin.CSRFToken, adminReview.ETag, "Renamed", "reviewed rename", 200); row.Changed || auditCount() != 1 {
		t.Fatal("same-current retry duplicated update/audit")
	}
	// Original actor proof remains required even when the current name matches.
	put(writeCookie, writeCSRF, adminReview.ETag, "Renamed", "reviewed rename", 409)
	// Read permission can be revoked independently; writer-only PUT uses its own
	// previously reviewed identity and remains subject to current write authority.
	writerReview = get(writeCookie)
	var assignment entity.UserRole
	if err := db.First(&assignment, "user_id = ?", writer.User.ID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveRole(ctx, admin.User.ID, assignment.RoleID, "System connection-writer", []string{"providers.write"}); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request("GET", path, "", "", writeCookie, ""), 403)
	if row := put(writeCookie, writeCSRF, writerReview.ETag, "Writer rename", "independent write", 200); !row.Changed {
		t.Fatal("writer-only rename not applied")
	}
	if _, err := svc.SaveRole(ctx, admin.User.ID, assignment.RoleID, "System connection-writer", []string{"providers.read"}); err != nil {
		t.Fatal(err)
	}
	put(writeCookie, writeCSRF, writerReview.ETag, "Writer rename", "independent write", 403)
	// Audit failure must roll back both name and shared revision on both drivers.
	beforeFault := readConnection()
	beforeAudit := auditCount()
	review := get(cookie)
	failAudit.Store(true)
	put(cookie, admin.CSRFToken, review.ETag, "Rollback", "audit persistence fault", 500)
	failAudit.Store(false)
	if !reflect.DeepEqual(readConnection(), beforeFault) || auditCount() != beforeAudit {
		t.Fatal("audit fault failed atomic rollback")
	}
	// A real shared egress edit invalidates the name review. Conversely, the old
	// raw egress review must fail after a rename before any diagnostic dispatch.
	if _, err := svc.SetConnectionEgress(ctx, admin.User.ID, "con_connection_meta", service.ConnectionEgressInput{ETag: original.ETag, Mode: "default"}); err == nil {
		t.Fatal("rename did not invalidate egress review")
	}
	rawBefore := readConnection()
	if _, err := svc.SetConnectionEgress(ctx, admin.User.ID, "con_connection_meta", service.ConnectionEgressInput{ETag: rawBefore.ETag, Mode: "default"}); err != nil {
		t.Fatal(err)
	}
	put(cookie, admin.CSRFToken, review.ETag, "Egress stale", "old egress generation", 409)
	changedEgress := readConnection()
	if changedEgress.EgressMode != "default" || changedEgress.ETag == rawBefore.ETag {
		t.Fatal("real egress edit missing")
	}
	original.EgressMode = "default" // The authorized egress edit is the sole intentional non-name change.
	preserved()
	// Portable persisted birth changes must reject identical-current retries.
	review = get(cookie)
	for _, test := range []struct {
		model any
		id    string
	}{{&entity.User{}, admin.User.ID}, {&entity.Provider{}, "prv_connection_meta"}, {&entity.ProviderConnection{}, "con_connection_meta"}} {
		var originalBirth struct{ CreatedAt time.Time }
		if err := db.Model(test.model).Where("id = ?", test.id).Select("created_at").Take(&originalBirth).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(test.model).Where("id = ?", test.id).Update("created_at", originalBirth.CreatedAt.Add(time.Millisecond)).Error; err != nil {
			t.Fatal(err)
		}
		put(cookie, admin.CSRFToken, review.ETag, review.Name, "old identity must not reconcile", 409)
		if err := db.Model(test.model).Where("id = ?", test.id).Update("created_at", originalBirth.CreatedAt).Error; err != nil {
			t.Fatal(err)
		}
	}
	// MySQL's FK collation permits this physical alias; exact service identities
	// must still reject it. PostgreSQL may enforce the FK before the read.
	aliasUpdate := db.Model(&entity.ProviderConnection{}).Where("id = ?", "con_connection_meta").Update("provider_id", "prv_CONNECTION_meta")
	if db.Name() == "mysql" {
		if aliasUpdate.Error != nil {
			t.Fatal(aliasUpdate.Error)
		}
		expectStatus(t, request("GET", path, "", "", cookie, ""), 404)
		put(cookie, admin.CSRFToken, review.ETag, review.Name, "provider alias", 404)
	} else if aliasUpdate.Error == nil {
		t.Fatal("exact Provider foreign key accepted alias")
	}
	if err := db.Model(&entity.ProviderConnection{}).Where("id = ?", "con_connection_meta").Update("provider_id", "prv_connection_meta").Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	// Publication failure commits one typed audit, but cannot claim application.
	review = get(cookie)
	beforeAudit = auditCount()
	failPublication.Store(true)
	put(cookie, admin.CSRFToken, review.ETag, "Publication pending", "immutable pending intent", 503)
	if row := get(cookie); row.Name != "Publication pending" || auditCount() != beforeAudit+1 {
		t.Fatal("failed publication lost saved configuration/audit")
	}
	if status := svc.RuntimeStatus(); status.ErrorCode != "database_unavailable" {
		t.Fatal("real publication failure not recorded", status)
	}
	put(cookie, admin.CSRFToken, review.ETag, "Publication pending", "immutable pending intent", 503)
	if auditCount() != beforeAudit+1 {
		t.Fatal("uncertain retry repeated committed audit")
	}
	failPublication.Store(false)
	if row := put(cookie, admin.CSRFToken, review.ETag, "Publication pending", "immutable pending intent", 200); row.Changed || auditCount() != beforeAudit+1 {
		t.Fatal("exact retry did not reconcile current saved publication")
	}
	preserved()
	// A disabled proxy/no ready route is configuration confirmation, never native
	// availability. It must not trigger egress probing or borrow another route.
	disabledPath := "/api/v1/admin/connections/con_connection_disabled/metadata"
	disabled := decodeCatalogResponse[service.ConnectionMetadataRecord](t, request("GET", disabledPath, "", "", cookie, ""), 200)
	disabledRaw := `{"name":"Disabled renamed","reason":"configuration only"}`
	disabledResult := decodeCatalogResponse[service.ConnectionMetadataWriteResult](t, request("PUT", disabledPath, disabledRaw, strconv.Quote(disabled.ETag), cookie, admin.CSRFToken), 200)
	if !disabledResult.RuntimeApplied || !disabledResult.Changed || disabledResult.Connection.EgressMode != "proxy" || disabledResult.Connection.EgressID == nil || *disabledResult.Connection.EgressID != "egr_connection_meta" {
		t.Fatal("disabled egress configuration confirmation mismatch")
	}
	page := decodeCatalogResponse[service.AuditPage](t, request("GET", "/api/v1/admin/audit?category=credentials&q=connection.metadata.update", "", "", cookie, ""), 200)
	if len(page.Items) != int(auditCount())+1 {
		t.Fatal("typed audit projection missing", len(page.Items), auditCount())
	}
	expectedAudit := map[string]struct{ before, reason, actor, target string }{
		"Renamed":             {"Original", "reviewed rename", admin.User.ID, "con_connection_meta"},
		"Writer rename":       {"Renamed", "independent write", writer.User.ID, "con_connection_meta"},
		"Publication pending": {"Writer rename", "immutable pending intent", admin.User.ID, "con_connection_meta"},
		"Disabled renamed":    {"Disabled route", "configuration only", admin.User.ID, "con_connection_disabled"},
	}
	for _, row := range page.Items {
		var details struct {
			Before, After map[string]string
			Reason        string
		}
		if err := json.Unmarshal(row.Changes, &details); err != nil || len(details.Before) != 1 || len(details.After) != 1 || details.Before["name"] == "" || details.After["name"] == "" || details.Reason == "" || row.ResourceType != "connection" || row.Result != "committed" {
			t.Fatal("unsafe/incomplete typed audit", err, string(row.Changes))
		}
		expected, ok := expectedAudit[details.After["name"]]
		if !ok || details.Before["name"] != expected.before || details.Reason != expected.reason || row.ActorID != expected.actor || row.ResourceID != expected.target || row.Action != "connection.metadata.update" {
			t.Fatal("typed audit did not preserve exact committed transition", row, string(row.Changes))
		}
		delete(expectedAudit, details.After["name"])
		for _, forbidden := range []string{"test-only-connection-secret", "ciphertext", "base_url", "protocol", "egress", "priority"} {
			if strings.Contains(string(row.Changes), forbidden) {
				t.Fatal("audit leaked unrelated metadata", forbidden)
			}
		}
	}
	if len(expectedAudit) != 0 {
		t.Fatal("missing exact typed audit transition", expectedAudit)
	}
	if nativeCalls.Load() != 0 {
		t.Fatal("metadata operation performed native inference", nativeCalls.Load())
	}
	// Even an identical saved value cannot be acknowledged with a closed worker.
	svc.StopRuntime()
	put(cookie, admin.CSRFToken, review.ETag, "Publication pending", "immutable pending intent", 503)
}

func ptrConnectionMetadata(value string) *string { return &value }
