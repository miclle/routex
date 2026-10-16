package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"gorm.io/gorm"
)

// The existing Insights scenario owns these controls on each real database.
func testInsightsReportDefinitions(t *testing.T, db *gorm.DB, svc *service.Service, router http.Handler, admin SessionResponse, cookie *http.Cookie) {
	t.Helper()
	ctx := db.Statement.Context
	post := func(body string, csrf string) *httptest.ResponseRecorder {
		return identityRequest(router, http.MethodPost, "/api/v1/insights/reports", body, cookie, csrf)
	}
	get := func(query string) *httptest.ResponseRecorder {
		return identityRequest(router, http.MethodGet, "/api/v1/insights/reports"+query, "", cookie, "")
	}
	remove := func(reportID string) *httptest.ResponseRecorder {
		return identityRequest(router, http.MethodDelete, "/api/v1/insights/reports/"+reportID, "", cookie, admin.CSRFToken)
	}
	var original entity.User
	if err := db.First(&original, "id = ?", admin.User.ID).Error; err != nil {
		t.Fatal(err)
	}
	other := entity.User{ID: "usr_insights_reports_other", Email: "insights-reports-other@example.invalid", Name: "Other creator", Role: entity.RoleAdmin, PasswordHash: original.PasswordHash, CreatedAt: time.Now().UTC().Truncate(time.Microsecond), UpdatedAt: time.Now().UTC()}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	login := identityRequest(router, http.MethodPost, "/api/v1/auth/login", `{"email":"insights-reports-other@example.invalid","password":"test-only-insights-password"}`, nil, "")
	expectStatus(t, login, 200)
	otherSession, otherCookie := readIdentity(t, login)
	t.Run("saved_definition_private_replay_and_deletion", func(t *testing.T) {
		expectStatus(t, post(insightsReportValidBody, ""), 403)
		expectStatus(t, identityRequest(router, http.MethodGet, "/api/v1/insights/reports", "", nil, ""), 401)
		fresh := post(insightsReportValidBody, admin.CSRFToken)
		expectStatus(t, fresh, 201)
		if fresh.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("saved definition must stay private")
		}
		var definition service.InsightsReportDefinition
		if err := json.Unmarshal(fresh.Body.Bytes(), &definition); err != nil || definition.From != "2026-10-01T00:00:00.123456789Z" || definition.To != "2026-10-02T00:00:00.12345679Z" {
			t.Fatal("definition exact instants", definition, err)
		}
		replay := post(insightsReportValidBody, admin.CSRFToken)
		expectStatus(t, replay, 200)
		var replayed service.InsightsReportDefinition
		if err := json.Unmarshal(replay.Body.Bytes(), &replayed); err != nil || !reflect.DeepEqual(definition, replayed) {
			t.Fatal("replay changed definition", err)
		}
		changed := strings.Replace(insightsReportValidBody, "123456790", "123456791", 1)
		expectStatus(t, post(changed, admin.CSRFToken), 409)
		expectStatus(t, post(strings.TrimSuffix(insightsReportValidBody, "}")+`,"question":"private"}`, admin.CSRFToken), 400)
		foreign := identityRequest(router, http.MethodDelete, "/api/v1/insights/reports/"+definition.ID, "", otherCookie, otherSession.CSRFToken)
		expectStatus(t, foreign, 404)
		foreignList := identityRequest(router, http.MethodGet, "/api/v1/insights/reports", "", otherCookie, "")
		expectStatus(t, foreignList, 200)
		if strings.Contains(foreignList.Body.String(), definition.ID) {
			t.Fatal("administrator read another creator definition")
		}
		expectStatus(t, remove(strings.ToUpper(definition.ID)), 404)
		var aggregateCalls, attempts int64
		if err := db.Model(&entity.CallRecord{}).Count(&aggregateCalls).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&entity.CallAttempt{}).Count(&attempts).Error; err != nil {
			t.Fatal(err)
		}
		permission := entity.RolePermission{RoleID: "rol_admin", Permission: "calls.read_all"}
		if result := db.Where("role_id = ? AND permission = ?", permission.RoleID, permission.Permission).Delete(&entity.RolePermission{}); result.Error != nil || result.RowsAffected != 1 {
			t.Fatal("revoke saved-report creation authority", result.Error)
		}
		func() {
			defer func() {
				if err := db.Create(&permission).Error; err != nil {
					t.Error(err)
				}
			}()
			expectStatus(t, post(strings.Replace(insightsReportValidBody, "d52b7d9c", "d52b7d9d", 1), admin.CSRFToken), 403)
			listWhileRevoked := get("")
			expectStatus(t, listWhileRevoked, 200)
			if listWhileRevoked.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatal("saved list must stay private")
			}
			expectStatus(t, remove(definition.ID), 204)
		}()
		var receipt entity.InsightsReport
		if err := db.First(&receipt, "id = ?", definition.ID).Error; err != nil || receipt.Kind != "" || receipt.From != "" || receipt.To != "" || receipt.Timezone != "" || receipt.DeletedAt == nil || receipt.IntentDigest == "" {
			t.Fatal("explicit deletion did not clear definition", err)
		}
		expectStatus(t, remove(definition.ID), 404)
		expectStatus(t, post(insightsReportValidBody, admin.CSRFToken), 410)
		expectStatus(t, post(changed, admin.CSRFToken), 409)
		list := get("")
		expectStatus(t, list, 200)
		if strings.Contains(list.Body.String(), definition.ID) {
			t.Fatal("deleted definition still listed")
		}
		var callsAfter, attemptsAfter int64
		if err := db.Model(&entity.CallRecord{}).Count(&callsAfter).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&entity.CallAttempt{}).Count(&attemptsAfter).Error; err != nil {
			t.Fatal(err)
		}
		if callsAfter != aggregateCalls || attemptsAfter != attempts {
			t.Fatal("definition CRUD executed inference")
		}
	})
	t.Run("saved_definition_authority_pagination_and_concurrency", func(t *testing.T) {
		value := service.CreateInsightsReportInput{RequestID: "d52b7d9e-6e5d-44b2-b697-c55bd6a77974", Kind: "provider_quality", From: "2026-10-01T00:00:00.123456789Z", To: "2026-10-02T00:00:00.123456790Z", Timezone: "UTC"}
		permission := entity.RolePermission{RoleID: "rol_admin", Permission: "providers.read"}
		if result := db.Where("role_id = ? AND permission = ?", permission.RoleID, permission.Permission).Delete(&entity.RolePermission{}); result.Error != nil || result.RowsAffected != 1 {
			t.Fatal(result.Error)
		}
		_, _, err := svc.CreateInsightsReport(ctx, admin.User.ID, value)
		if err == nil {
			t.Fatal("Provider report accepted without independent read")
		}
		if err := db.Create(&permission).Error; err != nil {
			t.Fatal(err)
		}
		results := make(chan *service.InsightsReportDefinition, 2)
		errs := make(chan error, 2)
		var workers sync.WaitGroup
		for range 2 {
			workers.Go(func() {
				row, _, err := svc.CreateInsightsReport(ctx, admin.User.ID, value)
				results <- row
				errs <- err
			})
		}
		workers.Wait()
		close(results)
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal("concurrent same-intent save", err)
			}
		}
		ids := []string{}
		for row := range results {
			if row == nil {
				t.Fatal("missing concurrent receipt")
			}
			ids = append(ids, row.ID)
		}
		if len(ids) != 2 || ids[0] != ids[1] {
			t.Fatal("same intent created multiple definitions", ids)
		}
		value.RequestID = "d52b7d9f-6e5d-44b2-b697-c55bd6a77974"
		value.Kind = "tokens_by_model"
		second, _, err := svc.CreateInsightsReport(ctx, admin.User.ID, value)
		if err != nil {
			t.Fatal(err)
		}
		firstPage, err := svc.ListInsightsReports(ctx, admin.User.ID, 1, "")
		if err != nil || len(firstPage.Items) != 1 || firstPage.NextCursor == "" {
			t.Fatal("bounded page", firstPage, err)
		}
		next, err := svc.ListInsightsReports(ctx, admin.User.ID, 1, firstPage.NextCursor)
		if err != nil || len(next.Items) != 1 || next.NextCursor != "" || next.Items[0].ID == firstPage.Items[0].ID {
			t.Fatal("stable next page", next, err)
		}
		if _, err := svc.ListInsightsReports(ctx, other.ID, 1, firstPage.NextCursor); err == nil {
			t.Fatal("foreign-owner cursor admitted")
		}
		if _, err := svc.ListInsightsReports(ctx, strings.ToUpper(admin.User.ID), 20, ""); err == nil {
			t.Fatal("case alias borrowed report owner")
		}
		if err := db.Model(&entity.User{}).Where("id = ?", other.ID).UpdateColumn("disabled", true).Error; err != nil {
			t.Fatal(err)
		}
		expectStatus(t, identityRequest(router, http.MethodGet, "/api/v1/insights/reports", "", otherCookie, ""), 401)
		if _, err := svc.ListInsightsReports(ctx, other.ID, 20, ""); err == nil {
			t.Fatal("disabled creator listed reports")
		}
		if err := svc.DeleteInsightsReport(ctx, other.ID, second.ID); err == nil {
			t.Fatal("disabled creator deleted report")
		}
		if err := db.Model(&entity.User{}).Where("id = ?", other.ID).UpdateColumn("disabled", false).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Where("user_id = ?", other.ID).Delete(&entity.Session{}).Error; err != nil {
			t.Fatal(err)
		}
		expectStatus(t, identityRequest(router, http.MethodGet, "/api/v1/insights/reports", "", otherCookie, ""), 401)
		// A recreated identity must not inherit this creator's private records.
		changedBirth := original.CreatedAt.Add(time.Microsecond)
		if err := db.Model(&entity.User{}).Where("id = ?", original.ID).UpdateColumn("created_at", changedBirth).Error; err != nil {
			t.Fatal(err)
		}
		reborn, err := svc.ListInsightsReports(ctx, original.ID, 20, "")
		if err != nil || len(reborn.Items) != 0 {
			t.Fatal("recreated actor inherited definitions", err)
		}
		if err := svc.DeleteInsightsReport(ctx, original.ID, second.ID); err == nil {
			t.Fatal("recreated actor deleted predecessor definition")
		}
		if err := db.Model(&entity.User{}).Where("id = ?", original.ID).UpdateColumn("created_at", original.CreatedAt).Error; err != nil {
			t.Fatal(err)
		}
		// Neither retention nor execution authority changes the definition's lifetime.
		if err := db.Model(&entity.InsightsReport{}).Where("id = ?", second.ID).UpdateColumn("created_at", time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)).Error; err != nil {
			t.Fatal(err)
		}
		retained, err := svc.ListInsightsReports(ctx, admin.User.ID, 20, "")
		if err != nil || len(retained.Items) != 2 {
			t.Fatal("private definition unexpectedly expired", err)
		}

		ownInput := value
		ownInput.RequestID = "d52b7d91-6e5d-44b2-b697-c55bd6a77974"
		own, _, err := svc.CreateInsightsReport(ctx, other.ID, ownInput)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&entity.User{}).Where("id = ?", other.ID).UpdateColumn("role", entity.RoleMember).Error; err != nil {
			t.Fatal(err)
		}
		ownList, err := svc.ListInsightsReports(ctx, other.ID, 20, "")
		if err != nil || len(ownList.Items) != 1 || ownList.Items[0].ID != own.ID {
			t.Fatal("member lost private definition management", err)
		}
		ownInput.RequestID = "d52b7d92-6e5d-44b2-b697-c55bd6a77974"
		if _, _, err := svc.CreateInsightsReport(ctx, other.ID, ownInput); err == nil {
			t.Fatal("member created aggregate definition without authority")
		}
		if err := svc.DeleteInsightsReport(ctx, other.ID, own.ID); err != nil {
			t.Fatal("member could not explicitly clear own definition", err)
		}
		// A concurrent replay may observe the existing definition before delete,
		// or its cleared receipt afterward; it can never create a second row.
		raceErrors := make(chan error, 2)
		workers.Go(func() { raceErrors <- svc.DeleteInsightsReport(ctx, admin.User.ID, second.ID) })
		workers.Go(func() {
			_, _, err := svc.CreateInsightsReport(ctx, admin.User.ID, value)
			if err != nil && err.Error() != "saved report was explicitly deleted" {
				raceErrors <- err
				return
			}
			raceErrors <- nil
		})
		workers.Wait()
		close(raceErrors)
		for err := range raceErrors {
			if err != nil {
				t.Fatal("delete/replay race", err)
			}
		}
		var cleared entity.InsightsReport
		if err := db.First(&cleared, "id = ?", second.ID).Error; err != nil || cleared.DeletedAt == nil || cleared.Kind != "" || cleared.From != "" || cleared.To != "" || cleared.Timezone != "" {
			t.Fatal("delete race resurrected parameters", err)
		}
		if _, _, err := svc.CreateInsightsReport(ctx, admin.User.ID, value); err == nil {
			t.Fatal("deleted intent replay resurrected after race")
		}
	})
	t.Run("quota_saved_definition_scope_replay_and_deletion", func(t *testing.T) { testInsightsQuotaReportDefinitions(t, db, svc, router, admin, cookie) })
}

func testInsightsReportMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	before := personalKeyBehaviorLedger(t, db)
	if len(before) != 118 || before[110].Version != 111 {
		t.Fatal("saved report requires current V111 head")
	}
	// Genuine V109 upgrade retains unrelated existing data and exact receipts.
	example := entity.Example{Title: "saved report predecessor retained data"}
	if err := db.Create(&example).Error; err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func([]personalKeyBehaviorMigrationEntry){func(v []personalKeyBehaviorMigrationEntry) { v[109].AppliedAt = "changed" }, func(v []personalKeyBehaviorMigrationEntry) { v[109].Version = 111 }} {
		invalid := append([]personalKeyBehaviorMigrationEntry(nil), before...)
		mutate(invalid)
		if err := insightsReportsPeelV110(db, invalid); err == nil || !reflect.DeepEqual(before, personalKeyBehaviorLedger(t, db)) {
			t.Fatal("invalid V110 receipt changed predecessor", err)
		}
	}
	if err := insightsReportsPeelV110(db, before); err != nil {
		t.Fatal("empty V110 fixture peel", err)
	}
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal("existing V109 upgrade", err)
	}
	var retainedExample entity.Example
	if err := db.First(&retainedExample, example.ID).Error; err != nil || retainedExample.Title != example.Title {
		t.Fatal("V110 upgrade changed unrelated existing data", err)
	}
	if err := db.Delete(&example).Error; err != nil {
		t.Fatal(err)
	}
	row := entity.InsightsReport{ID: "isr_00000000000000000000000001", OwnerDigest: strings.Repeat("a", 64), RequestID: "d52b7d9c-6e5d-44b2-b697-c55bd6a77974", IntentDigest: strings.Repeat("b", 64), Kind: "tokens_by_model", From: "2026-10-01T00:00:00.123456789Z", To: "2026-10-02T00:00:00.12345679Z", Timezone: "UTC", CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal("valid retained definition baseline", err)
	}
	retainedHead := personalKeyBehaviorLedger(t, db)
	if err := insightsReportsPeelV110(db, retainedHead); err == nil || err.Error() != "retained saved report receipt prevents historical peel" || !reflect.DeepEqual(retainedHead, personalKeyBehaviorLedger(t, db)) {
		t.Fatal("retained saved report peel must preserve head", err)
	}
	replay := func() {
		t.Helper()
		if err := insightsReportsPeelV111(db, personalKeyBehaviorLedger(t, db)); err != nil {
			t.Fatal("restore exact retained V110 schema", err)
		}
		removed := db.Table("schema_migrations").Where("version = ? AND applied_at = ?", 110, personalKeyBehaviorLedger(t, db)[109].AppliedAt).Delete(&struct{}{})
		if removed.Error != nil || removed.RowsAffected != 1 {
			t.Fatal("remove exact V110 receipt", removed.Error)
		}
		if err := database.Migrate(context.Background(), db); err != nil {
			t.Fatal(err)
		}
	}
	replay()
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	after := personalKeyBehaviorLedger(t, db)
	if len(after) != 118 || after[110].Version != 111 || !reflect.DeepEqual(before[:109], after[:109]) {
		t.Fatal("V110 replay changed immutable prefix")
	}
	var retained entity.InsightsReport
	if err := db.First(&retained, "id = ?", row.ID).Error; err != nil || !insightsReportSameStoredDefinition(row, retained) {
		t.Fatal("retained exact definition changed", err)
	}
	duplicate := row
	duplicate.ID = "isr_00000000000000000000000002"
	if err := db.Create(&duplicate).Error; !errors.Is(err, gorm.ErrDuplicatedKey) {
		t.Fatal("owner intent unique constraint", err)
	}
	for i, mutate := range []func(*entity.InsightsReport){func(v *entity.InsightsReport) { v.Kind = "TOKENS_BY_MODEL" }, func(v *entity.InsightsReport) { v.From = "" }, func(v *entity.InsightsReport) { v.DeletedAt = &v.CreatedAt }, func(v *entity.InsightsReport) { v.Kind = ""; v.From = ""; v.To = ""; v.Timezone = "" }} {
		invalid := row
		invalid.ID = "isr_0000000000000000000000000" + string(rune('3'+i))
		invalid.RequestID = "d52b7d9" + string(rune('a'+i)) + "-6e5d-44b2-b697-c55bd6a77974"
		mutate(&invalid)
		if err := db.Create(&invalid).Error; err == nil {
			t.Fatal("saved report CHECK admitted invalid lifecycle", i)
		}
	}
	cleared := row
	cleared.ID = "isr_00000000000000000000000009"
	cleared.RequestID = "d52b7d90-6e5d-44b2-b697-c55bd6a77974"
	cleared.Kind = ""
	cleared.From = ""
	cleared.To = ""
	cleared.Timezone = ""
	cleared.DeletedAt = &cleared.CreatedAt
	if err := db.Create(&cleared).Error; err != nil {
		t.Fatal("valid cleared receipt baseline", err)
	}
	replay()
	var retainedCleared entity.InsightsReport
	if err := db.First(&retainedCleared, "id = ?", cleared.ID).Error; err != nil || !insightsReportSameStoredDefinition(cleared, retainedCleared) {
		t.Fatal("deleted receipt changed during replay", err)
	}
	if err := db.Where("id = ?", row.ID).Delete(&entity.InsightsReport{}).Error; err != nil {
		t.Fatal(err)
	}
	onlyReceiptHead := personalKeyBehaviorLedger(t, db)
	if err := insightsReportsPeelV110(db, onlyReceiptHead); err == nil || err.Error() != "retained saved report receipt prevents historical peel" || !reflect.DeepEqual(onlyReceiptHead, personalKeyBehaviorLedger(t, db)) {
		t.Fatal("cleared intent receipt must prevent destructive peel", err)
	}
	if err := db.Where("owner_digest = ?", row.OwnerDigest).Delete(&entity.InsightsReport{}).Error; err != nil {
		t.Fatal(err)
	}
	testInsightsReportPartialMigration(t, db, row)
}

