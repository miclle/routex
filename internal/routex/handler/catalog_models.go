package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/fox-gonic/fox"

	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

var errCatalogBadRequest = apperrors.ErrBadRequest

type ModelNameResponse struct {
	Name      string     `json:"name"`
	IsCurrent bool       `json:"is_current"`
	ExpiresAt *time.Time `json:"expires_at"`
}
type ModelBindingResponse struct {
	Supply *service.ModelRoutingSupply `json:"supply,omitempty"`

	ID              string `json:"id"`
	ProviderModelID string `json:"provider_model_id"`
	ProviderID      string `json:"provider_id"`
	ConnectionID    string `json:"connection_id"`
	UpstreamName    string `json:"upstream_name"`
	Protocol        string `json:"protocol"`
	Weight          int    `json:"weight"`
	Ready           bool   `json:"ready"`
}
type ModelResponse struct {
	CreatedAt       *time.Time             `json:"created_at"`
	ConfigUpdatedAt *time.Time             `json:"config_updated_at"`
	ID              string                 `json:"id"`
	Name            string                 `json:"name"`
	Status          string                 `json:"status"`
	Names           []ModelNameResponse    `json:"names"`
	Bindings        []ModelBindingResponse `json:"bindings"`
	GrantedUserIDs  []string               `json:"granted_user_ids"`
}
type ModelsResponse struct {
	Items []ModelResponse `json:"items"`
}
type CreateModelRequest struct {
	Name            string `json:"name"`
	ProviderModelID string `json:"provider_model_id"`
}
type CreateModelBindingRequest struct {
	Protocol   *string `json:"protocol"`
	ReviewETag *string `json:"review_etag"`

	ModelID         string `uri:"model_id" json:"-"`
	ProviderModelID string `json:"provider_model_id"`
}

// The reviewed branch has an exact shape; old unreviewed callers retain their existing wire.
func (request *CreateModelBindingRequest) UnmarshalJSON(raw []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return errCatalogBadRequest
	}
	_, protocol := fields["protocol"]
	_, review := fields["review_etag"]
	if protocol || review {
		if !protocol || !review || len(fields) != 3 || bytes.Equal(bytes.TrimSpace(fields["protocol"]), []byte("null")) || bytes.Equal(bytes.TrimSpace(fields["review_etag"]), []byte("null")) {
			return errCatalogBadRequest
		}
		// Streaming field decoding rejects duplicate keys that map decoding would hide.
		decoder := json.NewDecoder(bytes.NewReader(raw))
		if _, err := decoder.Token(); err != nil {
			return errCatalogBadRequest
		}
		seen := map[string]bool{}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return errCatalogBadRequest
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return errCatalogBadRequest
			}
			seen[name] = true
			var value json.RawMessage
			if decoder.Decode(&value) != nil {
				return errCatalogBadRequest
			}
		}
		if _, err := decoder.Token(); err != nil {
			return errCatalogBadRequest
		}
		if _, err := decoder.Token(); err != io.EOF {
			return errCatalogBadRequest
		}
	}
	type wire CreateModelBindingRequest
	var value wire
	if err := json.Unmarshal(raw, &value); err != nil {
		return errCatalogBadRequest
	}
	// URI identity is bound independently by the handler framework.
	value.ModelID = request.ModelID
	*request = CreateModelBindingRequest(value)
	return nil
}

type ModelWeightRequest struct {
	BindingID string `json:"binding_id"`
	Weight    int    `json:"weight"`
}
type UpdateModelWeightsRequest struct {
	ModelID string               `uri:"model_id" json:"-"`
	Weights []ModelWeightRequest `json:"weights"`
}
type RenameModelRequest struct {
	ModelID        string     `uri:"model_id" json:"-"`
	Name           string     `json:"name"`
	AliasExpiresAt *time.Time `json:"alias_expires_at"`
}
type UpdateModelGrantsRequest struct {
	ModelID string    `uri:"model_id" json:"-"`
	UserIDs *[]string `json:"user_ids"`
}
type VisibleModelResponse struct {
	Protocols         []string            `json:"protocols"`
	InputCapabilities map[string][]string `json:"input_capabilities"`
	ID                string              `json:"id"`
	Name              string              `json:"name"`
	Status            string              `json:"status"`
	Protocol          string              `json:"protocol"`
}
type VisibleModelsResponse struct {
	Items []VisibleModelResponse `json:"items"`
}
type ModelGranteeResponse struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}
type ModelGranteesResponse struct {
	Items []ModelGranteeResponse `json:"items"`
}

