package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fox-gonic/fox"
	"github.com/gin-gonic/gin"
)

func TestAuthenticationPolicyQueryAdmissionAndEarlyRedaction(t *testing.T) {
	const admin = "usr_01arz3ndektsv4rrffq69g5fav"
	for _, test := range []struct {
		name, method, path string
		ok                 bool
		candidate          string
	}{
		{"public", "GET", "/api/v1/auth/policy", true, ""},
		{"no_candidate", "GET", "/api/v1/admin/auth/policy", true, ""},
		{"candidate", "GET", "/api/v1/admin/auth/policy?emergency_admin_id=" + admin, true, admin},
		{"duplicate_candidate", "GET", "/api/v1/admin/auth/policy?emergency_admin_id=" + admin + "&emergency_admin_id=" + admin, false, ""},
		{"empty", "GET", "/api/v1/admin/auth/policy?emergency_admin_id=", false, ""},
		{"empty_query", "GET", "/api/v1/admin/auth/policy?", false, ""},
		{"unknown", "GET", "/api/v1/admin/auth/policy?password=private-query", false, ""},
		{"public_proof", "GET", "/api/v1/auth/policy?code=private-query", false, ""},
		{"post_query", "POST", "/api/v1/auth/emergency/login?password=private-query", false, ""},
		{"unknown_emergency_path", "POST", "/api/v1/auth/emergency/unknown?challenge_token=private-query", false, ""},
		{"write_candidate", "PUT", "/api/v1/admin/auth/policy?emergency_admin_id=" + admin, false, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			router.Use(AuthenticationPolicyInputs)
			router.Any(strings.SplitN(test.path, "?", 2)[0], func(c *gin.Context) {
				if c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery || strings.Contains(c.Request.RequestURI, "?") {
					t.Error("query escaped redaction boundary")
				}
				v, ok := authenticationPolicyRequest(c.Request)
				if ok != test.ok || ok && v.candidate != test.candidate {
					t.Error("captured input admission mismatch")
				}
				c.Status(204)
			})
			r := httptest.NewRequest(test.method, "https://routex.test"+test.path, nil)
			out := httptest.NewRecorder()
			router.ServeHTTP(out, r)
			if out.Code != 204 || strings.Contains(out.Body.String(), "private-query") {
				t.Fatal("input material escaped")
			}
		})
	}
}
func TestEmergencyHTTPRejectsAmbiguousProofAndCrossSiteBeforeService(t *testing.T) {
	router := fox.New()
	New(nil).RegisterRoutes(router)
	for _, test := range []struct {
		name, path, body, origin string
		status                   int
	}{
		{"query", "/api/v1/auth/emergency/login?password=private-query", `{}`, "https://routex.test", 400},
		{"duplicate", "/api/v1/auth/emergency/login", `{"email":"a@example.invalid","email":"b@example.invalid","password":"private-password"}`, "https://routex.test", 400},
		{"null", "/api/v1/auth/emergency/login", `{"email":null,"password":"private-password"}`, "https://routex.test", 400},
		{"surrogate", "/api/v1/auth/emergency/login", `{"email":"a@example.invalid","password":"\ud800"}`, "https://routex.test", 400},
		{"both_factors", "/api/v1/auth/emergency/mfa/verify", `{"challenge_token":"` + strings.Repeat("A", 43) + `","code":"123456","recovery_code":"private-factor"}`, "https://routex.test", 400},
		{"cross_site", "/api/v1/auth/emergency/login", `{"email":"a@example.invalid","password":"private-password"}`, "https://other.invalid", 403},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "https://routex.test"+test.path, strings.NewReader(test.body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Origin", test.origin)
			out := httptest.NewRecorder()
			router.ServeHTTP(out, r)
			if out.Code != test.status {
				t.Fatalf("status=%d want=%d", out.Code, test.status)
			}
			if !strings.Contains(out.Header().Get("Cache-Control"), "no-store") || out.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal("private response headers missing")
			}
			for _, secret := range []string{"private-password", "private-factor", "private-query", strings.Repeat("A", 43)} {
				if strings.Contains(out.Body.String(), secret) {
					t.Fatal("input reflected in error")
				}
			}
		})
	}
}
