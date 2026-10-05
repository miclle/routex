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

func TestMemberAccessSummaryRejectsSelectorsBeforeAuthority(t *testing.T) {
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.GET("/members/:user_id/access", New(nil).MemberAccessSummary)
	for _, path := range []string{"usr_subject/access?role=rol_other", "usr_subject/access?team=tea_other", "usr_subject/access?limit=1", "usr_subject/access?x=1&x=2", "usr_subject/access?bad=%zz", "usr_subject%20/access", strings.Repeat("x", 31) + "/access", "usr_%E7%9B%AE%E6%A0%87/access"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", "/members/"+path, nil))
		if response.Code != http.StatusBadRequest || response.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal(path, response.Code, response.Body.String(), response.Header())
		}
	}
}

func TestMemberAccessSummarySixFieldShapeAndNullPrivacy(t *testing.T) {
	router := fox.New()
	router.GET("/members/:user_id/access", func(c *fox.Context) error {
		userID, err := memberOverviewSubject(c)
		if err != nil {
			return err
		}
		c.JSON(200, service.MemberAccessSummary{UserID: userID, ObservedAt: time.Date(2026, 10, 5, 0, 0, 0, 1, time.UTC), IdentityRole: "admin", Roles: service.MemberAccessRoles{Status: "not_authorized"}, Teams: service.MemberAccessTeams{Status: "available", Items: []service.MemberAccessTeam{}}})
		return nil
	})
	for _, userID := range []string{"usr_subject", "USR_LEGACY", "legacy-user"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", "/members/"+userID+"/access?", nil))
		var fields map[string]json.RawMessage
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &fields) != nil || len(fields) != 6 || string(fields["user_id"]) != `"`+userID+`"` || string(fields["updated_at"]) != "null" || string(fields["roles"]) != `{"status":"not_authorized","items":null}` || string(fields["teams"]) != `{"status":"available","items":[]}` {
			t.Fatal(response.Code, response.Body.String())
		}
		if response.Header().Get("Cache-Control") != "private, no-store" || response.Header().Get("ETag") != "" {
			t.Fatal(response.Header())
		}
	}
}
