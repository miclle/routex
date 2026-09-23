package handler

import (
	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/service"
)

func (ctrl *Ctrl) RuntimeStatus(c *fox.Context) service.RuntimeStatus {
	return ctrl.service.RuntimeStatus()
}

func (ctrl *Ctrl) PublishRuntime(c *fox.Context) (service.RuntimeStatus, error) {
	if err := ctrl.service.RefreshRuntime(c.Request.Context()); err != nil {
		return service.RuntimeStatus{}, err
	}
	return ctrl.service.RuntimeStatus(), nil
}
