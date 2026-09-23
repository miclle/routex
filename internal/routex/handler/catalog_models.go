package handler

import (
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
	ID             string                 `json:"id"`
	Name           string                 `json:"name"`
	Status         string                 `json:"status"`
	Names          []ModelNameResponse    `json:"names"`
	Bindings       []ModelBindingResponse `json:"bindings"`
	GrantedUserIDs []string               `json:"granted_user_ids"`
}
type ModelsResponse struct {
	Items []ModelResponse `json:"items"`
}
type CreateModelRequest struct {
	Name            string `json:"name"`
	ProviderModelID string `json:"provider_model_id"`
}
type CreateModelBindingRequest struct {
	ModelID         string `uri:"model_id" json:"-"`
	ProviderModelID string `json:"provider_model_id"`
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
	ID       string `json:"id"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	Protocol string `json:"protocol"`
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
	for _, name := range item.Names {
		result.Names = append(result.Names, ModelNameResponse{Name: name.Name, IsCurrent: name.CurrentModelID != nil, ExpiresAt: name.ExpiresAt})
	}
	for _, binding := range item.Bindings {
		result.Bindings = append(result.Bindings, ModelBindingResponse{ID: binding.Binding.ID, ProviderModelID: binding.Binding.ProviderModelID, ProviderID: binding.ProviderID, ConnectionID: binding.ConnectionID, UpstreamName: binding.UpstreamName, Protocol: binding.Protocol, Weight: binding.Binding.Weight, Ready: binding.Ready})
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
	result, err := ctrl.service.AddModelBinding(c.Request.Context(), currentAuthentication(c).User.ID, request.ModelID, request.ProviderModelID)
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
		result.Items = append(result.Items, VisibleModelResponse{ID: item.ID, Name: item.Name, Status: item.Status, Protocol: item.Protocol})
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
