package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

type credentialStatisticsMigrator struct {
	gorm.Migrator
	present bool
	creates int
	failure error
}

func (m *credentialStatisticsMigrator) HasIndex(any, string) bool { return m.present }
func (m *credentialStatisticsMigrator) CreateIndex(any, string) error {
	m.creates++
	if m.failure != nil {
		return m.failure
	}
	m.present = true
	return nil
}

type credentialStatisticsDialect struct {
	gorm.Dialector
	m *credentialStatisticsMigrator
}

func (d credentialStatisticsDialect) Migrator(*gorm.DB) gorm.Migrator { return d.m }

type credentialStatisticsIndexPool struct {
	rows    [][]driver.Value
	failure error
	query   string
	args    []any
	reads   int
}

type credentialStatisticsIndexConnector struct {
	pool *credentialStatisticsIndexPool
}
type credentialStatisticsIndexDriver struct{}
type credentialStatisticsIndexConn struct {
	pool *credentialStatisticsIndexPool
}
type credentialStatisticsIndexRows struct{ rows [][]driver.Value }

func (credentialStatisticsIndexDriver) Open(string) (driver.Conn, error) {
	panic("no database driver opens")
}
func (c credentialStatisticsIndexConnector) Driver() driver.Driver {
	return credentialStatisticsIndexDriver{}
}
func (c credentialStatisticsIndexConnector) Connect(context.Context) (driver.Conn, error) {
	return &credentialStatisticsIndexConn{pool: c.pool}, nil
}
func (*credentialStatisticsIndexConn) Prepare(string) (driver.Stmt, error) {
	panic("no preparation")
}
func (*credentialStatisticsIndexConn) Close() error              { return nil }
func (*credentialStatisticsIndexConn) Begin() (driver.Tx, error) { panic("no transactions") }
func (c *credentialStatisticsIndexConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	p := c.pool
	p.reads++
	p.query, p.args = query, nil
	for _, arg := range args {
		p.args = append(p.args, arg.Value)
	}
	if p.failure != nil {
		return nil, p.failure
	}
	return &credentialStatisticsIndexRows{rows: p.rows}, nil
}
func (*credentialStatisticsIndexRows) Columns() []string {
	return []string{"column_name", "position", "is_unique", "prefix_length"}
}
func (*credentialStatisticsIndexRows) Close() error { return nil }
func (r *credentialStatisticsIndexRows) Next(values []driver.Value) error {
	if len(r.rows) == 0 {
		return io.EOF
	}
	copy(values, r.rows[0])
	r.rows = r.rows[1:]
	return nil
}

