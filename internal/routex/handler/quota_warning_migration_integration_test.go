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
)

// Root must register the private V59 candidate after V58 before appending this
// scenario. It neither reaches an unexported migration nor bypasses the ledger.
func testPersonalMonthlyQuotaWarningMigration(t *testing.T, db *gorm.DB) {
	const candidateVersion = 59
	ctx := context.Background()
	observationModel, inboxModel := &entity.QuotaWarningObservation{}, &entity.QuotaWarningInbox{}
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
			t.Fatal("V59 must be registered once before reconstruction", result.Error)
		}
	}
	migrate := func() {
		t.Helper()
		if err := database.Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
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
	}
	for _, model := range []any{inboxModel, observationModel} {
		if err := db.Migrator().DropTable(model); err != nil {
			t.Fatal(err)
		}
	}
	removeLedger()
	concurrent()
	migrate()
	constraints := []string{"ck_quota_warning_owner", "ck_quota_warning_dimension", "ck_quota_warning_calendar", "ck_quota_warning_currency", "ck_quota_warning_level", "ck_quota_warning_generation"}
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
		for _, name := range constraints {
			if !db.Migrator().HasConstraint(observationModel, name) {
				t.Fatal("warning constraint absent", name)
			}
		}
		for _, index := range []struct {
			model any
			name  string
		}{{observationModel, "uq_quota_warning"}, {inboxModel, "uq_quota_warning_inbox"}, {inboxModel, "idx_quota_warning_recipient_created"}} {
			if !db.Migrator().HasIndex(index.model, index.name) {
				t.Fatal("warning dedup/paging index absent", index.name)
			}
		}
	}
	assertSchema()
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	asOf := start.Add(24 * time.Hour)
	original := entity.QuotaWarningObservation{ID: "qwo_retained_warning", OwnerID: "usr_retained_warning", Dimension: "tokens", MonthStart: start, MonthEnd: start.AddDate(0, 1, 0), PolicyRevision: "lim_retained_warning", Level: "near", Threshold: 80, ThresholdGeneration: "personal-monthly-80-90-v1", TimeZone: "UTC", AsOf: asOf, Limit: "10", Settled: "8", CoverageStart: start, ResourceCreatedAt: start}
	if err := db.Create(&original).Error; err != nil {
		t.Fatal("warning history must not depend on a live owner FK", err)
	}
	readAt := asOf.Add(time.Hour)
	originalInbox := entity.QuotaWarningInbox{ID: "qwi_retained_warning", ObservationID: original.ID, RecipientID: original.OwnerID, ReadAt: &readAt, CreatedAt: asOf}
	if err := db.Create(&originalInbox).Error; err != nil {
		t.Fatal("warning receipt must survive owner deletion", err)
	}
	duplicate := original
	duplicate.ID = "qwo_duplicate_warning"
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("warning eight-field dedup missing")
	}
	duplicateInbox := originalInbox
	duplicateInbox.ID = "qwi_duplicate_warning"
	if err := db.Create(&duplicateInbox).Error; err == nil {
		t.Fatal("warning recipient dedup missing")
	}
	for _, name := range []string{"owner", "dimension", "calendar", "currency", "level", "generation"} {
		t.Run("invalid_"+name, func(t *testing.T) {
			invalid := original
			invalid.ID = "qwo_invalid_" + name
			invalid.PolicyRevision = "lim_invalid_" + name
			switch name {
			case "owner":
				invalid.OwnerID = ""
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
	for _, name := range []string{"owner", "dimension", "revision", "month", "currency", "level", "birth"} {
		t.Run("distinct_"+name, func(t *testing.T) {
			changed := original
			changed.ID = "qwo_distinct_" + name
			switch name {
			case "owner":
				changed.OwnerID = "usr_second_warning"
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
	var retained entity.QuotaWarningObservation
	var retainedInbox entity.QuotaWarningInbox
	if err := db.Take(&retained, "id = ?", original.ID).Error; err != nil || !sameQuotaWarningMigrationObservation(retained, original) {
		t.Fatal("migration rewrote retained warning", err)
	}
	if err := db.Take(&retainedInbox, "id = ?", originalInbox.ID).Error; err != nil || !sameQuotaWarningMigrationInbox(retainedInbox, originalInbox) {
		t.Fatal("migration reset warning read state", err)
	}
	// Rebuild an empty interrupted frozen table with just its original PK; repair
	// must add all missing columns before dependent checks/indexes, on both drivers.
	for _, model := range []any{inboxModel, observationModel} {
		if err := db.Migrator().DropTable(model); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Migrator().CreateTable(&quotaWarningPartialObservation{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().CreateTable(&quotaWarningPartialInbox{}); err != nil {
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
	if !reflect.DeepEqual(exhaustionAfter, exhaustionBefore) || !reflect.DeepEqual(recipientsAfter, recipientsBefore) {
		t.Fatal("V59 altered released exhaustion rows or read state")
	}
}

type quotaWarningPartialObservation struct {
	ID string `gorm:"primaryKey;size:30"`
}

func (quotaWarningPartialObservation) TableName() string { return "quota_warning_observations" }

type quotaWarningPartialInbox struct {
	ID string `gorm:"primaryKey;size:30"`
}

func (quotaWarningPartialInbox) TableName() string { return "quota_warning_inboxes" }

// Compare every retained field, allowing only different time.Location or
// monotonic representations of the same persisted instant. No precision is lost.
func sameQuotaWarningMigrationObservation(a, b entity.QuotaWarningObservation) bool {
	at := []*time.Time{&a.MonthStart, &a.MonthEnd, &a.AsOf, &a.CoverageStart, &a.ResourceCreatedAt}
	bt := []*time.Time{&b.MonthStart, &b.MonthEnd, &b.AsOf, &b.CoverageStart, &b.ResourceCreatedAt}
	for i := range at {
		if !at[i].Equal(*bt[i]) {
			return false
		}
		*at[i], *bt[i] = time.Time{}, time.Time{}
	}
	return reflect.DeepEqual(a, b)
}

func sameQuotaWarningMigrationInbox(a, b entity.QuotaWarningInbox) bool {
	if !a.CreatedAt.Equal(b.CreatedAt) || (a.ReadAt == nil) != (b.ReadAt == nil) {
		return false
	}
	if a.ReadAt != nil && !a.ReadAt.Equal(*b.ReadAt) {
		return false
	}
	a.CreatedAt, b.CreatedAt = time.Time{}, time.Time{}
	a.ReadAt, b.ReadAt = nil, nil
	return reflect.DeepEqual(a, b)
}
