package database

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestSAMLV97FrozenSchemaParity(t *testing.T) {
	for _, pair := range [][2]any{{&samlProviderV97{}, &entity.SAMLProvider{}}, {&samlBindingV97{}, &entity.SAMLBinding{}}, {&samlCeremonyV97{}, &entity.SAMLCeremony{}}, {&samlAssertionReceiptV97{}, &entity.SAMLAssertionReceipt{}}} {
		f, c := oidcV94Schema(t, pair[0]), oidcV94Schema(t, pair[1])
		if f.Table != c.Table || len(f.Fields) != len(c.Fields) || len(f.Relationships.Relations) != 0 || len(c.Relationships.Relations) != 0 {
			t.Fatal("unbounded frozen schema", f.Table)
		}
		for _, field := range f.Fields {
			actual := c.FieldsByDBName[field.DBName]
			if actual == nil || actual.FieldType != field.FieldType || actual.Tag.Get("gorm") != field.Tag.Get("gorm") {
				t.Fatal("frozen field differs", f.Table, field.DBName)
			}
		}
	}
	for _, pair := range [][2]any{{&samlSessionV97{}, &entity.Session{}}, {&samlMFAChallengeV97{}, &entity.MFAChallenge{}}} {
		f, c := oidcV94Schema(t, pair[0]), oidcV94Schema(t, pair[1])
		if f.Table != c.Table {
			t.Fatal("wrong proof table")
		}
		for _, field := range f.Fields {
			actual := c.FieldsByDBName[field.DBName]
			if actual == nil || actual.FieldType != field.FieldType || actual.Tag.Get("gorm") != field.Tag.Get("gorm") {
				t.Fatal("physical proof differs", field.DBName)
			}
		}
	}
}
func TestSAMLV97PartialColumnValidation(t *testing.T) {
	models := []any{&samlProviderV97{}, &samlBindingV97{}, &samlCeremonyV97{}, &samlAssertionReceiptV97{}, &samlSessionV97{}, &samlMFAChallengeV97{}}
	for _, model := range models {
		s := oidcV94Schema(t, model)
		for _, field := range s.Fields {
			faults := []string{"missing", "unknown_nullability"}
			if field.NotNull || field.PrimaryKey {
				faults = append(faults, "nullable")
			}
			if field.PrimaryKey {
				faults = append(faults, "missing_primary", "unknown_primary")
			}
			if field.FieldType.Kind() == reflect.Pointer {
				faults = append(faults, "not_nullable")
			}
			switch string(field.DataType) {
			case "time":
				faults = append(faults, "precision3", "unknown_precision", "wrong_kind", "invented_time_default")
			case "bool", "text":
				faults = append(faults, "wrong_kind")
			case "string":
				faults = append(faults, "wrong_kind", "wrong_size", "unknown_size")
			}
			if !field.HasDefaultValue && string(field.DataType) != "time" {
				faults = append(faults, "invented_default")
			}
			if field.HasDefaultValue {
				faults = append(faults, "missing_default", "changed_default")
			}
			for _, fault := range faults {
				t.Run(s.Table+"/"+field.DBName+"/"+fault, func(t *testing.T) {
					m := &oidcV94ColumnsMigrator{columns: oidcV94Columns(t, model)}
					db := projectRateCheckDB(t, m)
					if err := validateSAMLColumnsV97(db, model); err != nil {
						t.Fatal("invalid positive metadata baseline", err)
					}
					for i, column := range m.columns {
						c := column.(oidcV94Column)
						if c.name != field.DBName {
							continue
						}
						switch fault {
						case "missing":
							m.columns = append(m.columns[:i], m.columns[i+1:]...)
						case "unknown_nullability":
							c.nullableKnown = false
						case "nullable":
							c.nullable = true
						case "not_nullable":
							c.nullable = false
						case "invented_default":
							c.defaultKnown = true
							c.def = "invented"
						case "invented_time_default":
							c.defaultKnown = true
							c.def = "CURRENT_TIMESTAMP"
						case "missing_primary":
							c.primary = false
						case "unknown_primary":
							c.primaryKnown = false
						case "precision3":
							c.precision = 3
						case "unknown_precision":
							c.precisionKnown = false
						case "wrong_kind":
							c.kind = "integer"
						case "wrong_size":
							c.size--
						case "unknown_size":
							c.sized = false
						case "missing_default":
							c.defaultKnown = false
						case "changed_default":
							c.def = "oidc"
						}
						if fault != "missing" {
							m.columns[i] = c
						}
						break
					}
					if err := validateSAMLColumnsV97(db, model); err == nil || !strings.Contains(err.Error(), field.DBName) {
						t.Fatal("incompatible partial column was not identified", field.DBName, fault, err)
					}
				})
			}
		}
		m := &oidcV94ColumnsMigrator{err: errors.New("OIDC metadata unavailable")}
		if err := validateSAMLColumnsV97(projectRateCheckDB(t, m), model); !errors.Is(err, m.err) {
			t.Fatal("lost metadata failure", err)
		}
	}
}

