package service

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
)

// Integrity describes one complete authenticated cold read. It is not an
// activation review, future readability guarantee or serving acknowledgement.
type RuntimeArchiveValidation struct {
	Scope            string    `json:"scope"`
	ArchiveID        string    `json:"archive_id"`
	ArchiveCreatedAt time.Time `json:"archive_created_at"`
	CodecVersion     int       `json:"codec_version"`
	ObservedAt       time.Time `json:"observed_at"`
	Integrity        string    `json:"integrity"`
}

func (s *Service) runtimeArchiveValidationActor(tx *gorm.DB, auth *Authentication) error {
	actor, err := s.runtimePublicationActor(tx, auth)
	if err != nil {
		return err
	}
	allowed, err := exactGovernancePermissionForAdmittedActor(tx, actor, "system.read")
	if err != nil {
		return err
	}
	if !allowed {
		return apperrors.ErrForbidden
	}
	return tx.Statement.Context.Err()
}

func (s *Service) ValidateRuntimeArchive(ctx context.Context, auth *Authentication, identity string) (*RuntimeArchiveValidation, error) {
	if !runtimeArchiveID.MatchString(identity) {
		return nil, apperrors.ErrBadRequest
	}
	if auth == nil {
		return nil, apperrors.ErrUnauthorized
	}
	if ctx == nil || s == nil || s.db == nil {
		return nil, runtimeUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var result *RuntimeArchiveValidation
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := s.runtimeArchiveValidationActor(tx, auth); err != nil {
			return err
		}
		var header entity.RuntimeConfigurationArchive
		if err := personalExact(tx, "id", identity).Take(&header).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return apperrors.ErrNotFound
			}
			return err
		}
		if header.ID != identity {
			return errRuntimeArchive
		}
		// The existing decoder authenticates every immutable chunk tuple and
		// complete canonical source. Detached facts never leave this transaction.
		if _, err := s.readRuntimeArchiveTx(ctx, tx, header); err != nil {
			return err
		}
		if err := s.runtimeArchiveValidationActor(tx, auth); err != nil {
			return err
		}
		result = &RuntimeArchiveValidation{Scope: "archive_integrity_only", ArchiveID: header.ID, ArchiveCreatedAt: header.CreatedAt.UTC(), CodecVersion: header.CodecVersion, ObservedAt: time.Now().UTC(), Integrity: "authenticated"}
		return ctx.Err()
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		for _, expected := range []error{apperrors.ErrBadRequest, apperrors.ErrUnauthorized, apperrors.ErrForbidden, apperrors.ErrNotFound} {
			if errors.Is(err, expected) {
				return nil, expected
			}
		}
		return nil, runtimeUnavailable
	}
	if ctx.Err() != nil {
		return nil, runtimeUnavailable
	}
	return result, nil
}
