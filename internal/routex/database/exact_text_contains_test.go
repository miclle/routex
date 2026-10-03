package database

import (
	"reflect"
	"strings"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestExactTextContainsBindsLiteralCaseSensitiveFragments(t *testing.T) {
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
			sqlDB, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := sqlDB.Close(); err != nil {
					t.Error(err)
				}
			})
			for _, value := range []string{"prj_A", "_%!", "é", "' OR 1=1", "trailing "} {
				query := db.Table("projects").Where(ExactTextContains(db, clause.Column{Name: "id"}, value)).Where("status = ?", "active").Find(&[]map[string]any{})
				pattern := "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(value) + "%"
				if query.Error != nil || !reflect.DeepEqual(query.Statement.Vars, []any{pattern, "active"}) {
					t.Fatalf("literal fragment was not bound: %v, %#v", query.Error, query.Statement.Vars)
				}
				sql := query.Statement.SQL.String()
				if !strings.Contains(sql, "ESCAPE '!'") || strings.Contains(sql, "LOWER(") || strings.Contains(sql, pattern) || (fixture.name == "mysql" && strings.Count(sql, "AS BINARY") != 2) {
					t.Fatalf("case-sensitive escaped predicate changed: %s", sql)
				}
			}
		})
	}
}
