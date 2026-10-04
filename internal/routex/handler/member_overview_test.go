package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/service"
)

func TestAdminMemberOverviewRejectsPathAndQuerySelectorsBeforeAuthority(t *testing.T) {
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.GET("/admin/members/:user_id/overview", New(nil).MemberOverview)
	for _, path := range []string{"usr_target?user_id=usr_other", "usr_target?team=tem_other", "usr_target?key=private", "usr_target?limit=1", "usr_target?x=1&x=2", "usr_target?bad=%zz", "usr_target?", "usr_target%20", strings.Repeat("x", 31), "usr_%E7%9B%AE%E6%A0%87"} {
		// A bare query marker contains no selector; test it in the valid case below.
		if path == "usr_target?" {
			continue
		}
		parts := strings.SplitN(path, "?", 2)
		url := "/admin/members/" + parts[0] + "/overview"
		if len(parts) == 2 {
			url += "?" + parts[1]
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, url, nil))
		if response.Code != http.StatusBadRequest || response.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal(path, response.Code, response.Header(), response.Body.String())
		}
	}
}

func TestAdminMemberOverviewExactPathAndResponseShape(t *testing.T) {
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	when := time.Date(2026, 10, 4, 1, 2, 3, 0, time.UTC)
	router.GET("/admin/members/:user_id/overview", func(c *fox.Context) error {
		subject, err := memberOverviewSubject(c)
		if err != nil {
			return err
		}
		c.JSON(http.StatusOK, service.MemberOverviewRecord{UserID: subject, ObservedAt: when, PlatformCurrency: "USD", TotalPersonalKeys: "9007199254740993", Personal: service.MemberOverviewMonthlyAccount{AccountID: "user:" + subject, PolicyETag: "0", UsageStatus: "unavailable"}})
		return nil
	})
	for _, id := range []string{"usr_legacy", "USR_CASE", "user-legacy"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/admin/members/"+id+"/overview?", nil))
		var value map[string]json.RawMessage
		if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil || response.Code != http.StatusOK || len(value) != 5 {
			t.Fatal(response.Code, response.Body.String(), err)
		}
		if string(value["user_id"]) != `"`+id+`"` || string(value["total_personal_keys"]) != `"9007199254740993"` || string(value["observed_at"]) != `"2026-10-04T01:02:03Z"` {
			t.Fatal(response.Body.String())
		}
		var account map[string]json.RawMessage
		if err := json.Unmarshal(value["personal"], &account); err != nil {
			t.Fatal(err)
		}
		for _, field := range []string{"tokens_month", "money_month", "currency", "usage", "active_reservations"} {
			if string(account[field]) != "null" {
				t.Fatal("unavailable or unlimited became zero", field, response.Body.String())
			}
		}
		if response.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal(response.Header())
		}
	}
}
