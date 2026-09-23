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
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/upstream"
)

// GatewayError carries only information safe to expose to an inference caller.
type GatewayError struct {
	Status  int
	Code    string
	Message string
}

func (e *GatewayError) Error() string { return e.Message }

func gatewayError(status int, code, message string) *GatewayError {
	return &GatewayError{Status: status, Code: code, Message: message}
}

// GatewayModel is the public model identity, independent of a provider name.
type GatewayModel struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// GatewayResult describes one actual upstream attempt without storing its secret.
// The caller must close Response.Body when Response is non-nil, including errors.
type GatewayResult struct {
	SnapshotID       string
	Response         *http.Response
	ProjectID        string
	UserID           string
	KeyID            string
	ModelID          string
	ModelName        string
	ProviderID       string
	ProviderModelID  string
	ConnectionID     string
	CredentialID     string
	Stream           bool
	AttemptID        string
	AttemptStartedAt time.Time
}

func (s *Service) GatewayModels(ctx context.Context, bearer string) ([]GatewayModel, error) {
	key, err := s.AuthenticateAPIKey(ctx, bearer)
	if err != nil {
		return nil, gatewayAuthError(err)
	}
	models := []GatewayModel{}
	if len(key.ModelIDs) == 0 {
		return models, nil
	}
	if s.runtime != nil {
		auth := s.runtime.auth.Load()
		if auth == nil || !time.Now().Before(auth.ValidUntil) {
			return nil, gatewayError(503, "service_unavailable", "Authorization is temporarily unavailable.")
		}
		for _, name := range auth.Names {
			if name.CurrentModelID != nil && auth.Models[name.ModelID] && slices.Contains(key.ModelIDs, name.ModelID) {
				models = append(models, GatewayModel{ID: name.Name, Object: "model", Created: auth.ModelCreated[name.ModelID].Unix(), OwnedBy: "routex"})
			}
		}
		sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
		return models, nil
	}
	var rows []struct {
		Name      string
		CreatedAt time.Time
	}
	err = s.authDB(ctx).Table("models AS m").Select("n.name, m.created_at").Joins("JOIN model_names n ON n.current_model_id = m.id").Where("m.id IN ? AND m.status = ?", key.ModelIDs, "active").Order("n.name").Scan(&rows).Error
	if err != nil {
		return nil, gatewayError(503, "service_unavailable", "Model catalog is unavailable.")
	}
	for _, row := range rows {
		models = append(models, GatewayModel{ID: row.Name, Object: "model", Created: row.CreatedAt.Unix(), OwnedBy: "routex"})
	}
	return models, nil
}

// GatewayChat authorizes the current key/grant state and opens exactly one
// upstream request. It does not retry requests or buffer a streaming response.
func (s *Service) GatewayChat(ctx context.Context, bearer string, body []byte, requestID string) (*GatewayResult, error) {
	key, err := s.AuthenticateAPIKey(ctx, bearer)
	if err != nil {
		return nil, gatewayAuthError(err)
	}
	payload, publicName, stream, err := parseGatewayChat(body)
	result := &GatewayResult{UserID: key.Key.UserID, ProjectID: key.ProjectID, KeyID: key.Key.ID, ModelName: publicName, Stream: stream}
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
		route, credential, err = s.runtimeRoute(name.ModelID)
	} else {
		route, err = selectGatewayRoute(s.authDB(ctx), name.ModelID)
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
	result.SnapshotID = route.SnapshotID
	result.ProviderID, result.ProviderModelID = route.ProviderID, route.ProviderModelID
	result.ConnectionID, result.CredentialID = route.ConnectionID, route.CredentialID
	base, err := upstream.ValidateBaseURL(route.BaseURL, s.allowPrivateUpstream)
	if err != nil {
		return result, gatewayError(503, "upstream_unavailable", "No usable upstream is available.")
	}
	// The configured base includes the provider API prefix (for example /v1).
	endpoint := strings.TrimRight(base.String(), "/") + "/chat/completions"
	payload["model"], err = json.Marshal(route.UpstreamName)
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
	if err := s.AdmitGatewayCall(requestID, result); err != nil {
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
	return payload, model, stream, nil
}

type gatewayRoute struct {
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

func selectGatewayRoute(db *gorm.DB, modelID string) (*gatewayRoute, error) {
	var routes []gatewayRoute
	err := db.Table("model_provider_bindings AS b").Select("b.id AS binding_id, b.weight, p.id AS provider_model_id, p.upstream_name, c.id AS connection_id, c.provider_id, c.base_url").Joins("JOIN provider_models p ON p.id = b.provider_model_id").Joins("JOIN provider_connections c ON c.id = p.connection_id").Where("b.model_id = ? AND c.protocol = ?", modelID, entity.ProtocolOpenAIChat).Order("b.id").Scan(&routes).Error
	if err != nil {
		return nil, gatewayError(503, "upstream_unavailable", "Routing is temporarily unavailable.")
	}
	weights := make([]int, len(routes))
	for i := range routes {
		weights[i] = routes[i].Weight
	}
	choice, err := chooseGatewayRoute(weights)
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
	draw, err := rand.Int(rand.Reader, big.NewInt(int64(total)))
	if err != nil {
		return 0, gatewayError(503, "upstream_unavailable", "Routing is temporarily unavailable.")
	}
	position := int(draw.Int64())
	for i, weight := range weights {
		if position < weight {
			return i, nil
		}
		position -= weight
	}
	return 0, gatewayError(503, "upstream_unavailable", "No valid route is configured.")
}
