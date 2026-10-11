package database

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestAPIKeyDeliveryV102FrozenSchemaAndPrivateBoundary(t *testing.T) {
	pairs := [][2]any{
		{&apiKeyDeliveryPolicyV102{}, &entity.APIKeyDeliveryPolicy{}},
		{&apiKeyDeliveryAuthV102{}, &entity.APIKeyDeliveryAuth{}},
		{&apiKeyCallerTemplateV102{}, &entity.APIKeyCallerTemplate{}},
		{&apiKeyDeliveryConsumerV102{}, &entity.APIKeyDeliveryConsumer{}},
		{&vaultAccessProfileV102{}, &entity.VaultAccessProfile{}},
		{&vaultPrincipalBindingV102{}, &entity.VaultPrincipalBinding{}},
		{&vaultPrincipalIsolationAttestationV102{}, &entity.VaultPrincipalIsolationAttestation{}},
		{&apiKeyDeliverySlotV102{}, &entity.APIKeyDeliverySlot{}},
		{&apiKeyVaultDeliveryBindingV102{}, &entity.APIKeyVaultDeliveryBinding{}},
		{&apiKeyDeliveryCommandV102{}, &entity.APIKeyDeliveryCommand{}},
	}
	fields := 0
	for _, pair := range pairs {
		frozen, current := oidcV94Schema(t, pair[0]), oidcV94Schema(t, pair[1])
		if frozen.Table != current.Table || len(frozen.Fields) != len(current.Fields) || len(frozen.Relationships.Relations) != 0 {
			t.Fatal("frozen table boundary")
		}
		fields += len(frozen.Fields)
		for _, field := range frozen.Fields {
			actual := current.FieldsByDBName[field.DBName]
			if actual == nil || actual.Name != field.Name || actual.FieldType != field.FieldType || actual.Tag.Get("gorm") != field.Tag.Get("gorm") || actual.Tag.Get("json") != "-" {
				t.Fatal("frozen field or private boundary", frozen.Table, field.DBName)
			}
			if string(field.DataType) == "time" && !strings.Contains(field.Tag.Get("gorm"), "precision:6") {
				t.Fatal("birth/time precision", field.DBName)
			}
		}
		encoded, err := json.Marshal(pair[1])
		if err != nil || string(encoded) != "{}" {
			t.Fatal("persistence became a wire DTO")
		}
	}
	if fields != 259 {
		t.Fatal("unreviewed persistence field set")
	}
	for _, pair := range [][2]any{{&apiKeyDeliveryPersonalV102{}, &entity.APIKey{}}, {&apiKeyDeliveryProjectV102{}, &entity.ProjectKey{}}} {
		a, c := oidcV94Schema(t, pair[0]), oidcV94Schema(t, pair[1])
		if a.FieldsByName["DeliveryMode"].Tag.Get("gorm") != c.FieldsByName["DeliveryMode"].Tag.Get("gorm") {
			t.Fatal("Key mode parity")
		}
	}
}

