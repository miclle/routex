package handler

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Discovery appends one scenario without changing the exact historical226 prefix.
func apiKeyDeliveryControllerDiscoveryRegistry227Current(names []string) bool {
	return len(names) == 227 && names[226] == "api_key_delivery_controller_discovery:testAPIKeyDeliveryControllerDiscovery" && apiKeyDeliveryControllerRegistry226Current(names[:226])
}

func TestAPIKeyDeliveryControllerDiscoveryRegistry227ExactAppend(t *testing.T) {
	raw, err := os.ReadFile("auth_integration_test.go")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, match := range regexp.MustCompile(`\{"([a-z0-9_]+)", (test[A-Za-z0-9]+)\}`).FindAllStringSubmatch(string(raw), -1) {
		names = append(names, match[1]+":"+match[2])
	}
	if !apiKeyDeliveryControllerDiscoveryRegistry227Current(names) || !strings.Contains(string(raw), "versions != 118") {
		t.Fatal("exact V118/227 discovery registry required")
	}
	if !apiKeyDeliveryControllerRegistry226Current(names[:226]) || apiKeyDeliveryControllerRegistry226Current(names) || apiKeyDeliveryControllerDiscoveryRegistry227Current(names[:226]) {
		t.Fatal("historical226 boundary changed")
	}
	for i := range names {
		for _, part := range []int{0, 1} {
			changed := append([]string(nil), names...)
			pair := strings.SplitN(changed[i], ":", 2)
			pair[part] += "Changed"
			changed[i] = strings.Join(pair, ":")
			if apiKeyDeliveryControllerDiscoveryRegistry227Current(changed) {
				t.Fatal("changed identity admitted", i, part)
			}
		}
		omitted := append([]string(nil), names[:i]...)
		omitted = append(omitted, names[i+1:]...)
		if apiKeyDeliveryControllerDiscoveryRegistry227Current(omitted) {
			t.Fatal("omission admitted", i)
		}
		duplicate := append([]string(nil), names...)
		duplicate[i] = names[(i+1)%len(names)]
		if apiKeyDeliveryControllerDiscoveryRegistry227Current(duplicate) {
			t.Fatal("duplicate admitted", i)
		}
		if i > 0 {
			reordered := append([]string(nil), names...)
			reordered[i], reordered[i-1] = reordered[i-1], reordered[i]
			if apiKeyDeliveryControllerDiscoveryRegistry227Current(reordered) {
				t.Fatal("reordering admitted", i)
			}
		}
	}
	if apiKeyDeliveryControllerDiscoveryRegistry227Current(append(append([]string(nil), names...), "extra:testUnexpected")) {
		t.Fatal("extra tail admitted")
	}
}
