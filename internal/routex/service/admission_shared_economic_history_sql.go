package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/routeattempt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var errSharedEconomicUnavailable = errors.New("shared economic storage unavailable")

// Historical reads/checkpoints require the exact accepted key, independently of
// current owner, authentication, mode and route authority. They permit no dispatch.
// New admission and dispatch additionally require the transient live authority.
type sharedEconomicSQLStore struct {
	db   *gorm.DB
	live *sharedEconomicLiveAuthority
}

func sharedEconomicIdentityDigest(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func sharedEconomicKeyValid(key sharedAdmissionKey) bool {
	return admissionOwnerJournal.MatchString(key.Owner.InstallationID) && admissionOwnerSpool.MatchString(key.Owner.SpoolID) &&
		admissionOwnerInstance.MatchString(key.Owner.IncarnationID) && key.Owner.ModeRevision > 0 && key.Owner.ModeRevision <= math.MaxInt64 &&
		key.Owner.Fence > 0 && key.Owner.Fence <= math.MaxInt64 && sharedIdentity(key.RequestID) && sharedIdentity(key.ExecutionID) &&
		runtimeRouteDigest.MatchString(key.AdmissionDigest)
}

func sharedEconomicSQLError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, gorm.ErrRecordNotFound):
		return eventqueue.ErrMissing
	case errors.Is(err, gorm.ErrDuplicatedKey):
		return errSharedAdmissionConflict
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, errSharedAdmissionConflict), errors.Is(err, errSharedAdmissionInvalid), errors.Is(err, eventqueue.ErrMissing):
		return err
	default:
		return errors.Join(errSharedEconomicUnavailable, err)
	}
}

func sharedEconomicExact(tx *gorm.DB, column, value string) *gorm.DB {
	return tx.Where(database.ExactText(tx, clause.Column{Name: column}, value))
}

func sharedEconomicDomainValid(domain entity.AdmissionEconomicDomain, installation string) bool {
	return domain.InstallationID == installation && admissionOwnerJournal.MatchString(installation) && domain.FormatVersion == 1 &&
		domain.Initialized && domain.CoverageStart > 0 && domain.ClockHighWater >= domain.CoverageStart &&
		domain.ReceiptCapacity > 0 && domain.ReceiptCount >= 0 && domain.ReceiptCount <= domain.ReceiptCapacity
}

// The domain lock serializes both absent-row creation and coherent history.
// Historical operations never acquire governance after this lock.
func lockSharedEconomicDomain(tx *gorm.DB, installation string) (entity.AdmissionEconomicDomain, error) {
	var row entity.AdmissionEconomicDomain
	err := sharedEconomicExact(sharedAuthorizationDB(tx).Clauses(clause.Locking{Strength: "UPDATE"}), "installation_id", installation).Take(&row).Error
	if err != nil {
		return row, err
	}
	if !sharedEconomicDomainValid(row, installation) {
		return row, errSharedAdmissionConflict
	}
	return row, nil
}

func sharedEconomicCanonicalBody(value any) ([]byte, error) {
	body, err := json.Marshal(value)
	if err != nil || len(body) == 0 || len(body) > eventqueue.SharedSpoolMaxDataBytes {
		return nil, errSharedAdmissionInvalid
	}
	return body, nil
}

func sharedEconomicDecodeBody[T any](body []byte, result *T) bool {
	if result == nil || len(body) == 0 || len(body) > eventqueue.SharedSpoolMaxDataBytes || !sharedDecodeExact(body, result) {
		return false
	}
	canonical, err := sharedEconomicCanonicalBody(*result)
	return err == nil && bytes.Equal(body, canonical)
}

