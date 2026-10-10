package database

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"unicode"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

func oidcV94Schema(t *testing.T, model any) *schema.Schema {
	t.Helper()
	s, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Historical primary columns predate the complete named-identity CHECK. Admit
// only the exact current frozen proof tag and the exact retained base tag.
func legacyUpgradeGORMTagsEqual(t *testing.T, table string, frozen, current *schema.Field) bool {
	t.Helper()
	if frozen == nil || current == nil {
		return false
	}
	if frozen.DBName != "primary_method" {
		return frozen.Tag.Get("gorm") == current.Tag.Get("gorm")
	}
	var model any
	switch table {
	case "sessions":
		model = &discordIdentitySessionProofV101{}
	case "mfa_challenges":
		model = &discordIdentityMFAChallengeProofV101{}
	default:
		return false
	}
	expected := oidcV94Schema(t, model).FieldsByDBName["primary_method"]
	return expected != nil && frozen.FieldType == expected.FieldType && current.FieldType == expected.FieldType && current.DBName == expected.DBName && frozen.Tag.Get("gorm") == "size:20;not null;default:''" && current.Tag.Get("gorm") == expected.Tag.Get("gorm")
}

func TestLegacyPrimaryParityRequiresExactCurrentProof(t *testing.T) {
	for _, model := range []any{&entity.Session{}, &entity.MFAChallenge{}} {
		s := oidcV94Schema(t, model)
		current := s.FieldsByDBName["primary_method"]
		if current == nil {
			t.Fatal("missing current primary column", s.Table)
		}
		frozen := *current
		frozen.Tag = reflect.StructTag(`gorm:"size:20;not null;default:''"`)
		if !legacyUpgradeGORMTagsEqual(t, s.Table, &frozen, current) {
			t.Fatal("exact current proof and historical base rejected", s.Table)
		}
		for _, tag := range []string{
			"size:20;not null;default:''",
			current.Tag.Get("gorm") + ";check:ck_unreviewed,1 = 1",
			strings.Replace(current.Tag.Get("gorm"), "size:20", "size:21", 1),
			strings.Replace(current.Tag.Get("gorm"), "default:''", "default:'github'", 1),
			strings.Replace(current.Tag.Get("gorm"), "OCTET_LENGTH(primary_method)", "CHAR_LENGTH(primary_method)", 1),
		} {
			changed := *current
			changed.Tag = reflect.StructTag(fmt.Sprintf("gorm:%q", tag))
			if tag == current.Tag.Get("gorm") || legacyUpgradeGORMTagsEqual(t, s.Table, &frozen, &changed) {
				t.Fatal("unreviewed current proof admitted", s.Table)
			}
		}
		changed := frozen
		changed.Tag = reflect.StructTag(`gorm:"size:21;not null;default:''"`)
		if legacyUpgradeGORMTagsEqual(t, s.Table, &changed, current) || legacyUpgradeGORMTagsEqual(t, "unreviewed", &frozen, current) {
			t.Fatal("historical base or table changed", s.Table)
		}
	}
}

// Literal V94 entity projection captured before V95 advances current root tags.
type oidcHistoricalEntityRootJobV94 struct {
	InventoryVersion int `gorm:"not null;default:1;check:ck_secret_inventory_version,inventory_version = 1 OR inventory_version = 2 OR inventory_version = 3"`
	Domain           int `gorm:"not null;check:ck_secret_rotation_domain,(inventory_version = 1 AND domain >= 0 AND domain <= 5) OR (inventory_version = 2 AND domain >= 0 AND domain <= 7) OR (inventory_version = 3 AND domain >= 0 AND domain <= 8)"`
}

func (oidcHistoricalEntityRootJobV94) TableName() string { return "secret_rotation_jobs" }

type oidcHistoricalEntityRootItemV94 struct {
	Domain string `gorm:"primaryKey;size:32;check:ck_secret_item_domain,((((CHAR_LENGTH(domain) = 20 AND ASCII(SUBSTRING(domain,1,1)) = 112 AND ASCII(SUBSTRING(domain,2,1)) = 114 AND ASCII(SUBSTRING(domain,3,1)) = 111 AND ASCII(SUBSTRING(domain,4,1)) = 118 AND ASCII(SUBSTRING(domain,5,1)) = 105 AND ASCII(SUBSTRING(domain,6,1)) = 100 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 114 AND ASCII(SUBSTRING(domain,9,1)) = 95 AND ASCII(SUBSTRING(domain,10,1)) = 99 AND ASCII(SUBSTRING(domain,11,1)) = 114 AND ASCII(SUBSTRING(domain,12,1)) = 101 AND ASCII(SUBSTRING(domain,13,1)) = 100 AND ASCII(SUBSTRING(domain,14,1)) = 101 AND ASCII(SUBSTRING(domain,15,1)) = 110 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 105 AND ASCII(SUBSTRING(domain,18,1)) = 97 AND ASCII(SUBSTRING(domain,19,1)) = 108 AND ASCII(SUBSTRING(domain,20,1)) = 115) OR (CHAR_LENGTH(domain) = 8 AND ASCII(SUBSTRING(domain,1,1)) = 101 AND ASCII(SUBSTRING(domain,2,1)) = 103 AND ASCII(SUBSTRING(domain,3,1)) = 114 AND ASCII(SUBSTRING(domain,4,1)) = 101 AND ASCII(SUBSTRING(domain,5,1)) = 115 AND ASCII(SUBSTRING(domain,6,1)) = 115 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 115) OR (CHAR_LENGTH(domain) = 13 AND ASCII(SUBSTRING(domain,1,1)) = 115 AND ASCII(SUBSTRING(domain,2,1)) = 109 AND ASCII(SUBSTRING(domain,3,1)) = 116 AND ASCII(SUBSTRING(domain,4,1)) = 112 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 115 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 116 AND ASCII(SUBSTRING(domain,9,1)) = 116 AND ASCII(SUBSTRING(domain,10,1)) = 105 AND ASCII(SUBSTRING(domain,11,1)) = 110 AND ASCII(SUBSTRING(domain,12,1)) = 103 AND ASCII(SUBSTRING(domain,13,1)) = 115) OR (CHAR_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 115 AND ASCII(SUBSTRING(domain,2,1)) = 116 AND ASCII(SUBSTRING(domain,3,1)) = 111 AND ASCII(SUBSTRING(domain,4,1)) = 114 AND ASCII(SUBSTRING(domain,5,1)) = 97 AND ASCII(SUBSTRING(domain,6,1)) = 103 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 95 AND ASCII(SUBSTRING(domain,9,1)) = 114 AND ASCII(SUBSTRING(domain,10,1)) = 101 AND ASCII(SUBSTRING(domain,11,1)) = 118 AND ASCII(SUBSTRING(domain,12,1)) = 105 AND ASCII(SUBSTRING(domain,13,1)) = 115 AND ASCII(SUBSTRING(domain,14,1)) = 105 AND ASCII(SUBSTRING(domain,15,1)) = 111 AND ASCII(SUBSTRING(domain,16,1)) = 110 AND ASCII(SUBSTRING(domain,17,1)) = 115) OR (CHAR_LENGTH(domain) = 8 AND ASCII(SUBSTRING(domain,1,1)) = 117 AND ASCII(SUBSTRING(domain,2,1)) = 115 AND ASCII(SUBSTRING(domain,3,1)) = 101 AND ASCII(SUBSTRING(domain,4,1)) = 114 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 109 AND ASCII(SUBSTRING(domain,7,1)) = 102 AND ASCII(SUBSTRING(domain,8,1)) = 97)) OR (OCTET_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 118 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 117 AND ASCII(SUBSTRING(domain,4,1)) = 108 AND ASCII(SUBSTRING(domain,5,1)) = 116 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 119 AND ASCII(SUBSTRING(domain,8,1)) = 114 AND ASCII(SUBSTRING(domain,9,1)) = 105 AND ASCII(SUBSTRING(domain,10,1)) = 116 AND ASCII(SUBSTRING(domain,11,1)) = 101 AND ASCII(SUBSTRING(domain,12,1)) = 114 AND ASCII(SUBSTRING(domain,13,1)) = 95 AND ASCII(SUBSTRING(domain,14,1)) = 97 AND ASCII(SUBSTRING(domain,15,1)) = 117 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 104) OR (OCTET_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 118 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 117 AND ASCII(SUBSTRING(domain,4,1)) = 108 AND ASCII(SUBSTRING(domain,5,1)) = 116 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 114 AND ASCII(SUBSTRING(domain,8,1)) = 101 AND ASCII(SUBSTRING(domain,9,1)) = 97 AND ASCII(SUBSTRING(domain,10,1)) = 100 AND ASCII(SUBSTRING(domain,11,1)) = 101 AND ASCII(SUBSTRING(domain,12,1)) = 114 AND ASCII(SUBSTRING(domain,13,1)) = 95 AND ASCII(SUBSTRING(domain,14,1)) = 97 AND ASCII(SUBSTRING(domain,15,1)) = 117 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 104))) OR (OCTET_LENGTH(domain) = 14 AND ASCII(SUBSTRING(domain,1,1)) = 111 AND ASCII(SUBSTRING(domain,2,1)) = 105 AND ASCII(SUBSTRING(domain,3,1)) = 100 AND ASCII(SUBSTRING(domain,4,1)) = 99 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 112 AND ASCII(SUBSTRING(domain,7,1)) = 114 AND ASCII(SUBSTRING(domain,8,1)) = 111 AND ASCII(SUBSTRING(domain,9,1)) = 118 AND ASCII(SUBSTRING(domain,10,1)) = 105 AND ASCII(SUBSTRING(domain,11,1)) = 100 AND ASCII(SUBSTRING(domain,12,1)) = 101 AND ASCII(SUBSTRING(domain,13,1)) = 114 AND ASCII(SUBSTRING(domain,14,1)) = 115)"`
}

func (oidcHistoricalEntityRootItemV94) TableName() string { return "secret_rotation_items" }

type oidcHistoricalEntityRootProcessV94 struct {
	InventoryVersion int `gorm:"not null;default:1;check:ck_secret_process_inventory_version,inventory_version = 1 OR inventory_version = 2 OR inventory_version = 3"`
}

func (oidcHistoricalEntityRootProcessV94) TableName() string { return "secret_process_verifications" }

func TestOIDCV94FrozenSchemaParity(t *testing.T) {
	for _, pair := range [][2]any{
		{&oidcProviderV94{}, &entity.OIDCProvider{}},
		{&oidcBindingV94{}, &entity.OIDCBinding{}},
		{&oidcCeremonyV94{}, &entity.OIDCCeremony{}},
	} {
		frozen, current := oidcV94Schema(t, pair[0]), oidcV94Schema(t, pair[1])
		if frozen.Table != current.Table || len(frozen.Fields) != len(current.Fields) || len(frozen.Relationships.Relations) != 0 || len(current.Relationships.Relations) != 0 {
			t.Fatal("unbounded frozen schema", frozen.Table)
		}
		for _, field := range frozen.Fields {
			actual := current.FieldsByDBName[field.DBName]
			if actual == nil || actual.FieldType != field.FieldType || actual.Tag.Get("gorm") != field.Tag.Get("gorm") {
				t.Fatal("frozen field differs", frozen.Table, field.DBName)
			}
		}
	}
	for _, pair := range [][2]any{
		{&oidcSessionV94{}, &entity.Session{}},
		{&oidcMFAChallengeV94{}, &entity.MFAChallenge{}},
		{&oidcRootJobV94{}, &oidcHistoricalEntityRootJobV94{}},
		{&oidcRootItemV94{}, &oidcHistoricalEntityRootItemV94{}},
		{&oidcRootProcessV94{}, &oidcHistoricalEntityRootProcessV94{}},
	} {
		frozen, current := oidcV94Schema(t, pair[0]), oidcV94Schema(t, pair[1])
		if frozen.Table != current.Table || len(frozen.Relationships.Relations) != 0 {
			t.Fatal("invalid bounded upgrade projection", frozen.Table)
		}
		for _, field := range frozen.Fields {
			actual := current.FieldsByDBName[field.DBName]
			if actual == nil || actual.FieldType != field.FieldType || !legacyUpgradeGORMTagsEqual(t, frozen.Table, field, actual) {
				t.Fatal("upgrade field differs", frozen.Table, field.DBName)
			}
		}
	}
}

func TestOIDCV94FrozenProvenanceDefaultsAndIndexes(t *testing.T) {
	// Hard-coded physical names are independent of schema parity and mock
	// metadata. GORM's ID initialism rewrite splits OIDC unless explicitly tagged.
	for _, model := range []any{&oidcSessionV94{}, &oidcMFAChallengeV94{}, &entity.Session{}, &entity.MFAChallenge{}} {
		s := oidcV94Schema(t, model)
		for name, column := range map[string]string{
			"PrimaryMethod":        "primary_method",
			"OIDCBindingID":        "oidc_binding_id",
			"OIDCBindingCreatedAt": "oidc_binding_created_at",
			"OIDCConfigRevision":   "oidc_config_revision",
			"OIDCPolicyRevision":   "oidc_policy_revision",
			"OIDCUserCreatedAt":    "oidc_user_created_at",
		} {
			field := s.FieldsByName[name]
			if field == nil || field.DBName != column || s.FieldsByDBName[column] != field {
				t.Fatal("primary proof physical column differs from constraints and reads", s.Table, name, column)
			}
			if name != "PrimaryMethod" && field.TagSettings["COLUMN"] != column {
				t.Fatal("OIDC physical column must not depend on initialism inference", s.Table, name)
			}
		}
	}
	for _, model := range []any{&oidcSessionV94{}, &oidcMFAChallengeV94{}} {
		s := oidcV94Schema(t, model)
		if len(s.Fields) != 6 {
			t.Fatal("unreviewed primary provenance", s.Table)
		}
		for _, field := range s.Fields {
			if field.FieldType.Kind() == reflect.Pointer {
				if field.NotNull || field.HasDefaultValue || field.Precision != 6 || field.AutoCreateTime != 0 || field.AutoUpdateTime != 0 {
					t.Fatal("unknown local provenance acquired an invented birth", s.Table, field.DBName)
				}
			} else if !field.NotNull || !field.HasDefaultValue || field.DefaultValue != "" {
				t.Fatal("legacy local primary no longer has blank defaults", s.Table, field.DBName)
			}
		}
	}
	for _, item := range []struct {
		model  any
		fields []string
	}{
		{&oidcProviderV94{}, []string{"CreatedAt", "UpdatedAt"}},
		{&oidcBindingV94{}, []string{"CreatedAt", "UserCreatedAt"}},
		{&oidcCeremonyV94{}, []string{"CreatedAt", "ExpiresAt"}},
	} {
		s := oidcV94Schema(t, item.model)
		for _, name := range item.fields {
			field := s.FieldsByName[name]
			if field == nil || !field.NotNull || field.Precision != 6 {
				t.Fatal("missing exact durable timestamp", s.Table, name)
			}
		}
	}
	reason := oidcV94Schema(t, &oidcCeremonyV94{}).FieldsByName["Reason"]
	if reason == nil || reason.Size != 1024 || !reason.NotNull {
		t.Fatal("ceremony reason is not retained and bounded")
	}
	for _, item := range []struct {
		model    any
		unique   []string
		ordinary []string
	}{
		{&oidcBindingV94{}, []string{"user_id", "subject_digest"}, nil},
		{&oidcCeremonyV94{}, []string{"state_hash", "cookie_hash"}, []string{"expires_at"}},
	} {
		s := oidcV94Schema(t, item.model)
		for _, group := range []struct {
			fields []string
			unique bool
		}{{item.unique, true}, {item.ordinary, false}} {
			for _, name := range group.fields {
				found := 0
				for _, index := range s.ParseIndexes() {
					if len(index.Fields) == 1 && index.Fields[0].DBName == name && (index.Class == "UNIQUE") == group.unique {
						found++
					}
				}
				if found != 1 {
					t.Fatal("missing exact independent index", s.Table, name, found)
				}
			}
		}
	}
}

type oidcV94ColumnMetadata = gorm.ColumnType

type oidcV94Column struct {
	oidcV94ColumnMetadata
	name, kind, def                                                                     string
	size, precision                                                                     int64
	nullable, nullableKnown, sized, precisionKnown, defaultKnown, primary, primaryKnown bool
}

func (c oidcV94Column) Name() string                      { return c.name }
func (c oidcV94Column) PrimaryKey() (bool, bool)          { return c.primary, c.primaryKnown }
func (c oidcV94Column) DatabaseTypeName() string          { return c.kind }
func (c oidcV94Column) Length() (int64, bool)             { return c.size, c.sized }
func (c oidcV94Column) DecimalSize() (int64, int64, bool) { return c.precision, 0, c.precisionKnown }
func (c oidcV94Column) Nullable() (bool, bool)            { return c.nullable, c.nullableKnown }
func (c oidcV94Column) DefaultValue() (string, bool)      { return c.def, c.defaultKnown }

type oidcV94ColumnsMigrator struct {
	gorm.Migrator
	columns []gorm.ColumnType
	err     error
}

func (m *oidcV94ColumnsMigrator) ColumnTypes(any) ([]gorm.ColumnType, error) { return m.columns, m.err }

func oidcV94Columns(t *testing.T, model any) []gorm.ColumnType {
	t.Helper()
	var columns []gorm.ColumnType
	for _, field := range oidcV94Schema(t, model).Fields {
		c := oidcV94Column{name: field.DBName, primary: field.PrimaryKey, primaryKnown: true, nullableKnown: true, nullable: field.FieldType.Kind() == reflect.Pointer, size: int64(field.Size), sized: true, precision: 6, precisionKnown: true, defaultKnown: field.HasDefaultValue, def: field.DefaultValue}
		switch string(field.DataType) {
		case "string":
			c.kind = "varchar"
		case "text":
			c.kind = "text"
		case "bool":
			c.kind = "boolean"
		case "time":
			c.kind = "timestamp"
		default:
			t.Fatal("unreviewed frozen type", field.DBName, field.DataType)
		}
		columns = append(columns, c)
	}
	return columns
}

func TestOIDCV94PartialColumnValidation(t *testing.T) {
	models := []any{&oidcProviderV94{}, &oidcBindingV94{}, &oidcCeremonyV94{}, &oidcSessionV94{}, &oidcMFAChallengeV94{}}
	for _, model := range models {
		s := oidcV94Schema(t, model)
		for _, field := range s.Fields {
			faults := []string{"missing", "unknown_nullability"}
			if field.NotNull || field.PrimaryKey {
				faults = append(faults, "nullable")
			}
			if field.PrimaryKey {
				faults = append(faults, "missing_primary", "unknown_primary")
			}
			if field.FieldType.Kind() == reflect.Pointer {
				faults = append(faults, "not_nullable")
			}
			switch string(field.DataType) {
			case "time":
				faults = append(faults, "precision3", "unknown_precision", "wrong_kind", "invented_time_default")
			case "bool", "text":
				faults = append(faults, "wrong_kind")
			case "string":
				faults = append(faults, "wrong_kind", "wrong_size", "unknown_size")
			}
			if field.HasDefaultValue {
				faults = append(faults, "missing_default", "changed_default")
			}
			for _, fault := range faults {
				t.Run(s.Table+"/"+field.DBName+"/"+fault, func(t *testing.T) {
					m := &oidcV94ColumnsMigrator{columns: oidcV94Columns(t, model)}
					db := projectRateCheckDB(t, m)
					if err := validateOIDCColumnsV94(db, model); err != nil {
						t.Fatal("invalid positive metadata baseline", err)
					}
					for i, column := range m.columns {
						c := column.(oidcV94Column)
						if c.name != field.DBName {
							continue
						}
						switch fault {
						case "missing":
							m.columns = append(m.columns[:i], m.columns[i+1:]...)
						case "unknown_nullability":
							c.nullableKnown = false
						case "nullable":
							c.nullable = true
						case "not_nullable":
							c.nullable = false
						case "invented_time_default":
							c.defaultKnown = true
							c.def = "CURRENT_TIMESTAMP"
						case "missing_primary":
							c.primary = false
						case "unknown_primary":
							c.primaryKnown = false
						case "precision3":
							c.precision = 3
						case "unknown_precision":
							c.precisionKnown = false
						case "wrong_kind":
							c.kind = "integer"
						case "wrong_size":
							c.size--
						case "unknown_size":
							c.sized = false
						case "missing_default":
							c.defaultKnown = false
						case "changed_default":
							c.def = "oidc"
						}
						if fault != "missing" {
							m.columns[i] = c
						}
						break
					}
					if err := validateOIDCColumnsV94(db, model); err == nil || !strings.Contains(err.Error(), field.DBName) {
						t.Fatal("incompatible partial column was not identified", field.DBName, fault, err)
					}
				})
			}
		}
		m := &oidcV94ColumnsMigrator{err: errors.New("OIDC metadata unavailable")}
		if err := validateOIDCColumnsV94(projectRateCheckDB(t, m), model); !errors.Is(err, m.err) {
			t.Fatal("lost metadata failure", err)
		}
	}
}

func TestOIDCV94PortableTimestampAndDefaultMetadata(t *testing.T) {
	for _, kind := range []string{"timestamp", "timestamptz", "datetime"} {
		t.Run(kind, func(t *testing.T) {
			m := &oidcV94ColumnsMigrator{columns: oidcV94Columns(t, &oidcCeremonyV94{})}
			for i, value := range m.columns {
				c := value.(oidcV94Column)
				if c.kind == "timestamp" {
					c.kind = kind
					m.columns[i] = c
				}
			}
			if err := validateOIDCColumnsV94(projectRateCheckDB(t, m), &oidcCeremonyV94{}); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, def := range []string{"NULL", "null"} {
		m := &oidcV94ColumnsMigrator{columns: oidcV94Columns(t, &oidcCeremonyV94{})}
		for i, value := range m.columns {
			c := value.(oidcV94Column)
			if c.kind == "timestamp" {
				c.defaultKnown = true
				c.def = def
				m.columns[i] = c
			}
		}
		if err := validateOIDCColumnsV94(projectRateCheckDB(t, m), &oidcCeremonyV94{}); err != nil {
			t.Fatal("SQL NULL is not an invented timestamp", def, err)
		}
	}
	for _, def := range []string{"", "''", "''::character varying"} {
		m := &oidcV94ColumnsMigrator{columns: oidcV94Columns(t, &oidcSessionV94{})}
		for i, value := range m.columns {
			c := value.(oidcV94Column)
			if c.defaultKnown {
				c.def = def
				m.columns[i] = c
			}
		}
		if err := validateOIDCColumnsV94(projectRateCheckDB(t, m), &oidcSessionV94{}); err != nil {
			t.Fatal("portable blank default rejected", def, err)
		}
	}
}

func oidcV94Check(t *testing.T, model any, name string) string {
	t.Helper()
	check, ok := oidcV94Schema(t, model).ParseCheckConstraints()[name]
	if !ok {
		t.Fatal("missing frozen check", name)
	}
	return check.Constraint
}

func oidcV94ASCII(column, value string) string {
	parts := []string{fmt.Sprintf("OCTET_LENGTH(%s) = %d", column, len(value))}
	for i := range len(value) {
		parts = append(parts, fmt.Sprintf("ASCII(SUBSTRING(%s,%d,1)) = %d", column, i+1, value[i]))
	}
	return strings.Join(parts, " AND ")
}

func TestOIDCV94RootChecksKeepV1V2AndAddOnlyV3(t *testing.T) {
	inventory := "inventory_version = 1 OR inventory_version = 2 OR inventory_version = 3"
	scope := "(inventory_version = 1 AND domain >= 0 AND domain <= 5) OR (inventory_version = 2 AND domain >= 0 AND domain <= 7) OR (inventory_version = 3 AND domain >= 0 AND domain <= 8)"
	if oidcV94Check(t, &oidcRootJobV94{}, "ck_secret_inventory_version") != inventory || oidcV94Check(t, &oidcRootJobV94{}, "ck_secret_rotation_domain") != scope || oidcV94Check(t, &oidcRootProcessV94{}, "ck_secret_process_inventory_version") != inventory {
		t.Fatal("current inventory scope lost a historical boundary or acquired a future version")
	}
	if oidcV94Check(t, &secretRotationJobV48{}, "ck_secret_rotation_domain") != "domain >= 0 AND domain <= 5" || oidcV94Check(t, &vaultRootJobV72{}, "ck_secret_rotation_domain") != "(inventory_version = 1 AND domain >= 0 AND domain <= 5) OR (inventory_version = 2 AND domain >= 0 AND domain <= 7)" || oidcV94Check(t, &vaultRootJobV72{}, "ck_secret_inventory_version") != "inventory_version = 1 OR inventory_version = 2" || oidcV94Check(t, &vaultRootProcessV72{}, "ck_secret_process_inventory_version") != "inventory_version = 1 OR inventory_version = 2" {
		t.Fatal("released inventory constraints were rewritten")
	}
	for _, model := range []any{&oidcRootJobV94{}, &oidcRootProcessV94{}} {
		field := oidcV94Schema(t, model).FieldsByName["InventoryVersion"]
		if !field.NotNull || !field.HasDefaultValue || field.DefaultValue != "1" {
			t.Fatal("retained inventory no longer defaults to historical V1")
		}
	}
	// Both expressions are pure disjunctions of exact domain names. Preserve every
	// old term, including case-sensitive ASCII guards, and append only this domain.
	normalize := func(v string) string {
		return strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) || r == '(' || r == ')' {
				return -1
			}
			return r
		}, v)
	}
	prior := oidcV94Check(t, &vaultRootItemV72{}, "ck_secret_item_domain")
	current := oidcV94Check(t, &oidcRootItemV94{}, "ck_secret_item_domain")
	if normalize(current) != normalize(prior+" OR "+oidcV94ASCII("domain", "oidc_providers")) {
		t.Fatal("item domain expanded beyond the exact retained set")
	}
}

func TestOIDCV94PrimaryConstraintsPreserveLocalAndExactOIDC(t *testing.T) {
	primary := strings.ReplaceAll(oidcV94ASCII("primary_method", "oidc"), "OCTET_LENGTH", "CHAR_LENGTH")
	expected := "(primary_method = '' AND oidc_binding_id = '' AND oidc_binding_created_at IS NULL AND oidc_config_revision = '' AND oidc_policy_revision = '' AND oidc_user_created_at IS NULL) OR (" + primary + " AND CHAR_LENGTH(oidc_binding_id) > 0 AND oidc_binding_created_at IS NOT NULL AND CHAR_LENGTH(oidc_config_revision) = 64 AND CHAR_LENGTH(oidc_policy_revision) = 64 AND oidc_user_created_at IS NOT NULL)"
	for _, item := range []struct {
		model any
		name  string
	}{
		{&oidcSessionProofV94{}, "ck_sessions_oidc_primary"},
		{&oidcMFAProofV94{}, "ck_mfa_challenges_oidc_primary"},
	} {
		if oidcV94Check(t, item.model, item.name) != expected {
			t.Fatal("local or exact OIDC primary shape changed", item.name)
		}
	}
	singleton := strings.ReplaceAll(oidcV94ASCII("id", "oidc"), "OCTET_LENGTH", "CHAR_LENGTH")
	if oidcV94Check(t, &oidcProviderV94{}, "ck_oidc_singleton") != singleton {
		t.Fatal("singleton admits a collation alias")
	}
}

type oidcV94RootMigrator struct {
	gorm.Migrator
	checks map[string]bool
	ops    []string
	fail   string
}

func (m *oidcV94RootMigrator) HasConstraint(_ any, name string) bool { return m.checks[name] }
func (m *oidcV94RootMigrator) DropConstraint(_ any, name string) error {
	m.ops = append(m.ops, "drop:"+name)
	if m.fail == "drop:"+name {
		return errors.New(m.fail)
	}
	m.checks[name] = false
	return nil
}
func (m *oidcV94RootMigrator) CreateConstraint(model any, name string) error {
	if _, ok := model.(*oidcRootJobV94); !ok {
		if _, ok := model.(*oidcRootItemV94); !ok {
			if _, ok := model.(*oidcRootProcessV94); !ok {
				panic("unowned root constraint")
			}
		}
	}
	m.ops = append(m.ops, "create:"+name)
	if m.fail == "create:"+name {
		return errors.New(m.fail)
	}
	m.checks[name] = true
	return nil
}

func TestOIDCV94RootRetainedScopeAndInterruptedChecks(t *testing.T) {
	names := []string{"ck_secret_inventory_version", "ck_secret_rotation_domain", "ck_secret_item_domain", "ck_secret_process_inventory_version"}
	faults := []string{"", "bad_jobs", "bad_proofs", "query_jobs", "query_proofs"}
	for _, name := range names {
		faults = append(faults, "drop:"+name, "create:"+name)
	}
	for _, fault := range faults {
		label := fault
		if label == "" {
			label = "valid"
		}
		t.Run(label, func(t *testing.T) {
			m := &oidcV94RootMigrator{checks: map[string]bool{}, fail: fault}
			for _, name := range names {
				m.checks[name] = true
			}
			db := projectRateCheckDB(t, m)
			queries := 0
			if err := db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
				queries++
				where, ok := tx.Statement.Clauses["WHERE"].Expression.(clause.Where)
				if !ok || len(where.Exprs) != 1 {
					t.Fatal("unbounded retained-scope predicate")
				}
				expr, ok := where.Exprs[0].(clause.Expr)
				if !ok {
					t.Fatal("unexpected retained-scope expression")
				}
				var expectedSQL string
				var expectedVars []any
				var suffix string
				switch tx.Statement.Table {
				case "secret_rotation_jobs":
					expectedSQL = "inventory_version NOT IN ? OR domain < 0 OR (inventory_version = ? AND domain > ?) OR (inventory_version = ? AND domain > ?) OR domain > ?"
					expectedVars = []any{[]int{1, 2, 3}, 1, 5, 2, 7, 8}
					suffix = "jobs"
				case "secret_process_verifications":
					expectedSQL = "inventory_version NOT IN ? OR inventory_version IS NULL"
					expectedVars = []any{[]int{1, 2, 3}}
					suffix = "proofs"
				default:
					t.Fatal("unowned retained query", tx.Statement.Table)
				}
				if expr.SQL != expectedSQL || !reflect.DeepEqual(expr.Vars, expectedVars) {
					t.Fatal("retained historical scope was weakened", expr)
				}
				if m.fail == "query_"+suffix {
					_ = tx.AddError(errors.New(m.fail))
					return
				}
				count, ok := tx.Statement.Dest.(*int64)
				if !ok {
					t.Fatal("unexpected retained query destination")
				}
				tx.RowsAffected = 1 // COUNT returns one aggregate row even when its value is zero.
				*count = 0
				if m.fail == "bad_"+suffix {
					*count = 1
				}
			}); err != nil {
				t.Fatal(err)
			}
			err := oidcRootInventoryV94(db)
			if (err != nil) != (fault != "") {
				t.Fatal("retained/DDL fault was lost", fault, err)
			}
			if strings.HasPrefix(fault, "bad_") || strings.HasPrefix(fault, "query_") {
				if len(m.ops) != 0 {
					t.Fatal("constraint changed before positive retained proof", m.ops)
				}
			}
			m.fail = ""
			for range 2 {
				if err := oidcRootInventoryV94(db); err != nil {
					t.Fatal("interrupted constraint replacement did not resume", err)
				}
				for _, name := range names {
					if !m.checks[name] {
						t.Fatal("missing resumed constraint", name)
					}
				}
			}
			if queries < 4 {
				t.Fatal("repeat skipped fresh retained-scope validation", queries)
			}
		})
	}
}
