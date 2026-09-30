package database

import (
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

func TestFrozenNotificationForeignKeyDirections(t *testing.T) {
	tests := []struct {
		model          any
		relation       string
		table          string
		referenceTable string
		foreignKey     string
	}{
		{&operationalAlertOccurrenceV29{}, "Alert", "operational_alert_occurrences", "operational_alerts", "alert_id"},
		{&notificationV29{}, "Recipient", "notifications", "users", "recipient_id"},
		{&notificationV29{}, "Alert", "notifications", "operational_alerts", "alert_id"},
		{&notificationV29{}, "LatestOccurrence", "notifications", "operational_alert_occurrences", "latest_occurrence_id"},
		{&notificationSettingV29{}, "User", "notification_settings", "users", "user_id"},
		{&notificationDeliveryIntentV29{}, "Occurrence", "notification_delivery_intents", "operational_alert_occurrences", "occurrence_id"},
		{&notificationDeliveryIntentV29{}, "Recipient", "notification_delivery_intents", "users", "recipient_id"},
	}
	for _, tc := range tests {
		parsed, err := schema.Parse(tc.model, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		relation := parsed.Relationships.Relations[tc.relation]
		if relation == nil || relation.Type != schema.BelongsTo {
			t.Fatalf("%s.%s must be a belongs-to relationship", tc.table, tc.relation)
		}
		constraint := relation.ParseConstraint()
		if constraint == nil || constraint.Schema.Table != tc.table || constraint.ReferenceSchema.Table != tc.referenceTable || len(constraint.ForeignKeys) != 1 || constraint.ForeignKeys[0].DBName != tc.foreignKey {
			t.Fatalf("wrong %s.%s constraint: %+v", tc.table, tc.relation, constraint)
		}
		if constraint.OnDelete != "RESTRICT" || constraint.OnUpdate != "RESTRICT" {
			t.Fatalf("historical notification rows must use restrictive foreign keys: %+v", constraint)
		}
	}
}