func TestAPIKeyDeliveryV102KeepsReleasedTailAndRootHistory(t *testing.T) {
	old := []func(*gorm.DB) error{
		callMigration, governanceMigration, runtimeMigration, resourcesMigration, projectKeyMigration, offboardingMigration, pricingMigration, projectRequestMigration, callPricingMigration, resourceLimitMigration, providerModelStateMigration, mfaMigration, siteMigration, egressMigration, quotaMigration, smtpMigration, storageMigration, providerModelCapabilitiesMigration, storageOwnerMigration, callMediaPricingMigration, callAttemptDiagnosticsMigration, callProviderAttributionMigration, systemInstanceMigration, systemJobMigration, notificationMigration, providerQualityMigration, credentialReplacementMigration, callCredentialAttributionMigration, callNativeCompletionMigration, credentialRetirementMigration, quotaNotificationMigration, projectQuotaRequestMigration, projectRateRequestMigration, callTeamAttributionMigration, teamResourceLimitMigration, teamQuotaRequestMigration, teamRoleMigration, defaultLimitMigration, personalModelRequestMigration, teamModelRequestMigration, projectCreationMigration, teamAttachmentMigration, teamQuotaNotificationMigration, rootKeyRotationMigration, repositoryPriceMigration, modelCreationBatchMigration, teamMemberQuotaNotificationMigration, memberKeyMigration, teamMembershipJoinedAtMigration, memberModelGrantMigration, memberRecentLoginMigration, memberRoleRevisionMigration, registrationApprovalMigration, registrationEmailDomainsMigration, quotaWarningMigration, teamQuotaWarningMigration, projectQuotaWarningMigration, teamMemberQuotaWarningMigration, teamCreationReceiptMigration, personalKeyQuotaWarningMigration, projectKeyQuotaWarningMigration, teamCreationModelsMigration, roleDescriptionMigration, dutyRoleMigration, providerModelBindingIndexMigration, personalMonthlyBehaviorMigration, teamMonthlyBehaviorMigration, vaultIntegrationMigration, personalKeyMonthlyBehaviorMigration, projectMonthlyBehaviorMigration, teamMemberMonthlyBehaviorMigration, migrateConnectionEnablementV76, credentialStorageMigration, vaultAppRoleMigration, azureDeploymentMigration, providerCredentialCleanupMigration, personalRollingQuotaWarningMigration, personalKeyRollingQuotaWarningMigration, projectRollingQuotaWarningMigration, teamRollingQuotaWarningMigration, projectKeyRollingQuotaWarningMigration, modelRecordedMetadataMigration, runtimeApplicationMigration, credentialSourceDrainMigration, credentialSourceClosedMigration, modelWeightHistoryMigration, providerEnablementMigration, connectionTransportMigration, credentialAttemptStatisticsMigration, oidcMigration, oauthMigration, ldapMigration, samlMigration, namedIdentityMigration, googleIdentityMigration, runtimeInstallationMigration, discordIdentityMigration,
	}
	for _, driver := range []string{"postgres", "mysql"} {
		steps := migrationSteps(driver)
		if len(legacyMigrationSQL(driver)) != 4 || len(steps) != 102 || len(old) != 97 {
			t.Fatal("exact V101 prefix/V102 length")
		}
		for i, expected := range old {
			if reflect.ValueOf(steps[i+4]).Pointer() != reflect.ValueOf(expected).Pointer() {
				t.Fatal("historical migration pointer changed", i+5)
			}
		}
		if reflect.ValueOf(steps[101]).Pointer() != reflect.ValueOf(apiKeyDeliveryMigration).Pointer() {
			t.Fatal("V102 registration")
		}
	}
	for _, tc := range []struct {
		old, next    any
		name, suffix string
	}{
		{&discordIdentityRootJobV101{}, &apiKeyDeliveryRootJobV102{}, "ck_secret_inventory_version", " OR inventory_version = 9"},
		{&discordIdentityRootJobV101{}, &apiKeyDeliveryRootJobV102{}, "ck_secret_rotation_domain", " OR (inventory_version = 9 AND domain >= 0 AND domain <= 12)"},
		{&discordIdentityRootProcessV101{}, &apiKeyDeliveryRootProcessV102{}, "ck_secret_process_inventory_version", " OR inventory_version = 9"},
	} {
		if oidcV94Check(t, tc.next, tc.name) != oidcV94Check(t, tc.old, tc.name)+tc.suffix {
			t.Fatal("historical inventory reinterpreted", tc.name)
		}
	}
	before := oidcV94Check(t, &namedIdentityRootItemV98{}, "ck_secret_item_domain")
	after := oidcV94Check(t, &apiKeyDeliveryRootItemV102{}, "ck_secret_item_domain")
	if after != before+" OR ("+oidcV94ASCII("domain", "api_key_delivery_auth")+")" {
		t.Fatal("old secret domain broadened")
	}
	if oidcV94Schema(t, &apiKeyDeliveryRootItemV102{}).FieldsByName["Reference"].Size != 160 {
		t.Fatal("historical reference width rewritten")
	}
	for _, pair := range [][2]any{
		{&apiKeyDeliveryRootJobV102{}, &entity.SecretRotationJob{}},
		{&apiKeyDeliveryRootProcessV102{}, &entity.SecretProcessVerification{}},
		{&apiKeyDeliveryRootItemV102{}, &entity.SecretRotationItem{}},
	} {
		frozen, current := oidcV94Schema(t, pair[0]), oidcV94Schema(t, pair[1])
		if frozen.Table != current.Table || len(frozen.Fields) != len(current.Fields) || len(frozen.Relationships.Relations) != 0 {
			t.Fatal("current root inventory boundary", frozen.Table)
		}
		for _, field := range frozen.Fields {
			actual := current.FieldsByDBName[field.DBName]
			if actual == nil || actual.FieldType != field.FieldType || actual.Tag.Get("gorm") != field.Tag.Get("gorm") {
				t.Fatal("current root inventory parity", frozen.Table, field.DBName)
			}
		}
	}
}

