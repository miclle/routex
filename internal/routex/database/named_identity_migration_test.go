package database

import (
	"errors"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestNamedIdentityV98FrozenCurrentParity(t *testing.T) {
	for _, pair := range [][2]any{{&namedIdentityProviderV98{}, &namedIdentityHistoricalProviderV98{}}, {&namedIdentityBindingV98{}, &namedIdentityHistoricalBindingV98{}}, {&namedIdentityCeremonyV98{}, &namedIdentityHistoricalCeremonyV98{}}, {&namedIdentitySessionV98{}, &namedIdentityHistoricalSessionV98{}}, {&namedIdentityMFAChallengeV98{}, &namedIdentityHistoricalMFAChallengeV98{}}, {&namedIdentitySessionProofV98{}, &namedIdentityHistoricalSessionV98{}}, {&namedIdentityMFAChallengeProofV98{}, &namedIdentityHistoricalMFAChallengeV98{}}, {&namedIdentityRootJobV98{}, &namedIdentityHistoricalSecretRotationJobV98{}}, {&namedIdentityRootItemV98{}, &entity.SecretRotationItem{}}, {&namedIdentityRootProcessV98{}, &namedIdentityHistoricalSecretProcessVerificationV98{}}} {
		f, c := oidcV94Schema(t, pair[0]), oidcV94Schema(t, pair[1])
		if f.Table != c.Table || len(f.Relationships.Relations) != 0 {
			t.Fatal("frozen table", f.Table)
		}
		for _, field := range f.Fields {
			actual := c.FieldsByDBName[field.DBName]
			if actual == nil || actual.FieldType != field.FieldType || actual.Tag.Get("gorm") != field.Tag.Get("gorm") {
				t.Fatal("frozen projection", f.Table, field.DBName)
			}
		}
	}
}
func TestNamedIdentityV98ExactProfileAndHistoricalPrimaryPrefix(t *testing.T) {
	legacy := oidcV94Check(t, &samlSessionProofV97{}, "ck_sessions_oidc_primary")
	for _, model := range []any{&namedIdentitySessionProofV98{}, &namedIdentityMFAChallengeProofV98{}} {
		name := "ck_sessions_oidc_primary"
		if _, ok := model.(*namedIdentityMFAChallengeProofV98); ok {
			name = "ck_mfa_challenges_oidc_primary"
		}
		value := oidcV94Check(t, model, name)
		if !strings.HasPrefix(value, "(("+legacy+") AND ") || !strings.Contains(value, oidcV94ASCII("primary_method", "github")) || !strings.Contains(value, oidcV94ASCII("named_identity_provider_id", "github")) || !strings.Contains(value, oidcV94ASCII("named_identity_profile_id", "github.com.oauth-app.v1")) {
			t.Fatal("released prefix or exact namespace changed")
		}
		for _, column := range []string{"named_identity_provider_id", "named_identity_profile_id", "named_identity_binding_id", "named_identity_config_revision", "named_identity_policy_revision"} {
			if !strings.Contains(value, "OCTET_LENGTH("+column+") = 0") {
				t.Fatal("PAD SPACE blank", column)
			}
		}
	}
	for _, model := range []any{&namedIdentityProviderV98{}, &namedIdentityBindingV98{}, &namedIdentityCeremonyV98{}} {
		schema := oidcV94Schema(t, model)
		for _, column := range []string{"profile_id", "identity_issuer"} {
			f := schema.FieldsByDBName[column]
			if f == nil || !strings.Contains(f.Tag.Get("gorm"), "OCTET_LENGTH(") {
				t.Fatal("unbounded named namespace")
			}
		}
	}
	// Exact ASCII predicates reject case aliases and PAD SPACE values on both drivers.
	for _, model := range []any{&namedIdentityCeremonyV98{}, &namedIdentityHistoricalCeremonyV98{}, &entity.NamedIdentityCeremony{}} {
		for _, enum := range []struct {
			column string
			values []string
		}{
			{"purpose", []string{"login", "bind", "verify"}},
			{"status", []string{"pending", "exchanging", "verified", "consumed", "failed"}},
		} {
			parts := make([]string, 0, len(enum.values))
			for _, value := range enum.values {
				parts = append(parts, "("+oidcV94ASCII(enum.column, value)+")")
			}
			if actual := oidcV94Check(t, model, "ck_named_identity_ceremonies_"+enum.column); actual != strings.Join(parts, " OR ") {
				t.Fatal("non-exact named ceremony enum", enum.column)
			}
		}
	}
}
func TestNamedIdentityV98ChecksValidateBeforeDDLAndResume(t *testing.T) {
	names := []string{"ck_named_identity_ceremonies_purpose", "ck_named_identity_ceremonies_status", "ck_sessions_oidc_primary", "ck_mfa_challenges_oidc_primary", "ck_secret_inventory_version", "ck_secret_rotation_domain", "ck_secret_item_domain", "ck_secret_process_inventory_version"}
	faults := []string{"", "bad:named_identity_ceremonies", "query:named_identity_ceremonies", "bad:enum:purpose", "bad:enum:status", "query:enum:purpose", "query:enum:status", "bad:sessions", "bad:mfa_challenges", "bad:secret_rotation_jobs", "bad:secret_rotation_items", "bad:secret_process_verifications", "query:sessions", "query:secret_rotation_jobs"}
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
			if err := db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
				queries++
				where, ok := tx.Statement.Clauses["WHERE"].Expression.(clause.Where)
				if !ok || len(where.Exprs) != 1 {
					t.Fatal("missing retained predicate")
				}
				expr, ok := where.Exprs[0].(clause.Expr)
				if !ok || !strings.HasPrefix(expr.SQL, "NOT (") || len(expr.Vars) != 0 {
					t.Fatal("unowned retained expression")
				}
				if m.fail == "query:"+tx.Statement.Table || tx.Statement.Table == "named_identity_ceremonies" && ((m.fail == "query:enum:purpose" && strings.Contains(expr.SQL, "OCTET_LENGTH(purpose)")) || (m.fail == "query:enum:status" && strings.Contains(expr.SQL, "OCTET_LENGTH(status)"))) {
					_ = tx.AddError(errors.New(m.fail))
					return
				}
				count, ok := tx.Statement.Dest.(*int64)
				if !ok {
					t.Fatal("unbounded retained query")
				}
				*count = 0
				if m.fail == "bad:"+tx.Statement.Table || tx.Statement.Table == "named_identity_ceremonies" && ((m.fail == "bad:enum:purpose" && strings.Contains(expr.SQL, "OCTET_LENGTH(purpose)")) || (m.fail == "bad:enum:status" && strings.Contains(expr.SQL, "OCTET_LENGTH(status)"))) {
					*count = 1
				}
				tx.RowsAffected = 1
			}); err != nil {
				t.Fatal(err)
			}
			err := namedIdentityConstraintsV98(db)
			if (err != nil) != (fault != "") {
				t.Fatal("lost retained/DDL failure", err)
			}
			if strings.HasPrefix(fault, "bad:") || strings.HasPrefix(fault, "query:") {
				if len(m.ops) != 0 {
					t.Fatal("removed constraint before retained proof")
				}
			}
			m.fail = ""
			for range 2 {
				if err := namedIdentityConstraintsV98(db); err != nil {
					t.Fatal("partial DDL did not resume", err)
				}
				for _, name := range names {
					if !m.checks[name] {
						t.Fatal("missing resumed check", name)
					}
				}
			}
			if queries < 2*len(names) {
				t.Fatal("repeat skipped retained validation")
			}
		})
	}
}
func TestNamedIdentityV98PartialColumnValidation(t *testing.T) {
	models := []any{&namedIdentityProviderV98{}, &namedIdentityBindingV98{}, &namedIdentityCeremonyV98{}, &namedIdentitySessionV98{}, &namedIdentityMFAChallengeV98{}}
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
					if err := validateNamedIdentityColumnsV98(db, model); err != nil {
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
					if err := validateNamedIdentityColumnsV98(db, model); err == nil || !strings.Contains(err.Error(), field.DBName) {
						t.Fatal("incompatible partial column was not identified", field.DBName, fault, err)
					}
				})
			}
		}
		m := &oidcV94ColumnsMigrator{err: errors.New("OIDC metadata unavailable")}
		if err := validateNamedIdentityColumnsV98(projectRateCheckDB(t, m), model); !errors.Is(err, m.err) {
			t.Fatal("lost metadata failure", err)
		}
	}
}

