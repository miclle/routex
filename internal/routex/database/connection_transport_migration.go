package database

import (
	"fmt"
	"gorm.io/gorm"
	"strings"
)

// V92 freezes only the four proof columns. Each addition is resumable across
// partially committed MySQL DDL and never changes legacy evidence or status.
type connectionTransportV92 struct {
	TransportGeneration string `gorm:"column:transport_generation;size:30;not null;default:0"`
}

func (connectionTransportV92) TableName() string { return "provider_connections" }

type credentialTransportV92 struct {
	VerifiedTransportGeneration string `gorm:"column:verified_transport_generation;size:30;not null;default:0"`
}

func (credentialTransportV92) TableName() string { return "provider_credentials" }

type capabilityTransportV92 struct {
	CapabilityTransportGeneration string `gorm:"column:capability_transport_generation;size:30;not null;default:0"`
}

func (capabilityTransportV92) TableName() string { return "provider_models" }

type capacityTransportV92 struct {
	TransportGeneration string `gorm:"column:transport_generation;size:30;not null;default:0"`
}

func (capacityTransportV92) TableName() string { return "reservation_bounds" }
func connectionTransportMigration(db *gorm.DB) error {
	for _, item := range []struct {
		model         any
		field, column string
	}{{&connectionTransportV92{}, "TransportGeneration", "transport_generation"}, {&credentialTransportV92{}, "VerifiedTransportGeneration", "verified_transport_generation"}, {&capabilityTransportV92{}, "CapabilityTransportGeneration", "capability_transport_generation"}, {&capacityTransportV92{}, "TransportGeneration", "transport_generation"}} {
		if !db.Migrator().HasColumn(item.model, item.field) {
			if e := db.Migrator().AddColumn(item.model, item.field); e != nil {
				return e
			}
		}
		columns, e := db.Migrator().ColumnTypes(item.model)
		if e != nil {
			return e
		}
		found := false
		for _, c := range columns {
			if c.Name() != item.column {
				continue
			}
			found = true
			null, known := c.Nullable()
			value, defaulted := c.DefaultValue()
			size, sized := c.Length()
			kind := strings.ToLower(c.DatabaseTypeName())
			if !known || null || !defaulted || !sized || size != 30 || kind != "varchar" && kind != "character varying" || value != "0" && value != "'0'" && value != "'0'::character varying" {
				return fmt.Errorf("invalid Connection transport proof column %s", item.column)
			}
		}
		if !found {
			return fmt.Errorf("missing Connection transport proof column %s", item.column)
		}
	}
	return nil
}
