package database

import (
	"reflect"
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestAPIKeyDeliveryControllerV114FrozenPrivateDigestAndExactScope(t *testing.T) {
	for _, pair := range [][2]any{{&apiKeyDeliveryControllerGrantV114{}, &entity.APIKeyDeliveryControllerGrant{}}, {&apiKeyDeliveryControllerAcknowledgementV114{}, &entity.APIKeyDeliveryControllerAcknowledgement{}}} {
		frozen, current := oidcV94Schema(t, pair[0]), oidcV94Schema(t, pair[1])
		if frozen.Table != current.Table || len(frozen.DBNames) != len(current.DBNames) || len(frozen.Relationships.Relations) != 0 {
			t.Fatal("frozen bounded controller schema")
		}
		for _, name := range frozen.DBNames {
			a, b := frozen.FieldsByDBName[name], current.FieldsByDBName[name]
			if b == nil || a.FieldType != b.FieldType || a.Tag.Get("gorm") != b.Tag.Get("gorm") || b.Tag.Get("json") != "-" || a.AutoCreateTime != 0 || a.AutoUpdateTime != 0 {
				t.Fatal("private frozen controller drift", name)
			}
			if strings.Contains(name, "ciphertext") || name == "token" || name == "bearer" || name == "api_key" || name == "agent_configuration" {
				t.Fatal("secret/configuration payload persisted", name)
			}
		}
	}
	grant := oidcV94Schema(t, &apiKeyDeliveryControllerGrantV114{})
	for _, name := range []string{"owner_birth", "slot_birth", "profile_birth", "principal_birth", "reviewed_binding_birth", "reviewed_key_birth", "created_by_birth", "configuration_review_birth"} {
		if grant.FieldsByDBName[name] == nil || !grant.FieldsByDBName[name].NotNull {
			t.Fatal("missing exact birth binding", name)
		}
	}
	checks := grant.ParseCheckConstraints()
	if len(checks) != 6 || !strings.Contains(checks["ck_delivery_controller_lifetime"].Constraint, "revoked_at IS NULL") || !strings.Contains(checks["ck_delivery_controller_assignment"].Constraint, "9223372036854775807") || !strings.Contains(checks["ck_delivery_controller_owner"].Constraint, "ASCII") {
		t.Fatal("missing closed controller constraints")
	}
	ack := oidcV94Schema(t, &apiKeyDeliveryControllerAcknowledgementV114{})
	if len(ack.ParseCheckConstraints()) != 3 || !strings.Contains(ack.ParseCheckConstraints()["ck_delivery_controller_ack_output"].Constraint, "ASCII") {
		t.Fatal("output report may not accept completion states")
	}
	for _, driver := range []string{"postgres", "mysql"} {
		steps := migrationSteps(driver)
		if len(steps) != 118 || reflect.ValueOf(steps[113]).Pointer() != reflect.ValueOf(apiKeyDeliveryControllerMigration).Pointer() || reflect.ValueOf(steps[112]).Pointer() != reflect.ValueOf(runtimeArchiveRetentionMigration).Pointer() {
			t.Fatal("V114 must follow immutable V113")
		}
	}
}
