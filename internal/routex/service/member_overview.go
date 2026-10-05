package service

import (
	"context"
	"database/sql"
	"strconv"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/pricing"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type MemberOverviewRecord struct {
	UserID            string                       `json:"user_id"`
	ObservedAt        time.Time                    `json:"observed_at"`
	PlatformCurrency  string                       `json:"platform_currency"`
	Personal          MemberOverviewMonthlyAccount `json:"personal"`
	TotalPersonalKeys string                       `json:"total_personal_keys"`
}

func memberOverviewSubjectQuery(tx *gorm.DB, userID string) *gorm.DB {
	return tx.Session(&gorm.Session{}).Model(&entity.User{}).
		Select("ID", "Disabled", "OffboardedAt", "CreatedAt", "ApprovalApplicationID").
		Where(database.ExactText(tx, clause.Column{Name: "id"}, userID))
}

func memberOverviewKeyCountQuery(tx *gorm.DB, userID string) *gorm.DB {
	// Retained Personal Keys are counted across every status. No Key identity or
	// secret is projected, and Project Keys belong to a separate table.
	return tx.Session(&gorm.Session{}).Model(&entity.APIKey{}).
		Where(database.ExactText(tx, clause.Column{Name: "user_id"}, userID))
}

func (s *Service) memberOverviewSubjectApplied(auth *runtimeAuthorization, subject entity.User, target overviewAccountTarget, setting entity.QuotaSetting, currency string, applications map[string]entity.RegistrationApprovalApplication) bool {
	if auth == nil || subject.Disabled || subject.OffboardedAt != nil || subject.ID != target.id || !subject.CreatedAt.Equal(target.created) || subject.CreatedAt.IsZero() {
		return false
	}
	proof, exists := auth.UserProofs[subject.ID]
	return exists && s.registrationAdvisoryPublished(auth, subject, applications) && proof.Enabled && proof.CreatedAt.Equal(subject.CreatedAt) && s.memberOverviewApplied(auth, subject.ID, target, []overviewAccountTarget{target}, setting, currency)
}

// MemberOverview reads an independently authorized retained subject. Saved
// policy and journal facts never impersonate that subject or expose their Keys.
func (s *Service) MemberOverview(ctx context.Context, actorID, userID string) (*MemberOverviewRecord, error) {
	if !safeTeamSessionID(actorID) {
		return nil, apperrors.ErrUnauthorized
	}
	if !safeTeamSessionID(userID) {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result := &MemberOverviewRecord{UserID: userID}
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, err := exactEnabledActor(tx, actorID)
		if err != nil {
			return err
		}
		allowed, err := exactGovernancePermissionForAdmittedActor(tx.Session(&gorm.Session{NewDB: true}), actor, "members.read")
		if err != nil {
			return err
		}
		if !allowed {
			return apperrors.ErrForbidden
		}
		var subject entity.User
		if err := memberOverviewSubjectQuery(tx, userID).First(&subject).Error; err != nil {
			return err
		}
		if subject.ID != userID {
			return apperrors.ErrNotFound
		}
		if subject.CreatedAt.IsZero() {
			return apperrors.ErrInternal
		}
		applications, err := loadRegistrationApplications(tx.Session(&gorm.Session{}), []entity.User{subject})
		if err != nil {
			return err
		}
		result.ObservedAt = time.Now().UTC()
		row, policy, err := readDefaultResourceLimitPolicy(tx, "user", userID)
		if err != nil {
			return err
		}
		if row.ETag == "" {
			return apperrors.ErrInternal
		}
		target := overviewAccountTarget{kind: "user", id: userID, created: subject.CreatedAt, row: row, policy: policy}
		var priceSetting entity.PricingSetting
		if err := tx.Select("platform_currency").Take(&priceSetting, 1).Error; err != nil {
			return err
		}
		if !pricing.Currency(priceSetting.PlatformCurrency) {
			return apperrors.ErrInternal
		}
		result.PlatformCurrency = priceSetting.PlatformCurrency
		var setting entity.QuotaSetting
		if err := memberOverviewCalendarQuery(tx).Take(&setting, 1).Error; err != nil {
			return err
		}
		var count int64
		if err := memberOverviewKeyCountQuery(tx, userID).Count(&count).Error; err != nil {
			return err
		}
		if count < 0 {
			return apperrors.ErrInternal
		}
		result.TotalPersonalKeys = strconv.FormatInt(count, 10)
		var auth *runtimeAuthorization
		if s.runtime != nil {
			auth = s.runtime.auth.Load()
		}
		var batch *eventqueue.QuotaUsageBatch
		if s.recorder != nil {
			// One coherent journal account read. An outage preserves known SQL facts
			// while leaving usage and live reservations unavailable, never zero.
			batch, _ = s.recorder.queue.AccountQuotaUsageBatch([]string{limitAccount("user", userID)}, result.ObservedAt)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		result.Personal = s.memberOverviewMonthlyAccount(target, []overviewAccountTarget{target}, batch, auth, userID, setting, result.PlatformCurrency)
		result.Personal.RuntimeApplied = result.Personal.RuntimeApplied && s.memberOverviewSubjectApplied(auth, subject, target, setting, result.PlatformCurrency, applications)
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, catalogError(err)
	}
	return result, nil
}
