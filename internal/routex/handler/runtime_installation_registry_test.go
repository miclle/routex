package handler

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func installationRegistry203Current(names []string) bool {
	return len(names) == 203 && names[200] == "runtime_installation_migration:testRuntimeInstallationMigration" && names[201] == "runtime_installation:testRuntimeInstallationLifecycle" && names[202] == "runtime_installation_restart:testRuntimeInstallationProcessRestart" && googleRegistry200Current(names[:200])
}
func TestRuntimeInstallationRegistry203ExactPrefixAndSuffix(t *testing.T) {
	raw, err := os.ReadFile("auth_integration_test.go")
	if err != nil {
		t.Fatal(err)
	}
	matches := regexp.MustCompile(`\{"([a-z_]+)", (test[A-Za-z0-9]+)\}`).FindAllStringSubmatch(string(raw), -1)
	names := make([]string, len(matches))
	for i, m := range matches {
		names[i] = m[1] + ":" + m[2]
	}
	if !installationRegistry203Current(names) || !strings.Contains(string(raw), "versions != 100") {
		t.Fatal("current V100 registry changed")
	}
	for i, want := range []string{"testRuntimeInstallationMigration", "testRuntimeInstallationLifecycle", "testRuntimeInstallationProcessRestart"} {
		if matches[200+i][2] != want {
			t.Fatal("installation helper mismatch")
		}
	}
	for i := range names {
		changed := append([]string(nil), names...)
		parts := strings.SplitN(names[i], ":", 2)
		changed[i] = "unexpected:" + parts[1]
		if installationRegistry203Current(changed) {
			t.Fatal("changed registry name accepted")
		}
		changed[i] = parts[0] + ":testUnexpected"
		if installationRegistry203Current(changed) {
			t.Fatal("changed registry function accepted")
		}
		if i > 0 {
			changed = append([]string(nil), names...)
			changed[i] = changed[i-1]
			if installationRegistry203Current(changed) {
				t.Fatal("duplicate registry entry accepted")
			}
			changed = append([]string(nil), names...)
			changed[i], changed[i-1] = changed[i-1], changed[i]
			if installationRegistry203Current(changed) {
				t.Fatal("reordered registry entry accepted")
			}
		}
		missing := append([]string(nil), names[:i]...)
		missing = append(missing, names[i+1:]...)
		if installationRegistry203Current(missing) {
			t.Fatal("missing registry entry accepted")
		}
	}
	if installationRegistry203Current(append(append([]string(nil), names...), "extra")) {
		t.Fatal("unexpected registry tail accepted")
	}
}
