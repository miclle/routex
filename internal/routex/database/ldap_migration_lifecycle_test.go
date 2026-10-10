package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

// This connector supplies metadata only. It opens no socket and rejects every
// statement except the exact two V96 index inspections.
type ldapV96IndexConnector struct{}

func (ldapV96IndexConnector) Driver() driver.Driver { return credentialStatisticsIndexDriver{} }
func (ldapV96IndexConnector) Connect(context.Context) (driver.Conn, error) {
	return &ldapV96IndexConn{}, nil
}

type ldapV96IndexConn struct{ credentialStatisticsIndexConn }

func (*ldapV96IndexConn) QueryContext(_ context.Context, _ string, args []driver.NamedValue) (driver.Rows, error) {
	if len(args) != 2 || args[0].Value != "ldap_bindings" {
		return nil, errors.New("unowned metadata query")
	}
	var column string
	switch args[1].Value {
	case "idx_ldap_bindings_user_id":
		column = "user_id"
	case "idx_ldap_bindings_subject_digest":
		column = "subject_digest"
	default:
		return nil, errors.New("unowned index")
	}
	return &credentialStatisticsIndexRows{rows: [][]driver.Value{{column, int64(1), true, nil}}}, nil
}

type ldapV96LifecycleMigrator struct {
	gorm.Migrator
	t                                *testing.T
	tables, columns, indexes, checks map[string]bool
	ops                              []string
	fail                             string
	provider                         *ldapProviderV96
}

func (m *ldapV96LifecycleMigrator) schemaTable(model any) string {
	return oidcV94Schema(m.t, model).Table
}
func (m *ldapV96LifecycleMigrator) HasTable(model any) bool { return m.tables[m.schemaTable(model)] }
func (m *ldapV96LifecycleMigrator) CreateTable(models ...any) error {
	for _, model := range models {
		name := m.schemaTable(model)
		op := "table:" + name
		m.ops = append(m.ops, op)
		if m.fail == op {
			return errors.New(op)
		}
		m.tables[name] = true
	}
	return nil
}
func (m *ldapV96LifecycleMigrator) HasColumn(model any, name string) bool {
	return m.columns[m.schemaTable(model)+":"+name]
}
func (m *ldapV96LifecycleMigrator) AddColumn(model any, name string) error {
	key := m.schemaTable(model) + ":" + name
	op := "column:" + key
	m.ops = append(m.ops, op)
	if m.fail == op {
		return errors.New(op)
	}
	m.columns[key] = true
	return nil
}
func (m *ldapV96LifecycleMigrator) HasIndex(_ any, name string) bool { return m.indexes[name] }
func (m *ldapV96LifecycleMigrator) CreateIndex(_ any, name string) error {
	op := "index:" + name
	m.ops = append(m.ops, op)
	if m.fail == op {
		return errors.New(op)
	}
	m.indexes[name] = true
	return nil
}
func (m *ldapV96LifecycleMigrator) HasConstraint(_ any, name string) bool { return m.checks[name] }
func (m *ldapV96LifecycleMigrator) DropConstraint(_ any, name string) error {
	op := "drop:" + name
	m.ops = append(m.ops, op)
	if m.fail == op {
		return errors.New(op)
	}
	m.checks[name] = false
	return nil
}
func (m *ldapV96LifecycleMigrator) CreateConstraint(_ any, name string) error {
	op := "check:" + name
	m.ops = append(m.ops, op)
	if m.fail == op {
		return errors.New(op)
	}
	m.checks[name] = true
	return nil
}
func (m *ldapV96LifecycleMigrator) ColumnTypes(model any) ([]gorm.ColumnType, error) {
	return oidcV94Columns(m.t, model), nil
}

type ldapV96LifecycleDialect struct {
	gorm.Dialector
	m *ldapV96LifecycleMigrator
}

func (d ldapV96LifecycleDialect) Migrator(*gorm.DB) gorm.Migrator { return d.m }

