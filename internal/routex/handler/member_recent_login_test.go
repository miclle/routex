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

func TestMemberRecentLoginGetOnlyDTOAndListUTC(t *testing.T) {
	stamp := time.Date(2026, 10, 5, 1, 2, 3, 123456000, time.FixedZone("test", 3600))
	record := service.MemberRecord{User: entity.User{ID: "legacy_subject", Role: entity.RoleMember, LastLoginAt: &stamp}, RoleIDs: []string{}}
	for _, value := range []*time.Time{nil, &stamp} {
		record.User.LastLoginAt = value
		detail := &MemberDetailResponse{MemberResponse: *memberResponse(record), LastLoginAt: memberLoginTime(value), LastLoginStatus: memberLoginStatus(value)}
		raw, err := json.Marshal(detail)
		if err != nil {
			t.Fatal(err)
		}
		if value == nil {
			if !strings.Contains(string(raw), `"last_login_at":null`) || !strings.Contains(string(raw), `"last_login_status":"historical_unavailable"`) {
				t.Fatal(string(raw))
			}
		} else {
			if !strings.Contains(string(raw), `"last_login_at":"2026-10-05T00:02:03.123456Z"`) || !strings.Contains(string(raw), `"last_login_status":"recorded"`) {
				t.Fatal(string(raw))
			}
		}
		legacy, _ := json.Marshal(memberResponse(record))
		if strings.Contains(string(legacy), "last_login") {
			t.Fatal("mutation/create DTO changed")
		}
		page := &service.MemberListPage{Members: []service.MemberListSummary{{MemberRecord: record}}}
		row := memberListResponse(page).Items[0]
		if row.LastLoginStatus != detail.LastLoginStatus || (row.LastLoginAt == nil) != (value == nil) {
			t.Fatal(row.LastLoginStatus)
		}
	}
}
func TestMemberRecentLoginGetRejectsQueryAndUnsafePathBeforeAuthority(t *testing.T) {
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.GET("/members/:user_id", New(nil).GetMember)
	for _, path := range []string{"/members/usr_target?user_id=other", "/members/usr_target?limit=", "/members/usr_target%20", "/members/usr_%E7%9B%AE%E6%A0%87", "/members/" + strings.Repeat("x", 31)} {
		got := httptest.NewRecorder()
		router.ServeHTTP(got, httptest.NewRequest(http.MethodGet, path, nil))
		if got.Code != 400 || got.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal(path, got.Code)
		}
	}
}
