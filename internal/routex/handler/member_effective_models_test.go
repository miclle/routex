package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

func TestMemberEffectiveModelsRejectsSelectorsAndUnsafeTargetsBeforeAuthority(t *testing.T) {
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.GET("/members/:user_id/effective-models", New(nil).MemberEffectiveModels)
	for _, suffix := range []string{"?user_id=usr_other", "?team=tea_other", "?cursor=", "?limit=1", "?model_id=", "?x=1&x=2", "?bad=%zz"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/members/usr_target/effective-models"+suffix, nil))
		if response.Code != 400 || response.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal(suffix, response.Code, response.Header())
		}
	}
	for _, target := range []string{"usr_target%20", strings.Repeat("x", 31), "usr_%E7%9B%AE%E6%A0%87"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/members/"+target+"/effective-models", nil))
		if response.Code != 400 || response.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal(target, response.Code)
		}
	}
}

func TestMemberEffectiveModelsPreHandlerDenialsRetainNoStore(t *testing.T) {
	for _, status := range []int{401, 403} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			router := fox.New()
			router.RenderErrorFunc = renderAPIError
			group := router.Group("/api/v1")
			group.Use(authResponseHeaders)
			if status == 401 {
				group.GET("/admin/members/:user_id/effective-models", New(nil).requireSession, New(nil).MemberEffectiveModels)
			} else {
				group.GET("/admin/members/:user_id/effective-models", func(*fox.Context) error { return apperrors.ErrForbidden }, New(nil).MemberEffectiveModels)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/admin/members/usr_target/effective-models", nil))
			if response.Code != status || response.Header().Get("Cache-Control") != "no-store" || strings.Contains(response.Body.String(), `"items"`) || strings.Contains(response.Body.String(), `"sources"`) {
				t.Fatal(response.Code, response.Header(), response.Body.String())
			}
		})
	}
}
