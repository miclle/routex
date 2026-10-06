package service

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/database"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

func (s *Service) PersonalUsage(ctx context.Context, actorID string, filter UsageFilter) (*UsageReport, error) {
	return s.queryUsage(ctx, actorID, "personal", "", filter)
}
func (s *Service) ProjectUsage(ctx context.Context, actorID, projectID string, filter UsageFilter) (*UsageReport, error) {
	return s.queryUsage(ctx, actorID, "project", projectID, filter)
}
func (s *Service) AdminUsage(ctx context.Context, actorID string, filter UsageFilter) (*UsageReport, error) {
	return s.queryUsage(ctx, actorID, "admin", "", filter)
}
func validateUsageFilter(filter UsageFilter, admin bool) error {
	if !admin && (filter.UserID != "" || filter.ProjectID != "" || filter.TeamID != "" || filter.ProviderID != "" || filter.ProviderModelID != "" || filter.ConnectionID != "") {
		return apperrors.ErrBadRequest
	}
	for _, value := range []string{filter.ModelID, filter.KeyID, filter.UserID, filter.ProjectID, filter.TeamID, filter.ProviderID, filter.ProviderModelID, filter.ConnectionID} {
		if value != "" && !safeCallID.MatchString(value) {
			return apperrors.ErrBadRequest
		}
	}
	if filter.UserID != "" && filter.ProjectID != "" {
		return apperrors.ErrBadRequest
	}
	if filter.TeamID != "" && (filter.UserID != "" || filter.ProjectID != "" || filter.KeyID != "") {
		return apperrors.ErrBadRequest
	}
	if filter.Status != "" && filter.Status != "success" && filter.Status != "error" && filter.Status != "canceled" {
		return apperrors.ErrBadRequest
	}
	if filter.Protocol != "" && !entity.SupportedNativeProtocol(filter.Protocol) {
		return apperrors.ErrBadRequest
	}
	return nil
}
func usageQueryFilters(query *gorm.DB, filter UsageFilter) *gorm.DB {
	if filter.TeamID != "" {
		query = teamUsageFacts(query, filter.TeamID)
	}
	for _, item := range []struct{ column, value string }{{"model_id", filter.ModelID}, {"key_id", filter.KeyID}, {"user_id", filter.UserID}, {"project_id", filter.ProjectID}, {"provider_id", filter.ProviderID}, {"provider_model_id", filter.ProviderModelID}, {"connection_id", filter.ConnectionID}} {
		if item.value != "" {
			query = query.Where(database.ExactText(query, clause.Column{Name: item.column}, item.value))
		}
	}
	for _, item := range []struct{ column, value string }{{"status", filter.Status}, {"protocol", filter.Protocol}} {
		if item.value != "" {
			query = query.Where(item.column+" = ?", item.value)
		}
	}
	if filter.Stream != nil {
		query = query.Where("stream = ?", *filter.Stream)
	}
	return query
}
func authorizeUsage(tx *gorm.DB, actorID, scope, projectID string) error {
	if scope == "team" {
		return authorizeTeamCalls(tx, actorID, projectID)
	}
	if scope == "admin" {
		actor, err := exactEnabledActor(tx, actorID)
		if err != nil {
			return err
		}
		allowed, err := exactGovernancePermission(tx, actor, "calls.read_all")
		if err != nil {
			return err
		}
		if !allowed {
			return apperrors.ErrForbidden
		}
		return nil
	}
	actor, err := exactEnabledActor(tx, actorID)
	if err != nil {
		return err
	}
	if scope != "project" {
		return nil
	}
	if projectID == "" || !safeCallID.MatchString(projectID) {
		return apperrors.ErrBadRequest
	}
	allowed, err := exactGovernancePermission(tx, actor, "calls.read_all")
	if err != nil {
		return err
	}
	if !allowed {
		manager, err := exactProjectRequestManager(tx, actorID, projectID)
		if err != nil {
			return err
		}
		if !manager {
			return apperrors.ErrNotFound
		}
	}
	var project entity.Project
	err = tx.Select("id").Where(database.ExactText(tx, clause.Column{Name: "id"}, projectID)).First(&project).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && project.ID != projectID {
		return apperrors.ErrNotFound
	}
	return err
}

// Historical attribution is scoped without mutable catalogue joins or collated aliases.
func usageScopeFacts(query *gorm.DB, actorID, scope, targetID string) *gorm.DB {
	switch scope {
	case "personal":
		query = query.Where(database.ExactText(query, clause.Column{Name: "user_id"}, actorID))
		return usagePersonalFacts(query)
	case "project":
		return query.Where(database.ExactText(query, clause.Column{Name: "project_id"}, targetID))
	case "team":
		return teamUsageFacts(query, targetID)
	default:
		return query
	}
}

func usagePersonalFacts(query *gorm.DB) *gorm.DB {
	return query.Where(database.ExactText(query, clause.Column{Name: "project_id"}, "")).
		Where(database.ExactText(query, clause.Column{Name: "team_id"}, ""))
}

func (s *Service) queryUsage(ctx context.Context, actorID, scope, projectID string, filter UsageFilter) (*UsageReport, error) {
	admin := scope == "admin"
	if err := validateUsageFilter(filter, admin); err != nil {
		return nil, err
	}
	if scope == "team" && filter.KeyID != "" {
		return nil, apperrors.ErrBadRequest
	}
	now := time.Now().UTC()
	plan, err := planUsage(filter, now)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows := []entity.CallRecord{}
	from := plan.current.from
	if plan.previous != nil {
		from = plan.previous.from
	}
	// A read snapshot keeps current authorization and the complete selected facts
	// together. No mutable provider/model joins can rewrite historical attribution.
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := authorizeUsage(tx, actorID, scope, projectID); err != nil {
			return err
		}
		query := tx.Model(&entity.CallRecord{}).Select(usageFactColumns(scope)).Where("started_at >= ? AND started_at < ?", from, plan.current.to)
		query = usageScopeFacts(query, actorID, scope, projectID)
		// An admin user filter selects Personal attribution, not Team actors or Project creators.
		if filter.UserID != "" {
			query = usagePersonalFacts(query)
		}
		query = usageQueryFilters(query, filter)
		if err := query.Order("started_at ASC").Order("request_id ASC").Limit(usageRowLimit + 1).Find(&rows).Error; err != nil {
			return err
		}
		teamID := filter.TeamID
		if scope == "team" {
			teamID = projectID
		}
		if teamID != "" {
			if err := validateTeamUsageFacts(rows, teamID); err != nil {
				return err
			}
		}
		if len(rows) > usageRowLimit {
			return usageTooLarge
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return nil, &apperrors.Error{Code: 503, Message: "usage query temporarily unavailable"}
		}
		return nil, catalogError(err)
	}
	current, err := aggregateUsage(rows, plan.current, plan, admin)
	if err != nil {
		return nil, err
	}
	result := &UsageReport{MemberCountBasis: usageMemberCountBasis, Timezone: plan.location.String(), Granularity: plan.grain, QueriedAt: now, Source: "persisted_call_records", MayLag: true, Current: current, AvailableDimensions: usageDimensions(scope, filter)}
	if scope == "team" {
		result.TeamID = projectID
	}
	if plan.previous != nil {
		previous, err := aggregateUsage(rows, *plan.previous, plan, admin)
		if err != nil {
			return nil, err
		}
		result.Previous = &previous
	}
	for _, row := range rows {
		if result.LatestCompletedAt == nil || row.CompletedAt.After(*result.LatestCompletedAt) {
			value := row.CompletedAt
			result.LatestCompletedAt = &value
		}
	}
	return result, nil
}
