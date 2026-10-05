package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

// Root registers and executes this fixture on real drivers. Source checks keep
// both integration DSNs empty; there is no schema or runtime test bypass.
func testMemberMetadataLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	store, err := secretstore.New([]byte(strings.Repeat("m", 32)))
	if err != nil {
		t.Fatal("metadata fixture cipher setup", err)
	}
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store))
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"metadata-admin@example.invalid","password":"metadata-test-password","name":"Metadata administrator"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	subject, _, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "metadata-subject", nil)
	_, readerCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "metadata-reader", []string{"members.read"})
	writer, writerCookie, writerCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "metadata-writer", []string{"members.write"})
	_, foreignCookie, foreignCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "metadata-foreign", nil)
	path := func(userID string) string { return "/api/v1/admin/members/" + userID + "/metadata" }
	get := func(cookie *http.Cookie, userID string) service.MemberMetadataRecord {
		t.Helper()
		res := identityRequest(router, "GET", path(userID), "", cookie, "")
		expectStatus(t, res, 200)
		var row service.MemberMetadataRecord
		if json.Unmarshal(res.Body.Bytes(), &row) != nil || row.UserID != userID || len(row.ETag) != 64 || res.Header().Get("ETag") != strconv.Quote(row.ETag) || res.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("invalid current projection", res.Body.String(), res.Header())
		}
		var fields map[string]any
		_ = json.Unmarshal(res.Body.Bytes(), &fields)
		if len(fields) != 5 {
			t.Fatal("private projection expanded", fields)
		}
		return row
	}
	put := func(cookie *http.Cookie, csrf, userID, etag, name string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(service.MemberMetadataInput{Name: name, Reason: "Reviewed name change"})
		req := httptest.NewRequest("PUT", "http://routex.test"+path(userID), strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("If-Match", strconv.Quote(etag))
		req.Header.Set("X-CSRF-Token", csrf)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		return out
	}
	audits := func() int64 {
		t.Helper()
		var count int64
		if err := db.Model(&entity.AuditEvent{}).Where("action = ? AND resource_id = ?", "member.metadata.update", subject.User.ID).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		return count
	}
	user := func(userID string) entity.User {
		t.Helper()
		var row entity.User
		if err := db.Where("id = ?", userID).First(&row).Error; err != nil {
			t.Fatal(err)
		}
		return row
	}
	snapshots := func() map[string][]map[string]any {
		t.Helper()
		result := map[string][]map[string]any{}
		for _, table := range []string{"sessions", "api_keys", "api_key_models", "user_model_grants", "user_roles", "role_permissions", "resource_limits", "providers", "provider_connections", "provider_credentials", "provider_models", "models", "model_names", "model_provider_bindings"} {
			var rows []map[string]any
			if err := db.Table(table).Find(&rows).Error; err != nil {
				t.Fatal(err)
			}
			slices.SortFunc(rows, func(a, b map[string]any) int {
				left, _ := json.Marshal(a)
				right, _ := json.Marshal(b)
				return strings.Compare(string(left), string(right))
			})
			result[table] = rows
		}
		return result
	}
	first := get(readerCookie, subject.User.ID)
	if first.CanEdit {
		t.Fatal("read-only can edit")
	}
	writeReview := get(writerCookie, subject.User.ID)
	if !writeReview.CanEdit {
		t.Fatal("write-only GET denied")
	}
	if get(writerCookie, writer.User.ID).CanEdit || get(writerCookie, admin.User.ID).CanEdit {
		t.Fatal("protected delegated targets editable")
	}
	expectStatus(t, put(writerCookie, writerCSRF, writer.User.ID, writeReview.ETag, writer.User.Name), 403)
	expectStatus(t, put(writerCookie, writerCSRF, admin.User.ID, writeReview.ETag, admin.User.Name), 403)
	expectStatus(t, identityRequest(router, "GET", path(subject.User.ID), "", foreignCookie, ""), 403)
	expectStatus(t, put(foreignCookie, foreignCSRF, subject.User.ID, first.ETag, "Denied"), 403)
	expectStatus(t, identityRequest(router, "GET", path(strings.ToUpper(subject.User.ID)), "", readerCookie, ""), 404)
	expectStatus(t, identityRequest(router, "GET", path(subject.User.ID)+"?email=true", "", readerCookie, ""), 400)
	if _, err := svc.GetMemberMetadata(ctx, strings.ToUpper(admin.User.ID), subject.User.ID); err == nil {
		t.Fatal("actor alias borrowed authority")
	}
	// Permission case aliases never supply current write authority, even if the
	// original Session remains valid. This is a controlled collation seed.
	var writerRoles []entity.UserRole
	if err := db.Where("user_id = ?", writer.User.ID).Find(&writerRoles).Error; err != nil || len(writerRoles) != 1 {
		t.Fatal(writerRoles, err)
	}
	permission := entity.RolePermission{RoleID: writerRoles[0].RoleID, Permission: "members.write"}
	if err := db.Model(&entity.RolePermission{}).Where("role_id = ? AND permission = ?", permission.RoleID, permission.Permission).UpdateColumn("permission", "MEMBERS.WRITE").Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, put(writerCookie, writerCSRF, subject.User.ID, writeReview.ETag, "Must not borrow permission"), 403)
	if err := db.Model(&entity.RolePermission{}).Where("role_id = ? AND permission = ?", permission.RoleID, "MEMBERS.WRITE").UpdateColumn("permission", permission.Permission).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, put(writerCookie, "invalid-csrf", subject.User.ID, writeReview.ETag, "CSRF denied"), 403)
	expectStatus(t, put(nil, "", subject.User.ID, writeReview.ETag, "Anonymous denied"), 401)
	// This legitimate pending Key and current Session/role/default rows are not
	// touched by a display-name edit; no secret is returned by this endpoint.
	// Ordinary product configuration and an explicit Personal grant make the
	// retained pending Key legal. The unverified credential and zero-weight
	// binding are never enabled or dispatched; no native readiness is claimed.
	provider, err := svc.CreateProvider(ctx, admin.User.ID, "Metadata preservation provider", service.CreateConnectionInput{Name: "Metadata preservation connection", BaseURL: "https://metadata-setup.example.invalid/v1", Protocol: entity.ProtocolOpenAIChat, CredentialName: "Metadata preservation credential", Secret: "test-only-metadata-setup-secret"})
	if err != nil {
		t.Fatal("metadata retained Key Provider setup", err)
	}
	pm, err := svc.CreateProviderModel(ctx, admin.User.ID, provider.Connections[0].Connection.ID, "metadata-preserved-upstream")
	if err != nil {
		t.Fatal("metadata retained Key Provider Model setup", err)
	}
	model, err := svc.CreateModel(ctx, admin.User.ID, "metadata-preserved-model", pm.ID)
	if err != nil {
		t.Fatal("metadata retained Key Model setup", err)
	}
	if _, err := svc.SetModelGrants(ctx, admin.User.ID, model.Model.ID, []string{subject.User.ID}); err != nil {
		t.Fatal("metadata retained Key explicit grant setup", err)
	}
	if _, err := svc.CreatePersonalKey(ctx, subject.User.ID, "Retained metadata fixture Key", []string{model.Model.ID}, nil); err != nil {
		t.Fatal("metadata retained pending Key setup", err)
	}

	beforeUser, beforeRows := user(subject.User.ID), snapshots()
	changed := put(writerCookie, writerCSRF, subject.User.ID, writeReview.ETag, "Updated target")
	expectStatus(t, changed, 200)
	var confirmed service.MemberMetadataWriteResult
	if json.Unmarshal(changed.Body.Bytes(), &confirmed) != nil || confirmed.Name != "Updated target" || confirmed.Confirmation != "current_member_name" || !confirmed.CanEdit {
		t.Fatal(changed.Body.String())
	}
	afterUser := user(subject.User.ID)
	afterUser.Name, afterUser.UpdatedAt = beforeUser.Name, beforeUser.UpdatedAt
	if !reflect.DeepEqual(afterUser, beforeUser) || !reflect.DeepEqual(snapshots(), beforeRows) || audits() != 1 {
		t.Fatal("name write mutated unrelated identity/resource facts")
	}
	events, err := svc.ListAudit(ctx, admin.User.ID, service.AuditFilter{Category: "identity", Range: "7d"})
	if err != nil {
		t.Fatal(err)
	}
	foundAudit := false
	for _, event := range events.Items {
		if event.Action == "member.metadata.update" && event.ResourceID == subject.User.ID {
			var values map[string]string
			if event.ActorID != writer.User.ID || event.ResourceType != "user" || json.Unmarshal(event.Changes, &values) != nil || len(values) != 4 || values["user_id"] != subject.User.ID || values["before_name"] != beforeUser.Name || values["after_name"] != "Updated target" || values["reason"] != "Reviewed name change" {
				t.Fatal("typed audit mismatch", event)
			}
			foundAudit = true
		}
	}
	if !foundAudit {
		t.Fatal("committed typed audit not visible to independent reader")
	}
	savedUser := user(subject.User.ID)
	expectStatus(t, put(writerCookie, writerCSRF, subject.User.ID, writeReview.ETag, "Updated target"), 200)
	if !reflect.DeepEqual(user(subject.User.ID), savedUser) || audits() != 1 {
		t.Fatal("current-state retry wrote or audited")
	}
	newer := get(writerCookie, subject.User.ID)
	expectStatus(t, put(writerCookie, writerCSRF, subject.User.ID, newer.ETag, "Later target"), 200)
	expectStatus(t, put(writerCookie, writerCSRF, subject.User.ID, writeReview.ETag, "Updated target"), 409)
	if user(subject.User.ID).Name != "Later target" || audits() != 2 {
		t.Fatal("stale desired name restored later value")
	}
	// Audit failure is atomic and leaves persisted timestamp unchanged.
	baseline := user(subject.User.ID)
	review := get(writerCookie, subject.User.ID)
	callback := "test:metadata-audit-rollback"
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if row, ok := tx.Statement.Dest.(*entity.AuditEvent); ok && row.Action == "member.metadata.update" {
			_ = tx.AddError(errors.New("controlled audit failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, put(writerCookie, writerCSRF, subject.User.ID, review.ETag, "Must roll back"), 500)
	if err := db.Callback().Create().Remove(callback); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(user(subject.User.ID), baseline) || audits() != 2 {
		t.Fatal("audit rollback changed target")
	}
	// A committed write whose fresh confirmation is unavailable retains a real
	// name/audit, and its original retry only confirms current state.
	var armed atomic.Bool
	createCallback, queryCallback := "test:metadata-arm-confirmation", "test:metadata-confirmation-outage"
	if err := db.Callback().Create().After("gorm:create").Register(createCallback, func(tx *gorm.DB) {
		if row, ok := tx.Statement.Dest.(*entity.AuditEvent); ok && row.Action == "member.metadata.update" && tx.Error == nil {
			armed.Store(true)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Query().Before("gorm:query").Register(queryCallback, func(tx *gorm.DB) {
		if tx.Statement.Table == "users" && armed.Load() {
			_ = tx.AddError(errors.New("controlled current confirmation outage"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, put(writerCookie, writerCSRF, subject.User.ID, review.ETag, "Committed uncertain"), 503)
	if err := db.Callback().Create().Remove(createCallback); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Query().Remove(queryCallback); err != nil {
		t.Fatal(err)
	}
	if user(subject.User.ID).Name != "Committed uncertain" || audits() != 3 {
		t.Fatal("outage lacked actual durable change")
	}
	expectStatus(t, put(writerCookie, writerCSRF, subject.User.ID, review.ETag, "Committed uncertain"), 200)
	if audits() != 3 {
		t.Fatal("uncertain retry duplicated audit")
	}
	// Exactly one differing write wins a reviewed state; no network/native retry.
	concurrentReview := get(writerCookie, subject.User.ID)
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for _, name := range []string{"Concurrent A", "Concurrent B"} {
		wg.Go(func() { codes <- put(writerCookie, writerCSRF, subject.User.ID, concurrentReview.ETag, name).Code })
	}
	wg.Wait()
	close(codes)
	counts := map[int]int{}
	for code := range codes {
		counts[code]++
	}
	if counts[200] != 1 || counts[409] != 1 || audits() != 4 {
		t.Fatal("reviewed writes were not serialized", counts)
	}
	profileBefore := get(writerCookie, subject.User.ID)
	if _, err := svc.UpdateProfile(ctx, subject.User.ID, "Self profile newer"); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, put(writerCookie, writerCSRF, subject.User.ID, profileBefore.ETag, profileBefore.Name), 409)
	if user(subject.User.ID).Name != "Self profile newer" || audits() != 4 {
		t.Fatal("administrative stale retry restored self profile")
	}
	disabled := true
	if _, err := svc.UpdateMember(ctx, admin.User.ID, subject.User.ID, &disabled, nil); err != nil {
		t.Fatal(err)
	}
	disabledBefore := user(subject.User.ID)
	disabledRows := snapshots()
	disabledReview := get(writerCookie, subject.User.ID)
	if disabledReview.Status != "disabled" || !disabledReview.CanEdit {
		t.Fatal(disabledReview)
	}
	expectStatus(t, put(writerCookie, writerCSRF, subject.User.ID, disabledReview.ETag, "Disabled label"), 200)
	disabledAfter := user(subject.User.ID)
	disabledAfter.Name, disabledAfter.UpdatedAt = disabledBefore.Name, disabledBefore.UpdatedAt
	if !reflect.DeepEqual(disabledBefore, disabledAfter) || !reflect.DeepEqual(disabledRows, snapshots()) {
		t.Fatal("disabled name write reactivated or revoked identity")
	}
	// Controlled retained-history seed is terminal, not a fabricated runtime proof.
	terminal := time.Now().UTC().Truncate(time.Millisecond)
	if err := db.Model(&entity.User{}).Where("id = ?", subject.User.ID).UpdateColumn("offboarded_at", terminal).Error; err != nil {
		t.Fatal(err)
	}
	offboarded := get(writerCookie, subject.User.ID)
	if offboarded.Status != "offboarded" || offboarded.CanEdit {
		t.Fatal(offboarded)
	}
	expectStatus(t, put(writerCookie, writerCSRF, subject.User.ID, offboarded.ETag, offboarded.Name), 409)
	// The older self-profile validator permitted controls. Read escaped retained
	// text and allow a stricter new label repair without fabricating history.
	if err := db.Model(&entity.User{}).Where("id = ?", writer.User.ID).UpdateColumn("name", "Legacy\nwriter").Error; err != nil {
		t.Fatal(err)
	}
	legacy := get(adminCookie, writer.User.ID)
	if legacy.Name != "Legacy\nwriter" {
		t.Fatal("legacy name became invented label")
	}
	expectStatus(t, put(adminCookie, admin.CSRFToken, writer.User.ID, legacy.ETag, "Repaired writer"), 200)
	freshSvc, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if row, err := freshSvc.GetMemberMetadata(ctx, admin.User.ID, writer.User.ID); err != nil || row.Name != "Repaired writer" {
		t.Fatal("persisted metadata unavailable to fresh service", row, err)
	}
}
