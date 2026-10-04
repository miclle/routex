package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fox-gonic/fox"
)

func TestModelCreationBatchHTTPRejectsUnsupportedIntent(t *testing.T) {
	ctrl := New(nil)
	router := fox.New()
	router.POST("/preview", ctrl.PreviewModelCreationBatch)
	router.POST("/commit", ctrl.CreateModelBatch)
	for _, test := range []struct{ path, body string }{
		{"/preview", `{"items":[],"grant":true}`}, {"/preview", `{"items":null}`}, {"/preview", `{"items":[],"items":[]}`}, {"/preview", `{"items":[{"provider_model_id":"pmd_one","target":"new","name":"one","weight":100}]}`},
		{"/commit", `{"request_id":"not-a-uuid","reason":"x","items":[]}`}, {"/commit", `{"request_id":"e26c3a0b-cfee-4391-bafd-7858fbf997ee","reason":null,"items":[]}`},
		{"/preview", `{"items":[]}` + strings.Repeat(" ", 32*1024)},
		{"/preview?actor=usr_other", `{"items":[]}`},
	} {
		request := httptest.NewRequest("POST", test.path, strings.NewReader(test.body))
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != 400 || response.Header().Get("Cache-Control") != "private, no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal(test.path, response.Code, response.Header())
		}
	}
}
func TestModelCreationBatchPickerRejectsExpansion(t *testing.T) {
	ctrl := New(nil)
	router := fox.New()
	router.GET("/connections", ctrl.ListModelCreationConnections)
	for _, query := range []string{"q=", "q=x&q=y", "actor=usr_other", "limit=0", "limit=51", "limit=01", "offset=1", "cursor=", "q=%ff"} {
		request := httptest.NewRequest("GET", "/connections?"+query, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != 400 {
			t.Fatal(query, response.Code)
		}
	}
}

func TestModelCreationBatchCommitRequiresExactStrongReview(t *testing.T) {
	ctrl := New(nil)
	router := fox.New()
	router.POST("/commit", ctrl.CreateModelBatch)
	body := `{"request_id":"e26c3a0b-cfee-4391-bafd-7858fbf997ee","reason":"reviewed","items":[{"provider_model_id":"pmd_one","target":"new","name":"New"}]}`
	for _, headers := range [][]string{nil, {`W/"` + strings.Repeat("a", 64) + `"`}, {`"short"`}, {`"` + strings.Repeat("A", 64) + `"`}, {`"` + strings.Repeat("g", 64) + `"`}, {`"` + strings.Repeat("a", 64) + `"`, `"` + strings.Repeat("a", 64) + `"`}} {
		req := httptest.NewRequest("POST", "/commit", strings.NewReader(body))
		for _, header := range headers {
			req.Header.Add("If-Match", header)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != 400 || response.Header().Get("ETag") != "" {
			t.Fatal("invalid review reached mutation", headers, response.Code)
		}
	}
}
