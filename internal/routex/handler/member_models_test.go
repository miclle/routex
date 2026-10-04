package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fox-gonic/fox"
)

func TestMemberModelsHandlersRejectExpansionAndAmbiguousReview(t *testing.T) {
	ctrl := New(nil)
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.GET("/members/:user_id/models", ctrl.GetMemberModels)
	router.PUT("/members/:user_id/models", ctrl.SetMemberModels)
	etag := `"` + strings.Repeat("a", 64) + `"`
	cases := []struct{ method, path, body, etag string }{
		{"GET", "/members/usr_subject/models?limit=1", "", ""}, {"GET", "/members/usr_subject/models?user_id=foreign", "", ""}, {"GET", "/members/usr_subject/models?q=", "", ""},
		{"PUT", "/members/usr_subject/models?limit=1", `{"model_ids":[],"reason":"review"}`, etag},
		{"PUT", "/members/usr_subject/models", `{"model_ids":[],"reason":"review"}`, ""},
		{"PUT", "/members/usr_subject/models", `{"model_ids":[],"reason":"review"}`, "W/" + etag},
		{"PUT", "/members/usr_subject/models", `{"model_ids":[],"reason":"review"}`, strings.Repeat("a", 64)},
		{"PUT", "/members/usr_subject/models", `{"model_ids":[],"reason":"review"}`, etag + "," + etag},
		{"PUT", "/members/usr_subject/models", `{"model_ids":[],"reason":"review","user_id":"foreign"}`, etag},
		{"PUT", "/members/usr_subject/models", `{"model_ids":[],"reason":"review","re\u0061son":"other"}`, etag},
		{"PUT", "/members/usr_subject/models", `{"model_ids":null,"reason":"review"}`, etag},
		{"PUT", "/members/usr_subject/models", `{"model_ids":[],"reason":"\ud800"}`, etag},
		{"PUT", "/members/usr_subject/models", `{"model_ids":[],"reason":"review"} {}`, etag},
		{"PUT", "/members/usr_subject/models", strings.Repeat(" ", 65537), etag},
	}
	for _, test := range cases {
		request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
		request.Header.Set("If-Match", test.etag)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != 400 || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(test, response.Code, response.Header())
		}
	}
	request := httptest.NewRequest("PUT", "/members/usr_subject/models", strings.NewReader(`{"model_ids":[],"reason":"review"}`))
	request.Header.Add("If-Match", etag)
	request.Header.Add("If-Match", etag)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 400 {
		t.Fatal("duplicate ETag accepted", response.Code)
	}
}
