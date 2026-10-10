package service

import (
	"reflect"
	"testing"
)

func TestOAuthRootInventoryPreservesHistoricalVersionPrefixes(t *testing.T) {
	want := []string{"provider_credentials", "egresses", "smtp_settings", "storage_revisions", "user_mfa", "vault_writer_auth", "vault_reader_auth", "oidc_providers", "oauth_providers"}
	if rootInventoryVersion != 4 || !reflect.DeepEqual(rootDomains, want) {
		t.Fatal("current root domain contract")
	}
	for v, n := range map[int]int{1: 5, 2: 7, 3: 8, 4: 9} {
		if !reflect.DeepEqual(rootInventoryDomains(v), want[:n]) {
			t.Fatal("historical inventory reinterpreted", v)
		}
	}
	for _, v := range []int{-1, 0, 5} {
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
}
