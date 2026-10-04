package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/service"
)

func TestUsageExportInvalidQueriesNeverSetDownloadHeaders(t *testing.T) {
	ctrl := New(nil)
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.GET("/usage/export.csv", ctrl.ExportPersonalUsage)
	router.GET("/projects/:project_id/usage/export.csv", ctrl.ExportProjectUsage)
	router.GET("/teams/:team_id/usage/export.csv", ctrl.ExportTeamUsage)
	router.GET("/admin/usage/export.csv", ctrl.ExportAdminUsage)
	for _, path := range []string{"/usage/export.csv", "/projects/prj_exact/usage/export.csv", "/teams/tea_exact/usage/export.csv", "/admin/usage/export.csv"} {
		queries := []string{"cursor=other", "limit=1", "unknown=x", "period=", "period=7d&period=7d", "compare=1", "stream=yes", "from=bad", "to=bad", "period=%zz", "model_id=one&model_id=two", "period=7d;model_id=other"}
		if !strings.HasPrefix(path, "/admin/") {
			queries = append(queries, "team_id=tea_other", "user_id=usr_other", "project_id=prj_other", "provider_id=prv_private", "connection_id=con_private")
		}
		if strings.HasPrefix(path, "/teams/") {
			queries = append(queries, "key_id=key_private")
		}
		if strings.HasPrefix(path, "/admin/") {
			queries = append(queries, "team_id=tea_other&user_id=usr_other", "team_id=tea_other&project_id=prj_other", "team_id=tea_other&key_id=key_private")
		}
		for _, query := range queries {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path+"?"+query, nil))
			if response.Code != http.StatusBadRequest {
				t.Fatal("export accepted invalid query", path, query, response.Code, response.Body.String())
			}
			if response.Header().Get("Content-Disposition") != "" || strings.HasPrefix(response.Header().Get("Content-Type"), "text/csv") {
				t.Fatal("failure became a download", response.Header())
			}
		}
	}
}
func TestUsageExportCompletedBufferResponseIsPrivateCSV(t *testing.T) {
	const data = "row_type,schema_version\nmetadata,routex_usage_v1\n"
	for _, scope := range []string{"personal", "project", "team", "platform"} {
		router := fox.New()
		router.GET("/export", func(c *fox.Context) {
			writeUsageCSV(c, &service.UsageCSVExport{CSV: []byte(data)}, "routex-"+scope+"-usage.csv")
		})
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/export", nil))
		if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "text/csv; charset=utf-8" || response.Header().Get("Cache-Control") != "private, no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" || response.Header().Get("Content-Disposition") != `attachment; filename="routex-`+scope+`-usage.csv"` || response.Body.String() != data {
			t.Fatal("download headers/body changed", scope, response.Header(), response.Body.String())
		}
	}
}
func TestUsageExportUsesUnmodifiedStrictReportFilter(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/usage/export.csv?period=7d&timezone=America%2FNew_York&granularity=day&compare=true&stream=false&model_id=mdl_exact&key_id=key_exact&protocol=openai-responses&status=error", nil)
	filter, err := usageFilter(request, false)
	if err != nil || filter.Period != "7d" || filter.Timezone != "America/New_York" || filter.Granularity != "day" || !filter.Compare || filter.Stream == nil || *filter.Stream || filter.ModelID != "mdl_exact" || filter.KeyID != "key_exact" || filter.Protocol != "openai-responses" || filter.Status != "error" {
		t.Fatal("report filters changed at export", filter, err)
	}
}
