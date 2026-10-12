package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secret"
)

func testOffboardingLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	offboardingNotificationMigrationControls(t, db)
	// Install the persistent fault before any Service worker starts. Only its
	// synchronized arming changes while runtime publication may be active.
	var rejectNotice atomic.Bool
	const callback = "offboarding:reject-recipient-receipt"
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if rejectNotice.Load() && tx.Statement.Table == "offboarding_notification_recipients" {
			_ = tx.AddError(errors.New("controlled recipient receipt failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Create().Remove(callback); err != nil {
			t.Error(err)
		}
	})
	ctx := context.Background()
	svc, err := service.New(ctx, db, service.WithRuntimeRefreshInterval(time.Hour), service.WithSMTPPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	admin, err := svc.Initialize(ctx, "offboarding-admin@example.invalid", "test-only-offboarding-password", "Offboarding Admin")
	if err != nil {
		t.Fatal(err)
	}
	var administrator entity.User
	if err := db.First(&administrator, "id = ?", admin.User.ID).Error; err != nil {
		t.Fatal(err)
	}
	for _, userID := range []string{"usr_departing", "usr_successor", "usr_remaining", "usr_emergency", "usr_empty_team"} {
		user := entity.User{ID: userID, Email: userID + "@example.invalid", Name: userID, PasswordHash: administrator.PasswordHash, Role: entity.RoleMember}
		if err := db.Create(&user).Error; err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	if err := db.Model(&entity.SMTPSetting{}).Where("id = ?", 1).Updates(map[string]any{"enabled": true, "sender_email": "notices@example.invalid", "sender_name": "RouteX", "e_tag": "rev_offboarding_smtp", "updated_at": now}).Error; err != nil {
		t.Fatal(err)
	}
	for _, setting := range []entity.NotificationSetting{
		{UserID: admin.User.ID, ExternalEmail: "executor@example.invalid", EmailMedium: false, ETag: "rev_executor", UpdatedAt: now},
		{UserID: "usr_successor", ExternalEmail: "successor@example.invalid", EmailMedium: true, ETag: "rev_successor", UpdatedAt: now},
		{UserID: "usr_remaining", ExternalEmail: "remaining@example.invalid", EmailMedium: true, ETag: "rev_remaining", UpdatedAt: now},
	} {
		if err := db.Create(&setting).Error; err != nil {
			t.Fatal(err)
		}
	}
	modelID := "mdl_offboarding"
	personalBearer := "rx_" + strings.Repeat("o", 43)
	projectBearer := "rxp_" + strings.Repeat("o", 43)
	rows := []any{
		&entity.Model{ID: modelID, Status: entity.ResourceActive},
		&entity.ModelName{Name: "offboarding-model", ModelID: modelID, CurrentModelID: &modelID},
		&entity.UserModelGrant{UserID: "usr_departing", ModelID: modelID},
		&entity.APIKey{ID: "key_departing", UserID: "usr_departing", Name: "Personal", Prefix: personalBearer[:11], TokenHash: secret.SHA256Hex(personalBearer), Status: entity.KeyActive},
		&entity.APIKeyModel{KeyID: "key_departing", ModelID: modelID},
		&entity.APIKey{ID: "key_pending_departing", UserID: "usr_departing", Name: "Pending", Prefix: "rx_pending", TokenHash: secret.SHA256Hex("test-only-pending"), Status: entity.KeyPending},
		&entity.Project{ID: "prj_sole", Name: "Sole", Status: entity.ResourceActive, CreatorID: "usr_departing"},
		&entity.Project{ID: "prj_shared", Name: "Shared", Status: entity.ResourceActive, CreatorID: "usr_departing"},
		&entity.ProjectManager{ID: "pjm_sole", ProjectID: "prj_sole", UserID: "usr_departing"},
		&entity.ProjectManager{ID: "pjm_shared_one", ProjectID: "prj_shared", UserID: "usr_departing"},
		&entity.ProjectManager{ID: "pjm_shared_two", ProjectID: "prj_shared", UserID: "usr_remaining"},
		&entity.ProjectModelGrant{ProjectID: "prj_sole", ModelID: modelID},
		&entity.ProjectKey{ID: "pky_preserved", ProjectID: "prj_sole", CreatorID: "usr_departing", Name: "Preserved", Prefix: projectBearer[:12], TokenHash: secret.SHA256Hex(projectBearer), Status: entity.KeyActive, DeliveryMode: "manual"},
		&entity.ProjectKeyModel{KeyID: "pky_preserved", ModelID: modelID},
		&entity.Team{ID: "tem_sole", Name: "Sole Team", Status: entity.ResourceActive},
		&entity.TeamMembership{ID: "tmm_departing", TeamID: "tem_sole", UserID: "usr_departing", Role: entity.TeamOwner, Status: entity.ResourceActive},
		&entity.UserRole{UserID: "usr_departing", RoleID: "rol_member"},
	}
	for _, row := range rows {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.Login(ctx, "usr_departing@example.invalid", "test-only-offboarding-password"); err != nil {
		t.Fatal(err)
	}
	var projectBefore entity.ProjectKey
	if err := db.First(&projectBefore, "id = ?", "pky_preserved").Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	defer svc.StopRuntime()
	inventory, err := svc.OffboardingInventory(ctx, admin.User.ID, "usr_departing")
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.PersonalKeys) != 2 || len(inventory.Projects) != 2 || len(inventory.Teams) != 1 {
		t.Fatal("offboarding inventory mixed personal keys and project assets")
	}
	if _, err := svc.OffboardingInventory(ctx, "usr_remaining", "usr_departing"); !errors.Is(err, apperrors.ErrForbidden) {
		t.Fatal("member read another member's offboarding inventory")
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	adminCookie := &http.Cookie{Name: sessionCookie, Value: admin.Token}
	assertReadIdentity := func(expected *service.OffboardingInventory) {
		t.Helper()
		var userBefore, userAfter entity.User
		var casesBefore, casesAfter []entity.OffboardingCase
		var keysBefore, keysAfter []entity.APIKey
		var auditBefore, auditAfter int64
		if err := db.First(&userBefore, "id = ?", expected.UserID).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Where("user_id = ?", expected.UserID).Order("id").Find(&casesBefore).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Where("user_id = ?", expected.UserID).Order("id").Find(&keysBefore).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&entity.AuditEvent{}).Count(&auditBefore).Error; err != nil {
			t.Fatal(err)
		}
		for _, alias := range []string{strings.ToUpper(expected.UserID), "usr_departinG", expected.UserID + " "} {
			result, err := svc.OffboardingInventory(ctx, admin.User.ID, alias)
			if result != nil || !errors.Is(err, apperrors.ErrNotFound) {
				t.Fatal("offboarding alias disclosed retained inventory or history")
			}
			expectStatus(t, identityRequest(router, "GET", "/api/v1/admin/members/"+url.PathEscape(alias)+"/offboarding", "", adminCookie, ""), http.StatusNotFound)
		}
		response := identityRequest(router, "GET", "/api/v1/admin/members/"+expected.UserID+"/offboarding", "", adminCookie, "")
		expectStatus(t, response, http.StatusOK)
		var got service.OffboardingInventory
		if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		// Compare the complete public DTO; its private stale-plan fields are not JSON.
		expectedJSON, err := json.Marshal(expected)
		if err != nil {
			t.Fatal(err)
		}
		actualJSON, err := json.Marshal(&got)
		if err != nil || string(expectedJSON) != string(actualJSON) {
			t.Fatal("canonical retained inventory or recorded history changed")
		}
		if err := db.First(&userAfter, "id = ?", expected.UserID).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Where("user_id = ?", expected.UserID).Order("id").Find(&casesAfter).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Where("user_id = ?", expected.UserID).Order("id").Find(&keysAfter).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&entity.AuditEvent{}).Count(&auditAfter).Error; err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(userBefore, userAfter) || !reflect.DeepEqual(casesBefore, casesAfter) || !reflect.DeepEqual(keysBefore, keysAfter) || auditBefore != auditAfter {
			t.Fatal("offboarding reads changed account, Key, case or audit history")
		}
	}
	assertReadIdentity(inventory)
	input := service.OffboardingPlanInput{RequestID: "req_offboarding_plan", InventoryVersion: inventory.InventoryVersion, PlannedAt: time.Now().UTC().Add(time.Hour), Reason: "Planned departure", OffboardingAssignments: service.OffboardingAssignments{Projects: []service.OffboardingProjectAssignment{{ProjectID: "prj_sole", ManagerUserIDs: []string{"usr_successor"}}}, Teams: []service.OffboardingTeamAssignment{{TeamID: "tem_sole", OwnerUserIDs: []string{"usr_successor"}}}}}
	if _, err := svc.CreateOffboardingPlan(ctx, admin.User.ID, "usr_departing", input); err == nil {
		t.Fatal("nonmember Team successor accepted without explicit addition")
	}
	input.Teams[0].AddMemberUserIDs = []string{"usr_successor"}
	plan, err := svc.CreateOffboardingPlan(ctx, admin.User.ID, "usr_departing", input)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != "ready_to_complete" {
		t.Fatal("plan did not persist preparation state")
	}
	if _, err := svc.AuthenticateAPIKey(ctx, personalBearer); err != nil {
		t.Fatal("planning changed effective credentials")
	}
	var auditsBefore int64
	if err := db.Model(&entity.AuditEvent{}).Count(&auditsBefore).Error; err != nil {
		t.Fatal(err)
	}
	replayed, err := svc.CreateOffboardingPlan(ctx, admin.User.ID, "usr_departing", input)
	if err != nil || replayed.ID != plan.ID {
		t.Fatal("plan retry did not return its original receipt")
	}
	if _, err := svc.CreateOffboardingPlan(ctx, admin.User.ID, "usr_remaining", input); err == nil {
		t.Fatal("reused request ID crossed the target-member boundary")
	}
	changed := input
	changed.Reason = "Changed request payload"
	if _, err := svc.CreateOffboardingPlan(ctx, admin.User.ID, "usr_departing", changed); err == nil {
		t.Fatal("reused request ID accepted a different payload")
	}
	var auditsAfter int64
	if err := db.Model(&entity.AuditEvent{}).Count(&auditsAfter).Error; err != nil || auditsAfter != auditsBefore {
		t.Fatal("idempotent plan retry duplicated audit history")
	}
	assertNoOffboardingNotifications(t, db)
	// A reviewed inventory cannot overwrite responsibilities added afterwards.
	added := entity.ProjectManager{ID: "pjm_concurrent", ProjectID: "prj_sole", UserID: "usr_remaining"}
	if err := db.Create(&added).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CompleteOffboarding(ctx, admin.User.ID, "usr_departing", plan.ID); err == nil {
		t.Fatal("stale responsibility inventory was accepted")
	}
	if err := db.Delete(&added).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.User{}).Where("id = ?", "usr_successor").Update("disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CompleteOffboarding(ctx, admin.User.ID, "usr_departing", plan.ID); err == nil {
		t.Fatal("disabled successor accepted")
	}
	var departing entity.User
	if err := db.First(&departing, "id = ?", "usr_departing").Error; err != nil || departing.Disabled {
		t.Fatal("rejected handover left a disabled account")
	}
	if err := db.Model(&entity.User{}).Where("id = ?", "usr_successor").Update("disabled", false).Error; err != nil {
		t.Fatal(err)
	}
	// Publication failure rolls back the entire handover, Case, audit and all
	// notification stages; it is not an eventually repaired partial completion.
	transactionBeforeFailure := offboardingNotificationTransactionSnapshot(t, db)
	var beforeFailure entity.OffboardingCase
	if err := db.First(&beforeFailure, "id = ?", plan.ID).Error; err != nil {
		t.Fatal(err)
	}
	var auditCountBeforeFailure int64
	if err := db.Model(&entity.AuditEvent{}).Count(&auditCountBeforeFailure).Error; err != nil {
		t.Fatal(err)
	}
	rejectNotice.Store(true)
	_, noticeFailure := svc.CompleteOffboarding(ctx, admin.User.ID, "usr_departing", plan.ID)
	rejectNotice.Store(false)
	if noticeFailure == nil {
		t.Fatal("recipient persistence failure did not abort completion")
	}
	var afterFailure entity.OffboardingCase
	if err := db.First(&afterFailure, "id = ?", plan.ID).Error; err != nil || !reflect.DeepEqual(beforeFailure, afterFailure) {
		t.Fatal("recipient failure changed Case receipt", err)
	}
	if err := db.First(&departing, "id = ?", "usr_departing").Error; err != nil || departing.Disabled {
		t.Fatal("recipient failure partially disabled departing member")
	}
	var auditsAfterFailure int64
	if err := db.Model(&entity.AuditEvent{}).Count(&auditsAfterFailure).Error; err != nil || auditsAfterFailure != auditCountBeforeFailure {
		t.Fatal("recipient failure retained completion audit")
	}
	assertNoOffboardingNotifications(t, db)
	if !reflect.DeepEqual(transactionBeforeFailure, offboardingNotificationTransactionSnapshot(t, db)) {
		t.Fatal("recipient failure retained partial handover state")
	}
	// Governance locking serializes repeated completion, including its audit.
	completed := make(chan error, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Go(func() {
			_, err := svc.CompleteOffboarding(ctx, admin.User.ID, "usr_departing", plan.ID)
			completed <- err
		})
	}
	wait.Wait()
	close(completed)
	for err := range completed {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := db.First(&departing, "id = ?", "usr_departing").Error; err != nil || !departing.Disabled || departing.OffboardedAt == nil {
		t.Fatal("completion did not distinguish offboarding from ordinary suspension")
	}
	if _, err := svc.AuthenticateAPIKey(ctx, personalBearer); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("completed handover retained personal gateway access")
	}
	projectAuth, err := svc.AuthenticateAPIKey(ctx, projectBearer)
	if err != nil || projectAuth.ProjectID != "prj_sole" {
		t.Fatal("creator departure disabled a Project Key")
	}
	for _, target := range []any{&entity.Session{}, &entity.UserRole{}, &entity.TeamMembership{}, &entity.ProjectManager{}} {
		var count int64
		if err := db.Model(target).Where("user_id = ?", "usr_departing").Count(&count).Error; err != nil || count != 0 {
			t.Fatal("completion left a personal session, role, or management relationship")
		}
	}
	var keys int64
	if err := db.Model(&entity.APIKey{}).Where("user_id = ? AND status <> ?", "usr_departing", entity.KeyRevoked).Count(&keys).Error; err != nil || keys != 0 {
		t.Fatal("completion missed a pending or active personal key")
	}
	assertOffboardingCompletionNotifications(t, db, svc, admin.User.ID, plan.ID, []string{admin.User.ID, "usr_successor"}, []string{"usr_successor"})
	var successor entity.TeamMembership
	if err := db.First(&successor, "team_id = ? AND user_id = ?", "tem_sole", "usr_successor").Error; err != nil || successor.Role != entity.TeamOwner || successor.Status != entity.ResourceActive {
		t.Fatal("Team successor was not joined before ownership became effective")
	}
	var projectAfter entity.ProjectKey
	if err := db.First(&projectAfter, "id = ?", "pky_preserved").Error; err != nil || !reflect.DeepEqual(projectBefore, projectAfter) {
		t.Fatal("handover changed Project Key ownership, state, or history")
	}
	var completedAudits int64
	if err := db.Model(&entity.AuditEvent{}).Where("action = ? AND resource_id = ?", "offboarding.complete", plan.ID).Count(&completedAudits).Error; err != nil || completedAudits != 1 {
		t.Fatal("concurrent completion duplicated audit history")
	}
	if err := db.Model(&entity.ProjectManager{}).Where("project_id = ?", "prj_shared").Count(&keys).Error; err != nil || keys != 1 {
		t.Fatal("shared Project unnecessarily gained a replacement manager")
	}
	retained, err := svc.OffboardingInventory(ctx, admin.User.ID, "usr_departing")
	if err != nil || !retained.Disabled || retained.OffboardedAt == nil || len(retained.Cases) != 1 || retained.Cases[0].ID != plan.ID || retained.Cases[0].Status != "completed" {
		t.Fatal("disabled/offboarded canonical history became unreadable")
	}
	assertReadIdentity(retained)
	// Explicit reactivation is an account action, not restoration of revoked
	// credentials, deleted memberships, direct roles, or old browser sessions.
	enabled := false
	reactivated, err := svc.UpdateMember(ctx, admin.User.ID, "usr_departing", &enabled, nil)
	if err != nil || reactivated.User.Disabled || reactivated.User.OffboardedAt != nil {
		t.Fatal("explicit reactivation did not clear current offboarding state")
	}
	if _, err := svc.AuthenticateAPIKey(ctx, personalBearer); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("reactivation resurrected a revoked key")
	}
	history, err := svc.OffboardingInventory(ctx, admin.User.ID, "usr_departing")
	if err != nil || len(history.Cases) != 1 || history.Cases[0].Status != "completed" {
		t.Fatal("reactivation erased offboarding history")
	}
	serialized, err := json.Marshal(history)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{personalBearer, projectBearer, administrator.PasswordHash, "token_hash", "password_hash"} {
		if strings.Contains(string(serialized), forbidden) {
			t.Fatal("offboarding inventory exposed credential material")
		}
	}
	testEmergencyOffboarding(t, db, svc, admin.User.ID)
	testOffboardedAdministratorReactivation(t, db, svc, administrator)
	testOffboardingNotificationSMTP(t, db, svc, administrator)
}

