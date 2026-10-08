package database

import (
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// DropIndex removes an index from the table resolved by GORM, preserving table
// overrides and naming strategies. Qualified index names must agree with the
// resolved PostgreSQL table namespace; identifiers are server-owned physical
// names, not SQL expressions.
//
// gorm.io/driver/postgres v1.6.2 emits DROP INDEX CURRENT_SCHEMA().name, which
// PostgreSQL rejects. An explicit table schema is instead bound as a value.
// GORM has no portable API to resolve the actual relation/index namespace or express
// this corrected DDL. Keep the bound PostgreSQL catalog lookup and GORM-quoted
// index-only DDL here; MySQL's table-scoped migrator remains usable unchanged.
func DropIndex(db *gorm.DB, table any, name string) error {
	migration := db.Migrator()
	if db.Name() != "postgres" {
		return migration.DropIndex(table, name)
	}
	parser, ok := migration.(interface {
		RunWithValue(any, func(*gorm.Statement) error) error
	})
	if !ok {
		return gorm.ErrUnsupportedDriver
	}
	return parser.RunWithValue(table, func(stmt *gorm.Statement) error {
		if stmt.Schema != nil {
			if index := stmt.Schema.LookIndex(name); index != nil {
				name = index.Name
			}
		}
		parts := strings.Split(name, ".")
		if len(parts) > 2 || parts[0] == "" || parts[len(parts)-1] == "" {
			return gorm.ErrInvalidValue
		}
		reference := stmt.Quote(clause.Table{Name: stmt.Table})
		if stmt.TableExpr != nil {
			if len(stmt.TableExpr.Vars) != 0 {
				return gorm.ErrInvalidValue
			}
			reference = stmt.TableExpr.SQL
		}
		var namespace string
		// to_regclass resolves the exact quoted table through PostgreSQL's native
		// search path. Bind the index to that relation, so neither a same-named
		// index in another namespace nor an index on another table is targeted.
		if err := db.Table("pg_catalog.pg_class AS relation").Select("namespace.nspname").
			Joins("JOIN pg_catalog.pg_namespace AS namespace ON namespace.oid = relation.relnamespace").
			Joins("JOIN pg_catalog.pg_index AS membership ON membership.indrelid = relation.oid").
			Joins("JOIN pg_catalog.pg_class AS index_relation ON index_relation.oid = membership.indexrelid").
			Where("relation.oid = to_regclass(?)", reference).
			Where("relation.relkind IN ?", []string{"r", "p"}).
			Where("index_relation.relname = ?", parts[len(parts)-1]).Row().Scan(&namespace); err != nil {
			return err
		}
		if namespace == "" || len(parts) == 2 && parts[0] != namespace {
			return gorm.ErrInvalidValue
		}
		// GORM treats dots in raw identifiers as namespace separators. Prequote
		// each catalog/name component so literal dots and quotes remain one name.
		quote := func(value string) string { return `"` + strings.ReplaceAll(value, `"`, `""`) + `"` }
		return db.Exec("DROP INDEX ?", clause.Column{Table: quote(namespace), Name: quote(parts[len(parts)-1])}).Error
	})
}
