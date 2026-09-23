package handler

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/pricing"
	"gorm.io/gorm"
)

func testPriceImportLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	router := identityRouter(t, db)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"csv@example.com","password":"csv-import-password","name":"CSV admin"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, cookie := readIdentity(t, setup)
	svc, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	send := func(c *http.Cookie, csrf, method, path string, body any) *httptest.ResponseRecorder {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		return identityRequest(router, method, path, string(encoded), c, csrf)
	}
	request := func(method, path string, body any) *httptest.ResponseRecorder {
		return send(cookie, admin.CSRFToken, method, path, body)
	}
	for _, row := range []any{
		&entity.Provider{ID: "prv_csv", Name: "CSV Provider"},
		&entity.ProviderConnection{ID: "con_csv", ProviderID: "prv_csv", Name: "Text", BaseURL: "https://example.com", Protocol: entity.ProtocolOpenAIChat},
		&entity.ProviderConnection{ID: "con_csv_other", ProviderID: "prv_csv", Name: "Other", BaseURL: "https://example.com", Protocol: "native_audio"},
		&entity.ProviderModel{ID: "pmo_csv_a", ConnectionID: "con_csv", UpstreamName: "\t=SUM(1,2)"},
		&entity.ProviderModel{ID: "pmo_csv_b", ConnectionID: "con_csv", UpstreamName: "Second"},
		&entity.ProviderModel{ID: "pmo_csv_other", ConnectionID: "con_csv_other", UpstreamName: "Other"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	page, err := svc.ListPrices(ctx, admin.User.ID, service.PriceFilter{})
	if err != nil {
		t.Fatal(err)
	}
	threshold := int64(128000)
	page, err = svc.WritePrices(ctx, admin.User.ID, page.ETag, []service.PriceInput{{ProviderModelID: "pmo_csv_a", ContextThreshold: &threshold, Rates: []pricing.Rate{{Metric: pricing.Input, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "1", Enabled: true}, {Metric: pricing.Output, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "2", Enabled: true}, {Metric: pricing.Output, Tier: pricing.Long, Unit: pricing.Unit, Currency: "USD", Amount: "4", Enabled: true}}}})
	if err != nil {
		t.Fatal(err)
	}
	originalID := page.Items[0].ID
	csvText := func(rows ...string) string {
		return "provider_model_id,metric,tier,unit,currency,amount,enabled,context_threshold,upstream_name\n" + strings.Join(rows, "\n") + "\n"
	}
	valid := csvText("pmo_csv_a,INPUT_TOKEN,base,1M_TOKEN,USD,0.00,false,,Ignored identity label", "pmo_csv_b,INPUT_TOKEN,base,1M_TOKEN,USD,2.500,true,0,Also ignored")
	base := "/api/v1/admin/prices/import"
	preview := func(raw string) service.PriceImportPreview {
		return decodeCatalogResponse[service.PriceImportPreview](t, request("POST", base+"/preview", map[string]any{"csv": raw}), 200)
	}
	write := func(raw string, p service.PriceImportPreview) *httptest.ResponseRecorder {
		return request("POST", base+"/commit", map[string]any{"csv": raw, "etag": p.ETag, "preview_digest": p.Digest})
	}
	auditCount := func() int64 {
		var count int64
		if err := db.Model(&entity.AuditEvent{}).Where("action = ?", "prices.update").Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		return count
	}
	baselineAudits := auditCount()
	p := preview(valid)
	if !p.Valid || len(p.Changes) != 2 || p.Digest == "" || p.Changes[0].After.Amount != "0" || p.Changes[0].After.Enabled {
		t.Fatalf("invalid normalized preview: %+v", p)
	}
	if auditCount() != baselineAudits {
		t.Fatal("preview wrote an audit")
	}
	current, err := svc.ListPrices(ctx, admin.User.ID, service.PriceFilter{})
	if err != nil || current.ETag != p.ETag || len(current.Items) != 1 {
		t.Fatal("preview mutated current catalogue")
	}
	invalid := csvText("pmo_missing,INPUT_TOKEN,base,1M_TOKEN,USD,1,true,0,Missing", "pmo_csv_other,INPUT_TOKEN,base,1M_TOKEN,USD,1,true,0,Other", "pmo_csv_b,INPUT_TOKEN,base,1M_TOKEN,EUR,1,true,0,Missing FX")
	bad := preview(invalid)
	if bad.Valid || len(bad.Errors) != 3 || bad.Errors[0].Row != 2 || bad.Errors[1].Row != 3 || bad.Errors[2].Row != 4 {
		t.Fatalf("business errors were not collected: %+v", bad.Errors)
	}
	rejected := decodeCatalogResponse[service.PriceImportCommit](t, write(invalid, p), 422)
	if rejected.Catalogue != nil || len(rejected.Preview.Errors) != 3 || auditCount() != baselineAudits {
		t.Fatal("invalid import partially wrote")
	}
	thresholdError := preview(csvText("pmo_csv_a,INPUT_TOKEN,base,1M_TOKEN,USD,1,true,0,Ignore"))
	if thresholdError.Valid || len(thresholdError.Errors) != 1 || thresholdError.Errors[0].Code != "invalid_schedule" {
		t.Fatal("retained long-context rate invalidity was not previewed")
	}
	expectStatus(t, write(strings.Replace(valid, "2.500", "3", 1), p), 400)
	expectStatus(t, request("POST", base+"/preview", map[string]any{"csv": valid, "source": "repository"}), 400)
	expectStatus(t, request("POST", base+"/commit", map[string]any{"csv": valid, "etag": p.ETag, "preview_digest": p.Digest, "items": p.Items}), 400)
	expectStatus(t, send(cookie, "", "POST", base+"/preview", map[string]any{"csv": valid}), 403)
	expectStatus(t, send(cookie, "", "POST", base+"/commit", map[string]any{"csv": valid, "etag": p.ETag, "preview_digest": p.Digest}), 403)
	expectStatus(t, identityRequest(router, "GET", "/api/v1/admin/prices/export.csv", "", nil, ""), 401)
	committed := decodeCatalogResponse[service.PriceImportCommit](t, write(valid, p), 200)
	if committed.Catalogue == nil || committed.Catalogue.ETag == p.ETag || auditCount() != baselineAudits+1 {
		t.Fatal("atomic import did not advance catalogue once")
	}
	current, err = svc.ListPrices(ctx, admin.User.ID, service.PriceFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range current.Items {
		if record.UpdateSource != "csv" || record.FollowRepository {
			t.Fatal("CSV provenance was not server assigned")
		}
		if record.ProviderModelID == "pmo_csv_a" {
			if record.ID != originalID || len(record.Rates) != 3 || record.ContextThreshold != 128000 || record.UpstreamName != "\t=SUM(1,2)" {
				t.Fatal("omitted data or stable mapping changed")
			}
			for _, rate := range record.Rates {
				if rate.Metric == pricing.Input && (rate.Amount != "0" || rate.Enabled) {
					t.Fatal("explicit free disabled rate lost")
				}
			}
		}
	}
	expectStatus(t, write(valid, p), 409)
	if auditCount() != baselineAudits+1 {
		t.Fatal("replay duplicated successful audit")
	}
	var audit entity.AuditEvent
	if err := db.Where("action = ?", "prices.update").Order("created_at DESC, id DESC").First(&audit).Error; err != nil {
		t.Fatal(err)
	}
	if audit.DetailsJSON == nil || !strings.Contains(*audit.DetailsJSON, `"source":"csv"`) {
		t.Fatal("audit provenance missing")
	}
	// Two confirmed previews against one ETag cannot overwrite one another.
	next := csvText("pmo_csv_b,INPUT_TOKEN,base,1M_TOKEN,USD,5,true,0,Second")
	racePreview := preview(next)
	codes := make(chan int, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { codes <- write(next, racePreview).Code })
	}
	wg.Wait()
	close(codes)
	counts := map[int]int{}
	for code := range codes {
		counts[code]++
	}
	if counts[200] != 1 || counts[409] != 1 || auditCount() != baselineAudits+2 {
		t.Fatalf("concurrent import results: %v", counts)
	}
	export := identityRequest(router, "GET", "/api/v1/admin/prices/export.csv", "", cookie, "")
	expectStatus(t, export, 200)
	if !strings.HasPrefix(export.Header().Get("Content-Type"), "text/csv") || export.Header().Get("ETag") == "" || !strings.Contains(export.Header().Get("Content-Disposition"), "attachment") {
		t.Fatal("CSV download headers missing")
	}
	records, err := csv.NewReader(strings.NewReader(export.Body.String())).ReadAll()
	if err != nil || len(records) != 5 {
		t.Fatalf("export was truncated or invalid: %v %d", err, len(records))
	}
	for _, row := range records[1:] {
		if row[0] == "pmo_csv_a" && row[8] != "'\t=SUM(1,2)" {
			t.Fatalf("formula name not protected: %q", row[8])
		}
	}
	if roundtrip := preview(export.Body.String()); !roundtrip.Valid {
		t.Fatalf("export cannot be reimported: %+v", roundtrip.Errors)
	}
	expectStatus(t, request("POST", "/api/v1/admin/members", map[string]any{"name": "CSV reader denied", "email": "csv-member@example.com", "password": "csv-import-password"}), 201)
	member, memberCookie := readIdentity(t, identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"csv-member@example.com","password":"csv-import-password"}`, nil, ""))
	for _, path := range []string{base + "/preview", base + "/commit"} {
		expectStatus(t, send(memberCookie, member.CSRFToken, "POST", path, map[string]any{"csv": valid, "etag": p.ETag, "preview_digest": p.Digest}), 403)
	}
	expectStatus(t, identityRequest(router, "GET", "/api/v1/admin/prices/export.csv", "", memberCookie, ""), 403)
	// A complete export has a hard bound; exceeding it fails rather than silently
	// exporting the first page. No changes to invocation grants or model status.
	extra := []entity.ModelPrice{}
	models := []entity.ProviderModel{}
	for index := range 499 {
		id := fmt.Sprintf("pmo_csv_extra_%03d", index)
		models = append(models, entity.ProviderModel{ID: id, ConnectionID: "con_csv", UpstreamName: fmt.Sprintf("Extra %03d", index)})
		extra = append(extra, entity.ModelPrice{ID: fmt.Sprintf("prc_csv_extra_%03d", index), ProviderModelID: id, UpdateSource: "api"})
	}
	if err := db.Create(&models).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&extra).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, identityRequest(router, "GET", "/api/v1/admin/prices/export.csv", "", cookie, ""), 422)
}
