package handler

import (
	"github.com/fox-gonic/fox"

	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func (ctrl *Ctrl) ProjectOverview(c *fox.Context) (*service.ProjectOverview, error) {
	if c.Request.URL.RawQuery != "" {
		return nil, apperrors.ErrBadRequest
	}
	return ctrl.service.ProjectOverview(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("project_id"))
}
