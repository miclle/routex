package handler

import (
	"errors"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/service"
)

func TestTeamNativeInputsRejectKeysAndPreserveOnlyStreamSelector(t *testing.T) {
	for _, test := range []struct {
		query, header string
		stream        bool
		status        int
	}{
		{"", "", false, 0}, {"alt=sse", "", true, 0}, {"alt=sse", "", false, 400},
		{"alt=sse&alt=sse", "", true, 400}, {"key=test-secret", "", true, 400},
		{"profile=test", "", false, 400}, {"bad=%zz", "", true, 400}, {strings.Repeat("x", 8193), "", true, 400},
		{"", "Authorization", false, 403}, {"", "x-api-key", false, 403}, {"", "x-goog-api-key", false, 403},
	} {
		request := httptest.NewRequest("POST", "/api/v1/teams/tem_one/models/public:streamGenerateContent", nil)
		request.URL.RawQuery = test.query
		request.RequestURI += "?" + test.query
		if test.header != "" {
			request.Header.Set(test.header, "test-secret")
		}
		err := validateTeamNativeInputs(request, test.stream)
		var gateway *service.GatewayError
		if test.status == 0 && err != nil || test.status != 0 && (!errors.As(err, &gateway) || gateway.Status != test.status) {
			t.Fatalf("validation status mismatch: %v", err)
		}
		if request.URL.RawQuery != "" || strings.Contains(request.RequestURI, "?") || request.Header.Get(test.header) != "" {
			t.Fatal("sensitive original inputs remained in request")
		}
		repeated := validateTeamNativeInputs(request, test.stream)
		if (repeated == nil) != (err == nil) {
			t.Fatal("scrubbing discarded the original denial")
		}
	}
}
func TestTeamNativeDefaultLoggerAndRecoveryRedaction(t *testing.T) {
	if os.Getenv("ROUTEX_TEAM_NATIVE_LOG_CHILD") == "1" {
		engine := fox.Default()
		engine.RedirectTrailingSlash = false
		engine.RedirectFixedPath = false
		engine.Engine.Use(TeamNativeInputs)
		engine.POST("/api/v1/teams/tem_one/responses", func(c *fox.Context) { panic("fixed test panic") })
		for _, path := range []string{"/api/v1/teams/tem_one/extra/messages", "/api/v1/teams/tem_one/messages/missing", "/api/v1/teams/tem_one/Responses", "/api/v1/teams/tem_one/responses/", "/api/v1/teams/tem_one/responses"} {
			request := httptest.NewRequest("POST", path+"?key=query-test-secret&bad=%zz", nil)
			request.Header.Set("x-api-key", "messages-test-secret")
			request.Header.Set("x-goog-api-key", "gemini-test-secret")
			request.Header.Set("Authorization", "Bearer bearer-test-secret")
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			if response.Header().Get("Location") != "" || strings.Contains(response.Body.String(), "test-secret") {
				t.Fatal("unsupported credentials reflected")
			}
			expected := 404
			if path == "/api/v1/teams/tem_one/responses" {
				expected = 500
			}
			if response.Code != expected {
				t.Fatalf("nonexact native status: %d", response.Code)
			}
		}
		return
	}
	command := exec.Command(os.Args[0], "-test.run=^TestTeamNativeDefaultLoggerAndRecoveryRedaction$", "-test.v")
	command.Env = append(os.Environ(), "ROUTEX_TEAM_NATIVE_LOG_CHILD=1")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("default engine child failed: %v %s", err, output)
	}
	for _, secret := range []string{"query-test-secret", "messages-test-secret", "gemini-test-secret", "bearer-test-secret"} {
		if strings.Contains(string(output), secret) {
			t.Fatal("logger/recovery exposed unsupported Team credential")
		}
	}
	if !strings.Contains(string(output), "/api/v1/teams/tem_one/Responses") || !strings.Contains(string(output), "panic recovered") {
		t.Fatal("missing real logger/recovery evidence")
	}
}
