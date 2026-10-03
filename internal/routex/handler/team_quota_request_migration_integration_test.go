package handler

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
)

func testTeamQuotaRequestMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	models := []any{&entity.TeamQuotaRequest{}, &entity.TeamQuotaRequestStep{}, &entity.TeamQuotaPendingSlot{}}
	// Preserve a released request with missing live identities and exact money
	// evidence while reconstructing every new-table prefix of V40.
	legacy := entity.ProjectModelRequest{
		ID:              "pmr_v39_team_upgrade",
		RequestID:       "v39_team_upgrade_intent",
		RequestHash:     strings.Repeat("a", 64),
		ProjectID:       "prj_v39_missing",
		ApplicantUserID: "usr_v39_missing",
		Kind:            entity.ProjectRequestQuota,
		BaselineJSON:    `{"tokens_month":null,"money_month":null,"currency":"USD"}`,
		RequestedJSON:   `{"money_month":"2.000000000000000001","currency":"USD"}`,
		Reason:          "Retain released quota evidence",
		Status:          entity.ProjectRequestPending,
		CreatedAt:       time.Now().UTC().Truncate(time.Microsecond),
	}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&legacy, "id = ?", legacy.ID).Error; err != nil {
		t.Fatal(err)
	}
	removeLedger := func() {
		t.Helper()
		if result := db.Table("schema_migrations").Where("version = ?", 40).Delete(&struct{}{}); result.Error != nil || result.RowsAffected != 1 {
			t.Fatal("cannot reconstruct V40 migration ledger", result.Error)
		}
	}
	assertLegacy := func() {
		t.Helper()
		var stored entity.ProjectModelRequest
		if err := db.First(&stored, "id = ?", legacy.ID).Error; err != nil || !reflect.DeepEqual(legacy, stored) {
			t.Fatal("V40 changed released immutable request history", err)
		}
	}
	for prefix := 0; prefix <= len(models); prefix++ {
		for _, model := range models {
			if err := db.Migrator().DropTable(model); err != nil {
				t.Fatal(err)
			}
		}
		removeLedger()
		for _, model := range models[:prefix] {
			if err := db.Migrator().CreateTable(model); err != nil {
				t.Fatal(err)
			}
		}
		var wg sync.WaitGroup
		outcomes := make(chan error, 2)
		for range 2 {
			wg.Go(func() { outcomes <- database.Migrate(context.Background(), db) })
		}
		wg.Wait()
		close(outcomes)
		for err := range outcomes {
			if err != nil {
				t.Fatal("concurrent startup cannot repair V40 table prefix", prefix, err)
			}
		}
		assertTeamQuotaRequestMigrationSchema(t, db)
		assertLegacy()
	}
	stamp := time.Now().UTC().Truncate(time.Microsecond)
	request := entity.TeamQuotaRequest{
		ID:                       "qrq_historical",
		RequestID:                "11111111-1111-4111-8111-111111111111",
		RequestHash:              strings.Repeat("b", 64),
		CreationReviewETag:       strings.Repeat("c", 64),
		TeamID:                   "tem_missing",
		ApplicantUserID:          "usr_missing",
		ApplicantMembershipID:    "tmm_missing",
		TeamName:                 "Historical Team",
		ApplicantName:            "Historical applicant",
		Dimension:                "money",
		TargetValue:              "2.000000000000000001",
		Currency:                 "USD",
		Reason:                   "Exact monthly adjustment",
		SubmittedSnapshotJSON:    `{"member":{"money_month":"1.000000000000000001","currency":"USD"},"team":{"money_month":"3","currency":"USD"}}`,
		Status:                   entity.TeamQuotaRequestApproved,
		ApprovedTeamPolicyETag:   "lim_historical_team",
		ApprovedMemberPolicyETag: "lim_historical_member",
		ApprovedTeamPolicyJSON:   `{"money_month":"3","currency":"USD","rpm":1}`,
		ApprovedMemberPolicyJSON: `{"money_month":"2.000000000000000001","currency":"USD","rpm":1}`,
		CreatedAt:                stamp,
		UpdatedAt:                stamp,
		ResolvedAt:               &stamp,
	}
	if err := db.Create(&request).Error; err != nil {
		t.Fatal("approved requests must survive missing live identities", err)
	}
	decisionID := "22222222-2222-4222-8222-222222222222"
	step := entity.TeamQuotaRequestStep{
		ID:           "qst_historical",
		RequestID:    request.ID,
		Ordinal:      1,
		Stage:        entity.TeamQuotaStageOwner,
		Status:       entity.TeamQuotaStepApproved,
		DecisionID:   &decisionID,
		DecisionHash: strings.Repeat("d", 64),
		ReviewETag:   strings.Repeat("e", 64),
		ActorID:      "usr_missing_owner",
		ActorName:    "Historical owner",
		Action:       "approve",
		Reason:       "Reviewed exact proposal",
		EnteredAt:    stamp,
		DecidedAt:    &stamp,
	}
	if err := db.Create(&step).Error; err != nil {
		t.Fatal(err)
	}
	pending := entity.TeamQuotaRequestStep{ID: "qst_pending_one", RequestID: "qrq_missing_one", Ordinal: 1, Stage: entity.TeamQuotaStageAdmin, Status: entity.TeamQuotaStepPending, EnteredAt: stamp}
	if err := db.Create(&pending).Error; err != nil {
		t.Fatal("pending stage needs no fabricated decision or live request FK", err)
	}
	secondPending := pending
	secondPending.ID, secondPending.RequestID = "qst_pending_two", "qrq_missing_two"
	if err := db.Create(&secondPending).Error; err != nil {
		t.Fatal("NULL pending decisions must not collide", err)
	}
	cancelled := pending
	cancelled.ID, cancelled.RequestID, cancelled.Status = "qst_cancelled_system", "qrq_cancelled_system", entity.TeamQuotaStepCancelled
	cancelled.DecidedAt = &stamp
	if err := db.Create(&cancelled).Error; err != nil {
		t.Fatal("system cancellation must not fabricate a human decision UUID", err)
	}
	slot := entity.TeamQuotaPendingSlot{TeamID: request.TeamID, ApplicantUserID: request.ApplicantUserID, Dimension: "money", RequestID: pending.RequestID, CreatedAt: stamp}
	if err := db.Create(&slot).Error; err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"intent", "dimension", "currency", "status", "team revision", "member revision", "team policy", "member policy", "resolved"} {
		invalid := request
		invalid.ID, invalid.RequestID = "qrq_invalid_"+strings.ReplaceAll(scenario, " ", "_"), "invalid_"+scenario
		switch scenario {
		case "intent":
			invalid.RequestID = request.RequestID
		case "dimension":
			invalid.Dimension = "rate"
		case "currency":
			invalid.Currency = ""
		case "status":
			invalid.Status = "future"
		case "team revision":
			invalid.ApprovedTeamPolicyETag = ""
		case "member revision":
			invalid.ApprovedMemberPolicyETag = ""
		case "team policy":
			invalid.ApprovedTeamPolicyJSON = ""
		case "member policy":
			invalid.ApprovedMemberPolicyJSON = ""
		case "resolved":
			invalid.ResolvedAt = nil
		}
		if err := db.Create(&invalid).Error; err == nil {
			t.Fatal("invalid immutable request accepted", scenario)
		}
	}
	for _, scenario := range []string{"ordinal", "stage", "status", "same stage", "same decision", "decision", "hash", "review", "actor", "action", "time"} {
		invalid := step
		invalid.ID, invalid.RequestID = "qst_invalid_"+strings.ReplaceAll(scenario, " ", "_"), "qrq_invalid_step"
		unique := "invalid_" + scenario
		invalid.DecisionID = &unique
		switch scenario {
		case "ordinal":
			invalid.Ordinal = 3
		case "stage":
			invalid.Stage = "future"
		case "status":
			invalid.Status = "future"
		case "same stage":
			invalid.RequestID = step.RequestID
		case "same decision":
			invalid.DecisionID = step.DecisionID
		case "decision":
			invalid.DecisionID = nil
		case "hash":
			invalid.DecisionHash = ""
		case "review":
			invalid.ReviewETag = ""
		case "actor":
			invalid.ActorID = ""
		case "action":
			invalid.Action = ""
		case "time":
			invalid.DecidedAt = nil
		}
		if err := db.Create(&invalid).Error; err == nil {
			t.Fatal("invalid or duplicate stage receipt accepted", scenario)
		}
	}
	for _, scenario := range []string{"pair dimension", "request", "dimension"} {
		duplicate := slot
		switch scenario {
		case "pair dimension":
			duplicate.RequestID = "qrq_other_pending"
		case "request":
			duplicate.ApplicantUserID = "usr_other_pending"
		case "dimension":
			duplicate.Dimension = "rate"
		}
		if err := db.Create(&duplicate).Error; err == nil {
			t.Fatal("pending slot did not constrain active intent", scenario)
		}
	}
	independent := slot
	independent.Dimension, independent.RequestID = "tokens", secondPending.RequestID
	if err := db.Create(&independent).Error; err != nil {
		t.Fatal("independent monthly dimensions must have separate pending slots", err)
	}
	if err := db.Where("team_id = ? AND applicant_user_id = ? AND dimension = ?", slot.TeamID, slot.ApplicantUserID, slot.Dimension).Delete(&entity.TeamQuotaPendingSlot{}).Error; err != nil {
		t.Fatal(err)
	}
	slot.RequestID = "qrq_next_money_intent"
	if err := db.Create(&slot).Error; err != nil {
		t.Fatal("resolved slot must admit a later independent intent", err)
	}
	// Capture the database's exact persisted timestamp precision before repair.
	if err := db.First(&request, "id = ?", request.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&step, "id = ?", step.ID).Error; err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ck_team_quota_request_approval", "ck_team_quota_step_receipt", "ck_team_quota_slot_dimension"} {
		model := models[0]
		if strings.Contains(name, "step") {
			model = models[1]
		} else if strings.Contains(name, "slot") {
			model = models[2]
		}
		if err := db.Migrator().DropConstraint(model, name); err != nil {
			t.Fatal(err)
		}
	}
	for _, index := range []struct {
		model any
		name  string
	}{{models[0], "idx_team_quota_request_team_cursor"}, {models[1], "uq_team_quota_step_decision"}, {models[2], "uq_team_quota_pending_request"}} {
		dropTeamQuotaRequestFixtureIndex(t, db, index.model, index.name)
	}
	if err := db.Where("role_id = ? AND permission = ?", "rol_admin", "teams.quota_requests.read_all").Delete(&entity.RolePermission{}).Error; err != nil {
		t.Fatal(err)
	}
	removeLedger()
	for range 2 {
		if err := database.Migrate(context.Background(), db); err != nil {
			t.Fatal("V40 interrupted guards/indexes/seed repair", err)
		}
	}
	assertTeamQuotaRequestMigrationSchema(t, db)
	assertLegacy()
	var retained entity.TeamQuotaRequest
	var retainedStep entity.TeamQuotaRequestStep
	if err := db.First(&retained, "id = ?", request.ID).Error; err != nil || !reflect.DeepEqual(request, retained) {
		t.Fatal("V40 repair changed exact approved intent or policies", err)
	}
	if err := db.First(&retainedStep, "id = ?", step.ID).Error; err != nil || !reflect.DeepEqual(step, retainedStep) {
		t.Fatal("V40 repair changed historical decision receipt", err)
	}
}

func assertTeamQuotaRequestMigrationSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, model := range []any{&entity.TeamQuotaRequest{}, &entity.TeamQuotaRequestStep{}, &entity.TeamQuotaPendingSlot{}} {
		if !db.Migrator().HasTable(model) {
			t.Fatal("V40 table absent")
		}
		statement := &gorm.Statement{DB: db}
		if err := statement.Parse(model); err != nil {
			t.Fatal(err)
		}
		columns, err := db.Migrator().ColumnTypes(model)
		if err != nil {
			t.Fatal(err)
		}
		for _, column := range columns {
			optional := column.Name() == "current_step_id" || column.Name() == "resolved_at" || column.Name() == "decision_id" || column.Name() == "decided_at"
			if nullable, known := column.Nullable(); known && nullable != optional {
				t.Fatal("V40 immutable field nullability changed", column.Name(), nullable)
			}
		}
		for name := range statement.Schema.ParseCheckConstraints() {
			if !db.Migrator().HasConstraint(model, name) {
				t.Fatal("V40 guard absent", name)
			}
		}
		for _, index := range statement.Schema.ParseIndexes() {
			if !db.Migrator().HasIndex(model, index.Name) {
				t.Fatal("V40 paging/uniqueness index absent", index.Name)
			}
		}
	}
	var count int64
	if err := db.Model(&entity.RolePermission{}).Where("role_id = ? AND permission = ?", "rol_admin", "teams.quota_requests.read_all").Count(&count).Error; err != nil || count != 1 {
		t.Fatal("independent read-all seed must be repaired exactly once", count, err)
	}
}

func dropTeamQuotaRequestFixtureIndex(t *testing.T, db *gorm.DB, model any, name string) {
	t.Helper()
	if db.Name() != "postgres" {
		if err := db.Migrator().DropIndex(model, name); err != nil {
			t.Fatal(err)
		}
		return
	}
	// Pinned GORM renders an invalid CURRENT_SCHEMA().index DropIndex target.
	// Only this fixture uses fixed allowlisted DDL to simulate interrupted steps.
	statements := map[string]string{
		"idx_team_quota_request_team_cursor": `DROP INDEX "idx_team_quota_request_team_cursor"`,
		"uq_team_quota_step_decision":        `DROP INDEX "uq_team_quota_step_decision"`,
		"uq_team_quota_pending_request":      `DROP INDEX "uq_team_quota_pending_request"`,
	}
	statement, ok := statements[name]
	if !ok {
		t.Fatal("unknown Team quota fixture index")
	}
	if err := db.Exec(statement).Error; err != nil {
		t.Fatal(err)
	}
}
