package handler

import (
	"os"
	"regexp"
	"slices"
	"testing"
)

func TestModelWeightHistoryExact174RegistryPrefix(t *testing.T) {
	raw, err := os.ReadFile("auth_integration_test.go")
	if err != nil {
		t.Fatal(err)
	}
	pairs := regexp.MustCompile(`\{"([a-z0-9_]+)", (test\w+)\}`).FindAllStringSubmatch(string(raw), -1)
	expected := []string{"catalog",
		"model_creation_batch",
		"model_creation_batch_migration",
		"model_alias_retirement",
		"model_detail_prices",
		"model_supply_status",
		"member_model_catalog",
		"personal_model_requests",
		"team_model_requests",
		"credential_metadata",
		"credential_delete",
		"credential_replacements",
		"credential_retirement_readiness",
		"credential_retirement",
		"keys",
		"gateway",
		"team_gateway",
		"team_native_protocols",
		"native_failover",
		"team_comparison",
		"team_limits_gateway",
		"team_session_auth",
		"call_native_completion",
		"call_credential_attribution",
		"calls",
		"team_calls",
		"call_export",
		"account",
		"governance",
		"member_overview",
		"runtime",
		"system_status",
		"resources",
		"project_authority",
		"project_creation_overview",
		"project_list",
		"project_initial_resources",
		"team_roles",
		"recorder",
		"project_keys",
		"personal_key_rotation",
		"offboarding",
		"pricing",
		"pricing_repository",
		"pricing_repository_migration",
		"project_requests",
		"project_quota_requests",
		"project_rate_limit_requests",
		"call_pricing",
		"price_import",
		"default_limits",
		"resource_limits",
		"team_resource_limits",
		"team_quota_requests",
		"team_quota_approval_gateway",
		"price_spreadsheet",
		"usage",
		"usage_identity",
		"usage_freshness",
		"team_usage",
		"usage_export",
		"member_overview_accounts",
		"member_overview_usage",
		"mfa",
		"responses",
		"messages",
		"gemini",
		"site",
		"audit",
		"egress",
		"quota",
		"attachment_quota_settlement",
		"smtp",
		"storage",
		"team_attachments",
		"team_attachment_migration",
		"notification_http",
		"monthly_quota_notifications",
		"team_quota_notifications",
		"team_quota_notification_migration",
		"team_member_quota_notifications",
		"team_member_quota_notification_migration",
		"root_key_rotation_migration",
		"root_key_rotation",
		"provider_quality_http",
		"member_key_migration",
		"member_keys",
		"member_teams_migration",
		"member_teams",
		"member_models_migration",
		"member_models",
		"member_metadata",
		"member_list_summary",
		"member_effective_models",
		"member_recent_login_migration",
		"member_recent_login",
		"member_access_summary",
		"member_role_revision_migration",
		"member_roles",
		"member_state",
		"local_registration_approval_migration",
		"local_registration_approval",
		"role_definition",
		"registration_email_domains_migration",
		"registration_email_domains",
		"personal_monthly_quota_warning_migration",
		"personal_monthly_quota_warnings",
		"team_monthly_quota_warning_migration",
		"team_monthly_quota_warnings",
		"project_monthly_quota_warning_migration",
		"project_monthly_quota_warnings",
		"team_member_monthly_quota_warning_migration",
		"team_member_monthly_quota_warnings",
		"team_creation_limits_migration",
		"team_creation_limits",
		"personal_key_quota_warning_migration",
		"personal_key_quota_warning",
		"project_key_monthly_quota_warning_migration",
		"project_key_monthly_quota_warnings",
		"team_creation_models_migration",
		"team_creation_models",
		"role_description_migration",
		"role_descriptions",
		"connection_metadata",
		"duty_role_migration",
		"duty_roles",
		"member_overview_roles",
		"provider_model_bindings",
		"provider_model_binding_index",
		"usage_members",
		"personal_monthly_behavior_migration",
		"personal_monthly_behavior",
		"provider_metadata",
		"team_monthly_behavior_migration",
		"team_monthly_behavior",
		"vault_integration_migration",
		"vault_integrations",
		"personal_key_monthly_behavior_migration",
		"personal_key_monthly_behavior",
		"project_monthly_behavior_migration",
		"project_monthly_behavior",
		"project_key_monthly_behavior",
		"team_member_monthly_behavior_migration",
		"team_member_monthly_behavior",
		"connection_enablement_migration",
		"connection_status",
		"provider_credential_storage_migration",
		"provider_credential_storage",
		"vault_saved_approle_migration",
		"vault_saved_approle",
		"azure_deployment_migration",
		"azure_deployment_coverage",
		"provider_orphan_cleanup_migration",
		"provider_orphan_cleanup",
		"personal_rolling_quota_warning_migration",
		"personal_rolling_quota_warnings",
		"personal_key_rolling_quota_warning_migration",
		"personal_key_rolling_quota_warnings",
		"project_rolling_quota_warning_migration",
		"project_rolling_quota_warnings",
		"team_rolling_quota_warning_migration",
		"team_rolling_quota_warnings",
		"project_key_rolling_quota_warning_migration",
		"project_key_rolling_quota_warnings",
		"model_recorded_metadata_migration",
		"model_recorded_metadata",
		"runtime_application_migration",
		"runtime_applications",
		"credential_source_drain_migration",
		"credential_published_cleanup",
		"credential_source_closed_migration",
		"credential_source_closed",
		"model_weight_history_migration",
		"model_weight_history"}
	names := []string{}
	functions := map[string]bool{}
	for _, p := range pairs {
		names = append(names, p[1])
		if functions[p[2]] {
			t.Fatal("duplicate actual scenario function", p[2])
		}
		functions[p[2]] = true
	}
	if len(names) != 179 || pairs[177][1] != "connection_transport_migration" || pairs[177][2] != "testConnectionTransportMigration" || pairs[178][1] != "connection_transport" || pairs[178][2] != "testConnectionTransportLifecycle" {
		t.Fatal("exact Connection transport successor changed")
	}
	names = names[:177]
	pairs = pairs[:177]
	if len(names) != 177 || pairs[176][1] != "connection_diagnostic" || pairs[176][2] != "testConnectionDiagnosticLifecycle" {
		t.Fatal("exact Connection diagnostic successor changed")
	}
	names = names[:176]
	pairs = pairs[:176]
	if len(names) != 176 || pairs[174][1] != "provider_enablement_migration" || pairs[174][2] != "testProviderEnablementMigration" || pairs[175][1] != "provider_status" || pairs[175][2] != "testProviderStatusLifecycle" {
		t.Fatal("V91 exact tail changed")
	}
	names = names[:174]
	if !slices.Equal(names, expected) || len(names) != 174 || pairs[172][2] != "testModelWeightHistoryMigration" || pairs[173][2] != "testModelWeightHistoryLifecycle" {
		t.Fatal("released172 prefix or actual appended function changed")
	}
}
