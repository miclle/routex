package handler

import (
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func vaultRequest(c *fox.Context) error {
	c.Header("Cache-Control", "private, no-store")
	return noTeamQuotaQuery(c)
}
func vaultHeader(c *fox.Context) (string, error) {
	if len(c.Request.Header.Values("If-Match")) == 0 {
		return "", &apperrors.Error{Code: 428, Message: "review required"}
	}
	return connectionMetadataETag(c)
}
func vaultBody(c *fox.Context, target interface{ UnmarshalJSON([]byte) error }) error {
	raw, e := io.ReadAll(io.LimitReader(c.Request.Body, (64<<10)+1))
	if e != nil || len(raw) > 64<<10 {
		return apperrors.ErrBadRequest
	}
	return target.UnmarshalJSON(raw)
}
func (ctrl *Ctrl) ListVaultIntegrations(c *fox.Context) (*service.VaultIntegrationPage, error) {
	c.Header("Cache-Control", "private, no-store")
	q, queryErr := url.ParseQuery(c.Request.URL.RawQuery)
	if queryErr != nil {
		return nil, apperrors.ErrBadRequest
	}
	if c.Request.URL.ForceQuery {
		return nil, apperrors.ErrBadRequest
	}
	for k, v := range q {
		if (k != "cursor" && k != "limit") || len(v) != 1 || v[0] == "" {
			return nil, apperrors.ErrBadRequest
		}
	}
	limit := 50
	if v := q.Get("limit"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil || strconv.Itoa(n) != v {
			return nil, apperrors.ErrBadRequest
		}
		limit = n
	}
	v, e := ctrl.service.ListVaultIntegrations(c.Request.Context(), currentAuthentication(c).User.ID, q.Get("cursor"), limit)
	if e == nil {
		c.Header("ETag", strconv.Quote(v.ReviewETag))
	}
	return v, e
}
func (ctrl *Ctrl) GetVaultIntegration(c *fox.Context) (*service.VaultIntegrationView, error) {
	if e := vaultRequest(c); e != nil {
		return nil, e
	}
	v, e := ctrl.service.GetVaultIntegration(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("integration_id"))
	if e == nil {
		c.Header("ETag", strconv.Quote(v.ReviewETag))
	}
	return v, e
}
func (ctrl *Ctrl) CreateVaultIntegration(c *fox.Context) (*service.VaultConfigResult, error) {
	return ctrl.saveVaultIntegration(c, "")
}
func (ctrl *Ctrl) WriteVaultIntegration(c *fox.Context) (*service.VaultConfigResult, error) {
	return ctrl.saveVaultIntegration(c, c.Param("integration_id"))
}
func (ctrl *Ctrl) saveVaultIntegration(c *fox.Context, target string) (*service.VaultConfigResult, error) {
	if e := vaultRequest(c); e != nil {
		return nil, e
	}
	etag, e := vaultHeader(c)
	if e != nil {
		return nil, e
	}
	var input service.VaultConfigInput
	if e = vaultBody(c, &input); e != nil {
		return nil, e
	}
	return ctrl.service.SaveVaultIntegration(c.Request.Context(), currentAuthentication(c).User.ID, target, etag, input)
}
func (ctrl *Ctrl) GetVaultProbe(c *fox.Context) (*service.VaultProbeView, error) {
	if e := vaultRequest(c); e != nil {
		return nil, e
	}
	v, e := ctrl.service.GetVaultProbe(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("integration_id"), c.Param("probe_id"))
	if e == nil {
		c.Header("ETag", strconv.Quote(v.ReviewETag))
	}
	return v, e
}
func (ctrl *Ctrl) WriteVaultProbe(c *fox.Context) error   { return ctrl.runVaultProbe(c, "write") }
func (ctrl *Ctrl) ReadVaultProbe(c *fox.Context) error    { return ctrl.runVaultProbe(c, "read") }
func (ctrl *Ctrl) CleanupVaultProbe(c *fox.Context) error { return ctrl.runVaultProbe(c, "cleanup") }
func (ctrl *Ctrl) runVaultProbe(c *fox.Context, kind string) error {
	if e := vaultRequest(c); e != nil {
		return e
	}
	etag, e := vaultHeader(c)
	if e != nil {
		return e
	}
	var input service.VaultStageInput
	if e = vaultBody(c, &input); e != nil {
		return e
	}
	v, running, e := ctrl.service.RunVaultProbe(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("integration_id"), c.Param("probe_id"), kind, etag, input)
	if e != nil {
		return e
	}
	c.Header("ETag", strconv.Quote(v.ReviewETag))
	status := http.StatusOK
	if running {
		status = http.StatusAccepted
	}
	c.JSON(status, v)
	return nil
}
