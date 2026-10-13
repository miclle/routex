package handler

import (
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
)

// Keep all predecessor matchers exact: this predicate admits only one appended
// name:function tuple for each forced SSO acceptance helper, in reviewed order.
func forcedSSORegistry214Current(names []string) bool {
	return len(names) == 214 &&
		names[210] == "forced_sso_migration:testForcedSSOMigration" &&
		names[211] == "forced_sso:testForcedSSOLifecycle" &&
		names[212] == "forced_sso_restart:testForcedSSOProcessRestart" &&
		names[213] == "forced_sso_interleavings:testForcedSSOInterleavings" &&
		apiKeyDeliveryRegistry210Current(names[:210])
}

func TestForcedSSORegistry214ExactPrefixAndSuffix(t *testing.T) {
	raw, err := os.ReadFile("auth_integration_test.go")
	if err != nil {
		t.Fatal(err)
	}
	matches := regexp.MustCompile(`\{"([a-z0-9_]+)", (test[A-Za-z0-9]+)\}`).FindAllStringSubmatch(string(raw), -1)
	names := make([]string, len(matches))
	for i, match := range matches {
		names[i] = match[1] + ":" + match[2]
	}
	if !credentialInferenceRegistry224Current(names) || !strings.Contains(string(raw), "versions != 111") {
		t.Fatal("exact V109/224 Credential registry required before historical checks")
	}
	names = names[:222]
	if !insightsRegistry222Current(names) {
		t.Fatal("exact222 Insights successor required before historical221 checks")
	}
	names = names[:221]
	if !telegramRegistry221Current(names) {
		t.Fatal("exact221 Telegram successor required before historical214 checks")
	}
	names = names[:214]
	if !apiKeyDeliveryRegistry210Current(names[:210]) || forcedSSORegistry214Current(names[:210]) || apiKeyDeliveryRegistry210Current(names) {
		t.Fatal("historical210 must remain exact without proving214")
	}
	for i := range names {
		for _, part := range []int{0, 1} {
			changed := append([]string(nil), names...)
			pair := strings.SplitN(changed[i], ":", 2)
			pair[part] += "Changed"
			changed[i] = strings.Join(pair, ":")
			if forcedSSORegistry214Current(changed) {
				t.Fatal("changed name or function admitted", i, part)
			}
		}
		omitted := append([]string(nil), names[:i]...)
		omitted = append(omitted, names[i+1:]...)
		if forcedSSORegistry214Current(omitted) {
			t.Fatal("omitted identity admitted", i)
		}
		duplicate := append([]string(nil), names...)
		duplicate[i] = names[(i+1)%len(names)]
		if forcedSSORegistry214Current(duplicate) {
			t.Fatal("duplicate identity admitted", i)
		}
		if i > 0 {
			reordered := append([]string(nil), names...)
			reordered[i], reordered[i-1] = reordered[i-1], reordered[i]
			if forcedSSORegistry214Current(reordered) {
				t.Fatal("reordered identity admitted", i)
			}
		}
	}
	if forcedSSORegistry214Current(append(append([]string(nil), names...), "extra:testUnexpected")) {
		t.Fatal("extra tail admitted")
	}
	// The registry head and observation schema retain the reviewed version boundary.
	migrations, err := os.ReadFile("../database/migrations.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(migrations), "apiKeyDeliveryMigration, forcedSSOMigration, vaultCertificateMigration, runtimePublicationCommandMigration, telegramIdentityMigration, offboardingNotificationMigration, modelDiscoveryNoticeMigration, credentialInferenceFailureMigration, insightsReportMigration, insightsQuotaReportMigration)") {
		t.Fatal("V111 migration suffix or retained forced SSO predecessor changed")
	}
	installation, err := os.ReadFile("../service/runtime_installation.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(installation), "const runtimeInstallationProjectionVersion = 2") {
		t.Fatal("current installation projection is not version2")
	}
	field, ok := reflect.TypeOf(entity.RuntimeInstallationObservation{}).FieldByName("ProjectionVersion")
	if !ok || field.Type.Kind() != reflect.Int || field.Tag.Get("gorm") != "not null;check:ck_runtime_installation_projection,projection_version = 1 OR projection_version = 2" {
		t.Fatal("retained projection1/current projection2 schema boundary changed")
	}
}

// The composed current registry retains the exact historical 214 prefix.
func enterpriseRegistry218Current(names []string) bool {
	return len(names) == 218 &&
		names[214] == "vault_saved_certificate_migration:testVaultSavedCertificateMigration" &&
		names[215] == "egress_https_process:testEgressHTTPSProcessLifecycle" &&
		names[216] == "runtime_publication_command_migration:testRuntimePublicationCommandMigration" &&
		names[217] == "runtime_publication_command:testRuntimePublicationCommandLifecycle" &&
		forcedSSORegistry214Current(names[:214])
}

func TestEnterpriseRegistry218ExactPrefixAndSuffix(t *testing.T) {
	raw, err := os.ReadFile("auth_integration_test.go")
	if err != nil {
		t.Fatal(err)
	}
	matches := regexp.MustCompile(`\{"([a-z0-9_]+)", (test[A-Za-z0-9]+)\}`).FindAllStringSubmatch(string(raw), -1)
	names := make([]string, len(matches))
	for i, match := range matches {
		names[i] = match[1] + ":" + match[2]
	}
	if !credentialInferenceRegistry224Current(names) {
		t.Fatal("exact222 Insights successor required before historical221 checks")
	}
	names = names[:221]
	if !telegramRegistry221Current(names) || !strings.Contains(string(raw), "versions != 111") {
		t.Fatal("exact V105/218 enterprise registry required")
	}
	names = names[:218] // Preserve the exact historical enterprise prefix and all mutations below.
	if !forcedSSORegistry214Current(names[:214]) || enterpriseRegistry218Current(names[:214]) || forcedSSORegistry214Current(names) {
		t.Fatal("historical214 must remain exact without proving218")
	}
	for i := range names {
		for _, part := range []int{0, 1} {
			changed := append([]string(nil), names...)
			pair := strings.SplitN(changed[i], ":", 2)
			pair[part] += "Changed"
			changed[i] = strings.Join(pair, ":")
			if enterpriseRegistry218Current(changed) {
				t.Fatal("changed name or function admitted", i, part)
			}
		}
		omitted := append([]string(nil), names[:i]...)
		omitted = append(omitted, names[i+1:]...)
		if enterpriseRegistry218Current(omitted) {
			t.Fatal("omitted identity admitted", i)
		}
		duplicate := append([]string(nil), names...)
		duplicate[i] = names[(i+1)%len(names)]
		if enterpriseRegistry218Current(duplicate) {
			t.Fatal("duplicate identity admitted", i)
		}
		if i > 0 {
			reordered := append([]string(nil), names...)
			reordered[i], reordered[i-1] = reordered[i-1], reordered[i]
			if enterpriseRegistry218Current(reordered) {
				t.Fatal("reordered identity admitted", i)
			}
		}
	}
	if enterpriseRegistry218Current(append(append([]string(nil), names...), "extra:testUnexpected")) {
		t.Fatal("extra tail admitted")
	}
}
