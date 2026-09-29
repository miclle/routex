package service

import (
	"context"
	"slices"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
)

func (s *Service) authorizeProjectCalls(ctx context.Context, actorID, projectID string) error {
	return catalogError(authorizeProjectCallsDB(s.authDB(ctx), actorID, projectID))
}

func authorizeProjectCallsDB(db *gorm.DB, actorID, projectID string) error {
	permissions, err := permissionsFor(db, actorID)
	if err != nil {
		return err
	}
	if !slices.Contains(permissions, "calls.read_all") {
		manager, err := resourceManager(db, actorID, projectID)
		if err != nil {
			return err
		}
		if !manager {
			return apperrors.ErrNotFound
		}
	}
	var count int64
	if err := db.Model(&entity.Project{}).Where("id = ?", projectID).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return apperrors.ErrNotFound
	}
	return nil
}

// Project histories remain readable after disable/archive, but never become
// personal history merely because the reader created or managed the resource.
func (s *Service) ListProjectCalls(ctx context.Context, actorID, projectID string, filter CallFilter) (*CallPage, error) {
	if err := validateCallFilter(filter, false); err != nil {
		return nil, err
	}
	if err := s.authorizeProjectCalls(ctx, actorID, projectID); err != nil {
		return nil, err
	}
	filter.ProjectID, filter.UserID = projectID, ""
	return s.ListCalls(ctx, "", filter)
}

func (s *Service) GetProjectCall(ctx context.Context, actorID, projectID, requestID string) (*entity.CallRecord, error) {
	if err := s.authorizeProjectCalls(ctx, actorID, projectID); err != nil {
		return nil, err
	}
	var record entity.CallRecord
	if err := s.authDB(ctx).Where("project_id = ? AND request_id = ?", projectID, requestID).First(&record).Error; err != nil {
		return nil, catalogError(err)
	}
	return &record, nil
}
