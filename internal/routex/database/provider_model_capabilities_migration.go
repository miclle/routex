package database

import "gorm.io/gorm"

// Version 22 adds explicit provider-model input capability declarations.
type providerModelCapabilitiesV22 struct {
	SupportsImageInput bool `gorm:"not null;default:false"`
	SupportsPDFInput   bool `gorm:"not null;default:false"`
}

func (providerModelCapabilitiesV22) TableName() string { return "provider_models" }

func providerModelCapabilitiesMigration(db *gorm.DB) error {
	for _, field := range []string{"SupportsImageInput", "SupportsPDFInput"} {
		if !db.Migrator().HasColumn(&providerModelCapabilitiesV22{}, field) {
			if err := db.Migrator().AddColumn(&providerModelCapabilitiesV22{}, field); err != nil {
				return err
			}
		}
	}
	return nil
}
