package handler

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

// This projection is frozen before the approval link: no prepared SELECT * result
// is reused across the deliberate additive-column reconstruction.
type approvalMigrationHistoricalUser struct {
	ID, Email, Name, PasswordHash, Role, MemberRoleRevision, PersonalGrantRevision string
	Disabled                                                                       bool
	OffboardedAt, LastLoginAt                                                      *time.Time
	CreatedAt, UpdatedAt                                                           time.Time
}

func (approvalMigrationHistoricalUser) TableName() string { return "users" }

type approvalMigrationUserColumn struct {
	ID                    string  `gorm:"primaryKey;size:30"`
	ApprovalApplicationID *string `gorm:"size:30"`
}

func (approvalMigrationUserColumn) TableName() string { return "users" }

type approvalMigrationPolicyColumns struct {
	ID                           int    `gorm:"primaryKey"`
	RegistrationApprovalRequired bool   `gorm:"not null;default:false"`
	RegistrationPolicyRevision   string `gorm:"size:64;not null;default:0000000000000000000000000000000000000000000000000000000000000000"`
}

func (approvalMigrationPolicyColumns) TableName() string { return "governance_settings" }

type approvalMigrationHistoricalPolicy struct {
	ID                  int
	RegistrationEnabled bool
}

func (approvalMigrationHistoricalPolicy) TableName() string { return "governance_settings" }

type approvalMigrationLedgerRow struct {
	Version   int
	AppliedAt string
}

