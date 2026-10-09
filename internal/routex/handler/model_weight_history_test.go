package handler

import (
	"fmt"
	"github.com/fox-gonic/fox"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestModelWeightHistoryRegisteredDenialsArePrivate(t *testing.T) {
	router := fox.New()
	New(nil).RegisterRoutes(router)
	for _, tc := range []struct{ method, path string }{
		{"GET", "weight-versions"}, {"GET", "weight-versions/mwv_01m36yee4gkbns18pfcqqc75a3"},
		{"GET", "weights/rollback-review?version_id=mwv_01m36yee4gkbns18pfcqqc75a3"},
		{"POST", "weights/rollback"}, {"GET", "weights/rollback-commands/e26c3a0b-cfee-4391-bafd-7858fbf997ee"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/api/v1/admin/models/mdl_recorded/"+tc.path, strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")
			out := httptest.NewRecorder()
			router.ServeHTTP(out, req)
			if out.Code != 401 || out.Header().Get("Cache-Control") != "private, no-store" || out.Header().Get("ETag") != "" {
				t.Fatal("denied route leaked private response", out.Code)
			}
		})
	}
}
func TestModelWeightHistoryHandlerStrictBoundary(t *testing.T) {
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	ctrl := New(nil)
	router.GET("/models/:model_id/weight-versions", ctrl.ListModelWeightVersions)
	router.GET("/models/:model_id/weight-versions/:version_id", ctrl.GetModelWeightVersion)
	router.GET("/models/:model_id/weights/rollback-review", ctrl.ReviewModelWeightRollback)
	router.GET("/models/:model_id/weights/rollback-commands/:request_id", ctrl.GetModelWeightRollbackReceipt)
	router.POST("/models/:model_id/weights/rollback", ctrl.RollbackModelWeights)
	valid := `{"version_id":"mwv_01m36yee4gkbns18pfcqqc75a3","request_id":"e26c3a0b-cfee-4391-bafd-7858fbf997ee","reason":"Restore reviewed set"}`
	proof := `"` + strings.Repeat("a", 64) + `"`
	for index, tc := range []struct{ method, path, body, etag string }{
		{"GET", "/models/mdl_recorded/weight-versions?limit=0", "", ""},
		{"GET", "/models/mdl_recorded/weight-versions?limit=101", "", ""},
		{"GET", "/models/mdl_recorded/weight-versions?limit=01", "", ""},
		{"GET", "/models/mdl_recorded/weight-versions?limit=1&limit=1", "", ""},
		{"GET", "/models/mdl_recorded/weight-versions?cursor=", "", ""},
		{"GET", "/models/mdl_recorded/weight-versions?cursor=" + strings.Repeat("x", 201), "", ""},
		{"GET", "/models/mdl_recorded/weight-versions?extra=1", "", ""},
		{"GET", "/models/mdl_recorded/weight-versions?limit=%xx", "", ""},
		{"GET", "/models/mdl_recorded%20/weight-versions", "", ""},
		{"GET", "/models/prv_recorded/weight-versions", "", ""},
		{"GET", "/models/mdl_recorded/weight-versions/mwv_recorded?extra=1", "", ""},
		{"GET", "/models/mdl_recorded/weights/rollback-review", "", ""},
		{"GET", "/models/mdl_recorded/weights/rollback-review?version_id=x&limit=1", "", ""},
		{"GET", "/models/mdl_recorded/weights/rollback-commands/request?extra=1", "", ""},
		{"POST", "/models/mdl_recorded/weights/rollback", valid, ""},
		{"POST", "/models/mdl_recorded/weights/rollback", valid, "W/" + proof},
		{"POST", "/models/mdl_recorded/weights/rollback", valid, strings.Repeat("a", 64)},
		{"POST", "/models/mdl_recorded/weights/rollback", valid, `"` + strings.Repeat("A", 64) + `"`},
		{"POST", "/models/mdl_recorded/weights/rollback?extra=1", valid, proof},
		{"POST", "/models/mdl_recorded/weights/rollback", strings.Replace(valid, `"reason":"Restore reviewed set"`, `"reason":null`, 1), proof},
		{"POST", "/models/mdl_recorded/weights/rollback", strings.Replace(valid, `"reason":"Restore reviewed set"`, `"reason":"x","reason":"y"`, 1), proof},
		{"POST", "/models/mdl_recorded/weights/rollback", strings.Replace(valid, `{"version_id"`, `{"weights":[],"version_id"`, 1), proof},
		{"POST", "/models/mdl_recorded/weights/rollback", strings.Repeat(" ", 32769), proof},
	} {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("If-Match", tc.etag)
			out := httptest.NewRecorder()
			router.ServeHTTP(out, req)
			if out.Code != 400 || out.Header().Get("Cache-Control") != "private, no-store" || out.Header().Get("ETag") != "" {
				t.Fatal("ambiguous request crossed strict boundary", out.Code)
			}
		})
	}
}
