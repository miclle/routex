package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fox-gonic/fox"
)

func TestModelWeightEditRejectsExpandedOrAmbiguousHTTP(t *testing.T) {
	ctrl := New(nil)
	router := fox.New()
	router.GET("/models/:model_id/weights/review", ctrl.ReviewModelWeightEdit)
	router.PUT("/models/:model_id/weights/reviewed", ctrl.SetReviewedModelWeights)
	good := `{"weights":[{"binding_id":"bnd_exact","weight":100}]}`
	proof := `"` + strings.Repeat("a", 64) + `"`
	for _, tc := range []struct{ method, path, body, etag string }{
		{"GET", "/models/mdl_exact/weights/review?actor=usr_other", "", ""},
		{"GET", "/models/mdl_exact/weights/review?limit=1", "", ""},
		{"PUT", "/models/mdl_exact/weights/reviewed", good, ""},
		{"PUT", "/models/mdl_exact/weights/reviewed", good, "W/" + proof},
		{"PUT", "/models/mdl_exact/weights/reviewed", strings.Replace(good, `100`, `null`, 1), proof},
		{"PUT", "/models/mdl_exact/weights/reviewed", strings.Replace(good, `"weight":100`, `"weight":100,"weight":0`, 1), proof},
		{"PUT", "/models/mdl_exact/weights/reviewed", `{"weights":[],"actor":"usr_other"}`, proof},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		if tc.etag != "" {
			req.Header.Set("If-Match", tc.etag)
		}
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		if out.Code != 400 || out.Header().Get("Cache-Control") != "private, no-store" || out.Header().Get("ETag") != "" {
			t.Fatal("invalid weight request reached authority/service", tc.path, out.Code)
		}
	}
}
