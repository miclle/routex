package handler

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// Root must register the private V61 candidate after V60 before appending this
// scenario. It neither reaches an unexported migration nor bypasses the ledger.
func testProjectMonthlyQuotaWarningMigration(t *testing.T, db *gorm.DB) {
	const candidateVersion = 61
	ctx := context.Background()
	observationModel, inboxModel := &entity.ProjectQuotaWarningObservation{}, &entity.ProjectQuotaWarningInbox{}
	// Seed nonempty predecessor history with a saved read timestamp, so upgrade
	// preservation is meaningful even when the harness resets each scenario.
	historyStart := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	historyAsOf := historyStart.Add(time.Hour)
	historyRead := historyAsOf.Add(time.Hour)
	personalHistory := entity.QuotaWarningObservation{ID: "qwo_v61_personal_history", OwnerID: "usr_v61_personal_history", Dimension: "tokens", MonthStart: historyStart, MonthEnd: historyStart.AddDate(0, 1, 0), PolicyRevision: "lim_v61_personal_history", Level: "near", Threshold: 80, ThresholdGeneration: "personal-monthly-80-90-v1", TimeZone: "UTC", AsOf: historyAsOf, Limit: "10", Settled: "8", CoverageStart: historyStart, ResourceCreatedAt: historyStart}
	personalReceipt := entity.QuotaWarningInbox{ID: "qwi_v61_personal_history", ObservationID: personalHistory.ID, RecipientID: personalHistory.OwnerID, ReadAt: &historyRead, CreatedAt: historyAsOf}
	exhaustionHistory := entity.QuotaNotificationObservation{ID: "qob_v61_exhaust_history", ScopeKind: "team", ScopeID: "tem_v61_exhaust_history", ScopeName: "Retained Team", Dimension: "tokens", PolicyRevision: "lim_v61_exhaust_history", MonthStart: historyStart, MonthEnd: historyStart.AddDate(0, 1, 0), TimeZone: "UTC", AsOf: historyAsOf, Limit: "10", Settled: "10", CoverageStart: historyStart, ResourceCreatedAt: historyStart}
	exhaustionReceipt := entity.QuotaNotificationInbox{ID: "qni_v61_exhaust_history", ObservationID: exhaustionHistory.ID, RecipientID: "usr_v61_exhaust_history", ReadAt: &historyRead, CreatedAt: historyAsOf}
	for _, row := range []any{&personalHistory, &personalReceipt, &exhaustionHistory, &exhaustionReceipt} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal("predecessor history setup", err)
		}
	}
	var personalWarningsBefore []entity.QuotaWarningObservation
	var personalInboxesBefore []entity.QuotaWarningInbox
	if err := db.Order("id").Find(&personalWarningsBefore).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Order("id").Find(&personalInboxesBefore).Error; err != nil {
		t.Fatal(err)
	}
	teamHistory := entity.TeamQuotaWarningObservation{ID: "two_v61_team_history", TeamID: "tem_v61_team_history", TeamName: "Retained Team", Dimension: "tokens", MonthStart: historyStart, MonthEnd: historyStart.AddDate(0, 1, 0), PolicyRevision: "lim_v61_team_history", Level: "near", Threshold: 80, ThresholdGeneration: "team-monthly-80-90-v1", TimeZone: "UTC", AsOf: historyAsOf, Limit: "10", Settled: "8", CoverageStart: historyStart, ResourceCreatedAt: historyStart}
	teamReceipt := entity.TeamQuotaWarningInbox{ID: "twi_v61_team_history", ObservationID: teamHistory.ID, RecipientID: "usr_v61_team_history", RecipientCreatedAt: historyStart, ReadAt: &historyRead, CreatedAt: historyAsOf}
	if err := db.Create(&teamHistory).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&teamReceipt).Error; err != nil {
		t.Fatal(err)
	}
	var teamWarningsBefore []entity.TeamQuotaWarningObservation
	var teamInboxesBefore []entity.TeamQuotaWarningInbox
	if err := db.Order("id").Find(&teamWarningsBefore).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Order("id").Find(&teamInboxesBefore).Error; err != nil {
		t.Fatal(err)
	}
	// Pin exact incoming ordered ledger rather than weakening it to a total count.
	var baseline []int
	if err := db.Table("schema_migrations").Order("version").Pluck("version", &baseline).Error; err != nil {
		t.Fatal(err)
	}
	withoutCandidate := []int{}
	candidateCount := 0
	for index, version := range baseline {
		if index > 0 && version <= baseline[index-1] {
			t.Fatal("incoming ledger is not ordered unique")
		}
		if version == candidateVersion {
			candidateCount++
		} else {
			withoutCandidate = append(withoutCandidate, version)
		}
	}
	if candidateCount != 1 {
		t.Fatal("V61 must be root-registered exactly once")
	}
	assertLedger := func(expected []int) {
		t.Helper()
		var got []int
		if err := db.Table("schema_migrations").Order("version").Pluck("version", &got).Error; err != nil || !reflect.DeepEqual(got, expected) {
			t.Fatal("unrelated ledger generation changed", err, got, expected)
		}
	}
	var exhaustionBefore []entity.QuotaNotificationObservation
	var recipientsBefore []entity.QuotaNotificationInbox
	if err := db.Order("id").Find(&exhaustionBefore).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Order("id").Find(&recipientsBefore).Error; err != nil {
		t.Fatal(err)
	}
	removeLedger := func() {
		t.Helper()
		result := db.Table("schema_migrations").Where("version = ?", candidateVersion).Delete(&struct{}{})
		if result.Error != nil || result.RowsAffected != 1 {
			t.Fatal("V61 must be registered once before reconstruction", result.Error)
		}
		assertLedger(withoutCandidate)
	}
	migrate := func() {
		t.Helper()
		if err := database.Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
		assertLedger(baseline)
	}
	concurrent := func() {
		t.Helper()
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
		assertLedger(baseline)
	}
	for _, model := range []any{inboxModel, observationModel} {
		if err := db.Migrator().DropTable(model); err != nil {
			t.Fatal(err)
		}
	}
	removeLedger()
	concurrent()
	migrate()
	constraints := []string{"ck_project_quota_warning_project", "ck_project_quota_warning_dimension", "ck_project_quota_warning_calendar", "ck_project_quota_warning_currency", "ck_project_quota_warning_level", "ck_project_quota_warning_generation"}
	assertSchema := func() {
		t.Helper()
		for _, model := range []any{observationModel, inboxModel} {
			if !db.Migrator().HasTable(model) {
				t.Fatal("warning table absent")
			}
			columns, err := db.Migrator().ColumnTypes(model)
			if err != nil {
				t.Fatal(err)
			}
			for _, column := range columns {
				if nullable, known := column.Nullable(); known && nullable && column.Name() != "read_at" {
					t.Fatal("warning immutable field nullable", column.Name())
				}
			}
		}
		if !db.Migrator().HasConstraint(inboxModel, "ck_project_quota_warning_recipient_birth") {
			t.Fatal("private recipient birth constraint missing")
		}
		for _, name := range constraints {
			if !db.Migrator().HasConstraint(observationModel, name) {
				t.Fatal("warning constraint absent", name)
			}
		}
		for _, index := range []struct {
			model any
			name  string
		}{{observationModel, "uq_project_quota_warning"}, {inboxModel, "uq_project_quota_warning_inbox"}, {inboxModel, "idx_project_quota_warning_recipient_created"}} {
			if !db.Migrator().HasIndex(index.model, index.name) {
				t.Fatal("warning dedup/paging index absent", index.name)
			}
		}
	}
	assertSchema()
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	asOf := start.Add(24 * time.Hour)
	original := entity.ProjectQuotaWarningObservation{ID: "pwo_retained_warning", ProjectID: "prj_retained_warning", Dimension: "tokens", MonthStart: start, MonthEnd: start.AddDate(0, 1, 0), PolicyRevision: "lim_retained_warning", Level: "near", Threshold: 80, ThresholdGeneration: "project-monthly-80-90-v1", TimeZone: "UTC", AsOf: asOf, Limit: "10", Settled: "8", CoverageStart: start, ResourceCreatedAt: start}
	if err := db.Create(&original).Error; err != nil {
		t.Fatal("warning history must not depend on a live owner FK", err)
	}
	readAt := asOf.Add(time.Hour)
	originalInbox := entity.ProjectQuotaWarningInbox{ID: "pwi_retained_warning", ObservationID: original.ID, RecipientID: "usr_retained_team_warning", RecipientCreatedAt: start, ReadAt: &readAt, CreatedAt: asOf}
	if err := db.Create(&originalInbox).Error; err != nil {
		t.Fatal("warning receipt must survive owner deletion", err)
	}
	if err := db.Take(&original, "id = ?", original.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Take(&originalInbox, "id = ?", originalInbox.ID).Error; err != nil {
		t.Fatal(err)
	}
	duplicate := original
	duplicate.ID = "pwo_duplicate_warning"
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("warning eight-field dedup missing")
	}
	duplicateInbox := originalInbox
	duplicateInbox.ID = "pwi_duplicate_warning"
	if err := db.Create(&duplicateInbox).Error; err == nil {
		t.Fatal("warning recipient dedup missing")
	}
	for _, name := range []string{"project", "dimension", "calendar", "currency", "level", "generation"} {
		t.Run("invalid_"+name, func(t *testing.T) {
			invalid := original
			invalid.ID = "pwo_invalid_" + name
			invalid.PolicyRevision = "lim_invalid_" + name
			switch name {
			case "project":
				invalid.ProjectID = ""
			case "dimension":
				invalid.Dimension = "requests"
			case "calendar":
				invalid.AsOf = invalid.MonthEnd
			case "currency":
				invalid.Currency = "USD"
			case "level":
				invalid.Threshold = 90
			case "generation":
				invalid.ThresholdGeneration = "arbitrary"
			}
			if err := db.Create(&invalid).Error; err == nil {
				t.Fatal("invalid frozen warning accepted")
			}
		})
	}
	for _, name := range []string{"project", "dimension", "revision", "month", "currency", "level", "birth"} {
		t.Run("distinct_"+name, func(t *testing.T) {
			changed := original
			changed.ID = "pwo_distinct_" + name
			switch name {
			case "project":
				changed.ProjectID = "prj_second_warning"
			case "dimension":
				changed.Dimension = "money"
				changed.Currency = "USD"
			case "revision":
				changed.PolicyRevision = "lim_next_warning"
			case "month":
				changed.MonthStart = changed.MonthEnd
				changed.MonthEnd = changed.MonthStart.AddDate(0, 1, 0)
				changed.AsOf = changed.MonthStart.Add(time.Hour)
			case "currency":
				changed.Dimension = "money"
				changed.Currency = "EUR"
			case "level":
				changed.Level = "critical"
				changed.Threshold = 90
				changed.Settled = "9"
			case "birth":
				changed.ResourceCreatedAt = start.Add(time.Hour)
			}
			if err := db.Create(&changed).Error; err != nil {
				t.Fatal("independent dedup identity collapsed", err)
			}
		})
	}
	if err := db.Model(observationModel).Where("id = ?", original.ID).UpdateColumn("policy_revision", nil).Error; err == nil {
		t.Fatal("required warning revision accepted NULL")
	}
	invalidBirth := originalInbox
	invalidBirth.ID = "pwi_future_birth"
	invalidBirth.RecipientID = "usr_future_birth"
	invalidBirth.RecipientCreatedAt = invalidBirth.CreatedAt.Add(time.Millisecond)
	if err := db.Create(&invalidBirth).Error; err == nil {
		t.Fatal("future recipient birth accepted")
	}
	if err := db.Model(inboxModel).Where("id = ?", originalInbox.ID).UpdateColumn("recipient_created_at", nil).Error; err == nil {
		t.Fatal("private recipient birth accepted NULL")
	}
	// Failed inbox DDL and check creation are portable partial-startup states.
	// Avoid pinned PostgreSQL DropIndex's invalid CURRENT_SCHEMA().index rendering.
	if err := db.Migrator().DropTable(inboxModel); err != nil {
		t.Fatal(err)
	}
	for _, name := range constraints {
		if err := db.Migrator().DropConstraint(observationModel, name); err != nil {
			t.Fatal(err)
		}
	}
	removeLedger()
	concurrent()
	assertSchema()
	if err := db.Create(&originalInbox).Error; err != nil {
		t.Fatal(err)
	}
	migrate()
	var retained entity.ProjectQuotaWarningObservation
	var retainedInbox entity.ProjectQuotaWarningInbox
	if err := db.Take(&retained, "id = ?", original.ID).Error; err != nil || !projectWarningObservationEqual(retained, original) {
		t.Fatal("migration rewrote retained warning", err)
	}
	if err := db.Take(&retainedInbox, "id = ?", originalInbox.ID).Error; err != nil || !projectWarningInboxEqual(retainedInbox, originalInbox) {
		t.Fatal("migration reset warning read state", err)
	}
	// Rebuild an empty interrupted frozen table with just its original PK; repair
	// must add all missing columns before dependent checks/indexes, on both drivers.
	for _, model := range []any{inboxModel, observationModel} {
		if err := db.Migrator().DropTable(model); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Migrator().CreateTable(&projectQuotaWarningPartialObservation{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().CreateTable(&projectQuotaWarningPartialInbox{}); err != nil {
		t.Fatal(err)
	}
	removeLedger()
	migrate()
	assertSchema()
	if err := db.Create(&original).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&originalInbox).Error; err != nil {
		t.Fatal(err)
	}
	concurrent()
	migrate()
	var exhaustionAfter []entity.QuotaNotificationObservation
	var recipientsAfter []entity.QuotaNotificationInbox
	if err := db.Order("id").Find(&exhaustionAfter).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Order("id").Find(&recipientsAfter).Error; err != nil {
		t.Fatal(err)
	}
	var personalWarningsAfter []entity.QuotaWarningObservation
	var personalInboxesAfter []entity.QuotaWarningInbox
	if err := db.Order("id").Find(&personalWarningsAfter).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Order("id").Find(&personalInboxesAfter).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(personalWarningsAfter, personalWarningsBefore) || !reflect.DeepEqual(personalInboxesAfter, personalInboxesBefore) {
		t.Fatal("V61 rewrote Personal warning history/read state")
	}
	var teamWarningsAfter []entity.TeamQuotaWarningObservation
	var teamInboxesAfter []entity.TeamQuotaWarningInbox
	if err := db.Order("id").Find(&teamWarningsAfter).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Order("id").Find(&teamInboxesAfter).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(teamWarningsBefore, teamWarningsAfter) || !reflect.DeepEqual(teamInboxesBefore, teamInboxesAfter) {
		t.Fatal("V61 rewrote Team warning history/read state")
	}
	assertLedger(baseline)
	if !reflect.DeepEqual(exhaustionAfter, exhaustionBefore) || !reflect.DeepEqual(recipientsAfter, recipientsBefore) {
		t.Fatal("V61 altered released exhaustion rows or read state")
	}
}

type projectQuotaWarningPartialObservation struct {
	ID string `gorm:"primaryKey;size:30"`
}

func (projectQuotaWarningPartialObservation) TableName() string {
	return "project_quota_warning_observations"
}

type projectQuotaWarningPartialInbox struct {
	ID string `gorm:"primaryKey;size:30"`
}

func (projectQuotaWarningPartialInbox) TableName() string { return "project_quota_warning_inboxes" }

// Timestamp representation is not an immutable fact. Compare instants, then all
// remaining scalar fields without dropping any persisted snapshot member.
func projectWarningObservationEqual(a, b entity.ProjectQuotaWarningObservation) bool {
	if !a.MonthStart.Equal(b.MonthStart) || !a.MonthEnd.Equal(b.MonthEnd) || !a.AsOf.Equal(b.AsOf) || !a.CoverageStart.Equal(b.CoverageStart) || !a.ResourceCreatedAt.Equal(b.ResourceCreatedAt) {
		return false
	}
	a.MonthStart, a.MonthEnd, a.AsOf, a.CoverageStart, a.ResourceCreatedAt = b.MonthStart, b.MonthEnd, b.AsOf, b.CoverageStart, b.ResourceCreatedAt
	return reflect.DeepEqual(a, b)
}
func projectWarningInboxEqual(a, b entity.ProjectQuotaWarningInbox) bool {
	if !a.CreatedAt.Equal(b.CreatedAt) || !a.RecipientCreatedAt.Equal(b.RecipientCreatedAt) || (a.ReadAt == nil) != (b.ReadAt == nil) {
		return false
	}
	if a.ReadAt != nil && !a.ReadAt.Equal(*b.ReadAt) {
		return false
	}
	a.CreatedAt, a.RecipientCreatedAt, a.ReadAt = b.CreatedAt, b.RecipientCreatedAt, b.ReadAt
	return reflect.DeepEqual(a, b)
}
func TestProjectWarningFixtureImmutableInstantComparison(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 123000000, time.UTC)
	other := now.In(time.FixedZone("representation", 3600))
	a := entity.ProjectQuotaWarningObservation{ID: "pwo_exact", ProjectID: "prj_exact", MonthStart: now, MonthEnd: now.AddDate(0, 1, 0), AsOf: now, CoverageStart: now, ResourceCreatedAt: now, Settled: "8"}
	b := a
	b.MonthStart = other
	b.MonthEnd = other.AddDate(0, 1, 0)
	b.AsOf = other
	b.CoverageStart = other
	b.ResourceCreatedAt = other
	if !projectWarningObservationEqual(a, b) {
		t.Fatal("equivalent persisted instants rejected")
	}
	for _, mutate := range []func(*entity.ProjectQuotaWarningObservation){func(x *entity.ProjectQuotaWarningObservation) { x.Settled = "9" }, func(x *entity.ProjectQuotaWarningObservation) {
		x.ResourceCreatedAt = x.ResourceCreatedAt.Add(time.Millisecond)
	}, func(x *entity.ProjectQuotaWarningObservation) { x.ProjectID = "prj_other" }} {
		changed := b
		mutate(&changed)
		if projectWarningObservationEqual(a, changed) {
			t.Fatal("changed immutable snapshot accepted")
		}
	}
	i := entity.ProjectQuotaWarningInbox{ID: "pwi_exact", ObservationID: a.ID, RecipientID: "usr_exact", CreatedAt: now, RecipientCreatedAt: now, ReadAt: &now}
	j := i
	j.CreatedAt = other
	j.RecipientCreatedAt = other
	j.ReadAt = &other
	if !projectWarningInboxEqual(i, j) {
		t.Fatal("equivalent receipt instants rejected")
	}
	j.ReadAt = nil
	if projectWarningInboxEqual(i, j) {
		t.Fatal("read state erased")
	}
}

func TestProjectWarningFixtureSchemaMappings(t *testing.T) {
	for _, model := range []any{&entity.ResourceLimit{}, &entity.QuotaSetting{}} {
		parsed, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{})
		if err != nil || parsed.LookUpField("ETag") == nil || parsed.LookUpField("ETag").DBName != "e_tag" {
			t.Fatal("fixture must address GORM ETag field, not literal etag", err)
		}
	}
}