func TestAPIKeyDeliveryV102FiniteModesAndCorrelatedCommand(t *testing.T) {
	for _, model := range []any{&apiKeyDeliveryPersonalV102{}, &apiKeyDeliveryProjectV102{}} {
		parsed := oidcV94Schema(t, model)
		name := "ck_api_keys_delivery_mode"
		if parsed.Table == "project_api_keys" {
			name = "ck_project_keys_delivery"
		}
		if oidcV94Check(t, model, name) != "("+oidcV94ASCII("delivery_mode", "manual")+") OR ("+oidcV94ASCII("delivery_mode", "managed_vault")+")" {
			t.Fatal("mode enum aliases or fallback")
		}
	}
	shape := oidcV94Check(t, &apiKeyDeliveryCommandV102{}, "ck_api_key_delivery_command_shape")
	for _, term := range []string{"key_birth IS NULL", "predecessor_birth IS NULL", "claim_process_birth IS NULL", "claimed_at IS NULL", "terminal_at IS NOT NULL", "revoked_at IS NOT NULL", "acknowledged_at IS NULL", "published_at IS NULL", "OCTET_LENGTH(target_review) = 64", "OCTET_LENGTH(isolation_revision) = 64", oidcV94ASCII("purpose", "resolve"), oidcV94ASCII("purpose", "create"), oidcV94ASCII("purpose", "rotate"), "published_at IS NULL OR acknowledged_at IS NOT NULL", "revoked_at IS NULL OR terminal_at IS NOT NULL"} {
		if !strings.Contains(shape, term) {
			t.Fatal("command shape lost", term)
		}
	}
	if strings.Contains(shape, "terminal_at IS NOT NULL AND published_at IS NULL)") {
		t.Fatal("revocation erases historical publication")
	}
	auth := oidcV94Check(t, &apiKeyDeliveryAuthV102{}, "ck_api_key_delivery_auth_purpose")
	if auth != "("+oidcV94ASCII("purpose", "writer")+") OR ("+oidcV94ASCII("purpose", "provisioner")+")" {
		t.Fatal("machine purposes broadened")
	}
	ttl := oidcV94Check(t, &apiKeyCallerTemplateV102{}, "ck_api_key_caller_template_ttl")
	if ttl != "token_ttl_seconds > 0 AND token_ttl_seconds <= token_max_ttl_seconds AND token_max_ttl_seconds <= 3600" {
		t.Fatal("configured finite TTL cap changed")
	}
	for _, model := range []any{&vaultPrincipalIsolationAttestationV102{}, &apiKeyVaultDeliveryBindingV102{}} {
		parsed := oidcV94Schema(t, model)
		for _, name := range []string{"VaultRevisionID", "VaultRevisionBirth", "ProfileConfigRevision", "ProfileSecurityRevision", "PrincipalSecurityRevision"} {
			if parsed.FieldsByName[name] == nil {
				t.Fatal("archived/ABA fact missing", name)
			}
		}
	}
}

