package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"gorm.io/gorm"
)

// Called by the shared PostgreSQL/MySQL harness with a fresh migrated database.
func testPriceSpreadsheetLifecycle(t *testing.T, db *gorm.DB) {
	router := identityRouter(t, db)
	response := identityRequest(router, "POST", "/api/v1/setup", `{"email":"workbook@example.com","password":"spreadsheet-password","name":"Workbook admin"}`, nil, "")
	expectStatus(t, response, 201)
	admin, cookie := readIdentity(t, response)
	for _, row := range []any{&entity.Provider{ID: "prv_sheet", Name: "Workbook"}, &entity.ProviderConnection{ID: "con_sheet", ProviderID: "prv_sheet", Name: "Text", BaseURL: "https://example.com", Protocol: entity.ProtocolOpenAIChat}, &entity.ProviderModel{ID: "pmo_one", ConnectionID: "con_sheet", UpstreamName: "Literal"}} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc, err := service.New(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	request := func(path string, body any) *httptest.ResponseRecorder {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		return identityRequest(router, "POST", "/api/v1/admin/prices/import/"+path, string(raw), cookie, admin.CSRFToken)
	}
	document := func(file string) map[string]any {
		raw, err := os.ReadFile("../service/testdata/prices/" + file)
		if err != nil {
			t.Fatal(err)
		}
		return map[string]any{"filename": file, "content_base64": base64.StdEncoding.EncodeToString(raw)}
	}
	var audits int64
	for _, test := range []struct{ file, source string }{{"literal-biff8.xls", "xls"}, {"literal.xlsx", "xlsx"}} {
		body := document(test.file)
		p := decodeCatalogResponse[service.PriceImportPreview](t, request("preview", body), 200)
		if !p.Valid || p.Sheet != "Prices" || len(p.Changes) != 1 || p.Changes[0].After.Amount != "123456789012345678.123456789012345678" {
			t.Fatalf("invalid preview: %+v", p)
		}
		body["etag"], body["preview_digest"] = p.ETag, p.Digest
		result := decodeCatalogResponse[service.PriceImportCommit](t, request("commit", body), 200)
		if result.Catalogue == nil || len(result.Catalogue.Items) != 1 || result.Catalogue.Items[0].UpdateSource != test.source || result.Catalogue.Items[0].Rates[0].Amount != "123456789012345678.123456789012345678" {
			t.Fatalf("lost exact/provenance value: %+v", result)
		}
		expectStatus(t, request("commit", body), 409)
		var count int64
		if err := db.Model(&entity.AuditEvent{}).Where("action = ?", "prices.update").Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		audits++
		if count != audits {
			t.Fatalf("replay duplicated audit: %d", count)
		}
	}
	page, err := svc.ListPrices(context.Background(), admin.User.ID, service.PriceFilter{})
	if err != nil {
		t.Fatal(err)
	}
	body := document("formula-biff8.xls")
	p := decodeCatalogResponse[service.PriceImportPreview](t, request("preview", body), 200)
	found := false
	for _, problem := range p.Errors {
		if problem.Code == "formula_cell" && problem.Sheet == "Prices" && problem.Cell == "F2" && problem.Row == 2 {
			found = true
		}
	}
	if p.Valid || !found {
		t.Fatalf("formula cached value accepted: %+v", p)
	}
	body["etag"], body["preview_digest"] = page.ETag, strings.Repeat("0", 64)
	expectStatus(t, request("commit", body), 422)
	current, err := svc.ListPrices(context.Background(), admin.User.ID, service.PriceFilter{})
	if err != nil || current.ETag != page.ETag {
		t.Fatal("invalid workbook consumed ETag")
	}
	body = document("literal.xlsx")
	body["csv"] = "header"
	expectStatus(t, request("preview", body), 400)
}
