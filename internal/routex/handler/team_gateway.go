package handler

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/service"
)

// Team native endpoints authenticate only the immutable published Session view.
// They never borrow a database connection through the control-plane middleware.
func (ctrl *Ctrl) prepareTeamGateway(c *fox.Context, csrf bool) (*service.TeamSessionIdentity, string, error) {
	requestID, err := prepareGateway(c)
	if err != nil {
		return nil, "", err
	}
	denied := &service.GatewayError{Status: 403, Code: "session_access_denied", Message: "The session request is not permitted."}
	if len(c.Request.Header.Values("Authorization")) != 0 || len(c.Request.Header.Values("Origin")) > 1 || len(c.Request.Header.Values("Sec-Fetch-Site")) > 1 {
		return nil, requestID, denied
	}
	origin := c.Request.Header.Get("Origin")
	if origin != "" {
		parsed, err := url.Parse(origin)
		scheme := "http"
		if c.Request.TLS != nil {
			scheme = "https"
		}
		if err != nil || parsed.Scheme != scheme || !strings.EqualFold(parsed.Host, c.Request.Host) || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return nil, requestID, denied
		}
	}
	if site := c.Request.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return nil, requestID, denied
	}
	cookies := []*http.Cookie{}
	for _, cookie := range c.Request.Cookies() {
		if cookie.Name == sessionCookie {
			cookies = append(cookies, cookie)
		}
	}
	if len(cookies) != 1 {
		return nil, requestID, &service.GatewayError{Status: 401, Code: "invalid_session", Message: "A current active session is required."}
	}
	identity, err := ctrl.service.RuntimeAuthenticateTeamSession(c.Request.Context(), cookies[0].Value, c.Param("team_id"))
	if err != nil {
		return nil, requestID, err
	}
	if csrf && (len(c.Request.Header.Values("X-CSRF-Token")) != 1 || !service.ValidateTeamSessionCSRF(identity, c.Request.Header.Get("X-CSRF-Token"))) {
		return nil, requestID, denied
	}
	return identity, requestID, nil
}

func (ctrl *Ctrl) TeamGatewayModels(c *fox.Context) {
	identity, _, err := ctrl.prepareTeamGateway(c, false)
	if err != nil {
		writeGatewayError(c, err)
		return
	}
	if c.Request.URL.RawQuery != "" {
		writeGatewayError(c, &service.GatewayError{Status: 400, Code: "invalid_request_error", Message: "Query parameters are not supported."})
		return
	}
	models, err := ctrl.service.TeamGatewayModels(c.Request.Context(), identity)
	if err != nil {
		writeGatewayError(c, err)
		return
	}
	c.JSON(http.StatusOK, map[string]any{"object": "list", "data": models})
}

func (ctrl *Ctrl) TeamGatewayChat(c *fox.Context) {
	started := time.Now().UTC()
	identity, requestID, err := ctrl.prepareTeamGateway(c, true)
	if err != nil {
		writeGatewayError(c, err)
		return
	}
	if c.Request.URL.RawQuery != "" {
		writeGatewayError(c, &service.GatewayError{Status: 400, Code: "invalid_request_error", Message: "Query parameters are not supported."})
		return
	}
	clientIP, err := ctrl.service.GatewayClientIP(c.Request)
	if err != nil {
		writeGatewayError(c, &service.GatewayError{Status: 400, Code: "invalid_request_error", Message: "A valid client network address is required."})
		return
	}
	mediaType, _, err := mime.ParseMediaType(c.Request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeGatewayError(c, &service.GatewayError{Status: 415, Code: "invalid_request_error", Message: "Content-Type must be application/json."})
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, gatewayRequestLimit))
	if err != nil {
		status := 400
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			status = 413
		}
		writeGatewayError(c, &service.GatewayError{Status: status, Code: "invalid_request_error", Message: "The request body is invalid or too large."})
		return
	}
	ctx, cancel := context.WithTimeout(service.WithGatewayClientIP(c.Request.Context(), clientIP), 5*time.Minute)
	defer cancel()
	result, callErr := ctrl.service.TeamGatewayChat(ctx, identity, body, requestID)
	ctrl.finishGatewayChat(c, ctx, requestID, started, body, result, callErr)
}
