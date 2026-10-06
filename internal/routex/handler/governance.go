package handler

import (
	"net/http"
	"time"

	"github.com/fox-gonic/fox"

	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

type RegistrationResponse = service.RegistrationStatus
type RegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}
type PermissionResponse struct {
	Permissions []string `json:"permissions"`
}
type MemberResponse struct {
	ID           string     `json:"id"`
	Email        string     `json:"email"`
	Name         string     `json:"name"`
	Role         string     `json:"role"`
	OffboardedAt *time.Time `json:"offboarded_at"`
	Disabled     bool       `json:"disabled"`
	CreatedAt    time.Time  `json:"created_at"`
	RoleIDs      []string   `json:"role_ids"`
}
type MembersResponse = MemberListResponse

type ListMembersRequest struct {
	Query  string `query:"q"`
	Status string `query:"status"`
	Role   string `query:"role"`
	Limit  int    `query:"limit"`
	Cursor string `query:"cursor"`
}
type MemberPath struct {
	UserID string `uri:"user_id" json:"-"`
}
type CreateMemberRequest struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Password string `json:"password"`
	Role     string `json:"role"`
}
type RoleResponse struct {
	Description string   `json:"description"`
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Builtin     bool     `json:"builtin"`
	Permissions []string `json:"permissions"`
	MemberCount *int64   `json:"member_count,omitempty"`
}
type RolesResponse struct {
	Items                []RoleResponse `json:"items"`
	AvailablePermissions []string       `json:"available_permissions"`
}
type SaveRoleRequest struct {
	Description *string   `json:"description,omitempty"`
	RoleID      string    `uri:"role_id" json:"-"`
	Name        string    `json:"name"`
	Permissions *[]string `json:"permissions"`
}
type RolePath struct {
	RoleID string `uri:"role_id" json:"-"`
}
type UpdateMemberRolesRequest struct {
	UserID  string    `uri:"user_id" json:"-"`
	RoleIDs *[]string `json:"role_ids"`
}

// RequirePermission checks the current database-backed permission union. It
// belongs after requireSession and before any resource-specific handler.
func (ctrl *Ctrl) RequirePermission(permission string) fox.HandlerFunc {
	return func(c *fox.Context) error {
		if err := ctrl.service.Authorize(c.Request.Context(), currentAuthentication(c).User.ID, permission); err != nil {
			return err
		}
		c.Next()
		return nil
	}
}

func memberResponse(item service.MemberRecord) *MemberResponse {
	return &MemberResponse{ID: item.User.ID, Email: item.User.Email, Name: item.User.Name, Role: item.User.Role, Disabled: item.User.Disabled, OffboardedAt: item.User.OffboardedAt, CreatedAt: item.User.CreatedAt, RoleIDs: item.RoleIDs}
}
func roleResponse(item service.RoleRecord) *RoleResponse {
	return &RoleResponse{ID: item.Role.ID, Name: item.Role.Name, Description: item.Role.Description, Builtin: item.Role.Builtin, Permissions: item.Permissions, MemberCount: item.MemberCount}
}

func (ctrl *Ctrl) RegistrationStatus(c *fox.Context) (*RegistrationResponse, error) {
	if c.Request.URL.RawQuery != "" {
		return nil, apperrors.ErrBadRequest
	}
	return ctrl.service.PublicRegistrationStatus(c.Request.Context())
}

// The root replaces the legacy policy route with SetRegistrationPolicy; this
// compatibility method uses the same strict reviewed transport, never BindJSON.
func (ctrl *Ctrl) UpdateRegistration(c *fox.Context) (*service.RegistrationPolicyResult, error) {
	return ctrl.SetRegistrationPolicy(c)
}
func (ctrl *Ctrl) Register(c *fox.Context, request RegisterRequest) error {
	result, err := ctrl.service.RegisterWithApproval(c.Request.Context(), request.Email, request.Password, request.Name)
	if err != nil {
		return err
	}
	if result.ApprovalPending {
		ctrl.registrationPending(c)
		return nil
	}
	setSessionCookie(c, result.Authentication)
	c.JSON(http.StatusCreated, sessionResponse(result.Authentication))
	return nil
}
func (ctrl *Ctrl) CurrentPermissions(c *fox.Context) (*PermissionResponse, error) {
	permissions, err := ctrl.service.Permissions(c.Request.Context(), currentAuthentication(c).User.ID)
	if err != nil {
		return nil, err
	}
	return &PermissionResponse{Permissions: permissions}, nil
}
func (ctrl *Ctrl) ListMembers(c *fox.Context) (*MembersResponse, error) {
	c.Header("Cache-Control", "private, no-store")
	filter, err := memberListFilter(c)
	if err != nil {
		return nil, err
	}
	page, err := ctrl.service.ListMemberSummaries(c.Request.Context(), currentAuthentication(c).User.ID, filter)
	if err != nil {
		return nil, err
	}
	return memberListResponse(page), nil
}

