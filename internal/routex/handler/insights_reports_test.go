package handler

import (
	"github.com/miclle/routex/internal/routex/entity"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const insightsReportValidBody = `{"request_id":"d52b7d9c-6e5d-44b2-b697-c55bd6a77974","kind":"tokens_by_model","from":"2026-10-01T00:00:00.123456789Z","to":"2026-10-02T00:00:00.123456790Z","timezone":"UTC"}`

func TestInsightsReportStrictBody(t *testing.T) {
	if _, err := decodeInsightsReportInput([]byte(insightsReportValidBody)); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{"", "null", "[]", insightsReportValidBody + "{}", strings.Replace(insightsReportValidBody, `"timezone":"UTC"`, `"timezone":null`, 1), strings.Replace(insightsReportValidBody, `"timezone":"UTC"`, `"timezone":1`, 1), strings.Replace(insightsReportValidBody, `"timezone":"UTC"`, `"timezone":"UTC","timezone":"UTC"`, 1), strings.Replace(insightsReportValidBody, `"timezone":"UTC"`, `"Timezone":"UTC"`, 1), strings.Replace(insightsReportValidBody, `"timezone":"UTC"`, `"question":"private"`, 1), insightsReportValidBody[:len(insightsReportValidBody)-1] + `,"statistics":{}}`, strings.Repeat(" ", 2049), string([]byte{0xff})} {
		if _, err := decodeInsightsReportInput([]byte(body)); err == nil {
			t.Fatal("malformed definition body admitted", body)
		}
	}
}
func TestInsightsReportStrictPagination(t *testing.T) {
	for _, query := range []string{"", "?limit=20", "?limit=100&cursor=opaque"} {
		limit, _, err := insightsReportListInput(httptest.NewRequest("GET", "/api"+query, nil))
		if err != nil || limit < 1 || limit > 100 {
			t.Fatal(query, err)
		}
	}
	for _, query := range []string{"?", "?limit=0", "?limit=101", "?limit=01", "?limit=+1", "?limit=20&limit=20", "?cursor=", "?owner_id=usr_other", "?sql=SELECT", "?cursor=" + strings.Repeat("a", 161), "?%zz"} {
		if _, _, err := insightsReportListInput(httptest.NewRequest("GET", "/api"+query, nil)); err == nil {
			t.Fatal("invalid query accepted", query)
		}
	}
}

func TestInsightsReportStoredTimestampRepresentation(t *testing.T) {
	at := time.Date(2026, 10, 12, 1, 2, 3, 123456000, time.UTC)
	original := entity.InsightsReport{ID: "retained", Kind: "tokens_by_model", From: "2026-10-01T00:00:00.123456789Z", CreatedAt: at, DeletedAt: &at}
	readback := original
	otherZone := at.In(time.FixedZone("driver", 8*60*60))
	readback.CreatedAt, readback.DeletedAt = otherZone, &otherZone
	if !insightsReportSameStoredDefinition(original, readback) {
		t.Fatal("equivalent database instants differ")
	}
	for _, change := range []func(*entity.InsightsReport){
		func(v *entity.InsightsReport) { v.CreatedAt = v.CreatedAt.Add(time.Nanosecond) },
		func(v *entity.InsightsReport) { value := v.DeletedAt.Add(time.Nanosecond); v.DeletedAt = &value },
		func(v *entity.InsightsReport) { v.DeletedAt = nil },
		func(v *entity.InsightsReport) { v.From = "2026-10-01T00:00:00.123456790Z" },
		func(v *entity.InsightsReport) { v.Kind = "provider_quality" },
	} {
		changed := readback
		change(&changed)
		if insightsReportSameStoredDefinition(original, changed) {
			t.Fatal("retained value change was ignored")
		}
	}
}
