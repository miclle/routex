package handler

import (
	"io"
	"strconv"
	"strings"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func roleDefinitionRequest(c *fox.Context) error {
	c.Header("Cache-Control", "private, no-store")
	if err := noTeamQuotaQuery(c); err != nil {
		return err
	}
	roleID := c.Param("role_id")
	if !memberOverviewUserID.MatchString(roleID) || !strings.HasPrefix(roleID, "rol_") {
		return apperrors.ErrBadRequest
	}
	return nil
}

func (ctrl *Ctrl) GetRoleDefinition(c *fox.Context) (*service.RoleDefinitionRecord, error) {
	if err := roleDefinitionRequest(c); err != nil {
		return nil, err
	}
	result, err := ctrl.service.GetRoleDefinition(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("role_id"))
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ReviewETag))
	}
	return result, err
}

func (ctrl *Ctrl) SetReviewedRoleDefinition(c *fox.Context) (*service.RoleDefinitionResult, error) {
	if err := roleDefinitionRequest(c); err != nil {
		return nil, err
	}
	etag, err := memberMetadataETag(c)
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 64*1024+1))
	if err != nil || len(raw) > 64*1024 {
		return nil, apperrors.ErrBadRequest
	}
	var input service.RoleDefinitionInput
	if err := input.UnmarshalJSON(raw); err != nil {
		return nil, err
	}
	result, err := ctrl.service.SetReviewedRoleDefinition(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("role_id"), etag, input)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ETag))
	}
	return result, err
}
