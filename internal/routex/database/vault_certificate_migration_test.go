package database

import (
	"errors"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestVaultCertificateV104FrozenCurrentAndHistoricalBoundary(t *testing.T) {
	for _, pair := range [][2]any{{&vaultWriterMethodV104{}, &entity.VaultWriterAuth{}}, {&vaultReaderMethodV104{}, &entity.VaultReaderAuth{}}} {
		frozen, err := schema.Parse(pair[0], &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		current, err := schema.Parse(pair[1], &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		f, c := frozen.LookUpField("Method"), current.LookUpField("Method")
		if len(frozen.Fields) != 1 || frozen.Table != current.Table || f.Tag != c.Tag || f.Size != 16 || !f.NotNull || f.DefaultValue != "token" {
			t.Fatal("frozen current method mismatch")
		}
		for _, check := range frozen.ParseCheckConstraints() {
			if !strings.Contains(check.Constraint, "OCTET_LENGTH(method) = 11") || !strings.Contains(check.Constraint, "OCTET_LENGTH(method) = 5") || !strings.Contains(check.Constraint, "OCTET_LENGTH(method) = 7") || strings.Contains(check.Constraint, "method = '") {
				t.Fatal("method check weakened")
			}
		}
	}
	for _, old := range []any{&vaultWriterMethodV78{}, &vaultReaderMethodV78{}} {
		frozen, err := schema.Parse(old, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		for _, check := range frozen.ParseCheckConstraints() {
			if strings.Contains(check.Constraint, "OCTET_LENGTH(method) = 11") {
				t.Fatal("V78 changed")
			}
		}
	}
}

type certificateDDLMigrator struct {
	gorm.Migrator
	checks map[string]bool
	fail   string
	ops    []string
}

func (m *certificateDDLMigrator) HasConstraint(_ any, name string) bool { return m.checks[name] }
func (m *certificateDDLMigrator) DropConstraint(_ any, name string) error {
	m.ops = append(m.ops, "drop:"+name)
	if m.fail == "drop:"+name {
		return errors.New("owned DDL interruption")
	}
	delete(m.checks, name)
	return nil
}
func (m *certificateDDLMigrator) CreateConstraint(_ any, name string) error {
	m.ops = append(m.ops, "create:"+name)
	if m.fail == "create:"+name {
		return errors.New("owned DDL interruption")
	}
	m.checks[name] = true
	return nil
}

type certificateDDLDialector struct {
	gorm.Dialector
	m *certificateDDLMigrator
}

func (d certificateDDLDialector) Migrator(*gorm.DB) gorm.Migrator { return d.m }
func TestVaultCertificateV104PartialCheckDDLResumesWithoutSwallowingErrors(t *testing.T) {
	for _, failure := range []string{"drop:ck_vault_writer_method", "create:ck_vault_writer_method", "drop:ck_vault_reader_method", "create:ck_vault_reader_method"} {
		t.Run(failure, func(t *testing.T) {
			m := &certificateDDLMigrator{checks: map[string]bool{"ck_vault_writer_method": true, "ck_vault_reader_method": true}, fail: failure}
			db := &gorm.DB{Config: &gorm.Config{Dialector: certificateDDLDialector{m: m}}}
			db.Statement = &gorm.Statement{DB: db}
			if vaultCertificateReplaceChecks(db, vaultCertificateChecks()) == nil {
				t.Fatal("interruption swallowed")
			}
			m.fail = ""
			if err := vaultCertificateReplaceChecks(db, vaultCertificateChecks()); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(m.checks, map[string]bool{"ck_vault_writer_method": true, "ck_vault_reader_method": true}) {
				t.Fatal("partial DDL not restored")
			}
		})
	}
}
func TestVaultCertificateV104RejectsMissingMethodBeforeDDL(t *testing.T) {
	m := &vaultMethodMigrator{monthlyBehaviorMigrator: &monthlyBehaviorMigrator{columns: map[string]bool{}, checks: map[string]bool{}}, methods: map[string]bool{}}
	if vaultCertificateColumn(vaultMethodDB(m), &vaultWriterMethodV104{}) == nil || len(m.ops) != 0 {
		t.Fatal("missing schema silently synthesized")
	}
}

func TestVaultCertificateV104RejectsIncompatibleRetainedMethodMetadata(t *testing.T) {
	for _, column := range []credentialStorageColumn{
		{roleRevisionColumn{name: "method", size: 10, defaultValue: "token"}, "varchar"},
		{roleRevisionColumn{name: "method", size: 16, defaultValue: "approle"}, "varchar"},
		{roleRevisionColumn{name: "method", size: 16, defaultValue: "token", nullable: true}, "varchar"},
		{roleRevisionColumn{name: "method", size: 16, defaultValue: "token"}, "integer"},
	} {
		m := &vaultMethodMigrator{monthlyBehaviorMigrator: &monthlyBehaviorMigrator{columns: map[string]bool{}, checks: map[string]bool{}, metadata: []gorm.ColumnType{column}}, methods: map[string]bool{"vault_writer_auth": true}}
		if vaultCertificateColumn(vaultMethodDB(m), &vaultWriterMethodV104{}) == nil || len(m.ops) != 0 {
			t.Fatal("incompatible retained column accepted or changed")
		}
	}
	m := &vaultMethodMigrator{monthlyBehaviorMigrator: &monthlyBehaviorMigrator{columns: map[string]bool{}, checks: map[string]bool{}, fail: "columns"}, methods: map[string]bool{"vault_writer_auth": true}}
	if vaultCertificateColumn(vaultMethodDB(m), &vaultWriterMethodV104{}) == nil || len(m.ops) != 0 {
		t.Fatal("metadata failure ignored")
	}
}