func testEmergencyOffboarding(t *testing.T, db *gorm.DB, svc *service.Service, adminID string) {
	t.Helper()
	ctx := context.Background()
	for _, target := range []string{"usr_emergency", "usr_empty_team"} {
		projectID := "prj_" + target
		teamID := "tem_" + target
		rows := []any{&entity.Project{ID: projectID, Name: target, Status: entity.ResourceActive, CreatorID: target}, &entity.ProjectManager{ID: "pjm_" + target, ProjectID: projectID, UserID: target}, &entity.Team{ID: teamID, Name: target, Status: entity.ResourceActive}, &entity.TeamMembership{ID: "tmm_" + target, TeamID: teamID, UserID: target, Role: entity.TeamOwner, Status: entity.ResourceActive}}
		if target == "usr_emergency" {
			rows = append(rows, &entity.TeamMembership{ID: "tmm_remaining", TeamID: teamID, UserID: "usr_remaining", Role: entity.TeamMember, Status: entity.ResourceActive})
		}
		for _, row := range rows {
			if err := db.Create(row).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	request := service.OffboardingEmergencyInput{RequestID: "req_emergency", Reason: "Incident containment", CurrentPassword: "test-only-wrong-password"}
	if _, err := svc.EmergencyOffboarding(ctx, adminID, "usr_emergency", request); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("emergency disable skipped reauthentication")
	}
	request.CurrentPassword = "test-only-offboarding-password"
	result, err := svc.EmergencyOffboarding(ctx, adminID, "usr_emergency", request)
	if err != nil || result.Status != "completed" || result.Mode != "emergency" {
		t.Fatal("emergency handover failed")
	}
	replay, err := svc.EmergencyOffboarding(ctx, adminID, "usr_emergency", request)
	if err != nil || replay.ID != result.ID {
		t.Fatal("emergency retry was not idempotent")
	}
	assertOffboardingCompletionNotifications(t, db, svc, adminID, result.ID, []string{adminID, "usr_remaining"}, []string{"usr_remaining"})
	var manager entity.ProjectManager
	if err := db.First(&manager, "project_id = ?", "prj_usr_emergency").Error; err != nil || manager.UserID != adminID {
		t.Fatal("emergency Project lost temporary administrator continuity")
	}
	var owner entity.TeamMembership
	if err := db.First(&owner, "team_id = ? AND role = ?", "tem_usr_emergency", entity.TeamOwner).Error; err != nil || owner.UserID != "usr_remaining" {
		t.Fatal("emergency did not promote an existing enabled member")
	}
	request.RequestID = "req_emergency_empty"
	if _, err := svc.EmergencyOffboarding(ctx, adminID, "usr_empty_team", request); err == nil {
		t.Fatal("emergency bypassed continuity for an empty Team")
	}
	var target entity.User
	if err := db.First(&target, "id = ?", "usr_empty_team").Error; err != nil || target.Disabled {
		t.Fatal("failed emergency transaction partially disabled the account")
	}
	request.Teams = []service.OffboardingTeamAssignment{{TeamID: "tem_usr_empty_team", OwnerUserIDs: []string{"usr_successor"}, AddMemberUserIDs: []string{"usr_successor"}}}
	if _, err := svc.EmergencyOffboarding(ctx, adminID, "usr_empty_team", request); err != nil {
		t.Fatal(err)
	}
	last, err := svc.OffboardingInventory(ctx, adminID, adminID)
	if err != nil || !last.LastAdministrator {
		t.Fatal("last administrator not identified")
	}
	if _, err := svc.EmergencyOffboarding(ctx, adminID, adminID, service.OffboardingEmergencyInput{RequestID: "req_last_admin", Reason: "Must fail", CurrentPassword: "test-only-offboarding-password"}); err == nil {
		t.Fatal("last administrator could be offboarded")
	}
}

func testOffboardedAdministratorReactivation(t *testing.T, db *gorm.DB, svc *service.Service, administrator entity.User) {
	t.Helper()
	ctx := context.Background()
	for _, mode := range []string{"planned", "emergency"} {
		target := entity.User{ID: "usr_admin_" + mode, Email: "offboarding-admin-" + mode + "@example.invalid", Name: "Second Administrator", PasswordHash: administrator.PasswordHash, Role: entity.RoleAdmin}
		if err := db.Create(&target).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&entity.UserRole{UserID: target.ID, RoleID: "rol_admin"}).Error; err != nil {
			t.Fatal(err)
		}
		if err := svc.Authorize(ctx, target.ID, "members.write"); err != nil {
			t.Fatal("administrator fixture lacked the permission under test")
		}
		if mode == "planned" {
			inventory, err := svc.OffboardingInventory(ctx, administrator.ID, target.ID)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := svc.CreateOffboardingPlan(ctx, administrator.ID, target.ID, service.OffboardingPlanInput{RequestID: "req_admin_planned", InventoryVersion: inventory.InventoryVersion, PlannedAt: time.Now().UTC().Add(time.Hour), Reason: "Administrator departure"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := svc.CompleteOffboarding(ctx, administrator.ID, target.ID, plan.ID); err != nil {
				t.Fatal(err)
			}
		} else {
			if _, err := svc.EmergencyOffboarding(ctx, administrator.ID, target.ID, service.OffboardingEmergencyInput{RequestID: "req_admin_emergency", CurrentPassword: "test-only-offboarding-password", Reason: "Administrator incident"}); err != nil {
				t.Fatal(err)
			}
		}
		var disabled entity.User
		if err := db.First(&disabled, "id = ?", target.ID).Error; err != nil || !disabled.Disabled || disabled.Role != entity.RoleMember {
			t.Fatal("offboarding preserved the base administrator role")
		}
		enable := false
		reactivated, err := svc.UpdateMember(ctx, administrator.ID, target.ID, &enable, nil)
		if err != nil || reactivated.User.Disabled || reactivated.User.OffboardedAt != nil || reactivated.User.Role != entity.RoleMember || len(reactivated.RoleIDs) != 0 {
			t.Fatal("explicit enable restored former administrator roles")
		}
		if err := svc.Authorize(ctx, target.ID, "members.write"); !errors.Is(err, apperrors.ErrForbidden) {
			t.Fatal("reactivated member retained former administrator permission")
		}
		var audits int64
		if err := db.Model(&entity.AuditEvent{}).Where("action = ? AND resource_id = ?", "member.role.remove", target.ID).Count(&audits).Error; err != nil || audits != 1 {
			t.Fatal("administrator role removal lacks a single audit receipt")
		}
	}
}

// exerciseEnterpriseOffboarding uses the normal reviewed HTTP handover workflow.
// Protocol-issued Session/challenge prerequisites belong to the calling TLS fixture.
func exerciseEnterpriseOffboarding(t *testing.T, ctx context.Context, db *gorm.DB, svc *service.Service, protocol, userID string, admin SessionResponse, adminCookie *http.Cookie, request func(string, string, any, *http.Cookie, string, string, *http.Cookie) *httptest.ResponseRecorder) func() {
	t.Helper()
	checkStatus := func(response *httptest.ResponseRecorder, want int) {
		t.Helper()
		if response.Code != want {
			t.Fatalf("%s offboarding HTTP status=%d want=%d", protocol, response.Code, want)
		}
	}
	modelID, projectID := "mdl_"+protocol+"_offboard", "prj_"+protocol+"_offboard"
	personalID, projectKeyID := "key_"+protocol+"_offboard", "pky_"+protocol+"_offboard"
	personalBearer, projectBearer := "rx_"+strings.Repeat("p", 43), "rxp_"+strings.Repeat("q", 43)
	// These are ordinary ownership fixtures, not fabricated native call evidence.
	rows := []any{
		&entity.Model{ID: modelID, Status: entity.ResourceActive},
		&entity.ModelName{Name: protocol + "-offboard-model", ModelID: modelID, CurrentModelID: &modelID},
		&entity.UserModelGrant{UserID: userID, ModelID: modelID},
		&entity.APIKey{ID: personalID, UserID: userID, Name: "Departing personal Key", Prefix: personalBearer[:11], TokenHash: secret.SHA256Hex(personalBearer), Status: entity.KeyActive},
		&entity.APIKeyModel{KeyID: personalID, ModelID: modelID},
		&entity.Project{ID: projectID, Name: "Retained enterprise Project", Status: entity.ResourceActive, CreatorID: userID},
		&entity.ProjectManager{ID: "pjm_" + protocol + "_departing", ProjectID: projectID, UserID: userID},
		&entity.ProjectManager{ID: "pjm_" + protocol + "_remaining", ProjectID: projectID, UserID: admin.User.ID},
		&entity.ProjectModelGrant{ProjectID: projectID, ModelID: modelID},
		&entity.ProjectKey{ID: projectKeyID, ProjectID: projectID, CreatorID: userID, Name: "Retained Project Key", Prefix: projectBearer[:12], TokenHash: secret.SHA256Hex(projectBearer), Status: entity.KeyActive, DeliveryMode: "manual"},
		&entity.ProjectKeyModel{KeyID: projectKeyID, ModelID: modelID},
	}
	for _, row := range rows {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	personal, err := svc.AuthenticateAPIKey(ctx, personalBearer)
	if err != nil || personal.Key.UserID != userID {
		t.Fatal("personal Key lacks admitted pre-offboarding baseline")
	}
	projectAuth, err := svc.AuthenticateAPIKey(ctx, projectBearer)
	if err != nil || projectAuth.ProjectID != projectID {
		t.Fatal("Project Key lacks admitted pre-offboarding baseline")
	}
	var projectBefore entity.Project
	var projectKeyBefore entity.ProjectKey
	var grantBefore entity.ProjectModelGrant
	var auditsBefore []entity.AuditEvent
	if db.Where("id = ?", projectID).Take(&projectBefore).Error != nil || db.Where("id = ?", projectKeyID).Take(&projectKeyBefore).Error != nil || db.Where("project_id = ? AND model_id = ?", projectID, modelID).Take(&grantBefore).Error != nil || db.Order("id").Find(&auditsBefore).Error != nil {
		t.Fatal("capture retained ownership and identity audit facts")
	}
	base := "/api/v1/admin/members/" + userID + "/offboarding"
	inventoryResponse := request("GET", base, nil, adminCookie, "", "", nil)
	checkStatus(inventoryResponse, 200)
	var inventory service.OffboardingInventory
	if json.Unmarshal(inventoryResponse.Body.Bytes(), &inventory) != nil || inventory.UserID != userID || inventory.Disabled || len(inventory.Projects) != 1 || inventory.Projects[0].ID != projectID || inventory.Projects[0].RequiresSuccessor {
		t.Fatal("offboarding inventory lost shared Project baseline")
	}
	input := OffboardingPlanRequest{RequestID: "req_" + protocol + "_enterprise_offboard", InventoryVersion: inventory.InventoryVersion, PlannedAt: time.Now().UTC().Truncate(time.Microsecond).Add(time.Hour), Reason: "Controlled enterprise identity departure"}
	planned := request("POST", base+"/plans", input, adminCookie, admin.CSRFToken, "", nil)
	checkStatus(planned, 201)
	var plan service.OffboardingCaseRecord
	if json.Unmarshal(planned.Body.Bytes(), &plan) != nil || plan.ID == "" || plan.UserID != userID || plan.ActorID != admin.User.ID || plan.RequestID != input.RequestID || plan.Status != "ready_to_complete" {
		t.Fatal("invalid original offboarding plan receipt")
	}
	planReplay := request("POST", base+"/plans", input, adminCookie, admin.CSRFToken, "", nil)
	checkStatus(planReplay, 201)
	if !bytes.Equal(planned.Body.Bytes(), planReplay.Body.Bytes()) {
		t.Fatal("identical plan replay changed original receipt")
	}
	completed := request("POST", base+"/"+plan.ID+"/complete", nil, adminCookie, admin.CSRFToken, "", nil)
	checkStatus(completed, 200)
	var receipt service.OffboardingCaseRecord
	if json.Unmarshal(completed.Body.Bytes(), &receipt) != nil || receipt.ID != plan.ID || receipt.RequestID != input.RequestID || receipt.Status != "completed" || receipt.CompletedAt == nil || receipt.CompletedBy != admin.User.ID {
		t.Fatal("offboarding did not return exact completed receipt")
	}
	replay := request("POST", base+"/"+plan.ID+"/complete", nil, adminCookie, admin.CSRFToken, "", nil)
	checkStatus(replay, 200)
	if !bytes.Equal(completed.Body.Bytes(), replay.Body.Bytes()) {
		t.Fatal("completion replay changed original receipt")
	}
	var durableReceipt entity.OffboardingCase
	if db.Where("id = ?", plan.ID).Take(&durableReceipt).Error != nil {
		t.Fatal("read original durable offboarding receipt")
	}
	assertRetained := func() {
		t.Helper()
		for _, target := range []any{&entity.Session{}, &entity.MFAChallenge{}, &entity.ProjectManager{}} {
			var count int64
			if db.Model(target).Where("user_id = ?", userID).Count(&count).Error != nil || count != 0 {
				t.Fatalf("%s departure retained personal authority %T count=%d", protocol, target, count)
			}
		}
		var member entity.User
		if db.Where("id = ?", userID).Take(&member).Error != nil || !member.Disabled || member.OffboardedAt == nil {
			t.Fatal("offboarding lost exact disabled departure state")
		}
		if _, err := svc.AuthenticateAPIKey(ctx, personalBearer); !errors.Is(err, apperrors.ErrUnauthorized) {
			t.Fatal("enterprise departure retained personal Key authority")
		}
		projectAuth, err := svc.AuthenticateAPIKey(ctx, projectBearer)
		if err != nil || projectAuth.ProjectID != projectID {
			t.Fatal("enterprise departure revoked independent Project Key")
		}
		var projectAfter entity.Project
		var keyAfter entity.ProjectKey
		var grantAfter entity.ProjectModelGrant
		var receiptAfter entity.OffboardingCase
		if db.Where("id = ?", projectID).Take(&projectAfter).Error != nil || db.Where("id = ?", projectKeyID).Take(&keyAfter).Error != nil || db.Where("project_id = ? AND model_id = ?", projectID, modelID).Take(&grantAfter).Error != nil || db.Where("id = ?", plan.ID).Take(&receiptAfter).Error != nil || !reflect.DeepEqual(projectBefore, projectAfter) || !reflect.DeepEqual(projectKeyBefore, keyAfter) || !reflect.DeepEqual(grantBefore, grantAfter) || !reflect.DeepEqual(durableReceipt, receiptAfter) {
			t.Fatal("departure or late callback changed retained Project or original receipt")
		}
		var managers []entity.ProjectManager
		if db.Where("project_id = ?", projectID).Find(&managers).Error != nil || len(managers) != 1 || managers[0].UserID != admin.User.ID {
			t.Fatal("departure did not retain exact independent Project manager")
		}
		for _, action := range []string{"offboarding.plan", "offboarding.complete"} {
			var count int64
			if db.Model(&entity.AuditEvent{}).Where("action = ? AND resource_id = ?", action, plan.ID).Count(&count).Error != nil || count != 1 {
				t.Fatalf("%s duplicated or lost %s audit count=%d", protocol, action, count)
			}
		}
		for _, original := range auditsBefore {
			var current entity.AuditEvent
			if db.Where("id = ?", original.ID).Take(&current).Error != nil || !reflect.DeepEqual(original, current) {
				t.Fatal("offboarding rewrote prior identity audit history")
			}
		}
		checkStatus(request("GET", "/api/v1/auth/session", nil, adminCookie, "", "", nil), 200)
	}
	assertRetained()
	return assertRetained
}

// Prepare the ownership and refusal proofs before holding remote identity I/O.
// The returned completion uses the real emergency HTTP entry, not planned handover.
func prepareEmergencyEnterpriseOffboarding(t *testing.T, ctx context.Context, db *gorm.DB, svc *service.Service, userID, password string, admin, member SessionResponse, adminCookie, memberCookie *http.Cookie, request func(string, string, any, *http.Cookie, string, string, *http.Cookie) *httptest.ResponseRecorder, setStage func(string), recordHTTPFailure func(int, int)) (func(), func()) {
	t.Helper()
	checkStatus := func(response *httptest.ResponseRecorder, want int) {
		t.Helper()
		if response.Code != want {
			recordHTTPFailure(response.Code, want)
			t.Fatalf("emergency OIDC offboarding status=%d want=%d", response.Code, want)
		}
	}
	modelID := "mdl_oidc_emergency"
	const projectID, teamID = "prj_oidc_emergency", "tem_oidc_emergency"
	personalBearer, projectBearer := "rx_"+strings.Repeat("e", 43), "rxp_"+strings.Repeat("r", 43)
	// Ordinary ownership fixtures do not claim upstream invocation evidence.
	rows := []any{
		&entity.Model{ID: modelID, Status: entity.ResourceActive},
		&entity.ModelName{Name: "oidc-emergency-model", ModelID: modelID, CurrentModelID: &modelID},
		&entity.UserModelGrant{UserID: userID, ModelID: modelID},
		&entity.APIKey{ID: "key_oidc_emergency", UserID: userID, Name: "Emergency personal Key", Prefix: personalBearer[:11], TokenHash: secret.SHA256Hex(personalBearer), Status: entity.KeyActive},
		&entity.APIKeyModel{KeyID: "key_oidc_emergency", ModelID: modelID},
		&entity.Project{ID: projectID, Name: "Emergency retained Project", Status: entity.ResourceActive, CreatorID: userID},
		&entity.ProjectManager{ID: "pjm_oidc_emergency", ProjectID: projectID, UserID: userID},
		&entity.ProjectModelGrant{ProjectID: projectID, ModelID: modelID},
		&entity.ProjectKey{ID: "pky_oidc_emergency", ProjectID: projectID, CreatorID: userID, Name: "Emergency retained Project Key", Prefix: projectBearer[:12], TokenHash: secret.SHA256Hex(projectBearer), Status: entity.KeyActive, DeliveryMode: "manual"},
		&entity.ProjectKeyModel{KeyID: "pky_oidc_emergency", ModelID: modelID},
		&entity.Team{ID: teamID, Name: "Emergency retained Team", Status: entity.ResourceActive},
		&entity.TeamMembership{ID: "tmm_oidc_emergency_owner", TeamID: teamID, UserID: userID, Role: entity.TeamOwner, Status: entity.ResourceActive},
		&entity.TeamMembership{ID: "tmm_oidc_emergency_successor", TeamID: teamID, UserID: admin.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive},
		&entity.TeamModelGrant{TeamID: teamID, ModelID: modelID},
	}
	setStage("emergency_ownership_create")
	for _, row := range rows {
		if db.Create(row).Error != nil {
			t.Fatal("create emergency ownership prerequisite")
		}
	}
	// Persisted synthetic history proves retention only. Neither these rows nor
	// their unknown native evidence claim an actual upstream invocation/completion.
	historyIDs := []string{"req_oidc_emergency_personal", "req_oidc_emergency_project"}
	stamp := time.Now().UTC().Truncate(time.Microsecond)
	setStage("emergency_history_create")
	for index, requestID := range historyIDs {
		call := entity.CallRecord{RequestID: requestID, UserID: userID, KeyID: "key_oidc_emergency", ModelID: modelID, ModelName: "oidc-emergency-model", SnapshotID: "cfg_retained_history", ProviderModelID: "pmd_retained_history", ConnectionID: "con_retained_history", Protocol: entity.ProtocolOpenAIChat, Status: "error", ErrorCode: "upstream_unavailable", StartedAt: stamp, CompletedAt: stamp.Add(time.Millisecond), DurationMS: 1, CallPricingFields: entity.CallPricingFields{PricingStatus: "not_captured"}}
		if index == 1 {
			call.UserID, call.KeyID, call.ProjectID = "", "pky_oidc_emergency", projectID
		}
		attempt := entity.CallAttempt{ID: requestID + "_attempt", RequestID: requestID, ProviderModelID: call.ProviderModelID, ConnectionID: call.ConnectionID, AttemptNumber: 1, Status: "error", FailureClass: "permanent_failure", WorkEvidence: "unknown", NativeCompletionEvidence: "unknown", HTTPStatus: 503, ErrorCode: call.ErrorCode, StartedAt: call.StartedAt, CompletedAt: call.CompletedAt}
		if db.Create(&call).Error != nil || db.Create(&attempt).Error != nil {
			t.Fatal("persist synthetic emergency call and attempt history")
		}
	}
	setStage("emergency_key_admission")
	if auth, err := svc.AuthenticateAPIKey(ctx, personalBearer); err != nil || auth.Key.UserID != userID {
		t.Fatal("emergency personal Key lacks admitted baseline")
	}
	if auth, err := svc.AuthenticateAPIKey(ctx, projectBearer); err != nil || auth.ProjectID != projectID {
		t.Fatal("emergency Project Key lacks admitted baseline")
	}
	// Read the retained prerequisite rows independently of the service's joined
	// admitted-successor queries. Count exact stored identities in Go, so this
	// diagnostic does not repeat the exclusion predicate being investigated.
	setStage("emergency_persisted_project_status")
	var persistedProject entity.Project
	if db.Where(database.ExactText(db, clause.Column{Name: "id"}, projectID)).Take(&persistedProject).Error != nil || persistedProject.ID != projectID || persistedProject.Status != entity.ResourceActive {
		t.Fatal("emergency persisted Project is not exactly active")
	}
	setStage("emergency_persisted_team_status")
	var persistedTeam entity.Team
	if db.Where(database.ExactText(db, clause.Column{Name: "id"}, teamID)).Take(&persistedTeam).Error != nil || persistedTeam.ID != teamID || persistedTeam.Status != entity.ResourceActive {
		t.Fatal("emergency persisted Team is not exactly active")
	}
	setStage("emergency_other_manager_count")
	var persistedManagers []entity.ProjectManager
	if db.Where(database.ExactText(db, clause.Column{Name: "project_id"}, projectID)).Order("id").Limit(3).Find(&persistedManagers).Error != nil || len(persistedManagers) == 3 {
		t.Fatal("emergency exact manager diagnostic query failed or overflowed")
	}
	otherManagers := 0
	for _, row := range persistedManagers {
		if row.ProjectID != projectID {
			t.Fatal("emergency manager diagnostic lost exact scope")
		}
		if row.UserID != userID {
			otherManagers++
		}
	}
	if len(persistedManagers) != 1 || otherManagers != 0 {
		t.Fatalf("emergency manager prerequisite count=%d other=%d", len(persistedManagers), otherManagers)
	}
	setStage("emergency_other_active_owner_count")
	var persistedMemberships []entity.TeamMembership
	if db.Where(database.ExactText(db, clause.Column{Name: "team_id"}, teamID)).Order("id").Limit(3).Find(&persistedMemberships).Error != nil || len(persistedMemberships) == 3 {
		t.Fatal("emergency exact owner diagnostic query failed or overflowed")
	}
	otherActiveOwners := 0
	for _, row := range persistedMemberships {
		if row.TeamID != teamID {
			t.Fatal("emergency owner diagnostic lost exact scope")
		}
		if row.UserID != userID && row.Role == entity.TeamOwner && row.Status == entity.ResourceActive {
			otherActiveOwners++
		}
	}
	if len(persistedMemberships) != 2 || otherActiveOwners != 0 {
		t.Fatalf("emergency owner prerequisite count=%d other=%d", len(persistedMemberships), otherActiveOwners)
	}
	base := "/api/v1/admin/members/" + userID + "/offboarding"
	setStage("emergency_inventory_response_headers")
	inventoryResponse := request("GET", base, nil, adminCookie, "", "", nil)
	checkStatus(inventoryResponse, 200)
	var inventory service.OffboardingInventory
	setStage("emergency_inventory_decode")
	if json.Unmarshal(inventoryResponse.Body.Bytes(), &inventory) != nil {
		t.Fatal("emergency inventory decoding failed")
	}
	setStage("emergency_inventory_user")
	if inventory.UserID != userID || inventory.Disabled {
		t.Fatal("emergency inventory lost current member baseline")
	}
	setStage("emergency_inventory_project_count")
	if len(inventory.Projects) != 1 {
		t.Fatal("emergency inventory lost exact Project count")
	}
	setStage("emergency_inventory_team_count")
	if len(inventory.Teams) != 1 {
		t.Fatal("emergency inventory lost exact Team count")
	}
	setStage("emergency_inventory_project_id")
	if inventory.Projects[0].ID != projectID {
		t.Fatal("emergency inventory lost exact Project identity")
	}
	setStage("emergency_inventory_team_id")
	if inventory.Teams[0].ID != teamID {
		t.Fatal("emergency inventory lost exact Team identity")
	}
	setStage("emergency_inventory_project_successor")
	if !inventory.Projects[0].RequiresSuccessor {
		t.Fatal("emergency baseline lacks sole-manager continuity")
	}
	setStage("emergency_inventory_team_status")
	if inventory.Teams[0].Status != entity.ResourceActive {
		t.Fatal("emergency inventory Team status is not active")
	}
	setStage("emergency_inventory_team_successor")
	if !inventory.Teams[0].RequiresSuccessor {
		t.Fatal("emergency baseline lacks sole-owner continuity")
	}
	// Stable rows include credentials only in memory; never print the snapshot.
	snapshot := func() []any {
		t.Helper()
		values := []struct {
			rows  any
			order string
		}{
			{&[]entity.User{}, "id"}, {&[]entity.Session{}, "id"}, {&[]entity.MFAChallenge{}, "user_id"},
			{&[]entity.MFARecoveryCode{}, "user_id, code_hash"}, {&[]entity.OIDCProvider{}, "id"}, {&[]entity.OIDCBinding{}, "id"},
			{&[]entity.APIKey{}, "id"}, {&[]entity.APIKeyModel{}, "key_id, model_id"},
			{&[]entity.Project{}, "id"}, {&[]entity.ProjectManager{}, "id"}, {&[]entity.ProjectModelGrant{}, "project_id, model_id"},
			{&[]entity.ProjectKey{}, "id"}, {&[]entity.ProjectKeyModel{}, "key_id, model_id"},
			{&[]entity.Team{}, "id"}, {&[]entity.TeamMembership{}, "id"}, {&[]entity.TeamModelGrant{}, "team_id, model_id"},
			{&[]entity.CallRecord{}, "request_id"}, {&[]entity.CallAttempt{}, "id"},
			{&[]entity.OffboardingCase{}, "id"}, {&[]entity.AuditEvent{}, "id"},
		}
		captured := make([]any, 0, len(values))
		for _, value := range values {
			if db.Order(value.order).Find(value.rows).Error != nil {
				t.Fatal("read emergency refusal snapshot")
			}
			captured = append(captured, value.rows)
		}
		return captured
	}
	input := OffboardingEmergencyRequest{RequestID: "req_oidc_emergency", CurrentPassword: password, Reason: "Controlled OIDC emergency containment"}
	setStage("emergency_refusal_snapshot")
	beforeRefusals := snapshot()
	wrongPassword := input
	wrongPassword.CurrentPassword = "test-only-wrong-emergency-password"
	for _, refusal := range []struct {
		stage  string
		target string
		body   OffboardingEmergencyRequest
		cookie *http.Cookie
		csrf   string
		status int
	}{
		{"emergency_refusal_no_session", userID, input, nil, "", 401},
		{"emergency_refusal_member", userID, input, memberCookie, member.CSRFToken, 403},
		{"emergency_refusal_csrf", userID, input, adminCookie, "", 403},
		{"emergency_refusal_password", userID, wrongPassword, adminCookie, admin.CSRFToken, 401},
		{"emergency_refusal_alias", strings.ToUpper(userID), input, adminCookie, admin.CSRFToken, 404},
	} {
		setStage(refusal.stage)
		checkStatus(request("POST", "/api/v1/admin/members/"+refusal.target+"/offboarding/emergency", refusal.body, refusal.cookie, refusal.csrf, "", nil), refusal.status)
		if !reflect.DeepEqual(beforeRefusals, snapshot()) {
			t.Fatal("refused emergency write changed authority, continuity or history")
		}
	}
	setStage("emergency_capture_retained")
	var projectBefore entity.Project
	var keyBefore entity.ProjectKey
	var grantBefore entity.ProjectModelGrant
	var teamBefore entity.Team
	var teamGrantBefore entity.TeamModelGrant
	var callsBefore []entity.CallRecord
	var attemptsBefore []entity.CallAttempt
	var auditsBefore []entity.AuditEvent
	if db.Take(&projectBefore, "id = ?", projectID).Error != nil || db.Take(&keyBefore, "id = ?", "pky_oidc_emergency").Error != nil || db.Take(&grantBefore, "project_id = ? AND model_id = ?", projectID, modelID).Error != nil || db.Take(&teamBefore, "id = ?", teamID).Error != nil || db.Take(&teamGrantBefore, "team_id = ? AND model_id = ?", teamID, modelID).Error != nil || db.Order("id").Find(&auditsBefore).Error != nil {
		t.Fatal("capture retained emergency facts")
	}
	if db.Where("request_id IN ?", historyIDs).Order("request_id").Find(&callsBefore).Error != nil || len(callsBefore) != 2 || db.Where("request_id IN ?", historyIDs).Order("id").Find(&attemptsBefore).Error != nil || len(attemptsBefore) != 2 {
		t.Fatal("capture exact persisted personal and Project history")
	}
	var receipt entity.OffboardingCase
	assertRetained := func() {
		t.Helper()
		setStage("emergency_retained_authority")
		if receipt.ID == "" {
			t.Fatal("emergency containment lacks durable completed receipt")
		}
		for _, target := range []any{&entity.Session{}, &entity.MFAChallenge{}, &entity.ProjectManager{}, &entity.TeamMembership{}} {
			var count int64
			if db.Model(target).Where("user_id = ?", userID).Count(&count).Error != nil || count != 0 {
				t.Fatal("emergency departure retained personal authority or responsibility")
			}
		}
		var current entity.User
		if db.Take(&current, "id = ?", userID).Error != nil || !current.Disabled || current.OffboardedAt == nil {
			t.Fatal("emergency departure lost terminal account state")
		}
		if _, err := svc.AuthenticateAPIKey(ctx, personalBearer); !errors.Is(err, apperrors.ErrUnauthorized) {
			t.Fatal("emergency departure retained personal Key authority")
		}
		var personalKey entity.APIKey
		if db.Take(&personalKey, "id = ?", "key_oidc_emergency").Error != nil || personalKey.UserID != userID || personalKey.Status != entity.KeyRevoked {
			t.Fatal("emergency departure did not persist exact personal Key revocation")
		}
		if auth, err := svc.AuthenticateAPIKey(ctx, projectBearer); err != nil || auth.ProjectID != projectID {
			t.Fatal("emergency departure revoked independent Project Key")
		}
		setStage("emergency_retained_resources")
		var projectAfter entity.Project
		var keyAfter entity.ProjectKey
		var grantAfter entity.ProjectModelGrant
		var teamAfter entity.Team
		var teamGrantAfter entity.TeamModelGrant
		var receiptAfter entity.OffboardingCase
		if db.Take(&projectAfter, "id = ?", projectID).Error != nil || db.Take(&keyAfter, "id = ?", keyBefore.ID).Error != nil || db.Take(&grantAfter, "project_id = ? AND model_id = ?", projectID, modelID).Error != nil || db.Take(&teamAfter, "id = ?", teamID).Error != nil || db.Take(&teamGrantAfter, "team_id = ? AND model_id = ?", teamID, modelID).Error != nil || db.Take(&receiptAfter, "id = ?", receipt.ID).Error != nil || !reflect.DeepEqual(projectBefore, projectAfter) || !reflect.DeepEqual(keyBefore, keyAfter) || !reflect.DeepEqual(grantBefore, grantAfter) || !reflect.DeepEqual(teamBefore, teamAfter) || !reflect.DeepEqual(teamGrantBefore, teamGrantAfter) || !reflect.DeepEqual(receipt, receiptAfter) {
			t.Fatal("emergency departure or late callback changed retained resources or receipt")
		}
		setStage("emergency_retained_calls")
		var callsAfter []entity.CallRecord
		var attemptsAfter []entity.CallAttempt
		if db.Where("request_id IN ?", historyIDs).Order("request_id").Find(&callsAfter).Error != nil || db.Where("request_id IN ?", historyIDs).Order("id").Find(&attemptsAfter).Error != nil || !reflect.DeepEqual(callsBefore, callsAfter) || !reflect.DeepEqual(attemptsBefore, attemptsAfter) {
			t.Fatal("emergency departure, replay or late callback changed persisted call history")
		}
		setStage("emergency_retained_succession")
		var managers []entity.ProjectManager
		var owners []entity.TeamMembership
		if db.Where("project_id = ?", projectID).Find(&managers).Error != nil || len(managers) != 1 || managers[0].UserID != admin.User.ID || db.Where("team_id = ?", teamID).Find(&owners).Error != nil || len(owners) != 1 || owners[0].UserID != admin.User.ID || owners[0].Role != entity.TeamOwner || owners[0].Status != entity.ResourceActive {
			t.Fatal("emergency departure lost automatic Project or Team succession")
		}
		setStage("emergency_retained_audit")
		var caseCount, completionCount, planCount int64
		if db.Model(&entity.OffboardingCase{}).Where("user_id = ?", userID).Count(&caseCount).Error != nil || caseCount != 1 || db.Model(&entity.AuditEvent{}).Where("action = ? AND resource_id = ?", "offboarding.complete", receipt.ID).Count(&completionCount).Error != nil || completionCount != 1 || db.Model(&entity.AuditEvent{}).Where("action = ? AND resource_id = ?", "offboarding.plan", receipt.ID).Count(&planCount).Error != nil || planCount != 0 {
			t.Fatal("emergency retry duplicated history or fabricated a planned case")
		}
		for _, original := range auditsBefore {
			var current entity.AuditEvent
			if db.Take(&current, "id = ?", original.ID).Error != nil || !reflect.DeepEqual(original, current) {
				t.Fatal("emergency departure rewrote prior audit history")
			}
		}
		setStage("emergency_administrator_session")
		checkStatus(request("GET", "/api/v1/auth/session", nil, adminCookie, "", "", nil), 200)
	}
	complete := func() {
		t.Helper()
		setStage("emergency_complete")
		response := request("POST", base+"/emergency", input, adminCookie, admin.CSRFToken, "", nil)
		checkStatus(response, 200)
		var result service.OffboardingCaseRecord
		if json.Unmarshal(response.Body.Bytes(), &result) != nil || result.ID == "" || result.UserID != userID || result.ActorID != admin.User.ID || result.RequestID != input.RequestID || result.Mode != "emergency" || result.Status != "completed" || result.PlannedAt != nil || result.CompletedAt == nil || result.CompletedBy != admin.User.ID || result.Reason != input.Reason || len(result.Assignments.Projects) != 1 || result.Assignments.Projects[0].ProjectID != projectID || !reflect.DeepEqual(result.Assignments.Projects[0].ManagerUserIDs, []string{admin.User.ID}) || len(result.Assignments.Teams) != 1 || result.Assignments.Teams[0].TeamID != teamID || !reflect.DeepEqual(result.Assignments.Teams[0].OwnerUserIDs, []string{admin.User.ID}) {
			t.Fatal("invalid exact emergency containment receipt")
		}
		if db.Take(&receipt, "id = ?", result.ID).Error != nil {
			t.Fatal("read durable emergency receipt")
		}
		setStage("emergency_receipt_replay")
		replay := request("POST", base+"/emergency", input, adminCookie, admin.CSRFToken, "", nil)
		checkStatus(replay, 200)
		if !bytes.Equal(response.Body.Bytes(), replay.Body.Bytes()) {
			t.Fatal("emergency retry changed original public receipt")
		}
		setStage("emergency_changed_intent")
		changed := input
		changed.Reason = "Different emergency intent"
		checkStatus(request("POST", base+"/emergency", changed, adminCookie, admin.CSRFToken, "", nil), 409)
	}
	return complete, assertRetained
}
