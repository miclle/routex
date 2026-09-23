package handler

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/fox-gonic/fox"
	"github.com/fox-gonic/fox/httperrors"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/httperr"
)

const sessionCookie = "routex_session"
const authenticationKey = "routex.authentication"

type SetupStatusResponse struct {
	Initialized bool `json:"initialized"`
}
type SetupRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}
type UserResponse struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
}
type SessionResponse struct {
	User      UserResponse `json:"user"`
	CSRFToken string       `json:"csrf_token"`
}
type ErrorResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func renderAPIError(c *fox.Context, err error) {
	code, message := http.StatusInternalServerError, "internal server error"
	var appErr *apperrors.Error
	var httpErr *httperr.StatusError
	var bindingErr *httperrors.Error
	switch {
	case errors.As(err, &appErr):
		code, message = appErr.Code, appErr.Message
	case errors.As(err, &httpErr):
		code, message = httpErr.Code, httpErr.Message
	case errors.As(err, &bindingErr) && bindingErr.HTTPCode == http.StatusBadRequest:
		code, message = http.StatusBadRequest, "bad request"
	}
	c.JSON(code, ErrorResponse{Code: code, Message: message})
	c.Abort()
}

func (ctrl *Ctrl) SetupStatus(c *fox.Context) (*SetupStatusResponse, error) {
	initialized, err := ctrl.service.Initialized(c.Request.Context())
	if err != nil {
		return nil, err
	}
	return &SetupStatusResponse{Initialized: initialized}, nil
}

func (ctrl *Ctrl) Initialize(c *fox.Context, request SetupRequest) error {
	auth, err := ctrl.service.Initialize(c.Request.Context(), request.Email, request.Password, request.Name)
	if err != nil {
		return err
	}
	setSessionCookie(c, auth)
	// fox v0.1.2 overwrites c.Status with 200 when automatically rendering a
	// DTO. Explicit JSON preserves the creation status with the same typed DTO.
	c.JSON(http.StatusCreated, sessionResponse(auth))
	return nil
}

func (ctrl *Ctrl) Login(c *fox.Context, request LoginRequest) (*SessionResponse, error) {
	auth, err := ctrl.service.Login(c.Request.Context(), request.Email, request.Password)
	if err != nil {
		return nil, err
	}
	setSessionCookie(c, auth)
	return sessionResponse(auth), nil
}

func (ctrl *Ctrl) CurrentSession(c *fox.Context) *SessionResponse {
	return sessionResponse(currentAuthentication(c))
}

func (ctrl *Ctrl) Logout(c *fox.Context) error {
	if err := ctrl.service.Logout(c.Request.Context(), currentAuthentication(c)); err != nil {
		return err
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, Secure: c.Request.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	c.Status(http.StatusNoContent)
	return nil
}

func (ctrl *Ctrl) AdminStatus(c *fox.Context) SetupStatusResponse {
	return SetupStatusResponse{Initialized: true}
}

func sessionResponse(auth *service.Authentication) *SessionResponse {
	return &SessionResponse{User: UserResponse{ID: auth.User.ID, Email: auth.User.Email, Name: auth.User.Name, Role: auth.User.Role}, CSRFToken: auth.CSRFToken()}
}

func setSessionCookie(c *fox.Context, auth *service.Authentication) {
	// Only transport TLS is trusted. Forwarded headers are untrusted until an
	// explicit deployment proxy trust configuration is introduced.
	http.SetCookie(c.Writer, &http.Cookie{Name: sessionCookie, Value: auth.Token, Path: "/", HttpOnly: true, Secure: c.Request.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: int(service.SessionLifetime.Seconds()), Expires: auth.Session.ExpiresAt})
}

func authResponseHeaders(c *fox.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Next()
}

// sameOrigin protects login and first initialization before a session exists.
// Non-browser clients may omit Origin; browsers must match the transport origin.
func sameOrigin(c *fox.Context) error {
	origin := c.Request.Header.Get("Origin")
	if origin != "" {
		u, err := url.Parse(origin)
		scheme := "http"
		if c.Request.TLS != nil {
			scheme = "https"
		}
		if err != nil || u.Scheme != scheme || !strings.EqualFold(u.Host, c.Request.Host) || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return apperrors.ErrForbidden
		}
	}
	if site := c.Request.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return apperrors.ErrForbidden
	}
	c.Next()
	return nil
}

func jsonAuthRequest(c *fox.Context) error {
	if c.ContentType() != "application/json" {
		return apperrors.ErrBadRequest
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	c.Next()
	return nil
}

func (ctrl *Ctrl) requireSession(c *fox.Context) error {
	cookie, err := c.Request.Cookie(sessionCookie)
	if err != nil {
		return apperrors.ErrUnauthorized
	}
	auth, err := ctrl.service.Authenticate(c.Request.Context(), cookie.Value)
	if err != nil {
		return err
	}
	c.Set(authenticationKey, auth)
	c.Next()
	return nil
}

func currentAuthentication(c *fox.Context) *service.Authentication {
	return c.MustGet(authenticationKey).(*service.Authentication)
}

func requireAdmin(c *fox.Context) error {
	if currentAuthentication(c).User.Role != entity.RoleAdmin {
		return apperrors.ErrForbidden
	}
	c.Next()
	return nil
}

func requireCSRF(c *fox.Context) error {
	if !currentAuthentication(c).CheckCSRF(c.Request.Header.Get("X-CSRF-Token")) {
		return apperrors.ErrForbidden
	}
	c.Next()
	return nil
}
