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

func TestCredentialStorageV77FrozenSchemasAndNoSecretValueDomain(t *testing.T) {
	for _, pair := range [][2]any{{&credentialStoragePolicyV77{}, &entity.CredentialStoragePolicy{}}, {&credentialStorageOperationV77{}, &entity.CredentialStorageOperation{}}, {&credentialVaultReferenceV77{}, &entity.CredentialVaultReference{}}} {
		f, e := schema.Parse(pair[0], &sync.Map{}, schema.NamingStrategy{})
		if e != nil {
			t.Fatal(e)
		}
		c, e := schema.Parse(pair[1], &sync.Map{}, schema.NamingStrategy{})
		if e != nil {
			t.Fatal(e)
		}
		if f.Table != c.Table || len(f.Relationships.Relations) != 0 {
			t.Fatal("unbounded frozen migration")
		}
		for _, field := range f.Fields {
			got := c.LookUpField(field.Name)
			if got == nil || got.Tag != field.Tag || got.DBName != field.DBName || got.FieldType != field.FieldType {
				t.Fatal("frozen field drift", field.Name)
			}
		}
		for _, check := range f.ParseCheckConstraints() {
			if check.Name != "ck_credential_store_singleton" && (!strings.Contains(check.Constraint, "OCTET_LENGTH(") || !strings.Contains(check.Constraint, "ASCII(SUBSTRING(")) {
				t.Fatal("collation-dependent enum")
			}
		}
		if f.LookUpField("Ciphertext") != nil || f.LookUpField("Secret") != nil || f.LookUpField("ValueHash") != nil {
			t.Fatal("new secret/value-digest domain")
		}
	}
	frozen, e := schema.Parse(&credentialStorageSourceV77{}, &sync.Map{}, schema.NamingStrategy{})
	if e != nil {
		t.Fatal(e)
	}
	live, e := schema.Parse(&entity.ProviderCredential{}, &sync.Map{}, schema.NamingStrategy{})
	if e != nil {
		t.Fatal(e)
	}
	field := frozen.LookUpField("StorageSource")
	if frozen.Table != "provider_credentials" || len(frozen.Fields) != 1 || field.Tag.Get("gorm") != live.LookUpField("StorageSource").Tag.Get("gorm") || field.DefaultValue != "inline" {
		t.Fatal("legacy inline/default source drift")
	}
}

type credentialStorageColumn struct {
	roleRevisionColumn
	typ string
}

func (c credentialStorageColumn) DatabaseTypeName() string { return c.typ }

type credentialStorageMigrator struct{ *monthlyBehaviorMigrator }

func (m *credentialStorageMigrator) ColumnTypes(any) ([]gorm.ColumnType, error) {
	if m.fail == "columns" {
		return nil, errors.New("metadata unavailable")
	}
	if m.metadata != nil {
		return m.metadata, nil
	}
	return []gorm.ColumnType{credentialStorageColumn{roleRevisionColumn{name: "storage_source", size: 16, defaultValue: "inline"}, "varchar"}}, nil
}
func TestCredentialStorageV77PartialSourceDDLValidatesBeforeReplay(t *testing.T) {
	for _, fault := range []string{"", "column:StorageSource", "check:ck_provider_credentials_storage_source", "columns"} {
		t.Run(fault, func(t *testing.T) {
			m := &credentialStorageMigrator{&monthlyBehaviorMigrator{columns: map[string]bool{}, checks: map[string]bool{}, fail: fault}}
			if fault == "columns" {
				m.columns["StorageSource"] = true
			}
			db := &gorm.DB{Config: &gorm.Config{Dialector: monthlyBehaviorDialector{m: m.monthlyBehaviorMigrator}}}
			db.Dialector = credentialStorageDialector{m: m}
			db.Statement = &gorm.Statement{DB: db}
			e := credentialStorageSourceMigration(db)
			if (e != nil) != (fault != "") {
				t.Fatal("partial DDL failure swallowed", e)
			}
			m.fail = ""
			if e = credentialStorageSourceMigration(db); e != nil {
				t.Fatal(e)
			}
			before := slicesClone(m.ops)
			if e = credentialStorageSourceMigration(db); e != nil || !reflect.DeepEqual(before, m.ops) {
				t.Fatal("replay altered source schema", e)
			}
		})
	}
	for _, shape := range []credentialStorageColumn{{roleRevisionColumn{name: "storage_source", size: 10, defaultValue: "inline"}, "varchar"}, {roleRevisionColumn{name: "storage_source", size: 16, defaultValue: "vault"}, "varchar"}, {roleRevisionColumn{name: "storage_source", size: 16, defaultValue: "inline", nullable: true}, "varchar"}, {roleRevisionColumn{name: "storage_source", size: 16, defaultValue: "inline"}, "integer"}} {
		m := &credentialStorageMigrator{&monthlyBehaviorMigrator{columns: map[string]bool{"StorageSource": true}, checks: map[string]bool{}, metadata: []gorm.ColumnType{shape}}}
		db := &gorm.DB{Config: &gorm.Config{Dialector: credentialStorageDialector{m: m}}}
		db.Statement = &gorm.Statement{DB: db}
		if e := credentialStorageSourceMigration(db); e == nil || len(m.ops) != 0 {
			t.Fatal("unsafe partial column accepted")
		}
	}
}

