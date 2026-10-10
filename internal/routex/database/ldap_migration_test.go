package database

import (
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"
	"unicode"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestLDAPV96FrozenSchemaParity(t *testing.T) {
	for _, pair := range [][2]any{
		{&ldapProviderV96{}, &entity.LDAPProvider{}},
		{&ldapBindingV96{}, &entity.LDAPBinding{}},
	} {
		frozen, current := oidcV94Schema(t, pair[0]), oidcV94Schema(t, pair[1])
		if frozen.Table != current.Table || len(frozen.Fields) != len(current.Fields) || len(frozen.Relationships.Relations) != 0 || len(current.Relationships.Relations) != 0 {
			t.Fatal("unbounded frozen schema", frozen.Table)
		}
		for _, field := range frozen.Fields {
			actual := current.FieldsByDBName[field.DBName]
			if actual == nil || actual.FieldType != field.FieldType || actual.Tag.Get("gorm") != field.Tag.Get("gorm") {
				t.Fatal("frozen field differs", frozen.Table, field.DBName)
			}
		}
	}
	for _, pair := range [][2]any{
		{&ldapSessionV96{}, &entity.Session{}},
		{&ldapMFAChallengeV96{}, &entity.MFAChallenge{}},
		{&ldapRootJobV96{}, &entity.SecretRotationJob{}},
		{&ldapRootItemV96{}, &entity.SecretRotationItem{}},
		{&ldapRootProcessV96{}, &entity.SecretProcessVerification{}},
	} {
		frozen, current := oidcV94Schema(t, pair[0]), oidcV94Schema(t, pair[1])
		if frozen.Table != current.Table || len(frozen.Relationships.Relations) != 0 {
			t.Fatal("invalid bounded upgrade projection", frozen.Table)
		}
		for _, field := range frozen.Fields {
			actual := current.FieldsByDBName[field.DBName]
			if actual == nil || actual.FieldType != field.FieldType || actual.Tag.Get("gorm") != field.Tag.Get("gorm") {
				t.Fatal("upgrade field differs", frozen.Table, field.DBName)
			}
		}
	}
}

func TestLDAPV96PartialColumnValidation(t *testing.T) {
	models := []any{&ldapProviderV96{}, &ldapBindingV96{}, &ldapSessionV96{}, &ldapMFAChallengeV96{}}
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
					if err := validateLDAPColumnsV96(db, model); err != nil {
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
					if err := validateLDAPColumnsV96(db, model); err == nil || !strings.Contains(err.Error(), field.DBName) {
						t.Fatal("incompatible partial column was not identified", field.DBName, fault, err)
					}
				})
			}
		}
		m := &oidcV94ColumnsMigrator{err: errors.New("OIDC metadata unavailable")}
		if err := validateLDAPColumnsV96(projectRateCheckDB(t, m), model); !errors.Is(err, m.err) {
			t.Fatal("lost metadata failure", err)
		}
	}
}

