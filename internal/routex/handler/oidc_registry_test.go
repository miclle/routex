package handler

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The exact182 predicate remains historical and unchanged. Current185 binds
// every appended OIDC scenario name and function before selecting that prefix.
func oidcRegistry185Current(names []string) bool {
	return len(names) == 185 &&
		names[182] == "oidc_migration:testOIDCMigration" &&
		names[183] == "oidc:testOIDCLifecycle" &&
		names[184] == "oidc_restart:testOIDCProcessRestart" &&
		adminModelConfiguredReadinessRegistry182Current(names[:182])
}

func TestOIDCRegistry185Preserves182AndRejectsIdentityDrift(t *testing.T) {
	raw, err := os.ReadFile("auth_integration_test.go")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, match := range regexp.MustCompile(`\{"([^"\n]+)", (test[A-Za-z0-9]+)\}`).FindAllStringSubmatch(string(raw), -1) {
		names = append(names, match[1]+":"+match[2])
	}
	if !samlRegistry194Current(names) {
		t.Fatal("exact194 ordered SAML successor required")
	}
	names = names[:185]
	if !oidcRegistry185Current(names) {
		t.Fatal("exact185 ordered OIDC predecessor required")
	}
	if !adminModelConfiguredReadinessRegistry182Current(names[:182]) || oidcRegistry185Current(names[:182]) {
		t.Fatal("historical182 must remain valid without proving185")
	}
	for i, identity := range names {
		pair := strings.SplitN(identity, ":", 2)
		if len(pair) != 2 {
			t.Fatal("invalid registered identity", i)
		}
		for _, replacement := range []string{"unreviewed:" + pair[1], pair[0] + ":testUnreviewed"} {
			changed := append([]string(nil), names...)
			changed[i] = replacement
			if oidcRegistry185Current(changed) {
				t.Fatal("changed scenario name or function accepted", i)
			}
		}
		omitted := append(append([]string(nil), names[:i]...), names[i+1:]...)
		if oidcRegistry185Current(omitted) {
			t.Fatal("omitted identity accepted", i)
		}
		duplicate := append(append([]string(nil), names[:i]...), names[i])
		duplicate = append(duplicate, names[i:]...)
		if oidcRegistry185Current(duplicate) {
			t.Fatal("extra duplicate identity accepted", i)
		}
		if i+1 < len(names) {
			changed := append([]string(nil), names...)
			changed[i], changed[i+1] = changed[i+1], changed[i]
			if oidcRegistry185Current(changed) {
				t.Fatal("reordered identities accepted", i)
			}
			changed = append([]string(nil), names...)
			changed[i] = names[i+1]
			if oidcRegistry185Current(changed) {
				t.Fatal("same-length duplicate identity accepted", i)
			}
		}
	}
	if oidcRegistry185Current(append(append([]string(nil), names...), "extra:testUnreviewed")) {
		t.Fatal("extra identity accepted")
	}
}
