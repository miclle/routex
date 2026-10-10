package database

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestDiscordV101FrozenCurrentParityAndAdditiveTail(t *testing.T) {
	for _, pair := range [][2]any{
		{&discordNamedIdentityProviderV101{}, &entity.NamedIdentityProvider{}},
		{&discordNamedIdentityBindingV101{}, &entity.NamedIdentityBinding{}},
		{&discordNamedIdentityCeremonyV101{}, &entity.NamedIdentityCeremony{}},
		{&discordIdentitySessionV101{}, &entity.Session{}},
		{&discordIdentityMFAChallengeV101{}, &entity.MFAChallenge{}},
		{&discordIdentitySessionProofV101{}, &entity.Session{}},
		{&discordIdentityMFAChallengeProofV101{}, &entity.MFAChallenge{}},
		{&discordIdentityRootJobV101{}, &entity.SecretRotationJob{}},
		{&discordIdentityRootProcessV101{}, &entity.SecretProcessVerification{}},
	} {
		frozen, current := oidcV94Schema(t, pair[0]), oidcV94Schema(t, pair[1])
		if frozen.Table != current.Table || len(frozen.Relationships.Relations) != 0 {
			t.Fatal("frozen table boundary")
		}
		for _, field := range frozen.Fields {
			actual := current.FieldsByDBName[field.DBName]
			if actual == nil || actual.FieldType != field.FieldType || actual.Tag.Get("gorm") != field.Tag.Get("gorm") {
				t.Fatal("frozen field parity", frozen.Table, field.DBName)
			}
		}
	}
	for _, driver := range []string{"postgres", "mysql"} {
		steps := migrationSteps(driver)
		if len(steps) != 101 || reflect.ValueOf(steps[97]).Pointer() != reflect.ValueOf(namedIdentityMigration).Pointer() || reflect.ValueOf(steps[98]).Pointer() != reflect.ValueOf(googleIdentityMigration).Pointer() || reflect.ValueOf(steps[99]).Pointer() != reflect.ValueOf(runtimeInstallationMigration).Pointer() || reflect.ValueOf(steps[100]).Pointer() != reflect.ValueOf(discordIdentityMigration).Pointer() {
			t.Fatal("released tail or new101 registration")
		}
	}
}

func TestDiscordV101PreservesHistoricalPredicatesAndExactTuple(t *testing.T) {
	for _, pair := range [][2]any{
		{&googleNamedIdentityProviderV99{}, &discordNamedIdentityProviderV101{}},
		{&googleNamedIdentityBindingV99{}, &discordNamedIdentityBindingV101{}},
		{&googleNamedIdentityCeremonyV99{}, &discordNamedIdentityCeremonyV101{}},
		{&googleIdentitySessionProofV99{}, &discordIdentitySessionProofV101{}},
		{&googleIdentityMFAChallengeProofV99{}, &discordIdentityMFAChallengeProofV101{}},
	} {
		old, next := oidcV94Schema(t, pair[0]), oidcV94Schema(t, pair[1])
		oldChecks := old.ParseCheckConstraints()
		nextChecks := next.ParseCheckConstraints()
		if len(oldChecks) != len(nextChecks) {
			t.Fatal("unexpected check scope", old.Table)
		}
		for name, before := range oldChecks {
			after, ok := nextChecks[name]
			if !ok {
				t.Fatal("lost historical check", name)
			}
			if name == "ck_named_identity_ceremonies_purpose" || name == "ck_named_identity_ceremonies_status" {
				if after.Constraint != before.Constraint {
					t.Fatal("independent finite enum changed", name)
				}
				continue
			}
			prefix := "(" + before.Constraint + ") OR ("
			if !strings.HasPrefix(after.Constraint, prefix) || !strings.HasSuffix(after.Constraint, ")") {
				t.Fatal("historical predicate rewritten", name)
			}
			branch := strings.TrimSuffix(strings.TrimPrefix(after.Constraint, prefix), ")")
			column, profile := "provider_id", "profile_id"
			if old.Table == "named_identity_providers" {
				column = "id"
			}
			if old.Table == "sessions" || old.Table == "mfa_challenges" {
				column, profile = "named_identity_provider_id", "named_identity_profile_id"
			}
			for _, exact := range []string{oidcV94ASCII(column, "discord"), oidcV94ASCII(profile, "discord.oauth2.v1")} {
				if !strings.Contains(branch, exact) {
					t.Fatal("missing exact Discord correlation", old.Table, name)
				}
			}
			if old.Table == "sessions" || old.Table == "mfa_challenges" {
				if !strings.Contains(branch, oidcV94ASCII("primary_method", "discord")) {
					t.Fatal("method-profile equality absent")
				}
				for _, proof := range []string{"oidc", "oauth", "ldap", "saml"} {
					for _, field := range []string{"binding_id", "config_revision", "policy_revision"} {
						if !strings.Contains(branch, "OCTET_LENGTH("+proof+"_"+field+") = 0") {
							t.Fatal("mixed proof permitted", proof, field)
						}
					}
					for _, field := range []string{"binding_created_at", "user_created_at"} {
						if !strings.Contains(branch, proof+"_"+field+" IS NULL") {
							t.Fatal("mixed birth permitted", proof, field)
						}
					}
				}
			} else if !strings.Contains(branch, oidcV94ASCII("identity_issuer", "https://discord.com")) {
				t.Fatal("issuer-profile equality absent")
			}
			if name == "ck_named_identity_binding_kind" && !strings.Contains(branch, oidcV94ASCII("subject_kind", "string")) {
				t.Fatal("Discord numeric JSON subject admitted")
			}
			if name == "ck_named_identity_ceremony_kind" && !strings.Contains(branch, "OCTET_LENGTH(subject_kind) = 0 OR ("+oidcV94ASCII("subject_kind", "string")+")") {
				t.Fatal("pending/verified typed subject boundary")
			}
		}
	}
	for _, tc := range []struct {
		old, next    any
		name, suffix string
	}{
		{&googleIdentityRootJobV99{}, &discordIdentityRootJobV101{}, "ck_secret_inventory_version", " OR inventory_version = 8"},
		{&googleIdentityRootJobV99{}, &discordIdentityRootJobV101{}, "ck_secret_rotation_domain", " OR (inventory_version = 8 AND domain >= 0 AND domain <= 11)"},
		{&googleIdentityRootProcessV99{}, &discordIdentityRootProcessV101{}, "ck_secret_process_inventory_version", " OR inventory_version = 8"},
	} {
		if oidcV94Check(t, tc.next, tc.name) != oidcV94Check(t, tc.old, tc.name)+tc.suffix {
			t.Fatal("historical root coverage reinterpreted", tc.name)
		}
	}
}

