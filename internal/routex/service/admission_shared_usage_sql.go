package service

import (
	"errors"
	"math"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type sharedUsageReadTarget struct {
	InstallationID string
	ModeRevision   uint64
}

type sharedRecordedAccountUsage struct {
	Birth       time.Time
	RPM         int64
	Concurrency int64
}

// A nil Recorded value is an unregistered account, not known zero usage. This
// internal proof establishes no resource authorization, coverage or dispatch.
type sharedAccountUsageProof struct {
	Account  string
	AsOf     time.Time
	Recorded *sharedRecordedAccountUsage
}

// sampleSharedAccountUsageSQL requires the caller's actual bounded transaction
// and governance lock BEFORE entering. It never acquires governance, commits,
// creates an account, or obtains an owner/dispatch permit. Lock order is binding,
// mode, economic domain, the single exact account, then expired projections.
// The caller must roll back the transaction on every error.
func sampleSharedAccountUsageSQL(tx *gorm.DB, target sharedUsageReadTarget, account string, now time.Time) (*sharedAccountUsageProof, error) {
	if tx == nil || tx.Statement == nil || tx.Statement.Context == nil || tx.Error != nil || !sharedUsageReadTargetValid(target) || !sharedIdentity(account) {
		return nil, errSharedAdmissionInvalid
	}
	if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok {
		return nil, errSharedAdmissionInvalid
	}
	if err := tx.Statement.Context.Err(); err != nil {
		return nil, err
	}
	if _, err := sharedUsageReadTime(now); err != nil {
		return nil, err
	}
	binding, mode, err := lockAdmissionOwnerDomain(sharedAuthorizationDB(tx))
	if err != nil {
		return nil, err
	}
	if !sharedUsageReadModeMatches(binding, mode, target) {
		return nil, errSharedAdmissionConflict
	}
	domain, err := lockSharedEconomicDomain(tx, target.InstallationID)
	if err != nil {
		return nil, err
	}
	instant, err := sharedUsageReadInstant(domain, now)
	if err != nil {
		return nil, err
	}
	var row entity.AdmissionEconomicAccount
	query := sharedEconomicAccountQuery(tx, target.InstallationID, sharedEconomicIdentityDigest(account)).Clauses(clause.Locking{Strength: "UPDATE"})
	err = query.Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if err := advanceSharedEconomicClockSQL(tx, domain, instant); err != nil {
			return nil, err
		}
		if err := tx.Statement.Context.Err(); err != nil {
			return nil, err
		}
		return &sharedAccountUsageProof{Account: account, AsOf: time.Unix(0, instant).UTC()}, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err := sharedUsageReadProof(target, account, instant, row); err != nil {
		return nil, err
	}
	// The existing single-account 4096-row overflow probe validates original
	// expiry bodies and never expires active quota or executor contributions.
	if err := expireSharedEconomicProjectionsSQL(tx, &row, instant); err != nil {
		return nil, err
	}
	proof, err := sharedUsageReadProof(target, account, instant, row)
	if err != nil {
		return nil, err
	}
	if err := advanceSharedEconomicClockSQL(tx, domain, instant); err != nil {
		return nil, err
	}
	if err := tx.Statement.Context.Err(); err != nil {
		return nil, err
	}
	return proof, nil
}

func sharedUsageReadTargetValid(target sharedUsageReadTarget) bool {
	return admissionOwnerJournal.MatchString(target.InstallationID) && target.ModeRevision > 0 && target.ModeRevision <= math.MaxInt64
}

func sharedUsageReadModeMatches(binding entity.LimitInstallation, mode entity.AdmissionMode, target sharedUsageReadTarget) bool {
	return sharedUsageReadTargetValid(target) && binding.ID == 1 && binding.Initialized && binding.JournalID == target.InstallationID &&
		mode.ID == 1 && mode.InstallationID == target.InstallationID && mode.Mode == "shared" && mode.Revision == int64(target.ModeRevision)
}

func sharedUsageReadTime(now time.Time) (int64, error) {
	instant := now.UnixNano()
	if now.IsZero() || instant <= 0 || instant > math.MaxInt64-int64(7*24*time.Hour) || !time.Unix(0, instant).Equal(now) {
		return 0, errSharedAdmissionInvalid
	}
	return instant, nil
}

func sharedUsageReadInstant(domain entity.AdmissionEconomicDomain, now time.Time) (int64, error) {
	instant, err := sharedUsageReadTime(now)
	if err != nil {
		return 0, err
	}
	if !sharedEconomicDomainValid(domain, domain.InstallationID) {
		return 0, errSharedAdmissionConflict
	}
	instant = max(instant, domain.ClockHighWater)
	if instant > math.MaxInt64-int64(7*24*time.Hour) {
		return 0, errSharedAdmissionConflict
	}
	return instant, nil
}

func sharedUsageReadProof(target sharedUsageReadTarget, account string, instant int64, row entity.AdmissionEconomicAccount) (*sharedAccountUsageProof, error) {
	if !sharedUsageReadTargetValid(target) || !sharedIdentity(account) || instant <= 0 || instant > math.MaxInt64-int64(7*24*time.Hour) ||
		row.InstallationID != target.InstallationID || row.Account != account || row.AccountDigest != sharedEconomicIdentityDigest(account) ||
		!admissionOwnerTimeValid(row.Birth) || row.Birth.UnixNano() <= 0 || !time.Unix(0, row.Birth.UnixNano()).Equal(row.Birth) ||
		row.Birth.UnixNano() > instant || row.RPMCount < 0 || row.ExecutorCount < 0 {
		return nil, errSharedAdmissionConflict
	}
	return &sharedAccountUsageProof{Account: account, AsOf: time.Unix(0, instant).UTC(),
		Recorded: &sharedRecordedAccountUsage{Birth: row.Birth.UTC(), RPM: row.RPMCount, Concurrency: row.ExecutorCount}}, nil
}
