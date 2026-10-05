package handler

import (
	"encoding/json"
	"io"
	"net/url"
	"strconv"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func memberRolesRequest(c *fox.Context) error {
	c.Header("Cache-Control", "private, no-store")
	if !memberOverviewUserID.MatchString(c.Param("user_id")) {
		return apperrors.ErrBadRequest
	}
	return noTeamQuotaQuery(c)
}
func memberRolesHeader(c *fox.Context, result any, err error) {
	if err != nil {
		return
	}
	var etag string
	switch value := result.(type) {
	case *service.MemberRolesWorkspace:
		etag = value.ETag
	case *service.MemberRoleCandidatePage:
		etag = value.ETag
	case *service.MemberRoleDetail:
		etag = value.ETag
	case *service.MemberRolesResult:
		etag = value.ETag
	}
	c.Header("ETag", strconv.Quote(etag))
}
func (ctrl *Ctrl) GetMemberRoles(c *fox.Context) (*service.MemberRolesWorkspace, error) {
	if err := memberRolesRequest(c); err != nil {
		return nil, err
	}
	result, err := ctrl.service.GetMemberRoles(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("user_id"))
	memberRolesHeader(c, result, err)
	return result, err
}
func (ctrl *Ctrl) GetMemberRoleDefinition(c *fox.Context) (*service.MemberRoleDetail, error) {
	if err := memberRolesRequest(c); err != nil {
		return nil, err
	}
	if !memberOverviewUserID.MatchString(c.Param("role_id")) {
		return nil, apperrors.ErrBadRequest
	}
	etag, err := memberMetadataETag(c)
	if err != nil {
		return nil, err
	}
	result, err := ctrl.service.GetMemberRoleDefinition(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("user_id"), c.Param("role_id"), etag)
	memberRolesHeader(c, result, err)
	return result, err
}
func memberRoleCandidateFilter(c *fox.Context) (service.MemberRoleCandidateFilter, error) {
	f := service.MemberRoleCandidateFilter{}
	params, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		return f, apperrors.ErrBadRequest
	}
	for key, values := range params {
		if len(values) != 1 {
			return f, apperrors.ErrBadRequest
		}
		switch key {
		case "q":
			f.Query = values[0]
		case "cursor":
			f.Cursor = values[0]
		case "limit":
			f.Limit, err = strconv.Atoi(values[0])
			if err != nil || f.Limit < 1 || f.Limit > 50 {
				return f, apperrors.ErrBadRequest
			}
		default:
			return f, apperrors.ErrBadRequest
		}
	}
	return f, nil
}
func (ctrl *Ctrl) MemberRoleCandidates(c *fox.Context) (*service.MemberRoleCandidatePage, error) {
	c.Header("Cache-Control", "private, no-store")
	if !memberOverviewUserID.MatchString(c.Param("user_id")) {
		return nil, apperrors.ErrBadRequest
	}
	filter, err := memberRoleCandidateFilter(c)
	if err != nil {
		return nil, err
	}
	etag, err := memberMetadataETag(c)
	if err != nil {
		return nil, err
	}
	result, err := ctrl.service.MemberRoleCandidates(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("user_id"), etag, filter)
	memberRolesHeader(c, result, err)
	return result, err
}
func (ctrl *Ctrl) SetReviewedMemberRoles(c *fox.Context) (*service.MemberRolesResult, error) {
	if err := memberRolesRequest(c); err != nil {
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
	var input service.MemberRolesInput
	if json.Unmarshal(raw, &input) != nil {
		return nil, apperrors.ErrBadRequest
	}
	result, err := ctrl.service.SetReviewedMemberRoles(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("user_id"), etag, input)
	memberRolesHeader(c, result, err)
	return result, err
}
