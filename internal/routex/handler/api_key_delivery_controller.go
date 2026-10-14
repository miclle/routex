package handler

import (
	"net/http"
	"strings"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

// A machine credential is accepted only in one canonical Authorization field.
// Cookies, inference Keys, CSRF and Vault caller credentials are not substitutes.
func apiKeyDeliveryControllerBearer(c *fox.Context) (string, error) {
	if err := apiKeyDeliveryRequest(c); err != nil {
		return "", err
	}
	values := c.Request.Header.Values("Authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer rxdctl_") || strings.ContainsAny(values[0], "\t\r\n,") || strings.Count(values[0], " ") != 1 {
		return "", apperrors.ErrUnauthorized
	}
	return strings.TrimPrefix(values[0], "Bearer "), nil
}

func (ctrl *Ctrl) ReviewAPIKeyDeliveryControllerConfiguration(c *fox.Context) error {
	if err := apiKeyDeliveryRequest(c); err != nil {
		return err
	}
	v, err := ctrl.service.ReviewAPIKeyDeliveryControllerConfiguration(c.Request.Context(), currentAuthentication(c), c.Param("slot_id"))
	if err != nil {
		return err
	}
	if err = apiKeyDeliveryReplyETag(c, v.ReviewETag); err != nil {
		return err
	}
	c.JSON(http.StatusOK, v)
	return nil
}

func (ctrl *Ctrl) GetAPIKeyDeliveryController(c *fox.Context) error {
	if err := apiKeyDeliveryRequest(c); err != nil {
		return err
	}
	v, err := ctrl.service.APIKeyDeliveryController(c.Request.Context(), currentAuthentication(c), c.Param("controller_id"))
	if err != nil {
		return err
	}
	if err = apiKeyDeliveryReplyETag(c, v.ReviewETag); err != nil {
		return err
	}
	c.JSON(http.StatusOK, v)
	return nil
}

type apiKeyDeliveryControllerEnrollmentResponse struct {
	Controller   service.APIKeyDeliveryControllerView `json:"controller"`
	Bearer       *string                              `json:"bearer"`
	SecretStatus string                               `json:"secret_status"`
}

func (ctrl *Ctrl) EnrollAPIKeyDeliveryController(c *fox.Context) error {
	if err := apiKeyDeliveryRequest(c); err != nil {
		return err
	}
	etag, err := apiKeyDeliveryReview(c)
	if err != nil {
		return err
	}
	var in service.APIKeyDeliveryControllerEnrollmentInput
	if err = apiKeyDeliveryBody(c, &in); err != nil {
		return err
	}
	v, err := ctrl.service.EnrollAPIKeyDeliveryController(c.Request.Context(), currentAuthentication(c), c.Param("slot_id"), etag, in)
	if err != nil {
		return err
	}
	if err = apiKeyDeliveryReplyETag(c, v.Controller.ReviewETag); err != nil {
		return err
	}
	response := apiKeyDeliveryControllerEnrollmentResponse{Controller: v.Controller, SecretStatus: "not_recoverable"}
	status := http.StatusOK
	if v.Created {
		if v.Bearer == "" {
			return apperrors.ErrInternal
		}
		response.Bearer, response.SecretStatus, status = &v.Bearer, "display_once", http.StatusCreated
	}
	c.JSON(status, response)
	return nil
}

func (ctrl *Ctrl) AssignAPIKeyDeliveryControllerConfiguration(c *fox.Context) error {
	if err := apiKeyDeliveryRequest(c); err != nil {
		return err
	}
	etag, err := apiKeyDeliveryReview(c)
	if err != nil {
		return err
	}
	var in service.APIKeyDeliveryControllerAssignmentInput
	if err = apiKeyDeliveryBody(c, &in); err != nil {
		return err
	}
	v, err := ctrl.service.AssignAPIKeyDeliveryControllerConfiguration(c.Request.Context(), currentAuthentication(c), c.Param("controller_id"), etag, in)
	if err != nil {
		return err
	}
	if err = apiKeyDeliveryReplyETag(c, v.ReviewETag); err != nil {
		return err
	}
	c.JSON(http.StatusOK, v)
	return nil
}

func (ctrl *Ctrl) RevokeAPIKeyDeliveryController(c *fox.Context) error {
	if err := apiKeyDeliveryRequest(c); err != nil {
		return err
	}
	etag, err := apiKeyDeliveryReview(c)
	if err != nil {
		return err
	}
	var in apiKeyDeliveryReasonInput
	if err = apiKeyDeliveryBody(c, &in); err != nil {
		return err
	}
	v, err := ctrl.service.RevokeAPIKeyDeliveryController(c.Request.Context(), currentAuthentication(c), c.Param("controller_id"), etag, in.Reason)
	if err != nil {
		return err
	}
	if err = apiKeyDeliveryReplyETag(c, v.ReviewETag); err != nil {
		return err
	}
	c.JSON(http.StatusOK, v)
	return nil
}

func (ctrl *Ctrl) PullAPIKeyDeliveryControllerConfiguration(c *fox.Context) error {
	bearer, err := apiKeyDeliveryControllerBearer(c)
	if err != nil {
		return err
	}
	v, err := ctrl.service.PullAPIKeyDeliveryControllerConfiguration(c.Request.Context(), bearer)
	if err != nil {
		return err
	}
	if err = apiKeyDeliveryReplyETag(c, v.ConfigurationDigest); err != nil {
		return err
	}
	c.JSON(http.StatusOK, v)
	return nil
}

func (ctrl *Ctrl) AcknowledgeAPIKeyDeliveryControllerConfiguration(c *fox.Context) error {
	bearer, err := apiKeyDeliveryControllerBearer(c)
	if err != nil {
		return err
	}
	var in service.APIKeyDeliveryControllerAcknowledgementInput
	if err = apiKeyDeliveryBody(c, &in); err != nil {
		return err
	}
	v, err := ctrl.service.AcknowledgeAPIKeyDeliveryControllerConfiguration(c.Request.Context(), bearer, in)
	if err != nil {
		return err
	}
	c.JSON(http.StatusOK, v)
	return nil
}
