package service

import (
	"reflect"
	"testing"
)

func TestOAuthRootInventoryPreservesHistoricalVersionPrefixes(t *testing.T) {
	want := []string{"provider_credentials", "egresses", "smtp_settings", "storage_revisions", "user_mfa", "vault_writer_auth", "vault_reader_auth", "oidc_providers", "oauth_providers"}
	if rootInventoryVersion != 5 || !reflect.DeepEqual(rootDomains, append(append([]string(nil), want...), "ldap_providers")) {
		t.Fatal("current root domain contract")
	}
	for v, n := range map[int]int{1: 5, 2: 7, 3: 8, 4: 9} {
		if !reflect.DeepEqual(rootInventoryDomains(v), want[:n]) {
			t.Fatal("historical inventory reinterpreted", v)
		}
	}
	if !reflect.DeepEqual(rootInventoryDomains(5), rootDomains) {
		t.Fatal("current LDAP inventory omitted a domain")
	}
	for _, v := range []int{-1, 0, 6} {
		if rootInventoryDomains(v) != nil {
			t.Fatal("unsupported root inventory", v)
		}
	}
	spec, e := rootSpec("oauth_providers")
	if e != nil || spec.table != "oauth_providers" || spec.id != "id" || spec.generation != "secret_generation" || spec.ciphertext != "auth_ciphertext" {
		t.Fatal("OAuth inventory projection")
	}
	if rootReference("oauth_providers", "oauth", "generation") != "oauth:oauth:generation" || rootReference("oidc_providers", "oidc", "generation") != "oidc:oidc:generation" {
		t.Fatal("independent secret references")
	}
	spec, e = rootSpec("ldap_providers")
	if e != nil || spec.table != "ldap_providers" || spec.id != "id" || spec.generation != "secret_generation" || spec.ciphertext != "auth_ciphertext" || rootReference("ldap_providers", "ldap", "generation") != "ldap:ldap:generation" {
		t.Fatal("independent LDAP inventory projection/reference")
	}
}
