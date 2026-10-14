package handler

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func credentialInferenceRegistry224Current(names []string) bool {
	return len(names) == 224 && names[222] == "credential_inference_failures:testCredentialInferenceFailureLifecycle" && names[223] == "credential_inference_failure_migration:testCredentialInferenceFailureMigration" && insightsRegistry222Current(names[:222])
}

func TestCredentialInferenceRegistry224ExactPrefixAndSuffix(t *testing.T) {
	raw, err := os.ReadFile("auth_integration_test.go")
	if err != nil {
		t.Fatal(err)
	}
	matches := regexp.MustCompile(`\{"([a-z0-9_]+)", (test[A-Za-z0-9]+)\}`).FindAllStringSubmatch(string(raw), -1)
	names := make([]string, len(matches))
	for i, m := range matches {
		names[i] = m[1] + ":" + m[2]
	}
	if !apiKeyDeliveryControllerDiscoveryRegistry227Current(names) {
		t.Fatal("exact227 controller discovery successor required")
	}
	names = names[:224]
	if !credentialInferenceRegistry224Current(names) || !strings.Contains(string(raw), "versions != 116") {
		t.Fatal("exact V109/224 Credential registry required")
	}
	if !insightsRegistry222Current(names[:222]) || credentialInferenceRegistry224Current(names[:222]) || insightsRegistry222Current(names) {
		t.Fatal("historical222 boundary")
	}
	for i := range names {
		for _, part := range []int{0, 1} {
			changed := append([]string(nil), names...)
			pair := strings.SplitN(changed[i], ":", 2)
			pair[part] += "Changed"
			changed[i] = strings.Join(pair, ":")
			if credentialInferenceRegistry224Current(changed) {
				t.Fatal("changed identity admitted", i, part)
			}
		}
		omitted := append([]string(nil), names[:i]...)
		omitted = append(omitted, names[i+1:]...)
		if credentialInferenceRegistry224Current(omitted) {
			t.Fatal("omission admitted", i)
		}
		duplicate := append([]string(nil), names...)
		duplicate[i] = names[(i+1)%len(names)]
		if credentialInferenceRegistry224Current(duplicate) {
			t.Fatal("duplicate admitted", i)
		}
		if i > 0 {
			reordered := append([]string(nil), names...)
			reordered[i], reordered[i-1] = reordered[i-1], reordered[i]
			if credentialInferenceRegistry224Current(reordered) {
				t.Fatal("reorder admitted", i)
			}
		}
	}
	if credentialInferenceRegistry224Current(append(append([]string(nil), names...), "extra:testUnexpected")) {
		t.Fatal("extra tail admitted")
	}
}
