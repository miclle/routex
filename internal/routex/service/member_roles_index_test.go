package service

import (
	"context"
	"database/sql/driver"
	"io"
	"reflect"
	"strings"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMemberRoleIDBatchesKeepIndexedSupersetAndExactAuthority(t *testing.T) {
	for _, fixture := range []struct {
		name      string
		dialector gorm.Dialector
	}{
		{"postgres", postgres.New(postgres.Config{DSN: "host=localhost user=test dbname=test sslmode=disable"})},
		{"mysql", mysql.New(mysql.Config{DSN: "test:test@tcp(localhost:3306)/test", SkipInitializeWithVersion: true})},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			db, err := gorm.Open(fixture.dialector, &gorm.Config{DryRun: true, DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = sqlDB.Close() })
			ids := []string{"rol_exact", "ROL_EXACT", "rol_' OR 1=1 --"}
			for _, column := range []string{"id", "role_id"} {
				query := memberRoleIDsQuery(db.Table("role_fixture"), column, ids).Find(&[]map[string]any{})
				if query.Error != nil {
					t.Fatal(query.Error)
				}
				sql := query.Statement.SQL.String()
				quoted := "\"" + column + "\""
				if fixture.name == "mysql" {
					quoted = "`" + column + "`"
				}
				if !strings.Contains(sql, quoted+" IN (") || !strings.Contains(sql, " AND (") {
					t.Fatal("indexed superset must remain conjunctive")
				}
				expected := []any{ids[0], ids[1], ids[2], ids[0], ids[1], ids[2]}
				if !reflect.DeepEqual(query.Statement.Vars, expected) {
					t.Fatal("independently bound candidate and exact values differ")
				}
				if fixture.name == "mysql" {
					if strings.Count(sql, "CAST("+quoted+" AS BINARY)") != len(ids) {
						t.Fatal("MySQL byte-exact authority condition lost")
					}
				} else if strings.Count(sql, quoted+" = ") != len(ids) {
					t.Fatal("PostgreSQL exact authority condition lost")
				}
				for _, id := range ids {
					if strings.Contains(sql, id) {
						t.Fatal("identifier escaped the bind list")
					}
				}
			}
			empty := memberRoleIDsQuery(db.Table("role_fixture"), "id", nil).Find(&[]map[string]any{})
			if empty.Error != nil || !strings.Contains(empty.Statement.SQL.String(), "1 = 0") || len(empty.Statement.Vars) != 0 {
				t.Fatal("empty batch broadened authority")
			}
		})
	}
}

func TestMemberRolesSQLFixtureDoesNotMultiplyRepeatedPredicateBinds(t *testing.T) {
	f := &rolesSQLFixture{data: rolesSQLData{permissions: map[string][]string{"rol_a": {"members.read"}, "rol_b": {"roles.read"}}}}
	c := &rolesSQLConnection{f: f}
	args := []driver.NamedValue{{Ordinal: 1, Value: "rol_a"}, {Ordinal: 2, Value: "rol_b"}, {Ordinal: 3, Value: "rol_a"}, {Ordinal: 4, Value: "rol_b"}, {Ordinal: 5, Value: int64(100001)}}
	rows, err := c.QueryContext(context.Background(), `SELECT * FROM "role_permissions" WHERE "role_id" IN ($1,$2) AND ("role_id" = $3 OR "role_id" = $4) LIMIT $5`, args)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	values := make([]driver.Value, len(rows.Columns()))
	got := map[string]string{}
	for {
		err := rows.Next(values)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		id, ok := values[0].(string)
		if !ok {
			t.Fatal("invalid role identity")
		}
		code, ok := values[1].(string)
		if !ok {
			t.Fatal("invalid recorded permission")
		}
		if _, dup := got[id]; dup {
			t.Fatal("duplicate bind became duplicate permission row")
		}
		got[id] = code
	}
	if !reflect.DeepEqual(got, map[string]string{"rol_a": "members.read", "rol_b": "roles.read"}) {
		t.Fatal("complete recorded permissions changed")
	}
}
