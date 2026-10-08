package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

type dropIndexFixture struct {
	ID int `gorm:"index:idx_runtime_routing_application_instance"`
}

func (dropIndexFixture) TableName() string { return "runtime_routing_applications" }

type namedIndexDropFixture struct {
	ID int `gorm:"index:idx_runtime_routing_application_instance"`
}

type indexDropPool struct {
	query, lookup string
	args          []any
	lookupArgs    []any
	calls, reads  int
	err, readErr  error
	namespace     string
	missing       bool
}

type indexDropConnector struct{ pool *indexDropPool }
type indexDropDriver struct{}
type indexDropConn struct{ pool *indexDropPool }
type indexDropRows struct {
	values []driver.Value
	done   bool
}

func (indexDropDriver) Open(string) (driver.Conn, error) { panic("no database driver opens") }
func (c indexDropConnector) Driver() driver.Driver       { return indexDropDriver{} }
func (c indexDropConnector) Connect(context.Context) (driver.Conn, error) {
	return &indexDropConn{pool: c.pool}, nil
}
func (*indexDropConn) Prepare(string) (driver.Stmt, error) { panic("no preparation") }
func (*indexDropConn) Close() error                        { return nil }
func (*indexDropConn) Begin() (driver.Tx, error)           { panic("no transactions") }
func (c *indexDropConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	p := c.pool
	p.calls++
	p.query, p.args = query, nil
	for _, arg := range args {
		p.args = append(p.args, arg.Value)
	}
	return driver.RowsAffected(1), p.err
}
func (c *indexDropConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	p := c.pool
	p.reads++
	p.lookup, p.lookupArgs = query, nil
	for _, arg := range args {
		p.lookupArgs = append(p.lookupArgs, arg.Value)
	}
	if p.readErr != nil {
		return nil, p.readErr
	}
	rows := &indexDropRows{values: []driver.Value{p.namespace}, done: p.missing}
	return rows, nil
}
func (*indexDropRows) Columns() []string { return []string{"nspname"} }
func (*indexDropRows) Close() error      { return nil }
func (r *indexDropRows) Next(values []driver.Value) error {
	if r.done {
		return io.EOF
	}
	copy(values, r.values)
	r.done = true
	return nil
}

func indexDropDB(t *testing.T, dialect string, pool *indexDropPool, naming schema.Namer) *gorm.DB {
	t.Helper()
	conn := sql.OpenDB(indexDropConnector{pool})
	t.Cleanup(func() { _ = conn.Close() })
	var adapter gorm.Dialector
	if dialect == "postgres" {
		adapter = postgres.New(postgres.Config{Conn: conn})
	} else {
		adapter = mysql.New(mysql.Config{Conn: conn, SkipInitializeWithVersion: true})
	}
	db, err := gorm.Open(adapter, &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent), NamingStrategy: naming})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestDropIndexPinnedPostgresMigratorLimitation(t *testing.T) {
	for _, table := range []string{"runtime_routing_applications", "archive.runtime_routing_applications"} {
		t.Run(table, func(t *testing.T) {
			pool := &indexDropPool{}
			db := indexDropDB(t, "postgres", pool, nil)
			if err := db.Migrator().DropIndex(table, "idx_runtime_routing_application_instance"); err != nil {
				t.Fatal(err)
			}
			if table == "runtime_routing_applications" {
				if pool.query != `DROP INDEX CURRENT_SCHEMA()."idx_runtime_routing_application_instance"` || len(pool.args) != 0 {
					t.Fatalf("pinned default regression changed: %q, %#v", pool.query, pool.args)
				}
			} else if pool.query != `DROP INDEX $1."idx_runtime_routing_application_instance"` || !reflect.DeepEqual(pool.args, []any{"archive"}) {
				t.Fatalf("explicit schema is a value: %q, %#v", pool.query, pool.args)
			}
		})
	}
}

