package handler

import (
	"io"
	"strconv"
	"strings"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func providerMetadataRequest(c *fox.Context) error {
	c.Header("Cache-Control", "private, no-store")
	if err := noTeamQuotaQuery(c); err != nil {
		return err
	}
	target := c.Param("provider_id")
	if !memberOverviewUserID.MatchString(target) || !strings.HasPrefix(target, "prv_") {
		return apperrors.ErrBadRequest
	}
	return nil
}
func (ctrl *Ctrl) GetProviderMetadata(c *fox.Context) (*service.ProviderMetadataRecord, error) {
	if err := providerMetadataRequest(c); err != nil {
		return nil, err
	}
	result, err := ctrl.service.GetProviderMetadata(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("provider_id"))
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ETag))
	}
	return result, err
}
func (ctrl *Ctrl) WriteProviderMetadata(c *fox.Context) (*service.ProviderMetadataWriteResult, error) {
	if err := providerMetadataRequest(c); err != nil {
		return nil, err
	}
	etag, err := connectionMetadataETag(c)
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 64*1024+1))
	if err != nil || len(raw) > 64*1024 {
		return nil, apperrors.ErrBadRequest
	}
	var input service.ProviderMetadataInput
	if err := input.UnmarshalJSON(raw); err != nil {
		return nil, err
	}
	result, err := ctrl.service.WriteProviderMetadata(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("provider_id"), etag, input)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.Provider.ETag))
	}
	return result, err
}