type credentialStorageDialector struct {
	gorm.Dialector
	m *credentialStorageMigrator
}

func (d credentialStorageDialector) Migrator(*gorm.DB) gorm.Migrator { return d.m }
func slicesClone[T any](values []T) []T                              { return append([]T(nil), values...) }

func TestCredentialStorageV77AfterExactV75V76Predecessors(t *testing.T) {
	for _, dialect := range []string{"postgres", "mysql"} {
		steps := migrationSteps(dialect)
		if len(steps) != 92 || reflect.ValueOf(steps[91]).Pointer() != reflect.ValueOf(connectionTransportMigration).Pointer() || reflect.ValueOf(steps[90]).Pointer() != reflect.ValueOf(providerEnablementMigration).Pointer() || reflect.ValueOf(steps[89]).Pointer() != reflect.ValueOf(modelWeightHistoryMigration).Pointer() || reflect.ValueOf(steps[88]).Pointer() != reflect.ValueOf(credentialSourceClosedMigration).Pointer() || reflect.ValueOf(steps[83]).Pointer() != reflect.ValueOf(teamRollingQuotaWarningMigration).Pointer() || reflect.ValueOf(steps[82]).Pointer() != reflect.ValueOf(projectRollingQuotaWarningMigration).Pointer() || reflect.ValueOf(steps[81]).Pointer() != reflect.ValueOf(personalKeyRollingQuotaWarningMigration).Pointer() || reflect.ValueOf(steps[80]).Pointer() != reflect.ValueOf(personalRollingQuotaWarningMigration).Pointer() || reflect.ValueOf(steps[74]).Pointer() != reflect.ValueOf(teamMemberMonthlyBehaviorMigration).Pointer() || reflect.ValueOf(steps[75]).Pointer() != reflect.ValueOf(migrateConnectionEnablementV76).Pointer() || reflect.ValueOf(steps[76]).Pointer() != reflect.ValueOf(credentialStorageMigration).Pointer() || reflect.ValueOf(steps[84]).Pointer() != reflect.ValueOf(projectKeyRollingQuotaWarningMigration).Pointer() || reflect.ValueOf(steps[85]).Pointer() != reflect.ValueOf(modelRecordedMetadataMigration).Pointer() || reflect.ValueOf(steps[86]).Pointer() != reflect.ValueOf(runtimeApplicationMigration).Pointer() || reflect.ValueOf(steps[87]).Pointer() != reflect.ValueOf(credentialSourceDrainMigration).Pointer() {
			t.Fatal("V77 registered without exact V75/V76 predecessors", dialect)
		}
	}
}
