package handler

import (
	"net/http"

	"github.com/fox-gonic/fox"

	"github.com/miclle/routex/internal/routex/service"
)

type ProjectCallExportRequest struct {
	ProjectID string `uri:"project_id" json:"-"`
	Cursor    string `query:"cursor"`
	Limit     int    `query:"limit"`
	Status    string `query:"status"`
	ModelID   string `query:"model_id"`
	KeyID     string `query:"key_id"`
	UserID    string `query:"user_id"`
	From      string `query:"from"`
	To        string `query:"to"`
}

func (request ProjectCallExportRequest) listCallsRequest() ListCallsRequest {
	return ListCallsRequest{
		Cursor:  request.Cursor,
		Limit:   request.Limit,
		Status:  request.Status,
		ModelID: request.ModelID,
		KeyID:   request.KeyID,
		UserID:  request.UserID,
		From:    request.From,
		To:      request.To,
	}
}

func writeCallCSV(c *fox.Context, result *service.CallCSVExport, filename string) {
	c.Header("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, "text/csv; charset=utf-8", result.CSV)
}

func (ctrl *Ctrl) ExportPersonalCalls(c *fox.Context, request ListCallsRequest) error {
	filter, err := callExportFilter(request, false)
	if err != nil {
		return err
	}
	result, err := ctrl.service.ExportPersonalCalls(c.Request.Context(), currentAuthentication(c).User.ID, filter)
	if err != nil {
		return err
	}
	writeCallCSV(c, result, "routex-personal-calls.csv")
	return nil
}

func (ctrl *Ctrl) ExportProjectCalls(c *fox.Context, request ProjectCallExportRequest) error {
	filter, err := callExportFilter(request.listCallsRequest(), false)
	if err != nil {
		return err
	}
	result, err := ctrl.service.ExportProjectCalls(c.Request.Context(), currentAuthentication(c).User.ID, request.ProjectID, filter)
	if err != nil {
		return err
	}
	writeCallCSV(c, result, "routex-project-calls.csv")
	return nil
}

func (ctrl *Ctrl) ExportAdminCalls(c *fox.Context, request ListCallsRequest) error {
	filter, err := callExportFilter(request, true)
	if err != nil {
		return err
	}
	result, err := ctrl.service.ExportAdminCalls(c.Request.Context(), currentAuthentication(c).User.ID, filter)
	if err != nil {
		return err
	}
	writeCallCSV(c, result, "routex-platform-calls.csv")
	return nil
}
