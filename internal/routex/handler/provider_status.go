package handler

import (
	"io"
	"strconv"

	"github.com/fox-gonic/fox"

	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func (ctrl *Ctrl) GetProviderStatus(c *fox.Context) (*service.ProviderStatusRecord, error) {
	if err := providerMetadataRequest(c); err != nil {
		return nil, err
	}
	result, err := ctrl.service.GetProviderStatus(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("provider_id"))
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ETag))
	}
	return result, err
}
func (ctrl *Ctrl) WriteProviderStatus(c *fox.Context) (*service.ProviderStatusWriteResult, error) {
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
	var input service.ProviderStatusInput
	if err := input.UnmarshalJSON(raw); err != nil {
		return nil, err
	}
	result, err := ctrl.service.WriteProviderStatus(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("provider_id"), etag, input)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.Provider.ETag))
	}
	return result, err
}
