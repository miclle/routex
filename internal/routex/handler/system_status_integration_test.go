package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/id"
	"gorm.io/gorm"
)

func testSystemStatusLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	metadata := func(name string) service.SystemInstanceMetadata {
		return service.SystemInstanceMetadata{
			Name: name, Hostname: name + ".example.invalid", Version: "test", Commit: "abc123",
			BuildTime: "2026-09-29T00:00:00Z", GoVersion: "go-test", OS: "test", Arch: "test",
			StoragePath: t.TempDir(),
		}
	}
	first, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.StartSystemInstance(ctx, metadata("first-process")); err != nil {
		t.Fatal(err)
	}
	firstID := first.CurrentSystemInstanceID()
	if err := first.StopSystemInstance(ctx); err != nil {
		t.Fatal(err)
	}

	svc, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.StartSystemInstance(ctx, metadata("serving-process")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stop, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := svc.StopSystemInstance(stop); err != nil {
			t.Error(err)
		}
	})
	currentID := svc.CurrentSystemInstanceID()
	if currentID == "" || currentID == firstID {
		t.Fatal("process restart reused an instance generation")
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"system-admin@example.invalid","password":"test-only-system-password","name":"System Admin"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	request := systemStatusRequest(t, router, adminCookie, admin.CSRFToken)

	expectStatus(t, identityRequest(router, "GET", "/api/v1/admin/system/instances", "", nil, ""), 401)
	expectStatus(t, request("GET", "/api/v1/admin/system/instances?unexpected=1", nil), 400)
	instances := decodeCatalogResponse[service.SystemInstancePage](t, request("GET", "/api/v1/admin/system/instances", nil), 200)
	if len(instances.Items) != 2 || instances.ObservedAt.IsZero() || instances.HeartbeatIntervalSeconds <= 0 || instances.LeaseDurationSeconds <= instances.HeartbeatIntervalSeconds || instances.CleanupAfterSeconds <= instances.LeaseDurationSeconds {
		t.Fatal("instance page omitted authoritative freshness metadata")
	}
	seenOnline, seenStopped := false, false
	for _, item := range instances.Items {
		switch item.ID {
		case currentID:
			seenOnline = item.Status == "online" && item.Role == "combined" && item.HeartbeatRevision > 0
		case firstID:
			seenStopped = item.Status == "offline" && item.StoppedAt != nil && !item.CleanupEligible
		}
	}
	if !seenOnline || !seenStopped {
		t.Fatal("process lifecycle was not represented truthfully")
	}
	emptyJobs := decodeCatalogResponse[service.SystemJobPage](t, request("GET", "/api/v1/admin/system/jobs", nil), 200)
	if len(emptyJobs.Items) != 0 {
		t.Fatal("new installation contained demonstration jobs")
	}
	// Terminal retention must never evict an older run that is still active.
	retentionNow := time.Now().UTC()
	retainedRunning := entity.SystemJob{
		ID: "job_keep_running", Code: service.SystemJobStorageCleanup, Status: "running", ExecutorID: currentID,
		ItemsTotal: 1, StartedAt: retentionNow.Add(-time.Hour), UpdatedAt: retentionNow.Add(-time.Hour),
	}
	if err := db.Create(&retainedRunning).Error; err != nil {
		t.Fatal(err)
	}
	terminalFixtures := make([]entity.SystemJob, 300)
	progress := 100
	for i := range terminalFixtures {
		terminalFixtures[i] = entity.SystemJob{
			ID: fmt.Sprintf("job_term_%020d", i), Code: service.SystemJobRuntimePublication,
			Status: "completed", ExecutorID: currentID, Progress: &progress, ItemsTotal: 1, ItemsCompleted: 1,
			DetailCode: "published", StartedAt: retentionNow, UpdatedAt: retentionNow, CompletedAt: &retentionNow,
		}
	}
	if err := db.CreateInBatches(terminalFixtures, 100).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	svc.StopRuntime()
	var retained entity.SystemJob
	if err := db.First(&retained, "id = ?", retainedRunning.ID).Error; err != nil || retained.Status != "running" {
		t.Fatal("terminal pruning removed an active system job")
	}
	var terminalCount int64
	if err := db.Model(&entity.SystemJob{}).Where("status IN ?", []string{"completed", "failed"}).Count(&terminalCount).Error; err != nil || terminalCount != 256 {
		t.Fatalf("terminal job retention was not bounded after finish: count=%d err=%v", terminalCount, err)
	}
	if err := db.Where("id = ? OR id LIKE ?", retainedRunning.ID, "job_term_%").Delete(&entity.SystemJob{}).Error; err != nil {
		t.Fatal(err)
	}
	jobs := decodeCatalogResponse[service.SystemJobPage](t, request("GET", "/api/v1/admin/system/jobs", nil), 200)
	if len(jobs.Items) != 1 || jobs.Items[0].Code != service.SystemJobRuntimePublication || jobs.Items[0].Status != "completed" || jobs.Items[0].ExecutorID != currentID || jobs.Items[0].DetailCode != "published" {
		t.Fatalf("actual runtime publication was not reported: %+v", jobs.Items)
	}

	reader, readerCookie, readerCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "reader", []string{"system.read"})
	readerRequest := systemStatusRequest(t, router, readerCookie, readerCSRF)
	expectStatus(t, readerRequest("GET", "/api/v1/admin/system/instances", nil), 200)
	expectStatus(t, readerRequest("GET", "/api/v1/admin/system/jobs", nil), 200)
	expectStatus(t, readerRequest("POST", "/api/v1/admin/system/instances/cleanup", map[string]any{"instances": []any{}}), 403)
	writer, writerCookie, writerCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "writer", []string{"system.write"})
	writerRequest := systemStatusRequest(t, router, writerCookie, writerCSRF)
	expectStatus(t, writerRequest("GET", "/api/v1/admin/system/instances", nil), 403)
	expectStatus(t, writerRequest("GET", "/api/v1/admin/system/jobs", nil), 403)
	_ = reader

	oldA := insertOfflineSystemInstance(t, db, "cleanup-a", 7)
	oldB := insertOfflineSystemInstance(t, db, "cleanup-b", 11)
	var current entity.SystemInstance
	if err := db.First(&current, "id = ?", currentID).Error; err != nil {
		t.Fatal(err)
	}
	cleanup := func(client func(string, string, any) *httptest.ResponseRecorder, targets ...service.SystemInstanceCleanupTarget) *httptest.ResponseRecorder {
		return client("POST", "/api/v1/admin/system/instances/cleanup", map[string]any{"instances": targets})
	}
	expectStatus(t, request("POST", "/api/v1/admin/system/instances/cleanup", map[string]any{"instances": []any{}, "unknown": true}), 400)
	expectStatus(t, identityRequest(router, "POST", "/api/v1/admin/system/instances/cleanup", `{"instances":[{"id":"`+oldA.ID+`","revision":7}]}`, adminCookie, ""), 403)
	expectStatus(t, cleanup(request, service.SystemInstanceCleanupTarget{ID: oldA.ID, Revision: oldA.HeartbeatRevision}, service.SystemInstanceCleanupTarget{ID: currentID, Revision: current.HeartbeatRevision}), 409)
	if err := db.First(&oldA, "id = ?", oldA.ID).Error; err != nil || oldA.RetiredAt != nil {
		t.Fatal("mixed live/offline cleanup partially committed")
	}
	expectStatus(t, cleanup(request, service.SystemInstanceCleanupTarget{ID: oldA.ID, Revision: oldA.HeartbeatRevision + 1}), 409)
	if err := db.Model(&entity.SystemInstance{}).Where("id = ?", currentID).Updates(map[string]any{
		"last_heartbeat_at": time.Now().UTC().Add(-time.Hour), "lease_expires_at": time.Now().UTC().Add(-time.Minute),
	}).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, cleanup(request, service.SystemInstanceCleanupTarget{ID: currentID, Revision: current.HeartbeatRevision}), 409)
	if err := db.Model(&entity.SystemInstance{}).Where("id = ?", currentID).Updates(map[string]any{
		"last_heartbeat_at": time.Now().UTC(), "lease_expires_at": time.Now().UTC().Add(time.Minute),
	}).Error; err != nil {
		t.Fatal(err)
	}
	cleanedA := decodeCatalogResponse[service.SystemInstanceCleanupResult](t, cleanup(request, service.SystemInstanceCleanupTarget{ID: oldA.ID, Revision: oldA.HeartbeatRevision}), 200)
	if cleanedA.CleanedCount != 1 || len(cleanedA.CleanedIDs) != 1 || cleanedA.CleanedIDs[0] != oldA.ID {
		t.Fatal("offline cleanup returned an incorrect reviewed set")
	}
	cleanedB := decodeCatalogResponse[service.SystemInstanceCleanupResult](t, cleanup(writerRequest, service.SystemInstanceCleanupTarget{ID: oldB.ID, Revision: oldB.HeartbeatRevision}), 200)
	if cleanedB.CleanedCount != 1 || cleanedB.CleanedIDs[0] != oldB.ID {
		t.Fatal("system.write did not independently authorize cleanup")
	}
	oldC := insertOfflineSystemInstance(t, db, "cleanup-c", 13)
	oldD := insertOfflineSystemInstance(t, db, "cleanup-d", 17)
	cleanedBatch := decodeCatalogResponse[service.SystemInstanceCleanupResult](t, cleanup(request,
		service.SystemInstanceCleanupTarget{ID: oldC.ID, Revision: oldC.HeartbeatRevision},
		service.SystemInstanceCleanupTarget{ID: oldD.ID, Revision: oldD.HeartbeatRevision},
	), 200)
	if cleanedBatch.CleanedCount != 2 || len(cleanedBatch.CleanedIDs) != 2 {
		t.Fatal("multi-instance cleanup returned an incorrect reviewed set")
	}
	var cleanupAudits []entity.AuditEvent
	if err := db.Where("action = ?", "system.instance.cleanup").Order("created_at, id").Find(&cleanupAudits).Error; err != nil || len(cleanupAudits) != 4 {
		t.Fatalf("successful cleanup did not produce one event per target: count=%d err=%v", len(cleanupAudits), err)
	}
	wantAuditRevision := map[string]uint64{oldA.ID: 7, oldB.ID: 11, oldC.ID: 13, oldD.ID: 17}
	for _, audit := range cleanupAudits {
		want, ok := wantAuditRevision[audit.ResourceID]
		if !ok || audit.DetailsJSON == nil || !strings.Contains(*audit.DetailsJSON, fmt.Sprintf(`"revision":%d`, want)) || strings.Contains(*audit.DetailsJSON, "hostname") {
			t.Fatalf("cleanup audit did not identify its retired target: %+v", audit)
		}
		delete(wantAuditRevision, audit.ResourceID)
	}
	if len(wantAuditRevision) != 0 {
		t.Fatalf("cleanup audit omitted targets: %v", wantAuditRevision)
	}

	// Race cleanup against the exact token-checked heartbeat path. If cleanup
	// wins its row lock, the immediately following valid heartbeat recovers the
	// registration; if heartbeat wins, cleanup observes the revision/lease change
	// and rejects the whole request.
	worker, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.StartSystemInstance(ctx, metadata("racing-process")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stop, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := worker.StopSystemInstance(stop); err != nil {
			t.Error(err)
		}
	})
	workerID := worker.CurrentSystemInstanceID()
	if err := db.Model(&entity.SystemInstance{}).Where("id = ?", workerID).Updates(map[string]any{
		"last_heartbeat_at": time.Now().UTC().Add(-time.Hour), "lease_expires_at": time.Now().UTC().Add(-time.Minute),
	}).Error; err != nil {
		t.Fatal(err)
	}
	var reviewed entity.SystemInstance
	if err := db.First(&reviewed, "id = ?", workerID).Error; err != nil {
		t.Fatal(err)
	}
	raceBody, err := json.Marshal(SystemInstanceCleanupRequest{Instances: []service.SystemInstanceCleanupTarget{{ID: workerID, Revision: reviewed.HeartbeatRevision}}})
	if err != nil {
		t.Fatal(err)
	}
	startRace := make(chan struct{})
	var raceWG sync.WaitGroup
	var cleanupResponse *httptest.ResponseRecorder
	var heartbeatErr error
	raceWG.Add(2)
	go func() {
		defer raceWG.Done()
		<-startRace
		cleanupResponse = identityRequest(router, "POST", "/api/v1/admin/system/instances/cleanup", string(raceBody), adminCookie, admin.CSRFToken)
	}()
	go func() {
		defer raceWG.Done()
		<-startRace
		heartbeatErr = worker.RefreshSystemInstance(ctx)
	}()
	close(startRace)
	raceWG.Wait()
	if heartbeatErr != nil || (cleanupResponse.Code != 200 && cleanupResponse.Code != 409) {
		t.Fatalf("cleanup/heartbeat race failed: heartbeat=%v cleanup=%d %s", heartbeatErr, cleanupResponse.Code, cleanupResponse.Body.String())
	}
	var recovered entity.SystemInstance
	if err := db.First(&recovered, "id = ?", workerID).Error; err != nil {
		t.Fatal(err)
	}
	if recovered.RetiredAt != nil || recovered.HeartbeatRevision <= reviewed.HeartbeatRevision || !recovered.LeaseExpiresAt.After(time.Now().UTC()) {
		t.Fatal("valid heartbeat did not prevent or recover concurrent cleanup")
	}

	if err := svc.RefreshSystemInstance(ctx); err != nil {
		t.Fatal(err)
	}
	reconcileNow := time.Now().UTC()
	liveJobs := make([]entity.SystemJob, systemStatusLiveJobCount)
	for i := range liveJobs {
		liveJobs[i] = entity.SystemJob{
			ID: fmt.Sprintf("job_live_%020d", i), Code: service.SystemJobStorageCleanup,
			Status: "running", ExecutorID: currentID, ItemsTotal: 1,
			StartedAt: reconcileNow, UpdatedAt: reconcileNow,
		}
	}
	if err := db.CreateInBatches(liveJobs, 100).Error; err != nil {
		t.Fatal(err)
	}
	orphanID := "job_zzzzzzzzzzzzzzzzzzzz"
	orphan := entity.SystemJob{ID: orphanID, Code: service.SystemJobCallRecordDelivery, Status: "running", ExecutorID: "ins_01m36yee4gkbns18pfcqqc75a3", ItemsTotal: 4, ItemsCompleted: 1, StartedAt: reconcileNow.Add(-time.Hour), UpdatedAt: reconcileNow.Add(-time.Hour)}
	if err := db.Create(&orphan).Error; err != nil {
		t.Fatal(err)
	}
	oldTerminal := make([]entity.SystemJob, 300)
	for i := range oldTerminal {
		oldTerminal[i] = entity.SystemJob{
			ID: fmt.Sprintf("job_done_%020d", i), Code: service.SystemJobRuntimePublication,
			Status: "completed", ExecutorID: currentID, Progress: &progress, ItemsTotal: 1, ItemsCompleted: 1,
			DetailCode: "published", StartedAt: reconcileNow.Add(-time.Hour), UpdatedAt: reconcileNow.Add(-time.Hour), CompletedAt: &reconcileNow,
		}
	}
	if err := db.CreateInBatches(oldTerminal, 100).Error; err != nil {
		t.Fatal(err)
	}
	jobs = decodeCatalogResponse[service.SystemJobPage](t, request("GET", "/api/v1/admin/system/jobs", nil), 200)
	var reconciled entity.SystemJob
	if err := db.First(&reconciled, "id = ?", orphanID).Error; err != nil || reconciled.Status != "failed" || reconciled.DetailCode != "executor_lost" || reconciled.CompletedAt == nil || reconciled.Progress == nil || *reconciled.Progress != 25 {
		t.Fatal("expired or missing executor was still presented as running")
	}
	var liveCount int64
	if err := db.Model(&entity.SystemJob{}).Where("id LIKE ? AND status = ?", "job_live_%", "running").Count(&liveCount).Error; err != nil || liveCount != systemStatusLiveJobCount {
		t.Fatalf("online jobs were lost during complete reconciliation: count=%d err=%v", liveCount, err)
	}
	if err := db.Model(&entity.SystemJob{}).Where("status IN ?", []string{"completed", "failed"}).Count(&terminalCount).Error; err != nil || terminalCount != 256 {
		t.Fatalf("reconciliation did not prune terminal history: count=%d err=%v", terminalCount, err)
	}
	if len(jobs.Items) != 100 {
		t.Fatalf("system jobs API was not bounded: %d", len(jobs.Items))
	}
	_ = writer
}

