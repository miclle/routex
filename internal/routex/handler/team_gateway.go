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
	if err := rejectTeamNativeCredentials(c.Request); err != nil {
		return nil, requestID, err
	}
	for _, header := range []string{"anthropic-workspace-id", "anthropic-user-profile-id", "OpenAI-Organization", "OpenAI-Project", "X-Team-ID", "X-User-ID", "X-Project-ID", "X-Workspace-ID"} {
		if len(c.Request.Header.Values(header)) > 0 {
			return nil, requestID, denied
		}
	}
	if len(c.Request.Header.Values("Origin")) > 1 || len(c.Request.Header.Values("Sec-Fetch-Site")) > 1 {
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
	if err := validateTeamNativeInputs(c.Request, false); err != nil {
		writeGatewayError(c, err)
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
	if err := validateTeamNativeInputs(c.Request, false); err != nil {
		writeGatewayError(c, err)
		return
	}
	ctx, body, cancel, err := ctrl.teamGatewayRequest(c)
	if err != nil {
		writeGatewayError(c, err)
		return
	}
	defer cancel()
	result, callErr := ctrl.service.TeamGatewayChat(ctx, identity, body, requestID)
	ctrl.finishGatewayChat(c, ctx, requestID, started, body, result, callErr)
}

// All native Team transports retain the same request/response bounds and timeout
// as their Key counterparts. Authentication is supplied only by the Team path.
func (ctrl *Ctrl) teamGatewayRequest(c *fox.Context) (context.Context, []byte, context.CancelFunc, error) {
	clientIP, err := ctrl.service.GatewayClientIP(c.Request)
	if err != nil {
		return nil, nil, nil, &service.GatewayError{Status: 400, Code: "invalid_request_error", Message: "A valid client network address is required."}
	}
	mediaType, _, err := mime.ParseMediaType(c.Request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return nil, nil, nil, &service.GatewayError{Status: 415, Code: "invalid_request_error", Message: "Content-Type must be application/json."}
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, gatewayRequestLimit))
	if err != nil {
		status := 400
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			status = 413
		}
		return nil, nil, nil, &service.GatewayError{Status: status, Code: "invalid_request_error", Message: "The request body is invalid or too large."}
	}
	ctx, cancel := context.WithTimeout(service.WithGatewayClientIP(c.Request.Context(), clientIP), 5*time.Minute)
	return ctx, body, cancel, nil
}

func (ctrl *Ctrl) TeamGatewayResponses(c *fox.Context) {
	started := time.Now().UTC()
	identity, requestID, err := ctrl.prepareTeamGateway(c, true)
	if err == nil {
		err = validateTeamNativeInputs(c.Request, false)
	}
	if err != nil {
		writeGatewayError(c, err)
		return
	}
	ctx, body, cancel, err := ctrl.teamGatewayRequest(c)
	if err != nil {
		writeGatewayError(c, err)
		return
	}
	defer cancel()
	result, callErr := ctrl.service.TeamGatewayResponses(ctx, identity, body, requestID)
	ctrl.finishGatewayResponses(c, ctx, requestID, started, result, callErr)
}

func (ctrl *Ctrl) TeamGatewayMessages(c *fox.Context) {
	started := time.Now().UTC()
	identity, requestID, err := ctrl.prepareTeamGateway(c, true)
	c.Header("request-id", requestID)
	if err == nil {
		err = validateTeamNativeInputs(c.Request, false)
	}
	if err != nil {
		writeMessagesError(c, err, requestID)
		return
	}
	_, headers, err := messagesRequestHeaders(c.Request)
	if err != nil {
		writeMessagesError(c, err, requestID)
		return
	}
	ctx, body, cancel, err := ctrl.teamGatewayRequest(c)
	if err != nil {
		writeMessagesError(c, err, requestID)
		return
	}
	defer cancel()
	result, callErr := ctrl.service.TeamGatewayMessages(ctx, identity, body, requestID, headers)
	ctrl.finishGatewayMessages(c, ctx, requestID, started, result, callErr)
}

func (ctrl *Ctrl) TeamGatewayGemini(c *fox.Context) {
	started := time.Now().UTC()
	identity, requestID, err := ctrl.prepareTeamGateway(c, true)
	if err != nil {
		writeGeminiError(c, err, requestID)
		return
	}
	model, stream, err := service.GeminiAction(c.Param("model_action"))
	if err == nil {
		err = validateTeamNativeInputs(c.Request, stream)
	}
	if err != nil {
		writeGeminiError(c, err, requestID)
		return
	}
	ctx, body, cancel, err := ctrl.teamGatewayRequest(c)
	if err != nil {
		writeGeminiError(c, err, requestID)
		return
	}
	defer cancel()
	result, callErr := ctrl.service.TeamGatewayGemini(ctx, identity, body, requestID, model, stream)
	ctrl.finishGatewayGemini(c, ctx, requestID, started, body, result, callErr)
}
