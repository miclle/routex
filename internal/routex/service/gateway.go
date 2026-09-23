package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"math/big"
	"mime"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/upstream"
)

// GatewayError carries only information safe to expose to an inference caller.
type GatewayError struct {
	Status      int
	Code        string
	Message     string
	NativeError map[string]any
}

func (e *GatewayError) Error() string { return e.Message }

func gatewayError(status int, code, message string) *GatewayError {
	return &GatewayError{Status: status, Code: code, Message: message}
}

// GatewayModel is the public model identity, independent of a provider name.
type GatewayModel struct {
	ID        string   `json:"id"`
	Object    string   `json:"object"`
	Created   int64    `json:"created"`
	OwnedBy   string   `json:"owned_by"`
	Protocols []string `json:"protocols"`
}

// GatewayResult describes one actual upstream attempt without storing its secret.
// The caller must close Response.Body when Response is non-nil, including errors.
type GatewayResult struct {
	Protocol           string
	admissionLimits    []eventqueue.Limit
	PriceBasis         *CallPriceBasis
	PricingUnsupported bool
	PricingDimensions  []string
	SnapshotID         string
	Response           *http.Response
	ProjectID          string
	UserID             string
	KeyID              string
	ModelID            string
	ModelName          string
	ProviderID         string
	ProviderModelID    string
	ConnectionID       string
	CredentialID       string
	Stream             bool
	AttemptID          string
	AttemptStartedAt   time.Time
}

func (s *Service) GatewayModels(ctx context.Context, bearer string) ([]GatewayModel, error) {
	key, err := s.AuthenticateAPIKey(ctx, bearer)
	if err != nil {
		return nil, gatewayAuthError(err)
	}
	if _, err := s.gatewayLimits(ctx, &GatewayResult{UserID: key.Key.UserID, ProjectID: key.ProjectID, KeyID: key.Key.ID}); err != nil {
		return nil, err
	}
	models := []GatewayModel{}
	if len(key.ModelIDs) == 0 {
		return models, nil
	}
	protocols, err := s.gatewayProtocols(ctx, key.ModelIDs)
	if err != nil {
		return nil, gatewayAuthError(err)
	}
	if s.runtime != nil {
		auth := s.runtime.auth.Load()
		if auth == nil || !time.Now().Before(auth.ValidUntil) {
			return nil, gatewayError(503, "service_unavailable", "Authorization is temporarily unavailable.")
		}
		for _, name := range auth.Names {
			if name.CurrentModelID != nil && auth.Models[name.ModelID] && slices.Contains(key.ModelIDs, name.ModelID) {
				models = append(models, GatewayModel{ID: name.Name, Object: "model", Created: auth.ModelCreated[name.ModelID].Unix(), OwnedBy: "routex", Protocols: protocols[name.ModelID]})
			}
		}
		sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
		return models, nil
	}
	var rows []struct {
		ID        string
		Name      string
		CreatedAt time.Time
	}
	err = s.authDB(ctx).Table("models AS m").Select("m.id, n.name, m.created_at").Joins("JOIN model_names n ON n.current_model_id = m.id").Where("m.id IN ? AND m.status = ?", key.ModelIDs, "active").Order("n.name").Scan(&rows).Error
	if err != nil {
		return nil, gatewayError(503, "service_unavailable", "Model catalog is unavailable.")
	}
	for _, row := range rows {
		models = append(models, GatewayModel{ID: row.Name, Object: "model", Created: row.CreatedAt.Unix(), OwnedBy: "routex", Protocols: protocols[row.ID]})
	}
	return models, nil
}

