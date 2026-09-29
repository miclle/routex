package database

import "gorm.io/gorm"

// Version 24 adds nullable media occurrence facts. Null preserves the unknown
// meaning for historical calls; new gateway calls persist explicit zero/counts.
type callMediaPricingV24 struct {
	RequestID   string `gorm:"primaryKey;size:64"`
	ImageInputs *int64
	PDFInputs   *int64
}

func (callMediaPricingV24) TableName() string { return "call_records" }

func callMediaPricingMigration(db *gorm.DB) error {
	for _, field := range []string{"ImageInputs", "PDFInputs"} {
		if !db.Migrator().HasColumn(&callMediaPricingV24{}, field) {
			if err := db.Migrator().AddColumn(&callMediaPricingV24{}, field); err != nil {
				return err
			}
		}
	}
	return nil
}
