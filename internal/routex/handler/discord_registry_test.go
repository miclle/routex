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
	if !credentialInferenceRegistry224Current(names) {
		t.Fatal("exact222 Insights successor required before historical221 checks")
	}
	names = names[:221]
	if !telegramRegistry221Current(names) {
		t.Fatal("exact214 forced SSO successor required before historical210 checks")
	}
	names = names[:210] // Preserve the exact predecessor guard and its historical controls.
	if !apiKeyDeliveryRegistry210Current(names) || !strings.Contains(string(raw), "versions != 109") {
		t.Fatal("current V102/210 registry")
	}
	names = names[:206] // Preserve every exact historical V101 registry control below.
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
