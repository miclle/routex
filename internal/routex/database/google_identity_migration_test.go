package database

import (
	"errors"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reflect"
	"strings"
	"testing"
)

func TestGoogleV99FrozenCurrentParityAndAdditiveTail(t *testing.T) {
	for _, pair := range [][2]any{{&googleNamedIdentityProviderV99{}, &entity.NamedIdentityProvider{}}, {&googleNamedIdentityBindingV99{}, &entity.NamedIdentityBinding{}}, {&googleNamedIdentityCeremonyV99{}, &entity.NamedIdentityCeremony{}}, {&googleIdentitySessionV99{}, &entity.Session{}}, {&googleIdentityMFAChallengeV99{}, &entity.MFAChallenge{}}, {&googleIdentitySessionProofV99{}, &entity.Session{}}, {&googleIdentityMFAChallengeProofV99{}, &entity.MFAChallenge{}}, {&googleIdentityRootJobV99{}, &entity.SecretRotationJob{}}, {&googleIdentityRootProcessV99{}, &entity.SecretProcessVerification{}}} {
		f, c := oidcV94Schema(t, pair[0]), oidcV94Schema(t, pair[1])
		if f.Table != c.Table || len(f.Relationships.Relations) != 0 {
			t.Fatal("frozen table boundary")
		}
		for _, field := range f.Fields {
			actual := c.FieldsByDBName[field.DBName]
			if actual == nil || actual.FieldType != field.FieldType || actual.Tag.Get("gorm") != field.Tag.Get("gorm") {
				t.Fatal("frozen field parity", f.Table, field.DBName)
			}
		}
	}
	for _, driver := range []string{"postgres", "mysql"} {
		steps := migrationSteps(driver)
		if len(steps) != 99 || reflect.ValueOf(steps[97]).Pointer() != reflect.ValueOf(namedIdentityMigration).Pointer() || reflect.ValueOf(steps[98]).Pointer() != reflect.ValueOf(googleIdentityMigration).Pointer() {
			t.Fatal("released tail or new99 registration")
		}
	}
	for _, pair := range [][2]any{{&namedIdentitySessionProofV98{}, &googleIdentitySessionProofV99{}}, {&namedIdentityMFAChallengeProofV98{}, &googleIdentityMFAChallengeProofV99{}}} {
		old := oidcV94Schema(t, pair[0]).FieldsByDBName["primary_method"].Tag.Get("gorm")
		next := oidcV94Schema(t, pair[1]).FieldsByDBName["primary_method"].Tag.Get("gorm")
		old = old[strings.Index(old, "primary,")+8:]
		next = next[strings.Index(next, "primary,")+8:]
		if !strings.HasPrefix(next, "("+old+") OR (") || !strings.Contains(next, oidcV94ASCII("primary_method", "google")) || !strings.Contains(next, oidcV94ASCII("named_identity_provider_id", "google")) || !strings.Contains(next, oidcV94ASCII("named_identity_profile_id", "google.oidc.v1")) {
			t.Fatal("historical proof rewritten or Google correlation absent")
		}
	}
}
func TestGoogleV99RetainedChecksBeforeDDLAndPartialResume(t *testing.T) {
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
			e := googleIdentityConstraintsV99(db)
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
				if e = googleIdentityConstraintsV99(db); e != nil {
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
func TestGoogleV99NamedTupleAndInventoryCoverageAreExact(t *testing.T) {
	for _, model := range []any{&googleNamedIdentityProviderV99{}, &googleNamedIdentityBindingV99{}, &googleNamedIdentityCeremonyV99{}} {
		s := oidcV94Schema(t, model)
		column := "provider_id"
		if s.Table == "named_identity_providers" {
			column = "id"
		}
		for _, check := range s.ParseCheckConstraints() {
			// Purpose/status are independent finite enums, not provider/profile tuples.
			if check.Name == "ck_named_identity_ceremonies_purpose" || check.Name == "ck_named_identity_ceremonies_status" {
				continue
			}
			for _, word := range []string{oidcV94ASCII(column, "github"), oidcV94ASCII(column, "google"), oidcV94ASCII("profile_id", "github.com.oauth-app.v1"), oidcV94ASCII("profile_id", "google.oidc.v1"), oidcV94ASCII("identity_issuer", "https://github.com"), oidcV94ASCII("identity_issuer", "https://accounts.google.com")} {
				if !strings.Contains(check.Constraint, word) {
					t.Fatal("tuple not fully correlated", s.Table, check.Name)
				}
			}
		}
	}
	for _, name := range []string{"ck_named_identity_ceremonies_purpose", "ck_named_identity_ceremonies_status"} {
		if old, next := oidcV94Check(t, &namedIdentityCeremonyV98{}, name), oidcV94Check(t, &googleNamedIdentityCeremonyV99{}, name); next != old {
			t.Fatal("Google changed independent ceremony enum", name)
		}
	}
	old := oidcV94Check(t, &namedIdentityRootJobV98{}, "ck_secret_inventory_version")
	next := oidcV94Check(t, &googleIdentityRootJobV99{}, "ck_secret_inventory_version")
	if next != old+" OR inventory_version = 7" {
		t.Fatal("historical root versions changed")
	}
	old = oidcV94Check(t, &namedIdentityRootJobV98{}, "ck_secret_rotation_domain")
	next = oidcV94Check(t, &googleIdentityRootJobV99{}, "ck_secret_rotation_domain")
	if next != old+" OR (inventory_version = 7 AND domain >= 0 AND domain <= 11)" {
		t.Fatal("coverage/domain reinterpretation")
	}
}
