package database

import "gorm.io/gorm"

// Candidate V69 adds only the reverse lookup index on the released binding table.
// Frozen fields declare its exact ordered key; no business entity is migrated.
type providerModelBindingIndexV69 struct {
	ProviderModelID string `gorm:"size:30;index:idx_bindings_provider_model,priority:1"`
	ModelID         string `gorm:"size:30;index:idx_bindings_provider_model,priority:2"`
	ID              string `gorm:"size:30;index:idx_bindings_provider_model,priority:3"`
}

func (providerModelBindingIndexV69) TableName() string { return "model_provider_bindings" }
func providerModelBindingIndexMigration(db *gorm.DB) error {
	frozen := &providerModelBindingIndexV69{}
	if !db.Migrator().HasIndex(frozen, "idx_bindings_provider_model") {
		return db.Migrator().CreateIndex(frozen, "idx_bindings_provider_model")
	}
	return nil
}
