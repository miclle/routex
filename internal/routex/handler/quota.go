package handler

import (
	"strconv"
	"strings"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func quotaETag(c *fox.Context) (string, error) {
	headers := c.Request.Header.Values("If-Match")
	if len(headers) != 1 {
		return "", apperrors.ErrBadRequest
	}
	raw := strings.TrimSpace(headers[0])
	if len(raw) < 3 || len(raw) > 66 || raw[0] != '"' || raw[len(raw)-1] != '"' || strings.Contains(raw[1:len(raw)-1], `"`) {
		return "", apperrors.ErrBadRequest
	}
	return raw[1 : len(raw)-1], nil
}
func (ctrl *Ctrl) GetReservationBound(c *fox.Context) (*service.ReservationBoundRecord, error) {
	result, err := ctrl.service.GetReservationBound(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("provider_model_id"))
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ETag))
	}
	return result, err
}
func (ctrl *Ctrl) WriteReservationBound(c *fox.Context) (*service.ReservationBoundRecord, error) {
	var input service.ReservationBoundInput
	if err := decodeStrictRequest(c, &input); err != nil {
		return nil, err
	}
	etag, err := quotaETag(c)
	if err != nil {
		return nil, err
	}
	result, err := ctrl.service.WriteReservationBound(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("provider_model_id"), etag, input)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ETag))
	}
	return result, err
}
func (ctrl *Ctrl) GetQuotaSettings(c *fox.Context) (*service.QuotaSettingsRecord, error) {
	result, err := ctrl.service.GetQuotaSettings(c.Request.Context(), currentAuthentication(c).User.ID)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ETag))
	}
	return result, err
}
func (ctrl *Ctrl) WriteQuotaSettings(c *fox.Context) (*service.QuotaSettingsRecord, error) {
	var input service.QuotaSettingsInput
	if err := decodeStrictRequest(c, &input); err != nil {
		return nil, err
	}
	etag, err := quotaETag(c)
	if err != nil {
		return nil, err
	}
	result, err := ctrl.service.WriteQuotaSettings(c.Request.Context(), currentAuthentication(c).User.ID, etag, input)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ETag))
	}
	return result, err
}
