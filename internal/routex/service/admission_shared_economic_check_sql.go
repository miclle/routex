package service

import (
	"errors"
	"math"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// The caller holds the initialized domain and exact, sorted account locks in
// one transaction, has expired their projections at instant, and has separately
// established current authorization, policies, calendar and the proven bound.
// Passing this read-only check neither reserves capacity nor permits dispatch.
func checkSharedEconomicAdmissionSQL(tx *gorm.DB, domain entity.AdmissionEconomicDomain, accounts []entity.AdmissionEconomicAccount, policies []eventqueue.QuotaLimit, bound eventqueue.QuotaBound, instant, monthStart int64) error {
	if tx == nil || tx.Statement == nil || tx.Error != nil {
		return errSharedAdmissionInvalid
	}
	if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok {
		return errSharedAdmissionInvalid
	}
	if err := tx.Statement.Context.Err(); err != nil {
		return err
	}
	err := checkSharedEconomicAdmission(domain, accounts, policies, bound, instant, monthStart, sharedEconomicCheckReaders{
		Counter: func(account entity.AdmissionEconomicAccount, window string) (eventqueue.QuotaCounter, error) {
			_, counter, _, err := readSharedEconomicCounterSQL(tx, account, window)
			return counter, err
		},
		InvalidBound: func(revision string) (bool, error) {
			return readSharedEconomicInvalidBoundSQL(tx, domain.InstallationID, revision, instant)
		},
	})
	if err != nil {
		return err
	}
	return tx.Statement.Context.Err()
}

// Readers are called in the original policy order, with active data before
// that policy's lazy quota windows. They expose no mutation or admission path.
type sharedEconomicCheckReaders struct {
	Counter      func(entity.AdmissionEconomicAccount, string) (eventqueue.QuotaCounter, error)
	InvalidBound func(string) (bool, error)
}

func checkSharedEconomicAdmission(domain entity.AdmissionEconomicDomain, accounts []entity.AdmissionEconomicAccount, policies []eventqueue.QuotaLimit, bound eventqueue.QuotaBound, instant, monthStart int64, readers sharedEconomicCheckReaders) error {
	if !sharedEconomicDomainValid(domain, domain.InstallationID) || instant < domain.ClockHighWater || instant <= 0 ||
		instant > math.MaxInt64-int64(7*24*time.Hour) || monthStart < 0 || monthStart > instant ||
		len(policies) < 1 || len(policies) > 2 || len(accounts) != len(policies) || readers.Counter == nil || readers.InvalidBound == nil {
		return errSharedAdmissionInvalid
	}
	byAccount := make(map[string]entity.AdmissionEconomicAccount, len(accounts))
	for i, account := range accounts {
		if account.InstallationID != domain.InstallationID || !sharedIdentity(account.Account) || account.AccountDigest != sharedEconomicIdentityDigest(account.Account) ||
			!admissionOwnerTimeValid(account.Birth) || account.Birth.UnixNano() <= 0 || account.Birth.UnixNano() > instant ||
			account.RPMCount < 0 || account.ExecutorCount < 0 || i > 0 && compareReservationSQLID(accounts[i-1].Account, account.Account) >= 0 {
			return errSharedAdmissionConflict
		}
		byAccount[account.Account] = account
	}
	seen := make(map[string]bool, len(policies))
	for _, policy := range policies {
		if !sharedIdentity(policy.Account) || seen[policy.Account] {
			return errSharedAdmissionConflict
		}
		seen[policy.Account] = true
		if _, exists := byAccount[policy.Account]; !exists {
			return errSharedAdmissionConflict
		}
	}
	// Keep the local queue's cross-category precedence: every ordered RPM and
	// executor ceiling is checked before the first quota policy or counter read.
	for _, policy := range policies {
		account := byAccount[policy.Account]
		if policy.RPM != nil && (*policy.RPM < 0 || account.RPMCount >= *policy.RPM) {
			return eventqueue.ErrRateLimit
		}
		if policy.Concurrency != nil && (*policy.Concurrency < 0 || account.ExecutorCount >= *policy.Concurrency) {
			return eventqueue.ErrConcurrency
		}
	}
	frame := eventqueue.QuotaDecisionFrame{Instant: instant, MonthStart: monthStart, CoverageStart: domain.CoverageStart}
	for _, policy := range policies {
		rules, err := eventqueue.NewQuotaPolicyRules(policy, bound, frame)
		if err != nil {
			return err
		}
		account := byAccount[policy.Account]
		if !account.Birth.Equal(policy.CreatedAt) {
			return errSharedAdmissionConflict
		}
		active, err := readers.Counter(account, "active")
		if err != nil {
			return err
		}
		if err := eventqueue.ValidateQuotaCounter(active); err != nil {
			return err
		}
		if err := rules.Check(eventqueue.QuotaPolicyView{
			Active: active,
			Window: func(window string) (eventqueue.QuotaCounter, error) {
				counter, err := readers.Counter(account, window)
				if err != nil {
					return counter, err
				}
				return counter, eventqueue.ValidateQuotaCounter(counter)
			},
			InvalidBound: readers.InvalidBound,
		}); err != nil {
			return err
		}
	}
	return nil
}

// The domain lock serializes first-cause insertion. This lookup never updates
// that cause or acquires a historical receipt lock after locking accounts.
func readSharedEconomicInvalidBoundSQL(tx *gorm.DB, installation, revision string, instant int64) (bool, error) {
	if revision == "" {
		return false, nil
	}
	var row entity.AdmissionEconomicInvalidBound
	q := sharedEconomicExact(sharedAuthorizationDB(tx).Clauses(clause.Locking{Strength: "UPDATE"}), "installation_id", installation)
	err := sharedEconomicExact(q, "revision_digest", sharedEconomicIdentityDigest(revision)).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !sharedEconomicInvalidBoundMatches(row, installation, revision, instant) {
		return false, errSharedAdmissionConflict
	}
	return true, nil
}

func sharedEconomicInvalidBoundMatches(row entity.AdmissionEconomicInvalidBound, installation, revision string, instant int64) bool {
	return admissionOwnerJournal.MatchString(installation) && sharedIdentity(revision) && row.InstallationID == installation &&
		row.Revision == revision && row.RevisionDigest == sharedEconomicIdentityDigest(revision) && sharedIdentity(row.RequestID) &&
		row.RequestDigest == sharedEconomicIdentityDigest(row.RequestID) && row.RecordedAt > 0 && row.RecordedAt <= instant
}
