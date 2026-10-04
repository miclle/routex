package database

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestFrozenTeamMemberQuotaNotificationSchema(t *testing.T) {
	frozen, err := schema.Parse(&quotaNotificationMemberV51{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	current, err := schema.Parse(&entity.QuotaNotificationObservation{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	historical, err := schema.Parse(&quotaNotificationHistoricalV47{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if teamMemberQuotaNotificationVersion != 51 || frozen.Table != historical.Table || len(frozen.Fields) != 4 || len(frozen.Relationships.Relations) != 0 || len(frozen.ParseIndexes()) != 0 || len(current.Relationships.Relations) != 0 || len(current.Fields) != len(historical.Fields)+2 {
		t.Fatal("V51 exceeds its reserved bounded schema change")
	}
	for _, old := range historical.Fields {
		field := current.LookUpField(old.Name)
		if field == nil || field.DBName != old.DBName || field.FieldType != old.FieldType || field.NotNull != old.NotNull || field.Precision != old.Precision || field.PrimaryKey != old.PrimaryKey || field.HasDefaultValue != old.HasDefaultValue {
			t.Fatal("historical column changed", old.Name)
		}
		wantSize := old.Size
		if old.Name == "ScopeID" {
			wantSize = 64
		}
		if field.Size != wantSize {
			t.Fatal("unbounded column change", old.Name)
		}
	}
	for _, name := range []string{"TeamID", "MemberUserID"} {
		field, live := frozen.LookUpField(name), current.LookUpField(name)
		if field == nil || live == nil || field.FieldType.Kind() != reflect.Pointer || field.Size != 30 || field.NotNull || field.HasDefaultValue || field.DBName != live.DBName || field.FieldType != live.FieldType || field.Size != live.Size {
			t.Fatal("historical pair proofs must remain nullable", name)
		}
	}
	if frozen.LookUpField("ScopeID").Size != 64 || current.LookUpField("ScopeID").Size != 64 {
		t.Fatal("stable52 pair digest cannot fit")
	}
	for name, old := range historical.ParseIndexes() {
		live := current.ParseIndexes()[name]
		if old.Class != live.Class || len(old.Fields) != len(live.Fields) {
			t.Fatal("historical uniqueness changed", name)
		}
		for i, field := range old.Fields {
			if field.DBName != live.Fields[i].DBName {
				t.Fatal("historical index reordered", name)
			}
		}
	}
	if len(current.ParseIndexes()) != len(historical.ParseIndexes()) {
		t.Fatal("unexpected new index")
	}
	for _, name := range []string{"ck_quota_notification_scope_v51", "ck_quota_notification_member_v51"} {
		check := frozen.ParseCheckConstraints()[name]
		if check.Constraint == "" || current.ParseCheckConstraints()[name].Constraint != check.Constraint {
			t.Fatal("frozen/current guard mismatch", name)
		}
	}
	scope := frozen.ParseCheckConstraints()["ck_quota_notification_scope_v51"].Constraint
	for _, kind := range []string{"user", "project", "team", "team_member"} {
		var predicates []string
		for i, char := range kind {
			predicates = append(predicates, fmtASCIIQuotaCheck(i+1, char))
		}
		for _, predicate := range predicates {
			if !strings.Contains(scope, predicate) {
				t.Fatal("scope kind case-folding remains possible", kind, predicate)
			}
		}
	}
	proof := frozen.ParseCheckConstraints()["ck_quota_notification_member_v51"].Constraint
	for _, column := range []string{"team_id", "member_user_id"} {
		if !strings.Contains(proof, column+" IS NULL") || !strings.Contains(proof, column+" IS NOT NULL") || !strings.Contains(proof, "CHAR_LENGTH("+column+") > 0") {
			t.Fatal("borrowed or empty pair proof allowed", column)
		}
	}
	if !strings.Contains(proof, "CHAR_LENGTH(scope_id) = 52") || !strings.Contains(proof, "CHAR_LENGTH(scope_id) BETWEEN 1 AND 30") {
		t.Fatal("aggregate/pair scope lengths unbounded")
	}
	// Standalone V47 cannot register V51 across pending independent V48/V49/V50.
	// Once integrated, the exact reserved step must be used without shifting history.
	for _, dialect := range []string{"postgres", "mysql"} {
		steps := migrationSteps(dialect)
		if len(steps) >= teamMemberQuotaNotificationVersion && reflect.ValueOf(steps[teamMemberQuotaNotificationVersion-1]).Pointer() != reflect.ValueOf(teamMemberQuotaNotificationMigration).Pointer() {
			t.Fatal("reserved V51 entry changed", dialect)
		}
	}
}

func fmtASCIIQuotaCheck(index int, char rune) string {
	return "ASCII(SUBSTRING(scope_kind," + strconv.Itoa(index) + ",1)) = " + strconv.Itoa(int(char))
}

type quotaColumnMethods interface{ gorm.ColumnType }

type memberQuotaColumn struct {
	quotaColumnMethods
	length int64
}

func (memberQuotaColumn) Name() string            { return "scope_id" }
func (c memberQuotaColumn) Length() (int64, bool) { return c.length, true }

type memberQuotaMigrator struct {
	gorm.Migrator
	columns, checks map[string]bool
	width           int64
	fail            string
	operations      []string
}

func (m *memberQuotaMigrator) HasColumn(_ any, name string) bool { return m.columns[name] }
func (m *memberQuotaMigrator) AddColumn(_ any, name string) error {
	m.operations = append(m.operations, "add:"+name)
	if m.fail == "add:"+name {
		return errInterruptedRateDDL
	}
	if name == "TeamID" && !m.columns["MemberUserID"] {
		return errors.New("guard-bearing column added before member proof")
	}
	m.columns[name] = true
	return nil
}
func (m *memberQuotaMigrator) ColumnTypes(_ any) ([]gorm.ColumnType, error) {
	if m.fail == "columns" {
		return nil, errInterruptedRateDDL
	}
	return []gorm.ColumnType{memberQuotaColumn{length: m.width}}, nil
}
func (m *memberQuotaMigrator) AlterColumn(_ any, name string) error {
	m.operations = append(m.operations, "alter:"+name)
	if m.fail == "alter:"+name {
		return errInterruptedRateDDL
	}
	m.width = 64
	return nil
}
func (m *memberQuotaMigrator) HasConstraint(_ any, name string) bool { return m.checks[name] }
func (m *memberQuotaMigrator) CreateConstraint(_ any, name string) error {
	m.operations = append(m.operations, "create:"+name)
	if m.fail == "create:"+name {
		return errInterruptedRateDDL
	}
	if !m.columns["TeamID"] || !m.columns["MemberUserID"] || m.width != 64 {
		return errors.New("guards precede required columns")
	}
	m.checks[name] = true
	return nil
}
func (m *memberQuotaMigrator) DropConstraint(_ any, name string) error {
	m.operations = append(m.operations, "drop:"+name)
	if !m.checks["ck_quota_notification_scope_v51"] || !m.checks["ck_quota_notification_member_v51"] {
		return errors.New("old guard dropped before new guards")
	}
	if m.fail == "drop:"+name {
		return errInterruptedRateDDL
	}
	delete(m.checks, name)
	return nil
}

func TestTeamMemberQuotaNotificationMigrationResumesDDL(t *testing.T) {
	for _, failure := range []string{"add:MemberUserID", "add:TeamID", "columns", "alter:ScopeID", "create:ck_quota_notification_member_v51", "create:ck_quota_notification_scope_v51", "drop:ck_quota_notification_scope_v47", "drop:ck_quota_notification_scope"} {
		t.Run(failure, func(t *testing.T) {
			m := &memberQuotaMigrator{columns: map[string]bool{}, checks: map[string]bool{"ck_quota_notification_scope_v47": true, "ck_quota_notification_scope": true, "ck_quota_notification_currency": true}, width: 30, fail: failure}
			db := projectRateCheckDB(t, m)
			if err := teamMemberQuotaNotificationMigration(db); !errors.Is(err, errInterruptedRateDDL) {
				t.Fatal("DDL failure suppressed", err)
			}
			m.fail = ""
			if err := teamMemberQuotaNotificationMigration(db); err != nil {
				t.Fatal("partial prefix did not resume", err)
			}
			if m.width != 64 || len(m.columns) != 2 || !m.checks["ck_quota_notification_member_v51"] || !m.checks["ck_quota_notification_scope_v51"] || !m.checks["ck_quota_notification_currency"] || m.checks["ck_quota_notification_scope_v47"] || m.checks["ck_quota_notification_scope"] {
				t.Fatal("resumed guard/column state invalid")
			}
			before := append([]string(nil), m.operations...)
			if err := teamMemberQuotaNotificationMigration(db); err != nil || !reflect.DeepEqual(before, m.operations) {
				t.Fatal("repeat changed completed schema", err)
			}
		})
	}
}
