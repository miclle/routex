package handler

import (
	"strconv"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/service"
)

func (ctrl *Ctrl) GetCredentialMetadata(c *fox.Context, path CredentialPath) (*service.CredentialMetadataRecord, error) {
	result, err := ctrl.service.GetCredentialMetadata(c.Request.Context(), currentAuthentication(c).User.ID, path.CredentialID)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ETag))
	}
	return result, err
}

func (ctrl *Ctrl) WriteCredentialMetadata(c *fox.Context) (*service.CredentialMetadataRecord, error) {
	var input service.CredentialMetadataInput
	if err := decodeStrictRequest(c, &input); err != nil {
		return nil, err
	}
	etag, err := quotaETag(c)
	if err != nil {
		return nil, err
	}
	result, err := ctrl.service.WriteCredentialMetadata(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("credential_id"), etag, input)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ETag))
	}
	return result, err
}
