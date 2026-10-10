package database

import (
	"errors"
	"gorm.io/gorm"
	"reflect"
	"testing"
)

type transportMigrationMigrator struct {
	gorm.Migrator
	Present   map[string]bool
	Adds      []string
	Fail      string
	Malformed string
	Shape     string
}

func transportMigrationKey(model any, field string) string {
	return model.(interface{ TableName() string }).TableName() + "." + field
}
func (m *transportMigrationMigrator) HasColumn(model any, field string) bool {
	return m.Present[transportMigrationKey(model, field)]
}
func (m *transportMigrationMigrator) AddColumn(model any, field string) error {
	key := transportMigrationKey(model, field)
	m.Adds = append(m.Adds, key)
	if key == m.Fail {
		return errors.New("controlled partial DDL")
	}
	m.Present[key] = true
	return nil
}
func (m *transportMigrationMigrator) ColumnTypes(model any) ([]gorm.ColumnType, error) {
	table := model.(interface{ TableName() string }).TableName()
	name := "transport_generation"
	if table == "provider_credentials" {
		name = "verified_transport_generation"
	}
	if table == "provider_models" {
		name = "capability_transport_generation"
	}
	column := providerEnablementColumn{connectionEnablementColumnFixture: connectionEnablementColumnFixture{name: name, kind: "varchar", value: "0", defaulted: true}, width: 30}
	if table == m.Malformed {
		switch m.Shape {
		case "nullable":
			column.nullable = true
		case "default":
			column.value = "unknown"
		default:
			column.width = 64
		}
	}
	return []gorm.ColumnType{column}, nil
}

type transportMigrationDialect struct {
	gorm.Dialector
	MigratorFixture *transportMigrationMigrator
}

func (d transportMigrationDialect) Name() string                    { return "fixture" }
func (d transportMigrationDialect) Initialize(*gorm.DB) error       { return nil }
func (d transportMigrationDialect) Migrator(*gorm.DB) gorm.Migrator { return d.MigratorFixture }
func TestConnectionTransportV92FrozenPartialDDLAndReplay(t *testing.T) {
	keys := []string{"provider_connections.TransportGeneration", "provider_credentials.VerifiedTransportGeneration", "provider_models.CapabilityTransportGeneration", "reservation_bounds.TransportGeneration"}
	for _, tc := range []struct {
		Name                   string
		Present                int
		Fail, Malformed, Shape string
		Error                  bool
	}{{Name: "empty"}, {Name: "all survived", Present: 4}, {Name: "first survived", Present: 1}, {Name: "three survived", Present: 3}, {Name: "interrupted", Fail: keys[2], Error: true}, {Name: "wrong shape", Malformed: "provider_models", Error: true}, {Name: "nullable proof", Malformed: "provider_credentials", Shape: "nullable", Error: true}, {Name: "unknown default", Malformed: "reservation_bounds", Shape: "default", Error: true}} {
		t.Run(tc.Name, func(t *testing.T) {
			m := &transportMigrationMigrator{Present: map[string]bool{}, Fail: tc.Fail, Malformed: tc.Malformed, Shape: tc.Shape}
			for _, key := range keys[:tc.Present] {
				m.Present[key] = true
			}
			db, e := gorm.Open(transportMigrationDialect{MigratorFixture: m}, &gorm.Config{DisableAutomaticPing: true})
			if e != nil {
				t.Fatal(e)
			}
			e = connectionTransportMigration(db)
			if (e != nil) != tc.Error {
				t.Fatal("shape/failure mismatch", e)
			}
			if tc.Fail != "" {
				m.Fail = ""
				if e := connectionTransportMigration(db); e != nil {
					t.Fatal("partial replay", e)
				}
			}
			if tc.Malformed == "" {
				before := len(m.Adds)
				if e := connectionTransportMigration(db); e != nil || len(m.Adds) != before {
					t.Fatal("repeat rewrote proof", e)
				}
			}
		})
	}
	for _, driver := range []string{"postgres", "mysql"} {
		steps := migrationSteps(driver)
		if len(steps) != 99 || reflect.ValueOf(steps[96]).Pointer() != reflect.ValueOf(samlMigration).Pointer() || reflect.ValueOf(steps[95]).Pointer() != reflect.ValueOf(ldapMigration).Pointer() || reflect.ValueOf(steps[94]).Pointer() != reflect.ValueOf(oauthMigration).Pointer() || reflect.ValueOf(steps[93]).Pointer() != reflect.ValueOf(oidcMigration).Pointer() || reflect.ValueOf(steps[92]).Pointer() != reflect.ValueOf(credentialAttemptStatisticsMigration).Pointer() || reflect.ValueOf(steps[91]).Pointer() != reflect.ValueOf(connectionTransportMigration).Pointer() || reflect.ValueOf(steps[90]).Pointer() != reflect.ValueOf(providerEnablementMigration).Pointer() || reflect.ValueOf(steps[97]).Pointer() != reflect.ValueOf(namedIdentityMigration).Pointer() || reflect.ValueOf(steps[98]).Pointer() != reflect.ValueOf(googleIdentityMigration).Pointer() {
			t.Fatal("V92 must follow immutable V91")
		}
	}
}
