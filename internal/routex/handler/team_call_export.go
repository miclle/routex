package handler

import (
	"net/http"
	"net/url"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

type TeamCallExportPath struct {
	TeamID string `uri:"team_id" json:"-"`
}

func teamCallExportFilter(request *http.Request) (service.CallFilter, error) {
	values, err := url.ParseQuery(request.URL.RawQuery)
	if err != nil {
		return service.CallFilter{}, apperrors.ErrBadRequest
	}
	for key, items := range values {
		if len(items) != 1 {
			return service.CallFilter{}, apperrors.ErrBadRequest
		}
		switch key {
		case "status", "model_id", "from", "to":
		default:
			return service.CallFilter{}, apperrors.ErrBadRequest
		}
	}
	return callExportFilter(ListCallsRequest{Status: values.Get("status"), ModelID: values.Get("model_id"), From: values.Get("from"), To: values.Get("to")}, false)
}

func (ctrl *Ctrl) ExportTeamCalls(c *fox.Context, path TeamCallExportPath) error {
	filter, err := teamCallExportFilter(c.Request)
	if err != nil {
		return err
	}
	result, err := ctrl.service.ExportTeamCalls(c.Request.Context(), currentAuthentication(c).User.ID, path.TeamID, filter)
	if err != nil {
		return err
	}
	writeCallCSV(c, result, "routex-team-calls.csv")
	return nil
}
