package database

import (
	"reflect"
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm/schema"
)

func TestAdmissionEconomicOriginV120FrozenSchema(t *testing.T) {
	frozen, current := oidcV94Schema(t, &admissionEconomicOriginV120{}), oidcV94Schema(t, &entity.AdmissionEconomicOrigin{})
	if frozen.Table != "admission_economic_origins" || current.Table != frozen.Table || !reflect.DeepEqual(frozen.DBNames, current.DBNames) || len(frozen.DBNames) != 10 {
		t.Fatal("origin frozen/current physical shape", frozen.DBNames, current.DBNames)
	}
	for _, name := range frozen.DBNames {
		a, b := frozen.FieldsByDBName[name], current.FieldsByDBName[name]
		if b == nil || a.FieldType != b.FieldType || a.Tag != b.Tag || a.HasDefaultValue || a.AutoIncrement || a.Tag.Get("json") != "-" {
			t.Fatal("origin scalar/default/public DTO drift", name)
		}
	}
	if len(frozen.PrimaryFields) != 1 || frozen.PrimaryFields[0].DBName != "installation_id" || len(frozen.ParseIndexes()) != 0 {
		t.Fatal("origin must have one bounded exact installation key")
	}
	if len(frozen.Relationships.Relations) != 1 {
		t.Fatal("origin must reference only its economic domain")
	}
	relation := frozen.Relationships.Relations["Domain"]
	fk := relation.ParseConstraint()
	if relation.Type != schema.BelongsTo || fk == nil || fk.Schema != frozen || fk.ReferenceSchema.Table != "admission_economic_domains" ||
		len(fk.ForeignKeys) != 1 || fk.ForeignKeys[0].DBName != "installation_id" || len(fk.References) != 1 || fk.References[0].DBName != "installation_id" ||
		fk.OnUpdate != "RESTRICT" || fk.OnDelete != "RESTRICT" {
		t.Fatal("origin restrictive domain FK", fk)
	}
	if len(fk.ReferenceSchema.DBNames) != 1 {
		t.Fatal("frozen FK stub must not own evolving predecessor fields")
	}
	admissionEconomicCheckColumnsV119(t, frozen)
	admissionEconomicIndexBoundsV119(t, frozen)
}

func TestAdmissionEconomicOriginV120FreshGuardsAndNoRegistration(t *testing.T) {
	s := oidcV94Schema(t, &admissionEconomicOriginV120{})
	checks := s.ParseCheckConstraints()
	if len(checks) != 7 {
		t.Fatal("origin must retain all seven bounded guards", len(checks))
	}
	for name, parts := range map[string][]string{
		"ck_economic_origin_source":   {"OCTET_LENGTH(source_kind) = 5", "ASCII(SUBSTRING(", "= 102", "= 104"},
		"ck_economic_origin_calendar": {"time_zone) BETWEEN 1 AND 100", "calendar_revision_at_freeze) BETWEEN 1 AND 64"},
		"ck_economic_origin_time":     {"coverage_start > 0", "9222767236854775807", "clock_high_water_at_freeze = coverage_start", "boundary_at = coverage_start"},
		"ck_economic_origin_body":     {"BETWEEN 1 AND 131072"},
	} {
		for _, part := range parts {
			if !strings.Contains(checks[name].Constraint, part) {
				t.Fatal("missing frozen origin guard", name, part)
			}
		}
	}
	if s.FieldsByDBName["calendar_revision_at_freeze"].Size != 64 || s.FieldsByDBName["time_zone"].Size != 100 || s.FieldsByDBName["evidence_body"].DataType != schema.Bytes {
		t.Fatal("origin revision/zone/body representations drifted")
	}
	for _, driver := range []string{"postgres", "mysql"} {
		for _, step := range migrationSteps(driver) {
			if reflect.ValueOf(step).Pointer() == reflect.ValueOf(admissionEconomicOriginMigration).Pointer() {
				t.Fatal("dormant origin candidate must not be registered", driver)
			}
		}
	}
}
