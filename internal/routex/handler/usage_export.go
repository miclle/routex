package handler

import (
	"net/http"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/service"
)

func writeUsageCSV(c *fox.Context, result *service.UsageCSVExport, filename string) {
	c.Header("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, "text/csv; charset=utf-8", result.CSV)
}
func (ctrl *Ctrl) ExportPersonalUsage(c *fox.Context) error {
	filter, err := usageFilter(c.Request, false)
	if err != nil {
		return err
	}
	result, err := ctrl.service.ExportPersonalUsage(c.Request.Context(), currentAuthentication(c).User.ID, filter)
	if err != nil {
		return err
	}
	writeUsageCSV(c, result, "routex-personal-usage.csv")
	return nil
}
func (ctrl *Ctrl) ExportProjectUsage(c *fox.Context, path ProjectPath) error {
	filter, err := usageFilter(c.Request, false)
	if err != nil {
		return err
	}
	result, err := ctrl.service.ExportProjectUsage(c.Request.Context(), currentAuthentication(c).User.ID, path.ProjectID, filter)
	if err != nil {
		return err
	}
	writeUsageCSV(c, result, "routex-project-usage.csv")
	return nil
}
func (ctrl *Ctrl) ExportTeamUsage(c *fox.Context, path TeamPath) error {
	filter, err := scopedUsageFilter(c.Request, false, true)
	if err != nil {
		return err
	}
	result, err := ctrl.service.ExportTeamUsage(c.Request.Context(), currentAuthentication(c).User.ID, path.TeamID, filter)
	if err != nil {
		return err
	}
	writeUsageCSV(c, result, "routex-team-usage.csv")
	return nil
}
func (ctrl *Ctrl) ExportAdminUsage(c *fox.Context) error {
	filter, err := usageFilter(c.Request, true)
	if err != nil {
		return err
	}
	result, err := ctrl.service.ExportAdminUsage(c.Request.Context(), currentAuthentication(c).User.ID, filter)
	if err != nil {
		return err
	}
	writeUsageCSV(c, result, "routex-platform-usage.csv")
	return nil
}
