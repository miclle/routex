package handler

import (
	"net/http"
	"time"

	"github.com/fox-gonic/fox"

	"github.com/miclle/routex/internal/routex/service"
)

type ProjectKeyResponse struct {
	KeyResponse
	ProjectID    string `json:"project_id"`
	CreatorID    string `json:"creator_id"`
	DeliveryMode string `json:"delivery_mode"`
}
type ProjectKeysResponse struct {
	Items      []ProjectKeyResponse `json:"items"`
	NextCursor *string              `json:"next_cursor"`
}
type CreatedProjectKeyResponse struct {
	Key    ProjectKeyResponse `json:"key"`
	Secret string             `json:"secret"`
}
type ProjectKeyPath struct {
	ProjectID string `uri:"project_id" json:"-"`
	KeyID     string `uri:"key_id" json:"-"`
}
type ListProjectKeysRequest struct {
	ProjectID string `uri:"project_id" json:"-"`
	Status    string `query:"status"`
	Cursor    string `query:"cursor"`
	Limit     int    `query:"limit"`
}
type CreateProjectKeyRequest struct {
	ProjectID    string     `uri:"project_id" json:"-"`
	Name         string     `json:"name"`
	ModelIDs     []string   `json:"model_ids"`
	ExpiresAt    *time.Time `json:"expires_at"`
	DeliveryMode string     `json:"delivery_mode"`
}
type UpdateProjectKeyRequest struct {
	ProjectID string  `uri:"project_id" json:"-"`
	KeyID     string  `uri:"key_id" json:"-"`
	Name      *string `json:"name"`
	Enabled   *bool   `json:"enabled"`
}
type RotateProjectKeyRequest struct {
	ProjectID    string `uri:"project_id" json:"-"`
	KeyID        string `uri:"key_id" json:"-"`
	DeliveryMode string `json:"delivery_mode"`
}
type CompleteProjectKeyRotationRequest struct {
	ProjectID        string `uri:"project_id" json:"-"`
	KeyID            string `uri:"key_id" json:"-"`
	ReplacementKeyID string `json:"replacement_key_id"`
}

func projectKeyResponse(record service.ProjectKeyRecord) ProjectKeyResponse {
	key := record.Key
	return ProjectKeyResponse{ProjectID: key.ProjectID, CreatorID: key.CreatorID, DeliveryMode: key.DeliveryMode, KeyResponse: KeyResponse{ID: key.ID, Name: key.Name, Prefix: key.Prefix, Status: key.Status, ModelIDs: record.ModelIDs, ExpiresAt: key.ExpiresAt, CreatedAt: key.CreatedAt, ReplacesKeyID: key.ReplacesKeyID, DeliveryExpiresAt: key.DeliveryExpiresAt}}
}
func (ctrl *Ctrl) ListProjectKeys(c *fox.Context, request ListProjectKeysRequest) (*ProjectKeysResponse, error) {
	page, err := ctrl.service.ListProjectKeys(c.Request.Context(), currentAuthentication(c).User.ID, request.ProjectID, service.ProjectKeyFilter{Status: request.Status, Cursor: request.Cursor, Limit: request.Limit})
	if err != nil {
		return nil, err
	}
	response := &ProjectKeysResponse{Items: []ProjectKeyResponse{}, NextCursor: callCursor(page.NextCursor)}
	for _, record := range page.Items {
		response.Items = append(response.Items, projectKeyResponse(record))
	}
	return response, nil
}
func (ctrl *Ctrl) GetProjectKey(c *fox.Context, request ProjectKeyPath) (*ProjectKeyResponse, error) {
	record, err := ctrl.service.GetProjectKey(c.Request.Context(), currentAuthentication(c).User.ID, request.ProjectID, request.KeyID)
	if err != nil {
		return nil, err
	}
	response := projectKeyResponse(*record)
	return &response, nil
}
func (ctrl *Ctrl) CreateProjectKey(c *fox.Context, request CreateProjectKeyRequest) error {
	result, err := ctrl.service.CreateProjectKey(c.Request.Context(), currentAuthentication(c).User.ID, request.ProjectID, request.Name, request.DeliveryMode, request.ModelIDs, request.ExpiresAt)
	if err != nil {
		return err
	}
	c.JSON(http.StatusCreated, CreatedProjectKeyResponse{Key: projectKeyResponse(result.Record), Secret: result.Secret})
	return nil
}
func (ctrl *Ctrl) ConfirmProjectKey(c *fox.Context, request ProjectKeyPath) (*ProjectKeyResponse, error) {
	record, err := ctrl.service.ConfirmProjectKey(c.Request.Context(), currentAuthentication(c).User.ID, request.ProjectID, request.KeyID)
	if err != nil {
		return nil, err
	}
	response := projectKeyResponse(*record)
	return &response, nil
}
func (ctrl *Ctrl) UpdateProjectKey(c *fox.Context, request UpdateProjectKeyRequest) (*ProjectKeyResponse, error) {
	record, err := ctrl.service.UpdateProjectKey(c.Request.Context(), currentAuthentication(c).User.ID, request.ProjectID, request.KeyID, request.Name, request.Enabled)
	if err != nil {
		return nil, err
	}
	response := projectKeyResponse(*record)
	return &response, nil
}
func (ctrl *Ctrl) RevokeProjectKey(c *fox.Context, request ProjectKeyPath) error {
	if err := ctrl.service.RevokeProjectKey(c.Request.Context(), currentAuthentication(c).User.ID, request.ProjectID, request.KeyID); err != nil {
		return err
	}
	c.Status(http.StatusNoContent)
	return nil
}
func (ctrl *Ctrl) RotateProjectKey(c *fox.Context, request RotateProjectKeyRequest) error {
	result, err := ctrl.service.RotateProjectKey(c.Request.Context(), currentAuthentication(c).User.ID, request.ProjectID, request.KeyID, request.DeliveryMode)
	if err != nil {
		return err
	}
	c.JSON(http.StatusCreated, CreatedProjectKeyResponse{Key: projectKeyResponse(result.Record), Secret: result.Secret})
	return nil
}
func (ctrl *Ctrl) CompleteProjectKeyRotation(c *fox.Context, request CompleteProjectKeyRotationRequest) error {
	if err := ctrl.service.CompleteProjectKeyRotation(c.Request.Context(), currentAuthentication(c).User.ID, request.ProjectID, request.KeyID, request.ReplacementKeyID); err != nil {
		return err
	}
	c.Status(http.StatusNoContent)
	return nil
}
