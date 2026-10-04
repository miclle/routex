package database

import (
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
	"testing"
)

func TestByteInventoryCursorAndOrderingAreBoundExact(t *testing.T) {
	for _, fixture := range []struct {
		name    string
		dialect gorm.Dialector
	}{{"postgres", postgres.New(postgres.Config{DSN: "host=localhost user=test dbname=test sslmode=disable"})}, {"mysql", mysql.New(mysql.Config{DSN: "test:test@tcp(localhost:3306)/test", SkipInitializeWithVersion: true})}} {
		t.Run(fixture.name, func(t *testing.T) {
			db, err := gorm.Open(fixture.dialect, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
			if err != nil {
				t.Fatal(err)
			}
			pool, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := pool.Close(); err != nil {
					t.Error(err)
				}
			}()
			value := "crd_' OR 1=1 --"
			column := clause.Column{Name: "id"}
			q := db.Table("provider_credentials").Where(ByteAfter(db, column, value)).Clauses(clause.OrderBy{Expression: ByteOrder(db, column)}).Limit(50).Find(&[]map[string]any{})
			if q.Error != nil {
				t.Fatal(q.Error)
			}
			sql := q.Statement.SQL.String()
			if len(q.Statement.Vars) != 2 || q.Statement.Vars[0] != value || strings.Contains(sql, value) || !strings.Contains(sql, "ORDER BY") {
				t.Fatal("cursor not independently parameterized", sql, q.Statement.Vars)
			}
			if fixture.name == "mysql" && !strings.Contains(sql, "AS BINARY") {
				t.Fatal("inventory follows aliasing collation")
			}
			if fixture.name == "postgres" && !strings.Contains(sql, `COLLATE "C"`) {
				t.Fatal("inventory follows mutable locale")
			}
		})
	}
}
