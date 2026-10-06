package handler

import (
	"io"
	"strconv"
	"strings"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func connectionMetadataRequest(c *fox.Context) error {
	c.Header("Cache-Control", "private, no-store")
	if err := noTeamQuotaQuery(c); err != nil {
		return err
	}
	target := c.Param("connection_id")
	if !memberOverviewUserID.MatchString(target) || !strings.HasPrefix(target, "con_") {
		return apperrors.ErrBadRequest
	}
	return nil
}
func connectionMetadataETag(c *fox.Context) (string, error) {
	headers := c.Request.Header.Values("If-Match")
	if len(headers) != 1 {
		return "", apperrors.ErrBadRequest
	}
	raw := strings.TrimSpace(headers[0])
	if len(raw) != 131 || raw[0] != '"' || raw[130] != '"' || raw[65] != '.' {
		return "", apperrors.ErrBadRequest
	}
	etag := raw[1:130]
	for i, c := range etag {
		if i != 64 && (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return "", apperrors.ErrBadRequest
		}
	}
	return etag, nil
}
func (ctrl *Ctrl) GetConnectionMetadata(c *fox.Context) (*service.ConnectionMetadataRecord, error) {
	if err := connectionMetadataRequest(c); err != nil {
		return nil, err
	}
	result, err := ctrl.service.GetConnectionMetadata(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("connection_id"))
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ETag))
	}
	return result, err
}
func (ctrl *Ctrl) WriteConnectionMetadata(c *fox.Context) (*service.ConnectionMetadataWriteResult, error) {
	if err := connectionMetadataRequest(c); err != nil {
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
	var input service.ConnectionMetadataInput
	if err := input.UnmarshalJSON(raw); err != nil {
		return nil, err
	}
	result, err := ctrl.service.WriteConnectionMetadata(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("connection_id"), etag, input)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.Connection.ETag))
	}
	return result, err
}
