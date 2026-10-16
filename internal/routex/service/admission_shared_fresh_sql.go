package service

import (
	"bytes"
	"errors"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Capture these coordinates once before dispatch. An uncertain commit must use
// this exact value again, including the original boundary and explicit capacity.
type sharedFreshDomainIntent struct {
	InstallationID  string
	TimeZone        string
	CalendarETag    string
	BoundaryAt      int64
	ReceiptCapacity int64
}

// initializeFreshSharedDomainSQL is a private, unwired source foundation. Before
// calling, the caller must establish a genuinely new deployment with exclusion
// of every old executor/startup since BoundaryAt, and hold governance in this
// actual bounded transaction. Empty tables, process leases and the origin digest
// are not evidence of that exclusion. No boolean can satisfy this prerequisite.
// Lock order is governance (caller), administrative installation, admission
// binding/mode, quota setting, economic domain/origin. Roll back on every error;
// publish a returned result only after successful commit. This grants no owner,
// dispatch permit or runtime activation and never converts local history.
func initializeFreshSharedDomainSQL(tx *gorm.DB, intent sharedFreshDomainIntent) (*sharedEconomicCalendarProof, error) {
	if err := sharedOriginSQLTransaction(tx); err != nil {
		return nil, err
	}
	expected, err := sharedFreshDomainOrigin(intent)
	if err != nil {
		return nil, err
	}
	var administrative []entity.Installation
	if err := sharedAuthorizationDB(tx).Clauses(clause.Locking{Strength: "UPDATE"}).Order("id").Limit(2).Find(&administrative).Error; err != nil {
		return nil, err
	}
	if len(administrative) != 1 || administrative[0].ID != 1 {
		return nil, errSharedAdmissionConflict
	}
	var bindings []entity.LimitInstallation
	if err := sharedAuthorizationDB(tx).Clauses(clause.Locking{Strength: "UPDATE"}).Order("id").Limit(2).Find(&bindings).Error; err != nil {
		return nil, err
	}
	if len(bindings) != 1 || bindings[0].ID != 1 {
		return nil, errSharedAdmissionConflict
	}
	var modes []entity.AdmissionMode
	if err := sharedAuthorizationDB(tx).Clauses(clause.Locking{Strength: "UPDATE"}).Order("id").Limit(2).Find(&modes).Error; err != nil {
		return nil, err
	}
	if len(modes) > 1 {
		return nil, errSharedAdmissionConflict
	}
	quota, err := lockSharedOriginCalendarSQL(tx)
	if err != nil {
		return nil, err
	}
	var domain entity.AdmissionEconomicDomain
	domainErr := sharedEconomicExact(sharedAuthorizationDB(tx).Clauses(clause.Locking{Strength: "UPDATE"}), "installation_id", intent.InstallationID).Take(&domain).Error
	if domainErr != nil && !errors.Is(domainErr, gorm.ErrRecordNotFound) {
		return nil, domainErr
	}
	var origin entity.AdmissionEconomicOrigin
	originErr := sharedEconomicExact(sharedAuthorizationDB(tx).Clauses(clause.Locking{Strength: "UPDATE"}), "installation_id", intent.InstallationID).Take(&origin).Error
	if originErr != nil && !errors.Is(originErr, gorm.ErrRecordNotFound) {
		return nil, originErr
	}
	if domainErr == nil || originErr == nil {
		// A successful exact retry remains valid after real accounts/receipts and
		// a later clock or same-zone calendar revision have been recorded.
		if domainErr != nil || originErr != nil || len(modes) != 1 ||
			domain.ReceiptCapacity != intent.ReceiptCapacity || origin.EvidenceDigest != expected.EvidenceDigest ||
			!bytes.Equal(origin.EvidenceBody, expected.EvidenceBody) {
			return nil, errSharedAdmissionConflict
		}
		return sharedEconomicCalendarView(bindings[0], modes[0], quota, domain, origin,
			sharedUsageReadTarget{InstallationID: intent.InstallationID, ModeRevision: 1})
	}
	if administrative[0].Initialized || bindings[0].Initialized || bindings[0].JournalID != "" || len(modes) != 0 ||
		quota.AccountingStarted || quota.TimeZone != intent.TimeZone || quota.ETag != intent.CalendarETag {
		return nil, errSharedAdmissionConflict
	}
	if err := rejectSharedFreshHistorySQL(tx); err != nil {
		return nil, err
	}
	domain = entity.AdmissionEconomicDomain{InstallationID: intent.InstallationID, FormatVersion: 1, Initialized: true,
		CoverageStart: intent.BoundaryAt, ClockHighWater: intent.BoundaryAt, ReceiptCapacity: intent.ReceiptCapacity}
	if err := sharedAuthorizationDB(tx).Omit(clause.Associations).Create(&domain).Error; err != nil {
		return nil, err
	}
	if err := sharedAuthorizationDB(tx).Omit(clause.Associations).Create(&expected).Error; err != nil {
		return nil, err
	}
	updated := sharedAuthorizationDB(tx).Model(&entity.LimitInstallation{}).
		Where("id = ? AND initialized = ? AND journal_id = ?", 1, false, "").
		Updates(map[string]any{"journal_id": intent.InstallationID, "initialized": true})
	if updated.Error != nil {
		return nil, updated.Error
	}
	if updated.RowsAffected != 1 {
		return nil, errSharedAdmissionConflict
	}
	mode := entity.AdmissionMode{ID: 1, InstallationID: intent.InstallationID, Mode: "shared", Revision: 1}
	if err := sharedAuthorizationDB(tx).Omit(clause.Associations).Create(&mode).Error; err != nil {
		return nil, err
	}
	updated = sharedAuthorizationDB(tx).Model(&entity.QuotaSetting{}).Where("id = ? AND accounting_started = ?", 1, false).Update("accounting_started", true)
	if updated.Error != nil {
		return nil, updated.Error
	}
	if updated.RowsAffected != 1 {
		return nil, errSharedAdmissionConflict
	}
	quota.AccountingStarted = true
	if err := tx.Statement.Context.Err(); err != nil {
		return nil, err
	}
	return sharedEconomicCalendarView(entity.LimitInstallation{ID: 1, JournalID: intent.InstallationID, Initialized: true}, mode, quota, domain, expected,
		sharedUsageReadTarget{InstallationID: intent.InstallationID, ModeRevision: 1})
}

func sharedFreshDomainOrigin(intent sharedFreshDomainIntent) (entity.AdmissionEconomicOrigin, error) {
	if intent.ReceiptCapacity <= 0 {
		return entity.AdmissionEconomicOrigin{}, errSharedAdmissionInvalid
	}
	body := sharedEconomicOriginBody{FormatVersion: 1, InstallationID: intent.InstallationID, SourceKind: "fresh", TimeZone: intent.TimeZone,
		CalendarRevisionAtFreeze: intent.CalendarETag, CoverageStart: intent.BoundaryAt, ClockHighWaterAtFreeze: intent.BoundaryAt, BoundaryAt: intent.BoundaryAt}
	encoded, err := sharedEconomicCanonicalBody(body)
	if err != nil {
		return entity.AdmissionEconomicOrigin{}, err
	}
	row := entity.AdmissionEconomicOrigin{InstallationID: intent.InstallationID, FormatVersion: 1, SourceKind: "fresh", TimeZone: intent.TimeZone,
		CalendarRevisionAtFreeze: intent.CalendarETag, CoverageStart: intent.BoundaryAt, ClockHighWaterAtFreeze: intent.BoundaryAt, BoundaryAt: intent.BoundaryAt,
		EvidenceBody: encoded, EvidenceDigest: sharedEconomicIdentityDigest(string(encoded))}
	if validateSharedEconomicOrigin(row) != nil {
		return entity.AdmissionEconomicOrigin{}, errSharedAdmissionInvalid
	}
	return row, nil
}

// These bounded existence probes refuse observable previous activity. They do
// not establish executor exclusion, erased-history absence or a fleet census.
func rejectSharedFreshHistorySQL(tx *gorm.DB) error {
	for _, model := range []any{
		&entity.User{}, &entity.Session{}, &entity.Team{}, &entity.Project{}, &entity.APIKey{}, &entity.CallRecord{}, &entity.CallAttempt{},
		&entity.ResourceLimit{}, &entity.AuditEvent{},
		&entity.AdmissionSpoolOwner{}, &entity.AdmissionEconomicDomain{}, &entity.AdmissionEconomicOrigin{}, &entity.AdmissionEconomicReceipt{},
		&entity.AdmissionEconomicAttempt{}, &entity.AdmissionEconomicAccount{}, &entity.AdmissionEconomicCounter{},
		&entity.AdmissionEconomicContribution{}, &entity.AdmissionEconomicInvalidBound{},
		&entity.SystemInstance{}, &entity.CredentialSourceProcess{}, &entity.RuntimePublication{},
		&entity.RuntimeRoutingApplication{}, &entity.RuntimeInstallationObservation{},
	} {
		var present []struct{ Present int }
		if err := sharedAuthorizationDB(tx).Unscoped().Model(model).Select("1 AS present").Limit(1).Find(&present).Error; err != nil {
			return err
		}
		if len(present) != 0 {
			return errSharedAdmissionConflict
		}
	}
	return tx.Statement.Context.Err()
}
