package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fox-gonic/fox"
)

func TestCredentialAttemptStatisticsStrictQueryAndPrivateErrors(t *testing.T) {
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.GET("/providers/:provider_id/statistics", func(c *fox.Context) error {
		ids, err := credentialAttemptStatisticsRequest(c)
		if err != nil {
			return err
		}
		if len(ids) != 2 || ids[0] != "crd_b" || ids[1] != "crd_a" {
			t.Fatal(ids)
		}
		c.Status(204)
		return nil
	})
	for _, tc := range []struct {
		target, body string
		want         int
	}{
		{"prv_target/statistics?credential_id=crd_b&credential_id=crd_a", "", 204},
		{"prv_target/statistics", "", 400},
		{"PRV_TARGET/statistics?credential_id=crd_b", "", 400},
		{"prv_target/statistics?credential_id=CRD_B", "", 400},
		{"prv_target/statistics?credential_id=crd_b&credential_id=crd_b", "", 400},
		{"prv_target/statistics?credential_id=crd_b&q=x", "", 400},
		{"prv_target/statistics?credential_id=crd_b;credential_id=crd_a", "", 400},
		{"prv_target/statistics?credential_id=%zz", "", 400},
		{"prv_target/statistics?credential_id=crd_b%20", "", 400},
		{"prv_target/statistics?credential_id=crd_b", "{}", 400},
		{"prv_target/statistics?" + strings.Repeat("credential_id=crd_b&", 21), "", 400},
		{"prv_target/statistics?credential_id=" + strings.Repeat("a", 2048), "", 400},
	} {
		req := httptest.NewRequest("GET", "/providers/"+tc.target, strings.NewReader(tc.body))
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		if out.Code != tc.want || out.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal(tc.target, out.Code, out.Body.String())
		}
	}
}
