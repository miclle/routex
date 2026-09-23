package handler

import (
	"net/http/httptest"
	"testing"
)

func TestUsageQueryContract(t *testing.T) {
	for _, query := range []string{"team_id=secret", "user_id=usr_other", "provider_model_id=pmd_other", "period=7d&period=24h", "period=", "compare=1", "stream=yes", "from=bad", "period=%zz", "period=7d;model_id=mdl_a"} {
		if _, err := usageFilter(httptest.NewRequest("GET", "/api/v1/usage?"+query, nil), false); err == nil {
			t.Fatalf("invalid query accepted: %s", query)
		}
	}
	filter, err := usageFilter(httptest.NewRequest("GET", "/api/v1/usage?from=2026-01-01T00%3A00%3A00Z&to=2026-01-02T00%3A00%3A00Z&stream=false&compare=true&model_id=mdl_a", nil), false)
	if err != nil || filter.From == nil || filter.To == nil || filter.Stream == nil || *filter.Stream || !filter.Compare || filter.ModelID != "mdl_a" {
		t.Fatalf("valid query failed: %+v %v", filter, err)
	}
	filter, err = usageFilter(httptest.NewRequest("GET", "/api/v1/admin/usage?project_id=prj_a&connection_id=con_a", nil), true)
	if err != nil || filter.ProjectID != "prj_a" || filter.ConnectionID != "con_a" {
		t.Fatal("admin filters lost")
	}
}
