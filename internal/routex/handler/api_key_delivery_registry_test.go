package handler

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func apiKeyDeliveryRegistry209Current(names []string) bool {
	return len(names) == 209 && names[206] == "api_key_delivery:testAPIKeyDeliveryLifecycle" && names[207] == "api_key_delivery_recovery:testAPIKeyDeliveryRecovery" && names[208] == "api_key_delivery_migration:testAPIKeyDeliveryMigration" && discordRegistry206Current(names[:206])
}
func TestAPIKeyDeliveryRegistry209ExactPrefixAndSuffix(t *testing.T) {
	raw, err := os.ReadFile("auth_integration_test.go")
	if err != nil {
		t.Fatal(err)
	}
	matches := regexp.MustCompile(`\{"([a-z_]+)", (test[A-Za-z0-9]+)\}`).FindAllStringSubmatch(string(raw), -1)
	names := make([]string, len(matches))
	for i, m := range matches {
		names[i] = m[1] + ":" + m[2]
	}
	if !enterpriseRegistry218Current(names) {
		t.Fatal("exact214 forced SSO successor required before historical210 checks")
	}
	names = names[:210] // Preserve the exact predecessor guard and its historical controls.
	if !apiKeyDeliveryRegistry210Current(names) {
		t.Fatal("exact210 delivery restart successor required before historical209 checks")
	}
	names = names[:209] // Preserve every original V102/209 matcher mutation and assertion below.
	if !apiKeyDeliveryRegistry209Current(names) || !strings.Contains(string(raw), "versions != 105") {
		t.Fatal("exact V102/209 registry")
	}
	if !discordRegistry206Current(names[:206]) || apiKeyDeliveryRegistry209Current(names[:206]) {
		t.Fatal("historical206 boundary")
	}
	for i := range names {
		for _, part := range []int{0, 1} {
			changed := append([]string(nil), names...)
			pair := strings.SplitN(changed[i], ":", 2)
			pair[part] += "Changed"
			changed[i] = strings.Join(pair, ":")
			if apiKeyDeliveryRegistry209Current(changed) {
				t.Fatal("changed identity accepted", i, part)
			}
		}
		missing := append([]string(nil), names[:i]...)
		missing = append(missing, names[i+1:]...)
		if apiKeyDeliveryRegistry209Current(missing) {
			t.Fatal("omission accepted", i)
		}
		changed := append([]string(nil), names...)
		changed[i] = names[(i+1)%len(names)]
		if apiKeyDeliveryRegistry209Current(changed) {
			t.Fatal("duplicate accepted", i)
		}
		if i > 0 {
			changed = append([]string(nil), names...)
			changed[i], changed[i-1] = changed[i-1], changed[i]
			if apiKeyDeliveryRegistry209Current(changed) {
				t.Fatal("reorder accepted", i)
			}
		}
	}
	if apiKeyDeliveryRegistry209Current(append(append([]string(nil), names...), "extra:testUnexpected")) {
		t.Fatal("extra tail accepted")
	}
}

func apiKeyDeliveryRegistry210Current(names []string) bool {
	return len(names) == 210 && names[209] == "api_key_delivery_restart:testAPIKeyDeliveryProcessRestart" && apiKeyDeliveryRegistry209Current(names[:209])
}
func TestAPIKeyDeliveryRegistry210ExactPrefixAndSuffix(t *testing.T) {
	raw, err := os.ReadFile("auth_integration_test.go")
	if err != nil {
		t.Fatal(err)
	}
	matches := regexp.MustCompile(`\{"([a-z_]+)", (test[A-Za-z0-9]+)\}`).FindAllStringSubmatch(string(raw), -1)
	names := make([]string, len(matches))
	for i, m := range matches {
		names[i] = m[1] + ":" + m[2]
	}
	if !enterpriseRegistry218Current(names) {
		t.Fatal("exact214 forced SSO successor required before historical210 checks")
	}
	names = names[:210] // Preserve the exact predecessor guard and its historical controls.
	if !apiKeyDeliveryRegistry210Current(names) || !strings.Contains(string(raw), "versions != 105") {
		t.Fatal("exact V102/210 registry")
	}
	if !apiKeyDeliveryRegistry209Current(names[:209]) || apiKeyDeliveryRegistry210Current(names[:209]) || apiKeyDeliveryRegistry209Current(names) {
		t.Fatal("historical209 prefix/successor boundary")
	}
	for i := range names {
		for _, part := range []int{0, 1} {
			changed := append([]string(nil), names...)
			pair := strings.SplitN(changed[i], ":", 2)
			pair[part] += "Changed"
			changed[i] = strings.Join(pair, ":")
			if apiKeyDeliveryRegistry210Current(changed) {
				t.Fatal("changed identity accepted", i, part)
			}
		}
		missing := append([]string(nil), names[:i]...)
		missing = append(missing, names[i+1:]...)
		if apiKeyDeliveryRegistry210Current(missing) {
			t.Fatal("omission accepted", i)
		}
		changed := append([]string(nil), names...)
		changed[i] = names[(i+1)%len(names)]
		if apiKeyDeliveryRegistry210Current(changed) {
			t.Fatal("duplicate accepted", i)
		}
		if i > 0 {
			changed = append([]string(nil), names...)
			changed[i], changed[i-1] = changed[i-1], changed[i]
			if apiKeyDeliveryRegistry210Current(changed) {
				t.Fatal("reorder accepted", i)
			}
		}
	}
	if apiKeyDeliveryRegistry210Current(append(append([]string(nil), names...), "extra:testUnexpected")) {
		t.Fatal("extra tail accepted")
	}
}
