package handler

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func googleRegistry200Current(names []string) bool {
	return len(names) == 200 && names[197] == "google_migration:testGoogleMigration" && names[198] == "google:testGoogleLifecycle" && names[199] == "google_restart:testGoogleProcessRestart" && githubRegistry197Current(names[:197])
}
func TestGoogleRegistry200ExactPrefixAndSuffix(t *testing.T) {
	raw, err := os.ReadFile("auth_integration_test.go")
	if err != nil {
		t.Fatal(err)
	}
	matches := regexp.MustCompile(`\{"([a-z_]+)", (test[A-Za-z0-9]+)\}`).FindAllStringSubmatch(string(raw), -1)
	var names []string
	for _, m := range matches {
		names = append(names, m[1]+":"+m[2])
	}
	if !installationRegistry203Current(names) || !strings.Contains(string(raw), "versions != 100") {
		t.Fatal("exact V100/203 current registry")
	}
	names = names[:200] // Exercise every unchanged historical V99 control below.
	if !githubRegistry197Current(names[:197]) || googleRegistry200Current(names[:197]) {
		t.Fatal("historical197 boundary")
	}
	for i := range names {
		for _, part := range []int{0, 1} {
			changed := append([]string(nil), names...)
			pair := strings.Split(changed[i], ":")
			pair[part] += "Changed"
			changed[i] = strings.Join(pair, ":")
			if googleRegistry200Current(changed) {
				t.Fatalf("changed identity accepted %d/%d", i, part)
			}
		}
		omitted := append([]string(nil), names[:i]...)
		omitted = append(omitted, names[i+1:]...)
		if googleRegistry200Current(omitted) {
			t.Fatalf("omission accepted %d", i)
		}
		changed := append([]string(nil), names...)
		changed[i] = names[(i+1)%len(names)]
		if googleRegistry200Current(changed) {
			t.Fatalf("duplicate accepted %d", i)
		}
		if i+1 < len(names) {
			changed = append([]string(nil), names...)
			changed[i], changed[i+1] = changed[i+1], changed[i]
			if googleRegistry200Current(changed) {
				t.Fatalf("reorder accepted %d", i)
			}
		}
	}
	if googleRegistry200Current(append(append([]string(nil), names...), "extra:testUnreviewed")) {
		t.Fatal("extra accepted")
	}
}
