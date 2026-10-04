package handler

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/miclle/routex/internal/routex/service"
)

type teamNativeInputsKey struct{}
type teamNativeInputs struct {
	credentials, invalidQuery bool
	alt                       string
}

// TeamNativeInputs removes unsupported Key credentials before logger/recovery
// observes a native Team request, including nonexact routes and 404s. Only
// non-secret validation flags survive in request context.
func TeamNativeInputs(c *gin.Context) {
	if isTeamNativePath(c.Request.URL.Path) {
		captureTeamNativeInputs(c.Request)
	}
	c.Next()
}
func isTeamNativePath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 5 || !strings.EqualFold(parts[0], "api") || !strings.EqualFold(parts[1], "v1") || !strings.EqualFold(parts[2], "teams") {
		return false
	}
	// Invalid embedded slashes must not move a native suffix outside redaction.
	for _, part := range parts[4:] {
		switch strings.ToLower(part) {
		case "responses", "messages", "chat", "inference-models", "models", "attachments":
			return true
		}
	}
	return false
}
func captureTeamNativeInputs(request *http.Request) teamNativeInputs {
	if saved, ok := request.Context().Value(teamNativeInputsKey{}).(teamNativeInputs); ok {
		return saved
	}
	value := teamNativeInputs{}
	for _, name := range []string{"Authorization", "x-api-key", "x-goog-api-key"} {
		if len(request.Header.Values(name)) > 0 {
			value.credentials = true
		}
		request.Header.Del(name)
	}
	raw := request.URL.RawQuery
	request.URL.RawQuery = ""
	request.URL.ForceQuery = false
	request.RequestURI = strings.SplitN(request.RequestURI, "?", 2)[0]
	var query url.Values
	var err error
	if len(raw) <= 8192 {
		query, err = url.ParseQuery(raw)
	}
	value.invalidQuery = err != nil || len(raw) > 8192
	for key, entries := range query {
		if key == "alt" && len(entries) == 1 && entries[0] == "sse" {
			value.alt = "sse"
		} else {
			value.invalidQuery = true
		}
	}
	*request = *request.WithContext(context.WithValue(request.Context(), teamNativeInputsKey{}, value))
	return value
}
func rejectTeamNativeCredentials(request *http.Request) error {
	if captureTeamNativeInputs(request).credentials {
		return &service.GatewayError{Status: 403, Code: "session_access_denied", Message: "The session request is not permitted."}
	}
	return nil
}
func validateTeamNativeInputs(request *http.Request, stream bool) error {
	value := captureTeamNativeInputs(request)
	if err := rejectTeamNativeCredentials(request); err != nil {
		return err
	}
	if value.invalidQuery || !stream && value.alt != "" {
		return &service.GatewayError{Status: 400, Code: "invalid_request_error", Message: "Native query parameters are invalid."}
	}
	return nil
}