// Database timestamps preserve instants, not the Go driver's Location pointer.
// Compare every retained field exactly while canonicalizing that representation.
func insightsReportSameStoredDefinition(a, b entity.InsightsReport) bool {
	a.CreatedAt, b.CreatedAt = a.CreatedAt.UTC(), b.CreatedAt.UTC()
	if a.DeletedAt != nil {
		value := a.DeletedAt.UTC()
		a.DeletedAt = &value
	}
	if b.DeletedAt != nil {
		value := b.DeletedAt.UTC()
		b.DeletedAt = &value
	}
	return reflect.DeepEqual(a, b)
}

// A historical fixture may remove V110 only after validating the full receipt
// and proving no definition or minimal deleted-intent receipt would be lost.
func insightsReportsPeelV110(db *gorm.DB, expected []personalKeyBehaviorMigrationEntry) error {
	if len(expected) < 1 || len(expected) > 118 {
		return errors.New("unsupported saved report fixture head")
	}
	for i, row := range expected {
		if row.Version != i+1 || row.AppliedAt == "" {
			return errors.New("invalid saved report fixture receipts")
		}
	}
	var actual []personalKeyBehaviorMigrationEntry
	if err := db.Table("schema_migrations").Order("version").Find(&actual).Error; err != nil || !reflect.DeepEqual(expected, actual) {
		return errors.New("saved report fixture receipt mismatch")
	}
	if len(expected) < 110 {
		return nil
	}
	var count int64
	if err := db.Model(&entity.InsightsReport{}).Count(&count).Error; err != nil || count != 0 {
		return errors.New("retained saved report receipt prevents historical peel")
	}
	if len(expected) == 112 || len(expected) == 113 || len(expected) == 114 || len(expected) == 115 || len(expected) == 116 || len(expected) == 117 || len(expected) == 118 {
		if err := runtimeArchivePeelV112(db, expected); err != nil {
			return err
		}
		expected = expected[:111]
	}
	if err := db.Migrator().DropTable(&entity.InsightsReport{}); err != nil {
		return err
	}
	for index := len(expected) - 1; index >= 109; index-- {
		removed := db.Table("schema_migrations").Where("version = ? AND applied_at = ?", index+1, expected[index].AppliedAt).Delete(&struct{}{})
		if removed.Error != nil || removed.RowsAffected != 1 {
			return errors.New("exact saved report receipt removal failed")
		}
	}
	actual = nil
	if err := db.Table("schema_migrations").Order("version").Find(&actual).Error; err != nil || !reflect.DeepEqual(actual, expected[:109]) {
		return errors.New("saved report peel changed retained prefix")
	}
	return nil
}

