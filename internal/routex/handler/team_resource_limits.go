package handler

import (
	"strings"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func (ctrl *Ctrl) GetTeamResourceLimit(c *fox.Context) (*service.LimitRecord, error) {
	if c.Request.URL.RawQuery != "" {
		return nil, apperrors.ErrBadRequest
	}
	result, err := ctrl.service.GetTeamResourceLimit(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("team_id"), c.Param("user_id"))
	if err == nil {
		c.Header("ETag", `"`+result.ETag+`"`)
	}
	return result, err
}

func (ctrl *Ctrl) SetTeamResourceLimit(c *fox.Context) (*service.LimitRecord, error) {
	if c.Request.URL.RawQuery != "" {
		return nil, apperrors.ErrBadRequest
	}
	var input service.TeamLimitInput
	if err := decodeStrictRequest(c, &input); err != nil {
		return nil, err
	}
	values := c.Request.Header.Values("If-Match")
	if len(values) != 1 {
		return nil, apperrors.ErrBadRequest
	}
	raw := strings.TrimSpace(values[0])
	if len(raw) != 66 || raw[0] != '"' || raw[65] != '"' {
		return nil, apperrors.ErrBadRequest
	}
	result, err := ctrl.service.SetTeamResourceLimit(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("team_id"), c.Param("user_id"), raw[1:65], input)
	if err == nil {
		c.Header("ETag", `"`+result.ETag+`"`)
	}
	return result, err
}