// GatewayChat authorizes the current key/grant state and opens exactly one
// upstream request. It does not retry requests or buffer a streaming response.
func (s *Service) GatewayChat(ctx context.Context, bearer string, body []byte, requestID string) (*GatewayResult, error) {
	return s.gatewayNative(ctx, bearer, body, requestID, entity.ProtocolOpenAIChat)
}
func (result *GatewayResult) NativeProtocol() string {
	if result.Protocol == "" {
		return entity.ProtocolOpenAIChat
	}
	return result.Protocol
}
func (s *Service) gatewayNative(ctx context.Context, bearer string, body []byte, requestID, protocol string, options ...gatewayNativeOptions) (*GatewayResult, error) {
	key, err := s.AuthenticateAPIKey(ctx, bearer)
	if err != nil {
		return nil, gatewayAuthError(err)
	}
	parse := parseGatewayChat
	if protocol == entity.ProtocolOpenAIResponses {
		parse = parseGatewayResponses
	}
	if protocol == entity.ProtocolAnthropicMessages {
		parse = parseGatewayMessages
	}
	if protocol == entity.ProtocolGeminiGenerateContent {
		parse = func(raw []byte) (map[string]json.RawMessage, string, bool, error) {
			if len(options) != 1 {
				return nil, "", false, gatewayError(400, "invalid_request_error", "Native model path is required.")
			}
			return parseGatewayGemini(raw, options[0].GeminiModel, options[0].GeminiStream)
		}
	}
	payload, publicName, stream, err := parse(body)
	result := &GatewayResult{Protocol: protocol, UserID: key.Key.UserID, ProjectID: key.ProjectID, KeyID: key.Key.ID, ModelName: publicName, Stream: stream}
	if err != nil {
		return result, err
	}
	var name entity.ModelName
	if s.runtime != nil {
		var exists bool
		name, exists = s.runtimeModelName(publicName)
		if !exists {
			err = gorm.ErrRecordNotFound
		}
	} else {
		err = s.authDB(ctx).Where("name = ? AND (current_model_id IS NOT NULL OR expires_at > ?)", publicName, time.Now().UTC()).First(&name).Error
	}
	if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && !slices.Contains(key.ModelIDs, name.ModelID)) {
		return result, gatewayError(404, "model_not_found", "The requested model is unavailable or not permitted.")
	}
	if err != nil {
		return result, gatewayError(503, "service_unavailable", "Model catalog is unavailable.")
	}
	result.ModelID = name.ModelID
	var route *gatewayRoute
	var credential string
	if s.runtime != nil {
		route, credential, err = s.runtimeProtocolRoute(name.ModelID, protocol)
	} else {
		route, err = selectGatewayProtocolRoute(s.authDB(ctx), name.ModelID, protocol)
		if err == nil {
			if s.secrets == nil {
				err = runtimeUnavailable
			} else {
				credential, err = s.secrets.Open(route.CredentialID, route.Ciphertext)
			}
		}
	}
	if err != nil {
		return result, gatewayError(503, "upstream_unavailable", "No usable upstream is available.")
	}
	result.PriceBasis = clonePriceBasis(route.PriceBasis)
	result.PricingDimensions = pricingRequestDimensions(payload)
	if protocol == entity.ProtocolOpenAIResponses {
		result.PricingDimensions = responsesPricingDimensions(payload)
	}
	if protocol == entity.ProtocolAnthropicMessages {
		result.PricingDimensions = messagesPricingDimensions(payload)
	}
	if protocol == entity.ProtocolGeminiGenerateContent {
		result.PricingDimensions = geminiPricingDimensions(payload)
	}
	result.PricingUnsupported = len(result.PricingDimensions) != 0
	if s.runtime == nil {
		result.PriceBasis, err = s.capturePriceBasis(ctx, route.ProviderModelID, protocol)
		if err != nil {
			return result, gatewayError(503, "service_unavailable", "Pricing configuration is temporarily unavailable.")
		}
	}
	result.SnapshotID = route.SnapshotID
	result.ProviderID, result.ProviderModelID = route.ProviderID, route.ProviderModelID
	result.ConnectionID, result.CredentialID = route.ConnectionID, route.CredentialID
	base, err := upstream.ValidateBaseURL(route.BaseURL, s.allowPrivateUpstream)
	if err != nil {
		return result, gatewayError(503, "upstream_unavailable", "No usable upstream is available.")
	}
	// The configured base includes the provider API prefix (for example /v1).
	suffix := "/chat/completions"
	if protocol == entity.ProtocolOpenAIResponses {
		suffix = "/responses"
	}
	if protocol == entity.ProtocolAnthropicMessages {
		suffix = "/messages"
	}
	if protocol == entity.ProtocolGeminiGenerateContent {
		if !geminiModelSegment.MatchString(route.UpstreamName) {
			return result, gatewayError(503, "upstream_unavailable", "The upstream model path is unavailable.")
		}
		suffix = "/models/" + route.UpstreamName + ":generateContent"
		if stream {
			suffix = "/models/" + route.UpstreamName + ":streamGenerateContent?alt=sse"
		}
	}
	endpoint := strings.TrimRight(base.String(), "/") + suffix
	if protocol != entity.ProtocolGeminiGenerateContent {
		payload["model"], err = json.Marshal(route.UpstreamName)
	}
	if err != nil {
		return result, gatewayError(500, "internal_error", "The request could not be prepared.")
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return result, gatewayError(400, "invalid_request_error", "The request must be a valid JSON object.")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return result, gatewayError(503, "upstream_unavailable", "No usable upstream is available.")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+credential)
	if protocol == entity.ProtocolAnthropicMessages {
		if len(options) != 1 {
			return result, gatewayError(400, "invalid_request_error", "Native headers are required.")
		}
		req.Header.Del("Authorization")
		req.Header.Set("x-api-key", credential)
		req.Header.Set("anthropic-version", options[0].Messages.Version)
		if options[0].Messages.Beta != "" {
			req.Header.Set("anthropic-beta", options[0].Messages.Beta)
		}
	}
	if protocol == entity.ProtocolGeminiGenerateContent {
		req.Header.Del("Authorization")
		req.Header.Set("x-goog-api-key", credential)
	}
	req.Header.Set("X-Request-ID", requestID)
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	client := *s.upstream
	if stream {
		// Handler owns the bounded streaming context; a shallow clone preserves
		// the guarded shared transport while removing the total 30-second limit.
		client.Timeout = 0
	}
	if err := s.admitLimitedGatewayCall(ctx, requestID, result); err != nil {
		return result, err
	}
	result.AttemptID, err = id.NewPrefixed("att")
	if err != nil {
		return result, gatewayError(500, "internal_error", "The request could not be initialized.")
	}
	result.AttemptStartedAt = time.Now().UTC()
	response, err := client.Do(req)
	result.Response = response
	if err != nil {
		var timedOut interface{ Timeout() bool }
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || (errors.As(err, &timedOut) && timedOut.Timeout()) {
			return result, gatewayError(504, "upstream_timeout", "The upstream request timed out.")
		}
		return result, gatewayError(502, "upstream_error", "The upstream request failed.")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if protocol == entity.ProtocolGeminiGenerateContent {
			return result, nativeGeminiHTTPError(response)
		}
		if protocol == entity.ProtocolAnthropicMessages {
			return result, nativeMessagesHTTPError(response)
		}
		if protocol == entity.ProtocolOpenAIResponses {
			return result, nativeResponsesHTTPError(response)
		}
		status, code := 502, "upstream_error"
		switch response.StatusCode {
		case 429:
			status, code = 429, "rate_limit_exceeded"
		case 408, 504:
			status, code = 504, "upstream_timeout"
		}
		return result, gatewayError(status, code, "The upstream could not complete the request.")
	}
	contentType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || (stream && contentType != "text/event-stream") || (!stream && contentType != "application/json") {
		return result, gatewayError(502, "invalid_upstream_response", "The upstream returned an invalid response.")
	}
	return result, nil
}

