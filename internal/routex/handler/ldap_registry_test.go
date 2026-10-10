package handler

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Preserve the exact188 historical predicate and bind every appended LDAP
// scenario name and function before selecting any historical prefix.
func ldapRegistry191Current(names []string) bool {
	return len(names) == 191 &&
		names[188] == "ldap_migration:testLDAPMigration" &&
		names[189] == "ldap:testLDAPLifecycle" &&
		names[190] == "ldap_restart:testLDAPProcessRestart" &&
		oauthRegistry188Current(names[:188])
}

func TestLDAPRegistry191Preserves188AndRejectsIdentityDrift(t *testing.T) {
	raw, err := os.ReadFile("auth_integration_test.go")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, match := range regexp.MustCompile(`\{"([^"\n]+)", (test[A-Za-z0-9]+)\}`).FindAllStringSubmatch(string(raw), -1) {
		names = append(names, match[1]+":"+match[2])
	}
	if !installationRegistry203Current(names) {
		t.Fatal("exact194 ordered SAML successor required")
	}
	names = names[:191]
	if !ldapRegistry191Current(names) {
		t.Fatal("exact191 ordered LDAP successor required")
	}
	if !oauthRegistry188Current(names[:188]) || ldapRegistry191Current(names[:188]) {
		t.Fatal("historical188 must remain valid without proving191")
	}
	for i, identity := range names {
		pair := strings.SplitN(identity, ":", 2)
		if len(pair) != 2 {
			t.Fatal("invalid registered identity", i)
		}
		for _, replacement := range []string{"unreviewed:" + pair[1], pair[0] + ":testUnreviewed"} {
			changed := append([]string(nil), names...)
			changed[i] = replacement
			if ldapRegistry191Current(changed) {
				t.Fatal("changed scenario name or function accepted", i)
			}
		}
		omitted := append(append([]string(nil), names[:i]...), names[i+1:]...)
		if ldapRegistry191Current(omitted) {
			t.Fatal("omitted identity accepted", i)
		}
		duplicate := append(append([]string(nil), names[:i]...), names[i])
		duplicate = append(duplicate, names[i:]...)
		if ldapRegistry191Current(duplicate) {
			t.Fatal("extra duplicate identity accepted", i)
		}
		if i+1 < len(names) {
			changed := append([]string(nil), names...)
			changed[i], changed[i+1] = changed[i+1], changed[i]
			if ldapRegistry191Current(changed) {
				t.Fatal("reordered identities accepted", i)
			}
			changed = append([]string(nil), names...)
			changed[i] = names[i+1]
			if ldapRegistry191Current(changed) {
				t.Fatal("same-length duplicate identity accepted", i)
			}
		}
	}
	if ldapRegistry191Current(append(append([]string(nil), names...), "extra:testUnreviewed")) {
		t.Fatal("extra identity accepted")
	}
}

// Preserve the exact191 historical prefix and all three SAML scenario identities.
func samlRegistry194Current(names []string) bool {
	return len(names) == 194 && names[191] == "saml_migration:testSAMLMigration" && names[192] == "saml:testSAMLLifecycle" && names[193] == "saml_restart:testSAMLProcessRestart" && ldapRegistry191Current(names[:191])
}

func TestSAMLRegistry194Preserves191AndRejectsIdentityDrift(t *testing.T) {
	raw, err := os.ReadFile("auth_integration_test.go")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, match := range regexp.MustCompile(`\{"([^"\n]+)", (test[A-Za-z0-9]+)\}`).FindAllStringSubmatch(string(raw), -1) {
		names = append(names, match[1]+":"+match[2])
	}
	if !installationRegistry203Current(names) {
		t.Fatal("exact194 ordered SAML successor required")
	}
	names = names[:194]
	if !ldapRegistry191Current(names[:191]) || samlRegistry194Current(names[:191]) {
		t.Fatal("historical191 must remain valid without proving194")
	}
	for i, identity := range names {
		pair := strings.SplitN(identity, ":", 2)
		if len(pair) != 2 {
			t.Fatal("invalid registered identity", i)
		}
		for _, replacement := range []string{"unreviewed:" + pair[1], pair[0] + ":testUnreviewed"} {
			changed := append([]string(nil), names...)
			changed[i] = replacement
			if samlRegistry194Current(changed) {
				t.Fatal("changed scenario name or function accepted", i)
			}
		}
		omitted := append(append([]string(nil), names[:i]...), names[i+1:]...)
		if samlRegistry194Current(omitted) {
			t.Fatal("omitted identity accepted", i)
		}
		duplicate := append(append([]string(nil), names[:i]...), names[i])
		duplicate = append(duplicate, names[i:]...)
		if samlRegistry194Current(duplicate) {
			t.Fatal("extra duplicate identity accepted", i)
		}
		if i+1 < len(names) {
			changed := append([]string(nil), names...)
			changed[i], changed[i+1] = changed[i+1], changed[i]
			if samlRegistry194Current(changed) {
				t.Fatal("reordered identities accepted", i)
			}
			changed = append([]string(nil), names...)
			changed[i] = names[i+1]
			if samlRegistry194Current(changed) {
				t.Fatal("same-length duplicate identity accepted", i)
			}
		}
	}
	if samlRegistry194Current(append(append([]string(nil), names...), "extra:testUnreviewed")) {
		t.Fatal("extra identity accepted")
	}
}
