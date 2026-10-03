package handler

import (
	"strconv"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func (ctrl *Ctrl) GetDefaultLimit(c *fox.Context) (*service.DefaultLimitRecord, error) {
	if c.Request.URL.RawQuery != "" {
		return nil, apperrors.ErrBadRequest
	}
	result, err := ctrl.service.GetDefaultLimit(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("kind"))
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ETag))
	}
	return result, err
}
func (ctrl *Ctrl) SetDefaultLimit(c *fox.Context) (*service.DefaultLimitRecord, error) {
	if c.Request.URL.RawQuery != "" {
		return nil, apperrors.ErrBadRequest
	}
	var input service.DefaultLimitInput
	if err := decodeStrictRequest(c, &input); err != nil {
		return nil, err
	}
	etag, err := quotaETag(c)
	if err != nil {
		return nil, err
	}
	result, err := ctrl.service.SetDefaultLimit(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("kind"), etag, input)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ETag))
	}
	return result, err
}
func defaultLimitResetTarget(c *fox.Context) service.LimitTarget {
	if teamID := c.Param("team_id"); teamID != "" {
		return service.LimitTarget{Kind: "team", ID: teamID, TeamID: teamID}
	}
	return service.LimitTarget{Kind: "user", ID: c.Param("user_id")}
}
func (ctrl *Ctrl) GetDefaultLimitResetContext(c *fox.Context) (*service.DefaultLimitResetContext, error) {
	if c.Request.URL.RawQuery != "" {
		return nil, apperrors.ErrBadRequest
	}
	result, err := ctrl.service.GetDefaultLimitResetContext(c.Request.Context(), currentAuthentication(c).User.ID, defaultLimitResetTarget(c))
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ETag))
	}
	return result, err
}
func (ctrl *Ctrl) ResetResourceLimitToDefault(c *fox.Context) (*service.DefaultLimitResetResult, error) {
	if c.Request.URL.RawQuery != "" {
		return nil, apperrors.ErrBadRequest
	}
	var input service.DefaultLimitResetInput
	if err := decodeStrictRequest(c, &input); err != nil {
		return nil, err
	}
	etag, err := quotaETag(c)
	if err != nil {
		return nil, err
	}
	result, err := ctrl.service.ResetResourceLimitToDefault(c.Request.Context(), currentAuthentication(c).User.ID, defaultLimitResetTarget(c), etag, input.Reason)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.Limit.ETag))
	}
	return result, err
}
