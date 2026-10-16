package service

import (
	"context"
	"database/sql"
	"errors"
	"os"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/id"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Startup locks follow governance -> installation -> mode. Owner registration
// also takes installation before mode. No lease expiry or process registration
// establishes local history or authorizes a change of admission mode.
func lockLocalRecorderDomain(tx *gorm.DB) (entity.LimitInstallation, *entity.AdmissionMode, error) {
	if err := lockGovernance(sharedAuthorizationDB(tx)); err != nil {
		return entity.LimitInstallation{}, nil, err
	}
	var bindings []entity.LimitInstallation
	if err := sharedAuthorizationDB(tx).Clauses(clause.Locking{Strength: "UPDATE"}).Order("id").Limit(2).Find(&bindings).Error; err != nil {
		return entity.LimitInstallation{}, nil, err
	}
	if len(bindings) != 1 || bindings[0].ID != 1 {
		return entity.LimitInstallation{}, nil, eventqueue.ErrInvalid
	}
	var modes []entity.AdmissionMode
	if err := sharedAuthorizationDB(tx).Clauses(clause.Locking{Strength: "UPDATE"}).Order("id").Limit(2).Find(&modes).Error; err != nil {
		return bindings[0], nil, err
	}
	if len(modes) > 1 {
		return bindings[0], nil, eventqueue.ErrInvalid
	}
	var mode *entity.AdmissionMode
	if len(modes) == 1 {
		mode = &modes[0]
	}
	if !localRecorderDomainValid(bindings[0], mode) {
		return bindings[0], mode, eventqueue.ErrInvalid
	}
	// A local startup cannot borrow a previous shared owner's retained history.
	var owners int64
	if err := sharedAuthorizationDB(tx).Model(&entity.AdmissionSpoolOwner{}).Count(&owners).Error; err != nil {
		return bindings[0], mode, err
	}
	if owners != 0 {
		return bindings[0], mode, eventqueue.ErrInvalid
	}
	if !bindings[0].Initialized {
		var quota entity.QuotaSetting
		if err := sharedAuthorizationDB(tx).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&quota, 1).Error; err != nil {
			return bindings[0], mode, err
		}
		var calls int64
		if err := sharedAuthorizationDB(tx).Model(&entity.CallRecord{}).Count(&calls).Error; err != nil {
			return bindings[0], mode, err
		}
		if quota.ID != 1 || quota.AccountingStarted || calls != 0 {
			return bindings[0], mode, eventqueue.ErrInvalid
		}
	}
	return bindings[0], mode, tx.Statement.Context.Err()
}

func localRecorderDomainValid(binding entity.LimitInstallation, mode *entity.AdmissionMode) bool {
	if binding.ID != 1 || binding.JournalID != "" && !admissionOwnerJournal.MatchString(binding.JournalID) {
		return false
	}
	if !binding.Initialized {
		return mode == nil
	}
	return admissionOwnerJournal.MatchString(binding.JournalID) && mode != nil && mode.ID == 1 &&
		mode.InstallationID == binding.JournalID && mode.Mode == "local" && mode.Revision > 0
}

// Persist only the immutable identity first. V118 forbids a mode row alongside
// an uninitialized binding; mode and Initialized are acknowledged together only
// after the exact file bind. An uncertain filesystem/SQL outcome retains this ID.
func (s *Service) prepareLocalRecorderIdentity(ctx context.Context, path string) error {
	if s == nil || s.db == nil || ctx == nil || path == "" {
		return eventqueue.ErrInvalid
	}
	return s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		binding, _, err := lockLocalRecorderDomain(tx)
		if err != nil || binding.JournalID != "" {
			return err
		}
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			if err != nil {
				return err
			}
			return eventqueue.ErrInvalid
		}
		journalID, err := id.NewPrefixed("led")
		if err != nil {
			return err
		}
		updated := sharedAuthorizationDB(tx).Model(&entity.LimitInstallation{}).Where("id = ? AND initialized = ? AND journal_id = ?", 1, false, "").Update("journal_id", journalID)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return eventqueue.ErrInvalid
		}
		return ctx.Err()
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
}

func (s *Service) openLocalRecorderJournal(ctx context.Context, path string) (*eventqueue.Queue, error) {
	if err := s.prepareLocalRecorderIdentity(ctx, path); err != nil {
		return nil, err
	}
	var queue *eventqueue.Queue
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		binding, mode, err := lockLocalRecorderDomain(tx)
		if err != nil || !admissionOwnerJournal.MatchString(binding.JournalID) {
			if err != nil {
				return err
			}
			return eventqueue.ErrInvalid
		}
		queue, err = eventqueue.OpenBoundLocal(ctx, path, callQueueCapacity, callQueuePayloadLimit, binding.JournalID, binding.Initialized)
		if err != nil {
			return err
		}
		if mode == nil {
			mode = &entity.AdmissionMode{ID: 1, InstallationID: binding.JournalID, Mode: "local", Revision: 1}
			if err := sharedAuthorizationDB(tx).Omit("Installation").Create(mode).Error; err != nil {
				return err
			}
			updated := sharedAuthorizationDB(tx).Model(&entity.LimitInstallation{}).Where("id = ? AND initialized = ? AND journal_id = ?", 1, false, binding.JournalID).Update("initialized", true)
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected != 1 {
				return eventqueue.ErrInvalid
			}
		}
		return ctx.Err()
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil && queue != nil {
		_ = queue.Close()
		queue = nil
	}
	return queue, err
}
