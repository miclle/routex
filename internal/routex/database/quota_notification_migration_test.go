package database

import (
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

func TestFrozenQuotaNotificationSchema(t *testing.T) {
	for _, model := range []any{&quotaNotificationObservationV35{}, &quotaNotificationInboxV35{}} {
		parsed, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		if len(parsed.Relationships.Relations) != 0 || len(parsed.PrimaryFields) != 1 || parsed.PrimaryFields[0].DBName != "id" {
			t.Fatal("quota records must have stable IDs without live FK")
		}
		for _, field := range parsed.Fields {
			if field.DBName != "id" && field.DBName != "read_at" && !field.NotNull {
				t.Fatalf("snapshot column %s nullable", field.DBName)
			}
		}
		if parsed.Table == "quota_notification_observations" {
			constraints := parsed.ParseCheckConstraints()
			for _, name := range []string{"ck_quota_notification_scope", "ck_quota_notification_dimension", "ck_quota_notification_calendar", "ck_quota_notification_currency"} {
				if _, ok := constraints[name]; !ok {
					t.Fatal("missing frozen check", name)
				}
			}
			indexes := parsed.ParseIndexes()
			if len(indexes) != 1 || indexes[0].Name != "uq_quota_notification_observation" || indexes[0].Class != "UNIQUE" || len(indexes[0].Fields) != 6 {
				t.Fatal("missing six-field source dedup key")
			}
			expected := []string{"scope_kind", "scope_id", "dimension", "month_start", "policy_revision", "currency"}
			for i, field := range indexes[0].Fields {
				if field.DBName != expected[i] {
					t.Fatal("source dedup key order changed")
				}
			}
			if parsed.FieldsByDBName["settled_value"].Size != 80 || parsed.FieldsByDBName["limit_value"].Size != 40 {
				t.Fatal("exact decimal snapshot bounds changed")
			}
		} else if parsed.Table != "quota_notification_inboxes" || len(parsed.ParseIndexes()) != 2 {
			t.Fatal("missing recipient indexes")
		}
	}
}
