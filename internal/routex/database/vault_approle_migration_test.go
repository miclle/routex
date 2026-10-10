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

func TestVaultAppRoleV78FrozenExactMethods(t *testing.T) {
	for _, pair := range [][2]any{{&vaultWriterMethodV78{}, &entity.VaultWriterAuth{}}, {&vaultReaderMethodV78{}, &entity.VaultReaderAuth{}}} {
		f, e := schema.Parse(pair[0], &sync.Map{}, schema.NamingStrategy{})
		if e != nil {
			t.Fatal(e)
		}
		c, e := schema.Parse(pair[1], &sync.Map{}, schema.NamingStrategy{})
		if e != nil {
			t.Fatal(e)
		}
		method := f.LookUpField("Method")
		if len(f.Fields) != 1 || f.Table != c.Table || method.Tag != c.LookUpField("Method").Tag || method.DefaultValue != "token" || !method.NotNull || len(f.Relationships.Relations) != 0 {
			t.Fatal("unbounded auth migration")
		}
		for _, check := range f.ParseCheckConstraints() {
			if !strings.Contains(check.Constraint, "OCTET_LENGTH(method)") || !strings.Contains(check.Constraint, "ASCII(SUBSTRING(method,") || strings.Contains(check.Constraint, " IN (") {
				t.Fatal("collation alias")
			}
		}
	}
	for _, dialect := range []string{"postgres", "mysql"} {
		steps := migrationSteps(dialect)
		if len(steps) != 101 || reflect.ValueOf(steps[96]).Pointer() != reflect.ValueOf(samlMigration).Pointer() || reflect.ValueOf(steps[95]).Pointer() != reflect.ValueOf(ldapMigration).Pointer() || reflect.ValueOf(steps[94]).Pointer() != reflect.ValueOf(oauthMigration).Pointer() || reflect.ValueOf(steps[93]).Pointer() != reflect.ValueOf(oidcMigration).Pointer() || reflect.ValueOf(steps[92]).Pointer() != reflect.ValueOf(credentialAttemptStatisticsMigration).Pointer() || reflect.ValueOf(steps[91]).Pointer() != reflect.ValueOf(connectionTransportMigration).Pointer() || reflect.ValueOf(steps[90]).Pointer() != reflect.ValueOf(providerEnablementMigration).Pointer() || reflect.ValueOf(steps[89]).Pointer() != reflect.ValueOf(modelWeightHistoryMigration).Pointer() || reflect.ValueOf(steps[88]).Pointer() != reflect.ValueOf(credentialSourceClosedMigration).Pointer() || reflect.ValueOf(steps[83]).Pointer() != reflect.ValueOf(teamRollingQuotaWarningMigration).Pointer() || reflect.ValueOf(steps[82]).Pointer() != reflect.ValueOf(projectRollingQuotaWarningMigration).Pointer() || reflect.ValueOf(steps[81]).Pointer() != reflect.ValueOf(personalKeyRollingQuotaWarningMigration).Pointer() || reflect.ValueOf(steps[80]).Pointer() != reflect.ValueOf(personalRollingQuotaWarningMigration).Pointer() || reflect.ValueOf(steps[77]).Pointer() != reflect.ValueOf(vaultAppRoleMigration).Pointer() || reflect.ValueOf(steps[84]).Pointer() != reflect.ValueOf(projectKeyRollingQuotaWarningMigration).Pointer() || reflect.ValueOf(steps[85]).Pointer() != reflect.ValueOf(modelRecordedMetadataMigration).Pointer() || reflect.ValueOf(steps[86]).Pointer() != reflect.ValueOf(runtimeApplicationMigration).Pointer() || reflect.ValueOf(steps[87]).Pointer() != reflect.ValueOf(credentialSourceDrainMigration).Pointer() || reflect.ValueOf(steps[97]).Pointer() != reflect.ValueOf(namedIdentityMigration).Pointer() || reflect.ValueOf(steps[98]).Pointer() != reflect.ValueOf(googleIdentityMigration).Pointer() || reflect.ValueOf(steps[99]).Pointer() != reflect.ValueOf(runtimeInstallationMigration).Pointer() || reflect.ValueOf(steps[100]).Pointer() != reflect.ValueOf(discordIdentityMigration).Pointer() {
			t.Fatal("V78 not registered after the exact current prefix")
		}
	}
}

type vaultMethodMigrator struct {
	*monthlyBehaviorMigrator
	methods map[string]bool
}

func (m *vaultMethodMigrator) HasColumn(model any, name string) bool {
	if m.methods == nil {
		return m.monthlyBehaviorMigrator.HasColumn(model, name)
	}
	return m.methods[model.(interface{ TableName() string }).TableName()]
}
func (m *vaultMethodMigrator) AddColumn(model any, name string) error {
	if m.methods == nil {
		return m.monthlyBehaviorMigrator.AddColumn(model, name)
	}
	table := model.(interface{ TableName() string }).TableName()
	m.ops = append(m.ops, "column:"+table)
	m.methods[table] = true
	return nil
}

