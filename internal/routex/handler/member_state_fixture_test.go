package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/service"
)

type memberStateFixtureRecord struct {
	UserID                      string     `json:"user_id"`
	Name                        string     `json:"name"`
	BaseRole                    string     `json:"base_role"`
	Disabled                    bool       `json:"disabled"`
	OffboardedAt                *time.Time `json:"offboarded_at"`
	Status                      string     `json:"status"`
	CanChangeBaseRole           bool       `json:"can_change_base_role"`
	CanChangeStatus             bool       `json:"can_change_status"`
	ActivationMode              *string    `json:"activation_mode"`
	ETag                        string     `json:"etag"`
	AccountAccessRuntimeApplied bool       `json:"account_access_runtime_applied"`
}

type memberStateFixtureResult struct {
	memberStateFixtureRecord
	Confirmation string `json:"confirmation"`
	Effect       string `json:"effect"`
}

func memberStateFixtureReview(t *testing.T, router http.Handler, cookie *http.Cookie, userID string) memberStateFixtureRecord {
	t.Helper()
	response := identityRequest(router, "GET", "/api/v1/admin/members/"+userID+"/state", "", cookie, "")
	review := decodeCatalogResponse[memberStateFixtureRecord](t, response, 200)
	if review.UserID != userID || len(review.ETag) != 64 || response.Header().Get("ETag") != strconv.Quote(review.ETag) {
		t.Fatal("state review identity/validator mismatch", response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("state review is not private/no-store")
	}
	return review
}

func memberStateFixturePATCH(t *testing.T, router http.Handler, cookie *http.Cookie, csrf, userID, etag string, body any) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("PATCH", "http://routex.test/api/v1/admin/members/"+userID, strings.NewReader(string(encoded)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", strconv.Quote(etag))
	request.Header.Set("X-CSRF-Token", csrf)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code == 200 {
		result := decodeCatalogResponse[memberStateFixtureResult](t, response, 200)
		if result.UserID != userID || result.Confirmation != "current_member_state" || response.Header().Get("ETag") != strconv.Quote(result.ETag) {
			t.Fatal("state write lacks exact current confirmation", response.Body.String())
		}
		if result.Effect != "current_base_identity" && (result.Effect != "current_account_access" || !result.AccountAccessRuntimeApplied) {
			t.Fatal("state write falsely confirms current effect", response.Body.String())
		}
	}
	return response
}

func reviewedMemberStateFixtureRequest(t *testing.T, router http.Handler, cookie *http.Cookie, csrf, userID string, fields any) *httptest.ResponseRecorder {
	t.Helper()
	review := memberStateFixtureReview(t, router, cookie, userID)
	encoded, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(encoded, &body); err != nil {
		t.Fatal(err)
	}
	body["reason"] = "Reviewed member state fixture operation"
	return memberStateFixturePATCH(t, router, cookie, csrf, userID, review.ETag, body)
}

// A real started publisher supplies account lifecycle proof. This deliberately
// does not modify identityRouter or a predecessor fixture's stopped runtime.
func memberStateRuntimeFixtureRouter(t *testing.T, db *gorm.DB, options ...service.Option) (*fox.Engine, *service.Service) {
	t.Helper()
	svc, err := service.New(context.Background(), db, options...)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.StartRuntime(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(svc.StopRuntime)
	router := fox.New()
	New(svc).RegisterRoutes(router)
	return router, svc
}

func TestReviewedMemberStateFixturePreservesDispatchAndBody(t *testing.T) {
	for _, want := range []int{200, 403} {
		t.Run(strconv.Itoa(want), func(t *testing.T) {
			var reads, writes int
			etag := strings.Repeat("a", 64)
			cookie := &http.Cookie{Name: "fixture", Value: "test-only"}
			fields := map[string]any{"disabled": true}
			router := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got, err := r.Cookie(cookie.Name); err != nil || got.Value != cookie.Value {
					t.Fatal("review/dispatch lost original actor")
				}
				w.Header().Set("Cache-Control", "private, no-store")
				w.Header().Set("ETag", strconv.Quote(etag))
				if r.Method == "GET" {
					reads++
					if r.URL.Path != "/api/v1/admin/members/usr_fixture/state" {
						t.Fatal(r.URL.Path)
					}
					_ = json.NewEncoder(w).Encode(memberStateFixtureRecord{UserID: "usr_fixture", ETag: etag})
					return
				}
				writes++
				if r.Method != "PATCH" || r.URL.Path != "/api/v1/admin/members/usr_fixture" || r.Header.Get("If-Match") != strconv.Quote(etag) || r.Header.Get("X-CSRF-Token") != "current-csrf" {
					t.Fatal("fixture dispatched an unreviewed or different request")
				}
				var body map[string]any
				if json.NewDecoder(r.Body).Decode(&body) != nil || len(body) != 2 || body["disabled"] != true || body["reason"] != "Reviewed member state fixture operation" {
					t.Fatal("fixture changed desired state or lost reason", body)
				}
				w.WriteHeader(want)
				if want == 200 {
					_ = json.NewEncoder(w).Encode(memberStateFixtureResult{memberStateFixtureRecord: memberStateFixtureRecord{UserID: "usr_fixture", ETag: etag, AccountAccessRuntimeApplied: true}, Confirmation: "current_member_state", Effect: "current_account_access"})
				}
			})
			response := reviewedMemberStateFixtureRequest(t, router, cookie, "current-csrf", "usr_fixture", fields)
			if response.Code != want || reads != 1 || writes != 1 || len(fields) != 1 {
				t.Fatal("fixture skipped denied dispatch, replayed or mutated caller intent", response.Code, reads, writes, fields)
			}
		})
	}
}
