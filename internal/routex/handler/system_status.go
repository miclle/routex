package handler

import (
	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

type SystemInstanceCleanupRequest struct {
	Instances []service.SystemInstanceCleanupTarget `json:"instances"`
}

func (ctrl *Ctrl) SystemInstances(c *fox.Context) (*service.SystemInstancePage, error) {
	if c.Request.URL.RawQuery != "" {
		return nil, apperrors.ErrBadRequest
	}
	return ctrl.service.ListSystemInstances(c.Request.Context(), currentAuthentication(c).User.ID)
}

func (ctrl *Ctrl) SystemJobs(c *fox.Context) (*service.SystemJobPage, error) {
	if c.Request.URL.RawQuery != "" {
		return nil, apperrors.ErrBadRequest
	}
	return ctrl.service.ListSystemJobs(c.Request.Context(), currentAuthentication(c).User.ID)
}

func (ctrl *Ctrl) CleanupSystemInstances(c *fox.Context) (*service.SystemInstanceCleanupResult, error) {
	var input SystemInstanceCleanupRequest
	if err := decodeStrictRequest(c, &input); err != nil {
		return nil, err
	}
	if len(input.Instances) == 0 || len(input.Instances) > 100 {
		return nil, apperrors.ErrBadRequest
	}
	return ctrl.service.CleanupOfflineSystemInstances(c.Request.Context(), currentAuthentication(c).User.ID, input.Instances)
}
