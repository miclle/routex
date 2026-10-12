package database

import (
	"errors"
	"fmt"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Embed the concrete dialector to preserve optional GORM interfaces, including
// savepoints and configuration, while filling one upstream translation gap.
type postgresErrorDialector struct {
	*postgres.Dialector
}

func (dialector postgresErrorDialector) Translate(err error) error {
	var state interface{ SQLState() string }
	// The pinned PostgreSQL translator handles foreign_key_violation (23503)
	// but omits restrict_violation (23001) from an explicit RESTRICT reference.
	// Keep vendor error inspection confined to the database adapter.
	if errors.As(err, &state) && state.SQLState() == "23001" {
		return fmt.Errorf("%w: %w", gorm.ErrForeignKeyViolated, err)
	}
	return dialector.Dialector.Translate(err)
}
