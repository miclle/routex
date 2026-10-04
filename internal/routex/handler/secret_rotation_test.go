package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fox-gonic/fox"
)

func TestSecretRotationBodyStrictIntent(t *testing.T) {
	const requestID = "10000000-0000-4000-8000-000000000001"
	valid := `{"request_id":"` + requestID + `","target_key_id":"next","reason":"reviewed"}`
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.POST("/start", func(c *fox.Context) error {
		fields, err := secretRotationBody(c, true)
		if err != nil {
			return err
		}
		c.JSON(http.StatusOK, fields)
		return nil
	})
	cases := []struct {
		name, body string
		valid      bool
	}{
		{"valid", valid, true},
		{"unicode pair", strings.Replace(valid, "reviewed", `\ud83d\ude00`, 1), true},
		{"literal escape", strings.Replace(valid, "reviewed", `\\ud800`, 1), true},
		{"1000 unicode", strings.Replace(valid, "reviewed", strings.Repeat("界", 1000), 1), true},
		{"too long", strings.Replace(valid, "reviewed", strings.Repeat("界", 1001), 1), false},
		{"duplicate", strings.Replace(valid, `"reason":"reviewed"`, `"reason":"reviewed","reason":"other"`, 1), false},
		{"case alias", strings.Replace(valid, `"reason"`, `"Reason"`, 1), false},
		{"unknown", strings.Replace(valid, `"reason":"reviewed"`, `"reason":"reviewed","key":"private"`, 1), false},
		{"null", strings.Replace(valid, `"reason":"reviewed"`, `"reason":null`, 1), false},
		{"number", strings.Replace(valid, `"reason":"reviewed"`, `"reason":42`, 1), false},
		{"empty", strings.Replace(valid, "reviewed", "", 1), false},
		{"trim", strings.Replace(valid, "reviewed", " reviewed", 1), false},
		{"control", strings.Replace(valid, "reviewed", `x\n`, 1), false},
		{"unpaired high", strings.Replace(valid, "reviewed", `\ud800`, 1), false},
		{"unpaired low", strings.Replace(valid, "reviewed", `\udc00`, 1), false},
		{"wrong pair", strings.Replace(valid, "reviewed", `\ud800\u0041`, 1), false},
		{"malformed utf8", strings.Replace(valid, "reviewed", string([]byte{0xff}), 1), false},
		{"wrong uuid", strings.Replace(valid, requestID, "10000000-0000-1000-8000-000000000001", 1), false},
		{"unsafe target", strings.Replace(valid, "next", " next", 1), false},
		{"trailing", valid + ` {}`, false},
		{"oversize", strings.Repeat(" ", 16<<10) + valid, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/start", strings.NewReader(test.body)))
			want := http.StatusBadRequest
			if test.valid {
				want = http.StatusOK
			}
			if response.Code != want {
				t.Fatalf("status = %d, want %d", response.Code, want)
			}
			if !test.valid && strings.Contains(response.Body.String(), "private") {
				t.Fatal("input leaked")
			}
		})
	}
}

func TestSecretRotationActionBodyAndQueries(t *testing.T) {
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.POST("/action", func(c *fox.Context) error {
		if err := secretRotationQuery(c); err != nil {
			return err
		}
		fields, err := secretRotationBody(c, false)
		if err != nil {
			return err
		}
		c.JSON(http.StatusOK, fields)
		return nil
	})
	input := map[string]string{"request_id": "10000000-0000-4000-8000-000000000001", "reason": "reviewed"}
	raw, _ := json.Marshal(input)
	for _, query := range []string{"", "?", "?cursor=", "?key=private"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/action"+query, strings.NewReader(string(raw))))
		want := http.StatusBadRequest
		if query == "" {
			want = http.StatusOK
		}
		if response.Code != want || response.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("query/cache boundary", query, response.Code)
		}
	}
	input["target_key_id"] = "next"
	raw, _ = json.Marshal(input)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/action", strings.NewReader(string(raw))))
	if response.Code != http.StatusBadRequest {
		t.Fatal("action accepted extra target")
	}
}

func TestSecretRotationHandlersRejectInvalidBoundaryBeforeService(t *testing.T) {
	ctrl := New(nil)
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.GET("/store", ctrl.GetSecretStore)
	router.GET("/jobs/:rotation_id", ctrl.GetSecretRotation)
	router.POST("/start", ctrl.StartSecretRotation)
	router.POST("/resume/:rotation_id", ctrl.ResumeSecretRotation)
	router.POST("/retire/:rotation_id", ctrl.RetireSecretRotation)
	router.POST("/rollback/:rotation_id", ctrl.RollbackSecretRotation)
	for _, path := range []string{"/store?expand=private", "/jobs/invalid", "/start?key=private", "/resume/invalid", "/retire/invalid", "/rollback/invalid"} {
		method := http.MethodPost
		if strings.HasPrefix(path, "/store") || strings.HasPrefix(path, "/jobs") {
			method = http.MethodGet
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(method, path, strings.NewReader(`{}`)))
		if response.Code != http.StatusBadRequest {
			t.Fatal("invalid input reached Service", path, response.Code)
		}
	}
}

func TestSecretRotationReviewedHeaderBeforeService(t *testing.T) {
	ctrl := New(nil)
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.POST("/start", ctrl.StartSecretRotation)
	router.POST("/action/:rotation_id", ctrl.RetireSecretRotation)
	for _, path := range []string{"/start", "/action/srt_01k00000000000000000000000"} {
		for _, header := range []string{"", strings.Repeat("a", 64), `W/"` + strings.Repeat("a", 64) + `"`, `"` + strings.Repeat("A", 64) + `"`} {
			body := `{"request_id":"10000000-0000-4000-8000-000000000001","reason":"reviewed"}`
			if path == "/start" {
				body = strings.Replace(body, `"reason"`, `"target_key_id":"next","reason"`, 1)
			}
			request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
			request.Header.Set("If-Match", header)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatal("invalid review reached Service", response.Code)
			}
		}
	}
}
