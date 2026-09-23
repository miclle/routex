// Package handler provides HTTP handlers and route registration.
package handler

import (
	"github.com/fox-gonic/fox"

	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/website"
)

// Ctrl is the controller that holds service dependencies and registers routes.
type Ctrl struct {
	service *service.Service
}

// New creates a new Ctrl instance.
func New(svc *service.Service) *Ctrl {
	return &Ctrl{
		service: svc,
	}
}

// RegisterRoutes registers all API routes on the given engine.
func (ctrl *Ctrl) RegisterRoutes(r *fox.Engine) {
	r.RenderErrorFunc = renderAPIError
	// embed website assets
	website.EmbedAssets(r)

	// ── Health check ────────────────────────────────────────────────────
	r.GET("/health", ctrl.Health)

	// ── API routes ──────────────────────────────────────────────────────
	api := r.Group("/api/v1")
	api.GET("/hello", ctrl.Hello)

	identity := api.Group("")
	identity.Use(authResponseHeaders)
	identity.GET("/setup", ctrl.SetupStatus)
	identity.POST("/setup", sameOrigin, jsonAuthRequest, ctrl.Initialize)
	identity.POST("/auth/login", sameOrigin, jsonAuthRequest, ctrl.Login)
	identity.GET("/auth/session", ctrl.requireSession, ctrl.CurrentSession)
	identity.POST("/auth/logout", sameOrigin, ctrl.requireSession, requireCSRF, ctrl.Logout)
	identity.GET("/admin/status", ctrl.requireSession, requireAdmin, ctrl.AdminStatus)
}

// Health returns a simple health check response.
func (ctrl *Ctrl) Health(c *fox.Context) string {
	return "ok"
}

// Hello returns a greeting message.
func (ctrl *Ctrl) Hello(c *fox.Context) any {
	return map[string]string{"message": "Hello from RouteX!"}
}