func TestNamedIdentityV98PortableTimestampAndDefaultMetadata(t *testing.T) {
	for _, kind := range []string{"timestamp", "timestamptz", "datetime"} {
		t.Run(kind, func(t *testing.T) {
			m := &oidcV94ColumnsMigrator{columns: oidcV94Columns(t, &namedIdentityProviderV98{})}
			for i, value := range m.columns {
				c := value.(oidcV94Column)
				if c.kind == "timestamp" {
					c.kind = kind
					m.columns[i] = c
				}
			}
			if err := validateNamedIdentityColumnsV98(projectRateCheckDB(t, m), &namedIdentityProviderV98{}); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, def := range []string{"NULL", "null"} {
		m := &oidcV94ColumnsMigrator{columns: oidcV94Columns(t, &namedIdentityProviderV98{})}
		for i, value := range m.columns {
			c := value.(oidcV94Column)
			if c.kind == "timestamp" {
				c.defaultKnown = true
				c.def = def
				m.columns[i] = c
			}
		}
		if err := validateNamedIdentityColumnsV98(projectRateCheckDB(t, m), &namedIdentityProviderV98{}); err != nil {
			t.Fatal("SQL NULL is not an invented timestamp", def, err)
		}
	}
	for _, def := range []string{"", "''", "''::character varying"} {
		m := &oidcV94ColumnsMigrator{columns: oidcV94Columns(t, &namedIdentitySessionV98{})}
		for i, value := range m.columns {
			c := value.(oidcV94Column)
			if c.defaultKnown {
				c.def = def
				m.columns[i] = c
			}
		}
		if err := validateNamedIdentityColumnsV98(projectRateCheckDB(t, m), &namedIdentitySessionV98{}); err != nil {
			t.Fatal("portable blank default rejected", def, err)
		}
	}
}

type namedIdentityHistoricalProviderV98 struct {
	ProfileID                string     `gorm:"size:64;not null;check:ck_namedidentityprovider_profile,OCTET_LENGTH(profile_id) = 23 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 105 AND ASCII(SUBSTRING(profile_id,3,1)) = 116 AND ASCII(SUBSTRING(profile_id,4,1)) = 104 AND ASCII(SUBSTRING(profile_id,5,1)) = 117 AND ASCII(SUBSTRING(profile_id,6,1)) = 98 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 99 AND ASCII(SUBSTRING(profile_id,9,1)) = 111 AND ASCII(SUBSTRING(profile_id,10,1)) = 109 AND ASCII(SUBSTRING(profile_id,11,1)) = 46 AND ASCII(SUBSTRING(profile_id,12,1)) = 111 AND ASCII(SUBSTRING(profile_id,13,1)) = 97 AND ASCII(SUBSTRING(profile_id,14,1)) = 117 AND ASCII(SUBSTRING(profile_id,15,1)) = 116 AND ASCII(SUBSTRING(profile_id,16,1)) = 104 AND ASCII(SUBSTRING(profile_id,17,1)) = 45 AND ASCII(SUBSTRING(profile_id,18,1)) = 97 AND ASCII(SUBSTRING(profile_id,19,1)) = 112 AND ASCII(SUBSTRING(profile_id,20,1)) = 112 AND ASCII(SUBSTRING(profile_id,21,1)) = 46 AND ASCII(SUBSTRING(profile_id,22,1)) = 118 AND ASCII(SUBSTRING(profile_id,23,1)) = 49" json:"-"`
	IdentityIssuer           string     `gorm:"size:2048;not null;check:ck_namedidentityprovider_issuer,OCTET_LENGTH(identity_issuer) = 18 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 105 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 98 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 109" json:"-"`
	ID                       string     `gorm:"primaryKey;size:30;check:ck_named_identity_singleton,OCTET_LENGTH(id) = 6 AND ASCII(SUBSTRING(id,1,1)) = 103 AND ASCII(SUBSTRING(id,2,1)) = 105 AND ASCII(SUBSTRING(id,3,1)) = 116 AND ASCII(SUBSTRING(id,4,1)) = 104 AND ASCII(SUBSTRING(id,5,1)) = 117 AND ASCII(SUBSTRING(id,6,1)) = 98"`
	ReviewRevision           string     `gorm:"size:64;not null"`
	ConfigRevision           string     `gorm:"size:64;not null"`
	PolicyRevision           string     `gorm:"size:64;not null"`
	Name                     string     `gorm:"size:100;not null"`
	ClientID                 string     `gorm:"size:256;not null"`
	CallbackURL              string     `gorm:"size:2048;not null"`
	SecretGeneration         string     `gorm:"size:64;not null"`
	AuthCiphertext           string     `gorm:"type:text;not null" json:"-"`
	Enabled                  bool       `gorm:"not null"`
	VerifiedConfigRevision   string     `gorm:"size:64;not null"`
	VerifiedBy               string     `gorm:"size:30;not null"`
	VerifiedUserCreatedAt    *time.Time `gorm:"precision:6"`
	VerifiedBindingID        string     `gorm:"size:30;not null"`
	VerifiedBindingCreatedAt *time.Time `gorm:"precision:6"`
	CreatedAt                time.Time  `gorm:"precision:6;not null"`
	UpdatedAt                time.Time  `gorm:"precision:6;not null"`
}

func (namedIdentityHistoricalProviderV98) TableName() string { return "named_identity_providers" }

type namedIdentityHistoricalBindingV98 struct {
	ProfileID      string    `gorm:"size:64;not null;check:ck_namedidentitybinding_profile,OCTET_LENGTH(profile_id) = 23 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 105 AND ASCII(SUBSTRING(profile_id,3,1)) = 116 AND ASCII(SUBSTRING(profile_id,4,1)) = 104 AND ASCII(SUBSTRING(profile_id,5,1)) = 117 AND ASCII(SUBSTRING(profile_id,6,1)) = 98 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 99 AND ASCII(SUBSTRING(profile_id,9,1)) = 111 AND ASCII(SUBSTRING(profile_id,10,1)) = 109 AND ASCII(SUBSTRING(profile_id,11,1)) = 46 AND ASCII(SUBSTRING(profile_id,12,1)) = 111 AND ASCII(SUBSTRING(profile_id,13,1)) = 97 AND ASCII(SUBSTRING(profile_id,14,1)) = 117 AND ASCII(SUBSTRING(profile_id,15,1)) = 116 AND ASCII(SUBSTRING(profile_id,16,1)) = 104 AND ASCII(SUBSTRING(profile_id,17,1)) = 45 AND ASCII(SUBSTRING(profile_id,18,1)) = 97 AND ASCII(SUBSTRING(profile_id,19,1)) = 112 AND ASCII(SUBSTRING(profile_id,20,1)) = 112 AND ASCII(SUBSTRING(profile_id,21,1)) = 46 AND ASCII(SUBSTRING(profile_id,22,1)) = 118 AND ASCII(SUBSTRING(profile_id,23,1)) = 49" json:"-"`
	IdentityIssuer string    `gorm:"size:2048;not null;check:ck_namedidentitybinding_issuer,OCTET_LENGTH(identity_issuer) = 18 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 105 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 98 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 109" json:"-"`
	ProviderID     string    `gorm:"size:30;not null;uniqueIndex:idx_named_identity_member,priority:1;check:ck_named_identity_binding_provider,OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 105 AND ASCII(SUBSTRING(provider_id,3,1)) = 116 AND ASCII(SUBSTRING(provider_id,4,1)) = 104 AND ASCII(SUBSTRING(provider_id,5,1)) = 117 AND ASCII(SUBSTRING(provider_id,6,1)) = 98"`
	SubjectKind    string    `gorm:"size:20;not null;check:ck_named_identity_binding_kind,OCTET_LENGTH(subject_kind) = 7 AND ASCII(SUBSTRING(subject_kind,1,1)) = 105 AND ASCII(SUBSTRING(subject_kind,2,1)) = 110 AND ASCII(SUBSTRING(subject_kind,3,1)) = 116 AND ASCII(SUBSTRING(subject_kind,4,1)) = 101 AND ASCII(SUBSTRING(subject_kind,5,1)) = 103 AND ASCII(SUBSTRING(subject_kind,6,1)) = 101 AND ASCII(SUBSTRING(subject_kind,7,1)) = 114" json:"-"`
	ID             string    `gorm:"primaryKey;size:30"`
	UserID         string    `gorm:"size:30;not null;uniqueIndex:idx_named_identity_member,priority:2"`
	UserCreatedAt  time.Time `gorm:"precision:6;not null"`
	ConfigRevision string    `gorm:"size:64;not null"`
	Subject        string    `gorm:"size:256;not null" json:"-"`
	SubjectDigest  string    `gorm:"size:64;not null;uniqueIndex" json:"-"`
	CreatedAt      time.Time `gorm:"precision:6;not null"`
}

func (namedIdentityHistoricalBindingV98) TableName() string { return "named_identity_bindings" }

type namedIdentityHistoricalCeremonyV98 struct {
	ProviderCreatedAt time.Time  `gorm:"precision:6;not null"`
	ConsumedAt        *time.Time `gorm:"precision:6" json:"-"`
	ProfileID         string     `gorm:"size:64;not null;check:ck_namedidentityceremony_profile,OCTET_LENGTH(profile_id) = 23 AND ASCII(SUBSTRING(profile_id,1,1)) = 103 AND ASCII(SUBSTRING(profile_id,2,1)) = 105 AND ASCII(SUBSTRING(profile_id,3,1)) = 116 AND ASCII(SUBSTRING(profile_id,4,1)) = 104 AND ASCII(SUBSTRING(profile_id,5,1)) = 117 AND ASCII(SUBSTRING(profile_id,6,1)) = 98 AND ASCII(SUBSTRING(profile_id,7,1)) = 46 AND ASCII(SUBSTRING(profile_id,8,1)) = 99 AND ASCII(SUBSTRING(profile_id,9,1)) = 111 AND ASCII(SUBSTRING(profile_id,10,1)) = 109 AND ASCII(SUBSTRING(profile_id,11,1)) = 46 AND ASCII(SUBSTRING(profile_id,12,1)) = 111 AND ASCII(SUBSTRING(profile_id,13,1)) = 97 AND ASCII(SUBSTRING(profile_id,14,1)) = 117 AND ASCII(SUBSTRING(profile_id,15,1)) = 116 AND ASCII(SUBSTRING(profile_id,16,1)) = 104 AND ASCII(SUBSTRING(profile_id,17,1)) = 45 AND ASCII(SUBSTRING(profile_id,18,1)) = 97 AND ASCII(SUBSTRING(profile_id,19,1)) = 112 AND ASCII(SUBSTRING(profile_id,20,1)) = 112 AND ASCII(SUBSTRING(profile_id,21,1)) = 46 AND ASCII(SUBSTRING(profile_id,22,1)) = 118 AND ASCII(SUBSTRING(profile_id,23,1)) = 49" json:"-"`
	IdentityIssuer    string     `gorm:"size:2048;not null;check:ck_namedidentityceremony_issuer,OCTET_LENGTH(identity_issuer) = 18 AND ASCII(SUBSTRING(identity_issuer,1,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,2,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,3,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,4,1)) = 112 AND ASCII(SUBSTRING(identity_issuer,5,1)) = 115 AND ASCII(SUBSTRING(identity_issuer,6,1)) = 58 AND ASCII(SUBSTRING(identity_issuer,7,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,8,1)) = 47 AND ASCII(SUBSTRING(identity_issuer,9,1)) = 103 AND ASCII(SUBSTRING(identity_issuer,10,1)) = 105 AND ASCII(SUBSTRING(identity_issuer,11,1)) = 116 AND ASCII(SUBSTRING(identity_issuer,12,1)) = 104 AND ASCII(SUBSTRING(identity_issuer,13,1)) = 117 AND ASCII(SUBSTRING(identity_issuer,14,1)) = 98 AND ASCII(SUBSTRING(identity_issuer,15,1)) = 46 AND ASCII(SUBSTRING(identity_issuer,16,1)) = 99 AND ASCII(SUBSTRING(identity_issuer,17,1)) = 111 AND ASCII(SUBSTRING(identity_issuer,18,1)) = 109" json:"-"`
	ProviderID        string     `gorm:"size:30;not null;check:ck_named_identity_ceremony_provider,OCTET_LENGTH(provider_id) = 6 AND ASCII(SUBSTRING(provider_id,1,1)) = 103 AND ASCII(SUBSTRING(provider_id,2,1)) = 105 AND ASCII(SUBSTRING(provider_id,3,1)) = 116 AND ASCII(SUBSTRING(provider_id,4,1)) = 104 AND ASCII(SUBSTRING(provider_id,5,1)) = 117 AND ASCII(SUBSTRING(provider_id,6,1)) = 98"`
	SubjectKind       string     `gorm:"size:20;not null;check:ck_named_identity_ceremony_kind,OCTET_LENGTH(subject_kind) = 0 OR (OCTET_LENGTH(subject_kind) = 7 AND ASCII(SUBSTRING(subject_kind,1,1)) = 105 AND ASCII(SUBSTRING(subject_kind,2,1)) = 110 AND ASCII(SUBSTRING(subject_kind,3,1)) = 116 AND ASCII(SUBSTRING(subject_kind,4,1)) = 101 AND ASCII(SUBSTRING(subject_kind,5,1)) = 103 AND ASCII(SUBSTRING(subject_kind,6,1)) = 101 AND ASCII(SUBSTRING(subject_kind,7,1)) = 114)" json:"-"`
	ID                string     `gorm:"primaryKey;size:30"`
	StateHash         string     `gorm:"size:64;not null;uniqueIndex" json:"-"`
	CookieHash        string     `gorm:"size:64;not null;uniqueIndex" json:"-"`
	Purpose           string     `gorm:"size:20;not null;check:ck_named_identity_ceremonies_purpose,(OCTET_LENGTH(purpose) = 5 AND ASCII(SUBSTRING(purpose,1,1)) = 108 AND ASCII(SUBSTRING(purpose,2,1)) = 111 AND ASCII(SUBSTRING(purpose,3,1)) = 103 AND ASCII(SUBSTRING(purpose,4,1)) = 105 AND ASCII(SUBSTRING(purpose,5,1)) = 110) OR (OCTET_LENGTH(purpose) = 4 AND ASCII(SUBSTRING(purpose,1,1)) = 98 AND ASCII(SUBSTRING(purpose,2,1)) = 105 AND ASCII(SUBSTRING(purpose,3,1)) = 110 AND ASCII(SUBSTRING(purpose,4,1)) = 100) OR (OCTET_LENGTH(purpose) = 6 AND ASCII(SUBSTRING(purpose,1,1)) = 118 AND ASCII(SUBSTRING(purpose,2,1)) = 101 AND ASCII(SUBSTRING(purpose,3,1)) = 114 AND ASCII(SUBSTRING(purpose,4,1)) = 105 AND ASCII(SUBSTRING(purpose,5,1)) = 102 AND ASCII(SUBSTRING(purpose,6,1)) = 121)"`
	Reason            string     `gorm:"size:1024;not null"`
	Status            string     `gorm:"size:20;not null;check:ck_named_identity_ceremonies_status,(OCTET_LENGTH(status) = 7 AND ASCII(SUBSTRING(status,1,1)) = 112 AND ASCII(SUBSTRING(status,2,1)) = 101 AND ASCII(SUBSTRING(status,3,1)) = 110 AND ASCII(SUBSTRING(status,4,1)) = 100 AND ASCII(SUBSTRING(status,5,1)) = 105 AND ASCII(SUBSTRING(status,6,1)) = 110 AND ASCII(SUBSTRING(status,7,1)) = 103) OR (OCTET_LENGTH(status) = 10 AND ASCII(SUBSTRING(status,1,1)) = 101 AND ASCII(SUBSTRING(status,2,1)) = 120 AND ASCII(SUBSTRING(status,3,1)) = 99 AND ASCII(SUBSTRING(status,4,1)) = 104 AND ASCII(SUBSTRING(status,5,1)) = 97 AND ASCII(SUBSTRING(status,6,1)) = 110 AND ASCII(SUBSTRING(status,7,1)) = 103 AND ASCII(SUBSTRING(status,8,1)) = 105 AND ASCII(SUBSTRING(status,9,1)) = 110 AND ASCII(SUBSTRING(status,10,1)) = 103) OR (OCTET_LENGTH(status) = 8 AND ASCII(SUBSTRING(status,1,1)) = 118 AND ASCII(SUBSTRING(status,2,1)) = 101 AND ASCII(SUBSTRING(status,3,1)) = 114 AND ASCII(SUBSTRING(status,4,1)) = 105 AND ASCII(SUBSTRING(status,5,1)) = 102 AND ASCII(SUBSTRING(status,6,1)) = 105 AND ASCII(SUBSTRING(status,7,1)) = 101 AND ASCII(SUBSTRING(status,8,1)) = 100) OR (OCTET_LENGTH(status) = 8 AND ASCII(SUBSTRING(status,1,1)) = 99 AND ASCII(SUBSTRING(status,2,1)) = 111 AND ASCII(SUBSTRING(status,3,1)) = 110 AND ASCII(SUBSTRING(status,4,1)) = 115 AND ASCII(SUBSTRING(status,5,1)) = 117 AND ASCII(SUBSTRING(status,6,1)) = 109 AND ASCII(SUBSTRING(status,7,1)) = 101 AND ASCII(SUBSTRING(status,8,1)) = 100) OR (OCTET_LENGTH(status) = 6 AND ASCII(SUBSTRING(status,1,1)) = 102 AND ASCII(SUBSTRING(status,2,1)) = 97 AND ASCII(SUBSTRING(status,3,1)) = 105 AND ASCII(SUBSTRING(status,4,1)) = 108 AND ASCII(SUBSTRING(status,5,1)) = 101 AND ASCII(SUBSTRING(status,6,1)) = 100)"`
	ConfigRevision    string     `gorm:"size:64;not null"`
	PolicyRevision    string     `gorm:"size:64;not null"`
	UserID            string     `gorm:"size:30;not null"`
	UserCreatedAt     *time.Time `gorm:"precision:6"`
	PasswordDigest    string     `gorm:"size:64;not null" json:"-"`
	MFAGeneration     string     `gorm:"size:64;not null" json:"-"`
	SessionID         string     `gorm:"size:30;not null"`
	SessionCreatedAt  *time.Time `gorm:"precision:6"`
	Subject           string     `gorm:"size:256;not null" json:"-"`
	BindingID         string     `gorm:"size:30;not null"`
	BindingCreatedAt  *time.Time `gorm:"precision:6"`
	ExpiresAt         time.Time  `gorm:"precision:6;not null;index"`
	CreatedAt         time.Time  `gorm:"precision:6;not null"`
	VerifiedAt        *time.Time `gorm:"precision:6"`
}

func (namedIdentityHistoricalCeremonyV98) TableName() string { return "named_identity_ceremonies" }

type namedIdentityHistoricalSessionV98 struct {
	NamedIdentityProviderID       string     `gorm:"column:named_identity_provider_id;size:30;not null;default:''" json:"-"`
	NamedIdentityProfileID        string     `gorm:"column:named_identity_profile_id;size:64;not null;default:''" json:"-"`
	NamedIdentityBindingID        string     `gorm:"column:named_identity_binding_id;size:30;not null;default:''" json:"-"`
	NamedIdentityBindingCreatedAt *time.Time `gorm:"column:named_identity_binding_created_at;precision:6" json:"-"`
	NamedIdentityConfigRevision   string     `gorm:"column:named_identity_config_revision;size:64;not null;default:''" json:"-"`
	NamedIdentityPolicyRevision   string     `gorm:"column:named_identity_policy_revision;size:64;not null;default:''" json:"-"`
	NamedIdentityUserCreatedAt    *time.Time `gorm:"column:named_identity_user_created_at;precision:6" json:"-"`
	PrimaryMethod                 string     `gorm:"size:20;not null;default:'';check:ck_sessions_oidc_primary,(((OCTET_LENGTH(primary_method) = 0 AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 100 AND ASCII(SUBSTRING(primary_method,4,1)) = 99 AND CHAR_LENGTH(oidc_binding_id) > 0 AND oidc_binding_created_at IS NOT NULL AND CHAR_LENGTH(oidc_config_revision) = 64 AND CHAR_LENGTH(oidc_policy_revision) = 64 AND oidc_user_created_at IS NOT NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 5 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 97 AND ASCII(SUBSTRING(primary_method,3,1)) = 117 AND ASCII(SUBSTRING(primary_method,4,1)) = 116 AND ASCII(SUBSTRING(primary_method,5,1)) = 104 AND CHAR_LENGTH(oauth_binding_id) > 0 AND oauth_binding_created_at IS NOT NULL AND CHAR_LENGTH(oauth_config_revision) = 64 AND CHAR_LENGTH(oauth_policy_revision) = 64 AND oauth_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 108 AND ASCII(SUBSTRING(primary_method,2,1)) = 100 AND ASCII(SUBSTRING(primary_method,3,1)) = 97 AND ASCII(SUBSTRING(primary_method,4,1)) = 112 AND CHAR_LENGTH(ldap_binding_id) > 0 AND ldap_binding_created_at IS NOT NULL AND CHAR_LENGTH(ldap_config_revision) = 64 AND CHAR_LENGTH(ldap_policy_revision) = 64 AND ldap_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 115 AND ASCII(SUBSTRING(primary_method,2,1)) = 97 AND ASCII(SUBSTRING(primary_method,3,1)) = 109 AND ASCII(SUBSTRING(primary_method,4,1)) = 108 AND CHAR_LENGTH(saml_binding_id) > 0 AND saml_binding_created_at IS NOT NULL AND CHAR_LENGTH(saml_config_revision) = 64 AND CHAR_LENGTH(saml_policy_revision) = 64 AND saml_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL)) AND OCTET_LENGTH(named_identity_provider_id) = 0 AND OCTET_LENGTH(named_identity_profile_id) = 0 AND OCTET_LENGTH(named_identity_binding_id) = 0 AND named_identity_binding_created_at IS NULL AND OCTET_LENGTH(named_identity_config_revision) = 0 AND OCTET_LENGTH(named_identity_policy_revision) = 0 AND named_identity_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 6 AND ASCII(SUBSTRING(primary_method,1,1)) = 103 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 116 AND ASCII(SUBSTRING(primary_method,4,1)) = 104 AND ASCII(SUBSTRING(primary_method,5,1)) = 117 AND ASCII(SUBSTRING(primary_method,6,1)) = 98 AND OCTET_LENGTH(named_identity_provider_id) = 6 AND ASCII(SUBSTRING(named_identity_provider_id,1,1)) = 103 AND ASCII(SUBSTRING(named_identity_provider_id,2,1)) = 105 AND ASCII(SUBSTRING(named_identity_provider_id,3,1)) = 116 AND ASCII(SUBSTRING(named_identity_provider_id,4,1)) = 104 AND ASCII(SUBSTRING(named_identity_provider_id,5,1)) = 117 AND ASCII(SUBSTRING(named_identity_provider_id,6,1)) = 98 AND OCTET_LENGTH(named_identity_profile_id) = 23 AND ASCII(SUBSTRING(named_identity_profile_id,1,1)) = 103 AND ASCII(SUBSTRING(named_identity_profile_id,2,1)) = 105 AND ASCII(SUBSTRING(named_identity_profile_id,3,1)) = 116 AND ASCII(SUBSTRING(named_identity_profile_id,4,1)) = 104 AND ASCII(SUBSTRING(named_identity_profile_id,5,1)) = 117 AND ASCII(SUBSTRING(named_identity_profile_id,6,1)) = 98 AND ASCII(SUBSTRING(named_identity_profile_id,7,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,8,1)) = 99 AND ASCII(SUBSTRING(named_identity_profile_id,9,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,10,1)) = 109 AND ASCII(SUBSTRING(named_identity_profile_id,11,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,12,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,13,1)) = 97 AND ASCII(SUBSTRING(named_identity_profile_id,14,1)) = 117 AND ASCII(SUBSTRING(named_identity_profile_id,15,1)) = 116 AND ASCII(SUBSTRING(named_identity_profile_id,16,1)) = 104 AND ASCII(SUBSTRING(named_identity_profile_id,17,1)) = 45 AND ASCII(SUBSTRING(named_identity_profile_id,18,1)) = 97 AND ASCII(SUBSTRING(named_identity_profile_id,19,1)) = 112 AND ASCII(SUBSTRING(named_identity_profile_id,20,1)) = 112 AND ASCII(SUBSTRING(named_identity_profile_id,21,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,22,1)) = 118 AND ASCII(SUBSTRING(named_identity_profile_id,23,1)) = 49 AND CHAR_LENGTH(named_identity_binding_id) > 0 AND named_identity_binding_created_at IS NOT NULL AND CHAR_LENGTH(named_identity_config_revision) = 64 AND CHAR_LENGTH(named_identity_policy_revision) = 64 AND named_identity_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL)" json:"-"`
}

func (namedIdentityHistoricalSessionV98) TableName() string { return "sessions" }

type namedIdentityHistoricalMFAChallengeV98 struct {
	NamedIdentityProviderID       string     `gorm:"column:named_identity_provider_id;size:30;not null;default:''" json:"-"`
	NamedIdentityProfileID        string     `gorm:"column:named_identity_profile_id;size:64;not null;default:''" json:"-"`
	NamedIdentityBindingID        string     `gorm:"column:named_identity_binding_id;size:30;not null;default:''" json:"-"`
	NamedIdentityBindingCreatedAt *time.Time `gorm:"column:named_identity_binding_created_at;precision:6" json:"-"`
	NamedIdentityConfigRevision   string     `gorm:"column:named_identity_config_revision;size:64;not null;default:''" json:"-"`
	NamedIdentityPolicyRevision   string     `gorm:"column:named_identity_policy_revision;size:64;not null;default:''" json:"-"`
	NamedIdentityUserCreatedAt    *time.Time `gorm:"column:named_identity_user_created_at;precision:6" json:"-"`
	PrimaryMethod                 string     `gorm:"size:20;not null;default:'';check:ck_mfa_challenges_oidc_primary,(((OCTET_LENGTH(primary_method) = 0 AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 100 AND ASCII(SUBSTRING(primary_method,4,1)) = 99 AND CHAR_LENGTH(oidc_binding_id) > 0 AND oidc_binding_created_at IS NOT NULL AND CHAR_LENGTH(oidc_config_revision) = 64 AND CHAR_LENGTH(oidc_policy_revision) = 64 AND oidc_user_created_at IS NOT NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 5 AND ASCII(SUBSTRING(primary_method,1,1)) = 111 AND ASCII(SUBSTRING(primary_method,2,1)) = 97 AND ASCII(SUBSTRING(primary_method,3,1)) = 117 AND ASCII(SUBSTRING(primary_method,4,1)) = 116 AND ASCII(SUBSTRING(primary_method,5,1)) = 104 AND CHAR_LENGTH(oauth_binding_id) > 0 AND oauth_binding_created_at IS NOT NULL AND CHAR_LENGTH(oauth_config_revision) = 64 AND CHAR_LENGTH(oauth_policy_revision) = 64 AND oauth_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 108 AND ASCII(SUBSTRING(primary_method,2,1)) = 100 AND ASCII(SUBSTRING(primary_method,3,1)) = 97 AND ASCII(SUBSTRING(primary_method,4,1)) = 112 AND CHAR_LENGTH(ldap_binding_id) > 0 AND ldap_binding_created_at IS NOT NULL AND CHAR_LENGTH(ldap_config_revision) = 64 AND CHAR_LENGTH(ldap_policy_revision) = 64 AND ldap_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 4 AND ASCII(SUBSTRING(primary_method,1,1)) = 115 AND ASCII(SUBSTRING(primary_method,2,1)) = 97 AND ASCII(SUBSTRING(primary_method,3,1)) = 109 AND ASCII(SUBSTRING(primary_method,4,1)) = 108 AND CHAR_LENGTH(saml_binding_id) > 0 AND saml_binding_created_at IS NOT NULL AND CHAR_LENGTH(saml_config_revision) = 64 AND CHAR_LENGTH(saml_policy_revision) = 64 AND saml_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL)) AND OCTET_LENGTH(named_identity_provider_id) = 0 AND OCTET_LENGTH(named_identity_profile_id) = 0 AND OCTET_LENGTH(named_identity_binding_id) = 0 AND named_identity_binding_created_at IS NULL AND OCTET_LENGTH(named_identity_config_revision) = 0 AND OCTET_LENGTH(named_identity_policy_revision) = 0 AND named_identity_user_created_at IS NULL) OR (OCTET_LENGTH(primary_method) = 6 AND ASCII(SUBSTRING(primary_method,1,1)) = 103 AND ASCII(SUBSTRING(primary_method,2,1)) = 105 AND ASCII(SUBSTRING(primary_method,3,1)) = 116 AND ASCII(SUBSTRING(primary_method,4,1)) = 104 AND ASCII(SUBSTRING(primary_method,5,1)) = 117 AND ASCII(SUBSTRING(primary_method,6,1)) = 98 AND OCTET_LENGTH(named_identity_provider_id) = 6 AND ASCII(SUBSTRING(named_identity_provider_id,1,1)) = 103 AND ASCII(SUBSTRING(named_identity_provider_id,2,1)) = 105 AND ASCII(SUBSTRING(named_identity_provider_id,3,1)) = 116 AND ASCII(SUBSTRING(named_identity_provider_id,4,1)) = 104 AND ASCII(SUBSTRING(named_identity_provider_id,5,1)) = 117 AND ASCII(SUBSTRING(named_identity_provider_id,6,1)) = 98 AND OCTET_LENGTH(named_identity_profile_id) = 23 AND ASCII(SUBSTRING(named_identity_profile_id,1,1)) = 103 AND ASCII(SUBSTRING(named_identity_profile_id,2,1)) = 105 AND ASCII(SUBSTRING(named_identity_profile_id,3,1)) = 116 AND ASCII(SUBSTRING(named_identity_profile_id,4,1)) = 104 AND ASCII(SUBSTRING(named_identity_profile_id,5,1)) = 117 AND ASCII(SUBSTRING(named_identity_profile_id,6,1)) = 98 AND ASCII(SUBSTRING(named_identity_profile_id,7,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,8,1)) = 99 AND ASCII(SUBSTRING(named_identity_profile_id,9,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,10,1)) = 109 AND ASCII(SUBSTRING(named_identity_profile_id,11,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,12,1)) = 111 AND ASCII(SUBSTRING(named_identity_profile_id,13,1)) = 97 AND ASCII(SUBSTRING(named_identity_profile_id,14,1)) = 117 AND ASCII(SUBSTRING(named_identity_profile_id,15,1)) = 116 AND ASCII(SUBSTRING(named_identity_profile_id,16,1)) = 104 AND ASCII(SUBSTRING(named_identity_profile_id,17,1)) = 45 AND ASCII(SUBSTRING(named_identity_profile_id,18,1)) = 97 AND ASCII(SUBSTRING(named_identity_profile_id,19,1)) = 112 AND ASCII(SUBSTRING(named_identity_profile_id,20,1)) = 112 AND ASCII(SUBSTRING(named_identity_profile_id,21,1)) = 46 AND ASCII(SUBSTRING(named_identity_profile_id,22,1)) = 118 AND ASCII(SUBSTRING(named_identity_profile_id,23,1)) = 49 AND CHAR_LENGTH(named_identity_binding_id) > 0 AND named_identity_binding_created_at IS NOT NULL AND CHAR_LENGTH(named_identity_config_revision) = 64 AND CHAR_LENGTH(named_identity_policy_revision) = 64 AND named_identity_user_created_at IS NOT NULL AND OCTET_LENGTH(oidc_binding_id) = 0 AND oidc_binding_created_at IS NULL AND OCTET_LENGTH(oidc_config_revision) = 0 AND OCTET_LENGTH(oidc_policy_revision) = 0 AND oidc_user_created_at IS NULL AND OCTET_LENGTH(oauth_binding_id) = 0 AND oauth_binding_created_at IS NULL AND OCTET_LENGTH(oauth_config_revision) = 0 AND OCTET_LENGTH(oauth_policy_revision) = 0 AND oauth_user_created_at IS NULL AND OCTET_LENGTH(ldap_binding_id) = 0 AND ldap_binding_created_at IS NULL AND OCTET_LENGTH(ldap_config_revision) = 0 AND OCTET_LENGTH(ldap_policy_revision) = 0 AND ldap_user_created_at IS NULL AND OCTET_LENGTH(saml_binding_id) = 0 AND saml_binding_created_at IS NULL AND OCTET_LENGTH(saml_config_revision) = 0 AND OCTET_LENGTH(saml_policy_revision) = 0 AND saml_user_created_at IS NULL)" json:"-"`
}

func (namedIdentityHistoricalMFAChallengeV98) TableName() string { return "mfa_challenges" }

type namedIdentityHistoricalSecretRotationJobV98 struct {
	InventoryVersion int `gorm:"not null;default:1;check:ck_secret_inventory_version,inventory_version = 1 OR inventory_version = 2 OR inventory_version = 3 OR inventory_version = 4 OR inventory_version = 5 OR inventory_version = 6"`
	Domain           int `gorm:"not null;check:ck_secret_rotation_domain,(inventory_version = 1 AND domain >= 0 AND domain <= 5) OR (inventory_version = 2 AND domain >= 0 AND domain <= 7) OR (inventory_version = 3 AND domain >= 0 AND domain <= 8) OR (inventory_version = 4 AND domain >= 0 AND domain <= 9) OR (inventory_version = 5 AND domain >= 0 AND domain <= 10) OR (inventory_version = 6 AND domain >= 0 AND domain <= 11)"`
}

func (namedIdentityHistoricalSecretRotationJobV98) TableName() string { return "secret_rotation_jobs" }

type namedIdentityHistoricalSecretProcessVerificationV98 struct {
	InventoryVersion int `gorm:"not null;default:1;check:ck_secret_process_inventory_version,inventory_version = 1 OR inventory_version = 2 OR inventory_version = 3 OR inventory_version = 4 OR inventory_version = 5 OR inventory_version = 6"`
}

func (namedIdentityHistoricalSecretProcessVerificationV98) TableName() string {
	return "secret_process_verifications"
}
