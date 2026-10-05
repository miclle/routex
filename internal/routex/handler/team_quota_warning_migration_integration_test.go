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

// Root must register the private V60 candidate after V59 before appending this
// scenario. It neither reaches an unexported migration nor bypasses the ledger.
func testTeamMonthlyQuotaWarningMigration(t *testing.T, db *gorm.DB) {
	const candidateVersion = 60
	ctx := context.Background()
	observationModel, inboxModel := &entity.TeamQuotaWarningObservation{}, &entity.TeamQuotaWarningInbox{}
	// Seed nonempty predecessor history with a saved read timestamp, so upgrade
	// preservation is meaningful even when the harness resets each scenario.
	historyStart := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	historyAsOf := historyStart.Add(time.Hour)
	historyRead := historyAsOf.Add(time.Hour)
	personalHistory := entity.QuotaWarningObservation{ID: "qwo_v60_personal_history", OwnerID: "usr_v60_personal_history", Dimension: "tokens", MonthStart: historyStart, MonthEnd: historyStart.AddDate(0, 1, 0), PolicyRevision: "lim_v60_personal_history", Level: "near", Threshold: 80, ThresholdGeneration: "personal-monthly-80-90-v1", TimeZone: "UTC", AsOf: historyAsOf, Limit: "10", Settled: "8", CoverageStart: historyStart, ResourceCreatedAt: historyStart}
	personalReceipt := entity.QuotaWarningInbox{ID: "qwi_v60_personal_history", ObservationID: personalHistory.ID, RecipientID: personalHistory.OwnerID, ReadAt: &historyRead, CreatedAt: historyAsOf}
	exhaustionHistory := entity.QuotaNotificationObservation{ID: "qob_v60_exhaust_history", ScopeKind: "team", ScopeID: "tem_v60_exhaust_history", ScopeName: "Retained Team", Dimension: "tokens", PolicyRevision: "lim_v60_exhaust_history", MonthStart: historyStart, MonthEnd: historyStart.AddDate(0, 1, 0), TimeZone: "UTC", AsOf: historyAsOf, Limit: "10", Settled: "10", CoverageStart: historyStart, ResourceCreatedAt: historyStart}
	exhaustionReceipt := entity.QuotaNotificationInbox{ID: "qni_v60_exhaust_history", ObservationID: exhaustionHistory.ID, RecipientID: "usr_v60_exhaust_history", ReadAt: &historyRead, CreatedAt: historyAsOf}
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
		t.Fatal("V60 must be root-registered exactly once")
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
			t.Fatal("V60 must be registered once before reconstruction", result.Error)
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
	constraints := []string{"ck_team_quota_warning_team", "ck_team_quota_warning_dimension", "ck_team_quota_warning_calendar", "ck_team_quota_warning_currency", "ck_team_quota_warning_level", "ck_team_quota_warning_generation"}
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
		if !db.Migrator().HasConstraint(inboxModel, "ck_team_quota_warning_recipient_birth") {
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
		}{{observationModel, "uq_team_quota_warning"}, {inboxModel, "uq_team_quota_warning_inbox"}, {inboxModel, "idx_team_quota_warning_recipient_created"}} {
			if !db.Migrator().HasIndex(index.model, index.name) {
				t.Fatal("warning dedup/paging index absent", index.name)
			}
		}
	}
	assertSchema()
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	asOf := start.Add(24 * time.Hour)
	original := entity.TeamQuotaWarningObservation{ID: "two_retained_warning", TeamID: "tem_retained_warning", Dimension: "tokens", MonthStart: start, MonthEnd: start.AddDate(0, 1, 0), PolicyRevision: "lim_retained_warning", Level: "near", Threshold: 80, ThresholdGeneration: "team-monthly-80-90-v1", TimeZone: "UTC", AsOf: asOf, Limit: "10", Settled: "8", CoverageStart: start, ResourceCreatedAt: start}
	if err := db.Create(&original).Error; err != nil {
		t.Fatal("warning history must not depend on a live owner FK", err)
	}
	readAt := asOf.Add(time.Hour)
	originalInbox := entity.TeamQuotaWarningInbox{ID: "twi_retained_warning", ObservationID: original.ID, RecipientID: "usr_retained_team_warning", RecipientCreatedAt: start, ReadAt: &readAt, CreatedAt: asOf}
	if err := db.Create(&originalInbox).Error; err != nil {
		t.Fatal("warning receipt must survive owner deletion", err)
	}
	duplicate := original
	duplicate.ID = "two_duplicate_warning"
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("warning eight-field dedup missing")
	}
	duplicateInbox := originalInbox
	duplicateInbox.ID = "twi_duplicate_warning"
	if err := db.Create(&duplicateInbox).Error; err == nil {
		t.Fatal("warning recipient dedup missing")
	}
	for _, name := range []string{"team", "dimension", "calendar", "currency", "level", "generation"} {
		t.Run("invalid_"+name, func(t *testing.T) {
			invalid := original
			invalid.ID = "two_invalid_" + name
			invalid.PolicyRevision = "lim_invalid_" + name
			switch name {
			case "team":
				invalid.TeamID = ""
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
	for _, name := range []string{"team", "dimension", "revision", "month", "currency", "level", "birth"} {
		t.Run("distinct_"+name, func(t *testing.T) {
			changed := original
			changed.ID = "two_distinct_" + name
			switch name {
			case "team":
				changed.TeamID = "tem_second_warning"
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
	invalidBirth.ID = "twi_future_birth"
	invalidBirth.RecipientID = "usr_future_birth"
	invalidBirth.RecipientCreatedAt = invalidBirth.CreatedAt.Add(time.Microsecond)
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
	var retained entity.TeamQuotaWarningObservation
	var retainedInbox entity.TeamQuotaWarningInbox
	if err := db.Take(&retained, "id = ?", original.ID).Error; err != nil || !sameTeamWarningMigrationObservation(retained, original) {
		t.Fatal("migration rewrote retained warning", err)
	}
	if err := db.Take(&retainedInbox, "id = ?", originalInbox.ID).Error; err != nil || !sameTeamWarningMigrationInbox(retainedInbox, originalInbox) {
		t.Fatal("migration reset warning read state", err)
	}
	// Rebuild an empty interrupted frozen table with just its original PK; repair
	// must add all missing columns before dependent checks/indexes, on both drivers.
	for _, model := range []any{inboxModel, observationModel} {
		if err := db.Migrator().DropTable(model); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Migrator().CreateTable(&teamQuotaWarningPartialObservation{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().CreateTable(&teamQuotaWarningPartialInbox{}); err != nil {
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
		t.Fatal("V60 rewrote Personal warning history/read state")
	}
	assertLedger(baseline)
	if !reflect.DeepEqual(exhaustionAfter, exhaustionBefore) || !reflect.DeepEqual(recipientsAfter, recipientsBefore) {
		t.Fatal("V60 altered released exhaustion rows or read state")
	}
}

type teamQuotaWarningPartialObservation struct {
	ID string `gorm:"primaryKey;size:30"`
}

func (teamQuotaWarningPartialObservation) TableName() string {
	return "team_quota_warning_observations"
}

type teamQuotaWarningPartialInbox struct {
	ID string `gorm:"primaryKey;size:30"`
}

func (teamQuotaWarningPartialInbox) TableName() string { return "team_quota_warning_inboxes" }

// Every retained field is compared. Only time.Location/monotonic representation
// may differ for the same exact persisted instant; no truncation or tolerance.
func sameTeamWarningMigrationObservation(a, b entity.TeamQuotaWarningObservation) bool {
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
func sameTeamWarningMigrationInbox(a, b entity.TeamQuotaWarningInbox) bool {
	if !a.CreatedAt.Equal(b.CreatedAt) || !a.RecipientCreatedAt.Equal(b.RecipientCreatedAt) || (a.ReadAt == nil) != (b.ReadAt == nil) {
		return false
	}
	if a.ReadAt != nil && !a.ReadAt.Equal(*b.ReadAt) {
		return false
	}
	a.CreatedAt, b.CreatedAt = time.Time{}, time.Time{}
	a.RecipientCreatedAt, b.RecipientCreatedAt = time.Time{}, time.Time{}
	a.ReadAt, b.ReadAt = nil, nil
	return reflect.DeepEqual(a, b)
}
func TestTeamWarningMigrationRetainedEquality(t *testing.T) {
	instant := time.Date(2026, 3, 1, 0, 0, 0, 123456000, time.UTC)
	otherLocation := instant.In(time.FixedZone("retained SQL offset", 3600))
	a := entity.TeamQuotaWarningObservation{ID: "two_retained", TeamID: "tem_retained", TeamName: "Recorded Team", Dimension: "tokens", MonthStart: instant, MonthEnd: instant.Add(time.Hour), PolicyRevision: "lim_retained", Level: "near", Threshold: 80, ThresholdGeneration: "team-monthly-80-90-v1", TimeZone: "UTC", AsOf: instant, Limit: "10", Settled: "8", CoverageStart: instant, ResourceCreatedAt: instant}
	b := a
	b.MonthStart = otherLocation
	b.MonthEnd = b.MonthEnd.In(otherLocation.Location())
	b.AsOf = otherLocation
	b.CoverageStart = otherLocation
	b.ResourceCreatedAt = otherLocation
	if !sameTeamWarningMigrationObservation(a, b) {
		t.Fatal("exact instant with different location rejected")
	}
	for _, field := range []string{"MonthStart", "MonthEnd", "AsOf", "CoverageStart", "ResourceCreatedAt"} {
		t.Run(field, func(t *testing.T) {
			changed := b
			v := reflect.ValueOf(&changed).Elem().FieldByName(field)
			v.Set(reflect.ValueOf(v.Interface().(time.Time).Add(time.Nanosecond)))
			if sameTeamWarningMigrationObservation(a, changed) {
				t.Fatal("changed exact instant accepted")
			}
		})
	}
	changed := b
	changed.TeamName = "Different"
	if sameTeamWarningMigrationObservation(a, changed) {
		t.Fatal("changed scalar accepted")
	}
	read := instant
	otherRead := otherLocation
	inbox := entity.TeamQuotaWarningInbox{ID: "twi_retained", ObservationID: a.ID, RecipientID: "usr_retained", RecipientCreatedAt: instant, ReadAt: &read, CreatedAt: instant}
	loaded := inbox
	loaded.RecipientCreatedAt = otherLocation
	loaded.CreatedAt = otherLocation
	loaded.ReadAt = &otherRead
	if !sameTeamWarningMigrationInbox(inbox, loaded) {
		t.Fatal("exact inbox instants rejected")
	}
	for _, field := range []string{"CreatedAt", "RecipientCreatedAt", "ReadAt", "RecipientID"} {
		t.Run(field, func(t *testing.T) {
			changed := loaded
			switch field {
			case "CreatedAt":
				changed.CreatedAt = changed.CreatedAt.Add(time.Nanosecond)
			case "RecipientCreatedAt":
				changed.RecipientCreatedAt = changed.RecipientCreatedAt.Add(time.Nanosecond)
			case "ReadAt":
				v := changed.ReadAt.Add(time.Nanosecond)
				changed.ReadAt = &v
			case "RecipientID":
				changed.RecipientID = "usr_other"
			}
			if sameTeamWarningMigrationInbox(inbox, changed) {
				t.Fatal("changed inbox field accepted")
			}
		})
	}
	loaded.ReadAt = nil
	if sameTeamWarningMigrationInbox(inbox, loaded) {
		t.Fatal("read state loss accepted")
	}
	inbox.ReadAt = nil
	if !sameTeamWarningMigrationInbox(inbox, loaded) {
		t.Fatal("equal unread state rejected")
	}
}
