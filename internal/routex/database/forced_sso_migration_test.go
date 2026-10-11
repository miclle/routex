package database

import (
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

func forcedSSOParsed(t *testing.T, model any) *schema.Schema {
	t.Helper()
	s, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func TestForcedSSOV103FrozenSchemaAndHistoricalBoundaries(t *testing.T) {
	// CHECK expressions must reference only actual columns of their frozen table.
	checkColumns := func(model *schema.Schema, expression string) bool {
		vocabulary := map[string]bool{"OCTET_LENGTH": true, "ASCII": true, "SUBSTRING": true, "CHAR_LENGTH": true, "BETWEEN": true, "AND": true, "OR": true, "NOT": true, "IS": true, "NULL": true, "EXTRACT": true, "YEAR": true, "FROM": true, "TRUE": true, "FALSE": true}
		for _, token := range regexp.MustCompile(`[A-Za-z_][A-Za-z_0-9]*`).FindAllString(expression, -1) {
			if !vocabulary[strings.ToUpper(token)] && model.FieldsByDBName[token] == nil {
				return false
			}
		}
		return true
	}
	for _, model := range []any{&forcedSSOSessionV103{}, &forcedSSOMFAChallengeV103{}, &forcedSSOGovernanceV103{}, &forcedSSOInstallationV103{}} {
		parsed := forcedSSOParsed(t, model)
		for name, constraint := range parsed.ParseCheckConstraints() {
			if !checkColumns(parsed, constraint.Constraint) {
				t.Fatal("CHECK references absent frozen column", name)
			}
		}
	}
	sessionColumns, challengeColumns := forcedSSOParsed(t, &forcedSSOSessionV103{}), forcedSSOParsed(t, &forcedSSOMFAChallengeV103{})
	if checkColumns(sessionColumns, "purpose IS NULL") || !checkColumns(challengeColumns, "purpose IS NULL") {
		t.Fatal("Session/challenge purpose column boundary lost")
	}
	sessionCheck := sessionColumns.ParseCheckConstraints()["ck_sessions_oidc_primary"].Constraint
	challengeCheck := challengeColumns.ParseCheckConstraints()["ck_mfa_challenges_oidc_primary"].Constraint
	if strings.Contains(sessionCheck, "purpose") || !strings.Contains(challengeCheck, "AND NOT (OCTET_LENGTH(purpose) = 9") {
		t.Fatal("ordinary emergency-purpose exclusion drift")
	}
	for _, pair := range []struct{ frozen, current any }{
		{&forcedSSOSessionV103{}, &entity.Session{}},
		{&forcedSSOMFAChallengeV103{}, &entity.MFAChallenge{}},
		{&forcedSSOInstallationV103{}, &entity.RuntimeInstallationObservation{}},
	} {
		frozen, current := forcedSSOParsed(t, pair.frozen), forcedSSOParsed(t, pair.current)
		if frozen.Table != current.Table || len(frozen.Fields) != len(current.Fields) || len(frozen.Relationships.Relations) != 0 {
			t.Fatal("frozen shape differs")
		}
		for name, f := range frozen.FieldsByName {
			c := current.FieldsByName[name]
			if c == nil || f.DBName != c.DBName || f.FieldType != c.FieldType || f.Tag.Get("gorm") != c.Tag.Get("gorm") {
				t.Fatal("frozen field differs", name)
			}
		}
	}
	gov, currentGov := forcedSSOParsed(t, &forcedSSOGovernanceV103{}), forcedSSOParsed(t, &entity.GovernanceSetting{})
	for _, name := range []string{"ForcedSSOEnabled", "AuthenticationPolicyRevision", "EmergencyAdminID", "EmergencyAdminCreatedAt"} {
		f, c := gov.FieldsByName[name], currentGov.FieldsByName[name]
		if c == nil || f.DBName != c.DBName || f.FieldType != c.FieldType || f.Tag.Get("gorm") != c.Tag.Get("gorm") || c.Tag.Get("json") != "-" {
			t.Fatal("policy field drift", name)
		}
	}
	for _, pair := range []struct {
		old, current any
		check        string
	}{
		{&forcedSSOHistoricalSessionV102{}, &forcedSSOSessionV103{}, "ck_sessions_oidc_primary"},
		{&forcedSSOHistoricalMFAChallengeV102{}, &forcedSSOMFAChallengeV103{}, "ck_mfa_challenges_oidc_primary"},
	} {
		old, current := forcedSSOParsed(t, pair.old), forcedSSOParsed(t, pair.current)
		oldPredicate := old.ParseCheckConstraints()[pair.check].Constraint
		newPredicate := current.ParseCheckConstraints()[pair.check].Constraint
		if strings.Contains(oldPredicate, "emergency_") || !strings.Contains(newPredicate, "(("+oldPredicate+") AND ") {
			t.Fatal("released primary arm changed")
		}
		for name, f := range old.FieldsByName {
			c := current.FieldsByName[name]
			if c == nil || f.DBName != c.DBName || f.FieldType != c.FieldType || (name != "PrimaryMethod" && f.Tag.Get("gorm") != c.Tag.Get("gorm")) {
				t.Fatal("predecessor field drift", name)
			}
		}
		for _, name := range []string{"EmergencyPolicyRevision", "EmergencyUserCreatedAt", "EmergencyMFAGeneration", "EmergencyPasswordDigest", "EmergencyFactorCompletedAt"} {
			if current.FieldsByName[name].Tag.Get("json") != "-" {
				t.Fatal("private emergency tuple exported")
			}
		}
		if !strings.Contains(newPredicate, "OCTET_LENGTH(emergency_mfa_generation) = 43") || strings.Contains(newPredicate, "OCTET_LENGTH(emergency_mfa_generation) = 64") {
			t.Fatal("native generation reinterpreted")
		}
	}
	old := forcedSSOParsed(t, &forcedSSOHistoricalInstallationV100{})
	now := forcedSSOParsed(t, &forcedSSOInstallationV103{})
	if old.ParseCheckConstraints()["ck_runtime_installation_projection"].Constraint != "projection_version = 1" || now.ParseCheckConstraints()["ck_runtime_installation_projection"].Constraint != "projection_version = 1 OR projection_version = 2" {
		t.Fatal("observation history redefined")
	}
	for _, driver := range []string{"postgres", "mysql"} {
		steps := migrationSteps(driver)
		if len(steps) != 105 || reflect.ValueOf(steps[101]).Pointer() != reflect.ValueOf(apiKeyDeliveryMigration).Pointer() || reflect.ValueOf(steps[102]).Pointer() != reflect.ValueOf(forcedSSOMigration).Pointer() || reflect.ValueOf(steps[103]).Pointer() != reflect.ValueOf(vaultCertificateMigration).Pointer() || reflect.ValueOf(steps[104]).Pointer() != reflect.ValueOf(runtimePublicationCommandMigration).Pointer() {
			t.Fatal("V103 suffix drift")
		}
	}
}

func TestForcedSSOV103StrictColumnMetadata(t *testing.T) {
	model := &forcedSSOGovernanceV103{}
	parsed := forcedSSOParsed(t, model)
	names := []string{"ForcedSSOEnabled", "AuthenticationPolicyRevision", "EmergencyAdminID", "EmergencyAdminCreatedAt"}
	baseline := []gorm.ColumnType{}
	for _, name := range names {
		f := parsed.FieldsByName[name]
		c := oidcV94Column{name: f.DBName, kind: "varchar", size: int64(f.Size), sized: true, primaryKnown: true, nullableKnown: true, nullable: f.FieldType.Kind() == reflect.Pointer, precision: 6, precisionKnown: true, defaultKnown: f.HasDefaultValue, def: f.DefaultValue}
		if string(f.DataType) == "bool" {
			c.kind = "boolean"
		}
		if string(f.DataType) == "time" {
			c.kind = "timestamp"
		}
		baseline = append(baseline, c)
	}
	validate := func(cols []gorm.ColumnType) error {
		m := &oidcV94ColumnsMigrator{columns: cols}
		db, err := gorm.Open(runtimeInstallationColumnsDialect{Dialector: postgres.New(postgres.Config{DSN: "host=localhost user=offline dbname=offline sslmode=disable"}), m: m}, &gorm.Config{DisableAutomaticPing: true})
		if err != nil {
			t.Fatal(err)
		}
		pool, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := pool.Close(); err != nil {
				t.Error(err)
			}
		}()
		return validateForcedSSOColumnsV103(db, model, names)
	}
	for _, which := range []string{"missing", "duplicate", "width", "kind", "nullable", "unknown_nullable", "primary", "unknown_primary", "default", "missing_default", "precision3", "unknown_precision", "time_default"} {
		t.Run(which, func(t *testing.T) {
			if err := validate(baseline); err != nil {
				t.Fatal("positive metadata prerequisite", err)
			}
			cols := append([]gorm.ColumnType(nil), baseline...)
			index := 1
			if strings.Contains(which, "precision") || which == "time_default" {
				index = 3
			}
			c := cols[index].(oidcV94Column)
			switch which {
			case "missing":
				cols = cols[:1]
			case "duplicate":
				cols = append(cols, cols[1])
			case "width":
				c.size++
			case "kind":
				c.kind = "text"
			case "nullable":
				c.nullable = !c.nullable
			case "unknown_nullable":
				c.nullableKnown = false
			case "primary":
				c.primary = true
			case "unknown_primary":
				c.primaryKnown = false
			case "default":
				c.def = "invented"
			case "missing_default":
				c.defaultKnown = false
			case "precision3":
				c.precision = 3
			case "unknown_precision":
				c.precisionKnown = false
			case "time_default":
				c.defaultKnown = true
				c.def = "CURRENT_TIMESTAMP"
			}
			if which != "missing" {
				cols[index] = c
			}
			if validate(cols) == nil {
				t.Fatal("incompatible metadata admitted", which)
			}
		})
	}
}

// Independent predecessor literals are test-only; released migration steps are unchanged.
type forcedSSOHistoricalSessionV102 struct {
	NamedIdentityProviderID       string     `gorm:"column:named_identity_provider_id;size:30;not null;default:''" json:"-"`
	NamedIdentityProfileID        string     `gorm:"column:named_identity_profile_id;size:64;not null;default:''" json:"-"`
	NamedIdentityBindingID        string     `gorm:"column:named_identity_binding_id;size:30;not null;default:''" json:"-"`
	NamedIdentityBindingCreatedAt *time.Time `gorm:"column:named_identity_binding_created_at;precision:6" json:"-"`
	NamedIdentityConfigRevision   string     `gorm:"column:named_identity_config_revision;size:64;not null;default:''" json:"-"`
	NamedIdentityPolicyRevision   string     `gorm:"column:named_identity_policy_revision;size:64;not null;default:''" json:"-"`
	NamedIdentityUserCreatedAt    *time.Time `gorm:"column:named_identity_user_created_at;precision:6" json:"-"`

	SAMLBindingID        string     `gorm:"column:saml_binding_id;size:30;not null;default:''" json:"-"`
	SAMLBindingCreatedAt *time.Time `gorm:"column:saml_binding_created_at;precision:6" json:"-"`
	SAMLConfigRevision   string     `gorm:"column:saml_config_revision;size:64;not null;default:''" json:"-"`
	SAMLPolicyRevision   string     `gorm:"column:saml_policy_revision;size:64;not null;default:''" json:"-"`
	SAMLUserCreatedAt    *time.Time `gorm:"column:saml_user_created_at;precision:6" json:"-"`

	LDAPBindingID         string     `gorm:"column:ldap_binding_id;size:30;not null;default:''" json:"-"`
	LDAPBindingCreatedAt  *time.Time `gorm:"column:ldap_binding_created_at;precision:6" json:"-"`
	LDAPConfigRevision    string     `gorm:"column:ldap_config_revision;size:64;not null;default:''" json:"-"`
	LDAPPolicyRevision    string     `gorm:"column:ldap_policy_revision;size:64;not null;default:''" json:"-"`
	LDAPUserCreatedAt     *time.Time `gorm:"column:ldap_user_created_at;precision:6" json:"-"`
	OAuthBindingID        string     `gorm:"column:oauth_binding_id;size:30;not null;default:''" json:"-"`
	OAuthBindingCreatedAt *time.Time `gorm:"column:oauth_binding_created_at;precision:6" json:"-"`
	OAuthConfigRevision   string     `gorm:"column:oauth_config_revision;size:64;not null;default:''" json:"-"`
	OAuthPolicyRevision   string     `gorm:"column:oauth_policy_revision;size:64;not null;default:''" json:"-"`
	OAuthUserCreatedAt    *time.Time `gorm:"column:oauth_user_created_at;precision:6" json:"-"`
	PrimaryMethod         string     `gorm:"size:20;not null;default:'';check:ck_sessions_oidc_primary,(((((OCTET_LENGTH(primary_method) = 0 AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 100 AND ASCII(SUBSTRING(primary_method,4,1)) = 99 AND CHAR_LENGTH(oidc_binding_id) > 0 AND oidc_binding_created_at IS NOT NULL AND CHAR_LENGTH(oidc_config_revision) = 64 AND CHAR_LENGTH(oidc_policy_revision) = 64 AND oidc_user_created_at IS NOT NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 5 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 97 AND ASCII(SUBSTRING(primary_method,3,1)) = 117 AND ASCII(SUBSTRING(primary_method,4,1)) = 116 AND ASCII(SUBSTRING(primary_method,5,1)) = 104 AND CHAR_LENGTH(oauth_binding_id) > 0 AND oauth_binding_created_at IS NOT NULL AND CHAR_LENGTH(oauth_config_revision) = 64 AND CHAR_LENGTH(oauth_policy_revision) = 64 AND oauth_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 108 AND ASCII(SUBSTRING(primary_method,2,1)) = 100 AND ASCII(SUBSTRING(primary_method,3,1)) = 97 AND ASCII(SUBSTRING(primary_method,4,1)) = 112 AND CHAR_LENGTH(ldap_binding_id) > 0 AND ldap_binding_created_at IS NOT NULL AND CHAR_LENGTH(ldap_config_revision) = 64 AND CHAR_LENGTH(ldap_policy_revision) = 64 AND ldap_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 115 AND ASCII(SUBSTRING(primary_method,2,1)) = 97 AND ASCII(SUBSTRING(primary_method,3,1)) = 109 AND ASCII(SUBSTRING(primary_method,4,1)) = 108 AND CHAR_LENGTH(saml_binding_id) > 0 AND saml_binding_created_at IS NOT NULL AND CHAR_LENGTH(saml_config_revision) = 64 AND CHAR_LENGTH(saml_policy_revision) = 64 AND saml_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL)) AND OCTET_LENGTH(named_identity_provider_id) = 0 AND OCTET_LENGTH(named_identity_profile_id) = 0 AND OCTET_LENGTH(named_identity_binding_id) = 0 AND named_identity_binding_created_at IS NULL AND OCTET_LENGTH(named_identity_config_revision) = 0 AND OCTET_LENGTH(named_identity_policy_revision) = 0 AND named_identity_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 6 AND ASCII(SUBSTRING(primary_method,1,1)) = 103 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 116 AND ASCII(SUBSTRING(primary_method,4,1)) = 104 AND ASCII(SUBSTRING(primary_method,5,1)) = 117 AND ASCII(SUBSTRING(primary_method,6,1)) = 98 AND OCTET_LENGTH(named_identity_provider_id) = 6 AND ASCII(SUBSTRING(named_identity_provider_id,1,1)) = 103 AND ASCII(SUBSTRING(named_identity_provider_id,2,1)) = 105 AND ASCII(SUBSTRING(named_identity_provider_id,3,1)) = 116 AND ASCII(SUBSTRING(named_identity_provider_id,4,1)) = 104 AND ASCII(SUBSTRING(named_identity_provider_id,5,1)) = 117 AND ASCII(SUBSTRING(named_identity_provider_id,6,1)) = 98 AND OCTET_LENGTH(named_identity_profile_id) = 23 AND ASCII(SUBSTRING(named_identity_profile_id,1,1)) = 103 AND ASCII(SUBSTRING(named_identity_profile_id,2,1)) = 105 AND ASCII(SUBSTRING(named_identity_profile_id,3,1)) = 116 AND ASCII(SUBSTRING(named_identity_profile_id,4,1)) = 104 AND ASCII(SUBSTRING(named_identity_profile_id,5,1)) = 117 AND ASCII(SUBSTRING(named_identity_profile_id,6,1)) = 98 AND ASCII(SUBSTRING(named_identity_profile_id,7,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,8,1)) = 99 AND ASCII(SUBSTRING(named_identity_profile_id,9,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,10,1)) = 109 AND ASCII(SUBSTRING(named_identity_profile_id,11,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,12,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,13,1)) = 97 AND ASCII(SUBSTRING(named_identity_profile_id,14,1)) = 117 AND ASCII(SUBSTRING(named_identity_profile_id,15,1)) = 116 AND ASCII(SUBSTRING(named_identity_profile_id,16,1)) = 104 AND ASCII(SUBSTRING(named_identity_profile_id,17,1)) = 45 AND ASCII(SUBSTRING(named_identity_profile_id,18,1)) = 97 AND ASCII(SUBSTRING(named_identity_profile_id,19,1)) = 112 AND ASCII(SUBSTRING(named_identity_profile_id,20,1)) = 112 AND ASCII(SUBSTRING(named_identity_profile_id,21,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,22,1)) = 118 AND ASCII(SUBSTRING(named_identity_profile_id,23,1)) = 49 AND CHAR_LENGTH(named_identity_binding_id) > 0 AND named_identity_binding_created_at IS NOT NULL AND CHAR_LENGTH(named_identity_config_revision) = 64 AND CHAR_LENGTH(named_identity_policy_revision) = 64 AND named_identity_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL)) OR (OCTET_LENGTH(primary_method) = 6 AND ASCII(SUBSTRING(primary_method,1,1)) = 103 AND ASCII(SUBSTRING(primary_method,2,1)) = 111 AND ASCII(SUBSTRING(primary_method,3,1)) = 111 AND ASCII(SUBSTRING(primary_method,4,1)) = 103 AND ASCII(SUBSTRING(primary_method,5,1)) = 108 AND ASCII(SUBSTRING(primary_method,6,1)) = 101 AND OCTET_LENGTH(named_identity_provider_id) = 6 AND ASCII(SUBSTRING(named_identity_provider_id,1,1)) = 103 AND ASCII(SUBSTRING(named_identity_provider_id,2,1)) = 111 AND ASCII(SUBSTRING(named_identity_provider_id,3,1)) = 111 AND ASCII(SUBSTRING(named_identity_provider_id,4,1)) = 103 AND ASCII(SUBSTRING(named_identity_provider_id,5,1)) = 108 AND ASCII(SUBSTRING(named_identity_provider_id,6,1)) = 101 AND OCTET_LENGTH(named_identity_profile_id) = 14 AND ASCII(SUBSTRING(named_identity_profile_id,1,1)) = 103 AND ASCII(SUBSTRING(named_identity_profile_id,2,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,3,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,4,1)) = 103 AND ASCII(SUBSTRING(named_identity_profile_id,5,1)) = 108 AND ASCII(SUBSTRING(named_identity_profile_id,6,1)) = 101 AND ASCII(SUBSTRING(named_identity_profile_id,7,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,8,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,9,1)) = 105 AND ASCII(SUBSTRING(named_identity_profile_id,10,1)) = 100 AND ASCII(SUBSTRING(named_identity_profile_id,11,1)) = 99 AND ASCII(SUBSTRING(named_identity_profile_id,12,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,13,1)) = 118 AND ASCII(SUBSTRING(named_identity_profile_id,14,1)) = 49 AND CHAR_LENGTH(named_identity_binding_id) > 0 AND named_identity_binding_created_at IS NOT NULL AND CHAR_LENGTH(named_identity_config_revision) = 64 AND CHAR_LENGTH(named_identity_policy_revision) = 64 AND named_identity_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL)) OR (OCTET_LENGTH(primary_method) = 7 AND ASCII(SUBSTRING(primary_method,1,1)) = 100 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 115 AND ASCII(SUBSTRING(primary_method,4,1)) = 99 AND ASCII(SUBSTRING(primary_method,5,1)) = 111 AND ASCII(SUBSTRING(primary_method,6,1)) = 114 AND ASCII(SUBSTRING(primary_method,7,1)) = 100 AND OCTET_LENGTH(named_identity_provider_id) = 7 AND ASCII(SUBSTRING(named_identity_provider_id,1,1)) = 100 AND ASCII(SUBSTRING(named_identity_provider_id,2,1)) = 105 AND ASCII(SUBSTRING(named_identity_provider_id,3,1)) = 115 AND ASCII(SUBSTRING(named_identity_provider_id,4,1)) = 99 AND ASCII(SUBSTRING(named_identity_provider_id,5,1)) = 111 AND ASCII(SUBSTRING(named_identity_provider_id,6,1)) = 114 AND ASCII(SUBSTRING(named_identity_provider_id,7,1)) = 100 AND OCTET_LENGTH(named_identity_profile_id) = 17 AND ASCII(SUBSTRING(named_identity_profile_id,1,1)) = 100 AND ASCII(SUBSTRING(named_identity_profile_id,2,1)) = 105 AND ASCII(SUBSTRING(named_identity_profile_id,3,1)) = 115 AND ASCII(SUBSTRING(named_identity_profile_id,4,1)) = 99 AND ASCII(SUBSTRING(named_identity_profile_id,5,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,6,1)) = 114 AND ASCII(SUBSTRING(named_identity_profile_id,7,1)) = 100 AND ASCII(SUBSTRING(named_identity_profile_id,8,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,9,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,10,1)) = 97 AND ASCII(SUBSTRING(named_identity_profile_id,11,1)) = 117 AND ASCII(SUBSTRING(named_identity_profile_id,12,1)) = 116 AND ASCII(SUBSTRING(named_identity_profile_id,13,1)) = 104 AND ASCII(SUBSTRING(named_identity_profile_id,14,1)) = 50 AND ASCII(SUBSTRING(named_identity_profile_id,15,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,16,1)) = 118 AND ASCII(SUBSTRING(named_identity_profile_id,17,1)) = 49 AND CHAR_LENGTH(named_identity_binding_id) > 0 AND named_identity_binding_created_at IS NOT NULL AND CHAR_LENGTH(named_identity_config_revision) = 64 AND CHAR_LENGTH(named_identity_policy_revision) = 64 AND named_identity_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL)" json:"-"`
	OIDCBindingID         string     `gorm:"column:oidc_binding_id;size:30;not null;default:''" json:"-"`
	OIDCBindingCreatedAt  *time.Time `gorm:"column:oidc_binding_created_at;precision:6" json:"-"`
	OIDCConfigRevision    string     `gorm:"column:oidc_config_revision;size:64;not null;default:''" json:"-"`
	OIDCPolicyRevision    string     `gorm:"column:oidc_policy_revision;size:64;not null;default:''" json:"-"`
	OIDCUserCreatedAt     *time.Time `gorm:"column:oidc_user_created_at;precision:6" json:"-"`

	ID        string    `gorm:"primaryKey;size:30"`
	UserID    string    `gorm:"size:30;not null;index"`
	TokenHash string    `gorm:"size:64;not null;uniqueIndex"`
	ExpiresAt time.Time `gorm:"not null;index"`
	CreatedAt time.Time
}

func (forcedSSOHistoricalSessionV102) TableName() string { return "sessions" }

type forcedSSOHistoricalMFAChallengeV102 struct {
	NamedIdentityProviderID       string     `gorm:"column:named_identity_provider_id;size:30;not null;default:''" json:"-"`
	NamedIdentityProfileID        string     `gorm:"column:named_identity_profile_id;size:64;not null;default:''" json:"-"`
	NamedIdentityBindingID        string     `gorm:"column:named_identity_binding_id;size:30;not null;default:''" json:"-"`
	NamedIdentityBindingCreatedAt *time.Time `gorm:"column:named_identity_binding_created_at;precision:6" json:"-"`
	NamedIdentityConfigRevision   string     `gorm:"column:named_identity_config_revision;size:64;not null;default:''" json:"-"`
	NamedIdentityPolicyRevision   string     `gorm:"column:named_identity_policy_revision;size:64;not null;default:''" json:"-"`
	NamedIdentityUserCreatedAt    *time.Time `gorm:"column:named_identity_user_created_at;precision:6" json:"-"`

	SAMLBindingID        string     `gorm:"column:saml_binding_id;size:30;not null;default:''" json:"-"`
	SAMLBindingCreatedAt *time.Time `gorm:"column:saml_binding_created_at;precision:6" json:"-"`
	SAMLConfigRevision   string     `gorm:"column:saml_config_revision;size:64;not null;default:''" json:"-"`
	SAMLPolicyRevision   string     `gorm:"column:saml_policy_revision;size:64;not null;default:''" json:"-"`
	SAMLUserCreatedAt    *time.Time `gorm:"column:saml_user_created_at;precision:6" json:"-"`

	LDAPBindingID         string     `gorm:"column:ldap_binding_id;size:30;not null;default:''" json:"-"`
	LDAPBindingCreatedAt  *time.Time `gorm:"column:ldap_binding_created_at;precision:6" json:"-"`
	LDAPConfigRevision    string     `gorm:"column:ldap_config_revision;size:64;not null;default:''" json:"-"`
	LDAPPolicyRevision    string     `gorm:"column:ldap_policy_revision;size:64;not null;default:''" json:"-"`
	LDAPUserCreatedAt     *time.Time `gorm:"column:ldap_user_created_at;precision:6" json:"-"`
	OAuthBindingID        string     `gorm:"column:oauth_binding_id;size:30;not null;default:''" json:"-"`
	OAuthBindingCreatedAt *time.Time `gorm:"column:oauth_binding_created_at;precision:6" json:"-"`
	OAuthConfigRevision   string     `gorm:"column:oauth_config_revision;size:64;not null;default:''" json:"-"`
	OAuthPolicyRevision   string     `gorm:"column:oauth_policy_revision;size:64;not null;default:''" json:"-"`
	OAuthUserCreatedAt    *time.Time `gorm:"column:oauth_user_created_at;precision:6" json:"-"`
	PrimaryMethod         string     `gorm:"size:20;not null;default:'';check:ck_mfa_challenges_oidc_primary,(((((OCTET_LENGTH(primary_method) = 0 AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 100 AND ASCII(SUBSTRING(primary_method,4,1)) = 99 AND CHAR_LENGTH(oidc_binding_id) > 0 AND oidc_binding_created_at IS NOT NULL AND CHAR_LENGTH(oidc_config_revision) = 64 AND CHAR_LENGTH(oidc_policy_revision) = 64 AND oidc_user_created_at IS NOT NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 5 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 97 AND ASCII(SUBSTRING(primary_method,3,1)) = 117 AND ASCII(SUBSTRING(primary_method,4,1)) = 116 AND ASCII(SUBSTRING(primary_method,5,1)) = 104 AND CHAR_LENGTH(oauth_binding_id) > 0 AND oauth_binding_created_at IS NOT NULL AND CHAR_LENGTH(oauth_config_revision) = 64 AND CHAR_LENGTH(oauth_policy_revision) = 64 AND oauth_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 108 AND ASCII(SUBSTRING(primary_method,2,1)) = 100 AND ASCII(SUBSTRING(primary_method,3,1)) = 97 AND ASCII(SUBSTRING(primary_method,4,1)) = 112 AND CHAR_LENGTH(ldap_binding_id) > 0 AND ldap_binding_created_at IS NOT NULL AND CHAR_LENGTH(ldap_config_revision) = 64 AND CHAR_LENGTH(ldap_policy_revision) = 64 AND ldap_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 115 AND ASCII(SUBSTRING(primary_method,2,1)) = 97 AND ASCII(SUBSTRING(primary_method,3,1)) = 109 AND ASCII(SUBSTRING(primary_method,4,1)) = 108 AND CHAR_LENGTH(saml_binding_id) > 0 AND saml_binding_created_at IS NOT NULL AND CHAR_LENGTH(saml_config_revision) = 64 AND CHAR_LENGTH(saml_policy_revision) = 64 AND saml_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL)) AND OCTET_LENGTH(named_identity_provider_id) = 0 AND OCTET_LENGTH(named_identity_profile_id) = 0 AND OCTET_LENGTH(named_identity_binding_id) = 0 AND named_identity_binding_created_at IS NULL AND OCTET_LENGTH(named_identity_config_revision) = 0 AND OCTET_LENGTH(named_identity_policy_revision) = 0 AND named_identity_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 6 AND ASCII(SUBSTRING(primary_method,1,1)) = 103 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 116 AND ASCII(SUBSTRING(primary_method,4,1)) = 104 AND ASCII(SUBSTRING(primary_method,5,1)) = 117 AND ASCII(SUBSTRING(primary_method,6,1)) = 98 AND OCTET_LENGTH(named_identity_provider_id) = 6 AND ASCII(SUBSTRING(named_identity_provider_id,1,1)) = 103 AND ASCII(SUBSTRING(named_identity_provider_id,2,1)) = 105 AND ASCII(SUBSTRING(named_identity_provider_id,3,1)) = 116 AND ASCII(SUBSTRING(named_identity_provider_id,4,1)) = 104 AND ASCII(SUBSTRING(named_identity_provider_id,5,1)) = 117 AND ASCII(SUBSTRING(named_identity_provider_id,6,1)) = 98 AND OCTET_LENGTH(named_identity_profile_id) = 23 AND ASCII(SUBSTRING(named_identity_profile_id,1,1)) = 103 AND ASCII(SUBSTRING(named_identity_profile_id,2,1)) = 105 AND ASCII(SUBSTRING(named_identity_profile_id,3,1)) = 116 AND ASCII(SUBSTRING(named_identity_profile_id,4,1)) = 104 AND ASCII(SUBSTRING(named_identity_profile_id,5,1)) = 117 AND ASCII(SUBSTRING(named_identity_profile_id,6,1)) = 98 AND ASCII(SUBSTRING(named_identity_profile_id,7,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,8,1)) = 99 AND ASCII(SUBSTRING(named_identity_profile_id,9,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,10,1)) = 109 AND ASCII(SUBSTRING(named_identity_profile_id,11,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,12,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,13,1)) = 97 AND ASCII(SUBSTRING(named_identity_profile_id,14,1)) = 117 AND ASCII(SUBSTRING(named_identity_profile_id,15,1)) = 116 AND ASCII(SUBSTRING(named_identity_profile_id,16,1)) = 104 AND ASCII(SUBSTRING(named_identity_profile_id,17,1)) = 45 AND ASCII(SUBSTRING(named_identity_profile_id,18,1)) = 97 AND ASCII(SUBSTRING(named_identity_profile_id,19,1)) = 112 AND ASCII(SUBSTRING(named_identity_profile_id,20,1)) = 112 AND ASCII(SUBSTRING(named_identity_profile_id,21,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,22,1)) = 118 AND ASCII(SUBSTRING(named_identity_profile_id,23,1)) = 49 AND CHAR_LENGTH(named_identity_binding_id) > 0 AND named_identity_binding_created_at IS NOT NULL AND CHAR_LENGTH(named_identity_config_revision) = 64 AND CHAR_LENGTH(named_identity_policy_revision) = 64 AND named_identity_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL)) OR (OCTET_LENGTH(primary_method) = 6 AND ASCII(SUBSTRING(primary_method,1,1)) = 103 AND ASCII(SUBSTRING(primary_method,2,1)) = 111 AND ASCII(SUBSTRING(primary_method,3,1)) = 111 AND ASCII(SUBSTRING(primary_method,4,1)) = 103 AND ASCII(SUBSTRING(primary_method,5,1)) = 108 AND ASCII(SUBSTRING(primary_method,6,1)) = 101 AND OCTET_LENGTH(named_identity_provider_id) = 6 AND ASCII(SUBSTRING(named_identity_provider_id,1,1)) = 103 AND ASCII(SUBSTRING(named_identity_provider_id,2,1)) = 111 AND ASCII(SUBSTRING(named_identity_provider_id,3,1)) = 111 AND ASCII(SUBSTRING(named_identity_provider_id,4,1)) = 103 AND ASCII(SUBSTRING(named_identity_provider_id,5,1)) = 108 AND ASCII(SUBSTRING(named_identity_provider_id,6,1)) = 101 AND OCTET_LENGTH(named_identity_profile_id) = 14 AND ASCII(SUBSTRING(named_identity_profile_id,1,1)) = 103 AND ASCII(SUBSTRING(named_identity_profile_id,2,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,3,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,4,1)) = 103 AND ASCII(SUBSTRING(named_identity_profile_id,5,1)) = 108 AND ASCII(SUBSTRING(named_identity_profile_id,6,1)) = 101 AND ASCII(SUBSTRING(named_identity_profile_id,7,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,8,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,9,1)) = 105 AND ASCII(SUBSTRING(named_identity_profile_id,10,1)) = 100 AND ASCII(SUBSTRING(named_identity_profile_id,11,1)) = 99 AND ASCII(SUBSTRING(named_identity_profile_id,12,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,13,1)) = 118 AND ASCII(SUBSTRING(named_identity_profile_id,14,1)) = 49 AND CHAR_LENGTH(named_identity_binding_id) > 0 AND named_identity_binding_created_at IS NOT NULL AND CHAR_LENGTH(named_identity_config_revision) = 64 AND CHAR_LENGTH(named_identity_policy_revision) = 64 AND named_identity_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL)) OR (OCTET_LENGTH(primary_method) = 7 AND ASCII(SUBSTRING(primary_method,1,1)) = 100 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 115 AND ASCII(SUBSTRING(primary_method,4,1)) = 99 AND ASCII(SUBSTRING(primary_method,5,1)) = 111 AND ASCII(SUBSTRING(primary_method,6,1)) = 114 AND ASCII(SUBSTRING(primary_method,7,1)) = 100 AND OCTET_LENGTH(named_identity_provider_id) = 7 AND ASCII(SUBSTRING(named_identity_provider_id,1,1)) = 100 AND ASCII(SUBSTRING(named_identity_provider_id,2,1)) = 105 AND ASCII(SUBSTRING(named_identity_provider_id,3,1)) = 115 AND ASCII(SUBSTRING(named_identity_provider_id,4,1)) = 99 AND ASCII(SUBSTRING(named_identity_provider_id,5,1)) = 111 AND ASCII(SUBSTRING(named_identity_provider_id,6,1)) = 114 AND ASCII(SUBSTRING(named_identity_provider_id,7,1)) = 100 AND OCTET_LENGTH(named_identity_profile_id) = 17 AND ASCII(SUBSTRING(named_identity_profile_id,1,1)) = 100 AND ASCII(SUBSTRING(named_identity_profile_id,2,1)) = 105 AND ASCII(SUBSTRING(named_identity_profile_id,3,1)) = 115 AND ASCII(SUBSTRING(named_identity_profile_id,4,1)) = 99 AND ASCII(SUBSTRING(named_identity_profile_id,5,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,6,1)) = 114 AND ASCII(SUBSTRING(named_identity_profile_id,7,1)) = 100 AND ASCII(SUBSTRING(named_identity_profile_id,8,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,9,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,10,1)) = 97 AND ASCII(SUBSTRING(named_identity_profile_id,11,1)) = 117 AND ASCII(SUBSTRING(named_identity_profile_id,12,1)) = 116 AND ASCII(SUBSTRING(named_identity_profile_id,13,1)) = 104 AND ASCII(SUBSTRING(named_identity_profile_id,14,1)) = 50 AND ASCII(SUBSTRING(named_identity_profile_id,15,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,16,1)) = 118 AND ASCII(SUBSTRING(named_identity_profile_id,17,1)) = 49 AND CHAR_LENGTH(named_identity_binding_id) > 0 AND named_identity_binding_created_at IS NOT NULL AND CHAR_LENGTH(named_identity_config_revision) = 64 AND CHAR_LENGTH(named_identity_policy_revision) = 64 AND named_identity_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL)" json:"-"`
	OIDCBindingID         string     `gorm:"column:oidc_binding_id;size:30;not null;default:''" json:"-"`
	OIDCBindingCreatedAt  *time.Time `gorm:"column:oidc_binding_created_at;precision:6" json:"-"`
	OIDCConfigRevision    string     `gorm:"column:oidc_config_revision;size:64;not null;default:''" json:"-"`
	OIDCPolicyRevision    string     `gorm:"column:oidc_policy_revision;size:64;not null;default:''" json:"-"`
	OIDCUserCreatedAt     *time.Time `gorm:"column:oidc_user_created_at;precision:6" json:"-"`

	UserID         string    `gorm:"primaryKey;size:30"`
	Purpose        string    `gorm:"primaryKey;size:20"`
	TokenHash      string    `gorm:"size:64;not null;uniqueIndex"`
	PasswordDigest string    `gorm:"size:64;not null"`
	Generation     string    `gorm:"size:64;not null"`
	SessionID      string    `gorm:"size:30;not null"`
	Attempts       int       `gorm:"not null"`
	ExpiresAt      time.Time `gorm:"not null"`
}

func (forcedSSOHistoricalMFAChallengeV102) TableName() string { return "mfa_challenges" }

type forcedSSOHistoricalInstallationV100 struct {
	ID                string    `gorm:"primaryKey;size:30"`
	InstanceID        string    `gorm:"size:30;not null;uniqueIndex:idx_runtime_installation_source,priority:1;index:idx_runtime_installation_instance,priority:1"`
	InstanceStartedAt time.Time `gorm:"precision:6;not null"`
	SnapshotID        string    `gorm:"size:30;not null"`
	ProjectionVersion int       `gorm:"not null;check:ck_runtime_installation_projection,projection_version = 1"`
	SourceDigest      string    `gorm:"size:64;not null;uniqueIndex:idx_runtime_installation_source,priority:2" json:"-"`
	RoutesPublishedAt time.Time `gorm:"precision:6;not null"`
	FirstObservedAt   time.Time `gorm:"precision:6;not null;index:idx_runtime_installation_instance,priority:2"`
}

func (forcedSSOHistoricalInstallationV100) TableName() string {
	return "runtime_installation_observations"
}