func TestSAMLV97PortableTimestampAndDefaultMetadata(t *testing.T) {
	for _, kind := range []string{"timestamp", "timestamptz", "datetime"} {
		t.Run(kind, func(t *testing.T) {
			m := &oidcV94ColumnsMigrator{columns: oidcV94Columns(t, &samlProviderV97{})}
			for i, value := range m.columns {
				c := value.(oidcV94Column)
				if c.kind == "timestamp" {
					c.kind = kind
					m.columns[i] = c
				}
			}
			if err := validateSAMLColumnsV97(projectRateCheckDB(t, m), &samlProviderV97{}); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, def := range []string{"NULL", "null"} {
		m := &oidcV94ColumnsMigrator{columns: oidcV94Columns(t, &samlProviderV97{})}
		for i, value := range m.columns {
			c := value.(oidcV94Column)
			if c.kind == "timestamp" {
				c.defaultKnown = true
				c.def = def
				m.columns[i] = c
			}
		}
		if err := validateSAMLColumnsV97(projectRateCheckDB(t, m), &samlProviderV97{}); err != nil {
			t.Fatal("SQL NULL is not an invented timestamp", def, err)
		}
	}
	for _, def := range []string{"", "''", "''::character varying"} {
		m := &oidcV94ColumnsMigrator{columns: oidcV94Columns(t, &samlSessionV97{})}
		for i, value := range m.columns {
			c := value.(oidcV94Column)
			if c.defaultKnown {
				c.def = def
				m.columns[i] = c
			}
		}
		if err := validateSAMLColumnsV97(projectRateCheckDB(t, m), &samlSessionV97{}); err != nil {
			t.Fatal("portable blank default rejected", def, err)
		}
	}
}

func TestSAMLV97PrimaryCheckPreservesReleasedMethodsAndRejectsMixedProof(t *testing.T) {
	blank := func(p string) string {
		return "OCTET_LENGTH(" + p + "_binding_id) = 0 AND " + p + "_binding_created_at IS NULL AND OCTET_LENGTH(" + p + "_config_revision) = 0 AND OCTET_LENGTH(" + p + "_policy_revision) = 0 AND " + p + "_user_created_at IS NULL"
	}
	complete := func(p string) string {
		return "CHAR_LENGTH(" + p + "_binding_id) > 0 AND " + p + "_binding_created_at IS NOT NULL AND CHAR_LENGTH(" + p + "_config_revision) = 64 AND CHAR_LENGTH(" + p + "_policy_revision) = 64 AND " + p + "_user_created_at IS NOT NULL"
	}
	expected := "(OCTET_LENGTH(primary_method) = 0 AND " + blank("oidc") + " AND " + blank("oauth") + " AND " + blank("ldap") + " AND " + blank("saml") + ")"
	for _, method := range []string{"oidc", "oauth", "ldap", "saml"} {
		expected += " OR (" + oidcV94ASCII("primary_method", method) + " AND " + complete(method)
		for _, other := range []string{"oidc", "oauth", "ldap", "saml"} {
			if other != method {
				expected += " AND " + blank(other)
			}
		}
		expected += ")"
	}
	for _, v := range []struct {
		model any
		name  string
	}{{&samlSessionProofV97{}, "ck_sessions_oidc_primary"}, {&samlMFAProofV97{}, "ck_mfa_challenges_oidc_primary"}} {
		if oidcV94Check(t, v.model, v.name) != expected {
			t.Fatal("primary alternatives/blank proof changed", v.name)
		}
	}
	if oidcV94Check(t, &samlProviderV97{}, "ck_saml_singleton") != oidcV94ASCII("id", "saml") || oidcV94Check(t, &samlBindingV97{}, "ck_saml_binding_provider") != oidcV94ASCII("provider_id", "saml") {
		t.Fatal("PAD SPACE singleton admitted")
	}
}
func TestSAMLV97PrivateProofAndPublicTrust(t *testing.T) {
	for _, v := range []struct {
		model  any
		fields []string
	}{{&entity.SAMLBinding{}, []string{"Issuer", "Subject", "SubjectDigest"}}, {&entity.SAMLCeremony{}, []string{"RelayHash", "CookieHash", "DeliveryHash", "Subject", "AssertionDigest", "PasswordDigest", "MFAGeneration"}}, {&entity.SAMLAssertionReceipt{}, []string{"Digest"}}} {
		s := oidcV94Schema(t, v.model)
		for _, name := range v.fields {
			if s.FieldsByName[name].Tag.Get("json") != "-" {
				t.Fatal("private proof projection", name)
			}
		}
	}

}
