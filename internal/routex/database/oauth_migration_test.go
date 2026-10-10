package database

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"unicode"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestOAuthV95FrozenSchemaParity(t *testing.T) {
	for _, pair := range [][2]any{
		{&oauthProviderV95{}, &entity.OAuthProvider{}},
		{&oauthBindingV95{}, &entity.OAuthBinding{}},
		{&oauthCeremonyV95{}, &entity.OAuthCeremony{}},
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
		{&oauthSessionV95{}, &entity.Session{}},
		{&oauthMFAChallengeV95{}, &entity.MFAChallenge{}},
		{&oauthRootJobV95{}, &oauthHistoricalEntityRootJobV95{}},
		{&oauthRootItemV95{}, &oauthHistoricalEntityRootItemV95{}},
		{&oauthRootProcessV95{}, &oauthHistoricalEntityRootProcessV95{}},
	} {
		frozen, current := oidcV94Schema(t, pair[0]), oidcV94Schema(t, pair[1])
		if frozen.Table != current.Table || len(frozen.Relationships.Relations) != 0 {
			t.Fatal("invalid bounded upgrade projection", frozen.Table)
		}
		for _, field := range frozen.Fields {
			actual := current.FieldsByDBName[field.DBName]
			if actual == nil || actual.FieldType != field.FieldType || !legacyUpgradeGORMTagsEqual(t, frozen.Table, field, actual) {
				t.Fatal("upgrade field differs", frozen.Table, field.DBName)
			}
		}
	}
}

func TestOAuthV95PartialColumnValidation(t *testing.T) {
	models := []any{&oauthProviderV95{}, &oauthBindingV95{}, &oauthCeremonyV95{}, &oauthSessionV95{}, &oauthMFAChallengeV95{}}
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
			if field.HasDefaultValue {
				faults = append(faults, "missing_default", "changed_default")
			}
			for _, fault := range faults {
				t.Run(s.Table+"/"+field.DBName+"/"+fault, func(t *testing.T) {
					m := &oidcV94ColumnsMigrator{columns: oidcV94Columns(t, model)}
					db := projectRateCheckDB(t, m)
					if err := validateOAuthColumnsV95(db, model); err != nil {
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
					if err := validateOAuthColumnsV95(db, model); err == nil || !strings.Contains(err.Error(), field.DBName) {
						t.Fatal("incompatible partial column was not identified", field.DBName, fault, err)
					}
				})
			}
		}
		m := &oidcV94ColumnsMigrator{err: errors.New("OIDC metadata unavailable")}
		if err := validateOAuthColumnsV95(projectRateCheckDB(t, m), model); !errors.Is(err, m.err) {
			t.Fatal("lost metadata failure", err)
		}
	}
}

