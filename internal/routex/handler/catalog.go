package handler

import (
	"net/http"
	"time"

	"github.com/fox-gonic/fox"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
)

type CredentialResponse struct {
	StorageSource        string     `json:"storage_source"`
	ReplacesCredentialID *string    `json:"replaces_credential_id"`
	ID                   string     `json:"id"`
	Name                 string     `json:"name"`
	Priority             int        `json:"priority"`
	Enabled              bool       `json:"enabled"`
	VerificationStatus   string     `json:"verification_status"`
	VerifiedAt           *time.Time `json:"verified_at"`
}
type ProviderModelResponse struct {
	Enabled            bool   `json:"enabled"`
	SupportsImageInput bool   `json:"supports_image_input"`
	SupportsPDFInput   bool   `json:"supports_pdf_input"`
	ETag               string `json:"etag"`
	ID                 string `json:"id"`
	UpstreamName       string `json:"upstream_name"`
}
type ConnectionResponse struct {
	Enabled        bool                    `json:"enabled"`
	EgressMode     string                  `json:"egress_mode"`
	EgressID       *string                 `json:"egress_id"`
	ETag           string                  `json:"etag"`
	ID             string                  `json:"id"`
	Name           string                  `json:"name"`
	BaseURL        string                  `json:"base_url"`
	Protocol       string                  `json:"protocol"`
	Credentials    []CredentialResponse    `json:"credentials"`
	ProviderModels []ProviderModelResponse `json:"provider_models"`
}
type ProviderResponse struct {
	ID          string               `json:"id"`
	Name        string               `json:"name"`
	Connections []ConnectionResponse `json:"connections"`
}
type ProvidersResponse struct {
	Items []ProviderResponse `json:"items"`
}

type CreateProviderRequest struct {
	RequestID         string  `json:"request_id"`
	EgressMode        string  `json:"egress_mode"`
	EgressID          *string `json:"egress_id"`
	Name              string  `json:"name"`
	ConnectionName    string  `json:"connection_name"`
	BaseURL           string  `json:"base_url"`
	Protocol          string  `json:"protocol"`
	CredentialName    string  `json:"credential_name"`
	Secret            string  `json:"secret"`
	StoragePolicyETag string  `json:"storage_policy_etag"`
}
type CreateConnectionRequest struct {
	RequestID         string  `json:"request_id"`
	EgressMode        string  `json:"egress_mode"`
	EgressID          *string `json:"egress_id"`
	ProviderID        string  `uri:"provider_id" json:"-"`
	Name              string  `json:"name"`
	BaseURL           string  `json:"base_url"`
	Protocol          string  `json:"protocol"`
	CredentialName    string  `json:"credential_name"`
	Secret            string  `json:"secret"`
	StoragePolicyETag string  `json:"storage_policy_etag"`
}
type CreateCredentialRequest struct {
	RequestID         string `json:"request_id"`
	ConnectionID      string `uri:"connection_id" json:"-"`
	Name              string `json:"name"`
	Secret            string `json:"secret"`
	Priority          int    `json:"priority"`
	StoragePolicyETag string `json:"storage_policy_etag"`
}
type CredentialPath struct {
	CredentialID string `uri:"credential_id" json:"-"`
}
type UpdateCredentialRequest struct {
	CredentialID string `uri:"credential_id" json:"-"`
	Enabled      *bool  `json:"enabled"`
}
type VerifyCredentialResponse struct {
	Verified         bool   `json:"verified"`
	DiscoveredModels int    `json:"discovered_models"`
	Message          string `json:"message"`
}
type CreateProviderModelRequest struct {
	ConnectionID string `uri:"connection_id" json:"-"`
	UpstreamName string `json:"upstream_name"`
}

func credentialResponse(item entity.ProviderCredential) CredentialResponse {
	return CredentialResponse{StorageSource: credentialStorageSource(item), ID: item.ID, Name: item.Name, Priority: item.Priority, Enabled: item.Enabled, VerificationStatus: item.VerificationStatus, VerifiedAt: item.VerifiedAt, ReplacesCredentialID: item.ReplacesCredentialID}
}
func providerModelResponse(item entity.ProviderModel) ProviderModelResponse {
	return ProviderModelResponse{ID: item.ID, UpstreamName: item.UpstreamName, Enabled: !item.Disabled, SupportsImageInput: item.SupportsImageInput, SupportsPDFInput: item.SupportsPDFInput, ETag: item.ETag}
}
func connectionResponse(item service.ConnectionCatalog) ConnectionResponse {
	result := ConnectionResponse{Enabled: item.Connection.Enabled, EgressMode: item.Connection.EgressMode, EgressID: item.Connection.EgressID, ETag: item.Connection.ETag, ID: item.Connection.ID, Name: item.Connection.Name, BaseURL: item.Connection.BaseURL, Protocol: item.Connection.Protocol, Credentials: []CredentialResponse{}, ProviderModels: []ProviderModelResponse{}}
	for _, credential := range item.Credentials {
		result.Credentials = append(result.Credentials, credentialResponse(credential))
	}
	for _, model := range item.Models {
		result.ProviderModels = append(result.ProviderModels, providerModelResponse(model))
	}
	return result
}
func providerResponse(item service.ProviderCatalog) ProviderResponse {
	result := ProviderResponse{ID: item.Provider.ID, Name: item.Provider.Name, Connections: []ConnectionResponse{}}
	for _, connection := range item.Connections {
		result.Connections = append(result.Connections, connectionResponse(connection))
	}
	return result
}

