package handler

import (
	"net/http/httptest"
	"testing"
)

func TestInsightsQuotaStrictQuery(t *testing.T) {
	for _, check := range []struct {
		query string
		keys  bool
		valid bool
	}{
		{"dimension=tokens", false, true}, {"dimension=money&limit=50", false, true}, {"dimension=tokens&scope_kind=member&scope_id=usr_exact&limit=1", true, true},
		{"", false, false}, {"dimension=token", false, false}, {"dimension=tokens&dimension=money", false, false}, {"dimension=tokens&scope_kind=member", false, false}, {"dimension=tokens&scope_kind=member", true, false}, {"dimension=tokens&scope_kind=user&scope_id=usr_exact", true, false},
		{"dimension=tokens&from=2026-01-01", false, false}, {"dimension=tokens&timezone=UTC", false, false}, {"dimension=tokens&cursor=", false, false}, {"dimension=tokens&limit=0", false, false}, {"dimension=tokens&limit=51", false, false}, {"dimension=tokens&limit=01", false, false}, {"dimension=tokens&limit=%2B1", false, false}, {"dimension=tokens&bad=%ZZ", false, false},
	} {
		_, err := insightsQuotaInput(httptest.NewRequest("GET", "/api/v1/admin/insights/quota/teams?"+check.query, nil), check.keys)
		if (err == nil) != check.valid {
			t.Fatal(check, err)
		}
	}
}
