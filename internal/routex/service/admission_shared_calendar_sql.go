package service

import (
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Shared live economics require the recorded calendar freeze, in addition to
// the preceding policy revision check. The caller retains governance and current
// owner/authorization/policy locks in this same READ COMMITTED transaction.
// Historical settlement does not depend on current calendar or authorization.
func requireSharedEconomicCalendarSQL(tx *gorm.DB, revision, zone string) error {
	if tx == nil || tx.Statement == nil || tx.Error != nil || revision == "" || len(revision) > 128 ||
		zone == "" || zone == "Local" || len(zone) > 100 {
		return errSharedAdmissionInvalid
	}
	if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok {
		return errSharedAdmissionInvalid
	}
	if err := tx.Statement.Context.Err(); err != nil {
		return err
	}
	if _, err := time.LoadLocation(zone); err != nil {
		return errSharedAdmissionInvalid
	}
	var calendar entity.QuotaSetting
	if err := sharedAuthorizationDB(tx).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&calendar, 1).Error; err != nil {
		return sharedAuthorizationSQLError(err)
	}
	if calendar.ID != 1 || !calendar.AccountingStarted || calendar.ETag != revision || calendar.TimeZone != zone {
		return errSharedAdmissionConflict
	}
	return tx.Statement.Context.Err()
}
