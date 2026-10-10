package database

import (
	"database/sql"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
	"reflect"
	"sync"
	"testing"
)

func TestRuntimeInstallationV100FrozenSchema(t *testing.T) {
	frozen, err := schema.Parse(&runtimeInstallationV100{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	current, err := schema.Parse(&entity.RuntimeInstallationObservation{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if frozen.Table != "runtime_installation_observations" || len(frozen.Fields) != 8 || len(current.Fields) != 8 || len(frozen.Relationships.Relations) != 0 {
		t.Fatal("unfrozen observation schema")
	}
	for name, f := range frozen.FieldsByName {
		c := current.FieldsByName[name]
		if c == nil || f.DBName != c.DBName || f.FieldType != c.FieldType || f.Tag.Get("gorm") != c.Tag.Get("gorm") || f.AutoCreateTime != 0 || f.AutoUpdateTime != 0 || f.HasDefaultValue {
			t.Fatal("frozen field changed", name)
		}
	}
	if current.FieldsByName["SourceDigest"].Tag.Get("json") != "-" {
		t.Fatal("private digest became public")
	}
	indexes := frozen.ParseIndexes()
	if len(indexes) != 2 {
		t.Fatal("index set changed")
	}
	for _, idx := range indexes {
		want := []string{"instance_id", "first_observed_at"}
		class := ""
		if idx.Name == "idx_runtime_installation_source" {
			want = []string{"instance_id", "source_digest"}
			class = "UNIQUE"
		} else if idx.Name != "idx_runtime_installation_instance" {
			t.Fatal("unexpected index")
		}
		if idx.Class != class || len(idx.Fields) != 2 {
			t.Fatal("index shape changed")
		}
		for i, c := range idx.Fields {
			if c.DBName != want[i] {
				t.Fatal("index order changed")
			}
		}
	}
	for _, driver := range []string{"postgres", "mysql"} {
		steps := migrationSteps(driver)
		if len(steps) != 100 || reflect.ValueOf(steps[99]).Pointer() != reflect.ValueOf(runtimeInstallationMigration).Pointer() || reflect.ValueOf(steps[98]).Pointer() != reflect.ValueOf(googleIdentityMigration).Pointer() {
			t.Fatal("V100 suffix changed")
		}
	}
}

func TestRuntimeInstallationV100RejectsIncompatibleColumns(t *testing.T) {
	parsed, err := schema.Parse(&runtimeInstallationV100{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	baseline := []gorm.ColumnType{}
	for _, f := range parsed.Fields {
		c := oidcV94Column{name: f.DBName, kind: "varchar", size: int64(f.Size), sized: true, primary: f.PrimaryKey, primaryKnown: true, nullableKnown: true, precision: 6, precisionKnown: true}
		if string(f.DataType) == "time" {
			c.kind = "timestamp"
		}
		if string(f.DataType) == "int" {
			c.kind = "bigint"
		}
		baseline = append(baseline, c)
	}
	for _, which := range []string{"valid", "missing", "extra", "width", "type", "precision", "nullable", "default", "primary", "unknown_primary"} {
		t.Run(which, func(t *testing.T) {
			cols := append([]gorm.ColumnType(nil), baseline...)
			index := 0
			if which == "precision" {
				index = 2
			}
			c := cols[index].(oidcV94Column)
			switch which {
			case "missing":
				cols = cols[1:]
			case "extra":
				cols = append(cols, oidcV94Column{name: "unexpected"})
			case "width":
				c.size++
			case "type":
				c.kind = "text"
			case "precision":
				c.precision = 3
			case "nullable":
				c.nullable = true
			case "default":
				c.defaultKnown = true
				c.def = "'invented'"
			case "primary":
				c.primary = false
			case "unknown_primary":
				c.primaryKnown = false
			}
			if which != "missing" {
				cols[index] = c
			}
			m := &oidcV94ColumnsMigrator{columns: cols}
			// A metadata-only adapter never opens a network connection.
			db, err := gorm.Open(runtimeInstallationColumnsDialect{Dialector: postgres.New(postgres.Config{DSN: "host=localhost user=offline dbname=offline sslmode=disable"}), m: m}, &gorm.Config{DisableAutomaticPing: true})
			if err != nil {
				t.Fatal(err)
			}
			pool, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := pool.Close(); err != nil {
					t.Error(err)
				}
			})
			got := validateRuntimeInstallationColumnsV100(db)
			if (got == nil) != (which == "valid") {
				t.Fatal("column admission changed", which)
			}
		})
	}
}

type runtimeInstallationColumnsDialect struct {
	gorm.Dialector
	m *oidcV94ColumnsMigrator
}

func (d runtimeInstallationColumnsDialect) Migrator(*gorm.DB) gorm.Migrator { return d.m }

func TestRuntimeInstallationV100RecognizesOnlyKnownPartialIndex(t *testing.T) {
	known := credentialAttemptStatisticsIndexColumn{ColumnName: sql.NullString{String: "instance_id", Valid: true}, Position: sql.NullInt64{Int64: 1, Valid: true}, IsUnique: sql.NullBool{Bool: false, Valid: true}}
	for _, which := range []string{"known", "absent", "complete", "wrong_column", "unknown_column", "wrong_position", "unknown_position", "unique", "unknown_unique", "prefix"} {
		t.Run(which, func(t *testing.T) {
			c := known
			columns := []credentialAttemptStatisticsIndexColumn{c}
			switch which {
			case "absent":
				columns = nil
			case "complete":
				columns = append(columns, credentialAttemptStatisticsIndexColumn{ColumnName: sql.NullString{String: "first_observed_at", Valid: true}, Position: sql.NullInt64{Int64: 2, Valid: true}, IsUnique: sql.NullBool{Bool: false, Valid: true}})
			case "wrong_column":
				c.ColumnName.String = "first_observed_at"
			case "unknown_column":
				c.ColumnName.Valid = false
			case "wrong_position":
				c.Position.Int64 = 2
			case "unknown_position":
				c.Position.Valid = false
			case "unique":
				c.IsUnique.Bool = true
			case "unknown_unique":
				c.IsUnique.Valid = false
			case "prefix":
				c.PrefixLength = sql.NullInt64{Int64: 4, Valid: true}
			}
			if len(columns) == 1 {
				columns[0] = c
			}
			if runtimeInstallationPartialInstanceIndexV100(columns) != (which == "known") {
				t.Fatal("partial index admission changed", which)
			}
		})
	}
}
