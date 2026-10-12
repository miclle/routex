package handler

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func telegramRegistry221Current(names []string) bool {
	return len(names) == 221 && names[218] == "telegram_migration:testTelegramMigration" && names[219] == "telegram:testTelegramLifecycle" && names[220] == "telegram_restart:testTelegramProcessRestart" && enterpriseRegistry218Current(names[:218])
}

func TestTelegramRegistry221ExactPrefixAndSuffix(t *testing.T) {
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
		t.Fatal("exact222 Insights successor required before historical221 checks")
	}
	names = names[:221]
	if !telegramRegistry221Current(names) || !strings.Contains(string(raw), "versions != 109") {
		t.Fatal("exact 221 Telegram prefix and V107 ledger required")
	}
	if !enterpriseRegistry218Current(names[:218]) || telegramRegistry221Current(names[:218]) || enterpriseRegistry218Current(names) {
		t.Fatal("historical218 boundary")
	}
	for i := range names {
		for _, part := range []int{0, 1} {
			changed := append([]string(nil), names...)
			pair := strings.SplitN(changed[i], ":", 2)
			pair[part] += "Changed"
			changed[i] = strings.Join(pair, ":")
			if telegramRegistry221Current(changed) {
				t.Fatal("changed identity admitted", i, part)
			}
		}
		omitted := append([]string(nil), names[:i]...)
		omitted = append(omitted, names[i+1:]...)
		if telegramRegistry221Current(omitted) {
			t.Fatal("omission admitted", i)
		}
		duplicate := append([]string(nil), names...)
		duplicate[i] = names[(i+1)%len(names)]
		if telegramRegistry221Current(duplicate) {
			t.Fatal("duplicate admitted", i)
		}
		if i > 0 {
			reordered := append([]string(nil), names...)
			reordered[i], reordered[i-1] = reordered[i-1], reordered[i]
			if telegramRegistry221Current(reordered) {
				t.Fatal("reorder admitted", i)
			}
		}
	}
	if telegramRegistry221Current(append(append([]string(nil), names...), "extra:testUnexpected")) {
		t.Fatal("extra tail admitted")
	}
}
