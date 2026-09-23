package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/miclle/routex/internal/routex/entity"
)

// Discovery commits no partial pages. Its caller owns the overall timeout.
func (s *Service) discoverMessagesModels(ctx context.Context, connection entity.ProviderConnection, plaintext string, client *http.Client) ([]string, bool) {
	const maxBytes = 2 << 20
	names := []string{}
	seen := map[string]bool{}
	cursors := map[string]bool{}
	cursor := ""
	remaining := maxBytes
	for page := 0; page < 20; page++ {
		endpoint, err := url.Parse(strings.TrimRight(connection.BaseURL, "/") + "/models")
		if err != nil {
			return nil, false
		}
		query := endpoint.Query()
		query.Set("limit", "1000")
		if cursor != "" {
			query.Set("after_id", cursor)
		}
		endpoint.RawQuery = query.Encode()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
		if err != nil {
			return nil, false
		}
		req.Header.Set("x-api-key", plaintext)
		req.Header.Set("anthropic-version", "2023-06-01")
		req.Header.Set("Accept", "application/json")
		response, err := client.Do(req)
		if err != nil {
			return nil, false
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, int64(remaining)+1))
		_ = response.Body.Close()
		if readErr != nil || response.StatusCode != http.StatusOK || len(body) > remaining {
			return nil, false
		}
		remaining -= len(body)
		var payload struct {
			Data []struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			} `json:"data"`
			HasMore *bool   `json:"has_more"`
			LastID  *string `json:"last_id"`
		}
		if json.Unmarshal(body, &payload) != nil || payload.Data == nil || payload.HasMore == nil || len(payload.Data) > 1000 {
			return nil, false
		}
		for _, item := range payload.Data {
			if item.Type != "model" || !validUpstreamName(item.ID) {
				return nil, false
			}
			if !seen[item.ID] {
				names = append(names, item.ID)
				seen[item.ID] = true
			}
			if len(names) > 2000 {
				return nil, false
			}
		}
		if !*payload.HasMore {
			return names, true
		}
		if payload.LastID == nil || !validUpstreamName(*payload.LastID) || len(payload.Data) == 0 || payload.Data[len(payload.Data)-1].ID != *payload.LastID || cursors[*payload.LastID] {
			return nil, false
		}
		cursor = *payload.LastID
		cursors[cursor] = true
	}
	return nil, false
}
