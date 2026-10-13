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

// These independent old-field data projections never define historical DDL.
// Explicit selected fields also avoid prepared SELECT * across owned additive DDL.
type deliveryMigrationPersonalV101 struct {
	LifecycleRevision string
	ID                string
	UserID            string
	Name              string
	Prefix            string
	TokenHash         string
	Status            string
	ExpiresAt         *time.Time
	DeliveryExpiresAt *time.Time
	ReplacesKeyID     *string
	ActivateOnConfirm bool
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func (deliveryMigrationPersonalV101) TableName() string { return "api_keys" }

type deliveryMigrationProjectV101 struct {
	ID                string
	ProjectID         string
	CreatorID         string
	Name              string
	Prefix            string
	TokenHash         string
	Status            string
	DeliveryMode      string
	ExpiresAt         *time.Time
	DeliveryExpiresAt *time.Time
	ReplacesKeyID     *string
	ActivateOnConfirm bool
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func (deliveryMigrationProjectV101) TableName() string { return "project_api_keys" }

// Intentionally incompatible physical shapes are used only by Migrator controls.
type deliveryMigrationConsumerWidth struct {
	ConsumerID string `gorm:"column:consumer_id;size:63;not null"`
}

func (deliveryMigrationConsumerWidth) TableName() string { return "api_key_delivery_consumers" }

type deliveryMigrationPolicyPrecision struct {
	VerifiedAt *time.Time `gorm:"column:verified_at;precision:3"`
}

func (deliveryMigrationPolicyPrecision) TableName() string { return "api_key_delivery_policies" }

type deliveryMigrationReversedConsumerIndex struct {
	OwnerDigest string `gorm:"column:owner_digest;uniqueIndex:idx_api_key_delivery_consumer_owner,priority:2"`
	ConsumerID  string `gorm:"column:consumer_id;uniqueIndex:idx_api_key_delivery_consumer_owner,priority:1"`
}

func (deliveryMigrationReversedConsumerIndex) TableName() string { return "api_key_delivery_consumers" }

func testAPIKeyDeliveryMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	db = db.WithContext(ctx)
	legacyForcedSSOBeforeV102(t, db)
	original := personalKeyBehaviorLedger(t, db)
	if len(original) != 102 {
		t.Fatal("delivery migration requires exact current102")
	}
	for i, row := range original {
		if row.Version != i+1 {
			t.Fatal("delivery migration noncontiguous ledger")
		}
	}
	// Harness reset and normal startup produce this state. No test inserts a policy
	// to make an otherwise missing application prerequisite appear configured.
	var initial entity.APIKeyDeliveryPolicy
	if db.Session(&gorm.Session{QueryFields: true}).Take(&initial, "id = ?", 1).Error != nil {
		t.Fatal("fresh migration missing manual policy")
	}
	expected := entity.APIKeyDeliveryPolicy{ID: 1, ReviewRevision: strings.Repeat("0", 64), SecurityRevision: strings.Repeat("0", 64), AllowManual: true, DefaultMode: "manual", CreatedAt: initial.CreatedAt, UpdatedAt: initial.UpdatedAt}
	if initial.CreatedAt.IsZero() || initial.UpdatedAt.IsZero() || !reflect.DeepEqual(initial, expected) {
		t.Fatal("fresh migration seeded managed authority")
	}
	for _, table := range deliveryCompatibilityTablesV102 {
		var count int64
		want := int64(0)
		if table == "api_key_delivery_policies" {
			want = 1
		}
		if !db.Migrator().HasTable(table) || db.Table(table).Count(&count).Error != nil || count != want {
			t.Fatal("fresh delivery table inventory", table)
		}
	}
	legacyDeliveryBeforeV101(t, db)
	prefix := personalKeyBehaviorLedger(t, db)
	if len(prefix) != 101 || !reflect.DeepEqual(prefix, original[:101]) {
		t.Fatal("pristine rewind changed101 receipts")
	}
	stamp := time.Now().UTC().Truncate(time.Microsecond)
	// Real retained owner rows satisfy the existing immutable foreign keys. No
	// service login/READY/native authority is inferred from these migration fixtures.
	user := entity.User{ID: "usr_delivery_old", Email: "delivery-migration@example.test", Name: "Migration fixture", PasswordHash: "not-a-login-password", Role: entity.RoleMember, CreatedAt: stamp, UpdatedAt: stamp}
	ownerProject := entity.Project{ID: "prj_delivery_old", Name: "Migration project", Status: "active", CreatorID: user.ID, CreatedAt: stamp, UpdatedAt: stamp}
	if db.Create(&user).Error != nil || db.Create(&ownerProject).Error != nil {
		t.Fatal("seed retained Key owners")
	}
	personal := deliveryMigrationPersonalV101{LifecycleRevision: "klr_delivery_old", ID: "key_delivery_old", UserID: "usr_delivery_old", Name: "Retained personal", Prefix: "rx_old", TokenHash: strings.Repeat("a", 64), Status: entity.KeyActive, ActivateOnConfirm: true, CreatedAt: stamp, UpdatedAt: stamp}
	project := deliveryMigrationProjectV101{ID: "pky_delivery_old", ProjectID: "prj_delivery_old", CreatorID: "usr_delivery_old", Name: "Retained project", Prefix: "rx_project", TokenHash: strings.Repeat("b", 64), Status: entity.KeyDisabled, DeliveryMode: "manual", CreatedAt: stamp, UpdatedAt: stamp}
	if db.Create(&personal).Error != nil || db.Create(&project).Error != nil {
		t.Fatal("seed retained pre102 Key rows")
	}
	if db.Session(&gorm.Session{QueryFields: true}).Take(&personal, "id = ?", personal.ID).Error != nil || db.Session(&gorm.Session{QueryFields: true}).Take(&project, "id = ?", project.ID).Error != nil {
		t.Fatal("capture persisted pre102 Key facts")
	}
	// This same named CHECK must still be manual-only at the historical boundary.
	if !db.Migrator().HasConstraint(&deliveryHistoricalProjectModeV101{}, "ck_project_keys_delivery") || db.Model(&deliveryMigrationProjectV101{}).Where("id = ?", project.ID).Update("delivery_mode", "managed_vault").Error == nil {
		t.Fatal("historical101 Project mode widened")
	}
	assertRetainedRootInventoryV8(t, db, "delivery_upgrade", stamp)
	var rootJobs []entity.SecretRotationJob
	var rootProofs []entity.SecretProcessVerification
	if db.Session(&gorm.Session{QueryFields: true}).Where("inventory_version = ?", 8).Order("id").Find(&rootJobs).Error != nil || db.Session(&gorm.Session{QueryFields: true}).Where("inventory_version = ?", 8).Order("process_id").Find(&rootProofs).Error != nil {
		t.Fatal("capture retained V8 root history")
	}
	checkRetained := func() {
		t.Helper()
		rows := personalKeyBehaviorLedger(t, db)
		if len(rows) != 102 || rows[101].Version != 102 || !reflect.DeepEqual(rows[:101], prefix) {
			t.Fatal("V102 changed historical ledger receipts")
		}
		var gotPersonal deliveryMigrationPersonalV101
		var gotProject deliveryMigrationProjectV101
		if db.Session(&gorm.Session{QueryFields: true}).Take(&gotPersonal, "id = ?", personal.ID).Error != nil || !reflect.DeepEqual(gotPersonal, personal) || db.Session(&gorm.Session{QueryFields: true}).Take(&gotProject, "id = ?", project.ID).Error != nil || !reflect.DeepEqual(gotProject, project) {
			t.Fatal("V102 changed retained Key facts")
		}
		var mode struct{ DeliveryMode string }
		if db.Table("api_keys").Select("delivery_mode").Where("id = ?", personal.ID).Take(&mode).Error != nil || mode.DeliveryMode != "manual" {
			t.Fatal("old Personal Key not backfilled manual")
		}
		var jobs []entity.SecretRotationJob
		var proofs []entity.SecretProcessVerification
		if db.Session(&gorm.Session{QueryFields: true}).Where("inventory_version = ?", 8).Order("id").Find(&jobs).Error != nil || !reflect.DeepEqual(jobs, rootJobs) || db.Session(&gorm.Session{QueryFields: true}).Where("inventory_version = ?", 8).Order("process_id").Find(&proofs).Error != nil || !reflect.DeepEqual(proofs, rootProofs) {
			t.Fatal("V8 root history promoted or changed")
		}
	}
	safeCategory := func(err error) string {
		for _, v := range []string{"nonempty partial API-Key delivery table", "invalid API-Key delivery column set", "invalid API-Key delivery primary key", "invalid API-Key delivery nullability", "invalid API-Key delivery default", "invalid API-Key delivery string", "invalid API-Key delivery text", "invalid API-Key delivery timestamp", "invalid API-Key delivery integer", "invalid API-Key delivery boolean", "invalid retained API-Key delivery facts", "invalid API-Key delivery index"} {
			if strings.HasSuffix(err.Error(), v) {
				return v
			}
		}
		return "unclassified_migration_error"
	}
	migrate := func(stage string) {
		t.Helper()
		if err := database.MigrateThrough(ctx, db, 102); err != nil {
			t.Fatalf("delivery migration: stage=%s category=%s", stage, safeCategory(err))
		}
		checkRetained()
	}
	dropReceipt := func() {
		t.Helper()
		r := db.Table("schema_migrations").Where("version = ?", 102).Delete(&struct{}{})
		if r.Error != nil || r.RowsAffected != 1 || !reflect.DeepEqual(personalKeyBehaviorLedger(t, db), prefix) {
			t.Fatal("remove only102 receipt")
		}
	}
	rejectMigration := func(stage string) {
		t.Helper()
		if database.MigrateThrough(ctx, db, 102) == nil {
			t.Fatal("incompatible partial delivery schema accepted", stage)
		}
		if !reflect.DeepEqual(personalKeyBehaviorLedger(t, db), prefix) {
			t.Fatal("failed delivery migration recorded success", stage)
		}
	}
	migrate("upgrade_from101")
	var seeded entity.APIKeyDeliveryPolicy
	if db.Session(&gorm.Session{QueryFields: true}).Take(&seeded, "id = ?", 1).Error != nil {
		t.Fatal("upgraded policy missing")
	}
	expected.CreatedAt, expected.UpdatedAt = seeded.CreatedAt, seeded.UpdatedAt
	if seeded.CreatedAt.IsZero() || seeded.UpdatedAt.IsZero() || !reflect.DeepEqual(seeded, expected) {
		t.Fatal("upgrade seeded delivery trust")
	}
	current := personalKeyBehaviorLedger(t, db)
	migrate("repeat")
	if !reflect.DeepEqual(current, personalKeyBehaviorLedger(t, db)) {
		t.Fatal("repeat rewrote102 receipt")
	}
	dropReceipt()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() { results <- database.MigrateThrough(ctx, db, 102) })
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("delivery concurrent migration category=%s", safeCategory(err))
		}
	}
	checkRetained()
	afterConcurrent := personalKeyBehaviorLedger(t, db)
	migrate("repeat_after_concurrent")
	if !reflect.DeepEqual(afterConcurrent, personalKeyBehaviorLedger(t, db)) {
		t.Fatal("concurrent receipt not durable")
	}
	// An empty partially applied table is repairable using the frozen migration.
	if db.Migrator().DropConstraint(&entity.APIKeyCallerTemplate{}, "ck_api_key_caller_template_ttl") != nil || db.Migrator().DropColumn(&entity.APIKeyCallerTemplate{}, "TokenMaxTTLSeconds") != nil {
		t.Fatal("construct empty partial template")
	}
	dropReceipt()
	migrate("empty_missing_column")
	// Nonempty means reject before synthesizing any omitted field, even nullable.
	if db.Migrator().DropConstraint(&entity.APIKeyDeliveryPolicy{}, "ck_api_key_delivery_policy_modes") != nil || db.Migrator().DropColumn(&entity.APIKeyDeliveryPolicy{}, "VerifiedAt") != nil {
		t.Fatal("construct nonempty partial policy")
	}
	dropReceipt()
	rejectMigration("nonempty_missing_column")
	var policyCount int64
	if db.Migrator().HasColumn(&entity.APIKeyDeliveryPolicy{}, "VerifiedAt") || db.Model(&entity.APIKeyDeliveryPolicy{}).Count(&policyCount).Error != nil || policyCount != 1 {
		t.Fatal("failed partial migration changed policy row/column")
	}
	if db.Migrator().AddColumn(&entity.APIKeyDeliveryPolicy{}, "VerifiedAt") != nil {
		t.Fatal("restore owned nullable policy column")
	}
	migrate("restored_nonempty_column")
	var restored entity.APIKeyDeliveryPolicy
	if db.Session(&gorm.Session{QueryFields: true}).Take(&restored, "id = ?", 1).Error != nil || !reflect.DeepEqual(restored, seeded) {
		t.Fatal("partial retry changed manual policy")
	}
	if db.Migrator().AlterColumn(&deliveryMigrationConsumerWidth{}, "ConsumerID") != nil {
		t.Fatal("construct wrong Consumer width")
	}
	dropReceipt()
	rejectMigration("wrong_width")
	if db.Migrator().AlterColumn(&entity.APIKeyDeliveryConsumer{}, "ConsumerID") != nil {
		t.Fatal("restore Consumer width")
	}
	migrate("restored_width")
	if db.Migrator().AlterColumn(&deliveryMigrationPolicyPrecision{}, "VerifiedAt") != nil {
		t.Fatal("construct wrong optional precision")
	}
	dropReceipt()
	rejectMigration("wrong_precision")
	if db.Migrator().AlterColumn(&entity.APIKeyDeliveryPolicy{}, "VerifiedAt") != nil {
		t.Fatal("restore optional precision")
	}
	migrate("restored_precision")
	const consumerIndex = "idx_api_key_delivery_consumer_owner"
	if database.DropIndex(db, &entity.APIKeyDeliveryConsumer{}, consumerIndex) != nil {
		t.Fatal("drop owned Consumer index")
	}
	dropReceipt()
	migrate("missing_index")
	if database.DropIndex(db, &entity.APIKeyDeliveryConsumer{}, consumerIndex) != nil || db.Migrator().CreateIndex(&deliveryMigrationReversedConsumerIndex{}, consumerIndex) != nil {
		t.Fatal("construct reversed Consumer index")
	}
	dropReceipt()
	rejectMigration("wrong_index_order")
	if database.DropIndex(db, &entity.APIKeyDeliveryConsumer{}, consumerIndex) != nil || db.Migrator().CreateIndex(&entity.APIKeyDeliveryConsumer{}, consumerIndex) != nil {
		t.Fatal("restore exact Consumer index")
	}
	migrate("restored_index")
	// Explicit fixture rows below test physical constraints only. They never prove
	// machine validation, control/isolation, remote acknowledgement or app READY.
	consumer := entity.APIKeyDeliveryConsumer{ID: "kdc_delivery_fixture", OwnerKind: "personal", OwnerID: personal.UserID, OwnerBirth: personal.CreatedAt, OwnerDigest: strings.Repeat("c", 64), ConsumerID: "fixture-consumer", ApplicationName: "Fixture", CreatedAt: stamp, UpdatedAt: stamp}
	if db.Create(&consumer).Error != nil {
		t.Fatal("positive Consumer before duplicate denial")
	}
	var savedConsumer entity.APIKeyDeliveryConsumer
	if db.Session(&gorm.Session{QueryFields: true}).Take(&savedConsumer, "id = ?", consumer.ID).Error != nil {
		t.Fatal("capture positive Consumer")
	}
	duplicate := consumer
	duplicate.ID = "kdc_delivery_duplicate"
	if db.Create(&duplicate).Error == nil {
		t.Fatal("duplicate owner Consumer admitted")
	}
	for _, bad := range []string{"PERSONAL", "personal ", "unknown"} {
		if db.Model(&entity.APIKeyDeliveryConsumer{}).Where("id = ?", consumer.ID).Update("owner_kind", bad).Error == nil {
			t.Fatal("invalid exact Consumer owner admitted")
		}
		var got entity.APIKeyDeliveryConsumer
		if db.Session(&gorm.Session{QueryFields: true}).Take(&got, "id = ?", consumer.ID).Error != nil || !reflect.DeepEqual(got, savedConsumer) {
			t.Fatal("Consumer denial changed original row")
		}
	}
	template := entity.APIKeyCallerTemplate{ID: "jwt", ReviewRevision: strings.Repeat("d", 64), SecurityRevision: strings.Repeat("e", 64), AuthMount: "jwt", Issuer: "https://issuer.invalid", JWKSURL: "https://issuer.invalid/jwks", Audience: "fixture", AllowedSubjectsJSON: `["fixture"]`, TrustSecurityRevision: strings.Repeat("f", 64), TokenTTLSeconds: 30, TokenMaxTTLSeconds: 60, CreatedAt: stamp, UpdatedAt: stamp}
	if db.Create(&template).Error != nil {
		t.Fatal("positive template before TTL denials")
	}
	var savedTemplate entity.APIKeyCallerTemplate
	if db.Session(&gorm.Session{QueryFields: true}).Take(&savedTemplate, "id = ?", "jwt").Error != nil {
		t.Fatal("capture positive template")
	}
	for _, change := range []map[string]any{{"token_ttl_seconds": 0}, {"token_ttl_seconds": 61}, {"token_max_ttl_seconds": 3601}, {"id": "JWT"}} {
		if db.Model(&entity.APIKeyCallerTemplate{}).Where("id = ?", "jwt").Updates(change).Error == nil {
			t.Fatal("invalid template envelope admitted")
		}
		var got entity.APIKeyCallerTemplate
		if db.Session(&gorm.Session{QueryFields: true}).Take(&got, "id = ?", "jwt").Error != nil || !reflect.DeepEqual(got, savedTemplate) {
			t.Fatal("template denial changed original row")
		}
	}
	command := entity.APIKeyDeliveryCommand{ID: "kdo_delivery_fixture", OwnerKind: "personal", OwnerID: personal.UserID, OwnerBirth: stamp, OwnerDigest: strings.Repeat("c", 64), RequestID: "00000000-0000-4000-8000-000000000001", ActorID: personal.UserID, ActorBirth: stamp, SessionID: "ses_delivery_fixture", SessionBirth: stamp, Purpose: "resolve", IntentDigest: strings.Repeat("a", 64), ReviewRevision: strings.Repeat("b", 64), ProfileID: "vap_delivery_fixture", ProfileBirth: stamp, PrincipalID: "vpb_delivery_fixture", PrincipalBirth: stamp, PolicySecurityRevision: strings.Repeat("c", 64), DeliveryStatus: "not_attempted", RuntimeStatus: "pending", Reason: "migration fixture only", CreatedAt: stamp, UpdatedAt: stamp}
	if db.Create(&command).Error != nil {
		t.Fatal("positive resolve command before correlated denials")
	}
	var savedCommand entity.APIKeyDeliveryCommand
	if db.Session(&gorm.Session{QueryFields: true}).Take(&savedCommand, "id = ?", command.ID).Error != nil {
		t.Fatal("capture positive command")
	}
	for _, change := range []map[string]any{{"purpose": "RESOLVE"}, {"purpose": "resolve "}, {"key_id": "key_illegal"}, {"delivery_status": "acknowledged", "acknowledged_at": stamp}, {"runtime_status": "confirmed", "published_at": stamp}, {"claim_process_id": "ins_illegal", "claim_process_birth": stamp, "claimed_at": stamp}} {
		if db.Model(&entity.APIKeyDeliveryCommand{}).Where("id = ?", command.ID).Updates(change).Error == nil {
			t.Fatal("invalid correlated resolve command admitted")
		}
		var got entity.APIKeyDeliveryCommand
		if db.Session(&gorm.Session{QueryFields: true}).Take(&got, "id = ?", command.ID).Error != nil || !reflect.DeepEqual(got, savedCommand) {
			t.Fatal("command denial changed original row")
		}
	}
	// Retained invalid facts must reject before a success ledger is written.
	// Only the owned CHECK is removed, then the exact fixture value is restored.
	if db.Migrator().DropConstraint(&entity.APIKeyDeliveryCommand{}, "ck_api_key_delivery_command_shape") != nil || db.Model(&entity.APIKeyDeliveryCommand{}).Where("id = ?", command.ID).UpdateColumn("runtime_status", "confirmed").Error != nil {
		t.Fatal("construct retained invalid command")
	}
	dropReceipt()
	rejectMigration("invalid_retained_command")
	if db.Model(&entity.APIKeyDeliveryCommand{}).Where("id = ?", command.ID).UpdateColumn("runtime_status", savedCommand.RuntimeStatus).Error != nil {
		t.Fatal("restore exact owned command state")
	}
	migrate("restored_retained_command")
	// Resuming after an interrupted CHECK replacement preserves the same name.
	if db.Migrator().DropConstraint(&entity.ProjectKey{}, "ck_project_keys_delivery") != nil {
		t.Fatal("construct missing Project delivery check")
	}
	dropReceipt()
	migrate("missing_project_delivery_check")
	// Accepted managed mode is a schema capability, never an activation receipt.
	if !db.Migrator().HasConstraint(&entity.ProjectKey{}, "ck_project_keys_delivery") {
		t.Fatal("same named Project delivery check missing")
	}
	rollback := errors.New("fixture rollback")
	if err := db.Transaction(func(tx *gorm.DB) error {
		r := tx.Model(&deliveryMigrationProjectV101{}).Where("id = ?", project.ID).Update("delivery_mode", "managed_vault")
		if r.Error != nil || r.RowsAffected != 1 {
			return errors.New("positive managed Project mode rejected")
		}
		var got deliveryMigrationProjectV101
		if tx.Session(&gorm.Session{QueryFields: true}).Take(&got, "id = ?", project.ID).Error != nil || got.DeliveryMode != "managed_vault" {
			return errors.New("managed Project mode not persisted")
		}
		personalChange := tx.Table("api_keys").Where("id = ?", personal.ID).Update("delivery_mode", "managed_vault")
		if personalChange.Error != nil || personalChange.RowsAffected != 1 {
			return errors.New("positive managed Personal mode rejected")
		}
		var personalMode struct{ DeliveryMode string }
		if tx.Table("api_keys").Select("delivery_mode").Where("id = ?", personal.ID).Take(&personalMode).Error != nil || personalMode.DeliveryMode != "managed_vault" {
			return errors.New("managed Personal mode not persisted")
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal("same-name Project check did not admit reviewed managed enum")
	}
	for _, bad := range []string{"MANUAL", "manual ", "managed_vault ", "unknown"} {
		if db.Model(&deliveryMigrationProjectV101{}).Where("id = ?", project.ID).Update("delivery_mode", bad).Error == nil {
			t.Fatal("invalid Project delivery enum admitted")
		}
		var got deliveryMigrationProjectV101
		if db.Session(&gorm.Session{QueryFields: true}).Take(&got, "id = ?", project.ID).Error != nil || !reflect.DeepEqual(got, project) {
			t.Fatal("Project enum denial changed retained row")
		}
		if db.Table("api_keys").Where("id = ?", personal.ID).Update("delivery_mode", bad).Error == nil {
			t.Fatal("invalid Personal delivery enum admitted")
		}
	}
	assertRetainedRootInventoryV9(t, db, "delivery_upgrade", stamp)
	checkRetained()
	migrate("repeat_with_fixture_rows")
	var finalConsumer entity.APIKeyDeliveryConsumer
	var finalTemplate entity.APIKeyCallerTemplate
	var finalCommand entity.APIKeyDeliveryCommand
	if db.Session(&gorm.Session{QueryFields: true}).Take(&finalConsumer, "id = ?", consumer.ID).Error != nil || !reflect.DeepEqual(finalConsumer, savedConsumer) || db.Session(&gorm.Session{QueryFields: true}).Take(&finalTemplate, "id = ?", "jwt").Error != nil || !reflect.DeepEqual(finalTemplate, savedTemplate) || db.Session(&gorm.Session{QueryFields: true}).Take(&finalCommand, "id = ?", command.ID).Error != nil || !reflect.DeepEqual(finalCommand, savedCommand) {
		t.Fatal("repeat changed retained delivery fixture rows")
	}

	before103 := personalKeyBehaviorLedger(t, db)
	if len(before103) != 102 {
		t.Fatal("exact immediate pre103 receipt")
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal("restore full103", safeCategory(err))
	}
	after103 := personalKeyBehaviorLedger(t, db)
	if len(after103) != 111 || after103[110].Version != 111 || after103[109].Version != 110 || after103[108].Version != 109 || after103[107].Version != 108 || after103[106].Version != 107 || after103[105].Version != 106 || after103[104].Version != 105 || after103[103].Version != 104 || after103[102].Version != 103 || !reflect.DeepEqual(before103, after103[:102]) {
		t.Fatal("103 restoration changed102 receipts")
	}
}