func (ctrl *Ctrl) GetMember(c *fox.Context, request MemberPath) (*MemberDetailResponse, error) {
	userID, err := memberOverviewSubject(c)
	if err != nil {
		return nil, err
	}
	item, err := ctrl.service.GetMemberDetail(c.Request.Context(), currentAuthentication(c).User.ID, userID)
	if err != nil {
		return nil, err
	}
	return &MemberDetailResponse{MemberResponse: *memberResponse(item.MemberRecord), LastLoginAt: item.LastLoginAt, LastLoginStatus: item.LastLoginStatus, RegistrationApproval: item.RegistrationApproval}, nil
}
func (ctrl *Ctrl) CreateMember(c *fox.Context, request CreateMemberRequest) error {
	item, err := ctrl.service.CreateMember(c.Request.Context(), currentAuthentication(c).User.ID, request.Email, request.Password, request.Name, request.Role)
	if err != nil {
		return err
	}
	c.JSON(http.StatusCreated, memberResponse(*item))
	return nil
}
func (ctrl *Ctrl) ListRoles(c *fox.Context) (*RolesResponse, error) {
	items, err := ctrl.service.ListRoles(c.Request.Context(), currentAuthentication(c).User.ID)
	if err != nil {
		return nil, err
	}
	result := &RolesResponse{Items: []RoleResponse{}, AvailablePermissions: service.AssignablePermissions()}
	for _, item := range items {
		result.Items = append(result.Items, *roleResponse(item))
	}
	return result, nil
}
func (ctrl *Ctrl) CreateRole(c *fox.Context, request SaveRoleRequest) error {
	if request.Permissions == nil {
		return apperrors.ErrBadRequest
	}
	var item *service.RoleRecord
	var err error
	if request.Description == nil {
		item, err = ctrl.service.SaveRole(c.Request.Context(), currentAuthentication(c).User.ID, "", request.Name, *request.Permissions)
	} else {
		item, err = ctrl.service.CreateRoleWithDescription(c.Request.Context(), currentAuthentication(c).User.ID, request.Name, *request.Description, *request.Permissions)
	}
	if err != nil {
		return err
	}
	c.JSON(http.StatusCreated, roleResponse(*item))
	return nil
}
func (ctrl *Ctrl) UpdateRole(c *fox.Context, request SaveRoleRequest) (*RoleResponse, error) {
	if request.Permissions == nil {
		return nil, apperrors.ErrBadRequest
	}
	item, err := ctrl.service.SaveRole(c.Request.Context(), currentAuthentication(c).User.ID, request.RoleID, request.Name, *request.Permissions)
	if err != nil {
		return nil, err
	}
	return roleResponse(*item), nil
}
func (ctrl *Ctrl) DeleteRole(c *fox.Context, request RolePath) error {
	if err := ctrl.service.DeleteRole(c.Request.Context(), currentAuthentication(c).User.ID, request.RoleID); err != nil {
		return err
	}
	c.Status(http.StatusNoContent)
	return nil
}
func (ctrl *Ctrl) UpdateMemberRoles(c *fox.Context, request UpdateMemberRolesRequest) (*MemberResponse, error) {
	if request.RoleIDs == nil {
		return nil, apperrors.ErrBadRequest
	}
	item, err := ctrl.service.SetMemberRoles(c.Request.Context(), currentAuthentication(c).User.ID, request.UserID, *request.RoleIDs)
	if err != nil {
		return nil, err
	}
	return memberResponse(*item), nil
}