func TestLDAPV96PortableTimestampAndDefaultMetadata(t *testing.T) {
	for _, kind := range []string{"timestamp", "timestamptz", "datetime"} {
		t.Run(kind, func(t *testing.T) {
			m := &oidcV94ColumnsMigrator{columns: oidcV94Columns(t, &ldapProviderV96{})}
			for i, value := range m.columns {
				c := value.(oidcV94Column)
				if c.kind == "timestamp" {
					c.kind = kind
					m.columns[i] = c
				}
			}
			if err := validateLDAPColumnsV96(projectRateCheckDB(t, m), &ldapProviderV96{}); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, def := range []string{"NULL", "null"} {
		m := &oidcV94ColumnsMigrator{columns: oidcV94Columns(t, &ldapProviderV96{})}
		for i, value := range m.columns {
			c := value.(oidcV94Column)
			if c.kind == "timestamp" {
				c.defaultKnown = true
				c.def = def
				m.columns[i] = c
			}
		}
		if err := validateLDAPColumnsV96(projectRateCheckDB(t, m), &ldapProviderV96{}); err != nil {
			t.Fatal("SQL NULL is not an invented timestamp", def, err)
		}
	}
	for _, def := range []string{"", "''", "''::character varying"} {
		m := &oidcV94ColumnsMigrator{columns: oidcV94Columns(t, &ldapSessionV96{})}
		for i, value := range m.columns {
			c := value.(oidcV94Column)
			if c.defaultKnown {
				c.def = def
				m.columns[i] = c
			}
		}
		if err := validateLDAPColumnsV96(projectRateCheckDB(t, m), &ldapSessionV96{}); err != nil {
			t.Fatal("portable blank default rejected", def, err)
		}
	}
}

func TestLDAPV96ExactPrimaryAndIdentityChecks(t *testing.T) {
	blank := func(p string) string {
		return "OCTET_LENGTH(" + p + "_binding_id) = 0 AND " + p + "_binding_created_at IS NULL AND OCTET_LENGTH(" + p + "_config_revision) = 0 AND OCTET_LENGTH(" + p + "_policy_revision) = 0 AND " + p + "_user_created_at IS NULL"
	}
	complete := func(p string) string {
		return "CHAR_LENGTH(" + p + "_binding_id) > 0 AND " + p + "_binding_created_at IS NOT NULL AND CHAR_LENGTH(" + p + "_config_revision) = 64 AND CHAR_LENGTH(" + p + "_policy_revision) = 64 AND " + p + "_user_created_at IS NOT NULL"
	}
	expected := "(OCTET_LENGTH(primary_method) = 0 AND " + blank("oidc") + " AND " + blank("oauth") + " AND " + blank("ldap") + ")"
	for _, method := range []string{"oidc", "oauth", "ldap"} {
		expected += " OR (" + oidcV94ASCII("primary_method", method) + " AND " + complete(method)
		for _, other := range []string{"oidc", "oauth", "ldap"} {
			if other != method {
				expected += " AND " + blank(other)
			}
		}
		expected += ")"
	}
	for _, item := range []struct {
		model any
		name  string
	}{{&ldapSessionProofV96{}, "ck_sessions_oidc_primary"}, {&ldapMFAProofV96{}, "ck_mfa_challenges_oidc_primary"}} {
		got := oidcV94Check(t, item.model, item.name)
		if got != expected {
			t.Fatal("local or exact primary/nonmatching blank proof changed", item.name)
		}
		for _, column := range []string{"primary_method", "ldap_binding_id", "ldap_config_revision", "ldap_policy_revision", "oidc_binding_id", "oauth_binding_id"} {
			if strings.Contains(got, column+" = ''") {
				t.Fatal("PAD SPACE blank admitted", column)
			}
		}
	}
	kinds := "(" + oidcV94ASCII("identity_attribute", "entryUUID") + ") OR (" + oidcV94ASCII("identity_attribute", "objectGUID") + ")"
	if oidcV94Check(t, &ldapBindingV96{}, "ck_ldap_binding_attribute") != kinds {
		t.Fatal("mutable or aliased attribute admitted")
	}
	unconfigured := "(OCTET_LENGTH(identity_attribute) = 0 AND enabled = FALSE AND OCTET_LENGTH(auth_ciphertext) = 0 AND config_revision = '" + strings.Repeat("0", 64) + "') OR "
	if oidcV94Check(t, &ldapProviderV96{}, "ck_ldap_provider_attribute") != unconfigured+kinds {
		t.Fatal("unknown directory mode invented or active empty mode permitted")
	}
	if oidcV94Check(t, &ldapProviderV96{}, "ck_ldap_singleton") != oidcV94ASCII("id", "ldap") || oidcV94Check(t, &ldapBindingV96{}, "ck_ldap_binding_provider") != oidcV94ASCII("provider_id", "ldap") {
		t.Fatal("singleton alias admitted")
	}
}

func TestLDAPV96PhysicalProofAndPrivateSubject(t *testing.T) {
	for _, model := range []any{&ldapSessionV96{}, &ldapMFAChallengeV96{}, &entity.Session{}, &entity.MFAChallenge{}} {
		s := oidcV94Schema(t, model)
		for field, column := range map[string]string{"LDAPBindingID": "ldap_binding_id", "LDAPBindingCreatedAt": "ldap_binding_created_at", "LDAPConfigRevision": "ldap_config_revision", "LDAPPolicyRevision": "ldap_policy_revision", "LDAPUserCreatedAt": "ldap_user_created_at"} {
			f := s.FieldsByName[field]
			if f == nil || f.DBName != column || f.TagSettings["COLUMN"] != column {
				t.Fatal("initialism changed physical proof", field)
			}
			if f.FieldType.Kind() == reflect.Pointer {
				if f.NotNull || f.HasDefaultValue || f.Precision != 6 {
					t.Fatal("birth unknown acquired default", field)
				}
			} else if !f.NotNull || !f.HasDefaultValue || f.DefaultValue != "" {
				t.Fatal("legacy blank changed", field)
			}
		}
	}
	s := oidcV94Schema(t, &entity.LDAPBinding{})
	if len(s.Fields) != 9 || s.FieldsByName["Subject"].Size != 64 || s.FieldsByName["SubjectDigest"].Size != 64 || s.FieldsByName["Subject"].Tag.Get("json") != "-" || s.FieldsByName["SubjectDigest"].Tag.Get("json") != "-" {
		t.Fatal("opaque proof privacy/bounds changed")
	}
	if oidcV94Schema(t, &entity.LDAPProvider{}).FieldsByName["AuthCiphertext"].Tag.Get("json") != "-" {
		t.Fatal("directory password envelope exposed")
	}
}

func TestLDAPV96RootScopePreservesExactHistoricalPrefix(t *testing.T) {
	versions := "inventory_version = 1 OR inventory_version = 2 OR inventory_version = 3 OR inventory_version = 4 OR inventory_version = 5"
	scope := "(inventory_version = 1 AND domain >= 0 AND domain <= 5) OR (inventory_version = 2 AND domain >= 0 AND domain <= 7) OR (inventory_version = 3 AND domain >= 0 AND domain <= 8) OR (inventory_version = 4 AND domain >= 0 AND domain <= 9) OR (inventory_version = 5 AND domain >= 0 AND domain <= 10)"
	if oidcV94Check(t, &ldapRootJobV96{}, "ck_secret_inventory_version") != versions || oidcV94Check(t, &ldapRootProcessV96{}, "ck_secret_process_inventory_version") != versions || oidcV94Check(t, &ldapRootJobV96{}, "ck_secret_rotation_domain") != scope {
		t.Fatal("historical root scope widened")
	}
	norm := func(s string) string {
		return strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) || r == '(' || r == ')' {
				return -1
			}
			return r
		}, s)
	}
	if norm(oidcV94Check(t, &ldapRootItemV96{}, "ck_secret_item_domain")) != norm(oidcV94Check(t, &oauthRootItemV95{}, "ck_secret_item_domain")+" OR "+oidcV94ASCII("domain", "ldap_providers")) {
		t.Fatal("item history changed beyond one exact suffix")
	}
	for _, model := range []any{&ldapRootJobV96{}, &ldapRootProcessV96{}} {
		f := oidcV94Schema(t, model).FieldsByName["InventoryVersion"]
		if !f.NotNull || !f.HasDefaultValue || f.DefaultValue != "1" {
			t.Fatal("old V1 default changed")
		}
	}
}