func sharedEconomicReceiptIntent(row entity.AdmissionEconomicReceipt) (sharedAdmissionIntent, error) {
	var intent sharedAdmissionIntent
	if !sharedEconomicDecodeBody(row.IntentBody, &intent) || validateSharedSpoolAdmission(intent) != nil ||
		eventqueue.ValidateQuotaBound(intent.Bound) != nil || eventqueue.ValidateQuotaRecovery(intent.Bound, intent.Recovery) != nil ||
		row.InstallationID != intent.Owner.InstallationID || row.RequestID != intent.RequestID || row.RequestDigest != sharedEconomicIdentityDigest(intent.RequestID) ||
		row.ExecutionID != intent.ExecutionID || row.ExecutionDigest != sharedEconomicIdentityDigest(intent.ExecutionID) ||
		!sharedIdentity(row.ReceiptID) || row.ReceiptDigest != sharedEconomicIdentityDigest(row.ReceiptID) || row.AdmissionDigest != intent.Digest ||
		row.SpoolID != intent.Owner.SpoolID || row.IncarnationID != intent.Owner.IncarnationID || row.ModeRevision <= 0 || row.Fence <= 0 ||
		uint64(row.ModeRevision) != intent.Owner.ModeRevision || uint64(row.Fence) != intent.Owner.Fence ||
		!admissionOwnerIdentityValid(intent.Owner, row.OwnerStartedAt) || !row.OwnerStartedAt.Equal(intent.OwnerStartedAt) || row.AdmittedAt <= 0 || row.MonthStart < 0 ||
		row.OwnerStartedAt.UnixNano() <= 0 || row.OwnerStartedAt.UnixNano() > row.AdmittedAt || intent.TimeZone == "Local" ||
		intent.Authorization.EvaluatedAt.UnixNano() <= 0 || intent.Authorization.EvaluatedAt.UnixNano() > row.AdmittedAt ||
		row.MonthStart > row.AdmittedAt || row.MonthEnd <= row.AdmittedAt || (row.TerminalDigest == nil) != (row.TerminalBody == nil) {
		return intent, errSharedAdmissionConflict
	}
	start, end, err := eventqueue.QuotaMonthBounds(row.AdmittedAt, intent.TimeZone)
	if err != nil || start != row.MonthStart || end != row.MonthEnd {
		return intent, errSharedAdmissionConflict
	}
	return intent, nil
}

// Caller holds the domain before taking the request lock. Reconstruct each
// transition against its exact immutable predecessor, not current resources.
func readSharedEconomicHistorySQL(tx *gorm.DB, key sharedAdmissionKey) (entity.AdmissionEconomicReceipt, *sharedSpoolRecord, error) {
	var row entity.AdmissionEconomicReceipt
	q := sharedEconomicExact(sharedAuthorizationDB(tx).Clauses(clause.Locking{Strength: "UPDATE"}), "installation_id", key.Owner.InstallationID)
	if err := sharedEconomicExact(q, "request_digest", sharedEconomicIdentityDigest(key.RequestID)).Take(&row).Error; err != nil {
		return row, nil, err
	}
	intent, err := sharedEconomicReceiptIntent(row)
	if err != nil || sharedIntentKey(intent) != key {
		return row, nil, errSharedAdmissionConflict
	}
	local := &sharedSpoolRecord{Intent: intent, ReceiptID: row.ReceiptID}
	var attempts []entity.AdmissionEconomicAttempt
	q = sharedEconomicExact(sharedAuthorizationDB(tx).Clauses(clause.Locking{Strength: "UPDATE"}), "installation_id", row.InstallationID)
	if err := sharedEconomicExact(q, "request_digest", row.RequestDigest).Order("number").Limit(routeattempt.MaxAttempts + 1).Find(&attempts).Error; err != nil {
		return row, nil, err
	}
	if len(attempts) > routeattempt.MaxAttempts {
		return row, nil, errSharedAdmissionConflict
	}
	for i, retained := range attempts {
		var attempt sharedAttemptIntent
		if retained.InstallationID != row.InstallationID || retained.RequestDigest != row.RequestDigest || retained.RequestID != row.RequestID ||
			retained.Number != int64(i+1) || !sharedEconomicDecodeBody(retained.DispatchBody, &attempt) ||
			retained.AttemptID != attempt.AttemptID || retained.AttemptIdentityDigest != sharedEconomicIdentityDigest(attempt.AttemptID) ||
			retained.AttemptDigest != attempt.Digest || attempt.Number != i+1 || validateSharedSpoolAttempt(local, attempt) != nil ||
			validateSharedSpoolPrefix(local, attempt) != nil || (retained.CheckpointDigest == nil) != (retained.CheckpointBody == nil) {
			return row, nil, errSharedAdmissionConflict
		}
		local.Attempts = append(local.Attempts, attempt)
		local.Dispatches = append(local.Dispatches, sharedDispatchReceipt{Key: key, Number: attempt.Number, AttemptDigest: attempt.Digest})
		if retained.CheckpointDigest != nil {
			var checkpoint sharedAttemptCheckpoint
			if !sharedEconomicDecodeBody(retained.CheckpointBody, &checkpoint) || *retained.CheckpointDigest != checkpoint.Digest ||
				checkpoint.Number != attempt.Number || validateSharedSpoolCheckpoint(local, checkpoint) != nil {
				return row, nil, errSharedAdmissionConflict
			}
			local.Checkpoints = append(local.Checkpoints, checkpoint)
		}
	}
	if row.TerminalDigest != nil {
		var terminal sharedTerminalIntent
		if !sharedEconomicDecodeBody(row.TerminalBody, &terminal) || *row.TerminalDigest != terminal.Digest || validateSharedSpoolTerminal(local, terminal) != nil {
			return row, nil, errSharedAdmissionConflict
		}
		local.Terminal = &terminal
	}
	return row, local, tx.Statement.Context.Err()
}

