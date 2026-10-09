package database

import (
	"fmt"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm/schema"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestProviderCleanupV80FrozenSchemaAndExactAppend(t *testing.T) {
	frozen, e := schema.Parse(&providerCredentialCleanupV80{}, &sync.Map{}, schema.NamingStrategy{})
	if e != nil {
		t.Fatal(e)
	}
	current, e := schema.Parse(&entity.ProviderCredentialCleanup{}, &sync.Map{}, schema.NamingStrategy{})
	if e != nil {
		t.Fatal(e)
	}
	if frozen.Table != "provider_credential_cleanups" || current.Table != frozen.Table || len(frozen.Fields) != len(current.Fields) {
		t.Fatal("frozen schema shape")
	}
	for _, f := range frozen.Fields {
		c := current.FieldsByName[f.Name]
		if c == nil || c.FieldType != f.FieldType || c.Tag != f.Tag || c.DBName != f.DBName {
			t.Fatal("frozen field mismatch", f.Name)
		}
		if strings.Contains(strings.ToLower(f.Name), "token") || strings.Contains(strings.ToLower(f.Name), "ciphertext") {
			t.Fatal("cleanup authentication persisted")
		}
	}
	if frozen.FieldsByName["ReviewedETag"].DBName != "reviewed_etag" || frozen.FieldsByName["CreationRequestID"].Size != 36 || frozen.FieldsByName["RequestID"].Size != 36 || !frozen.FieldsByName["CreationRequestID"].PrimaryKey {
		t.Fatal("UUID/ETag column contract")
	}
	indexes := frozen.ParseIndexes()
	found := false
	for _, idx := range indexes {
		if idx.Name == "idx_provider_cleanup_command" {
			found = idx.Class == "UNIQUE" && len(idx.Fields) == 1 && idx.Fields[0].DBName == "request_id"
		}
	}
	if !found {
		t.Fatal("command UUID must be globally unique")
	}
	check := frozen.ParseCheckConstraints()["ck_provider_cleanup_state_v80"].Constraint
	for _, state := range []string{"pending", "unknown", "failed", "acknowledged"} {
		if !strings.Contains(check, "OCTET_LENGTH(state) = ") {
			t.Fatal(state)
		}
		for i, c := range state {
			if !strings.Contains(check, fmt.Sprintf("ASCII(SUBSTRING(state,%d,1)) = %d", i+1, c)) {
				t.Fatal("collation-safe byte constraint", state, i)
			}
		}
	}

	useFrozen, e := schema.Parse(&providerCredentialCreationUseV80{}, &sync.Map{}, schema.NamingStrategy{})
	if e != nil {
		t.Fatal(e)
	}
	useCurrent, e := schema.Parse(&entity.ProviderCredentialCreationUse{}, &sync.Map{}, schema.NamingStrategy{})
	if e != nil {
		t.Fatal(e)
	}
	if useFrozen.Table != "provider_credential_creation_uses" || useCurrent.Table != useFrozen.Table || len(useFrozen.Fields) != 4 || len(useCurrent.Fields) != 4 {
		t.Fatal("retained creation-use schema")
	}
	for _, f := range useFrozen.Fields {
		c := useCurrent.FieldsByName[f.Name]
		if c == nil || c.FieldType != f.FieldType || c.Tag != f.Tag || c.DBName != f.DBName {
			t.Fatal("creation-use frozen field", f.Name)
		}
	}
	if !useFrozen.FieldsByName["CreationRequestID"].PrimaryKey {
		t.Fatal("exact creation-use identity")
	}
	for _, dialect := range []string{"postgres", "mysql"} {
		steps := migrationSteps(dialect)
		if len(steps) != 90 || reflect.ValueOf(steps[89]).Pointer() != reflect.ValueOf(modelWeightHistoryMigration).Pointer() || reflect.ValueOf(steps[88]).Pointer() != reflect.ValueOf(credentialSourceClosedMigration).Pointer() || reflect.ValueOf(steps[83]).Pointer() != reflect.ValueOf(teamRollingQuotaWarningMigration).Pointer() || reflect.ValueOf(steps[82]).Pointer() != reflect.ValueOf(projectRollingQuotaWarningMigration).Pointer() || reflect.ValueOf(steps[81]).Pointer() != reflect.ValueOf(personalKeyRollingQuotaWarningMigration).Pointer() || reflect.ValueOf(steps[80]).Pointer() != reflect.ValueOf(personalRollingQuotaWarningMigration).Pointer() || reflect.ValueOf(steps[78]).Pointer() != reflect.ValueOf(azureDeploymentMigration).Pointer() || reflect.ValueOf(steps[79]).Pointer() != reflect.ValueOf(providerCredentialCleanupMigration).Pointer() || reflect.ValueOf(steps[84]).Pointer() != reflect.ValueOf(projectKeyRollingQuotaWarningMigration).Pointer() || reflect.ValueOf(steps[85]).Pointer() != reflect.ValueOf(modelRecordedMetadataMigration).Pointer() || reflect.ValueOf(steps[86]).Pointer() != reflect.ValueOf(runtimeApplicationMigration).Pointer() || reflect.ValueOf(steps[87]).Pointer() != reflect.ValueOf(credentialSourceDrainMigration).Pointer() {
			t.Fatal("exact79+80 append", dialect)
		}
	}
}