type ldapV96ChecksMigrator struct {
	gorm.Migrator
	checks map[string]bool
	ops    []string
	fail   string
}

func (m *ldapV96ChecksMigrator) HasConstraint(_ any, name string) bool { return m.checks[name] }
func (m *ldapV96ChecksMigrator) DropConstraint(_ any, name string) error {
	m.ops = append(m.ops, "drop:"+name)
	if m.fail == "drop:"+name {
		return errors.New(m.fail)
	}
	m.checks[name] = false
	return nil
}
func (m *ldapV96ChecksMigrator) CreateConstraint(model any, name string) error {
	_ = model
	m.ops = append(m.ops, "create:"+name)
	if m.fail == "create:"+name {
		return errors.New(m.fail)
	}
	m.checks[name] = true
	return nil
}

func TestLDAPV96PrimaryConstraintResumeAndRetainedDenials(t *testing.T) {
	names := []string{"ck_sessions_oidc_primary", "ck_mfa_challenges_oidc_primary"}
	faults := []string{"", "bad:sessions", "bad:mfa_challenges", "query:sessions", "query:mfa_challenges"}
	for _, name := range names {
		faults = append(faults, "drop:"+name, "create:"+name)
	}
	for _, fault := range faults {
		label := fault
		if label == "" {
			label = "valid"
		}
		t.Run(label, func(t *testing.T) {
			m := &ldapV96ChecksMigrator{checks: map[string]bool{names[0]: true, names[1]: true}, fail: fault}
			db := projectRateCheckDB(t, m)
			queries := 0
			if err := db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
				queries++
				var model any
				var name string
				switch tx.Statement.Table {
				case "sessions":
					model = &ldapSessionProofV96{}
					name = names[0]
				case "mfa_challenges":
					model = &ldapMFAProofV96{}
					name = names[1]
				default:
					t.Fatal("unowned proof scan")
				}
				where, ok := tx.Statement.Clauses["WHERE"].Expression.(clause.Where)
				if !ok || len(where.Exprs) != 1 {
					t.Fatal("missing exact proof scan")
				}
				expr, ok := where.Exprs[0].(clause.Expr)
				if !ok || expr.SQL != "NOT ("+oidcV94Check(t, model, name)+")" || len(expr.Vars) != 0 {
					t.Fatal("retained primary proof predicate changed")
				}
				if m.fail == "query:"+tx.Statement.Table {
					_ = tx.AddError(errors.New(m.fail))
					return
				}
				count, ok := tx.Statement.Dest.(*int64)
				if !ok {
					t.Fatal("unbounded proof destination")
				}
				*count = 0
				tx.RowsAffected = 1
				if m.fail == "bad:"+tx.Statement.Table {
					*count = 1
				}
			}); err != nil {
				t.Fatal(err)
			}
			err := ldapPrimaryConstraintsV96(db)
			if (err != nil) != (fault != "") {
				t.Fatal("lost retained/DDL failure", fault, err)
			}
			if strings.HasPrefix(fault, "bad:") || strings.HasPrefix(fault, "query:") {
				if len(m.ops) != 0 {
					t.Fatal("CHECK removed before complete retained proof")
				}
			}
			m.fail = ""
			for range 2 {
				if err := ldapPrimaryConstraintsV96(db); err != nil {
					t.Fatal("partial MySQL CHECK replacement did not resume", err)
				}
				for _, name := range names {
					if !m.checks[name] {
						t.Fatal("missing resumed CHECK", name)
					}
				}
			}
			if queries < 4 {
				t.Fatal("repeat skipped retained proof")
			}
		})
	}
}

