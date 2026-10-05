package service

import (
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Called only after actual sign-in creates a Session in its existing locked User
// transaction. Generic Session creation and security replacements never call it.
func recordSuccessfulLogin(tx *gorm.DB, user *entity.User) error {
	if user == nil || !safeTeamSessionID(user.ID) || user.Disabled || user.OffboardedAt != nil {
		return apperrors.ErrUnauthorized
	}
	next, err := nextRecentLogin(user.LastLoginAt, time.Now().UTC())
	if err != nil {
		return err
	}
	if user.LastLoginAt != nil && user.LastLoginAt.Equal(next) {
		return nil
	}
	return persistRecentLogin(tx, user, next)
}

func persistRecentLogin(tx *gorm.DB, user *entity.User, next time.Time) error {
	write := modelCreationDB(tx).Model(&entity.User{}).Where(database.ExactText(tx, clause.Column{Name: "id"}, user.ID)).UpdateColumn("LastLoginAt", next)
	if write.Error != nil {
		return write.Error
	}
	if write.RowsAffected != 1 {
		// MySQL reports changed rows, not matched rows. Prove the exact locked row
		// already has this value; never manufacture a write to obtain rowcount1.
		var saved entity.User
		if write.RowsAffected != 0 {
			return apperrors.ErrInternal
		}
		if err := modelCreationDB(tx).Select("ID", "LastLoginAt").Where(database.ExactText(tx, clause.Column{Name: "id"}, user.ID)).Take(&saved).Error; err != nil {
			return err
		}
		if saved.ID != user.ID || saved.LastLoginAt == nil || !saved.LastLoginAt.Equal(next) {
			return apperrors.ErrInternal
		}
	}
	user.LastLoginAt = &next
	return nil
}
func nextRecentLogin(previous *time.Time, now time.Time) (time.Time, error) {
	if now.IsZero() {
		return time.Time{}, apperrors.ErrInternal
	}
	next := now.UTC().Truncate(time.Microsecond)
	if _, err := memberRecentLoginProjection(&next); err != nil {
		return time.Time{}, err
	}
	if previous != nil {
		if _, err := memberRecentLoginProjection(previous); err != nil {
			return time.Time{}, err
		}
		if previous.After(next) {
			next = previous.UTC()
		}
	}
	return next, nil
}
func memberRecentLoginProjection(value *time.Time) (*time.Time, error) {
	if value == nil {
		return nil, nil
	}
	if value.IsZero() || value.UTC().Year() < 1 || value.UTC().Year() > 9999 || value.Nanosecond()%1000 != 0 {
		return nil, apperrors.ErrInternal
	}
	utc := value.UTC()
	return &utc, nil
}