func TestOAuthV95PortableTimestampAndDefaultMetadata(t *testing.T) {
	for _, kind := range []string{"timestamp", "timestamptz", "datetime"} {
		t.Run(kind, func(t *testing.T) {
			m := &oidcV94ColumnsMigrator{columns: oidcV94Columns(t, &oauthCeremonyV95{})}
			for i, value := range m.columns {
				c := value.(oidcV94Column)
				if c.kind == "timestamp" {
					c.kind = kind
					m.columns[i] = c
				}
			}
			if err := validateOAuthColumnsV95(projectRateCheckDB(t, m), &oauthCeremonyV95{}); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, def := range []string{"NULL", "null"} {
		m := &oidcV94ColumnsMigrator{columns: oidcV94Columns(t, &oauthCeremonyV95{})}
		for i, value := range m.columns {
			c := value.(oidcV94Column)
			if c.kind == "timestamp" {
				c.defaultKnown = true
				c.def = def
				m.columns[i] = c
			}
		}
		if err := validateOAuthColumnsV95(projectRateCheckDB(t, m), &oauthCeremonyV95{}); err != nil {
			t.Fatal("SQL NULL is not an invented timestamp", def, err)
		}
	}
	for _, def := range []string{"", "''", "''::character varying"} {
		m := &oidcV94ColumnsMigrator{columns: oidcV94Columns(t, &oauthSessionV95{})}
		for i, value := range m.columns {
			c := value.(oidcV94Column)
			if c.defaultKnown {
				c.def = def
				m.columns[i] = c
			}
		}
		if err := validateOAuthColumnsV95(projectRateCheckDB(t, m), &oauthSessionV95{}); err != nil {
			t.Fatal("portable blank default rejected", def, err)
		}
	}
}

func TestOAuthV95PhysicalProvenanceAndBounds(t *testing.T) {
	for _, model := range []any{&oauthSessionV95{}, &oauthMFAChallengeV95{}, &entity.Session{}, &entity.MFAChallenge{}} {
		s := oidcV94Schema(t, model)
		for name, column := range map[string]string{"OAuthBindingID": "oauth_binding_id", "OAuthBindingCreatedAt": "oauth_binding_created_at", "OAuthConfigRevision": "oauth_config_revision", "OAuthPolicyRevision": "oauth_policy_revision", "OAuthUserCreatedAt": "oauth_user_created_at"} {
			field := s.FieldsByName[name]
			if field == nil || field.DBName != column || field.TagSettings["COLUMN"] != column {
				t.Fatal("OAuth physical provenance differs", s.Table, name)
			}
			if field.FieldType.Kind() == reflect.Pointer {
				if field.NotNull || field.HasDefaultValue || field.Precision != 6 || field.AutoCreateTime != 0 || field.AutoUpdateTime != 0 {
					t.Fatal("unknown birth acquired a default", name)
				}
			} else if !field.NotNull || !field.HasDefaultValue || field.DefaultValue != "" {
				t.Fatal("legacy blank provenance changed", name)
			}
		}
	}
	for _, model := range []any{&oauthProviderV95{}, &oauthBindingV95{}, &oauthCeremonyV95{}} {
		s := oidcV94Schema(t, model)
		for _, field := range s.Fields {
			if field.DBName == "subject" && field.Size != 256 {
				t.Fatal("typed subject bound changed")
			}
			if field.DBName == "reason" && (field.Size != 1024 || !field.NotNull) {
				t.Fatal("reason bound changed")
			}
			if field.DBName == "scopes_json" || field.DBName == "subject_path_json" {
				if string(field.DataType) != "text" || !field.NotNull {
					t.Fatal("configuration JSON storage changed")
				}
			}
		}
	}
}

func TestOAuthV95ConstraintsKeepLocalOIDCAndExactOAuth(t *testing.T) {
	blank := func(prefix string) string {
		return "OCTET_LENGTH(" + prefix + "_binding_id) = 0 AND " + prefix + "_binding_created_at IS NULL AND OCTET_LENGTH(" + prefix + "_config_revision) = 0 AND OCTET_LENGTH(" + prefix + "_policy_revision) = 0 AND " + prefix + "_user_created_at IS NULL"
	}
	complete := func(prefix string) string {
		return "CHAR_LENGTH(" + prefix + "_binding_id) > 0 AND " + prefix + "_binding_created_at IS NOT NULL AND CHAR_LENGTH(" + prefix + "_config_revision) = 64 AND CHAR_LENGTH(" + prefix + "_policy_revision) = 64 AND " + prefix + "_user_created_at IS NOT NULL"
	}
	expected := "(OCTET_LENGTH(primary_method) = 0 AND " + blank("oidc") + " AND " + blank("oauth") + ") OR (" + oidcV94ASCII("primary_method", "oidc") + " AND " + complete("oidc") + " AND " + blank("oauth") + ") OR (" + oidcV94ASCII("primary_method", "oauth") + " AND " + complete("oauth") + " AND " + blank("oidc") + ")"
	for _, item := range []struct {
		model any
		name  string
	}{{&oauthSessionProofV95{}, "ck_sessions_oidc_primary"}, {&oauthMFAProofV95{}, "ck_mfa_challenges_oidc_primary"}} {
		if oidcV94Check(t, item.model, item.name) != expected {
			t.Fatal("unknown/mixed/incomplete primary admitted or historical shape changed", item.name)
		}
	}
	if oidcV94Check(t, &oauthProviderV95{}, "ck_oauth_singleton") != oidcV94ASCII("id", "oauth") {
		t.Fatal("singleton alias admitted")
	}
	for _, item := range []struct {
		model any
		name  string
	}{{&oauthBindingV95{}, "ck_oauth_binding_provider"}, {&oauthCeremonyV95{}, "ck_oauth_ceremony_provider"}} {
		if oidcV94Check(t, item.model, item.name) != oidcV94ASCII("provider_id", "oauth") {
			t.Fatal("Provider alias admitted")
		}
	}
	kinds := "(" + oidcV94ASCII("subject_kind", "string") + ") OR (" + oidcV94ASCII("subject_kind", "integer") + ")"
	if oidcV94Check(t, &oauthBindingV95{}, "ck_oauth_binding_kind") != kinds || oidcV94Check(t, &oauthCeremonyV95{}, "ck_oauth_ceremony_kind") != "OCTET_LENGTH(subject_kind) = 0 OR "+kinds {
		t.Fatal("typed subject contract changed")
	}
}

func TestOAuthV95RootChecksPreserveV1V2V3(t *testing.T) {
	versions := "inventory_version = 1 OR inventory_version = 2 OR inventory_version = 3 OR inventory_version = 4"
	scope := "(inventory_version = 1 AND domain >= 0 AND domain <= 5) OR (inventory_version = 2 AND domain >= 0 AND domain <= 7) OR (inventory_version = 3 AND domain >= 0 AND domain <= 8) OR (inventory_version = 4 AND domain >= 0 AND domain <= 9)"
	if oidcV94Check(t, &oauthRootJobV95{}, "ck_secret_inventory_version") != versions || oidcV94Check(t, &oauthRootProcessV95{}, "ck_secret_process_inventory_version") != versions || oidcV94Check(t, &oauthRootJobV95{}, "ck_secret_rotation_domain") != scope {
		t.Fatal("historical root scope changed")
	}
	normalize := func(v string) string {
		return strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) || r == '(' || r == ')' {
				return -1
			}
			return r
		}, v)
	}
	if normalize(oidcV94Check(t, &oauthRootItemV95{}, "ck_secret_item_domain")) != normalize(oidcV94Check(t, &oidcRootItemV94{}, "ck_secret_item_domain")+" OR "+oidcV94ASCII("domain", "oauth_providers")) {
		t.Fatal("root domains expanded beyond exact OAuth suffix")
	}
	for _, model := range []any{&oauthRootJobV95{}, &oauthRootProcessV95{}} {
		f := oidcV94Schema(t, model).FieldsByName["InventoryVersion"]
		if !f.NotNull || !f.HasDefaultValue || f.DefaultValue != "1" {
			t.Fatal("retained default no longer V1")
		}
	}
	// Independent historical literal constraints remain guarded by the retained V94 tests.
}

