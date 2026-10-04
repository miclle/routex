package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/service"
)

func TestTeamCallExportStrictQueryRejectsEveryForeignOrDuplicateSelector(t *testing.T) {
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.GET("/teams/:team_id/calls/export.csv", New(nil).ExportTeamCalls)
	for _, query := range []string{"user_id=", "key_id=", "project_id=", "team_id=", "cursor=", "limit=", "page=", "unknown=", "status=success&status=success", "model_id=mdl_one&model_id=mdl_two", "from=bad", "to=bad", "model_id=%zz", "status=success;model_id=mdl_one"} {
		t.Run(query, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/teams/tea_exact/calls/export.csv?"+query, nil))
			if response.Code != 400 || response.Header().Get("Content-Disposition") != "" || strings.HasPrefix(response.Header().Get("Content-Type"), "text/csv") {
				t.Fatal("invalid query became a download", response.Code, response.Header(), response.Body.String())
			}
		})
	}
}

func TestTeamCallExportFilterPreservesAppliedValues(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/teams/tea_exact/calls/export.csv?status=error&model_id=mdl_exact&from=2026-10-04T01%3A02%3A03.123456789%2B08%3A00&to=2026-10-05T00%3A00%3A00Z", nil)
	filter, err := teamCallExportFilter(request)
	if err != nil || filter.Status != "error" || filter.ModelID != "mdl_exact" || filter.From == nil || filter.From.Format("2006-01-02T15:04:05.999999999Z07:00") != "2026-10-04T01:02:03.123456789+08:00" || filter.To == nil || filter.UserID != "" || filter.KeyID != "" || filter.ProjectID != "" || filter.TeamID != "" || filter.Cursor != "" || filter.Limit != 0 {
		t.Fatal("applied Team filter changed or expanded", filter, err)
	}
}

func TestTeamCallExportCompletedBufferHasFixedPrivateMetadata(t *testing.T) {
	const data = "request_id,model_id\nreq_owned,mdl_exact\n"
	router := fox.New()
	router.GET("/export", func(c *fox.Context) {
		writeCallCSV(c, &service.CallCSVExport{CSV: []byte(data)}, "routex-team-calls.csv")
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/export", nil))
	if response.Code != 200 || response.Header().Get("Content-Type") != "text/csv; charset=utf-8" || response.Header().Get("Cache-Control") != "private, no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" || response.Header().Get("Content-Disposition") != `attachment; filename="routex-team-calls.csv"` || response.Body.String() != data {
		t.Fatal("Team download metadata changed", response.Header(), response.Body.String())
	}
}
