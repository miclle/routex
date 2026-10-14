package handler

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func apiKeyDeliveryControllerRegistry226Current(names []string) bool {
	return len(names) == 226 && names[224] == "api_key_delivery_controller:testAPIKeyDeliveryController" && names[225] == "api_key_delivery_controller_migration:testAPIKeyDeliveryControllerMigration" && credentialInferenceRegistry224Current(names[:224])
}

func TestAPIKeyDeliveryControllerRegistry226ExactAppend(t *testing.T) {
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
		t.Fatal("exact227 controller discovery successor required before historical226 checks")
	}
	names = names[:226]
	if !apiKeyDeliveryControllerRegistry226Current(names) || !strings.Contains(string(raw), "versions != 116") {
		t.Fatal("exact V114/226 registry required")
	}
	if !credentialInferenceRegistry224Current(names[:224]) || credentialInferenceRegistry224Current(names) || apiKeyDeliveryControllerRegistry226Current(names[:224]) {
		t.Fatal("historical224 retained")
	}
	for i := range names {
		for _, part := range []int{0, 1} {
			changed := append([]string(nil), names...)
			pair := strings.SplitN(changed[i], ":", 2)
			pair[part] += "Changed"
			changed[i] = strings.Join(pair, ":")
			if apiKeyDeliveryControllerRegistry226Current(changed) {
				t.Fatal("changed identity admitted", i, part)
			}
		}
		omitted := append([]string(nil), names[:i]...)
		omitted = append(omitted, names[i+1:]...)
		if apiKeyDeliveryControllerRegistry226Current(omitted) {
			t.Fatal("omission admitted", i)
		}
		duplicate := append([]string(nil), names...)
		duplicate[i] = names[(i+1)%len(names)]
		if apiKeyDeliveryControllerRegistry226Current(duplicate) {
			t.Fatal("duplicate admitted", i)
		}
		if i > 0 {
			reordered := append([]string(nil), names...)
			reordered[i], reordered[i-1] = reordered[i-1], reordered[i]
			if apiKeyDeliveryControllerRegistry226Current(reordered) {
				t.Fatal("reordering admitted", i)
			}
		}
	}
	if apiKeyDeliveryControllerRegistry226Current(append(append([]string(nil), names...), "extra:testUnexpected")) {
		t.Fatal("extra tail admitted")
	}
}