func gatewayAuthError(err error) error {
	var app *apperrors.Error
	if errors.As(err, &app) && app.Code == http.StatusUnauthorized {
		return gatewayError(401, "invalid_api_key", "A valid active API key is required.")
	}
	return gatewayError(503, "service_unavailable", "Authorization is temporarily unavailable.")
}

func parseGatewayChat(body []byte) (map[string]json.RawMessage, string, bool, error) {
	invalid := gatewayError(400, "invalid_request_error", "A model and a nonempty messages array are required.")
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil || payload == nil {
		return nil, "", false, invalid
	}
	var model string
	if err := json.Unmarshal(payload["model"], &model); err != nil || !publicModelName.MatchString(model) {
		return nil, "", false, invalid
	}
	var messages []json.RawMessage
	if err := json.Unmarshal(payload["messages"], &messages); err != nil || len(messages) == 0 {
		return nil, model, false, invalid
	}
	stream := false
	if raw, exists := payload["stream"]; exists {
		if !bytes.Equal(raw, []byte("true")) && !bytes.Equal(raw, []byte("false")) {
			return nil, model, false, invalid
		}
		stream = bytes.Equal(raw, []byte("true"))
	}
	if stream {
		options := map[string]json.RawMessage{}
		if raw, exists := payload["stream_options"]; exists && !bytes.Equal(raw, []byte("null")) {
			if json.Unmarshal(raw, &options) != nil || options == nil {
				return nil, model, stream, invalid
			}
		}
		options["include_usage"] = json.RawMessage("true")
		encoded, err := json.Marshal(options)
		if err != nil {
			return nil, model, stream, invalid
		}
		payload["stream_options"] = encoded
	}
	return payload, model, stream, nil
}

