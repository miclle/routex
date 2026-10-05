package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
)

func TestMemberListInvalidQueryNeverReachesAuthentication(t *testing.T) {
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.GET("/members", New(nil).ListMembers)
	for _, q := range []string{"limit=", "limit=0", "limit=101", "limit=01", "limit=-1", "limit=1.5", "limit=1&limit=2", "user_id=private", "q=a&q=b", "q=%zz", "currency=USD", "cursor=a&cursor=b", "cursor=bad%2Fid", "cursor=bad+", "status=Active", "role=Admin", "q=%FF", "q=" + strings.Repeat("x", 201)} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", "/members?"+q, nil))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("ambiguous list query reached authority/service: %q status=%d", q, response.Code)
		}
		if response.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal(response.Header())
		}
	}
}
func TestMemberListSupportedEmptyFiltersAndLimit(t *testing.T) {
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.GET("/members", func(c *fox.Context) error {
		f, err := memberListFilter(c)
		if err != nil {
			return err
		}
		c.JSON(200, f)
		return nil
	})
	for _, query := range []string{"", "q=&status=&role=&cursor=", "limit=1", "limit=100&cursor=Z_legacy"} {
		r := httptest.NewRecorder()
		router.ServeHTTP(r, httptest.NewRequest("GET", "/members?"+query, nil))
		if r.Code != 200 {
			t.Fatal(query, r.Code)
		}
	}
}
func TestMemberListResponsePreservesDetailAndSafeSummary(t *testing.T) {
	now := time.Now().UTC()
	before := service.MemberRecord{User: entity.User{ID: "usr_one", Name: "Legacy\nlabel", Email: "one@example.test", Role: "member", CreatedAt: now, UpdatedAt: now, PasswordHash: "DO_NOT_EXPOSE"}, RoleIDs: []string{"rol_one"}}
	detail, _ := json.Marshal(memberResponse(before))
	page := &service.MemberListPage{ActorUserID: "usr_reader", ObservedAt: now, PlatformCurrency: "USD", Members: []service.MemberListSummary{{MemberRecord: before, TotalPersonalKeys: "9007199254740993", Personal: service.MemberOverviewMonthlyAccount{AccountID: "user_usr_one", PolicyETag: "0", UsageStatus: "unavailable"}, Teams: service.MemberListTeams{Status: "not_authorized"}}}}
	raw, err := json.Marshal(memberListResponse(page))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"last_login_at":null`, `"last_login_status":"historical_unavailable"`, `"total_personal_keys":"9007199254740993"`, `"personal_policy_stored":false`, `"items":null`, `"active_reservations":null`} {
		if !strings.Contains(string(raw), expected) {
			t.Fatal(expected, string(raw))
		}
	}
	for _, secret := range []string{"DO_NOT_EXPOSE", "password", "token_hash", "key_id", "membership_id"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal(secret)
		}
	}
	var old map[string]any
	var current struct{ Items []map[string]any }
	_ = json.Unmarshal(detail, &old)
	_ = json.Unmarshal(raw, &current)
	for key, value := range old {
		if !jsonEqualMemberList(value, current.Items[0][key]) {
			t.Fatal("list lost existing detail field", key)
		}
	}
}
func jsonEqualMemberList(a, b any) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}
