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

// These check-only fixtures freeze the released and replacement scope guards.
// They deliberately contain no evolving entity tags or foreign-key relations.
type teamQuotaScopeBeforeV47 struct {
	ScopeKind string `gorm:"size:20;not null;check:ck_quota_notification_scope,scope_kind IN ('user','project')"`
}

func (teamQuotaScopeBeforeV47) TableName() string { return "quota_notification_observations" }

type teamQuotaScopeAfterV47 struct {
	ScopeKind string `gorm:"size:20;not null;check:ck_quota_notification_scope_v47,scope_kind IN ('user','project','team')"`
}

func (teamQuotaScopeAfterV47) TableName() string { return "quota_notification_observations" }

func testTeamQuotaNotificationMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	before, after := &teamQuotaScopeBeforeV47{}, &teamQuotaScopeAfterV47{}
	assertSchema := func() {
		t.Helper()
		if !db.Migrator().HasConstraint(after, "ck_quota_notification_scope_v47") || db.Migrator().HasConstraint(before, "ck_quota_notification_scope") {
			t.Fatal("V47 did not replace the released scope check")
		}
		for _, check := range []string{"ck_quota_notification_dimension", "ck_quota_notification_calendar", "ck_quota_notification_currency"} {
			if !db.Migrator().HasConstraint(after, check) {
				t.Fatal("V47 removed an independent observation constraint", check)
			}
		}
		for _, index := range []struct {
			model any
			name  string
		}{
			{after, "uq_quota_notification_observation"},
			{&entity.QuotaNotificationInbox{}, "uq_quota_inbox_observation_recipient"},
			{&entity.QuotaNotificationInbox{}, "idx_quota_inbox_recipient_created"},
		} {
			if !db.Migrator().HasIndex(index.model, index.name) {
				t.Fatal("V47 lost a retained deduplication/paging index", index.name)
			}
		}
	}
	assertSchema() // The fresh harness has applied the complete empty-schema chain.
	month := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	for _, kind := range []string{"user", "project"} {
		observation := entity.QuotaNotificationObservation{ID: "qob_v47_" + kind, ScopeKind: kind, ScopeID: "old_v47_" + kind, ScopeName: "Retained historical " + kind, Dimension: "tokens", PolicyRevision: "rev_v47_" + kind, MonthStart: month, MonthEnd: month.AddDate(0, 1, 0), TimeZone: "UTC", AsOf: month.Add(time.Hour), Limit: "0", Settled: "0", CoverageStart: month, ResourceCreatedAt: month}
		if err := db.Create(&observation).Error; err != nil {
			t.Fatal("historical observation acquired a live identity/resource FK", err)
		}
		readAt := month.Add(2 * time.Hour)
		inbox := entity.QuotaNotificationInbox{ID: "qni_v47_" + kind, ObservationID: observation.ID, RecipientID: "usr_v47_deleted", CreatedAt: observation.AsOf, ReadAt: &readAt}
		if err := db.Create(&inbox).Error; err != nil {
			t.Fatal("historical recipient acquired a live identity FK", err)
		}
	}
	var baselineObservations []entity.QuotaNotificationObservation
	var baselineInboxes []entity.QuotaNotificationInbox
	if err := db.Order("id").Find(&baselineObservations).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Order("id").Find(&baselineInboxes).Error; err != nil {
		t.Fatal(err)
	}
	assertHistory := func() {
		t.Helper()
		var observations []entity.QuotaNotificationObservation
		var inboxes []entity.QuotaNotificationInbox
		if err := db.Order("id").Find(&observations).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Order("id").Find(&inboxes).Error; err != nil {
			t.Fatal(err)
		}
		// Compare persisted baselines, including database timestamp precision.
		if !reflect.DeepEqual(observations, baselineObservations) || !reflect.DeepEqual(inboxes, baselineInboxes) {
			t.Fatalf("V47 repair changed immutable history or read state: observations=%+v inboxes=%+v", observations, inboxes)
		}
	}
	removeLedger := func() {
		t.Helper()
		result := db.Table("schema_migrations").Where("version = ?", 47).Delete(&struct{}{})
		if result.Error != nil || result.RowsAffected != 1 {
			t.Fatal("cannot reconstruct V47 ledger", result.Error, result.RowsAffected)
		}
	}
	if err := db.Migrator().DropConstraint(after, "ck_quota_notification_scope_v47"); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().CreateConstraint(before, "ck_quota_notification_scope"); err != nil {
		t.Fatal(err)
	}
	removeLedger()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal("V46 existing-data upgrade", err)
	}
	assertSchema()
	assertHistory()
	// On MySQL, replacement-check DDL can commit before old-check removal/ledger.
	if err := db.Migrator().CreateConstraint(before, "ck_quota_notification_scope"); err != nil {
		t.Fatal(err)
	}
	removeLedger()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal("partial V47 check replacement", err)
	}
	assertSchema()
	assertHistory()
	// Missing replacement check and ledger must be repaired by concurrent starts.
	if err := db.Migrator().DropConstraint(after, "ck_quota_notification_scope_v47"); err != nil {
		t.Fatal(err)
	}
	removeLedger()
	var wait sync.WaitGroup
	failures := make(chan error, 2)
	for range 2 {
		wait.Go(func() { failures <- database.Migrate(ctx, db) })
	}
	wait.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal("concurrent V47 repair", err)
		}
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal("repeat V47 migration", err)
	}
	assertSchema()
	assertHistory()
	team := baselineObservations[0]
	team.ID, team.ScopeKind, team.ScopeID, team.PolicyRevision = "qob_v47_orphan_team", "team", "tea_v47_deleted", "rev_v47_team"
	if err := db.Create(&team).Error; err != nil {
		t.Fatal("Team history acquired a live Team FK or old scope check", err)
	}
	inbox := entity.QuotaNotificationInbox{ID: "qni_v47_orphan_team", ObservationID: team.ID, RecipientID: "usr_v47_deleted", CreatedAt: team.AsOf}
	if err := db.Create(&inbox).Error; err != nil {
		t.Fatal(err)
	}
	duplicate := team
	duplicate.ID = "qob_v47_duplicate"
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("V47 lost observation uniqueness")
	}
	duplicateInbox := inbox
	duplicateInbox.ID = "qni_v47_duplicate"
	if err := db.Create(&duplicateInbox).Error; err == nil {
		t.Fatal("V47 lost recipient uniqueness")
	}
	for _, change := range []string{"scope", "dimension", "currency", "calendar", "null"} {
		invalid := team
		invalid.ID, invalid.PolicyRevision = "qob_v47_bad_"+change, "rev_v47_bad_"+change
		switch change {
		case "scope":
			invalid.ScopeKind = "team_member"
		case "dimension":
			invalid.Dimension = "requests"
		case "currency":
			invalid.Currency = "USD"
		case "calendar":
			invalid.AsOf = invalid.MonthEnd
		}
		if change == "null" {
			if err := db.Model(&entity.QuotaNotificationObservation{}).Where("id = ?", team.ID).Update("scope_kind", nil).Error; err == nil {
				t.Fatal("V47 accepted NULL scope")
			}
		} else if err := db.Create(&invalid).Error; err == nil {
			t.Fatal("V47 accepted invalid observation", change)
		}
	}
}
