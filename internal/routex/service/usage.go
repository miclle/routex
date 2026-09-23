package service

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"time"

	"gorm.io/gorm"

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
	if !admin && (filter.UserID != "" || filter.ProjectID != "" || filter.ProviderModelID != "" || filter.ConnectionID != "") {
		return apperrors.ErrBadRequest
	}
	for _, value := range []string{filter.ModelID, filter.KeyID, filter.UserID, filter.ProjectID, filter.ProviderModelID, filter.ConnectionID} {
		if value != "" && !safeCallID.MatchString(value) {
			return apperrors.ErrBadRequest
		}
	}
	if filter.UserID != "" && filter.ProjectID != "" {
		return apperrors.ErrBadRequest
	}
	if filter.Status != "" && filter.Status != "success" && filter.Status != "error" && filter.Status != "canceled" {
		return apperrors.ErrBadRequest
	}
	if filter.Protocol != "" && filter.Protocol != entity.ProtocolOpenAIChat {
		return apperrors.ErrBadRequest
	}
	return nil
}
func usageQueryFilters(query *gorm.DB, filter UsageFilter) *gorm.DB {
	for _, item := range []struct{ column, value string }{{"model_id", filter.ModelID}, {"key_id", filter.KeyID}, {"status", filter.Status}, {"protocol", filter.Protocol}, {"user_id", filter.UserID}, {"project_id", filter.ProjectID}, {"provider_model_id", filter.ProviderModelID}, {"connection_id", filter.ConnectionID}} {
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
	permissions, err := permissionsFor(tx, actorID)
	if err != nil {
		return err
	}
	if scope == "admin" && !slices.Contains(permissions, "calls.read_all") {
		return apperrors.ErrForbidden
	}
	if scope != "project" {
		return nil
	}
	if projectID == "" || !safeCallID.MatchString(projectID) {
		return apperrors.ErrBadRequest
	}
	if !slices.Contains(permissions, "calls.read_all") {
		manager, err := resourceManager(tx, actorID, projectID)
		if err != nil {
			return err
		}
		if !manager {
			return apperrors.ErrNotFound
		}
	}
	var count int64
	if err := tx.Model(&entity.Project{}).Where("id = ?", projectID).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return apperrors.ErrNotFound
	}
	return nil
}
func (s *Service) queryUsage(ctx context.Context, actorID, scope, projectID string, filter UsageFilter) (*UsageReport, error) {
	admin := scope == "admin"
	if err := validateUsageFilter(filter, admin); err != nil {
		return nil, err
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
		query := tx.Model(&entity.CallRecord{}).Select([]string{"request_id", "key_id", "model_id", "model_name", "provider_model_id", "connection_id", "status", "started_at", "completed_at", "duration_ms", "input_tokens", "output_tokens", "pricing_status", "charge_amount", "charge_currency"}).Where("started_at >= ? AND started_at < ?", from, plan.current.to)
		switch scope {
		case "personal":
			query = query.Where("user_id = ? AND project_id = ?", actorID, "")
		case "project":
			query = query.Where("project_id = ?", projectID)
		}
		// An admin user filter also means personal attribution, not Project creator.
		if filter.UserID != "" {
			query = query.Where("project_id = ?", "")
		}
		query = usageQueryFilters(query, filter)
		if err := query.Order("started_at ASC").Order("request_id ASC").Limit(usageRowLimit + 1).Find(&rows).Error; err != nil {
			return err
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
	result := &UsageReport{Timezone: plan.location.String(), Granularity: plan.grain, QueriedAt: now, Source: "persisted_call_records", MayLag: true, Current: current, AvailableDimensions: []string{"model", "key"}}
	if plan.previous != nil {
		previous, err := aggregateUsage(rows, *plan.previous, plan, admin)
		if err != nil {
			return nil, err
		}
		result.Previous = &previous
	}
	if admin {
		result.AvailableDimensions = append(result.AvailableDimensions, "provider_model", "connection")
	}
	for _, row := range rows {
		if result.LatestCompletedAt == nil || row.CompletedAt.After(*result.LatestCompletedAt) {
			value := row.CompletedAt
			result.LatestCompletedAt = &value
		}
	}
	return result, nil
}
