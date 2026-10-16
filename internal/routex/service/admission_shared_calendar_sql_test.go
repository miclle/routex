package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

func TestSharedEconomicCalendarRequiresRecordedFreeze(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*entity.QuotaSetting)
		want   error
	}{
		{"recorded_freeze", func(*entity.QuotaSetting) {}, nil},
		{"unstarted", func(c *entity.QuotaSetting) { c.AccountingStarted = false }, errSharedAdmissionConflict},
		{"wrong_id", func(c *entity.QuotaSetting) { c.ID = 2 }, errSharedAdmissionConflict},
		{"changed_revision", func(c *entity.QuotaSetting) { c.ETag = "qst_other" }, errSharedAdmissionConflict},
		{"changed_zone", func(c *entity.QuotaSetting) { c.TimeZone = "Asia/Shanghai" }, errSharedAdmissionConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, f, _, _ := sharedPolicySQLTestFixture(t, "personal_session")
			f.calendar.AccountingStarted = true
			test.change(&f.calendar)
			err := db.Transaction(func(tx *gorm.DB) error {
				return requireSharedEconomicCalendarSQL(tx, "qst_current", "UTC")
			}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
			if !errors.Is(err, test.want) {
				t.Fatalf("recorded calendar gate: got=%v want=%v", err, test.want)
			}
			if len(f.queries) != 1 || !strings.Contains(f.queries[0], `FROM "quota_settings"`) || !strings.Contains(f.queries[0], "FOR UPDATE") {
				t.Fatal("calendar read did not retain its exact SQL row lock", f.queries)
			}
		})
	}
}

func TestSharedEconomicCalendarRejectsInvalidUnboundedAndCancelledReads(t *testing.T) {
	db, f, _, _ := sharedPolicySQLTestFixture(t, "personal_session")
	if err := requireSharedEconomicCalendarSQL(db, "qst_current", "UTC"); !errors.Is(err, errSharedAdmissionInvalid) || len(f.queries) != 0 {
		t.Fatal("calendar gate accepted a nontransactional handle", err)
	}
	for _, test := range []struct{ revision, zone string }{
		{"", "UTC"}, {strings.Repeat("a", 129), "UTC"}, {"qst_current", ""},
		{"qst_current", "Local"}, {"qst_current", "Unknown/Zone"}, {"qst_current", strings.Repeat("a", 101)},
	} {
		err := db.Transaction(func(tx *gorm.DB) error {
			return requireSharedEconomicCalendarSQL(tx, test.revision, test.zone)
		}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if !errors.Is(err, errSharedAdmissionInvalid) || len(f.queries) != 0 {
			t.Fatal("invalid calendar performed a SQL read", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return requireSharedEconomicCalendarSQL(tx, "qst_current", "UTC")
	}); !errors.Is(err, context.Canceled) || len(f.queries) != 0 {
		t.Fatal("cancelled calendar read exposed or queried current facts", err)
	}
}
