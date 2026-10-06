package service

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

// TeamUsage aggregates the Team's immutable attribution; current membership
// authorizes the report without rewriting facts from prior memberships.
func (s *Service) TeamUsage(ctx context.Context, actorID, teamID string, filter UsageFilter) (*UsageReport, error) {
	if err := validateTeamUsageFilter(filter); err != nil {
		return nil, err
	}
	return s.queryUsage(ctx, actorID, "team", teamID, filter)
}

func validateTeamUsageFilter(filter UsageFilter) error {
	if err := validateUsageFilter(filter, false); err != nil {
		return err
	}
	if filter.KeyID != "" {
		return apperrors.ErrBadRequest
	}
	return nil
}

func teamUsageFacts(query *gorm.DB, teamID string) *gorm.DB {
	return query.Where(database.ExactText(query, clause.Column{Name: "team_id"}, teamID)).
		Where(database.ExactText(query, clause.Column{Name: "project_id"}, "")).
		Where(database.ExactText(query, clause.Column{Name: "key_id"}, ""))
}

func validateTeamUsageFacts(rows []entity.CallRecord, teamID string) error {
	for _, row := range rows {
		if row.TeamID != teamID {
			return apperrors.ErrInternal
		}
	}
	return nil
}

func usageFactColumns(scope string) []string {
	columns := []string{"request_id", "user_id", "team_id", "model_id", "model_name", "status", "started_at", "completed_at", "duration_ms", "input_tokens", "output_tokens", "pricing_status", "charge_amount", "charge_currency"}
	if scope != "team" {
		columns = append(columns, "key_id", "provider_id", "provider_name", "provider_model_id", "upstream_model_name", "connection_id", "connection_name")
	}
	return columns
}

func usageDimensions(scope string, filter UsageFilter) []string {
	dimensions := []string{"model"}
	if scope != "team" && (scope != "admin" || filter.TeamID == "") {
		dimensions = append(dimensions, "key")
	}
	if scope == "admin" {
		dimensions = append(dimensions, "provider", "provider_model", "connection")
	}
	return dimensions
}