func TestLDAPV96RootRetainedScopeAndInterruptedChecks(t *testing.T) {
	names := []string{"ck_secret_inventory_version", "ck_secret_rotation_domain", "ck_secret_item_domain", "ck_secret_process_inventory_version"}
	faults := []string{"", "bad_jobs", "bad_proofs", "bad_items", "query_jobs", "query_proofs", "query_items"}
	for _, name := range names {
		faults = append(faults, "drop:"+name, "create:"+name)
	}
	for _, fault := range faults {
		label := fault
		if label == "" {
			label = "valid"
		}
		t.Run(label, func(t *testing.T) {
			m := &ldapV96ChecksMigrator{checks: map[string]bool{}, fail: fault}
			for _, name := range names {
				m.checks[name] = true
			}
			db := projectRateCheckDB(t, m)
			queries := 0
			if err := db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
				queries++
				where, ok := tx.Statement.Clauses["WHERE"].Expression.(clause.Where)
				if !ok || len(where.Exprs) != 1 {
					t.Fatal("unbounded retained-scope predicate")
				}
				expr, ok := where.Exprs[0].(clause.Expr)
				if !ok {
					t.Fatal("unexpected retained-scope expression")
				}
				var expectedSQL string
				var expectedVars []any
				var suffix string
				switch tx.Statement.Table {
				case "secret_rotation_jobs":
					expectedSQL = "inventory_version NOT IN ? OR inventory_version IS NULL OR domain IS NULL OR domain < 0 OR (inventory_version = ? AND domain > ?) OR (inventory_version = ? AND domain > ?) OR (inventory_version = ? AND domain > ?) OR (inventory_version = ? AND domain > ?) OR domain > ?"
					expectedVars = []any{[]int{1, 2, 3, 4, 5}, 1, 5, 2, 7, 3, 8, 4, 9, 10}
					suffix = "jobs"
				case "secret_process_verifications":
					expectedSQL = "inventory_version NOT IN ? OR inventory_version IS NULL"
					expectedVars = []any{[]int{1, 2, 3, 4, 5}}
					suffix = "proofs"
				case "secret_rotation_items":
					expectedSQL = "NOT (" + oidcV94Check(t, &ldapRootItemV96{}, "ck_secret_item_domain") + ")"
					suffix = "items"
				default:
					t.Fatal("unowned retained query", tx.Statement.Table)
				}
				if expr.SQL != expectedSQL || !reflect.DeepEqual(expr.Vars, expectedVars) {
					t.Fatal("retained historical scope was weakened", expr)
				}
				if m.fail == "query_"+suffix {
					_ = tx.AddError(errors.New(m.fail))
					return
				}
				count, ok := tx.Statement.Dest.(*int64)
				if !ok {
					t.Fatal("unexpected retained query destination")
				}
				tx.RowsAffected = 1 // COUNT returns one aggregate row even when its value is zero.
				*count = 0
				if m.fail == "bad_"+suffix {
					*count = 1
				}
			}); err != nil {
				t.Fatal(err)
			}
			err := ldapRootInventoryV96(db)
			if (err != nil) != (fault != "") {
				t.Fatal("retained/DDL fault was lost", fault, err)
			}
			if strings.HasPrefix(fault, "bad_") || strings.HasPrefix(fault, "query_") {
				if len(m.ops) != 0 {
					t.Fatal("constraint changed before positive retained proof", m.ops)
				}
			}
			m.fail = ""
			for range 2 {
				if err := ldapRootInventoryV96(db); err != nil {
					t.Fatal("interrupted constraint replacement did not resume", err)
				}
				for _, name := range names {
					if !m.checks[name] {
						t.Fatal("missing resumed constraint", name)
					}
				}
			}
			if queries < 4 {
				t.Fatal("repeat skipped fresh retained-scope validation", queries)
			}
		})
	}
}

