package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"slices"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
)

// ProjectCreationInput distinguishes a legacy creator-only request from an
// explicitly reviewed complete initial manager selection.
type ProjectCreationInput struct {
	Name             string                   `json:"name"`
	Description      string                   `json:"description"`
	ManagerIDs       *[]string                `json:"manager_ids,omitempty"`
	CreationID       string                   `json:"creation_id,omitempty"`
	ReviewETag       string                   `json:"-"`
	InitialResources *ProjectInitialResources `json:"initial_resources,omitempty"`
	InitialRequest   *ProjectInitialResources `json:"initial_request,omitempty"`
}

func (input *ProjectCreationInput) UnmarshalJSON(raw []byte) error {
	if !utf8.Valid(raw) {
		return apperrors.ErrBadRequest
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return apperrors.ErrBadRequest
	}
	seen := map[string]bool{}
	var result ProjectCreationInput
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return apperrors.ErrBadRequest
		}
		field, ok := token.(string)
		if !ok || seen[field] {
			return apperrors.ErrBadRequest
		}
		seen[field] = true
		var value json.RawMessage
		if decoder.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return apperrors.ErrBadRequest
		}
		switch field {
		case "name":
			if json.Unmarshal(value, &result.Name) != nil {
				return apperrors.ErrBadRequest
			}
		case "description":
			if json.Unmarshal(value, &result.Description) != nil {
				return apperrors.ErrBadRequest
			}
		case "manager_ids":
			selected, err := projectCreationStringIDs(value, false)
			if err != nil {
				return apperrors.ErrBadRequest
			}
			result.ManagerIDs = &selected
		case "creation_id":
			if json.Unmarshal(value, &result.CreationID) != nil || result.CreationID == "" {
				return apperrors.ErrBadRequest
			}
		case "initial_resources":
			if json.Unmarshal(value, &result.InitialResources) != nil {
				return apperrors.ErrBadRequest
			}
		case "initial_request":
			if json.Unmarshal(value, &result.InitialRequest) != nil {
				return apperrors.ErrBadRequest
			}
		default:
			return apperrors.ErrBadRequest
		}
	}
	if _, err := decoder.Token(); err != nil {
		return apperrors.ErrBadRequest
	}
	if _, err := decoder.Token(); err != io.EOF {
		return apperrors.ErrBadRequest
	}
	*input = result
	return nil
}

func projectCreationManagers(actorID string, platform bool, submitted *[]string) ([]string, error) {
	if submitted == nil {
		return []string{actorID}, nil
	}
	if len(*submitted) == 0 || len(*submitted) > 1000 {
		return nil, apperrors.ErrBadRequest
	}
	selected := make([]string, 0, len(*submitted)+1)
	seen := make(map[string]bool, len(*submitted)+1)
	for _, userID := range *submitted {
		if !strings.HasPrefix(userID, "usr_") || !safeTeamSessionID(userID) || seen[userID] {
			return nil, apperrors.ErrBadRequest
		}
		seen[userID] = true
		selected = append(selected, userID)
	}
	if !platform && !seen[actorID] {
		selected = append(selected, actorID)
	}
	if len(selected) > 1000 {
		return nil, apperrors.ErrBadRequest
	}
	slices.Sort(selected)
	return selected, nil
}

// createProjectWithManagers runs inside the caller's governance transaction.
// No model grants, Keys, limits or pending requests are implicit in creation.
func createProjectWithManagers(tx *gorm.DB, actorID, projectID, name, description string, submitted *[]string) error {
	actor, err := exactEnabledActor(tx, actorID)
	if err != nil {
		return err
	}
	platform, err := exactGovernancePermission(tx, actor, "projects.write")
	if err != nil {
		return err
	}
	selected, err := projectCreationManagers(actor.ID, platform, submitted)
	if err != nil {
		return err
	}
	if err := activeProjectResourceUsers(tx, selected); err != nil {
		return err
	}
	project := entity.Project{ID: projectID, Name: name, Description: description, Status: entity.ResourceActive, CreatorID: actor.ID}
	if err := tx.Create(&project).Error; err != nil {
		return err
	}
	for _, userID := range selected {
		relationID, err := id.NewPrefixed("pmg")
		if err != nil {
			return err
		}
		if err := tx.Create(&entity.ProjectManager{ID: relationID, ProjectID: project.ID, UserID: userID}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) CreateProject(ctx context.Context, actorID string, input ProjectCreationInput) (*ResourceRecord, error) {
	if input.CreationID != "" || input.InitialResources != nil || input.InitialRequest != nil || input.ReviewETag != "" {
		return nil, apperrors.ErrBadRequest
	}
	name := strings.TrimSpace(input.Name)
	if !validCatalogLabel(name) || !validResourceDescription(input.Description) {
		return nil, apperrors.ErrBadRequest
	}
	projectID, err := id.NewPrefixed("prj")
	if err != nil {
		return nil, apperrors.ErrInternal
	}
	var result *ResourceRecord
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := createProjectWithManagers(tx, actorID, projectID, name, input.Description, input.ManagerIDs); err != nil {
			return err
		}
		if err := appendAudit(tx, actorID, "resource.create", string(ProjectResource), projectID); err != nil {
			return err
		}
		result, err = resourceRecord(tx, ProjectResource, projectID, false)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	return result, catalogError(err)
}