func TestDiscordV101RetainedChecksBeforeDDLAndPartialResume(t *testing.T) {
	names := []string{"ck_namedidentityprovider_profile", "ck_namedidentityprovider_issuer", "ck_named_identity_singleton", "ck_namedidentitybinding_profile", "ck_namedidentitybinding_issuer", "ck_named_identity_binding_provider", "ck_named_identity_binding_kind", "ck_namedidentityceremony_profile", "ck_namedidentityceremony_issuer", "ck_named_identity_ceremony_provider", "ck_named_identity_ceremony_kind", "ck_named_identity_ceremonies_purpose", "ck_named_identity_ceremonies_status", "ck_sessions_oidc_primary", "ck_mfa_challenges_oidc_primary", "ck_secret_inventory_version", "ck_secret_rotation_domain", "ck_secret_process_inventory_version"}
	faults := []string{"", "bad:enum:purpose", "bad:enum:status", "query:enum:purpose", "query:enum:status", "bad:named_identity_providers", "bad:named_identity_bindings", "bad:named_identity_ceremonies", "bad:sessions", "bad:mfa_challenges", "bad:secret_rotation_jobs", "bad:secret_process_verifications", "query:named_identity_providers", "query:sessions"}
	for _, name := range names {
		faults = append(faults, "drop:"+name, "create:"+name)
	}
	for _, fault := range faults {
		t.Run(fault, func(t *testing.T) {
			m := &oauthV95ChecksMigrator{checks: map[string]bool{}, fail: fault}
			for _, n := range names {
				m.checks[n] = true
			}
			db := projectRateCheckDB(t, m)
			queries := 0
			if e := db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
				queries++
				where, ok := tx.Statement.Clauses["WHERE"].Expression.(clause.Where)
				if !ok || len(where.Exprs) != 1 {
					t.Fatal("retained predicate missing")
				}
				expr, ok := where.Exprs[0].(clause.Expr)
				if !ok || !strings.HasPrefix(expr.SQL, "NOT (") || len(expr.Vars) != 0 {
					t.Fatal("unowned query")
				}
				if m.fail == "query:"+tx.Statement.Table || tx.Statement.Table == "named_identity_ceremonies" && ((m.fail == "query:enum:purpose" && strings.Contains(expr.SQL, "OCTET_LENGTH(purpose)")) || (m.fail == "query:enum:status" && strings.Contains(expr.SQL, "OCTET_LENGTH(status)"))) {
					_ = tx.AddError(errors.New("controlled query failure"))
					return
				}
				count, ok := tx.Statement.Dest.(*int64)
				if !ok {
					t.Fatal("retained count bound")
				}
				*count = 0
				if m.fail == "bad:"+tx.Statement.Table || tx.Statement.Table == "named_identity_ceremonies" && ((m.fail == "bad:enum:purpose" && strings.Contains(expr.SQL, "OCTET_LENGTH(purpose)")) || (m.fail == "bad:enum:status" && strings.Contains(expr.SQL, "OCTET_LENGTH(status)"))) {
					*count = 1
				}
				tx.RowsAffected = 1
			}); e != nil {
				t.Fatal(e)
			}
			e := discordIdentityConstraintsV101(db)
			if (e != nil) != (fault != "") {
				t.Fatal("failure lost", e)
			}
			if strings.HasPrefix(fault, "bad:") || strings.HasPrefix(fault, "query:") {
				if len(m.ops) != 0 {
					t.Fatal("DDL before retained validation")
				}
			}
			m.fail = ""
			for range 2 {
				if e = discordIdentityConstraintsV101(db); e != nil {
					t.Fatal("partial DDL resume", e)
				}
				for _, name := range names {
					if !m.checks[name] {
						t.Fatal("missing restored check", name)
					}
				}
			}
			if queries < 2*len(names) {
				t.Fatal("repeat skipped current validation")
			}
		})
	}
}
