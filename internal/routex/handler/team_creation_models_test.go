package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

func TestTeamCreationModelsRegisteredPrivateUnauthenticated(t *testing.T) {
	router := fox.New()
	New(nil).RegisterRoutes(router)
	for _, path := range []string{"/api/v1/admin/teams/creation-model-candidates", "/api/v1/admin/teams/creation-model-review"} {
		method := http.MethodGet
		if strings.HasSuffix(path, "review") {
			method = http.MethodPost
		}
		req := httptest.NewRequest(method, path, strings.NewReader(`{"model_ids":["mdl_one"]}`))
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		if res.Code != 401 || res.Header().Get("Cache-Control") != "private, no-store" || res.Header().Get("ETag") != "" {
			t.Fatal("private review route admission", method, res.Code, res.Header())
		}
	}
}

func TestTeamCreationModelsBoundedTeamOnlyBodyReader(t *testing.T) {
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.POST("/team", jsonTeamCreationRequest, func(c *fox.Context) error {
		_, err := io.ReadAll(c.Request.Body)
		if err != nil {
			return apperrors.ErrBadRequest
		}
		c.Status(204)
		return nil
	})
	router.POST("/ordinary", jsonManagementRequest, func(c *fox.Context) error {
		_, err := io.ReadAll(c.Request.Body)
		if err != nil {
			return apperrors.ErrBadRequest
		}
		c.Status(204)
		return nil
	})
	for _, tc := range []struct {
		path, tag    string
		size, status int
	}{{"/team", "", 64 << 10, 204}, {"/team", "", (64 << 10) + 1, 400}, {"/team", `"review"`, 128 << 10, 204}, {"/team", `W/"malformed"`, (128 << 10) + 1, 400}, {"/ordinary", `"review"`, (64 << 10) + 1, 400}} {
		req := httptest.NewRequest("POST", tc.path, strings.NewReader(strings.Repeat("x", tc.size)))
		req.Header.Set("Content-Type", "application/json")
		if tc.tag != "" {
			req.Header.Set("If-Match", tc.tag)
		}
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		if res.Code != tc.status {
			t.Fatal("bounded Team reader changed", tc.path, tc.size, res.Code)
		}
	}
}

func TestTeamCreationModelsRejectMalformedRawQueryBeforeService(t *testing.T) {
	ctrl := New(nil)
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.GET("/candidates", memberMetadataResponseHeaders, ctrl.ListTeamCreationModelCandidates)
	for _, raw := range []string{"q=bad%zz", "q=a;private=1", "q=a&q=b", "limit=1&limit=2", "unknown=private", "limit=51"} {
		out := httptest.NewRecorder()
		router.ServeHTTP(out, httptest.NewRequest("GET", "/candidates?"+raw, nil))
		if out.Code != 400 || out.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("malformed candidate query reached service", raw, out.Code)
		}
	}
}

func TestTeamCreationModelsLargeLegacyReviewHeaderNeverOptsIntoWrite(t *testing.T) {
	ctrl := New(nil)
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.POST("/team", jsonTeamCreationRequest, ctrl.CreateTeam)
	body := `{"name":"Legacy","owner_ids":["usr_owner"]}` + strings.Repeat(" ", 65536)
	for _, tag := range []string{`"` + strings.Repeat("a", 64) + `"`, `W/"weak"`, `malformed`} {
		req := httptest.NewRequest("POST", "/team", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("If-Match", tag)
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		if out.Code != 400 {
			t.Fatal("large legacy header reached service", tag, out.Code)
		}
	}
}
