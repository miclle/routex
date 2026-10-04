package service

import (
	"context"
	"errors"
	"net/http"
	"time"

	apperrors "github.com/miclle/routex/internal/routex/errors"
)

const usageExportBytes = 8 << 20

var (
	usageExportTooLarge    = &apperrors.Error{Code: http.StatusUnprocessableEntity, Message: "usage report exceeds the CSV export limit"}
	usageExportUnavailable = &apperrors.Error{Code: http.StatusServiceUnavailable, Message: "usage export temporarily unavailable"}
)

type UsageCSVExport struct{ CSV []byte }

func (s *Service) ExportPersonalUsage(ctx context.Context, actorID string, filter UsageFilter) (*UsageCSVExport, error) {
	return exportUsageCapture(ctx, "personal", actorID, filter, func(ctx context.Context) (*UsageReport, error) {
		return s.PersonalUsage(ctx, actorID, filter)
	})
}
func (s *Service) ExportProjectUsage(ctx context.Context, actorID, projectID string, filter UsageFilter) (*UsageCSVExport, error) {
	return exportUsageCapture(ctx, "project", projectID, filter, func(ctx context.Context) (*UsageReport, error) {
		return s.ProjectUsage(ctx, actorID, projectID, filter)
	})
}
func (s *Service) ExportTeamUsage(ctx context.Context, actorID, teamID string, filter UsageFilter) (*UsageCSVExport, error) {
	return exportUsageCapture(ctx, "team", teamID, filter, func(ctx context.Context) (*UsageReport, error) {
		return s.TeamUsage(ctx, actorID, teamID, filter)
	})
}
func (s *Service) ExportAdminUsage(ctx context.Context, actorID string, filter UsageFilter) (*UsageCSVExport, error) {
	return exportUsageCapture(ctx, "admin", "", filter, func(ctx context.Context) (*UsageReport, error) {
		return s.AdminUsage(ctx, actorID, filter)
	})
}

// The report owns authorization, planning and its complete read snapshot. The
// outer deadline includes that single capture and the entire CSV encoding.
func exportUsageCapture(ctx context.Context, scope, targetID string, filter UsageFilter, capture func(context.Context) (*UsageReport, error)) (*UsageCSVExport, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if ctx.Err() != nil {
		return nil, usageExportUnavailable
	}
	report, err := capture(ctx)
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil, usageExportUnavailable
	}
	if err != nil {
		return nil, err
	}
	encoded, err := encodeUsageCSV(ctx, scope, targetID, filter, report)
	if err != nil {
		return nil, err
	}
	return &UsageCSVExport{CSV: encoded}, nil
}
