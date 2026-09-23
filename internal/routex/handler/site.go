package handler

import (
	"net/http"
	"net/url"

	"github.com/fox-gonic/fox"
	"github.com/fox-gonic/fox/render"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func (ctrl *Ctrl) SiteSettings(c *fox.Context) (*entity.SiteSetting, error) {
	return ctrl.service.SiteSettings(c.Request.Context())
}
func (ctrl *Ctrl) UpdateSiteSettings(c *fox.Context) (*entity.SiteSetting, error) {
	var input service.SiteInput
	if err := decodeStrictRequest(c, &input); err != nil {
		return nil, err
	}
	return ctrl.service.WriteSiteSettings(c.Request.Context(), currentAuthentication(c).User.ID, input)
}
func (ctrl *Ctrl) ActiveAnnouncements(c *fox.Context) (*service.AnnouncementPage, error) {
	if c.Request.URL.RawQuery != "" {
		return nil, apperrors.ErrBadRequest
	}
	return ctrl.service.ListAnnouncements(c.Request.Context(), currentAuthentication(c).User.ID, false, "")
}
func (ctrl *Ctrl) AdminAnnouncements(c *fox.Context) (*service.AnnouncementPage, error) {
	values, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		return nil, apperrors.ErrBadRequest
	}
	for key, value := range values {
		if key != "cursor" || len(value) != 1 || value[0] == "" {
			return nil, apperrors.ErrBadRequest
		}
	}
	return ctrl.service.ListAnnouncements(c.Request.Context(), currentAuthentication(c).User.ID, true, values.Get("cursor"))
}
func (ctrl *Ctrl) PublishAnnouncement(c *fox.Context) error {
	var input service.AnnouncementInput
	if err := decodeStrictRequest(c, &input); err != nil {
		return err
	}
	result, err := ctrl.service.WriteAnnouncement(c.Request.Context(), currentAuthentication(c).User.ID, "", input, false)
	if err != nil {
		return err
	}
	c.Render(http.StatusCreated, render.JSON{Data: result})
	return nil
}

func (ctrl *Ctrl) UpdateAnnouncement(c *fox.Context) (*entity.Announcement, error) {
	var input service.AnnouncementInput
	if err := decodeStrictRequest(c, &input); err != nil {
		return nil, err
	}
	return ctrl.service.WriteAnnouncement(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("announcement_id"), input, false)
}
func (ctrl *Ctrl) CloseAnnouncement(c *fox.Context) (*entity.Announcement, error) {
	var input struct {
		ETag string `json:"etag"`
	}
	if err := decodeStrictRequest(c, &input); err != nil {
		return nil, err
	}
	return ctrl.service.WriteAnnouncement(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("announcement_id"), service.AnnouncementInput{ETag: input.ETag}, true)
}
