package service

import (
	"context"
	"errors"
	"strconv"
	"time"

	"database/sql"
	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/pricing"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const teamMemberQuotaWarningGeneration = "team-member-monthly-80-90-v1"

func monthlyTeamMemberQuotaWarnings(current *teamLimitContext, usage *eventqueue.AccountQuotaUsage) []entity.TeamMemberQuotaWarningObservation {
	if current == nil || current.Member == nil || current.Actor.ID != current.Member.UserID || current.Actor.CreatedAt.IsZero() || !safeTeamSessionID(current.Team.ID) || !safeTeamSessionID(current.Actor.ID) || current.Member.TeamID != current.Team.ID || !safeTeamSessionID(current.Member.ID) || current.Member.Status != entity.ResourceActive || current.Member.Role != entity.TeamMember && current.Member.Role != entity.TeamOwner || current.Team.Status != entity.ResourceActive || current.Actor.Disabled || current.Actor.OffboardedAt != nil || !validCatalogLabel(current.Team.Name) {
		return nil
	}
	row, created, currency := current.Row, current.Team.CreatedAt, current.Pricing.PlatformCurrency
	if row.ScopeKind != "team_member" || row.ScopeID != teamMemberLimitScopeID(current.Team.ID, current.Actor.ID) || !validMonthlyQuotaFacts(row, created, usage, 52) || current.Actor.CreatedAt.After(usage.AsOf) {
		return nil
	}
	policy, err := policyFromRow(row)
	if err != nil || validateTeamLimitPolicy("team_member", policy) != nil {
		return nil
	}
	location, err := time.LoadLocation(usage.TimeZone)
	if err != nil {
		return nil
	}
	local := usage.AsOf.In(location)
	start := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, location)
	end := start.AddDate(0, 1, 0)
	coveredFrom := start
	if created.After(coveredFrom) {
		coveredFrom = created
	}
	if coveredFrom.Before(usage.CoverageStart) {
		return nil
	}
	base := entity.TeamMemberQuotaWarningObservation{ScopeID: row.ScopeID, TeamID: current.Team.ID, MemberUserID: current.Actor.ID, TeamName: current.Team.Name, UserCreatedAt: current.Actor.CreatedAt.UTC().Truncate(time.Microsecond), PolicyRevision: row.ETag, MonthStart: start.UTC(), MonthEnd: end.UTC(), TimeZone: usage.TimeZone, AsOf: usage.AsOf.UTC().Truncate(time.Microsecond), CoverageStart: usage.CoverageStart.UTC().Truncate(time.Microsecond), ResourceCreatedAt: created.UTC().Truncate(time.Microsecond), ThresholdGeneration: teamMemberQuotaWarningGeneration}
	result := []entity.TeamMemberQuotaWarningObservation{}
	add := func(dimension, cap, settled, denomination string) {
		level, threshold := quotaWarningLevel(settled, cap)
		if level == "" {
			return
		}
		value := base
		value.Dimension = dimension
		value.Limit = cap
		value.Settled = settled
		value.Currency = denomination
		value.Level = level
		value.Threshold = threshold
		result = append(result, value)
	}
	if policy.TokensMonth != nil && *policy.TokensMonth > 0 && usage.Month.TokensUnknown == 0 && usage.Month.TokensUsed >= 0 {
		add("tokens", strconv.FormatInt(*policy.TokensMonth, 10), strconv.FormatInt(usage.Month.TokensUsed, 10), "")
	}
	if policy.MoneyMonth != nil && pricing.Currency(currency) && policy.Currency == currency && usage.Month.MoneyUnknown == 0 {
		amount := "0"
		for code, value := range usage.Month.MoneyUsed {
			if code != currency {
				return result
			}
			amount = value
		}
		add("money", *policy.MoneyMonth, amount, currency)
	}
	return result
}

func validTeamMemberQuotaWarningObservation(v entity.TeamMemberQuotaWarningObservation) bool {
	level, threshold := quotaWarningLevel(v.Settled, v.Limit)
	return safeTeamSessionID(v.TeamID) && safeTeamSessionID(v.MemberUserID) && v.ScopeID == teamMemberLimitScopeID(v.TeamID, v.MemberUserID) && !v.ResourceCreatedAt.IsZero() && !v.UserCreatedAt.IsZero() && !v.AsOf.IsZero() && !v.ResourceCreatedAt.After(v.AsOf) && !v.UserCreatedAt.After(v.AsOf) && !v.CoverageStart.After(v.AsOf) && !v.AsOf.Before(v.MonthStart) && v.AsOf.Before(v.MonthEnd) && v.ThresholdGeneration == teamMemberQuotaWarningGeneration && (v.Dimension == "tokens" && v.Currency == "" || v.Dimension == "money" && pricing.Currency(v.Currency)) && level != "" && v.Level == level && v.Threshold == threshold
}