// Root registers the future additive migration last. The fixture discovers that
// exact ledger row and baseline; it reserves no version and hardcodes no total.
func testLocalRegistrationApprovalMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	var baselineLedger []approvalMigrationLedgerRow
	if err := db.Table("schema_migrations").Order("version").Find(&baselineLedger).Error; err != nil || len(baselineLedger) == 0 {
		t.Fatal("missing migrated ledger baseline", err)
	}
	version := baselineLedger[len(baselineLedger)-1].Version
	if !db.Migrator().HasTable(&entity.RegistrationApprovalApplication{}) || !db.Migrator().HasColumn(&approvalMigrationUserColumn{}, "ApprovalApplicationID") {
		t.Fatal("root did not register approval as the final additive migration")
	}
	timestamp := time.Now().UTC().Truncate(time.Microsecond)
	ids := []string{"usr_approval_hist_a", "usr_approval_hist_b", "usr_approval_hist_c"}
	for i, id := range ids {
		user := entity.User{ID: id, Email: id + "@example.invalid", Name: "Retained approval history", PasswordHash: "retained-only-not-login", Role: entity.RoleMember, Disabled: i > 0, PersonalGrantRevision: strings.Repeat("0", 64), MemberRoleRevision: strings.Repeat("0", 64)}
		if i == 2 {
			user.OffboardedAt = &timestamp
			user.LastLoginAt = &timestamp
		}
		if err := db.Create(&user).Error; err != nil {
			t.Fatal(err)
		}
	}
	readHistory := func() []approvalMigrationHistoricalUser {
		t.Helper()
		var rows []approvalMigrationHistoricalUser
		if err := db.Select("ID", "Email", "Name", "PasswordHash", "Role", "MemberRoleRevision", "PersonalGrantRevision", "Disabled", "OffboardedAt", "LastLoginAt", "CreatedAt", "UpdatedAt").Where("id IN ?", ids).Order("id").Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		return rows
	}
	historical := readHistory()
	if err := db.Model(&entity.GovernanceSetting{}).Where("id = ?", 1).UpdateColumn("RegistrationEnabled", true).Error; err != nil {
		t.Fatal(err)
	}
	readPolicyHistory := func() []approvalMigrationHistoricalPolicy {
		t.Helper()
		var rows []approvalMigrationHistoricalPolicy
		if err := db.Select("ID", "RegistrationEnabled").Order("id").Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		return rows
	}
	policyHistory := readPolicyHistory()
	key := entity.APIKey{ID: "key_approval_history", UserID: ids[0], Name: "Retained Key", Prefix: "rx_fixture", TokenHash: strings.Repeat("a", 64), Status: entity.KeyRevoked}
	session := entity.Session{ID: "ses_approval_history", UserID: ids[0], TokenHash: strings.Repeat("b", 64), ExpiresAt: timestamp.Add(time.Hour)}
	if err := db.Create(&key).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	readKey := func() entity.APIKey {
		t.Helper()
		var row entity.APIKey
		if err := db.First(&row, "id = ?", key.ID).Error; err != nil {
			t.Fatal(err)
		}
		return row
	}
	readSession := func() entity.Session {
		t.Helper()
		var row entity.Session
		if err := db.First(&row, "id = ?", session.ID).Error; err != nil {
			t.Fatal(err)
		}
		return row
	}
	keyBefore, sessionBefore := readKey(), readSession()
	removeLedger := func() {
		t.Helper()
		q := db.Table("schema_migrations").Where("version = ?", version).Delete(&struct{}{})
		if q.Error != nil || q.RowsAffected != 1 {
			t.Fatal("approval ledger reconstruction", q.Error, q.RowsAffected)
		}
	}
	migrate := func() {
		t.Helper()
		if err := database.Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	assertRetained := func() {
		t.Helper()
		if !reflect.DeepEqual(readHistory(), historical) || !reflect.DeepEqual(readPolicyHistory(), policyHistory) || !reflect.DeepEqual(readKey(), keyBefore) || !reflect.DeepEqual(readSession(), sessionBefore) {
			t.Fatal("approval migration mutated historical identity, policy, credential or Session")
		}
		var ledger []approvalMigrationLedgerRow
		if err := db.Table("schema_migrations").Order("version").Find(&ledger).Error; err != nil {
			t.Fatal(err)
		}
		if len(ledger) != len(baselineLedger) || ledger[len(ledger)-1].Version != version || !reflect.DeepEqual(ledger[:len(ledger)-1], baselineLedger[:len(baselineLedger)-1]) {
			t.Fatal("approval migration changed released ledger or duplicated its row")
		}
	}
	removeLedger()
	if err := db.Migrator().DropTable(&entity.RegistrationApprovalApplication{}); err != nil {
		t.Fatal(err)
	}
	if db.Migrator().HasConstraint(&approvalMigrationPolicyColumns{}, "ck_registration_policy_revision") {
		if err := db.Migrator().DropConstraint(&approvalMigrationPolicyColumns{}, "ck_registration_policy_revision"); err != nil {
			t.Fatal(err)
		}
	}
	for _, column := range []struct {
		model any
		field string
	}{{&approvalMigrationUserColumn{}, "ApprovalApplicationID"}, {&approvalMigrationPolicyColumns{}, "RegistrationApprovalRequired"}, {&approvalMigrationPolicyColumns{}, "RegistrationPolicyRevision"}} {
		if err := db.Migrator().DropColumn(column.model, column.field); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(readHistory(), historical) || !reflect.DeepEqual(readPolicyHistory(), policyHistory) {
		t.Fatal("pre-approval reconstruction changed persisted history")
	}
	migrate()
	assertRetained()
	var rows []approvalMigrationUserColumn
	if err := db.Where("id IN ?", ids).Find(&rows).Error; err != nil || len(rows) != len(ids) {
		t.Fatal("historical link projection", err)
	}
	for _, row := range rows {
		if row.ApprovalApplicationID != nil {
			t.Fatal("historical account assigned fabricated application")
		}
	}
	var policy entity.GovernanceSetting
	if err := db.First(&policy, "id = ?", 1).Error; err != nil || policy.RegistrationApprovalRequired {
		t.Fatal("historical approval default changed", err)
	}
	var applications int64
	if err := db.Model(&entity.RegistrationApprovalApplication{}).Count(&applications).Error; err != nil || applications != 0 {
		t.Fatal("migration invented historical approvals", err)
	}
	// Each independently interrupted additive DDL boundary must reenter safely.
	for _, column := range []struct {
		model any
		field string
	}{{&approvalMigrationUserColumn{}, "ApprovalApplicationID"}, {&approvalMigrationPolicyColumns{}, "RegistrationApprovalRequired"}, {&approvalMigrationPolicyColumns{}, "RegistrationPolicyRevision"}} {
		removeLedger()
		if column.field == "RegistrationPolicyRevision" && db.Migrator().HasConstraint(&approvalMigrationPolicyColumns{}, "ck_registration_policy_revision") {
			if err := db.Migrator().DropConstraint(&approvalMigrationPolicyColumns{}, "ck_registration_policy_revision"); err != nil {
				t.Fatal(err)
			}
		}
		if err := db.Migrator().DropColumn(column.model, column.field); err != nil {
			t.Fatal(err)
		}
		migrate()
		assertRetained()
	}
	if !db.Migrator().HasIndex(&entity.RegistrationApprovalApplication{}, "uidx_registration_approval_user") {
		t.Fatal("unique exact application owner index missing")
	}
	for _, name := range []string{"ck_registration_approval_id", "ck_registration_approval_state", "ck_registration_approval_decision", "ck_registration_approval_revision"} {
		if !db.Migrator().HasConstraint(&entity.RegistrationApprovalApplication{}, name) {
			t.Fatal("approval constraint absent", name)
		}
	}
	if !db.Migrator().HasConstraint(&approvalMigrationPolicyColumns{}, "ck_registration_policy_revision") {
		t.Fatal("policy revision constraint absent")
	}
	var owner entity.User
	if err := db.First(&owner, "id = ?", ids[0]).Error; err != nil {
		t.Fatal(err)
	}
	application := entity.RegistrationApprovalApplication{ID: "raa_01j00000000000000000000001", UserID: owner.ID, UserCreatedAt: owner.CreatedAt, CreatedAt: timestamp, State: "pending", Revision: strings.Repeat("0", 64)}
	if err := db.Create(&application).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&approvalMigrationUserColumn{}).Where("id = ?", owner.ID).UpdateColumn("ApprovalApplicationID", application.ID).Error; err != nil {
		t.Fatal(err)
	}
	readApplication := func() entity.RegistrationApprovalApplication {
		t.Helper()
		var row entity.RegistrationApprovalApplication
		if err := db.First(&row, "id = ?", application.ID).Error; err != nil {
			t.Fatal(err)
		}
		return row
	}
	applicationBefore := readApplication()
	// Repair of a deliberately missing builtin grant must advance only the
	// corresponding definition revision. Reentry after that repair is a no-op.
	var priorRole entity.Role
	if err := db.First(&priorRole, "id = ?", "rol_admin").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("role_id = ? AND permission = ?", "rol_admin", "members.approvals.write").Delete(&entity.RolePermission{}).Error; err != nil {
		t.Fatal(err)
	}
	removeLedger()
	migrate()
	assertRetained()
	var repairedRole entity.Role
	if err := db.First(&repairedRole, "id = ?", "rol_admin").Error; err != nil {
		t.Fatal(err)
	}
	if repairedRole.DefinitionRevision == priorRole.DefinitionRevision {
		t.Fatal("builtin approval grant repair did not fence definition review")
	}
	priorRole.DefinitionRevision = repairedRole.DefinitionRevision
	if !reflect.DeepEqual(priorRole, repairedRole) {
		t.Fatal("builtin grant repair changed unrelated role metadata")
	}
	var repairedGrant int64
	if err := db.Model(&entity.RolePermission{}).Where("role_id = ? AND permission = ?", "rol_admin", "members.approvals.write").Count(&repairedGrant).Error; err != nil || repairedGrant != 1 {
		t.Fatal("builtin approval grant repair differs", err)
	}
	var roleBefore entity.Role
	if err := db.First(&roleBefore, "id = ?", "rol_admin").Error; err != nil {
		t.Fatal(err)
	}
	var permissionsBefore []entity.RolePermission
	if err := db.Where("role_id = ?", "rol_admin").Order("permission").Find(&permissionsBefore).Error; err != nil {
		t.Fatal(err)
	}
	removeLedger()
	migrate()
	migrate()
	assertRetained()
	removeLedger()
	var wg sync.WaitGroup
	failures := make(chan error, 2)
	for range 2 {
		wg.Go(func() { failures <- database.Migrate(ctx, db) })
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	assertRetained()
	var roleAfter entity.Role
	var permissionsAfter []entity.RolePermission
	if err := db.First(&roleAfter, "id = ?", "rol_admin").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("role_id = ?", "rol_admin").Order("permission").Find(&permissionsAfter).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(roleBefore, roleAfter) || !reflect.DeepEqual(permissionsBefore, permissionsAfter) || !reflect.DeepEqual(applicationBefore, readApplication()) {
		t.Fatal("repeat/concurrent migration mutated retained role definition, grant or application")
	}
	var link approvalMigrationUserColumn
	if err := db.First(&link, "id = ?", owner.ID).Error; err != nil || link.ApprovalApplicationID == nil || *link.ApprovalApplicationID != application.ID {
		t.Fatal("repeat migration lost immutable user link", err)
	}
	if err := db.Where("id = ?", owner.ID).Delete(&entity.User{}).Error; !errors.Is(err, gorm.ErrForeignKeyViolated) {
		t.Fatal("application owner removal bypassed retained FK", err)
	}
	duplicate := application
	duplicate.ID = "raa_01j00000000000000000000002"
	if err := db.Create(&duplicate).Error; !errors.Is(err, gorm.ErrDuplicatedKey) {
		t.Fatal("multiple applications accepted for exact User", err)
	}
	foreign := application
	foreign.ID = "raa_01j00000000000000000000003"
	foreign.UserID = "usr_approval_absent"
	if err := db.Create(&foreign).Error; !errors.Is(err, gorm.ErrForeignKeyViolated) {
		t.Fatal("absent application owner bypassed FK", err)
	}
	invalid := application
	invalid.ID = "raa_01j00000000000000000000004"
	invalid.UserID = ids[1]
	invalid.State = "unknown"
	if err := db.Create(&invalid).Error; err == nil {
		t.Fatal("unknown application state accepted")
	}
	invalid.State = "rejected"
	if err := db.Create(&invalid).Error; err == nil {
		t.Fatal("terminal application without recorded decision accepted")
	}
	invalid.State = "pending"
	invalid.ID = strings.ToUpper("raa_01j00000000000000000000004")
	if err := db.Create(&invalid).Error; err == nil {
		t.Fatal("noncanonical case-alias application identifier accepted")
	}
	invalid.ID = "raa_01j00000000000000000000004"
	invalid.State = "pending"
	invalid.Revision = "BAD"
	if err := db.Create(&invalid).Error; err == nil {
		t.Fatal("malformed application revision accepted")
	}
	assertRetained()
	if !reflect.DeepEqual(applicationBefore, readApplication()) {
		t.Fatal("invalid constraints changed retained application")
	}
}
