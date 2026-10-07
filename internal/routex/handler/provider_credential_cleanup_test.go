package handler

import (
	"context"
	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/service"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProviderCleanupRoutesRequireRealSessionAndPrivateResponses(t *testing.T) {
	svc, e := service.New(context.Background(), nil)
	if e == nil || svc != nil {
		t.Fatal("nil database unexpectedly usable")
	}
	// A controller with no Service can safely test the mandatory session barrier:
	// crossing it would panic and therefore expose an unprotected new route.
	router := fox.New()
	New(nil).RegisterRoutes(router)
	base := "/api/v1/admin/secrets/integrations/vlt_01aaaaaaaaaaaaaaaaaaaaaaaa/provider-orphans"
	for _, target := range []struct{ method, path string }{{"GET", base}, {"GET", base + "/11111111-1111-4111-8111-111111111111"}, {"GET", base + "/11111111-1111-4111-8111-111111111111/commands/22222222-2222-4222-8222-222222222222"}, {"POST", base + "/11111111-1111-4111-8111-111111111111/cleanup"}} {
		req := httptest.NewRequest(target.method, target.path, strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		if out.Code != 401 {
			t.Fatal("session barrier", target, out.Code)
		}
	}
}
