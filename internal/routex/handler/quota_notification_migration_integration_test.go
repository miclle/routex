package handler

import (
	"context"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
)

func testQuotaNotificationMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	observationModel := &entity.QuotaNotificationObservation{}
	inboxModel := &entity.QuotaNotificationInbox{}
	for _, model := range []any{inboxModel, observationModel} {
		if err := db.Migrator().DropTable(model); err != nil {
			t.Fatal(err)
		}
	}
	removeLedger := func() {
		t.Helper()
		result := db.Table("schema_migrations").Where("version = ?", 35).Delete(&struct{}{})
		if result.Error != nil || result.RowsAffected != 1 {
			t.Fatal("cannot reconstruct V35 ledger", result.Error)
		}
	}
	removeLedger()
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
	assertSchema := func() {
		t.Helper()
		for _, model := range []any{observationModel, inboxModel} {
			if !db.Migrator().HasTable(model) {
				t.Fatal("quota notification table absent")
			}
			columns, err := db.Migrator().ColumnTypes(model)
			if err != nil {
				t.Fatal(err)
			}
			for _, column := range columns {
				if nullable, ok := column.Nullable(); ok && nullable && column.Name() != "read_at" {
					t.Fatal("immutable quota field nullable", column.Name())
				}
			}
		}
		for _, name := range []string{"ck_quota_notification_scope", "ck_quota_notification_dimension", "ck_quota_notification_calendar", "ck_quota_notification_currency"} {
			if !db.Migrator().HasConstraint(observationModel, name) {
				t.Fatal("quota notification check absent", name)
			}
		}
		if !db.Migrator().HasIndex(observationModel, "uq_quota_notification_observation") || !db.Migrator().HasIndex(inboxModel, "uq_quota_inbox_observation_recipient") || !db.Migrator().HasIndex(inboxModel, "idx_quota_inbox_recipient_created") {
			t.Fatal("quota dedupe or recipient paging index absent")
		}
	}
	assertSchema()
	month := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	now := month.Add(24 * time.Hour)
	observation := entity.QuotaNotificationObservation{ID: "qob_historical_notice", ScopeKind: "user", ScopeID: "usr_historical_notice", Dimension: "tokens", PolicyRevision: "lim_historical_notice", MonthStart: month, MonthEnd: month.AddDate(0, 1, 0), TimeZone: "UTC", AsOf: now, Limit: "0", Settled: "0", CoverageStart: month, ResourceCreatedAt: month}
	if err := db.Create(&observation).Error; err != nil {
		t.Fatal("historical observations need no live resource FK", err)
	}
	inbox := entity.QuotaNotificationInbox{ID: "qni_historical_notice", ObservationID: observation.ID, RecipientID: "usr_historical_recipient", CreatedAt: now}
	if err := db.Create(&inbox).Error; err != nil {
		t.Fatal("historical inbox must survive identity deletion", err)
	}
	duplicate := observation
	duplicate.ID = "qob_duplicate_notice"
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("source observation dedup missing")
	}
	duplicateInbox := inbox
	duplicateInbox.ID = "qni_duplicate_notice"
	if err := db.Create(&duplicateInbox).Error; err == nil {
		t.Fatal("recipient observation dedup missing")
	}
	for _, field := range []string{"scope", "dimension", "currency", "calendar"} {
		invalid := observation
		invalid.ID = "qob_invalid_" + field
		switch field {
		case "scope":
			invalid.ScopeKind = "team"
		case "dimension":
			invalid.Dimension = "requests"
		case "currency":
			invalid.Currency = "USD"
		case "calendar":
			invalid.AsOf = invalid.MonthEnd
		}
		if err := db.Create(&invalid).Error; err == nil {
			t.Fatal("invalid frozen observation accepted", field)
		}
	}
	for _, change := range []string{"revision", "month", "currency", "scope"} {
		changed := observation
		changed.ID = "qob_next_" + change
		switch change {
		case "revision":
			changed.PolicyRevision = "lim_next_notice"
		case "month":
			changed.MonthStart = changed.MonthEnd
			changed.MonthEnd = changed.MonthStart.AddDate(0, 1, 0)
			changed.AsOf = changed.MonthStart.Add(time.Hour)
		case "currency":
			changed.Dimension = "money"
			changed.Currency = "USD"
		case "scope":
			changed.ScopeKind = "project"
		}
		if err := db.Create(&changed).Error; err != nil {
			t.Fatal("new current revision/month/dimension/scope incorrectly deduped", err)
		}
	}
	if err := db.Model(observationModel).Where("id = ?", observation.ID).Update("policy_revision", nil).Error; err == nil {
		t.Fatal("required immutable policy accepted NULL")
	}
	readAt := now.Add(time.Hour)
	if err := db.Model(inboxModel).Where("id = ?", inbox.ID).Update("read_at", readAt).Error; err != nil {
		t.Fatal(err)
	}
	// A complete observation table followed by failed inbox DDL/ledger is a valid
	// interrupted MySQL startup. Re-entry preserves facts and creates the inbox.
	if err := db.Migrator().DropTable(inboxModel); err != nil {
		t.Fatal(err)
	}
	removeLedger()
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	assertSchema()
	if err := db.Create(&inbox).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(inboxModel).Where("id = ?", inbox.ID).Update("read_at", readAt).Error; err != nil {
		t.Fatal(err)
	}
	for _, constraint := range []string{"ck_quota_notification_scope", "ck_quota_notification_dimension", "ck_quota_notification_calendar", "ck_quota_notification_currency"} {
		if err := db.Migrator().DropConstraint(observationModel, constraint); err != nil {
			t.Fatal(err)
		}
	}
	for _, index := range []struct {
		model any
		name  string
	}{{observationModel, "uq_quota_notification_observation"}, {inboxModel, "uq_quota_inbox_observation_recipient"}, {inboxModel, "idx_quota_inbox_recipient_created"}} {
		dropQuotaNotificationIndex(t, db, index.model, index.name)
	}
	removeLedger()
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	assertSchema()
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	var retained entity.QuotaNotificationObservation
	if err := db.First(&retained, "id = ?", observation.ID).Error; err != nil || retained.Limit != "0" || retained.PolicyRevision != observation.PolicyRevision || !retained.AsOf.Equal(now) {
		t.Fatal("V35 repair changed historical observation", err)
	}
	var retainedInbox entity.QuotaNotificationInbox
	if err := db.First(&retainedInbox, "id = ?", inbox.ID).Error; err != nil || retainedInbox.ReadAt == nil || !retainedInbox.ReadAt.Equal(readAt) {
		t.Fatal("V35 repair reset read state", err)
	}
}

func dropQuotaNotificationIndex(t *testing.T, db *gorm.DB, model any, name string) {
	t.Helper()
	if db.Name() == "postgres" {
		// Pinned GORM DropIndex renders invalid CURRENT_SCHEMA().index SQL. This
		// test-only DDL uses fixed allowlisted identifiers, with no user interpolation.
		statements := map[string]string{"uq_quota_notification_observation": `DROP INDEX "uq_quota_notification_observation"`, "uq_quota_inbox_observation_recipient": `DROP INDEX "uq_quota_inbox_observation_recipient"`, "idx_quota_inbox_recipient_created": `DROP INDEX "idx_quota_inbox_recipient_created"`}
		statement, ok := statements[name]
		if !ok {
			t.Fatal("unknown quota fixture index")
		}
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	} else if err := db.Migrator().DropIndex(model, name); err != nil {
		t.Fatal(err)
	}
}
