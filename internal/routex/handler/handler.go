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
	r.GET("/v1/models", ctrl.GatewayModels)
	r.POST("/v1/chat/completions", ctrl.GatewayChat)

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

	identity.GET("/calls", ctrl.requireSession, ctrl.ListPersonalCalls)
	identity.GET("/calls/:request_id", ctrl.requireSession, ctrl.GetPersonalCall)
	identity.GET("/account/sessions", ctrl.requireSession, ctrl.ListAccountSessions)
	identity.PATCH("/account", sameOrigin, ctrl.requireSession, requireCSRF, jsonAuthRequest, ctrl.UpdateProfile)
	identity.POST("/account/password", sameOrigin, ctrl.requireSession, requireCSRF, jsonAuthRequest, ctrl.ChangePassword)
	identity.DELETE("/account/sessions/:session_id", sameOrigin, ctrl.requireSession, requireCSRF, ctrl.RevokeAccountSession)
	identity.GET("/models", ctrl.requireSession, ctrl.ListVisibleModels)
	admin := identity.Group("/admin")
	admin.Use(ctrl.requireSession, requireAdmin)
	admin.GET("/calls", ctrl.ListAdminCalls)
	admin.GET("/calls/:request_id", ctrl.GetAdminCall)
	admin.GET("/providers", ctrl.ListProviders)
	admin.GET("/models", ctrl.ListAdminModels)
	admin.GET("/model-grantees", ctrl.ListModelGrantees)
	admin.POST("/providers", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.CreateProvider)
	admin.POST("/providers/:provider_id/connections", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.CreateConnection)
	admin.POST("/connections/:connection_id/credentials", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.CreateCredential)
	admin.POST("/credentials/:credential_id/verify", sameOrigin, requireCSRF, ctrl.VerifyCredential)
	admin.PATCH("/credentials/:credential_id", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.UpdateCredential)
	admin.POST("/connections/:connection_id/models", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.CreateProviderModel)
	admin.POST("/models", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.CreateModel)
	admin.POST("/models/:model_id/bindings", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.AddModelBinding)
	admin.PUT("/models/:model_id/weights", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.UpdateModelWeights)
	admin.POST("/models/:model_id/rename", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.RenameModel)
	admin.PUT("/models/:model_id/grants", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.UpdateModelGrants)

	keys := identity.Group("/keys")
	keys.Use(ctrl.requireSession)
	keys.GET("", ctrl.ListPersonalKeys)
	keys.POST("", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.CreatePersonalKey)
	keys.POST("/:key_id/confirm", sameOrigin, requireCSRF, ctrl.ConfirmKeyDelivery)
	keys.PATCH("/:key_id", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.UpdatePersonalKey)
	keys.DELETE("/:key_id", sameOrigin, requireCSRF, ctrl.RevokePersonalKey)
	keys.POST("/:key_id/rotate", sameOrigin, requireCSRF, ctrl.RotatePersonalKey)
}

// Health returns a simple health check response.
func (ctrl *Ctrl) Health(c *fox.Context) string {
	return "ok"
}

// Hello returns a greeting message.
func (ctrl *Ctrl) Hello(c *fox.Context) any {
	return map[string]string{"message": "Hello from RouteX!"}
}
