package handler

import (
	"net/http/httptest"
	"testing"

	"github.com/fox-gonic/fox"
)

func TestProviderModelBindingsStrictRequestAndNoStore(t *testing.T) {
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.GET("/providers/:provider_id/model-bindings", func(c *fox.Context) error {
		err := providerModelBindingsRequest(c)
		if err == nil {
			c.Status(204)
		}
		return err
	})
	for _, target := range []string{"prv_target", "PRV_target", "mdl_other", "prv_target%20", "prv_abcdefghijklmnopqrstuvwxyz1234567890"} {
		for _, query := range []string{"", "?limit=10", "?cursor=x", "?provider_id=prv_other", "?q=x", "?q=bad%zz"} {
			r := httptest.NewRecorder()
			router.ServeHTTP(r, httptest.NewRequest("GET", "/providers/"+target+"/model-bindings"+query, nil))
			want := 400
			if target == "prv_target" && query == "" {
				want = 204
			}
			if r.Code != want || r.Header().Get("Cache-Control") != "no-store" {
				t.Fatal(target, query, r.Code, r.Header())
			}
		}
	}
	actual := fox.New()
	New(nil).RegisterRoutes(actual)
	r := httptest.NewRecorder()
	actual.ServeHTTP(r, httptest.NewRequest("GET", "/api/v1/admin/providers/prv_target/model-bindings", nil))
	if r.Code != 401 || r.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("private auth error cached", r.Code, r.Header())
	}
}
