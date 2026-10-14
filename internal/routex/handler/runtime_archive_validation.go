package handler

import (
	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/service"
)

func (ctrl *Ctrl) ValidateRuntimeArchive(c *fox.Context) (*service.RuntimeArchiveValidation, error) {
	if err := runtimePublicationRequest(c); err != nil {
		return nil, err
	}
	return ctrl.service.ValidateRuntimeArchive(c.Request.Context(), currentAuthentication(c), c.Param("archive_id"))
}
