package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var errAzureChat = errors.New("invalid Azure classic Chat request")

// ValidAzureDeployment is the finite deployment path segment supported by this adapter.
func ValidAzureDeployment(value string) bool {
	if len(value) == 0 || len(value) > 255 || value == "." || value == ".." {
		return false
	}
	for _, c := range []byte(value) {
		if c != '_' && c != '-' && c != '.' && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// ValidAzureAPIVersion validates an explicit dated version, without choosing one.
func ValidAzureAPIVersion(value string) bool {
	date := strings.TrimSuffix(value, "-preview")
	if len(date) != 10 || len(value) > 18 {
		return false
	}
	parsed, err := time.Parse("2006-01-02", date)
	return err == nil && parsed.Year() > 0 && parsed.Format("2006-01-02") == date
}

// AzureClassicOrigin applies the ordinary outbound policy and requires a resource origin.
func AzureClassicOrigin(raw string, allowPrivate bool) (*url.URL, error) {
	u, err := ValidateBaseURL(raw, allowPrivate)
	if err != nil || u.RawPath != "" || u.Path != "" && u.Path != "/" {
		return nil, errAzureChat
	}
	u.Path = ""
	return u, nil
}

// NewAzureChatRequest only constructs a request. The caller owns the non-replaying
// transport, native parser, cancellation, accounting and response bounds.
func NewAzureChatRequest(ctx context.Context, endpoint, deployment, version, key string, payload map[string]json.RawMessage, allowPrivate bool) (*http.Request, error) {
	u, err := AzureClassicOrigin(endpoint, allowPrivate)
	if err != nil || !ValidAzureDeployment(deployment) || !ValidAzureAPIVersion(version) || len(key) == 0 || len(key) > 2048 {
		return nil, errAzureChat
	}
	for _, c := range []byte(key) {
		if c < 33 || c > 126 {
			return nil, errAzureChat
		}
	}
	if payload == nil {
		return nil, errAzureChat
	}
	stream := false
	if raw, exists := payload["stream"]; exists && (string(raw) == "null" || json.Unmarshal(raw, &stream) != nil) {
		return nil, errAzureChat
	}
	body := make(map[string]json.RawMessage, len(payload))
	for name, raw := range payload {
		if name != "model" {
			body[name] = raw
		}
	}
	encoded, err := json.Marshal(body)
	if err != nil || len(encoded) > 4<<20 {
		return nil, errAzureChat
	}
	u.Path = "/openai/deployments/" + deployment + "/chat/completions"
	u.RawQuery = url.Values{"api-version": []string{version}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(encoded))
	if err != nil {
		return nil, errAzureChat
	}
	req.GetBody = nil
	req.Header.Set("api-key", key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	return req, nil
}
