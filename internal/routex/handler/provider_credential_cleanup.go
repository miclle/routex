package handler

import (
	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"net/http"
	"net/url"
	"strconv"
)

func (ctrl *Ctrl) ListProviderCredentialOrphans(c *fox.Context) (*service.ProviderCredentialOrphanPage, error) {
	c.Header("Cache-Control", "private, no-store")
	q, e := url.ParseQuery(c.Request.URL.RawQuery)
	if e != nil || c.Request.URL.ForceQuery {
		return nil, apperrors.ErrBadRequest
	}
	for k, values := range q {
		if (k != "limit" && k != "cursor") || len(values) != 1 || values[0] == "" {
			return nil, apperrors.ErrBadRequest
		}
	}
	limit := 20
	if raw := q.Get("limit"); raw != "" {
		limit, e = strconv.Atoi(raw)
		if e != nil || strconv.Itoa(limit) != raw {
			return nil, apperrors.ErrBadRequest
		}
	}
	return ctrl.service.ListProviderCredentialOrphans(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("integration_id"), q.Get("cursor"), limit)
}
func (ctrl *Ctrl) GetProviderCredentialOrphan(c *fox.Context) (*service.ProviderCredentialOrphanView, error) {
	if e := vaultRequest(c); e != nil {
		return nil, e
	}
	v, e := ctrl.service.GetProviderCredentialOrphan(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("integration_id"), c.Param("creation_request_id"))
	if e == nil {
		c.Header("ETag", strconv.Quote(v.ReviewETag))
	}
	return v, e
}
func (ctrl *Ctrl) GetProviderCredentialCleanup(c *fox.Context) (*service.ProviderCredentialCleanupReceipt, error) {
	if e := vaultRequest(c); e != nil {
		return nil, e
	}
	return ctrl.service.GetProviderCredentialCleanup(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("integration_id"), c.Param("creation_request_id"), c.Param("command_id"))
}
func (ctrl *Ctrl) CleanupProviderCredentialOrphan(c *fox.Context) error {
	if e := vaultRequest(c); e != nil {
		return e
	}
	review, e := vaultHeader(c)
	if e != nil {
		return e
	}
	var input service.ProviderCredentialCleanupInput
	if e = vaultBody(c, &input); e != nil {
		return e
	}
	result, e := ctrl.service.CleanupProviderCredentialOrphan(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("integration_id"), c.Param("creation_request_id"), review, input)
	if e != nil {
		return e
	}
	status := http.StatusOK
	if result.Running {
		status = http.StatusAccepted
	}
	c.JSON(status, result)
	return nil
}
