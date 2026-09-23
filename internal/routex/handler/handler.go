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
	identity.GET("/auth/registration", ctrl.RegistrationStatus)
	identity.POST("/auth/register", sameOrigin, jsonAuthRequest, ctrl.Register)
	identity.GET("/auth/permissions", ctrl.requireSession, ctrl.CurrentPermissions)
	identity.POST("/auth/login", sameOrigin, jsonAuthRequest, ctrl.Login)
	identity.GET("/auth/session", ctrl.requireSession, ctrl.CurrentSession)
	identity.POST("/auth/logout", sameOrigin, ctrl.requireSession, requireCSRF, ctrl.Logout)
	identity.GET("/admin/status", ctrl.requireSession, requireAdmin, ctrl.AdminStatus)

	identity.GET("/teams", ctrl.requireSession, ctrl.ListTeams)
	identity.GET("/teams/:team_id", ctrl.requireSession, ctrl.GetTeam)
	identity.GET("/projects", ctrl.requireSession, ctrl.ListProjects)
	identity.GET("/projects/:project_id/calls", ctrl.requireSession, ctrl.ListProjectCalls)
	identity.GET("/projects/:project_id/calls/:request_id", ctrl.requireSession, ctrl.GetProjectCall)
	identity.GET("/projects/:project_id/manager-candidates", ctrl.requireSession, ctrl.ProjectManagerCandidates)
	identity.GET("/projects/:project_id/limits", ctrl.requireSession, ctrl.GetResourceLimit)
	identity.PUT("/projects/:project_id/limits", ctrl.requireSession, sameOrigin, requireCSRF, jsonManagementRequest, ctrl.SetResourceLimit)
	identity.GET("/projects/:project_id/requests", ctrl.requireSession, ctrl.ListProjectRequests)
	identity.GET("/projects/:project_id/request-model-candidates", ctrl.requireSession, ctrl.ProjectRequestCandidates)
	identity.POST("/projects/:project_id/requests", ctrl.requireSession, sameOrigin, requireCSRF, jsonManagementRequest, ctrl.CreateProjectRequest)
	identity.POST("/projects/:project_id/requests/:request_id/decision", ctrl.requireSession, sameOrigin, requireCSRF, jsonManagementRequest, ctrl.DecideProjectRequest)
	identity.GET("/projects/:project_id", ctrl.requireSession, ctrl.GetProject)
	identity.POST("/projects", ctrl.requireSession, sameOrigin, requireCSRF, jsonManagementRequest, ctrl.CreateProject)
	identity.PATCH("/projects/:project_id", ctrl.requireSession, sameOrigin, requireCSRF, jsonManagementRequest, ctrl.UpdateProject)
	identity.PUT("/projects/:project_id/managers", ctrl.requireSession, sameOrigin, requireCSRF, jsonManagementRequest, ctrl.SetProjectManagers)
	identity.PUT("/projects/:project_id/models", ctrl.requireSession, sameOrigin, requireCSRF, jsonManagementRequest, ctrl.SetProjectModels)
	projectKeys := identity.Group("/projects/:project_id/keys")
	projectKeys.Use(ctrl.requireSession)
	projectKeys.GET("/:key_id/limits", ctrl.GetResourceLimit)
	projectKeys.PUT("/:key_id/limits", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.SetResourceLimit)
	projectKeys.GET("", ctrl.ListProjectKeys)
	projectKeys.POST("", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.CreateProjectKey)
	projectKeys.GET("/:key_id", ctrl.GetProjectKey)
	projectKeys.PATCH("/:key_id", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.UpdateProjectKey)
	projectKeys.DELETE("/:key_id", sameOrigin, requireCSRF, ctrl.RevokeProjectKey)
	projectKeys.POST("/:key_id/confirm", sameOrigin, requireCSRF, ctrl.ConfirmProjectKey)
	projectKeys.POST("/:key_id/rotate", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.RotateProjectKey)
	projectKeys.POST("/:key_id/complete-rotation", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.CompleteProjectKeyRotation)
	identity.GET("/calls", ctrl.requireSession, ctrl.ListPersonalCalls)
	identity.GET("/calls/:request_id", ctrl.requireSession, ctrl.GetPersonalCall)
	identity.GET("/usage", ctrl.requireSession, ctrl.PersonalUsage)
	identity.GET("/projects/:project_id/usage", ctrl.requireSession, ctrl.ProjectUsage)
	identity.GET("/account/sessions", ctrl.requireSession, ctrl.ListAccountSessions)
	identity.PATCH("/account", sameOrigin, ctrl.requireSession, requireCSRF, jsonAuthRequest, ctrl.UpdateProfile)
	identity.POST("/account/password", sameOrigin, ctrl.requireSession, requireCSRF, jsonAuthRequest, ctrl.ChangePassword)
	identity.DELETE("/account/sessions/:session_id", sameOrigin, ctrl.requireSession, requireCSRF, ctrl.RevokeAccountSession)
	identity.GET("/models", ctrl.requireSession, ctrl.ListVisibleModels)
	admin := identity.Group("/admin")
	admin.Use(ctrl.requireSession)
	admin.GET("/team-member-candidates", ctrl.TeamMemberCandidates)
	admin.GET("/resource-model-candidates", ctrl.ResourceModelCandidates)
	admin.GET("/teams", ctrl.RequirePermission("teams.read_all"), ctrl.ListAdminTeams)
	admin.GET("/teams/:team_id", ctrl.GetTeam)
	admin.POST("/teams", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.CreateTeam)
	admin.PATCH("/teams/:team_id", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.UpdateTeam)
	admin.PUT("/teams/:team_id/members", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.SetTeamMembers)
	admin.PUT("/teams/:team_id/models", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.SetTeamModels)
	admin.GET("/projects", ctrl.RequirePermission("projects.read_all"), ctrl.ListAdminProjects)
	admin.GET("/projects/:project_id", ctrl.GetProject)
	admin.PATCH("/projects/:project_id", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.UpdateProject)
	admin.PUT("/projects/:project_id/managers", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.SetProjectManagers)
	admin.PUT("/projects/:project_id/models", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.SetProjectModels)

	admin.GET("/members/:user_id/limits", ctrl.GetResourceLimit)
	admin.PUT("/members/:user_id/limits", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.SetResourceLimit)
	admin.GET("/members/:user_id/offboarding", ctrl.RequirePermission("members.read"), ctrl.OffboardingInventory)
	admin.POST("/members/:user_id/offboarding/plans", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.RequirePermission("members.write"), ctrl.CreateOffboardingPlan)
	admin.POST("/members/:user_id/offboarding/:case_id/complete", sameOrigin, requireCSRF, ctrl.RequirePermission("members.write"), ctrl.CompleteOffboarding)
	admin.POST("/members/:user_id/offboarding/emergency", requireAdmin, sameOrigin, requireCSRF, jsonManagementRequest, ctrl.EmergencyOffboarding)
	admin.GET("/members", ctrl.RequirePermission("members.read"), ctrl.ListMembers)
	admin.GET("/members/:user_id", ctrl.RequirePermission("members.read"), ctrl.GetMember)
	admin.POST("/members", sameOrigin, requireCSRF, jsonAuthRequest, ctrl.RequirePermission("members.write"), ctrl.CreateMember)
	admin.PATCH("/members/:user_id", sameOrigin, requireCSRF, jsonAuthRequest, ctrl.RequirePermission("members.write"), ctrl.UpdateMember)
	admin.GET("/roles", ctrl.RequirePermission("roles.read"), ctrl.ListRoles)
	admin.POST("/roles", requireAdmin, sameOrigin, requireCSRF, jsonManagementRequest, ctrl.CreateRole)
	admin.PUT("/roles/:role_id", requireAdmin, sameOrigin, requireCSRF, jsonManagementRequest, ctrl.UpdateRole)
	admin.DELETE("/roles/:role_id", requireAdmin, sameOrigin, requireCSRF, ctrl.DeleteRole)
	admin.PUT("/members/:user_id/roles", requireAdmin, sameOrigin, requireCSRF, jsonManagementRequest, ctrl.UpdateMemberRoles)
	admin.GET("/registration", requireAdmin, ctrl.RegistrationStatus)
	admin.PATCH("/registration", requireAdmin, sameOrigin, requireCSRF, jsonAuthRequest, ctrl.UpdateRegistration)
	admin.GET("/runtime", ctrl.RequirePermission("system.read"), ctrl.RuntimeStatus)
	admin.POST("/runtime/publish", requireAdmin, sameOrigin, requireCSRF, ctrl.PublishRuntime)

	admin.GET("/prices", ctrl.RequirePermission("prices.read"), ctrl.ListPrices)
	admin.PATCH("/provider-models/:provider_model_id", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.RequirePermission("providers.write"), ctrl.SetProviderModelState)
	admin.GET("/provider-models/:provider_model_id/price", ctrl.RequirePermission("prices.read"), ctrl.GetPrice)
	admin.PUT("/prices", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.RequirePermission("prices.write"), ctrl.WritePrices)
	admin.PUT("/prices/currency", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.RequirePermission("prices.write"), ctrl.WritePricingCurrency)
	admin.GET("/prices/currency", ctrl.RequirePermission("prices.read"), ctrl.GetPricingCurrency)
	admin.POST("/prices/import/preview", sameOrigin, requireCSRF, jsonPriceImportRequest, ctrl.RequirePermission("prices.read"), ctrl.PreviewPriceImport)
	admin.POST("/prices/import/commit", sameOrigin, requireCSRF, jsonPriceImportRequest, ctrl.RequirePermission("prices.write"), ctrl.CommitPriceImport)
	admin.GET("/prices/export.csv", ctrl.RequirePermission("prices.read"), ctrl.ExportPriceCSV)
	admin.POST("/prices/quote", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.RequirePermission("prices.read"), ctrl.QuotePrice)

	admin.GET("/usage", ctrl.RequirePermission("calls.read_all"), ctrl.AdminUsage)
	admin.GET("/calls", ctrl.RequirePermission("calls.read_all"), ctrl.ListAdminCalls)
	admin.GET("/calls/:request_id", ctrl.RequirePermission("calls.read_all"), ctrl.GetAdminCall)
	admin.GET("/providers", ctrl.RequirePermission("providers.read"), ctrl.ListProviders)
	admin.GET("/models", ctrl.RequirePermission("models.read_all"), ctrl.ListAdminModels)
	admin.GET("/model-grantees", ctrl.RequirePermission("models.write"), ctrl.ListModelGrantees)
	admin.POST("/providers", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.RequirePermission("providers.write"), ctrl.CreateProvider)
	admin.POST("/providers/:provider_id/connections", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.RequirePermission("providers.write"), ctrl.CreateConnection)
	admin.POST("/connections/:connection_id/credentials", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.RequirePermission("providers.write"), ctrl.CreateCredential)
	admin.POST("/credentials/:credential_id/verify", sameOrigin, requireCSRF, ctrl.RequirePermission("providers.write"), ctrl.VerifyCredential)
	admin.PATCH("/credentials/:credential_id", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.RequirePermission("providers.write"), ctrl.UpdateCredential)
	admin.POST("/connections/:connection_id/models", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.RequirePermission("providers.write"), ctrl.CreateProviderModel)
	admin.POST("/models", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.RequirePermission("models.write"), ctrl.CreateModel)
	admin.POST("/models/:model_id/bindings", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.RequirePermission("models.write"), ctrl.AddModelBinding)
	admin.PUT("/models/:model_id/weights", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.RequirePermission("models.write"), ctrl.UpdateModelWeights)
	admin.POST("/models/:model_id/rename", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.RequirePermission("models.write"), ctrl.RenameModel)
	admin.PUT("/models/:model_id/grants", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.RequirePermission("models.write"), ctrl.UpdateModelGrants)

	keys := identity.Group("/keys")
	keys.Use(ctrl.requireSession)
	keys.GET("/:key_id/limits", ctrl.GetResourceLimit)
	keys.PUT("/:key_id/limits", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.SetResourceLimit)
	keys.GET("", ctrl.ListPersonalKeys)
	keys.POST("", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.CreatePersonalKey)
	keys.POST("/:key_id/confirm", sameOrigin, requireCSRF, ctrl.ConfirmKeyDelivery)
	keys.PATCH("/:key_id", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.UpdatePersonalKey)
	keys.DELETE("/:key_id", sameOrigin, requireCSRF, ctrl.RevokePersonalKey)
	keys.POST("/:key_id/complete-rotation", sameOrigin, requireCSRF, jsonManagementRequest, ctrl.CompletePersonalKeyRotation)
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