type gatewayRoute struct {
	Protocol        string
	Disabled        bool
	PriceBasis      *CallPriceBasis `gorm:"-"`
	SnapshotID      string
	BindingID       string
	Weight          int
	ProviderID      string
	ProviderModelID string
	ConnectionID    string
	CredentialID    string
	Ciphertext      string
	UpstreamName    string
	BaseURL         string
}

func selectGatewayProtocolRoute(db *gorm.DB, modelID, protocol string) (*gatewayRoute, error) {
	var routes []gatewayRoute
	err := db.Table("model_provider_bindings AS b").Select("b.id AS binding_id, b.weight, p.id AS provider_model_id, p.upstream_name, p.disabled, c.id AS connection_id, c.provider_id, c.base_url, c.protocol").Joins("JOIN provider_models p ON p.id = b.provider_model_id").Joins("JOIN provider_connections c ON c.id = p.connection_id").Where("b.model_id = ? AND c.protocol = ?", modelID, protocol).Order("b.id").Scan(&routes).Error
	if err != nil {
		return nil, gatewayError(503, "upstream_unavailable", "Routing is temporarily unavailable.")
	}
	weights := make([]int, len(routes))
	available := make([]bool, len(routes))
	for i := range routes {
		weights[i] = routes[i].Weight
		available[i] = !routes[i].Disabled
	}
	choice, err := chooseAvailableGatewayRoute(weights, available)
	if err != nil {
		return nil, err
	}
	route := &routes[choice]
	var credential entity.ProviderCredential
	err = db.Table("provider_credentials AS c").Select("c.*").Joins("JOIN credential_model_accesses a ON a.credential_id = c.id").Where("c.connection_id = ? AND c.enabled = ? AND c.verification_status = ? AND a.provider_model_id = ?", route.ConnectionID, true, "verified", route.ProviderModelID).Order("c.priority, c.created_at, c.id").First(&credential).Error
	if err != nil {
		return nil, gatewayError(503, "upstream_unavailable", "No usable upstream is available.")
	}
	route.CredentialID, route.Ciphertext = credential.ID, credential.Ciphertext
	return route, nil
}

func chooseGatewayRoute(weights []int) (int, error) {
	available := make([]bool, len(weights))
	for i := range available {
		available[i] = true
	}
	return chooseAvailableGatewayRoute(weights, available)
}

// Keep configured weights intact, drawing only among explicitly enabled supply.
func chooseAvailableGatewayRoute(weights []int, available []bool) (int, error) {
	if len(weights) != len(available) {
		return 0, gatewayError(503, "upstream_unavailable", "No valid route is configured.")
	}
	total := 0
	for _, weight := range weights {
		if weight < 0 || weight > 100 {
			return 0, gatewayError(503, "upstream_unavailable", "No valid route is configured.")
		}
		total += weight
	}
	if total != 100 {
		return 0, gatewayError(503, "upstream_unavailable", "No valid route is configured.")
	}
	eligible := 0
	for i, weight := range weights {
		if available[i] {
			eligible += weight
		}
	}
	if eligible == 0 {
		return 0, gatewayError(503, "upstream_unavailable", "No usable upstream is available.")
	}
	draw, err := rand.Int(rand.Reader, big.NewInt(int64(eligible)))
	if err != nil {
		return 0, gatewayError(503, "upstream_unavailable", "Routing is temporarily unavailable.")
	}
	position := int(draw.Int64())
	for i, weight := range weights {
		if !available[i] {
			continue
		}
		if position < weight {
			return i, nil
		}
		position -= weight
	}
	return 0, gatewayError(503, "upstream_unavailable", "No valid route is configured.")
}
