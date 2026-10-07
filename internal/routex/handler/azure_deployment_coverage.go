package handler

import (
	"io"
	"strconv"
	"strings"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func deploymentCoverageRequest(c *fox.Context) error {
	c.Header("Cache-Control", "private, no-store")
	if err := noTeamQuotaQuery(c); err != nil {
		return err
	}
	if !memberOverviewUserID.MatchString(c.Param("credential_id")) || !strings.HasPrefix(c.Param("credential_id"), "crd_") {
		return apperrors.ErrBadRequest
	}
	return nil
}
func (ctrl *Ctrl) GetDeploymentCoverage(c *fox.Context) (*service.DeploymentCoverageRecord, error) {
	if err := deploymentCoverageRequest(c); err != nil {
		return nil, err
	}
	result, err := ctrl.service.GetDeploymentCoverage(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("credential_id"))
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ETag))
	}
	return result, err
}
func (ctrl *Ctrl) WriteDeploymentCoverage(c *fox.Context) (*service.DeploymentCoverageWriteResult, error) {
	if err := deploymentCoverageRequest(c); err != nil {
		return nil, err
	}
	etag, err := connectionMetadataETag(c)
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, (256<<10)+1))
	if err != nil || len(raw) > 256<<10 {
		return nil, apperrors.ErrBadRequest
	}
	var input service.DeploymentCoverageInput
	if err = input.UnmarshalJSON(raw); err != nil {
		return nil, err
	}
	result, err := ctrl.service.WriteDeploymentCoverage(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("credential_id"), etag, input)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.Coverage.ETag))
	}
	return result, err
}
