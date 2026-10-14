package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/miclle/routex/internal/routex/database"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"gorm.io/gorm"
)

// This child uses the existing real-driver Insights fixture. CRUD must require
// current resource authority while remaining independent of inference and usage.
func testInsightsQuotaReportDefinitions(t *testing.T, db *gorm.DB, svc *service.Service, router http.Handler, admin SessionResponse, cookie *http.Cookie) {
	t.Helper()
	ctx := db.Statement.Context
	project, err := svc.CreateProject(ctx, admin.User.ID, service.ProjectCreationInput{Name: "Saved quota definition Project"})
	if err != nil {
		t.Fatal(err)
	}
	analyst, err := svc.CreateMember(ctx, admin.User.ID, "saved-quota-analyst@example.invalid", "saved-quota-analysis-password", "Saved quota analyst", entity.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	role, err := svc.CreateRoleWithDescription(ctx, admin.User.ID, "Saved quota analyst", "Scoped saved quota definitions", []string{"calls.read_all"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetMemberRoles(ctx, admin.User.ID, analyst.User.ID, []string{role.Role.ID}); err != nil {
		t.Fatal(err)
	}
	managers := []string{admin.User.ID, analyst.User.ID}
	managed, err := svc.CreateProject(ctx, admin.User.ID, service.ProjectCreationInput{Name: "Managed saved quota definition Project", ManagerIDs: &managers})
	if err != nil {
		t.Fatal(err)
	}
	// The guard is installed before fixture workers start. Tag only the
	// definition requests, leaving unrelated background accounting untouched.
	ctx = context.WithValue(ctx, insightsSavedQuotaReadGuardKey{}, true)
	originalRouter := router
	router = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originalRouter.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), insightsSavedQuotaReadGuardKey{}, true)))
	})
	var forbiddenRead int64
	if err := db.WithContext(ctx).Model(&entity.APIKey{}).Count(&forbiddenRead).Error; !errors.As(err, new(assertNoSavedQuotaAccountingRead)) {
		t.Fatal("definition accounting guard did not reject a real query", err)
	}
	inputs := []service.CreateInsightsReportInput{
		{RequestID: "d52b7d81-6e5d-44b2-b697-c55bd6a77974", Kind: "team_quota", Dimension: "tokens"},
		{RequestID: "d52b7d82-6e5d-44b2-b697-c55bd6a77974", Kind: "key_quota", Dimension: "money", ScopeKind: "member", ScopeID: admin.User.ID},
		{RequestID: "d52b7d83-6e5d-44b2-b697-c55bd6a77974", Kind: "key_quota", Dimension: "tokens", ScopeKind: "project", ScopeID: project.ID},
	}
	for _, input := range inputs {
		body, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		saved := identityRequest(router, http.MethodPost, "/api/v1/insights/reports", string(body), cookie, admin.CSRFToken)
		expectStatus(t, saved, http.StatusCreated)
		var definition service.InsightsReportDefinition
		if err := json.Unmarshal(saved.Body.Bytes(), &definition); err != nil || definition.Kind != input.Kind || definition.Dimension != input.Dimension || definition.ScopeKind != input.ScopeKind || definition.ScopeID != input.ScopeID || definition.From != "" || definition.To != "" || definition.Timezone != "" {
			t.Fatal("quota definition wire parameters", err, saved.Body.String())
		}
		expectStatus(t, identityRequest(router, http.MethodPost, "/api/v1/insights/reports", string(body), cookie, admin.CSRFToken), http.StatusOK)
		changed := input
		changed.Dimension = "money"
		if input.Dimension == "money" {
			changed.Dimension = "tokens"
		}
		changedBody, _ := json.Marshal(changed)
		expectStatus(t, identityRequest(router, http.MethodPost, "/api/v1/insights/reports", string(changedBody), cookie, admin.CSRFToken), http.StatusConflict)
		permission := entity.RolePermission{RoleID: "rol_admin", Permission: "calls.read_all"}
		if removed := db.Where("role_id = ? AND permission = ?", permission.RoleID, permission.Permission).Delete(&entity.RolePermission{}); removed.Error != nil || removed.RowsAffected != 1 {
			t.Fatal(removed.Error)
		}
		func() {
			defer func() {
				if err := db.Create(&permission).Error; err != nil {
					t.Error(err)
				}
			}()
			expectStatus(t, identityRequest(router, http.MethodPost, "/api/v1/insights/reports", string(body), cookie, admin.CSRFToken), http.StatusForbidden)
			expectStatus(t, identityRequest(router, http.MethodGet, "/api/v1/insights/reports", "", cookie, ""), http.StatusOK)
			expectStatus(t, identityRequest(router, http.MethodDelete, "/api/v1/insights/reports/"+definition.ID, "", cookie, admin.CSRFToken), http.StatusNoContent)
		}()
		var receipt entity.InsightsReport
		if err := db.Where("id = ?", definition.ID).Take(&receipt).Error; err != nil || receipt.Dimension != "" || receipt.ScopeKind != "" || receipt.ScopeID != "" || receipt.Kind != "" || receipt.DeletedAt == nil {
			t.Fatal("quota definition deletion left parameters", err)
		}
		expectStatus(t, identityRequest(router, http.MethodPost, "/api/v1/insights/reports", string(body), cookie, admin.CSRFToken), http.StatusGone)
	}
	input := inputs[1]
	input.RequestID = "d52b7d84-6e5d-44b2-b697-c55bd6a77974"
	permission := entity.RolePermission{RoleID: "rol_admin", Permission: "members.read"}
	if removed := db.Where("role_id = ? AND permission = ?", permission.RoleID, permission.Permission).Delete(&entity.RolePermission{}); removed.Error != nil || removed.RowsAffected != 1 {
		t.Fatal(removed.Error)
	}
	func() {
		defer func() {
			if err := db.Create(&permission).Error; err != nil {
				t.Error(err)
			}
		}()
		if _, _, err := svc.CreateInsightsReport(ctx, admin.User.ID, input); err == nil {
			t.Fatal("saved member Key scope bypassed members.read")
		}
	}()
	input.ScopeID = "usr_01k7g000000000000000000000"
	if _, _, err := svc.CreateInsightsReport(ctx, admin.User.ID, input); err == nil {
		t.Fatal("missing member accepted")
	}
	input = inputs[2]
	input.RequestID = "d52b7d85-6e5d-44b2-b697-c55bd6a77974"
	input.ScopeID = "prj_missing"
	if _, _, err := svc.CreateInsightsReport(ctx, admin.User.ID, input); err == nil {
		t.Fatal("missing Project accepted")
	}
	// A scoped analyst can save a dynamic readable-Team query and their managed
	// Project's Key query, without gaining the global member or Project directory.
	scoped := service.CreateInsightsReportInput{RequestID: "d52b7d86-6e5d-44b2-b697-c55bd6a77974", Kind: "team_quota", Dimension: "tokens"}
	own, _, err := svc.CreateInsightsReport(ctx, analyst.User.ID, scoped)
	if err != nil {
		t.Fatal("scoped readable-Team definition", err)
	}
	if err := svc.DeleteInsightsReport(ctx, admin.User.ID, own.ID); err == nil {
		t.Fatal("administrator deleted another owner's quota definition")
	}
	scoped.Kind = "key_quota"
	scoped.ScopeKind = "member"
	scoped.ScopeID = admin.User.ID
	scoped.RequestID = "d52b7d87-6e5d-44b2-b697-c55bd6a77974"
	if _, _, err := svc.CreateInsightsReport(ctx, analyst.User.ID, scoped); err == nil {
		t.Fatal("calls.read_all granted member Key scope")
	}
	scoped.ScopeKind = "project"
	scoped.ScopeID = project.ID
	if _, _, err := svc.CreateInsightsReport(ctx, analyst.User.ID, scoped); err == nil {
		t.Fatal("calls.read_all granted unrelated Project Key scope")
	}
	scoped.ScopeID = managed.ID
	managedDefinition, _, err := svc.CreateInsightsReport(ctx, analyst.User.ID, scoped)
	if err != nil {
		t.Fatal("current Project manager could not save scoped Key definition", err)
	}
	scoped.RequestID = "d52b7d88-6e5d-44b2-b697-c55bd6a77974"
	scoped.ScopeID = strings.ToUpper(managed.ID)
	if _, _, err := svc.CreateInsightsReport(ctx, admin.User.ID, scoped); err == nil {
		t.Fatal("collation alias borrowed Project scope")
	}
	// Remove analysis authority while preserving creator-owned parameter CRUD.
	if removed := db.Where("role_id = ? AND permission = ?", role.Role.ID, "calls.read_all").Delete(&entity.RolePermission{}); removed.Error != nil || removed.RowsAffected != 1 {
		t.Fatal(removed.Error)
	}
	if _, _, err := svc.CreateInsightsReport(ctx, analyst.User.ID, scoped); err == nil {
		t.Fatal("revoked analysis authority saved a quota definition")
	}
	listed, err := svc.ListInsightsReports(ctx, analyst.User.ID, 20, "")
	if err != nil || len(listed.Items) != 2 {
		t.Fatal("creator lost definitions on analysis revocation", err)
	}
	if err := svc.DeleteInsightsReport(ctx, analyst.User.ID, managedDefinition.ID); err != nil {
		t.Fatal("creator could not remove definition after analysis revocation", err)
	}
	if err := svc.DeleteInsightsReport(ctx, analyst.User.ID, own.ID); err != nil {
		t.Fatal(err)
	}

}

