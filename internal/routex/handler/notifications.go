package handler

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/fox-gonic/fox"

	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func notificationFilter(rawQuery string) (service.NotificationFilter, error) {
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return service.NotificationFilter{}, apperrors.ErrBadRequest
	}
	for key, entries := range values {
		if len(entries) != 1 || entries[0] == "" {
			return service.NotificationFilter{}, apperrors.ErrBadRequest
		}
		switch key {
		case "status", "severity", "cursor", "limit":
		default:
			return service.NotificationFilter{}, apperrors.ErrBadRequest
		}
	}
	filter := service.NotificationFilter{Severity: values.Get("severity"), Cursor: values.Get("cursor")}
	if value := values.Get("status"); value != "" {
		if value != "unread" && value != "all" {
			return service.NotificationFilter{}, apperrors.ErrBadRequest
		}
		filter.UnreadOnly = value == "unread"
	}
	if value := values.Get("limit"); value != "" {
		filter.Limit, err = strconv.Atoi(value)
		if err != nil {
			return service.NotificationFilter{}, apperrors.ErrBadRequest
		}
	}
	return filter, nil
}

func operationalAlertFilter(rawQuery string) (service.OperationalAlertFilter, error) {
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return service.OperationalAlertFilter{}, apperrors.ErrBadRequest
	}
	for key, entries := range values {
		if len(entries) != 1 || entries[0] == "" {
			return service.OperationalAlertFilter{}, apperrors.ErrBadRequest
		}
		switch key {
		case "state", "severity", "cursor", "limit":
		default:
			return service.OperationalAlertFilter{}, apperrors.ErrBadRequest
		}
	}
	filter := service.OperationalAlertFilter{State: values.Get("state"), Severity: values.Get("severity"), Cursor: values.Get("cursor")}
	if value := values.Get("limit"); value != "" {
		filter.Limit, err = strconv.Atoi(value)
		if err != nil {
			return service.OperationalAlertFilter{}, apperrors.ErrBadRequest
		}
	}
	return filter, nil
}

func (ctrl *Ctrl) Notifications(c *fox.Context) (*service.NotificationPage, error) {
	filter, err := notificationFilter(c.Request.URL.RawQuery)
	if err != nil {
		return nil, err
	}
	return ctrl.service.ListNotifications(c.Request.Context(), currentAuthentication(c).User.ID, filter)
}

func (ctrl *Ctrl) MarkNotificationRead(c *fox.Context) (*service.NotificationRecord, error) {
	return ctrl.service.MarkNotificationRead(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("notification_id"))
}

func (ctrl *Ctrl) MarkAllNotificationsRead(c *fox.Context) error {
	if err := ctrl.service.MarkAllNotificationsRead(c.Request.Context(), currentAuthentication(c).User.ID); err != nil {
		return err
	}
	c.Status(http.StatusNoContent)
	return nil
}

func (ctrl *Ctrl) NotificationSettings(c *fox.Context) (*service.NotificationSettingsView, error) {
	if c.Request.URL.RawQuery != "" {
		return nil, apperrors.ErrBadRequest
	}
	return ctrl.service.GetNotificationSettings(c.Request.Context(), currentAuthentication(c).User.ID)
}

func (ctrl *Ctrl) WriteNotificationSettings(c *fox.Context) (*service.NotificationSettingsView, error) {
	var input service.NotificationSettingsInput
	if err := decodeStrictRequest(c, &input); err != nil {
		return nil, err
	}
	return ctrl.service.WriteNotificationSettings(c.Request.Context(), currentAuthentication(c).User.ID, input)
}

func (ctrl *Ctrl) AdminOverview(c *fox.Context) (*service.AdminOverview, error) {
	if c.Request.URL.RawQuery != "" {
		return nil, apperrors.ErrBadRequest
	}
	return ctrl.service.AdministrationOverview(c.Request.Context(), currentAuthentication(c).User.ID)
}

func (ctrl *Ctrl) OperationalAlerts(c *fox.Context) (*service.OperationalAlertPage, error) {
	filter, err := operationalAlertFilter(c.Request.URL.RawQuery)
	if err != nil {
		return nil, err
	}
	return ctrl.service.ListOperationalAlerts(c.Request.Context(), currentAuthentication(c).User.ID, filter)
}

func (ctrl *Ctrl) UpdateOperationalAlert(c *fox.Context) (*service.OperationalAlertRecord, error) {
	var input service.OperationalAlertStateInput
	if err := decodeStrictRequest(c, &input); err != nil {
		return nil, err
	}
	return ctrl.service.UpdateOperationalAlertState(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("alert_id"), input)
}
