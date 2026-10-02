package handler

import (
	"net/http"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/service"
)

func (ctrl *Ctrl) CreateCredentialReplacement(c *fox.Context) error {
	var input service.CredentialReplacementInput
	if err := decodeStrictRequest(c, &input); err != nil {
		return err
	}
	etag, err := quotaETag(c)
	if err != nil {
		return err
	}
	result, created, err := ctrl.service.CreateCredentialReplacement(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("credential_id"), etag, input)
	if err != nil {
		return err
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	// Explicit rendering preserves the distinct first-creation/retry status;
	// this fox version's automatic DTO renderer overwrites a prior c.Status.
	c.JSON(status, result)
	return nil
}