func (ctrl *Ctrl) ListProviders(c *fox.Context) (*ProvidersResponse, error) {
	items, err := ctrl.service.ListProviders(c.Request.Context())
	if err != nil {
		return nil, err
	}
	result := &ProvidersResponse{Items: []ProviderResponse{}}
	for _, item := range items {
		result.Items = append(result.Items, providerResponse(item))
	}
	return result, nil
}
func (ctrl *Ctrl) CreateProvider(c *fox.Context, request CreateProviderRequest) error {
	result, err := ctrl.service.CreateProvider(c.Request.Context(), currentAuthentication(c).User.ID, request.Name, service.CreateConnectionInput{RequestID: request.RequestID, StoragePolicyETag: request.StoragePolicyETag, EgressMode: request.EgressMode, EgressID: request.EgressID, Name: request.ConnectionName, BaseURL: request.BaseURL, Protocol: request.Protocol, CredentialName: request.CredentialName, Secret: request.Secret})
	if err != nil {
		return err
	}
	c.JSON(http.StatusCreated, providerResponse(*result))
	return nil
}
func (ctrl *Ctrl) CreateConnection(c *fox.Context, request CreateConnectionRequest) error {
	result, err := ctrl.service.CreateConnection(c.Request.Context(), currentAuthentication(c).User.ID, request.ProviderID, service.CreateConnectionInput{RequestID: request.RequestID, StoragePolicyETag: request.StoragePolicyETag, EgressMode: request.EgressMode, EgressID: request.EgressID, Name: request.Name, BaseURL: request.BaseURL, Protocol: request.Protocol, CredentialName: request.CredentialName, Secret: request.Secret})
	if err != nil {
		return err
	}
	c.JSON(http.StatusCreated, connectionResponse(*result))
	return nil
}
func (ctrl *Ctrl) CreateCredential(c *fox.Context, request CreateCredentialRequest) error {
	result, err := ctrl.service.CreateCredential(c.Request.Context(), currentAuthentication(c).User.ID, request.ConnectionID, request.Name, request.Secret, request.Priority, request.RequestID, request.StoragePolicyETag)
	if err != nil {
		return err
	}
	c.JSON(http.StatusCreated, credentialResponse(*result))
	return nil
}
func (ctrl *Ctrl) VerifyCredential(c *fox.Context, request CredentialPath) (*VerifyCredentialResponse, error) {
	result, err := ctrl.service.VerifyCredential(c.Request.Context(), currentAuthentication(c).User.ID, request.CredentialID)
	if err != nil {
		return nil, err
	}
	return &VerifyCredentialResponse{Verified: result.Verified, DiscoveredModels: result.DiscoveredModels, Message: result.Message}, nil
}
func (ctrl *Ctrl) UpdateCredential(c *fox.Context, request UpdateCredentialRequest) (*CredentialResponse, error) {
	if request.Enabled == nil {
		return nil, errCatalogBadRequest
	}
	result, err := ctrl.service.SetCredentialEnabled(c.Request.Context(), currentAuthentication(c).User.ID, request.CredentialID, *request.Enabled)
	if err != nil {
		return nil, err
	}
	response := credentialResponse(*result)
	return &response, nil
}
func (ctrl *Ctrl) CreateProviderModel(c *fox.Context, request CreateProviderModelRequest) error {
	result, err := ctrl.service.CreateProviderModel(c.Request.Context(), currentAuthentication(c).User.ID, request.ConnectionID, request.UpstreamName)
	if err != nil {
		return err
	}
	c.JSON(http.StatusCreated, providerModelResponse(*result))
	return nil
}

func credentialStorageSource(c entity.ProviderCredential) string {
	if c.StorageSource == "vault" {
		return "vault"
	}
	return "inline"
}
