package service

import (
	"bytes"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	apperrors "github.com/miclle/routex/internal/routex/errors"
)

const roleDefinitionPermissionBudget = 100
const roleDefinitionAuditBudget = 60 * 1024

var roleDefinitionOverflow = &apperrors.Error{Code: http.StatusUnprocessableEntity, Message: "role definition exceeds supported bounds"}
var roleDefinitionUnavailable = &apperrors.Error{Code: http.StatusServiceUnavailable, Message: "role definition unavailable"}

// RoleDefinitionRecord is a complete current resource review, without assignment facts.
type RoleDefinitionRecord struct {
	Description          string   `json:"description"`
	ID                   string   `json:"id"`
	Name                 string   `json:"name"`
	Builtin              bool     `json:"builtin"`
	Permissions          []string `json:"permissions"`
	AvailablePermissions []string `json:"available_permissions"`
	DefinitionETag       string   `json:"definition_etag"`
	IdentityETag         *string  `json:"identity_etag"`
	ReviewETag           string   `json:"review_etag"`
	CanEdit              bool     `json:"can_edit"`
}

// RoleDefinitionInput replaces the entire reviewed custom definition.
type RoleDefinitionInput struct {
	Description  string   `json:"description"`
	Name         string   `json:"name"`
	Permissions  []string `json:"permissions"`
	IdentityETag string   `json:"identity_etag"`
	Reason       string   `json:"reason"`
}

// RoleDefinitionResult confirms current database contents, never a historical operation.
type RoleDefinitionResult struct {
	Description  string   `json:"description"`
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Permissions  []string `json:"permissions"`
	IdentityETag string   `json:"identity_etag"`
	ETag         string   `json:"etag"`
	Confirmation string   `json:"confirmation"`
	Effect       string   `json:"effect"`
}

func validRoleDefinitionReason(reason string) bool {
	return reason != "" && len(reason) <= 1024 && utf8.ValidString(reason) && strings.TrimSpace(reason) == reason && !strings.ContainsFunc(reason, unicode.IsControl)
}

func validateRoleDefinitionInput(input RoleDefinitionInput) error {
	if !validRoleDescription(input.Description, false) || !validCatalogLabel(input.Name) || input.Permissions == nil || len(input.Permissions) > roleDefinitionPermissionBudget || !slices.IsSorted(input.Permissions) || !validMemberRoleDigest(input.IdentityETag) || !validRoleDefinitionReason(input.Reason) {
		return apperrors.ErrBadRequest
	}
	for i, code := range input.Permissions {
		if !memberRolePermissionCode(code) || i > 0 && input.Permissions[i-1] == code {
			return apperrors.ErrBadRequest
		}
	}
	return nil
}

// UnmarshalJSON rejects ambiguous input instead of normalizing a reviewed operation.
func (input *RoleDefinitionInput) UnmarshalJSON(raw []byte) error {
	if len(raw) > 64*1024 || !utf8.Valid(raw) || !modelCreationUnicode(raw) {
		return apperrors.ErrBadRequest
	}
	fields, err := memberRolesObject(raw)
	if err != nil || len(fields) != 5 {
		return apperrors.ErrBadRequest
	}
	var next RoleDefinitionInput
	for name, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return apperrors.ErrBadRequest
		}
		switch name {
		case "description":
			err = json.Unmarshal(value, &next.Description)
		case "name":
			err = json.Unmarshal(value, &next.Name)
		case "permissions":
			err = json.Unmarshal(value, &next.Permissions)
		case "identity_etag":
			err = json.Unmarshal(value, &next.IdentityETag)
		case "reason":
			err = json.Unmarshal(value, &next.Reason)
		default:
			return apperrors.ErrBadRequest
		}
		if err != nil {
			return apperrors.ErrBadRequest
		}
	}
	if err := validateRoleDefinitionInput(next); err != nil {
		return err
	}
	*input = next
	return nil
}

func roleDefinitionIDs(actorID, roleID string) error {
	if !safeTeamSessionID(actorID) {
		return apperrors.ErrUnauthorized
	}
	if !memberRoleID(roleID) {
		return apperrors.ErrBadRequest
	}
	return nil
}
