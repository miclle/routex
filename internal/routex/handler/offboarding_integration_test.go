package handler

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secret"
)

func testOffboardingLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	svc, err := service.New(ctx, db)
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
	svc.StopRuntime()
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
