package database

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// This complete snapshot bounds the released V47 changes independently of later entities.
type quotaNotificationHistoricalV47 struct {
	ID                string    `gorm:"primaryKey;size:30"`
	ScopeKind         string    `gorm:"size:20;not null;uniqueIndex:uq_quota_notification_observation,priority:1;check:ck_quota_notification_scope_v47,scope_kind IN ('user','project','team')"`
	ScopeID           string    `gorm:"size:30;not null;uniqueIndex:uq_quota_notification_observation,priority:2"`
	ScopeName         string    `gorm:"size:100;not null"`
	Dimension         string    `gorm:"size:20;not null;uniqueIndex:uq_quota_notification_observation,priority:3;check:ck_quota_notification_dimension,dimension IN ('tokens','money')"`
	PolicyRevision    string    `gorm:"size:64;not null;uniqueIndex:uq_quota_notification_observation,priority:5"`
	MonthStart        time.Time `gorm:"precision:6;not null;uniqueIndex:uq_quota_notification_observation,priority:4;check:ck_quota_notification_calendar,month_end > month_start AND as_of >= month_start AND as_of < month_end"`
	MonthEnd          time.Time `gorm:"precision:6;not null"`
	TimeZone          string    `gorm:"size:100;not null"`
	AsOf              time.Time `gorm:"precision:6;not null"`
	Limit             string    `gorm:"column:limit_value;size:40;not null"`
	Settled           string    `gorm:"column:settled_value;size:80;not null"`
	Currency          string    `gorm:"size:3;not null;uniqueIndex:uq_quota_notification_observation,priority:6;check:ck_quota_notification_currency,(dimension = 'tokens' AND currency = '') OR (dimension = 'money' AND currency <> '')"`
	CoverageStart     time.Time `gorm:"precision:6;not null"`
	ResourceCreatedAt time.Time `gorm:"precision:6;not null"`
}

func (quotaNotificationHistoricalV47) TableName() string { return "quota_notification_observations" }

func TestFrozenTeamQuotaNotificationScope(t *testing.T) {
	frozen, err := schema.Parse(&quotaNotificationScopeV47{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if frozen.Table != "quota_notification_observations" || len(frozen.Fields) != 1 || len(frozen.PrimaryFields) != 0 || len(frozen.Relationships.Relations) != 0 || len(frozen.ParseIndexes()) != 0 {
		t.Fatal("V47 must change only the existing scope check")
	}
	field := frozen.LookUpField("ScopeKind")
	if field.DBName != "scope_kind" || field.Size != 0 || field.NotNull || field.HasDefaultValue {
		t.Fatal("check-only migration redefines a column")
	}
	check := frozen.ParseCheckConstraints()["ck_quota_notification_scope_v47"]
	if len(frozen.ParseCheckConstraints()) != 1 || check.Constraint != "scope_kind IN ('user','project','team')" || check.DBName != field.DBName {
		t.Fatal("scope check uses an unbounded kind or nonexistent DB column")
	}
	current, err := schema.Parse(&quotaNotificationHistoricalV47{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	released, err := schema.Parse(&quotaNotificationObservationV35{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if released.ParseCheckConstraints()["ck_quota_notification_scope"].Constraint != "scope_kind IN ('user','project')" || current.ParseCheckConstraints()["ck_quota_notification_scope_v47"].Constraint != check.Constraint {
		t.Fatal("released V35 changed or current scope disagrees with V47")
	}
	if len(current.Fields) != len(released.Fields) || len(current.Relationships.Relations) != 0 || len(current.ParseIndexes()) != len(released.ParseIndexes()) {
		t.Fatal("V47 must preserve historical columns, indexes and absence of live FKs")
	}
	for _, old := range released.Fields {
		actual := current.LookUpField(old.Name)
		if actual == nil || actual.DBName != old.DBName || actual.FieldType != old.FieldType || actual.Size != old.Size || actual.Precision != old.Precision || actual.NotNull != old.NotNull || actual.PrimaryKey != old.PrimaryKey || actual.HasDefaultValue != old.HasDefaultValue {
			t.Fatal("historical column changed", old.Name)
		}
	}
	for name, old := range released.ParseIndexes() {
		actual := current.ParseIndexes()[name]
		if actual.Name != old.Name || actual.Class != old.Class || len(actual.Fields) != len(old.Fields) {
			t.Fatal("historical observation index changed", old.Name)
		}
		for i, oldField := range old.Fields {
			if actual.Fields[i].DBName != oldField.DBName {
				t.Fatal("historical observation uniqueness changed", old.Name)
			}
		}
	}
	for _, dialect := range []string{"postgres", "mysql"} {
		steps := migrationSteps(dialect)
		if len(steps) < 47 || reflect.ValueOf(steps[46]).Pointer() != reflect.ValueOf(teamQuotaNotificationMigration).Pointer() || reflect.ValueOf(steps[45]).Pointer() != reflect.ValueOf(teamAttachmentMigration).Pointer() {
			t.Fatal("V47 must append without modifying V46", dialect)
		}
	}
}

type teamQuotaScopeMigrator struct {
	gorm.Migrator
	checks     map[string]bool
	operations []string
	fail       string
}

func (m *teamQuotaScopeMigrator) HasConstraint(_ any, name string) bool { return m.checks[name] }
func (m *teamQuotaScopeMigrator) CreateConstraint(_ any, name string) error {
	m.operations = append(m.operations, "create:"+name)
	if m.fail == "create:"+name {
		return errInterruptedRateDDL
	}
	m.checks[name] = true
	return nil
}
func (m *teamQuotaScopeMigrator) DropConstraint(_ any, name string) error {
	if !m.checks["ck_quota_notification_scope_v47"] {
		return errors.New("released scope guard dropped before new guard")
	}
	m.operations = append(m.operations, "drop:"+name)
	if m.fail == "drop:"+name {
		return errInterruptedRateDDL
	}
	delete(m.checks, name)
	return nil
}

func TestTeamQuotaNotificationMigrationResumesDDL(t *testing.T) {
	for _, failure := range []string{"create:ck_quota_notification_scope_v47", "drop:ck_quota_notification_scope"} {
		t.Run(failure, func(t *testing.T) {
			m := &teamQuotaScopeMigrator{checks: map[string]bool{"ck_quota_notification_scope": true, "ck_quota_notification_currency": true}, fail: failure}
			db := projectRateCheckDB(t, m)
			if err := teamQuotaNotificationMigration(db); !errors.Is(err, errInterruptedRateDDL) || !m.checks["ck_quota_notification_scope"] {
				t.Fatal("interrupted DDL must preserve the old guard", err)
			}
			m.fail = ""
			if err := teamQuotaNotificationMigration(db); err != nil || m.checks["ck_quota_notification_scope"] || !m.checks["ck_quota_notification_scope_v47"] || !m.checks["ck_quota_notification_currency"] {
				t.Fatal("resumed DDL lost a guard", err)
			}
			before := append([]string(nil), m.operations...)
			if err := teamQuotaNotificationMigration(db); err != nil || !reflect.DeepEqual(before, m.operations) {
				t.Fatal("repeated migration rewrote completed guards", err)
			}
		})
	}
}
