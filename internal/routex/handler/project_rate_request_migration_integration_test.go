package handler

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
)

// These test-only check definitions reconstruct released V36 independently of
// the current entity, then simulate the safe prefixes of the frozen V37 step.
type projectRequestChecksV36Fixture struct {
	Kind               string `gorm:"check:ck_project_request_kind,kind IN ('MODEL_ACCESS','QUOTA')"`
	ApprovedPolicyJSON string `gorm:"check:ck_project_quota_approval,kind <> 'QUOTA' OR status <> 'approved' OR (decision_review_etag <> '' AND approved_policy_etag <> '' AND decision_request_hash <> '' AND approved_policy_json <> '')"`
}

func (projectRequestChecksV36Fixture) TableName() string { return "project_model_requests" }

type projectRequestChecksV37Fixture struct {
	Kind                string `gorm:"check:ck_project_request_kind_v37,kind IN ('MODEL_ACCESS','QUOTA','RATE_LIMIT')"`
	DecisionRequestHash string `gorm:"check:ck_project_rate_approval,kind <> 'RATE_LIMIT' OR status <> 'approved' OR (decision_review_etag <> '' AND approved_policy_etag <> '' AND decision_request_hash <> '' AND approved_policy_json <> '')"`
}

func (projectRequestChecksV37Fixture) TableName() string { return "project_model_requests" }

func testProjectRateRequestMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	model := &entity.ProjectModelRequest{}
	released := &projectRequestChecksV36Fixture{}
	current := &projectRequestChecksV37Fixture{}
	reconstructV36 := func() {
		t.Helper()
		for _, name := range []string{"ck_project_rate_approval", "ck_project_request_kind_v37"} {
			if db.Migrator().HasConstraint(model, name) {
				if err := db.Migrator().DropConstraint(model, name); err != nil {
					t.Fatal(err)
				}
			}
		}
		if !db.Migrator().HasConstraint(released, "ck_project_request_kind") {
			if err := db.Migrator().CreateConstraint(released, "ck_project_request_kind"); err != nil {
				t.Fatal(err)
			}
		}
		removeProjectRateRequestLedger(t, db)
	}
	reconstructV36()
	columnNames := projectRateRequestColumns(t, db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	history := map[string]entity.ProjectModelRequest{}
	for _, kind := range []string{entity.ProjectRequestModelAccess, entity.ProjectRequestQuota} {
		for _, status := range []string{entity.ProjectRequestPending, entity.ProjectRequestApproved, entity.ProjectRequestRejected, entity.ProjectRequestWithdrawn} {
			prefix := "model"
			if kind == entity.ProjectRequestQuota {
				prefix = "quota"
			}
			id := "pmr_v36_" + prefix + "_" + status
			row := entity.ProjectModelRequest{ID: id, RequestID: id + "_intent", RequestHash: strings.Repeat("a", 64), ProjectID: "prj_missing_v36", ApplicantUserID: "usr_missing_v36", Kind: kind, BaselineJSON: `["mdl_historical"]`, RequestedJSON: `["mdl_added"]`, Reason: "Released request intent", Status: status, CreatedAt: now}
			if status != entity.ProjectRequestPending {
				row.DecidedAt, row.DecisionActorID, row.DecisionReason = &now, "usr_missing_reviewer", "Released decision"
			}
			if kind == entity.ProjectRequestQuota {
				row.BaselinePolicyETag = "0"
				row.BaselineJSON = `{"tokens_month":null,"money_month":null,"currency":"","platform_currency":"USD"}`
				row.RequestedJSON = `{"tokens_month":0}`
				if status == entity.ProjectRequestApproved {
					row.DecisionReviewETag = strings.Repeat("b", 64)
					row.ApprovedPolicyETag = "lim_v36_historical"
					row.DecisionRequestHash = strings.Repeat("c", 64)
					row.ApprovedPolicyJSON = `{"tokens_month":0,"money_month":"0.000000000000000001","currency":"USD","ip_mode":"none","ip_ranges":[]}`
				}
			}
			if err := db.Create(&row).Error; err != nil {
				t.Fatal("cannot seed released request history without live identities", err)
			}
			var stored entity.ProjectModelRequest
			if err := db.First(&stored, "id = ?", id).Error; err != nil {
				t.Fatal(err)
			}
			history[id] = stored
		}
	}
	assertHistory := func() {
		t.Helper()
		for id, original := range history {
			var stored entity.ProjectModelRequest
			if err := db.First(&stored, "id = ?", id).Error; err != nil || !reflect.DeepEqual(original, stored) {
				t.Fatal("V37 changed immutable released request or stored timestamp", id, err)
			}
		}
		if !reflect.DeepEqual(columnNames, projectRateRequestColumns(t, db)) {
			t.Fatal("check-only migration changed request columns")
		}
		assertProjectQuotaRequestSchema(t, db)
	}
	var wg sync.WaitGroup
	outcomes := make(chan error, 2)
	for range 2 {
		wg.Go(func() { outcomes <- database.Migrate(ctx, db) })
	}
	wg.Wait()
	close(outcomes)
	for err := range outcomes {
		if err != nil {
			t.Fatal("concurrent V36 to V37 upgrade", err)
		}
	}
	assertHistory()
	for _, prefix := range []string{"rate guard only", "both new guards", "old guard removed"} {
		reconstructV36()
		if err := db.Migrator().CreateConstraint(current, "ck_project_rate_approval"); err != nil {
			t.Fatal(err)
		}
		if prefix != "rate guard only" {
			if err := db.Migrator().CreateConstraint(current, "ck_project_request_kind_v37"); err != nil {
				t.Fatal(err)
			}
		}
		if prefix == "old guard removed" {
			if err := db.Migrator().DropConstraint(released, "ck_project_request_kind"); err != nil {
				t.Fatal(err)
			}
		} else if !db.Migrator().HasConstraint(released, "ck_project_request_kind") {
			t.Fatal("interrupted prefix lost conservative released kind guard")
		}
		if err := database.Migrate(ctx, db); err != nil {
			t.Fatal("interrupted prefix did not resume", prefix, err)
		}
		assertHistory()
	}
	rate := entity.ProjectModelRequest{ID: "pmr_rate_approved_history", RequestID: "request_rate_approved_history", RequestHash: strings.Repeat("d", 64), ProjectID: "prj_rate_missing", ApplicantUserID: "usr_rate_missing", Kind: entity.ProjectRequestRateLimit, BaselinePolicyETag: "0", BaselineJSON: `{"rpm":null,"tpm":null,"concurrency":null}`, RequestedJSON: `{"rpm":0,"tpm":100,"concurrency":1}`, Reason: "Exact rate adjustment", Status: entity.ProjectRequestApproved, DecisionActorID: "usr_rate_reviewer_missing", DecisionReason: "Reviewed rate adjustment", DecisionReviewETag: strings.Repeat("e", 64), ApprovedPolicyETag: "lim_rate_historical", DecisionRequestHash: strings.Repeat("f", 64), ApprovedPolicyJSON: `{"rpm":0,"tpm":100,"concurrency":1,"tokens_month":1000,"money_month":"2.000000000000000001","currency":"USD","ip_mode":"none","ip_ranges":[]}`, CreatedAt: now, DecidedAt: &now}
	if err := db.Create(&rate).Error; err != nil {
		t.Fatal("approved RATE_LIMIT must retain history without live identity FK", err)
	}
	var savedRate entity.ProjectModelRequest
	if err := db.First(&savedRate, "id = ?", rate.ID).Error; err != nil {
		t.Fatal(err)
	}
	history[rate.ID] = savedRate
	for _, missing := range []string{"review", "revision", "intent", "policy"} {
		invalid := rate
		invalid.ID, invalid.RequestID = "pmr_rate_invalid_"+missing, "request_rate_invalid_"+missing
		switch missing {
		case "review":
			invalid.DecisionReviewETag = ""
		case "revision":
			invalid.ApprovedPolicyETag = ""
		case "intent":
			invalid.DecisionRequestHash = ""
		case "policy":
			invalid.ApprovedPolicyJSON = ""
		}
		if err := db.Create(&invalid).Error; err == nil {
			t.Fatal("approved RATE_LIMIT accepted missing immutable receipt", missing)
		}
	}
	pending := rate
	pending.ID, pending.RequestID, pending.Status = "pmr_rate_pending_history", "request_rate_pending_history", entity.ProjectRequestPending
	pending.DecisionReviewETag, pending.ApprovedPolicyETag, pending.DecisionRequestHash, pending.ApprovedPolicyJSON = "", "", "", ""
	pending.DecisionActorID, pending.DecisionReason, pending.DecidedAt = "", "", nil
	if err := db.Create(&pending).Error; err != nil {
		t.Fatal("pending RATE_LIMIT must not require a fabricated approval", err)
	}
	if err := db.Model(model).Where("id = ?", pending.ID).Update("kind", "FUTURE_KIND").Error; err == nil {
		t.Fatal("V37 accepted unsupported request kind")
	}
	duplicate := pending
	duplicate.ID = "pmr_rate_duplicate_history"
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("RATE_LIMIT lost released request intent uniqueness")
	}
	// Repair either missing new guard with RATE_LIMIT rows already present; the
	// migration must not reinstall V36's incompatible two-kind restriction.
	for _, missing := range []string{"ck_project_rate_approval", "ck_project_request_kind_v37"} {
		if err := db.Migrator().DropConstraint(current, missing); err != nil {
			t.Fatal(err)
		}
		removeProjectRateRequestLedger(t, db)
		if err := database.Migrate(ctx, db); err != nil {
			t.Fatal("missing current guard repair", missing, err)
		}
		assertHistory()
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	assertHistory()
}

func removeProjectRateRequestLedger(t *testing.T, db *gorm.DB) {
	t.Helper()
	result := db.Table("schema_migrations").Where("version = ?", 37).Delete(&struct{}{})
	if result.Error != nil || result.RowsAffected != 1 {
		t.Fatal("cannot reconstruct V37 migration ledger", result.Error)
	}
}

func projectRateRequestColumns(t *testing.T, db *gorm.DB) []string {
	t.Helper()
	columns, err := db.Migrator().ColumnTypes(&entity.ProjectModelRequest{})
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(columns))
	for _, column := range columns {
		names = append(names, column.Name())
	}
	sort.Strings(names)
	return names
}
