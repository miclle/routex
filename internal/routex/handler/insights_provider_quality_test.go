package handler

import (
	"net/http/httptest"
	"testing"
)

func TestInsightsProviderQualityStrictQuery(t *testing.T) {
	const valid = "from=2026-10-01T00:00:00Z&to=2026-10-02T00:00:00Z&timezone=UTC"
	if _, err := insightsProviderQualityInput(httptest.NewRequest("GET", "/api?"+valid, nil)); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{valid + "&from=x", valid + "&provider_id=prv_other", valid + "&sql=SELECT", valid + "&period=30d", "from=x&to=y", valid + "&timezone=", "%zz"} {
		if _, err := insightsProviderQualityInput(httptest.NewRequest("GET", "/api?"+query, nil)); err == nil {
			t.Fatal("unexpected query accepted", query)
		}
	}
}
