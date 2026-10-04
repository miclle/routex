package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"gorm.io/gorm"
)

type memberModelsAuditChanges struct {
	UserID string   `json:"user_id"`
	Before []string `json:"before"`
	After  []string `json:"after"`
	Reason string   `json:"reason"`
}

func appendMemberModelsAudit(tx *gorm.DB, actorID, userID string, before, after []string, reason string) error {
	raw, err := json.Marshal(memberModelsAuditChanges{userID, before, after, reason})
	if err != nil {
		return err
	}
	eventID, err := id.NewPrefixed("aud")
	if err != nil {
		return err
	}
	details := string(raw)
	return tx.Create(&entity.AuditEvent{ID: eventID, ActorID: actorID, Action: "member.models.update", ResourceType: "user_model_grant", ResourceID: userID, DetailsJSON: &details}).Error
}
func memberModelsAuditProjection(row entity.AuditEvent) (memberModelsAuditChanges, bool) {
	var result memberModelsAuditChanges
	if row.ResourceType != "user_model_grant" || row.DetailsJSON == nil || json.Unmarshal([]byte(*row.DetailsJSON), &result) != nil || result.UserID != row.ResourceID || !safeTeamSessionID(result.UserID) || !validCredentialMetadataReason(result.Reason) || result.Before == nil || result.After == nil {
		return result, false
	}
	for _, ids := range [][]string{result.Before, result.After} {
		if len(ids) > 1000 || !slices.IsSorted(ids) {
			return result, false
		}
		for i, id := range ids {
			if !validAdminModelTarget(id) || i > 0 && ids[i-1] == id {
				return result, false
			}
		}
	}
	return result, true
}
func memberModelsGrantIDs(grants []entity.UserModelGrant) []string {
	ids := make([]string, 0, len(grants))
	for _, g := range grants {
		ids = append(ids, g.ModelID)
	}
	slices.Sort(ids)
	return ids
}
func (s *Service) SetMemberModels(ctx context.Context, actorID, userID, etag string, input MemberModelsWriteInput) (*MemberModelsWriteResult, error) {
	if !safeTeamSessionID(actorID) {
		return nil, apperrors.ErrUnauthorized
	}
	if !safeTeamSessionID(userID) || !personalModelETag(etag) {
		return nil, apperrors.ErrBadRequest
	}
	normalized, err := normalizeMemberModels(input)
	if err != nil {
		return nil, err
	}
	input = normalized
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	release := s.pinPersonalKeyMutation()
	defer release()
	reduced := false
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		_, edit, providers, prices, err := memberModelsPermissions(tx, actorID, true)
		if err != nil {
			return err
		}
		subject, err := memberModelsSubject(tx, userID, true)
		if err != nil {
			return err
		}
		if subject.Disabled || subject.OffboardedAt != nil {
			return catalogConflict
		}
		data, err := readMemberModels(tx, subject, edit, providers, prices, true)
		if err != nil {
			return err
		}
		before := memberModelsGrantIDs(data.Grants)
		// Exact equal-state retries confirm only the current configuration. They do
		// not replay an old operation, advance its generation or append an audit.
		if slices.Equal(before, input.ModelIDs) {
			return nil
		}
		review := s.projectMemberModels(actorID, data)
		_, additions := memberModelsGrantDiff(data.Grants, input.ModelIDs)
		for _, modelID := range additions {
			eligible := false
			for _, row := range review.AvailableModels {
				if row.ID == modelID {
					if row.Availability == "unknown" {
						return runtimeUnavailable
					}
					eligible = row.Selectable
					break
				}
			}
			if !eligible {
				return catalogConflict
			}
		}
		if review.ETag != etag {
			return catalogConflict
		}

		reduced, err = replacePersonalModelGrants(tx, userID, data.Grants, input.ModelIDs)
		if err != nil {
			return err
		}
		return appendMemberModelsAudit(tx, actorID, userID, before, input.ModelIDs, input.Reason)
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err == nil && reduced {
		s.invalidatePersonalModelGrants(userID)
	}
	release()
	if err != nil {
		return nil, catalogError(err)
	}
	if err := s.RefreshRuntime(ctx); err != nil {
		return nil, runtimeUnavailable
	}
	return s.confirmMemberModels(ctx, userID, input.ModelIDs, func(ctx context.Context) (*MemberModelsWorkspace, error) {
		return s.MemberModelsWorkspace(ctx, actorID, userID)
	})
}
