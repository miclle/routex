package handler

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func discordRegistry206Current(names []string) bool {
	return len(names) == 206 && names[203] == "discord_migration:testDiscordMigration" && names[204] == "discord:testDiscordLifecycle" && names[205] == "discord_restart:testDiscordProcessRestart" && installationRegistry203Current(names[:203])
}
func TestDiscordRegistry206ExactPrefixAndSuffix(t *testing.T) {
	raw, err := os.ReadFile("auth_integration_test.go")
	if err != nil {
		t.Fatal(err)
	}
	matches := regexp.MustCompile(`\{"([a-z_]+)", (test[A-Za-z0-9]+)\}`).FindAllStringSubmatch(string(raw), -1)
	names := make([]string, len(matches))
	for i, m := range matches {
		names[i] = m[1] + ":" + m[2]
	}
	if !discordRegistry206Current(names) || !strings.Contains(string(raw), "versions != 101") {
		t.Fatal("exact V101/206 registry")
	}
	if !installationRegistry203Current(names[:203]) || discordRegistry206Current(names[:203]) {
		t.Fatal("historical203 boundary")
	}
	for i := range names {
		for _, part := range []int{0, 1} {
			changed := append([]string(nil), names...)
			pair := strings.SplitN(changed[i], ":", 2)
			pair[part] += "Changed"
			changed[i] = strings.Join(pair, ":")
			if discordRegistry206Current(changed) {
				t.Fatal("changed identity accepted", i, part)
			}
		}
		missing := append([]string(nil), names[:i]...)
		missing = append(missing, names[i+1:]...)
		if discordRegistry206Current(missing) {
			t.Fatal("omission accepted", i)
		}
		changed := append([]string(nil), names...)
		changed[i] = names[(i+1)%len(names)]
		if discordRegistry206Current(changed) {
			t.Fatal("duplicate accepted", i)
		}
		if i > 0 {
			changed = append([]string(nil), names...)
			changed[i], changed[i-1] = changed[i-1], changed[i]
			if discordRegistry206Current(changed) {
				t.Fatal("reorder accepted", i)
			}
		}
	}
	if discordRegistry206Current(append(append([]string(nil), names...), "extra:testUnexpected")) {
		t.Fatal("extra tail accepted")
	}
}
