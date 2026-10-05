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

const projectQuotaWarningGeneration = "project-monthly-80-90-v1"

func monthlyProjectQuotaWarnings(row entity.ResourceLimit, created time.Time, usage *eventqueue.AccountQuotaUsage, currency string) []entity.ProjectQuotaWarningObservation {
	if row.ScopeKind != "project" || !validMonthlyQuotaFacts(row, created, usage, 30) {
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
	base := entity.ProjectQuotaWarningObservation{ProjectID: row.ScopeID, PolicyRevision: row.ETag, MonthStart: start.UTC(), MonthEnd: end.UTC(), TimeZone: usage.TimeZone, AsOf: usage.AsOf.UTC().Truncate(time.Microsecond), CoverageStart: usage.CoverageStart.UTC().Truncate(time.Microsecond), ResourceCreatedAt: created.UTC().Truncate(time.Microsecond), ThresholdGeneration: projectQuotaWarningGeneration}
	result := []entity.ProjectQuotaWarningObservation{}
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

func sameProjectQuotaWarningIdentity(a, b entity.ProjectQuotaWarningObservation) bool {
	return a.ProjectID == b.ProjectID && a.Dimension == b.Dimension && a.MonthStart.Equal(b.MonthStart) && a.PolicyRevision == b.PolicyRevision && a.Currency == b.Currency && a.Level == b.Level && a.Threshold == b.Threshold && a.ThresholdGeneration == b.ThresholdGeneration && a.ResourceCreatedAt.Equal(b.ResourceCreatedAt)
}
func projectQuotaWarningIdentityQuery(tx *gorm.DB, observation entity.ProjectQuotaWarningObservation) *gorm.DB {
	return tx.Model(&entity.ProjectQuotaWarningObservation{}).
		Where(database.ExactText(tx, clause.Column{Name: "project_id"}, observation.ProjectID)).
		Where(database.ExactText(tx, clause.Column{Name: "dimension"}, observation.Dimension)).
		Where("month_start = ?", observation.MonthStart).
		Where("resource_created_at = ?", observation.ResourceCreatedAt).
		Where(database.ExactText(tx, clause.Column{Name: "policy_revision"}, observation.PolicyRevision)).
		Where(database.ExactText(tx, clause.Column{Name: "currency"}, observation.Currency)).
		Where(database.ExactText(tx, clause.Column{Name: "level"}, observation.Level)).
		Where(database.ExactText(tx, clause.Column{Name: "threshold_generation"}, observation.ThresholdGeneration))
}
func persistProjectQuotaWarning(tx *gorm.DB, observation entity.ProjectQuotaWarningObservation, recipients []projectQuotaWarningRecipient) error {
	var existing entity.ProjectQuotaWarningObservation
	err := projectQuotaWarningIdentityQuery(tx, observation).Take(&existing).Error
	if err == nil {
		if !sameProjectQuotaWarningIdentity(existing, observation) {
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
	observation.ID, err = id.NewPrefixed("pwo")
	if err != nil {
		return err
	}
	if err = tx.Create(&observation).Error; err != nil {
		return err
	}
	for _, recipient := range recipients {
		inboxID, err := id.NewPrefixed("pwi")
		if err != nil {
			return err
		}
		if err = tx.Create(&entity.ProjectQuotaWarningInbox{ID: inboxID, ObservationID: observation.ID, RecipientID: recipient.ID, RecipientCreatedAt: recipient.CreatedAt.UTC().Truncate(time.Microsecond), CreatedAt: observation.AsOf}).Error; err != nil {
			return err
		}
	}
	return nil
}

type projectQuotaWarningManagerIdentity struct {
	ManagerID, ManagerUserID, ManagerProjectID, UserID, ProjectID, ProjectStatus string
	Disabled, Offboarded                                                         bool
}

func validProjectQuotaWarningManager(row projectQuotaWarningManagerIdentity, projectID string) bool {
	return safeTeamSessionID(row.ManagerID) && safeTeamSessionID(row.UserID) && safeTeamSessionID(projectID) && validQuotaManager(quotaManagerIdentity{ManagerUserID: row.ManagerUserID, ManagerProjectID: row.ManagerProjectID, UserID: row.UserID, ProjectID: row.ProjectID, ProjectStatus: row.ProjectStatus, Disabled: row.Disabled, Offboarded: row.Offboarded}, "", projectID)
}

func projectQuotaWarningManagersQuery(tx *gorm.DB, projectID string) *gorm.DB {
	return quotaManagerQuery(tx).Select("manager.id AS manager_id, manager.user_id AS manager_user_id, manager.project_id AS manager_project_id, actor.id AS user_id, project.id AS project_id, project.status AS project_status, actor.disabled, actor.offboarded_at IS NOT NULL AS offboarded").Where(database.ExactText(tx, clause.Column{Table: "project", Name: "id"}, projectID)).Limit(quotaInboxManagerLimit + 1)
}

type projectQuotaWarningRecipient struct {
	ID        string
	CreatedAt time.Time
	ManagerID string
}

// IDs are supplied only by the existing bounded current Project management scope.
// Complete admission facts and birth are loaded in the same governance-locked
// transaction; this is not a global directory or an administrator recipient set.
func (s *Service) projectQuotaWarningRecipients(tx *gorm.DB, auth *runtimeAuthorization, projectID string) ([]projectQuotaWarningRecipient, error) {
	var members []projectQuotaWarningManagerIdentity
	err := projectQuotaWarningManagersQuery(tx, projectID).Scan(&members).Error
	if err != nil {
		return nil, err
	}
	if len(members) > quotaInboxManagerLimit {
		return nil, errQuotaNotificationIdentity
	}
	ids := []string{}
	managerIDs := map[string]string{}
	for _, member := range members {
		if !validProjectQuotaWarningManager(member, projectID) || managerIDs[member.UserID] != "" {
			return nil, errQuotaNotificationIdentity
		}
		ids = append(ids, member.UserID)
		managerIDs[member.UserID] = member.ManagerID
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
	recipients := []projectQuotaWarningRecipient{}
	for _, user := range users {
		if !allowed[user.ID] || seen[user.ID] || user.CreatedAt.IsZero() {
			return nil, errQuotaNotificationIdentity
		}
		seen[user.ID] = true
		if s.projectQuotaWarningRecipientPublished(auth, user, applications, projectID, managerIDs[user.ID]) {
			recipients = append(recipients, projectQuotaWarningRecipient{ID: user.ID, CreatedAt: user.CreatedAt, ManagerID: managerIDs[user.ID]})
		}
	}
	return recipients, nil
}
func (s *Service) projectQuotaWarningRecipientPublished(auth *runtimeAuthorization, user entity.User, applications map[string]entity.RegistrationApprovalApplication, projectID, managerID string) bool {
	if !safeTeamSessionID(projectID) || !safeTeamSessionID(managerID) || !s.registrationAdvisoryPublished(auth, user, applications) {
		return false
	}
	state, exists := auth.ProjectCreationStates[projectID]
	return exists && state.Project.ID == projectID && state.Project.Status == entity.ResourceActive && !state.Project.CreatedAt.IsZero() && state.Managers[user.ID] == managerID && state.EnabledManagers[user.ID] && !runtimeDenied(&s.runtime.deniedProjects, projectID)
}
func (s *Service) projectQuotaWarningApplied(ctx context.Context, row entity.ResourceLimit, project entity.Project, policy limits.Policy, calendar entity.QuotaSetting, currency string) bool {
	if ctx.Err() != nil || s.runtime == nil || row.ScopeKind != "project" || row.ScopeID != project.ID || project.Status != entity.ResourceActive || project.CreatedAt.IsZero() || !validCatalogLabel(project.Name) {
		return false
	}
	auth := s.runtime.auth.Load()
	if auth == nil {
		return false
	}
	state, exists := auth.ProjectCreationStates[project.ID]
	return exists && state.Project.ID == project.ID && state.Project.Status == entity.ResourceActive && state.Project.CreatedAt.Equal(project.CreatedAt) && auth.Quota != nil && calendar.ETag != "" && calendar.AccountingStarted && auth.Quota.Setting.ETag == calendar.ETag && auth.Quota.Setting.AccountingStarted == calendar.AccountingStarted && s.quotaNotificationResourceApplied(row, project.CreatedAt, policy, calendar.TimeZone, currency) && ctx.Err() == nil && s.runtime.auth.Load() == auth && !runtimeDenied(&s.runtime.deniedProjects, project.ID) && !runtimeDenied(&s.runtime.deniedLimits, limitAccount("project", project.ID)) && !runtimeDenied(&s.runtime.deniedLimits, "quota_settings") && time.Now().Before(auth.ValidUntil)
}
func (s *Service) observeMonthlyProjectQuotaWarning(ctx context.Context, kind, projectID string) error {
	if kind != "project" || !safeTeamSessionID(projectID) || s.recorder == nil || s.runtime == nil {
		return nil
	}
	s.limitMu.RLock()
	defer s.limitMu.RUnlock()
	return s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		var project entity.Project
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(database.ExactText(tx, clause.Column{Name: "id"}, projectID)).Take(&project).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if project.ID != projectID || project.Status != entity.ResourceActive || project.CreatedAt.IsZero() || !validCatalogLabel(project.Name) {
			return nil
		}
		var row entity.ResourceLimit
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(database.ExactText(tx, clause.Column{Name: "scope_kind"}, "project")).Where(database.ExactText(tx, clause.Column{Name: "scope_id"}, projectID)).Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if row.ScopeKind != "project" || row.ScopeID != projectID {
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
		if !s.projectQuotaWarningApplied(ctx, row, project, policy, calendar, pricingSetting.PlatformCurrency) {
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
		usage, err := s.recorder.queue.AccountQuotaUsage(limitAccount("project", projectID), time.Now())
		if err != nil {
			return runtimeUnavailable
		}
		if usage.TimeZone != calendar.TimeZone || status.TimeZone != usage.TimeZone || status.CoverageStart == nil || !status.CoverageStart.Equal(usage.CoverageStart) {
			return nil
		}
		observations := monthlyProjectQuotaWarnings(row, project.CreatedAt, usage, pricingSetting.PlatformCurrency)
		if len(observations) == 0 {
			return nil
		}
		recipients, err := s.projectQuotaWarningRecipients(tx, auth, projectID)
		if err != nil {
			return err
		}
		for _, observation := range observations {
			observation.ProjectName = project.Name
			if err = persistProjectQuotaWarning(tx, observation, recipients); err != nil {
				return err
			}
		}
		if s.runtime.auth.Load() != auth || !s.projectQuotaWarningApplied(ctx, row, project, policy, calendar, pricingSetting.PlatformCurrency) {
			return runtimeUnavailable
		}
		// Recipient admission publication is also fenced after the atomic writes.
		for _, recipient := range recipients {
			if runtimeDenied(&s.runtime.deniedUsers, recipient.ID) || runtimeDenied(&s.runtime.deniedSessionUsers, recipient.ID) || runtimeDenied(&s.runtime.deniedProjects, projectID) {
				return runtimeUnavailable
			}
		}
		return nil
	})
}
