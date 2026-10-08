package service

import (
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

func newRecordedModel(modelID, status string, now time.Time) entity.Model {
	// The released Model birth column has millisecond precision on MySQL.
	// Preserve identical birth/update values on both supported databases.
	birth := now.UTC().Truncate(time.Millisecond)
	return entity.Model{ID: modelID, Status: status, CreatedAt: birth, ConfigUpdatedAt: &birth}
}

// Call only within the configuration writer's locked transaction, after an
// actual change. Grants and upstream readiness do not change Model configuration.
func stampModelConfiguration(tx *gorm.DB, modelID string, now time.Time) error {
	recorded := now.UTC().Truncate(time.Microsecond)
	write := personalExact(modelCreationDB(tx).Model(&entity.Model{}), "id", modelID).
		Update("config_updated_at", recorded)
	if write.Error != nil {
		return write.Error
	}
	if write.RowsAffected == 1 {
		return nil
	}
	// MySQL can report zero changed rows when two actual writes share the same
	// microsecond. Confirm the exact persisted target rather than accepting a
	// collation alias or a missing row as recorded configuration.
	var model entity.Model
	if err := personalExact(modelCreationDB(tx), "id", modelID).Take(&model).Error; err != nil {
		return err
	}
	if model.ID != modelID || model.ConfigUpdatedAt == nil || !model.ConfigUpdatedAt.Equal(recorded) {
		return catalogConflict
	}
	return nil
}
