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
	"reflect"
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
	sequence, getCalls  int
	failGet, failDelete bool
	blockGetStarted     chan struct{}
	blockGetContinue    chan struct{}
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
			fixture.getCalls++
			if fixture.blockGetStarted != nil {
				close(fixture.blockGetStarted)
				fixture.blockGetStarted = nil
				<-fixture.blockGetContinue
				fixture.blockGetContinue = nil
			}
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
	var storedObject entity.StorageObject
	if err := db.First(&storedObject, "id = ?", object.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !storedObject.UploadConfirmed || storedObject.VersionID == "" || !storedObject.NextCleanupAt.After(time.Now().Add(50*time.Minute)) {
		t.Fatal("ready attachment does not have a durable expiry")
	}
	if err := svc.FlushStorageCleanup(ctx, 32); err != nil {
		t.Fatal(err)
	}
	if current, err := svc.Attachment(ctx, auth.User.ID, object.ID); err != nil || current.State != "ready" {
		t.Fatal("attachment was cleaned before its deadline")
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
	projectManager, err := svc.CreateMember(ctx, auth.User.ID, "project-storage-manager@example.invalid", "project-manager-password", "Project manager", "member")
	if err != nil {
		t.Fatal(err)
	}
	successor, err := svc.CreateMember(ctx, auth.User.ID, "project-storage-successor@example.invalid", "project-successor-password", "Project successor", "member")
	if err != nil {
		t.Fatal(err)
	}
	managerAuth, managerCookie := readIdentity(t, identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"project-storage-manager@example.invalid","password":"project-manager-password"}`, nil, ""))
	successorAuth, successorCookie := readIdentity(t, identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"project-storage-successor@example.invalid","password":"project-successor-password"}`, nil, ""))
	managerRequest := func(method, path string, input any) *httptest.ResponseRecorder {
		encoded, _ := json.Marshal(input)
		return identityRequest(router, method, path, string(encoded), managerCookie, managerAuth.CSRFToken)
	}
	successorRequest := func(method, path string, input any) *httptest.ResponseRecorder {
		encoded, _ := json.Marshal(input)
		return identityRequest(router, method, path, string(encoded), successorCookie, successorAuth.CSRFToken)
	}
	project := decodeCatalogResponse[ProjectResponse](t, managerRequest("POST", "/api/v1/projects", map[string]any{"name": "Project attachment scope"}), 201)
	if len(project.Managers) != 1 || project.Managers[0].UserID != projectManager.User.ID {
		t.Fatal("Project was not created under the authenticated manager")
	}
	projectUpload := func(projectID, filename string, payload []byte, sessionCookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
		var content bytes.Buffer
		writer := multipart.NewWriter(&content)
		part, e := writer.CreateFormFile("file", filename)
		if e != nil {
			t.Fatal(e)
		}
		_, _ = part.Write(payload)
		if e := writer.Close(); e != nil {
			t.Fatal(e)
		}
		req := httptest.NewRequest("POST", "/api/v1/projects/"+projectID+"/attachments", &content)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("Origin", "http://example.com")
		req.Header.Set("X-CSRF-Token", csrf)
		req.AddCookie(sessionCookie)
		result := httptest.NewRecorder()
		router.ServeHTTP(result, req)
		return result
	}
	projectObject := decodeCatalogResponse[service.AttachmentView](t, projectUpload(project.ID, "project-document.pdf", body, managerCookie, managerAuth.CSRFToken), 201)
	var projectStoredObject entity.StorageObject
	if err := db.First(&projectStoredObject, "id = ? AND owner_kind = ? AND owner_id = ?", projectObject.ID, entity.StorageOwnerProject, project.ID).Error; err != nil {
		t.Fatalf("Project attachment ownership was not durable: %v", err)
	}
	projectPath := "/api/v1/projects/" + project.ID + "/attachments/" + projectObject.ID
	expectStatus(t, managerRequest("GET", projectPath+"/content", nil), 200)
	expectStatus(t, request("GET", projectPath, nil), 404)
	expectStatus(t, request("GET", projectPath+"/content", nil), 404)
	expectStatus(t, request("DELETE", projectPath, nil), 404)
	expectStatus(t, projectUpload(project.ID, "admin-document.pdf", body, cookie, auth.CSRFToken), 404)
	expectStatus(t, managerRequest("GET", "/api/v1/attachments/"+projectObject.ID, nil), 404)
	expectStatus(t, managerRequest("GET", "/api/v1/projects/"+project.ID+"/attachments/"+object.ID, nil), 404)
	otherProject := decodeCatalogResponse[ProjectResponse](t, managerRequest("POST", "/api/v1/projects", map[string]any{"name": "Other Project attachment scope"}), 201)
	expectStatus(t, managerRequest("GET", "/api/v1/projects/"+otherProject.ID+"/attachments/"+projectObject.ID, nil), 404)

	fixture.mu.Lock()
	fixture.blockGetStarted = make(chan struct{})
	fixture.blockGetContinue = make(chan struct{})
	started, resume := fixture.blockGetStarted, fixture.blockGetContinue
	fixture.mu.Unlock()
	racingUpload := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		racingUpload <- projectUpload(project.ID, "racing-document.pdf", body, managerCookie, managerAuth.CSRFToken)
	}()
	<-started
	if _, err := svc.SetProjectManagers(ctx, auth.User.ID, project.ID, []string{successor.User.ID}); err != nil {
		t.Fatal(err)
	}
	close(resume)
	expectStatus(t, <-racingUpload, 404)
	var raced entity.StorageObject
	if err := db.Where("owner_kind = ? AND owner_id = ? AND name = ?", entity.StorageOwnerProject, project.ID, "racing-document.pdf").First(&raced).Error; err != nil || raced.State != "delete_pending" {
		t.Fatalf("manager removal published in-flight Project upload: state=%q err=%v", raced.State, err)
	}
	expectStatus(t, managerRequest("GET", projectPath+"/content", nil), 404)
	successorContent := successorRequest("GET", projectPath+"/content", nil)
	expectStatus(t, successorContent, 200)
	if !bytes.Equal(successorContent.Body.Bytes(), body) {
		t.Fatal("successor manager could not read the existing Project object")
	}
	fixture.mu.Lock()
	fixture.blockGetStarted = make(chan struct{})
	fixture.blockGetContinue = make(chan struct{})
	started, resume = fixture.blockGetStarted, fixture.blockGetContinue
	fixture.mu.Unlock()
	racingContent := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		racingContent <- successorRequest("GET", projectPath+"/content", nil)
	}()
	<-started
	if _, err := svc.SetProjectManagers(ctx, auth.User.ID, project.ID, []string{projectManager.User.ID}); err != nil {
		t.Fatal(err)
	}
	close(resume)
	expectStatus(t, <-racingContent, 404)
	expectStatus(t, successorRequest("GET", projectPath+"/content", nil), 404)
	expectStatus(t, managerRequest("GET", projectPath+"/content", nil), 200)
	disabledStatus := entity.ResourceDisabled
	if _, err := svc.UpdateResource(ctx, auth.User.ID, service.ProjectResource, project.ID, service.ResourceUpdate{Status: &disabledStatus}); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, projectUpload(project.ID, "disabled-document.pdf", body, managerCookie, managerAuth.CSRFToken), 404)
	expectStatus(t, managerRequest("GET", projectPath, nil), 200)
	expectStatus(t, managerRequest("GET", projectPath+"/content", nil), 200)
	archived := entity.ResourceArchived
	if _, err := svc.UpdateResource(ctx, auth.User.ID, service.ProjectResource, project.ID, service.ResourceUpdate{Status: &archived}); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, managerRequest("GET", projectPath, nil), 200)
	expectStatus(t, managerRequest("GET", projectPath+"/content", nil), 200)
	expectStatus(t, managerRequest("DELETE", projectPath, nil), 200)
	if err := db.Model(&entity.StorageObject{}).Where("owner_kind = ? AND owner_id = ? AND state = ?", entity.StorageOwnerProject, project.ID, "delete_pending").Update("next_cleanup_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.FlushStorageCleanup(ctx, 32); err != nil {
		t.Fatal(err)
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
	expiring := decodeCatalogResponse[service.AttachmentView](t, upload(body, auth.CSRFToken), 201)
	if err := db.Model(&entity.StorageObject{}).Where("id = ?", expiring.ID).Updates(map[string]any{"next_cleanup_at": time.Now().Add(-time.Minute), "created_at": time.Now().Add(-2 * time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.AttachmentContent(ctx, auth.User.ID, expiring.ID); err == nil {
		t.Fatal("expired attachment remained readable before cleanup")
	}
	probe := entity.StorageObject{ID: "obj_ready_probe", OwnerID: auth.User.ID, RevisionID: enabled.Revision.ID, Purpose: "probe", State: "ready", UploadConfirmed: true, VersionID: "probe-version", NextCleanupAt: time.Now().Add(-time.Minute), CreatedAt: time.Now().Add(-2 * time.Hour)}
	if err := db.Create(&probe).Error; err != nil {
		t.Fatal(err)
	}
	fixture.mu.Lock()
	fixture.failDelete = true
	fixture.mu.Unlock()
	if err := svc.FlushStorageCleanup(ctx, 32); err != nil {
		t.Fatal(err)
	}
	var expiredRow entity.StorageObject
	if err := db.First(&expiredRow, "id = ?", expiring.ID).Error; err != nil || expiredRow.State != "delete_pending" || expiredRow.CleanupCode != "delete_failed" {
		t.Fatal("failed expiry deletion did not preserve retry intent")
	}
	if err := db.First(&probe, "id = ?", probe.ID).Error; err != nil || probe.State != "ready" {
		t.Fatal("ready non-attachment was claimed by attachment expiry")
	}
	fixture.mu.Lock()
	fixture.failDelete = false
	fixture.mu.Unlock()
	if err := db.Model(&entity.StorageObject{}).Where("id = ?", expiring.ID).Update("next_cleanup_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.FlushStorageCleanup(ctx, 32); err != nil {
		t.Fatal(err)
	}
	expired, err := svc.Attachment(ctx, auth.User.ID, expiring.ID)
	if err != nil || expired.State != "deleted" {
		t.Fatal("expired ready attachment was not cleaned")
	}
	if _, _, err := svc.AttachmentContent(ctx, auth.User.ID, expiring.ID); err == nil {
		t.Fatal("expired attachment remained readable")
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
	var cleanupJobs []entity.SystemJob
	if err := db.Where("code = ?", service.SystemJobStorageCleanup).Order("started_at").Find(&cleanupJobs).Error; err != nil {
		t.Fatal(err)
	}
	seenCompleted, seenFailed := false, false
	for _, job := range cleanupJobs {
		seenCompleted = seenCompleted || (job.Status == "completed" && job.DetailCode == "cleaned" && job.ItemsCompleted > 0)
		seenFailed = seenFailed || (job.Status == "failed" && job.DetailCode == "delete_failed")
	}
	if !seenCompleted || !seenFailed {
		t.Fatalf("real storage cleanup outcomes were not reported accurately: %+v", cleanupJobs)
	}
	storageLateAcceptedUpload(t, db, store, svc, router, auth, cookie)
}

// Exercise cancellation with a real attempted PUT and durable intent, rather
// than seeding an aged uploading row. Service reconstruction is not a process restart.
func storageLateAcceptedUpload(t *testing.T, db *gorm.DB, store *secretstore.Store, svc *service.Service, router *fox.Engine, auth SessionResponse, cookie *http.Cookie) {
	t.Helper()
	caseCtx, cancelCase := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancelCase()
	var oldObjects []entity.StorageObject
	var oldRevisions []entity.StorageRevision
	if err := db.WithContext(caseCtx).Order("id").Find(&oldObjects).Error; err != nil {
		t.Fatal("late upload original object readback", err)
	}
	if err := db.WithContext(caseCtx).Order("id").Find(&oldRevisions).Error; err != nil {
		t.Fatal("late upload original revision readback", err)
	}
	unchanged := []struct {
		model any
		count int64
	}{{model: &entity.APIKey{}}, {model: &entity.ProjectKey{}}, {model: &entity.CallRecord{}}, {model: &entity.CallAttempt{}}}
	for i := range unchanged {
		if err := db.WithContext(caseCtx).Model(unchanged[i].model).Count(&unchanged[i].count).Error; err != nil {
			t.Fatal("late upload native/key baseline", err)
		}
	}
	payload := []byte("%PDF-1.7\nlate accepted original upload\n%%EOF")
	const targetVersion = "late-owned-version"
	var mu sync.Mutex
	objects := map[string]storageFixtureObject{}
	armed, heldStarted := false, false
	var publishedRevision, targetPath string
	puts, gets, heads, deletes := 0, 0, 0, 0
	entered := make(chan entity.StorageObject, 1)
	release := make(chan struct{})
	putDone := make(chan struct{})
	var releaseOnce sync.Once
	releasePut := func() { releaseOnce.Do(func() { close(release) }) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=test-only-access/") || !strings.HasPrefix(r.URL.Path, "/routex-test/late/routex/") {
			t.Error("late upload storage authority/path")
			w.WriteHeader(403)
			return
		}
		if r.Method == "PUT" {
			owner := r.Header.Get("X-Amz-Meta-Routex-Id")
			if owner == "" || r.URL.Path != "/routex-test/late/routex/"+owner || r.URL.RawQuery != "x-id=PutObject" || r.Header.Get("If-None-Match") != "*" {
				t.Error("late upload conditional ownership")
				w.WriteHeader(400)
				return
			}
			data, err := io.ReadAll(io.LimitReader(r.Body, objectstore.MaxBytes+1))
			if err != nil || len(data) > objectstore.MaxBytes {
				t.Error("late upload bounded body")
				w.WriteHeader(400)
				return
			}
			mu.Lock()
			_, duplicate := objects[r.URL.Path]
			hold := armed
			if hold {
				armed, heldStarted = false, true
				targetPath = r.URL.Path
			}
			isTarget := r.URL.Path == targetPath
			if isTarget {
				puts++
			}
			revision := publishedRevision
			mu.Unlock()
			if duplicate || isTarget && !hold {
				t.Error("late upload repeated conditional PUT")
				w.WriteHeader(412)
				return
			}
			version := "probe-version"
			if hold {
				defer close(putDone)
				var intent entity.StorageObject
				if err := db.WithContext(caseCtx).Take(&intent, "id = ?", owner).Error; err != nil || intent.ID != owner || intent.OwnerKind != entity.StorageOwnerUser || intent.OwnerID != auth.User.ID || intent.RevisionID != revision || intent.Purpose != "attachment" || intent.State != "uploading" || intent.UploadConfirmed || intent.VersionID != "" || intent.Size != int64(len(payload)) || !bytes.Equal(data, payload) {
					t.Error("late upload network entry lacks exact committed intent")
					w.WriteHeader(400)
					return
				}
				entered <- intent
				// Deliberately model a server that has already read the request and
				// can accept it after the caller cancels. The case deadline remains
				// authoritative; no detached unbounded work is permitted.
				select {
				case <-release:
				case <-caseCtx.Done():
					return
				}
				version = targetVersion
			}
			mu.Lock()
			objects[r.URL.Path] = storageFixtureObject{data: data, owner: owner, version: version}
			mu.Unlock()
			w.Header().Set("X-Amz-Version-Id", version)
			return
		}
		mu.Lock()
		isTarget := r.URL.Path == targetPath
		if isTarget {
			switch r.Method {
			case "GET":
				gets++
			case "HEAD":
				heads++
			case "DELETE":
				deletes++
			}
		}
		object, found := objects[r.URL.Path]
		mu.Unlock()
		if !found {
			w.WriteHeader(404)
			return
		}
		query := r.URL.Query()
		// The pinned S3 SDK adds an operation selector to GET and DELETE.
		// Admit only that exact selector before checking the owned version.
		operation := ""
		switch r.Method {
		case "GET":
			operation = "GetObject"
		case "DELETE":
			operation = "DeleteObject"
		}
		if operation != "" {
			if len(query["x-id"]) != 1 || query.Get("x-id") != operation {
				t.Error("late upload nonexact SDK operation selection")
				w.WriteHeader(400)
				return
			}
			query.Del("x-id")
		}
		if len(query) > 1 || len(query) != 0 && (len(query["versionId"]) != 1 || query.Get("versionId") != object.version) {
			t.Error("late upload nonexact version selection")
			w.WriteHeader(400)
			return
		}
		w.Header().Set("X-Amz-Version-Id", object.version)
		w.Header().Set("X-Amz-Meta-Routex-Id", object.owner)
		switch r.Method {
		case "GET":
			_, _ = w.Write(object.data)
		case "HEAD":
			w.Header().Set("Content-Length", fmt.Sprint(len(object.data)))
		case "DELETE":
			if query.Get("versionId") != object.version {
				t.Error("late upload cleanup omitted the resolved exact version")
				w.WriteHeader(400)
				return
			}
			mu.Lock()
			delete(objects, r.URL.Path)
			mu.Unlock()
			w.WriteHeader(204)
		default:
			t.Error("late upload extra storage operation")
			w.WriteHeader(400)
		}
	}))
	var apiDone chan *httptest.ResponseRecorder
	apiJoined, putJoined := false, false
	cancelUpload := func() {}
	defer func() {
		cancelUpload()
		cancelCase()
		releasePut()
		if apiDone != nil && !apiJoined {
			select {
			case <-apiDone:
				apiJoined = true
			case <-time.After(5 * time.Second):
				t.Error("late upload API handler did not join during cleanup")
			}
		}
		mu.Lock()
		started := heldStarted
		mu.Unlock()
		if started && !putJoined {
			select {
			case <-putDone:
				putJoined = true
			case <-time.After(5 * time.Second):
				t.Error("late upload origin handler did not join during cleanup")
			}
		}
		server.Close()
	}()
	request := func(method, path string, input any) *httptest.ResponseRecorder {
		encoded, err := json.Marshal(input)
		if err != nil {
			t.Fatal("late upload request encoding", err)
		}
		req := httptest.NewRequestWithContext(caseCtx, method, "http://example.com"+path, bytes.NewReader(encoded))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://example.com")
		req.Header.Set("X-CSRF-Token", auth.CSRFToken)
		req.AddCookie(cookie)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		for _, forbidden := range []string{"test-only-access", "test-only-secret", "AWS4-HMAC-SHA256", "auth_ciphertext", "secret_generation"} {
			if strings.Contains(response.Body.String(), forbidden) {
				t.Error("late upload response exposed private storage material")
			}
		}
		return response
	}
	current := decodeCatalogResponse[service.StorageView](t, request("GET", "/api/v1/admin/storage", nil), 200)
	enabled := decodeCatalogResponse[service.StorageView](t, request("PUT", "/api/v1/admin/storage", service.StorageInput{Enabled: true, Endpoint: server.URL, Region: "us-east-1", Bucket: "routex-test", Prefix: "late", ETag: current.ETag, Auth: service.StorageAuthInput{Action: "replace", AccessKey: "test-only-access", SecretKey: "test-only-secret"}}), 200)
	if !enabled.Enabled || enabled.Revision == nil || enabled.Revision.VerifiedAt == nil {
		t.Fatal("late upload configuration was not genuinely verified")
	}
	mu.Lock()
	if len(objects) != 0 {
		mu.Unlock()
		t.Fatal("late upload configuration probe did not clean its exact object")
	}
	publishedRevision, armed = enabled.Revision.ID, true
	mu.Unlock()
	var content bytes.Buffer
	writer := multipart.NewWriter(&content)
	part, err := writer.CreateFormFile("file", "late.pdf")
	if err != nil {
		t.Fatal("late upload multipart file", err)
	}
	if _, err = part.Write(payload); err != nil {
		t.Fatal("late upload multipart bytes", err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal("late upload multipart close", err)
	}
	uploadCtx, stopUpload := context.WithCancel(caseCtx)
	cancelUpload = stopUpload
	req := httptest.NewRequestWithContext(uploadCtx, "POST", "http://example.com/api/v1/attachments", &content)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Origin", "http://example.com")
	req.Header.Set("X-CSRF-Token", auth.CSRFToken)
	req.AddCookie(cookie)
	apiDone = make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		apiDone <- response
	}()
	var intent entity.StorageObject
	select {
	case intent = <-entered:
	case <-caseCtx.Done():
		t.Fatal("late upload case expired before original PUT entry")
	case <-time.After(5 * time.Second):
		t.Fatal("late upload original PUT did not enter")
	}
	cancelUpload()
	select {
	case response := <-apiDone:
		apiJoined = true
		expectStatus(t, response, 503)
		if strings.Contains(response.Body.String(), "test-only") || strings.Contains(response.Body.String(), targetVersion) {
			t.Fatal("late upload failure exposed storage material")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("late upload cancellation did not join API persistence")
	}
	var abandoned entity.StorageObject
	if err = db.WithContext(caseCtx).Take(&abandoned, "id = ?", intent.ID).Error; err != nil || abandoned.State != "delete_pending" || abandoned.UploadConfirmed || abandoned.VersionID != "" || abandoned.Name != intent.Name || abandoned.MIME != intent.MIME || abandoned.Purpose != intent.Purpose || abandoned.OwnerKind != intent.OwnerKind || abandoned.OwnerID != intent.OwnerID || abandoned.RevisionID != intent.RevisionID || abandoned.SHA256 != intent.SHA256 || abandoned.Size != intent.Size || !abandoned.CreatedAt.Equal(intent.CreatedAt) {
		t.Fatal("late upload cancellation did not independently persist exact intent", err)
	}
	firstStarted := time.Now().UTC()
	if err = svc.FlushStorageCleanup(caseCtx, 32); err != nil {
		t.Fatal("late upload absent-object cleanup", err)
	}
	firstFinished := time.Now().UTC()
	var pending entity.StorageObject
	if err = db.WithContext(caseCtx).Take(&pending, "id = ?", intent.ID).Error; err != nil || pending.State != "delete_pending" || pending.CleanupCode != "upload_uncertain" || pending.CleanupAttempts != 1 || pending.UploadConfirmed || pending.VersionID != "" || pending.LeaseToken != "" || pending.LeaseUntil != nil {
		t.Fatal("late upload HEAD404 erased uncertainty", err)
	}
	// Released storage timestamp columns share at least millisecond precision.
	// Bracket the actual persisted ten-second delay without changing any clock.
	if pending.NextCleanupAt.Before(firstStarted.Add(10*time.Second).Truncate(time.Millisecond)) || pending.NextCleanupAt.After(firstFinished.Add(10*time.Second)) {
		t.Fatal("late upload did not persist the original ten-second backoff")
	}
	mu.Lock()
	_, exists := objects[targetPath]
	beforeLate := []int{puts, gets, heads, deletes}
	mu.Unlock()
	if exists || !reflect.DeepEqual(beforeLate, []int{1, 0, 1, 0}) {
		t.Fatal("late upload absence/replay evidence changed", exists, beforeLate)
	}
	releasePut()
	select {
	case <-putDone:
		putJoined = true
	case <-caseCtx.Done():
		t.Fatal("late upload case expired joining original PUT")
	case <-time.After(5 * time.Second):
		t.Fatal("late upload original PUT did not join after release")
	}
	mu.Lock()
	late, exists := objects[targetPath]
	mu.Unlock()
	if !exists || late.owner != intent.ID || late.version != targetVersion || !bytes.Equal(late.data, payload) {
		t.Fatal("late upload original request did not actually commit its owned version")
	}
	fresh, err := service.New(caseCtx, db, service.WithCredentialStorage(store), service.WithStoragePolicy(true))
	if err != nil {
		t.Fatal("late upload Service reconstruction", err)
	}
	var restored entity.StorageObject
	if err = db.WithContext(caseCtx).Take(&restored, "id = ?", intent.ID).Error; err != nil || !reflect.DeepEqual(pending, restored) {
		t.Fatal("late upload reconstruction changed persisted uncertainty/backoff", err)
	}
	wait := time.Until(restored.NextCleanupAt)
	if wait <= 0 {
		t.Fatal("late upload backoff elapsed before its early-cleanup control")
	}
	if err = fresh.FlushStorageCleanup(caseCtx, 32); err != nil {
		t.Fatal("late upload pre-backoff cleanup", err)
	}
	var early entity.StorageObject
	if err = db.WithContext(caseCtx).Take(&early, "id = ?", intent.ID).Error; err != nil || !reflect.DeepEqual(restored, early) {
		t.Fatal("late upload cleaned before its persisted retry time", err)
	}
	mu.Lock()
	earlyCounts := []int{puts, gets, heads, deletes}
	mu.Unlock()
	if !reflect.DeepEqual(earlyCounts, beforeLate) {
		t.Fatal("late upload dispatched cleanup before durable backoff", earlyCounts)
	}
	// Wait for the actual retained timestamp under the same outer deadline.
	timer := time.NewTimer(max(time.Duration(0), time.Until(restored.NextCleanupAt)))
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-caseCtx.Done():
		t.Fatal("late upload case expired during genuine retry backoff")
	}
	if time.Now().Before(restored.NextCleanupAt) {
		t.Fatal("late upload retry timer returned before the stored timestamp")
	}
	if err = fresh.FlushStorageCleanup(caseCtx, 32); err != nil {
		t.Fatal("late upload retained-version recovery", err)
	}
	var cleaned entity.StorageObject
	if err = db.WithContext(caseCtx).Take(&cleaned, "id = ?", intent.ID).Error; err != nil || cleaned.State != "deleted" || cleaned.CleanupAttempts != 2 || cleaned.CleanupCode != "" || cleaned.LeaseToken != "" || cleaned.LeaseUntil != nil || cleaned.UploadConfirmed || cleaned.VersionID != "" || cleaned.Name != intent.Name || cleaned.MIME != intent.MIME || cleaned.Purpose != intent.Purpose || cleaned.OwnerKind != intent.OwnerKind || cleaned.OwnerID != intent.OwnerID || cleaned.RevisionID != intent.RevisionID || cleaned.SHA256 != intent.SHA256 || cleaned.Size != intent.Size || !cleaned.CreatedAt.Equal(intent.CreatedAt) {
		t.Fatal("late upload recovery changed identity or fabricated acknowledgement", err)
	}
	if err = fresh.FlushStorageCleanup(caseCtx, 32); err != nil {
		t.Fatal("late upload repeated recovery", err)
	}
	var repeated entity.StorageObject
	if err = db.WithContext(caseCtx).Take(&repeated, "id = ?", intent.ID).Error; err != nil || !reflect.DeepEqual(cleaned, repeated) {
		t.Fatal("late upload repeated cleanup changed retained history", err)
	}
	mu.Lock()
	remaining := len(objects)
	finalCounts := []int{puts, gets, heads, deletes}
	mu.Unlock()
	if remaining != 0 || !reflect.DeepEqual(finalCounts, []int{1, 0, 3, 1}) {
		t.Fatal("late upload exact cleanup/replay counts", remaining, finalCounts)
	}
	for _, original := range oldObjects {
		var current entity.StorageObject
		if err = db.WithContext(caseCtx).Take(&current, "id = ?", original.ID).Error; err != nil || !reflect.DeepEqual(original, current) {
			t.Fatal("late upload changed another retained object", err)
		}
	}
	for _, original := range oldRevisions {
		var current entity.StorageRevision
		if err = db.WithContext(caseCtx).Take(&current, "id = ?", original.ID).Error; err != nil || !reflect.DeepEqual(original, current) {
			t.Fatal("late upload changed an original descriptor", err)
		}
	}
	for _, baseline := range unchanged {
		var count int64
		if err = db.WithContext(caseCtx).Model(baseline.model).Count(&count).Error; err != nil || count != baseline.count {
			t.Fatal("late upload created native/key effects", err, count, baseline.count)
		}
	}
}
