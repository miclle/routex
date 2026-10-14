package service

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

func deliveryAuthRootFixture() entity.APIKeyDeliveryAuth {
	birth := time.Date(2026, 10, 10, 1, 2, 3, 123456000, time.UTC)
	return entity.APIKeyDeliveryAuth{ID: "dau_fixture", Purpose: entity.APIKeyDeliveryWriter, IntegrationID: "vlt_fixture", IntegrationBirth: birth, IntegrationRevision: strings.Repeat("a", 64), VaultRevisionID: "vlr_fixture", VaultRevisionBirth: birth.Add(time.Microsecond), Method: "token", SecretGeneration: "gen_fixture", CreatedAt: birth.Add(2 * time.Microsecond)}
}
func TestAPIKeyDeliveryRootAADBindsExactPurposeAndArchivedTuple(t *testing.T) {
	auth := deliveryAuthRootFixture()
	reference, err := apiKeyDeliveryAuthReference(auth)
	if err != nil || len(reference) > 160 || !strings.HasPrefix(reference, "api-key-delivery:v1:writer:") {
		t.Fatal("bounded typed reference")
	}
	store, err := secretstore.New(bytes.Repeat([]byte{37}, 32))
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := store.Seal(reference, "unit-fixture-token")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := store.Open(reference, ciphertext)
	if err != nil || plain != "unit-fixture-token" {
		t.Fatal("baseline failed")
	}
	for _, tc := range []struct {
		name   string
		change func(*entity.APIKeyDeliveryAuth)
	}{
		{"purpose", func(v *entity.APIKeyDeliveryAuth) { v.Purpose = entity.APIKeyDeliveryProvisioner }},
		{"id", func(v *entity.APIKeyDeliveryAuth) { v.ID += "_other" }},
		{"birth", func(v *entity.APIKeyDeliveryAuth) { v.CreatedAt = v.CreatedAt.Add(time.Microsecond) }},
		{"generation", func(v *entity.APIKeyDeliveryAuth) { v.SecretGeneration += "_other" }},
		{"integration_id", func(v *entity.APIKeyDeliveryAuth) { v.IntegrationID += "_other" }},
		{"integration_birth", func(v *entity.APIKeyDeliveryAuth) { v.IntegrationBirth = v.IntegrationBirth.Add(time.Microsecond) }},
		{"integration_revision", func(v *entity.APIKeyDeliveryAuth) { v.IntegrationRevision = strings.Repeat("b", 64) }},
		{"vault_revision_id", func(v *entity.APIKeyDeliveryAuth) { v.VaultRevisionID += "_other" }},
		{"vault_revision_birth", func(v *entity.APIKeyDeliveryAuth) { v.VaultRevisionBirth = v.VaultRevisionBirth.Add(time.Microsecond) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := auth
			tc.change(&changed)
			other, err := apiKeyDeliveryAuthReference(changed)
			if err != nil || other == reference {
				t.Fatal("tuple substitution did not change valid reference")
			}
			if _, err := store.Open(other, ciphertext); err == nil {
				t.Fatal("tuple substitution decrypted material")
			}
		})
	}
	equivalent := auth
	equivalent.CreatedAt = auth.CreatedAt.In(time.FixedZone("unit", 8*3600))
	equivalent.IntegrationBirth = auth.IntegrationBirth.In(time.FixedZone("unit", 8*3600))
	equivalent.VaultRevisionBirth = auth.VaultRevisionBirth.In(time.FixedZone("unit", 8*3600))
	got, err := apiKeyDeliveryAuthReference(equivalent)
	if err != nil || got != reference {
		t.Fatal("same exact instant changed AAD")
	}
	equivalent.AuthCiphertext = "unrelated envelope"
	got, err = apiKeyDeliveryAuthReference(equivalent)
	if err != nil || got != reference {
		t.Fatal("ciphertext used as identity")
	}
	for _, change := range []func(*entity.APIKeyDeliveryAuth){
		func(v *entity.APIKeyDeliveryAuth) { v.Purpose = "Writer" },
		func(v *entity.APIKeyDeliveryAuth) { v.Method = "approle" },
		func(v *entity.APIKeyDeliveryAuth) { v.ID = "" },
		func(v *entity.APIKeyDeliveryAuth) { v.IntegrationRevision = strings.Repeat("A", 64) },
		func(v *entity.APIKeyDeliveryAuth) { v.CreatedAt = v.CreatedAt.Add(time.Nanosecond) },
		func(v *entity.APIKeyDeliveryAuth) { v.IntegrationBirth = time.Time{} },
		func(v *entity.APIKeyDeliveryAuth) { v.VaultRevisionID = "" },
		func(v *entity.APIKeyDeliveryAuth) { v.VaultRevisionBirth = time.Time{} },
	} {
		bad := auth
		change(&bad)
		if ref, err := apiKeyDeliveryAuthReference(bad); err == nil || ref != "" {
			t.Fatal("malformed tuple admitted")
		}
	}
}