type insightsReportWeakCheckV110 struct {
	Kind string `gorm:"size:20;not null;check:ck_insights_report_definition,kind IS NOT NULL"`
}

func (insightsReportWeakCheckV110) TableName() string { return "insights_reports" }

type insightsReportWrongIndexV110 struct {
	Kind string `gorm:"index:idx_insights_report_owner"`
}

func (insightsReportWrongIndexV110) TableName() string { return "insights_reports" }

// Both columns are valid, but reversing the physical key must be rejected.
type insightsReportReversedIndexV110 struct {
	ID          string `gorm:"index:idx_insights_report_owner,priority:1"`
	OwnerDigest string `gorm:"index:idx_insights_report_owner,priority:2"`
}

func (insightsReportReversedIndexV110) TableName() string { return "insights_reports" }

type insightsReportNarrowTimezoneV110 struct {
	Timezone string `gorm:"size:99;not null"`
}

func (insightsReportNarrowTimezoneV110) TableName() string { return "insights_reports" }

type insightsReportNullableTimezoneV110 struct {
	Timezone *string `gorm:"size:100"`
}

func (insightsReportNullableTimezoneV110) TableName() string { return "insights_reports" }

type insightsReportWrongTypeTimezoneV110 struct {
	Timezone int64 `gorm:"not null"`
}