func TestLDAPV96CreationAndPartialDDLResume(t *testing.T) {
	faults := []string{"", "column:sessions:LDAPPolicyRevision", "column:mfa_challenges:LDAPBindingCreatedAt", "table:ldap_bindings", "index:idx_ldap_bindings_user_id", "drop:ck_sessions_oidc_primary", "check:ck_mfa_challenges_oidc_primary", "check:ck_secret_item_domain"}
	for _, fault := range faults {
		label := fault
		if label == "" {
			label = "empty_and_repeat"
		}
		t.Run(label, func(t *testing.T) {
			m := &ldapV96LifecycleMigrator{t: t, tables: map[string]bool{}, columns: map[string]bool{}, indexes: map[string]bool{}, checks: map[string]bool{}, fail: fault}
			for _, name := range []string{"ck_sessions_oidc_primary", "ck_mfa_challenges_oidc_primary", "ck_secret_inventory_version", "ck_secret_rotation_domain", "ck_secret_item_domain", "ck_secret_process_inventory_version"} {
				m.checks[name] = true
			}
			conn := sql.OpenDB(ldapV96IndexConnector{})
			t.Cleanup(func() { _ = conn.Close() })
			db, err := gorm.Open(ldapV96LifecycleDialect{Dialector: postgres.New(postgres.Config{Conn: conn}), m: m}, &gorm.Config{DisableAutomaticPing: true, SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil {
				t.Fatal(err)
			}
			scans := 0
			if err := db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
				switch tx.Statement.Table {
				case "sessions", "mfa_challenges", "secret_rotation_jobs", "secret_process_verifications", "secret_rotation_items":
				default:
					t.Fatal("unowned retained scan", tx.Statement.Table)
				}
				count, ok := tx.Statement.Dest.(*int64)
				if !ok {
					t.Fatal("unbounded retained destination")
				}
				*count = 0
				tx.RowsAffected = 1
				scans++
			}); err != nil {
				t.Fatal(err)
			}
			creates := 0
			if err := db.Callback().Create().Replace("gorm:create", func(tx *gorm.DB) {
				row, ok := tx.Statement.Dest.(*ldapProviderV96)
				if !ok {
					t.Fatal("unowned migration write")
				}
				conflict, ok := tx.Statement.Clauses["ON CONFLICT"].Expression.(clause.OnConflict)
				if !ok || !conflict.DoNothing {
					t.Fatal("repeat may overwrite current singleton")
				}
				creates++
				if m.provider == nil {
					copy := *row
					m.provider = &copy
				}
				tx.RowsAffected = 1
			}); err != nil {
				t.Fatal(err)
			}
			err = ldapMigration(db)
			if (err != nil) != (fault != "") {
				t.Fatal("DDL failure not retained", fault, err)
			}
			m.fail = ""
			if err := ldapMigration(db); err != nil {
				t.Fatal("partial DDL did not resume", err)
			}
			if m.provider == nil || m.provider.ID != "ldap" || m.provider.Enabled || m.provider.IdentityAttribute != "" || m.provider.AuthCiphertext != "" || m.provider.ConfigRevision != strings.Repeat("0", 64) || m.provider.VerifiedBindingCreatedAt != nil || m.provider.VerifiedUserCreatedAt != nil {
				t.Fatal("default invents mode, secret or verifier")
			}
			m.provider.Name = "already saved"
			m.provider.ConfigRevision = strings.Repeat("a", 64)
			m.provider.IdentityAttribute = "entryUUID"
			saved := *m.provider
			opsBefore := len(m.ops)
			if err := ldapMigration(db); err != nil {
				t.Fatal("repeat failed", err)
			}
			if !reflect.DeepEqual(saved, *m.provider) {
				t.Fatal("repeat replaced saved singleton")
			}
			for _, op := range m.ops[opsBefore:] {
				if strings.HasPrefix(op, "column:") || strings.HasPrefix(op, "table:") || strings.HasPrefix(op, "index:") {
					t.Fatal("repeat recreated completed DDL", op)
				}
			}
			for _, table := range []string{"sessions", "mfa_challenges"} {
				for _, field := range []string{"LDAPBindingID", "LDAPBindingCreatedAt", "LDAPConfigRevision", "LDAPPolicyRevision", "LDAPUserCreatedAt"} {
					if !m.columns[table+":"+field] {
						t.Fatal("missing additive proof", table, field)
					}
				}
			}
			for _, name := range []string{"ck_sessions_oidc_primary", "ck_mfa_challenges_oidc_primary", "ck_secret_inventory_version", "ck_secret_rotation_domain", "ck_secret_item_domain", "ck_secret_process_inventory_version"} {
				if !m.checks[name] {
					t.Fatal("partial CHECK not restored", name)
				}
			}
			if !m.tables["ldap_providers"] || !m.tables["ldap_bindings"] || !m.indexes["idx_ldap_bindings_user_id"] || !m.indexes["idx_ldap_bindings_subject_digest"] || scans < 10 || creates < 2 {
				t.Fatal("finite full migration did not reach every proof phase")
			}
		})
	}
}
