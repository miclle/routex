package database

import (
	"strings"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestExactAuthorityPredicatesKeepValuesBound(t *testing.T) {
	for _, fixture := range []struct {
		name      string
		dialector gorm.Dialector
	}{
		{"postgres", postgres.New(postgres.Config{DSN: "host=localhost user=test dbname=test sslmode=disable"})},
		{"mysql", mysql.New(mysql.Config{DSN: "test:test@tcp(localhost:3306)/test", SkipInitializeWithVersion: true})},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			db, err := gorm.Open(fixture.dialector, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
			if err != nil {
				t.Fatal(err)
			}
			// Quotes and SQL-looking user input must remain one bound value.
			actor := "usr_' OR recipient_id <> '"
			query := db.Table("quota_notification_inboxes AS inbox").
				Where(ExactText(db, clause.Column{Table: "inbox", Name: "recipient_id"}, actor)).
				Where(ExactTextColumns(db, clause.Column{Table: "inbox", Name: "observation_id"}, clause.Column{Table: "observation", Name: "id"})).
				Find(&[]map[string]any{})
			if query.Error != nil {
				t.Fatal(query.Error)
			}
			if len(query.Statement.Vars) != 1 || query.Statement.Vars[0] != actor || strings.Contains(query.Statement.SQL.String(), actor) {
				t.Fatalf("authority value was not independently bound: %s, %#v", query.Statement.SQL.String(), query.Statement.Vars)
			}
			sqlDB, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			if err := sqlDB.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
