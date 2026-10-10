package handler

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Every historical prefix predicate remains literal and independent.
func githubRegistry197Current(names []string) bool {
	return len(names) == 197 && names[194] == "github_migration:testGitHubMigration" && names[195] == "github:testGitHubLifecycle" && names[196] == "github_restart:testGitHubProcessRestart" && samlRegistry194Current(names[:194])
}
func TestGitHubRegistry197ExactPrefixAndSuffix(t *testing.T) {
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
	names = names[:197] // Exercise the unchanged historical V98 predicate below.
	if !samlRegistry194Current(names[:194]) || githubRegistry197Current(names[:194]) {
		t.Fatal("historical194 prefix/successor boundary")
	}
	for i := range names {
		for _, part := range []int{0, 1} {
			changed := append([]string(nil), names...)
			pair := strings.Split(changed[i], ":")
			pair[part] += "Changed"
			changed[i] = strings.Join(pair, ":")
			if githubRegistry197Current(changed) {
				t.Fatalf("changed identity accepted %d/%d", i, part)
			}
		}
		omitted := append([]string(nil), names[:i]...)
		omitted = append(omitted, names[i+1:]...)
		if githubRegistry197Current(omitted) {
			t.Fatalf("omission accepted %d", i)
		}
		changed := append([]string(nil), names...)
		changed[i] = names[(i+1)%len(names)]
		if githubRegistry197Current(changed) {
			t.Fatalf("duplicate accepted %d", i)
		}
		if i+1 < len(names) {
			changed = append([]string(nil), names...)
			changed[i], changed[i+1] = changed[i+1], changed[i]
			if githubRegistry197Current(changed) {
				t.Fatalf("reorder accepted %d", i)
			}
		}
	}
	if githubRegistry197Current(append(append([]string(nil), names...), "extra:testUnreviewed")) {
		t.Fatal("extra case accepted")
	}
}