func (m *vaultMethodMigrator) ColumnTypes(any) ([]gorm.ColumnType, error) {
	if m.fail == "columns" {
		return nil, errors.New("metadata unavailable")
	}
	if m.metadata != nil {
		return m.metadata, nil
	}
	return []gorm.ColumnType{credentialStorageColumn{roleRevisionColumn{name: "method", size: 16, defaultValue: "token"}, "varchar"}}, nil
}

type vaultMethodDialector struct {
	gorm.Dialector
	m *vaultMethodMigrator
}

func (d vaultMethodDialector) Migrator(*gorm.DB) gorm.Migrator { return d.m }
func vaultMethodDB(m *vaultMethodMigrator) *gorm.DB {
	db := &gorm.DB{Config: &gorm.Config{Dialector: vaultMethodDialector{m: m}}}
	db.Statement = &gorm.Statement{DB: db}
	return db
}
func TestVaultAppRoleV78PartialDDLIsValidatedAndResumable(t *testing.T) {
	for _, pair := range []struct {
		model any
		check string
	}{{&vaultWriterMethodV78{}, "ck_vault_writer_method"}, {&vaultReaderMethodV78{}, "ck_vault_reader_method"}} {
		for _, fault := range []string{"", "column:Method", "check:" + pair.check, "columns"} {
			t.Run(pair.check+fault, func(t *testing.T) {
				m := &vaultMethodMigrator{monthlyBehaviorMigrator: &monthlyBehaviorMigrator{columns: map[string]bool{}, checks: map[string]bool{}, fail: fault}}
				if fault == "columns" {
					m.columns["Method"] = true
				}
				e := vaultAuthMethodMigration(vaultMethodDB(m), pair.model, pair.check)
				if (e != nil) != (fault != "") {
					t.Fatal("DDL failure swallowed", e)
				}
				m.fail = ""
				if e = vaultAuthMethodMigration(vaultMethodDB(m), pair.model, pair.check); e != nil {
					t.Fatal(e)
				}
				before := slicesClone(m.ops)
				if e = vaultAuthMethodMigration(vaultMethodDB(m), pair.model, pair.check); e != nil || !reflect.DeepEqual(before, m.ops) {
					t.Fatal("repeat changed schema", e)
				}
			})
		}
	}
	for _, shape := range []credentialStorageColumn{{roleRevisionColumn{name: "method", size: 10, defaultValue: "token"}, "varchar"}, {roleRevisionColumn{name: "method", size: 16, defaultValue: "approle"}, "varchar"}, {roleRevisionColumn{name: "method", size: 16, defaultValue: "token", nullable: true}, "varchar"}, {roleRevisionColumn{name: "method", size: 16, defaultValue: "token"}, "integer"}} {
		m := &vaultMethodMigrator{monthlyBehaviorMigrator: &monthlyBehaviorMigrator{columns: map[string]bool{"Method": true}, checks: map[string]bool{}, metadata: []gorm.ColumnType{shape}}}
		if e := vaultAuthMethodMigration(vaultMethodDB(m), &vaultWriterMethodV78{}, "ck_vault_writer_method"); e == nil || len(m.ops) != 0 {
			t.Fatal("unsafe partial method column accepted")
		}
	}
}

func TestVaultAppRoleV78ResumesAfterWriterDDLWithoutRewritingAuth(t *testing.T) {
	m := &vaultMethodMigrator{monthlyBehaviorMigrator: &monthlyBehaviorMigrator{columns: map[string]bool{}, checks: map[string]bool{}, fail: "check:ck_vault_reader_method"}, methods: map[string]bool{}}
	if e := vaultAppRoleMigration(vaultMethodDB(m)); e == nil || !m.methods["vault_writer_auth"] || !m.methods["vault_reader_auth"] || !m.checks["ck_vault_writer_method"] {
		t.Fatal("two-domain partial DDL not exercised")
	}
	m.fail = ""
	if e := vaultAppRoleMigration(vaultMethodDB(m)); e != nil {
		t.Fatal(e)
	}
	before := slicesClone(m.ops)
	if e := vaultAppRoleMigration(vaultMethodDB(m)); e != nil || !reflect.DeepEqual(before, m.ops) {
		t.Fatal("replay changed completed auth schema", e)
	}
	if !m.checks["ck_vault_reader_method"] {
		t.Fatal("missing resumed reader constraint")
	}
	for _, op := range m.ops {
		if !strings.HasPrefix(op, "column:") && !strings.HasPrefix(op, "check:") {
			t.Fatal("migration touched auth material")
		}
	}
}
