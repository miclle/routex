package handler

import (
	"net/http"
	"strconv"

	"github.com/fox-gonic/fox"

	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func personalModelHeader(c *fox.Context) (string, error) {
	value, err := projectRequestReviewHeader(c, true)
	if err != nil || len(value) != 64 {
		return "", apperrors.ErrBadRequest
	}
	for _, b := range value {
		if (b < '0' || b > '9') && (b < 'a' || b > 'f') {
			return "", apperrors.ErrBadRequest
		}
	}
	return value, nil
}
func (ctrl *Ctrl) CreatePersonalModelRequest(c *fox.Context) error {
	if err := noTeamQuotaQuery(c); err != nil {
		return err
	}
	var input service.PersonalModelRequestInput
	if err := decodeStrictRequest(c, &input); err != nil {
		return err
	}
	etag, err := personalModelHeader(c)
	if err != nil {
		return err
	}
	result, created, err := ctrl.service.CreatePersonalModelRequest(c.Request.Context(), currentAuthentication(c).User.ID, etag, input)
	if err != nil {
		return err
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	c.Header("ETag", `"`+result.ReviewETag+`"`)
	c.JSON(status, result)
	return nil
}
func personalModelRequestFilter(c *fox.Context) (service.PersonalModelRequestFilter, error) {
	q, err := parseTeamQuotaQuery(c, map[string]bool{"status": true, "cursor": true, "limit": true})
	if err != nil {
		return service.PersonalModelRequestFilter{}, err
	}
	limit := 0
	if q["limit"] != "" {
		limit, err = strconv.Atoi(q["limit"])
		if err != nil || limit < 1 || limit > 50 || strconv.Itoa(limit) != q["limit"] {
			return service.PersonalModelRequestFilter{}, apperrors.ErrBadRequest
		}
	}
	return service.PersonalModelRequestFilter{Status: q["status"], Cursor: q["cursor"], Limit: limit}, nil
}
func (ctrl *Ctrl) ListPersonalModelRequests(c *fox.Context) (*service.PersonalModelRequestPage, error) {
	return ctrl.listPersonalModelRequests(c, false)
}
func (ctrl *Ctrl) ListMemberModelRequests(c *fox.Context) (*service.PersonalModelRequestPage, error) {
	return ctrl.listPersonalModelRequests(c, true)
}
func personalModelRequestUser(c *fox.Context, reviewer bool) string {
	if reviewer {
		return c.Param("user_id")
	}
	return currentAuthentication(c).User.ID
}
func (ctrl *Ctrl) listPersonalModelRequests(c *fox.Context, reviewer bool) (*service.PersonalModelRequestPage, error) {
	filter, err := personalModelRequestFilter(c)
	if err != nil {
		return nil, err
	}
	return ctrl.service.ListPersonalModelRequests(c.Request.Context(), currentAuthentication(c).User.ID, personalModelRequestUser(c, reviewer), filter, reviewer)
}
func (ctrl *Ctrl) GetPersonalModelRequest(c *fox.Context) (*service.PersonalModelRequestDetail, error) {
	return ctrl.getPersonalModelRequest(c, false)
}
func (ctrl *Ctrl) GetMemberModelRequest(c *fox.Context) (*service.PersonalModelRequestDetail, error) {
	return ctrl.getPersonalModelRequest(c, true)
}
func (ctrl *Ctrl) getPersonalModelRequest(c *fox.Context, reviewer bool) (*service.PersonalModelRequestDetail, error) {
	if err := noTeamQuotaQuery(c); err != nil {
		return nil, err
	}
	result, err := ctrl.service.GetPersonalModelRequest(c.Request.Context(), currentAuthentication(c).User.ID, personalModelRequestUser(c, reviewer), c.Param("request_id"), reviewer)
	if err == nil {
		c.Header("ETag", `"`+result.ReviewETag+`"`)
	}
	return result, err
}
func (ctrl *Ctrl) DecidePersonalModelRequest(c *fox.Context) (*service.PersonalModelDecisionRecord, error) {
	return ctrl.decidePersonalModelRequest(c, false)
}
func (ctrl *Ctrl) DecideMemberModelRequest(c *fox.Context) (*service.PersonalModelDecisionRecord, error) {
	return ctrl.decidePersonalModelRequest(c, true)
}
func (ctrl *Ctrl) decidePersonalModelRequest(c *fox.Context, reviewer bool) (*service.PersonalModelDecisionRecord, error) {
	if err := noTeamQuotaQuery(c); err != nil {
		return nil, err
	}
	var input service.PersonalModelDecisionInput
	if err := decodeStrictRequest(c, &input); err != nil {
		return nil, err
	}
	etag, err := personalModelHeader(c)
	if err != nil {
		return nil, err
	}
	return ctrl.service.DecidePersonalModelRequest(c.Request.Context(), currentAuthentication(c).User.ID, personalModelRequestUser(c, reviewer), c.Param("request_id"), etag, input, reviewer)
}
func (ctrl *Ctrl) MemberModelAccessWorkspace(c *fox.Context) (*service.MemberModelAccessWorkspace, error) {
	if err := noTeamQuotaQuery(c); err != nil {
		return nil, err
	}
	return ctrl.service.MemberModelAccessWorkspace(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("user_id"))
}
