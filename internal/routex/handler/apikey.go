package handler

import (
	"net/http"
	"time"

	"github.com/fox-gonic/fox"

	"github.com/miclle/routex/internal/routex/service"
)

type KeyResponse struct {
	ID                string     `json:"id"`
	Name              string     `json:"name"`
	Prefix            string     `json:"prefix"`
	Status            string     `json:"status"`
	ModelIDs          []string   `json:"model_ids"`
	ExpiresAt         *time.Time `json:"expires_at"`
	CreatedAt         time.Time  `json:"created_at"`
	ReplacesKeyID     *string    `json:"replaces_key_id"`
	DeliveryExpiresAt *time.Time `json:"delivery_expires_at"`
}

type KeyListResponse struct {
	Items []KeyResponse `json:"items"`
}

type CreateKeyRequest struct {
	Name      string     `json:"name"`
	ModelIDs  []string   `json:"model_ids"`
	ExpiresAt *time.Time `json:"expires_at"`
}

type KeyPath struct {
	KeyID string `uri:"key_id" binding:"required"`
}

type UpdateKeyRequest struct {
	KeyID   string  `uri:"key_id" binding:"required"`
	Name    *string `json:"name"`
	Enabled *bool   `json:"enabled"`
}

type CreatedKeyResponse struct {
	Key    KeyResponse `json:"key"`
	Secret string      `json:"secret"`
}

func keyResponse(record service.KeyRecord) KeyResponse {
	key := record.Key
	return KeyResponse{ID: key.ID, Name: key.Name, Prefix: key.Prefix, Status: key.Status, ModelIDs: record.ModelIDs, ExpiresAt: key.ExpiresAt, CreatedAt: key.CreatedAt, ReplacesKeyID: key.ReplacesKeyID, DeliveryExpiresAt: key.DeliveryExpiresAt}
}

func (ctrl *Ctrl) ListPersonalKeys(c *fox.Context) (*KeyListResponse, error) {
	records, err := ctrl.service.ListPersonalKeys(c.Request.Context(), currentAuthentication(c).User.ID)
	if err != nil {
		return nil, err
	}
	items := make([]KeyResponse, 0, len(records))
	for _, record := range records {
		items = append(items, keyResponse(record))
	}
	return &KeyListResponse{Items: items}, nil
}

func (ctrl *Ctrl) CreatePersonalKey(c *fox.Context, request CreateKeyRequest) error {
	result, err := ctrl.service.CreatePersonalKey(c.Request.Context(), currentAuthentication(c).User.ID, request.Name, request.ModelIDs, request.ExpiresAt)
	if err != nil {
		return err
	}
	c.JSON(http.StatusCreated, CreatedKeyResponse{Key: keyResponse(result.Record), Secret: result.Secret})
	return nil
}

func (ctrl *Ctrl) ConfirmKeyDelivery(c *fox.Context, request KeyPath) (*KeyResponse, error) {
	record, err := ctrl.service.ConfirmKeyDelivery(c.Request.Context(), currentAuthentication(c).User.ID, request.KeyID)
	if err != nil {
		return nil, err
	}
	response := keyResponse(*record)
	return &response, nil
}

func (ctrl *Ctrl) UpdatePersonalKey(c *fox.Context, request UpdateKeyRequest) (*KeyResponse, error) {
	record, err := ctrl.service.UpdatePersonalKey(c.Request.Context(), currentAuthentication(c).User.ID, request.KeyID, request.Name, request.Enabled)
	if err != nil {
		return nil, err
	}
	response := keyResponse(*record)
	return &response, nil
}

func (ctrl *Ctrl) RevokePersonalKey(c *fox.Context, request KeyPath) error {
	if err := ctrl.service.RevokePersonalKey(c.Request.Context(), currentAuthentication(c).User.ID, request.KeyID); err != nil {
		return err
	}
	c.Status(http.StatusNoContent)
	return nil
}

func (ctrl *Ctrl) RotatePersonalKey(c *fox.Context, request KeyPath) error {
	result, err := ctrl.service.RotatePersonalKey(c.Request.Context(), currentAuthentication(c).User.ID, request.KeyID)
	if err != nil {
		return err
	}
	c.JSON(http.StatusCreated, CreatedKeyResponse{Key: keyResponse(result.Record), Secret: result.Secret})
	return nil
}