func TestLDAPV96OrderedUniqueIndexMetadata(t *testing.T) {
	for _, dialect := range []string{"postgres", "mysql"} {
		for _, fault := range []string{"valid", "missing", "nonunique", "unknown_unique", "prefix", "wrong_column", "extra", "position", "query"} {
			t.Run(dialect+"/"+fault, func(t *testing.T) {
				pool := &credentialStatisticsIndexPool{rows: [][]driver.Value{{"user_id", int64(1), true, nil}}}
				db := credentialStatisticsIndexDB(t, dialect, pool, &credentialStatisticsMigrator{})
				// Narrow frozen projection owns one unique index, isolating each fault.
				if err := validateLDAPIndexesV96(db, &ldapIndexOwnerV96{}); err != nil {
					t.Fatal("positive unique index baseline", err)
				}
				pool.rows = [][]driver.Value{{"user_id", int64(1), true, nil}}
				switch fault {
				case "missing":
					pool.rows = nil
				case "nonunique":
					pool.rows[0][2] = false
				case "unknown_unique":
					pool.rows[0][2] = nil
				case "prefix":
					pool.rows[0][3] = int64(4)
				case "wrong_column":
					pool.rows[0][0] = "subject_digest"
				case "extra":
					pool.rows = append(pool.rows, []driver.Value{"config_revision", int64(2), true, nil})
				case "position":
					pool.rows[0][1] = int64(2)
				case "query":
					pool.failure = errors.New("metadata unavailable")
				}
				err := validateLDAPIndexesV96(db, &ldapIndexOwnerV96{})
				if (err != nil) != (fault != "valid") {
					t.Fatal("unproved unique index accepted", fault, err)
				}
				if !reflect.DeepEqual(pool.args, []any{"ldap_bindings", "idx_ldap_bindings_user_id"}) {
					t.Fatal("index identity not bound", pool.args)
				}
			})
		}
	}
}

type ldapIndexOwnerV96 struct {
	UserID string `gorm:"size:30;not null;uniqueIndex:idx_ldap_bindings_user_id"`
}

func (ldapIndexOwnerV96) TableName() string { return "ldap_bindings" }
