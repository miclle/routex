package service

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"regexp"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	errAdmissionOwnerInvalid     = errors.New("invalid admission owner intent")
	errAdmissionOwnerConflict    = errors.New("admission owner intent conflicts")
	errAdmissionOwnerUnavailable = errors.New("admission owner storage unavailable")
	admissionOwnerUUID           = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	admissionOwnerJournal        = regexp.MustCompile(`^led_[0-7][0-9a-hjkmnp-tv-z]{25}$`)
	admissionOwnerInstance       = regexp.MustCompile(`^ins_[0-7][0-9a-hjkmnp-tv-z]{25}$`)
	admissionOwnerSpool          = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,63}$`)
)

type admissionOwnerRegistration struct {
	RegistrationID    string
	Owner             sharedAdmissionOwner
	InstanceStartedAt time.Time
}
type admissionOwnerRegistrationReceipt struct {
	admissionOwnerRegistration
	RegisteredAt time.Time
}
type admissionOwnerFence struct {
	FencingID         string
	Owner             sharedAdmissionOwner
	InstanceStartedAt time.Time
}
type admissionOwnerFenceReceipt struct {
	admissionOwnerFence
	ResultFence uint64
	FencedAt    time.Time
}

// This store is intentionally not sharedEconomicAuthority. Receipts identify
// durable owner intent only, never current call authorization or economics.
type admissionOwnerStore struct{ db *gorm.DB }

func admissionOwnerTimeValid(at time.Time) bool {
	return !at.IsZero() && at.Year() >= 1 && at.Year() <= 9999 && at.Nanosecond()%1000 == 0
}
func admissionOwnerIdentityValid(owner sharedAdmissionOwner, birth time.Time) bool {
	return admissionOwnerJournal.MatchString(owner.InstallationID) &&
		admissionOwnerSpool.MatchString(owner.SpoolID) && admissionOwnerInstance.MatchString(owner.IncarnationID) &&
		owner.ModeRevision > 0 && owner.ModeRevision <= math.MaxInt64 && owner.Fence == 1 && admissionOwnerTimeValid(birth)
}
func admissionOwnerRegistrationValid(in admissionOwnerRegistration) bool {
	return admissionOwnerUUID.MatchString(in.RegistrationID) && admissionOwnerIdentityValid(in.Owner, in.InstanceStartedAt)
}
func admissionOwnerFenceValid(in admissionOwnerFence) bool {
	return admissionOwnerUUID.MatchString(in.FencingID) && admissionOwnerIdentityValid(in.Owner, in.InstanceStartedAt)
}
func admissionOwnerMatches(row entity.AdmissionSpoolOwner, owner sharedAdmissionOwner, birth time.Time) bool {
	return row.InstallationID == owner.InstallationID && row.SpoolID == owner.SpoolID &&
		row.ModeRevision == int64(owner.ModeRevision) && row.IncarnationID == owner.IncarnationID &&
		row.RegisteredFence == int64(owner.Fence) && row.InstanceStartedAt.Equal(birth)
}
func admissionOwnerOriginal(row entity.AdmissionSpoolOwner) sharedAdmissionOwner {
	return sharedAdmissionOwner{InstallationID: row.InstallationID, ModeRevision: uint64(row.ModeRevision), SpoolID: row.SpoolID, IncarnationID: row.IncarnationID, Fence: uint64(row.RegisteredFence)}
}
func admissionOwnerRowValid(row entity.AdmissionSpoolOwner) bool {
	in := admissionOwnerRegistration{RegistrationID: row.RegistrationID, Owner: admissionOwnerOriginal(row), InstanceStartedAt: row.InstanceStartedAt}
	if !admissionOwnerRegistrationValid(in) || row.ModeID != 1 || !admissionOwnerTimeValid(row.RegisteredAt) || row.RegisteredAt.Before(row.InstanceStartedAt) {
		return false
	}
	if row.CurrentFence == 1 {
		return row.FencingID == nil && row.FencedAt == nil
	}
	return row.CurrentFence == 2 && row.FencingID != nil && admissionOwnerUUID.MatchString(*row.FencingID) && row.FencedAt != nil && admissionOwnerTimeValid(*row.FencedAt) && !row.FencedAt.Before(row.RegisteredAt)
}
func admissionOwnerRegistrationView(row entity.AdmissionSpoolOwner) admissionOwnerRegistrationReceipt {
	return admissionOwnerRegistrationReceipt{admissionOwnerRegistration: admissionOwnerRegistration{RegistrationID: row.RegistrationID, Owner: admissionOwnerOriginal(row), InstanceStartedAt: row.InstanceStartedAt.UTC()}, RegisteredAt: row.RegisteredAt.UTC()}
}
func admissionOwnerFenceView(row entity.AdmissionSpoolOwner) admissionOwnerFenceReceipt {
	return admissionOwnerFenceReceipt{admissionOwnerFence: admissionOwnerFence{FencingID: *row.FencingID, Owner: admissionOwnerOriginal(row), InstanceStartedAt: row.InstanceStartedAt.UTC()}, ResultFence: uint64(row.CurrentFence), FencedAt: row.FencedAt.UTC()}
}

// Lock order is initialized LimitInstallation -> mode -> owner -> process.
// Existing journal binding takes only LimitInstallation; SystemInstance writers
// take governance then process, or process alone, and never these owner locks.
// A future combined authority must take governance BEFORE this private order.
func lockAdmissionOwnerDomain(tx *gorm.DB) (entity.LimitInstallation, entity.AdmissionMode, error) {
	var binding entity.LimitInstallation
	var mode entity.AdmissionMode
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&binding, 1).Error; err != nil {
		return binding, mode, err
	}
	if binding.ID != 1 {
		return binding, mode, errAdmissionOwnerConflict
	}
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&mode, 1).Error; err != nil {
		return binding, mode, err
	}
	if mode.ID != 1 {
		return binding, mode, errAdmissionOwnerConflict
	}
	return binding, mode, nil
}
func admissionOwnerDomainMatches(binding entity.LimitInstallation, mode entity.AdmissionMode, owner sharedAdmissionOwner) bool {
	return binding.ID == 1 && binding.Initialized && admissionOwnerJournal.MatchString(binding.JournalID) && binding.JournalID == owner.InstallationID && mode.ID == 1 && mode.InstallationID == owner.InstallationID && mode.Mode == "shared" && mode.Revision == int64(owner.ModeRevision)
}
func admissionOwnerExact(tx *gorm.DB, column, value string) *gorm.DB {
	return tx.Where(database.ExactText(tx, clause.Column{Name: column}, value))
}
func admissionOwnerStorageError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, gorm.ErrDuplicatedKey) || errors.Is(err, errAdmissionOwnerConflict) {
		return errAdmissionOwnerConflict
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errors.Join(errAdmissionOwnerUnavailable, err)
}

func (s *admissionOwnerStore) RegisterOwner(ctx context.Context, in admissionOwnerRegistration) (*admissionOwnerRegistrationReceipt, error) {
	if ctx == nil || s == nil || s.db == nil || !admissionOwnerRegistrationValid(in) {
		return nil, errAdmissionOwnerInvalid
	}
	var receipt admissionOwnerRegistrationReceipt
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		binding, mode, err := lockAdmissionOwnerDomain(tx)
		if err != nil {
			return err
		}
		var prior entity.AdmissionSpoolOwner
		err = admissionOwnerExact(tx.Clauses(clause.Locking{Strength: "UPDATE"}), "registration_id", in.RegistrationID).Take(&prior).Error
		if err == nil {
			if !admissionOwnerRowValid(prior) || prior.RegistrationID != in.RegistrationID || !admissionOwnerMatches(prior, in.Owner, in.InstanceStartedAt) {
				return errAdmissionOwnerConflict
			}
			// Original registration survives fencing. This is a historical receipt,
			// not a check that the old fence or current mode still authorizes work.
			receipt = admissionOwnerRegistrationView(prior)
			return ctx.Err()
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if !admissionOwnerDomainMatches(binding, mode, in.Owner) {
			return errAdmissionOwnerConflict
		}
		var occupied entity.AdmissionSpoolOwner
		err = admissionOwnerExact(admissionOwnerExact(tx.Clauses(clause.Locking{Strength: "UPDATE"}), "installation_id", in.Owner.InstallationID), "spool_id", in.Owner.SpoolID).Take(&occupied).Error
		if err == nil {
			return errAdmissionOwnerConflict
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var process entity.SystemInstance
		if err := admissionOwnerExact(tx.Clauses(clause.Locking{Strength: "UPDATE"}), "id", in.Owner.IncarnationID).Take(&process).Error; err != nil {
			return err
		}
		if process.ID != in.Owner.IncarnationID || !process.StartedAt.Equal(in.InstanceStartedAt) {
			return errAdmissionOwnerConflict
		}
		// Lease expiry/stopped metadata does not prove executor closure, nor does it
		// permit reusing a spool slot occupied by another registration.
		now := time.Now().UTC().Truncate(time.Microsecond)
		if now.Before(in.InstanceStartedAt) {
			return errAdmissionOwnerConflict
		}
		row := entity.AdmissionSpoolOwner{InstallationID: in.Owner.InstallationID, SpoolID: in.Owner.SpoolID, ModeID: 1, ModeRevision: int64(in.Owner.ModeRevision), RegistrationID: in.RegistrationID, IncarnationID: in.Owner.IncarnationID, InstanceStartedAt: in.InstanceStartedAt.UTC(), RegisteredAt: now, RegisteredFence: 1, CurrentFence: 1}
		if err := ctx.Err(); err != nil {
			return err
		}
		insert := tx.Omit("Mode").Create(&row)
		if insert.Error != nil {
			return insert.Error
		}
		if insert.RowsAffected != 1 {
			return errAdmissionOwnerUnavailable
		}
		receipt = admissionOwnerRegistrationView(row)
		return ctx.Err()
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, admissionOwnerStorageError(err)
	}
	return &receipt, nil
}

func (s *admissionOwnerStore) FenceOwner(ctx context.Context, in admissionOwnerFence) (*admissionOwnerFenceReceipt, error) {
	if ctx == nil || s == nil || s.db == nil || !admissionOwnerFenceValid(in) {
		return nil, errAdmissionOwnerInvalid
	}
	var receipt admissionOwnerFenceReceipt
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		binding, mode, err := lockAdmissionOwnerDomain(tx)
		if err != nil {
			return err
		}
		var row entity.AdmissionSpoolOwner
		if err := admissionOwnerExact(admissionOwnerExact(tx.Clauses(clause.Locking{Strength: "UPDATE"}), "installation_id", in.Owner.InstallationID), "spool_id", in.Owner.SpoolID).Take(&row).Error; err != nil {
			return err
		}
		if !admissionOwnerRowValid(row) || !admissionOwnerMatches(row, in.Owner, in.InstanceStartedAt) {
			return errAdmissionOwnerConflict
		}
		if row.FencingID != nil {
			if *row.FencingID != in.FencingID {
				return errAdmissionOwnerConflict
			}
			receipt = admissionOwnerFenceView(row)
			return ctx.Err()
		}
		if !admissionOwnerDomainMatches(binding, mode, in.Owner) || row.CurrentFence != 1 {
			return errAdmissionOwnerConflict
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		if now.Before(row.RegisteredAt) {
			return errAdmissionOwnerConflict
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		// The row lock and exact original body fence this single terminal change.
		// No economic hold, process metadata or spool record is modified.
		updateQuery := tx.Model(&entity.AdmissionSpoolOwner{}).
			Where("mode_id = ? AND mode_revision = ? AND registered_fence = ? AND current_fence = ? AND instance_started_at = ? AND registered_at = ? AND fencing_id IS NULL AND fenced_at IS NULL", row.ModeID, row.ModeRevision, row.RegisteredFence, int64(1), row.InstanceStartedAt, row.RegisteredAt)
		for _, identity := range []struct{ column, value string }{
			{"installation_id", row.InstallationID}, {"spool_id", row.SpoolID},
			{"registration_id", row.RegistrationID}, {"incarnation_id", row.IncarnationID},
		} {
			updateQuery = admissionOwnerExact(updateQuery, identity.column, identity.value)
		}
		update := updateQuery.Updates(map[string]any{"current_fence": int64(2), "fencing_id": in.FencingID, "fenced_at": now})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return errAdmissionOwnerConflict
		}
		row.CurrentFence, row.FencingID, row.FencedAt = 2, &in.FencingID, &now
		receipt = admissionOwnerFenceView(row)
		return ctx.Err()
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, admissionOwnerStorageError(err)
	}
	return &receipt, nil
}

// CheckCurrentOwner must be called inside the future authority's transaction.
// It checks only the durable owner fence, never a Session/Key/grant, quota,
// executor closure, dispatch eligibility or economic settlement.
func (s *admissionOwnerStore) CheckCurrentOwner(tx *gorm.DB, owner sharedAdmissionOwner, birth time.Time) error {
	if tx == nil || !admissionOwnerIdentityValid(owner, birth) {
		return errAdmissionOwnerInvalid
	}
	binding, mode, err := lockAdmissionOwnerDomain(tx)
	if err != nil {
		return admissionOwnerStorageError(err)
	}
	if !admissionOwnerDomainMatches(binding, mode, owner) {
		return errAdmissionOwnerConflict
	}
	var row entity.AdmissionSpoolOwner
	if err := admissionOwnerExact(admissionOwnerExact(tx.Clauses(clause.Locking{Strength: "UPDATE"}), "installation_id", owner.InstallationID), "spool_id", owner.SpoolID).Take(&row).Error; err != nil {
		return admissionOwnerStorageError(err)
	}
	if !admissionOwnerRowValid(row) || !admissionOwnerMatches(row, owner, birth) || row.CurrentFence != int64(owner.Fence) {
		return errAdmissionOwnerConflict
	}
	var process entity.SystemInstance
	if err := admissionOwnerExact(tx.Clauses(clause.Locking{Strength: "UPDATE"}), "id", owner.IncarnationID).Take(&process).Error; err != nil {
		return admissionOwnerStorageError(err)
	}
	if process.ID != owner.IncarnationID || !process.StartedAt.Equal(birth) {
		return errAdmissionOwnerConflict
	}
	return nil
}