const systemStatusLiveJobCount = 257

func systemStatusRequest(t *testing.T, router *fox.Engine, cookie *http.Cookie, csrf string) func(string, string, any) *httptest.ResponseRecorder {
	t.Helper()
	return func(method, path string, body any) *httptest.ResponseRecorder {
		encoded := ""
		if body != nil {
			value, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			encoded = string(value)
		}
		return identityRequest(router, method, path, encoded, cookie, csrf)
	}
}

func createSystemStatusMember(t *testing.T, svc *service.Service, router *fox.Engine, adminID, suffix string, permissions []string) (*service.MemberRecord, *http.Cookie, string) {
	t.Helper()
	ctx := context.Background()
	member, err := svc.CreateMember(ctx, adminID, "system-"+suffix+"@example.invalid", "test-only-system-password", "System "+suffix, "member")
	if err != nil {
		t.Fatal(err)
	}
	role, err := svc.SaveRole(ctx, adminID, "", "System "+suffix, permissions)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetMemberRoles(ctx, adminID, member.User.ID, []string{role.Role.ID}); err != nil {
		t.Fatal(err)
	}
	login := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"system-`+suffix+`@example.invalid","password":"test-only-system-password"}`, nil, "")
	expectStatus(t, login, 200)
	auth, cookie := readIdentity(t, login)
	return member, cookie, auth.CSRFToken
}

func insertOfflineSystemInstance(t *testing.T, db *gorm.DB, name string, revision uint64) entity.SystemInstance {
	t.Helper()
	instanceID, err := id.NewPrefixed("ins")
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-10 * time.Minute)
	row := entity.SystemInstance{
		ID: instanceID, LeaseToken: "lck_fixture", HeartbeatRevision: revision,
		Name: name, Hostname: name + ".example.invalid", Role: "combined", Version: "test",
		GoVersion: "go-test", OS: "test", Arch: "test", StartedAt: old.Add(-time.Hour),
		LastHeartbeatAt: old, LeaseExpiresAt: old.Add(time.Second),
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	return row
}
