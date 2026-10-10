package database

import (
	"database/sql"
	"gorm.io/gorm"
	"gorm.io/gorm/migrator"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm/schema"
)

func TestAzureV79FrozenFieldsAndHistoricalPrefix(t *testing.T) {
	frozen, e := schema.Parse(&azureConnectionV79{}, &sync.Map{}, schema.NamingStrategy{})
	if e != nil {
		t.Fatal(e)
	}
	current, e := schema.Parse(&entity.ProviderConnection{}, &sync.Map{}, schema.NamingStrategy{})
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"Adapter", "APIVersion"} {
		a, b := frozen.FieldsByName[name], current.FieldsByName[name]
		if a.DBName != b.DBName || a.Size != b.Size || a.NotNull != b.NotNull || a.HasDefaultValue != b.HasDefaultValue || a.DefaultValue != b.DefaultValue || a.TagSettings["CHECK"] != b.TagSettings["CHECK"] {
			t.Fatal("frozen/current transport mismatch", name)
		}
	}
	credential, e := schema.Parse(&azureCredentialV79{}, &sync.Map{}, schema.NamingStrategy{})
	if e != nil {
		t.Fatal(e)
	}
	currentCredential, e := schema.Parse(&entity.ProviderCredential{}, &sync.Map{}, schema.NamingStrategy{})
	if e != nil {
		t.Fatal(e)
	}
	x, y := credential.FieldsByName["CoverageRevision"], currentCredential.FieldsByName["CoverageRevision"]
	if x.FieldType != y.FieldType || x.TagSettings["CHECK"] != y.TagSettings["CHECK"] || x.DefaultValue != "0" || !x.NotNull {
		t.Fatal("frozen coverage revision")
	}
	for _, name := range []string{"CoverageReviewETag", "CoverageIntentSHA256"} {
		f, c := credential.FieldsByName[name], currentCredential.FieldsByName[name]
		if f.DBName != c.DBName || f.Size != c.Size || !f.NotNull || !f.HasDefaultValue || f.DefaultValue != "" || f.Tag.Get("gorm") != c.Tag.Get("gorm") {
			t.Fatal("frozen retained review", name)
		}
	}
	a, e := schema.Parse(&credentialDeploymentAttestationV79{}, &sync.Map{}, schema.NamingStrategy{})
	if e != nil {
		t.Fatal(e)
	}
	b, e := schema.Parse(&entity.CredentialDeploymentAttestation{}, &sync.Map{}, schema.NamingStrategy{})
	if e != nil {
		t.Fatal(e)
	}
	if a.Table != b.Table || len(a.Fields) != 3 || len(b.Fields) != 3 {
		t.Fatal("relation shape")
	}
	for _, f := range a.Fields {
		g := b.FieldsByName[f.Name]
		if f.Tag != g.Tag || f.FieldType != g.FieldType {
			t.Fatal("attestation frozen mismatch", f.Name)
		}
	}
	for _, dialect := range []string{"postgres", "mysql"} {
		steps := migrationSteps(dialect)
		if len(steps) != 97 || reflect.ValueOf(steps[96]).Pointer() != reflect.ValueOf(samlMigration).Pointer() || reflect.ValueOf(steps[95]).Pointer() != reflect.ValueOf(ldapMigration).Pointer() || reflect.ValueOf(steps[94]).Pointer() != reflect.ValueOf(oauthMigration).Pointer() || reflect.ValueOf(steps[93]).Pointer() != reflect.ValueOf(oidcMigration).Pointer() || reflect.ValueOf(steps[92]).Pointer() != reflect.ValueOf(credentialAttemptStatisticsMigration).Pointer() || reflect.ValueOf(steps[91]).Pointer() != reflect.ValueOf(connectionTransportMigration).Pointer() || reflect.ValueOf(steps[90]).Pointer() != reflect.ValueOf(providerEnablementMigration).Pointer() || reflect.ValueOf(steps[89]).Pointer() != reflect.ValueOf(modelWeightHistoryMigration).Pointer() || reflect.ValueOf(steps[88]).Pointer() != reflect.ValueOf(credentialSourceClosedMigration).Pointer() || reflect.ValueOf(steps[83]).Pointer() != reflect.ValueOf(teamRollingQuotaWarningMigration).Pointer() || reflect.ValueOf(steps[82]).Pointer() != reflect.ValueOf(projectRollingQuotaWarningMigration).Pointer() || reflect.ValueOf(steps[81]).Pointer() != reflect.ValueOf(personalKeyRollingQuotaWarningMigration).Pointer() || reflect.ValueOf(steps[80]).Pointer() != reflect.ValueOf(personalRollingQuotaWarningMigration).Pointer() || reflect.ValueOf(steps[77]).Pointer() != reflect.ValueOf(vaultAppRoleMigration).Pointer() || reflect.ValueOf(steps[78]).Pointer() != reflect.ValueOf(azureDeploymentMigration).Pointer() || reflect.ValueOf(steps[84]).Pointer() != reflect.ValueOf(projectKeyRollingQuotaWarningMigration).Pointer() || reflect.ValueOf(steps[85]).Pointer() != reflect.ValueOf(modelRecordedMetadataMigration).Pointer() || reflect.ValueOf(steps[86]).Pointer() != reflect.ValueOf(runtimeApplicationMigration).Pointer() || reflect.ValueOf(steps[87]).Pointer() != reflect.ValueOf(credentialSourceDrainMigration).Pointer() {
			t.Fatal("V78/V79 ordered append changed")
		}
	}
}

