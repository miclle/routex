package service

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/routeattempt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var _ sharedEconomicAuthority = (*sharedEconomicSQLStore)(nil)

func (s *sharedEconomicSQLStore) MarkDispatch(ctx context.Context, attempt sharedAttemptIntent) (*sharedDispatchReceipt, error) {
	return s.markSharedEconomicDispatchSQL(ctx, attempt, time.Now().UTC())
}

// A persisted acknowledgement never executes a request. The coordinator poisons
// uncertain attempts before this call; only its original live executor may use a
// successful response, followed by the existing transport/source dispatch guard.
func (s *sharedEconomicSQLStore) markSharedEconomicDispatchSQL(ctx context.Context, attempt sharedAttemptIntent, now time.Time) (*sharedDispatchReceipt, error) {
	if ctx == nil || !sharedEconomicKeyValid(attempt.Key) || !sharedHasDigest(attempt, attempt.Digest) || now.UnixNano() <= 0 {
		return nil, errSharedAdmissionInvalid
	}
	body, err := sharedEconomicCanonicalBody(attempt)
	if err != nil {
		return nil, err
	}
	release, err := s.lockLive()
	if err != nil {
		return nil, err
	}
	defer release()
	if !s.live.acceptedHere(attempt.Key) {
		return nil, errSharedDispatchForbidden
	}
	// Only this transient request supplies the original intent before economic
	// locks. Historical Read remains independently available without live inputs.
	intent := sharedAdmissionIntent{sharedAdmissionRequest: s.live.original, Owner: s.live.owner,
		ExecutionID: attempt.Key.ExecutionID, Digest: attempt.Key.AdmissionDigest}
	if sharedIntentKey(intent) != attempt.Key || validateSharedSpoolAdmission(intent) != nil {
		return nil, errSharedAdmissionConflict
	}
	selected, err := sharedEconomicSelectedAttempt(intent, attempt)
	if err != nil {
		return nil, err
	}
	if err := s.live.check(ctx, intent, &selected); err != nil {
		return nil, err
	}
	var result *sharedDispatchReceipt
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.validateCurrentSQL(tx, intent, &selected, now); err != nil {
			return err
		}
		domain, err := lockSharedEconomicDomain(tx, intent.Owner.InstallationID)
		if err != nil {
			return err
		}
		receipt, local, err := readSharedEconomicHistorySQL(tx, attempt.Key)
		if err != nil {
			return err
		}
		if local.Terminal != nil {
			return errSharedDispatchForbidden
		}
		accounts, err := lockSharedEconomicAccountsSQL(tx, intent.Owner.InstallationID, intent.Policies, receipt.AdmittedAt, false)
		if err != nil {
			return err
		}
		for _, account := range accounts {
			for _, kind := range []string{"active_quota", "executor"} {
				var quota *eventqueue.QuotaCounterContribution
				if kind == "active_quota" {
					quota = &eventqueue.QuotaCounterContribution{Bound: intent.Bound}
				}
				expected, err := newSharedEconomicProjection(receipt, account, kind, "active", quota)
				if err != nil {
					return err
				}
				retained, err := readSharedEconomicProjectionSQL(tx, account, receipt.RequestDigest, kind, "active")
				if err != nil || !bytes.Equal(retained.ContributionBody, expected.ContributionBody) || retained.ExpiresAt != nil {
					return errSharedAdmissionConflict
				}
			}
		}
		invalid, err := readSharedEconomicInvalidBoundSQL(tx, intent.Owner.InstallationID, intent.Bound.Revision, max(now.UnixNano(), domain.ClockHighWater))
		if err != nil {
			return err
		}
		if invalid {
			return eventqueue.ErrQuotaBound
		}
		if err := validateSharedSpoolAttempt(local, attempt); err != nil {
			return err
		}
		if attempt.Number <= len(local.Attempts) {
			prior, err := sharedEconomicCanonicalBody(local.Attempts[attempt.Number-1])
			if err != nil || !bytes.Equal(prior, body) {
				return errSharedAdmissionConflict
			}
			result = &sharedDispatchReceipt{Key: attempt.Key, Number: attempt.Number, AttemptDigest: attempt.Digest}
			return s.live.check(ctx, intent, &selected)
		}
		if attempt.Number != len(local.Attempts)+1 || len(local.Checkpoints) != len(local.Attempts) {
			return errSharedAdmissionConflict
		}
		if err := validateSharedSpoolPrefix(local, attempt); err != nil {
			return err
		}
		row := entity.AdmissionEconomicAttempt{InstallationID: intent.Owner.InstallationID, RequestDigest: sharedEconomicIdentityDigest(intent.RequestID),
			Number: int64(attempt.Number), RequestID: intent.RequestID, AttemptID: attempt.AttemptID, AttemptIdentityDigest: sharedEconomicIdentityDigest(attempt.AttemptID),
			AttemptDigest: attempt.Digest, DispatchBody: body}
		inserted := sharedAuthorizationDB(tx).Omit(clause.Associations).Create(&row)
		if inserted.Error != nil {
			return inserted.Error
		}
		if inserted.RowsAffected != 1 {
			return errSharedAdmissionConflict
		}
		if err := advanceSharedEconomicClockSQL(tx, domain, max(now.UnixNano(), domain.ClockHighWater)); err != nil {
			return err
		}
		result = &sharedDispatchReceipt{Key: attempt.Key, Number: attempt.Number, AttemptDigest: attempt.Digest}
		return s.live.check(ctx, intent, &selected)
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, sharedEconomicSQLError(err)
	}
	return result, nil
}

func sharedEconomicSelectedAttempt(intent sharedAdmissionIntent, attempt sharedAttemptIntent) (routeattempt.Attempt, error) {
	if intent.Reservation == nil || attempt.SnapshotID != intent.Reservation.SnapshotID || attempt.Number < 1 || attempt.Number > routeattempt.MaxAttempts {
		return routeattempt.Attempt{}, errSharedAdmissionConflict
	}
	var selected routeattempt.Attempt
	for _, candidate := range intent.Reservation.Candidates {
		if candidate.Credential.Identity.ID == attempt.CredentialID && candidate.Connection.ID == attempt.ConnectionID && candidate.ProviderModel.ID == attempt.ProviderModelID {
			if selected.TargetID != "" {
				return routeattempt.Attempt{}, errSharedAdmissionConflict
			}
			selected = routeattempt.Attempt{Number: attempt.Number, TargetID: candidate.Binding.ID, ConnectionID: candidate.Connection.ID,
				CredentialID: candidate.Credential.Identity.ID, Protocol: intent.Reservation.Protocol}
		}
	}
	if selected.TargetID == "" {
		return selected, errSharedDispatchForbidden
	}
	_, err := gatewayReservationSQLSelection(intent.Reservation, &selected)
	if err != nil {
		return routeattempt.Attempt{}, errors.Join(errSharedAdmissionConflict, err)
	}
	return selected, nil
}
