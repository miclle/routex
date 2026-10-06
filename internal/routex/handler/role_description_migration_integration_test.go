package handler

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/secret"
	"gorm.io/gorm"
)

// Proposed V67 scenario: the root registers the immutable migration and appends
// this scenario after the original V66/121 ordered prefix before actual execution.
func testRoleDescriptionMigration(t *testing.T, db *gorm.DB) {
	var baseline []int
	if err := db.Table("schema_migrations").Order("version").Pluck("version", &baseline).Error; err != nil {
		t.Fatal(err)
	}
	without := []int{}
	count := 0
	for i, v := range baseline {
		if i > 0 && v <= baseline[i-1] {
			t.Fatal("nonunique ordered migration baseline")
		}
		if v == 67 {
			count++
		} else {
			without = append(without, v)
		}
	}
	if count != 1 {
		t.Fatal("V67 must be registered exactly once")
	}
	ledger := func(want []int) {
		t.Helper()
		var got []int
		if err := db.Table("schema_migrations").Order("version").Pluck("version", &got).Error; err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("unrelated migration ledger changed", err, got, want)
		}
	}
	remove := func() {
		t.Helper()
		q := db.Table("schema_migrations").Where("version = ?", 67).Delete(&struct{}{})
		if q.Error != nil || q.RowsAffected != 1 {
			t.Fatal("V67 ledger removal", q.Error)
		}
		ledger(without)
	}
	migrate := func() {
		t.Helper()
		if err := database.Migrate(context.Background(), db); err != nil {
			t.Fatal(err)
		}
		ledger(baseline)
	}
	concurrent := func() {
		t.Helper()
		var wg sync.WaitGroup
		failures := make(chan error, 2)
		for range 2 {
			wg.Go(func() { failures <- database.Migrate(context.Background(), db) })
		}
		wg.Wait()
		close(failures)
		for err := range failures {
			if err != nil {
				t.Fatal(err)
			}
		}
		ledger(baseline)
	}
	birth := time.Date(2026, 7, 8, 9, 10, 11, 123000000, time.UTC)
	role := entity.Role{ID: "rol_history67", Name: "Historical exact role", NameKey: secret.SHA256Hex("Historical exact role"), DefinitionRevision: strings.Repeat("a", 64), CreatedAt: birth}
	if err := db.Create(&role).Error; err != nil {
		t.Fatal(err)
	}
	permission := entity.RolePermission{RoleID: role.ID, Permission: "Retained.Mixed_CASE"}
	if err := db.Create(&permission).Error; err != nil {
		t.Fatal(err)
	}
	// Select frozen original columns, avoiding cached SELECT * shape after DDL.
	type retainedRole struct {
		ID, Name, NameKey, DefinitionRevision string
		Builtin                               bool
		CreatedAt                             time.Time
	}
	read := func() retainedRole {
		t.Helper()
		var row retainedRole
		if err := db.Table("roles").Select("ID", "Name", "NameKey", "DefinitionRevision", "Builtin", "CreatedAt").Take(&row, "id = ?", role.ID).Error; err != nil {
			t.Fatal(err)
		}
		row.CreatedAt = row.CreatedAt.UTC()
		return row
	}
	original := read()
	preserved := func() {
		t.Helper()
		got := read()
		if !reflect.DeepEqual(got, original) || !got.CreatedAt.Equal(original.CreatedAt) {
			t.Fatal("historical Role identity/revision/name changed")
		}
		var permissions []entity.RolePermission
		if err := db.Where("role_id = ?", role.ID).Find(&permissions).Error; err != nil || !reflect.DeepEqual(permissions, []entity.RolePermission{permission}) {
			t.Fatal("historical permission changed", err)
		}
	}
	if err := db.Migrator().DropConstraint(&entity.Role{}, "ck_role_description_bytes"); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropColumn(&entity.Role{}, "Description"); err != nil {
		t.Fatal(err)
	}
	remove()
	preserved()
	concurrent()
	preserved()
	var stored entity.Role
	if err := db.Take(&stored, "id = ?", role.ID).Error; err != nil || stored.Description != "" {
		t.Fatal("historical description invented", err)
	}
	migrate()
	concurrent()
	preserved()
	if !db.Migrator().HasConstraint(&entity.Role{}, "ck_role_description_bytes") {
		t.Fatal("portable byte constraint missing")
	}
	allowed := strings.Repeat("界", 666) + "ab"
	if err := db.Model(&entity.Role{}).Where("id = ?", role.ID).UpdateColumn("Description", allowed).Error; err != nil {
		t.Fatal("exact2000-byte description rejected", err)
	}
	for _, value := range []any{strings.Repeat("界", 667), strings.Repeat("a", 2001), nil} {
		if err := db.Model(&entity.Role{}).Where("id = ?", role.ID).UpdateColumn("Description", value).Error; err == nil {
			t.Fatal("invalid stored description accepted")
		}
		if err := db.Take(&stored, "id = ?", role.ID).Error; err != nil || stored.Description != allowed {
			t.Fatal("constraint denial changed persisted description", err)
		}
	}
	// Partial DDL replay recreates only V67's absent check and retains saved text.
	if err := db.Migrator().DropConstraint(&entity.Role{}, "ck_role_description_bytes"); err != nil {
		t.Fatal(err)
	}
	remove()
	concurrent()
	migrate()
	preserved()
	if err := db.Take(&stored, "id = ?", role.ID).Error; err != nil || stored.Description != allowed || !db.Migrator().HasConstraint(&entity.Role{}, "ck_role_description_bytes") {
		t.Fatal("partial DDL lost saved description/check", err)
	}
}
