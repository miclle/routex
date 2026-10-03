package handler

import (
	"net/http/httptest"
	"testing"
)

func TestUsageQueryContract(t *testing.T) {
	for _, query := range []string{"team_id=secret", "user_id=usr_other", "provider_id=prv_other", "provider_model_id=pmd_other", "period=7d&period=24h", "period=", "compare=1", "stream=yes", "from=bad", "period=%zz", "period=7d;model_id=mdl_a"} {
		if _, err := usageFilter(httptest.NewRequest("GET", "/api/v1/usage?"+query, nil), false); err == nil {
			t.Fatalf("invalid query accepted: %s", query)
		}
	}
	filter, err := usageFilter(httptest.NewRequest("GET", "/api/v1/usage?from=2026-01-01T00%3A00%3A00Z&to=2026-01-02T00%3A00%3A00Z&stream=false&compare=true&model_id=mdl_a", nil), false)
	if err != nil || filter.From == nil || filter.To == nil || filter.Stream == nil || *filter.Stream || !filter.Compare || filter.ModelID != "mdl_a" {
		t.Fatalf("valid query failed: %+v %v", filter, err)
	}
	filter, err = usageFilter(httptest.NewRequest("GET", "/api/v1/admin/usage?project_id=prj_a&provider_id=prv_a&connection_id=con_a", nil), true)
	if err != nil || filter.ProjectID != "prj_a" || filter.ProviderID != "prv_a" || filter.ConnectionID != "con_a" {
		t.Fatal("admin filters lost")
	}
}

func TestTeamUsageQueryContract(t *testing.T) {
	for _, query := range []string{"key_id=key_private", "user_id=usr_other", "project_id=prj_other", "team_id=tea_other", "provider_id=prv_other", "provider_model_id=pmd_other", "connection_id=con_other", "model_id=mdl_a&model_id=mdl_b", "compare=1", "stream=", "unknown=x"} {
		if _, err := scopedUsageFilter(httptest.NewRequest("GET", "/api/v1/teams/tea_a/usage?"+query, nil), false, true); err == nil {
			t.Fatal("Team usage accepted invalid scoped query", query)
		}
	}
	filter, err := scopedUsageFilter(httptest.NewRequest("GET", "/api/v1/teams/tea_a/usage?period=7d&model_id=mdl_a&stream=false&compare=true", nil), false, true)
	if err != nil || filter.ModelID != "mdl_a" || filter.Stream == nil || *filter.Stream || !filter.Compare || filter.TeamID != "" || filter.KeyID != "" {
		t.Fatal("valid model-only Team usage lost its scope", filter, err)
	}
	filter, err = usageFilter(httptest.NewRequest("GET", "/api/v1/admin/usage?team_id=tea_historical&provider_id=prv_diagnostic", nil), true)
	if err != nil || filter.TeamID != "tea_historical" || filter.ProviderID != "prv_diagnostic" {
		t.Fatal("authorized historical Team diagnostics rejected", filter, err)
	}
	for _, query := range []string{"team_id=tea_a&user_id=usr_actor", "team_id=tea_a&project_id=prj_other", "team_id=tea_a&key_id=key_other", "team_id=tea_a&team_id=tea_b"} {
		if _, err := usageFilter(httptest.NewRequest("GET", "/api/v1/admin/usage?"+query, nil), true); err == nil {
			t.Fatal("ambiguous administrator Team attribution accepted", query)
		}
	}
}