func apiKeyDeliveryColumnsV102(t *testing.T, model any) []gorm.ColumnType {
	t.Helper()
	columns := []gorm.ColumnType{}
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
		case "int", "uint":
			c.kind = "bigint"
		default:
			t.Fatal("unreviewed type", field.DBName)
		}
		columns = append(columns, c)
	}
	return columns
}
func TestAPIKeyDeliveryV102RejectsMalformedPhysicalColumns(t *testing.T) {
	for _, model := range apiKeyDeliveryModelsV102() {
		table := oidcV94Schema(t, model).Table
		for _, which := range []string{"valid", "missing", "extra", "duplicate", "width", "type", "nullable", "unknown_nullable", "primary", "unknown_primary", "default", "precision", "unknown_precision"} {
			t.Run(table+"/"+which, func(t *testing.T) {
				columns := apiKeyDeliveryColumnsV102(t, model)
				index := 0
				if which == "precision" || which == "unknown_precision" {
					for i, c := range columns {
						if c.DatabaseTypeName() == "timestamp" {
							index = i
							break
						}
					}
				}
				if which == "width" || which == "type" {
					for i, c := range columns {
						if c.DatabaseTypeName() == "varchar" {
							index = i
							break
						}
					}
				}
				c := columns[index].(oidcV94Column)
				switch which {
				case "missing":
					columns = columns[1:]
				case "extra":
					columns = append(columns, oidcV94Column{name: "unexpected"})
				case "duplicate":
					columns = append(columns, columns[0])
				case "width":
					c.size++
				case "type":
					c.kind = "text"
				case "nullable":
					c.nullable = !c.nullable
				case "unknown_nullable":
					c.nullableKnown = false
				case "primary":
					c.primary = !c.primary
				case "unknown_primary":
					c.primaryKnown = false
				case "default":
					c.defaultKnown = true
					c.def = "invented"
				case "precision":
					c.precision = 3
				case "unknown_precision":
					c.precisionKnown = false
				}
				if which != "missing" {
					columns[index] = c
				}
				m := &oidcV94ColumnsMigrator{columns: columns}
				db := projectRateCheckDB(t, m)
				err := validateAPIKeyDeliveryColumnsV102(db, model, false)
				if (err == nil) != (which == "valid") {
					t.Fatal("physical metadata admission", which)
				}
			})
		}
	}
}

func TestAPIKeyDeliveryV102RetainedValidationAndPartialCheckRetry(t *testing.T) {
	for _, fault := range []string{"", "query", "invalid"} {
		m := &oauthV95ChecksMigrator{checks: map[string]bool{}}
		db := projectRateCheckDB(t, m)
		seen := 0
		if err := db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
			seen++
			where, ok := tx.Statement.Clauses["WHERE"].Expression.(clause.Where)
			if !ok || len(where.Exprs) != 1 {
				t.Fatal("missing retained predicate")
			}
			expr, ok := where.Exprs[0].(clause.Expr)
			if !ok || !strings.HasPrefix(expr.SQL, "NOT (") || len(expr.Vars) != 0 {
				t.Fatal("unbounded/unowned retained predicate")
			}
			if fault == "query" {
				_ = tx.AddError(errors.New("controlled query failure"))
				return
			}
			count, ok := tx.Statement.Dest.(*int64)
			if !ok {
				t.Fatal("unexpected query")
			}
			*count = 0
			if fault == "invalid" {
				*count = 1
			}
			tx.RowsAffected = 1
		}); err != nil {
			t.Fatal(err)
		}
		checks, err := apiKeyDeliveryRetainedChecksV102(db)
		if (err != nil) != (fault != "") || len(m.ops) != 0 {
			t.Fatal("retained error lost or DDL before validation")
		}
		if fault != "" {
			if seen != 1 {
				t.Fatal("validation continued after failure")
			}
			continue
		}
		if len(checks) != 48 || seen != 48 {
			t.Fatal("missing retained constraints", len(checks), seen)
		}
		for _, check := range checks {
			m.checks[check.name] = true
		}
		for _, name := range []string{"ck_api_key_delivery_command_shape", "ck_vault_principal_isolation_attestations_security", "ck_api_keys_delivery_mode", "ck_secret_item_domain"} {
			for _, stage := range []string{"drop:", "create:"} {
				m.fail = stage + name
				if apiKeyDeliveryReplaceChecksV102(db, checks) == nil {
					t.Fatal("partial CHECK failure lost", stage, name)
				}
				m.fail = ""
				for range 2 {
					if err := apiKeyDeliveryReplaceChecksV102(db, checks); err != nil {
						t.Fatal("partial CHECK retry", err)
					}
				}
				for _, check := range checks {
					if !m.checks[check.name] {
						t.Fatal("retry left CHECK absent", check.name)
					}
				}
			}
		}
	}
}
