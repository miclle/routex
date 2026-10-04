package handler

import (
	"strconv"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func memberKeyListFilter(c *fox.Context) (service.MemberKeyFilter, error) {
	query, err := parseTeamQuotaQuery(c, map[string]bool{"cursor": true, "limit": true})
	if err != nil {
		return service.MemberKeyFilter{}, err
	}
	limit := 40
	if raw := query["limit"]; raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || strconv.Itoa(limit) != raw || limit < 1 || limit > 100 {
			return service.MemberKeyFilter{}, apperrors.ErrBadRequest
		}
	}
	return service.MemberKeyFilter{Cursor: query["cursor"], Limit: limit}, nil
}

func (ctrl *Ctrl) ListMemberKeys(c *fox.Context) (*service.MemberKeyPage, error) {
	c.Header("Cache-Control", "no-store")
	filter, err := memberKeyListFilter(c)
	if err != nil {
		return nil, err
	}
	return ctrl.service.ListMemberKeys(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("user_id"), filter)
}

func (ctrl *Ctrl) GetMemberKey(c *fox.Context) (*service.MemberKeyRecord, error) {
	c.Header("Cache-Control", "no-store")
	if err := noTeamQuotaQuery(c); err != nil {
		return nil, err
	}
	record, err := ctrl.service.GetMemberKey(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("user_id"), c.Param("key_id"))
	if err == nil {
		c.Header("ETag", strconv.Quote(record.ETag))
	}
	return record, err
}

func (ctrl *Ctrl) DisableMemberKey(c *fox.Context) (*service.MemberKeyDisableRecord, error) {
	c.Header("Cache-Control", "no-store")
	if err := noTeamQuotaQuery(c); err != nil {
		return nil, err
	}
	var input service.MemberKeyDisableInput
	if err := decodeStrictRequest(c, &input); err != nil {
		return nil, err
	}
	etag, err := quotaETag(c)
	if err != nil {
		return nil, err
	}
	record, err := ctrl.service.DisableMemberKey(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("user_id"), c.Param("key_id"), etag, input)
	if err == nil {
		c.Header("ETag", strconv.Quote(record.ETag))
	}
	return record, err
}
