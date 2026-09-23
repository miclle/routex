package handler

import (
	"encoding/json"
	"time"

	"github.com/fox-gonic/fox"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

type CallResponse struct {
	RequestID        string    `json:"request_id"`
	ModelID          string    `json:"model_id"`
	ModelName        string    `json:"model_name"`
	KeyID            string    `json:"key_id"`
	Protocol         string    `json:"protocol"`
	Status           string    `json:"status"`
	Stream           bool      `json:"stream"`
	StartedAt        time.Time `json:"started_at"`
	CompletedAt      time.Time `json:"completed_at"`
	DurationMS       int64     `json:"duration_ms"`
	InputTokens      *int64    `json:"input_tokens"`
	OutputTokens     *int64    `json:"output_tokens"`
	CacheReadTokens  *int64    `json:"cache_read_tokens"`
	CacheWriteTokens *int64    `json:"cache_write_tokens"`
	PricingStatus    string    `json:"pricing_status"`
	ChargeAmount     *string   `json:"charge_amount"`
	ChargeCurrency   *string   `json:"charge_currency"`
}
type CallsResponse struct {
	Items      []CallResponse `json:"items"`
	NextCursor *string        `json:"next_cursor"`
}
type AdminCallResponse struct {
	CallResponse
	UserID    string `json:"user_id"`
	ProjectID string `json:"project_id,omitempty"`
}
type AdminCallsResponse struct {
	Items      []AdminCallResponse `json:"items"`
	NextCursor *string             `json:"next_cursor"`
}
type CallAttemptResponse struct {
	ID              string    `json:"id"`
	ProviderModelID string    `json:"provider_model_id"`
	ConnectionID    string    `json:"connection_id"`
	Status          string    `json:"status"`
	HTTPStatus      int       `json:"http_status"`
	ErrorCode       string    `json:"error_code"`
	StartedAt       time.Time `json:"started_at"`
	CompletedAt     time.Time `json:"completed_at"`
}
type AdminCallDetailResponse struct {
	AdminCallResponse
	ProviderModelID string                `json:"provider_model_id"`
	ConnectionID    string                `json:"connection_id"`
	ErrorCode       string                `json:"error_code"`
	Attempts        []CallAttemptResponse `json:"attempts"`
	PriceETag       string                `json:"price_etag"`
	PricingSnapshot json.RawMessage       `json:"pricing_snapshot"`
}
type ListCallsRequest struct {
	Cursor  string `query:"cursor"`
	Limit   int    `query:"limit"`
	Status  string `query:"status"`
	ModelID string `query:"model_id"`
	KeyID   string `query:"key_id"`
	UserID  string `query:"user_id"`
	From    string `query:"from"`
	To      string `query:"to"`
}
type CallPath struct {
	RequestID string `uri:"request_id"`
}

func callResponse(record entity.CallRecord) CallResponse {
	return CallResponse{RequestID: record.RequestID, ModelID: record.ModelID, ModelName: record.ModelName, KeyID: record.KeyID, Protocol: record.Protocol, Status: record.Status, Stream: record.Stream, StartedAt: record.StartedAt, CompletedAt: record.CompletedAt, DurationMS: record.DurationMS, InputTokens: record.InputTokens, OutputTokens: record.OutputTokens, CacheReadTokens: record.CacheReadTokens, CacheWriteTokens: record.CacheWriteTokens, PricingStatus: record.PricingStatus, ChargeAmount: record.ChargeAmount, ChargeCurrency: record.ChargeCurrency}
}
func callFilter(request ListCallsRequest) (service.CallFilter, error) {
	filter := service.CallFilter{Cursor: request.Cursor, Limit: request.Limit, Status: request.Status, ModelID: request.ModelID, KeyID: request.KeyID, UserID: request.UserID}
	if request.From != "" {
		parsed, err := time.Parse(time.RFC3339Nano, request.From)
		if err != nil {
			return filter, apperrors.ErrBadRequest
		}
		filter.From = &parsed
	}
	if request.To != "" {
		parsed, err := time.Parse(time.RFC3339Nano, request.To)
		if err != nil {
			return filter, apperrors.ErrBadRequest
		}
		filter.To = &parsed
	}
	return filter, nil
}
func callCursor(cursor string) *string {
	if cursor == "" {
		return nil
	}
	return &cursor
}

func (ctrl *Ctrl) ListPersonalCalls(c *fox.Context, request ListCallsRequest) (*CallsResponse, error) {
	filter, err := callFilter(request)
	if err != nil {
		return nil, err
	}
	page, err := ctrl.service.ListCalls(c.Request.Context(), currentAuthentication(c).User.ID, filter)
	if err != nil {
		return nil, err
	}
	result := &CallsResponse{Items: []CallResponse{}, NextCursor: callCursor(page.NextCursor)}
	for _, record := range page.Records {
		result.Items = append(result.Items, callResponse(record))
	}
	return result, nil
}
func (ctrl *Ctrl) GetPersonalCall(c *fox.Context, request CallPath) (*CallResponse, error) {
	result, err := ctrl.service.GetCall(c.Request.Context(), currentAuthentication(c).User.ID, request.RequestID)
	if err != nil {
		return nil, err
	}
	response := callResponse(result.Record)
	return &response, nil
}
func (ctrl *Ctrl) ListAdminCalls(c *fox.Context, request ListCallsRequest) (*AdminCallsResponse, error) {
	filter, err := callFilter(request)
	if err != nil {
		return nil, err
	}
	page, err := ctrl.service.ListCalls(c.Request.Context(), "", filter)
	if err != nil {
		return nil, err
	}
	result := &AdminCallsResponse{Items: []AdminCallResponse{}, NextCursor: callCursor(page.NextCursor)}
	for _, record := range page.Records {
		result.Items = append(result.Items, AdminCallResponse{CallResponse: callResponse(record), UserID: record.UserID, ProjectID: record.ProjectID})
	}
	return result, nil
}
func (ctrl *Ctrl) GetAdminCall(c *fox.Context, request CallPath) (*AdminCallDetailResponse, error) {
	result, err := ctrl.service.GetCall(c.Request.Context(), "", request.RequestID)
	if err != nil {
		return nil, err
	}
	response := &AdminCallDetailResponse{AdminCallResponse: AdminCallResponse{CallResponse: callResponse(result.Record), UserID: result.Record.UserID, ProjectID: result.Record.ProjectID}, ProviderModelID: result.Record.ProviderModelID, ConnectionID: result.Record.ConnectionID, ErrorCode: result.Record.ErrorCode, Attempts: []CallAttemptResponse{}}
	response.PriceETag = result.Record.PriceETag
	if result.Record.PricingSnapshotJSON != nil {
		response.PricingSnapshot = json.RawMessage(*result.Record.PricingSnapshotJSON)
	}
	for _, attempt := range result.Attempts {
		response.Attempts = append(response.Attempts, CallAttemptResponse{ID: attempt.ID, ProviderModelID: attempt.ProviderModelID, ConnectionID: attempt.ConnectionID, Status: attempt.Status, HTTPStatus: attempt.HTTPStatus, ErrorCode: attempt.ErrorCode, StartedAt: attempt.StartedAt, CompletedAt: attempt.CompletedAt})
	}
	return response, nil
}
