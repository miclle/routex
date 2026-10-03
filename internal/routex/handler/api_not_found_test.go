package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fox-gonic/fox"
	"github.com/fox-gonic/fox/httperrors"

	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/httperr"
)

func TestCombinedUnknownAPIRoutesReturnJSON404(t *testing.T) {
	router := fox.New()
	New(nil).RegisterRoutes(router)
	for _, path := range []string{"/api", "/api/v1/missing", "/v1", "/v1/missing", "/v1beta", "/v1beta/missing"} {
		for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions} {
			t.Run(method+path, func(t *testing.T) {
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest(method, path, nil))
				assertAPIErrorResponse(t, response, method, ErrorResponse{Code: http.StatusNotFound, Message: "not found"})
			})
		}
	}
	// A canonical-looking ID must not make this unsupported route a server error.
	path := "/api/v1/admin/providers/prv_" + strings.Repeat("0", 26)
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method+path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(method, path, nil))
			assertAPIErrorResponse(t, response, method, ErrorResponse{Code: http.StatusNotFound, Message: "not found"})
		})
	}
}

func TestAPIErrorRenderingPreservesSanitizedMappings(t *testing.T) {
	private := "private diagnostic must not appear"
	for _, test := range []struct {
		name string
		err  error
		want ErrorResponse
	}{
		{"fox not found", httperrors.ErrNotFound, ErrorResponse{404, "not found"}},
		{"wrapped fox not found", fmt.Errorf("%s: %w", private, httperrors.ErrNotFound), ErrorResponse{404, "not found"}},
		{"fox not found details", &httperrors.Error{HTTPCode: 404, Err: errors.New(private)}, ErrorResponse{404, "not found"}},
		{"wrapped fox bad request", fmt.Errorf("%s: %w", private, &httperrors.Error{HTTPCode: 400, Err: errors.New(private)}), ErrorResponse{400, "bad request"}},
		{"unexpected error", errors.New(private), ErrorResponse{500, "internal server error"}},
		{"fox internal error", &httperrors.Error{HTTPCode: 500, Err: errors.New(private)}, ErrorResponse{500, "internal server error"}},
		{"application not found", apperrors.ErrNotFound, ErrorResponse{404, "resource not found"}},
		{"wrapped application unauthorized", fmt.Errorf("%s: %w", private, apperrors.ErrUnauthorized), ErrorResponse{401, "unauthorized"}},
		{"application forbidden", apperrors.ErrForbidden, ErrorResponse{403, "forbidden"}},
		{"application conflict", apperrors.ErrAlreadyInitialized, ErrorResponse{409, "installation already initialized"}},
		{"wrapped status error", fmt.Errorf("%s: %w", private, httperr.NewNotFound("example not found")), ErrorResponse{404, "example not found"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := fox.New()
			New(nil).RegisterRoutes(router)
			router.GET("/renderer-error", func() error { return test.err })
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/renderer-error", nil))
			assertAPIErrorResponse(t, response, http.MethodGet, test.want)
			if strings.Contains(response.Body.String(), private) {
				t.Fatal("error renderer exposed private diagnostics")
			}
		})
	}
}

func assertAPIErrorResponse(t *testing.T, response *httptest.ResponseRecorder, method string, want ErrorResponse) {
	t.Helper()
	if response.Code != want.Code || !strings.HasPrefix(response.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("status=%d content_type=%q, want JSON %d", response.Code, response.Header().Get("Content-Type"), want.Code)
	}
	if method == http.MethodHead {
		return
	}
	var got ErrorResponse
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil || got != want {
		t.Fatalf("error response=%q, want %+v: %v", response.Body.String(), want, err)
	}
}
