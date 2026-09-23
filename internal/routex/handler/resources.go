package handler

import (
	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"net/http"
	"time"
)

type ResourcePersonResponse struct {
	ID     string `json:"id"`
	UserID string `json:"user_id"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Role   string `json:"role,omitempty"`
	Status string `json:"status,omitempty"`
}
type TeamResponse struct {
	ID          string                   `json:"id"`
	Name        string                   `json:"name"`
	Description string                   `json:"description"`
	Status      string                   `json:"status"`
	CreatedAt   time.Time                `json:"created_at"`
	Members     []ResourcePersonResponse `json:"members"`
	ModelIDs    []string                 `json:"model_ids"`
}
type ProjectResponse struct {
	ID          string                   `json:"id"`
	Name        string                   `json:"name"`
	Description string                   `json:"description"`
	Status      string                   `json:"status"`
	CreatorID   string                   `json:"creator_id"`
	CreatedAt   time.Time                `json:"created_at"`
	Managers    []ResourcePersonResponse `json:"managers"`
	ModelIDs    []string                 `json:"model_ids"`
}
type ListResourcesRequest struct {
	Query  string `query:"q"`
	Status string `query:"status"`
	Cursor string `query:"cursor"`
	Limit  int    `query:"limit"`
}

func resourceFilter(input ListResourcesRequest) service.ResourceFilter {
	return service.ResourceFilter{Query: input.Query, Status: input.Status, Cursor: input.Cursor, Limit: input.Limit}
}
func resourcePeople(items []service.ResourcePerson) []ResourcePersonResponse {
	result := []ResourcePersonResponse{}
	for _, item := range items {
		result = append(result, ResourcePersonResponse{ID: item.ID, UserID: item.UserID, Name: item.Name, Email: item.Email, Role: item.Role, Status: item.Status})
	}
	return result
}
func teamResponse(item *service.ResourceRecord) *TeamResponse {
	return &TeamResponse{ID: item.ID, Name: item.Name, Description: item.Description, Status: item.Status, CreatedAt: item.CreatedAt, Members: resourcePeople(item.Members), ModelIDs: item.ModelIDs}
}
func projectResponse(item *service.ResourceRecord) *ProjectResponse {
	return &ProjectResponse{ID: item.ID, Name: item.Name, Description: item.Description, Status: item.Status, CreatorID: item.CreatorID, CreatedAt: item.CreatedAt, Managers: resourcePeople(item.Managers), ModelIDs: item.ModelIDs}
}

type TeamPath struct {
	TeamID string `uri:"team_id" json:"-"`
}
type TeamsResponse struct {
	Items      []TeamResponse `json:"items"`
	NextCursor *string        `json:"next_cursor"`
}
type CreateTeamRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	OwnerIDs    []string `json:"owner_ids"`
}
type UpdateTeamRequest struct {
	TeamID      string  `uri:"team_id" json:"-"`
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Status      *string `json:"status"`
}
type SetTeamModelsRequest struct {
	TeamID   string    `uri:"team_id" json:"-"`
	ModelIDs *[]string `json:"model_ids"`
}

func (ctrl *Ctrl) ListTeams(c *fox.Context, request ListResourcesRequest) (*TeamsResponse, error) {
	return ctrl.listTeams(c, request, false)
}
func (ctrl *Ctrl) ListAdminTeams(c *fox.Context, request ListResourcesRequest) (*TeamsResponse, error) {
	return ctrl.listTeams(c, request, true)
}
func (ctrl *Ctrl) listTeams(c *fox.Context, request ListResourcesRequest, all bool) (*TeamsResponse, error) {
	page, err := ctrl.service.ListResources(c.Request.Context(), currentAuthentication(c).User.ID, service.TeamResource, all, resourceFilter(request))
	if err != nil {
		return nil, err
	}
	result := &TeamsResponse{Items: []TeamResponse{}, NextCursor: callCursor(page.NextCursor)}
	for _, item := range page.Items {
		result.Items = append(result.Items, *teamResponse(&item))
	}
	return result, nil
}
func (ctrl *Ctrl) GetTeam(c *fox.Context, request TeamPath) (*TeamResponse, error) {
	item, err := ctrl.service.GetResource(c.Request.Context(), currentAuthentication(c).User.ID, service.TeamResource, request.TeamID)
	if err != nil {
		return nil, err
	}
	return teamResponse(item), nil
}
func (ctrl *Ctrl) CreateTeam(c *fox.Context, request CreateTeamRequest) error {
	item, err := ctrl.service.CreateResource(c.Request.Context(), currentAuthentication(c).User.ID, service.TeamResource, request.Name, request.Description, request.OwnerIDs)
	if err != nil {
		return err
	}
	c.JSON(http.StatusCreated, teamResponse(item))
	return nil
}
func (ctrl *Ctrl) UpdateTeam(c *fox.Context, request UpdateTeamRequest) (*TeamResponse, error) {
	item, err := ctrl.service.UpdateResource(c.Request.Context(), currentAuthentication(c).User.ID, service.TeamResource, request.TeamID, service.ResourceUpdate{Name: request.Name, Description: request.Description, Status: request.Status})
	if err != nil {
		return nil, err
	}
	return teamResponse(item), nil
}
func (ctrl *Ctrl) SetTeamModels(c *fox.Context, request SetTeamModelsRequest) (*TeamResponse, error) {
	if request.ModelIDs == nil {
		return nil, apperrors.ErrBadRequest
	}
	item, err := ctrl.service.SetResourceModels(c.Request.Context(), currentAuthentication(c).User.ID, service.TeamResource, request.TeamID, *request.ModelIDs)
	if err != nil {
		return nil, err
	}
	return teamResponse(item), nil
}

type ProjectPath struct {
	ProjectID string `uri:"project_id" json:"-"`
}
type ProjectsResponse struct {
	Items      []ProjectResponse `json:"items"`
	NextCursor *string           `json:"next_cursor"`
}
type CreateProjectRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}
type UpdateProjectRequest struct {
	ProjectID   string  `uri:"project_id" json:"-"`
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Status      *string `json:"status"`
}
type SetProjectModelsRequest struct {
	ProjectID string    `uri:"project_id" json:"-"`
	ModelIDs  *[]string `json:"model_ids"`
}

func (ctrl *Ctrl) ListProjects(c *fox.Context, request ListResourcesRequest) (*ProjectsResponse, error) {
	return ctrl.listProjects(c, request, false)
}
func (ctrl *Ctrl) ListAdminProjects(c *fox.Context, request ListResourcesRequest) (*ProjectsResponse, error) {
	return ctrl.listProjects(c, request, true)
}
func (ctrl *Ctrl) listProjects(c *fox.Context, request ListResourcesRequest, all bool) (*ProjectsResponse, error) {
	page, err := ctrl.service.ListResources(c.Request.Context(), currentAuthentication(c).User.ID, service.ProjectResource, all, resourceFilter(request))
	if err != nil {
		return nil, err
	}
	result := &ProjectsResponse{Items: []ProjectResponse{}, NextCursor: callCursor(page.NextCursor)}
	for _, item := range page.Items {
		result.Items = append(result.Items, *projectResponse(&item))
	}
	return result, nil
}
func (ctrl *Ctrl) GetProject(c *fox.Context, request ProjectPath) (*ProjectResponse, error) {
	item, err := ctrl.service.GetResource(c.Request.Context(), currentAuthentication(c).User.ID, service.ProjectResource, request.ProjectID)
	if err != nil {
		return nil, err
	}
	return projectResponse(item), nil
}
func (ctrl *Ctrl) CreateProject(c *fox.Context, request CreateProjectRequest) error {
	item, err := ctrl.service.CreateResource(c.Request.Context(), currentAuthentication(c).User.ID, service.ProjectResource, request.Name, request.Description, nil)
	if err != nil {
		return err
	}
	c.JSON(http.StatusCreated, projectResponse(item))
	return nil
}
func (ctrl *Ctrl) UpdateProject(c *fox.Context, request UpdateProjectRequest) (*ProjectResponse, error) {
	item, err := ctrl.service.UpdateResource(c.Request.Context(), currentAuthentication(c).User.ID, service.ProjectResource, request.ProjectID, service.ResourceUpdate{Name: request.Name, Description: request.Description, Status: request.Status})
	if err != nil {
		return nil, err
	}
	return projectResponse(item), nil
}
func (ctrl *Ctrl) SetProjectModels(c *fox.Context, request SetProjectModelsRequest) (*ProjectResponse, error) {
	if request.ModelIDs == nil {
		return nil, apperrors.ErrBadRequest
	}
	item, err := ctrl.service.SetResourceModels(c.Request.Context(), currentAuthentication(c).User.ID, service.ProjectResource, request.ProjectID, *request.ModelIDs)
	if err != nil {
		return nil, err
	}
	return projectResponse(item), nil
}

type TeamMemberRequest struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
	Status string `json:"status"`
}
type SetTeamMembersRequest struct {
	TeamID  string               `uri:"team_id" json:"-"`
	Members *[]TeamMemberRequest `json:"members"`
}
type SetProjectManagersRequest struct {
	ProjectID string    `uri:"project_id" json:"-"`
	UserIDs   *[]string `json:"user_ids"`
}

func (ctrl *Ctrl) SetTeamMembers(c *fox.Context, request SetTeamMembersRequest) (*TeamResponse, error) {
	if request.Members == nil {
		return nil, apperrors.ErrBadRequest
	}
	members := make([]service.TeamMemberInput, 0, len(*request.Members))
	for _, member := range *request.Members {
		members = append(members, service.TeamMemberInput{UserID: member.UserID, Role: member.Role, Status: member.Status})
	}
	item, err := ctrl.service.SetTeamMembers(c.Request.Context(), currentAuthentication(c).User.ID, request.TeamID, members)
	if err != nil {
		return nil, err
	}
	return teamResponse(item), nil
}
func (ctrl *Ctrl) SetProjectManagers(c *fox.Context, request SetProjectManagersRequest) (*ProjectResponse, error) {
	if request.UserIDs == nil {
		return nil, apperrors.ErrBadRequest
	}
	item, err := ctrl.service.SetProjectManagers(c.Request.Context(), currentAuthentication(c).User.ID, request.ProjectID, *request.UserIDs)
	if err != nil {
		return nil, err
	}
	return projectResponse(item), nil
}
