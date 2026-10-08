package handler

import (
	"context"
	"encoding/json"
	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
	"reflect"
	"strings"
	"testing"
	"time"
)

func testRuntimeApplicationLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	store, err := secretstore.New([]byte(strings.Repeat("a", 32)))
	if err != nil {
		t.Fatal(err)
	}
	metadata := func(name string) service.SystemInstanceMetadata {
		return service.SystemInstanceMetadata{Name: name, Hostname: name + ".invalid", Version: "test", GoVersion: "go-test", OS: "test", Arch: "test", StoragePath: t.TempDir()}
	}
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store))
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"application-admin@example.invalid","password":"application-evidence-password","name":"Evidence admin"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, cookie := readIdentity(t, setup)
	_, readerCookie, readerCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "routing-reader", []string{"system.read"})
	_, writerCookie, writerCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "routing-writer", []string{"system.write"})
	if err := svc.StartSystemInstance(ctx, metadata("first")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		svc.StopRuntime()
		stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := svc.StopSystemInstance(stop); err != nil {
			t.Error(err)
		}
	})
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/admin/runtime/applications"
	req := systemStatusRequest(t, router, cookie, admin.CSRFToken)
	first := decodeCatalogResponse[service.RuntimeApplicationPage](t, req("GET", path, nil), 200)
	if first.Scope != "routing_only" || len(first.Items) != 1 || first.Items[0].CurrentServingSnapshotMatches == nil || !*first.Items[0].CurrentServingSnapshotMatches || first.Items[0].InstanceStatus != "online" || first.NextCursor != nil {
		t.Fatal("first exact process application absent")
	}
	response := req("GET", path, nil)
	if response.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("private application response cacheable")
	}
	// Public shape cannot accidentally return route digests, process tokens or
	// authentication material. Recorded facts and current observation are separate.
	var public map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &public); err != nil {
		t.Fatal(err)
	}
	if len(public) != 4 || len(public["items"].([]any)[0].(map[string]any)) != 8 {
		t.Fatal("unexpected public evidence fields")
	}
	var original []entity.RuntimeRoutingApplication
	if err := db.Order("id").Find(&original).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := svc.RefreshRuntime(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var after []entity.RuntimeRoutingApplication
	if err := db.Order("id").Find(&after).Error; err != nil || !reflect.DeepEqual(original, after) {
		t.Fatal("renewal rewrote first observation", err)
	}
	expectStatus(t, identityRequest(router, "GET", path, "", nil, ""), 401)
	for _, query := range []string{"?unknown=1", "?limit=", "?limit=0", "?limit=101", "?limit=1&limit=2", "?instance_id=" + strings.ToUpper(first.Items[0].InstanceID), "?instance_id=" + first.Items[0].InstanceID + "%20", "?cursor=invalid"} {
		expectStatus(t, req("GET", path+query, nil), 400)
	}

	reader := systemStatusRequest(t, router, readerCookie, readerCSRF)
	writer := systemStatusRequest(t, router, writerCookie, writerCSRF)
	expectStatus(t, reader("GET", path, nil), 200)
	expectStatus(t, writer("GET", path, nil), 403)
	svc.StopRuntime()
	if err := svc.StopSystemInstance(ctx); err != nil {
		t.Fatal(err)
	}
	restart, err := service.New(ctx, db, service.WithCredentialStorage(store))
	if err != nil {
		t.Fatal(err)
	}
	if err := restart.StartSystemInstance(ctx, metadata("restart")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		restart.StopRuntime()
		stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := restart.StopSystemInstance(stop); err != nil {
			t.Error(err)
		}
	})
	if err := restart.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	if restart.CurrentSystemInstanceID() == first.Items[0].InstanceID {
		t.Fatal("restart reused birth")
	}
	restarted, err := restart.ListRuntimeApplications(ctx, admin.User.ID, service.RuntimeApplicationFilter{})
	if err != nil || len(restarted.Items) != 2 {
		t.Fatal("restart history absent", err)
	}
	oldSeen, newSeen := false, false
	for _, row := range restarted.Items {
		if row.InstanceID == first.Items[0].InstanceID {
			oldSeen = row.InstanceStatus == "offline" && row.CurrentServingSnapshotMatches == nil && row.AppliedAt.Equal(first.Items[0].AppliedAt)
		} else if row.InstanceID == restart.CurrentSystemInstanceID() {
			newSeen = row.InstanceStatus == "online" && row.CurrentServingSnapshotMatches != nil && *row.CurrentServingSnapshotMatches
		}
	}
	if !oldSeen || !newSeen {
		t.Fatal("restart borrowed old current proof")
	}
	// The current database snapshot contains both retained processes. A cursor
	// remains scoped to the exact reader/filter and never expands authority.
	page, err := restart.ListRuntimeApplications(ctx, admin.User.ID, service.RuntimeApplicationFilter{Limit: 1})
	if err != nil || page.NextCursor == nil {
		t.Fatal("bounded first page", err)
	}
	tail, err := restart.ListRuntimeApplications(ctx, admin.User.ID, service.RuntimeApplicationFilter{Limit: 1, Cursor: *page.NextCursor})
	if err != nil || len(tail.Items) != 1 || tail.Items[0].ID == page.Items[0].ID {
		t.Fatal("bounded cursor", err)
	}
	if _, err := restart.ListRuntimeApplications(ctx, admin.User.ID, service.RuntimeApplicationFilter{InstanceID: first.Items[0].InstanceID, Cursor: *page.NextCursor}); err == nil {
		t.Fatal("filter borrowed cursor")
	}
	if err := db.Order("id").Find(&after).Error; err != nil || len(after) != 2 {
		t.Fatal("reads changed evidence", err)
	}
	for _, row := range after {
		if row.ID == original[0].ID && !reflect.DeepEqual(row, original[0]) {
			t.Fatal("history changed after restart")
		}
	}
}
