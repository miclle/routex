package handler

import (
	"net/url"
	"strconv"

	"github.com/fox-gonic/fox"

	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func (ctrl *Ctrl) CredentialRetirementReadiness(c *fox.Context) (*service.CredentialRetirementReadiness, error) {
	if len(c.Request.URL.RawQuery) > 256 {
		return nil, apperrors.ErrBadRequest
	}
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil || len(query) != 1 || len(query["replacement_credential_id"]) != 1 {
		return nil, apperrors.ErrBadRequest
	}
	result, err := ctrl.service.GetCredentialRetirementReadiness(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("credential_id"), query.Get("replacement_credential_id"))
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ETag))
	}
	return result, err
}
