package database

import (
	"errors"
	"testing"

	"gorm.io/gorm"
)

type connectionEnablementColumnMethods interface{ gorm.ColumnType }

type connectionEnablementColumnFixture struct {
	connectionEnablementColumnMethods
	name, kind, value   string
	nullable, defaulted bool
}

func (c connectionEnablementColumnFixture) Name() string                 { return c.name }
func (c connectionEnablementColumnFixture) DatabaseTypeName() string     { return c.kind }
func (c connectionEnablementColumnFixture) Nullable() (bool, bool)       { return c.nullable, true }
func (c connectionEnablementColumnFixture) DefaultValue() (string, bool) { return c.value, c.defaulted }

type connectionEnablementMigratorFixture struct {
	gorm.Migrator
	has     bool
	adds    int
	columns []gorm.ColumnType
	failure error
}

func (m *connectionEnablementMigratorFixture) HasColumn(any, string) bool { return m.has }
func (m *connectionEnablementMigratorFixture) AddColumn(any, string) error {
	m.adds++
	if m.failure != nil {
		return m.failure
	}
	m.has = true
	return nil
}
func (m *connectionEnablementMigratorFixture) ColumnTypes(any) ([]gorm.ColumnType, error) {
	return m.columns, nil
}

type connectionEnablementDialectFixture struct {
	gorm.Dialector
	m *connectionEnablementMigratorFixture
}

func (d connectionEnablementDialectFixture) Name() string                    { return "fixture" }
func (d connectionEnablementDialectFixture) Initialize(*gorm.DB) error       { return nil }
func (d connectionEnablementDialectFixture) Migrator(*gorm.DB) gorm.Migrator { return d.m }
func TestConnectionEnablementMigrationPartialReplayValidation(t *testing.T) {
	good := connectionEnablementColumnFixture{name: "enabled", kind: "bool", value: "true", defaulted: true}
	for _, test := range []struct {
		name      string
		column    connectionEnablementColumnFixture
		has       bool
		failure   error
		wantError bool
	}{
		{name: "new", column: good}, {name: "replay", column: good, has: true},
		{name: "nullable", column: connectionEnablementColumnFixture{name: "enabled", kind: "bool", value: "true", defaulted: true, nullable: true}, has: true, wantError: true},
		{name: "wrong default", column: connectionEnablementColumnFixture{name: "enabled", kind: "bool", value: "false", defaulted: true}, has: true, wantError: true},
		{name: "wrong type", column: connectionEnablementColumnFixture{name: "enabled", kind: "text", value: "true", defaulted: true}, has: true, wantError: true},
		{name: "missing column", column: connectionEnablementColumnFixture{name: "other"}, has: true, wantError: true},
		{name: "DDL failure", column: good, failure: errors.New("DDL failed"), wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := &connectionEnablementMigratorFixture{has: test.has, columns: []gorm.ColumnType{test.column}, failure: test.failure}
			db, err := gorm.Open(connectionEnablementDialectFixture{m: m}, &gorm.Config{DisableAutomaticPing: true})
			if err != nil {
				t.Fatal(err)
			}
			err = migrateConnectionEnablementV76(db)
			if (err != nil) != test.wantError {
				t.Fatal(err)
			}
			if test.has && m.adds != 0 || !test.has && m.adds != 1 {
				t.Fatal("unexpected DDL replay", m.adds)
			}
			if err == nil {
				if err = migrateConnectionEnablementV76(db); err != nil || m.adds > 1 {
					t.Fatal("non-idempotent replay", err, m.adds)
				}
			}
		})
	}
	// Version remains proposed; this package does not register it before V75.
	if connectionEnablementVersion != 76 {
		t.Fatal("proposal identity changed")
	}
}
