package handler

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

// Only canonical bounded pagination is accepted; no hidden filtering or global
// enumeration surface is added to these parent-scoped reads.
func apiKeyDeliveryDiscoveryQuery(raw string, force bool) (string, int, error) {
	if force || len(raw) > 1024 {
		return "", 0, apperrors.ErrBadRequest
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return "", 0, apperrors.ErrBadRequest
	}
	limit := 20
	cursor := ""
	for key, items := range values {
		if len(items) != 1 || items[0] == "" {
			return "", 0, apperrors.ErrBadRequest
		}
		switch key {
		case "cursor":
			cursor = items[0]
			if len(cursor) > 512 {
				return "", 0, apperrors.ErrBadRequest
			}
		case "limit":
			limit, err = strconv.Atoi(items[0])
			if err != nil || limit < 1 || limit > 50 || strconv.Itoa(limit) != items[0] {
				return "", 0, apperrors.ErrBadRequest
			}
		default:
			return "", 0, apperrors.ErrBadRequest
		}
	}
	// Reject alternate encodings, stray separators and malformed bare keys.
	if values.Encode() != raw {
		return "", 0, apperrors.ErrBadRequest
	}
	return cursor, limit, nil
}

func (ctrl *Ctrl) ListAPIKeyDeliverySlots(c *fox.Context) error {
	cursor, limit, err := apiKeyDeliveryDiscoveryQuery(c.Request.URL.RawQuery, c.Request.URL.ForceQuery)
	if err != nil {
		return err
	}
	v, err := ctrl.service.ListAPIKeyDeliverySlots(c.Request.Context(), currentAuthentication(c), c.Param("target_id"), cursor, limit)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, v)
	return nil
}

func (ctrl *Ctrl) ListAPIKeyDeliveryControllers(c *fox.Context) error {
	cursor, limit, err := apiKeyDeliveryDiscoveryQuery(c.Request.URL.RawQuery, c.Request.URL.ForceQuery)
	if err != nil {
		return err
	}
	v, err := ctrl.service.ListAPIKeyDeliveryControllers(c.Request.Context(), currentAuthentication(c), c.Param("slot_id"), cursor, limit)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, v)
	return nil
}

func (ctrl *Ctrl) GetAPIKeyDeliveryControllerEnrollmentReceipt(c *fox.Context) error {
	if err := apiKeyDeliveryRequest(c); err != nil {
		return err
	}
	v, err := ctrl.service.APIKeyDeliveryControllerEnrollmentReceipt(c.Request.Context(), currentAuthentication(c), c.Param("slot_id"), c.Param("request_id"))
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, v)
	return nil
}