func (insightsReportWrongTypeTimezoneV110) TableName() string { return "insights_reports" }

type insightsReportDefaultTimezoneV110 struct {
	Timezone string `gorm:"size:100;not null;default:UTC"`
}

func (insightsReportDefaultTimezoneV110) TableName() string { return "insights_reports" }

func testInsightsReportPartialMigration(t *testing.T, db *gorm.DB, row entity.InsightsReport) {
	t.Helper()
	model := &entity.InsightsReport{}
	deleteReceipt := func() {
		t.Helper()
		if err := insightsReportsPeelV111(db, personalKeyBehaviorLedger(t, db)); err != nil {
			t.Fatal("restore exact V110 partial fixture", err)
		}
		ledger := personalKeyBehaviorLedger(t, db)
		if len(ledger) != 110 {
			t.Fatal("exact V110 replay precondition")
		}
		removed := db.Table("schema_migrations").Where("version = ? AND applied_at = ?", 110, ledger[109].AppliedAt).Delete(&struct{}{})
		if removed.Error != nil || removed.RowsAffected != 1 {
			t.Fatal("exact saved report replay receipt", removed.Error)
		}
	}
	restore := func() {
		t.Helper()
		if err := database.Migrate(context.Background(), db); err != nil {
			t.Fatal("restore V110", err)
		}
	}
	unchangedFailure := func() {
		t.Helper()
		before := personalKeyBehaviorLedger(t, db)
		if err := database.Migrate(context.Background(), db); err == nil {
			t.Fatal("invalid V110 partial schema accepted")
		}
		if !reflect.DeepEqual(before, personalKeyBehaviorLedger(t, db)) {
			t.Fatal("failed saved report migration changed ledger")
		}
	}
	// Empty interrupted DDL adds the missing column and restores its CHECK.
	deleteReceipt()
	if err := db.Migrator().DropConstraint(model, "ck_insights_report_definition"); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropColumn(model, "Timezone"); err != nil {
		t.Fatal(err)
	}
	restore()
	for _, malformed := range []any{&insightsReportNarrowTimezoneV110{}, &insightsReportNullableTimezoneV110{}, &insightsReportDefaultTimezoneV110{}} {
		deleteReceipt()
		if err := db.Migrator().AlterColumn(malformed, "Timezone"); err != nil {
			t.Fatal("controlled malformed empty schema", err)
		}
		unchangedFailure()
		if err := db.Migrator().AlterColumn(model, "Timezone"); err != nil {
			t.Fatal(err)
		}
		restore()
	}
	deleteReceipt()
	if err := db.Migrator().DropConstraint(model, "ck_insights_report_definition"); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().AlterColumn(&insightsReportWrongTypeTimezoneV110{}, "Timezone"); err != nil {
		t.Fatal("controlled wrong type", err)
	}
	unchangedFailure()
	if err := db.Migrator().AlterColumn(model, "Timezone"); err != nil {
		t.Fatal(err)
	}
	restore()
	deleteReceipt()
	if err := database.DropIndex(db, model, "idx_insights_report_owner"); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().CreateIndex(&insightsReportWrongIndexV110{}, "idx_insights_report_owner"); err != nil {
		t.Fatal(err)
	}
	unchangedFailure()
	if err := database.DropIndex(db, model, "idx_insights_report_owner"); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().CreateIndex(&insightsReportReversedIndexV110{}, "idx_insights_report_owner"); err != nil {
		t.Fatal(err)
	}
	unchangedFailure()
	if err := database.DropIndex(db, model, "idx_insights_report_owner"); err != nil {
		t.Fatal(err)
	}
	restore()
	// A same-name weakened CHECK cannot bless retained invalid data or survive
	// a successful replay when every retained row satisfies the frozen predicate.
	if err := insightsReportsPeelV111(db, personalKeyBehaviorLedger(t, db)); err != nil {
		t.Fatal("restore exact V110 weak-check fixture", err)
	}
	if err := db.Migrator().DropConstraint(model, "ck_insights_report_definition"); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().CreateConstraint(&insightsReportWeakCheckV110{}, "ck_insights_report_definition"); err != nil {
		t.Fatal(err)
	}
	if err := db.Omit("Dimension", "ScopeKind", "ScopeID").Create(&row).Error; err != nil {
		t.Fatal("valid retained partial baseline", err)
	}
	invalid := row
	invalid.ID = "isr_00000000000000000000000008"
	invalid.RequestID = "d52b7d98-6e5d-44b2-b697-c55bd6a77974"
	invalid.Kind = "TOKENS_BY_MODEL"
	if err := db.Omit("Dimension", "ScopeKind", "ScopeID").Create(&invalid).Error; err != nil {
		t.Fatal("weak CHECK negative fixture must actually insert", err)
	}
	deleteReceipt()
	unchangedFailure()
	var count int64
	if err := db.Model(model).Count(&count).Error; err != nil || count != 2 {
		t.Fatal("invalid retained rows were changed", err, count)
	}
	if err := db.Where("id = ?", invalid.ID).Delete(model).Error; err != nil {
		t.Fatal(err)
	}
	restore()
	if err := db.Omit("Dimension", "ScopeKind", "ScopeID").Create(&invalid).Error; err == nil {
		t.Fatal("weak same-name CHECK survived replay")
	}
	if err := db.Where("id = ?", row.ID).Delete(model).Error; err != nil {
		t.Fatal(err)
	}
}
