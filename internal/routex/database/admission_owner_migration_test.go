package database

import (
	"reflect"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm/schema"
)

func TestAdmissionOwnerV118FrozenSchema(t *testing.T) {
	for _, pair := range []struct {
		frozen, current any
		table           string
		columns         int
	}{
		{&admissionModeV118{}, &entity.AdmissionMode{}, "admission_modes", 4},
		{&admissionSpoolOwnerV118{}, &entity.AdmissionSpoolOwner{}, "admission_spool_owners", 12},
	} {
		a, b := oidcV94Schema(t, pair.frozen), oidcV94Schema(t, pair.current)
		if a.Table != pair.table || b.Table != pair.table || len(a.DBNames) != pair.columns || len(b.DBNames) != pair.columns {
			t.Fatal("owner schema shape", pair.table)
		}
		for _, name := range a.DBNames {
			x, y := a.FieldsByDBName[name], b.FieldsByDBName[name]
			if y == nil || x.FieldType != y.FieldType || x.Tag.Get("gorm") != y.Tag.Get("gorm") || x.Tag.Get("json") != "-" || y.Tag.Get("json") != "-" {
				t.Fatal("frozen scalar drift", name)
			}
		}
		for _, relation := range a.Relationships.Relations {
			c := relation.ParseConstraint()
			if relation.Type != schema.BelongsTo || c == nil || c.Schema != a || c.OnDelete != "RESTRICT" || c.OnUpdate != "RESTRICT" || len(c.ForeignKeys) != 1 || len(c.References) != 1 {
				t.Fatal("owner reference direction", pair.table)
			}
		}
	}
	mode := oidcV94Schema(t, &admissionModeV118{})
	if mode.FieldsByDBName["mode"].DefaultValue != "local" || mode.FieldsByDBName["revision"].DefaultValue != "1" || mode.Relationships.Relations["Installation"].FieldSchema.Table != "limit_installation" {
		t.Fatal("dormant local defaults or installation mapping changed")
	}
	// Preserve trailing bytes so the exact-value CHECK can reject padded aliases.
	if mode.FieldsByDBName["mode"].Size != 16 {
		t.Fatal("mode width must retain padding for exact-value validation")
	}
	owner := oidcV94Schema(t, &admissionSpoolOwnerV118{})
	if owner.Relationships.Relations["Mode"].FieldSchema.Table != "admission_modes" || len(owner.Relationships.Relations) != 1 {
		t.Fatal("process metadata must not be retained by a synthetic closure FK")
	}
	for _, driver := range []string{"postgres", "mysql"} {
		steps := migrationSteps(driver)
		if len(steps) != 118 || reflect.ValueOf(steps[115]).Pointer() != reflect.ValueOf(runtimeArchiveRollbackMigration).Pointer() || reflect.ValueOf(steps[116]).Pointer() != reflect.ValueOf(runtimePublicationAcknowledgementMigration).Pointer() || reflect.ValueOf(steps[117]).Pointer() != reflect.ValueOf(admissionOwnerMigration).Pointer() {
			t.Fatal("V118 must append without replacing V116/V117")
		}
	}
}

func TestAdmissionOwnerV118JournalIdentity(t *testing.T) {
	for _, value := range []string{"", "installation", "led_01jjjjjjjjjjjjjjjjjjjjjjjj", "LED_01jjjjjjjjjjjjjjjjjjjjjjjj", "led_81jjjjjjjjjjjjjjjjjjjjjjjj", "led_01iiiiiiiiiiiiiiiiiiiiiiii"} {
		want := value == "led_01jjjjjjjjjjjjjjjjjjjjjjjj"
		if admissionJournalV118.MatchString(value) != want {
			t.Fatal("journal identity boundary", value)
		}
	}
}
