package service

import (
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Private coordinates for a coherent read, not a public activation receipt.
// CalendarETag is current; CalendarRevisionAtFreeze is immutable history.
type sharedEconomicCalendarProof struct {
	InstallationID           string
	ModeRevision             uint64
	TimeZone                 string
	CalendarETag             string
	CalendarRevisionAtFreeze string
	CoverageStart            int64
	ClockHighWater           int64
}

// readSharedEconomicCalendarSQL requires caller-owned governance and current
// resource authorization in this actual bounded transaction. It locks binding,
// mode, quota setting, domain, then origin; subsequent account sampling must not
// reverse that order. The reader makes no writes and does not authenticate an
// origin or prove prior executor exclusion. Missing/legacy provenance fails.
func readSharedEconomicCalendarSQL(tx *gorm.DB, target sharedUsageReadTarget) (*sharedEconomicCalendarProof, error) {
	if err := sharedOriginSQLTransaction(tx); err != nil {
		return nil, err
	}
	if !sharedUsageReadTargetValid(target) {
		return nil, errSharedAdmissionInvalid
	}
	binding, mode, err := lockAdmissionOwnerDomain(sharedAuthorizationDB(tx))
	if err != nil {
		return nil, err
	}
	if !sharedUsageReadModeMatches(binding, mode, target) {
		return nil, errSharedAdmissionConflict
	}
	quota, err := lockSharedOriginCalendarSQL(tx)
	if err != nil {
		return nil, err
	}
	domain, err := lockSharedEconomicDomain(tx, target.InstallationID)
	if err != nil {
		return nil, err
	}
	var origin entity.AdmissionEconomicOrigin
	if err := sharedEconomicExact(sharedAuthorizationDB(tx).Clauses(clause.Locking{Strength: "UPDATE"}), "installation_id", target.InstallationID).Take(&origin).Error; err != nil {
		return nil, err
	}
	if err := tx.Statement.Context.Err(); err != nil {
		return nil, err
	}
	return sharedEconomicCalendarView(binding, mode, quota, domain, origin, target)
}

func sharedOriginSQLTransaction(tx *gorm.DB) error {
	if tx == nil || tx.Statement == nil || tx.Statement.Context == nil || tx.Error != nil {
		return errSharedAdmissionInvalid
	}
	if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok {
		return errSharedAdmissionInvalid
	}
	return tx.Statement.Context.Err()
}

func lockSharedOriginCalendarSQL(tx *gorm.DB) (entity.QuotaSetting, error) {
	var rows []entity.QuotaSetting
	if err := sharedAuthorizationDB(tx).Clauses(clause.Locking{Strength: "UPDATE"}).Order("id").Limit(2).Find(&rows).Error; err != nil {
		return entity.QuotaSetting{}, err
	}
	if len(rows) != 1 || rows[0].ID != 1 {
		return entity.QuotaSetting{}, errSharedAdmissionConflict
	}
	return rows[0], nil
}

func sharedEconomicCalendarView(binding entity.LimitInstallation, mode entity.AdmissionMode, quota entity.QuotaSetting,
	domain entity.AdmissionEconomicDomain, origin entity.AdmissionEconomicOrigin, target sharedUsageReadTarget) (*sharedEconomicCalendarProof, error) {
	if !sharedUsageReadModeMatches(binding, mode, target) || !sharedEconomicDomainValid(domain, target.InstallationID) ||
		validateSharedEconomicOrigin(origin) != nil || origin.InstallationID != target.InstallationID ||
		domain.CoverageStart != origin.CoverageStart || domain.ClockHighWater < origin.ClockHighWaterAtFreeze ||
		!sharedEconomicOriginInstantValid(domain.ClockHighWater) || quota.ID != 1 || !quota.AccountingStarted ||
		quota.TimeZone != origin.TimeZone || !safeCallID.MatchString(quota.ETag) || len(quota.ETag) > 64 {
		return nil, errSharedAdmissionConflict
	}
	return &sharedEconomicCalendarProof{InstallationID: target.InstallationID, ModeRevision: target.ModeRevision, TimeZone: origin.TimeZone,
		CalendarETag: quota.ETag, CalendarRevisionAtFreeze: origin.CalendarRevisionAtFreeze, CoverageStart: origin.CoverageStart, ClockHighWater: domain.ClockHighWater}, nil
}
