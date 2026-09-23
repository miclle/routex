package handler

import (
	"net/http"
	"time"

	"github.com/fox-gonic/fox"

	"github.com/miclle/routex/internal/routex/service"
)

type OffboardingPlanRequest struct {
	UserID             string                                 `uri:"user_id" json:"-"`
	RequestID          string                                 `json:"request_id"`
	InventoryVersion   string                                 `json:"inventory_version"`
	PlannedAt          time.Time                              `json:"planned_at"`
	Reason             string                                 `json:"reason"`
	ProjectAssignments []service.OffboardingProjectAssignment `json:"project_assignments"`
	TeamAssignments    []service.OffboardingTeamAssignment    `json:"team_assignments"`
}
type OffboardingCasePath struct {
	UserID string `uri:"user_id" json:"-"`
	CaseID string `uri:"case_id" json:"-"`
}
type OffboardingEmergencyRequest struct {
	UserID          string                              `uri:"user_id" json:"-"`
	RequestID       string                              `json:"request_id"`
	CurrentPassword string                              `json:"current_password"`
	Reason          string                              `json:"reason"`
	TeamAssignments []service.OffboardingTeamAssignment `json:"team_assignments"`
}

func (ctrl *Ctrl) OffboardingInventory(c *fox.Context, request MemberPath) (*service.OffboardingInventory, error) {
	return ctrl.service.OffboardingInventory(c.Request.Context(), currentAuthentication(c).User.ID, request.UserID)
}
func (ctrl *Ctrl) CreateOffboardingPlan(c *fox.Context, request OffboardingPlanRequest) error {
	result, err := ctrl.service.CreateOffboardingPlan(c.Request.Context(), currentAuthentication(c).User.ID, request.UserID, service.OffboardingPlanInput{RequestID: request.RequestID, InventoryVersion: request.InventoryVersion, PlannedAt: request.PlannedAt, Reason: request.Reason, OffboardingAssignments: service.OffboardingAssignments{Projects: request.ProjectAssignments, Teams: request.TeamAssignments}})
	if err != nil {
		return err
	}
	c.JSON(http.StatusCreated, result)
	return nil
}
func (ctrl *Ctrl) CompleteOffboarding(c *fox.Context, request OffboardingCasePath) (*service.OffboardingCaseRecord, error) {
	return ctrl.service.CompleteOffboarding(c.Request.Context(), currentAuthentication(c).User.ID, request.UserID, request.CaseID)
}
func (ctrl *Ctrl) EmergencyOffboarding(c *fox.Context, request OffboardingEmergencyRequest) (*service.OffboardingCaseRecord, error) {
	return ctrl.service.EmergencyOffboarding(c.Request.Context(), currentAuthentication(c).User.ID, request.UserID, service.OffboardingEmergencyInput{RequestID: request.RequestID, CurrentPassword: request.CurrentPassword, Reason: request.Reason, Teams: request.TeamAssignments})
}