func modelResponse(item service.ModelCatalog) *ModelResponse {
	result := &ModelResponse{ID: item.Model.ID, Name: item.Name, Status: item.Model.Status, Names: []ModelNameResponse{}, Bindings: []ModelBindingResponse{}, GrantedUserIDs: item.GrantedUserIDs}
	if !item.Model.CreatedAt.IsZero() {
		value := item.Model.CreatedAt.UTC()
		result.CreatedAt = &value
	}
	if item.Model.ConfigUpdatedAt != nil && !item.Model.ConfigUpdatedAt.IsZero() {
		value := item.Model.ConfigUpdatedAt.UTC()
		result.ConfigUpdatedAt = &value
	}
	for _, name := range item.Names {
		result.Names = append(result.Names, ModelNameResponse{Name: name.Name, IsCurrent: name.CurrentModelID != nil, ExpiresAt: name.ExpiresAt})
	}
	for _, binding := range item.Bindings {
		result.Bindings = append(result.Bindings, ModelBindingResponse{ID: binding.Binding.ID, ProviderModelID: binding.Binding.ProviderModelID, ProviderID: binding.ProviderID, ConnectionID: binding.ConnectionID, UpstreamName: binding.UpstreamName, Protocol: binding.Protocol, Weight: binding.Binding.Weight, Ready: binding.Ready, Supply: binding.Supply})
	}
	return result
}
func (ctrl *Ctrl) ListAdminModels(c *fox.Context) (*ModelsResponse, error) {
	items, err := ctrl.service.ListAdminModels(c.Request.Context())
	if err != nil {
		return nil, err
	}
	result := &ModelsResponse{Items: []ModelResponse{}}
	for _, item := range items {
		result.Items = append(result.Items, *modelResponse(item))
	}
	return result, nil
}
func (ctrl *Ctrl) CreateModel(c *fox.Context, request CreateModelRequest) error {
	result, err := ctrl.service.CreateModel(c.Request.Context(), currentAuthentication(c).User.ID, request.Name, request.ProviderModelID)
	if err != nil {
		return err
	}
	c.JSON(http.StatusCreated, modelResponse(*result))
	return nil
}
func (ctrl *Ctrl) AddModelBinding(c *fox.Context, request CreateModelBindingRequest) error {
	var result *service.ModelCatalog
	var err error
	if request.Protocol != nil || request.ReviewETag != nil {
		if request.Protocol == nil || request.ReviewETag == nil {
			return apperrors.ErrBadRequest
		}
		result, err = ctrl.service.AddReviewedModelBinding(c.Request.Context(), currentAuthentication(c).User.ID, request.ModelID, request.ProviderModelID, *request.Protocol, *request.ReviewETag)
	} else {
		result, err = ctrl.service.AddModelBinding(c.Request.Context(), currentAuthentication(c).User.ID, request.ModelID, request.ProviderModelID)
	}
	if err != nil {
		return err
	}
	c.JSON(http.StatusCreated, modelResponse(*result))
	return nil
}
func (ctrl *Ctrl) UpdateModelWeights(c *fox.Context, request UpdateModelWeightsRequest) (*ModelResponse, error) {
	weights := make([]service.ModelWeight, 0, len(request.Weights))
	for _, weight := range request.Weights {
		weights = append(weights, service.ModelWeight{BindingID: weight.BindingID, Weight: weight.Weight})
	}
	result, err := ctrl.service.SetModelWeights(c.Request.Context(), currentAuthentication(c).User.ID, request.ModelID, weights)
	if err != nil {
		return nil, err
	}
	return modelResponse(*result), nil
}
func (ctrl *Ctrl) RenameModel(c *fox.Context, request RenameModelRequest) (*ModelResponse, error) {
	result, err := ctrl.service.RenameModel(c.Request.Context(), currentAuthentication(c).User.ID, request.ModelID, request.Name, request.AliasExpiresAt)
	if err != nil {
		return nil, err
	}
	return modelResponse(*result), nil
}
func (ctrl *Ctrl) UpdateModelGrants(c *fox.Context, request UpdateModelGrantsRequest) (*ModelResponse, error) {
	if request.UserIDs == nil {
		return nil, errCatalogBadRequest
	}
	result, err := ctrl.service.SetModelGrants(c.Request.Context(), currentAuthentication(c).User.ID, request.ModelID, *request.UserIDs)
	if err != nil {
		return nil, err
	}
	return modelResponse(*result), nil
}
func (ctrl *Ctrl) ListVisibleModels(c *fox.Context) (*VisibleModelsResponse, error) {
	items, err := ctrl.service.ListVisibleModels(c.Request.Context(), currentAuthentication(c).User.ID)
	if err != nil {
		return nil, err
	}
	result := &VisibleModelsResponse{Items: []VisibleModelResponse{}}
	for _, item := range items {
		result.Items = append(result.Items, VisibleModelResponse{ID: item.ID, Name: item.Name, Status: item.Status, Protocol: item.Protocol, Protocols: item.Protocols, InputCapabilities: item.InputCapabilities})
	}
	return result, nil
}
func (ctrl *Ctrl) ListModelGrantees(c *fox.Context) (*ModelGranteesResponse, error) {
	items, err := ctrl.service.ListModelGrantees(c.Request.Context())
	if err != nil {
		return nil, err
	}
	result := &ModelGranteesResponse{Items: []ModelGranteeResponse{}}
	for _, item := range items {
		result.Items = append(result.Items, ModelGranteeResponse{ID: item.ID, Email: item.Email, Name: item.Name})
	}
	return result, nil
}
