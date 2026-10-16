package service

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Settlement is historical: no current owner, authentication, mode, policy or
// route check may prevent retention of the original executor's terminal fact.
// It permits no dispatch. New admissions/dispatches remain unimplemented.
func (s *sharedEconomicSQLStore) Settle(ctx context.Context, terminal sharedTerminalIntent) (*sharedAuthorityReceipt, error) {
	return s.settleSharedEconomicSQL(ctx, terminal, time.Now().UTC())
}

func (s *sharedEconomicSQLStore) settleSharedEconomicSQL(ctx context.Context, terminal sharedTerminalIntent, now time.Time) (*sharedAuthorityReceipt, error) {
	if s == nil || s.db == nil || ctx == nil || !sharedEconomicKeyValid(terminal.Key) || !sharedHasDigest(terminal, terminal.Digest) ||
		!terminal.ExecutorClosed || now.UnixNano() <= 0 || eventqueue.ValidateQuotaSettlement(terminal.Actual) != nil {
		return nil, errSharedAdmissionInvalid
	}
	body, err := sharedEconomicCanonicalBody(terminal)
	if err != nil {
		return nil, err
	}
	var result *sharedAuthorityReceipt
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		domain, err := lockSharedEconomicDomain(tx, terminal.Key.Owner.InstallationID)
		if err != nil {
			return err
		}
		row, local, err := readSharedEconomicHistorySQL(tx, terminal.Key)
		if err != nil {
			return err
		}
		if err := validateSharedSpoolTerminal(local, terminal); err != nil {
			return err
		}
		if local.Terminal != nil {
			if row.TerminalDigest == nil || *row.TerminalDigest != terminal.Digest || !bytes.Equal(row.TerminalBody, body) {
				return errSharedAdmissionConflict
			}
			result = sharedEconomicAuthorityView(local)
			return ctx.Err()
		}
		instant := max(now.UnixNano(), domain.ClockHighWater)
		if row.AdmittedAt > instant || row.AdmittedAt < domain.CoverageStart {
			return errSharedAdmissionConflict
		}
		accounts, err := lockSharedEconomicAccountsSQL(tx, row.InstallationID, local.Intent.Policies, row.AdmittedAt, false)
		if err != nil {
			return err
		}
		windows, err := sharedEconomicWindows(row.AdmittedAt, row.MonthStart, row.MonthEnd)
		if err != nil {
			return err
		}
		for i := range accounts {
			account := &accounts[i]
			if err := expireSharedEconomicProjectionsSQL(tx, account, instant); err != nil {
				return err
			}
			for _, kind := range []string{"active_quota", "executor"} {
				var quota *eventqueue.QuotaCounterContribution
				if kind == "active_quota" {
					quota = &eventqueue.QuotaCounterContribution{Bound: local.Intent.Bound}
				}
				expected, err := newSharedEconomicProjection(row, *account, kind, "active", quota)
				if err != nil {
					return err
				}
				retained, err := readSharedEconomicProjectionSQL(tx, *account, row.RequestDigest, kind, "active")
				if err != nil {
					return err
				}
				if !bytes.Equal(retained.ContributionBody, expected.ContributionBody) || retained.ExpiresAt != nil {
					return errSharedAdmissionConflict
				}
				if err := deleteSharedEconomicProjectionSQL(tx, account, retained); err != nil {
					return err
				}
			}
			settled := eventqueue.QuotaCounterContribution{Bound: local.Intent.Bound, Actual: terminal.Actual, Settled: true}
			for _, window := range windows {
				if window.End <= instant {
					continue
				}
				projection, err := newSharedEconomicProjection(row, *account, "terminal_quota", window.Name, &settled)
				if err != nil {
					return err
				}
				if err := insertSharedEconomicProjectionSQL(tx, account, projection); err != nil {
					return err
				}
			}
		}
		if err := invalidateSharedEconomicBoundSQL(tx, row, local.Intent.Bound, terminal.Actual, instant); err != nil {
			return err
		}
		q := sharedEconomicExact(sharedAuthorizationDB(tx).Model(&entity.AdmissionEconomicReceipt{}), "installation_id", row.InstallationID)
		q = sharedEconomicExact(q, "request_digest", row.RequestDigest)
		updated := q.Where("terminal_digest IS NULL AND terminal_body IS NULL").Updates(map[string]any{"terminal_digest": terminal.Digest, "terminal_body": body})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return errSharedAdmissionConflict
		}
		if err := advanceSharedEconomicClockSQL(tx, domain, instant); err != nil {
			return err
		}
		local.Terminal = &terminal
		result = sharedEconomicAuthorityView(local)
		return ctx.Err()
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, sharedEconomicSQLError(err)
	}
	return result, nil
}

