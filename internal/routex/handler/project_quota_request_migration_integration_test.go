package handler

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
)

func testProjectQuotaRequestMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	model := &entity.ProjectModelRequest{}
	columns := []string{"Kind", "BaselinePolicyETag", "DecisionReviewETag", "ApprovedPolicyETag", "DecisionRequestHash", "ApprovedPolicyJSON"}
	dropChecks := func() {
		t.Helper()
		for _, name := range []string{"ck_project_request_kind", "ck_project_quota_approval"} {
			if err := db.Migrator().DropConstraint(model, name); err != nil {
				t.Fatal(err)
			}
		}
	}
	dropChecks()
	for _, column := range columns {
		if err := db.Migrator().DropColumn(model, column); err != nil {
			t.Fatal(err)
		}
	}
	removeProjectQuotaRequestLedger(t, db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	historicalIDs := []string{}
	historicalTimes := map[string]entity.ProjectModelRequest{}
	for _, status := range []string{entity.ProjectRequestPending, entity.ProjectRequestApproved, entity.ProjectRequestRejected, entity.ProjectRequestWithdrawn} {
		id := "pmr_v35_" + status
		historicalIDs = append(historicalIDs, id)
		// A V35 writer has no kind or approval-policy fields. Historical identity
		// and JSON arrays must survive the additive upgrade exactly as recorded.
		row := map[string]any{"id": id, "request_id": "request_v35_" + status, "request_hash": strings.Repeat("a", 64), "project_id": "prj_historical_request", "applicant_user_id": "usr_historical_applicant", "baseline_json": `["mdl_original"]`, "requested_json": `["mdl_addition"]`, "reason": "Historical model request", "status": status, "decision_actor_id": "usr_historical_reviewer", "decision_reason": "Historical decision", "created_at": now, "decided_at": now}
		if err := db.Table("project_model_requests").Create(row).Error; err != nil {
			t.Fatal("cannot insert released V35 request without live identities", err)
		}
		// Released V12 timestamps use the driver's existing precision. Freeze the
		// stored values before migration, rather than require unrepresentable
		// microseconds from MySQL's historical DATETIME(3) columns.
		var stored entity.ProjectModelRequest
		if err := db.Select("created_at", "decided_at").First(&stored, "id = ?", id).Error; err != nil || stored.CreatedAt.IsZero() || stored.DecidedAt == nil {
			t.Fatal("cannot capture released request timestamps", err)
		}
		historicalTimes[id] = stored
	}
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
	assertHistorical := func() {
		t.Helper()
		for _, id := range historicalIDs {
			original := historicalTimes[id]
			var row entity.ProjectModelRequest
			// A stable projection avoids prepared SELECT * shape changes while
			// individual columns are dropped/re-added in this migration fixture.
			if err := db.Select("id", "request_id", "request_hash", "project_id", "applicant_user_id", "kind", "baseline_policy_etag", "decision_review_etag", "approved_policy_etag", "decision_request_hash", "approved_policy_json", "baseline_json", "requested_json", "reason", "status", "decision_actor_id", "decision_reason", "created_at", "decided_at").First(&row, "id = ?", id).Error; err != nil {
				t.Fatal(err)
			}
			if row.Kind != entity.ProjectRequestModelAccess || row.BaselinePolicyETag != "" || row.DecisionReviewETag != "" || row.ApprovedPolicyETag != "" || row.DecisionRequestHash != "" || row.ApprovedPolicyJSON != "" || row.BaselineJSON != `["mdl_original"]` || row.RequestedJSON != `["mdl_addition"]` || row.Reason != "Historical model request" || row.RequestHash != strings.Repeat("a", 64) || row.DecisionActorID != "usr_historical_reviewer" || row.DecisionReason != "Historical decision" || !row.CreatedAt.Equal(original.CreatedAt) || row.DecidedAt == nil || !row.DecidedAt.Equal(*original.DecidedAt) || row.ID != "pmr_v35_"+row.Status || row.ProjectID != "prj_historical_request" || row.ApplicantUserID != "usr_historical_applicant" {
				t.Fatal("V36 changed historical model request or inferred quota approval", id)
			}
		}
	}
	assertProjectQuotaRequestSchema(t, db)
	assertHistorical()
	// Each missing column represents an interrupted additive MySQL DDL prefix.
	// Existing columns and released model-request facts must remain untouched.
	for _, column := range columns {
		dropChecks()
		if err := db.Migrator().DropColumn(model, column); err != nil {
			t.Fatal(err)
		}
		removeProjectQuotaRequestLedger(t, db)
		if err := database.Migrate(context.Background(), db); err != nil {
			t.Fatalf("partial column repair %s: %v", column, err)
		}
		assertProjectQuotaRequestSchema(t, db)
		assertHistorical()
	}
	quota := entity.ProjectModelRequest{ID: "pmr_quota_approved_history", RequestID: "request_quota_approved_history", RequestHash: strings.Repeat("b", 64), ProjectID: "prj_historical_quota", ApplicantUserID: "usr_historical_quota", Kind: entity.ProjectRequestQuota, BaselinePolicyETag: "0", BaselineJSON: `{"tokens_month":null,"money_month":null,"currency":"","platform_currency":"USD"}`, RequestedJSON: `{"tokens_month":0}`, Reason: "Reviewed monthly adjustment", Status: entity.ProjectRequestApproved, DecisionActorID: "usr_historical_quota_reviewer", DecisionReason: "Approved exact reviewed request", DecisionReviewETag: strings.Repeat("c", 64), ApprovedPolicyETag: "lim_historical_quota", DecisionRequestHash: strings.Repeat("d", 64), ApprovedPolicyJSON: `{"tokens_month":0,"money_month":"0.000000000000000001","currency":"USD","ip_mode":"none","ip_ranges":[]}`, CreatedAt: now, DecidedAt: &now}
	if err := db.Create(&quota).Error; err != nil {
		t.Fatal("approved quota history needs no live actor or resource FK", err)
	}
	var quotaTimes entity.ProjectModelRequest
	if err := db.Select("created_at", "decided_at").First(&quotaTimes, "id = ?", quota.ID).Error; err != nil || quotaTimes.DecidedAt == nil {
		t.Fatal("cannot capture saved quota approval timestamps", err)
	}
	for _, missing := range []string{"review", "revision", "intent", "policy"} {
		invalid := quota
		invalid.ID = "pmr_quota_invalid_" + missing
		invalid.RequestID = "request_quota_invalid_" + missing
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
			t.Fatal("approved quota missing immutable approval evidence accepted", missing)
		}
	}
	pending := quota
	pending.ID, pending.RequestID, pending.Status = "pmr_quota_pending_history", "request_quota_pending_history", entity.ProjectRequestPending
	pending.DecisionReviewETag, pending.ApprovedPolicyETag, pending.DecisionRequestHash, pending.ApprovedPolicyJSON = "", "", "", ""
	pending.DecisionActorID, pending.DecisionReason, pending.DecidedAt = "", "", nil
	if err := db.Create(&pending).Error; err != nil {
		t.Fatal("pending quota must not require a fabricated approval", err)
	}
	for _, name := range []string{"kind", "baseline_policy_etag", "decision_review_etag", "approved_policy_etag", "decision_request_hash", "approved_policy_json"} {
		if err := db.Model(model).Where("id = ?", pending.ID).Update(name, nil).Error; err == nil {
			t.Fatal("new request field accepted NULL", name)
		}
	}
	if err := db.Model(model).Where("id = ?", pending.ID).Update("kind", "FUTURE_KIND").Error; err == nil {
		t.Fatal("unsupported request kind accepted")
	}
	duplicate := pending
	duplicate.ID = "pmr_quota_duplicate_history"
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("V36 lost existing client-request uniqueness")
	}
	dropChecks()
	removeProjectQuotaRequestLedger(t, db)
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	assertProjectQuotaRequestSchema(t, db)
	assertHistorical()
	var retained entity.ProjectModelRequest
	if err := db.First(&retained, "id = ?", quota.ID).Error; err != nil || retained.Kind != quota.Kind || retained.BaselinePolicyETag != quota.BaselinePolicyETag || retained.DecisionReviewETag != quota.DecisionReviewETag || retained.ApprovedPolicyETag != quota.ApprovedPolicyETag || retained.DecisionRequestHash != quota.DecisionRequestHash || retained.ApprovedPolicyJSON != quota.ApprovedPolicyJSON || retained.BaselineJSON != quota.BaselineJSON || retained.RequestedJSON != quota.RequestedJSON || !retained.CreatedAt.Equal(quotaTimes.CreatedAt) || retained.DecidedAt == nil || !retained.DecidedAt.Equal(*quotaTimes.DecidedAt) {
		t.Fatal("check repair or repeat changed immutable quota review/policy history", err)
	}
}

