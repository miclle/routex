package handler

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Historical177/179 predicates remain immutable. Current181 additionally binds
// both new scenario names and functions before selecting the released prefix.
func credentialAttemptStatisticsRegistry181Current(names []string) bool {
	return len(names) == 181 &&
		names[179] == "credential_attempt_statistics_migration:testCredentialAttemptStatisticsMigration" &&
		names[180] == "credential_attempt_statistics:testCredentialAttemptStatistics" &&
		connectionTransportRegistry179Current(names[:179])
}

func TestCredentialAttemptStatisticsRegistry181Preserves179AndRejectsSuffixDrift(t *testing.T) {
	raw, err := os.ReadFile("auth_integration_test.go")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, match := range regexp.MustCompile(`\{"([^"\n]+)", (test[A-Za-z0-9]+)\}`).FindAllStringSubmatch(string(raw), -1) {
		names = append(names, match[1]+":"+match[2])
	}
	if !oidcRegistry185Current(names) {
		t.Fatal("exact185 OIDC successor changed")
	}
	names = names[:181]
	if !credentialAttemptStatisticsRegistry181Current(names) || !strings.Contains(string(raw), "versions != 94") {
		t.Fatal("exact181/V93 required")
	}
	if !connectionTransportRegistry179Current(names[:179]) || credentialAttemptStatisticsRegistry181Current(names[:179]) {
		t.Fatal("released179 prefix must not prove181")
	}
	for i := range names {
		changed := append([]string(nil), names...)
		changed[i] = "unreviewed:replacement"
		if credentialAttemptStatisticsRegistry181Current(changed) {
			t.Fatal("changed identity accepted", i)
		}
	}
	for _, changed := range [][]string{
		names[:180], append(append([]string(nil), names...), "extra:unreviewed"),
		append(append([]string(nil), names[:179]...), names[180], names[179]),
		append(append([]string(nil), names[:179]...), names[179], names[179]),
	} {
		if credentialAttemptStatisticsRegistry181Current(changed) {
			t.Fatal("omitted, extra, reordered or duplicate suffix accepted")
		}
	}
}
