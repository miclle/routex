package database

import "gorm.io/gorm"

// Version 15 preserves existing supply eligibility and adds explicit state edits.
type providerModelStateV15 struct {
	Disabled bool   `gorm:"not null;default:false"`
	ETag     string `gorm:"size:64;not null;default:0"`
}

func (providerModelStateV15) TableName() string { return "provider_models" }
func providerModelStateMigration(db *gorm.DB) error {
	for _, field := range []string{"Disabled", "ETag"} {
		if !db.Migrator().HasColumn(&providerModelStateV15{}, field) {
			if err := db.Migrator().AddColumn(&providerModelStateV15{}, field); err != nil {
				return err
			}
		}
	}
	return nil
}
