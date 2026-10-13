package handler

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func insightsRegistry222Current(names []string) bool {
	return len(names) == 222 && names[221] == "insights_ai:testInsightsAIIntegration" && telegramRegistry221Current(names[:221])
}

func TestInsightsRegistry222ExactPrefixAndSuffix(t *testing.T) {
	raw, err := os.ReadFile("auth_integration_test.go")
	if err != nil {
		t.Fatal(err)
	}
	matches := regexp.MustCompile(`\{"([a-z0-9_]+)", (test[A-Za-z0-9]+)\}`).FindAllStringSubmatch(string(raw), -1)
	names := make([]string, len(matches))
	for i, m := range matches {
		names[i] = m[1] + ":" + m[2]
	}
	if !credentialInferenceRegistry224Current(names) {
		t.Fatal("exact224 Credential observer successor required")
	}
	names = names[:222]
	if !insightsRegistry222Current(names) || !strings.Contains(string(raw), "versions != 111") {
		t.Fatal("exact V107/222 Insights registry required")
	}
	if !telegramRegistry221Current(names[:221]) || insightsRegistry222Current(names[:221]) || telegramRegistry221Current(names) {
		t.Fatal("historical221 boundary")
	}
	for i := range names {
		for _, part := range []int{0, 1} {
			changed := append([]string(nil), names...)
			pair := strings.SplitN(changed[i], ":", 2)
			pair[part] += "Changed"
			changed[i] = strings.Join(pair, ":")
			if insightsRegistry222Current(changed) {
				t.Fatal("changed identity admitted", i, part)
			}
		}
		omitted := append([]string(nil), names[:i]...)
		omitted = append(omitted, names[i+1:]...)
		if insightsRegistry222Current(omitted) {
			t.Fatal("omission admitted", i)
		}
		duplicate := append([]string(nil), names...)
		duplicate[i] = names[(i+1)%len(names)]
		if insightsRegistry222Current(duplicate) {
			t.Fatal("duplicate admitted", i)
		}
		if i > 0 {
			reordered := append([]string(nil), names...)
			reordered[i], reordered[i-1] = reordered[i-1], reordered[i]
			if insightsRegistry222Current(reordered) {
				t.Fatal("reorder admitted", i)
			}
		}
	}
	if insightsRegistry222Current(append(append([]string(nil), names...), "extra:testUnexpected")) {
		t.Fatal("extra tail admitted")
	}
	// The current pair must not displace any historical usage or later case.
	for i, pair := range names[:221] {
		if pair != "usage_freshness:testUsageFreshnessLifecycle" {
			continue
		}
		misplaced := append([]string(nil), names[:i+1]...)
		misplaced = append(misplaced, names[221])
		misplaced = append(misplaced, names[i+1:221]...)
		if insightsRegistry222Current(misplaced) {
			t.Fatal("middle insertion admitted")
		}
		return
	}
	t.Fatal("historical usage-freshness case missing")
}