func removeProjectQuotaRequestLedger(t *testing.T, db *gorm.DB) {
	t.Helper()
	result := db.Table("schema_migrations").Where("version = ?", 36).Delete(&struct{}{})
	if result.Error != nil || result.RowsAffected != 1 {
		t.Fatal("cannot reconstruct V36 migration ledger", result.Error)
	}
}

func assertProjectQuotaRequestSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	model := &entity.ProjectModelRequest{}
	columns, err := db.Migrator().ColumnTypes(model)
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]int64{"kind": 20, "baseline_policy_etag": 64, "decision_review_etag": 64, "approved_policy_etag": 64, "decision_request_hash": 64, "approved_policy_json": 0}
	for _, column := range columns {
		size, needed := expected[column.Name()]
		if !needed {
			continue
		}
		delete(expected, column.Name())
		if nullable, known := column.Nullable(); !known || nullable {
			t.Fatal("additive request field must be nonnull", column.Name())
		}
		if size > 0 {
			if actual, known := column.Length(); !known || actual != size {
				t.Fatal("request field bound changed", column.Name())
			}
		}
	}
	if len(expected) != 0 {
		t.Fatal("missing quota request fields", expected)
	}
	for _, name := range []string{"ck_project_request_kind", "ck_project_quota_approval"} {
		if !db.Migrator().HasConstraint(model, name) {
			t.Fatal("missing quota request check", name)
		}
	}
	for _, name := range []string{"uq_project_model_request", "idx_project_model_request_project", "idx_project_model_request_status"} {
		if !db.Migrator().HasIndex(model, name) {
			t.Fatal("quota migration lost released request index", name)
		}
	}
}
