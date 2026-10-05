package handler

import (
	"context"
	"encoding/json"
	"gorm.io/gorm/schema"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

type roleRevisionUserColumn struct {
	ID                 string  `gorm:"primaryKey;size:30"`
	MemberRoleRevision *string `gorm:"size:64"`
}

func (roleRevisionUserColumn) TableName() string { return "users" }

type roleRevisionDefinitionColumn struct {
	ID                 string  `gorm:"primaryKey;size:30"`
	DefinitionRevision *string `gorm:"size:64"`
}

func (roleRevisionDefinitionColumn) TableName() string { return "roles" }

// Explicit old projections survive deliberate pre-V56 schema reconstruction.
// Existing timestamps, password/security and grant revisions are not backfilled.
func testMemberRoleRevisionMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	zero := strings.Repeat("0", 64)
	userColumns := []string{"id", "email", "name", "password_hash", "role", "disabled", "offboarded_at", "last_login_at", "personal_grant_revision", "created_at", "updated_at"}
	roleColumns := []string{"id", "name", "name_key", "builtin", "created_at"}
	recorded := time.Now().UTC().Truncate(time.Millisecond)
	user := entity.User{ID: "usr_role_migration", Email: "role-history@example.invalid", Name: "Retained role history", PasswordHash: "retained-hash-not-used", Role: entity.RoleMember, Disabled: true, LastLoginAt: &recorded, PersonalGrantRevision: strings.Repeat("a", 64)}
	role := entity.Role{ID: "rol_role_migration", Name: "Retained custom", NameKey: strings.Repeat("b", 64)}
	nullUser := user
	nullUser.ID = "usr_role_partial_null"
	nullUser.Email = "role-null@example.invalid"
	blankUser := user
	blankUser.ID = "usr_role_partial_blank"
	blankUser.Email = "role-blank@example.invalid"

	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&nullUser).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&blankUser).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&role).Error; err != nil {
		t.Fatal(err)
	}
	link := entity.UserRole{UserID: user.ID, RoleID: role.ID}
	permission := entity.RolePermission{RoleID: role.ID, Permission: "prices.read"}
	if err := db.Create(&link).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&permission).Error; err != nil {
		t.Fatal(err)
	}
	read := func(table, id string, columns []string) map[string]any {
		t.Helper()
		row := map[string]any{}
		if err := db.Table(table).Select(columns).Where("id = ?", id).Take(&row).Error; err != nil {
			t.Fatal(err)
		}
		return row
	}
	userBefore, roleBefore := read("users", user.ID, userColumns), read("roles", role.ID, roleColumns)
	nullBefore, blankBefore := read("users", nullUser.ID, userColumns), read("users", blankUser.ID, userColumns)

	session := entity.Session{ID: "ses_role_migration", UserID: user.ID, TokenHash: strings.Repeat("c", 64), ExpiresAt: recorded.Add(time.Hour)}
	if err := db.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	retained := func() map[string][]map[string]any {
		t.Helper()
		out := map[string][]map[string]any{}
		for _, table := range []string{"sessions", "user_roles", "role_permissions", "audit_events", "api_keys", "user_model_grants", "call_records", "call_attempts"} {
			var rows []map[string]any
			if err := db.Table(table).Find(&rows).Error; err != nil {
				t.Fatal(err)
			}
			slices.SortFunc(rows, func(a, b map[string]any) int {
				left, _ := json.Marshal(a)
				right, _ := json.Marshal(b)
				return strings.Compare(string(left), string(right))
			})
			out[table] = rows
		}
		return out
	}
	history := retained()
	assertHistory := func() {
		t.Helper()
		if !reflect.DeepEqual(read("users", user.ID, userColumns), userBefore) || !reflect.DeepEqual(read("roles", role.ID, roleColumns), roleBefore) || !reflect.DeepEqual(retained(), history) || !reflect.DeepEqual(read("users", nullUser.ID, userColumns), nullBefore) || !reflect.DeepEqual(read("users", blankUser.ID, userColumns), blankBefore) {
			t.Fatal("V56 changed timestamps, identity, existing revisions, assignments or history")
		}
	}
	removeLedger := func() {
		t.Helper()
		out := db.Table("schema_migrations").Where("version = ?", 56).Delete(&struct{}{})
		if out.Error != nil || out.RowsAffected != 1 {
			t.Fatal("expected exactly one V56 ledger row", out.RowsAffected, out.Error)
		}
	}
	migrate := func() {
		t.Helper()
		if err := database.Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	userColumn, definitionColumn := &roleRevisionUserColumn{}, &roleRevisionDefinitionColumn{}
	if !db.Migrator().HasColumn(userColumn, "MemberRoleRevision") || !db.Migrator().HasColumn(definitionColumn, "DefinitionRevision") {
		t.Fatal("fresh V56 missing revision columns")
	}
	for _, step := range []struct {
		model        any
		check, field string
	}{{userColumn, "ck_users_member_role_revision", "MemberRoleRevision"}, {definitionColumn, "ck_roles_definition_revision", "DefinitionRevision"}} {
		if !db.Migrator().HasConstraint(step.model, step.check) {
			t.Fatal("fresh revision guard missing", step.check)
		}
	}
	removeLedger()
	for _, step := range []struct {
		model        any
		check, field string
	}{{userColumn, "ck_users_member_role_revision", "MemberRoleRevision"}, {definitionColumn, "ck_roles_definition_revision", "DefinitionRevision"}} {
		if err := db.Migrator().DropConstraint(step.model, step.check); err != nil {
			t.Fatal(err)
		}
		if err := db.Migrator().DropColumn(step.model, step.field); err != nil {
			t.Fatal(err)
		}
	}
	assertHistory()
	migrate()
	assertHistory()
	readRevision := func(model any, id, field string) string {
		t.Helper()
		var revisions []string
		if err := db.Model(model).Where("id = ?", id).Pluck(field, &revisions).Error; err != nil {
			t.Fatal(err)
		}
		if len(revisions) != 1 {
			t.Fatal("missing or nonunique revision", field, len(revisions))
		}
		return revisions[0]
	}
	if readRevision(userColumn, user.ID, "member_role_revision") != zero || readRevision(definitionColumn, role.ID, "definition_revision") != zero {
		t.Fatal("historical revision baseline is not zero")
	}
	chosenUser, chosenRole := strings.Repeat("d", 64), strings.Repeat("e", 64)
	if err := db.Model(userColumn).Where("id = ?", user.ID).UpdateColumn("MemberRoleRevision", chosenUser).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(definitionColumn).Where("id = ?", role.ID).UpdateColumn("DefinitionRevision", chosenRole).Error; err != nil {
		t.Fatal(err)
	}
	removeLedger()
	migrate()
	migrate()
	assertHistory()
	removeLedger()
	var group sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		group.Go(func() { results <- database.Migrate(ctx, db) })
	}
	group.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if readRevision(userColumn, user.ID, "member_role_revision") != chosenUser || readRevision(definitionColumn, role.ID, "definition_revision") != chosenRole {
		t.Fatal("repeat/concurrent migration changed nonzero generations")
	}
	assertHistory()
	// Partial DDL: one existing nullable column with NULL/blank retained values,
	// one missing column. Recover without replacing valid current generations.
	removeLedger()
	if err := db.Migrator().DropConstraint(userColumn, "ck_users_member_role_revision"); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropColumn(userColumn, "MemberRoleRevision"); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().AddColumn(userColumn, "MemberRoleRevision"); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(userColumn).Where("id = ?", user.ID).UpdateColumn("MemberRoleRevision", chosenUser).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(userColumn).Where("id = ?", blankUser.ID).UpdateColumn("MemberRoleRevision", "").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropConstraint(definitionColumn, "ck_roles_definition_revision"); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropColumn(definitionColumn, "DefinitionRevision"); err != nil {
		t.Fatal(err)
	}
	migrate()
	assertHistory()
	if readRevision(userColumn, user.ID, "member_role_revision") != chosenUser || readRevision(definitionColumn, role.ID, "definition_revision") != zero {
		t.Fatal("partial DDL rewrote valid generation or omitted baseline")
	}
	if readRevision(userColumn, nullUser.ID, "member_role_revision") != zero || readRevision(userColumn, blankUser.ID, "member_role_revision") != zero {
		t.Fatal("partial NULL/blank baseline not repaired")
	}
	for _, step := range []struct {
		model        any
		field, check string
	}{{userColumn, "member_role_revision", "ck_users_member_role_revision"}, {definitionColumn, "definition_revision", "ck_roles_definition_revision"}} {
		if !db.Migrator().HasConstraint(step.model, step.check) {
			t.Fatal("repair failed constraint", step.check)
		}
		cols, err := db.Migrator().ColumnTypes(step.model)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, col := range cols {
			if col.Name() == step.field {
				found = true
				n, known := col.Nullable()
				length, sized := col.Length()
				if !known || n || !sized || length != 64 {
					t.Fatal("invalid repaired revision column", step.field)
				}
			}
		}
		if !found {
			t.Fatal("missing repaired column", step.field)
		}
		id := user.ID
		if step.field == "definition_revision" {
			id = role.ID
		}
		for _, invalid := range []any{nil, "", "f", strings.Repeat("A", 64), strings.Repeat("g", 64), strings.Repeat("a", 63) + " ", strings.Repeat("a", 65)} {
			if err := db.Model(step.model).Where("id = ?", id).UpdateColumn(step.field, invalid).Error; err == nil {
				t.Fatal("revision constraint accepted ambiguous generation", step.field, invalid)
			}
		}
	}
	assertHistory()
}

func TestMemberRoleRevisionFixtureSchema(t *testing.T) {
	for _, test := range []struct {
		model                any
		field, column, table string
	}{{roleRevisionUserColumn{}, "MemberRoleRevision", "member_role_revision", "users"}, {roleRevisionDefinitionColumn{}, "DefinitionRevision", "definition_revision", "roles"}} {
		parsed, err := schema.Parse(test.model, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		field := parsed.FieldsByName[test.field]
		if parsed.Table != test.table || field == nil || field.DBName != test.column || field.NotNull || field.HasDefaultValue {
			t.Fatal("partial-DDL fixture must use the exact nullable column", test.field)
		}
	}
}
