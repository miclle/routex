package handler

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secret"
)

func testProjectRequestLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	svc, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := svc.Initialize(ctx, "request-admin@example.invalid", "test-only-request-password", "Request Admin")
	if err != nil {
		t.Fatal(err)
	}
	for _, userID := range []string{"usr_request_manager", "usr_request_other"} {
		if err := db.Create(&entity.User{ID: userID, Email: userID + "@example.invalid", Name: userID, Role: entity.RoleMember, PasswordHash: "test-only-unused"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, modelID := range []string{"mdl_request_a", "mdl_request_b", "mdl_request_c", "mdl_request_d"} {
		if err := db.Create(&entity.Model{ID: modelID, Status: entity.ResourceActive}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&entity.ModelName{Name: modelID, ModelID: modelID, CurrentModelID: &modelID}).Error; err != nil {
			t.Fatal(err)
		}
	}
	projectID := "prj_model_request"
	manager := "usr_request_manager"
	bearer := "rxp_" + strings.Repeat("r", 43)
	historicalBearer := "rxp_" + strings.Repeat("s", 43)
	for _, row := range []any{
		&entity.Project{ID: projectID, Name: "Model requests", Status: entity.ResourceActive, CreatorID: manager},
		&entity.ProjectManager{ID: "pjm_request", ProjectID: projectID, UserID: manager},
		&entity.ProjectModelGrant{ProjectID: projectID, ModelID: "mdl_request_a"},
		&entity.ProjectKey{ID: "pky_request", ProjectID: projectID, CreatorID: manager, Name: "Fixed scope", Prefix: bearer[:12], TokenHash: secret.SHA256Hex(bearer), Status: entity.KeyActive, DeliveryMode: "manual"},
		&entity.ProjectKeyModel{KeyID: "pky_request", ModelID: "mdl_request_a"},
		&entity.ProjectKey{ID: "pky_request_historical", ProjectID: projectID, CreatorID: manager, Name: "Historical scope", Prefix: historicalBearer[:12], TokenHash: secret.SHA256Hex(historicalBearer), Status: entity.KeyActive, DeliveryMode: "manual"},
		&entity.ProjectKeyModel{KeyID: "pky_request_historical", ModelID: "mdl_request_b"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	defer svc.StopRuntime()
	svc.StopRuntime() // All changes must publish synchronously; polling cannot mask a missing hook.
	input := service.ProjectRequestInput{RequestID: "req_request_one", ModelIDs: []string{"mdl_request_b"}, Reason: "New application"}
	if _, err := svc.CreateProjectRequest(ctx, admin.User.ID, projectID, input); !errors.Is(err, apperrors.ErrForbidden) {
		t.Fatal("administrator bypassed actual-manager creation requirement")
	}
	if _, err := svc.CreateProjectRequest(ctx, "usr_request_other", projectID, input); !errors.Is(err, apperrors.ErrForbidden) {
		t.Fatal("outsider created request")
	}
	request, err := svc.CreateProjectRequest(ctx, manager, projectID, input)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(request.BaselineModelIDs, []string{"mdl_request_a"}) || !reflect.DeepEqual(request.RequestedModelIDs, input.ModelIDs) || request.Status != entity.ProjectRequestPending {
		t.Fatal("request did not preserve explicit additions and baseline")
	}
	assertProjectRequestGrants(t, db, projectID, []string{"mdl_request_a"})
	pendingKey, err := svc.AuthenticateAPIKey(ctx, historicalBearer)
	if err != nil || len(pendingKey.ModelIDs) != 0 {
		t.Fatalf("pending request changed runtime grants: %v", err)
	}
	replay, err := svc.CreateProjectRequest(ctx, manager, projectID, input)
	if err != nil || replay.ID != request.ID {
		t.Fatalf("idempotent creation: %v", err)
	}
	changed := input
	changed.Reason = "Changed intent"
	if _, err := svc.CreateProjectRequest(ctx, manager, projectID, changed); err == nil {
		t.Fatal("reused idempotency key accepted another payload")
	}
	sameModels := input
	sameModels.RequestID = "req_noop"
	sameModels.ModelIDs = []string{"mdl_request_a"}
	if _, err := svc.CreateProjectRequest(ctx, manager, projectID, sameModels); err == nil {
		t.Fatal("already granted model accepted as an addition")
	}
	if _, err := svc.ListProjectRequests(ctx, "usr_request_other", projectID, service.ProjectRequestFilter{}); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatal("outsider read request history")
	}
	if _, err := svc.DecideProjectRequest(ctx, "usr_request_other", projectID, request.ID, service.ProjectRequestDecision{Action: "approve"}); !errors.Is(err, apperrors.ErrForbidden) {
		t.Fatal("unprivileged member approved request")
	}
	// Even an applicant subsequently granted approval permission cannot self-approve.
	if err := db.Create(&entity.UserRole{UserID: manager, RoleID: "rol_admin"}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DecideProjectRequest(ctx, manager, projectID, request.ID, service.ProjectRequestDecision{Action: "approve"}); !errors.Is(err, apperrors.ErrForbidden) {
		t.Fatal("self approval accepted")
	}
	if err := db.Where("user_id = ?", manager).Delete(&entity.UserRole{}).Error; err != nil {
		t.Fatal(err)
	}
	// Approval must preserve the latest C grant and must not resurrect removed A.
	if _, err := svc.SetResourceModels(ctx, admin.User.ID, service.ProjectResource, projectID, []string{"mdl_request_c"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DecideProjectRequest(ctx, admin.User.ID, "prj_missing_scope", request.ID, service.ProjectRequestDecision{Action: "approve"}); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatal("cross-project request path accepted")
	}
	decision := service.ProjectRequestDecision{Action: "approve", Reason: "Approved workload"}
	approved, err := svc.DecideProjectRequest(ctx, admin.User.ID, projectID, request.ID, decision)
	if err != nil || approved.Status != entity.ProjectRequestApproved {
		t.Fatalf("approval: %v", err)
	}
	assertProjectRequestGrants(t, db, projectID, []string{"mdl_request_b", "mdl_request_c"})
	publishedKey, err := svc.AuthenticateAPIKey(ctx, historicalBearer)
	if err != nil || !reflect.DeepEqual(publishedKey.ModelIDs, []string{"mdl_request_b"}) {
		t.Fatalf("approval returned before runtime grant publication: %v", err)
	}
	var ceilings []string
	if err := db.Model(&entity.ProjectKeyModel{}).Where("key_id = ?", "pky_request").Order("model_id").Pluck("model_id", &ceilings).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ceilings, []string{"mdl_request_a"}) {
		t.Fatal("approval expanded immutable existing key ceiling")
	}
	key, err := svc.AuthenticateAPIKey(ctx, bearer)
	if err != nil || len(key.ModelIDs) != 0 {
		t.Fatalf("published runtime retained removed grant: %v", err)
	}
	if _, err := svc.DecideProjectRequest(ctx, admin.User.ID, projectID, request.ID, decision); err != nil {
		t.Fatal("exact decision retry failed", err)
	}
	if _, err := svc.DecideProjectRequest(ctx, admin.User.ID, projectID, request.ID, service.ProjectRequestDecision{Action: "reject", Reason: "Too late"}); err == nil {
		t.Fatal("terminal approval overwritten")
	}
	var audits int64
	if err := db.Model(&entity.AuditEvent{}).Where("resource_id = ? AND action = ?", request.ID, "project.request.approve").Count(&audits).Error; err != nil || audits != 1 {
		t.Fatalf("approval audit not exactly once: %d %v", audits, err)
	}
	testProjectRequestDecisions(t, db, svc, admin.User.ID, manager, projectID)
	testProjectRequestHTTP(t, db)
}
func assertProjectRequestGrants(t *testing.T, db *gorm.DB, projectID string, want []string) {
	t.Helper()
	var got []string
	if err := db.Model(&entity.ProjectModelGrant{}).Where("project_id = ?", projectID).Order("model_id").Pluck("model_id", &got).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("project grants: got %v, want %v", got, want)
	}
}
func testProjectRequestDecisions(t *testing.T, db *gorm.DB, svc *service.Service, adminID, manager, projectID string) {
	t.Helper()
	ctx := context.Background()
	create := func(requestID string) *service.ProjectRequestRecord {
		t.Helper()
		record, err := svc.CreateProjectRequest(ctx, manager, projectID, service.ProjectRequestInput{RequestID: requestID, ModelIDs: []string{"mdl_request_d"}, Reason: "Additional model"})
		if err != nil {
			t.Fatal(err)
		}
		return record
	}
	rejected := create("req_rejected")
	if _, err := svc.DecideProjectRequest(ctx, adminID, projectID, rejected.ID, service.ProjectRequestDecision{Action: "reject"}); err == nil {
		t.Fatal("unreasoned rejection accepted")
	}
	if _, err := svc.DecideProjectRequest(ctx, adminID, projectID, rejected.ID, service.ProjectRequestDecision{Action: "reject", Reason: "Not required"}); err != nil {
		t.Fatal(err)
	}
	withdrawn := create("req_withdrawn")
	if _, err := svc.DecideProjectRequest(ctx, adminID, projectID, withdrawn.ID, service.ProjectRequestDecision{Action: "withdraw"}); !errors.Is(err, apperrors.ErrForbidden) {
		t.Fatal("another actor withdrew request")
	}
	if _, err := svc.DecideProjectRequest(ctx, manager, projectID, withdrawn.ID, service.ProjectRequestDecision{Action: "withdraw"}); err != nil {
		t.Fatal(err)
	}
	assertProjectRequestGrants(t, db, projectID, []string{"mdl_request_b", "mdl_request_c"})
	stale := create("req_revalidate")
	approve := func() error {
		_, err := svc.DecideProjectRequest(ctx, adminID, projectID, stale.ID, service.ProjectRequestDecision{Action: "approve"})
		return err
	}
	for _, change := range []struct {
		model     any
		where     string
		value     any
		column    string
		bad, good any
	}{
		{&entity.User{}, "id = ?", manager, "disabled", true, false},
		{&entity.Project{}, "id = ?", projectID, "status", entity.ResourceDisabled, entity.ResourceActive},
		{&entity.Model{}, "id = ?", "mdl_request_d", "status", entity.ResourceDisabled, entity.ResourceActive},
	} {
		if err := db.Model(change.model).Where(change.where, change.value).Update(change.column, change.bad).Error; err != nil {
			t.Fatal(err)
		}
		if err := approve(); err == nil {
			t.Fatal("approval ignored current account/project/model state")
		}
		if err := db.Model(change.model).Where(change.where, change.value).Update(change.column, change.good).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Where("project_id = ? AND user_id = ?", projectID, manager).Delete(&entity.ProjectManager{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := approve(); err == nil {
		t.Fatal("former manager's request approved")
	}
	// Former managers may clean up their own pending intent but cannot list history.
	if _, err := svc.ListProjectRequests(ctx, manager, projectID, service.ProjectRequestFilter{}); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatal("former manager retained history access")
	}
	if _, err := svc.DecideProjectRequest(ctx, manager, projectID, stale.ID, service.ProjectRequestDecision{Action: "withdraw"}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.ProjectManager{ID: "pjm_request_rejoin", ProjectID: projectID, UserID: manager}).Error; err != nil {
		t.Fatal(err)
	}
	racing := create("req_racing")
	var wg sync.WaitGroup
	outcomes := make(chan error, 2)
	for _, action := range []string{"approve", "withdraw"} {
		wg.Go(func() {
			actor := adminID
			if action == "withdraw" {
				actor = manager
			}
			_, err := svc.DecideProjectRequest(ctx, actor, projectID, racing.ID, service.ProjectRequestDecision{Action: action})
			outcomes <- err
		})
	}
	wg.Wait()
	close(outcomes)
	success := 0
	for err := range outcomes {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("concurrent terminal decisions: %d succeeded", success)
	}
	var count int64
	if err := db.Model(&entity.AuditEvent{}).Where("resource_id = ? AND action IN ?", racing.ID, []string{"project.request.approve", "project.request.withdraw"}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("concurrent decision audit: %d %v", count, err)
	}
	page, err := svc.ListProjectRequests(ctx, manager, projectID, service.ProjectRequestFilter{Limit: 2})
	if err != nil || len(page.Items) != 2 || page.NextCursor == "" {
		t.Fatalf("history pagination: %v", err)
	}
	next, err := svc.ListProjectRequests(ctx, adminID, projectID, service.ProjectRequestFilter{Limit: 100, Cursor: page.NextCursor})
	if err != nil || len(next.Items) == 0 || next.Items[0].ID == page.Items[1].ID {
		t.Fatalf("history next page: %v", err)
	}
}
