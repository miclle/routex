package handler

import (
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"gorm.io/gorm"
)

// This is called by the existing real-driver catalogue lifecycle only. These
// empty Providers prove selection does not require a Connection or Credential.
func assertModelInlineAccessPickers(t *testing.T, db *gorm.DB, send func(string, string, any, string) *httptest.ResponseRecorder) {
	t.Helper()
	providers := []entity.Provider{{ID: "prv_inline_A", Name: "Literal %_! Provider"}, {ID: "prv_inline_B", Name: "Other Provider"}, {ID: "prv_other_inline_A", Name: "Other retained Provider"}}
	if err := db.Create(&providers).Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/admin/model-creation/providers"
	first := decodeCatalogResponse[service.ModelCreationProviderPage](t, send("GET", path+"?q=prv_inline_&limit=1", nil, ""), 200)
	if len(first.Items) != 1 || first.Items[0].ID != "prv_inline_A" || first.NextCursor == nil || *first.NextCursor != "prv_inline_A" {
		t.Fatal("empty Provider prefix page", first)
	}
	next := decodeCatalogResponse[service.ModelCreationProviderPage](t, send("GET", path+"?q=prv_inline_&limit=1&cursor="+*first.NextCursor, nil, ""), 200)
	if len(next.Items) != 1 || next.Items[0].ID != "prv_inline_B" || next.NextCursor != nil {
		t.Fatal("exact Provider cursor", next)
	}
	for _, q := range []string{"PRV_INLINE_", "prv_missing", "%27%20OR%201%3D1"} {
		page := decodeCatalogResponse[service.ModelCreationProviderPage](t, send("GET", path+"?q="+q, nil, ""), 200)
		if len(page.Items) != 0 {
			t.Fatal("case or syntax borrowed Provider", q, page)
		}
	}
	literal := decodeCatalogResponse[service.ModelCreationProviderPage](t, send("GET", path+"?q=%25_%21", nil, ""), 200)
	if !reflect.DeepEqual(literal.Items, []service.ModelCreationProvider{{ID: "prv_inline_A", Name: "Literal %_! Provider"}}) {
		t.Fatal("wildcard expanded", literal)
	}
	for _, suffix := range []string{"?limit=51", "?limit=01", "?cursor=con_other", "?cursor=prv_missing", "?q=x&q=y", "?unexpected=x"} {
		expectStatus(t, send("GET", path+suffix, nil, ""), 400)
	}
	// Ordinary literal-name search can legitimately return an earlier alias.
	// Exact identity rereads must still select the intended retained Provider.
	if err := db.Create(&entity.Provider{ID: "prv_aaa_alias", Name: "Earlier name contains prv_inline_A"}).Error; err != nil {
		t.Fatal(err)
	}
	alias := decodeCatalogResponse[service.ModelCreationProviderPage](t, send("GET", path+"?q=prv_inline_A&limit=1", nil, ""), 200)
	if len(alias.Items) != 1 || alias.Items[0].ID != "prv_aaa_alias" {
		t.Fatal("name collision premise absent", alias)
	}
	exact := decodeCatalogResponse[service.ModelCreationProviderPage](t, send("GET", path+"?exact_id=prv_inline_A", nil, ""), 200)
	if !reflect.DeepEqual(exact.Items, []service.ModelCreationProvider{{ID: "prv_inline_A", Name: "Literal %_! Provider"}}) || exact.NextCursor != nil {
		t.Fatal("name alias hid exact Provider", exact)
	}
	for _, id := range []string{"prv_inline_a", "prv_missing"} {
		page := decodeCatalogResponse[service.ModelCreationProviderPage](t, send("GET", path+"?exact_id="+id, nil, ""), 200)
		if len(page.Items) != 0 || page.NextCursor != nil {
			t.Fatal("case or missing ID borrowed a Provider", page)
		}
	}
	for _, suffix := range []string{"?exact_id=", "?exact_id=con_other", "?exact_id=PRV_inline_A", "?exact_id=prv_inline_A%20", "?exact_id=prv_inline_A&exact_id=prv_inline_B", "?exact_id=prv_inline_A&q=prv_inline_A", "?exact_id=prv_inline_A&cursor=prv_inline_B", "?exact_id=prv_inline_A&limit=51"} {
		expectStatus(t, send("GET", path+suffix, nil, ""), 400)
	}
	for _, limit := range []string{"20", "50"} {
		decodeCatalogResponse[service.ModelCreationProviderPage](t, send("GET", path+"?limit="+limit, nil, ""), 200)
	}
	egress := entity.Egress{ID: "egr_inline_A", Name: "Inline egress", Kind: "socks5", Host: "example.invalid", Port: 1080, Enabled: true, ETag: "0", SecretGeneration: "fixture", AuthCiphertext: "", LastDiagnostic: ""}
	if err := db.Create(&egress).Error; err != nil {
		t.Fatal(err)
	}
	raw := send("GET", "/api/v1/admin/model-creation/egresses?q=egr_inline_&limit=1", nil, "")
	page := decodeCatalogResponse[service.ModelCreationEgressPage](t, raw, 200)
	if !reflect.DeepEqual(page.Items, []service.ModelCreationEgress{{ID: egress.ID, Name: egress.Name, Enabled: true}}) || page.NextCursor != nil || strings.Contains(raw.Body.String(), egress.Host) || strings.Contains(raw.Body.String(), "auth") || strings.Contains(raw.Body.String(), "diagnostic") {
		t.Fatal("egress picker exposed transport details", raw.Body.String())
	}
	aliasEgress := egress
	aliasEgress.ID = "egr_aaa_alias"
	aliasEgress.Name = "Earlier name contains egr_inline_A"
	if err := db.Create(&aliasEgress).Error; err != nil {
		t.Fatal(err)
	}
	egPath := "/api/v1/admin/model-creation/egresses"
	egAlias := decodeCatalogResponse[service.ModelCreationEgressPage](t, send("GET", egPath+"?q=egr_inline_A&limit=1", nil, ""), 200)
	if len(egAlias.Items) != 1 || egAlias.Items[0].ID != aliasEgress.ID {
		t.Fatal("egress collision premise absent", egAlias)
	}
	egExact := decodeCatalogResponse[service.ModelCreationEgressPage](t, send("GET", egPath+"?exact_id="+egress.ID, nil, ""), 200)
	if !reflect.DeepEqual(egExact.Items, []service.ModelCreationEgress{{ID: egress.ID, Name: egress.Name, Enabled: true}}) || egExact.NextCursor != nil {
		t.Fatal("name alias hid exact egress", egExact)
	}
	caseEgress := decodeCatalogResponse[service.ModelCreationEgressPage](t, send("GET", egPath+"?exact_id=egr_inline_a", nil, ""), 200)
	if len(caseEgress.Items) != 0 {
		t.Fatal("egress case alias accepted", caseEgress)
	}
	for _, suffix := range []string{"?exact_id=prv_inline_A", "?exact_id=EGR_inline_A", "?exact_id=egr_inline_A&q=egress", "?exact_id=egr_inline_A&cursor=egr_aaa_alias"} {
		expectStatus(t, send("GET", egPath+suffix, nil, ""), 400)
	}
	if raw.Header().Get("Cache-Control") != "private, no-store" || raw.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("private picker headers")
	}
}

func TestModelInlineAccessPickerStrictHTTPFilters(t *testing.T) {
	router := fox.New()
	router.GET("/picker", func(c *fox.Context) (service.ModelAccessPickerFilter, error) { return modelAccessPickerFilter(c) })
	for _, query := range []string{"", "?limit=20", "?limit=50", "?q=Literal", "?cursor=prv_cursor", "?exact_id=prv_one"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest("GET", "/picker"+query, nil))
		if rec.Code != 200 {
			t.Fatal("valid picker filter", query, rec.Code)
		}
	}
	for _, query := range []string{"?exact_id=", "?exact_id=x&exact_id=y", "?exact_id=prv_one&q=One", "?exact_id=prv_one&cursor=prv_two", "?q=", "?limit=0", "?limit=51", "?limit=01", "?unknown=x"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest("GET", "/picker"+query, nil))
		if rec.Code != 400 {
			t.Fatal("unsafe picker filter", query, rec.Code)
		}
	}
}
