package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/service"
)

func TestAPIKeyDeliveryControllerMachineBearerDoesNotBorrowSessionOrInference(t *testing.T) {
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.GET("/machine", apiKeyDeliveryResponseHeaders, func(c *fox.Context) error {
		_, err := apiKeyDeliveryControllerBearer(c)
		if err != nil {
			return err
		}
		c.Status(204)
		return nil
	})
	for _, tc := range []struct {
		name, path string
		headers    []string
		status     int
	}{
		{"exact", "/machine", []string{"Bearer rxdctl_" + strings.Repeat("A", 43)}, 204},
		{"session_only", "/machine", nil, 401},
		{"inference", "/machine", []string{"Bearer rx_live_private"}, 401},
		{"vault", "/machine", []string{"Bearer hvs.private"}, 401},
		{"duplicate", "/machine", []string{"Bearer rxdctl_private", "Bearer rxdctl_other"}, 401},
		{"comma", "/machine", []string{"Bearer rxdctl_private,Bearer rxdctl_other"}, 401},
		{"lowercase", "/machine", []string{"bearer rxdctl_private"}, 401},
		{"extra_space", "/machine", []string{"Bearer  rxdctl_private"}, 401},
		{"query", "/machine?bearer=private", []string{"Bearer rxdctl_private"}, 400},
		{"force_query", "/machine?", []string{"Bearer rxdctl_private"}, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tc.path, nil)
			req.Header.Set("Cookie", "routex_session=unrelated-private-session")
			req.Header.Set("X-CSRF-Token", "unrelated-private-proof")
			for _, v := range tc.headers {
				req.Header.Add("Authorization", v)
			}
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			if res.Code != tc.status || res.Header().Get("Cache-Control") != "private, no-store" || res.Header().Get("X-Content-Type-Options") != "nosniff" || strings.Contains(res.Body.String(), "private") {
				t.Fatal("machine identity boundary failed", res.Code)
			}
		})
	}
}

func TestAPIKeyDeliveryControllerClosedJSON(t *testing.T) {
	good := `{"request_id":"00000000-0000-4000-8000-000000000001","name":"Controller","expires_at":"2026-10-20T00:00:00Z","opt_in":true,"descriptor_revision":"1","configuration_digest":"` + strings.Repeat("a", 64) + `","reason":"Reviewed exact configuration"}`
	for _, tc := range []struct {
		name, body string
		want       bool
	}{
		{"exact", good, true},
		{"missing_expiry", strings.Replace(good, `"expires_at":"2026-10-20T00:00:00Z",`, "", 1), false},
		{"null_expiry", strings.Replace(good, `"expires_at":"2026-10-20T00:00:00Z"`, `"expires_at":null`, 1), false},
		{"duplicate", strings.Replace(good, `"opt_in":true`, `"opt_in":true,"opt_in":false`, 1), false},
		{"plaintext", strings.TrimSuffix(good, "}") + `,"api_key":"private-inference-key"}`, false},
		{"script", strings.TrimSuffix(good, "}") + `,"command":"arbitrary-script"}`, false},
		{"numeric_revision", strings.Replace(good, `"descriptor_revision":"1"`, `"descriptor_revision":1`, 1), false},
		{"missing_opt_in", strings.Replace(good, `"opt_in":true,`, "", 1), false},
		{"trailing", good + `{}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var in service.APIKeyDeliveryControllerEnrollmentInput
			err := decodeAPIKeyDeliveryJSON([]byte(tc.body), &in)
			if (err == nil) != tc.want {
				t.Fatal("unexpected closed enrollment grammar result")
			}
		})
	}
	ack := `{"request_id":"00000000-0000-4000-8000-000000000001","descriptor_revision":"1","configuration_digest":"` + strings.Repeat("a", 64) + `","agent_state":"running","output_state":"written"}`
	var in service.APIKeyDeliveryControllerAcknowledgementInput
	if decodeAPIKeyDeliveryJSON([]byte(ack), &in) != nil || decodeAPIKeyDeliveryJSON([]byte(strings.TrimSuffix(ack, "}")+`,"completed":true}`), &in) == nil {
		t.Fatal("acknowledgement grammar accepts completion proof")
	}
}
