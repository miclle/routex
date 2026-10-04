package handler

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

// These private fixtures retain only the exact released and new column guards.
// No business entity is used to reconstruct a historical migration shape.
type teamMemberNoticeBeforeV51 struct {
	ScopeKind string `gorm:"size:20;not null;check:ck_quota_notification_scope_v47,scope_kind IN ('user','project','team')"`
	ScopeID   string `gorm:"size:30;not null"`
}

func (teamMemberNoticeBeforeV51) TableName() string { return "quota_notification_observations" }

type teamMemberNoticeAfterV51 struct {
	ScopeKind    string  `gorm:"check:ck_quota_notification_scope_v51,(CHAR_LENGTH(scope_kind) = 4 AND ASCII(SUBSTRING(scope_kind,1,1)) = 117 AND ASCII(SUBSTRING(scope_kind,2,1)) = 115 AND ASCII(SUBSTRING(scope_kind,3,1)) = 101 AND ASCII(SUBSTRING(scope_kind,4,1)) = 114) OR (CHAR_LENGTH(scope_kind) = 7 AND ASCII(SUBSTRING(scope_kind,1,1)) = 112 AND ASCII(SUBSTRING(scope_kind,2,1)) = 114 AND ASCII(SUBSTRING(scope_kind,3,1)) = 111 AND ASCII(SUBSTRING(scope_kind,4,1)) = 106 AND ASCII(SUBSTRING(scope_kind,5,1)) = 101 AND ASCII(SUBSTRING(scope_kind,6,1)) = 99 AND ASCII(SUBSTRING(scope_kind,7,1)) = 116) OR (CHAR_LENGTH(scope_kind) = 4 AND ASCII(SUBSTRING(scope_kind,1,1)) = 116 AND ASCII(SUBSTRING(scope_kind,2,1)) = 101 AND ASCII(SUBSTRING(scope_kind,3,1)) = 97 AND ASCII(SUBSTRING(scope_kind,4,1)) = 109) OR (CHAR_LENGTH(scope_kind) = 11 AND ASCII(SUBSTRING(scope_kind,1,1)) = 116 AND ASCII(SUBSTRING(scope_kind,2,1)) = 101 AND ASCII(SUBSTRING(scope_kind,3,1)) = 97 AND ASCII(SUBSTRING(scope_kind,4,1)) = 109 AND ASCII(SUBSTRING(scope_kind,5,1)) = 95 AND ASCII(SUBSTRING(scope_kind,6,1)) = 109 AND ASCII(SUBSTRING(scope_kind,7,1)) = 101 AND ASCII(SUBSTRING(scope_kind,8,1)) = 109 AND ASCII(SUBSTRING(scope_kind,9,1)) = 98 AND ASCII(SUBSTRING(scope_kind,10,1)) = 101 AND ASCII(SUBSTRING(scope_kind,11,1)) = 114)"`
	ScopeID      string  `gorm:"size:64;not null"`
	MemberUserID *string `gorm:"size:30"`
	TeamID       *string `gorm:"size:30;check:ck_quota_notification_member_v51,(scope_kind = 'team_member' AND CHAR_LENGTH(scope_id) = 52 AND team_id IS NOT NULL AND CHAR_LENGTH(team_id) > 0 AND member_user_id IS NOT NULL AND CHAR_LENGTH(member_user_id) > 0) OR (scope_kind IN ('user','project','team') AND CHAR_LENGTH(scope_id) BETWEEN 1 AND 30 AND team_id IS NULL AND member_user_id IS NULL)"`
}

func (teamMemberNoticeAfterV51) TableName() string { return "quota_notification_observations" }

func testTeamMemberQuotaNotificationMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	before, after := &teamMemberNoticeBeforeV51{}, &teamMemberNoticeAfterV51{}
	assertSchema := func() {
		t.Helper()
		for _, check := range []string{"ck_quota_notification_scope_v51", "ck_quota_notification_member_v51", "ck_quota_notification_dimension", "ck_quota_notification_calendar", "ck_quota_notification_currency"} {
			if !db.Migrator().HasConstraint(after, check) {
				t.Fatal("V51 missing guard", check)
			}
		}
		for _, check := range []string{"ck_quota_notification_scope_v47", "ck_quota_notification_scope"} {
			if db.Migrator().HasConstraint(after, check) {
				t.Fatal("old scope guard retained", check)
			}
		}
		columns, err := db.Migrator().ColumnTypes(after)
		if err != nil {
			t.Fatal(err)
		}
		found := map[string]bool{}
		for _, column := range columns {
			switch column.Name() {
			case "scope_id":
				length, known := column.Length()
				if !known || length != 64 {
					t.Fatal("digest column was not widened", length, known)
				}
				found[column.Name()] = true
			case "team_id", "member_user_id":
				nullable, known := column.Nullable()
				length, lengthKnown := column.Length()
				if !known || !nullable || !lengthKnown || length != 30 {
					t.Fatal("proof columns changed", column.Name())
				}
				found[column.Name()] = true
			}
		}
		if len(found) != 3 {
			t.Fatal("new columns missing", found)
		}
		for _, index := range []struct {
			model any
			name  string
		}{{after, "uq_quota_notification_observation"}, {&entity.QuotaNotificationInbox{}, "uq_quota_inbox_observation_recipient"}, {&entity.QuotaNotificationInbox{}, "idx_quota_inbox_recipient_created"}} {
			if !db.Migrator().HasIndex(index.model, index.name) {
				t.Fatal("retained index missing", index.name)
			}
		}
	}
	assertSchema() // Root's empty-database chain must include the reserved V51.
	month := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	for _, kind := range []string{"user", "project", "team"} {
		row := entity.QuotaNotificationObservation{ID: "qob_v51_" + kind, ScopeKind: kind, ScopeID: "old_v51_" + kind, ScopeName: "Retained aggregate", Dimension: "tokens", PolicyRevision: "rev_v51_" + kind, MonthStart: month, MonthEnd: month.AddDate(0, 1, 0), TimeZone: "UTC", AsOf: month.Add(time.Hour), Limit: "0", Settled: "0", CoverageStart: month, ResourceCreatedAt: month}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal("aggregate acquired live resource FK", err)
		}
		readAt := month.Add(2 * time.Hour)
		if err := db.Create(&entity.QuotaNotificationInbox{ID: "qni_v51_" + kind, ObservationID: row.ID, RecipientID: "usr_v51_deleted", CreatedAt: row.AsOf, ReadAt: &readAt}).Error; err != nil {
			t.Fatal("historical inbox acquired live User FK", err)
		}
	}
	var observations []entity.QuotaNotificationObservation
	var inboxes []entity.QuotaNotificationInbox
	if err := db.Order("id").Find(&observations).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Order("id").Find(&inboxes).Error; err != nil {
		t.Fatal(err)
	}
	history := func() {
		t.Helper()
		var rows []entity.QuotaNotificationObservation
		var recipients []entity.QuotaNotificationInbox
		if err := db.Order("id").Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Order("id").Find(&recipients).Error; err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(rows, observations) || !reflect.DeepEqual(recipients, inboxes) {
			t.Fatal("V51 rewrote aggregate history/read receipts")
		}
	}
	clearLedger := func() {
		t.Helper()
		result := db.Table("schema_migrations").Where("version = ?", 51).Delete(&struct{}{})
		if result.Error != nil || result.RowsAffected != 1 {
			t.Fatal("reserved V51 ledger missing", result.Error, result.RowsAffected)
		}
	}
	upgrade := func() {
		t.Helper()
		clearLedger()
		if err := database.Migrate(ctx, db); err != nil {
			t.Fatal("V51 repair", err)
		}
		assertSchema()
		history()
	}
	// Reconstruct the released V47 shape with retained historical data. Every
	// subsequent prefix is a possible committed MySQL DDL interruption.
	for _, check := range []string{"ck_quota_notification_member_v51", "ck_quota_notification_scope_v51"} {
		if err := db.Migrator().DropConstraint(after, check); err != nil {
			t.Fatal(err)
		}
	}
	for _, field := range []string{"TeamID", "MemberUserID"} {
		if err := db.Migrator().DropColumn(after, field); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Migrator().AlterColumn(before, "ScopeID"); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().CreateConstraint(before, "ck_quota_notification_scope_v47"); err != nil {
		t.Fatal(err)
	}
	upgrade()
	for _, prefix := range []string{"member_column", "both_columns", "widened", "new_guards"} {
		for _, check := range []string{"ck_quota_notification_member_v51", "ck_quota_notification_scope_v51"} {
			if err := db.Migrator().DropConstraint(after, check); err != nil {
				t.Fatal(err)
			}
		}
		if err := db.Migrator().CreateConstraint(before, "ck_quota_notification_scope_v47"); err != nil {
			t.Fatal(err)
		}
		if prefix == "member_column" {
			if err := db.Migrator().DropColumn(after, "TeamID"); err != nil {
				t.Fatal(err)
			}
		}
		if prefix == "member_column" || prefix == "both_columns" {
			if err := db.Migrator().AlterColumn(before, "ScopeID"); err != nil {
				t.Fatal(err)
			}
		}
		if prefix == "new_guards" {
			for _, check := range []string{"ck_quota_notification_member_v51", "ck_quota_notification_scope_v51"} {
				if err := db.Migrator().CreateConstraint(after, check); err != nil {
					t.Fatal(err)
				}
			}
		}
		upgrade()
	}
	clearLedger()
	var wait sync.WaitGroup
	failures := make(chan error, 2)
	for range 2 {
		wait.Go(func() { failures <- database.Migrate(ctx, db) })
	}
	wait.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal("concurrent V51 startup", err)
		}
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal("repeat V51", err)
	}
	assertSchema()
	history()
	member := observations[0]
	team, user := "tea_v51_deleted", "usr_v51_deleted"
	member.ID, member.ScopeKind, member.ScopeID, member.PolicyRevision = "qob_v51_member", "team_member", strings.Repeat("a", 52), "rev_v51_member"
	member.TeamID, member.MemberUserID = &team, &user
	if err := db.Create(&member).Error; err != nil {
		t.Fatal("historical pair acquired live FK", err)
	}
	inbox := entity.QuotaNotificationInbox{ID: "qni_v51_member", ObservationID: member.ID, RecipientID: user, CreatedAt: member.AsOf}
	if err := db.Create(&inbox).Error; err != nil {
		t.Fatal(err)
	}
	duplicate := member
	duplicate.ID = "qob_v51_duplicate"
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("member observation uniqueness lost")
	}
	duplicateInbox := inbox
	duplicateInbox.ID = "qni_v51_duplicate"
	if err := db.Create(&duplicateInbox).Error; err == nil {
		t.Fatal("self inbox uniqueness lost")
	}
	for index, change := range []string{"upper_kind", "space_kind", "short_digest", "long_digest", "nil_team", "nil_user", "empty_team", "empty_user", "aggregate_proof", "long_aggregate", "dimension", "calendar", "currency"} {
		row := member
		// IDs remain inside the historical30-byte column independently of the test label.
		row.ID = "qob_v51_bad_" + string(rune('a'+index))
		row.PolicyRevision = "bad_v51_" + string(rune('a'+index))
		empty := ""
		switch change {
		case "upper_kind":
			row.ScopeKind = "TEAM_MEMBER"
		case "space_kind":
			row.ScopeKind = "team_member "
		case "short_digest":
			row.ScopeID = strings.Repeat("a", 51)
		case "long_digest":
			row.ScopeID = strings.Repeat("a", 53)
		case "nil_team":
			row.TeamID = nil
		case "nil_user":
			row.MemberUserID = nil
		case "empty_team":
			row.TeamID = &empty
		case "empty_user":
			row.MemberUserID = &empty
		case "aggregate_proof":
			row.ScopeKind = "team"
			row.ScopeID = team
		case "long_aggregate":
			row.ScopeKind = "team"
			row.ScopeID = strings.Repeat("a", 31)
			row.TeamID = nil
			row.MemberUserID = nil
		case "dimension":
			row.Dimension = "requests"
		case "calendar":
			row.AsOf = row.MonthEnd
		case "currency":
			row.Currency = "USD"
		}
		if err := db.Create(&row).Error; err == nil {
			t.Fatal("V51 accepted invalid historical scope/proof", change)
		}
	}
}
