package database

import (
	"errors"
	"gorm.io/gorm"
	"reflect"
	"testing"
)

type providerEnablementColumn struct {
	connectionEnablementColumnFixture
	width int64
}

func (c providerEnablementColumn) Length() (int64, bool) { return c.width, true }

type providerEnablementMigrator struct {
	gorm.Migrator
	present map[string]bool
	adds    []string
	columns []gorm.ColumnType
	fail    string
}

func (m *providerEnablementMigrator) HasColumn(_ any, name string) bool { return m.present[name] }
func (m *providerEnablementMigrator) AddColumn(_ any, name string) error {
	m.adds = append(m.adds, name)
	if name == m.fail {
		return errors.New("controlled DDL failure")
	}
	m.present[name] = true
	return nil
}
func (m *providerEnablementMigrator) ColumnTypes(any) ([]gorm.ColumnType, error) {
	return m.columns, nil
}

type providerEnablementDialect struct {
	gorm.Dialector
	m *providerEnablementMigrator
}

func (d providerEnablementDialect) Name() string                    { return "fixture" }
func (d providerEnablementDialect) Initialize(*gorm.DB) error       { return nil }
func (d providerEnablementDialect) Migrator(*gorm.DB) gorm.Migrator { return d.m }
func TestProviderEnablementV91PartialDDLReplayAndShape(t *testing.T) {
	goodEnabled := providerEnablementColumn{connectionEnablementColumnFixture: connectionEnablementColumnFixture{name: "enabled", kind: "bool", value: "true", defaulted: true}}
	goodRevision := providerEnablementColumn{connectionEnablementColumnFixture: connectionEnablementColumnFixture{name: "e_tag", kind: "varchar", value: "0", defaulted: true}, width: 30}
	for _, tc := range []struct {
		name      string
		present   map[string]bool
		mutate    func(*providerEnablementMigrator)
		wantError bool
		adds      int
	}{
		{name: "new", adds: 2}, {name: "repeat", present: map[string]bool{"Enabled": true, "ETag": true}},
		{name: "partial enabled", present: map[string]bool{"Enabled": true}, adds: 1},
		{name: "partial revision", present: map[string]bool{"ETag": true}, adds: 1},
		{name: "DDL interrupted", mutate: func(m *providerEnablementMigrator) { m.fail = "ETag" }, wantError: true, adds: 2},
		{name: "wrong enabled default", present: map[string]bool{"Enabled": true, "ETag": true}, mutate: func(m *providerEnablementMigrator) { bad := goodEnabled; bad.value = "false"; m.columns[0] = bad }, wantError: true},
		{name: "nullable revision", present: map[string]bool{"Enabled": true, "ETag": true}, mutate: func(m *providerEnablementMigrator) { bad := goodRevision; bad.nullable = true; m.columns[1] = bad }, wantError: true},
		{name: "wrong revision width", present: map[string]bool{"Enabled": true, "ETag": true}, mutate: func(m *providerEnablementMigrator) { bad := goodRevision; bad.width = 64; m.columns[1] = bad }, wantError: true},
		{name: "wrong revision default", present: map[string]bool{"Enabled": true, "ETag": true}, mutate: func(m *providerEnablementMigrator) { bad := goodRevision; bad.value = "unknown"; m.columns[1] = bad }, wantError: true},
		{name: "missing revision", present: map[string]bool{"Enabled": true, "ETag": true}, mutate: func(m *providerEnablementMigrator) { m.columns = m.columns[:1] }, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &providerEnablementMigrator{present: map[string]bool{}, columns: []gorm.ColumnType{goodEnabled, goodRevision}}
			for k, v := range tc.present {
				m.present[k] = v
			}
			if tc.mutate != nil {
				tc.mutate(m)
			}
			db, err := gorm.Open(providerEnablementDialect{m: m}, &gorm.Config{DisableAutomaticPing: true})
			if err != nil {
				t.Fatal(err)
			}
			err = providerEnablementMigration(db)
			if (err != nil) != tc.wantError || len(m.adds) != tc.adds {
				t.Fatal(err, m.adds)
			}
			if err == nil {
				before := len(m.adds)
				if err = providerEnablementMigration(db); err != nil || len(m.adds) != before {
					t.Fatal("non-idempotent replay", err)
				}
			}
			if tc.name == "DDL interrupted" {
				m.fail = ""
				if err = providerEnablementMigration(db); err != nil || !reflect.DeepEqual(m.adds, []string{"Enabled", "ETag", "ETag"}) {
					t.Fatal("partial replay", err, m.adds)
				}
			}
		})
	}
	for _, driver := range []string{"postgres", "mysql"} {
		steps := migrationSteps(driver)
		if len(steps) != 94 || reflect.ValueOf(steps[93]).Pointer() != reflect.ValueOf(oidcMigration).Pointer() || reflect.ValueOf(steps[92]).Pointer() != reflect.ValueOf(credentialAttemptStatisticsMigration).Pointer() || reflect.ValueOf(steps[91]).Pointer() != reflect.ValueOf(connectionTransportMigration).Pointer() || reflect.ValueOf(steps[90]).Pointer() != reflect.ValueOf(providerEnablementMigration).Pointer() || reflect.ValueOf(steps[89]).Pointer() != reflect.ValueOf(modelWeightHistoryMigration).Pointer() {
			t.Fatal("append-only V91 identity")
		}
	}
}