type insightsSavedQuotaReadGuardKey struct{}

// Install while the callback registry has no active fixture workers. Cleanup
// runs after the caller's later-deferred runtime/recorder shutdown.
func installInsightsSavedQuotaReadGuard(t *testing.T, db *gorm.DB) func() {
	t.Helper()
	const callback = "insights_saved_quota_forbid_accounting_reads"
	if err := db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if enabled, _ := tx.Statement.Context.Value(insightsSavedQuotaReadGuardKey{}).(bool); !enabled {
			return
		}
		switch tx.Statement.Table {
		case "call_records", "call_attempts", "api_keys", "project_keys", "resource_limits", "pricing_settings", "quota_settings":
			_ = tx.AddError(assertNoSavedQuotaAccountingRead{})
		}
	}); err != nil {
		t.Fatal(err)
	}
	return func() {
		if err := db.Callback().Query().Remove(callback); err != nil {
			t.Error(err)
		}
	}
}

type assertNoSavedQuotaAccountingRead struct{}

func (assertNoSavedQuotaAccountingRead) Error() string {
	return "saved quota definition must not enumerate Keys or read accounting"
}

// V111 keeps the exact V110 receipts and demonstrates repeat, upgrade and
// interrupted-DDL behavior on the real driver supplied by the Insights registry.
func testInsightsQuotaReportMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	// This owned fixture changes result-column types deliberately. Retire each
	// physical connection so prepared plans cannot cross its historical DDL.
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxIdleConns(0)
	defer pool.SetMaxIdleConns(2)
	if head := personalKeyBehaviorLedger(t, db); len(head) != 112 {
		t.Fatal("quota reports require exact V111 head")
	}
	model := &entity.InsightsReport{}
	var count int64
	if err := db.Model(model).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("quota migration fixture must start empty", count, err)
	}
	removeReceipt := func(t *testing.T) {
		t.Helper()
		head := personalKeyBehaviorLedger(t, db)
		if len(head) != 112 {
			t.Fatal("exact quota report replay precondition")
		}
		removed := db.Table("schema_migrations").Where("version = ? AND applied_at = ?", 111, head[110].AppliedAt).Delete(&struct{}{})
		if removed.Error != nil || removed.RowsAffected != 1 {
			t.Fatal("remove exact V111 receipt", removed.Error)
		}
	}
	migrate := func(t *testing.T) {
		t.Helper()
		if err := database.Migrate(context.Background(), db); err != nil {
			t.Fatal("V111 migration", err)
		}
	}
	unchangedFailure := func(t *testing.T) {
		t.Helper()
		before := personalKeyBehaviorLedger(t, db)
		var rowsBefore []entity.InsightsReport
		if err := db.Order("id").Find(&rowsBefore).Error; err != nil {
			t.Fatal(err)
		}
		if err := database.Migrate(context.Background(), db); err == nil {
			t.Fatal("invalid V111 schema or retained data accepted")
		}
		if !reflect.DeepEqual(before, personalKeyBehaviorLedger(t, db)) {
			t.Fatal("failed V111 migration changed receipts")
		}
		var rowsAfter []entity.InsightsReport
		if err := db.Order("id").Find(&rowsAfter).Error; err != nil || len(rowsBefore) != len(rowsAfter) {
			t.Fatal("failed migration changed retained rows", err)
		}
		for i, row := range rowsBefore {
			if !insightsReportSameStoredDefinition(row, rowsAfter[i]) {
				t.Fatal("failed migration changed retained row")
			}
		}
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	legacy := entity.InsightsReport{ID: "isr_00000000000000000000000100", OwnerDigest: strings.Repeat("c", 64), RequestID: "d52b7d70-6e5d-44b2-b697-c55bd6a77974", IntentDigest: strings.Repeat("d", 64), Kind: "tokens_by_model", From: "2026-10-01T00:00:00.123456789Z", To: "2026-10-02T00:00:00.12345679Z", Timezone: "UTC", CreatedAt: at}
	cleared := legacy
	cleared.ID = "isr_00000000000000000000000101"
	cleared.RequestID = "d52b7d71-6e5d-44b2-b697-c55bd6a77974"
	cleared.Kind = ""
	cleared.From = ""
	cleared.To = ""
	cleared.Timezone = ""
	cleared.DeletedAt = &at
	if t.Failed() {
		return
	}
	t.Run("quota_report_empty_repeat_and_concurrent_startup", func(t *testing.T) {
		before := personalKeyBehaviorLedger(t, db)
		migrate(t)
		migrate(t)
		if !reflect.DeepEqual(before, personalKeyBehaviorLedger(t, db)) {
			t.Fatal("repeat rewrote immutable receipts")
		}
		removeReceipt(t)
		results := make(chan error, 2)
		var workers sync.WaitGroup
		for range 2 {
			workers.Go(func() { results <- database.Migrate(context.Background(), db) })
		}
		workers.Wait()
		close(results)
		for err := range results {
			if err != nil {
				t.Fatal("concurrent V111 startup", err)
			}
		}
		after := personalKeyBehaviorLedger(t, db)
		if len(after) != 112 || !reflect.DeepEqual(before[:110], after[:110]) {
			t.Fatal("concurrent startup changed retained prefix")
		}
		if db.Migrator().HasConstraint(model, "ck_insights_report_definition") || !db.Migrator().HasConstraint(model, "ck_insights_report_definition_v111") {
			t.Fatal("four-kind guard not exclusively installed")
		}
	})
	if t.Failed() {
		return
	}
	t.Run("quota_report_retained_v110_upgrade", func(t *testing.T) {
		if err := db.Create(&legacy).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&cleared).Error; err != nil {
			t.Fatal(err)
		}
		head := personalKeyBehaviorLedger(t, db)
		if err := insightsReportsPeelV111(db, head); err != nil {
			t.Fatal("genuine retained V110 predecessor", err)
		}
		if db.Migrator().HasColumn(model, "Dimension") || len(personalKeyBehaviorLedger(t, db)) != 110 || !db.Migrator().HasConstraint(model, "ck_insights_report_definition") {
			t.Fatal("V110 predecessor was not established")
		}
		migrate(t)
		for _, want := range []entity.InsightsReport{legacy, cleared} {
			var got entity.InsightsReport
			if err := db.Where("id = ?", want.ID).Take(&got).Error; err != nil || !insightsReportSameStoredDefinition(want, got) {
				t.Fatal("upgrade changed historical parameters or receipt", err)
			}
		}
		if !reflect.DeepEqual(head[:110], personalKeyBehaviorLedger(t, db)[:110]) {
			t.Fatal("upgrade rewrote released prefix")
		}
	})
	quota := legacy
	quota.ID = "isr_00000000000000000000000102"
	quota.RequestID = "d52b7d72-6e5d-44b2-b697-c55bd6a77974"
	quota.Kind = "key_quota"
	quota.From = ""
	quota.To = ""
	quota.Timezone = ""
	quota.Dimension = "tokens"
	quota.ScopeKind = "project"
	quota.ScopeID = "prj_quota_definition_scope"
	if t.Failed() {
		return
	}
	t.Run("quota_report_constraints_and_retained_replay", func(t *testing.T) {
		if err := db.Create(&quota).Error; err != nil {
			t.Fatal("six-character Tokens dimension must persist", err)
		}
		duplicate := quota
		duplicate.ID = "isr_00000000000000000000000103"
		if err := db.Create(&duplicate).Error; !errors.Is(err, gorm.ErrDuplicatedKey) {
			t.Fatal("intent uniqueness lost", err)
		}
		for i, mutate := range []func(*entity.InsightsReport){
			func(v *entity.InsightsReport) { v.Dimension = "TOKENS" },
			func(v *entity.InsightsReport) { v.ScopeKind = "Project" },
			func(v *entity.InsightsReport) { v.Kind = "KEY_QUOTA" },
			func(v *entity.InsightsReport) { v.From = legacy.From },
			func(v *entity.InsightsReport) { v.ScopeID = "" },
			func(v *entity.InsightsReport) { v.Kind = "team_quota" },
			func(v *entity.InsightsReport) { v.DeletedAt = &at },
		} {
			invalid := quota
			invalid.ID = fmt.Sprintf("isr_%026d", 110+i)
			invalid.RequestID = fmt.Sprintf("d52b7d%02x-6e5d-44b2-b697-c55bd6a77974", 0x73+i)
			mutate(&invalid)
			if err := db.Create(&invalid).Error; err == nil {
				t.Fatal("quota CHECK admitted mixed or collation-alias definition", i)
			}
		}
		head := personalKeyBehaviorLedger(t, db)
		if err := insightsReportsPeelV111(db, head); err == nil || !reflect.DeepEqual(head, personalKeyBehaviorLedger(t, db)) {
			t.Fatal("quota parameters lost by historical fixture peel")
		}
		removeReceipt(t)
		migrate(t)
		var retained entity.InsightsReport
		if err := db.Where("id = ?", quota.ID).Take(&retained).Error; err != nil || !insightsReportSameStoredDefinition(quota, retained) {
			t.Fatal("replay changed quota definition", err)
		}
		if err := db.Where("id = ?", quota.ID).Delete(model).Error; err != nil {
			t.Fatal(err)
		}
	})
	if t.Failed() {
		return
	}
	t.Run("quota_report_partial_new_columns_and_invalid_metadata", func(t *testing.T) {
		// Reproduce the exact interrupted boundary after the first new MySQL column
		// was added. Real legacy definitions and deletion receipts remain retained.
		head := personalKeyBehaviorLedger(t, db)
		if err := insightsReportsPeelV111(db, head); err != nil {
			t.Fatal(err)
		}
		if err := db.Migrator().AddColumn(&insightsQuotaDimensionV111{}, "Dimension"); err != nil {
			t.Fatal(err)
		}
		migrate(t)
		for _, want := range []entity.InsightsReport{legacy, cleared} {
			var got entity.InsightsReport
			if err := db.Where("id = ?", want.ID).Take(&got).Error; err != nil || !insightsReportSameStoredDefinition(want, got) {
				t.Fatal("partial DDL backfill changed retained legacy row", err)
			}
		}
		for _, malformed := range []any{&insightsQuotaNarrowDimensionV111{}, &insightsQuotaNullableDimensionV111{}, &insightsQuotaWrongDefaultDimensionV111{}} {
			removeReceipt(t)
			if err := db.Migrator().AlterColumn(malformed, "Dimension"); err != nil {
				t.Fatal("construct incompatible new column", err)
			}
			unchangedFailure(t)
			if err := db.Migrator().AlterColumn(&insightsQuotaDimensionV111{}, "Dimension"); err != nil {
				t.Fatal(err)
			}
			migrate(t)
		}
		removeReceipt(t)
		if err := database.DropIndex(db, model, "idx_insights_report_owner"); err != nil {
			t.Fatal(err)
		}
		if err := db.Migrator().CreateIndex(&insightsReportReversedIndexV110{}, "idx_insights_report_owner"); err != nil {
			t.Fatal(err)
		}
		unchangedFailure(t)
		if err := database.DropIndex(db, model, "idx_insights_report_owner"); err != nil {
			t.Fatal(err)
		}
		if err := db.Migrator().CreateIndex(model, "idx_insights_report_owner"); err != nil {
			t.Fatal(err)
		}
		migrate(t)
	})
	if t.Failed() {
		return
	}
	t.Run("quota_report_weakened_guard_requires_valid_retained_rows", func(t *testing.T) {
		if err := db.Migrator().DropConstraint(model, "ck_insights_report_definition_v111"); err != nil {
			t.Fatal(err)
		}
		if err := db.Migrator().CreateConstraint(&insightsQuotaWeakGuardV111{}, "ck_insights_report_definition_v111"); err != nil {
			t.Fatal(err)
		}
		invalid := quota
		invalid.Kind = "KEY_QUOTA"
		if err := db.Create(&invalid).Error; err != nil {
			t.Fatal("weakened guard negative must actually insert", err)
		}
		removeReceipt(t)
		unchangedFailure(t)
		if err := db.Where("id = ?", invalid.ID).Delete(model).Error; err != nil {
			t.Fatal(err)
		}
		migrate(t)
		if err := db.Create(&invalid).Error; err == nil {
			t.Fatal("weakened guard survived successful replay")
		}
	})
	if err := db.Where("owner_digest = ?", legacy.OwnerDigest).Delete(model).Error; err != nil {
		t.Fatal(err)
	}
}

type insightsQuotaDimensionV111 struct {
	Dimension string `gorm:"size:6;not null;default:''"`
}

func (insightsQuotaDimensionV111) TableName() string { return "insights_reports" }

type insightsQuotaNarrowDimensionV111 struct {
	Dimension string `gorm:"size:5;not null;default:''"`
}

func (insightsQuotaNarrowDimensionV111) TableName() string { return "insights_reports" }

type insightsQuotaNullableDimensionV111 struct {
	Dimension string `gorm:"size:6;default:''"`
}

func (insightsQuotaNullableDimensionV111) TableName() string { return "insights_reports" }

type insightsQuotaWrongDefaultDimensionV111 struct {
	Dimension string `gorm:"size:6;not null;default:money"`
}

func (insightsQuotaWrongDefaultDimensionV111) TableName() string { return "insights_reports" }

type insightsQuotaWeakGuardV111 struct {
	Kind string `gorm:"check:ck_insights_report_definition_v111,kind IS NOT NULL"`
}

func (insightsQuotaWeakGuardV111) TableName() string { return "insights_reports" }