func TestMigrateThroughBoundsAndCurrentPrefix(t *testing.T) {
	for _, dialect := range []string{"postgres", "mysql"} {
		all := migrationSteps(dialect)
		if len(all) != 100 || reflect.ValueOf(all[96]).Pointer() != reflect.ValueOf(samlMigration).Pointer() || reflect.ValueOf(all[95]).Pointer() != reflect.ValueOf(ldapMigration).Pointer() || reflect.ValueOf(all[93]).Pointer() != reflect.ValueOf(oidcMigration).Pointer() || reflect.ValueOf(all[94]).Pointer() != reflect.ValueOf(oauthMigration).Pointer() || reflect.ValueOf(all[97]).Pointer() != reflect.ValueOf(namedIdentityMigration).Pointer() || reflect.ValueOf(all[98]).Pointer() != reflect.ValueOf(googleIdentityMigration).Pointer() || reflect.ValueOf(all[99]).Pointer() != reflect.ValueOf(runtimeInstallationMigration).Pointer() {
			t.Fatal("current migration suffix changed", dialect)
		}
		for _, bound := range []int{-1, 0, 101} {
			if _, err := migrationPrefix(all, bound); err == nil {
				t.Fatal("invalid bound accepted", bound)
			}
		}
		for _, bound := range []int{1, 94, 95, 96, 97, 98, 99, 100} {
			got, err := migrationPrefix(all, bound)
			if err != nil || len(got) != bound {
				t.Fatal("valid bound rejected", bound, err)
			}
			for i := range got {
				if reflect.ValueOf(got[i]).Pointer() != reflect.ValueOf(all[i]).Pointer() {
					t.Fatal("bound changed a released migration", i)
				}
			}
		}
	}
	if err := MigrateThrough(context.Background(), nil, 0); err == nil {
		t.Fatal("zero bound accepted")
	}
	if err := MigrateThrough(context.Background(), nil, 1); err == nil {
		t.Fatal("nil database accepted")
	}
}

