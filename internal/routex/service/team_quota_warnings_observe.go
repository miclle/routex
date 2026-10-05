package service

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/limits"
	"github.com/miclle/routex/pkg/pricing"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const teamQuotaWarningGeneration = "team-monthly-80-90-v1"

func monthlyTeamQuotaWarnings(row entity.ResourceLimit, created time.Time, usage *eventqueue.AccountQuotaUsage, currency string) []entity.TeamQuotaWarningObservation {
	if row.ScopeKind != "team" || !validMonthlyQuotaFacts(row, created, usage, 30) {
		return nil
	}
	policy, err := policyFromRow(row)
	if err != nil {
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
	base := entity.TeamQuotaWarningObservation{TeamID: row.ScopeID, PolicyRevision: row.ETag, MonthStart: start.UTC(), MonthEnd: end.UTC(), TimeZone: usage.TimeZone, AsOf: usage.AsOf.UTC().Truncate(time.Microsecond), CoverageStart: usage.CoverageStart.UTC().Truncate(time.Microsecond), ResourceCreatedAt: created.UTC().Truncate(time.Microsecond), ThresholdGeneration: teamQuotaWarningGeneration}
	result := []entity.TeamQuotaWarningObservation{}
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

func sameTeamQuotaWarningIdentity(a, b entity.TeamQuotaWarningObservation) bool {
	return a.TeamID == b.TeamID && a.Dimension == b.Dimension && a.MonthStart.Equal(b.MonthStart) && a.PolicyRevision == b.PolicyRevision && a.Currency == b.Currency && a.Level == b.Level && a.Threshold == b.Threshold && a.ThresholdGeneration == b.ThresholdGeneration && a.ResourceCreatedAt.Equal(b.ResourceCreatedAt)
}
func teamQuotaWarningIdentityQuery(tx *gorm.DB, observation entity.TeamQuotaWarningObservation) *gorm.DB {
	return tx.Model(&entity.TeamQuotaWarningObservation{}).
		Where(database.ExactText(tx, clause.Column{Name: "team_id"}, observation.TeamID)).
		Where(database.ExactText(tx, clause.Column{Name: "dimension"}, observation.Dimension)).
		Where("month_start = ?", observation.MonthStart).
		Where("resource_created_at = ?", observation.ResourceCreatedAt).
		Where(database.ExactText(tx, clause.Column{Name: "policy_revision"}, observation.PolicyRevision)).
		Where(database.ExactText(tx, clause.Column{Name: "currency"}, observation.Currency)).
		Where(database.ExactText(tx, clause.Column{Name: "level"}, observation.Level)).
		Where(database.ExactText(tx, clause.Column{Name: "threshold_generation"}, observation.ThresholdGeneration))
}
func persistTeamQuotaWarning(tx *gorm.DB, observation entity.TeamQuotaWarningObservation, recipients []teamQuotaWarningRecipient) error {
	var existing entity.TeamQuotaWarningObservation
	err := teamQuotaWarningIdentityQuery(tx, observation).Take(&existing).Error
	if err == nil {
		if !sameTeamQuotaWarningIdentity(existing, observation) {
			return errQuotaNotificationIdentity
		}
		return nil // Original snapshot, recipient births and read state never expand or change.
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if len(recipients) == 0 {
		return nil
	}
	seen := map[string]bool{}
	for _, recipient := range recipients {
		if !safeTeamSessionID(recipient.ID) || recipient.CreatedAt.IsZero() || recipient.CreatedAt.After(observation.AsOf) || seen[recipient.ID] {
			return errQuotaNotificationIdentity
		}
		seen[recipient.ID] = true
	}
	observation.ID, err = id.NewPrefixed("two")
	if err != nil {
		return err
	}
	if err = tx.Create(&observation).Error; err != nil {
		return err
	}
	for _, recipient := range recipients {
		inboxID, err := id.NewPrefixed("twi")
		if err != nil {
			return err
		}
		if err = tx.Create(&entity.TeamQuotaWarningInbox{ID: inboxID, ObservationID: observation.ID, RecipientID: recipient.ID, RecipientCreatedAt: recipient.CreatedAt.UTC().Truncate(time.Microsecond), CreatedAt: observation.AsOf}).Error; err != nil {
			return err
		}
	}
	return nil
}

type teamQuotaWarningRecipient struct {
	ID           string
	CreatedAt    time.Time
	MembershipID string
}

// IDs are supplied only by the existing bounded current Team membership scope.
// Complete admission facts and birth are loaded in the same governance-locked
// transaction; this is not a global directory or an administrator recipient set.
func (s *Service) teamQuotaWarningRecipients(tx *gorm.DB, auth *runtimeAuthorization, teamID string) ([]teamQuotaWarningRecipient, error) {
	var members []quotaTeamIdentity
	err := quotaTeamQuery(tx).Where(database.ExactText(tx, clause.Column{Table: "team", Name: "id"}, teamID)).Limit(quotaInboxManagerLimit + 1).Scan(&members).Error
	if err != nil {
		return nil, err
	}
	if len(members) > quotaInboxManagerLimit {
		return nil, errQuotaNotificationIdentity
	}
	ids := []string{}
	membershipIDs := map[string]string{}
	for _, member := range members {
		if !validQuotaTeamMember(member, "", teamID) || membershipIDs[member.UserID] != "" {
			return nil, errQuotaNotificationIdentity
		}
		ids = append(ids, member.UserID)
		membershipIDs[member.UserID] = member.MembershipID
	}
	if len(ids) == 0 {
		return nil, nil
	}
	var users []entity.User
	if err = tx.Where("id IN ?", ids).Limit(quotaInboxManagerLimit + 1).Find(&users).Error; err != nil {
		return nil, err
	}
	if len(users) != len(ids) {
		return nil, errQuotaNotificationIdentity
	}
	applications, err := loadRegistrationApplications(tx, users)
	if err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	for _, id := range ids {
		allowed[id] = true
	}
	seen := map[string]bool{}
	recipients := []teamQuotaWarningRecipient{}
	for _, user := range users {
		if !allowed[user.ID] || seen[user.ID] || user.CreatedAt.IsZero() {
			return nil, errQuotaNotificationIdentity
		}
		seen[user.ID] = true
		if s.teamQuotaWarningRecipientPublished(auth, user, applications, teamID, membershipIDs[user.ID]) {
			recipients = append(recipients, teamQuotaWarningRecipient{ID: user.ID, CreatedAt: user.CreatedAt, MembershipID: membershipIDs[user.ID]})
		}
	}
	return recipients, nil
}
func (s *Service) teamQuotaWarningRecipientPublished(auth *runtimeAuthorization, user entity.User, applications map[string]entity.RegistrationApprovalApplication, teamID, membershipID string) bool {
	return safeTeamSessionID(membershipID) && s.registrationAdvisoryPublished(auth, user, applications) && auth.Teams[teamID].Members[user.ID] == membershipID && !runtimeDenied(&s.runtime.deniedTeamMembers, teamMemberRuntimeKey(teamID, user.ID))
}
func (s *Service) teamQuotaWarningApplied(ctx context.Context, row entity.ResourceLimit, team entity.Team, policy limits.Policy, calendar entity.QuotaSetting, currency string) bool {
	if ctx.Err() != nil || s.runtime == nil || row.ScopeKind != "team" || row.ScopeID != team.ID || team.Status != entity.ResourceActive || team.CreatedAt.IsZero() {
		return false
	}
	auth := s.runtime.auth.Load()
	return auth != nil && auth.Quota != nil && auth.Quota.Setting.ETag == calendar.ETag && auth.Quota.Setting.AccountingStarted == calendar.AccountingStarted && s.quotaNotificationResourceApplied(row, team.CreatedAt, policy, calendar.TimeZone, currency) && ctx.Err() == nil && s.runtime.auth.Load() == auth && !runtimeDenied(&s.runtime.deniedTeams, team.ID) && !runtimeDenied(&s.runtime.deniedLimits, limitAccount("team", team.ID)) && !runtimeDenied(&s.runtime.deniedLimits, "quota_settings") && time.Now().Before(auth.ValidUntil)
}
func (s *Service) observeMonthlyTeamQuotaWarning(ctx context.Context, kind, teamID string) error {
	if kind != "team" || !safeTeamSessionID(teamID) || s.recorder == nil || s.runtime == nil {
		return nil
	}
	s.limitMu.RLock()
	defer s.limitMu.RUnlock()
	return s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		var team entity.Team
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(database.ExactText(tx, clause.Column{Name: "id"}, teamID)).Take(&team).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if team.ID != teamID || team.Status != entity.ResourceActive || team.CreatedAt.IsZero() {
			return nil
		}
		var row entity.ResourceLimit
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(database.ExactText(tx, clause.Column{Name: "scope_kind"}, "team")).Where(database.ExactText(tx, clause.Column{Name: "scope_id"}, teamID)).Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if row.ScopeKind != "team" || row.ScopeID != teamID {
			return nil
		}
		policy, err := policyFromRow(row)
		if err != nil {
			return nil
		}
		var calendar entity.QuotaSetting
		if err = tx.Take(&calendar, 1).Error; err != nil {
			return err
		}
		var pricingSetting entity.PricingSetting
		if err = tx.Take(&pricingSetting, 1).Error; err != nil {
			return err
		}
		if !s.teamQuotaWarningApplied(ctx, row, team, policy, calendar, pricingSetting.PlatformCurrency) {
			return nil
		}
		auth := s.runtime.auth.Load()
		status, err := s.recorder.queue.QuotaStatus()
		if err != nil {
			return runtimeUnavailable
		}
		if !status.Active {
			return nil
		}
		usage, err := s.recorder.queue.AccountQuotaUsage(limitAccount("team", teamID), time.Now())
		if err != nil {
			return runtimeUnavailable
		}
		if usage.TimeZone != calendar.TimeZone || status.TimeZone != usage.TimeZone || status.CoverageStart == nil || !status.CoverageStart.Equal(usage.CoverageStart) {
			return nil
		}
		observations := monthlyTeamQuotaWarnings(row, team.CreatedAt, usage, pricingSetting.PlatformCurrency)
		if len(observations) == 0 {
			return nil
		}
		recipients, err := s.teamQuotaWarningRecipients(tx, auth, teamID)
		if err != nil {
			return err
		}
		for _, observation := range observations {
			observation.TeamName = team.Name
			if err = persistTeamQuotaWarning(tx, observation, recipients); err != nil {
				return err
			}
		}
		if s.runtime.auth.Load() != auth || !s.teamQuotaWarningApplied(ctx, row, team, policy, calendar, pricingSetting.PlatformCurrency) {
			return runtimeUnavailable
		}
		// Recipient admission publication is also fenced after the atomic writes.
		for _, recipient := range recipients {
			if runtimeDenied(&s.runtime.deniedUsers, recipient.ID) || runtimeDenied(&s.runtime.deniedSessionUsers, recipient.ID) || runtimeDenied(&s.runtime.deniedTeamMembers, teamMemberRuntimeKey(teamID, recipient.ID)) {
				return runtimeUnavailable
			}
		}
		return nil
	})
}
