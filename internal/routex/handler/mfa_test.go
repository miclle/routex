package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fox-gonic/fox"
)

func TestMFAStrictBodies(t *testing.T) {
	// Malformed bodies must fail before session lookup or service access. A nil
	// service makes an accidental transition visible without a database fixture.
	ctrl := New(nil)
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.POST("/enrollment", jsonAuthRequest, ctrl.BeginMFAEnrollment)
	router.POST("/enable", jsonAuthRequest, ctrl.EnableMFA)
	router.POST("/recovery", jsonAuthRequest, ctrl.RegenerateMFARecoveryCodes)
	router.POST("/disable", jsonAuthRequest, ctrl.DisableMFA)
	router.POST("/verify", jsonAuthRequest, ctrl.CompleteMFALogin)
	for _, route := range []string{"/enrollment", "/enable", "/recovery", "/disable", "/verify"} {
		for _, body := range []string{`{"unexpected":"private-proof-must-not-leak"}`, `{} {"unexpected":"private-proof-must-not-leak"}`, `{} trailing-private-proof-must-not-leak`, `null`, `[]`, `"private-proof-must-not-leak"`, `{"current_password":123,"unexpected":"private-proof-must-not-leak"}`, strings.Repeat(" ", 4096) + `{}`} {
			request := httptest.NewRequest("POST", route, strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest || strings.Contains(response.Body.String(), "private-proof") {
				t.Fatalf("MFA body rejected incorrectly on %s: status=%d", route, response.Code)
			}
		}
	}
}
func TestMFADecodesKnownProofFields(t *testing.T) {
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.POST("/proof", jsonAuthRequest, func(c *fox.Context) error {
		input, err := decodeMFARequest[MFALoginRequest](c)
		if err != nil {
			return err
		}
		if input.ChallengeToken != "challenge" || input.RecoveryCode != "recovery" || input.Code != "" {
			t.Fatal("MFA request fields changed")
		}
		c.Status(http.StatusNoContent)
		return nil
	})
	request := httptest.NewRequest("POST", "/proof", strings.NewReader(`{"challenge_token":"challenge","recovery_code":"recovery"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("known proof fields rejected: %d", response.Code)
	}
}
