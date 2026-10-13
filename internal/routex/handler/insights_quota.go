package handler

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func insightsQuotaInput(request *http.Request, keys bool) (service.InsightsQuotaInput, error) {
	input := service.InsightsQuotaInput{}
	if len(request.URL.RawQuery) > 512 {
		return input, apperrors.ErrBadRequest
	}
	values, err := url.ParseQuery(request.URL.RawQuery)
	if err != nil {
		return input, apperrors.ErrBadRequest
	}
	for name, entries := range values {
		allowed := name == "dimension" || name == "limit" || name == "cursor" || keys && (name == "scope_kind" || name == "scope_id")
		if !allowed || len(entries) != 1 || entries[0] == "" {
			return input, apperrors.ErrBadRequest
		}
	}
	input.Dimension, input.ScopeKind, input.ScopeID, input.Cursor = values.Get("dimension"), values.Get("scope_kind"), values.Get("scope_id"), values.Get("cursor")
	if input.Dimension != "tokens" && input.Dimension != "money" || len(input.Cursor) > 160 {
		return input, apperrors.ErrBadRequest
	}
	if keys && (input.ScopeKind != "member" && input.ScopeKind != "project" || input.ScopeID == "" || len(input.ScopeID) > 30) {
		return input, apperrors.ErrBadRequest
	}
	if raw := values.Get("limit"); raw != "" {
		input.Limit, err = strconv.Atoi(raw)
		if err != nil || input.Limit < 1 || input.Limit > 50 || strconv.Itoa(input.Limit) != raw {
			return input, apperrors.ErrBadRequest
		}
	}
	return input, nil
}

func (ctrl *Ctrl) InsightsTeamQuota(c *fox.Context) (*service.InsightsQuotaReport, error) {
	return ctrl.insightsQuota(c, false)
}
func (ctrl *Ctrl) InsightsKeyQuota(c *fox.Context) (*service.InsightsQuotaReport, error) {
	return ctrl.insightsQuota(c, true)
}
func (ctrl *Ctrl) insightsQuota(c *fox.Context, keys bool) (*service.InsightsQuotaReport, error) {
	input, err := insightsQuotaInput(c.Request, keys)
	if err != nil {
		return nil, err
	}
	return ctrl.service.InsightsQuota(c.Request.Context(), currentAuthentication(c).User.ID, input, keys)
}

func (ctrl *Ctrl) ExportInsightsTeamQuota(c *fox.Context) error {
	return ctrl.exportInsightsQuota(c, false)
}
func (ctrl *Ctrl) ExportInsightsKeyQuota(c *fox.Context) error {
	return ctrl.exportInsightsQuota(c, true)
}
func (ctrl *Ctrl) exportInsightsQuota(c *fox.Context, keys bool) error {
	input, err := insightsQuotaInput(c.Request, keys)
	if err != nil {
		return err
	}
	result, err := ctrl.service.ExportInsightsQuota(c.Request.Context(), currentAuthentication(c).User.ID, input, keys)
	if err != nil {
		return err
	}
	filename := "routex-team-quota.csv"
	if keys {
		filename = "routex-key-quota.csv"
	}
	writeUsageCSV(c, result, filename)
	return nil
}
