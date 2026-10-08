package handler

import (
	"encoding/json"
	"github.com/fox-gonic/fox"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReviewedModelBindingStrictAdditiveWire(t *testing.T) {
	for _, raw := range []string{`{"provider_model_id":"pmd_one"}`, `{"provider_model_id":"pmd_one","legacy":true}`, `{"provider_model_id":"pmd_one","protocol":"openai_chat","review_etag":"` + strings.Repeat("a", 64) + `"}`} {
		var request CreateModelBindingRequest
		request.ModelID = "mdl_target"
		if json.Unmarshal([]byte(raw), &request) != nil || request.ModelID != "mdl_target" {
			t.Fatal("compatible wire/URI identity changed")
		}
	}
	for _, raw := range []string{`null`, `[]`, `{"provider_model_id":"pmd_one","protocol":null,"review_etag":null}`, `{"provider_model_id":"pmd_one","protocol":"openai_chat"}`, `{"provider_model_id":"pmd_one","review_etag":"abc"}`, `{"provider_model_id":"pmd_one","protocol":"openai_chat","review_etag":"abc","secret":"no"}`, `{"provider_model_id":"pmd_one","protocol":"openai_chat","protocol":"openai_responses","review_etag":"abc"}`, `{"provider_model_id":"pmd_one","protocol":1,"review_etag":"abc"}`} {
		var request CreateModelBindingRequest
		if json.Unmarshal([]byte(raw), &request) == nil {
			t.Fatal("malformed reviewed wire accepted")
		}
	}
}

func TestModelRoutingCandidateHTTPRejectsScopeExpansion(t *testing.T) {
	ctrl := New(nil)
	router := fox.New()
	router.GET("/providers", ctrl.ListModelRoutingProviders)
	router.GET("/candidates", ctrl.ListModelRoutingCandidates)
	for _, path := range []string{"/providers?actor=usr_other", "/providers?provider_id=prv_one", "/candidates?protocol=x&protocol=y", "/providers?limit=0", "/providers?limit=51", "/providers?limit=01", "/providers?cursor=", "/providers?q=%ff", "/candidates?q=x&q=y", "/candidates?offset=1"} {
		r := httptest.NewRecorder()
		router.ServeHTTP(r, httptest.NewRequest("GET", path, nil))
		if r.Code != 400 || r.Header().Get("Cache-Control") != "private, no-store" || r.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal("expanded scope reached service", path, r.Code)
		}
	}
}
