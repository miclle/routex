package handler

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

type connectionTransportMigrationColumn struct {
	TransportGeneration string `gorm:"column:transport_generation;size:30;not null;default:0"`
}

func (connectionTransportMigrationColumn) TableName() string { return "provider_connections" }

type credentialTransportMigrationColumn struct {
	VerifiedTransportGeneration string `gorm:"column:verified_transport_generation;size:30;not null;default:0"`
}

func (credentialTransportMigrationColumn) TableName() string { return "provider_credentials" }

type capabilityTransportMigrationColumn struct {
	CapabilityTransportGeneration string `gorm:"column:capability_transport_generation;size:30;not null;default:0"`
}

func (capabilityTransportMigrationColumn) TableName() string { return "provider_models" }

type capacityTransportMigrationColumn struct {
	TransportGeneration string `gorm:"column:transport_generation;size:30;not null;default:0"`
}

func (capacityTransportMigrationColumn) TableName() string { return "reservation_bounds" }

// Deliberately unregistered until the root composes the exact 179-case release.
func testConnectionTransportMigration(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	before := personalKeyBehaviorLedger(t, db)
	if len(before) != 95 || before[94].Version != 95 || before[93].Version != 94 || before[92].Version != 93 || before[91].Version != 92 || before[90].Version != 91 {
		t.Fatal("exact V92 ledger")
	}
	for _, row := range []any{&entity.Provider{ID: "prv_transport_migrate", Name: "Retained supplier"}, &entity.ProviderConnection{ID: "con_transport_migrate", ProviderID: "prv_transport_migrate", Name: "Retained", Protocol: entity.ProtocolOpenAIChat, BaseURL: "https://example.invalid/v1", EgressMode: "direct"}, &entity.ProviderCredential{ID: "crd_transport_migrate", ConnectionID: "con_transport_migrate", Name: "Retained verified", Ciphertext: "retained encrypted fixture", Enabled: true, VerificationStatus: "verified"}, &entity.ProviderModel{ID: "pmd_transport_migrate", ConnectionID: "con_transport_migrate", UpstreamName: "retained", SupportsImageInput: true}, &entity.ReservationBound{ProviderModelID: "pmd_transport_migrate", Protocol: entity.ProtocolOpenAIChat, ETag: "bnd_retained", PreviousETag: "0", ActorID: "usr_retained", Reason: "Historical reason", Evidence: "Historical capacity", MaxInputTokens: 100, MaxOutputTokens: 20}} {
		if e := db.Create(row).Error; e != nil {
			t.Fatal(e)
		}
	}
	type facts struct {
		Connection entity.ProviderConnection
		Credential entity.ProviderCredential
		Model      entity.ProviderModel
		Bound      entity.ReservationBound
	}
	read := func() facts {
		t.Helper()
		var f facts
		q := db.Session(&gorm.Session{QueryFields: true})
		for _, item := range []struct {
			value      any
			column, id string
		}{{&f.Connection, "id", "con_transport_migrate"}, {&f.Credential, "id", "crd_transport_migrate"}, {&f.Model, "id", "pmd_transport_migrate"}, {&f.Bound, "provider_model_id", "pmd_transport_migrate"}} {
			if e := q.Where(item.column+" = ?", item.id).Take(item.value).Error; e != nil {
				t.Fatal(e)
			}
		}
		return f
	}
	original := read()
	if original.Connection.TransportGeneration != "0" || original.Credential.VerifiedTransportGeneration != "0" || original.Model.CapabilityTransportGeneration != "0" || original.Bound.TransportGeneration != "0" {
		t.Fatal("legacy defaults")
	}
	items := []struct {
		model any
		field string
	}{{&connectionTransportMigrationColumn{}, "TransportGeneration"}, {&credentialTransportMigrationColumn{}, "VerifiedTransportGeneration"}, {&capabilityTransportMigrationColumn{}, "CapabilityTransportGeneration"}, {&capacityTransportMigrationColumn{}, "TransportGeneration"}}
	removeLedger := func() {
		t.Helper()
		r := db.Table("schema_migrations").Where("version = ?", 92).Delete(&struct{}{})
		if r.Error != nil || r.RowsAffected != 1 {
			t.Fatal("remove only V92", r.Error)
		}
	}
	migrate := func() {
		t.Helper()
		if e := database.Migrate(ctx, db); e != nil {
			t.Fatal(e)
		}
	}
	for _, item := range items {
		if e := db.Migrator().DropColumn(item.model, item.field); e != nil {
			t.Fatal(e)
		}
	}
	removeLedger()
	migrate()
	if !reflect.DeepEqual(original, read()) {
		t.Fatal("upgrade rewrote historical status/capabilities/capacity/secret/timestamps")
	}
	generation := "rev_01j0000000000000000000000a"
	for _, item := range []struct {
		model             any
		column, id, field string
	}{{&entity.ProviderConnection{}, "id", "con_transport_migrate", "transport_generation"}, {&entity.ProviderCredential{}, "id", "crd_transport_migrate", "verified_transport_generation"}, {&entity.ProviderModel{}, "id", "pmd_transport_migrate", "capability_transport_generation"}, {&entity.ReservationBound{}, "provider_model_id", "pmd_transport_migrate", "transport_generation"}} {
		if e := db.Model(item.model).Where(item.column+" = ?", item.id).Update(item.field, generation).Error; e != nil {
			t.Fatal(e)
		}
	}
	stamped := read()
	removeLedger()
	migrate()
	migrate()
	if !reflect.DeepEqual(stamped, read()) {
		t.Fatal("repeat or surviving MySQL DDL reset evidence")
	}
	for _, item := range items {
		if e := db.Migrator().DropColumn(item.model, item.field); e != nil {
			t.Fatal(e)
		}
		removeLedger()
		migrate()
		restored := read()
		switch item.field {
		case "VerifiedTransportGeneration":
			stamped.Credential.VerifiedTransportGeneration = "0"
		case "CapabilityTransportGeneration":
			stamped.Model.CapabilityTransportGeneration = "0"
		default:
			if _, ok := item.model.(*connectionTransportMigrationColumn); ok {
				stamped.Connection.TransportGeneration = "0"
			} else {
				stamped.Bound.TransportGeneration = "0"
			}
		}
		if !reflect.DeepEqual(stamped, restored) {
			t.Fatal("partial replay rewrote unrelated columns")
		}
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() { errs <- database.Migrate(ctx, db) })
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	if !personalKeyBehaviorLedgerPreserved(before, personalKeyBehaviorLedger(t, db), 92) {
		t.Fatal("unrelated ledger changed")
	}
	for _, item := range items {
		if !db.Migrator().HasColumn(item.model, item.field) {
			t.Fatal("missing proof column")
		}
	}
}
