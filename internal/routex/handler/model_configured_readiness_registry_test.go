package handler

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The exact181 predicate remains historical and unchanged.
func adminModelConfiguredReadinessRegistry182Current(names []string) bool {
	return len(names) == 182 &&
		names[181] == "admin_model_configured_readiness:testAdminModelConfiguredReadiness" &&
		credentialAttemptStatisticsRegistry181Current(names[:181])
}

func TestAdminModelConfiguredReadinessRegistry182Preserves181AndRejectsSuffixDrift(t *testing.T) {
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
	names = names[:182]
	if !adminModelConfiguredReadinessRegistry182Current(names) || !strings.Contains(string(raw), "versions != 94") {
		t.Fatal("exact182 and unchanged migration93 required")
	}
	if adminModelConfiguredReadinessRegistry182Current(names[:181]) || !credentialAttemptStatisticsRegistry181Current(names[:181]) {
		t.Fatal("historical181 must remain valid without proving182")
	}
	for i := range names {
		changed := append([]string(nil), names...)
		changed[i] = "unreviewed:replacement"
		if adminModelConfiguredReadinessRegistry182Current(changed) {
			t.Fatal("changed historical or current identity accepted", i)
		}
	}
	for _, changed := range [][]string{
		names[:181],
		append(append([]string(nil), names...), "extra:unreviewed"),
		append(append([]string(nil), names[:180]...), names[181], names[180]),
		append(append([]string(nil), names[:181]...), names[180]),
	} {
		if adminModelConfiguredReadinessRegistry182Current(changed) {
			t.Fatal("omitted, extra, reordered or duplicate suffix accepted")
		}
	}
}