func sharedEconomicAuthorityView(local *sharedSpoolRecord) *sharedAuthorityReceipt {
	return &sharedAuthorityReceipt{Key: sharedIntentKey(local.Intent), ReceiptID: local.ReceiptID,
		Dispatches: local.Dispatches, Checkpoints: local.Checkpoints, Terminal: local.Terminal}
}

func (s *sharedEconomicSQLStore) Read(ctx context.Context, key sharedAdmissionKey) (*sharedAuthorityReceipt, error) {
	if s == nil || s.db == nil || ctx == nil || !sharedEconomicKeyValid(key) {
		return nil, errSharedAdmissionInvalid
	}
	var receipt *sharedAuthorityReceipt
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := lockSharedEconomicDomain(tx, key.Owner.InstallationID); err != nil {
			return err
		}
		_, local, err := readSharedEconomicHistorySQL(tx, key)
		if err != nil {
			return err
		}
		receipt = sharedEconomicAuthorityView(local)
		return ctx.Err()
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, sharedEconomicSQLError(err)
	}
	return receipt, nil
}

func (s *sharedEconomicSQLStore) Checkpoint(ctx context.Context, checkpoint sharedAttemptCheckpoint) error {
	if s == nil || s.db == nil || ctx == nil || !sharedEconomicKeyValid(checkpoint.Key) || !sharedHasDigest(checkpoint, checkpoint.Digest) {
		return errSharedAdmissionInvalid
	}
	body, err := sharedEconomicCanonicalBody(checkpoint)
	if err != nil {
		return err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := lockSharedEconomicDomain(tx, checkpoint.Key.Owner.InstallationID); err != nil {
			return err
		}
		row, local, err := readSharedEconomicHistorySQL(tx, checkpoint.Key)
		if err != nil {
			return err
		}
		if err := validateSharedSpoolCheckpoint(local, checkpoint); err != nil {
			return err
		}
		if checkpoint.Number <= len(local.Checkpoints) {
			prior, err := sharedEconomicCanonicalBody(local.Checkpoints[checkpoint.Number-1])
			if err != nil || !bytes.Equal(prior, body) {
				return errSharedAdmissionConflict
			}
			return ctx.Err()
		}
		if local.Terminal != nil || checkpoint.Number != len(local.Checkpoints)+1 || checkpoint.Number != len(local.Attempts) {
			return errSharedAdmissionConflict
		}
		q := sharedEconomicExact(sharedAuthorizationDB(tx).Model(&entity.AdmissionEconomicAttempt{}), "installation_id", row.InstallationID)
		q = sharedEconomicExact(q, "request_digest", row.RequestDigest).Where("number = ? AND checkpoint_digest IS NULL AND checkpoint_body IS NULL", checkpoint.Number)
		result := q.Updates(map[string]any{"checkpoint_digest": checkpoint.Digest, "checkpoint_body": body})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errSharedAdmissionConflict
		}
		return ctx.Err()
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	return sharedEconomicSQLError(err)
}
