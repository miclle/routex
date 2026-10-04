package handler

import (
	"encoding/json"
	"io"
	"strconv"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func (ctrl *Ctrl) GetMemberModels(c *fox.Context) (*service.MemberModelsWorkspace, error) {
	c.Header("Cache-Control", "no-store")
	if err := noTeamQuotaQuery(c); err != nil {
		return nil, err
	}
	result, err := ctrl.service.MemberModelsWorkspace(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("user_id"))
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ETag))
	}
	return result, err
}
func decodeMemberModels(c *fox.Context) (service.MemberModelsWriteInput, error) {
	var input service.MemberModelsWriteInput
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 64*1024+1))
	if err != nil || len(raw) > 64*1024 || json.Unmarshal(raw, &input) != nil {
		return input, apperrors.ErrBadRequest
	}
	return input, nil
}
func (ctrl *Ctrl) SetMemberModels(c *fox.Context) (*service.MemberModelsWriteResult, error) {
	c.Header("Cache-Control", "no-store")
	if err := noTeamQuotaQuery(c); err != nil {
		return nil, err
	}
	etag, err := quotaETag(c)
	if err != nil {
		return nil, err
	}
	input, err := decodeMemberModels(c)
	if err != nil {
		return nil, err
	}
	result, err := ctrl.service.SetMemberModels(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("user_id"), etag, input)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ETag))
	}
	return result, err
}
