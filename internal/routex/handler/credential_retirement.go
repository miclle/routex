package handler

import (
	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/service"
)

func (ctrl *Ctrl) RetireCredential(c *fox.Context) (*service.CredentialRetirementRecord, error) {
	var input service.CredentialRetirementInput
	if err := decodeStrictRequest(c, &input); err != nil {
		return nil, err
	}
	etag, err := quotaETag(c)
	if err != nil {
		return nil, err
	}
	return ctrl.service.RetireCredential(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("credential_id"), etag, input)
}