func TestAPIKeyDeliveryRootV9KeepsAllHistoricalScopes(t *testing.T) {
	expected := []string{"provider_credentials", "egresses", "smtp_settings", "storage_revisions", "user_mfa", "vault_writer_auth", "vault_reader_auth", "oidc_providers", "oauth_providers", "ldap_providers", "named_identity_providers", "api_key_delivery_auth"}
	if rootInventoryVersion != 11 || len(rootDomains) != len(expected)+1 {
		t.Fatal("current inventory")
	}
	for i, domain := range expected {
		if rootDomains[i] != domain {
			t.Fatal("domain order changed", i)
		}
	}
	for version, count := range map[int]int{1: 5, 2: 7, 3: 8, 4: 9, 5: 10, 6: 11, 7: 11, 8: 11, 9: 12} {
		got := rootInventoryDomains(version)
		if len(got) != count {
			t.Fatal("historical coverage promoted", version)
		}
		for i, domain := range got {
			if domain != expected[i] {
				t.Fatal("historical domain changed", version, i)
			}
		}
	}
	if rootInventoryDomains(12) != nil || rootInventoryDomains(0) != nil {
		t.Fatal("unknown inventory claimed")
	}
	spec, err := rootSpec("api_key_delivery_auth")
	if err != nil || spec.table != "api_key_delivery_auth" || spec.id != "id" || spec.generation != "secret_generation" || spec.ciphertext != "auth_ciphertext" {
		t.Fatal("dedicated domain mapping")
	}
	if rootReference("api_key_delivery_auth", "dau_fixture", "gen_fixture") != "" {
		t.Fatal("incomplete generic tuple produced reference")
	}
}

func TestAPIKeyDeliveryRootCASRejectsTupleRebinding(t *testing.T) {
	baseline := deliveryAuthRootFixture()
	baseline.AuthCiphertext = "captured-envelope"
	reference, err := apiKeyDeliveryAuthReference(baseline)
	if err != nil {
		t.Fatal(err)
	}
	captured := rootInventoryRow{baseline.ID, baseline.SecretGeneration, reference, baseline.AuthCiphertext}
	for _, which := range []string{"unchanged", "purpose", "birth", "integration", "revision", "vault_revision", "generation", "ciphertext", "missing", "alias"} {
		t.Run(which, func(t *testing.T) {
			current := baseline
			switch which {
			case "purpose":
				current.Purpose = entity.APIKeyDeliveryProvisioner
			case "birth":
				current.CreatedAt = current.CreatedAt.Add(time.Microsecond)
			case "integration":
				current.IntegrationBirth = current.IntegrationBirth.Add(time.Microsecond)
			case "revision":
				current.IntegrationRevision = strings.Repeat("b", 64)
			case "vault_revision":
				current.VaultRevisionID += "_other"
			case "generation":
				current.SecretGeneration += "_other"
			case "ciphertext":
				current.AuthCiphertext = "replacement-envelope"
			case "alias":
				current.ID = "DAU_fixture"
			}
			db := rootFenceDB(t, &rootFenceFixture{})
			if err := db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
				if which == "missing" {
					_ = tx.AddError(gorm.ErrRecordNotFound)
					return
				}
				dest, ok := tx.Statement.Dest.(*entity.APIKeyDeliveryAuth)
				if !ok {
					t.Fatal("unexpected read shape")
				}
				*dest = current
				tx.RowsAffected = 1
			}); err != nil {
				t.Fatal(err)
			}
			updates := 0
			if err := db.Callback().Update().Replace("gorm:update", func(tx *gorm.DB) {
				updates++
				values, ok := tx.Statement.Dest.(map[string]any)
				if !ok || len(values) != 1 || values["auth_ciphertext"] != "new-envelope" {
					t.Fatal("CAS changed business tuple")
				}
				tx.RowsAffected = 1
			}); err != nil {
				t.Fatal(err)
			}
			result, err := rootDeliveryAuthCAS(db, captured, "new-envelope")
			want := "changed"
			if which == "unchanged" {
				want = "rewrapped"
			}
			if which == "missing" {
				want = "deleted"
			}
			if err != nil || result != want || (updates == 1) != (which == "unchanged") {
				t.Fatal("CAS tuple admission", which)
			}
		})
	}
}
