package handler

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The exact185 predicate remains historical and unchanged. This historical188
// predicate binds every appended OAuth scenario name and function.
func oauthRegistry188Current(names []string) bool {
	return len(names) == 188 &&
		names[185] == "oauth_migration:testOAuthMigration" &&
		names[186] == "oauth:testOAuthLifecycle" &&
		names[187] == "oauth_restart:testOAuthProcessRestart" &&
		oidcRegistry185Current(names[:185])
}

func TestOAuthRegistry188Preserves185AndRejectsIdentityDrift(t *testing.T) {
	raw, err := os.ReadFile("auth_integration_test.go")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, match := range regexp.MustCompile(`\{"([^"\n]+)", (test[A-Za-z0-9]+)\}`).FindAllStringSubmatch(string(raw), -1) {
		names = append(names, match[1]+":"+match[2])
	}
	if !discordRegistry206Current(names) {
		t.Fatal("exact206 ordered Discord successor required")
	}
	names = names[:188]
	if !oauthRegistry188Current(names) {
		t.Fatal("historical188 prefix changed")
	}
	if !oidcRegistry185Current(names[:185]) || oauthRegistry188Current(names[:185]) {
		t.Fatal("historical185 must remain valid without proving188")
	}
	for i, identity := range names {
		pair := strings.SplitN(identity, ":", 2)
		if len(pair) != 2 {
			t.Fatal("invalid registered identity", i)
		}
		for _, replacement := range []string{"unreviewed:" + pair[1], pair[0] + ":testUnreviewed"} {
			changed := append([]string(nil), names...)
			changed[i] = replacement
			if oauthRegistry188Current(changed) {
				t.Fatal("changed scenario name or function accepted", i)
			}
		}
		omitted := append(append([]string(nil), names[:i]...), names[i+1:]...)
		if oauthRegistry188Current(omitted) {
			t.Fatal("omitted identity accepted", i)
		}
		duplicate := append(append([]string(nil), names[:i]...), names[i])
		duplicate = append(duplicate, names[i:]...)
		if oauthRegistry188Current(duplicate) {
			t.Fatal("extra duplicate identity accepted", i)
		}
		if i+1 < len(names) {
			changed := append([]string(nil), names...)
			changed[i], changed[i+1] = changed[i+1], changed[i]
			if oauthRegistry188Current(changed) {
				t.Fatal("reordered identities accepted", i)
			}
			changed = append([]string(nil), names...)
			changed[i] = names[i+1]
			if oauthRegistry188Current(changed) {
				t.Fatal("same-length duplicate identity accepted", i)
			}
		}
	}
	if oauthRegistry188Current(append(append([]string(nil), names...), "extra:testUnreviewed")) {
		t.Fatal("extra identity accepted")
	}
}
