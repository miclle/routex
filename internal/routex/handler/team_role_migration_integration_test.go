package handler

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

// A table prefix without indexes or FKs reproduces interrupted reconciliation;
// its existing assignments must survive installing the frozen V41 guards.
type teamRoleBareV41Fixture struct {
	TeamID string `gorm:"primaryKey;size:30;not null"`
	RoleID string `gorm:"primaryKey;size:30;not null"`
}

func (teamRoleBareV41Fixture) TableName() string { return "team_roles" }

func testTeamRoleMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	model := &entity.TeamRole{}
	team := entity.Team{ID: "tem_v41_roles", Name: "Preserved Team", Description: "Live role assignment", Status: entity.ResourceActive}
	role := entity.Role{ID: "rol_v41_assignment", Name: "Team assignment", NameKey: "v41-team-assignment"}
	directRole := entity.Role{ID: "rol_v40_direct", Name: "Direct user role", NameKey: "v40-direct-role"}
	user := entity.User{ID: "usr_v40_direct", Email: "v40-direct@example.invalid", Name: "Direct member", PasswordHash: "unused-test-hash", Role: entity.RoleMember}
	createdTeam, createdUser, createdRole, createdDirectRole := false, false, false, false
	// This helper shares its caller's test and database. Cleanup must run before
	// returning to the caller's baseline assertions, not at parent-test cleanup.
	defer func() {
		cleanup := func(query *gorm.DB, model any) {
			if err := query.Delete(model).Error; err != nil {
				t.Error("cannot clean owned Team role migration fixture", err)
			}
		}
		if createdTeam {
			cleanup(db.Where("team_id = ?", team.ID), &entity.TeamRole{})
		}
		if createdUser && createdDirectRole {
			cleanup(db.Where("user_id = ? AND role_id = ?", user.ID, directRole.ID), &entity.UserRole{})
		}
		if createdDirectRole {
			cleanup(db.Where("role_id = ?", directRole.ID), &entity.RolePermission{})
		}
		if createdUser {
			cleanup(db.Where("id = ?", user.ID), &entity.User{})
		}
		if createdRole {
			cleanup(db.Where("id = ?", role.ID), &entity.Role{})
		}
		if createdDirectRole {
			cleanup(db.Where("id = ?", directRole.ID), &entity.Role{})
		}
		if createdTeam {
			cleanup(db.Where("id = ?", team.ID), &entity.Team{})
		}
	}()
	for _, row := range []any{&team, &role, &directRole, &user} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
		switch row {
		case &team:
			createdTeam = true
		case &role:
			createdRole = true
		case &directRole:
			createdDirectRole = true
		case &user:
			createdUser = true
		}
	}
	direct := entity.UserRole{UserID: user.ID, RoleID: directRole.ID}
	permission := entity.RolePermission{RoleID: directRole.ID, Permission: "projects.read_all"}
	for _, row := range []any{&direct, &permission} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.First(&team, "id = ?", team.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&role, "id = ?", role.ID).Error; err != nil {
		t.Fatal(err)
	}
	var directBefore []entity.UserRole
	var permissionsBefore []entity.RolePermission
	if err := db.Order("user_id, role_id").Find(&directBefore).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Order("role_id, permission").Find(&permissionsBefore).Error; err != nil {
		t.Fatal(err)
	}
	assertPreserved := func() {
		t.Helper()
		var storedTeam entity.Team
		var storedRole entity.Role
		var storedDirect []entity.UserRole
		var storedPermissions []entity.RolePermission
		if err := db.First(&storedTeam, "id = ?", team.ID).Error; err != nil || !reflect.DeepEqual(team, storedTeam) {
			t.Fatal("V41 rewrote existing Team metadata", err)
		}
		if err := db.First(&storedRole, "id = ?", role.ID).Error; err != nil || !reflect.DeepEqual(role, storedRole) {
			t.Fatal("V41 rewrote the referenced role", err)
		}
		if err := db.Order("user_id, role_id").Find(&storedDirect).Error; err != nil || !reflect.DeepEqual(directBefore, storedDirect) {
			t.Fatal("Team assignment migration changed direct user roles", err)
		}
		if err := db.Order("role_id, permission").Find(&storedPermissions).Error; err != nil || !reflect.DeepEqual(permissionsBefore, storedPermissions) {
			t.Fatal("Team assignment migration changed platform permission grants", err)
		}
	}
	removeLedger := func() {
		t.Helper()
		result := db.Table("schema_migrations").Where("version = ?", 41).Delete(&struct{}{})
		if result.Error != nil || result.RowsAffected != 1 {
			t.Fatal("cannot reconstruct V41 ledger", result.Error)
		}
	}
	assignment := entity.TeamRole{TeamID: team.ID, RoleID: role.ID}
	for prefix := 0; prefix <= 4; prefix++ {
		if err := db.Migrator().DropTable(model); err != nil {
			t.Fatal(err)
		}
		removeLedger()
		if prefix > 0 {
			if err := db.Migrator().CreateTable(&teamRoleBareV41Fixture{}); err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&assignment).Error; err != nil {
				t.Fatal(err)
			}
		}
		if prefix > 1 {
			if err := db.Migrator().CreateIndex(model, "idx_team_roles_role"); err != nil {
				t.Fatal(err)
			}
		}
		if prefix > 2 {
			if err := db.Migrator().CreateConstraint(model, "fk_team_roles_team"); err != nil {
				t.Fatal(err)
			}
		}
		if prefix > 3 {
			if err := db.Migrator().CreateConstraint(model, "fk_team_roles_role"); err != nil {
				t.Fatal(err)
			}
		}
		var group sync.WaitGroup
		outcomes := make(chan error, 2)
		for range 2 {
			group.Go(func() { outcomes <- database.Migrate(context.Background(), db) })
		}
		group.Wait()
		close(outcomes)
		for err := range outcomes {
			if err != nil {
				t.Fatal("V41 concurrent prefix recovery failed", prefix, err)
			}
		}
		if prefix == 0 {
			if err := db.Create(&assignment).Error; err != nil {
				t.Fatal(err)
			}
		}
		for _, name := range []string{"fk_team_roles_team", "fk_team_roles_role"} {
			if !db.Migrator().HasConstraint(model, name) {
				t.Fatal("restrictive live assignment FK missing", name)
			}
		}
		if !db.Migrator().HasIndex(model, "idx_team_roles_role") {
			t.Fatal("role assignment lookup index missing")
		}
		var retained entity.TeamRole
		if err := db.First(&retained, "team_id = ? AND role_id = ?", team.ID, role.ID).Error; err != nil || retained.TeamID != assignment.TeamID || retained.RoleID != assignment.RoleID {
			t.Fatal("migration recovery lost existing assignment", err)
		}
		if err := database.Migrate(context.Background(), db); err != nil {
			t.Fatal("V41 repeat execution failed", err)
		}
		assertPreserved()
	}
	if err := db.Create(&assignment).Error; err == nil {
		t.Fatal("duplicate Team/role assignment accepted")
	}
	for _, orphan := range []entity.TeamRole{{TeamID: "tem_v41_missing", RoleID: role.ID}, {TeamID: team.ID, RoleID: "rol_v41_missing"}} {
		if err := db.Create(&orphan).Error; err == nil {
			t.Fatal("orphan live Team/role assignment accepted", orphan)
		}
	}
	if err := db.Delete(&entity.Team{}, "id = ?", team.ID).Error; err == nil {
		t.Fatal("assigned Team deletion must be restricted")
	}
	if err := db.Delete(&entity.Role{}, "id = ?", role.ID).Error; err == nil {
		t.Fatal("assigned role deletion must be restricted")
	}
	if err := db.Model(&entity.Team{}).Where("id = ?", team.ID).Update("id", "tem_v41_renamed").Error; err == nil {
		t.Fatal("assigned Team identity update must be restricted")
	}
	if err := db.Model(&entity.Role{}).Where("id = ?", role.ID).Update("id", "rol_v41_renamed").Error; err == nil {
		t.Fatal("assigned role identity update must be restricted")
	}
	assertPreserved()
	if err := db.Model(&entity.Team{}).Where("id = ?", team.ID).Update("status", entity.ResourceArchived).Error; err != nil {
		t.Fatal("archival must retain the live Team and assignments", err)
	}
	var count int64
	if err := db.Model(model).Where("team_id = ? AND role_id = ?", team.ID, role.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatal("Team archival removed its role assignment", count, err)
	}
	if err := db.Where("team_id = ? AND role_id = ?", team.ID, role.ID).Delete(model).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&entity.Role{}, "id = ?", role.ID).Error; err != nil {
		t.Fatal("unassigned role deletion should remain available", err)
	}
}
