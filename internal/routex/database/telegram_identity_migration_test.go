package database

import (
	"errors"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestTelegramV106FrozenCurrentParityAndAdditiveTail(t *testing.T) {
	for _, pair := range [][2]any{
		{&telegramNamedIdentityProviderV106{}, &entity.NamedIdentityProvider{}},
		{&telegramNamedIdentityBindingV106{}, &entity.NamedIdentityBinding{}},
		{&telegramNamedIdentityCeremonyV106{}, &entity.NamedIdentityCeremony{}},
		{&telegramIdentitySessionV106{}, &entity.Session{}},
		{&telegramIdentityMFAChallengeV106{}, &entity.MFAChallenge{}},
		{&telegramIdentityRootJobV106{}, &entity.SecretRotationJob{}},
		{&telegramIdentityRootProcessV106{}, &entity.SecretProcessVerification{}},
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
		if len(steps) != 110 || reflect.ValueOf(steps[97]).Pointer() != reflect.ValueOf(namedIdentityMigration).Pointer() || reflect.ValueOf(steps[98]).Pointer() != reflect.ValueOf(googleIdentityMigration).Pointer() || reflect.ValueOf(steps[99]).Pointer() != reflect.ValueOf(runtimeInstallationMigration).Pointer() || reflect.ValueOf(steps[100]).Pointer() != reflect.ValueOf(discordIdentityMigration).Pointer() || reflect.ValueOf(steps[101]).Pointer() != reflect.ValueOf(apiKeyDeliveryMigration).Pointer() || reflect.ValueOf(steps[102]).Pointer() != reflect.ValueOf(forcedSSOMigration).Pointer() || reflect.ValueOf(steps[103]).Pointer() != reflect.ValueOf(vaultCertificateMigration).Pointer() || reflect.ValueOf(steps[104]).Pointer() != reflect.ValueOf(runtimePublicationCommandMigration).Pointer() || reflect.ValueOf(steps[105]).Pointer() != reflect.ValueOf(telegramIdentityMigration).Pointer() {
			t.Fatal("released tail or new106 registration")
		}
	}
}

func TestTelegramV106PreservesHistoricalPredicatesAndExactTuple(t *testing.T) {
	for _, pair := range [][2]any{
		{&discordNamedIdentityProviderV101{}, &telegramNamedIdentityProviderV106{}},
		{&discordNamedIdentityBindingV101{}, &telegramNamedIdentityBindingV106{}},
		{&discordNamedIdentityCeremonyV101{}, &telegramNamedIdentityCeremonyV106{}},
		{&forcedSSOSessionV103{}, &telegramIdentitySessionV106{}},
		{&forcedSSOMFAChallengeV103{}, &telegramIdentityMFAChallengeV106{}},
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
			for _, exact := range []string{oidcV94ASCII(column, "telegram"), oidcV94ASCII(profile, "telegram.oidc.v1")} {
				if !strings.Contains(branch, exact) {
					t.Fatal("missing exact Telegram correlation", old.Table, name)
				}
			}
			if old.Table == "sessions" || old.Table == "mfa_challenges" {
				if !strings.Contains(branch, oidcV94ASCII("primary_method", "telegram")) {
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
				for _, field := range []string{"policy_revision", "mfa_generation", "password_digest"} {
					if !strings.Contains(branch, "OCTET_LENGTH(emergency_"+field+") = 0") {
						t.Fatal("mixed emergency proof permitted", field)
					}
				}
				for _, field := range []string{"user_created_at", "factor_completed_at"} {
					if !strings.Contains(branch, "emergency_"+field+" IS NULL") {
						t.Fatal("mixed emergency birth permitted", field)
					}
				}
				if old.Table == "mfa_challenges" && !strings.Contains(branch, "NOT ("+oidcV94ASCII("purpose", "emergency")+")") {
					t.Fatal("Telegram proof admitted emergency purpose")
				}
			} else if !strings.Contains(branch, oidcV94ASCII("identity_issuer", "https://oauth.telegram.org")) {
				t.Fatal("issuer-profile equality absent")
			}
			if name == "ck_named_identity_binding_kind" && !strings.Contains(branch, oidcV94ASCII("subject_kind", "string")) {
				t.Fatal("Telegram numeric JSON subject admitted")
			}
			if name == "ck_named_identity_binding_kind" || name == "ck_named_identity_ceremony_kind" {
				for _, bounded := range []string{"OCTET_LENGTH(subject) BETWEEN 1 AND 255", "ASCII(SUBSTRING(subject,1,1)) BETWEEN 32 AND 126", "ASCII(SUBSTRING(subject,255,1)) BETWEEN 32 AND 126"} {
					if !strings.Contains(branch, bounded) {
						t.Fatal("opaque printable subject bound absent")
					}
				}
			}
			if name == "ck_named_identity_ceremony_kind" && !strings.Contains(branch, "OCTET_LENGTH(subject_kind) = 0 AND OCTET_LENGTH(subject) = 0") {
				t.Fatal("pending empty typed subject boundary")
			}
		}
	}
	for _, tc := range []struct {
		old, next    any
		name, suffix string
	}{
		{&apiKeyDeliveryRootJobV102{}, &telegramIdentityRootJobV106{}, "ck_secret_inventory_version", " OR inventory_version = 10"},
		{&apiKeyDeliveryRootJobV102{}, &telegramIdentityRootJobV106{}, "ck_secret_rotation_domain", " OR (inventory_version = 10 AND domain >= 0 AND domain <= 12)"},
		{&apiKeyDeliveryRootProcessV102{}, &telegramIdentityRootProcessV106{}, "ck_secret_process_inventory_version", " OR inventory_version = 10"},
	} {
		if oidcV94Check(t, tc.next, tc.name) != oidcV94Check(t, tc.old, tc.name)+tc.suffix {
			t.Fatal("historical root coverage reinterpreted", tc.name)
		}
	}
}

func TestTelegramV106RetainedChecksBeforeDDLAndPartialResume(t *testing.T) {
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
			e := telegramIdentityConstraintsV106(db)
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
				if e = telegramIdentityConstraintsV106(db); e != nil {
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

func TestTelegramV106CheckColumnsStayWithinFrozenTables(t *testing.T) {
	vocabulary := map[string]bool{"OCTET_LENGTH": true, "ASCII": true, "SUBSTRING": true, "CHAR_LENGTH": true, "BETWEEN": true, "AND": true, "OR": true, "NOT": true, "IS": true, "NULL": true, "EXTRACT": true, "YEAR": true, "FROM": true, "TRUE": true, "FALSE": true}
	for _, model := range []any{&telegramNamedIdentityProviderV106{}, &telegramNamedIdentityBindingV106{}, &telegramNamedIdentityCeremonyV106{}, &telegramIdentitySessionV106{}, &telegramIdentityMFAChallengeV106{}, &telegramIdentityRootJobV106{}, &telegramIdentityRootProcessV106{}} {
		frozen := oidcV94Schema(t, model)
		for name, check := range frozen.ParseCheckConstraints() {
			for _, token := range regexp.MustCompile(`[A-Za-z_][A-Za-z_0-9]*`).FindAllString(check.Constraint, -1) {
				if !vocabulary[strings.ToUpper(token)] && frozen.FieldsByDBName[token] == nil {
					t.Fatal("CHECK references absent frozen column", frozen.Table, name)
				}
			}
		}
	}
	if strings.Contains(oidcV94Check(t, &telegramIdentitySessionV106{}, "ck_sessions_oidc_primary"), "purpose") || !strings.Contains(oidcV94Check(t, &telegramIdentityMFAChallengeV106{}, "ck_mfa_challenges_oidc_primary"), "NOT ("+oidcV94ASCII("purpose", "emergency")+")") {
		t.Fatal("Session/challenge purpose boundary changed")
	}
}
