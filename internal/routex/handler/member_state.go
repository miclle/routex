package handler

import (
	"io"
	"strconv"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func (ctrl *Ctrl) GetMemberState(c *fox.Context) (*service.MemberStateRecord, error) {
	if err := memberMetadataRequest(c); err != nil {
		return nil, err
	}
	result, err := ctrl.service.GetMemberState(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("user_id"))
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ETag))
	}
	return result, err
}
func (ctrl *Ctrl) SetReviewedMemberState(c *fox.Context) (*service.MemberStateResult, error) {
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
	var input service.MemberStateInput
	if err := input.UnmarshalJSON(raw); err != nil {
		return nil, err
	}
	result, err := ctrl.service.SetReviewedMemberState(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("user_id"), etag, input)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ETag))
	}
	return result, err
}