func readSharedEconomicProjectionSQL(tx *gorm.DB, account entity.AdmissionEconomicAccount, requestDigest, kind, window string) (entity.AdmissionEconomicContribution, error) {
	var row entity.AdmissionEconomicContribution
	q := sharedEconomicAccountQuery(tx, account.InstallationID, account.AccountDigest).Clauses(clause.Locking{Strength: "UPDATE"})
	q = sharedEconomicExact(q, "request_digest", requestDigest)
	q = sharedEconomicExact(q, "kind", kind)
	err := sharedEconomicExact(q, "window_name", window).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return row, errSharedAdmissionConflict
	}
	if err != nil {
		return row, err
	}
	if _, err := sharedEconomicProjectionDecode(row, account); err != nil {
		return row, err
	}
	return row, nil
}

func advanceSharedEconomicClockSQL(tx *gorm.DB, domain entity.AdmissionEconomicDomain, instant int64) error {
	if instant < domain.ClockHighWater {
		return errSharedAdmissionConflict
	}
	if instant == domain.ClockHighWater {
		return nil
	}
	q := sharedEconomicExact(sharedAuthorizationDB(tx).Model(&entity.AdmissionEconomicDomain{}), "installation_id", domain.InstallationID)
	result := q.Where("clock_high_water = ?", domain.ClockHighWater).Update("clock_high_water", instant)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errSharedAdmissionConflict
	}
	return nil
}

// Record full overrun debt in projections and permanently retire only the
// original nonempty bound revision. Preserve the first recorded cause on retry.
func invalidateSharedEconomicBoundSQL(tx *gorm.DB, receipt entity.AdmissionEconomicReceipt, bound eventqueue.QuotaBound, actual eventqueue.QuotaSettlement, instant int64) error {
	if bound.Revision == "" || eventqueue.ValidateQuotaRecovery(bound, actual) == nil {
		return nil
	}
	digest := sharedEconomicIdentityDigest(bound.Revision)
	var row entity.AdmissionEconomicInvalidBound
	q := sharedEconomicExact(sharedAuthorizationDB(tx).Clauses(clause.Locking{Strength: "UPDATE"}), "installation_id", receipt.InstallationID)
	err := sharedEconomicExact(q, "revision_digest", digest).Take(&row).Error
	if err == nil {
		if row.InstallationID != receipt.InstallationID || row.RevisionDigest != digest || row.Revision != bound.Revision ||
			!sharedIdentity(row.RequestID) || row.RequestDigest != sharedEconomicIdentityDigest(row.RequestID) || row.RecordedAt <= 0 {
			return errSharedAdmissionConflict
		}
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	row = entity.AdmissionEconomicInvalidBound{InstallationID: receipt.InstallationID, RevisionDigest: digest, Revision: bound.Revision,
		RequestDigest: receipt.RequestDigest, RequestID: receipt.RequestID, RecordedAt: instant}
	result := sharedAuthorizationDB(tx).Omit(clause.Associations).Create(&row)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errSharedAdmissionConflict
	}
	return nil
}
