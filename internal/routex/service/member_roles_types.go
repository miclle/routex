package service

import (
	"net/http"
	"time"

	apperrors "github.com/miclle/routex/internal/routex/errors"
)

const memberRolesReadBudget = 10000
const memberRolesWriteBeforeBudget = 1000
const memberRolesWriteBudget = 100
const memberRolesCatalogueBudget = 1000
const memberRolesPermissionBudget = 100000
const memberRolesBatchSize = 500
const memberRolesAuditBudget = 60 * 1024

var memberRolesOverflow = &apperrors.Error{Code: http.StatusUnprocessableEntity, Message: "member roles exceed supported bounds"}
var memberRolesUnavailable = &apperrors.Error{Code: http.StatusServiceUnavailable, Message: "member roles unavailable"}

type MemberRoleSummary struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Builtin         bool   `json:"builtin"`
	PermissionCount int    `json:"permission_count"`
	DefinitionETag  string `json:"definition_etag"`
}
type MemberRolesWorkspace struct {
	UserID               string              `json:"user_id"`
	ObservedAt           time.Time           `json:"observed_at"`
	IdentityRole         string              `json:"identity_role"`
	SubjectStatus        string              `json:"subject_status"`
	BuiltinRole          MemberRoleSummary   `json:"builtin_role"`
	AssignedRoles        []MemberRoleSummary `json:"assigned_roles"`
	EffectivePermissions []string            `json:"effective_permissions"`
	PermissionUse        string              `json:"permission_use"`
	ETag                 string              `json:"etag"`
	CanEdit              bool                `json:"can_edit"`
	EditBlockers         []string            `json:"edit_blockers"`
	CandidateStatus      string              `json:"candidate_status"`
}
type MemberRoleCandidateFilter struct {
	Query, Cursor string
	Limit         int
}
type MemberRoleCandidatePage struct {
	Items      []MemberRoleSummary `json:"items"`
	NextCursor *string             `json:"next_cursor"`
	ETag       string              `json:"etag"`
}
type MemberRoleDetail struct {
	UserID      string            `json:"user_id"`
	Role        MemberRoleSummary `json:"role"`
	Permissions []string          `json:"permissions"`
	ETag        string            `json:"etag"`
}
type MemberRoleDefinitionProof struct {
	ID   string `json:"id"`
	ETag string `json:"etag"`
}
type MemberRolesInput struct {
	RoleIDs               []string                    `json:"role_ids"`
	RoleDefinitions       []MemberRoleDefinitionProof `json:"role_definitions"`
	BuiltinDefinitionETag string                      `json:"builtin_definition_etag"`
	Reason                string                      `json:"reason"`
}
type MemberRolesResult struct {
	UserID       string   `json:"user_id"`
	RoleIDs      []string `json:"role_ids"`
	ETag         string   `json:"etag"`
	Confirmation string   `json:"confirmation"`
	Effect       string   `json:"effect"`
}
