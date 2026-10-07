package database

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestVaultFrozenSchemasAndRetainedReferences(t *testing.T) {
	pairs := [][2]any{{&vaultCatalogueV72{}, &entity.VaultCatalogue{}}, {&vaultIntegrationV72{}, &entity.VaultIntegration{}}, {&vaultRevisionV72{}, &entity.VaultRevision{}}, {&vaultWriterAuthV72{}, &entity.VaultWriterAuth{}}, {&vaultReaderAuthV72{}, &entity.VaultReaderAuth{}}, {&vaultConfigReceiptV72{}, &entity.VaultConfigReceipt{}}, {&vaultProbeV72{}, &entity.VaultProbe{}}, {&vaultProbeCommandV72{}, &entity.VaultProbeCommand{}}}
	for _, pair := range pairs {
		frozen, e := schema.Parse(pair[0], &sync.Map{}, schema.NamingStrategy{})
		if e != nil {
			t.Fatal(e)
		}
		current, e := schema.Parse(pair[1], &sync.Map{}, schema.NamingStrategy{})
		if e != nil {
			t.Fatal(e)
		}
		extra := 0
		if frozen.Table == "vault_writer_auth" || frozen.Table == "vault_reader_auth" {
			extra = 1 // V78 is additive; every historical V72 field remains exact.
			method := current.LookUpField("Method")
			if method == nil || method.DBName != "method" || method.DefaultValue != "token" {
				t.Fatal("missing explicit V78 discriminator")
			}
		}
		if frozen.Table != current.Table || len(frozen.Fields)+extra != len(current.Fields) || len(frozen.Relationships.Relations) != 0 {
			t.Fatal("unbounded migration", frozen.Table)
		}
		for _, field := range frozen.Fields {
			got := current.LookUpField(field.Name)
			if got == nil || got.Tag != field.Tag || got.DBName != field.DBName || got.FieldType != field.FieldType {
				t.Fatal("schema mismatch", field.Name)
			}
		}
		if len(frozen.PrimaryFields) != 1 {
			t.Fatal("missing exact primary identity", frozen.Table)
		}
	}
	for _, m := range []any{&vaultRevisionLinksV72{}, &vaultWriterLinksV72{}, &vaultReaderLinksV72{}, &vaultReceiptLinksV72{}, &vaultProbeLinksV72{}, &vaultCommandLinksV72{}} {
		parsed, e := schema.Parse(m, &sync.Map{}, schema.NamingStrategy{})
		if e != nil {
			t.Fatal(e)
		}
		if len(parsed.Relationships.Relations) == 0 {
			t.Fatal("missing retained foreign reference")
		}
		for _, rel := range parsed.Relationships.Relations {
			c := rel.ParseConstraint()
			if c == nil || c.OnDelete != "RESTRICT" || c.Schema != parsed {
				t.Fatal("unsafe owned reference", rel.Name)
			}
		}
	}
	if vaultIntegrationVersion != 72 || reflect.ValueOf(vaultIntegrationMigration).IsNil() {
		t.Fatal("missing reserved step")
	}
}
func TestVaultProbeChecksAreExactAcrossCollation(t *testing.T) {
	for _, m := range []any{&vaultProbeV72{}, &vaultProbeCommandV72{}} {
		parsed, e := schema.Parse(m, &sync.Map{}, schema.NamingStrategy{})
		if e != nil {
			t.Fatal(e)
		}
		name := "ck_vault_probe_state"
		if parsed.Table == "vault_probe_commands" {
			name = "ck_vault_command_kind"
		}
		guard := parsed.ParseCheckConstraints()[name].Constraint
		if !strings.Contains(guard, "OCTET_LENGTH(") || !strings.Contains(guard, "ASCII(SUBSTRING(") || strings.Contains(guard, " IN (") {
			t.Fatal("case/padding alias permitted")
		}
	}
	job, e := schema.Parse(&vaultRootJobV72{}, &sync.Map{}, schema.NamingStrategy{})
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(job.ParseCheckConstraints()["ck_secret_rotation_domain"].Constraint, "inventory_version = 1 AND domain >= 0 AND domain <= 5") || !strings.Contains(job.ParseCheckConstraints()["ck_secret_rotation_domain"].Constraint, "inventory_version = 2 AND domain >= 0 AND domain <= 7") {
		t.Fatal("old sentinel reinterpreted")
	}
}

type vaultInventoryMigrator struct {
	gorm.Migrator
	exists  bool
	fail    string
	ops     []string
	columns []gorm.ColumnType
}

func (m *vaultInventoryMigrator) HasColumn(any, string) bool { return m.exists }
func (m *vaultInventoryMigrator) AddColumn(any, string) error {
	m.ops = append(m.ops, "add")
	if m.fail == "add" {
		return errors.New("DDL failure")
	}
	m.exists = true
	return nil
}
func (m *vaultInventoryMigrator) ColumnTypes(any) ([]gorm.ColumnType, error) {
	if m.fail == "columns" {
		return nil, errors.New("metadata unavailable")
	}
	if m.columns != nil {
		return m.columns, nil
	}
	return []gorm.ColumnType{roleRevisionColumn{name: "inventory_version", defaultValue: "1"}}, nil
}
func (m *vaultInventoryMigrator) AlterColumn(any, string) error {
	m.ops = append(m.ops, "alter")
	if m.fail == "alter" {
		return errors.New("DDL failure")
	}
	m.columns = nil
	return nil
}

type vaultInventoryDialector struct {
	gorm.Dialector
	m *vaultInventoryMigrator
}

func (d vaultInventoryDialector) Migrator(*gorm.DB) gorm.Migrator { return d.m }
func TestVaultInventoryPartialDDLFailuresAndExactRepeat(t *testing.T) {
	for _, fail := range []string{"", "add", "columns"} {
		t.Run(fail, func(t *testing.T) {
			m := &vaultInventoryMigrator{fail: fail}
			db := &gorm.DB{Config: &gorm.Config{Dialector: vaultInventoryDialector{m: m}}}
			db.Statement = &gorm.Statement{DB: db}
			e := vaultInventoryColumnV72(db, &vaultRootJobV72{})
			if (e != nil) != (fail != "") {
				t.Fatal("failure hidden", e)
			}
			m.fail = ""
			if e = vaultInventoryColumnV72(db, &vaultRootJobV72{}); e != nil {
				t.Fatal(e)
			}
			before := append([]string(nil), m.ops...)
			if e = vaultInventoryColumnV72(db, &vaultRootJobV72{}); e != nil || !reflect.DeepEqual(before, m.ops) {
				t.Fatal("repeat changed stable metadata", e)
			}
		})
	}
}
