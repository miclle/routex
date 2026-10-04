package handler

import (
	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/service"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRepositoryPriceHTTPRejectsDiscardedIntentBeforeService(t *testing.T) {
	ctrl := New(nil)
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.PUT("/config", ctrl.WriteRepositoryPriceSource)
	router.POST("/preview", ctrl.PreviewRepositoryPrices)
	router.POST("/apply", ctrl.ApplyRepositoryPrices)
	config := `{"request_id":"11111111-1111-4111-8111-111111111111","enabled":true,"mappings":[],"reason":"review"}`
	preview := `{"mode":"sync","provider_model_ids":["pmo_one"],"rate_ids":[]}`
	apply := `{"request_id":"11111111-1111-4111-8111-111111111111","preview_digest":"` + strings.Repeat("a", 64) + `","selection":` + preview + `,"reason":"review"}`
	cases := []struct{ method, path, body string }{
		{"PUT", "/config", strings.Replace(config, `"enabled":true`, `"enabled":null`, 1)},
		{"PUT", "/config", strings.Replace(config, `"enabled":true`, `"enabled":true,"enabled":false`, 1)},
		{"PUT", "/config", strings.Replace(config, `"enabled":true`, `"Enabled":true`, 1)},
		{"PUT", "/config", strings.Replace(config, `"mappings":[]`, `"mappings":null`, 1)},
		{"PUT", "/config", strings.Replace(config, `"mappings":[]`, `"mappings":[{"provider_model_id":"pmo_one","source_model_key":"provider/model","amount":"0"}]`, 1)},
		{"PUT", "/config", strings.Replace(config, `"mappings":[]`, `"mappings":[{"provider_model_id":"pmo_one","source_model_key":"provider/model"},{"provider_model_id":"pmo_one","source_model_key":"provider/other"}]`, 1)},
		{"PUT", "/config", strings.Replace(config, `"request_id":"11111111-1111-4111-8111-111111111111",`, "", 1)},
		{"PUT", "/config", strings.Replace(config, `"reason":"review"`, `"reason":"\ud800"`, 1)},
		{"PUT", "/config", strings.Replace(config, `"reason":"review"`, `"reason":"\u0000"`, 1)},
		{"PUT", "/config", strings.Replace(config, `"reason":"review"`, `"reason":" review"`, 1)},
		{"PUT", "/config", strings.Replace(config, `"reason":"review"`, `"reason":"`+strings.Repeat("x", 1001)+`"`, 1)},
		{"PUT", "/config", strings.Replace(config, "review", string([]byte{0xff}), 1)},
		{"POST", "/preview", strings.Replace(preview, `"rate_ids":[]`, `"rate_ids":["rat_one"]`, 1)},
		{"POST", "/preview", strings.Replace(preview, `"mode":"sync"`, `"mode":"restore"`, 1)},
		{"POST", "/preview", strings.Replace(preview, `["pmo_one"]`, `["pmo_one","pmo_one"]`, 1)},
		{"POST", "/preview", strings.Replace(preview, `"rate_ids":[]`, `"rate_ids":null`, 1)},
		{"POST", "/apply", strings.Replace(apply, `"preview_digest":"`+strings.Repeat("a", 64)+`"`, `"preview_digest":"private"`, 1)},
		{"POST", "/apply", strings.Replace(apply, `"request_id":"11111111-1111-4111-8111-111111111111"`, `"request_id":"PRIVATE"`, 1)},
		{"POST", "/apply", strings.Replace(apply, `"rate_ids":[]`, `"rate_ids":[],"amount":"0"`, 1)},
	}
	for _, route := range []struct{ method, path string }{{"PUT", "/config"}, {"POST", "/preview"}, {"POST", "/apply"}} {
		for _, body := range []string{"null", "[]", "{}", config + " {}", strings.Repeat(" ", 64<<10+1)} {
			cases = append(cases, struct{ method, path, body string }{route.method, route.path, body})
		}
	}
	for i, item := range cases {
		t.Run(item.path+"/"+http.StatusText(i+400), func(t *testing.T) {
			request := httptest.NewRequest(item.method, item.path, strings.NewReader(item.body))
			request.Header.Set("If-Match", `"`+strings.Repeat("a", 64)+`"`)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest || strings.Contains(response.Body.String(), "PRIVATE") {
				t.Fatal("invalid intent reached authority or leaked input", response.Code)
			}
		})
	}
}
func TestRepositoryPriceQueriesRemainExactAndPrivate(t *testing.T) {
	ctrl := New(nil)
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.GET("/config", ctrl.GetRepositoryPriceSource)
	router.GET("/candidates", ctrl.ListRepositoryPriceCandidates)
	router.GET("/receipts/:request_id", ctrl.GetRepositoryPriceReceipt)
	for _, path := range []string{"/config?", "/config?source=private", "/receipts/11111111-1111-4111-8111-111111111111?model=private", "/receipts/private", "/candidates?limit=0", "/candidates?limit=51", "/candidates?limit=01", "/candidates?limit=1&limit=2", "/candidates?q=", "/candidates?cursor=../private", "/candidates?cursor=" + strings.Repeat("x", 31), "/candidates?q=" + strings.Repeat("x", 201), "/candidates?q=%FF", "/candidates?q=%00", "/candidates?secret=private"} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			if response.Code != http.StatusBadRequest || response.Header().Get("Cache-Control") != "private, no-store" || strings.Contains(response.Body.String(), "private") {
				t.Fatal("query expanded read or leaked input", response.Code)
			}
		})
	}
}
func TestRepositoryPriceSelectorAndUnicodeBoundaries(t *testing.T) {
	valid := `{"mode":"restore","provider_model_ids":["pmo_one"],"rate_ids":["rat_one"]}`
	if selected, err := repositorySelection([]byte(valid)); err != nil || len(selected.RateIDs) != 1 {
		t.Fatal("exact restore selection rejected", err)
	}
	for _, raw := range []string{`{"mode":"sync","provider_model_ids":[],"rate_ids":[]}`, `{"mode":"sync","provider_model_ids":["../private"],"rate_ids":[]}`, strings.Replace(valid, `"rat_one"`, `"rat_one","rat_one"`, 1)} {
		if _, err := repositorySelection([]byte(raw)); err == nil {
			t.Fatal("ambiguous selector accepted")
		}
	}
	for _, raw := range []string{`"\ud800"`, `"\udc00"`, `"\ud800\u0000"`} {
		if repositoryUnicode([]byte(raw)) {
			t.Fatal("unpaired Unicode surrogate accepted")
		}
	}
	for _, raw := range []string{`"\ud83d\ude00"`, `"literal\\ud800"`} {
		if !repositoryUnicode([]byte(raw)) {
			t.Fatal("valid Unicode rejected")
		}
	}
	if !repositoryValidReason(strings.Repeat("😀", 1000)) {
		t.Fatal("reason bound used bytes instead of characters")
	}
}
func TestRepositoryPriceResultCreatedAndReplayStatus(t *testing.T) {
	for _, created := range []bool{false, true} {
		router := fox.New()
		router.POST("/result", func(c *fox.Context) error {
			return repositoryResult(c, &service.RepositoryPriceResult{Created: created, Committed: true})
		})
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/result", nil))
		want := http.StatusOK
		if created {
			want = http.StatusCreated
		}
		if response.Code != want || strings.Contains(response.Body.String(), `"Created"`) {
			t.Fatal("wrong durable operation status", response.Code)
		}
	}
}

func TestRepositoryPriceRegisteredRoutesRequireSession(t *testing.T) {
	router := fox.New()
	New(nil).RegisterRoutes(router)
	for _, route := range []struct{ method, path string }{{"GET", "/api/v1/admin/prices/repository"}, {"PUT", "/api/v1/admin/prices/repository"}, {"GET", "/api/v1/admin/prices/repository/candidates"}, {"POST", "/api/v1/admin/prices/repository/preview"}, {"POST", "/api/v1/admin/prices/repository/apply"}, {"GET", "/api/v1/admin/prices/repository/receipts/11111111-1111-4111-8111-111111111111"}} {
		t.Run(route.method+route.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(route.method, route.path, nil))
			if response.Code != http.StatusUnauthorized {
				t.Fatal("repository route was absent or allowed unauthenticated access", response.Code)
			}
		})
	}
}