func sameTeamMemberQuotaWarningIdentity(a, b entity.TeamMemberQuotaWarningObservation) bool {
	return a.ScopeID == b.ScopeID && a.TeamID == b.TeamID && a.MemberUserID == b.MemberUserID && a.UserCreatedAt.Equal(b.UserCreatedAt) && a.Dimension == b.Dimension && a.MonthStart.Equal(b.MonthStart) && a.PolicyRevision == b.PolicyRevision && a.Currency == b.Currency && a.Level == b.Level && a.Threshold == b.Threshold && a.ThresholdGeneration == b.ThresholdGeneration && a.ResourceCreatedAt.Equal(b.ResourceCreatedAt)
}
func teamMemberQuotaWarningIdentityQuery(tx *gorm.DB, v entity.TeamMemberQuotaWarningObservation) *gorm.DB {
	return tx.Model(&entity.TeamMemberQuotaWarningObservation{}).
		Where(database.ExactText(tx, clause.Column{Name: "scope_id"}, v.ScopeID)).
		Where(database.ExactText(tx, clause.Column{Name: "team_id"}, v.TeamID)).
		Where(database.ExactText(tx, clause.Column{Name: "member_user_id"}, v.MemberUserID)).
		Where("resource_created_at = ? AND user_created_at = ?", v.ResourceCreatedAt, v.UserCreatedAt).
		Where(database.ExactText(tx, clause.Column{Name: "dimension"}, v.Dimension)).
		Where("month_start = ?", v.MonthStart).
		Where(database.ExactText(tx, clause.Column{Name: "policy_revision"}, v.PolicyRevision)).
		Where(database.ExactText(tx, clause.Column{Name: "currency"}, v.Currency)).
		Where(database.ExactText(tx, clause.Column{Name: "level"}, v.Level)).
		Where(database.ExactText(tx, clause.Column{Name: "threshold_generation"}, v.ThresholdGeneration))
}
func persistTeamMemberQuotaWarning(tx *gorm.DB, observation entity.TeamMemberQuotaWarningObservation) error {
	if !validTeamMemberQuotaWarningObservation(observation) {
		return errQuotaNotificationIdentity
	}
	var existing entity.TeamMemberQuotaWarningObservation
	err := teamMemberQuotaWarningIdentityQuery(tx, observation).Take(&existing).Error
	if err == nil {
		if !sameTeamMemberQuotaWarningIdentity(existing, observation) {
			return errQuotaNotificationIdentity
		}
		return nil // Original self inbox, immutable snapshot and read state never change.
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	observation.ID, err = id.NewPrefixed("mwo")
	if err != nil {
		return err
	}
	if err = tx.Create(&observation).Error; err != nil {
		return err
	}
	inboxID, err := id.NewPrefixed("mwi")
	if err != nil {
		return err
	}
	return tx.Create(&entity.TeamMemberQuotaWarningInbox{ID: inboxID, ObservationID: observation.ID, RecipientID: observation.MemberUserID, RecipientCreatedAt: observation.UserCreatedAt, CreatedAt: observation.AsOf}).Error
}

func (s *Service) teamMemberQuotaWarningApplied(ctx context.Context, auth *runtimeAuthorization, current *teamLimitContext, calendar entity.QuotaSetting, applications map[string]entity.RegistrationApprovalApplication) bool {
	if ctx.Err() != nil || current == nil || current.Actor.CreatedAt.IsZero() || !s.registrationAdvisoryPublished(auth, current.Actor, applications) || auth.Quota == nil || !calendar.AccountingStarted || auth.Quota.Setting.AccountingStarted != calendar.AccountingStarted || !s.teamMemberQuotaNotificationApplied(current, calendar) {
		return false
	}
	return ctx.Err() == nil && s.runtime.auth.Load() == auth && s.registrationAdvisoryPublished(auth, current.Actor, applications) && s.teamMemberQuotaNotificationApplied(current, calendar)
}
func (s *Service) observeMonthlyTeamMemberQuotaWarning(ctx context.Context, target teamMemberQuotaTarget) error {
	if !validTeamMemberQuotaTarget(target) || s.recorder == nil || s.runtime == nil {
		return nil
	}
	s.limitMu.RLock()
	defer s.limitMu.RUnlock()
	return s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		current, err := loadTeamMemberQuotaContext(tx, target)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if current.Row.TokensMonth == nil && current.Row.MoneyMonth == nil {
			return nil
		}
		applications, err := loadRegistrationApplications(tx, []entity.User{current.Actor})
		if err != nil {
			return err
		}
		var calendar entity.QuotaSetting
		if err = tx.Session(&gorm.Session{NewDB: true}).First(&calendar, 1).Error; err != nil {
			return err
		}
		auth := s.runtime.auth.Load()
		if !s.teamMemberQuotaWarningApplied(ctx, auth, current, calendar, applications) {
			return nil
		}
		status, err := s.recorder.queue.QuotaStatus()
		if err != nil {
			return runtimeUnavailable
		}
		if !status.Active {
			return nil
		}
		usage, err := s.recorder.queue.AccountQuotaUsage(teamMemberLimitAccount(target.TeamID, target.UserID), time.Now())
		if err != nil {
			return runtimeUnavailable
		}
		if usage.TimeZone != calendar.TimeZone || status.TimeZone != usage.TimeZone || status.CoverageStart == nil || !status.CoverageStart.Equal(usage.CoverageStart) {
			return nil
		}
		for _, observation := range monthlyTeamMemberQuotaWarnings(current, usage) {
			if err = persistTeamMemberQuotaWarning(tx, observation); err != nil {
				return err
			}
		}
		if !s.teamMemberQuotaWarningApplied(ctx, auth, current, calendar, applications) {
			return runtimeUnavailable
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
}
