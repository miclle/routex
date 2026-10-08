package service

import (
	"reflect"
	"strings"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestModelCreationProviderQueryHasNoChildReadsAndEscapesLiteralSearch(t *testing.T) {
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: "host=localhost user=test dbname=test sslmode=disable"}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := pool.Close(); err != nil {
			t.Error(err)
		}
	})
	f, pattern, err := normalizeModelCreationFilter(ModelCreationFilter{Query: "prv_%!", Cursor: "prv_before", Limit: 2}, "prv")
	if err != nil {
		t.Fatal(err)
	}
	q := modelCreationProviderQuery(db, f, pattern).Find(&[]ModelCreationProvider{})
	sql := q.Statement.SQL.String()
	if q.Error != nil || !strings.Contains(sql, "LIMIT") || strings.Contains(sql, "JOIN") || strings.Contains(sql, "credential") || !strings.Contains(sql, `SELECT "id","name"`) || !reflect.DeepEqual(q.Statement.Vars, []any{"%prv!_!%!!%", "prv!_!%!!%", "prv_before", 3}) {
		t.Fatal(sql, q.Statement.Vars, q.Error)
	}
	for _, bad := range []ModelCreationFilter{{Cursor: "con_wrong"}, {Limit: 51}, {Query: strings.Repeat("x", 201)}} {
		if _, _, err := normalizeModelCreationFilter(bad, "prv"); err == nil {
			t.Fatal("unbounded or wrong-scope Provider filter", bad)
		}
	}
}

func TestModelAccessPickerExactIdentityDoesNotUseNameOrPrefixSearch(t *testing.T) {
	for _, driver := range []struct {
		name      string
		dialector gorm.Dialector
	}{
		{"postgres", postgres.New(postgres.Config{DSN: "host=localhost user=test dbname=test sslmode=disable"})},
		{"mysql", mysql.New(mysql.Config{DSN: "test:test@tcp(localhost:3306)/test", SkipInitializeWithVersion: true})},
	} {
		t.Run(driver.name, func(t *testing.T) {
			db, err := gorm.Open(driver.dialector, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
			if err != nil {
				t.Fatal(err)
			}
			pool, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := pool.Close(); err != nil {
					t.Error(err)
				}
			})
			for _, prefix := range []string{"prv", "egr"} {
				f, pattern, err := normalizeModelAccessPickerFilter(ModelAccessPickerFilter{ExactID: prefix + "_inline_A"}, prefix)
				if err != nil || f.Limit != 1 {
					t.Fatal("exact bounds", f, err)
				}
				q := modelAccessProviderQuery(db, f, pattern)
				if prefix == "egr" {
					q = modelAccessEgressQuery(db, f, pattern)
				}
				q = q.Find(&[]map[string]any{})
				sql := q.Statement.SQL.String()
				if q.Error != nil || strings.Contains(sql, "LIKE") || strings.Contains(sql, "LOWER") || strings.Contains(sql, " OR ") || strings.Contains(sql, f.ExactID) || !reflect.DeepEqual(q.Statement.Vars, []any{f.ExactID, 1}) {
					t.Fatal("selected identity borrowed a name/prefix query", sql, q.Statement.Vars, q.Error)
				}
			}
		})
	}
	for _, f := range []ModelAccessPickerFilter{
		{ExactID: "con_other"}, {ExactID: "PRV_alias"}, {ExactID: "prv_one\n"},
		{ExactID: "prv_one", ModelCreationFilter: ModelCreationFilter{Query: "One"}},
		{ExactID: "prv_one", ModelCreationFilter: ModelCreationFilter{Cursor: "prv_cursor"}},
		{ExactID: "prv_one", ModelCreationFilter: ModelCreationFilter{Limit: 51}},
	} {
		if _, _, err := normalizeModelAccessPickerFilter(f, "prv"); err == nil {
			t.Fatal("ambiguous or wrong-target selector", f)
		}
	}
	for _, limit := range []int{0, 20, 50} {
		f, _, err := normalizeModelAccessPickerFilter(ModelAccessPickerFilter{ModelCreationFilter: ModelCreationFilter{Limit: limit}}, "prv")
		want := limit
		if want == 0 {
			want = 20
		}
		if err != nil || f.Limit != want {
			t.Fatal("ordinary page bounds changed", f, err)
		}
	}
}