func credentialStatisticsIndexDB(t *testing.T, dialect string, pool *credentialStatisticsIndexPool, migrator *credentialStatisticsMigrator) *gorm.DB {
	t.Helper()
	conn := sql.OpenDB(credentialStatisticsIndexConnector{pool})
	t.Cleanup(func() { _ = conn.Close() })
	var adapter gorm.Dialector
	if dialect == "postgres" {
		adapter = postgres.New(postgres.Config{Conn: conn})
	} else {
		adapter = mysql.New(mysql.Config{Conn: conn, SkipInitializeWithVersion: true})
	}
	db, err := gorm.Open(credentialStatisticsDialect{Dialector: adapter, m: migrator}, &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestCredentialAttemptStatisticsV93IndexShapeAndPartialReplay(t *testing.T) {
	for _, dialect := range []string{"postgres", "mysql"} {
		for _, tc := range []struct {
			name                                                          string
			present, unique, unknown, fail, readFail, badPosition, prefix bool
			columns                                                       []string
			wantError                                                     bool
		}{
			{name: "new", columns: []string{"credential_id", "completed_at", "id"}},
			{name: "survived DDL without ledger", present: true, columns: []string{"credential_id", "completed_at", "id"}},
			{name: "wrong order", present: true, columns: []string{"credential_id", "id", "completed_at"}, wantError: true},
			{name: "physical table order", present: true, columns: []string{"id", "completed_at", "credential_id"}, wantError: true},
			{name: "missing index", present: true, wantError: true},
			{name: "missing column", present: true, columns: []string{"credential_id", "completed_at"}, wantError: true},
			{name: "extra column", present: true, columns: []string{"credential_id", "completed_at", "id", "snapshot_id"}, wantError: true},
			{name: "prefix column", present: true, prefix: true, columns: []string{"credential_id", "completed_at", "id"}, wantError: true},
			{name: "extra prefix column", present: true, prefix: true, columns: []string{"credential_id", "completed_at", "id", "error_code"}, wantError: true},
			{name: "wrong unique", present: true, unique: true, columns: []string{"credential_id", "completed_at", "id"}, wantError: true},
			{name: "unknown uniqueness", present: true, unknown: true, columns: []string{"credential_id", "completed_at", "id"}, wantError: true},
			{name: "invalid ordinal", present: true, badPosition: true, columns: []string{"credential_id", "completed_at", "id"}, wantError: true},
			{name: "metadata unavailable", present: true, readFail: true, wantError: true},
			{name: "DDL interrupted", fail: true, wantError: true},
		} {
			t.Run(dialect+"/"+tc.name, func(t *testing.T) {
				m := &credentialStatisticsMigrator{present: tc.present}
				pool := &credentialStatisticsIndexPool{}
				for i, column := range tc.columns {
					var unique driver.Value = tc.unique
					if tc.unknown {
						unique = nil
					}
					position := int64(i + 1)
					if tc.badPosition {
						position++
					}
					var prefix driver.Value
					if tc.prefix && i == len(tc.columns)-1 {
						prefix = int64(4)
					}
					pool.rows = append(pool.rows, []driver.Value{column, position, unique, prefix})
				}
				if tc.fail {
					m.failure = errors.New("controlled index creation failure")
				}
				if tc.readFail {
					pool.failure = errors.New("controlled index metadata failure")
				}
				db := credentialStatisticsIndexDB(t, dialect, pool, m)
				err := credentialAttemptStatisticsMigration(db)
				if (err != nil) != tc.wantError || tc.fail && !errors.Is(err, m.failure) || tc.readFail && !errors.Is(err, pool.failure) {
					t.Fatal("shape/failure mismatch", err)
				}
				if tc.fail && pool.reads != 0 {
					t.Fatal("failed DDL reached metadata")
				}
				if !tc.wantError {
					if err := credentialAttemptStatisticsMigration(db); err != nil {
						t.Fatal(err)
					}
					want := 1
					if tc.present {
						want = 0
					}
					if m.creates != want || pool.reads != 2 {
						t.Fatal("repeat did not revalidate without DDL", m.creates, pool.reads)
					}
				}
			})
		}
	}
}

func TestCredentialAttemptStatisticsV93OrderedMetadataAndBoundIdentities(t *testing.T) {
	for _, dialect := range []string{"postgres", "mysql"} {
		t.Run(dialect, func(t *testing.T) {
			pool := &credentialStatisticsIndexPool{}
			db := credentialStatisticsIndexDB(t, dialect, pool, &credentialStatisticsMigrator{})
			table, index := "call_attempts'quoted", "idx_attempts'quoted"
			if _, err := credentialAttemptStatisticsIndexColumns(db, table, index); err != nil {
				t.Fatal(err)
			}
			if pool.reads != 1 || !reflect.DeepEqual(pool.args, []any{table, index}) || strings.Contains(pool.query, table) || strings.Contains(pool.query, index) {
				t.Fatal("metadata identities were not bound", pool.query, pool.args)
			}
			var needles []string
			if dialect == "postgres" {
				needles = []string{"pg_catalog.pg_index AS membership", "WITH ORDINALITY AS index_key(attnum, ordinality)", "attribute.attnum = index_key.attnum", "relation.oid = to_regclass($1)", "index_relation.relname = $2", "membership.indisunique AS is_unique", "membership.indisvalid AND membership.indisready", "membership.indpred IS NULL AND membership.indexprs IS NULL", "ORDER BY index_key.ordinality ASC"}
			} else {
				needles = []string{"`information_schema`.`statistics`", "seq_in_index AS position", "non_unique = 0 AS is_unique", "sub_part AS prefix_length", "table_schema = DATABASE() AND table_name = ? AND index_name = ?", "ORDER BY seq_in_index ASC"}
			}
			if strings.Contains(pool.query, "sub_part IS NULL") {
				t.Fatal("prefix metadata rows were hidden", pool.query)
			}
			for _, needle := range needles {
				if !strings.Contains(pool.query, needle) {
					t.Fatal("ordered metadata contract missing", needle, pool.query)
				}
			}
		})
	}
}

func TestCredentialAttemptStatisticsV93FrozenSchemaAndRegistry(t *testing.T) {
	parsed, err := schema.Parse(&credentialAttemptStatisticsV93{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Table != "call_attempts" || len(parsed.Fields) != 3 {
		t.Fatal(parsed)
	}
	indexes := parsed.ParseIndexes()
	if len(indexes) != 1 || indexes[0].Name != "idx_attempts_credential_time" || len(indexes[0].Fields) != 3 {
		t.Fatal(indexes)
	}
	for i, name := range []string{"credential_id", "completed_at", "id"} {
		if indexes[0].Fields[i].DBName != name {
			t.Fatal(indexes)
		}
	}
	for _, dialect := range []string{"postgres", "mysql"} {
		steps := migrationSteps(dialect)
		if len(steps) != 96 || reflect.ValueOf(steps[95]).Pointer() != reflect.ValueOf(ldapMigration).Pointer() || reflect.ValueOf(steps[94]).Pointer() != reflect.ValueOf(oauthMigration).Pointer() || reflect.ValueOf(steps[93]).Pointer() != reflect.ValueOf(oidcMigration).Pointer() || reflect.ValueOf(steps[92]).Pointer() != reflect.ValueOf(credentialAttemptStatisticsMigration).Pointer() || reflect.ValueOf(steps[91]).Pointer() != reflect.ValueOf(connectionTransportMigration).Pointer() {
			t.Fatal("exact V92 prefix and V93 suffix", dialect)
		}
	}
}
