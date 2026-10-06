package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/service"
)

func TestPersonalMonthlyBehaviorStrictHTTPInput(t *testing.T) {
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.PUT("/limits", func(c *fox.Context) error {
		var input service.LimitInput
		if err := decodeStrictRequest(c, &input); err != nil {
			return err
		}
		c.Status(204)
		return nil
	})
	for _, test := range []struct {
		body   string
		status int
	}{
		{`{"reason":"legacy"}`, 204},
		{`{"tokens_month":0,"money_month":null,"tokens_month_behavior":"alert_only","money_month_behavior":"stop","reason":"review"}`, 204},
		{`{"tokens_month_behavior":null}`, 400},
		{`{"money_month_behavior":[]}`, 400},
		{`{"tokens_month_behavior":"ALERT_ONLY"}`, 400},
		{`{"tokens_month_behavior":"alert_only "}`, 400},
		{`{"tokens_month_behavior":"stop","tokens_month_behavior":"alert_only"}`, 400},
		{`{"reason":"r","reason":"other"}`, 400},
		{`{"tokens_month_behavior":"alert_only","unrecognized":1}`, 400},
		{`{} {}`, 400},
	} {
		out := httptest.NewRecorder()
		router.ServeHTTP(out, httptest.NewRequest("PUT", "/limits", strings.NewReader(test.body)))
		if out.Code != test.status {
			t.Fatal("strict mode intent status", test.body, out.Code)
		}
	}
}
