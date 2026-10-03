package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fox-gonic/fox"
)

func TestPersonalModelHeaderRequiresSingleStrongReviewedHash(t *testing.T) {
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.POST("/review", func(c *fox.Context) error {
		value, err := personalModelHeader(c)
		if err != nil {
			return err
		}
		if value != strings.Repeat("a", 64) {
			t.Fatal("reviewed hash changed", value)
		}
		c.Status(http.StatusNoContent)
		return nil
	})
	for _, values := range [][]string{nil, {strings.Repeat("a", 64)}, {`W/"` + strings.Repeat("a", 64) + `"`}, {`"` + strings.Repeat("A", 64) + `"`}, {`"` + strings.Repeat("a", 63) + `"`}, {`"` + strings.Repeat("a", 64) + `","` + strings.Repeat("a", 64) + `"`}, {`"` + strings.Repeat("a", 64) + `"`, `"` + strings.Repeat("a", 64) + `"`}, {`"` + strings.Repeat("a", 64) + `"`}} {
		request := httptest.NewRequest(http.MethodPost, "/review", nil)
		for _, value := range values {
			request.Header.Add("If-Match", value)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		want := http.StatusBadRequest
		if len(values) == 1 && values[0] == `"`+strings.Repeat("a", 64)+`"` {
			want = http.StatusNoContent
		}
		if response.Code != want {
			t.Fatal("incorrect reviewed header boundary", values, response.Code)
		}
	}
}

func TestPersonalModelListQueryRejectsUnsupportedOrAmbiguousIntent(t *testing.T) {
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.GET("/requests", func(c *fox.Context) error {
		_, err := personalModelRequestFilter(c)
		if err != nil {
			return err
		}
		c.Status(http.StatusNoContent)
		return nil
	})
	for _, query := range []string{"limit=0", "limit=51", "limit=-1", "limit=01", "limit=%2B1", "limit=1&limit=2", "limit=", "user_id=private", "team_id=private", "status=", "status=pending&status=approved", "cursor=bad%zz"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/requests?"+query, nil))
		if response.Code != http.StatusBadRequest {
			t.Fatal("ambiguous list query accepted", query, response.Code)
		}
	}
	for _, query := range []string{"", "limit=50", "status=pending&limit=1"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/requests?"+query, nil))
		if response.Code != http.StatusNoContent {
			t.Fatal("declared list query rejected", query, response.Code)
		}
	}
}

func TestPersonalModelBodiesRejectDiscardedIntentBeforeServiceAccess(t *testing.T) {
	ctrl := New(nil)
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.POST("/requests", ctrl.CreatePersonalModelRequest)
	router.POST("/decisions", ctrl.DecidePersonalModelRequest)
	router.POST("/member-decisions", ctrl.DecideMemberModelRequest)
	for _, route := range []string{"/requests", "/decisions", "/member-decisions"} {
		for _, body := range []string{`null`, `[]`, `{}`, `{"unexpected":"private"}`, `{"request_id":"one","request_id":"two","model_id":"model","reason":"why"}`, `{"decision_id":"one","action":"approve","action":"reject"}`, `{"decision_id":"one","action":"approve","reason":null}`, `{} {}`, `{} trailing`} {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, route, strings.NewReader(body)))
			if response.Code != http.StatusBadRequest || strings.Contains(response.Body.String(), "private") {
				t.Fatal("discarded body reached session/service or leaked input", route, body, response.Code)
			}
		}
	}
}
