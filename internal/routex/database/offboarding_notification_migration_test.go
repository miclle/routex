package database

import (
	"reflect"
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestOffboardingNotificationV107FrozenReceiptAndRegistration(t *testing.T) {
	frozen := oidcV94Schema(t, &offboardingNotificationRecipientV107{})
	current := oidcV94Schema(t, &entity.OffboardingNotificationRecipient{})
	if frozen.Table != "offboarding_notification_recipients" || current.Table != frozen.Table || len(frozen.DBNames) != 5 || len(current.DBNames) != 5 {
		t.Fatal("receipt must remain a five-column private tuple")
	}
	for _, name := range frozen.DBNames {
		a, b := frozen.FieldsByDBName[name], current.FieldsByDBName[name]
		if b == nil || a.FieldType != b.FieldType || a.PrimaryKey != b.PrimaryKey || a.Size != b.Size || a.Precision != b.Precision || a.AutoCreateTime != 0 || a.AutoUpdateTime != 0 || a.HasDefaultValue {
			t.Fatal("frozen receipt differs", name)
		}
	}
	for _, driver := range []string{"postgres", "mysql"} {
		steps := migrationSteps(driver)
		if len(steps) != 112 || reflect.ValueOf(steps[105]).Pointer() != reflect.ValueOf(telegramIdentityMigration).Pointer() || reflect.ValueOf(steps[106]).Pointer() != reflect.ValueOf(offboardingNotificationMigration).Pointer() {
			t.Fatal("V107 must append without rewriting released tail")
		}
	}
	if len(frozen.Relationships.Relations) != 4 {
		t.Fatal("receipt must bind Case, recipient, occurrence and Notification")
	}
	for _, relation := range frozen.Relationships.Relations {
		c := relation.ParseConstraint()
		if c == nil || c.OnDelete != "RESTRICT" || c.OnUpdate != "RESTRICT" || len(c.ForeignKeys) != 1 || len(c.References) != 1 {
			t.Fatal("receipt reference must remain restrictive")
		}
	}
	checks := frozen.ParseCheckConstraints()
	if len(checks) != 1 || !strings.Contains(checks["ck_offboarding_notice_birth"].Constraint, "recipient_created_at >") {
		t.Fatal("nonzero immutable recipient birth required")
	}
	for _, pair := range []struct {
		model         any
		check, legacy string
	}{{&offboardingAlertV107{}, "ck_operational_alerts_kind", "kind IN ('system_job_failure','credential_verification_failure','provider_quality_degraded','route_unavailable')"}, {&offboardingOccurrenceV107{}, "ck_alert_occurrences_source", "source_type IN ('system_job','credential_verification','provider_quality_window','gateway_call')"}} {
		expression := oidcV94Schema(t, pair.model).ParseCheckConstraints()[pair.check].Constraint
		if !strings.HasPrefix(expression, pair.legacy+" OR (") || !strings.Contains(expression, "OCTET_LENGTH(subject_name) = 0") || !strings.Contains(expression, "ASCII(SUBSTRING(") {
			t.Fatal("new closed branch weakened historical or private source predicate")
		}
	}
}
