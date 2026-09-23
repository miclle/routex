package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/miclle/routex/internal/routex/entity"
)

func (s *Service) discoverGeminiModels(ctx context.Context, connection entity.ProviderConnection, plaintext string, client *http.Client) ([]string, bool) {
	names := []string{}
	seen := map[string]bool{}
	cursors := map[string]bool{}
	cursor := ""
	remaining := 2 << 20
	for page := 0; page < 20; page++ {
		endpoint, err := url.Parse(strings.TrimRight(connection.BaseURL, "/") + "/models")
		if err != nil {
			return nil, false
		}
		query := endpoint.Query()
		query.Set("pageSize", "1000")
		if cursor != "" {
			query.Set("pageToken", cursor)
		}
		endpoint.RawQuery = query.Encode()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
		if err != nil {
			return nil, false
		}
		request.Header.Set("x-goog-api-key", plaintext)
		request.Header.Set("Accept", "application/json")
		response, err := client.Do(request)
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
			Models []struct {
				Name    string   `json:"name"`
				Methods []string `json:"supportedGenerationMethods"`
			} `json:"models"`
			Next string `json:"nextPageToken"`
		}
		if json.Unmarshal(body, &payload) != nil || payload.Models == nil || len(payload.Models) > 1000 {
			return nil, false
		}
		for _, model := range payload.Models {
			name, ok := strings.CutPrefix(model.Name, "models/")
			if !ok || !geminiModelSegment.MatchString(name) {
				return nil, false
			}
			if !seen[name] {
				seen[name] = true
				if slices.Contains(model.Methods, "generateContent") {
					names = append(names, name)
				}
			}
			if len(seen) > 2000 {
				return nil, false
			}
		}
		if payload.Next == "" {
			return names, true
		}
		if len(payload.Next) > 4096 || strings.ContainsAny(payload.Next, "\r\n") || cursors[payload.Next] || len(payload.Models) == 0 {
			return nil, false
		}
		cursor = payload.Next
		cursors[cursor] = true
	}
	return nil, false
}