func TestAzureV79CoverageColumnsMatchCompleteSetWrite(t *testing.T) {
	for _, model := range []any{&azureCredentialV79{}, &entity.ProviderCredential{}} {
		parsed, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		for field, column := range map[string]string{"CoverageRevision": "coverage_revision", "CoverageReviewETag": "coverage_review_etag", "CoverageIntentSHA256": "coverage_intent_sha256"} {
			actual := parsed.FieldsByName[field]
			if actual == nil || actual.DBName != column || parsed.LookUpField(column) != actual {
				t.Fatalf("%T %s maps to %q, cannot read/write canonical complete-set column %q", model, field, actual.DBName, column)
			}
		}
	}
}

// Metadata names come from the same parsed frozen GORM schema used by AddColumn,
// rather than assuming Go initialisms map to the validator's literal columns.
type azureV79ColumnMigrator struct {
	gorm.Migrator
	columns map[string]bool
	checks  map[string]bool
	adds    int
	fault   func([]gorm.ColumnType) []gorm.ColumnType
}

func (m *azureV79ColumnMigrator) HasColumn(model any, field string) bool {
	return m.columns[reflect.TypeOf(model).String()+field]
}
func (m *azureV79ColumnMigrator) AddColumn(model any, field string) error {
	m.columns[reflect.TypeOf(model).String()+field] = true
	m.adds++
	return nil
}
func (m *azureV79ColumnMigrator) HasConstraint(_ any, name string) bool { return m.checks[name] }
func (m *azureV79ColumnMigrator) CreateConstraint(_ any, name string) error {
	m.checks[name] = true
	return nil
}
func (m *azureV79ColumnMigrator) HasTable(any) bool { return true }
func (m *azureV79ColumnMigrator) ColumnTypes(model any) ([]gorm.ColumnType, error) {
	parsed, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		return nil, err
	}
	columns := make([]gorm.ColumnType, 0, len(parsed.Fields))
	for _, f := range parsed.Fields {
		kind := "varchar"
		if f.DataType == schema.Int {
			kind = "bigint"
		}
		columns = append(columns, migrator.ColumnType{
			NameValue:         sql.NullString{String: f.DBName, Valid: true},
			DataTypeValue:     sql.NullString{String: kind, Valid: true},
			LengthValue:       sql.NullInt64{Int64: int64(f.Size), Valid: true},
			NullableValue:     sql.NullBool{Bool: !f.NotNull, Valid: true},
			DefaultValueValue: sql.NullString{String: f.DefaultValue, Valid: f.HasDefaultValue},
		})
	}
	if m.fault != nil && parsed.Table == "provider_credentials" {
		columns = m.fault(columns)
	}
	return columns, nil
}

type azureV79ColumnDialect struct {
	gorm.Dialector
	m *azureV79ColumnMigrator
}

func (d azureV79ColumnDialect) Name() string                    { return "fixture" }
func (d azureV79ColumnDialect) Initialize(*gorm.DB) error       { return nil }
func (d azureV79ColumnDialect) Migrator(*gorm.DB) gorm.Migrator { return d.m }
func TestAzureV79ParsedColumnReplayRejectsUnsafeMetadata(t *testing.T) {
	for _, fault := range []string{"", "legacy_initialism", "wrong_width", "nullable", "no_default", "wrong_default", "wrong_type", "missing_revision", "missing_intent"} {
		t.Run(fault, func(t *testing.T) {
			m := &azureV79ColumnMigrator{columns: map[string]bool{}, checks: map[string]bool{}}
			m.fault = func(columns []gorm.ColumnType) []gorm.ColumnType {
				out := make([]gorm.ColumnType, 0, len(columns))
				for _, column := range columns {
					c := column.(migrator.ColumnType)
					if fault == "missing_revision" && c.Name() == "coverage_revision" || fault == "missing_intent" && c.Name() == "coverage_intent_sha256" {
						continue
					}
					if c.Name() == "coverage_review_etag" {
						switch fault {
						case "legacy_initialism":
							c.NameValue.String = "coverage_review_e_tag"
						case "wrong_width":
							c.LengthValue.Int64 = 128
						case "nullable":
							c.NullableValue.Bool = true
						case "no_default":
							c.DefaultValueValue.Valid = false
						case "wrong_default":
							c.DefaultValueValue.String = "unexpected"
						case "wrong_type":
							c.DataTypeValue.String = "text"
						}
					}
					out = append(out, c)
				}
				return out
			}
			db, err := gorm.Open(azureV79ColumnDialect{m: m}, &gorm.Config{DisableAutomaticPing: true})
			if err != nil {
				t.Fatal(err)
			}
			err = azureDeploymentMigration(db)
			if fault != "" {
				if err == nil || !strings.Contains(err.Error(), "unexpected credential coverage revision column") || m.checks["ck_credential_coverage_revision_v79"] {
					t.Fatal("unsafe partial schema accepted", err)
				}
				return
			}
			if err != nil || m.adds != 5 || !m.checks["ck_connection_adapter_v79"] || !m.checks["ck_credential_coverage_revision_v79"] {
				t.Fatal("canonical GORM columns rejected", err, m.adds)
			}
			if err = azureDeploymentMigration(db); err != nil || m.adds != 5 {
				t.Fatal("repeat changed schema", err, m.adds)
			}
		})
	}
}
