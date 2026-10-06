package service

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
)

const roleDescriptionBudget = 2000

func validRoleDescription(value string, allowEmpty bool) bool {
	return (allowEmpty || value != "") && len(value) <= roleDescriptionBudget && utf8.ValidString(value) && strings.TrimSpace(value) == value && !strings.ContainsFunc(value, func(r rune) bool { return r != '\n' && unicode.IsControl(r) })
}

// RoleCreationInput preserves legacy creation without a description. An explicit
// description is a complete validated value, never a null or empty legacy fallback.
type RoleCreationInput struct {
	Name        string    `json:"name"`
	Permissions *[]string `json:"permissions"`
	Description *string   `json:"description,omitempty"`
}

func (input *RoleCreationInput) UnmarshalJSON(raw []byte) error {
	if len(raw) > 64*1024 || !utf8.Valid(raw) || !modelCreationUnicode(raw) {
		return apperrors.ErrBadRequest
	}
	fields, err := memberRolesObject(raw)
	if err != nil {
		return apperrors.ErrBadRequest
	}
	for key := range fields {
		if strings.EqualFold(key, "description") && key != "description" {
			return apperrors.ErrBadRequest
		}
	}
	type plain RoleCreationInput
	var next plain
	if err := json.Unmarshal(raw, &next); err != nil {
		return apperrors.ErrBadRequest
	}
	if value, present := fields["description"]; present {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) || next.Description == nil || !validRoleDescription(*next.Description, false) {
			return apperrors.ErrBadRequest
		}
	}
	*input = RoleCreationInput(next)
	return nil
}

// CreateRoleWithDescription is create-only; public existing-role changes use the
// independently authorized reviewed editor. Trusted SaveRole preserves omission.
func (s *Service) CreateRoleWithDescription(ctx context.Context, actorID, name, description string, permissions []string) (*RoleRecord, error) {
	name = strings.TrimSpace(name)
	if !validRoleDescription(description, false) {
		return nil, apperrors.ErrBadRequest
	}
	if err := validateRoleInput(name, permissions); err != nil {
		return nil, err
	}
	roleID, err := id.NewPrefixed("rol")
	if err != nil {
		return nil, err
	}
	if err := roleDefinitionIDs(actorID, roleID); err != nil {
		return nil, err
	}
	permissions = append([]string{}, permissions...)
	// The shared engine receives the same canonical permission ordering as SaveRole.
	slices.Sort(permissions)
	record, err := s.mutateRoleDefinition(ctx, actorID, roleID, "", RoleDefinitionInput{Name: name, Description: description, Permissions: permissions}, false, true)
	return record, catalogError(err)
}
