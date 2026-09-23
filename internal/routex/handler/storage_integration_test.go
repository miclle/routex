package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/objectstore"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

type storageFixtureObject struct {
	data           []byte
	owner, version string
}
type storageFixture struct {
	mu                  sync.Mutex
	objects             map[string]storageFixtureObject
	sequence            int
	failGet, failDelete bool
}

func newStorageFixture(t *testing.T) (*httptest.Server, *storageFixture) {
	t.Helper()
	fixture := &storageFixture{objects: map[string]storageFixtureObject{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fixture.mu.Lock()
		defer fixture.mu.Unlock()
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=test-only-access/") || !strings.HasPrefix(r.URL.Path, "/routex-test/") {
			w.WriteHeader(403)
			return
		}
		object, found := fixture.objects[r.URL.Path]
		if r.Method == "PUT" {
			if found || r.Header.Get("If-None-Match") != "*" {
				w.WriteHeader(412)
				return
			}
			data, err := io.ReadAll(io.LimitReader(r.Body, objectstore.MaxBytes+1))
			if err != nil {
				w.WriteHeader(500)
				return
			}
			fixture.sequence++
			version := fmt.Sprintf("version-%d", fixture.sequence)
			fixture.objects[r.URL.Path] = storageFixtureObject{data, r.Header.Get("X-Amz-Meta-Routex-Id"), version}
			w.Header().Set("X-Amz-Version-Id", version)
			return
		}
		if !found {
			w.WriteHeader(404)
			return
		}
		if q := r.URL.Query().Get("versionId"); q != "" && q != object.version {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("X-Amz-Version-Id", object.version)
		w.Header().Set("X-Amz-Meta-Routex-Id", object.owner)
		switch r.Method {
		case "GET":
			if fixture.failGet {
				w.WriteHeader(503)
				return
			}
			_, _ = w.Write(object.data)
		case "HEAD":
			w.Header().Set("Content-Length", fmt.Sprint(len(object.data)))
		case "DELETE":
			if fixture.failDelete {
				w.WriteHeader(503)
				return
			}
			if r.URL.Query().Get("versionId") != object.version {
				t.Error("cleanup omitted exact version")
				w.WriteHeader(400)
				return
			}
			delete(fixture.objects, r.URL.Path)
			w.WriteHeader(204)
		default:
			t.Error("unexpected S3 operation")
			w.WriteHeader(400)
		}
	}))
	t.Cleanup(server.Close)
	return server, fixture
}
func testStorageLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	server, fixture := newStorageFixture(t)
	store, err := secretstore.New(bytes.Repeat([]byte{37}, 32))
	if err != nil {
		t.Fatal(err)
	}
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithStoragePolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"storage-admin@example.invalid","password":"test-only-admin-password","name":"Storage Admin"}`, nil, "")
	expectStatus(t, setup, 201)
	auth, cookie := readIdentity(t, setup)
	request := func(method, path string, input any) *httptest.ResponseRecorder {
		encoded, _ := json.Marshal(input)
		return identityRequest(router, method, path, string(encoded), cookie, auth.CSRFToken)
	}
	initial := decodeCatalogResponse[service.StorageView](t, request("GET", "/api/v1/admin/storage", nil), 200)
	if initial.Enabled || initial.Revision != nil || initial.ETag != "0" {
		t.Fatal("storage not disabled by default")
	}
	expectStatus(t, request("PUT", "/api/v1/admin/storage", map[string]any{"unknown": true}), 400)
	expectStatus(t, identityRequest(router, "PUT", "/api/v1/admin/storage", `{}`, cookie, ""), 403)
	input := service.StorageInput{Enabled: true, Endpoint: server.URL, Region: "us-east-1", Bucket: "routex-test", Prefix: "first", ETag: initial.ETag, Auth: service.StorageAuthInput{Action: "replace", AccessKey: "test-only-access", SecretKey: "test-only-secret"}}
	enabled := decodeCatalogResponse[service.StorageView](t, request("PUT", "/api/v1/admin/storage", input), 200)
	if !enabled.Enabled || enabled.Revision == nil || enabled.Revision.VerifiedAt == nil {
		t.Fatal("verified revision not published")
	}
	fixture.mu.Lock()
	remaining := len(fixture.objects)
	fixture.mu.Unlock()
	if remaining != 0 {
		t.Fatal("configuration probe leaked a version")
	}
	var revision entity.StorageRevision
	if err := db.First(&revision, "id = ?", enabled.Revision.ID).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(revision.AuthCiphertext, "test-only") || revision.AuthCiphertext == "" {
		t.Fatal("plaintext credentials")
	}
	encoded, _ := json.Marshal(enabled)
	if strings.Contains(string(encoded), "test-only") {
		t.Fatal("credentials returned to client")
	}
	body := append([]byte("%PDF-1.7\n"), bytes.Repeat([]byte("a"), 70<<10)...)
	body = append(body, []byte("\n%%EOF")...)
	upload := func(payload []byte, csrf string) *httptest.ResponseRecorder {
		var content bytes.Buffer
		writer := multipart.NewWriter(&content)
		part, e := writer.CreateFormFile("file", "document.pdf")
		if e != nil {
			t.Fatal(e)
		}
		_, _ = part.Write(payload)
		if e := writer.Close(); e != nil {
			t.Fatal(e)
		}
		req := httptest.NewRequest("POST", "/api/v1/attachments", &content)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("Origin", "http://example.com")
		req.Header.Set("X-CSRF-Token", csrf)
		req.AddCookie(cookie)
		result := httptest.NewRecorder()
		router.ServeHTTP(result, req)
		return result
	}
	expectStatus(t, upload(body, ""), 403)
	object := decodeCatalogResponse[service.AttachmentView](t, upload(body, auth.CSRFToken), 201)
	if object.State != "ready" || object.MIME != "application/pdf" {
		t.Fatal("unverified attachment published")
	}
	download := request("GET", "/api/v1/attachments/"+object.ID+"/content", nil)
	expectStatus(t, download, 200)
	if !bytes.Equal(download.Body.Bytes(), body) || download.Header().Get("Cache-Control") != "private, no-store" || !strings.HasPrefix(download.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatal("unsafe content response")
	}
	expectStatus(t, upload([]byte("<script>not a file</script>"), auth.CSRFToken), 400)
	member, err := svc.CreateMember(ctx, auth.User.ID, "storage-member@example.invalid", "test-only-member-password", "Member", "member")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Attachment(ctx, member.User.ID, object.ID); err == nil {
		t.Fatal("foreign attachment metadata disclosed")
	}
	if _, _, err := svc.AttachmentContent(ctx, member.User.ID, object.ID); err == nil {
		t.Fatal("foreign bytes disclosed")
	}
	if _, err := svc.DeleteAttachment(ctx, member.User.ID, object.ID); err == nil {
		t.Fatal("foreign attachment deleted")
	}
	if _, err := svc.StorageSettings(ctx, member.User.ID); err == nil {
		t.Fatal("unprivileged storage configuration read")
	}
	// Changing the active prefix never changes the descriptor of old attachments.
	input.ETag = enabled.ETag
	input.Prefix = "second"
	input.Auth = service.StorageAuthInput{Action: "keep"}
	changed := decodeCatalogResponse[service.StorageView](t, request("PUT", "/api/v1/admin/storage", input), 200)
	if changed.Revision.ID == enabled.Revision.ID {
		t.Fatal("mutable storage descriptor")
	}
	if _, data, err := svc.AttachmentContent(ctx, auth.User.ID, object.ID); err != nil || !bytes.Equal(data, body) {
		t.Fatal("configuration change broke historical attachment")
	}
	expectStatus(t, request("POST", "/api/v1/admin/storage/rollback", service.StorageRollbackInput{RevisionID: enabled.Revision.ID, ETag: enabled.ETag, Enabled: true}), 409)
	rollback := decodeCatalogResponse[service.StorageView](t, request("POST", "/api/v1/admin/storage/rollback", service.StorageRollbackInput{RevisionID: enabled.Revision.ID, ETag: changed.ETag, Enabled: true}), 200)
	if rollback.Revision.ID != enabled.Revision.ID {
		t.Fatal("rollback did not restore revision")
	}
	// Failed real verification retains the last valid active revision and durable cleanup.
	fixture.mu.Lock()
	fixture.failGet = true
	fixture.failDelete = true
	fixture.mu.Unlock()
	input.Prefix = "failed"
	input.ETag = rollback.ETag
	expectStatus(t, request("PUT", "/api/v1/admin/storage", input), 422)
	retained := decodeCatalogResponse[service.StorageView](t, request("GET", "/api/v1/admin/storage", nil), 200)
	if retained.ETag != rollback.ETag || retained.Revision.ID != rollback.Revision.ID {
		t.Fatal("failed verification replaced active config")
	}
	fixture.mu.Lock()
	fixture.failGet = false
	fixture.failDelete = false
	fixture.mu.Unlock()
	input.Prefix = "first"
	input.Enabled = false
	input.ETag = rollback.ETag
	disabled := decodeCatalogResponse[service.StorageView](t, request("PUT", "/api/v1/admin/storage", input), 200)
	if disabled.Enabled {
		t.Fatal("disable ignored")
	}
	expectStatus(t, upload(body, auth.CSRFToken), 503)
	if _, data, err := svc.AttachmentContent(ctx, auth.User.ID, object.ID); err != nil || !bytes.Equal(data, body) {
		t.Fatal("disable should not orphan stored attachments")
	}
	pending := decodeCatalogResponse[service.AttachmentView](t, request("DELETE", "/api/v1/attachments/"+object.ID, nil), 200)
	if pending.State != "delete_pending" {
		t.Fatal("delete intent not durable")
	}
	expectStatus(t, request("GET", "/api/v1/attachments/"+object.ID+"/content", nil), 404)
	restarted, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithStoragePolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.StorageObject{}).Where("state = ?", "delete_pending").Update("next_cleanup_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	if err := restarted.FlushStorageCleanup(ctx, 32); err != nil {
		t.Fatal(err)
	}
	deleted, err := restarted.Attachment(ctx, auth.User.ID, object.ID)
	if err != nil || deleted.State != "deleted" {
		t.Fatal("restart did not complete durable cleanup")
	}
	fixture.mu.Lock()
	remaining = len(fixture.objects)
	fixture.mu.Unlock()
	if remaining != 0 {
		t.Fatal("exact owned versions remain after cleanup")
	}
	if err := restarted.FlushStorageCleanup(ctx, 32); err != nil {
		t.Fatal("idempotent cleanup failed")
	}
	// An ambiguous interrupted PUT cannot be declared cleaned just because HEAD
	// currently returns 404: a server may still finish accepting that request.
	uncertain := entity.StorageObject{ID: "obj_storage_uncertain", OwnerID: auth.User.ID, RevisionID: enabled.Revision.ID, Purpose: "probe", State: "uploading", CreatedAt: time.Now().Add(-2 * time.Minute), NextCleanupAt: time.Now().Add(-2 * time.Minute)}
	if err := db.Create(&uncertain).Error; err != nil {
		t.Fatal(err)
	}
	if err := restarted.FlushStorageCleanup(ctx, 32); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&uncertain, "id = ?", uncertain.ID).Error; err != nil {
		t.Fatal(err)
	}
	if uncertain.State != "delete_pending" || uncertain.CleanupCode != "upload_uncertain" {
		t.Fatal("uncertain write incorrectly declared clean")
	}
	fixture.mu.Lock()
	fixture.objects["/routex-test/first/routex/"+uncertain.ID] = storageFixtureObject{[]byte("late upload"), uncertain.ID, "late-version"}
	fixture.mu.Unlock()
	if err := db.Model(&uncertain).Update("next_cleanup_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	if err := restarted.FlushStorageCleanup(ctx, 32); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&uncertain, "id = ?", uncertain.ID).Error; err != nil || uncertain.State != "deleted" {
		t.Fatal("late accepted upload was not cleaned")
	}
}
