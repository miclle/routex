package handler

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/miclle/routex/internal/routex/service"
)

type geminiCredentialsKey struct{}
type geminiCredentials struct {
	bearer, alt string
	invalid     bool
}

// GeminiQueryCredentials must run before native route dispatch, including 404s.
// Credentials leave the URL and headers before logger/recovery middleware sees
// the completed request. Nothing from the original query appears in errors.
func GeminiQueryCredentials(c *gin.Context) {
	if c.Request.URL.Path != "/v1beta" && !strings.HasPrefix(c.Request.URL.Path, "/v1beta/") {
		c.Next()
		return
	}
	captureGeminiCredentials(c.Request)
	c.Next()
}
func captureGeminiCredentials(request *http.Request) {
	if request.Context().Value(geminiCredentialsKey{}) != nil {
		return
	}
	raw := request.URL.RawQuery
	request.URL.RawQuery = ""
	request.URL.ForceQuery = false
	request.RequestURI = strings.SplitN(request.RequestURI, "?", 2)[0]
	native := request.Header.Values("x-goog-api-key")
	auth := request.Header.Values("Authorization")
	bearer := gatewayBearer(request)
	request.Header.Del("x-goog-api-key")
	request.Header.Del("Authorization")
	var query url.Values
	var err error
	if len(raw) <= 8192 {
		query, err = url.ParseQuery(raw)
	}
	credentials := geminiCredentials{invalid: err != nil || len(raw) > 8192 || len(native) > 1 || len(auth) > 1}
	forms := 0
	if len(native) > 0 {
		forms++
		credentials.bearer = native[0]
		if native[0] == "" {
			credentials.invalid = true
		}
	}
	if len(auth) > 0 {
		forms++
		credentials.bearer = bearer
		if bearer == "" {
			credentials.invalid = true
		}
	}
	if keys, ok := query["key"]; ok {
		forms++
		if len(keys) != 1 || keys[0] == "" {
			credentials.invalid = true
		} else {
			credentials.bearer = keys[0]
		}
	}
	if forms > 1 {
		credentials.invalid = true
	}
	for key, values := range query {
		switch key {
		case "key":
		case "alt":
			if len(values) != 1 || values[0] != "sse" {
				credentials.invalid = true
			} else {
				credentials.alt = "sse"
			}
		default:
			credentials.invalid = true
		}
	}
	requestWithContext := request.WithContext(context.WithValue(request.Context(), geminiCredentialsKey{}, credentials))
	*request = *requestWithContext
}
func geminiRequestCredentials(request *http.Request, stream bool) (string, error) {
	captureGeminiCredentials(request)
	credentials := request.Context().Value(geminiCredentialsKey{}).(geminiCredentials)
	if credentials.invalid || (!stream && credentials.alt != "") {
		return "", &service.GatewayError{Status: 400, Code: "invalid_request_error", Message: "Native authentication or query parameters are invalid."}
	}
	return credentials.bearer, nil
}