type oauthV95ChecksMigrator struct {
	gorm.Migrator
	checks map[string]bool
	ops    []string
	fail   string
}

func (m *oauthV95ChecksMigrator) HasConstraint(_ any, name string) bool { return m.checks[name] }
func (m *oauthV95ChecksMigrator) DropConstraint(_ any, name string) error {
	m.ops = append(m.ops, "drop:"+name)
	if m.fail == "drop:"+name {
		return errors.New(m.fail)
	}
	m.checks[name] = false
	return nil
}
func (m *oauthV95ChecksMigrator) CreateConstraint(model any, name string) error {
	_ = model
	m.ops = append(m.ops, "create:"+name)
	if m.fail == "create:"+name {
		return errors.New(m.fail)
	}
	m.checks[name] = true
	return nil
}

func TestOAuthV95PrimaryConstraintResumeAndRetainedDenials(t *testing.T) {
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
			m := &oauthV95ChecksMigrator{checks: map[string]bool{names[0]: true, names[1]: true}, fail: fault}
			db := projectRateCheckDB(t, m)
			queries := 0
			if err := db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
				queries++
				var model any
				var name string
				switch tx.Statement.Table {
				case "sessions":
					model = &oauthSessionProofV95{}
					name = names[0]
				case "mfa_challenges":
					model = &oauthMFAProofV95{}
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
			err := oauthPrimaryConstraintsV95(db)
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
				if err := oauthPrimaryConstraintsV95(db); err != nil {
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

func TestOAuthV95RootRetainedScopeAndInterruptedChecks(t *testing.T) {
	names := []string{"ck_secret_inventory_version", "ck_secret_rotation_domain", "ck_secret_item_domain", "ck_secret_process_inventory_version"}
	faults := []string{"", "bad_jobs", "bad_proofs", "query_jobs", "query_proofs"}
	for _, name := range names {
		faults = append(faults, "drop:"+name, "create:"+name)
	}
	for _, fault := range faults {
		label := fault
		if label == "" {
			label = "valid"
		}
		t.Run(label, func(t *testing.T) {
			m := &oauthV95ChecksMigrator{checks: map[string]bool{}, fail: fault}
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
					expectedSQL = "inventory_version NOT IN ? OR inventory_version IS NULL OR domain IS NULL OR domain < 0 OR (inventory_version = ? AND domain > ?) OR (inventory_version = ? AND domain > ?) OR (inventory_version = ? AND domain > ?) OR domain > ?"
					expectedVars = []any{[]int{1, 2, 3, 4}, 1, 5, 2, 7, 3, 8, 9}
					suffix = "jobs"
				case "secret_process_verifications":
					expectedSQL = "inventory_version NOT IN ? OR inventory_version IS NULL"
					expectedVars = []any{[]int{1, 2, 3, 4}}
					suffix = "proofs"
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
			err := oauthRootInventoryV95(db)
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
				if err := oauthRootInventoryV95(db); err != nil {
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

// SQL equality to the empty string is not an exact blank under PAD SPACE
// collations. These independent literal checks protect all three primary arms.
func TestOAuthV95ExactBlankPredicates(t *testing.T) {
	for _, item := range []struct {
		model any
		name  string
	}{{&oauthSessionProofV95{}, "ck_sessions_oidc_primary"}, {&oauthMFAProofV95{}, "ck_mfa_challenges_oidc_primary"}} {
		check := oidcV94Check(t, item.model, item.name)
		for _, column := range []string{"primary_method", "oidc_binding_id", "oidc_config_revision", "oidc_policy_revision", "oauth_binding_id", "oauth_config_revision", "oauth_policy_revision"} {
			if strings.Contains(check, column+" = ''") || !strings.Contains(check, "OCTET_LENGTH("+column+") = 0") {
				t.Fatal("collation-sensitive empty proof", column)
			}
		}
		for _, method := range []string{"oidc", "oauth"} {
			if !strings.Contains(check, oidcV94ASCII("primary_method", method)) {
				t.Fatal("method discriminant not byte-exact", method)
			}
		}
	}
	for _, model := range []any{&oauthCeremonyV95{}, &entity.OAuthCeremony{}} {
		check := oidcV94Check(t, model, "ck_oauth_ceremony_kind")
		if !strings.HasPrefix(check, "OCTET_LENGTH(subject_kind) = 0 OR ") || strings.Contains(check, "subject_kind = ''") {
			t.Fatal("ceremony pending kind is not exactly blank")
		}
	}
}

// Literal current-entity V95 root projection captured before V96; independent of V95 frozen models.
type oauthHistoricalEntityRootJobV95 struct {
	InventoryVersion int `gorm:"not null;default:1;check:ck_secret_inventory_version,inventory_version = 1 OR inventory_version = 2 OR inventory_version = 3 OR inventory_version = 4"`
	Domain           int `gorm:"not null;check:ck_secret_rotation_domain,(inventory_version = 1 AND domain >= 0 AND domain <= 5) OR (inventory_version = 2 AND domain >= 0 AND domain <= 7) OR (inventory_version = 3 AND domain >= 0 AND domain <= 8) OR (inventory_version = 4 AND domain >= 0 AND domain <= 9)"`
}

func (oauthHistoricalEntityRootJobV95) TableName() string { return "secret_rotation_jobs" }

type oauthHistoricalEntityRootItemV95 struct {
	Domain string `gorm:"primaryKey;size:32;check:ck_secret_item_domain,((((CHAR_LENGTH(domain) = 20 AND ASCII(SUBSTRING(domain,1,1)) = 112 AND ASCII(SUBSTRING(domain,2,1)) = 114 AND ASCII(SUBSTRING(domain,3,1)) = 111 AND ASCII(SUBSTRING(domain,4,1)) = 118 AND ASCII(SUBSTRING(domain,5,1)) = 105 AND ASCII(SUBSTRING(domain,6,1)) = 100 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 114 AND ASCII(SUBSTRING(domain,9,1)) = 95 AND ASCII(SUBSTRING(domain,10,1)) = 99 AND ASCII(SUBSTRING(domain,11,1)) = 114 AND ASCII(SUBSTRING(domain,12,1)) = 101 AND ASCII(SUBSTRING(domain,13,1)) = 100 AND ASCII(SUBSTRING(domain,14,1)) = 101 AND ASCII(SUBSTRING(domain,15,1)) = 110 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 105 AND ASCII(SUBSTRING(domain,18,1)) = 97 AND ASCII(SUBSTRING(domain,19,1)) = 108 AND ASCII(SUBSTRING(domain,20,1)) = 115) OR (CHAR_LENGTH(domain) = 8 AND ASCII(SUBSTRING(domain,1,1)) = 101 AND ASCII(SUBSTRING(domain,2,1)) = 103 AND ASCII(SUBSTRING(domain,3,1)) = 114 AND ASCII(SUBSTRING(domain,4,1)) = 101 AND ASCII(SUBSTRING(domain,5,1)) = 115 AND ASCII(SUBSTRING(domain,6,1)) = 115 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 115) OR (CHAR_LENGTH(domain) = 13 AND ASCII(SUBSTRING(domain,1,1)) = 115 AND ASCII(SUBSTRING(domain,2,1)) = 109 AND ASCII(SUBSTRING(domain,3,1)) = 116 AND ASCII(SUBSTRING(domain,4,1)) = 112 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 115 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 116 AND ASCII(SUBSTRING(domain,9,1)) = 116 AND ASCII(SUBSTRING(domain,10,1)) = 105 AND ASCII(SUBSTRING(domain,11,1)) = 110 AND ASCII(SUBSTRING(domain,12,1)) = 103 AND ASCII(SUBSTRING(domain,13,1)) = 115) OR (CHAR_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 115 AND ASCII(SUBSTRING(domain,2,1)) = 116 AND ASCII(SUBSTRING(domain,3,1)) = 111 AND ASCII(SUBSTRING(domain,4,1)) = 114 AND ASCII(SUBSTRING(domain,5,1)) = 97 AND ASCII(SUBSTRING(domain,6,1)) = 103 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 95 AND ASCII(SUBSTRING(domain,9,1)) = 114 AND ASCII(SUBSTRING(domain,10,1)) = 101 AND ASCII(SUBSTRING(domain,11,1)) = 118 AND ASCII(SUBSTRING(domain,12,1)) = 105 AND ASCII(SUBSTRING(domain,13,1)) = 115 AND ASCII(SUBSTRING(domain,14,1)) = 105 AND ASCII(SUBSTRING(domain,15,1)) = 111 AND ASCII(SUBSTRING(domain,16,1)) = 110 AND ASCII(SUBSTRING(domain,17,1)) = 115) OR (CHAR_LENGTH(domain) = 8 AND ASCII(SUBSTRING(domain,1,1)) = 117 AND ASCII(SUBSTRING(domain,2,1)) = 115 AND ASCII(SUBSTRING(domain,3,1)) = 101 AND ASCII(SUBSTRING(domain,4,1)) = 114 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 109 AND ASCII(SUBSTRING(domain,7,1)) = 102 AND ASCII(SUBSTRING(domain,8,1)) = 97)) OR (OCTET_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 118 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 117 AND ASCII(SUBSTRING(domain,4,1)) = 108 AND ASCII(SUBSTRING(domain,5,1)) = 116 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 119 AND ASCII(SUBSTRING(domain,8,1)) = 114 AND ASCII(SUBSTRING(domain,9,1)) = 105 AND ASCII(SUBSTRING(domain,10,1)) = 116 AND ASCII(SUBSTRING(domain,11,1)) = 101 AND ASCII(SUBSTRING(domain,12,1)) = 114 AND ASCII(SUBSTRING(domain,13,1)) = 95 AND ASCII(SUBSTRING(domain,14,1)) = 97 AND ASCII(SUBSTRING(domain,15,1)) = 117 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 104) OR (OCTET_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 118 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 117 AND ASCII(SUBSTRING(domain,4,1)) = 108 AND ASCII(SUBSTRING(domain,5,1)) = 116 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 114 AND ASCII(SUBSTRING(domain,8,1)) = 101 AND ASCII(SUBSTRING(domain,9,1)) = 97 AND ASCII(SUBSTRING(domain,10,1)) = 100 AND ASCII(SUBSTRING(domain,11,1)) = 101 AND ASCII(SUBSTRING(domain,12,1)) = 114 AND ASCII(SUBSTRING(domain,13,1)) = 95 AND ASCII(SUBSTRING(domain,14,1)) = 97 AND ASCII(SUBSTRING(domain,15,1)) = 117 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 104))) OR (OCTET_LENGTH(domain) = 14 AND ASCII(SUBSTRING(domain,1,1)) = 111 AND ASCII(SUBSTRING(domain,2,1)) = 105 AND ASCII(SUBSTRING(domain,3,1)) = 100 AND ASCII(SUBSTRING(domain,4,1)) = 99 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 112 AND ASCII(SUBSTRING(domain,7,1)) = 114 AND ASCII(SUBSTRING(domain,8,1)) = 111 AND ASCII(SUBSTRING(domain,9,1)) = 118 AND ASCII(SUBSTRING(domain,10,1)) = 105 AND ASCII(SUBSTRING(domain,11,1)) = 100 AND ASCII(SUBSTRING(domain,12,1)) = 101 AND ASCII(SUBSTRING(domain,13,1)) = 114 AND ASCII(SUBSTRING(domain,14,1)) = 115) OR (OCTET_LENGTH(domain) = 15 AND ASCII(SUBSTRING(domain,1,1)) = 111 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 117 AND ASCII(SUBSTRING(domain,4,1)) = 116 AND ASCII(SUBSTRING(domain,5,1)) = 104 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 112 AND ASCII(SUBSTRING(domain,8,1)) = 114 AND ASCII(SUBSTRING(domain,9,1)) = 111 AND ASCII(SUBSTRING(domain,10,1)) = 118 AND ASCII(SUBSTRING(domain,11,1)) = 105 AND ASCII(SUBSTRING(domain,12,1)) = 100 AND ASCII(SUBSTRING(domain,13,1)) = 101 AND ASCII(SUBSTRING(domain,14,1)) = 114 AND ASCII(SUBSTRING(domain,15,1)) = 115)"`
}

func (oauthHistoricalEntityRootItemV95) TableName() string { return "secret_rotation_items" }

type oauthHistoricalEntityRootProcessV95 struct {
	InventoryVersion int `gorm:"not null;default:1;check:ck_secret_process_inventory_version,inventory_version = 1 OR inventory_version = 2 OR inventory_version = 3 OR inventory_version = 4"`
}

func (oauthHistoricalEntityRootProcessV95) TableName() string { return "secret_process_verifications" }
