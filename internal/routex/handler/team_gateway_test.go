package handler

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/service"
)

func TestTeamNativeRequestPreservesBodyBoundsAndTimeout(t *testing.T) {
	for _, tc := range []struct {
		name, contentType, body string
		status                  int
	}{
		{"native JSON", "application/json", `{"contents":[{"parts":[{"text":"literal"}]}]}`, 200},
		{"missing JSON type", "", `{}`, 415},
		{"unsupported type", "text/plain", `{}`, 415},
		{"oversized native body", "application/json", strings.Repeat("x", gatewayRequestLimit+1), 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := fox.New()
			ctrl := &Ctrl{service: &service.Service{}}
			router.POST("/native", func(c *fox.Context) {
				ctx, body, cancel, err := ctrl.teamGatewayRequest(c)
				if err != nil {
					writeGatewayError(c, err)
					return
				}
				defer cancel()
				if string(body) != tc.body {
					t.Error("native bytes changed")
				}
				deadline, ok := ctx.Deadline()
				remaining := time.Until(deadline)
				if !ok || remaining < 299*time.Second || remaining > 300*time.Second {
					t.Error("native timeout changed", remaining)
				}
				c.Status(http.StatusOK)
			})
			request := httptest.NewRequest(http.MethodPost, "/native", strings.NewReader(tc.body))
			if tc.contentType != "" {
				request.Header.Set("Content-Type", tc.contentType)
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code != tc.status {
				t.Fatal("native transport status", recorder.Code, tc.status, recorder.Body.String())
			}
		})
	}
}
func TestTeamNativePreparationRejectsAlternateAuthorityBeforeRuntime(t *testing.T) {
	for _, tc := range []struct{ name, value string }{
		{"Authorization", "Bearer private"}, {"x-api-key", "private"}, {"x-goog-api-key", "private"},
		{"OpenAI-Project", "project_other"}, {"OpenAI-Organization", "org_other"},
		{"anthropic-workspace-id", "workspace_other"}, {"anthropic-user-profile-id", "profile_other"},
		{"X-Team-ID", "tem_other"}, {"X-User-ID", "usr_other"}, {"X-Project-ID", "prj_other"},
		{"Origin", "https://cross-origin.invalid"}, {"Sec-Fetch-Site", "cross-site"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := fox.New()
			ctrl := &Ctrl{service: &service.Service{}}
			router.POST("/api/v1/teams/:team_id/messages", func(c *fox.Context) {
				_, requestID, err := ctrl.prepareTeamGateway(c, true)
				writeMessagesError(c, err, requestID)
			})
			request := httptest.NewRequest(http.MethodPost, "http://routex.local/api/v1/teams/tem_one/messages", strings.NewReader(`{}`))
			request.Header.Set(tc.name, tc.value)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusForbidden || strings.Contains(recorder.Body.String(), tc.value) {
				t.Fatal("alternate authority reached runtime or leaked", recorder.Code, recorder.Body.String())
			}
		})
	}
}

type teamNativeBrokenReader struct{}

func (teamNativeBrokenReader) Read([]byte) (int, error) { return 0, errors.New("private read failure") }
func (teamNativeBrokenReader) Close() error             { return nil }
func TestTeamNativeBodyReadFailureIsBoundedAndGeneric(t *testing.T) {
	router := fox.New()
	ctrl := &Ctrl{service: &service.Service{}}
	router.POST("/native", func(c *fox.Context) { _, _, _, err := ctrl.teamGatewayRequest(c); writeGatewayError(c, err) })
	request := httptest.NewRequest(http.MethodPost, "/native", nil)
	request.Header.Set("Content-Type", "application/json")
	request.Body = teamNativeBrokenReader{}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	body, _ := io.ReadAll(recorder.Result().Body)
	if recorder.Code != http.StatusBadRequest || strings.Contains(string(body), "private read failure") {
		t.Fatal("transport error leaked", recorder.Code, string(body))
	}
}
