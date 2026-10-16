package service

import (
	"context"
	"database/sql"
	"math"
	"time"

	"github.com/miclle/routex/pkg/eventqueue"
)

// Preflight speculates in a bounded transaction and always rolls it back. This
// reuses the complete conjunctive-account evaluator, including lazy expiry and
// missing-account zero projections, without committing a receipt, hold, RPM,
// executor or clock change before attachment storage reads. Positive preflight
// is advisory; final Accept repeats current checks and atomically reserves.
func (s *sharedEconomicSQLStore) preflightSharedGatewaySQL(ctx context.Context, intent sharedAdmissionIntent, timeout time.Duration) error {
	if ctx == nil || timeout <= 0 || timeout > 30*time.Second || validateSharedSpoolAdmission(intent) != nil {
		return errSharedAdmissionInvalid
	}
	op, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	release, err := s.lockLive()
	if err != nil {
		return err
	}
	defer release()
	if err := s.live.check(op, intent, nil); err != nil {
		return err
	}
	tx := s.db.WithContext(op).Begin(&sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if tx.Error != nil {
		return sharedEconomicSQLError(tx.Error)
	}
	defer func() { _ = tx.Rollback().Error }()
	now := time.Now().UTC()
	if err := s.validateCurrentSQL(tx, intent, nil, now); err != nil {
		return sharedEconomicSQLError(err)
	}
	domain, err := lockSharedEconomicDomain(tx, intent.Owner.InstallationID)
	if err != nil {
		return sharedEconomicSQLError(err)
	}
	if domain.ReceiptCount >= domain.ReceiptCapacity {
		return eventqueue.ErrFull
	}
	instant := max(now.UnixNano(), domain.ClockHighWater)
	if instant > math.MaxInt64-int64(7*24*time.Hour) || instant < intent.OwnerStartedAt.UnixNano() || instant < intent.Authorization.EvaluatedAt.UnixNano() {
		return errSharedAdmissionConflict
	}
	start, _, err := eventqueue.QuotaMonthBounds(instant, intent.TimeZone)
	if err != nil {
		return err
	}
	accounts, err := lockSharedEconomicAccountsSQL(tx, domain.InstallationID, intent.Policies, instant, true)
	if err != nil {
		return sharedEconomicSQLError(err)
	}
	for i := range accounts {
		if err := expireSharedEconomicProjectionsSQL(tx, &accounts[i], instant); err != nil {
			return sharedEconomicSQLError(err)
		}
	}
	if err := checkSharedEconomicAdmissionSQL(tx, domain, accounts, intent.Policies, intent.Bound, instant, start); err != nil {
		return err
	}
	if err := s.live.check(op, intent, nil); err != nil {
		return err
	}
	if err := tx.Rollback().Error; err != nil {
		return sharedEconomicSQLError(err)
	}
	return op.Err()
}
