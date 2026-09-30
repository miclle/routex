package database

import (
	"slices"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

func TestFrozenProviderQualityForeignKeyDirections(t *testing.T) {
	tests := []struct {
		model    any
		relation string
		table    string
	}{
		{&providerQualityPolicyV30{}, "Provider", "provider_quality_policies"},
		{&providerQualityStateV30{}, "Provider", "provider_quality_states"},
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
		if constraint == nil || constraint.Schema.Table != tc.table || constraint.ReferenceSchema.Table != "providers" || len(constraint.ForeignKeys) != 1 || constraint.ForeignKeys[0].DBName != "provider_id" {
			t.Fatalf("wrong %s.%s constraint: %+v", tc.table, tc.relation, constraint)
		}
		if constraint.OnDelete != "RESTRICT" || constraint.OnUpdate != "RESTRICT" {
			t.Fatalf("provider quality rows must use restrictive Provider foreign keys: %+v", constraint)
		}
	}

	parsed, err := schema.Parse(&providerQualityWindowV30{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Relationships.Relations) != 0 {
		t.Fatalf("historical quality windows must not reference mutable catalog rows: %+v", parsed.Relationships.Relations)
	}
}

func TestFrozenProviderQualityIndexes(t *testing.T) {
	tests := []struct {
		model any
		name  string
		want  []string
	}{
		{&callAttemptQualityV30{}, "idx_attempts_provider_time", []string{"provider_id", "completed_at", "id"}},
		{&callAttemptQualityV30{}, "idx_attempts_connection_time", []string{"connection_id", "completed_at", "id"}},
		{&callAttemptQualityV30{}, "idx_attempts_provider_model_time", []string{"provider_model_id", "completed_at", "id"}},
		{&providerQualityWindowV30{}, "idx_provider_quality_windows_identity", []string{"provider_id", "window_end"}},
		{&providerQualityWindowV30{}, "idx_provider_quality_windows_provider_end", []string{"provider_id", "window_end"}},
		{&providerQualityWindowV30{}, "idx_provider_quality_windows_policy_end", []string{"provider_id", "policy_e_tag", "window_end", "id"}},
	}
	for _, tc := range tests {
		parsed, err := schema.Parse(tc.model, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		indexes := parsed.ParseIndexes()
		var got []string
		for _, index := range indexes {
			if index.Name != tc.name {
				continue
			}
			for _, field := range index.Fields {
				got = append(got, field.DBName)
			}
		}
		if !slices.Equal(got, tc.want) {
			t.Fatalf("%s columns = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestFrozenProviderQualityCheckConstraints(t *testing.T) {
	tests := []struct {
		model any
		want  []string
	}{
		{&operationalAlertQualityV30{}, []string{"ck_operational_alerts_kind", "ck_operational_alerts_subject"}},
		{&operationalAlertOccurrenceQualityV30{}, []string{"ck_alert_occurrences_source", "ck_alert_occurrences_subject"}},
		{&notificationQualityV30{}, []string{"ck_notifications_subject"}},
		{&notificationDeliveryQualityV30{}, []string{"ck_notification_delivery_subject"}},
		{&providerQualityPolicyV30{}, []string{"ck_provider_quality_policy_window", "ck_provider_quality_policy_minimum", "ck_provider_quality_policy_success_rate", "ck_provider_quality_policy_duration"}},
		{&providerQualityWindowV30{}, []string{"ck_provider_quality_windows_eligible", "ck_provider_quality_windows_excluded", "ck_provider_quality_windows_unknown", "ck_provider_quality_windows_successes", "ck_provider_quality_windows_429", "ck_provider_quality_windows_5xx", "ck_provider_quality_windows_duration_count", "ck_provider_quality_windows_duration", "ck_provider_quality_windows_state"}},
		{&providerQualityStateV30{}, []string{"ck_provider_quality_states_state"}},
	}
	for _, tc := range tests {
		parsed, err := schema.Parse(tc.model, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		checks := parsed.ParseCheckConstraints()
		for _, name := range tc.want {
			if _, ok := checks[name]; !ok {
				t.Fatalf("%T omitted check constraint %s", tc.model, name)
			}
		}
	}
}

func TestFrozenProviderQualityPolicyChecksMatchServiceBounds(t *testing.T) {
	parsed, err := schema.Parse(&providerQualityPolicyV30{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	checks := parsed.ParseCheckConstraints()
	for name, fragments := range map[string][]string{
		"ck_provider_quality_policy_window":   {"window_minutes >= 5", "window_minutes <= 1440"},
		"ck_provider_quality_policy_minimum":  {"minimum_attempts >= 1", "minimum_attempts <= 100000"},
		"ck_provider_quality_policy_duration": {"max_p95_duration_ms >= 1", "max_p95_duration_ms <= 3600000"},
	} {
		constraint, ok := checks[name]
		if !ok {
			t.Fatalf("missing constraint %s", name)
		}
		for _, fragment := range fragments {
			if !strings.Contains(constraint.Constraint, fragment) {
				t.Fatalf("%s = %q, missing %q", name, constraint.Constraint, fragment)
			}
		}
	}
}