func TestDropIndexPortableDDLAndQuotedIdentifier(t *testing.T) {
	for _, fixture := range []struct {
		dialect, table, namespace, name, reference, sql string
	}{
		{"postgres", "runtime_routing_applications", "public", "idx_runtime_routing_application_instance", `"runtime_routing_applications"`, `DROP INDEX "public"."idx_runtime_routing_application_instance"`},
		// public and archive may both contain an index of this name. The explicit
		// table wins even when public is first in the connection's search path.
		{"postgres", "archive.runtime_routing_applications", "archive", "idx_runtime_routing_application_instance", `"archive"."runtime_routing_applications"`, `DROP INDEX "archive"."idx_runtime_routing_application_instance"`},
		// An unqualified table can resolve to archive while a public index shares
		// the name; native table resolution, never index search path, is used.
		{"postgres", "runtime_routing_applications", "archive", "idx_runtime_routing_application_instance", `"runtime_routing_applications"`, `DROP INDEX "archive"."idx_runtime_routing_application_instance"`},
		{"postgres", "runtime_routing_applications", "public", "public.idx_runtime_routing_application_instance", `"runtime_routing_applications"`, `DROP INDEX "public"."idx_runtime_routing_application_instance"`},
		{"postgres", "archive.runtime_routing_applications", "archive", "archive.idx_runtime_routing_application_instance", `"archive"."runtime_routing_applications"`, `DROP INDEX "archive"."idx_runtime_routing_application_instance"`},
		{"postgres", "runtime_routing_applications", `arch"ive.dot`, `idx"quoted; DROP TABLE unrelated --`, `"runtime_routing_applications"`, `DROP INDEX "arch""ive.dot"."idx""quoted; DROP TABLE unrelated --"`},
		{"mysql", "runtime_routing_applications", "", "idx_runtime_routing_application_instance", "", "DROP INDEX `idx_runtime_routing_application_instance` ON `runtime_routing_applications`"},
		{"mysql", "archive.runtime_routing_applications", "", "idx_runtime_routing_application_instance", "", "DROP INDEX `idx_runtime_routing_application_instance` ON `archive`.`runtime_routing_applications`"},
		{"mysql", "runtime_routing_applications", "", "idx`quoted; DROP TABLE unrelated --", "", "DROP INDEX `idx``quoted; DROP TABLE unrelated --` ON `runtime_routing_applications`"},
	} {
		t.Run(fixture.dialect+"/"+fixture.table+"/"+fixture.name, func(t *testing.T) {
			pool := &indexDropPool{namespace: fixture.namespace}
			db := indexDropDB(t, fixture.dialect, pool, nil)
			if err := DropIndex(db, fixture.table, fixture.name); err != nil {
				t.Fatal(err)
			}
			if pool.calls != 1 || pool.query != fixture.sql || len(pool.args) != 0 || strings.Contains(pool.query, "CURRENT_SCHEMA()") {
				t.Fatalf("wrong table-scoped index DDL: %q, %#v", pool.query, pool.args)
			}
			if fixture.dialect == "postgres" {
				if pool.reads != 1 || !strings.Contains(pool.lookup, "relation.oid = to_regclass($1)") || !strings.Contains(pool.lookup, "membership.indrelid = relation.oid") || !strings.Contains(pool.lookup, "index_relation.relname = $4") || !reflect.DeepEqual(pool.lookupArgs, []any{fixture.reference, "r", "p", strings.Split(fixture.name, ".")[len(strings.Split(fixture.name, "."))-1]}) || strings.Contains(pool.lookup, fixture.reference) {
					t.Fatalf("table lookup was not independently bound: %q, %#v", pool.lookup, pool.lookupArgs)
				}
			} else if pool.reads != 0 {
				t.Fatal("MySQL did not retain its direct migrator path")
			}
		})
	}
}

func TestDropIndexGORMModelNamingAndTableOverride(t *testing.T) {
	for _, override := range []bool{false, true} {
		t.Run(map[bool]string{false: "naming_strategy", true: "table_override"}[override], func(t *testing.T) {
			pool := &indexDropPool{namespace: "archive"}
			db := indexDropDB(t, "postgres", pool, schema.NamingStrategy{TablePrefix: "archive."})
			table := &namedIndexDropFixture{}
			// The prefix and explicit override use GORM's migration parser.
			reference := `"archive"."named_index_drop_fixtures"`
			if override {
				db = db.Table("archive.explicit_table")
				reference = `"archive"."explicit_table"`
			}
			if err := DropIndex(db, table, "ID"); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(pool.lookupArgs, []any{reference, "r", "p", "idx_runtime_routing_application_instance"}) || pool.query != `DROP INDEX "archive"."idx_runtime_routing_application_instance"` {
				t.Fatalf("GORM statement/index lookup lost: %q, %#v", pool.query, pool.lookupArgs)
			}
		})
	}
}

func TestDropIndexRejectsNamespaceConflictAndUnknownTable(t *testing.T) {
	for _, fixture := range []struct {
		name, index, namespace string
		missing                bool
		readErr                error
	}{
		{"conflicting_qualified_index", "public.idx_runtime_routing_application_instance", "archive", false, nil},
		{"missing_table", "idx_runtime_routing_application_instance", "", true, nil},
		{"index_belongs_to_another_table", "idx_runtime_routing_application_instance", "", true, nil},
		{"empty_namespace", "idx_runtime_routing_application_instance", "", false, nil},
		{"lookup_failure", "idx_runtime_routing_application_instance", "", false, errors.New("controlled lookup failure")},
		{"empty_index", "", "archive", false, nil},
		{"malformed_qualified_index", "archive..index", "archive", false, nil},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			pool := &indexDropPool{namespace: fixture.namespace, missing: fixture.missing, readErr: fixture.readErr}
			db := indexDropDB(t, "postgres", pool, nil)
			if err := DropIndex(db, "archive.runtime_routing_applications", fixture.index); err == nil || pool.calls != 0 {
				t.Fatalf("unknown/conflicting target reached DDL: %v, calls=%d", err, pool.calls)
			}
		})
	}
}

func TestDropIndexPropagatesDriverFailure(t *testing.T) {
	for _, dialect := range []string{"postgres", "mysql"} {
		t.Run(dialect, func(t *testing.T) {
			failure := errors.New("controlled index drop failure")
			pool := &indexDropPool{err: failure, namespace: "public"}
			db := indexDropDB(t, dialect, pool, nil)
			if err := DropIndex(db, &dropIndexFixture{}, "idx_runtime_routing_application_instance"); !errors.Is(err, failure) || pool.calls != 1 {
				t.Fatalf("failure must propagate without retry: %v, calls=%d", err, pool.calls)
			}
		})
	}
}
