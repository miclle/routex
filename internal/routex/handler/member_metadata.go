package handler

import (
	"io"
	"strconv"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

// memberMetadataResponseHeaders precedes Session/CSRF middleware in central registration
// so authentication and management denials cannot be cached.
func memberMetadataResponseHeaders(c *fox.Context) error {
	c.Header("Cache-Control", "private, no-store")
	c.Next()
	return nil
}

func memberMetadataRequest(c *fox.Context) error {
	c.Header("Cache-Control", "private, no-store")
	if err := noTeamQuotaQuery(c); err != nil {
		return err
	}
	if !memberOverviewUserID.MatchString(c.Param("user_id")) {
		return apperrors.ErrBadRequest
	}
	return nil
}
func memberMetadataETag(c *fox.Context) (string, error) {
	etag, err := quotaETag(c)
	if err != nil || len(etag) != 64 {
		return "", apperrors.ErrBadRequest
	}
	for _, r := range etag {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return "", apperrors.ErrBadRequest
		}
	}
	return etag, nil
}
func (ctrl *Ctrl) GetMemberMetadata(c *fox.Context) (*service.MemberMetadataRecord, error) {
	if err := memberMetadataRequest(c); err != nil {
		return nil, err
	}
	result, err := ctrl.service.GetMemberMetadata(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("user_id"))
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ETag))
	}
	return result, err
}
func (ctrl *Ctrl) SetMemberMetadata(c *fox.Context) (*service.MemberMetadataWriteResult, error) {
	if err := memberMetadataRequest(c); err != nil {
		return nil, err
	}
	etag, err := memberMetadataETag(c)
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 64*1024+1))
	if err != nil || len(raw) > 64*1024 {
		return nil, apperrors.ErrBadRequest
	}
	var input service.MemberMetadataInput
	if err := input.UnmarshalJSON(raw); err != nil {
		return nil, err
	}
	result, err := ctrl.service.SetMemberMetadata(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("user_id"), etag, input)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ETag))
	}
	return result, err
}
