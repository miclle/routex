package handler

import (
	"regexp"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

var memberOverviewUserID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,30}$`)

func memberOverviewSubject(c *fox.Context) (string, error) {
	c.Header("Cache-Control", "private, no-store")
	if err := noTeamQuotaQuery(c); err != nil {
		return "", err
	}
	userID := c.Param("user_id")
	if !memberOverviewUserID.MatchString(userID) {
		return "", apperrors.ErrBadRequest
	}
	return userID, nil
}

func (ctrl *Ctrl) MemberOverview(c *fox.Context) (*service.MemberOverviewRecord, error) {
	userID, err := memberOverviewSubject(c)
	if err != nil {
		return nil, err
	}
	return ctrl.service.MemberOverview(c.Request.Context(), currentAuthentication(c).User.ID, userID)
}
