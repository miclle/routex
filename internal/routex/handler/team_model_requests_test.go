package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fox-gonic/fox"
)

func TestTeamModelRequestBodiesRejectAlteredScopeBeforeSessionService(t *testing.T) {
	ctrl := New(nil)
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.POST("/requests", ctrl.CreateTeamModelRequest)
	router.POST("/own-decisions", ctrl.DecideOwnTeamModelRequest)
	router.POST("/decisions", ctrl.DecideTeamModelRequest)
	for _, route := range []string{"/requests", "/own-decisions", "/decisions"} {
		for _, body := range []string{`null`, `{}`, `[]`, `{"request_id":"x","team_id":"one","team_id":"two","model_id":"model","reason":"why"}`, `{"decision_id":"x","action":"approve","action":"reject"}`, `{"decision_id":"x","action":"approve","reason":null}`, `{"unexpected":"private"}`, `{} {}`, `{} trailing`} {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, route, strings.NewReader(body)))
			if response.Code != http.StatusBadRequest || strings.Contains(response.Body.String(), "private") {
				t.Fatal("altered Team intent reached identity/service or leaked payload", route, body, response.Code)
			}
		}
	}
}
func TestTeamModelRequestHistoryQueryHasIndependentOwnReviewScopes(t *testing.T) {
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	for _, reviewer := range []bool{false, true} {
		path := "/own"
		if reviewer {
			path = "/review"
		}
		router.GET(path, func(c *fox.Context) error {
			_, err := teamModelRequestFilter(c, reviewer)
			if err != nil {
				return err
			}
			c.Status(http.StatusNoContent)
			return nil
		})
	}
	for _, path := range []string{"/own", "/review"} {
		for _, query := range []string{"limit=0", "limit=51", "limit=01", "limit=1&limit=2", "user_id=private", "status=", "model_id=a&model_id=b", "cursor=bad%zz"} {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path+"?"+query, nil))
			if response.Code != http.StatusBadRequest {
				t.Fatal("ambiguous/expanded request history query accepted", path, query, response.Code)
			}
		}
	}
	for _, test := range []struct {
		path string
		want int
	}{{"/own?team_id=tem_left&model_id=mdl_old&status=approved&limit=50", http.StatusNoContent}, {"/review?team_id=tem_other", http.StatusBadRequest}, {"/review?model_id=mdl_old&status=pending&limit=1", http.StatusNoContent}} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
		if response.Code != test.want {
			t.Fatal("own/reviewer Team scope conflated", test.path, response.Code)
		}
	}
}
